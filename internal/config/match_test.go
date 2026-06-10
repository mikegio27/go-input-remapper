package config

import (
	"testing"

	"github.com/mikegio27/nereus/internal/device"
)

func TestMatcherMatches(t *testing.T) {
	id := device.Identity{
		Name:    "Keychron K8 Pro",
		Vendor:  0x3434,
		Product: 0x0280,
		Uniq:    "AA:BB:CC",
		Phys:    "usb-0000:00:14.0-1/input0",
	}

	tests := []struct {
		name    string
		matcher DeviceMatcher
		want    bool
	}{
		{"empty matches nothing", DeviceMatcher{}, false},
		{"name only", DeviceMatcher{Name: "Keychron K8 Pro"}, true},
		{"wrong name", DeviceMatcher{Name: "Other"}, false},
		{"vendor+product", DeviceMatcher{Vendor: 0x3434, Product: 0x0280}, true},
		{"wrong product", DeviceMatcher{Vendor: 0x3434, Product: 0x9999}, false},
		{"uniq match", DeviceMatcher{Uniq: "AA:BB:CC"}, true},
		{"uniq mismatch", DeviceMatcher{Uniq: "ZZ"}, false},
		{"phys match", DeviceMatcher{Phys: "usb-0000:00:14.0-1/input0"}, true},
		{"all fields AND, one wrong", DeviceMatcher{Name: "Keychron K8 Pro", Vendor: 0x3434, Product: 0x9999}, false},
		{"all fields AND, all right", DeviceMatcher{Name: "Keychron K8 Pro", Vendor: 0x3434, Product: 0x0280, Uniq: "AA:BB:CC"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.matcher.Matches(id); got != tc.want {
				t.Errorf("Matches() = %v, want %v", got, tc.want)
			}
		})
	}
}
