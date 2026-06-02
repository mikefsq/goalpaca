package conformance

import (
	"errors"
	"testing"
	"time"

	"github.com/mikefsq/goalpaca/client"
	alpacadev "github.com/mikefsq/goalpaca/server"
)

// telescopeRATolerance is the slew arrival tolerance for right ascension (hours).
const telescopeRATolerance = 0.1

// telescopeDecTolerance is the slew arrival tolerance for declination (degrees).
const telescopeDecTolerance = 0.5

// CheckTelescope runs the ConformU Telescope conformance checks against c. Ported
// from ConformU's TelescopeTester, restricted to the behaviour the goalpaca
// simulator implements: NotConnected gating, capability flags, enum/property
// reads, tracking, target round-trips, asynchronous slew/sync arrival,
// park/unpark/home, site round-trips, UTCDate, axis capability/rates, and
// DestinationSideOfPier. Out-of-range writes are expected to fault InvalidValue.
//
// Skipped (the sim does not model these, so ConformU's corresponding checks are
// intentionally omitted): alt/az slew arrival accuracy, precise side-of-pier
// flip semantics, refraction effects, guide-rate precision, and MoveAxis motion.
func CheckTelescope(t *testing.T, c *client.Telescope) {
	t.Helper()

	// NotConnected gating: an operational member must fault while disconnected.
	_ = c.SetConnected(false)
	if _, err := c.RightAscension(); !errors.Is(err, alpacadev.ErrNotConnected) {
		t.Errorf("RightAscension() while disconnected: want NotConnected, got %v", err)
	}
	if err := c.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Capability flags must be readable; assert the ones the sim sets true.
	telescopeAssertCan(t, "CanSlew", c.CanSlew)
	telescopeAssertCan(t, "CanSlewAsync", c.CanSlewAsync)
	telescopeAssertCan(t, "CanSync", c.CanSync)
	telescopeAssertCan(t, "CanPark", c.CanPark)
	telescopeAssertCan(t, "CanUnpark", c.CanUnpark)
	telescopeAssertCan(t, "CanFindHome", c.CanFindHome)
	telescopeAssertCan(t, "CanSetTracking", c.CanSetTracking)
	telescopeAssertCan(t, "CanPulseGuide", c.CanPulseGuide)

	// Enum-typed properties must be readable.
	if mode, err := c.AlignmentMode(); err != nil {
		t.Errorf("AlignmentMode(): %v", err)
	} else if mode != alpacadev.AlignGermanPolar {
		t.Errorf("AlignmentMode() = %v; want AlignGermanPolar", mode)
	}
	if sys, err := c.EquatorialSystem(); err != nil {
		t.Errorf("EquatorialSystem(): %v", err)
	} else if sys != alpacadev.EquJ2000 {
		t.Errorf("EquatorialSystem() = %v; want EquJ2000", sys)
	}
	if _, err := c.SideOfPier(); err != nil {
		t.Errorf("SideOfPier(): %v", err)
	}
	if _, err := c.TrackingRate(); err != nil {
		t.Errorf("TrackingRate(): %v", err)
	}
	if rates, err := c.TrackingRates(); err != nil || len(rates) == 0 {
		t.Errorf("TrackingRates() = %v, %v; want non-empty", rates, err)
	}

	// Tracking read/write.
	if err := c.SetTracking(true); err != nil {
		t.Errorf("SetTracking(true): %v", err)
	}
	if on, err := c.Tracking(); err != nil || !on {
		t.Errorf("Tracking() after enable = %v, %v; want true", on, err)
	}
	if err := c.SetTracking(false); err != nil {
		t.Errorf("SetTracking(false): %v", err)
	}
	if on, err := c.Tracking(); err != nil || on {
		t.Errorf("Tracking() after disable = %v, %v; want false", on, err)
	}

	// Target RA/Dec round-trip, then out-of-range rejections.
	if err := c.SetTargetRightAscension(5); err != nil {
		t.Errorf("SetTargetRightAscension(5): %v", err)
	}
	if ra, err := c.TargetRightAscension(); err != nil || telescopeAbs(ra-5) > telescopeRATolerance {
		t.Errorf("TargetRightAscension() = %v, %v; want ~5", ra, err)
	}
	if err := c.SetTargetDeclination(20); err != nil {
		t.Errorf("SetTargetDeclination(20): %v", err)
	}
	if dec, err := c.TargetDeclination(); err != nil || telescopeAbs(dec-20) > telescopeDecTolerance {
		t.Errorf("TargetDeclination() = %v, %v; want ~20", dec, err)
	}
	if err := c.SetTargetRightAscension(30); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetTargetRightAscension(30): want InvalidValue, got %v", err)
	}
	if err := c.SetTargetDeclination(100); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetTargetDeclination(100): want InvalidValue, got %v", err)
	}

	// Asynchronous slew to coordinates, then out-of-range rejection.
	if err := c.SlewToCoordinatesAsync(10, 25); err != nil {
		t.Errorf("SlewToCoordinatesAsync(10, 25): %v", err)
	} else {
		telescopeWaitSlewDone(t, c)
		if ra, err := c.RightAscension(); err != nil || telescopeAbs(ra-10) > telescopeRATolerance {
			t.Errorf("RightAscension() after slew = %v, %v; want ~10", ra, err)
		}
		if dec, err := c.Declination(); err != nil || telescopeAbs(dec-25) > telescopeDecTolerance {
			t.Errorf("Declination() after slew = %v, %v; want ~25", dec, err)
		}
	}
	if err := c.SlewToCoordinatesAsync(30, 0); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SlewToCoordinatesAsync(30, 0): want InvalidValue, got %v", err)
	}

	// Synchronous sync to coordinates.
	if err := c.SyncToCoordinates(6, 30); err != nil {
		t.Errorf("SyncToCoordinates(6, 30): %v", err)
	} else {
		if ra, err := c.RightAscension(); err != nil || telescopeAbs(ra-6) > telescopeRATolerance {
			t.Errorf("RightAscension() after sync = %v, %v; want ~6", ra, err)
		}
		if dec, err := c.Declination(); err != nil || telescopeAbs(dec-30) > telescopeDecTolerance {
			t.Errorf("Declination() after sync = %v, %v; want ~30", dec, err)
		}
	}

	// Park / Unpark.
	if err := c.Park(); err != nil {
		t.Errorf("Park(): %v", err)
	} else {
		telescopeWaitSlewDone(t, c)
		if parked, err := c.AtPark(); err != nil || !parked {
			t.Errorf("AtPark() after park = %v, %v; want true", parked, err)
		}
	}
	if err := c.Unpark(); err != nil {
		t.Errorf("Unpark(): %v", err)
	}
	if parked, err := c.AtPark(); err != nil || parked {
		t.Errorf("AtPark() after unpark = %v, %v; want false", parked, err)
	}

	// FindHome.
	if err := c.FindHome(); err != nil {
		t.Errorf("FindHome(): %v", err)
	} else {
		telescopeWaitHome(t, c)
		if home, err := c.AtHome(); err != nil || !home {
			t.Errorf("AtHome() after find home = %v, %v; want true", home, err)
		}
	}

	// Site latitude/longitude/elevation round-trip, then out-of-range rejection.
	if err := c.SetSiteLatitude(40); err != nil {
		t.Errorf("SetSiteLatitude(40): %v", err)
	}
	if lat, err := c.SiteLatitude(); err != nil || telescopeAbs(lat-40) > telescopeDecTolerance {
		t.Errorf("SiteLatitude() = %v, %v; want ~40", lat, err)
	}
	if err := c.SetSiteLongitude(-105); err != nil {
		t.Errorf("SetSiteLongitude(-105): %v", err)
	}
	if lon, err := c.SiteLongitude(); err != nil || telescopeAbs(lon-(-105)) > telescopeDecTolerance {
		t.Errorf("SiteLongitude() = %v, %v; want ~-105", lon, err)
	}
	if err := c.SetSiteElevation(1600); err != nil {
		t.Errorf("SetSiteElevation(1600): %v", err)
	}
	if elev, err := c.SiteElevation(); err != nil || telescopeAbs(elev-1600) > 1 {
		t.Errorf("SiteElevation() = %v, %v; want ~1600", elev, err)
	}
	if err := c.SetSiteLatitude(200); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetSiteLatitude(200): want InvalidValue, got %v", err)
	}

	// UTCDate read, valid write, and invalid write.
	if utc, err := c.UTCDate(); err != nil || utc == "" {
		t.Errorf("UTCDate() = %q, %v; want non-empty", utc, err)
	}
	if err := c.SetUTCDate("2024-01-02T03:04:05.000Z"); err != nil {
		t.Errorf("SetUTCDate(valid): %v", err)
	}
	if err := c.SetUTCDate("not-a-date"); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetUTCDate(invalid): want InvalidValue, got %v", err)
	}

	// Axis capability and rates.
	if can, err := c.CanMoveAxis(alpacadev.AxisPrimary); err != nil || !can {
		t.Errorf("CanMoveAxis(AxisPrimary) = %v, %v; want true", can, err)
	}
	if can, err := c.CanMoveAxis(alpacadev.AxisTertiary); err != nil || can {
		t.Errorf("CanMoveAxis(AxisTertiary) = %v, %v; want false", can, err)
	}
	if rates, err := c.AxisRates(alpacadev.AxisPrimary); err != nil || len(rates) == 0 {
		t.Errorf("AxisRates(AxisPrimary) = %v, %v; want non-empty", rates, err)
	}

	// DestinationSideOfPier returns a value without error.
	if _, err := c.DestinationSideOfPier(6, 0); err != nil {
		t.Errorf("DestinationSideOfPier(6, 0): %v", err)
	}

	// Remaining capability flags the sim sets true.
	telescopeAssertCan(t, "CanSetGuideRates", c.CanSetGuideRates)
	telescopeAssertCan(t, "CanSetDeclinationRate", c.CanSetDeclinationRate)
	telescopeAssertCan(t, "CanSetRightAscensionRate", c.CanSetRightAscensionRate)
	telescopeAssertCan(t, "CanSetPark", c.CanSetPark)
	telescopeAssertCan(t, "CanSetPierSide", c.CanSetPierSide)
	telescopeAssertCan(t, "CanSlewAltAz", c.CanSlewAltAz)
	telescopeAssertCan(t, "CanSlewAltAzAsync", c.CanSlewAltAzAsync)
	telescopeAssertCan(t, "CanSyncAltAz", c.CanSyncAltAz)
	if can, err := c.CanMoveAxis(alpacadev.AxisSecondary); err != nil || !can {
		t.Errorf("CanMoveAxis(AxisSecondary) = %v, %v; want true", can, err)
	}

	// SiderealTime and pointing/optics reads.
	if st, err := c.SiderealTime(); err != nil || st < 0 || st >= 24 {
		t.Errorf("SiderealTime() = %v, %v; want [0,24)", st, err)
	}
	if alt, err := c.Altitude(); err != nil || alt < 0 || alt > 90 {
		t.Errorf("Altitude() = %v, %v; want [0,90]", alt, err)
	}
	if az, err := c.Azimuth(); err != nil || az < 0 || az >= 360 {
		t.Errorf("Azimuth() = %v, %v; want [0,360)", az, err)
	}
	if area, err := c.ApertureArea(); err != nil || area < 0 {
		t.Errorf("ApertureArea() = %v, %v; want >= 0", area, err)
	}
	if dia, err := c.ApertureDiameter(); err != nil || dia < 0 {
		t.Errorf("ApertureDiameter() = %v, %v; want >= 0", dia, err)
	}
	if fl, err := c.FocalLength(); err != nil || fl < 0 {
		t.Errorf("FocalLength() = %v, %v; want >= 0", fl, err)
	}

	// Guide rates: read, write+read-back, and negative rejection (sim validates).
	if _, err := c.GuideRateRightAscension(); err != nil {
		t.Errorf("GuideRateRightAscension(): %v", err)
	}
	if _, err := c.GuideRateDeclination(); err != nil {
		t.Errorf("GuideRateDeclination(): %v", err)
	}
	if err := c.SetGuideRateRightAscension(0.5); err != nil {
		t.Errorf("SetGuideRateRightAscension(0.5): %v", err)
	}
	if gr, err := c.GuideRateRightAscension(); err != nil || telescopeAbs(gr-0.5) > 1e-6 {
		t.Errorf("GuideRateRightAscension() after set = %v, %v; want ~0.5", gr, err)
	}
	if err := c.SetGuideRateDeclination(0.5); err != nil {
		t.Errorf("SetGuideRateDeclination(0.5): %v", err)
	}
	if gd, err := c.GuideRateDeclination(); err != nil || telescopeAbs(gd-0.5) > 1e-6 {
		t.Errorf("GuideRateDeclination() after set = %v, %v; want ~0.5", gd, err)
	}
	if err := c.SetGuideRateRightAscension(-1); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetGuideRateRightAscension(-1): want InvalidValue, got %v", err)
	}
	if err := c.SetGuideRateDeclination(-1); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetGuideRateDeclination(-1): want InvalidValue, got %v", err)
	}

	// RA/Dec rates: read, write+read-back.
	if _, err := c.RightAscensionRate(); err != nil {
		t.Errorf("RightAscensionRate(): %v", err)
	}
	if err := c.SetRightAscensionRate(0); err != nil {
		t.Errorf("SetRightAscensionRate(0): %v", err)
	}
	if rr, err := c.RightAscensionRate(); err != nil || telescopeAbs(rr) > 1e-6 {
		t.Errorf("RightAscensionRate() after set = %v, %v; want ~0", rr, err)
	}
	if _, err := c.DeclinationRate(); err != nil {
		t.Errorf("DeclinationRate(): %v", err)
	}
	if err := c.SetDeclinationRate(0); err != nil {
		t.Errorf("SetDeclinationRate(0): %v", err)
	}
	if dr, err := c.DeclinationRate(); err != nil || telescopeAbs(dr) > 1e-6 {
		t.Errorf("DeclinationRate() after set = %v, %v; want ~0", dr, err)
	}

	// DoesRefraction: read, toggle+read-back, restore.
	refr, err := c.DoesRefraction()
	if err != nil {
		t.Errorf("DoesRefraction(): %v", err)
	}
	if err := c.SetDoesRefraction(!refr); err != nil {
		t.Errorf("SetDoesRefraction(toggle): %v", err)
	}
	if got, err := c.DoesRefraction(); err != nil || got != !refr {
		t.Errorf("DoesRefraction() after toggle = %v, %v; want %v", got, err, !refr)
	}
	if err := c.SetDoesRefraction(refr); err != nil {
		t.Errorf("SetDoesRefraction(restore): %v", err)
	}

	// SlewSettleTime: read, write+read-back, restore, negative rejection.
	if _, err := c.SlewSettleTime(); err != nil {
		t.Errorf("SlewSettleTime(): %v", err)
	}
	if err := c.SetSlewSettleTime(5); err != nil {
		t.Errorf("SetSlewSettleTime(5): %v", err)
	}
	if st, err := c.SlewSettleTime(); err != nil || st != 5 {
		t.Errorf("SlewSettleTime() after set = %v, %v; want 5", st, err)
	}
	if err := c.SetSlewSettleTime(0); err != nil {
		t.Errorf("SetSlewSettleTime(0): %v", err)
	}
	if err := c.SetSlewSettleTime(-1); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetSlewSettleTime(-1): want InvalidValue, got %v", err)
	}

	// TrackingRate write round-trip, then restore.
	if err := c.SetTrackingRate(alpacadev.DriveLunar); err != nil {
		t.Errorf("SetTrackingRate(DriveLunar): %v", err)
	}
	if tr, err := c.TrackingRate(); err != nil || tr != alpacadev.DriveLunar {
		t.Errorf("TrackingRate() after set = %v, %v; want DriveLunar", tr, err)
	}
	if err := c.SetTrackingRate(alpacadev.DriveSidereal); err != nil {
		t.Errorf("SetTrackingRate(DriveSidereal): %v", err)
	}

	// DestinationSideOfPier varies with RA and is never PierUnknown.
	east, eerr := c.DestinationSideOfPier(6, 0)
	west, werr := c.DestinationSideOfPier(18, 0)
	if eerr != nil || werr != nil {
		t.Errorf("DestinationSideOfPier: %v, %v", eerr, werr)
	}
	if east == alpacadev.PierUnknown || west == alpacadev.PierUnknown {
		t.Errorf("DestinationSideOfPier = %v, %v; neither should be PierUnknown", east, west)
	}
	if east == west {
		t.Errorf("DestinationSideOfPier(6,0)=%v and (18,0)=%v; want different", east, west)
	}

	// Async slew reports Slewing()==true immediately after the call returns.
	if err := c.SlewToCoordinatesAsync(8, 15); err != nil {
		t.Errorf("SlewToCoordinatesAsync(8, 15): %v", err)
	} else {
		if slewing, err := c.Slewing(); err != nil || !slewing {
			t.Errorf("Slewing() immediately after async slew = %v, %v; want true", slewing, err)
		}
		telescopeWaitSlewDone(t, c)
	}

	// AbortSlew stops an in-progress slew.
	if err := c.SlewToCoordinatesAsync(2, -10); err != nil {
		t.Errorf("SlewToCoordinatesAsync(2, -10): %v", err)
	} else {
		if slewing, err := c.Slewing(); err != nil || !slewing {
			t.Errorf("Slewing() before abort = %v, %v; want true", slewing, err)
		}
		if err := c.AbortSlew(); err != nil {
			t.Errorf("AbortSlew(): %v", err)
		}
		telescopeWaitSlewDone(t, c)
	}

	// Synchronous SlewToCoordinates arrival.
	if err := c.SlewToCoordinates(12, 40); err != nil {
		t.Errorf("SlewToCoordinates(12, 40): %v", err)
	} else {
		telescopeWaitSlewDone(t, c)
		if ra, err := c.RightAscension(); err != nil || telescopeAbs(ra-12) > telescopeRATolerance {
			t.Errorf("RightAscension() after sync slew = %v, %v; want ~12", ra, err)
		}
		if dec, err := c.Declination(); err != nil || telescopeAbs(dec-40) > telescopeDecTolerance {
			t.Errorf("Declination() after sync slew = %v, %v; want ~40", dec, err)
		}
	}

	// SetTarget* then SlewToTargetAsync arrival.
	if err := c.SetTargetRightAscension(15); err != nil {
		t.Errorf("SetTargetRightAscension(15): %v", err)
	}
	if err := c.SetTargetDeclination(-20); err != nil {
		t.Errorf("SetTargetDeclination(-20): %v", err)
	}
	if err := c.SlewToTargetAsync(); err != nil {
		t.Errorf("SlewToTargetAsync(): %v", err)
	} else {
		telescopeWaitSlewDone(t, c)
		if ra, err := c.RightAscension(); err != nil || telescopeAbs(ra-15) > telescopeRATolerance {
			t.Errorf("RightAscension() after slew-to-target = %v, %v; want ~15", ra, err)
		}
		if dec, err := c.Declination(); err != nil || telescopeAbs(dec-(-20)) > telescopeDecTolerance {
			t.Errorf("Declination() after slew-to-target = %v, %v; want ~-20", dec, err)
		}
	}

	// SyncToTarget aligns the reported position to the target.
	if err := c.SetTargetRightAscension(9); err != nil {
		t.Errorf("SetTargetRightAscension(9): %v", err)
	}
	if err := c.SetTargetDeclination(5); err != nil {
		t.Errorf("SetTargetDeclination(5): %v", err)
	}
	if err := c.SyncToTarget(); err != nil {
		t.Errorf("SyncToTarget(): %v", err)
	} else {
		if ra, err := c.RightAscension(); err != nil || telescopeAbs(ra-9) > telescopeRATolerance {
			t.Errorf("RightAscension() after sync-to-target = %v, %v; want ~9", ra, err)
		}
		if dec, err := c.Declination(); err != nil || telescopeAbs(dec-5) > telescopeDecTolerance {
			t.Errorf("Declination() after sync-to-target = %v, %v; want ~5", dec, err)
		}
	}

	// Out-of-range sync/site rejections.
	if err := c.SyncToCoordinates(30, 0); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SyncToCoordinates(30, 0): want InvalidValue, got %v", err)
	}
	if err := c.SetSiteLongitude(200); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetSiteLongitude(200): want InvalidValue, got %v", err)
	}
	if err := c.SetSiteElevation(-1000); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetSiteElevation(-1000): want InvalidValue, got %v", err)
	}
	if err := c.SetSiteElevation(99999); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("SetSiteElevation(99999): want InvalidValue, got %v", err)
	}

	// Pulse guiding.
	if guiding, err := c.IsPulseGuiding(); err != nil || guiding {
		t.Errorf("IsPulseGuiding() = %v, %v; want false", guiding, err)
	}
	if err := c.PulseGuide(alpacadev.GuideNorth, 100); err != nil {
		t.Errorf("PulseGuide(GuideNorth, 100): %v", err)
	}

	// AxisRates bounds for the primary axis.
	if rates, err := c.AxisRates(alpacadev.AxisPrimary); err != nil {
		t.Errorf("AxisRates(AxisPrimary): %v", err)
	} else {
		for i, r := range rates {
			if r.Minimum < 0 {
				t.Errorf("AxisRates(AxisPrimary)[%d].Minimum = %v; want >= 0", i, r.Minimum)
			}
			if r.Minimum > r.Maximum {
				t.Errorf("AxisRates(AxisPrimary)[%d]: Minimum %v > Maximum %v", i, r.Minimum, r.Maximum)
			}
		}
	}
}

// telescopeAssertCan reads a capability flag and requires it to be true.
func telescopeAssertCan(t *testing.T, name string, read func() (bool, error)) {
	t.Helper()
	if v, err := read(); err != nil || !v {
		t.Errorf("%s() = %v, %v; want true", name, v, err)
	}
}

// telescopeWaitSlewDone polls Slewing() until it reports false or times out.
func telescopeWaitSlewDone(t *testing.T, c *client.Telescope) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		slewing, err := c.Slewing()
		if err != nil {
			t.Fatalf("Slewing(): %v", err)
		}
		if !slewing {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("telescope still slewing after timeout")
}

// telescopeWaitHome polls AtHome() until it reports true or times out.
func telescopeWaitHome(t *testing.T, c *client.Telescope) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		home, err := c.AtHome()
		if err != nil {
			t.Fatalf("AtHome(): %v", err)
		}
		if home {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("telescope did not reach home after timeout")
}

// telescopeAbs returns the absolute value of v.
func telescopeAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
