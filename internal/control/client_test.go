package control

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeServer listens on a unix socket and, for a capture request, streams a
// couple of events then ends the stream when it reads stop_capture.
func fakeServer(t *testing.T) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)

		// First line is the capture request.
		var req Request
		if line, err := r.ReadBytes('\n'); err == nil {
			json.Unmarshal(line, &req)
		}
		emit := func(name string, value int32) {
			raw, _ := json.Marshal(CaptureEvent{KeyName: name, Value: value})
			line, _ := json.Marshal(Response{ID: req.ID, OK: true, Stream: true, Result: raw})
			conn.Write(append(line, '\n'))
		}
		emit("KEY_LEFTCTRL", 1)
		emit("KEY_J", 1)

		// Wait for stop_capture, then send the final non-stream response.
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var rq Request
			if json.Unmarshal(line, &rq) == nil && rq.Method == MethodStopCapture {
				final, _ := json.Marshal(Response{ID: req.ID, OK: true})
				conn.Write(append(final, '\n'))
				return
			}
		}
	}()
	return sock
}

// errorServer answers a capture request with a single non-stream error response,
// as the daemon does when the device can't be opened or grabbed.
func errorServer(t *testing.T, msg string) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		var req Request
		if line, err := r.ReadBytes('\n'); err == nil {
			json.Unmarshal(line, &req)
		}
		final, _ := json.Marshal(Response{ID: req.ID, OK: false, Error: msg})
		conn.Write(append(final, '\n'))
	}()
	return sock
}

// TestClientCaptureError verifies a server-side failure ending the stream is
// surfaced via CaptureErr rather than looking like a clean close.
func TestClientCaptureError(t *testing.T) {
	sock := errorServer(t, "open /dev/input/event9: no such device")
	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	events, _, err := c.Capture(CaptureParams{DevicePath: "/dev/input/event9", Mode: "key"})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	// Drain until the stream closes (no events are sent).
	for range events {
	}
	capErr := c.CaptureErr()
	if capErr == nil {
		t.Fatal("expected CaptureErr to report the server-side failure")
	}
	if !strings.Contains(capErr.Error(), "no such device") {
		t.Errorf("CaptureErr = %q, want it to mention the server error", capErr)
	}
}

func TestClientCaptureStream(t *testing.T) {
	sock := fakeServer(t)
	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	events, stop, err := c.Capture(CaptureParams{DevicePath: "/dev/input/event0", Mode: "chord"})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	var got []string
	timeout := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case ce, ok := <-events:
			if !ok {
				t.Fatal("stream closed early")
			}
			got = append(got, ce.KeyName)
		case <-timeout:
			t.Fatalf("timed out; got %v", got)
		}
	}

	stop()

	// After stop, the stream channel should close.
	select {
	case _, ok := <-events:
		if ok {
			// Drain any late event, then expect close.
			select {
			case _, ok2 := <-events:
				if ok2 {
					t.Error("expected stream to close after stop")
				}
			case <-time.After(time.Second):
				t.Error("stream did not close after stop")
			}
		}
	case <-time.After(2 * time.Second):
		t.Error("stream did not close after stop")
	}
}
