package conformance

import (
	"net/http/httptest"
	"testing"

	"github.com/mikefsq/goalpaca/client"
	alpacadev "github.com/mikefsq/goalpaca/server"
	"github.com/mikefsq/goalpaca/sim"
)

// serveSim hosts one simulated device on the real server (over httptest) and
// returns its URL. Each conformance test points a client at it and runs the
// ported ConformU checks (sim -> server -> client).
func serveSim(t *testing.T, typ alpacadev.DeviceType, dev alpacadev.Device) string {
	t.Helper()
	srv := alpacadev.New(alpacadev.Config{Discovery: alpacadev.DiscoveryConfig{Mode: alpacadev.DiscoveryOff}})
	if err := srv.Register(typ, 0, dev); err != nil {
		t.Fatalf("register %s: %v", typ, err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestCameraConformance(t *testing.T) {
	url := serveSim(t, alpacadev.CameraType, sim.NewCamera())
	c := client.NewCamera(url, 0)
	CheckCommon(t, c)
	CheckCamera(t, c)
}

func TestCoverCalibratorConformance(t *testing.T) {
	url := serveSim(t, alpacadev.CoverCalibratorType, sim.NewCoverCalibrator())
	c := client.NewCoverCalibrator(url, 0)
	CheckCommon(t, c)
	CheckCoverCalibrator(t, c)
}

func TestDomeConformance(t *testing.T) {
	url := serveSim(t, alpacadev.DomeType, sim.NewDome(sim.WithDomeRate(720)))
	c := client.NewDome(url, 0)
	CheckCommon(t, c)
	CheckDome(t, c)
}

func TestFilterWheelConformance(t *testing.T) {
	url := serveSim(t, alpacadev.FilterWheelType, sim.NewFilterWheel())
	c := client.NewFilterWheel(url, 0)
	CheckCommon(t, c)
	CheckFilterWheel(t, c)
}

func TestFocuserConformance(t *testing.T) {
	url := serveSim(t, alpacadev.FocuserType, sim.NewFocuser(sim.WithStepRate(200000)))
	c := client.NewFocuser(url, 0)
	CheckCommon(t, c)
	CheckFocuser(t, c)
}

func TestObservingConditionsConformance(t *testing.T) {
	url := serveSim(t, alpacadev.ObservingConditionsType, sim.NewObservingConditions())
	c := client.NewObservingConditions(url, 0)
	CheckCommon(t, c)
	CheckObservingConditions(t, c)
}

func TestSafetyMonitorConformance(t *testing.T) {
	url := serveSim(t, alpacadev.SafetyMonitorType, sim.NewSafetyMonitor())
	c := client.NewSafetyMonitor(url, 0)
	CheckCommon(t, c)
	CheckSafetyMonitor(t, c)
}

func TestSwitchConformance(t *testing.T) {
	url := serveSim(t, alpacadev.SwitchType, sim.NewSwitch())
	c := client.NewSwitch(url, 0)
	CheckCommon(t, c)
	CheckSwitch(t, c)
}

func TestTelescopeConformance(t *testing.T) {
	url := serveSim(t, alpacadev.TelescopeType, sim.NewTelescope(sim.WithSlewRate(720)))
	c := client.NewTelescope(url, 0)
	CheckCommon(t, c)
	CheckTelescope(t, c)
}
