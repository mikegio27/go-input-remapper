package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/user"
	"strconv"

	"github.com/mikegio27/nereus/internal/config"
	"github.com/mikegio27/nereus/internal/control"
	"github.com/mikegio27/nereus/internal/device"
)

// controlServer serves the control protocol on a Unix socket, translating client
// requests into supervisor and config operations.
type controlServer struct {
	ln        net.Listener
	sup       *Supervisor
	configDir string
	path      string
}

// newControlServer binds the Unix socket at path, replacing any stale socket
// file left by a previous run.
func newControlServer(path string, sup *Supervisor, configDir string) (*controlServer, error) {
	// A leftover socket file from a crash would make Listen fail with EADDRINUSE.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// 0660 so the owner and the socket's group can reach it.
	if err := os.Chmod(path, 0o660); err != nil {
		ln.Close()
		return nil, err
	}
	// When running as the system service (root), hand group ownership to `input`
	// so the TUI/CLI — run by a user in that group — can connect. Best-effort:
	// ignored when not root or the group is absent (e.g. a --user service, where
	// the socket already belongs to the user).
	if grp, err := user.LookupGroup("input"); err == nil {
		if gid, err := strconv.Atoi(grp.Gid); err == nil {
			_ = os.Chown(path, -1, gid)
		}
	}
	return &controlServer{ln: ln, sup: sup, configDir: configDir, path: path}, nil
}

// serve accepts connections until the listener is closed.
func (s *controlServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // listener closed on shutdown
		}
		go s.handle(conn)
	}
}

// close stops the server and removes the socket file.
func (s *controlServer) close() error {
	err := s.ln.Close()
	os.Remove(s.path)
	return err
}

// handle processes newline-delimited requests on one connection until it closes.
func (s *controlServer) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req control.Request
		if err := json.Unmarshal(line, &req); err != nil {
			writeResponse(conn, control.Response{OK: false, Error: "bad request: " + err.Error()})
			continue
		}
		// Capture takes over the connection for a streamed session; everything
		// else is a single request/response.
		if req.Method == control.MethodCapture {
			s.handleCapture(conn, r, req)
			continue
		}
		writeResponse(conn, s.dispatch(req))
	}
}

// dispatch routes one request to its handler and packages the result.
func (s *controlServer) dispatch(req control.Request) control.Response {
	result, err := s.handleMethod(req)
	if err != nil {
		return control.Response{ID: req.ID, OK: false, Error: err.Error()}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return control.Response{ID: req.ID, OK: false, Error: "marshal result: " + err.Error()}
	}
	return control.Response{ID: req.ID, OK: true, Result: raw}
}

func (s *controlServer) handleMethod(req control.Request) (any, error) {
	switch req.Method {
	case control.MethodStatus:
		return s.status(), nil
	case control.MethodListDevices:
		return s.listDevices(), nil
	case control.MethodReload:
		return s.reload(), nil
	case control.MethodSetProfile:
		var p control.SetProfileParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, fmt.Errorf("bad params: %w", err)
		}
		return s.setProfile(p.Profile)
	default:
		return nil, fmt.Errorf("unknown method %q", req.Method)
	}
}

func (s *controlServer) status() control.StatusResult {
	profile, bound := s.sup.Snapshot()
	engines := make([]control.EngineInfo, 0, len(bound))
	for _, b := range bound {
		engines = append(engines, control.EngineInfo{Path: b.Path, Name: b.Name})
	}
	return control.StatusResult{ActiveProfile: profile, Engines: engines}
}

func (s *controlServer) listDevices() control.ListDevicesResult {
	prefix := s.sup.VirtualPrefix()
	_, bound := s.sup.Snapshot()
	boundPaths := make(map[string]bool, len(bound))
	for _, b := range bound {
		boundPaths[b.Path] = true
	}

	infos, err := device.InspectAll(prefix)
	if err != nil {
		slog.Warn("list_devices: enumerate failed", "err", err)
	}
	recs := device.Recommend(infos)

	out := make([]control.DeviceInfo, 0, len(recs))
	for _, r := range recs {
		out = append(out, control.DeviceInfo{
			Path:        r.Info.Identity.Path,
			Name:        r.Info.Identity.Name,
			Kind:        r.Info.Kind.String(),
			Vendor:      r.Info.Identity.Vendor,
			Product:     r.Info.Identity.Product,
			Bound:       boundPaths[r.Info.Identity.Path],
			Recommended: r.Remappable,
			Primary:     r.Primary,
			IsVirtual:   r.Info.IsVirtual,
			Reasons:     r.Reasons,
		})
	}
	return control.ListDevicesResult{Devices: out}
}

func (s *controlServer) reload() control.ReloadResult {
	problems := applyFromDisk(s.configDir, s.sup)
	if len(problems) > 0 {
		msgs := make([]string, len(problems))
		for i, p := range problems {
			msgs[i] = p.Error()
		}
		slog.Error("reload (control) rejected; keeping current config", "problems", errors.Join(problems...))
		return control.ReloadResult{OK: false, Errors: msgs}
	}
	slog.Info("config reloaded via control socket")
	return control.ReloadResult{OK: true}
}

// setProfile switches the active profile: it updates and persists config.toml
// (keeping config files the source of truth) and applies the change immediately.
func (s *controlServer) setProfile(name string) (control.StatusResult, error) {
	cfg, err := config.Load(s.configDir)
	if err != nil {
		return control.StatusResult{}, err
	}
	profile, ok := cfg.Profiles[name]
	if !ok {
		return control.StatusResult{}, fmt.Errorf("no such profile %q", name)
	}
	cfg.ActiveProfile = name
	if err := config.SaveGlobal(s.configDir, cfg); err != nil {
		return control.StatusResult{}, fmt.Errorf("persist active profile: %w", err)
	}
	s.sup.Apply(cfg, profile)
	slog.Info("active profile set via control socket", "profile", name)
	return s.status(), nil
}

// writeResponse marshals and writes one response line, ignoring write errors
// (the client may have disconnected).
func writeResponse(conn net.Conn, resp control.Response) {
	line, err := json.Marshal(resp)
	if err != nil {
		return
	}
	conn.Write(append(line, '\n'))
}
