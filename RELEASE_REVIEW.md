# goalpaca release review & punch list

Full-codebase review ahead of general release, checked against upstream ASCOM
Alpaca (July 2026). Reviewed at `864610c`; all completed work below is in the
working tree, uncommitted. `build` / `vet` / `gofmt` / full test suite /
`-race` / windows cross-compile all green. **All ten device types pass ASCOM
ConformU v4.4.0 against `cmd/alpacasim` with zero issues and zero errors.**

## Upstream standard status

- **Vendored specs are current** — diffed against upstream HEAD
  ([ASCOMRemote/Swagger](https://github.com/ASCOMInitiative/ASCOMRemote/tree/main/Swagger));
  only cosmetic doc-link differences. No re-vendor needed.
- **Alpaca API Reference v12 (Feb 2026)** requirements all met: discovery
  port sharing via SO_REUSEADDR+SO_REUSEPORT (§5.8.2), mandatory `AlpacaPort`
  discovery key, mandatory `Content-Type` on JSON.
- **Platform 7.1.x**: no Alpaca protocol changes. Verified against
  **ConformU v4.4.0** (Z-terminated UTCDate, MoveAxis rate 0.0, ITelescopeV4
  sidereal-only rate offsets, ISwitchV3 async members) — all compliant.
- Track (don't implement): draft ISafetyMonitorV4 `SafetyEvents` Action.

## Completed

- `specs/` staged for vendoring (upstream MIT headers verified); README dead
  `docs/` links replaced with pkg.go.dev links; conformance wording corrected.
- CI workflow (`.github/workflows/ci.yml`): build+vet+test+race on
  ubuntu/macos at Go 1.23 & stable; windows + linux/arm64 cross-compile;
  gofmt gate. `go.mod` bumped to 1.23.
- Busy gate: `haltcover`, `calibratoroff`, `cancelasync` exempted (the
  interrupts that end cover motion / calibrator ramp / Switch async), with
  gating tests.
- HTTP server timeouts, configurable via `Config.Timeouts` (zero = default
  10s/30s/5m/2m, negative = off).
- **JSON ImageArray implemented both sides**: server streams the mandatory
  `{Type,Rank,Value}` form (`server/imagearray.go`, constant-memory); client
  decodes it as fallback when a server ignores the ImageBytes Accept header
  (`client/imagearray.go`). Round-trip tested against ImageBytes.
- Sim ConformU gaps: telescope park gating (movement members return 0x408
  while `AtPark`); camera `StopExposure` keeps the partial image /
  `AbortExposure` discards it; `ImageArray` with no image returns 0x40B;
  frame geometry snapshotted at `StartExposure`. Inverted harness check
  fixed; parked/mid-exposure coverage added.
- Client hardening: split envelope (30s timeout) vs image (ctx +
  connect/response-header bounds only) HTTP clients — large downloads no
  longer aborted; `WithTimeout`/`WithHTTPClient` compose in either order;
  capped reads (8 MB envelope / 2 GiB image); `ConfiguredDevices` timeout +
  status check; 64 KB discovery buffer; Windows discovery SO_BROADCAST fixed.
- Server logging routed through `Config.Logger` (nil → standard logger;
  `io.Discard` logger for silence); listener-error goroutine leak fixed;
  discovery responder and discover_proxy read-error backoff; rotator
  `Reverse()` race fixed.
- **Compliance moved from sims into the `server` library.** The spec-fixed
  validation that first landed in the simulators — parameter ranges,
  capability→`NotImplemented` gating, telescope parked gating, target
  read-before-set, image-not-ready and sidereal-only-rate rules — now runs in
  the HTTP dispatch layer (`server/gate.go` + per-type `*_http.go`), so any
  driver implementing the typed interfaces is conformant without writing it.
  `BaseTelescope` fully implements the target properties. Sims cleaned up to
  keep only hardware behavior/physics. Proven by naive-driver tests
  (`server/compliance_gate_test.go`: zero-validation drivers, asserting the
  library returns the right Alpaca error numbers). Dome `Slewing` fixed to
  cover shutter motion (IDomeV3).

## Remaining: P1 API-shape (decide before any tag; ~zero external users today)

| # | Item | Agreed approach | External impact |
|---|------|-----------------|-----------------|
| 8 | Client context support | **Done** (additive only): `DiscoverContext` (cancels the listen window via read-deadline expiry, both legs), `ConfiguredDevicesContext`, `ImageArrayCtx` kept, godoc examples (`Example_pollWithCancellation`, `ExampleDiscoverContext`) documenting the poll-loop cancellation pattern. No change to the ~200 typed methods — Alpaca is initiate-then-poll by design, so no other call blocks long. Cancellation tests added. | none |
| 9 | `client` publicly imports `server` for shared enums | **Done.** Wire vocabulary (device types, enums, error model, ImageBytes codec) moved to leaf package `alpaca/`; `server` re-exports everything via identity-preserving aliases (`server/aliases.go`) so existing code is unaffected; client now depends only on the leaf (`go list -deps ./client` confirms). | none (aliases) |
| 10 | Async Connect failure unobservable (`Connecting() bool` can't surface `Op.Err()`) | **Done.** `ConnectErrorReporter` optional interface (`ConnectError() error`); `BaseDevice` implements it via `ConnectOp().Err()`, so every embedder gets it free; GET `connecting` reports a failed async connect in-band once the op completes; cleared by the next `Begin`/`Reset`. Test: `TestAsyncConnectFailureSurfaced`. | none (additive) |
| 11 | `Device.DeviceState()` ignored for all 10 standard types | **Done.** Driver `DeviceState()` merged onto the library-built set (same-name overrides, new names append); `BaseDevice` default changed to nil so the merge is opt-in and wire output is unchanged by default; library TimeStamp supplied unless the driver provides one. Test: `TestDeviceStateMerge`. | none |
| 12 | Package `alpacadev` in directory `server/` | **Done.** Package renamed to `server` to match the import path; every internal importer already aliased the import, so only package declarations and doc/log strings changed (log/error prefixes now `goalpaca:`); stale aliases in sim/conformance/cmd/client-tests dropped in favour of the real package name. | breaks only unaliased importers (none known) |
| 13 | `Device.DeviceType()` returns `string` not the typed enum | **Done.** Now returns `alpaca.DeviceType`; doc comments added to the target-inspection getters. | compile break for callers binding to `string` (none known) |

## P2 polish — done

- Sim semantics: all resolved, and the ones that were protocol rules (not
  physics) were subsequently lifted into the `server` library so every driver
  inherits them — `MoveAxis` drives motion with `Slewing` true; `SetSwitchName`
  round-trips; `HaltCover` mid-travel reports `Unknown`; target read-before-set
  (now `BaseTelescope`); sensor-name validation (now the library).
- Harness: `conformance.SettleTimeout` parameterizes settle waits; FilterWheel
  moving sentinel asserted; SafetyMonitor DeviceState checked; added coverage
  for parked gating, mid-exposure Stop/Abort, camera set-point / UTC
  start-time, and Dome shutter `Slewing`.
- Server: `Register` type-checks the device against its `DeviceType`;
  management endpoints are GET-only; bound port exposed via `Server.Port()`
  and advertised; `Op.TryBegin` added.
- cmd: `alpacasim -discovery` validates its argument; `discover_proxy` flag
  help, `-bind`, and per-interface multicast joins fixed.
- Docs/packaging: `Example*` functions added (client + server); sim package
  comment moved to `sim/doc.go`; CHANGELOG maintained; leaf `alpaca/` package
  documented.

## Remaining (optional, non-blocking)

- `OpticsStore`/`Supervise` export scope — judgment call, left as-is pending a
  decision.

## Settled: param-case strictness vs ConformU "Check Alpaca Protocol" mode

Run empirically: 0 errors, 31 issues, 33 info messages, all in one family —
ConformU's "Bad casing" tests invert every character's case in a parameter
name (`DeclinationRate` → `dECLINATIONrATE`) and expect a 400, i.e. that the
server treats a differently-cased required parameter as missing. That is the
**inverse** of the vendored spec: `specs/AlpacaDeviceAPI_v1.yaml:70` (and the
identical text in the Management API spec) states "Parameter names are not
case sensitive, so clients and drivers should be prepared for parameter names
to be supplied and returned with any casing." `server/transaction.go` matches
parameter names case-insensitively, which is what the spec requires — making
it case-sensitive to silence this ConformU check would mean silently dropping
required parameters from real clients that send different casing, which the
spec explicitly forbids penalizing. ConformU itself classifies these as
advisory *issues*, not errors, consistent with this being a known
stricter-than-spec check. The default stays spec-correct; added
`server.Config.StrictParamCasing` (and `cmd/alpacasim -strict-param-casing`)
as an opt-in for a clean run under ConformU's protocol mode specifically,
without changing real-world behavior for anyone who doesn't ask for it.

## Final gate

1. **Done.** Real **ConformU v4.4.0** (device mode) run against
   `cmd/alpacasim` — all ten device types pass with zero issues, zero errors.
2. Tag a fresh release (current `v0.1.1` ships broken links and no specs).
