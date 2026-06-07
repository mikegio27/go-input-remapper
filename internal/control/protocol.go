// Package control defines the daemon's Unix-socket control protocol and a client
// for it. The config files remain the source of truth; this socket is the live
// layer the TUI and CLI use to see the daemon's real state, trigger reloads,
// switch profiles, and (in a later milestone) capture keypresses from grabbed
// devices.
//
// The wire format is newline-delimited JSON: the client writes one Request line
// and the server replies with one or more Response lines (a plain call gets
// exactly one; a capture subscription gets a stream).
package control

import "encoding/json"

// Method names for requests.
const (
	MethodStatus      = "status"       // -> StatusResult
	MethodListDevices = "list_devices" // -> ListDevicesResult
	MethodReload      = "reload"       // -> ReloadResult
	MethodSetProfile  = "set_profile"  // SetProfileParams -> StatusResult
	MethodCapture     = "capture"      // CaptureParams -> stream of CaptureEvent (M7)
	MethodStopCapture = "stop_capture" // ends a capture stream (M7)
)

// Request is a single client call. ID echoes back on the Response so a client
// can match replies; the server processes one connection's requests in order.
type Request struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is a reply to a Request. For a streaming method, Stream is true on the
// intermediate pushes and the final reply has Stream false.
type Response struct {
	ID     int             `json:"id"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Stream bool            `json:"stream,omitempty"`
}

// StatusResult reports the daemon's active profile and bound devices.
type StatusResult struct {
	ActiveProfile string       `json:"active_profile"`
	Engines       []EngineInfo `json:"engines"`
}

// EngineInfo is one running engine.
type EngineInfo struct {
	Path string `json:"path"` // source device node
	Name string `json:"name"` // virtual device name
}

// ListDevicesResult is the daemon's view of all input devices.
type ListDevicesResult struct {
	Devices []DeviceInfo `json:"devices"`
}

// DeviceInfo describes one device for the TUI/CLI: identity, classification, and
// whether the daemon currently has it bound.
type DeviceInfo struct {
	Path        string   `json:"path"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Vendor      uint16   `json:"vendor"`
	Product     uint16   `json:"product"`
	Bound       bool     `json:"bound"`
	Recommended bool     `json:"recommended"`
	IsVirtual   bool     `json:"is_virtual"`
	Reasons     []string `json:"reasons,omitempty"`
}

// SetProfileParams selects the active profile.
type SetProfileParams struct {
	Profile string `json:"profile"`
}

// ReloadResult reports the outcome of a reload. Errors is non-empty when the
// config on disk is invalid and the daemon kept its previous config.
type ReloadResult struct {
	OK     bool     `json:"ok"`
	Errors []string `json:"errors,omitempty"`
}

// CaptureParams starts a capture stream on a device (M7).
type CaptureParams struct {
	DevicePath string `json:"device_path"`
	Mode       string `json:"mode"` // "key" | "chord" | "macro"
}

// CaptureEvent is one pushed key event during capture (M7).
type CaptureEvent struct {
	KeyName    string   `json:"key_name"`
	Value      int32    `json:"value"`
	Chord      []string `json:"chord,omitempty"`
	DurationMs int      `json:"duration_ms,omitempty"`
}
