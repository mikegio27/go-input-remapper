package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// dialTimeout bounds how long Dial waits to connect to the socket; callTimeout
// bounds how long a single request waits for its reply, so a wedged daemon can
// never freeze a caller (e.g. the TUI refresh).
const (
	dialTimeout = 2 * time.Second
	callTimeout = 5 * time.Second
)

// Client is a connection to the daemon's control socket. It is not safe for
// concurrent use by multiple goroutines; use one Client per goroutine.
type Client struct {
	conn   net.Conn
	r      *bufio.Reader
	nextID int

	// capErr records why the last capture stream ended with a server error. It is
	// written before the events channel is closed, so a reader that observes the
	// close also observes this value. CaptureErr reads it back.
	capErr error
}

// Dial connects to the control socket at path.
func Dial(path string) (*Client, error) {
	conn, err := net.DialTimeout("unix", path, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon at %s: %w", path, err)
	}
	return &Client{conn: conn, r: bufio.NewReader(conn)}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Call sends a request and decodes the single response. params may be nil; if
// result is non-nil the response payload is unmarshaled into it. A server-side
// error is returned as a Go error.
func (c *Client) Call(method string, params, result any) error {
	if err := c.send(method, params); err != nil {
		return err
	}
	// Bound the wait so a non-responsive daemon surfaces as an error, not a hang.
	c.conn.SetReadDeadline(time.Now().Add(callTimeout))
	defer c.conn.SetReadDeadline(time.Time{})
	resp, err := c.readResponse()
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon: %s", resp.Error)
	}
	if result != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, result)
	}
	return nil
}

// Capture starts a capture session and returns a channel of captured key events
// plus a stop function. The channel is closed when capture ends (stop is called,
// the daemon ends it, or the connection drops). The client must not be used for
// other calls while a capture is active.
func (c *Client) Capture(params CaptureParams) (<-chan CaptureEvent, func(), error) {
	if err := c.send(MethodCapture, params); err != nil {
		return nil, nil, err
	}
	c.capErr = nil
	events := make(chan CaptureEvent)
	go func() {
		defer close(events)
		for {
			resp, err := c.readResponse()
			if err != nil {
				return // connection closed (e.g. by our own stop) — not an error to report
			}
			if !resp.Stream {
				// Final response. A failure here (bad device, grab denied, refusing
				// to capture from a virtual device) is the reason the overlay would
				// otherwise vanish silently, so record it for CaptureErr.
				if !resp.OK {
					c.capErr = fmt.Errorf("daemon: %s", resp.Error)
				}
				return
			}
			var ce CaptureEvent
			if json.Unmarshal(resp.Result, &ce) == nil {
				events <- ce
			}
		}
	}()
	stop := func() { _ = c.send(MethodStopCapture, nil) }
	return events, stop, nil
}

// CaptureErr returns the error that ended the last capture stream, or nil if it
// ended cleanly. Call it only after the events channel returned by Capture has
// been closed; the value is established before the close.
func (c *Client) CaptureErr() error { return c.capErr }

// send marshals and writes one request line.
func (c *Client) send(method string, params any) error {
	c.nextID++
	req := Request{ID: c.nextID, Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = raw
	}
	line, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := c.conn.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write request: %w", err)
	}
	return nil
}

// readResponse reads and decodes one response line.
func (c *Client) readResponse() (Response, error) {
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}
	return resp, nil
}
