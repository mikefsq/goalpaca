# goalpaca

A Go framework for the [ASCOM Alpaca](https://ascom-standards.org/) astronomy
device protocol (HTTP/JSON REST + UDP discovery): a device-hosting **server**
library, a typed **client** library, a full set of device **simulators**, and a
ConformU-derived **conformance** test harness.

Module path: `github.com/mikefsq/goalpaca` · pure Go (standard library only).

## Scope & interoperability

goalpaca is a **Go-native** Alpaca implementation. Its design goals:

- **Track the ASCOM standard.** The OpenAPI specs vendored in `specs/` are the
  source of truth; the conformance harness is how the implementation stays in
  step as the standard evolves (update the spec → re-run conformance). goalpaca
  follows the ASCOM committee's decisions rather than inventing protocol.
- **Interoperate at the wire level.** The only contract with the rest of the
  ecosystem is the Alpaca protocol itself (HTTP/JSON REST + UDP discovery). A
  goalpaca server is discoverable and usable by any conformant client (NINA,
  Ekos, …); the goalpaca client can drive any conformant device or server (a
  real driver, the .NET OmniSimulator) — with no shared code on either side.
- **Pure Go, Unix-first.** Standard library only; runs on Linux (incl. Raspberry
  Pi) and macOS. It does **not** target the Windows ASCOM Platform / COM, and is
  not part of the Python (Alpyca) ecosystem — interop with those happens purely
  over the wire, never through a shared runtime.

## Packages

| Path | Purpose |
|------|---------|
| `server/` | Host one or more hardware devices as a standalone Alpaca server. Authors implement a typed per-type interface (`Camera`, `Focuser`, …) plus an optional `Hardware` lifecycle by embedding a `Base<Type>`; the library handles the wire protocol, discovery, Platform 7 async, image transport, connection + busy write-gating, and the management API. Package name: `alpacadev`. See [docs/server_spec.md](docs/server_spec.md). |
| `client/` | Typed client for talking to Alpaca devices over HTTP — one client per device type, plus dual-stack (IPv4 + IPv6) discovery. See [docs/client_spec.md](docs/client_spec.md). |
| `sim/` | Simulator implementations of all ten device types (modeled on the official ASCOM simulators) for testing with no hardware. |
| `conformance/` | ConformU-derived conformance checks that drive the client against a device (a sim or a real server). See [docs/conformU_testing.md](docs/conformU_testing.md). |
| `cmd/alpacasim/` | Serves all ten simulated devices behind one Alpaca port — a ConformU target and a dev server. |
| `cmd/alpacadiscover/` | CLI that runs discovery and prints the servers found and each one's configured devices. |
| `cmd/discover_proxy/` | Optional, non-standard discovery proxy: answers Alpaca UDP discovery on behalf of drivers that register via unicast heartbeat. See [docs/discovery_proxy_spec.md](docs/discovery_proxy_spec.md). |
| `specs/` | Vendored upstream ASCOM Alpaca OpenAPI specs (MIT, © ASCOM Initiative) — the conformance reference. |

## Quick start

```sh
# Serve all ten simulated devices (per-request logging is on by default).
go run ./cmd/alpacasim                 # :11111, discovery=direct
#   -port N       choose the HTTP port
#   -discovery    direct (default, no proxy) | off
#   -ipv6         also answer IPv6 multicast discovery
#   -quiet        disable per-request logging

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
against the simulators; the real ConformU binary against `alpacasim` is the
external arbiter. See [docs/conformU_testing.md](docs/conformU_testing.md).

## Documentation

- [docs/server_spec.md](docs/server_spec.md) — server library reference
- [docs/client_spec.md](docs/client_spec.md) — client library reference
- [docs/conformU_testing.md](docs/conformU_testing.md) — conformance testing strategy
- [docs/discovery_proxy_spec.md](docs/discovery_proxy_spec.md) — discovery proxy spec

## License

[MIT](LICENSE) © 2026 @mikefsq. The vendored ASCOM OpenAPI specs in
`specs/` are MIT © ASCOM Initiative (upstream notices preserved).
