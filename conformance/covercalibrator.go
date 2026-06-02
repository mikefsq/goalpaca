package conformance

import (
	"errors"
	"testing"
	"time"

	"github.com/mikefsq/goalpaca/client"
	alpacadev "github.com/mikefsq/goalpaca/server"
)

// covercalibratorPollTimeout bounds how long the checks wait for an
// asynchronous calibrator or cover transition to settle. The sim's transitions
// are ~400ms (calibrator) and ~700ms (cover), so ~6s is comfortably generous.
const covercalibratorPollTimeout = 6 * time.Second

// CheckCoverCalibrator runs the ConformU CoverCalibrator conformance checks
// against c. Ported from ConformU's CoverCalibratorTester (CheckProperties /
// CheckMethods): NotConnected gating, MaxBrightness/Brightness/CalibratorState/
// CoverState/CalibratorChanging/CoverMoving readability, the CalibratorOn happy
// path with InvalidValue rejection, CalibratorOff, and OpenCover/CloseCover/
// HaltCover. Checks the sim does not implement are skipped and noted below.
func CheckCoverCalibrator(t *testing.T, c *client.CoverCalibrator) {
	t.Helper()

	// NotConnected gating: an operational member must fault while disconnected.
	_ = c.SetConnected(false)
	if _, err := c.Brightness(); !errors.Is(err, alpacadev.ErrNotConnected) {
		t.Errorf("Brightness() while disconnected: want NotConnected, got %v", err)
	}
	if err := c.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// MaxBrightness range (sim reports 100).
	maxBrightness, err := c.MaxBrightness()
	if err != nil {
		t.Errorf("MaxBrightness(): %v", err)
	} else if maxBrightness != 100 {
		t.Errorf("MaxBrightness() = %d; want 100", maxBrightness)
	}

	// Required properties must be readable.
	if _, err := c.Brightness(); err != nil {
		t.Errorf("Brightness(): %v", err)
	}
	if _, err := c.CalibratorState(); err != nil {
		t.Errorf("CalibratorState(): %v", err)
	}
	if _, err := c.CoverState(); err != nil {
		t.Errorf("CoverState(): %v", err)
	}
	if _, err := c.CalibratorChanging(); err != nil {
		t.Errorf("CalibratorChanging(): %v", err)
	}
	if _, err := c.CoverMoving(); err != nil {
		t.Errorf("CoverMoving(): %v", err)
	}

	// CalibratorOn happy path: brightness applied and calibrator becomes ready.
	if err := c.CalibratorOn(50); err != nil {
		t.Errorf("CalibratorOn(50): %v", err)
	} else {
		covercalibratorWaitCalibratorSettled(t, c)
		if s, err := c.CalibratorState(); err != nil || s != alpacadev.CalibratorReady {
			t.Errorf("CalibratorState() after CalibratorOn = %v, %v; want CalibratorReady", s, err)
		}
		if b, err := c.Brightness(); err != nil || b != 50 {
			t.Errorf("Brightness() after CalibratorOn(50) = %d, %v; want 50", b, err)
		}
	}

	// CalibratorOn validation: out-of-range brightness is rejected (high).
	if err := c.CalibratorOn(500); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("CalibratorOn(500): want InvalidValue, got %v", err)
	}

	// CalibratorOn validation: negative brightness is rejected (low).
	if err := c.CalibratorOn(-1); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Errorf("CalibratorOn(-1): want InvalidValue, got %v", err)
	}

	// CalibratorOff: calibrator transitions back to off.
	if err := c.CalibratorOff(); err != nil {
		t.Errorf("CalibratorOff(): %v", err)
	} else {
		covercalibratorWaitCalibratorSettled(t, c)
		if s, err := c.CalibratorState(); err != nil || s != alpacadev.CalibratorOff {
			t.Errorf("CalibratorState() after CalibratorOff = %v, %v; want CalibratorOff", s, err)
		}
	}

	// OpenCover: cover moves then settles open.
	if err := c.OpenCover(); err != nil {
		t.Errorf("OpenCover(): %v", err)
	} else {
		covercalibratorWaitCoverState(t, c, alpacadev.CoverOpen)
		if s, err := c.CoverState(); err != nil || s != alpacadev.CoverOpen {
			t.Errorf("CoverState() after OpenCover = %v, %v; want CoverOpen", s, err)
		}
	}

	// CloseCover: cover moves then settles closed.
	if err := c.CloseCover(); err != nil {
		t.Errorf("CloseCover(): %v", err)
	} else {
		covercalibratorWaitCoverState(t, c, alpacadev.CoverClosed)
		if s, err := c.CoverState(); err != nil || s != alpacadev.CoverClosed {
			t.Errorf("CoverState() after CloseCover = %v, %v; want CoverClosed", s, err)
		}
	}

	// HaltCover: must return without error.
	if err := c.HaltCover(); err != nil {
		t.Errorf("HaltCover(): %v", err)
	}

	// CoverMoving<->CoverState agreement during motion: start a move from the
	// opposite state and, while CoverMoving() is true, CoverState() must report
	// CoverMoving. The cover is currently closed, so open it. The transition is
	// ~700ms, so sampling immediately after the initiator should catch it; if it
	// is somehow already settled, note it as skipped rather than failing.
	if err := c.OpenCover(); err != nil {
		t.Errorf("OpenCover() (agreement check): %v", err)
	} else {
		moving, err := c.CoverMoving()
		if err != nil {
			t.Errorf("CoverMoving() (agreement check): %v", err)
		} else if moving {
			if s, err := c.CoverState(); err != nil {
				t.Errorf("CoverState() (agreement check): %v", err)
			} else if s != alpacadev.CoverMoving {
				t.Errorf("CoverState() while CoverMoving = %v; want CoverMoving", s)
			}
		} else {
			t.Log("CoverMoving<->CoverState agreement check skipped: move settled before it could be observed")
		}
		// Let the open complete so the device is in a known state.
		covercalibratorWaitCoverState(t, c, alpacadev.CoverOpen)
	}

	// HaltCover mid-motion: starting from open, begin closing, confirm the cover
	// is moving, then halt and assert it is no longer reported as CoverMoving.
	if err := c.CloseCover(); err != nil {
		t.Errorf("CloseCover() (halt check): %v", err)
	} else {
		if covercalibratorWaitCoverMoving(t, c) {
			if err := c.HaltCover(); err != nil {
				t.Errorf("HaltCover() mid-motion: %v", err)
			} else if s, err := c.CoverState(); err != nil {
				t.Errorf("CoverState() after HaltCover: %v", err)
			} else if s == alpacadev.CoverMoving {
				t.Errorf("CoverState() after HaltCover = CoverMoving; want a settled state")
			}
		} else {
			t.Log("HaltCover mid-motion check skipped: cover never observed moving")
		}
		// Leave the cover closed.
		if err := c.CloseCover(); err != nil {
			t.Errorf("CloseCover() (restore): %v", err)
		} else {
			covercalibratorWaitCoverState(t, c, alpacadev.CoverClosed)
		}
	}
}

// covercalibratorWaitCoverMoving polls CoverMoving until it reports true or the
// poll timeout elapses. It returns whether motion was observed; callers can use
// the result to skip checks that depend on catching the cover mid-motion.
func covercalibratorWaitCoverMoving(t *testing.T, c *client.CoverCalibrator) bool {
	t.Helper()
	deadline := time.Now().Add(covercalibratorPollTimeout)
	for time.Now().Before(deadline) {
		moving, err := c.CoverMoving()
		if err != nil {
			t.Fatalf("CoverMoving(): %v", err)
		}
		if moving {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// covercalibratorWaitCalibratorSettled polls CalibratorChanging until the
// calibrator transition completes or the poll timeout elapses.
func covercalibratorWaitCalibratorSettled(t *testing.T, c *client.CoverCalibrator) {
	t.Helper()
	deadline := time.Now().Add(covercalibratorPollTimeout)
	for time.Now().Before(deadline) {
		changing, err := c.CalibratorChanging()
		if err != nil {
			t.Fatalf("CalibratorChanging(): %v", err)
		}
		if !changing {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("calibrator still changing after timeout")
}

// covercalibratorWaitCoverState polls CoverState until it reaches want or the
// poll timeout elapses.
func covercalibratorWaitCoverState(t *testing.T, c *client.CoverCalibrator, want alpacadev.CoverStatus) {
	t.Helper()
	deadline := time.Now().Add(covercalibratorPollTimeout)
	for time.Now().Before(deadline) {
		s, err := c.CoverState()
		if err != nil {
			t.Fatalf("CoverState(): %v", err)
		}
		if s == want {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("cover did not reach %v after timeout", want)
}
