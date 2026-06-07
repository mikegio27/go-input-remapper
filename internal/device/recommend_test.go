package device

import (
	"strings"
	"testing"

	evdev "github.com/mikegio27/go-evdev"
)

// kbNode builds a keyboard Info with nkeys capability codes, for primary-node tests.
func kbNode(path, uniq string, vendor, product uint16, nkeys int) Info {
	keys := map[evdev.EvCode]bool{}
	for i := range nkeys {
		keys[evdev.EvCode(i)] = true
	}
	return Info{
		Identity: Identity{Path: path, Name: "kbd", Uniq: uniq, Vendor: vendor, Product: product},
		Kind:     KindKeyboard,
		Caps:     Caps{Keys: keys},
	}
}

func TestMarkPrimaryByUniq(t *testing.T) {
	// Two nodes of one physical keyboard (shared Uniq); the richer one wins.
	recs := Recommend([]Info{
		kbNode("/dev/input/event5", "u1", 1, 1, 8),   // sparse consumer-style node
		kbNode("/dev/input/event3", "u1", 1, 1, 120), // full keyboard node
	})
	if recs[0].Primary {
		t.Error("sparse node should not be primary")
	}
	if !recs[1].Primary {
		t.Error("full keyboard node should be primary")
	}
	if joined := strings.Join(recs[0].Reasons, " | "); !strings.Contains(joined, "/dev/input/event3") {
		t.Errorf("secondary node should point at the primary path; reasons: %q", joined)
	}
}

func TestMarkPrimaryFallbackByModel(t *testing.T) {
	// No Uniq: nodes group by vendor:product:name instead.
	recs := Recommend([]Info{
		kbNode("/dev/input/event3", "", 0x046d, 0xc31c, 130),
		kbNode("/dev/input/event9", "", 0x046d, 0xc31c, 4),
	})
	if !recs[0].Primary || recs[1].Primary {
		t.Errorf("expected event3 (richer) primary; got primary flags %v, %v", recs[0].Primary, recs[1].Primary)
	}
}

func TestMarkPrimarySingleNode(t *testing.T) {
	// A lone node has nothing to disambiguate, so it should NOT be starred —
	// otherwise every single-node device gets a star and the marker is noise.
	recs := Recommend([]Info{kbNode("/dev/input/event3", "solo", 1, 1, 120)})
	if recs[0].Primary {
		t.Error("a single-node device should not be flagged primary")
	}
	if joined := strings.Join(recs[0].Reasons, " | "); strings.Contains(joined, "multiple event nodes") {
		t.Errorf("single-node device should not mention multiple nodes; reasons: %q", joined)
	}
}

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
