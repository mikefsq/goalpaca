package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mikefsq/goalpaca/server"
	"github.com/mikefsq/goalpaca/sim"
)

func TestGatesAgreeInProcessAndOverHTTP(t *testing.T) {
	cases := []struct {
		name string
		path string
		form url.Values
		// gate calls the exported gate against the same device the server holds.
		gate func(t *testing.T, s *server.Server) error
	}{
		{
			name: "switch id out of range",
			path: "/api/v1/switch/0/getswitch?Id=99",
			form: nil, // GET
			gate: func(t *testing.T, s *server.Server) error {
				return server.GateSwitchID(deviceOf[server.Switch](t, s, server.SwitchType), 99)
			},
		},
		{
			name: "switch value out of range",
			path: "/api/v1/switch/0/setswitchvalue",
			form: url.Values{"Id": {"0"}, "Value": {"1e9"}},
			gate: func(t *testing.T, s *server.Server) error {
				return server.GateSwitchValue(deviceOf[server.Switch](t, s, server.SwitchType), 0, 1e9)
			},
		},
		{
			name: "camera binning above MaxBinX",
			path: "/api/v1/camera/0/binx",
			form: url.Values{"BinX": {"99"}},
			gate: func(t *testing.T, s *server.Server) error {
				return server.GateCameraSetBinX(deviceOf[server.Camera](t, s, server.CameraType), 99)
			},
		},
		{
			name: "camera cooler setpoint below absolute zero",
			path: "/api/v1/camera/0/setccdtemperature",
			form: url.Values{"SetCCDTemperature": {"-400"}},
			gate: func(t *testing.T, s *server.Server) error {
				return server.GateCameraSetCCDTemperature(deviceOf[server.Camera](t, s, server.CameraType), -400)
			},
		},
		{
			name: "telescope target RA out of range",
			path: "/api/v1/telescope/0/targetrightascension",
			form: url.Values{"TargetRightAscension": {"25"}},
			gate: func(t *testing.T, s *server.Server) error { return server.GateTelescopeTargetRightAscension(25) },
		},
		{
			name: "telescope site longitude out of range",
			path: "/api/v1/telescope/0/sitelongitude",
			form: url.Values{"SiteLongitude": {"200"}},
			gate: func(t *testing.T, s *server.Server) error { return server.GateTelescopeSiteLongitude(200) },
		},
		{
			name: "telescope move axis at an unsupported rate",
			path: "/api/v1/telescope/0/moveaxis",
			form: url.Values{"Axis": {"0"}, "Rate": {"1e6"}},
			gate: func(t *testing.T, s *server.Server) error {
				return server.GateTelescopeMoveAxis(deviceOf[server.Telescope](t, s, server.TelescopeType), server.AxisPrimary, 1e6)
			},
		},
		{
			name: "filter wheel slot out of range",
			path: "/api/v1/filterwheel/0/position",
			form: url.Values{"Position": {"99"}},
			gate: func(t *testing.T, s *server.Server) error {
				return server.GateFilterWheelPosition(deviceOf[server.FilterWheel](t, s, server.FilterWheelType), 99)
			},
		},
		{
			name: "rotator position out of range",
			path: "/api/v1/rotator/0/moveabsolute",
			form: url.Values{"Position": {"400"}},
			gate: func(t *testing.T, s *server.Server) error { return server.GateRotatorPosition(400) },
		},
		{
			name: "cover calibrator brightness above maximum",
			path: "/api/v1/covercalibrator/0/calibratoron",
			form: url.Values{"Brightness": {"999999"}},
			gate: func(t *testing.T, s *server.Server) error {
				return server.GateCoverCalibratorOn(deviceOf[server.CoverCalibrator](t, s, server.CoverCalibratorType), 999999)
			},
		},
		{
			name: "observing conditions unknown sensor",
			path: "/api/v1/observingconditions/0/sensordescription?SensorName=nonsense",
			form: nil, // GET
			gate: func(t *testing.T, s *server.Server) error {
				return server.GateObservingConditionsSensor("nonsense", false)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newGateTestServer(t)

			inProcess := tc.gate(t, s)
			if inProcess == nil {
				t.Fatalf("the exported gate ALLOWED this, so an in-process host would pass it " +
					"straight to the driver")
			}
			var ae *server.AlpacaError
			if !errors.As(inProcess, &ae) {
				t.Fatalf("the gate returned %v, which is not an *server.AlpacaError — a host cannot "+
					"classify it", inProcess)
			}

			var overHTTP int
			if tc.form == nil {
				overHTTP = getValue(t, s, tc.path).ErrorNumber
			} else {
				overHTTP = put(t, s, tc.path, tc.form).ErrorNumber
			}
			if overHTTP == 0 {
				t.Fatalf("the HTTP dispatch ALLOWED what the gate refused (%v) — the dispatch is "+
					"not calling the exported gate", inProcess)
			}
			if int(ae.Number) != overHTTP {
				t.Errorf("in-process answered %#x and HTTP answered %#x for the same request.\n"+
					"\tThe two paths must classify identically: %#x and %#x mean different things "+
					"to a client's error handling — a capability report, a refusal and a fault are "+
					"acted on differently.", int(ae.Number), overHTTP, int(ae.Number), overHTTP)
			}
		})
	}
}

// newGateTestServer stands up one simulated device of every type the cases above use.
func newGateTestServer(t *testing.T) *server.Server {
	t.Helper()
	s := server.New(server.Config{Discovery: server.DiscoveryConfig{Mode: server.DiscoveryOff}})
	reg := func(typ server.DeviceType, d server.Device) {
		t.Helper()
		if err := s.Register(typ, 0, d); err != nil {
			t.Fatalf("register %s: %v", typ, err)
		}
		if err := d.Connect(context.Background()); err != nil {
			t.Fatalf("connect %s: %v", typ, err)
		}
	}
	reg(server.SwitchType, sim.NewSwitch())
	reg(server.CameraType, sim.NewCamera())
	reg(server.TelescopeType, sim.NewTelescope())
	reg(server.FilterWheelType, sim.NewFilterWheel())
	reg(server.RotatorType, sim.NewRotator())
	reg(server.CoverCalibratorType, sim.NewCoverCalibrator())
	reg(server.ObservingConditionsType, sim.NewObservingConditions())
	return s
}

// deviceOf returns the registered device of a type, narrowed — the same object the dispatch will
// reach, so the two halves of each case are genuinely talking to one device rather than to two
// equivalent ones.
func deviceOf[T any](t *testing.T, s *server.Server, typ server.DeviceType) T {
	t.Helper()
	d, ok := s.Device(typ, 0)
	if !ok {
		t.Fatalf("no %s registered", typ)
	}
	v, ok := d.(T)
	if !ok {
		t.Fatalf("registered %s is a %T, which does not satisfy the typed interface", typ, d)
	}
	return v
}

// The two HTTP helpers, repeated here because an external test package cannot reach the internal
// ones. That is not duplication worth avoiding: an external package is the point — it is how this
// test proves the gates are usable by a HOST, which is the whole reason they were exported.

type valueResponse struct {
	Value        any    `json:"Value"`
	ErrorNumber  int    `json:"ErrorNumber"`
	ErrorMessage string `json:"ErrorMessage"`
}

type methodResponse struct {
	ErrorNumber  int    `json:"ErrorNumber"`
	ErrorMessage string `json:"ErrorMessage"`
}

func getValue(t *testing.T, s *server.Server, path string) valueResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d body %q", path, rec.Code, rec.Body.String())
	}
	var vr valueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &vr); err != nil {
		t.Fatalf("GET %s: decode: %v body %q", path, err, rec.Body.String())
	}
	return vr
}

func put(t *testing.T, s *server.Server, path string, form url.Values) methodResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT %s: status %d body %q", path, rec.Code, rec.Body.String())
	}
	var mr methodResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &mr); err != nil {
		t.Fatalf("PUT %s: decode: %v", path, err)
	}
	return mr
}
