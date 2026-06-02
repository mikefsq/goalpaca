package alpacadev

// cameraGet dispatches Camera GET members. Returns (value, handled, err).
// The "imagearray" member is handled by the router (binary ImageBytes path),
// not here. (params is unused for Camera but kept for dispatch uniformity.)
func cameraGet(member string, c Camera, _ params) (any, bool, error) {
	switch member {
	// Geometry / description
	case "cameraxsize":
		return c.CameraXSize(), true, nil
	case "cameraysize":
		return c.CameraYSize(), true, nil
	case "pixelsizex":
		return c.PixelSizeX(), true, nil
	case "pixelsizey":
		return c.PixelSizeY(), true, nil
	case "maxadu":
		return c.MaxADU(), true, nil
	case "electronsperadu":
		return c.ElectronsPerADU(), true, nil
	case "fullwellcapacity":
		return c.FullWellCapacity(), true, nil
	case "sensorname":
		return c.SensorName(), true, nil
	case "sensortype":
		return int(c.SensorType()), true, nil
	case "bayeroffsetx":
		v, err := c.BayerOffsetX()
		return v, true, err
	case "bayeroffsety":
		v, err := c.BayerOffsetY()
		return v, true, err

	// Binning
	case "binx":
		return c.BinX(), true, nil
	case "biny":
		return c.BinY(), true, nil
	case "maxbinx":
		return c.MaxBinX(), true, nil
	case "maxbiny":
		return c.MaxBinY(), true, nil
	case "canasymmetricbin":
		return c.CanAsymmetricBin(), true, nil

	// Subframe
	case "startx":
		return c.StartX(), true, nil
	case "starty":
		return c.StartY(), true, nil
	case "numx":
		return c.NumX(), true, nil
	case "numy":
		return c.NumY(), true, nil

	// Exposure
	case "camerastate":
		return int(c.CameraState()), true, nil
	case "imageready":
		return c.ImageReady(), true, nil
	case "percentcompleted":
		return c.PercentCompleted(), true, nil
	case "exposuremin":
		return c.ExposureMin(), true, nil
	case "exposuremax":
		return c.ExposureMax(), true, nil
	case "exposureresolution":
		return c.ExposureResolution(), true, nil
	case "hasshutter":
		return c.HasShutter(), true, nil
	case "canstopexposure":
		return c.CanStopExposure(), true, nil
	case "canabortexposure":
		return c.CanAbortExposure(), true, nil
	case "lastexposureduration":
		v, err := c.LastExposureDuration()
		return v, true, err
	case "lastexposurestarttime":
		v, err := c.LastExposureStartTime()
		return v, true, err
	case "subexposureduration":
		v, err := c.SubExposureDuration()
		return v, true, err

	// Gain / Offset
	case "gain":
		return c.Gain(), true, nil
	case "gainmin":
		return c.GainMin(), true, nil
	case "gainmax":
		return c.GainMax(), true, nil
	case "gains":
		v, err := c.Gains()
		return v, true, err
	case "offset":
		return c.Offset(), true, nil
	case "offsetmin":
		return c.OffsetMin(), true, nil
	case "offsetmax":
		return c.OffsetMax(), true, nil
	case "offsets":
		v, err := c.Offsets()
		return v, true, err

	// Readout modes
	case "readoutmode":
		return c.ReadoutMode(), true, nil
	case "readoutmodes":
		return c.ReadoutModes(), true, nil
	case "fastreadout":
		v, err := c.FastReadout()
		return v, true, err
	case "canfastreadout":
		return c.CanFastReadout(), true, nil

	// Cooling
	case "ccdtemperature":
		v, err := c.CCDTemperature()
		return v, true, err
	case "heatsinktemperature":
		v, err := c.HeatSinkTemperature()
		return v, true, err
	case "cooleron":
		return c.CoolerOn(), true, nil
	case "coolerpower":
		v, err := c.CoolerPower()
		return v, true, err
	case "cangetcoolerpower":
		return c.CanGetCoolerPower(), true, nil
	case "setccdtemperature":
		v, err := c.SetCCDTemperature()
		return v, true, err
	case "cansetccdtemperature":
		return c.CanSetCCDTemperature(), true, nil

	// Guiding
	case "canpulseguide":
		return c.CanPulseGuide(), true, nil
	case "ispulseguiding":
		return c.IsPulseGuiding(), true, nil
	}
	return nil, false, nil
}

// cameraPut dispatches Camera PUT members (setters / async initiators / methods).
func cameraPut(member string, c Camera, p params) (bool, error) {
	switch member {
	// Binning
	case "binx":
		n, err := p.reqInt("BinX")
		if err != nil {
			return true, err
		}
		return true, c.SetBinX(n)
	case "biny":
		n, err := p.reqInt("BinY")
		if err != nil {
			return true, err
		}
		return true, c.SetBinY(n)

	// Subframe
	case "startx":
		n, err := p.reqInt("StartX")
		if err != nil {
			return true, err
		}
		return true, c.SetStartX(n)
	case "starty":
		n, err := p.reqInt("StartY")
		if err != nil {
			return true, err
		}
		return true, c.SetStartY(n)
	case "numx":
		n, err := p.reqInt("NumX")
		if err != nil {
			return true, err
		}
		return true, c.SetNumX(n)
	case "numy":
		n, err := p.reqInt("NumY")
		if err != nil {
			return true, err
		}
		return true, c.SetNumY(n)

	// Gain / Offset / Readout
	case "gain":
		n, err := p.reqInt("Gain")
		if err != nil {
			return true, err
		}
		return true, c.SetGain(n)
	case "offset":
		n, err := p.reqInt("Offset")
		if err != nil {
			return true, err
		}
		return true, c.SetOffset(n)
	case "readoutmode":
		n, err := p.reqInt("ReadoutMode")
		if err != nil {
			return true, err
		}
		return true, c.SetReadoutMode(n)
	case "fastreadout":
		b, err := p.reqBool("FastReadout")
		if err != nil {
			return true, err
		}
		return true, c.SetFastReadout(b)

	// Cooling
	case "cooleron":
		b, err := p.reqBool("CoolerOn")
		if err != nil {
			return true, err
		}
		return true, c.SetCoolerOn(b)
	case "setccdtemperature":
		f, err := p.reqFloat("SetCCDTemperature")
		if err != nil {
			return true, err
		}
		return true, c.SetSetCCDTemperature(f)

	// Exposure
	case "startexposure":
		dur, err := p.reqFloat("Duration")
		if err != nil {
			return true, err
		}
		light, err := p.reqBool("Light")
		if err != nil {
			return true, err
		}
		return true, c.StartExposure(dur, light)
	case "stopexposure":
		return true, c.StopExposure()
	case "abortexposure":
		return true, c.AbortExposure()
	case "subexposureduration":
		f, err := p.reqFloat("SubExposureDuration")
		if err != nil {
			return true, err
		}
		return true, c.SetSubExposureDuration(f)

	// Guiding
	case "pulseguide":
		dir, err := p.reqInt("Direction")
		if err != nil {
			return true, err
		}
		dur, err := p.reqInt("Duration")
		if err != nil {
			return true, err
		}
		return true, c.PulseGuide(GuideDirection(dir), dur)
	}
	return false, nil
}
