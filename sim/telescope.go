package sim

import (
	"math"
	"sync"
	"time"

	alpacadev "github.com/mikefsq/goalpaca/server"
)

// Telescope is a simulated ASCOM Telescope (German-equatorial mount). RA/Dec
// converge on their targets at a fixed slew rate computed from the clock (no
// background goroutine); Slewing, AtPark, AtHome and IsPulseGuiding are derived
// on read. It models a credible, ConformU-friendly mount with validated writes.
type Telescope struct {
	alpacadev.BaseTelescope

	mu sync.Mutex

	// Slew model (compute-on-read). RA in hours, Dec in degrees.
	raSlewRate  float64 // hours per second
	decSlewRate float64 // degrees per second

	startRA, startDec   float64
	targetRA, targetDec float64
	slewStart           time.Time
	slewing             bool

	// What state to apply when the current slew completes.
	parkOnArrive bool
	homeOnArrive bool

	atPark bool
	atHome bool

	// Stored targets (last values set by the client).
	wantRA, wantDec float64

	// Alt/Az (approximate; stored values).
	altitude, azimuth float64

	tracking     bool
	trackingRate alpacadev.DriveRate

	siteLatitude  float64
	siteLongitude float64
	siteElevation float64

	raRate, decRate         float64
	guideRateRA, guideRateD float64
	doesRefraction          bool
	slewSettleTime          int
	sideOfPier              alpacadev.PierSide

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
		trackingRate:  alpacadev.DriveSidereal,
		siteLatitude:  45.0,
		siteLongitude: 0.0,
		siteElevation: 100.0,
		guideRateRA:   0.5 / 3600.0 * 15.0, // ~half sidereal, deg/s
		guideRateD:    0.5 / 3600.0 * 15.0,
		sideOfPier:    alpacadev.PierEast,
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

// currentRALocked returns the present right ascension (hours). Caller holds t.mu.
func (t *Telescope) currentRALocked() float64 {
	if !t.slewing {
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
	if !t.slewing {
		return t.startDec
	}
	dist := t.targetDec - t.startDec
	travel := time.Since(t.slewStart).Seconds() * t.decSlewRate
	if travel >= math.Abs(dist) {
		return t.targetDec
	}
	return t.startDec + math.Copysign(travel, dist)
}

// beginSlewLocked starts a slew to the given RA/Dec target. Caller holds t.mu.
func (t *Telescope) beginSlewLocked(ra, dec float64) {
	t.startRA = t.currentRALocked()
	t.startDec = t.currentDecLocked()
	t.targetRA = ra
	t.targetDec = dec
	t.slewStart = time.Now()
	t.slewing = (t.startRA != t.targetRA) || (t.startDec != t.targetDec)
	t.atPark = false
	t.atHome = false
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

func (t *Telescope) AlignmentMode() alpacadev.AlignmentMode {
	return alpacadev.AlignGermanPolar
}

func (t *Telescope) EquatorialSystem() alpacadev.EquatorialCoordinateType {
	return alpacadev.EquJ2000
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
	return t.slewing
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

func (t *Telescope) TargetRightAscension() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.wantRA
}

func (t *Telescope) SetTargetRightAscension(v float64) error {
	if v < 0 || v >= 24 {
		return alpacadev.ErrInvalidValue
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.wantRA = v
	return nil
}

func (t *Telescope) TargetDeclination() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.wantDec
}

func (t *Telescope) SetTargetDeclination(v float64) error {
	if v < -90 || v > 90 {
		return alpacadev.ErrInvalidValue
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.wantDec = v
	return nil
}

// --- slews ---

func (t *Telescope) SlewToCoordinates(ra, dec float64) error {
	return t.SlewToCoordinatesAsync(ra, dec)
}

func (t *Telescope) SlewToCoordinatesAsync(ra, dec float64) error {
	if ra < 0 || ra >= 24 || dec < -90 || dec > 90 {
		return alpacadev.ErrInvalidValue
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.wantRA = ra
	t.wantDec = dec
	t.beginSlewLocked(ra, dec)
	return nil
}

func (t *Telescope) SlewToTarget() error {
	return t.SlewToTargetAsync()
}

func (t *Telescope) SlewToTargetAsync() error {
	t.mu.Lock()
	ra, dec := t.wantRA, t.wantDec
	t.mu.Unlock()
	return t.SlewToCoordinatesAsync(ra, dec)
}

func (t *Telescope) SyncToCoordinates(ra, dec float64) error {
	if ra < 0 || ra >= 24 || dec < -90 || dec > 90 {
		return alpacadev.ErrInvalidValue
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.wantRA = ra
	t.wantDec = dec
	t.startRA = ra
	t.startDec = dec
	t.targetRA = ra
	t.targetDec = dec
	t.slewing = false
	return nil
}

func (t *Telescope) SyncToTarget() error {
	t.mu.Lock()
	ra, dec := t.wantRA, t.wantDec
	t.mu.Unlock()
	return t.SyncToCoordinates(ra, dec)
}

func (t *Telescope) SlewToAltAz(az, alt float64) error {
	return t.SlewToAltAzAsync(az, alt)
}

func (t *Telescope) SlewToAltAzAsync(az, alt float64) error {
	if az < 0 || az >= 360 || alt < 0 || alt > 90 {
		return alpacadev.ErrInvalidValue
	}
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
	if az < 0 || az >= 360 || alt < 0 || alt > 90 {
		return alpacadev.ErrInvalidValue
	}
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
	t.tracking = v
	return nil
}

func (t *Telescope) TrackingRate() alpacadev.DriveRate {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.trackingRate
}

func (t *Telescope) SetTrackingRate(r alpacadev.DriveRate) error {
	if r < alpacadev.DriveSidereal || r > alpacadev.DriveKing {
		return alpacadev.ErrInvalidValue
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.trackingRate = r
	return nil
}

func (t *Telescope) TrackingRates() []alpacadev.DriveRate {
	return []alpacadev.DriveRate{
		alpacadev.DriveSidereal,
		alpacadev.DriveLunar,
		alpacadev.DriveSolar,
		alpacadev.DriveKing,
	}
}

// --- site ---

func (t *Telescope) SiteLatitude() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.siteLatitude
}

func (t *Telescope) SetSiteLatitude(v float64) error {
	if v < -90 || v > 90 {
		return alpacadev.ErrInvalidValue
	}
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
	if v < -180 || v > 180 { // ASCOM SiteLongitude range, positive East
		return alpacadev.ErrInvalidValue
	}
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
	if v < -300 || v > 10000 {
		return alpacadev.ErrInvalidValue
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.siteElevation = v
	return nil
}

// --- time ---

func (t *Telescope) UTCDate() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func (t *Telescope) SetUTCDate(s string) error {
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		return alpacadev.ErrInvalidValue
	}
	return nil
}

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
	t.decRate = v
	return nil
}

func (t *Telescope) GuideRateRightAscension() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.guideRateRA
}

func (t *Telescope) SetGuideRateRightAscension(v float64) error {
	if v < 0 {
		return alpacadev.ErrInvalidValue
	}
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
	if v < 0 {
		return alpacadev.ErrInvalidValue
	}
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
	if v < 0 {
		return alpacadev.ErrInvalidValue
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.slewSettleTime = v
	return nil
}

func (t *Telescope) SideOfPier() alpacadev.PierSide {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sideOfPier
}

func (t *Telescope) SetSideOfPier(v alpacadev.PierSide) error {
	if v != alpacadev.PierEast && v != alpacadev.PierWest {
		return alpacadev.ErrInvalidValue
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sideOfPier = v
	return nil
}

func (t *Telescope) DestinationSideOfPier(ra, dec float64) (alpacadev.PierSide, error) {
	if ra < 12 {
		return alpacadev.PierEast, nil
	}
	return alpacadev.PierWest, nil
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

func (t *Telescope) PulseGuide(direction alpacadev.GuideDirection, duration int) error {
	if duration < 0 {
		return alpacadev.ErrInvalidValue
	}
	if direction < alpacadev.GuideNorth || direction > alpacadev.GuideWest {
		return alpacadev.ErrInvalidValue
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pulseGuiding = true
	t.pulseUntil = time.Now().Add(time.Duration(duration) * time.Millisecond)
	return nil
}

// --- axis motion ---

func (t *Telescope) CanMoveAxis(axis alpacadev.TelescopeAxis) bool {
	return axis == alpacadev.AxisPrimary || axis == alpacadev.AxisSecondary
}

func (t *Telescope) AxisRates(axis alpacadev.TelescopeAxis) []alpacadev.AxisRate {
	return []alpacadev.AxisRate{{Minimum: 0, Maximum: 5}}
}

func (t *Telescope) MoveAxis(axis alpacadev.TelescopeAxis, rate float64) error {
	if !t.CanMoveAxis(axis) {
		return alpacadev.ErrInvalidValue
	}
	rates := t.AxisRates(axis)
	for _, r := range rates {
		if math.Abs(rate) >= r.Minimum && math.Abs(rate) <= r.Maximum {
			return nil
		}
	}
	return alpacadev.ErrInvalidValue
}
