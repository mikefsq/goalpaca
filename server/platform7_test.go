package server

import (
	"context"
	"net/url"
	"testing"
)

// deviceStateNames decodes a /devicestate response into a name->value map.
func deviceStateNames(t *testing.T, s *Server, path string) map[string]any {
	t.Helper()
	vr := getValue(t, s, path)
	arr, ok := vr.Value.([]any)
	if !ok {
		t.Fatalf("devicestate not an array: %#v", vr.Value)
	}
	names := map[string]any{}
	for _, e := range arr {
		m := e.(map[string]any)
		names[m["Name"].(string)] = m["Value"]
	}
	return names
}

func TestDeviceStateCamera(t *testing.T) {
	s := newTestServer(t)
	names := deviceStateNames(t, s, "/api/v1/camera/0/devicestate")

	for _, n := range []string{"CameraState", "ImageReady", "IsPulseGuiding", "PercentCompleted", "TimeStamp"} {
		if _, ok := names[n]; !ok {
			t.Errorf("camera devicestate missing %q", n)
		}
	}
	// CCDTemperature is NotImplemented on the fake camera -> must be omitted.
	if _, ok := names["CCDTemperature"]; ok {
		t.Errorf("camera devicestate should omit unsupported CCDTemperature")
	}
	// TimeStamp must be a non-empty ISO-8601 string.
	if ts, _ := names["TimeStamp"].(string); ts == "" {
		t.Errorf("camera devicestate TimeStamp empty/non-string: %#v", names["TimeStamp"])
	}
}

func TestSwitchAsync(t *testing.T) {
	s := multiTypeServer(t)
	if v := getValue(t, s, "/api/v1/switch/0/canasync?Id=0").Value; v != false {
		t.Errorf("canasync = %v, want false", v)
	}
	// StateChangeComplete defaults to true (nothing in progress).
	if v := getValue(t, s, "/api/v1/switch/0/statechangecomplete?Id=0").Value; v != true {
		t.Errorf("statechangecomplete = %v, want true", v)
	}
	// SetAsync is unsupported on the base -> NotImplemented in-band.
	mr := put(t, s, "/api/v1/switch/0/setasync", url.Values{"Id": {"0"}, "State": {"true"}})
	if mr.ErrorNumber != ErrNumNotImplemented {
		t.Errorf("setasync ErrorNumber = %#x, want %#x", mr.ErrorNumber, ErrNumNotImplemented)
	}
}

func TestErrorCodeValues(t *testing.T) {
	// Guard the Platform 7 numeric values against accidental drift.
	cases := map[string]int{
		"Parked":             ErrNumParked,
		"Slaved":             ErrNumSlaved,
		"OperationCancelled": ErrNumOperationCancelled,
	}
	want := map[string]int{"Parked": 0x408, "Slaved": 0x409, "OperationCancelled": 0x40E}
	for k, v := range cases {
		if v != want[k] {
			t.Errorf("%s = %#x, want %#x", k, v, want[k])
		}
	}
}

// asyncConnectDevice simulates a Platform 7 async-connect driver whose
// connect attempt fails: Connect starts the op and the "hardware" fails it.
type asyncConnectDevice struct {
	BaseFocuser
	failWith error
}

func (d *asyncConnectDevice) Connect(ctx context.Context) error {
	d.ConnectOp().Begin()
	// Synchronous for test determinism; a real driver would do this in a
	// goroutine — the HTTP contract is identical.
	if d.failWith != nil {
		d.ConnectOp().Fail(d.failWith)
		return nil // async initiator returns immediately; failure surfaces on poll
	}
	d.ConnectOp().Complete()
	d.MarkConnected()
	return nil
}

// TestAsyncConnectFailureSurfaced verifies the Platform 7 contract: PUT
// connect returns success (the initiator), and the recorded failure of the
// async operation is reported in-band when the client polls `connecting`.
func TestAsyncConnectFailureSurfaced(t *testing.T) {
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	dev := &asyncConnectDevice{failWith: NewError(0x500, "USB enumeration failed")}
	dev.DevName = "AsyncFoc"
	dev.IfaceVer = 4
	if err := s.Register(FocuserType, 0, dev); err != nil {
		t.Fatalf("register: %v", err)
	}

	// The initiator itself succeeds.
	if mr := put(t, s, "/api/v1/focuser/0/connect", url.Values{}); mr.ErrorNumber != 0 {
		t.Fatalf("connect initiator ErrorNumber = %#x, want 0", mr.ErrorNumber)
	}

	// Polling connecting must surface the recorded failure in-band.
	vr := getValue(t, s, "/api/v1/focuser/0/connecting")
	if vr.ErrorNumber != 0x500 {
		t.Errorf("connecting after failed connect: ErrorNumber = %#x, want 0x500", vr.ErrorNumber)
	}
	if vr.ErrorMessage != "USB enumeration failed" {
		t.Errorf("connecting ErrorMessage = %q, want the driver's message", vr.ErrorMessage)
	}
	// And connected stays plain false (valid data, not an error).
	if vr := getValue(t, s, "/api/v1/focuser/0/connected"); vr.ErrorNumber != 0 || vr.Value != false {
		t.Errorf("connected = %#v (err %#x), want false/0", vr.Value, vr.ErrorNumber)
	}

	// A subsequent successful connect clears the recorded error.
	dev.failWith = nil
	if mr := put(t, s, "/api/v1/focuser/0/connect", url.Values{}); mr.ErrorNumber != 0 {
		t.Fatalf("second connect ErrorNumber = %#x, want 0", mr.ErrorNumber)
	}
	if vr := getValue(t, s, "/api/v1/focuser/0/connecting"); vr.ErrorNumber != 0 || vr.Value != false {
		t.Errorf("connecting after successful connect = %#v (err %#x), want false/0", vr.Value, vr.ErrorNumber)
	}
	if vr := getValue(t, s, "/api/v1/focuser/0/connected"); vr.Value != true {
		t.Errorf("connected after successful connect = %#v, want true", vr.Value)
	}
}

// statefulSwitch publishes per-switch state through a DeviceState override —
// the case the merge exists for (the library derives no scalar Switch state).
type statefulSwitch struct {
	BaseSwitch
}

func (s *statefulSwitch) MaxSwitch() int { return 2 }
func (s *statefulSwitch) DeviceState() []StateValue {
	return []StateValue{
		{Name: "GetSwitch0", Value: true},
		{Name: "GetSwitchValue1", Value: 0.5},
	}
}

// stateOverrideCamera overrides a library-built DeviceState entry by name.
type stateOverrideCamera struct {
	fakeCamera
}

func (c *stateOverrideCamera) DeviceState() []StateValue {
	return []StateValue{
		{Name: "PercentCompleted", Value: 42}, // override built entry
		{Name: "VendorFanRPM", Value: 1200},   // vendor extra
	}
}

// TestDeviceStateMerge verifies driver DeviceState overrides are merged into
// the library-built set: new names append, same names override, and the
// mandatory TimeStamp is still supplied.
func TestDeviceStateMerge(t *testing.T) {
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	sw := &statefulSwitch{}
	sw.DevName = "SW"
	sw.IfaceVer = 3
	sw.MarkConnected()
	cam := &stateOverrideCamera{}
	cam.DevName = "Cam"
	cam.IfaceVer = 4
	cam.MarkConnected()
	if err := s.Register(SwitchType, 0, sw); err != nil {
		t.Fatalf("register switch: %v", err)
	}
	if err := s.Register(CameraType, 0, cam); err != nil {
		t.Fatalf("register camera: %v", err)
	}

	// Switch: driver-only entries appear (the library derives none).
	swState := deviceStateNames(t, s, "/api/v1/switch/0/devicestate")
	if swState["GetSwitch0"] != true {
		t.Errorf("switch GetSwitch0 = %#v, want true", swState["GetSwitch0"])
	}
	if swState["GetSwitchValue1"] != 0.5 {
		t.Errorf("switch GetSwitchValue1 = %#v, want 0.5", swState["GetSwitchValue1"])
	}
	if _, ok := swState["TimeStamp"]; !ok {
		t.Error("switch devicestate missing TimeStamp")
	}

	// Camera: override replaces the built value; vendor extra appends;
	// built entries the driver didn't touch survive.
	camState := deviceStateNames(t, s, "/api/v1/camera/0/devicestate")
	if camState["PercentCompleted"] != float64(42) {
		t.Errorf("camera PercentCompleted = %#v, want 42 (driver override)", camState["PercentCompleted"])
	}
	if camState["VendorFanRPM"] != float64(1200) {
		t.Errorf("camera VendorFanRPM = %#v, want 1200", camState["VendorFanRPM"])
	}
	if _, ok := camState["CameraState"]; !ok {
		t.Error("camera devicestate lost the built CameraState entry")
	}
}
