package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// hwCamera is a fakeCamera that owns hardware, counting Open and Close.
type hwCamera struct {
	*fakeCamera
	opens, closes  int
	failOpen       bool
	openCtx        context.Context // the ctx Open received
	ctxLiveAtOpen  bool
	ctxDoneAtClose bool // Open's ctx was already cancelled when Close ran
}

func (h *hwCamera) Open(ctx context.Context) error {
	if h.failOpen {
		return errors.New("no camera")
	}
	h.opens++
	h.openCtx = ctx
	h.ctxLiveAtOpen = ctx.Err() == nil
	return nil
}
func (h *hwCamera) Close(context.Context) error {
	h.closes++
	h.ctxDoneAtClose = h.openCtx != nil && h.openCtx.Err() != nil
	return nil
}

func newHWCamera(name string) *hwCamera {
	c := &hwCamera{fakeCamera: newFakeCamera()}
	c.DevName = name
	c.MarkConnected()
	return c
}

// runServer starts s and waits for its port.
func runServer(t *testing.T, s *Server) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = s.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for s.Port() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.Port() == 0 {
		t.Fatal("server did not bind")
	}
}

func TestReloadSwapsDeviceAndHardware(t *testing.T) {
	first := newHWCamera("first")
	second := newHWCamera("second")
	s := New(Config{AlpacaPort: 0, Discovery: DiscoveryConfig{Mode: DiscoveryOff}, ServerName: "reload"})
	if err := s.Register(CameraType, 0, first); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(context.Background(), CameraType, 0); !errors.Is(err, ErrNotReloadable) {
		t.Fatalf("reload without a reloader: %v", err)
	}
	if err := s.SetReloader(CameraType, 0, func(context.Context) (Device, Configurable, error) {
		return second, nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	runServer(t, s)
	if first.opens != 1 {
		t.Fatalf("first opens %d before reload", first.opens)
	}
	port := s.Port()

	// Reload from a short-lived context, as an HTTP request's is: the new
	// device's Open context outlives it, and the old device's was cancelled
	// before its Close ran.
	reqCtx, reqCancel := context.WithCancel(context.Background())
	if err := s.Reload(reqCtx, CameraType, 0); err != nil {
		t.Fatalf("reload: %v", err)
	}
	reqCancel()
	if first.closes != 1 || second.opens != 1 {
		t.Fatalf("first closes %d, second opens %d", first.closes, second.opens)
	}
	if !first.ctxDoneAtClose {
		t.Fatal("old device's Open context was still live at Close; its loop could re-acquire")
	}
	if !second.ctxLiveAtOpen || second.openCtx.Err() != nil {
		t.Fatal("new device's Open context ended with the reload request")
	}
	if s.Port() != port {
		t.Fatalf("port changed %d -> %d", port, s.Port())
	}
	dev, _ := s.lookup(CameraType, 0)
	if dev != Device(second) {
		t.Fatalf("serving %T %q, want second", dev, dev.Name())
	}
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/management/v1/configureddevices", port))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `"DeviceName":"second"`) {
		t.Fatalf("configureddevices after reload: %s", body)
	}
}

func TestReloadFailureLeavesOldDevice(t *testing.T) {
	first := newHWCamera("first")
	s := New(Config{AlpacaPort: 0, Discovery: DiscoveryConfig{Mode: DiscoveryOff}, ServerName: "reload"})
	if err := s.Register(CameraType, 0, first); err != nil {
		t.Fatal(err)
	}
	fail := true
	broken := newHWCamera("broken")
	broken.failOpen = true
	_ = s.SetReloader(CameraType, 0, func(context.Context) (Device, Configurable, error) {
		if fail {
			return nil, nil, errors.New("bad config")
		}
		return broken, nil, nil
	})
	runServer(t, s)

	if err := s.Reload(context.Background(), CameraType, 0); err == nil || !strings.Contains(err.Error(), "bad config") {
		t.Fatalf("reload with a failing reloader: %v", err)
	}
	if first.closes != 0 {
		t.Fatalf("first closed %d times after a failed rebuild", first.closes)
	}
	if dev, _ := s.lookup(CameraType, 0); dev != Device(first) {
		t.Fatalf("serving %T after a failed rebuild", dev)
	}

	fail = false
	err := s.Reload(context.Background(), CameraType, 0)
	if err == nil || !strings.Contains(err.Error(), "hardware open") {
		t.Fatalf("reload with a failing Open: %v", err)
	}
	if first.closes != 1 {
		t.Fatalf("first closes %d, want 1", first.closes)
	}
	if dev, _ := s.lookup(CameraType, 0); dev != Device(broken) {
		t.Fatalf("serving %T after a failed Open, want broken", dev)
	}
	rd, _ := s.lookupRegistered(CameraType, 0)
	if rd.hw != nil {
		t.Fatal("hardware recorded open after Open failed")
	}
}

func TestReloadFromSetupPage(t *testing.T) {
	first := newHWCamera("first")
	second := newHWCamera("second")
	s := New(Config{AlpacaPort: 0, Discovery: DiscoveryConfig{Mode: DiscoveryOff}, ServerName: "reload"})
	_ = s.Register(CameraType, 0, first)
	runServer(t, s)
	page := fmt.Sprintf("http://127.0.0.1:%d/setup/v1/camera/0/setup", s.Port())

	get := func() string {
		resp, err := http.Get(page)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return string(b)
	}
	if strings.Contains(get(), `value="reload"`) {
		t.Fatal("page offers reload with no reloader")
	}
	_ = s.SetReloader(CameraType, 0, func(context.Context) (Device, Configurable, error) { return second, nil, nil })
	if !strings.Contains(get(), `value="reload"`) {
		t.Fatal("page does not offer reload with a reloader")
	}
	resp, err := http.PostForm(page, url.Values{"_form": {"reload"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "Reloaded") || !strings.Contains(string(b), "second") {
		t.Fatalf("reload page: %s", b)
	}
	if first.closes != 1 || second.opens != 1 {
		t.Fatalf("first closes %d, second opens %d", first.closes, second.opens)
	}
	if err := s.ReloadAll(context.Background()); err != nil {
		t.Fatalf("ReloadAll: %v", err)
	}
	if second.closes != 1 || second.opens != 2 {
		t.Fatalf("after ReloadAll: second closes %d opens %d", second.closes, second.opens)
	}
}

func TestLiveRegisterAndUnregister(t *testing.T) {
	first := newHWCamera("first")
	s := New(Config{AlpacaPort: 0, Discovery: DiscoveryConfig{Mode: DiscoveryOff}, ServerName: "live"})
	_ = s.Register(CameraType, 0, first)
	runServer(t, s)

	second := newHWCamera("second")
	if err := s.Register(CameraType, 1, second); err != nil {
		t.Fatal(err)
	}
	if second.opens != 1 || !second.ctxLiveAtOpen {
		t.Fatalf("live Register: opens %d live %v", second.opens, second.ctxLiveAtOpen)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/management/v1/configureddevices", s.Port())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `"second"`) {
		t.Fatalf("configureddevices without the live device: %s", body)
	}

	if err := s.Unregister(CameraType, 1); err != nil {
		t.Fatal(err)
	}
	if second.closes != 1 || !second.ctxDoneAtClose {
		t.Fatalf("Unregister: closes %d ctx done %v", second.closes, second.ctxDoneAtClose)
	}
	resp, _ = http.Get(url)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(body), `"second"`) {
		t.Fatalf("configureddevices still lists the unregistered device: %s", body)
	}
	if err := s.Register(CameraType, 1, newHWCamera("third")); err != nil {
		t.Fatalf("number not freed: %v", err)
	}
	if err := s.Unregister(CameraType, 7); err == nil {
		t.Fatal("unregistering an unknown device succeeded")
	}
}
