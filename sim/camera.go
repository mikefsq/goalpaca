package sim

import (
	"encoding/binary"
	"sync"
	"time"

	"github.com/mikefsq/goalpaca/server"
)

// Camera is a simulated monochrome CMOS ASCOM Camera. Exposure progress,
// readiness and cooling are computed from the clock on read (no background
// goroutine). The server library validates the bin/subframe/gain/offset writes;
// the simulator only clamps the exposure duration and cooler set point to its
// own hardware limits. The image it returns is a synthetic horizontal gradient
// sized to the current subframe.
type Camera struct {
	server.BaseCamera

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
	expW, expH    int  // subframe geometry captured at StartExposure
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
func (c *Camera) SensorType() server.SensorType {
	return server.SensorMonochrome
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.binX = v
	return nil
}

func (c *Camera) SetBinY(v int) error {
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startX = v
	return nil
}

func (c *Camera) SetStartY(v int) error {
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.numX = v
	return nil
}

func (c *Camera) SetNumY(v int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.numY = v
	return nil
}

// --- Exposure ---

// StartExposure begins a simulated exposure. The server library has already
// validated the subframe geometry against the current binning and sensor
// size; this only enforces the camera's own advertised duration range.
func (c *Camera) StartExposure(duration float64, light bool) error {
	if duration < c.ExposureMin() || duration > c.ExposureMax() {
		return server.ErrInvalidValue
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.startTime = now
	c.readyAt = now.Add(time.Duration(duration * float64(time.Second)))
	c.lastDuration = duration
	c.lastStartTime = now.UTC().Format("2006-01-02T15:04:05") // FITS-style, UTC per ICameraV4
	c.hasExposure = true
	c.exposing = true
	c.expW, c.expH = c.numX, c.numY // the frame is the geometry exposed, not a later ROI edit
	return nil
}

// StopExposure ends an in-progress exposure early, KEEPING the data gathered
// so far: the image becomes ready immediately and the recorded duration is
// the actual exposure time. A no-op when no exposure is in progress.
func (c *Camera) StopExposure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now := time.Now(); c.exposing && now.Before(c.readyAt) {
		c.readyAt = now
		c.lastDuration = now.Sub(c.startTime).Seconds()
	}
	c.exposing = false
	return nil
}

// AbortExposure cancels an in-progress exposure, DISCARDING it: no image may
// be offered afterwards (ICameraV4). A no-op when no exposure is in progress.
func (c *Camera) AbortExposure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exposing && time.Now().Before(c.readyAt) {
		c.hasExposure = false
	}
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

func (c *Camera) CameraState() server.CameraState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exposing && time.Now().Before(c.readyAt) {
		return server.CameraExposing
	}
	return server.CameraIdle
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
		return 0, server.ErrValueNotSet
	}
	return c.lastDuration, nil
}

func (c *Camera) LastExposureStartTime() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.hasExposure {
		return "", server.ErrValueNotSet
	}
	return c.lastStartTime, nil
}

func (c *Camera) HasShutter() bool { return true }

// SubExposureDuration/SetSubExposureDuration use the BaseCamera default
// (NotImplemented): sub-exposure stacking is not simulated.

// --- Image transport ---

// ImageFrame builds a synthetic monochrome frame (a horizontal 16-bit gradient)
// sized to the current subframe. It is available only once an exposure is ready.
func (c *Camera) ImageFrame() (server.ImageFrame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.hasExposure || time.Now().Before(c.readyAt) {
		// ICameraV4: ImageArray while ImageReady is false is an
		// InvalidOperationException (0x40B), not ValueNotSet.
		return server.ImageFrame{}, server.ErrInvalidOperation
	}
	w := c.expW
	h := c.expH
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
	return server.ImageFrame{
		Rank:                    2,
		Width:                   w,
		Height:                  h,
		ElementType:             server.ImgInt32,
		TransmissionElementType: server.ImgUInt16,
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset = v
	return nil
}

func (c *Camera) OffsetMin() int { return 0 }
func (c *Camera) OffsetMax() int { return 100 }

// Offsets uses the BaseCamera default (NotImplemented): value (min/max) offset mode.

// --- Readout modes ---

func (c *Camera) ReadoutMode() int         { return 0 }
func (c *Camera) SetReadoutMode(int) error { return nil }
func (c *Camera) ReadoutModes() []string   { return []string{"Default"} }

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
	// Coolers can only cool: the set point tops out a little above ambient.
	// (ConformU flags any driver that accepts a set point of 100 °C.)
	if v < -273.15 || v > 30 {
		return server.ErrInvalidValue
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
