package conformance

import (
	"errors"
	"testing"
	"time"

	"github.com/mikefsq/goalpaca/client"
	"github.com/mikefsq/goalpaca/server"
)

// CheckCamera checks camera properties, exposure, image transport, and errors.
// It assumes the goalpaca simulator's geometry, capabilities, and short exposures.
func CheckCamera(t *testing.T, c *client.Camera) {
	t.Helper()

	// NotConnected gating: an operational member must fault while disconnected.
	_ = c.SetConnected(false)
	if _, err := c.CameraXSize(); !errors.Is(err, server.ErrNotConnected) {
		t.Errorf("CameraXSize() while disconnected: want NotConnected, got %v", err)
	}
	if err := c.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// --- Sensor geometry & description ---
	if x, err := c.CameraXSize(); err != nil || x != 1936 {
		t.Errorf("CameraXSize() = %v, %v; want 1936", x, err)
	}
	if y, err := c.CameraYSize(); err != nil || y != 1096 {
		t.Errorf("CameraYSize() = %v, %v; want 1096", y, err)
	}
	if v, err := c.PixelSizeX(); err != nil || v <= 0 {
		t.Errorf("PixelSizeX() = %v, %v; want > 0", v, err)
	}
	if v, err := c.PixelSizeY(); err != nil || v <= 0 {
		t.Errorf("PixelSizeY() = %v, %v; want > 0", v, err)
	}
	if v, err := c.MaxADU(); err != nil || v <= 0 {
		t.Errorf("MaxADU() = %v, %v; want > 0", v, err)
	}
	if v, err := c.ElectronsPerADU(); err != nil || v <= 0 {
		t.Errorf("ElectronsPerADU() = %v, %v; want > 0", v, err)
	}
	if v, err := c.FullWellCapacity(); err != nil || v <= 0 {
		t.Errorf("FullWellCapacity() = %v, %v; want > 0", v, err)
	}
	if name, err := c.SensorName(); err != nil || name == "" {
		t.Errorf("SensorName() = %q, %v; want non-empty", name, err)
	}
	if _, err := c.SensorType(); err != nil {
		t.Errorf("SensorType(): %v", err)
	}

	// --- Unsupported-capability members must report NotImplemented (ASCOM) ---
	// Monochrome sensor: Bayer offsets are not implemented.
	if _, err := c.BayerOffsetX(); !errors.Is(err, server.ErrNotImplemented) {
		t.Errorf("BayerOffsetX() on monochrome: want NotImplemented, got %v", err)
	}
	if _, err := c.BayerOffsetY(); !errors.Is(err, server.ErrNotImplemented) {
		t.Errorf("BayerOffsetY() on monochrome: want NotImplemented, got %v", err)
	}
	// Value (min/max) gain & offset mode: the name lists are not implemented.
	if _, err := c.Gains(); !errors.Is(err, server.ErrNotImplemented) {
		t.Errorf("Gains() in value mode: want NotImplemented, got %v", err)
	}
	if _, err := c.Offsets(); !errors.Is(err, server.ErrNotImplemented) {
		t.Errorf("Offsets() in value mode: want NotImplemented, got %v", err)
	}
	// CanFastReadout is false ⇒ reading FastReadout must report NotImplemented.
	if _, err := c.FastReadout(); !errors.Is(err, server.ErrNotImplemented) {
		t.Errorf("FastReadout() when CanFastReadout false: want NotImplemented, got %v", err)
	}
	// CanPulseGuide is false ⇒ PulseGuide must report NotImplemented.
	if err := c.PulseGuide(server.GuideNorth, 10); !errors.Is(err, server.ErrNotImplemented) {
		t.Errorf("PulseGuide() when CanPulseGuide false: want NotImplemented, got %v", err)
	}

	// --- Binning ---
	if b, err := c.BinX(); err != nil || b != 1 {
		t.Errorf("BinX() default = %v, %v; want 1", b, err)
	}
	if b, err := c.BinY(); err != nil || b != 1 {
		t.Errorf("BinY() default = %v, %v; want 1", b, err)
	}
	if err := c.SetBinX(2); err != nil {
		t.Errorf("SetBinX(2): %v", err)
	}
	if err := c.SetBinY(2); err != nil {
		t.Errorf("SetBinY(2): %v", err)
	}
	if b, err := c.BinX(); err != nil || b != 2 {
		t.Errorf("BinX() after set = %v, %v; want 2", b, err)
	}
	if b, err := c.BinY(); err != nil || b != 2 {
		t.Errorf("BinY() after set = %v, %v; want 2", b, err)
	}
	if err := c.SetBinX(1); err != nil { // restore
		t.Errorf("SetBinX(1) restore: %v", err)
	}
	if err := c.SetBinY(1); err != nil { // restore
		t.Errorf("SetBinY(1) restore: %v", err)
	}
	for _, bad := range []int{99, 0} { // MaxBinX is 4
		if err := c.SetBinX(bad); !errors.Is(err, server.ErrInvalidValue) {
			t.Errorf("SetBinX(%d): want InvalidValue, got %v", bad, err)
		}
	}

	// --- Subframe / Gain / Offset ---
	if v, err := c.NumX(); err != nil || v <= 0 {
		t.Errorf("NumX() = %v, %v; want > 0", v, err)
	}
	if v, err := c.NumY(); err != nil || v <= 0 {
		t.Errorf("NumY() = %v, %v; want > 0", v, err)
	}

	if err := c.SetGain(150); err != nil { // in range 0..300
		t.Errorf("SetGain(150): %v", err)
	}
	if g, err := c.Gain(); err != nil || g != 150 {
		t.Errorf("Gain() after set = %v, %v; want 150", g, err)
	}
	if err := c.SetGain(9999); !errors.Is(err, server.ErrInvalidValue) {
		t.Errorf("SetGain(9999): want InvalidValue, got %v", err)
	}

	if err := c.SetOffset(50); err != nil { // in range 0..100
		t.Errorf("SetOffset(50): %v", err)
	}
	if o, err := c.Offset(); err != nil || o != 50 {
		t.Errorf("Offset() after set = %v, %v; want 50", o, err)
	}
	if err := c.SetOffset(9999); !errors.Is(err, server.ErrInvalidValue) {
		t.Errorf("SetOffset(9999): want InvalidValue, got %v", err)
	}

	// --- Cooling ---
	if can, err := c.CanSetCCDTemperature(); err != nil || !can {
		t.Errorf("CanSetCCDTemperature() = %v, %v; want true", can, err)
	}
	if err := c.SetSetCCDTemperature(-10); err != nil {
		t.Errorf("SetSetCCDTemperature(-10): %v", err)
	}
	if sp, err := c.SetCCDTemperature(); err != nil || sp < -10.5 || sp > -9.5 {
		t.Errorf("SetCCDTemperature() setpoint = %v, %v; want ~-10", sp, err)
	}
	// ConformU steps the set point up from 0 °C in 5° increments and flags an
	// issue if no error occurs before reaching 100 °C: a real cooler's set
	// point must top out below boiling.
	if err := c.SetSetCCDTemperature(100); !errors.Is(err, server.ErrInvalidValue) {
		t.Errorf("SetSetCCDTemperature(100): want InvalidValue (upper limit below 100 C), got %v", err)
	}
	if err := c.SetSetCCDTemperature(-300); !errors.Is(err, server.ErrInvalidValue) {
		t.Errorf("SetSetCCDTemperature(-300): want InvalidValue (below absolute zero), got %v", err)
	}
	if err := c.SetCoolerOn(true); err != nil {
		t.Errorf("SetCoolerOn(true): %v", err)
	}
	if _, err := c.CCDTemperature(); err != nil {
		t.Errorf("CCDTemperature(): %v", err)
	}
	if _, err := c.CoolerPower(); err != nil {
		t.Errorf("CoolerPower(): %v", err)
	}

	// --- Exposure happy path ---
	// Before any exposure, ImageArray must report InvalidOperation (0x40B) —
	// ICameraV4 specifies InvalidOperationException while ImageReady is false.
	if _, err := c.ImageArray(); !errors.Is(err, server.ErrInvalidOperation) {
		t.Errorf("ImageArray() before exposure: want InvalidOperation, got %v", err)
	}

	numX, err := c.NumX()
	if err != nil {
		t.Fatalf("NumX(): %v", err)
	}
	numY, err := c.NumY()
	if err != nil {
		t.Fatalf("NumY(): %v", err)
	}

	if err := c.StartExposure(0.05, true); err != nil {
		t.Fatalf("StartExposure(0.05, true): %v", err)
	}
	if !cameraWaitImageReady(t, c) {
		t.Fatal("exposure did not become ready before timeout")
	}

	frame, err := c.ImageArray()
	if err != nil {
		t.Fatalf("ImageArray(): %v", err)
	}
	if frame.Rank != 2 {
		t.Errorf("ImageArray() Rank = %d; want 2", frame.Rank)
	}
	if frame.Width != numX {
		t.Errorf("ImageArray() Width = %d; want NumX %d", frame.Width, numX)
	}
	if frame.Height != numY {
		t.Errorf("ImageArray() Height = %d; want NumY %d", frame.Height, numY)
	}
	if want := frame.Width * frame.Height * 2; len(frame.Pixels) != want {
		t.Errorf("ImageArray() len(Pixels) = %d; want Width*Height*2 = %d", len(frame.Pixels), want)
	}
	if _, err := c.CameraState(); err != nil {
		t.Errorf("CameraState(): %v", err)
	}

	// --- Exposure validation ---
	if err := c.StartExposure(99999, true); !errors.Is(err, server.ErrInvalidValue) { // > ExposureMax
		t.Errorf("StartExposure(99999): want InvalidValue, got %v", err)
	}

	// A subframe larger than the sensor must be rejected at StartExposure.
	origNumX, _ := c.NumX()
	if err := c.SetNumX(99999); err != nil {
		t.Errorf("SetNumX(99999): %v", err)
	}
	if err := c.StartExposure(0.05, true); !errors.Is(err, server.ErrInvalidValue) {
		t.Errorf("StartExposure with oversized NumX: want InvalidValue, got %v", err)
	}
	_ = c.SetNumX(origNumX) // restore

	// --- Binning ranges & symmetry ---
	maxBinX, err := c.MaxBinX()
	if err != nil || maxBinX < 1 || maxBinX > 16 {
		t.Errorf("MaxBinX() = %v, %v; want in [1,16]", maxBinX, err)
	}
	maxBinY, err := c.MaxBinY()
	if err != nil || maxBinY < 1 || maxBinY > 16 {
		t.Errorf("MaxBinY() = %v, %v; want in [1,16]", maxBinY, err)
	}
	// CanAsymmetricBin is false ⇒ X/Y maxima must match.
	if can, err := c.CanAsymmetricBin(); err != nil {
		t.Errorf("CanAsymmetricBin(): %v", err)
	} else if !can && maxBinX != maxBinY {
		t.Errorf("MaxBinX (%d) != MaxBinY (%d) with CanAsymmetricBin false", maxBinX, maxBinY)
	}

	// BinY out-of-range writes must report InvalidValue (mirrors the BinX checks).
	for _, bad := range []int{0, 99} {
		if err := c.SetBinY(bad); !errors.Is(err, server.ErrInvalidValue) {
			t.Errorf("SetBinY(%d): want InvalidValue, got %v", bad, err)
		}
	}

	// --- Gain / Offset ranges ---
	gMin, err := c.GainMin()
	if err != nil {
		t.Errorf("GainMin(): %v", err)
	}
	gMax, err := c.GainMax()
	if err != nil {
		t.Errorf("GainMax(): %v", err)
	}
	if gMax <= gMin {
		t.Errorf("GainMax (%d) must be > GainMin (%d)", gMax, gMin)
	}
	oMin, err := c.OffsetMin()
	if err != nil {
		t.Errorf("OffsetMin(): %v", err)
	}
	oMax, err := c.OffsetMax()
	if err != nil {
		t.Errorf("OffsetMax(): %v", err)
	}
	if oMax <= oMin {
		t.Errorf("OffsetMax (%d) must be > OffsetMin (%d)", oMax, oMin)
	}

	// --- Exposure limits ---
	expMin, err := c.ExposureMin()
	if err != nil || expMin < 0 {
		t.Errorf("ExposureMin() = %v, %v; want >= 0", expMin, err)
	}
	expMax, err := c.ExposureMax()
	if err != nil || expMax <= 0 {
		t.Errorf("ExposureMax() = %v, %v; want > 0", expMax, err)
	}
	if expMin > expMax {
		t.Errorf("ExposureMin (%v) must be <= ExposureMax (%v)", expMin, expMax)
	}
	if res, err := c.ExposureResolution(); err != nil || res < 0 {
		t.Errorf("ExposureResolution() = %v, %v; want >= 0", res, err)
	}

	// --- Readout modes ---
	modes, err := c.ReadoutModes()
	if err != nil {
		t.Errorf("ReadoutModes(): %v", err)
	}
	if len(modes) == 0 {
		t.Errorf("ReadoutModes() is empty; want at least one mode")
	}
	if mode, err := c.ReadoutMode(); err != nil || mode < 0 || mode >= len(modes) {
		t.Errorf("ReadoutMode() = %v, %v; want in [0,%d)", mode, err, len(modes))
	}

	// --- Misc readable properties ---
	if _, err := c.HeatSinkTemperature(); err != nil {
		t.Errorf("HeatSinkTemperature(): %v", err)
	}
	if _, err := c.HasShutter(); err != nil {
		t.Errorf("HasShutter(): %v", err)
	}

	// --- Subframe geometry & legal round-trips ---
	xSize, err := c.CameraXSize()
	if err != nil {
		t.Fatalf("CameraXSize(): %v", err)
	}
	ySize, err := c.CameraYSize()
	if err != nil {
		t.Fatalf("CameraYSize(): %v", err)
	}
	if v, err := c.StartX(); err != nil || v < 0 {
		t.Errorf("StartX() = %v, %v; want >= 0", v, err)
	}
	if v, err := c.StartY(); err != nil || v < 0 {
		t.Errorf("StartY() = %v, %v; want >= 0", v, err)
	}
	if v, err := c.NumX(); err != nil || v < 1 || v > xSize {
		t.Errorf("NumX() = %v, %v; want in [1,%d]", v, err, xSize)
	}
	if v, err := c.NumY(); err != nil || v < 1 || v > ySize {
		t.Errorf("NumY() = %v, %v; want in [1,%d]", v, err, ySize)
	}

	// Legal subframe writes must round-trip, then restore to the full frame.
	if err := c.SetStartX(10); err != nil {
		t.Errorf("SetStartX(10): %v", err)
	}
	if v, err := c.StartX(); err != nil || v != 10 {
		t.Errorf("StartX() after set = %v, %v; want 10", v, err)
	}
	if err := c.SetNumX(100); err != nil {
		t.Errorf("SetNumX(100): %v", err)
	}
	if v, err := c.NumX(); err != nil || v != 100 {
		t.Errorf("NumX() after set = %v, %v; want 100", v, err)
	}
	if err := c.SetStartX(0); err != nil { // restore
		t.Errorf("SetStartX(0) restore: %v", err)
	}
	if err := c.SetNumX(xSize); err != nil { // restore to full width
		t.Errorf("SetNumX(%d) restore: %v", xSize, err)
	}

	if err := c.SetStartY(10); err != nil {
		t.Errorf("SetStartY(10): %v", err)
	}
	if v, err := c.StartY(); err != nil || v != 10 {
		t.Errorf("StartY() after set = %v, %v; want 10", v, err)
	}
	if err := c.SetNumY(100); err != nil {
		t.Errorf("SetNumY(100): %v", err)
	}
	if v, err := c.NumY(); err != nil || v != 100 {
		t.Errorf("NumY() after set = %v, %v; want 100", v, err)
	}
	if err := c.SetStartY(0); err != nil { // restore
		t.Errorf("SetStartY(0) restore: %v", err)
	}
	if err := c.SetNumY(ySize); err != nil { // restore to full height
		t.Errorf("SetNumY(%d) restore: %v", ySize, err)
	}

	// --- CameraState transition over a timed exposure ---
	// A ~0.5s exposure makes the Exposing state observable before completion.
	const stateExposure = 0.5
	if err := c.StartExposure(stateExposure, true); err != nil {
		t.Fatalf("StartExposure(%.1f, true): %v", stateExposure, err)
	}
	// Shortly after starting, the camera must report Exposing.
	time.Sleep(50 * time.Millisecond)
	if st, err := c.CameraState(); err != nil {
		t.Errorf("CameraState() during exposure: %v", err)
	} else if st != server.CameraExposing {
		t.Errorf("CameraState() during exposure = %v; want CameraExposing", st)
	}
	if !cameraWaitImageReady(t, c) {
		t.Fatal("timed exposure did not become ready before timeout")
	}
	if st, err := c.CameraState(); err != nil {
		t.Errorf("CameraState() after exposure: %v", err)
	} else if st != server.CameraIdle {
		t.Errorf("CameraState() after exposure = %v; want CameraIdle", st)
	}

	// --- Last-exposure metadata after the timed exposure ---
	if d, err := c.LastExposureDuration(); err != nil {
		t.Errorf("LastExposureDuration(): %v", err)
	} else if d < stateExposure-0.01 || d > stateExposure+0.01 {
		t.Errorf("LastExposureDuration() = %v; want ~%.1f", d, stateExposure)
	}
	if ts, err := c.LastExposureStartTime(); err != nil {
		t.Errorf("LastExposureStartTime(): %v", err)
	} else if ts == "" {
		t.Errorf("LastExposureStartTime() is empty; want a non-empty timestamp")
	} else if start, perr := time.Parse("2006-01-02T15:04:05", ts[:min(len(ts), 19)]); perr != nil {
		// ConformU requires the FITS-style yyyy-mm-ddThh:mm:ss prefix.
		t.Errorf("LastExposureStartTime() = %q; want yyyy-mm-ddThh:mm:ss format: %v", ts, perr)
	} else if d := time.Now().UTC().Sub(start); d < 0 || d > 2*time.Minute {
		// ConformU parses the string as UTC and requires it within 2 seconds
		// of the exposure start; a local-time string shows up as a whole
		// timezone offset, which this window catches.
		t.Errorf("LastExposureStartTime() = %q; not a recent UTC timestamp (off by %v)", ts, d)
	}

	// --- Abort / Stop when idle must succeed ---
	if err := c.AbortExposure(); err != nil {
		t.Errorf("AbortExposure() when idle: %v", err)
	}
	if err := c.StopExposure(); err != nil {
		t.Errorf("StopExposure() when idle: %v", err)
	}
	// Idle Abort/Stop must not have discarded the completed image.
	if ready, err := c.ImageReady(); err != nil || !ready {
		t.Errorf("ImageReady() after idle Abort/Stop = %v, %v; want true (completed image kept)", ready, err)
	}

	// --- StopExposure mid-exposure keeps the partial image ---
	if err := c.StartExposure(2, true); err != nil {
		t.Fatalf("StartExposure(2, true): %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := c.StopExposure(); err != nil {
		t.Fatalf("StopExposure() mid-exposure: %v", err)
	}
	if !cameraWaitImageReady(t, c) {
		t.Error("image did not become ready after mid-exposure StopExposure")
	}
	if st, err := c.CameraState(); err != nil || st != server.CameraIdle {
		t.Errorf("CameraState() after StopExposure = %v, %v; want CameraIdle", st, err)
	}
	if d, err := c.LastExposureDuration(); err != nil {
		t.Errorf("LastExposureDuration() after stop: %v", err)
	} else if d >= 2 {
		t.Errorf("LastExposureDuration() after stop = %v; want the actual (shortened) duration", d)
	}

	// --- AbortExposure mid-exposure discards the image ---
	if err := c.StartExposure(2, true); err != nil {
		t.Fatalf("StartExposure(2, true): %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := c.AbortExposure(); err != nil {
		t.Fatalf("AbortExposure() mid-exposure: %v", err)
	}
	if st, err := c.CameraState(); err != nil || st != server.CameraIdle {
		t.Errorf("CameraState() after AbortExposure = %v, %v; want CameraIdle", st, err)
	}
	// The aborted exposure must never yield an image: ImageReady stays false
	// (checked past the original exposure window would be ideal, but 2s is too
	// slow for a unit run)
	// and ImageArray reports InvalidOperation.
	if ready, err := c.ImageReady(); err != nil || ready {
		t.Errorf("ImageReady() after abort = %v, %v; want false", ready, err)
	}
	if _, err := c.ImageArray(); !errors.Is(err, server.ErrInvalidOperation) {
		t.Errorf("ImageArray() after abort: want InvalidOperation, got %v", err)
	}
}

// cameraWaitImageReady polls ImageReady until it reports true or the timeout
// elapses, returning whether the image became ready.
func cameraWaitImageReady(t *testing.T, c *client.Camera) bool {
	t.Helper()
	deadline := time.Now().Add(SettleTimeout)
	for time.Now().Before(deadline) {
		ready, err := c.ImageReady()
		if err != nil {
			t.Fatalf("ImageReady(): %v", err)
		}
		if ready {
			return true
		}
		time.Sleep(15 * time.Millisecond)
	}
	return false
}
