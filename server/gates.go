package server

// The Gate functions validate typed calls before dispatch to a driver.
// HTTP handlers apply them automatically; in-process callers must apply the
// matching gates themselves. Request parsing remains in the HTTP handlers.

// NotImplementedError builds the ASCOM NotImplemented (0x400) answer: the device has no such
// control. It is a CAPABILITY answer, not a failure — a client must not count it as a fault.
func NotImplementedError(member string) error { return notImplErr(member) }

// InvalidValueError builds the ASCOM InvalidValue (0x401) answer: the device understood the request
// and the value was out of range.
func InvalidValueError(format string, a ...any) error { return invalidValuef(format, a...) }

// InvalidOperationError builds the ASCOM InvalidOperation (0x40B) answer: a valid value, invalid in
// the device's current state.
func InvalidOperationError(message string) error { return invalidOpErr(message) }

// ParkedError builds the ASCOM Parked (0x408) answer.
func ParkedError(member string) error { return parkedErr(member) }

// ---- Switch -----------------------------------------------------------------

// GateSwitchID rejects IDs outside [0, MaxSwitch). Call it before any
// ID-taking driver method, including capability getters.
func GateSwitchID(sw Switch, id int) error { return validSwitchID(sw, id) }

// GateSwitchValue validates the ID and the channel's Min/Max range.
// If either bound is unavailable, the driver must validate the value.
func GateSwitchValue(sw Switch, id int, value float64) error {
	if err := validSwitchID(sw, id); err != nil {
		return err
	}
	min, err := sw.MinSwitchValue(id)
	if err != nil {
		return nil
	}
	max, err := sw.MaxSwitchValue(id)
	if err != nil {
		return nil
	}
	if value < min || value > max {
		return invalidValuef("switch %d value %g is outside the valid range %g to %g", id, value, min, max)
	}
	return nil
}

// GateSwitchAsync enforces the ISwitchV3 rule that the asynchronous members (SetAsync,
// SetAsyncValue, CancelAsync, StateChangeComplete) answer NotImplemented for a channel whose
// CanAsync is false. The Id is gated first, since CanAsync itself takes one.
func GateSwitchAsync(sw Switch, id int, member string) error {
	if err := validSwitchID(sw, id); err != nil {
		return err
	}
	return switchAsyncGate(sw, id, member)
}

// ---- FilterWheel ------------------------------------------------------------

// GateFilterWheelPosition bounds a slot against the wheel's Names list. ASCOM slots are 0-based;
// -1 is the driver's own "moving" answer and is never a valid REQUEST.
func GateFilterWheelPosition(w FilterWheel, slot int) error {
	if slots := len(w.Names()); slot < 0 || slot >= slots {
		return invalidValuef("Position %d is outside the valid range 0 to %d", slot, slots-1)
	}
	return nil
}

// ---- Focuser ----------------------------------------------------------------

// GateFocuserTempComp answers NotImplemented when temperature compensation is being turned ON for a
// focuser that has none. Turning it OFF is always valid — a device that cannot compensate is
// already not compensating.
func GateFocuserTempComp(f Focuser, on bool) error {
	if on && !f.TempCompAvailable() {
		return notImplErr("TempComp")
	}
	return nil
}

// GateFocuserMove refuses a move while temperature compensation is active, for IFocuserV2 and
// earlier. IFocuserV3 (Platform 6.4+) permits it, so the interface version decides.
func GateFocuserMove(f Focuser) error {
	if f.InterfaceVersion() < 3 && f.TempComp() {
		return invalidOpErr("Move is not valid while temperature compensation is active")
	}
	return nil
}

// ---- Rotator ----------------------------------------------------------------

// GateRotatorReverse answers NotImplemented for a rotator that cannot reverse.
func GateRotatorReverse(r Rotator) error {
	if !r.CanReverse() {
		return notImplErr("Reverse")
	}
	return nil
}

// GateRotatorPosition bounds a sky or mechanical position angle. IRotatorV3 positions are
// 0..359.999 degrees; a relative Move is NOT gated by this, since its argument is an offset.
func GateRotatorPosition(position float64) error {
	if position < 0 || position >= 360 {
		return invalidValuef("Position %g is outside the valid range 0 to 359.999", position)
	}
	return nil
}

// ---- CoverCalibrator --------------------------------------------------------

// GateCoverCalibratorCalibrator answers NotImplemented for the calibrator members of a device that
// has no calibrator (CalibratorState reports NotPresent).
func GateCoverCalibratorCalibrator(cc CoverCalibrator, member string) error {
	if cc.CalibratorState() == CalibratorNotPresent {
		return notImplErr(member)
	}
	return nil
}

// GateCoverCalibratorCover answers NotImplemented for the cover members of a device that has no
// cover — a flat panel with no lid is the common case, not a fault.
func GateCoverCalibratorCover(cc CoverCalibrator, member string) error {
	if cc.CoverState() == CoverNotPresent {
		return notImplErr(member)
	}
	return nil
}

// GateCoverCalibratorOn gates CalibratorOn: the device must have a calibrator, and the brightness
// must be within 0..MaxBrightness.
func GateCoverCalibratorOn(cc CoverCalibrator, brightness int) error {
	if err := GateCoverCalibratorCalibrator(cc, "CalibratorOn"); err != nil {
		return err
	}
	if max := cc.MaxBrightness(); brightness < 0 || brightness > max {
		return invalidValuef("Brightness %d is outside the valid range 0 to %d", brightness, max)
	}
	return nil
}

// ---- ObservingConditions ----------------------------------------------------

// GateObservingConditionsSensor rejects a sensor name outside the canonical
// IObservingConditionsV2 set. allowEmpty admits "", which TimeSinceLastUpdate uses to mean
// "any sensor"; SensorDescription does not.
func GateObservingConditionsSensor(name string, allowEmpty bool) error {
	if !validOCSensor(name, allowEmpty) {
		return invalidValuef("SensorName %q is not an ObservingConditions sensor", name)
	}
	return nil
}

// GateObservingConditionsAveragePeriod rejects a negative averaging window. Zero is valid and means
// "report the instantaneous value".
func GateObservingConditionsAveragePeriod(hours float64) error {
	if hours < 0 {
		return invalidValuef("AveragePeriod %g is negative", hours)
	}
	return nil
}

// ---- shared numeric helpers -------------------------------------------------

// GateRange rejects v outside [lo, hi], naming the member. Exported because the site ranges
// (latitude, longitude, elevation) are spec constants a host may need to apply itself.
func GateRange(member string, v, lo, hi float64) error { return invalidRange(member, v, lo, hi) }

// GateAxisRate reports whether a MoveAxis rate is acceptable: zero (stop) is always allowed, and
// any other magnitude must fall inside one of the device's advertised AxisRates ranges.
func GateAxisRate(t Telescope, axis TelescopeAxis, rate float64) error {
	if !validAxisRate(t, axis, rate) {
		return invalidValuef("Rate %g is outside the axis' supported ranges", rate)
	}
	return nil
}

// nonNegative is the shape repeated across several members whose only constraint is a sign.
func nonNegative(member string, v float64) error {
	if v < 0 {
		return invalidValuef("%s %g is negative", member, v)
	}
	return nil
}

// ---- Camera -----------------------------------------------------------------

// GateCameraBayerOffset enforces the ICameraV4 rule that a MONOCHROME sensor answers
// NotImplemented for the Bayer offsets, never a value.
//
// Never a value is the point. A mono camera reporting offset 0 is indistinguishable from a colour
// camera whose mosaic starts at the origin, so a client would debayer a monochrome frame into a
// quarter-resolution colour image of nothing.
func GateCameraBayerOffset(c Camera, member string) error {
	if c.SensorType() == SensorMonochrome {
		return notImplErr(member)
	}
	return nil
}

// GateCameraGain answers NotImplemented for the whole gain family — Gain, GainMin, GainMax, Gains
// and the setter — when CanGain is false. See camera.go's CanGain for why the flag exists at all:
// an int-typed getter cannot throw, so the gate has to be outside the driver.
func GateCameraGain(c Camera, member string) error {
	if !c.CanGain() {
		return notImplErr(member)
	}
	return nil
}

// GateCameraOffset is GateCameraGain for the offset family.
func GateCameraOffset(c Camera, member string) error {
	if !c.CanOffset() {
		return notImplErr(member)
	}
	return nil
}

// GateCameraCoolerPower answers NotImplemented when the camera cannot report cooler power.
func GateCameraCoolerPower(c Camera) error {
	if !c.CanGetCoolerPower() {
		return notImplErr("CoolerPower")
	}
	return nil
}

// GateCameraFastReadout answers NotImplemented for the fast-readout members when CanFastReadout is
// false.
func GateCameraFastReadout(c Camera, member string) error {
	if !c.CanFastReadout() {
		return notImplErr(member)
	}
	return nil
}

// GateCameraSetGain gates a gain WRITE: the capability, then the value.
//
// The value check has two modes and the driver decides which by whether Gains() answers. In list
// mode the value is an INDEX into Gains; in value mode it is bounded by GainMin/GainMax. Applying
// the wrong one is how a legal gain gets rejected on a camera that publishes a list.
func GateCameraSetGain(c Camera, n int) error {
	if err := GateCameraGain(c, "Gain"); err != nil {
		return err
	}
	if gains, err := c.Gains(); err == nil {
		if n < 0 || n >= len(gains) {
			return invalidValuef("Gain index %d is outside the Gains list (0 to %d)", n, len(gains)-1)
		}
		return nil
	}
	if min, max := c.GainMin(), c.GainMax(); n < min || n > max {
		return invalidValuef("Gain %d is outside the valid range %d to %d", n, min, max)
	}
	return nil
}

// GateCameraSetOffset is GateCameraSetGain for offset, with the same two modes.
func GateCameraSetOffset(c Camera, n int) error {
	if err := GateCameraOffset(c, "Offset"); err != nil {
		return err
	}
	if offsets, err := c.Offsets(); err == nil {
		if n < 0 || n >= len(offsets) {
			return invalidValuef("Offset index %d is outside the Offsets list (0 to %d)", n, len(offsets)-1)
		}
		return nil
	}
	if min, max := c.OffsetMin(), c.OffsetMax(); n < min || n > max {
		return invalidValuef("Offset %d is outside the valid range %d to %d", n, min, max)
	}
	return nil
}

// GateCameraSetReadoutMode bounds a readout mode against the ReadoutModes list.
func GateCameraSetReadoutMode(c Camera, n int) error {
	if modes := c.ReadoutModes(); n < 0 || n >= len(modes) {
		return invalidValuef("ReadoutMode %d is outside the ReadoutModes list (0 to %d)", n, len(modes)-1)
	}
	return nil
}

// GateCameraSetFastReadout gates a fast-readout WRITE on the capability flag.
func GateCameraSetFastReadout(c Camera) error { return GateCameraFastReadout(c, "FastReadout") }

// GateCameraSetBinX bounds a binning factor by MaxBinX. Binning is 1-based: 0 is not "no binning",
// it is invalid.
func GateCameraSetBinX(c Camera, n int) error {
	if max := c.MaxBinX(); n < 1 || n > max {
		return invalidValuef("BinX %d is outside the valid range 1 to %d", n, max)
	}
	return nil
}

// GateCameraSetBinY bounds a binning factor by MaxBinY.
func GateCameraSetBinY(c Camera, n int) error {
	if max := c.MaxBinY(); n < 1 || n > max {
		return invalidValuef("BinY %d is outside the valid range 1 to %d", n, max)
	}
	return nil
}

// GateCameraSubframeOrigin rejects a negative StartX/StartY. The UPPER bound is not checked here:
// ASCOM validates the whole rectangle against the binned sensor at StartExposure, because a client
// setting a subframe writes the four properties one at a time and any intermediate combination may
// legitimately be out of range.
func GateCameraSubframeOrigin(member string, n int) error {
	if n < 0 {
		return invalidValuef("%s %d is negative", member, n)
	}
	return nil
}

// GateCameraSubframeSize rejects a negative NumX/NumY, with the same staged-validation reasoning as
// GateCameraSubframeOrigin.
func GateCameraSubframeSize(member string, n int) error {
	if n < 0 {
		return invalidValuef("%s %d is negative", member, n)
	}
	return nil
}

// GateCameraSetCCDTemperature gates a cooler setpoint WRITE: the capability, then the physically
// plausible range ConformU requires (above absolute zero, below 100 °C).
//
// A driver may impose a tighter range of its own — this is the outer bound every camera shares, not
// a claim about what any given camera can reach.
func GateCameraSetCCDTemperature(c Camera, celsius float64) error {
	if !c.CanSetCCDTemperature() {
		return notImplErr("SetCCDTemperature")
	}
	// >= 100, not > 100: ConformU flags both -273.15 °C and 100 °C as implausible limits, so the
	// upper bound is EXCLUSIVE. Transcribed exactly rather than approximately — a gate that is one
	// value more permissive than the dispatch it replaces is the drift this extraction exists to
	// prevent, and it was nearly introduced here.
	if celsius < -273.15 || celsius >= 100 {
		return invalidValuef("SetCCDTemperature %g is outside the physically plausible range -273.15 to 100", celsius)
	}
	return nil
}

// GateCameraSubframe validates the WHOLE readout rectangle against the binned sensor, which is the
// ICameraV4 check performed at StartExposure rather than at each property write.
//
// It is deliberately not the same as the per-property gates above. A client sets a subframe one
// property at a time, so an intermediate combination is routinely out of range and must not be
// refused; the rectangle only has to be coherent at the moment an exposure starts. Applying this
// check per-write would make a legal sequence of writes fail depending on their order.
func GateCameraSubframe(c Camera) error { return validSubframe(c) }

// GateCameraStartExposure rejects a negative duration. Zero is valid: it is how a bias frame is
// taken on a camera whose ExposureMin is zero.
func GateCameraStartExposure(duration float64) error {
	return nonNegative("Duration", duration)
}

// GateCameraStopExposure answers NotImplemented when the camera cannot stop an exposure and keep
// the frame.
func GateCameraStopExposure(c Camera) error {
	if !c.CanStopExposure() {
		return notImplErr("StopExposure")
	}
	return nil
}

// GateCameraAbortExposure answers NotImplemented when the camera cannot abort an exposure.
func GateCameraAbortExposure(c Camera) error {
	if !c.CanAbortExposure() {
		return notImplErr("AbortExposure")
	}
	return nil
}

// GateCameraSubExposureDuration rejects a negative sub-exposure duration (ICameraV3+).
func GateCameraSubExposureDuration(seconds float64) error {
	return nonNegative("SubExposureDuration", seconds)
}

// GateCameraPulseGuide gates ST4 guiding through the CAMERA: the capability, the direction, and a
// non-negative duration.
func GateCameraPulseGuide(c Camera, dir GuideDirection, duration int) error {
	if !c.CanPulseGuide() {
		return notImplErr("PulseGuide")
	}
	if !validGuideDirection(dir) {
		return invalidValuef("Direction %d is not a valid guide direction", int(dir))
	}
	if duration < 0 {
		return invalidValuef("Duration %d is negative", duration)
	}
	return nil
}

// validGuideDirection reports whether dir is one of the four ASCOM cardinal directions.
func validGuideDirection(dir GuideDirection) bool {
	return dir >= GuideNorth && dir <= GuideWest
}

// ---- Telescope --------------------------------------------------------------
//
// The mount carries most of the gates, and it is the one device where a missing gate MOVES
// SOMETHING. Parked gating in particular is not a classification nicety: a mount is parked because
// something is in the way, and a slew accepted while parked drives the tube into it.

// GateTelescopeDeclinationRate gates a Dec tracking-rate offset WRITE.
//
// The tracking-rate condition is ITelescopeV4's: rate offsets are defined as offsets FROM SIDEREAL,
// so setting one while tracking Lunar or Solar has no defined meaning and the spec says to refuse
// it rather than guess.
func GateTelescopeDeclinationRate(t Telescope) error {
	if !t.CanSetDeclinationRate() {
		return notImplErr("DeclinationRate")
	}
	if t.TrackingRate() != DriveSidereal {
		return invalidOpErr("DeclinationRate can only be set when tracking at the Sidereal rate")
	}
	return nil
}

// GateTelescopeRightAscensionRate gates an RA tracking-rate offset WRITE. Same reasoning as
// GateTelescopeDeclinationRate.
func GateTelescopeRightAscensionRate(t Telescope) error {
	if !t.CanSetRightAscensionRate() {
		return notImplErr("RightAscensionRate")
	}
	if t.TrackingRate() != DriveSidereal {
		return invalidOpErr("RightAscensionRate can only be set when tracking at the Sidereal rate")
	}
	return nil
}

// GateTelescopeGuideRate gates a guide-rate WRITE (either axis): the capability, then the sign.
// A guide rate is a magnitude — direction comes from the pulse, not the rate.
func GateTelescopeGuideRate(t Telescope, member string, rate float64) error {
	if !t.CanSetGuideRates() {
		return notImplErr(member)
	}
	return nonNegative(member, rate)
}

// GateTelescopeSideOfPier gates a pier-side WRITE: the capability, then the value. Unknown (-1) is
// a legitimate READING and never a legitimate request.
func GateTelescopeSideOfPier(t Telescope, side PierSide) error {
	if !t.CanSetPierSide() {
		return notImplErr("SideOfPier")
	}
	if side != PierEast && side != PierWest {
		return invalidValuef("SideOfPier %d is not a valid pier side", int(side))
	}
	return nil
}

// The site ranges, fixed by the spec rather than by any mount.
func GateTelescopeSiteElevation(m float64) error {
	return invalidRange("SiteElevation", m, -300, 10000)
}
func GateTelescopeSiteLatitude(d float64) error { return invalidRange("SiteLatitude", d, -90, 90) }

// GateTelescopeSiteLongitude bounds longitude to ±180 EAST-positive, which is ASCOM's convention.
// INDI uses 0..360 and a transport bridging the two converts before this sees it.
func GateTelescopeSiteLongitude(d float64) error { return invalidRange("SiteLongitude", d, -180, 180) }

// GateTelescopeSlewSettleTime rejects a negative settle time.
func GateTelescopeSlewSettleTime(seconds int) error {
	if seconds < 0 {
		return invalidValuef("SlewSettleTime %d is negative", seconds)
	}
	return nil
}

// GateTelescopeTargetDeclination and GateTelescopeTargetRightAscension bound the target
// coordinates. RA is in HOURS (0..24), Dec in degrees — the units are ASCOM's and mixing them is a
// silent pointing error rather than a refusal, which is why the bound is worth having.
func GateTelescopeTargetDeclination(deg float64) error {
	return invalidRange("TargetDeclination", deg, -90, 90)
}

func GateTelescopeTargetRightAscension(hours float64) error {
	if hours < 0 || hours >= 24 {
		return invalidValuef("TargetRightAscension %g is outside the valid range 0 to 23.999", hours)
	}
	return nil
}

// GateTelescopeTracking gates a tracking WRITE. Turning tracking ON while parked is refused;
// turning it OFF is always valid.
func GateTelescopeTracking(t Telescope, on bool) error {
	if !t.CanSetTracking() {
		return notImplErr("Tracking")
	}
	if on && t.AtPark() {
		return parkedErr("Tracking = true")
	}
	return nil
}

// GateTelescopeTrackingRate requires the rate to be one the mount advertises in TrackingRates.
func GateTelescopeTrackingRate(t Telescope, dr DriveRate) error {
	if !validDriveRate(t, dr) {
		return invalidValuef("TrackingRate %d is not a supported drive rate", int(dr))
	}
	return nil
}

// GateTelescopeUTCDate validates an ISO-8601 date-time string.
func GateTelescopeUTCDate(s string) error { return parseUTCDate(s) }

// GateTelescopeAbortSlew refuses an abort while parked — there is nothing to abort, and the spec
// treats the call as invalid rather than as a no-op.
func GateTelescopeAbortSlew(t Telescope) error {
	if t.AtPark() {
		return parkedErr("AbortSlew")
	}
	return nil
}

// GateTelescopeFindHome gates a homing run: the capability, then parked.
func GateTelescopeFindHome(t Telescope) error {
	if !t.CanFindHome() {
		return notImplErr("FindHome")
	}
	if t.AtPark() {
		return parkedErr("FindHome")
	}
	return nil
}

// GateTelescopeMoveAxis gates a mechanical axis move: the axis exists, the mount can move it, the
// rate is one it advertises, and it is not parked.
//
// The ORDER is the spec's and is worth preserving — an invalid axis is reported as an invalid axis
// even on a parked mount, rather than as a parked error that sends the reader somewhere else.
func GateTelescopeMoveAxis(t Telescope, axis TelescopeAxis, rate float64) error {
	if axis < AxisPrimary || axis > AxisTertiary {
		return invalidValuef("Axis %d is not a valid telescope axis", int(axis))
	}
	if !t.CanMoveAxis(axis) {
		return notImplErr("MoveAxis")
	}
	if err := GateAxisRate(t, axis, rate); err != nil {
		return err
	}
	if t.AtPark() {
		return parkedErr("MoveAxis")
	}
	return nil
}

// GateTelescopePark, GateTelescopeUnpark and GateTelescopeSetPark are plain capability gates.
func GateTelescopePark(t Telescope) error {
	if !t.CanPark() {
		return notImplErr("Park")
	}
	return nil
}

func GateTelescopeUnpark(t Telescope) error {
	if !t.CanUnpark() {
		return notImplErr("Unpark")
	}
	return nil
}

func GateTelescopeSetPark(t Telescope) error {
	if !t.CanSetPark() {
		return notImplErr("SetPark")
	}
	return nil
}

// GateTelescopePulseGuide gates mount pulse guiding: the capability, the direction, a non-negative
// duration, and not parked.
func GateTelescopePulseGuide(t Telescope, dir GuideDirection, duration int) error {
	if !t.CanPulseGuide() {
		return notImplErr("PulseGuide")
	}
	if !validGuideDirection(dir) {
		return invalidValuef("Direction %d is not a valid guide direction", int(dir))
	}
	if duration < 0 {
		return invalidValuef("Duration %d is negative", duration)
	}
	if t.AtPark() {
		return parkedErr("PulseGuide")
	}
	return nil
}

// GateTelescopeCoordinates bounds an equatorial pair: RA in HOURS (0..23.999), Dec in degrees.
//
// The units are ASCOM's and mixing them is a silent pointing error rather than a refusal, which is
// most of why the bound is worth having at all.
func GateTelescopeCoordinates(ra, dec float64) error { return validRADec(ra, dec) }

// GateTelescopeAltAz bounds a horizontal pair: azimuth 0..360, altitude ±90.
func GateTelescopeAltAz(az, alt float64) error { return validAltAz(az, alt) }

// GateTelescopeSlewToCoordinates gates a coordinate slew or sync — capability, coordinates, parked.
// can is the mount's flag for the specific variant (CanSlew, CanSlewAsync, CanSync), which differ
// per member and which the caller therefore supplies.
func GateTelescopeSlewToCoordinates(t Telescope, member string, can bool, ra, dec float64) error {
	if !can {
		return notImplErr(member)
	}
	if err := GateTelescopeCoordinates(ra, dec); err != nil {
		return err
	}
	if t.AtPark() {
		return parkedErr(member)
	}
	return nil
}

// GateTelescopeSlewToAltAz gates an alt-az slew or sync, with the same shape.
func GateTelescopeSlewToAltAz(t Telescope, member string, can bool, az, alt float64) error {
	if !can {
		return notImplErr(member)
	}
	if err := GateTelescopeAltAz(az, alt); err != nil {
		return err
	}
	if t.AtPark() {
		return parkedErr(member)
	}
	return nil
}

// GateTelescopeSlewToTarget gates a slew or sync to the TARGET properties: capability, parked, and
// the ASCOM read-before-set rule that both target properties must have been set first.
func GateTelescopeSlewToTarget(t Telescope, member string, can bool) error {
	if !can {
		return notImplErr(member)
	}
	if t.AtPark() {
		return parkedErr(member)
	}
	return requireTargetsSet(t)
}
