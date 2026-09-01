package server

import (
	"net/url"
	"testing"
)

// These tests prove that protocol compliance is enforced by the LIBRARY, not
// by well-behaved drivers: every driver here is deliberately naive — it
// advertises capabilities and accepts whatever it is given, with no
// validation of its own — and the HTTP layer must still return the ASCOM
// error numbers ConformU requires (NotImplemented 0x400, InvalidValue 0x401,
// Parked 0x408, InvalidOperation 0x40B).

// --- naive telescope: full capabilities, zero validation ---

type naiveTelescope struct {
	BaseTelescope
	parked   bool
	rate     DriveRate
	decRate  float64
	lastSlew [2]float64
	slews    int
}

func newNaiveTelescope() *naiveTelescope {
	tel := &naiveTelescope{rate: DriveSidereal}
	tel.ID = "naive-scope"
	tel.DevName = "NaiveScope"
	tel.IfaceVer = 4
	tel.MarkConnected()
	return tel
}

func (t *naiveTelescope) AtPark() bool                   { return t.parked }
func (t *naiveTelescope) CanFindHome() bool              { return false } // the one thing it cannot do
func (t *naiveTelescope) CanPark() bool                  { return true }
func (t *naiveTelescope) CanUnpark() bool                { return true }
func (t *naiveTelescope) CanPulseGuide() bool            { return true }
func (t *naiveTelescope) CanSetDeclinationRate() bool    { return true }
func (t *naiveTelescope) CanSetRightAscensionRate() bool { return true }
func (t *naiveTelescope) CanSetTracking() bool           { return true }
func (t *naiveTelescope) CanSlew() bool                  { return true }
func (t *naiveTelescope) CanSlewAsync() bool             { return true }
func (t *naiveTelescope) CanSync() bool                  { return true }
func (t *naiveTelescope) CanMoveAxis(TelescopeAxis) bool { return true }
func (t *naiveTelescope) AxisRates(TelescopeAxis) []AxisRate {
	return []AxisRate{{Minimum: 0, Maximum: 5}}
}
func (t *naiveTelescope) TrackingRate() DriveRate { return t.rate }
func (t *naiveTelescope) SetTrackingRate(r DriveRate) error {
	t.rate = r // accepts anything
	return nil
}
func (t *naiveTelescope) TrackingRates() []DriveRate {
	return []DriveRate{DriveSidereal, DriveLunar}
}
func (t *naiveTelescope) DeclinationRate() float64 { return t.decRate }
func (t *naiveTelescope) SetDeclinationRate(v float64) error {
	t.decRate = v // accepts anything, any tracking rate
	return nil
}
func (t *naiveTelescope) Park() error            { t.parked = true; return nil }
func (t *naiveTelescope) Unpark() error          { t.parked = false; return nil }
func (t *naiveTelescope) SetTracking(bool) error { return nil }
func (t *naiveTelescope) SlewToCoordinatesAsync(ra, dec float64) error {
	t.lastSlew = [2]float64{ra, dec} // no range checks, no park checks
	t.slews++
	return nil
}
func (t *naiveTelescope) SlewToTargetAsync() error              { t.slews++; return nil }
func (t *naiveTelescope) MoveAxis(TelescopeAxis, float64) error { return nil }
func (t *naiveTelescope) PulseGuide(GuideDirection, int) error  { return nil }
func (t *naiveTelescope) AbortSlew() error                      { return nil }
func (t *naiveTelescope) SetSiteLatitude(float64) error         { return nil }
func (t *naiveTelescope) SetUTCDate(string) error               { return nil }

func newNaiveTelescopeServer(t *testing.T) (*Server, *naiveTelescope) {
	t.Helper()
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	tel := newNaiveTelescope()
	if err := s.Register(TelescopeType, 0, tel); err != nil {
		t.Fatalf("register: %v", err)
	}
	return s, tel
}

func TestGateTelescopeRanges(t *testing.T) {
	s, _ := newNaiveTelescopeServer(t)

	cases := []struct {
		member string
		form   url.Values
	}{
		{"targetrightascension", url.Values{"TargetRightAscension": {"25"}}},
		{"targetrightascension", url.Values{"TargetRightAscension": {"-1"}}},
		{"targetdeclination", url.Values{"TargetDeclination": {"100"}}},
		{"sitelatitude", url.Values{"SiteLatitude": {"91"}}},
		{"siteelevation", url.Values{"SiteElevation": {"-301"}}},
		{"sitelongitude", url.Values{"SiteLongitude": {"181"}}},
		{"slewsettletime", url.Values{"SlewSettleTime": {"-1"}}},
		{"slewtocoordinatesasync", url.Values{"RightAscension": {"25"}, "Declination": {"0"}}},
		{"slewtocoordinatesasync", url.Values{"RightAscension": {"1"}, "Declination": {"-100"}}},
		{"trackingrate", url.Values{"TrackingRate": {"5"}}},
		{"pulseguide", url.Values{"Direction": {"4"}, "Duration": {"10"}}},
		{"pulseguide", url.Values{"Direction": {"0"}, "Duration": {"-1"}}},
		{"moveaxis", url.Values{"Axis": {"0"}, "Rate": {"6"}}},
		{"moveaxis", url.Values{"Axis": {"3"}, "Rate": {"1"}}},
		{"utcdate", url.Values{"UTCDate": {"not-a-date"}}},
	}
	for _, c := range cases {
		if mr := put(t, s, "/api/v1/telescope/0/"+c.member, c.form); mr.ErrorNumber != ErrNumInvalidValue {
			t.Errorf("naive driver: PUT %s %v ErrorNumber = %#x, want InvalidValue %#x",
				c.member, c.form, mr.ErrorNumber, ErrNumInvalidValue)
		}
	}
}

func TestGateTelescopeParked(t *testing.T) {
	s, tel := newNaiveTelescopeServer(t)
	tel.parked = true

	parked := []struct {
		member string
		form   url.Values
	}{
		{"abortslew", url.Values{}},
		{"moveaxis", url.Values{"Axis": {"0"}, "Rate": {"1"}}},
		{"pulseguide", url.Values{"Direction": {"0"}, "Duration": {"10"}}},
		{"slewtocoordinatesasync", url.Values{"RightAscension": {"1"}, "Declination": {"0"}}},
		{"slewtotargetasync", url.Values{}},
		{"synctocoordinates", url.Values{"RightAscension": {"1"}, "Declination": {"0"}}},
		{"tracking", url.Values{"Tracking": {"true"}}},
	}
	for _, c := range parked {
		if mr := put(t, s, "/api/v1/telescope/0/"+c.member, c.form); mr.ErrorNumber != ErrNumParked {
			t.Errorf("parked naive driver: PUT %s ErrorNumber = %#x, want Parked %#x",
				c.member, mr.ErrorNumber, ErrNumParked)
		}
	}
	if tel.slews != 0 {
		t.Errorf("parked slews reached the naive driver %d times; the gate must reject them", tel.slews)
	}
	// Tracking = false is explicitly allowed while parked.
	if mr := put(t, s, "/api/v1/telescope/0/tracking", url.Values{"Tracking": {"false"}}); mr.ErrorNumber != 0 {
		t.Errorf("parked Tracking=false ErrorNumber = %#x, want success", mr.ErrorNumber)
	}
}

func TestGateTelescopeTargetsAndRates(t *testing.T) {
	s, tel := newNaiveTelescopeServer(t)

	// Read-before-set (BaseTelescope) → InvalidOperation.
	if vr := getValue(t, s, "/api/v1/telescope/0/targetrightascension"); vr.ErrorNumber != ErrNumInvalidOperation {
		t.Errorf("target RA read before set ErrorNumber = %#x, want InvalidOperation", vr.ErrorNumber)
	}
	// SlewToTargetAsync before targets are set → InvalidOperation.
	if mr := put(t, s, "/api/v1/telescope/0/slewtotargetasync", url.Values{}); mr.ErrorNumber != ErrNumInvalidOperation {
		t.Errorf("slewtotargetasync before targets ErrorNumber = %#x, want InvalidOperation", mr.ErrorNumber)
	}

	// A successful coordinate slew must populate the targets (library-propagated).
	if mr := put(t, s, "/api/v1/telescope/0/slewtocoordinatesasync",
		url.Values{"RightAscension": {"12.5"}, "Declination": {"-20"}}); mr.ErrorNumber != 0 {
		t.Fatalf("slewtocoordinatesasync ErrorNumber = %#x, want success", mr.ErrorNumber)
	}
	if vr := getValue(t, s, "/api/v1/telescope/0/targetrightascension"); vr.ErrorNumber != 0 || vr.Value != 12.5 {
		t.Errorf("target RA after slew = %v (err %#x), want 12.5", vr.Value, vr.ErrorNumber)
	}
	if vr := getValue(t, s, "/api/v1/telescope/0/targetdeclination"); vr.ErrorNumber != 0 || vr.Value != float64(-20) {
		t.Errorf("target Dec after slew = %v (err %#x), want -20", vr.Value, vr.ErrorNumber)
	}

	// Rate offsets: the naive driver stores anything, but under a non-sidereal
	// drive rate the library must reject writes and read the offset as zero.
	tel.decRate = 1.5 // pretend a stale rate is left in the driver
	tel.rate = DriveLunar
	if mr := put(t, s, "/api/v1/telescope/0/declinationrate",
		url.Values{"DeclinationRate": {"0.05"}}); mr.ErrorNumber != ErrNumInvalidOperation {
		t.Errorf("set dec rate under Lunar ErrorNumber = %#x, want InvalidOperation", mr.ErrorNumber)
	}
	if vr := getValue(t, s, "/api/v1/telescope/0/declinationrate"); vr.ErrorNumber != 0 || vr.Value != float64(0) {
		t.Errorf("dec rate under Lunar = %v (err %#x), want 0", vr.Value, vr.ErrorNumber)
	}
	tel.rate = DriveSidereal
	if mr := put(t, s, "/api/v1/telescope/0/declinationrate",
		url.Values{"DeclinationRate": {"0.05"}}); mr.ErrorNumber != 0 {
		t.Errorf("set dec rate under Sidereal ErrorNumber = %#x, want success", mr.ErrorNumber)
	}

	// Can-flag gate: CanFindHome is false → NotImplemented.
	if mr := put(t, s, "/api/v1/telescope/0/findhome", url.Values{}); mr.ErrorNumber != ErrNumNotImplemented {
		t.Errorf("findhome with CanFindHome=false ErrorNumber = %#x, want NotImplemented", mr.ErrorNumber)
	}
}

// --- naive camera: capabilities on, no validation ---

type naiveCamera struct {
	BaseCamera
	gain    int
	started bool
}

func newNaiveCamera() *naiveCamera {
	c := &naiveCamera{}
	c.ID = "naive-cam"
	c.DevName = "NaiveCam"
	c.IfaceVer = 4
	c.MarkConnected()
	return c
}

func (c *naiveCamera) CameraXSize() int           { return 100 }
func (c *naiveCamera) CameraYSize() int           { return 50 }
func (c *naiveCamera) MaxBinX() int               { return 4 }
func (c *naiveCamera) MaxBinY() int               { return 4 }
func (c *naiveCamera) NumX() int                  { return 100 }
func (c *naiveCamera) NumY() int                  { return 60 } // taller than the sensor: driver misconfigured
func (c *naiveCamera) GainMax() int               { return 300 }
func (c *naiveCamera) SetGain(g int) error        { c.gain = g; return nil } // accepts anything
func (c *naiveCamera) SensorType() SensorType     { return SensorMonochrome }
func (c *naiveCamera) BayerOffsetX() (int, error) { return 0, nil } // wrongly implemented for mono
func (c *naiveCamera) CanSetCCDTemperature() bool { return true }
func (c *naiveCamera) SetSetCCDTemperature(float64) error {
	return nil // accepts 1000 °C happily
}
func (c *naiveCamera) StartExposure(float64, bool) error { c.started = true; return nil }

func TestGateCamera(t *testing.T) {
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	cam := newNaiveCamera()
	if err := s.Register(CameraType, 0, cam); err != nil {
		t.Fatalf("register: %v", err)
	}

	invalid := []struct {
		member string
		form   url.Values
	}{
		{"binx", url.Values{"BinX": {"5"}}},
		{"binx", url.Values{"BinX": {"0"}}},
		{"gain", url.Values{"Gain": {"301"}}},
		{"gain", url.Values{"Gain": {"-1"}}},
		{"setccdtemperature", url.Values{"SetCCDTemperature": {"100"}}},
		{"setccdtemperature", url.Values{"SetCCDTemperature": {"-300"}}},
		{"readoutmode", url.Values{"ReadoutMode": {"1"}}}, // Base has a single mode
		{"startexposure", url.Values{"Duration": {"-1"}, "Light": {"true"}}},
		// NumY=60 on a 50-pixel-tall sensor: geometry must be rejected.
		{"startexposure", url.Values{"Duration": {"1"}, "Light": {"true"}}},
	}
	for _, c := range invalid {
		if mr := put(t, s, "/api/v1/camera/0/"+c.member, c.form); mr.ErrorNumber != ErrNumInvalidValue {
			t.Errorf("naive camera: PUT %s %v ErrorNumber = %#x, want InvalidValue",
				c.member, c.form, mr.ErrorNumber)
		}
	}
	if cam.started {
		t.Error("invalid startexposure reached the naive driver; the gate must reject it")
	}

	// ImageReady is false → image reads are InvalidOperation.
	if vr := getValue(t, s, "/api/v1/camera/0/imagearray"); vr.ErrorNumber != ErrNumInvalidOperation {
		t.Errorf("imagearray with no image ErrorNumber = %#x, want InvalidOperation", vr.ErrorNumber)
	}

	// Monochrome sensor → BayerOffsetX must be NotImplemented even though the
	// naive driver returns a value.
	if vr := getValue(t, s, "/api/v1/camera/0/bayeroffsetx"); vr.ErrorNumber != ErrNumNotImplemented {
		t.Errorf("bayeroffsetx on monochrome ErrorNumber = %#x, want NotImplemented", vr.ErrorNumber)
	}
	// CanFastReadout / CanStopExposure / CanPulseGuide are false (Base).
	for _, m := range []string{"fastreadout"} {
		if vr := getValue(t, s, "/api/v1/camera/0/"+m); vr.ErrorNumber != ErrNumNotImplemented {
			t.Errorf("GET %s ErrorNumber = %#x, want NotImplemented", m, vr.ErrorNumber)
		}
	}
	if mr := put(t, s, "/api/v1/camera/0/stopexposure", url.Values{}); mr.ErrorNumber != ErrNumNotImplemented {
		t.Errorf("stopexposure with CanStopExposure=false ErrorNumber = %#x, want NotImplemented", mr.ErrorNumber)
	}
	if mr := put(t, s, "/api/v1/camera/0/pulseguide",
		url.Values{"Direction": {"0"}, "Duration": {"10"}}); mr.ErrorNumber != ErrNumNotImplemented {
		t.Errorf("pulseguide with CanPulseGuide=false ErrorNumber = %#x, want NotImplemented", mr.ErrorNumber)
	}
}

// --- gain-less camera: CanGain/CanOffset off, getters left at Base defaults ---

type gainlessCamera struct {
	BaseCamera
	setterCalled bool
}

func newGainlessCamera() *gainlessCamera {
	c := &gainlessCamera{}
	c.ID = "gainless-cam"
	c.DevName = "GainlessCam"
	c.IfaceVer = 4
	c.MarkConnected()
	return c
}

func (c *gainlessCamera) CanGain() bool     { return false }
func (c *gainlessCamera) CanOffset() bool   { return false }
func (c *gainlessCamera) SetGain(int) error { c.setterCalled = true; return nil }

func TestGateGainOffsetCapability(t *testing.T) {
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	cam := newGainlessCamera()
	if err := s.Register(CameraType, 0, cam); err != nil {
		t.Fatalf("register: %v", err)
	}

	// The whole family answers NotImplemented, and Gain must NOT read as 0 —
	// a client treats a zero as a real setting and writes it into headers.
	for _, m := range []string{
		"gain", "gainmin", "gainmax", "gains",
		"offset", "offsetmin", "offsetmax", "offsets",
	} {
		if vr := getValue(t, s, "/api/v1/camera/0/"+m); vr.ErrorNumber != ErrNumNotImplemented {
			t.Errorf("GET %s with CanGain/CanOffset=false ErrorNumber = %#x, want NotImplemented",
				m, vr.ErrorNumber)
		}
	}
	if mr := put(t, s, "/api/v1/camera/0/gain", url.Values{"Gain": {"10"}}); mr.ErrorNumber != ErrNumNotImplemented {
		t.Errorf("PUT gain with CanGain=false ErrorNumber = %#x, want NotImplemented", mr.ErrorNumber)
	}
	if mr := put(t, s, "/api/v1/camera/0/offset", url.Values{"Offset": {"10"}}); mr.ErrorNumber != ErrNumNotImplemented {
		t.Errorf("PUT offset with CanOffset=false ErrorNumber = %#x, want NotImplemented", mr.ErrorNumber)
	}
	if cam.setterCalled {
		t.Error("PUT gain reached the driver; the capability gate must reject it first")
	}

	// Default (BaseCamera) is CanGain=true: naiveCamera's gain keeps serving,
	// so the flags are backward-compatible for drivers that predate them.
	s2 := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	if err := s2.Register(CameraType, 0, newNaiveCamera()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if vr := getValue(t, s2, "/api/v1/camera/0/gain"); vr.ErrorNumber != 0 {
		t.Errorf("GET gain with default CanGain ErrorNumber = %#x, want success", vr.ErrorNumber)
	}
}

// --- naive switch: accepts every id ---

type naiveSwitch struct {
	BaseSwitch
	sets int
}

func newNaiveSwitch() *naiveSwitch {
	sw := &naiveSwitch{}
	sw.ID = "naive-switch"
	sw.DevName = "NaiveSwitch"
	sw.IfaceVer = 3
	sw.MarkConnected()
	return sw
}

func (s *naiveSwitch) MaxSwitch() int                      { return 2 }
func (s *naiveSwitch) GetSwitch(int) (bool, error)         { return true, nil } // any id "works"
func (s *naiveSwitch) GetSwitchValue(int) (float64, error) { return 1, nil }
func (s *naiveSwitch) SetSwitch(int, bool) error           { s.sets++; return nil }
func (s *naiveSwitch) SetSwitchValue(int, float64) error   { s.sets++; return nil }
func (s *naiveSwitch) MinSwitchValue(int) (float64, error) { return 0, nil }
func (s *naiveSwitch) MaxSwitchValue(int) (float64, error) { return 10, nil }

func TestGateSwitch(t *testing.T) {
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	sw := newNaiveSwitch()
	if err := s.Register(SwitchType, 0, sw); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Out-of-range ids are rejected on every member even though the naive
	// driver would accept them.
	for _, m := range []string{"getswitch", "getswitchvalue", "canwrite", "getswitchname",
		"getswitchdescription", "minswitchvalue", "maxswitchvalue", "switchstep",
		"canasync", "statechangecomplete"} {
		if vr := getValue(t, s, "/api/v1/switch/0/"+m+"?Id=2"); vr.ErrorNumber != ErrNumInvalidValue {
			t.Errorf("GET %s Id=2 ErrorNumber = %#x, want InvalidValue", m, vr.ErrorNumber)
		}
	}
	if mr := put(t, s, "/api/v1/switch/0/setswitch",
		url.Values{"Id": {"-1"}, "State": {"true"}}); mr.ErrorNumber != ErrNumInvalidValue {
		t.Errorf("setswitch Id=-1 ErrorNumber = %#x, want InvalidValue", mr.ErrorNumber)
	}
	// Value outside the advertised Min/MaxSwitchValue range.
	if mr := put(t, s, "/api/v1/switch/0/setswitchvalue",
		url.Values{"Id": {"0"}, "Value": {"11"}}); mr.ErrorNumber != ErrNumInvalidValue {
		t.Errorf("setswitchvalue 11 of max 10 ErrorNumber = %#x, want InvalidValue", mr.ErrorNumber)
	}
	if sw.sets != 0 {
		t.Errorf("invalid writes reached the naive driver %d times", sw.sets)
	}
	// Async members on a CanAsync=false switch → NotImplemented.
	if mr := put(t, s, "/api/v1/switch/0/setasync",
		url.Values{"Id": {"0"}, "State": {"true"}}); mr.ErrorNumber != ErrNumNotImplemented {
		t.Errorf("setasync with CanAsync=false ErrorNumber = %#x, want NotImplemented", mr.ErrorNumber)
	}
}

// --- remaining device types, one naive driver each ---

type naiveDome struct{ BaseDome }

func (d *naiveDome) CanSetAzimuth() bool         { return true }
func (d *naiveDome) SlewToAzimuth(float64) error { return nil }

type naiveFilterWheel struct{ BaseFilterWheel }

func (f *naiveFilterWheel) Names() []string       { return []string{"R", "G", "B"} }
func (f *naiveFilterWheel) SetPosition(int) error { return nil }

type naiveRotator struct{ BaseRotator }

func (r *naiveRotator) MoveAbsolute(float64) error { return nil }

type naiveCoverCal struct{ BaseCoverCalibrator }

func (c *naiveCoverCal) CalibratorState() CalibratorStatus { return CalibratorOff }
func (c *naiveCoverCal) MaxBrightness() int                { return 100 }
func (c *naiveCoverCal) CalibratorOn(int) error            { return nil }

type naiveObsCon struct{ BaseObservingConditions }

func (o *naiveObsCon) SensorDescription(string) (string, error) { return "anything", nil }

type naiveFocuser struct {
	BaseFocuser
	moved bool
}

func (f *naiveFocuser) TempComp() bool          { return true }
func (f *naiveFocuser) TempCompAvailable() bool { return true }
func (f *naiveFocuser) Move(int) error          { f.moved = true; return nil }

func TestGateOtherDevices(t *testing.T) {
	s := New(Config{Discovery: DiscoveryConfig{Mode: DiscoveryOff}})
	dome := &naiveDome{}
	dome.ID, dome.DevName, dome.IfaceVer = "naive-dome", "NaiveDome", 3
	dome.MarkConnected()
	fw := &naiveFilterWheel{}
	fw.ID, fw.DevName, fw.IfaceVer = "naive-fw", "NaiveFW", 3
	fw.MarkConnected()
	rot := &naiveRotator{}
	rot.ID, rot.DevName, rot.IfaceVer = "naive-rot", "NaiveRot", 4
	rot.MarkConnected()
	cc := &naiveCoverCal{}
	cc.ID, cc.DevName, cc.IfaceVer = "naive-cc", "NaiveCC", 2
	cc.MarkConnected()
	oc := &naiveObsCon{}
	oc.ID, oc.DevName, oc.IfaceVer = "naive-oc", "NaiveOC", 2
	oc.MarkConnected()
	foc := &naiveFocuser{}
	foc.ID, foc.DevName, foc.IfaceVer = "naive-foc", "NaiveFoc", 2 // V2: TempComp move rule applies
	foc.MarkConnected()
	for typ, dev := range map[DeviceType]Device{
		DomeType: dome, FilterWheelType: fw, RotatorType: rot,
		CoverCalibratorType: cc, ObservingConditionsType: oc, FocuserType: foc,
	} {
		if err := s.Register(typ, 0, dev); err != nil {
			t.Fatalf("register %s: %v", typ, err)
		}
	}

	// Dome: azimuth range; shutter methods NotImplemented (CanSetShutter false).
	if mr := put(t, s, "/api/v1/dome/0/slewtoazimuth", url.Values{"Azimuth": {"370"}}); mr.ErrorNumber != ErrNumInvalidValue {
		t.Errorf("dome slewtoazimuth 370 ErrorNumber = %#x, want InvalidValue", mr.ErrorNumber)
	}
	if mr := put(t, s, "/api/v1/dome/0/openshutter", url.Values{}); mr.ErrorNumber != ErrNumNotImplemented {
		t.Errorf("dome openshutter ErrorNumber = %#x, want NotImplemented", mr.ErrorNumber)
	}

	// FilterWheel: slot range.
	if mr := put(t, s, "/api/v1/filterwheel/0/position", url.Values{"Position": {"3"}}); mr.ErrorNumber != ErrNumInvalidValue {
		t.Errorf("filterwheel position 3 of 3 slots ErrorNumber = %#x, want InvalidValue", mr.ErrorNumber)
	}

	// Rotator: angle range; reverse NotImplemented (CanReverse false).
	if mr := put(t, s, "/api/v1/rotator/0/moveabsolute", url.Values{"Position": {"360"}}); mr.ErrorNumber != ErrNumInvalidValue {
		t.Errorf("rotator moveabsolute 360 ErrorNumber = %#x, want InvalidValue", mr.ErrorNumber)
	}
	if mr := put(t, s, "/api/v1/rotator/0/reverse", url.Values{"Reverse": {"true"}}); mr.ErrorNumber != ErrNumNotImplemented {
		t.Errorf("rotator reverse ErrorNumber = %#x, want NotImplemented", mr.ErrorNumber)
	}

	// CoverCalibrator: brightness range; cover methods NotImplemented (no cover).
	if mr := put(t, s, "/api/v1/covercalibrator/0/calibratoron", url.Values{"Brightness": {"101"}}); mr.ErrorNumber != ErrNumInvalidValue {
		t.Errorf("calibratoron 101 of max 100 ErrorNumber = %#x, want InvalidValue", mr.ErrorNumber)
	}
	if mr := put(t, s, "/api/v1/covercalibrator/0/opencover", url.Values{}); mr.ErrorNumber != ErrNumNotImplemented {
		t.Errorf("opencover with no cover ErrorNumber = %#x, want NotImplemented", mr.ErrorNumber)
	}

	// ObservingConditions: unknown sensor name and negative average period.
	if vr := getValue(t, s, "/api/v1/observingconditions/0/sensordescription?SensorName=Flux"); vr.ErrorNumber != ErrNumInvalidValue {
		t.Errorf("sensordescription Flux ErrorNumber = %#x, want InvalidValue", vr.ErrorNumber)
	}
	if mr := put(t, s, "/api/v1/observingconditions/0/averageperiod", url.Values{"AveragePeriod": {"-1"}}); mr.ErrorNumber != ErrNumInvalidValue {
		t.Errorf("averageperiod -1 ErrorNumber = %#x, want InvalidValue", mr.ErrorNumber)
	}

	// Focuser (V2): Move while TempComp is active → InvalidOperation.
	if mr := put(t, s, "/api/v1/focuser/0/move", url.Values{"Position": {"100"}}); mr.ErrorNumber != ErrNumInvalidOperation {
		t.Errorf("focuser V2 move under tempcomp ErrorNumber = %#x, want InvalidOperation", mr.ErrorNumber)
	}
	if foc.moved {
		t.Error("gated focuser move reached the naive driver")
	}
}
