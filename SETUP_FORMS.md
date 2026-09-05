# Setup forms

Drivers can expose browser settings through a tagged config struct and
`server.Reconfigurable`. For the complete driver workflow, see
[DRIVERS.md](DRIVERS.md).

## Define the fields

Return a pointer to your config struct from `registry.Driver.Config`:

```go
type Config struct {
    Serial string `json:"serial" alpaca:"label=Serial number,when=start"`
    Speed  int    `json:"speed" alpaca:"label=Speed,min=1,max=100"`
    Mode   string `json:"mode" alpaca:"options=normal|quiet"`
}

// In the registry.Driver entry:
// Config: func() any { return &Config{} },
```

The `json` tag names the setting. The `alpaca` tag controls its form field:

| Tag | Meaning |
|-----|---------|
| `label` | Label; defaults to the JSON field name |
| `help` | Help text beneath the control |
| `type` | Override: `text`, `number`, `checkbox`, `select`, or `password` |
| `min`, `max` | Numeric bounds |
| `options` | Select choices separated by `\|` |
| `when` | `live` (default) or `start` |
| `secret` | Password control with no current value sent to the browser |
| `hidden` | Omit the field |

Without a type override, booleans become checkboxes, numbers become numeric
controls, and strings become text controls or selects when options are supplied.
Tag values containing commas can be quoted. See
[ParseSettingTag](server/settingtag.go) for the full syntax.

`when=start` fields are read-only. Change them in the config file and restart
or reload the device. Live fields are editable only when the device implements
`server.Reconfigurable`.

Use the form for driver configuration that clients cannot already set through
the standard Alpaca API. The device page also provides an Actions console from
`SupportedActions`; actions need no form tags.

## Apply changes

Implement this method on the device:

```go
func (d *Device) Reconfigure(cfg any) error
```

The generated adapter passes a fresh pointer of the type returned by
`Driver.Config`, containing the current values plus submitted changes. It
validates field types, numeric bounds, and select options before calling
Reconfigure. The driver must validate hardware-specific constraints and
synchronize changes with normal device operations.

Return an error to reject a submission. The adapter commits its current values
only after Reconfigure succeeds; it cannot undo hardware changes made by the
driver before an error. Validate before applying changes.

Config-file values outside tag bounds can be displayed, but submissions are
validated against those bounds. Tags do not replace validation in the driver's
constructor.

## Attach the form

`devicemain` attaches generated forms for registered drivers. A custom host uses
`server.NewStructConfig` with the device, config factory, effective JSON entry,
and any pinned fields, then calls `Server.RegisterConfigurable`.

Pinned fields are read-only and excluded from submitted changes. Settings
precedence is defaults, then persisted values, then host overrides. Set up the
adapter and settings path before starting the server. Attaching an adapter to
a running server applies its persisted settings immediately.

A device can implement `server.Configurable` directly to supply `SettingsForm`
and `ApplySettings`. That implementation takes precedence over the generated
adapter. ApplySettings must update only the supplied fields and provide its
own synchronization.

## Persistence

Set `server.Config.Settings` to a `SettingsStore` to save accepted changes.
`server.NewFileStore()` stores JSON using atomic replacement and mode 0600.
`Server.SettingsPath` selects the file for a device; the default is
`<StateDir(ServerName)>/<type>-<number>.json`. Hosts can choose a different layout.
See [directory resolution](server/dirs.go) for platform paths and environment
overrides.

Generated forms persist typed JSON values from `StructConfig.PersistValues`,
excluding host-pinned keys. A custom Configurable can provide the same method;
otherwise the server saves form strings, merging the submission with the prior
editable values. Persisted values reflect the accepted submission, without
reading back hardware rounding or clamping.

A save failure leaves the live changes applied and displays a warning on the
page.

## Pages and templates

`/setup` lists devices. Each device has a page at
`/setup/v1/{device_type}/{device_number}/setup`. Devices without settings still
have a setup page. `GET /` redirects to `/setup`.

Set `Config.Setup` for a server-level form. `Config.SetupTemplates` can override
the server, device, and error templates and stylesheet; use
`server.DefaultSetupTemplates()` as a starting point.

Setup POSTs with an Origin or Referer naming another host receive HTTP 403.
Requests with neither header are accepted for scripts and command-line clients.

## Implementation reference

- [Registry config factory](registry/registry.go)
- [Tag parsing](server/settingtag.go) and [struct conversion](server/settingstruct.go)
- [Generated adapter and Reconfigurable](server/configadapter.go)
- [Forms, Actions console, and templates](server/setup.go)
- [Settings storage](server/settings_store.go)
