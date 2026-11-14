package sim

import (
	"encoding/binary"
	"testing"
	"time"
)

// frameMean exposes cam for secs, waits ready, returns mean ADU.
func frameMean(t *testing.T, cam *Camera, secs float64) float64 {
	t.Helper()
	if err := cam.StartExposure(secs, true); err != nil {
		t.Fatal(err)
	}
	for !cam.ImageReady() {
		time.Sleep(time.Millisecond)
	}
	f, err := cam.ImageFrame()
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for i := 0; i+1 < len(f.Pixels); i += 2 {
		sum += float64(binary.LittleEndian.Uint16(f.Pixels[i:]))
	}
	return sum / float64(len(f.Pixels)/2)
}

func waitPanel(p *CoverCalibrator) {
	for p.CoverMoving() || p.CalibratorChanging() {
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLightPanelDrivesCameraBrightness(t *testing.T) {
	l := NewLight()
	panel := NewCoverCalibrator(WithCalibratorLight(l))
	cam := NewCamera(WithCameraLight(l))
	_ = cam.SetNumX(64)
	_ = cam.SetNumY(48)
	_ = cam.SetStartX((1936 - 64) / 2) // sensor centre: vignetting under 1%
	_ = cam.SetStartY((1096 - 48) / 2)
	bias := lightBiasPerOffset * 10 // default offset 10

	// Closed and dark: bias frame.
	if !cam.SeesPanel() {
		t.Fatal("closed panel not seen")
	}
	if m := frameMean(t, cam, 0.01); m < bias-5 || m > bias+5 {
		t.Fatalf("dark panel mean %.1f, want bias %.0f", m, bias)
	}
	// Lit: mean tracks brightness linearly; vignetting only trims a few percent.
	_ = panel.CalibratorOn(25)
	waitPanel(panel)
	low := frameMean(t, cam, 0.01) - bias
	_ = panel.CalibratorOn(50)
	waitPanel(panel)
	high := frameMean(t, cam, 0.01) - bias
	if want := lightFullADUPerSec * 0.5 * 0.01; high < 0.97*want || high > 1.01*want {
		t.Fatalf("50%% panel signal %.0f, want just under %.0f", high, want)
	}
	if r := high / low; r < 1.9 || r > 2.1 {
		t.Fatalf("brightness 50/25 signal ratio %.2f, want 2", r)
	}
	// Open: own gradient again, panel light gone.
	_ = panel.OpenCover()
	waitPanel(panel)
	if cam.SeesPanel() {
		t.Fatal("open panel still seen")
	}
	if m := frameMean(t, cam, 0.01); m < 30000 || m > 35000 {
		t.Fatalf("open cover mean %.0f, want gradient ~32767", m)
	}
}

func TestCameraWithoutLightIgnoresPanel(t *testing.T) {
	cam := NewCamera()
	if cam.SeesPanel() {
		t.Fatal("unbound camera sees a panel")
	}
}
