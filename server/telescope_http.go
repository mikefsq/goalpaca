package server

func telescopeGet(member string, t Telescope, p params) (any, bool, error) {
	switch member {
	case "alignmentmode":
		return int(t.AlignmentMode()), true, nil
	case "altitude":
		return t.Altitude(), true, nil
	case "aperturearea":
		return t.ApertureArea(), true, nil
	case "aperturediameter":
		return t.ApertureDiameter(), true, nil
	case "athome":
		return t.AtHome(), true, nil
	case "atpark":
		return t.AtPark(), true, nil
	case "azimuth":
		return t.Azimuth(), true, nil
	case "canfindhome":
		return t.CanFindHome(), true, nil
	case "canpark":
		return t.CanPark(), true, nil
	case "canpulseguide":
		return t.CanPulseGuide(), true, nil
	case "cansetdeclinationrate":
		return t.CanSetDeclinationRate(), true, nil
	case "cansetguiderates":
		return t.CanSetGuideRates(), true, nil
	case "cansetpark":
		return t.CanSetPark(), true, nil
	case "cansetpierside":
		return t.CanSetPierSide(), true, nil
	case "cansetrightascensionrate":
		return t.CanSetRightAscensionRate(), true, nil
	case "cansettracking":
		return t.CanSetTracking(), true, nil
	case "canslew":
		return t.CanSlew(), true, nil
	case "canslewaltaz":
		return t.CanSlewAltAz(), true, nil
	case "canslewaltazasync":
		return t.CanSlewAltAzAsync(), true, nil
	case "canslewasync":
		return t.CanSlewAsync(), true, nil
	case "cansync":
		return t.CanSync(), true, nil
	case "cansyncaltaz":
		return t.CanSyncAltAz(), true, nil
	case "canunpark":
		return t.CanUnpark(), true, nil
	case "declination":
		return t.Declination(), true, nil
	case "declinationrate":
		// ITelescopeV4: rate offsets are only valid when tracking at Sidereal;
		// under any other drive rate the property must read 0.0.
		if t.TrackingRate() != DriveSidereal {
			return 0.0, true, nil
		}
		return t.DeclinationRate(), true, nil
	case "doesrefraction":
		return t.DoesRefraction(), true, nil
	case "equatorialsystem":
		return int(t.EquatorialSystem()), true, nil
	case "focallength":
		return t.FocalLength(), true, nil
	case "guideratedeclination":
		return t.GuideRateDeclination(), true, nil
	case "guideraterightascension":
		return t.GuideRateRightAscension(), true, nil
	case "ispulseguiding":
		return t.IsPulseGuiding(), true, nil
	case "rightascension":
		return t.RightAscension(), true, nil
	case "rightascensionrate":
		if t.TrackingRate() != DriveSidereal { // see declinationrate
			return 0.0, true, nil
		}
		return t.RightAscensionRate(), true, nil
	case "sideofpier":
		return int(t.SideOfPier()), true, nil
	case "siderealtime":
		return t.SiderealTime(), true, nil
	case "siteelevation":
		return t.SiteElevation(), true, nil
	case "sitelatitude":
		return t.SiteLatitude(), true, nil
	case "sitelongitude":
		return t.SiteLongitude(), true, nil
	case "slewing":
		return t.Slewing(), true, nil
	case "slewsettletime":
		return t.SlewSettleTime(), true, nil
	case "targetdeclination":
		v, err := t.TargetDeclination()
		return v, true, err
	case "targetrightascension":
		v, err := t.TargetRightAscension()
		return v, true, err
	case "tracking":
		return t.Tracking(), true, nil
	case "trackingrate":
		return int(t.TrackingRate()), true, nil
	case "trackingrates":
		rates := t.TrackingRates()
		out := make([]int, len(rates))
		for i, r := range rates {
			out[i] = int(r)
		}
		return out, true, nil
	case "utcdate":
		return t.UTCDate(), true, nil

	// Parameterized getters.
	case "axisrates":
		axis, err := p.reqInt("Axis")
		if err != nil {
			return nil, true, err
		}
		return t.AxisRates(TelescopeAxis(axis)), true, nil
	case "canmoveaxis":
		axis, err := p.reqInt("Axis")
		if err != nil {
			return nil, true, err
		}
		return t.CanMoveAxis(TelescopeAxis(axis)), true, nil
	case "destinationsideofpier":
		ra, dec, err := raDecParams(p)
		if err != nil {
			return nil, true, err
		}
		if err := validRADec(ra, dec); err != nil {
			return nil, true, err
		}
		v, err := t.DestinationSideOfPier(ra, dec)
		return int(v), true, err
	}
	return nil, false, nil
}

// telescopePut dispatches Telescope PUT members. Each member applies the
// spec-fixed gates in order — Can-flag → NotImplemented, parameter ranges →
// InvalidValue, AtPark → Parked, target read-before-set → InvalidOperation —
// before the driver is called, so drivers only implement mount behavior.
func telescopePut(member string, t Telescope, p params) (bool, error) {
	switch member {
	// Setters
	case "declinationrate":
		f, err := p.reqFloat("DeclinationRate")
		if err != nil {
			return true, err
		}
		if !t.CanSetDeclinationRate() {
			return true, notImplErr("DeclinationRate")
		}
		// ITelescopeV4: rate offsets can only be set when tracking at Sidereal.
		if t.TrackingRate() != DriveSidereal {
			return true, invalidOpErr("DeclinationRate can only be set when tracking at the Sidereal rate")
		}
		return true, t.SetDeclinationRate(f)
	case "doesrefraction":
		b, err := p.reqBool("DoesRefraction")
		if err != nil {
			return true, err
		}
		return true, t.SetDoesRefraction(b)
	case "guideratedeclination":
		f, err := p.reqFloat("GuideRateDeclination")
		if err != nil {
			return true, err
		}
		if !t.CanSetGuideRates() {
			return true, notImplErr("GuideRateDeclination")
		}
		if f < 0 {
			return true, invalidValuef("GuideRateDeclination %g is negative", f)
		}
		return true, t.SetGuideRateDeclination(f)
	case "guideraterightascension":
		f, err := p.reqFloat("GuideRateRightAscension")
		if err != nil {
			return true, err
		}
		if !t.CanSetGuideRates() {
			return true, notImplErr("GuideRateRightAscension")
		}
		if f < 0 {
			return true, invalidValuef("GuideRateRightAscension %g is negative", f)
		}
		return true, t.SetGuideRateRightAscension(f)
	case "rightascensionrate":
		f, err := p.reqFloat("RightAscensionRate")
		if err != nil {
			return true, err
		}
		if !t.CanSetRightAscensionRate() {
			return true, notImplErr("RightAscensionRate")
		}
		if t.TrackingRate() != DriveSidereal { // see declinationrate
			return true, invalidOpErr("RightAscensionRate can only be set when tracking at the Sidereal rate")
		}
		return true, t.SetRightAscensionRate(f)
	case "sideofpier":
		n, err := p.reqInt("SideOfPier")
		if err != nil {
			return true, err
		}
		if !t.CanSetPierSide() {
			return true, notImplErr("SideOfPier")
		}
		if n != int(PierEast) && n != int(PierWest) {
			return true, invalidValuef("SideOfPier %d is not a valid pier side", n)
		}
		return true, t.SetSideOfPier(PierSide(n))
	case "siteelevation":
		f, err := p.reqFloat("SiteElevation")
		if err != nil {
			return true, err
		}
		if err := invalidRange("SiteElevation", f, -300, 10000); err != nil {
			return true, err
		}
		return true, t.SetSiteElevation(f)
	case "sitelatitude":
		f, err := p.reqFloat("SiteLatitude")
		if err != nil {
			return true, err
		}
		if err := invalidRange("SiteLatitude", f, -90, 90); err != nil {
			return true, err
		}
		return true, t.SetSiteLatitude(f)
	case "sitelongitude":
		f, err := p.reqFloat("SiteLongitude")
		if err != nil {
			return true, err
		}
		if err := invalidRange("SiteLongitude", f, -180, 180); err != nil {
			return true, err
		}
		return true, t.SetSiteLongitude(f)
	case "slewsettletime":
		n, err := p.reqInt("SlewSettleTime")
		if err != nil {
			return true, err
		}
		if n < 0 {
			return true, invalidValuef("SlewSettleTime %d is negative", n)
		}
		return true, t.SetSlewSettleTime(n)
	case "targetdeclination":
		f, err := p.reqFloat("TargetDeclination")
		if err != nil {
			return true, err
		}
		if err := invalidRange("TargetDeclination", f, -90, 90); err != nil {
			return true, err
		}
		return true, t.SetTargetDeclination(f)
	case "targetrightascension":
		f, err := p.reqFloat("TargetRightAscension")
		if err != nil {
			return true, err
		}
		if f < 0 || f >= 24 {
			return true, invalidValuef("TargetRightAscension %g is outside the valid range 0 to 23.999", f)
		}
		return true, t.SetTargetRightAscension(f)
	case "tracking":
		b, err := p.reqBool("Tracking")
		if err != nil {
			return true, err
		}
		if !t.CanSetTracking() {
			return true, notImplErr("Tracking")
		}
		if b && t.AtPark() {
			return true, parkedErr("Tracking = true")
		}
		return true, t.SetTracking(b)
	case "trackingrate":
		n, err := p.reqInt("TrackingRate")
		if err != nil {
			return true, err
		}
		if !validDriveRate(t, DriveRate(n)) {
			return true, invalidValuef("TrackingRate %d is not a supported drive rate", n)
		}
		return true, t.SetTrackingRate(DriveRate(n))
	case "utcdate":
		v, err := p.reqString("UTCDate")
		if err != nil {
			return true, err
		}
		if err := parseUTCDate(v); err != nil {
			return true, err
		}
		return true, t.SetUTCDate(v)

	// Methods
	case "abortslew":
		if t.AtPark() {
			return true, parkedErr("AbortSlew")
		}
		return true, t.AbortSlew()
	case "findhome":
		if !t.CanFindHome() {
			return true, notImplErr("FindHome")
		}
		if t.AtPark() {
			return true, parkedErr("FindHome")
		}
		return true, t.FindHome()
	case "moveaxis":
		axis, err := p.reqInt("Axis")
		if err != nil {
			return true, err
		}
		rate, err := p.reqFloat("Rate")
		if err != nil {
			return true, err
		}
		if axis < int(AxisPrimary) || axis > int(AxisTertiary) {
			return true, invalidValuef("Axis %d is not a valid telescope axis", axis)
		}
		if !t.CanMoveAxis(TelescopeAxis(axis)) {
			return true, notImplErr("MoveAxis")
		}
		if !validAxisRate(t, TelescopeAxis(axis), rate) {
			return true, invalidValuef("Rate %g is outside the axis' supported ranges", rate)
		}
		if t.AtPark() {
			return true, parkedErr("MoveAxis")
		}
		return true, t.MoveAxis(TelescopeAxis(axis), rate)
	case "park":
		if !t.CanPark() {
			return true, notImplErr("Park")
		}
		return true, t.Park()
	case "pulseguide":
		dir, err := p.reqInt("Direction")
		if err != nil {
			return true, err
		}
		dur, err := p.reqInt("Duration")
		if err != nil {
			return true, err
		}
		if !t.CanPulseGuide() {
			return true, notImplErr("PulseGuide")
		}
		if dir < int(GuideNorth) || dir > int(GuideWest) {
			return true, invalidValuef("Direction %d is not a valid guide direction", dir)
		}
		if dur < 0 {
			return true, invalidValuef("Duration %d is negative", dur)
		}
		if t.AtPark() {
			return true, parkedErr("PulseGuide")
		}
		return true, t.PulseGuide(GuideDirection(dir), dur)
	case "setpark":
		if !t.CanSetPark() {
			return true, notImplErr("SetPark")
		}
		return true, t.SetPark()
	case "slewtoaltaz":
		az, alt, err := altAzParams(p)
		if err != nil {
			return true, err
		}
		if !t.CanSlewAltAz() {
			return true, notImplErr("SlewToAltAz")
		}
		if err := validAltAz(az, alt); err != nil {
			return true, err
		}
		if t.AtPark() {
			return true, parkedErr("SlewToAltAz")
		}
		return true, t.SlewToAltAz(az, alt)
	case "slewtoaltazasync":
		az, alt, err := altAzParams(p)
		if err != nil {
			return true, err
		}
		if !t.CanSlewAltAzAsync() {
			return true, notImplErr("SlewToAltAzAsync")
		}
		if err := validAltAz(az, alt); err != nil {
			return true, err
		}
		if t.AtPark() {
			return true, parkedErr("SlewToAltAzAsync")
		}
		return true, t.SlewToAltAzAsync(az, alt)
	case "slewtocoordinates":
		ra, dec, err := raDecParams(p)
		if err != nil {
			return true, err
		}
		if !t.CanSlew() {
			return true, notImplErr("SlewToCoordinates")
		}
		if err := validRADec(ra, dec); err != nil {
			return true, err
		}
		if t.AtPark() {
			return true, parkedErr("SlewToCoordinates")
		}
		if err := t.SlewToCoordinates(ra, dec); err != nil {
			return true, err
		}
		setTargets(t, ra, dec)
		return true, nil
	case "slewtocoordinatesasync":
		ra, dec, err := raDecParams(p)
		if err != nil {
			return true, err
		}
		if !t.CanSlewAsync() {
			return true, notImplErr("SlewToCoordinatesAsync")
		}
		if err := validRADec(ra, dec); err != nil {
			return true, err
		}
		if t.AtPark() {
			return true, parkedErr("SlewToCoordinatesAsync")
		}
		if err := t.SlewToCoordinatesAsync(ra, dec); err != nil {
			return true, err
		}
		setTargets(t, ra, dec)
		return true, nil
	case "slewtotarget":
		if !t.CanSlew() {
			return true, notImplErr("SlewToTarget")
		}
		if t.AtPark() {
			return true, parkedErr("SlewToTarget")
		}
		if err := requireTargetsSet(t); err != nil {
			return true, err
		}
		return true, t.SlewToTarget()
	case "slewtotargetasync":
		if !t.CanSlewAsync() {
			return true, notImplErr("SlewToTargetAsync")
		}
		if t.AtPark() {
			return true, parkedErr("SlewToTargetAsync")
		}
		if err := requireTargetsSet(t); err != nil {
			return true, err
		}
		return true, t.SlewToTargetAsync()
	case "synctoaltaz":
		az, alt, err := altAzParams(p)
		if err != nil {
			return true, err
		}
		if !t.CanSyncAltAz() {
			return true, notImplErr("SyncToAltAz")
		}
		if err := validAltAz(az, alt); err != nil {
			return true, err
		}
		if t.AtPark() {
			return true, parkedErr("SyncToAltAz")
		}
		return true, t.SyncToAltAz(az, alt)
	case "synctocoordinates":
		ra, dec, err := raDecParams(p)
		if err != nil {
			return true, err
		}
		if !t.CanSync() {
			return true, notImplErr("SyncToCoordinates")
		}
		if err := validRADec(ra, dec); err != nil {
			return true, err
		}
		if t.AtPark() {
			return true, parkedErr("SyncToCoordinates")
		}
		if err := t.SyncToCoordinates(ra, dec); err != nil {
			return true, err
		}
		setTargets(t, ra, dec)
		return true, nil
	case "synctotarget":
		if !t.CanSync() {
			return true, notImplErr("SyncToTarget")
		}
		if t.AtPark() {
			return true, parkedErr("SyncToTarget")
		}
		if err := requireTargetsSet(t); err != nil {
			return true, err
		}
		return true, t.SyncToTarget()
	case "unpark":
		if !t.CanUnpark() {
			return true, notImplErr("Unpark")
		}
		return true, t.Unpark()
	}
	return false, nil
}

// validRADec rejects out-of-range equatorial coordinates: right ascension is
// 0 to 23.999… hours, declination -90 to +90 degrees.
func validRADec(ra, dec float64) error {
	if ra < 0 || ra >= 24 {
		return invalidValuef("RightAscension %g is outside the valid range 0 to 23.999", ra)
	}
	return invalidRange("Declination", dec, -90, 90)
}

// validAltAz rejects out-of-range horizontal coordinates: azimuth 0 to 360
// degrees, altitude -90 to +90 degrees (-90 allows a tube parked pointing
// straight down).
func validAltAz(az, alt float64) error {
	if err := invalidRange("Azimuth", az, 0, 360); err != nil {
		return err
	}
	return invalidRange("Altitude", alt, -90, 90)
}

func altAzParams(p params) (az, alt float64, err error) {
	if az, err = p.reqFloat("Azimuth"); err != nil {
		return
	}
	alt, err = p.reqFloat("Altitude")
	return
}

func raDecParams(p params) (ra, dec float64, err error) {
	if ra, err = p.reqFloat("RightAscension"); err != nil {
		return
	}
	dec, err = p.reqFloat("Declination")
	return
}
