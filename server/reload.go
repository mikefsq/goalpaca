package server

import (
	"context"
	"errors"
	"fmt"
)

// A Reloader rebuilds a device from its configuration: the construction the
// host did at startup, run again. It re-reads whatever the host reads (a
// device file, flags, a state overlay), constructs the device without touching
// hardware, and returns it with the Configurable the host attaches for its
// setup form (nil when the device has its own or none). Reload does the rest.
//
// A host registers one with SetReloader; a device without one cannot be
// reloaded in place and needs a restart from its supervisor.
type Reloader func(ctx context.Context) (Device, Configurable, error)

// ErrNotReloadable is returned by Reload for a device with no Reloader.
var ErrNotReloadable = errors.New("goalpaca: this device has no reloader; restart the process to reload it")

// SetReloader gives a registered device a Reloader. Call before or after Run.
func (s *Server) SetReloader(devType DeviceType, number int, r Reloader) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rd, ok := s.devices[regKey{devType, number}]
	if !ok {
		return fmt.Errorf("goalpaca: %s device %d is not registered", devType, number)
	}
	rd.reloader = r
	return nil
}

// Reload rebuilds one device in place: it runs the device's Reloader, and if
// that succeeds closes the old device's hardware, swaps the new device in
// under the same type and number, applies its persisted settings, and opens
// its hardware. The HTTP listener, the port, and discovery are untouched, so a
// client keeps the same address and a restart is needed only for a port
// change.
//
// A Reloader failure leaves the old device serving with its hardware open,
// since the new one was never constructed; a hardware Open failure leaves the
// new device serving with its hardware closed, as at startup. Reloads of one
// device are serialized; requests to it during the swap see one device or the
// other, whole.
func (s *Server) Reload(ctx context.Context, devType DeviceType, number int) error {
	rd, ok := s.lookupRegistered(devType, number)
	if !ok {
		return fmt.Errorf("goalpaca: %s device %d is not registered", devType, number)
	}
	s.mu.RLock()
	reload := rd.reloader
	s.mu.RUnlock()
	if reload == nil {
		return ErrNotReloadable
	}
	rd.reloadMu.Lock()
	defer rd.reloadMu.Unlock()

	dev, cfg, err := reload(ctx)
	if err != nil {
		return fmt.Errorf("goalpaca: reload %s %d: %w", devType, number, err)
	}
	if dev == nil {
		return fmt.Errorf("goalpaca: reload %s %d: reloader returned no device", devType, number)
	}
	if !implementsType(devType, dev) {
		return fmt.Errorf("goalpaca: reload %s %d: device %T does not implement the %s interface", devType, number, dev, devType)
	}

	// Old hardware first, so a device that owns a USB handle releases it
	// before its replacement opens the same one.
	s.closeHardwareFor(ctx, rd)
	s.mu.Lock()
	rd.dev = dev
	rd.cfg = cfg
	s.mu.Unlock()
	s.loadSettingsFor(rd)
	if err := s.openHardwareFor(ctx, rd); err != nil {
		return fmt.Errorf("goalpaca: reload %s %d: hardware open: %w", devType, number, err)
	}
	s.logf("goalpaca: %s %d reloaded", devType, number)
	return nil
}

// ReloadAll reloads every device that has a Reloader, in registration order,
// and returns the errors joined; devices without one are skipped.
func (s *Server) ReloadAll(ctx context.Context) error {
	s.mu.RLock()
	order := append([]*registeredDevice(nil), s.order...)
	s.mu.RUnlock()
	var errs []error
	for _, rd := range order {
		if !s.reloadable(rd) {
			continue
		}
		if err := s.Reload(ctx, rd.typ, rd.num); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// reloadable reports whether rd has a Reloader.
func (s *Server) reloadable(rd *registeredDevice) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return rd.reloader != nil
}
