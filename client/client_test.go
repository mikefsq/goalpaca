package client

import (
	"errors"
	"net/http/httptest"
	"net/url"
	"testing"

	alpaca "github.com/mikefsq/goalpaca/server"
)

// fakeFocuser is a minimal server-side Focuser for round-trip tests. It embeds
// the library's BaseFocuser and overrides only what the tests touch.
type fakeFocuser struct {
	alpaca.BaseFocuser
	pos int
}

func (f *fakeFocuser) Absolute() bool         { return true }
func (f *fakeFocuser) MaxStep() int           { return 50000 }
func (f *fakeFocuser) Position() (int, error) { return f.pos, nil }
func (f *fakeFocuser) Move(p int) error       { f.pos = p; return nil }

// newServer starts the real goalpaca server (over httptest) hosting a fake
// focuser at device 0, so the client exercises the genuine wire protocol.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := alpaca.New(alpaca.Config{Discovery: alpaca.DiscoveryConfig{Mode: alpaca.DiscoveryOff}})
	foc := &fakeFocuser{}
	foc.DevName = "FakeFocuser"
	foc.IfaceVer = 4
	if err := srv.Register(alpaca.FocuserType, 0, foc); err != nil {
		t.Fatalf("register: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

func TestFocuserRoundTrip(t *testing.T) {
	ts := newServer(t)
	f := NewFocuser(ts.URL, 0)

	// Identity members work without being connected.
	if name, err := f.Name(); err != nil || name != "FakeFocuser" {
		t.Fatalf("Name() = %q, %v; want FakeFocuser", name, err)
	}

	// Operational members are gated until connected.
	if _, err := f.Position(); !errors.Is(err, alpaca.ErrNotConnected) {
		t.Fatalf("Position() before connect: want NotConnected, got %v", err)
	}

	// Connect, then operate.
	if err := f.SetConnected(true); err != nil {
		t.Fatalf("SetConnected(true): %v", err)
	}
	if c, err := f.Connected(); err != nil || !c {
		t.Fatalf("Connected() = %v, %v; want true", c, err)
	}
	if abs, err := f.Absolute(); err != nil || !abs {
		t.Fatalf("Absolute() = %v, %v; want true", abs, err)
	}
	if err := f.Move(1234); err != nil {
		t.Fatalf("Move(1234): %v", err)
	}
	if pos, err := f.Position(); err != nil || pos != 1234 {
		t.Fatalf("Position() = %d, %v; want 1234", pos, err)
	}
}

// A device fault maps to *alpaca.AlpacaError and matches by number via errors.Is.
func TestErrorMapping(t *testing.T) {
	ts := newServer(t)
	f := NewFocuser(ts.URL, 0)
	if err := f.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}
	// StepSize is a BaseFocuser default -> NotImplemented, carried in-band.
	_, err := f.StepSize()
	if !errors.Is(err, alpaca.ErrNotImplemented) {
		t.Fatalf("StepSize(): want NotImplemented, got %v", err)
	}
	var ae *alpaca.AlpacaError
	if !errors.As(err, &ae) || ae.Number != alpaca.ErrNumNotImplemented {
		t.Fatalf("StepSize(): want AlpacaError %#x, got %v", alpaca.ErrNumNotImplemented, err)
	}
}

// A malformed parameter is rejected at the HTTP level -> *RequestError (400).
func TestBadParameterIsRequestError(t *testing.T) {
	ts := newServer(t)
	f := NewFocuser(ts.URL, 0)
	if err := f.SetConnected(true); err != nil {
		t.Fatalf("connect: %v", err)
	}
	err := f.put("move", url.Values{"Position": {"not-a-number"}})
	var re *RequestError
	if !errors.As(err, &re) || re.Status != 400 {
		t.Fatalf("move bad Position: want RequestError 400, got %v", err)
	}
}
