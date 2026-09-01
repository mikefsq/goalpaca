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
		// ITelescopeV4: rate offsets can only be set when tracking at Sidereal.
		if err := GateTelescopeDeclinationRate(t); err != nil {
			return true, err
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
		if err := GateTelescopeGuideRate(t, "GuideRateDeclination", f); err != nil {
			return true, err
		}
		return true, t.SetGuideRateDeclination(f)
	case "guideraterightascension":
		f, err := p.reqFloat("GuideRateRightAscension")
		if err != nil {
			return true, err
		}
		if err := GateTelescopeGuideRate(t, "GuideRateRightAscension", f); err != nil {
			return true, err
		}
		return true, t.SetGuideRateRightAscension(f)
	case "rightascensionrate":
		f, err := p.reqFloat("RightAscensionRate")
		if err != nil {
			return true, err
		}
		if err := GateTelescopeRightAscensionRate(t); err != nil { // see declinationrate
			return true, err
		}
		return true, t.SetRightAscensionRate(f)
	case "sideofpier":
		n, err := p.reqInt("SideOfPier")
		if err != nil {
			return true, err
		}
		if err := GateTelescopeSideOfPier(t, PierSide(n)); err != nil {
			return true, err
		}
		return true, t.SetSideOfPier(PierSide(n))
	case "siteelevation":
		f, err := p.reqFloat("SiteElevation")
		if err != nil {
			return true, err
		}
		if err := GateTelescopeSiteElevation(f); err != nil {
			return true, err
		}
		return true, t.SetSiteElevation(f)
	case "sitelatitude":
		f, err := p.reqFloat("SiteLatitude")
		if err != nil {
			return true, err
		}
		if err := GateTelescopeSiteLatitude(f); err != nil {
			return true, err
		}
		return true, t.SetSiteLatitude(f)
	case "sitelongitude":
		f, err := p.reqFloat("SiteLongitude")
		if err != nil {
			return true, err
		}
		if err := GateTelescopeSiteLongitude(f); err != nil {
			return true, err
		}
		return true, t.SetSiteLongitude(f)
	case "slewsettletime":
		n, err := p.reqInt("SlewSettleTime")
		if err != nil {
			return true, err
		}
		if err := GateTelescopeSlewSettleTime(n); err != nil {
			return true, err
		}
		return true, t.SetSlewSettleTime(n)
	case "targetdeclination":
		f, err := p.reqFloat("TargetDeclination")
		if err != nil {
			return true, err
		}
		if err := GateTelescopeTargetDeclination(f); err != nil {
			return true, err
		}
		return true, t.SetTargetDeclination(f)
	case "targetrightascension":
		f, err := p.reqFloat("TargetRightAscension")
		if err != nil {
			return true, err
		}
		if err := GateTelescopeTargetRightAscension(f); err != nil {
			return true, err
		}
		return true, t.SetTargetRightAscension(f)
	case "tracking":
		b, err := p.reqBool("Tracking")
		if err != nil {
			return true, err
		}
		if err := GateTelescopeTracking(t, b); err != nil {
			return true, err
		}
		return true, t.SetTracking(b)
	case "trackingrate":
		n, err := p.reqInt("TrackingRate")
		if err != nil {
			return true, err
		}
		if err := GateTelescopeTrackingRate(t, DriveRate(n)); err != nil {
			return true, err
		}
		return true, t.SetTrackingRate(DriveRate(n))
	case "utcdate":
		v, err := p.reqString("UTCDate")
		if err != nil {
			return true, err
		}
		if err := GateTelescopeUTCDate(v); err != nil {
			return true, err
		}
		return true, t.SetUTCDate(v)

	// Methods
	case "abortslew":
		if err := GateTelescopeAbortSlew(t); err != nil {
			return true, err
		}
		return true, t.AbortSlew()
	case "findhome":
		if err := GateTelescopeFindHome(t); err != nil {
			return true, err
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
		if err := GateTelescopeMoveAxis(t, TelescopeAxis(axis), rate); err != nil {
			return true, err
		}
		return true, t.MoveAxis(TelescopeAxis(axis), rate)
	case "park":
		if err := GateTelescopePark(t); err != nil {
			return true, err
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
		if err := GateTelescopePulseGuide(t, GuideDirection(dir), dur); err != nil {
			return true, err
		}
		return true, t.PulseGuide(GuideDirection(dir), dur)
	case "setpark":
		if err := GateTelescopeSetPark(t); err != nil {
			return true, err
		}
		return true, t.SetPark()
	case "slewtoaltaz":
		az, alt, err := altAzParams(p)
		if err != nil {
			return true, err
		}
		if err := GateTelescopeSlewToAltAz(t, "SlewToAltAz", t.CanSlewAltAz(), az, alt); err != nil {
			return true, err
		}
		return true, t.SlewToAltAz(az, alt)
	case "slewtoaltazasync":
		az, alt, err := altAzParams(p)
		if err != nil {
			return true, err
		}
		if err := GateTelescopeSlewToAltAz(t, "SlewToAltAzAsync", t.CanSlewAltAzAsync(), az, alt); err != nil {
			return true, err
		}
		return true, t.SlewToAltAzAsync(az, alt)
	case "slewtocoordinates":
		ra, dec, err := raDecParams(p)
		if err != nil {
			return true, err
		}
		if err := GateTelescopeSlewToCoordinates(t, "SlewToCoordinates", t.CanSlew(), ra, dec); err != nil {
			return true, err
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
		if err := GateTelescopeSlewToCoordinates(t, "SlewToCoordinatesAsync", t.CanSlewAsync(), ra, dec); err != nil {
			return true, err
		}
		if err := t.SlewToCoordinatesAsync(ra, dec); err != nil {
			return true, err
		}
		setTargets(t, ra, dec)
		return true, nil
	case "slewtotarget":
		if err := GateTelescopeSlewToTarget(t, "SlewToTarget", t.CanSlew()); err != nil {
			return true, err
		}
		return true, t.SlewToTarget()
	case "slewtotargetasync":
		if err := GateTelescopeSlewToTarget(t, "SlewToTargetAsync", t.CanSlewAsync()); err != nil {
			return true, err
		}
		return true, t.SlewToTargetAsync()
	case "synctoaltaz":
		az, alt, err := altAzParams(p)
		if err != nil {
			return true, err
		}
		if err := GateTelescopeSlewToAltAz(t, "SyncToAltAz", t.CanSyncAltAz(), az, alt); err != nil {
			return true, err
		}
		return true, t.SyncToAltAz(az, alt)
	case "synctocoordinates":
		ra, dec, err := raDecParams(p)
		if err != nil {
			return true, err
		}
		if err := GateTelescopeSlewToCoordinates(t, "SyncToCoordinates", t.CanSync(), ra, dec); err != nil {
			return true, err
		}
		if err := t.SyncToCoordinates(ra, dec); err != nil {
			return true, err
		}
		setTargets(t, ra, dec)
		return true, nil
	case "synctotarget":
		if err := GateTelescopeSlewToTarget(t, "SyncToTarget", t.CanSync()); err != nil {
			return true, err
		}
		return true, t.SyncToTarget()
	case "unpark":
		if err := GateTelescopeUnpark(t); err != nil {
			return true, err
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
