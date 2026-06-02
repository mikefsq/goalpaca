package client

import (
	"encoding/binary"
	"errors"
	"testing"

	alpaca "github.com/mikefsq/goalpaca/server"
)

// fakeCamera produces a tiny 4x3 16-bit frame after an exposure.
type fakeCamera struct {
	alpaca.BaseCamera
	started bool
}

func (c *fakeCamera) CameraXSize() int                  { return 4 }
func (c *fakeCamera) CameraYSize() int                  { return 3 }
func (c *fakeCamera) StartExposure(float64, bool) error { c.started = true; return nil }
func (c *fakeCamera) ImageReady() bool                  { return c.started }
func (c *fakeCamera) ImageFrame() (alpaca.ImageFrame, error) {
	if !c.started {
		return alpaca.ImageFrame{}, alpaca.ErrValueNotSet
	}
	pix := make([]byte, 4*3*2)
	binary.LittleEndian.PutUint16(pix[0:], 0xBEEF)
	return alpaca.ImageFrame{
		Rank: 2, Width: 4, Height: 3,
		ElementType:             alpaca.ImgInt32,
		TransmissionElementType: alpaca.ImgUInt16,
		Pixels:                  pix,
	}, nil
}

func TestCameraImageArray(t *testing.T) {
	dev := &fakeCamera{}
	dev.DevName = "Cam"
	dev.IfaceVer = 4
	ts := serve(t, alpaca.CameraType, dev)
	c := NewCamera(ts.URL, 0)
	if err := c.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if x, err := c.CameraXSize(); err != nil || x != 4 {
		t.Fatalf("CameraXSize() = %d, %v; want 4", x, err)
	}

	// Before exposure, the device error round-trips through the ImageBytes envelope.
	if _, err := c.ImageArray(); !errors.Is(err, alpaca.ErrValueNotSet) {
		t.Fatalf("ImageArray() before exposure: want ValueNotSet, got %v", err)
	}

	if err := c.StartExposure(1.0, true); err != nil {
		t.Fatalf("StartExposure: %v", err)
	}
	if ready, err := c.ImageReady(); err != nil || !ready {
		t.Fatalf("ImageReady() = %v, %v; want true", ready, err)
	}

	frame, err := c.ImageArray()
	if err != nil {
		t.Fatalf("ImageArray: %v", err)
	}
	if frame.Width != 4 || frame.Height != 3 || frame.Rank != 2 {
		t.Fatalf("frame = %dx%d rank %d; want 4x3 rank 2", frame.Width, frame.Height, frame.Rank)
	}
	if frame.ElementType != alpaca.ImgInt32 || frame.TransmissionElementType != alpaca.ImgUInt16 {
		t.Fatalf("frame types = %d/%d; want Int32/UInt16", frame.ElementType, frame.TransmissionElementType)
	}
	if len(frame.Pixels) != 4*3*2 {
		t.Fatalf("pixels = %d bytes; want %d", len(frame.Pixels), 4*3*2)
	}
	if got := binary.LittleEndian.Uint16(frame.Pixels[0:]); got != 0xBEEF {
		t.Fatalf("pixel[0] = %#x; want 0xBEEF", got)
	}
}
