# goalpaca

A Go framework for the [ASCOM Alpaca](https://ascom-standards.org/) astronomy
device protocol (HTTP/JSON REST + UDP discovery): a device-hosting **server**
library, a typed **client** library, a full set of device **simulators**, a
driver **registry** for composed hosts, and a ConformU-derived **conformance**
test harness.

Module path: `github.com/mikefsq/goalpaca` · Go (standard library only).

## Scope & interoperability

goalpaca is a **Go-native** Alpaca implementation. Its design goals:

- **Track the ASCOM standard.** The upstream OpenAPI specs are vendored in
  `specs/` as the reference the implementation. 
- **Interoperate at the wire level.** A goalpaca server is discoverable and 
  usable by any conformant client alpaca client. The goalpaca client can 
  drive any conformant device or server (a real driver, the .NET OmniSimulator).
- **Go Standard library.** Runs on Linux (incl. Raspberry Pi), macOS, and Windows. 


## Packages

| Path | Purpose |
|------|---------|
| `alpaca/` | The alpaca wire protocol, device-type names, per-type enums, the error model, and the ImageBytes codec.  |
| `server/` | A library implementing an Alpaca device (server). Include into an existing hardware device driver to add Alpaca server support.  |
| `client/` | A library implementing an Alpaca client for talking to compliant Alpaca devices.  Include in a client application to connect to Alpaca devices.|
| `registry/` | Methods for managing multiple alpaca devices on one host ([alpacahurd](https://github.com/mikefsq/alpacahurd)). |
| `sim/` | Simulator implementations of all ten device types for testing with no hardware. |
| `conformance/` | ConformU-derived conformance checks that drive the client against a device (a sim or a real server). |
| `cmd/alpacasim/` | Serves all ten simulated devices behind one Alpaca port, useful as a ConformU target and a dev server. |
| `cmd/alpacadiscover/` | CLI that runs discovery and prints the servers found and each one's configured devices. |
| `cmd/discover_proxy/` | Non-standard. A discovery proxy that answers Alpaca UDP discovery on behalf of drivers that register via a unicast heartbeat. |
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

The conformance layer ports [ConformU's](https://github.com/ASCOMInitiative/ConformU) 
checks to Go and runs them through the client against the simulators. All ten device 
types pass ConformU. The compliance rules are enforced by the `server` library.  
Any driver built on this library therefore gains this layer of compliance checking.

## Documentation

API documentation lives in the godoc:

- [server](https://pkg.go.dev/github.com/mikefsq/goalpaca/server) — device/server library
- [client](https://pkg.go.dev/github.com/mikefsq/goalpaca/client) — client library
- [registry](https://pkg.go.dev/github.com/mikefsq/goalpaca/registry) — driver catalogue for composed hosts
- [sim](https://pkg.go.dev/github.com/mikefsq/goalpaca/sim) — the ten device simulators
- [conformance](https://pkg.go.dev/github.com/mikefsq/goalpaca/conformance) — conformance checks

Protocol references: the vendored OpenAPI specs in [`specs/`](specs/) and the
[ASCOM Alpaca API Reference](https://ascom-standards.org/AlpacaDeveloper/ASCOMAlpacaAPIReference.html).

## License

[MIT](LICENSE) © 2026 @mikefsq. The vendored ASCOM OpenAPI specs in
`specs/` are MIT © ASCOM Initiative (upstream notices preserved).
