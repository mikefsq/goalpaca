package server

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
		// ICameraV4: monochrome sensors must report NotImplemented, never a value.
		if c.SensorType() == SensorMonochrome {
			return nil, true, notImplErr("BayerOffsetX")
		}
		v, err := c.BayerOffsetX()
		return v, true, err
	case "bayeroffsety":
		if c.SensorType() == SensorMonochrome {
			return nil, true, notImplErr("BayerOffsetY")
		}
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
		if !c.CanFastReadout() {
			return nil, true, notImplErr("FastReadout")
		}
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
		if !c.CanGetCoolerPower() {
			return nil, true, notImplErr("CoolerPower")
		}
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
		if max := c.MaxBinX(); n < 1 || n > max {
			return true, invalidValuef("BinX %d is outside the valid range 1 to %d", n, max)
		}
		return true, c.SetBinX(n)
	case "biny":
		n, err := p.reqInt("BinY")
		if err != nil {
			return true, err
		}
		if max := c.MaxBinY(); n < 1 || n > max {
			return true, invalidValuef("BinY %d is outside the valid range 1 to %d", n, max)
		}
		return true, c.SetBinY(n)

	// Subframe
	case "startx":
		n, err := p.reqInt("StartX")
		if err != nil {
			return true, err
		}
		if n < 0 {
			return true, invalidValuef("StartX %d is negative", n)
		}
		return true, c.SetStartX(n)
	case "starty":
		n, err := p.reqInt("StartY")
		if err != nil {
			return true, err
		}
		if n < 0 {
			return true, invalidValuef("StartY %d is negative", n)
		}
		return true, c.SetStartY(n)
	case "numx":
		n, err := p.reqInt("NumX")
		if err != nil {
			return true, err
		}
		if n < 0 {
			return true, invalidValuef("NumX %d is negative", n)
		}
		return true, c.SetNumX(n)
	case "numy":
		n, err := p.reqInt("NumY")
		if err != nil {
			return true, err
		}
		if n < 0 {
			return true, invalidValuef("NumY %d is negative", n)
		}
		return true, c.SetNumY(n)

	// Gain / Offset / Readout
	case "gain":
		n, err := p.reqInt("Gain")
		if err != nil {
			return true, err
		}
		// In "Gains list" mode the value is an index into Gains; in
		// "Gain value" mode (Gains errors) it is bounded by GainMin/GainMax.
		if gains, gerr := c.Gains(); gerr == nil {
			if n < 0 || n >= len(gains) {
				return true, invalidValuef("Gain index %d is outside the Gains list (0 to %d)", n, len(gains)-1)
			}
		} else if min, max := c.GainMin(), c.GainMax(); n < min || n > max {
			return true, invalidValuef("Gain %d is outside the valid range %d to %d", n, min, max)
		}
		return true, c.SetGain(n)
	case "offset":
		n, err := p.reqInt("Offset")
		if err != nil {
			return true, err
		}
		if offsets, oerr := c.Offsets(); oerr == nil { // see gain
			if n < 0 || n >= len(offsets) {
				return true, invalidValuef("Offset index %d is outside the Offsets list (0 to %d)", n, len(offsets)-1)
			}
		} else if min, max := c.OffsetMin(), c.OffsetMax(); n < min || n > max {
			return true, invalidValuef("Offset %d is outside the valid range %d to %d", n, min, max)
		}
		return true, c.SetOffset(n)
	case "readoutmode":
		n, err := p.reqInt("ReadoutMode")
		if err != nil {
			return true, err
		}
		if modes := c.ReadoutModes(); n < 0 || n >= len(modes) {
			return true, invalidValuef("ReadoutMode %d is outside the ReadoutModes list (0 to %d)", n, len(modes)-1)
		}
		return true, c.SetReadoutMode(n)
	case "fastreadout":
		b, err := p.reqBool("FastReadout")
		if err != nil {
			return true, err
		}
		if !c.CanFastReadout() {
			return true, notImplErr("FastReadout")
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
		if !c.CanSetCCDTemperature() {
			return true, notImplErr("SetCCDTemperature")
		}
		// Physically impossible set points are rejected here (ConformU flags
		// -273.15 °C and 100 °C as "silly" limits); the driver applies its
		// hardware's actual, narrower range.
		if f < -273.15 || f >= 100 {
			return true, invalidValuef("SetCCDTemperature %g is outside the physically plausible range -273.15 to 100", f)
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
		if dur < 0 {
			return true, invalidValuef("Duration %g is negative", dur)
		}
		if err := validSubframe(c); err != nil {
			return true, err
		}
		return true, c.StartExposure(dur, light)
	case "stopexposure":
		if !c.CanStopExposure() {
			return true, notImplErr("StopExposure")
		}
		return true, c.StopExposure()
	case "abortexposure":
		if !c.CanAbortExposure() {
			return true, notImplErr("AbortExposure")
		}
		return true, c.AbortExposure()
	case "subexposureduration":
		f, err := p.reqFloat("SubExposureDuration")
		if err != nil {
			return true, err
		}
		if f < 0 {
			return true, invalidValuef("SubExposureDuration %g is negative", f)
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
		if !c.CanPulseGuide() {
			return true, notImplErr("PulseGuide")
		}
		if dir < int(GuideNorth) || dir > int(GuideWest) {
			return true, invalidValuef("Direction %d is not a valid guide direction", dir)
		}
		if dur < 0 {
			return true, invalidValuef("Duration %d is negative", dur)
		}
		return true, c.PulseGuide(GuideDirection(dir), dur)
	}
	return false, nil
}

// validSubframe checks the subframe geometry at StartExposure time, per
// ICameraV4: the requested region, in binned pixels, must be at least one
// pixel and fit on the binned sensor.
func validSubframe(c Camera) error {
	binX, binY := c.BinX(), c.BinY()
	if binX < 1 || binY < 1 {
		return invalidValuef("BinX/BinY %dx%d is not a valid binning", binX, binY)
	}
	maxW, maxH := c.CameraXSize()/binX, c.CameraYSize()/binY
	startX, startY := c.StartX(), c.StartY()
	numX, numY := c.NumX(), c.NumY()
	switch {
	case numX < 1 || numY < 1:
		return invalidValuef("NumX/NumY %dx%d is not a valid subframe size", numX, numY)
	case startX < 0 || startY < 0:
		return invalidValuef("StartX/StartY %d,%d is not a valid subframe origin", startX, startY)
	case startX+numX > maxW:
		return invalidValuef("subframe X extent %d exceeds the binned sensor width %d", startX+numX, maxW)
	case startY+numY > maxH:
		return invalidValuef("subframe Y extent %d exceeds the binned sensor height %d", startY+numY, maxH)
	}
	return nil
}
