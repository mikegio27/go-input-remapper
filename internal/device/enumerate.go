package device

import (
	"log/slog"

	evdev "github.com/mikegio27/go-evdev"
)

// DefaultVirtualPrefix is the default device-name prefix for go-input-remapper's
// virtual output devices, used by the feedback-loop guard. The config may
// override it, but the default lives here next to IsVirtualName, its main user.
const DefaultVirtualPrefix = "go-input-remapper"

// InspectAll opens every evdev device node and inspects the ones it can read,
// skipping any node it can't (permission denied, or a node that vanished or won't
// answer its ioctls — common when a wireless device re-enumerates as the daemon
// grabs it). Enumeration is best-effort per node: one bad node never fails the
// whole listing. Only a failure to list the nodes at all is returned as an error.
// The returned Info slice carries no open handles — each device is inspected and
// closed.
func InspectAll(virtualPrefix string) ([]Info, error) {
	paths, err := evdev.ListDevicePaths()
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, path := range paths {
		d, err := evdev.Open(path)
		if err != nil {
			// Permission errors are expected for an unprivileged caller; others
			// (ENODEV/ENOENT from a node that disappeared) are transient. Either
			// way, skip this node and keep going so the rest of the list survives.
			slog.Debug("inspect: skipping unreadable device", "path", path, "err", err)
			continue
		}
		info, err := Inspect(d, virtualPrefix)
		d.Close()
		if err != nil {
			slog.Debug("inspect: skipping device that won't answer ioctls", "path", path, "err", err)
			continue
		}
		out = append(out, info)
	}
	return out, nil
}
