# goalpaca

A Go framework for the [ASCOM Alpaca](https://ascom-standards.org/) astronomy
device protocol (HTTP/JSON REST + UDP discovery): a device-hosting **server**
library, a typed **client** library, a full set of device **simulators**, a
driver **registry** for composed hosts, and a ConformU-derived **conformance**
test harness.

Module path: `github.com/mikefsq/goalpaca` · pure Go (standard library only).

## Scope & interoperability

goalpaca is a **Go-native** Alpaca implementation. Its design goals:

- **Track the ASCOM standard.** The upstream OpenAPI specs are vendored in
  `specs/` as the reference the implementation is written against, and the
  conformance harness (a hand-port of ConformU's checks) keeps it honest as
  the standard evolves. goalpaca follows the ASCOM committee's decisions
  rather than inventing protocol.
- **Interoperate at the wire level.** The only contract with the rest of the
  ecosystem is the Alpaca protocol itself (HTTP/JSON REST + UDP discovery). A
  goalpaca server is discoverable and usable by any conformant client (NINA,
  PHD2, …); the goalpaca client can drive any conformant device or server (a
  real driver, the .NET OmniSimulator) — with no shared code on either side.
- **Pure Go.** Standard library only; runs on Linux (incl. Raspberry Pi),
  macOS, and Windows. It does **not** target the Windows ASCOM Platform / COM,
  and is not part of the Python (Alpyca) ecosystem — interop with those happens
  purely over the wire, never through a shared runtime.

## Packages

| Path | Purpose |
|------|---------|
| `alpaca/` | The wire vocabulary both sides share: device-type names, per-type enums, the error model, and the ImageBytes codec. The server re-exports it all under its original names, so it is mostly an implementation detail — import it directly only when writing code that is neither server nor client. |
| `server/` | Host one or more hardware devices as a standalone Alpaca server. Authors implement a typed per-type interface (`Camera`, `Focuser`, …) plus an optional `Hardware` lifecycle by embedding a `Base<Type>`; the library handles the wire protocol, discovery, Platform 7 async, image transport, connection + busy write-gating, the management API, and **ASCOM protocol compliance** — parameter-range validation, capability gating, parked/target/image-ready rules — so implementing the interface yields a conformant device. Drivers write only hardware-specific behavior. |
| `client/` | Typed client for talking to Alpaca devices over HTTP — one client per device type, plus dual-stack (IPv4 + IPv6) discovery. |
| `registry/` | Process-wide driver catalogue for composed hosts ([alpacahurd](https://github.com/mikefsq/alpacahurd)): a driver package registers a name, ASCOM type, config example, and factory from `init()`, and decodes its own config entry strictly via `Spec.Decode` — so a host constructs devices from a config file with no compile-time knowledge of each driver. |
| `sim/` | Simulator implementations of all ten device types (modeled on the official ASCOM simulators) for testing with no hardware. |
| `conformance/` | ConformU-derived conformance checks that drive the client against a device (a sim or a real server). |
| `cmd/alpacasim/` | Serves all ten simulated devices behind one Alpaca port — a ConformU target and a dev server. |
| `cmd/alpacadiscover/` | CLI that runs discovery and prints the servers found and each one's configured devices. |
| `cmd/discover_proxy/` | Optional, non-standard discovery proxy: answers Alpaca UDP discovery on behalf of drivers that register via unicast heartbeat. |
| `cmd/fault_proxy/` | Fault-injecting reverse proxy for testing Alpaca *clients*: forwards to an upstream device unchanged until a fault (device errors, corrupt ImageBytes frames, degraded-network behavior, …) is armed over its control channel. See [its README](cmd/fault_proxy/README.md). |
| `specs/` | Vendored upstream ASCOM Alpaca OpenAPI specs (MIT, © ASCOM Initiative) — the reference the implementation is written against. |

## Quick start

```sh
# Serve all ten simulated devices (per-request logging is on by default).
go run ./cmd/alpacasim                 # :11111, discovery=direct
#   -port N       choose the HTTP port
#   -discovery    direct (default, no proxy) | off
#   -ipv6         also answer IPv6 multicast discovery
#   -quiet        disable per-request logging
#   -strict-param-casing
#                 ConformU protocol-mode only; rejects differently-cased
#                 parameter names (differs from the Swagger API spec)

# Discover them from another terminal.
go run ./cmd/alpacadiscover            # -timeout sets the listen window
```

In Go:

```go
cam := client.NewCamera("127.0.0.1:11111", 0)
if err := cam.SetConnected(true); err != nil { /* … */ }
defer cam.SetConnected(false)
x, _ := cam.CameraXSize()
```

## Testing

```sh
go test ./...          # server protocol + client + sims + conformance
go test -race ./...
```

The conformance layer ports ConformU's checks and runs them through the client
against the simulators; the real
[ConformU](https://github.com/ASCOMInitiative/ConformU) binary against
`alpacasim` is the external arbiter. All ten device types pass ConformU v4.4.0
with zero issues — and because the compliance rules are enforced by the
`server` library rather than the simulators, that guarantee extends to any
driver built on it.

## Documentation

API documentation lives in the godoc:

- [server](https://pkg.go.dev/github.com/mikefsq/goalpaca/server) — device-hosting server library
- [client](https://pkg.go.dev/github.com/mikefsq/goalpaca/client) — typed Alpaca client + discovery
- [registry](https://pkg.go.dev/github.com/mikefsq/goalpaca/registry) — driver catalogue for composed hosts
- [sim](https://pkg.go.dev/github.com/mikefsq/goalpaca/sim) — the ten device simulators
- [conformance](https://pkg.go.dev/github.com/mikefsq/goalpaca/conformance) — conformance checks

Protocol references: the vendored OpenAPI specs in [`specs/`](specs/) and the
[ASCOM Alpaca API Reference](https://ascom-standards.org/AlpacaDeveloper/ASCOMAlpacaAPIReference.html).

## License

[MIT](LICENSE) © 2026 @mikefsq. The vendored ASCOM OpenAPI specs in
`specs/` are MIT © ASCOM Initiative (upstream notices preserved).
