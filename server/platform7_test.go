package alpacadev

import (
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
