# Changelog

## Unreleased

Pre-release hardening pass ahead of general availability. Breaking changes are
called out; the module is pre-v1, so minor versions may break.

### Added

- `server.Config.StrictParamCasing`: opt-in exact-case parameter-name
  matching (default off). The spec requires parameter names be treated
  case-insensitively (`specs/AlpacaDeviceAPI_v1.yaml`), and the default
  honors that; enable this only to satisfy ConformU's "Check Alpaca Protocol"
  mode, whose "Bad casing" tests expect a differently-cased required
  parameter to be rejected as missing (400) — stricter than the spec text,
  and it will reject real clients that send a legally different casing.
  `cmd/alpacasim -strict-param-casing` exposes it on the sim binary.
- **Library-enforced protocol compliance.** The server now applies the
  device-independent ASCOM rules in its HTTP dispatch layer, before the driver
  method runs: parameter-range validation (→ `InvalidValue`), capability-flag
  gating (→ `NotImplemented`), telescope parked gating (→ `ParkedException`),
  target read-before-set and image-not-ready gating (→ `InvalidOperation`),
  sidereal-only rate offsets, axis-rate and drive-rate validation. Any driver
  that implements the typed interfaces is Alpaca/ConformU-compliant without
  writing this logic — drivers implement only hardware-specific behavior (and
  may add their own stricter hardware limits, which run after these gates).
  `BaseTelescope` now fully implements the target properties (values stored,
  getters return `InvalidOperation` until set), so embedders get the
  read-before-set rule for free. All ten device types pass ASCOM ConformU
  v4.4.0 with zero issues, zero errors. See `server/gate.go` and the
  naive-driver proofs in `server/compliance_gate_test.go`.
- **JSON ImageArray transport, both sides.** The server streams the mandatory
  `{Type, Rank, Value}` form to clients that don't negotiate ImageBytes; the
  client decodes it as a fallback from servers that ignore the negotiation.
- New leaf package **`alpaca/`**: the wire vocabulary (device types, enums,
  error model, ImageBytes codec) shared by server and client. The server
  re-exports everything under its original names — existing code compiles
  unchanged — and the client no longer links the server package.
- `client.DiscoverContext` and `DiscoveredServer.ConfiguredDevicesContext`:
  cancellable discovery. Godoc examples for the initiate-then-poll
  cancellation pattern and for hosting a server.
- `server.Config.Timeouts`: configurable HTTP I/O bounds (defaults
  10s/30s/5m/2m; negative disables).
- `server.ConnectErrorReporter`: a failed Platform 7 async Connect/Disconnect
  is now reported in-band when `connecting` is polled (BaseDevice implements
  it via ConnectOp).
- Driver `DeviceState()` overrides are merged into the library-built state
  (same-name overrides, new names append) — Switch drivers can publish
  per-switch state.
- `server.Server.Port()`: the actual bound port (usable with `AlpacaPort: 0`);
  discovery advertises it.
- `server.Op.TryBegin()`: atomic check-and-start for initiators.
- `conformance.SettleTimeout`: settle waits are configurable for real
  hardware (default 6s suits the sims).
- CI (build/vet/test/race on ubuntu+macos, windows and linux/arm64
  cross-compile, gofmt gate).

### Changed (breaking for pre-release users)

- Module requires **Go 1.23**.
- Package `alpacadev` is renamed **`server`**, matching its import path
  (aliased imports are unaffected). Log prefixes are now `goalpaca:`.
- `client.Device.DeviceType()` returns `alpaca.DeviceType` (was `string`).
- `server.Telescope.TargetRightAscension`/`TargetDeclination` return
  `(float64, error)` so drivers can express the ASCOM read-before-set rule.
- `server.BaseDevice.DeviceState()` default is now nil (contribute-nothing);
  it was `[{Connected: …}]`, which only applied to unknown device types.
- The HTTP server sets read/write/idle timeouts by default (see
  `Config.Timeouts`); previously unbounded.
- `client.WithTimeout` no longer replaces a client supplied via
  `WithHTTPClient`; the options compose in either order. The default image
  download is no longer subject to the 30s envelope timeout (bounded by
  connect/response-header limits and `ImageArrayCtx`).
- The client rejects success responses with a missing/null `Value` instead of
  silently decoding Go zero values.
- `server.Register` validates that the device implements the typed interface
  for its DeviceType.
- Management endpoints are GET-only per spec.

### Fixed

- Busy gate: `abortslew`, `haltcover`, `calibratoroff`, `cancelasync` are
  interrupts and now bypass it — a busy mount/cover/switch can always be
  stopped.
- Windows discovery: SO_BROADCAST is set, so broadcast probes work.
- Discovery client: 64 KB read buffer (1 KB truncated oversized replies);
  `ConfiguredDevices` gained a timeout and status check; all client reads are
  capped (8 MB envelope / 2 GiB image).
- Server: listener-error goroutine leak; discovery responder busy-spin;
  library logging routed through `Config.Logger`.
- Dome `Slewing` now reports true while the shutter is opening or closing;
  IDomeV3 requires `Slewing` during any dome motion, including the shutter (it
  previously reflected only azimuth/altitude motion).
- Simulators (ConformU alignment): the spec-fixed validation that these fixes
  first landed in the sims (parked gating, coordinate/rate ranges, sensor-name
  checks, `LastExposureStartTime` in UTC, cooler set-point sanity, subframe
  geometry) now lives in the `server` library — the sims retain only the
  hardware behavior the library can't derive: MoveAxis drives real motion with
  `Slewing` true, camera `StopExposure` keeps the partial image /
  `AbortExposure` discards it, frame geometry snapshots at `StartExposure`,
  `SetSwitchName` round-trips, `HaltCover` mid-travel reports `Unknown`,
  rotator `Reverse()` race.
- Conformance harness: the ImageArray-error check asserted the wrong code
  (would fail spec-correct devices); coverage added for parked gating,
  mid-exposure Stop/Abort, the filter-wheel moving sentinel, and
  SafetyMonitor DeviceState.

## v0.1.1

Initial public tag.
