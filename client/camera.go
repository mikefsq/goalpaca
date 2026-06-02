package client

import (
	"net/url"

	alpaca "github.com/mikefsq/goalpaca/server"
)

// Camera is a client for an ASCOM Camera device.
type Camera struct{ Device }

// NewCamera returns a client for the camera at the given Alpaca address and
// device number.
func NewCamera(address string, deviceNumber int, opts ...Option) *Camera {
	return &Camera{newDevice(address, alpaca.CameraType, deviceNumber, opts...)}
}

// Sensor geometry & description.
func (c *Camera) CameraXSize() (int, error)          { return c.getInt("cameraxsize") }
func (c *Camera) CameraYSize() (int, error)          { return c.getInt("cameraysize") }
func (c *Camera) PixelSizeX() (float64, error)       { return c.getFloat("pixelsizex") }
func (c *Camera) PixelSizeY() (float64, error)       { return c.getFloat("pixelsizey") }
func (c *Camera) MaxADU() (int, error)               { return c.getInt("maxadu") }
func (c *Camera) ElectronsPerADU() (float64, error)  { return c.getFloat("electronsperadu") }
func (c *Camera) FullWellCapacity() (float64, error) { return c.getFloat("fullwellcapacity") }
func (c *Camera) SensorName() (string, error)        { return c.getString("sensorname") }
func (c *Camera) BayerOffsetX() (int, error)         { return c.getInt("bayeroffsetx") }
func (c *Camera) BayerOffsetY() (int, error)         { return c.getInt("bayeroffsety") }
func (c *Camera) SensorType() (alpaca.SensorType, error) {
	v, err := c.getInt("sensortype")
	return alpaca.SensorType(v), err
}

// Binning.
func (c *Camera) BinX() (int, error)              { return c.getInt("binx") }
func (c *Camera) BinY() (int, error)              { return c.getInt("biny") }
func (c *Camera) SetBinX(n int) error             { return c.put("binx", url.Values{"BinX": {intParam(n)}}) }
func (c *Camera) SetBinY(n int) error             { return c.put("biny", url.Values{"BinY": {intParam(n)}}) }
func (c *Camera) MaxBinX() (int, error)           { return c.getInt("maxbinx") }
func (c *Camera) MaxBinY() (int, error)           { return c.getInt("maxbiny") }
func (c *Camera) CanAsymmetricBin() (bool, error) { return c.getBool("canasymmetricbin") }

// Subframe (ROI), in binned pixels.
func (c *Camera) StartX() (int, error)  { return c.getInt("startx") }
func (c *Camera) StartY() (int, error)  { return c.getInt("starty") }
func (c *Camera) SetStartX(n int) error { return c.put("startx", url.Values{"StartX": {intParam(n)}}) }
func (c *Camera) SetStartY(n int) error { return c.put("starty", url.Values{"StartY": {intParam(n)}}) }
func (c *Camera) NumX() (int, error)    { return c.getInt("numx") }
func (c *Camera) NumY() (int, error)    { return c.getInt("numy") }
func (c *Camera) SetNumX(n int) error   { return c.put("numx", url.Values{"NumX": {intParam(n)}}) }
func (c *Camera) SetNumY(n int) error   { return c.put("numy", url.Values{"NumY": {intParam(n)}}) }

// Exposure.
func (c *Camera) StartExposure(duration float64, light bool) error {
	return c.put("startexposure", url.Values{"Duration": {floatParam(duration)}, "Light": {boolParam(light)}})
}
func (c *Camera) StopExposure() error                    { return c.put("stopexposure", nil) }
func (c *Camera) AbortExposure() error                   { return c.put("abortexposure", nil) }
func (c *Camera) CanStopExposure() (bool, error)         { return c.getBool("canstopexposure") }
func (c *Camera) CanAbortExposure() (bool, error)        { return c.getBool("canabortexposure") }
func (c *Camera) ImageReady() (bool, error)              { return c.getBool("imageready") }
func (c *Camera) PercentCompleted() (int, error)         { return c.getInt("percentcompleted") }
func (c *Camera) ExposureMin() (float64, error)          { return c.getFloat("exposuremin") }
func (c *Camera) ExposureMax() (float64, error)          { return c.getFloat("exposuremax") }
func (c *Camera) ExposureResolution() (float64, error)   { return c.getFloat("exposureresolution") }
func (c *Camera) LastExposureDuration() (float64, error) { return c.getFloat("lastexposureduration") }
func (c *Camera) LastExposureStartTime() (string, error) { return c.getString("lastexposurestarttime") }
func (c *Camera) HasShutter() (bool, error)              { return c.getBool("hasshutter") }
func (c *Camera) CameraState() (alpaca.CameraState, error) {
	v, err := c.getInt("camerastate")
	return alpaca.CameraState(v), err
}
func (c *Camera) SubExposureDuration() (float64, error) { return c.getFloat("subexposureduration") }
func (c *Camera) SetSubExposureDuration(seconds float64) error {
	return c.put("subexposureduration", url.Values{"SubExposureDuration": {floatParam(seconds)}})
}

// ImageArray fetches the latest frame via the binary ImageBytes transport and
// decodes it into an ImageFrame.
func (c *Camera) ImageArray() (alpaca.ImageFrame, error) { return c.getImageBytes("imagearray") }

// Gain / Offset.
func (c *Camera) Gain() (int, error)         { return c.getInt("gain") }
func (c *Camera) SetGain(n int) error        { return c.put("gain", url.Values{"Gain": {intParam(n)}}) }
func (c *Camera) GainMin() (int, error)      { return c.getInt("gainmin") }
func (c *Camera) GainMax() (int, error)      { return c.getInt("gainmax") }
func (c *Camera) Gains() ([]string, error)   { return c.getStringList("gains") }
func (c *Camera) Offset() (int, error)       { return c.getInt("offset") }
func (c *Camera) SetOffset(n int) error      { return c.put("offset", url.Values{"Offset": {intParam(n)}}) }
func (c *Camera) OffsetMin() (int, error)    { return c.getInt("offsetmin") }
func (c *Camera) OffsetMax() (int, error)    { return c.getInt("offsetmax") }
func (c *Camera) Offsets() ([]string, error) { return c.getStringList("offsets") }

// Readout modes.
func (c *Camera) ReadoutMode() (int, error) { return c.getInt("readoutmode") }
func (c *Camera) SetReadoutMode(n int) error {
	return c.put("readoutmode", url.Values{"ReadoutMode": {intParam(n)}})
}
func (c *Camera) ReadoutModes() ([]string, error) { return c.getStringList("readoutmodes") }
func (c *Camera) FastReadout() (bool, error)      { return c.getBool("fastreadout") }
func (c *Camera) SetFastReadout(on bool) error {
	return c.put("fastreadout", url.Values{"FastReadout": {boolParam(on)}})
}
func (c *Camera) CanFastReadout() (bool, error) { return c.getBool("canfastreadout") }

// Cooling.
func (c *Camera) CCDTemperature() (float64, error)      { return c.getFloat("ccdtemperature") }
func (c *Camera) HeatSinkTemperature() (float64, error) { return c.getFloat("heatsinktemperature") }
func (c *Camera) CoolerOn() (bool, error)               { return c.getBool("cooleron") }
func (c *Camera) SetCoolerOn(on bool) error {
	return c.put("cooleron", url.Values{"CoolerOn": {boolParam(on)}})
}
func (c *Camera) CoolerPower() (float64, error)       { return c.getFloat("coolerpower") }
func (c *Camera) CanGetCoolerPower() (bool, error)    { return c.getBool("cangetcoolerpower") }
func (c *Camera) CanSetCCDTemperature() (bool, error) { return c.getBool("cansetccdtemperature") }

// SetCCDTemperature returns the current cooler setpoint (°C); SetSetCCDTemperature
// writes it. The names mirror the ASCOM property "SetCCDTemperature".
func (c *Camera) SetCCDTemperature() (float64, error) { return c.getFloat("setccdtemperature") }
func (c *Camera) SetSetCCDTemperature(celsius float64) error {
	return c.put("setccdtemperature", url.Values{"SetCCDTemperature": {floatParam(celsius)}})
}

// Guiding.
func (c *Camera) CanPulseGuide() (bool, error)  { return c.getBool("canpulseguide") }
func (c *Camera) IsPulseGuiding() (bool, error) { return c.getBool("ispulseguiding") }
func (c *Camera) PulseGuide(direction alpaca.GuideDirection, duration int) error {
	return c.put("pulseguide", url.Values{"Direction": {intParam(int(direction))}, "Duration": {intParam(duration)}})
}
