package sim

import (
	"math"
	"sync"
	"time"

	"github.com/mikefsq/goalpaca/server"
)

// Telescope is a simulated ASCOM Telescope (German-equatorial mount). RA/Dec
// converge on their targets at a fixed slew rate computed from the clock (no
// background goroutine); Slewing, AtPark, AtHome and IsPulseGuiding are derived
// on read. It models the mount's motion only — the server library validates
// writes and applies the parked/target/rate protocol rules before calling in.
type Telescope struct {
	server.BaseTelescope

	mu sync.Mutex

	// Slew model (compute-on-read). RA in hours, Dec in degrees.
	raSlewRate  float64 // hours per second
	decSlewRate float64 // degrees per second

	startRA, startDec   float64
	targetRA, targetDec float64
	slewStart           time.Time
	slewing             bool

	// MoveAxis model (compute-on-read): a nonzero rate drifts the axis
	// linearly from startRA/startDec since axisStart. Axis 0 moves RA
	// (rate deg/s ÷ 15 → hours/s), axis 1 moves Dec; axis 2 (Tertiary) is
	// unsupported (CanMoveAxis is false) and never drifts the pointing.
	// Sized to 3 so any TelescopeAxis value indexes safely. Mutually
	// exclusive with a target slew: MoveAxis freezes and takes over any slew
	// in progress.
	axisRates [3]float64 // deg/s, as given by the client
	axisStart time.Time

	// Rate-offset model (compute-on-read): while Tracking is on (and no slew
	// or MoveAxis is in progress) the pointing drifts at the ITelescope
	// offset rates — RightAscensionRate (seconds of RA per second) and
	// DeclinationRate (arcseconds per second) — accrued since rateEpoch.
	// ConformU measures this motion over wall-clock time. Every state
	// transition folds accrued drift via settleRatesLocked so elapsed time is
	// never double-counted.
	rateEpoch time.Time

	// What state to apply when the current slew completes.
	parkOnArrive bool
	homeOnArrive bool

	atPark bool
	atHome bool

	// Stored targets (last values set by the client). The Set flags implement
	// the ASCOM read-before-set rule: Target* reads are InvalidOperation until
	// a target has been established (explicitly or by a coordinate slew/sync).
	wantRA, wantDec       float64
	wantRASet, wantDecSet bool

	// Alt/Az (approximate; stored values).
	altitude, azimuth float64

	tracking     bool
	trackingRate server.DriveRate

	siteLatitude  float64
	siteLongitude float64
	siteElevation float64

	raRate, decRate         float64
	guideRateRA, guideRateD float64
	doesRefraction          bool
	slewSettleTime          int
	sideOfPier              server.PierSide

	pulseGuiding bool
	pulseUntil   time.Time
}

// TelescopeOption configures a simulated Telescope.
type TelescopeOption func(*Telescope)

// WithSlewRate sets the simulated slew rate in degrees per second (applied to
// declination; right ascension uses an equivalent rate in hours per second).
func WithSlewRate(degPerSec float64) TelescopeOption {
	return func(t *Telescope) {
		t.decSlewRate = degPerSec
		t.raSlewRate = degPerSec / 15.0
	}
}

// NewTelescope creates a simulated Telescope parked-capable mount pointed at the
// celestial pole, tracking off. The default slew rate makes a typical slew take
// roughly one second.
func NewTelescope(opts ...TelescopeOption) *Telescope {
	t := &Telescope{
		decSlewRate:   90.0, // deg/s — a 90° dec slew takes ~1s
		raSlewRate:    6.0,  // hours/s — a 6h RA slew takes ~1s
		trackingRate:  server.DriveSidereal,
		siteLatitude:  45.0,
		siteLongitude: 0.0,
		siteElevation: 100.0,
		guideRateRA:   0.5 / 3600.0 * 15.0, // ~half sidereal, deg/s
		guideRateD:    0.5 / 3600.0 * 15.0,
		sideOfPier:    server.PierEast,
		startDec:      90.0,
		targetDec:     90.0,
		wantDec:       90.0,
	}
	t.ID = "goalpaca-sim-telescope-1"
	t.DevName = "Alpaca Telescope Simulator"
	t.Desc = "goalpaca simulated telescope"
	t.Info = "goalpaca sim"
	t.Version = "1.0"
	t.IfaceVer = 4
	for _, o := range opts {
		o(t)
	}
	return t
}

// --- slew model (compute-on-read) ---

// settleLocked advances the slew to the present time, completing it (and applying
// any park/home transition) once both axes have reached their targets. Caller
// holds t.mu.
func (t *Telescope) settleLocked() {
	if !t.slewing {
		return
	}
	elapsed := time.Since(t.slewStart).Seconds()

	raDist := t.targetRA - t.startRA
	decDist := t.targetDec - t.startDec

	raTravel := elapsed * t.raSlewRate
	decTravel := elapsed * t.decSlewRate

	raDone := raTravel >= math.Abs(raDist)
	decDone := decTravel >= math.Abs(decDist)

	if raDone && decDone {
		t.startRA = t.targetRA
		t.startDec = t.targetDec
		t.slewing = false
		t.rateEpoch = time.Now() // rate-offset drift resumes from completion
		if t.parkOnArrive {
			t.parkOnArrive = false
			t.atPark = true
			t.tracking = false
		}
		if t.homeOnArrive {
			t.homeOnArrive = false
			t.atHome = true
		}
	}
}

// axisMovingLocked reports whether a MoveAxis drift is active. Caller holds t.mu.
func (t *Telescope) axisMovingLocked() bool {
	return t.axisRates[0] != 0 || t.axisRates[1] != 0
}

// rateDriftActiveLocked reports whether the tracking rate offsets are moving
// the pointing. Rate offsets are only valid when tracking at Sidereal
// (ITelescopeV4). Caller holds t.mu.
func (t *Telescope) rateDriftActiveLocked() bool {
	return t.tracking && t.trackingRate == server.DriveSidereal &&
		!t.slewing && !t.axisMovingLocked() &&
		(t.raRate != 0 || t.decRate != 0) && !t.rateEpoch.IsZero()
}

// settleRatesLocked folds accumulated rate-offset drift into the resting
// position and restarts the drift clock. Call at every state transition that
// changes what governs the pointing. Caller holds t.mu.
func (t *Telescope) settleRatesLocked() {
	if t.rateDriftActiveLocked() {
		el := time.Since(t.rateEpoch).Seconds()
		t.startRA = wrap24(t.startRA + t.raRate*el/3600.0)      // sec RA → hours
		t.startDec = clampDec(t.startDec + t.decRate*el/3600.0) // arcsec → deg
		t.targetRA, t.targetDec = t.startRA, t.startDec
	}
	t.rateEpoch = time.Now()
}

// settleAxesLocked folds accumulated MoveAxis drift into the resting position
// and restarts the drift clock. Caller holds t.mu.
func (t *Telescope) settleAxesLocked() {
	if !t.axisMovingLocked() {
		return
	}
	el := time.Since(t.axisStart).Seconds()
	t.startRA = wrap24(t.startRA + t.axisRates[0]/15.0*el)
	t.startDec = clampDec(t.startDec + t.axisRates[1]*el)
	t.targetRA, t.targetDec = t.startRA, t.startDec
	t.axisStart = time.Now()
}

// currentRALocked returns the present right ascension (hours). Caller holds t.mu.
func (t *Telescope) currentRALocked() float64 {
	if t.axisMovingLocked() {
		el := time.Since(t.axisStart).Seconds()
		return wrap24(t.startRA + t.axisRates[0]/15.0*el)
	}
	if !t.slewing {
		if t.rateDriftActiveLocked() {
			el := time.Since(t.rateEpoch).Seconds()
			return wrap24(t.startRA + t.raRate*el/3600.0)
		}
		return t.startRA
	}
	dist := t.targetRA - t.startRA
	travel := time.Since(t.slewStart).Seconds() * t.raSlewRate
	if travel >= math.Abs(dist) {
		return t.targetRA
	}
	return t.startRA + math.Copysign(travel, dist)
}

// currentDecLocked returns the present declination (degrees). Caller holds t.mu.
func (t *Telescope) currentDecLocked() float64 {
	if t.axisMovingLocked() {
		el := time.Since(t.axisStart).Seconds()
		return clampDec(t.startDec + t.axisRates[1]*el)
	}
	if !t.slewing {
		if t.rateDriftActiveLocked() {
			el := time.Since(t.rateEpoch).Seconds()
			return clampDec(t.startDec + t.decRate*el/3600.0)
		}
		return t.startDec
	}
	dist := t.targetDec - t.startDec
	travel := time.Since(t.slewStart).Seconds() * t.decSlewRate
	if travel >= math.Abs(dist) {
		return t.targetDec
	}
	return t.startDec + math.Copysign(travel, dist)
}

// beginSlewLocked starts a slew to the given RA/Dec target, taking over from
// any MoveAxis or rate-offset drift. Caller holds t.mu.
func (t *Telescope) beginSlewLocked(ra, dec float64) {
	t.settleAxesLocked()
	t.axisRates = [3]float64{}
	t.settleRatesLocked()
	t.startRA = t.currentRALocked()
	t.startDec = t.currentDecLocked()
	t.targetRA = ra
	t.targetDec = dec
	t.slewStart = time.Now()
	t.slewing = (t.startRA != t.targetRA) || (t.startDec != t.targetDec)
	t.atPark = false
	t.atHome = false
}

func wrap24(h float64) float64 {
	h = math.Mod(h, 24)
	if h < 0 {
		h += 24
	}
	return h
}

func clampDec(d float64) float64 {
	if d > 90 {
		return 90
	}
	if d < -90 {
		return -90
	}
	return d
}

// --- capability flags ---

func (t *Telescope) CanFindHome() bool              { return true }
func (t *Telescope) CanPark() bool                  { return true }
func (t *Telescope) CanPulseGuide() bool            { return true }
func (t *Telescope) CanSetDeclinationRate() bool    { return true }
func (t *Telescope) CanSetGuideRates() bool         { return true }
func (t *Telescope) CanSetPark() bool               { return true }
func (t *Telescope) CanSetPierSide() bool           { return true }
func (t *Telescope) CanSetRightAscensionRate() bool { return true }
func (t *Telescope) CanSetTracking() bool           { return true }
func (t *Telescope) CanSlew() bool                  { return true }
func (t *Telescope) CanSlewAltAz() bool             { return true }
func (t *Telescope) CanSlewAltAzAsync() bool        { return true }
func (t *Telescope) CanSlewAsync() bool             { return true }
func (t *Telescope) CanSync() bool                  { return true }
func (t *Telescope) CanSyncAltAz() bool             { return true }
func (t *Telescope) CanUnpark() bool                { return true }

func (t *Telescope) AlignmentMode() server.AlignmentMode {
	return server.AlignGermanPolar
}

func (t *Telescope) EquatorialSystem() server.EquatorialCoordinateType {
	return server.EquJ2000
}

// --- optics ---

func (t *Telescope) ApertureDiameter() float64 { return 0.2 }
func (t *Telescope) ApertureArea() float64     { return math.Pi * 0.1 * 0.1 } // ~0.0314 m²
func (t *Telescope) FocalLength() float64      { return 1.0 }

// --- position ---

func (t *Telescope) RightAscension() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.settleLocked()
	return t.currentRALocked()
}

func (t *Telescope) Declination() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.settleLocked()
	return t.currentDecLocked()
}

func (t *Telescope) Altitude() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.altitude
}

func (t *Telescope) Azimuth() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.azimuth
}

func (t *Telescope) Slewing() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.settleLocked()
	// A MoveAxis drift is motion too: ConformU requires Slewing == true while
	// any nonzero axis rate is applied.
	return t.slewing || t.axisMovingLocked()
}

func (t *Telescope) AtPark() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.settleLocked()
	return t.atPark
}

func (t *Telescope) AtHome() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.settleLocked()
	return t.atHome
}

// --- targets ---

func (t *Telescope) TargetRightAscension() (float64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.wantRASet {
		// ASCOM read-before-set rule: no target has been established yet.
		return 0, server.ErrInvalidOperation
	}
	return t.wantRA, nil
}

func (t *Telescope) SetTargetRightAscension(v float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.wantRA = v
	t.wantRASet = true
	return nil
}

func (t *Telescope) TargetDeclination() (float64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.wantDecSet {
		return 0, server.ErrInvalidOperation
	}
	return t.wantDec, nil
}

func (t *Telescope) SetTargetDeclination(v float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.wantDec = v
	t.wantDecSet = true
	return nil
}

// --- slews ---

func (t *Telescope) SlewToCoordinates(ra, dec float64) error {
	if err := t.SlewToCoordinatesAsync(ra, dec); err != nil {
		return err
	}
	t.waitSlewDone()
	return nil
}

// waitSlewDone blocks until the in-progress slew settles — the synchronous
// slew contract: the method returns with the mount AT the target (ConformU
// reads the position immediately on return). Sim slews finish in ~1s.
func (t *Telescope) waitSlewDone() {
	for i := 0; i < 600; i++ { // 30s cap
		if !t.Slewing() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (t *Telescope) SlewToCoordinatesAsync(ra, dec float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.wantRA, t.wantRASet = ra, true // a coordinate slew establishes the target
	t.wantDec, t.wantDecSet = dec, true
	t.beginSlewLocked(ra, dec)
	return nil
}

func (t *Telescope) SlewToTarget() error {
	if err := t.SlewToTargetAsync(); err != nil {
		return err
	}
	t.waitSlewDone()
	return nil
}

func (t *Telescope) SlewToTargetAsync() error {
	t.mu.Lock()
	ra, dec := t.wantRA, t.wantDec
	t.mu.Unlock()
	return t.SlewToCoordinatesAsync(ra, dec)
}

func (t *Telescope) SyncToCoordinates(ra, dec float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.wantRA, t.wantRASet = ra, true
	t.wantDec, t.wantDecSet = dec, true
	t.startRA = ra
	t.startDec = dec
	t.targetRA = ra
	t.targetDec = dec
	t.slewing = false
	t.rateEpoch = time.Now() // sync repositions; drift resumes from here
	return nil
}

func (t *Telescope) SyncToTarget() error {
	t.mu.Lock()
	ra, dec := t.wantRA, t.wantDec
	t.mu.Unlock()
	return t.SyncToCoordinates(ra, dec)
}

func (t *Telescope) SlewToAltAz(az, alt float64) error {
	if err := t.SlewToAltAzAsync(az, alt); err != nil {
		return err
	}
	t.waitSlewDone()
	return nil
}

func (t *Telescope) SlewToAltAzAsync(az, alt float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.azimuth = az
	t.altitude = alt
	// Approximate: mark a brief (~1s) slew while leaving the equatorial readout
	// at its current value. The slew start/target span one second of RA travel.
	cur := t.currentRALocked()
	curDec := t.currentDecLocked()
	t.startRA = cur
	t.startDec = curDec
	t.targetRA = math.Mod(cur+t.raSlewRate, 24) // one second of travel
	t.targetDec = curDec
	t.slewStart = time.Now()
	t.slewing = true
	t.atPark = false
	t.atHome = false
	return nil
}

func (t *Telescope) SyncToAltAz(az, alt float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.azimuth = az
	t.altitude = alt
	return nil
}

func (t *Telescope) AbortSlew() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.settleLocked()
	t.settleAxesLocked()
	t.axisRates = [3]float64{}
	t.settleRatesLocked()
	t.startRA = t.currentRALocked()
	t.startDec = t.currentDecLocked()
	t.targetRA = t.startRA
	t.targetDec = t.startDec
	t.slewing = false
	t.parkOnArrive = false
	t.homeOnArrive = false
	return nil
}

// --- park / home ---

func (t *Telescope) Park() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.atPark {
		return nil // already parked; Park is idempotent
	}
	t.beginSlewLocked(0, 90) // park at the celestial pole
	t.parkOnArrive = true
	if !t.slewing {
		t.atPark = true
		t.tracking = false
	}
	return nil
}

func (t *Telescope) Unpark() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.atPark = false
	return nil
}

func (t *Telescope) SetPark() error { return nil }

func (t *Telescope) FindHome() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.beginSlewLocked(0, 90)
	t.homeOnArrive = true
	if !t.slewing {
		t.atHome = true
	}
	return nil
}

// --- tracking ---

func (t *Telescope) Tracking() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tracking
}

func (t *Telescope) SetTracking(v bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.settleRatesLocked() // fold drift accrued under the old tracking state
	t.tracking = v
	return nil
}

func (t *Telescope) TrackingRate() server.DriveRate {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.trackingRate
}

func (t *Telescope) SetTrackingRate(r server.DriveRate) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.settleRatesLocked()
	// ITelescopeV4: rate offsets are only valid when tracking at Sidereal, so
	// changing the drive rate zeroes them (and reads return 0 afterwards).
	if r != t.trackingRate {
		t.raRate, t.decRate = 0, 0
	}
	t.trackingRate = r
	return nil
}

func (t *Telescope) TrackingRates() []server.DriveRate {
	return []server.DriveRate{
		server.DriveSidereal,
		server.DriveLunar,
		server.DriveSolar,
		server.DriveKing,
	}
}

// --- site ---

func (t *Telescope) SiteLatitude() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.siteLatitude
}

func (t *Telescope) SetSiteLatitude(v float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.siteLatitude = v
	return nil
}

func (t *Telescope) SiteLongitude() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.siteLongitude
}

func (t *Telescope) SetSiteLongitude(v float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.siteLongitude = v
	return nil
}

func (t *Telescope) SiteElevation() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.siteElevation
}

func (t *Telescope) SetSiteElevation(v float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.siteElevation = v
	return nil
}

// --- time ---

func (t *Telescope) UTCDate() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func (t *Telescope) SetUTCDate(string) error { return nil }

// SiderealTime returns local apparent sidereal time in hours, derived from the
// clock and site longitude.
func (t *Telescope) SiderealTime() float64 {
	t.mu.Lock()
	lon := t.siteLongitude
	t.mu.Unlock()
	// Days since J2000.0.
	jd := float64(time.Now().UTC().Unix())/86400.0 + 2440587.5
	d := jd - 2451545.0
	gmst := 18.697374558 + 24.06570982441908*d // hours
	lst := gmst + lon/15.0
	lst = math.Mod(lst, 24)
	if lst < 0 {
		lst += 24
	}
	return lst
}

// --- rates ---

func (t *Telescope) RightAscensionRate() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.raRate
}

func (t *Telescope) SetRightAscensionRate(v float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.settleRatesLocked() // fold drift accrued at the old rate
	t.raRate = v
	return nil
}

func (t *Telescope) DeclinationRate() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.decRate
}

func (t *Telescope) SetDeclinationRate(v float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.settleRatesLocked() // fold drift accrued at the old rate
	t.decRate = v
	return nil
}

func (t *Telescope) GuideRateRightAscension() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.guideRateRA
}

func (t *Telescope) SetGuideRateRightAscension(v float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.guideRateRA = v
	return nil
}

func (t *Telescope) GuideRateDeclination() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.guideRateD
}

func (t *Telescope) SetGuideRateDeclination(v float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.guideRateD = v
	return nil
}

// --- misc properties ---

func (t *Telescope) DoesRefraction() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.doesRefraction
}

func (t *Telescope) SetDoesRefraction(v bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.doesRefraction = v
	return nil
}

func (t *Telescope) SlewSettleTime() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.slewSettleTime
}

func (t *Telescope) SetSlewSettleTime(v int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.slewSettleTime = v
	return nil
}

func (t *Telescope) SideOfPier() server.PierSide {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sideOfPier
}

func (t *Telescope) SetSideOfPier(v server.PierSide) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sideOfPier = v
	return nil
}

// DestinationSideOfPier reports which side of the pier a German-equatorial
// mount would settle on for the given coordinates: decided by the target's
// hour angle (LST − RA), so the answer flips across the meridian — ConformU
// probes both sides and requires different values.
func (t *Telescope) DestinationSideOfPier(ra, dec float64) (server.PierSide, error) {
	ha := math.Mod(t.SiderealTime()-ra, 24)
	if ha < -12 {
		ha += 24
	}
	if ha >= 12 {
		ha -= 24
	}
	if ha < 0 {
		return server.PierWest, nil // target east of the meridian
	}
	return server.PierEast, nil
}

// --- pulse guiding ---

func (t *Telescope) IsPulseGuiding() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pulseGuiding && time.Now().After(t.pulseUntil) {
		t.pulseGuiding = false
	}
	return t.pulseGuiding
}

func (t *Telescope) PulseGuide(direction server.GuideDirection, duration int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Apply the guide displacement (guide rate × duration) to the pointing:
	// ConformU measures the position change a pulse produces. Applied up
	// front for model simplicity; IsPulseGuiding still runs the clock.
	t.settleRatesLocked()
	dur := float64(duration) / 1000.0
	switch direction {
	case server.GuideNorth:
		t.startDec = clampDec(t.startDec + t.guideRateD*dur)
	case server.GuideSouth:
		t.startDec = clampDec(t.startDec - t.guideRateD*dur)
	case server.GuideEast:
		t.startRA = wrap24(t.startRA + t.guideRateRA*dur/15.0) // deg → hours
	case server.GuideWest:
		t.startRA = wrap24(t.startRA - t.guideRateRA*dur/15.0)
	}
	t.targetRA, t.targetDec = t.startRA, t.startDec
	t.pulseGuiding = true
	t.pulseUntil = time.Now().Add(time.Duration(duration) * time.Millisecond)
	return nil
}

// --- axis motion ---

func (t *Telescope) CanMoveAxis(axis server.TelescopeAxis) bool {
	return axis == server.AxisPrimary || axis == server.AxisSecondary
}

func (t *Telescope) AxisRates(axis server.TelescopeAxis) []server.AxisRate {
	return []server.AxisRate{{Minimum: 0, Maximum: 5}}
}

func (t *Telescope) MoveAxis(axis server.TelescopeAxis, rate float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Take over motion: freeze any target slew at its present position, fold
	// accumulated drift, then apply the new rate (0 stops this axis).
	t.settleRatesLocked()
	t.startRA = t.currentRALocked()
	t.startDec = t.currentDecLocked()
	t.slewing = false
	t.parkOnArrive = false
	t.homeOnArrive = false
	t.settleAxesLocked()
	t.axisRates[axis] = rate
	t.axisStart = time.Now()
	if rate != 0 {
		t.atHome = false
	}
	return nil
}
