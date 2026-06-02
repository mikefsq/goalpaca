package sim

import (
	"encoding/binary"
	"sync"
	"time"

	alpacadev "github.com/mikefsq/goalpaca/server"
)

// Camera is a simulated monochrome CMOS ASCOM Camera. Exposure progress,
// readiness and cooling are computed from the clock on read (no background
// goroutine); writes are validated against the documented limits. The image it
// returns is a synthetic horizontal gradient sized to the current subframe.
type Camera struct {
	alpacadev.BaseCamera

	mu sync.Mutex

	// Binning
	binX int
	binY int

	// Subframe (ROI)
	startX int
	startY int
	numX   int
	numY   int

	// Gain / Offset
	gain   int
	offset int

	// Cooling
	coolerOn bool
	setpoint float64

	// Exposure
	startTime     time.Time
	readyAt       time.Time
	lastDuration  float64
	lastStartTime string
	hasExposure   bool // an exposure has been taken (StartExposure called)
	exposing      bool // an exposure is currently active (not stopped/aborted)
}

// CameraOption configures a simulated Camera.
type CameraOption func(*Camera)

// NewCamera creates a simulated monochrome CMOS camera.
func NewCamera(opts ...CameraOption) *Camera {
	c := &Camera{
		binX:   1,
		binY:   1,
		startX: 0,
		startY: 0,
		gain:   100,
		offset: 10,
	}
	c.numX = c.CameraXSize()
	c.numY = c.CameraYSize()
	c.ID = "goalpaca-sim-camera-1"
	c.DevName = "Alpaca Camera Simulator"
	c.Desc = "goalpaca simulated camera"
	c.Info = "goalpaca sim"
	c.Version = "1.0"
	c.IfaceVer = 4
	for _, o := range opts {
		o(c)
	}
	return c
}

// --- Sensor geometry & description ---

func (c *Camera) CameraXSize() int          { return 1936 }
func (c *Camera) CameraYSize() int          { return 1096 }
func (c *Camera) PixelSizeX() float64       { return 5.86 }
func (c *Camera) PixelSizeY() float64       { return 5.86 }
func (c *Camera) MaxADU() int               { return 65535 }
func (c *Camera) ElectronsPerADU() float64  { return 0.25 }
func (c *Camera) FullWellCapacity() float64 { return 51000 }
func (c *Camera) SensorName() string        { return "SimSensor" }
func (c *Camera) SensorType() alpacadev.SensorType {
	return alpacadev.SensorMonochrome
}

// BayerOffsetX/Y use the BaseCamera default (NotImplemented): monochrome sensor.

// --- Binning ---

func (c *Camera) BinX() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.binX
}

func (c *Camera) BinY() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.binY
}

func (c *Camera) SetBinX(v int) error {
	if v < 1 || v > c.MaxBinX() {
		return alpacadev.ErrInvalidValue
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.binX = v
	return nil
}

func (c *Camera) SetBinY(v int) error {
	if v < 1 || v > c.MaxBinY() {
		return alpacadev.ErrInvalidValue
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.binY = v
	return nil
}

func (c *Camera) MaxBinX() int           { return 4 }
func (c *Camera) MaxBinY() int           { return 4 }
func (c *Camera) CanAsymmetricBin() bool { return false }

// --- Subframe (ROI) ---

func (c *Camera) StartX() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startX
}

func (c *Camera) StartY() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startY
}

func (c *Camera) SetStartX(v int) error {
	if v < 0 {
		return alpacadev.ErrInvalidValue
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startX = v
	return nil
}

func (c *Camera) SetStartY(v int) error {
	if v < 0 {
		return alpacadev.ErrInvalidValue
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startY = v
	return nil
}

func (c *Camera) NumX() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.numX
}

func (c *Camera) NumY() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.numY
}

func (c *Camera) SetNumX(v int) error {
	if v < 0 {
		return alpacadev.ErrInvalidValue
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.numX = v
	return nil
}

func (c *Camera) SetNumY(v int) error {
	if v < 0 {
		return alpacadev.ErrInvalidValue
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.numY = v
	return nil
}

// --- Exposure ---

func (c *Camera) StartExposure(duration float64, light bool) error {
	if duration < c.ExposureMin() || duration > c.ExposureMax() {
		return alpacadev.ErrInvalidValue
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// The requested subframe (in binned pixels) must fit on the sensor.
	if c.numX <= 0 || c.numY <= 0 ||
		(c.startX+c.numX)*c.binX > c.CameraXSize() ||
		(c.startY+c.numY)*c.binY > c.CameraYSize() {
		return alpacadev.ErrInvalidValue
	}
	now := time.Now()
	c.startTime = now
	c.readyAt = now.Add(time.Duration(duration * float64(time.Second)))
	c.lastDuration = duration
	c.lastStartTime = now.Format("2006-01-02T15:04:05")
	c.hasExposure = true
	c.exposing = true
	return nil
}

func (c *Camera) StopExposure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.exposing = false
	return nil
}

func (c *Camera) AbortExposure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.exposing = false
	return nil
}

func (c *Camera) CanStopExposure() bool  { return true }
func (c *Camera) CanAbortExposure() bool { return true }

func (c *Camera) ImageReady() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hasExposure && !time.Now().Before(c.readyAt)
}

func (c *Camera) CameraState() alpacadev.CameraState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exposing && time.Now().Before(c.readyAt) {
		return alpacadev.CameraExposing
	}
	return alpacadev.CameraIdle
}

func (c *Camera) PercentCompleted() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.hasExposure || c.lastDuration <= 0 {
		return 0
	}
	now := time.Now()
	if !now.Before(c.readyAt) {
		return 100
	}
	elapsed := now.Sub(c.startTime).Seconds()
	pct := int((elapsed / c.lastDuration) * 100)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

func (c *Camera) ExposureMin() float64        { return 0.001 }
func (c *Camera) ExposureMax() float64        { return 3600 }
func (c *Camera) ExposureResolution() float64 { return 0.001 }

func (c *Camera) LastExposureDuration() (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.hasExposure {
		return 0, alpacadev.ErrValueNotSet
	}
	return c.lastDuration, nil
}

func (c *Camera) LastExposureStartTime() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.hasExposure {
		return "", alpacadev.ErrValueNotSet
	}
	return c.lastStartTime, nil
}

func (c *Camera) HasShutter() bool { return true }

// SubExposureDuration/SetSubExposureDuration use the BaseCamera default
// (NotImplemented): sub-exposure stacking is not simulated.

// --- Image transport ---

// ImageFrame builds a synthetic monochrome frame (a horizontal 16-bit gradient)
// sized to the current subframe. It is available only once an exposure is ready.
func (c *Camera) ImageFrame() (alpacadev.ImageFrame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.hasExposure || time.Now().Before(c.readyAt) {
		return alpacadev.ImageFrame{}, alpacadev.ErrValueNotSet
	}
	w := c.numX
	h := c.numY
	buf := make([]byte, w*h*2)
	for x := 0; x < w; x++ {
		var v uint16
		if w > 0 {
			v = uint16((x * 65535) / w)
		}
		for y := 0; y < h; y++ {
			off := (y*w + x) * 2
			binary.LittleEndian.PutUint16(buf[off:], v)
		}
	}
	return alpacadev.ImageFrame{
		Rank:                    2,
		Width:                   w,
		Height:                  h,
		ElementType:             alpacadev.ImgInt32,
		TransmissionElementType: alpacadev.ImgUInt16,
		Pixels:                  buf,
	}, nil
}

// --- Gain / Offset ---

func (c *Camera) Gain() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gain
}

func (c *Camera) SetGain(v int) error {
	if v < c.GainMin() || v > c.GainMax() {
		return alpacadev.ErrInvalidValue
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gain = v
	return nil
}

func (c *Camera) GainMin() int { return 0 }
func (c *Camera) GainMax() int { return 300 }

// Gains uses the BaseCamera default (NotImplemented): value (min/max) gain mode.

func (c *Camera) Offset() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.offset
}

func (c *Camera) SetOffset(v int) error {
	if v < c.OffsetMin() || v > c.OffsetMax() {
		return alpacadev.ErrInvalidValue
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset = v
	return nil
}

func (c *Camera) OffsetMin() int { return 0 }
func (c *Camera) OffsetMax() int { return 100 }

// Offsets uses the BaseCamera default (NotImplemented): value (min/max) offset mode.

// --- Readout modes ---

func (c *Camera) ReadoutMode() int { return 0 }
func (c *Camera) SetReadoutMode(v int) error {
	if v != 0 {
		return alpacadev.ErrInvalidValue
	}
	return nil
}
func (c *Camera) ReadoutModes() []string { return []string{"Default"} }

// FastReadout/SetFastReadout/CanFastReadout use the BaseCamera defaults
// (NotImplemented / false): this camera has no fast-readout mode.

// --- Cooling ---

// CCDTemperature converges from ambient toward the active target over a few
// seconds, computed from the time since the last exposure start (the simulator's
// reference clock), so repeated reads show a settling curve.
func (c *Camera) CCDTemperature() (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	const ambient = 20.0
	const tau = 5.0 // seconds to converge
	target := ambient
	if c.coolerOn {
		target = c.setpoint
	}
	elapsed := time.Since(c.startTime).Seconds()
	if c.startTime.IsZero() {
		elapsed = tau
	}
	frac := elapsed / tau
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	return ambient + (target-ambient)*frac, nil
}

func (c *Camera) HeatSinkTemperature() (float64, error) { return 20, nil }

func (c *Camera) CoolerOn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.coolerOn
}

func (c *Camera) SetCoolerOn(v bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.coolerOn = v
	return nil
}

func (c *Camera) CoolerPower() (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.coolerOn {
		return 50, nil
	}
	return 0, nil
}

func (c *Camera) CanGetCoolerPower() bool { return true }

func (c *Camera) SetCCDTemperature() (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.setpoint, nil
}

func (c *Camera) SetSetCCDTemperature(v float64) error {
	if v < -273.15 || v > 100 {
		return alpacadev.ErrInvalidValue
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setpoint = v
	return nil
}

func (c *Camera) CanSetCCDTemperature() bool { return true }

// --- Guiding ---
//
// CanPulseGuide/IsPulseGuiding/PulseGuide use the BaseCamera defaults
// (false / false / NotImplemented): this camera does not pulse-guide.
