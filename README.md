# goalpaca

Go libraries and tools for [ASCOM Alpaca](https://ascom-standards.org/) astronomy
devices: clients, device servers, simulators, and conformance checks.

Requires Go 1.23 or later. Uses only the standard library and supports Linux,
macOS, and Windows.

## Try the simulators

From a checkout, serve all ten device types on HTTP port 11111:

```sh
go run ./cmd/alpacasim
```

Connect your Alpaca application to `127.0.0.1:11111`, or use its discovery
feature. Each device type has device number 0. Open
`http://127.0.0.1:11111/setup` for the device list and setup pages.

Discover servers from another terminal:

```sh
go run ./cmd/alpacadiscover -timeout 2s
```

Useful simulator options:

| Flag | Purpose |
|------|---------|
| `-port 11112` | Change the HTTP port |
| `-discovery off` | Disable UDP discovery (enabled by default) |
| `-ipv6` | Also answer IPv6 multicast discovery |
| `-quiet` | Disable per-request logging |

Discovery uses UDP port 32227. Clients on another machine need access to both
the HTTP port and discovery port. Direct addresses work without discovery.

## Use the client library

Add goalpaca to your Go module:

```sh
go get github.com/mikefsq/goalpaca
```

Read a camera's sensor width:

```go
package main

import (
    "fmt"
    "log"

    "github.com/mikefsq/goalpaca/client"
)

func main() {
    cam := client.NewCamera("127.0.0.1:11111", 0)
    if err := cam.SetConnected(true); err != nil {
        log.Fatal(err)
    }
    defer cam.SetConnected(false)

    width, err := cam.CameraXSize()
    if err != nil {
        log.Print(err)
        return
    }
    fmt.Println(width)
}
```

The client supports all ten Alpaca device types and both JSON and ImageBytes
camera transport. See [client examples](client/example_test.go) and
[API documentation](https://pkg.go.dev/github.com/mikefsq/goalpaca/client).

## Write a driver

See [DRIVERS.md](DRIVERS.md) for device interfaces, hardware lifecycle,
configuration, standalone binaries, and testing. The [simulators](sim/) provide
working implementations without hardware.

## Packages and tools

| Package | Purpose |
|---------|---------|
| `alpaca` | Protocol types, errors, and ImageBytes encoding |
| `client` | Typed clients and discovery |
| `server` | HTTP serving, discovery, setup forms, and protocol validation |
| `registry` | Driver registration and construction from configuration |
| `devicemain` | Command-line entry point for standalone drivers |
| `sim` | Simulators for all ten device types |
| `conformance` | Device checks based on ConformU |

`cmd/discover_proxy` answers discovery for servers using goalpaca's registration
extension; see [discovery relay setup](DISCOVERY_RELAY.md). `cmd/fault_proxy` injects faults for testing client recovery; see its
[usage guide](cmd/fault_proxy/README.md).

Standalone drivers using `devicemain` accept `-discover` to scan attached hardware
and print JSON before configuration loading or device construction. The response
contains `driver`, `supported`, `identity` keys in preference order, and `devices`
with `label` and configuration `values`. Unsupported scanners return
`supported: false`; a supported scan with no matches returns an empty array.
Scan errors appear in JSON and cause a nonzero exit. Scanners receive a ten-second
context deadline and must leave devices already in use undisturbed. Custom
launchers can call `devicemain.Discover` for the same output contract.
This hardware scan is separate from `-discovery` and `client` network discovery.

## Tests

```sh
go test ./...
go test -race ./...
```

The suite covers protocol handling, clients, simulators, and ConformU-derived
checks. Hardware drivers need their own tests for capabilities, limits, and
failure handling.

## References

- [Go API documentation](https://pkg.go.dev/github.com/mikefsq/goalpaca)
- [Vendored Alpaca OpenAPI specifications](specs/)

## License

[MIT](LICENSE), © 2026 @mikefsq. Vendored ASCOM specifications retain their
upstream MIT notices.
