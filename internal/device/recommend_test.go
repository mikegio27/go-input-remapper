package device

import (
	"strings"
	"testing"
)

func infoOf(name string, kind Kind, hasAbs, virtual bool) Info {
	return Info{
		Identity:  Identity{Name: name},
		Kind:      kind,
		Caps:      Caps{HasAbs: hasAbs},
		IsVirtual: virtual,
	}
}

func TestRecommend(t *testing.T) {
	tests := []struct {
		name           string
		info           Info
		wantRemappable bool
		wantReasonHas  string
	}{
		{"keyboard", infoOf("kbd", KindKeyboard, false, false), true, "keyboard"},
		{"mouse", infoOf("mouse", KindMouse, false, false), true, "mouse"},
		{"gamepad", infoOf("pad", KindGamepad, true, false), true, "EV_ABS"},
		{"touchpad not recommended", infoOf("tp", KindTouchpad, true, false), false, "EV_ABS"},
		{"unknown not recommended", infoOf("?", KindUnknown, false, false), false, "unrecognized"},
		{"virtual device hard-blocked", infoOf("go-input-remapper kbd", KindKeyboard, false, true), false, "feedback loop"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Recommend([]Info{tc.info})
			if len(got) != 1 {
				t.Fatalf("Recommend returned %d results, want 1", len(got))
			}
			r := got[0]
			if r.Remappable != tc.wantRemappable {
				t.Errorf("Remappable = %v, want %v", r.Remappable, tc.wantRemappable)
			}
			joined := strings.Join(r.Reasons, " | ")
			if !strings.Contains(joined, tc.wantReasonHas) {
				t.Errorf("reasons %q do not mention %q", joined, tc.wantReasonHas)
			}
		})
	}
}

func TestIsVirtualName(t *testing.T) {
	const prefix = "go-input-remapper"
	cases := map[string]bool{
		"go-input-remapper kbd": true,
		"Keychron K8 Pro":       false,
		"":                      false,
	}
	for name, want := range cases {
		if got := IsVirtualName(name, prefix); got != want {
			t.Errorf("IsVirtualName(%q) = %v, want %v", name, got, want)
		}
	}
	if IsVirtualName("anything", "") {
		t.Error("empty prefix should match nothing")
	}
}
