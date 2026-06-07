// Package device provides read-only inspection of evdev input devices: stable
// identity, type classification, and remap recommendations. It never grabs a
// device or writes uinput — that belongs to the engine package. Both the daemon
// (matcher resolution) and the TUI (recommendations) build on it.
package device

import (
	evdev "github.com/mikegio27/go-evdev"
)

// Identity is the stable, human-meaningful description of a device, used both to
// display it and to match it across reboots (the /dev/input/eventX path is not
// stable, so matching keys off Name/Vendor/Product/Uniq/Phys instead). Path is
// the current node and is informational only.
type Identity struct {
	Path    string
	Name    string
	Vendor  uint16
	Product uint16
	Version uint16
	Bus     evdev.BusType
	Uniq    string
	Phys    string
}

// ReadIdentity collects a device's identity via the evdev ioctls. A device whose
// individual queries fail (e.g. a transient node) yields an error; callers
// enumerating many devices typically skip those.
func ReadIdentity(d *evdev.Device) (Identity, error) {
	id, err := d.ID()
	if err != nil {
		return Identity{}, err
	}
	name, err := d.Name()
	if err != nil {
		return Identity{}, err
	}
	// Uniq and Phys are optional metadata; not every driver sets them, and some
	// return an error rather than an empty string. Treat failures as absent
	// rather than fatal, since identity is still usable without them.
	uniq, _ := d.Uniq()
	phys, _ := d.Phys()

	return Identity{
		Path:    d.Path(),
		Name:    name,
		Vendor:  id.Vendor,
		Product: id.Product,
		Version: id.Version,
		Bus:     id.BusType,
		Uniq:    uniq,
		Phys:    phys,
	}, nil
}
