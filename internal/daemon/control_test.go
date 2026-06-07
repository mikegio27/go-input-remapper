package daemon

import (
	"path/filepath"
	"testing"

	"github.com/mikegio27/go-input-remapper/internal/config"
	"github.com/mikegio27/go-input-remapper/internal/control"
)

// startTestServer writes a two-profile config, applies it, and starts a control
// server on a temp socket. It returns a connected client.
func startTestServer(t *testing.T) (*control.Client, string) {
	t.Helper()
	dir := t.TempDir()

	cfg := &config.Config{
		ActiveProfile: "default",
		VirtualPrefix: "go-input-remapper",
		Profiles: map[string]*config.Profile{
			"default": {Name: "default", Devices: []config.DeviceBinding{
				{Match: config.DeviceMatcher{Name: "Nonexistent Keyboard"}},
			}},
			"gaming": {Name: "gaming", Devices: []config.DeviceBinding{
				{Match: config.DeviceMatcher{Name: "Nonexistent Mouse"}},
			}},
		},
	}
	if err := config.Save(dir, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	sup := NewSupervisor()
	sup.Apply(cfg, cfg.ActiveProfileOrNil())
	t.Cleanup(sup.Shutdown)

	sock := filepath.Join(dir, "sock")
	cs, err := newControlServer(sock, sup, dir)
	if err != nil {
		t.Fatalf("newControlServer: %v", err)
	}
	go cs.serve()
	t.Cleanup(func() { cs.close() })

	client, err := control.Dial(sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client, dir
}

func TestControlStatusAndSetProfile(t *testing.T) {
	c, dir := startTestServer(t)

	var st control.StatusResult
	if err := c.Call(control.MethodStatus, nil, &st); err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.ActiveProfile != "default" {
		t.Errorf("active profile = %q, want default", st.ActiveProfile)
	}

	// Switch to gaming; status should reflect it and config.toml should persist it.
	if err := c.Call(control.MethodSetProfile, control.SetProfileParams{Profile: "gaming"}, &st); err != nil {
		t.Fatalf("set_profile: %v", err)
	}
	if st.ActiveProfile != "gaming" {
		t.Errorf("after set_profile, active = %q, want gaming", st.ActiveProfile)
	}
	reloaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ActiveProfile != "gaming" {
		t.Errorf("config.toml active_profile = %q, want gaming (not persisted)", reloaded.ActiveProfile)
	}

	// Unknown profile must error.
	if err := c.Call(control.MethodSetProfile, control.SetProfileParams{Profile: "ghost"}, &st); err == nil {
		t.Error("expected error switching to nonexistent profile")
	}
}

func TestControlReloadAndListDevices(t *testing.T) {
	c, _ := startTestServer(t)

	var rr control.ReloadResult
	if err := c.Call(control.MethodReload, nil, &rr); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !rr.OK {
		t.Errorf("reload not OK: %v", rr.Errors)
	}

	// list_devices should succeed (the device set itself depends on the host).
	var ld control.ListDevicesResult
	if err := c.Call(control.MethodListDevices, nil, &ld); err != nil {
		t.Fatalf("list_devices: %v", err)
	}
}

func TestControlUnknownMethod(t *testing.T) {
	c, _ := startTestServer(t)
	if err := c.Call("bogus", nil, nil); err == nil {
		t.Error("expected error for unknown method")
	}
}
