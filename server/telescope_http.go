package alpacadev

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
		return t.TargetDeclination(), true, nil
	case "targetrightascension":
		return t.TargetRightAscension(), true, nil
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
		ra, err := p.reqFloat("RightAscension")
		if err != nil {
			return nil, true, err
		}
		dec, err := p.reqFloat("Declination")
		if err != nil {
			return nil, true, err
		}
		v, err := t.DestinationSideOfPier(ra, dec)
		return int(v), true, err
	}
	return nil, false, nil
}

func telescopePut(member string, t Telescope, p params) (bool, error) {
	switch member {
	// Setters
	case "declinationrate":
		f, err := p.reqFloat("DeclinationRate")
		if err != nil {
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
		return true, t.SetGuideRateDeclination(f)
	case "guideraterightascension":
		f, err := p.reqFloat("GuideRateRightAscension")
		if err != nil {
			return true, err
		}
		return true, t.SetGuideRateRightAscension(f)
	case "rightascensionrate":
		f, err := p.reqFloat("RightAscensionRate")
		if err != nil {
			return true, err
		}
		return true, t.SetRightAscensionRate(f)
	case "sideofpier":
		n, err := p.reqInt("SideOfPier")
		if err != nil {
			return true, err
		}
		return true, t.SetSideOfPier(PierSide(n))
	case "siteelevation":
		f, err := p.reqFloat("SiteElevation")
		if err != nil {
			return true, err
		}
		return true, t.SetSiteElevation(f)
	case "sitelatitude":
		f, err := p.reqFloat("SiteLatitude")
		if err != nil {
			return true, err
		}
		return true, t.SetSiteLatitude(f)
	case "sitelongitude":
		f, err := p.reqFloat("SiteLongitude")
		if err != nil {
			return true, err
		}
		return true, t.SetSiteLongitude(f)
	case "slewsettletime":
		n, err := p.reqInt("SlewSettleTime")
		if err != nil {
			return true, err
		}
		return true, t.SetSlewSettleTime(n)
	case "targetdeclination":
		f, err := p.reqFloat("TargetDeclination")
		if err != nil {
			return true, err
		}
		return true, t.SetTargetDeclination(f)
	case "targetrightascension":
		f, err := p.reqFloat("TargetRightAscension")
		if err != nil {
			return true, err
		}
		return true, t.SetTargetRightAscension(f)
	case "tracking":
		b, err := p.reqBool("Tracking")
		if err != nil {
			return true, err
		}
		return true, t.SetTracking(b)
	case "trackingrate":
		n, err := p.reqInt("TrackingRate")
		if err != nil {
			return true, err
		}
		return true, t.SetTrackingRate(DriveRate(n))
	case "utcdate":
		v, _ := p.get("UTCDate")
		return true, t.SetUTCDate(v)

	// Methods
	case "abortslew":
		return true, t.AbortSlew()
	case "findhome":
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
		return true, t.MoveAxis(TelescopeAxis(axis), rate)
	case "park":
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
		return true, t.PulseGuide(GuideDirection(dir), dur)
	case "setpark":
		return true, t.SetPark()
	case "slewtoaltaz":
		az, alt, err := altAzParams(p)
		if err != nil {
			return true, err
		}
		return true, t.SlewToAltAz(az, alt)
	case "slewtoaltazasync":
		az, alt, err := altAzParams(p)
		if err != nil {
			return true, err
		}
		return true, t.SlewToAltAzAsync(az, alt)
	case "slewtocoordinates":
		ra, dec, err := raDecParams(p)
		if err != nil {
			return true, err
		}
		return true, t.SlewToCoordinates(ra, dec)
	case "slewtocoordinatesasync":
		ra, dec, err := raDecParams(p)
		if err != nil {
			return true, err
		}
		return true, t.SlewToCoordinatesAsync(ra, dec)
	case "slewtotarget":
		return true, t.SlewToTarget()
	case "slewtotargetasync":
		return true, t.SlewToTargetAsync()
	case "synctoaltaz":
		az, alt, err := altAzParams(p)
		if err != nil {
			return true, err
		}
		return true, t.SyncToAltAz(az, alt)
	case "synctocoordinates":
		ra, dec, err := raDecParams(p)
		if err != nil {
			return true, err
		}
		return true, t.SyncToCoordinates(ra, dec)
	case "synctotarget":
		return true, t.SyncToTarget()
	case "unpark":
		return true, t.Unpark()
	}
	return false, nil
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
