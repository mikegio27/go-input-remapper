package device

import (
	"errors"
	"io/fs"

	evdev "github.com/mikegio27/go-evdev"
)

// DefaultVirtualPrefix is the default device-name prefix for go-input-remapper's
// virtual output devices, used by the feedback-loop guard. The config may
// override it, but the default lives here next to IsVirtualName, its main user.
const DefaultVirtualPrefix = "go-input-remapper"

// InspectAll opens every evdev device node and inspects the ones it can read,
// silently skipping nodes that fail with a permission error (so an unprivileged
// caller still gets a partial list, like evdev.ListDevices). Other open errors
// abort. The returned Info slice carries no open handles — each device is
// inspected and closed.
func InspectAll(virtualPrefix string) ([]Info, error) {
	paths, err := evdev.ListDevicePaths()
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, path := range paths {
		d, err := evdev.Open(path)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				continue
			}
			return nil, err
		}
		info, err := Inspect(d, virtualPrefix)
		d.Close()
		if err != nil {
			// A device that opened but won't answer its ioctls (racing
			// removal, odd driver) is skipped rather than failing the listing.
			continue
		}
		out = append(out, info)
	}
	return out, nil
}
