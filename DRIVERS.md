# Writing a goalpaca driver

A driver implements one of the typed interfaces in `server`, such as
`server.Focuser` or `server.Camera`. The server supplies HTTP encoding,
discovery, setup pages, and common protocol validation. Your driver supplies
hardware behavior, capability reporting, and hardware-specific limits.

For client applications and simulators, see [README.md](README.md).

## Implement the device

Embed the matching base type, such as `server.BaseFocuser`, and override the
methods your hardware supports. Base types provide identity, logical connection
state, and defaults for unsupported operations.

Set `ID`, `DevName`, `Desc`, `Info`, `Version`, and `IfaceVer` in the constructor.
Use a stable device ID and advertise the interface version your driver implements.
The current version constants are in [platform7.go](server/platform7.go).
Copy `registry.Spec.Instance` into `BaseDevice.Instance` so `Label()` identifies
the device consistently in host and driver logs.

See the [focuser example](server/example_test.go) for direct hosting and
[sim/focuser.go](sim/focuser.go) for a complete simulated device. Add a compile-time
interface assertion to your implementation:

```go
var _ server.Focuser = (*Device)(nil)
```

Return Alpaca errors for unsupported operations, invalid values, and device
state errors. `server.ErrNotImplemented`, `server.ErrInvalidValue`, and
`server.InvalidOperationError` cover common cases. Capability flags must agree
with the operations the device actually supports.

## Own the hardware lifecycle

Implement `server.Hardware` to acquire and release hardware:

- Keep constructors free of hardware access. Hosts use them for config checks
  and to prepare replacements before reload.
- `Open(ctx)` starts hardware access. Its context lasts until the device closes.
- `Close(ctx)` stops workers and releases handles. It runs on shutdown,
  unregistration, and reload.
- Keep logical Alpaca `Connect` and `Disconnect` separate from hardware ownership.
  The base type's connection flag is shared device state, not a per-client map.

The server cancels the Open context before calling Close. Wait for workers to
stop before releasing hardware so they cannot reacquire it during reload.
`server.RunLoop` supplies supervision and a cancellation/wait function; its
wait can time out, so workers still need to honor cancellation. Open failures
are logged and leave the device registered. Implement recovery in the driver,
or require a reload to retry.

HTTP requests can call methods concurrently. Protect shared state and serialize
hardware operations where required. `server.Busyable` rejects ordinary writes
while busy, but drivers must guard operation starts atomically: two requests
can pass the HTTP busy check together. `server.Op.TryBegin` provides that guard;
use Complete or Fail to finish the operation and expose the appropriate status.
Interrupt operations must remain available while busy.

The HTTP server applies the exported `Gate*` validation functions automatically.
Hosts calling drivers directly must apply the matching gates before calling
methods, particularly ID-taking switch methods. Drivers still enforce hardware
limits and synchronization.

## Register the driver

A reusable driver package registers itself from `init`. Hosts include it with
a blank import; standalone and bundled hosts use the same constructor.

```go
func init() {
    registry.Register(registry.Driver{
        Name:          "myfocuser",
        Type:          server.FocuserType,
        Description:   "My focuser over a serial port",
        ConfigExample: `{"driver":"myfocuser","addr":"/dev/ttyUSB0"}`,
        Config:        func() any { return &Config{} },
        New: func(spec registry.Spec) (server.Device, error) {
            var cfg Config
            if err := spec.Decode(&cfg); err != nil {
                return nil, err
            }
            return New(cfg, spec)
        },
    })
}
```

Here `Config` and `New` are your driver's types and constructor. Validate
configuration in New without connecting to hardware. Honor `spec.Name` when it
is set. `spec.Device` contains the host's assigned device number when known.

`Spec.Decode` removes host-owned keys and rejects unknown driver fields. Do not
reuse names returned by `registry.CommonKeys`, including `driver`, `name`,
`port`, and `device`. `ConfigExample` must be valid JSON and omit `port`, which
the host assigns.

For drivers exposing multiple devices from one entry, `MultiKey` names the array
that supporting hosts expand into individual specs. `FrontEnd` can start an
additional protocol service. It must return promptly, follow its context, and
resolve the supplied device function on each use because reload can replace the
device. See [registry.Driver](registry/registry.go) for the full contracts.

## Configuration and setup forms

Return a pointer to a zero-valued config struct from `Driver.Config`. Use `json`
tags for field names and `alpaca` tags for form metadata:

```go
type Config struct {
    Addr  string `json:"addr" alpaca:"label=Serial port,when=start"`
    Speed int    `json:"speed" alpaca:"label=Speed,min=1,max=100"`
}
```

Tags support labels, help text, numeric bounds, select options, secret fields,
and start-only settings. See [SettingTag](server/settingtag.go) for syntax.
Implement `server.Reconfigurable` to receive live changes as a fresh config
struct. Validate before applying and synchronize with normal device operations.
Without Reconfigurable, generated forms are read-only; `when=start` fields are
always read-only and must be changed in the config file.

Hosts apply settings in this order: defaults, persisted values, then pinned host
overrides. A custom host attaches generated forms with `NewStructConfig` and
`RegisterConfigurable`. A device can instead implement `server.Configurable`
for its own form definition. See [SETUP_FORMS.md](SETUP_FORMS.md) for tag syntax,
form integration, and persistence.

## Build a standalone binary

Add a command that imports the driver and calls `devicemain.Run`:

```go
package main

import (
    "github.com/mikefsq/goalpaca/devicemain"
    _ "example.com/myfocuser"
)

func main() { devicemain.Run("myfocuser") }
```

The command supplies `-config`, `-port`, `-check`, `-schema`, discovery options,
and flags derived from the config struct. Use `-h` for the full list.
Device files accept JSONC (`//` and `/* */` comments), without trailing commas.
Explicit flags override file values. `registry.Spec.Decode` itself expects JSON;
custom hosts must strip comments before constructing specs.

`-check` validates and constructs the device without opening hardware.
`-schema commented` prints a commented starter entry. Setup changes are stored
under the driver's state directory when the run has a named config-file instance.
`ALPACA_STATE_DIR` overrides that directory; platform defaults are resolved by
[server.StateDir](server/dirs.go).

To include the package in alpacahurd, follow its driver import and configuration
instructions. No separate hardware implementation is needed.

## Test the driver

Test config validation without hardware, then test the typed device methods
with a fake transport or SDK. Cover concurrent starts, cancellation, disconnects,
and cleanup during reload. For protocol tests, register the device with a server
and drive it through the typed client; see [server tests](server/server_test.go).

Run `go test ./...` and `go test -race ./...` in the driver module.
The `conformance.Check*` functions exercise protocol and device behavior through
typed clients. Several checks assume simulator-specific values and capabilities; adapt them
before testing hardware. They can move devices and change settings. Increase `conformance.SettleTimeout` for slower hardware.
Passing these checks does not replace testing with the intended clients and
ConformU against the actual driver.
