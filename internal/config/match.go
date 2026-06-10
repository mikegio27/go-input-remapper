package config

import "github.com/mikegio27/nereus/internal/device"

// Matches reports whether a device identity satisfies the matcher. Every
// criterion the matcher specifies must equal the device's value (logical AND);
// unset fields are ignored. An empty matcher matches nothing.
//
// Use the strongest stable identifiers you have: Uniq is unique per physical
// device when the driver reports it; Vendor+Product+Name identifies a model
// (and matches every unit of it); Phys pins a particular port.
func (m DeviceMatcher) Matches(id device.Identity) bool {
	if m.IsEmpty() {
		return false
	}
	if m.Uniq != "" && m.Uniq != id.Uniq {
		return false
	}
	if m.Name != "" && m.Name != id.Name {
		return false
	}
	if m.Vendor != 0 && uint16(m.Vendor) != id.Vendor {
		return false
	}
	if m.Product != 0 && uint16(m.Product) != id.Product {
		return false
	}
	if m.Phys != "" && m.Phys != id.Phys {
		return false
	}
	return true
}
