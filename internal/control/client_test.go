package control

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
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
