package sim

import (
	"math"
	"math/rand/v2"
	"sync"

	"github.com/mikefsq/goalpaca/server"
)

// Light couples sim flat panels to sim cameras. Camera with a Light sees the panel while any
// bound panel cover reads closed: lit panel gives flat field, dark panel gives bias frame.
// Cover open, moving or unknown: camera keeps its own image. One Light per optical train;
// every camera on it sees the same panel.
type Light struct {
	mu     sync.Mutex
	panels []*CoverCalibrator
}

// NewLight returns a Light with no panel bound.
func NewLight() *Light { return &Light{} }

// Flat model constants. Full panel gives lightFullADUPerSec ADU per second at gain 100, bin 1.
const (
	lightFullADUPerSec = 60000.0
	lightBiasPerOffset = 10.0 // ADU of bias per offset unit
	lightReadNoiseADU  = 3.0
	lightVignette      = 0.2 // fractional falloff at the sensor corner
)

// WithCalibratorLight binds the panel to l.
func WithCalibratorLight(l *Light) CoverCalibratorOption {
	return func(c *CoverCalibrator) {
		l.mu.Lock()
		l.panels = append(l.panels, c)
		l.mu.Unlock()
	}
}

// WithCameraLight makes the camera see panels bound to l.
func WithCameraLight(l *Light) CameraOption {
	return func(c *Camera) { c.light = l }
}

// state reports whether a bound panel covers the optics and its lit fraction (0..1). Lit counts
// only once the calibrator reads Ready.
func (l *Light) state() (covered bool, frac float64) {
	l.mu.Lock()
	panels := append([]*CoverCalibrator(nil), l.panels...)
	l.mu.Unlock()
	for _, p := range panels {
		if p.CoverState() != server.CoverClosed {
			continue
		}
		covered = true
		if p.CalibratorState() == server.CalibratorReady && p.MaxBrightness() > 0 {
			frac = max(frac, float64(p.Brightness())/float64(p.MaxBrightness()))
		}
	}
	return covered, frac
}

// lightGeom is the exposed geometry a panel frame needs: binned size and origin, full binned
// sensor size, bin factors.
type lightGeom struct {
	w, h, x0, y0, fullW, fullH, binX, binY int
}

// render writes the panel frame into buf as little-endian uint16: bias + signal · vignetting,
// shot and read noise, clipped to 65535. signal = full rate · lit fraction · seconds · gain/100 ·
// bin area.
func (l *Light) render(buf []byte, g lightGeom, frac, secs float64, gain, offset int) {
	bias := lightBiasPerOffset * float64(offset)
	signal := lightFullADUPerSec * frac * secs * float64(gain) / 100 * float64(g.binX*g.binY)
	cx, cy := float64(g.fullW)/2, float64(g.fullH)/2
	r2max := cx*cx + cy*cy
	for y := 0; y < g.h; y++ {
		dy := float64(g.y0+y) + 0.5 - cy
		for x := 0; x < g.w; x++ {
			dx := float64(g.x0+x) + 0.5 - cx
			s := signal * (1 - lightVignette*(dx*dx+dy*dy)/r2max)
			// Shot noise: 0.25 e-/ADU makes sigma 2·sqrt(ADU).
			v := bias + s + rand.NormFloat64()*math.Sqrt(4*s+lightReadNoiseADU*lightReadNoiseADU)
			off := (y*g.w + x) * 2
			u := uint16(max(0, min(65535, math.Round(v))))
			buf[off], buf[off+1] = byte(u), byte(u>>8)
		}
	}
}
