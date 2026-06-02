package sim

import (
	"errors"
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mikefsq/goalpaca/client"
	alpacadev "github.com/mikefsq/goalpaca/server"
)

// rotatorClient hosts a sim Rotator on the real server (over httptest) and
// returns a connected client pointed at it — exercising sim + server + client.
func rotatorClient(t *testing.T, opts ...RotatorOption) *client.Rotator {
	t.Helper()
	srv := alpacadev.New(alpacadev.Config{Discovery: alpacadev.DiscoveryConfig{Mode: alpacadev.DiscoveryOff}})
	if err := srv.Register(alpacadev.RotatorType, 0, NewRotator(opts...)); err != nil {
		t.Fatalf("register: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	rc := client.NewRotator(ts.URL, 0)
	if err := rc.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return rc
}

func waitNotMoving(t *testing.T, rc *client.Rotator) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		moving, err := rc.IsMoving()
		if err != nil {
			t.Fatalf("IsMoving: %v", err)
		}
		if !moving {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("rotator never stopped moving")
}

func TestRotatorSimMoveAbsolute(t *testing.T) {
	rc := rotatorClient(t, WithRotationRate(180)) // 90° in 0.5s

	if cr, err := rc.CanReverse(); err != nil || !cr {
		t.Fatalf("CanReverse = %v, %v; want true", cr, err)
	}
	if err := rc.MoveAbsolute(90); err != nil {
		t.Fatalf("MoveAbsolute: %v", err)
	}
	if moving, err := rc.IsMoving(); err != nil || !moving {
		t.Fatalf("IsMoving right after move = %v, %v; want true", moving, err)
	}
	if tp, err := rc.TargetPosition(); err != nil || math.Abs(tp-90) > 0.01 {
		t.Fatalf("TargetPosition = %v, %v; want 90", tp, err)
	}
	waitNotMoving(t, rc)
	if pos, err := rc.Position(); err != nil || math.Abs(pos-90) > 0.5 {
		t.Fatalf("Position after move = %v, %v; want ~90", pos, err)
	}
}

func TestRotatorSimValidation(t *testing.T) {
	rc := rotatorClient(t)
	if err := rc.MoveAbsolute(400); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Fatalf("MoveAbsolute(400): want InvalidValue, got %v", err)
	}
	if err := rc.Move(400); !errors.Is(err, alpacadev.ErrInvalidValue) {
		t.Fatalf("Move(400): want InvalidValue, got %v", err)
	}
}

func TestRotatorSimSync(t *testing.T) {
	rc := rotatorClient(t)
	// At mechanical 0; sync so the current position reads as 100°.
	if err := rc.Sync(100); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if pos, err := rc.Position(); err != nil || math.Abs(pos-100) > 0.5 {
		t.Fatalf("Position after sync = %v, %v; want ~100", pos, err)
	}
	if mech, err := rc.MechanicalPosition(); err != nil || math.Abs(mech) > 0.5 {
		t.Fatalf("MechanicalPosition after sync = %v, %v; want ~0", mech, err)
	}
}

func TestRotatorSimHalt(t *testing.T) {
	rc := rotatorClient(t, WithRotationRate(30)) // slow, so we can halt mid-move
	if err := rc.MoveAbsolute(180); err != nil {
		t.Fatalf("MoveAbsolute: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := rc.Halt(); err != nil {
		t.Fatalf("Halt: %v", err)
	}
	if moving, err := rc.IsMoving(); err != nil || moving {
		t.Fatalf("IsMoving after halt = %v, %v; want false", moving, err)
	}
}
