# Writing a goalpaca driver

A driver implements one of the typed interfaces in `server`, such as
`server.Focuser` or `server.Camera`. The server supplies HTTP encoding,
discovery, setup pages and common protocol validation. The driver supplies
hardware behavior, capability reporting and hardware-specific limits.

[README.md](README.md) covers client applications and simulators.

## Implementing the device

A device embeds the matching base type, such as `server.BaseFocuser`, and
overrides the methods its hardware supports. The base types provide identity,
logical connection state and defaults for unsupported operations.

The constructor sets `ID`, `DevName`, `Desc`, `Info`, `Version` and `IfaceVer`.
`ID` is a stable device ID, and `IfaceVer` is the interface version the driver
implements. The current version constants are in [platform7.go](server/platform7.go).
The constructor also copies `registry.Spec.Instance` into `BaseDevice.Instance`,
so `Label()` names the device the same way in host and driver logs.

The [focuser example](server/example_test.go) shows direct hosting, and
[sim/focuser.go](sim/focuser.go) is a complete simulated device. A compile-time
assertion confirms the implementation:

```go
var _ server.Focuser = (*Device)(nil)
```

Unsupported operations, invalid values and device state errors return Alpaca
errors. `server.ErrNotImplemented`, `server.ErrInvalidValue` and
`server.InvalidOperationError` cover the common cases. Capability flags agree
with the operations the device supports.

## Hardware lifecycle

The device implements `server.Hardware` to acquire and release hardware:

- The constructor does not touch hardware. Hosts call it for config checks and
  to prepare a replacement before a reload.
- `Open(ctx)` starts hardware access. Its context lasts until the device closes.
- `Close(ctx)` stops workers and releases handles. It runs on shutdown,
  unregistration and reload.
- Alpaca `Connect` and `Disconnect` stay separate from hardware ownership. The
  base type's connection flag is shared device state, with no per-client map.

The server cancels the Open context before calling Close. Workers stop before
the hardware is released, so they cannot reacquire it during a reload.
`server.RunLoop` supplies supervision and a function that cancels and waits.
That wait can time out, so workers still honor cancellation. An Open failure is
logged and leaves the device registered. Recovery is the driver's job, or a
reload retries.

HTTP requests can call methods concurrently. Shared state needs protecting, and
hardware operations need serializing where the hardware requires it.
`server.Busyable` rejects ordinary writes while the device is busy. Two requests
can still pass that check together, so the driver guards each operation start
atomically. `server.Op.TryBegin` provides that guard, and `Complete` or `Fail`
finishes the operation and sets its status. Interrupt operations stay available
while the device is busy.

The HTTP server applies the exported `Gate*` validation functions automatically.
A host calling a driver directly applies the matching gates first, particularly
before ID-taking switch methods. The driver still enforces hardware limits and
synchronization.

## Registration

A reusable driver package registers itself from `init`. A host includes it with
a blank import, and standalone and bundled hosts use the same constructor.

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

`Config` and `New` here are the driver's own types and constructor. `New`
validates the configuration without connecting to hardware and applies
`spec.Name` when it is set. `spec.Device` holds the host's assigned device
number when it is known.

`Spec.Decode` removes host-owned keys and rejects unknown driver fields. A
driver's config keys avoid the names in `registry.CommonKeys`, which include
`driver`, `name`, `port` and `device`. `ConfigExample` is valid JSON and omits
`port`, which the host assigns.

`MultiKey` names an array of devices in one entry, which supporting hosts
expand into individual specs. `FrontEnd` starts an additional protocol service.
It returns promptly and follows its context. It resolves the supplied device
function on each use, because a reload can replace the device.
[registry.Driver](registry/registry.go) documents the full contracts.

## Configuration and setup forms

`Driver.Config` returns a pointer to a zero-valued config struct. The `json`
tags name the fields, and the `alpaca` tags carry the form metadata:

```go
type Config struct {
    Addr  string `json:"addr" alpaca:"label=Serial port,when=start"`
    Speed int    `json:"speed" alpaca:"label=Speed,min=1,max=100"`
}
```

The tags support labels, help text, numeric bounds, select options, secret
fields and start-only settings. [SettingTag](server/settingtag.go) defines the
syntax. A device that implements `server.Reconfigurable` receives live changes
as a fresh config struct. It validates the struct before applying it, and
synchronizes with normal device operations. Without Reconfigurable, the
generated form is read-only. A `when=start` field is always read-only and
changes only in the config file.

Hosts apply settings in this order: defaults, persisted values, then pinned host
overrides. A custom host attaches a generated form with `NewStructConfig` and
`RegisterConfigurable`. A device can instead implement `server.Configurable`
for its own form. [SETUP_FORMS.md](SETUP_FORMS.md) covers tag syntax, form
integration and persistence.

## Identity and scanning

A host that configures devices needs two facts a config struct cannot express:
which fields address one device uniquely, and whether the driver can find its
hardware itself.

`Identity` lists the config keys that pin a device, most stable first. A camera
declares `{"serial", "index"}`, since the factory serial survives a replug and
the enumeration index does not. A networked mount declares `{"addr"}`.

`Scan` lists the devices attached now, so a host can offer them to configure.
An operator then never has to know a serial number in advance. `Scan` stays nil
for a driver that cannot enumerate, and the host shows the config form for
manual entry.

```go
registry.Register(registry.Driver{
    Name:     "astrocam",
    Identity: []string{"serial", "index"},
    Scan:     scanCameras,
    // ...
})

func scanCameras(ctx context.Context) ([]registry.Found, error) {
    cams, err := astrocam.EnumerateWithSerials()
    if err != nil {
        return nil, err
    }
    out := make([]registry.Found, 0, len(cams))
    for i, c := range cams {
        out = append(out, registry.Found{
            Label:  fmt.Sprintf("%s (serial %s)", c.Name, c.Serial),
            Values: map[string]any{"serial": c.Serial, "index": i},
        })
    }
    return out, nil
}
```

`Found.Values` uses the driver's own config keys, so a host writes the entry and
renders the form without knowing which driver it holds. A key the scan could not
read is omitted, never set empty.

A scan touches the bus, so it retains nothing. It opens only what reading the
identity requires and releases it before returning. A device already in use is
still listed, with whatever was readable, because an operator looking for a
camera that was just connected needs to see it. Hosts call `Scan` from an
operator's action and pin the result in a config entry, so the bus is walked
once and never polled.

## Standalone binary

A command imports the driver and calls `devicemain.Run`:

```go
package main

import (
    "github.com/mikefsq/goalpaca/devicemain"
    _ "example.com/myfocuser"
)

func main() { devicemain.Run("myfocuser") }
```

The command supplies `-config`, `-port`, `-check`, `-schema`, `-discover`, the
`-discovery` options, `-hurd-port`, and flags derived from the config struct.
`-h` lists them all. Device files accept JSONC with `//` and `/* */` comments,
without trailing commas. Explicit flags override file values.
`registry.Spec.Decode` itself expects JSON, so a custom host strips comments
before constructing specs.

`-check` validates and constructs the device without opening hardware.
`-schema commented` prints a commented starter entry. `-discover` prints the
`Scan` results as JSON. Setup changes are stored under the driver's state
directory when the run has a named config-file instance. `ALPACA_STATE_DIR`
overrides that directory, and [server.StateDir](server/dirs.go) resolves the
platform defaults.

alpacahurd includes a driver package through its own import list and
configuration. The driver needs no separate hardware implementation for it.

## Testing

Config validation is tested without hardware, and the typed device methods
with a fake transport or SDK. The tests cover concurrent starts, cancellation,
disconnects and cleanup during a reload. Protocol tests register the device with
a server and drive it through the typed client, as the
[server tests](server/server_test.go) do.

`go test ./...` and `go test -race ./...` run in the driver module. The
`conformance.Check*` functions exercise protocol and device behavior through
typed clients. Several checks assume simulator values and capabilities, so they
need adapting before a hardware test. They can move devices and change settings.
`conformance.SettleTimeout` needs raising for slower hardware. Passing these
checks does not replace testing with the intended clients and with ConformU
against the actual driver.
