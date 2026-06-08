package engine

// Profiling harness for the daemon's per-event hot path.
//
// BenchmarkEngineLoop* drive the REAL loop end to end: a uinput "feeder" device
// stands in for physical hardware, a live Engine grabs it (New → Run), and we read
// the engine's virtual-device output to know when each event has flowed all the way
// through read → transform → inject. This exercises every syscall on the path, not
// just the pure logic.
//
// It needs write access to /dev/uinput and read access to the resulting
// /dev/input/event* nodes (input group / root, or a uaccess seat). Where that's
// unavailable the loop benchmarks skip; BenchmarkProcess (pure CPU, no devices)
// always runs.
//
// Capture a CPU profile of the hot path:
//
//	go test ./internal/engine -run '^$' -bench BenchmarkEngineLoopRemap \
//	    -benchmem -cpuprofile /tmp/engine.cpu -memprofile /tmp/engine.mem
//	go tool pprof -top /tmp/engine.cpu
//	go tool pprof -http=: /tmp/engine.cpu   # flamegraph in a browser

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	evdev "github.com/mikegio27/go-evdev"
	"github.com/mikegio27/go-input-remapper/internal/config"
)

// feedWindow caps how far the feeder runs ahead of the engine so the kernel's
// per-reader event buffer can't overflow (which would drop events via SYN_DROPPED
// and stall the benchmark). Each iteration injects a KEY plus a SYN, so the raw
// kernel-event backlog is ~2× this — keep it well under the evdev client buffer.
const feedWindow = 16

// uinputWritable reports whether /dev/uinput can be opened for writing; the loop
// benchmarks need it to synthesize a source device.
func uinputWritable() bool {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// findNodeByName scans /dev/input for the device node exposing the given name,
// retrying briefly while udev creates it. Returns nil if it can't be opened (no
// read access) so callers can skip.
func findNodeByName(name string) *evdev.Device {
	for range 100 {
		paths, err := evdev.ListDevicePaths()
		if err == nil {
			for _, p := range paths {
				d, err := evdev.Open(p)
				if err != nil {
					continue
				}
				if n, err := d.Name(); err == nil && n == name {
					return d
				}
				d.Close()
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// benchLoop runs the shared end-to-end benchmark for a given binding. It feeds
// b.N EV_KEY events through a live engine and waits for each to surface on the
// engine's output, timing the whole round trip.
func benchLoop(b *testing.B, binding config.DeviceBinding) {
	if !uinputWritable() {
		b.Skip("need write access to /dev/uinput (input group / root)")
	}
	nonce := time.Now().UnixNano()
	feederName := name32("gir-bench-feeder", nonce)
	outName := name32("go-input-remapper gir-bench", nonce)

	// The feeder stands in for the physical device the daemon would grab.
	id := evdev.InputID{BusType: evdev.BUS_USB, Vendor: 0x9991, Product: 0x9992, Version: 1}
	feeder, err := evdev.CreateVirtualDevice(feederName, id, evdev.Capabilities{
		Keys: []evdev.EvCode{evdev.KEY_A, evdev.KEY_B, evdev.KEY_C, evdev.KEY_LEFTCTRL},
	})
	if err != nil {
		b.Fatalf("create feeder: %v", err)
	}
	defer feeder.Close()

	src := findNodeByName(feederName)
	if src == nil {
		b.Skip("cannot read the feeder's event node (need input group / root / uaccess)")
	}
	// engine.New takes ownership of src (grabs it, closes it on Close).
	eng, err := New(src, binding, outName)
	if err != nil {
		src.Close()
		b.Fatalf("engine.New: %v", err)
	}
	runErr := make(chan error, 1)
	go func() { runErr <- eng.Run() }()
	defer func() {
		eng.Close()
		<-runErr
	}()

	// Read the engine's output so we know when events have completed the loop.
	out := findNodeByName(outName)
	if out == nil {
		b.Skip("cannot read the engine's output node (need input group / root / uaccess)")
	}
	defer out.Close()

	var received int64
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			ev, err := out.ReadOne()
			if err != nil {
				return
			}
			if ev.Type == evdev.EV_KEY {
				atomic.AddInt64(&received, 1)
			}
		}
	}()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Alternate press/release so each iteration is a distinct key transition.
		val := int32(i&1 ^ 1) // 1,0,1,0,…
		_ = feeder.WriteEvent(evdev.EV_KEY, evdev.KEY_A, val)
		_ = feeder.Sync()
		// Backpressure: never let the feeder outrun the engine past the window.
		for int64(i)-atomic.LoadInt64(&received) > feedWindow {
			time.Sleep(20 * time.Microsecond)
		}
	}
	// Wait for the tail to drain (bounded, so a dropped event can't hang forever).
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt64(&received) < int64(b.N) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Microsecond)
	}
	b.StopTimer()

	if got := atomic.LoadInt64(&received); got < int64(b.N) {
		b.Fatalf("only %d/%d events completed the loop (kernel buffer drops?)", got, b.N)
	}
}

// BenchmarkEngineLoopPassthrough measures the loop with no remaps/macros: every
// EV_KEY is read, run through the (empty) ruleset, and re-injected unchanged.
func BenchmarkEngineLoopPassthrough(b *testing.B) {
	benchLoop(b, config.DeviceBinding{})
}

// BenchmarkEngineLoopRemap measures the loop with a single key swap, the common
// real-world case (the transform actually rewrites the event).
func BenchmarkEngineLoopRemap(b *testing.B) {
	benchLoop(b, config.DeviceBinding{
		Remaps: []config.Remap{{From: "KEY_A", To: "KEY_B"}},
	})
}

// BenchmarkProcess isolates the pure CPU cost of the per-event transform + state
// machine (no syscalls), so it runs anywhere and pinpoints logic hot spots.
func BenchmarkProcess(b *testing.B) {
	rules, _, err := CompileRemaps([]config.Remap{{From: "KEY_A", To: "KEY_B"}})
	if err != nil {
		b.Fatal(err)
	}
	macros, _, err := CompileMacros([]config.Macro{
		{Name: "m", Trigger: []string{"KEY_LEFTCTRL", "KEY_C"}, Steps: []config.MacroStep{{Key: "KEY_X"}}},
	})
	if err != nil {
		b.Fatal(err)
	}
	proc := newProcessor(rules, macros)
	down := evdev.InputEvent{Type: evdev.EV_KEY, Code: evdev.KEY_A, Value: 1}
	up := evdev.InputEvent{Type: evdev.EV_KEY, Code: evdev.KEY_A, Value: 0}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc.process(down)
		proc.process(up)
	}
}

// name32 builds a stable, unique-per-run device name (uinput truncates names, so
// keep the distinguishing nonce within the kept prefix).
func name32(prefix string, nonce int64) string {
	return prefix + " " + itoaBase36(nonce)
}

func itoaBase36(n int64) string {
	if n == 0 {
		return "0"
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var buf [13]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%36]
		n /= 36
	}
	return string(buf[i:])
}
