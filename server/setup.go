package server

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

// SettingField describes one configurable value on a device's setup page. The
// framework renders it as an HTML form control and, on submit, hands the posted
// value back to Configurable.ApplySettings keyed by Name.
type SettingField struct {
	Name    string   // form key (stable identifier used in the values map)
	Label   string   // human-readable label
	Type    string   // control: "text" (default), "number", "checkbox", "select", "password"
	Value   string   // current value as a string ("true"/"false" for checkbox)
	Options []string // choices when Type == "select"
	Help    string   // optional help text shown under the control

	// Locked marks a field whose value is pinned by a higher-precedence source
	// (typically a hurd.conf override) and therefore cannot be changed here. The
	// framework renders it disabled, ignores it on submit, and never persists or
	// restores it — so precedence stays default < persisted < host override. The
	// device sets this for keys its host supplied; Source names that source for
	// the note shown beside the control (e.g. "set in hurd.conf").
	Locked bool
	Source string
}

// Configurable is an optional interface a Device may implement to expose a
// browser configuration form at /setup/v1/{device_type}/{device_number}/setup,
// as described by the ASCOM Alpaca Management API. The framework renders
// SettingsForm() as an HTML form and, on submit, calls ApplySettings with the
// posted name→value pairs.
//
// A device that does NOT implement Configurable still gets a spec-conformant
// "this device has no configurable settings" page — the Alpaca spec requires
// the setup page to exist even when the device is not configurable.
//
// ApplySettings runs on an HTTP handler goroutine, concurrently with normal
// device operation: the implementation owns its locking. Return a non-nil error
// to reject the submission; its message is shown on the page and no success is
// reported. On success the page re-renders from a fresh SettingsForm(), so
// applied values are reflected back.
//
// Settings resolve by precedence — code default < persisted config file <
// host override (hurd.conf). The framework enforces this around the interface:
// a field the device marks Locked (pinned by the host) is never fed a persisted
// value at startup and is dropped from any submit, so ApplySettings only ever
// receives values for editable fields. ApplySettings must apply just the keys
// it is given and leave the rest (including locked ones) untouched.
type Configurable interface {
	SettingsForm() []SettingField
	ApplySettings(values map[string]string) error
}

// handleSetup serves the Alpaca browser setup interface:
//
//	GET  /setup                                     — server-wide page + device links
//	GET  /setup/v1/{device_type}/{device_number}/setup — per-device form (or "not configurable")
//	POST /setup/v1/{device_type}/{device_number}/setup — apply submitted settings
//
// Per the spec, an invalid /setup URL returns 403 with an HTML message.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/setup" {
		if r.Method != http.MethodGet {
			setupError(w, http.StatusMethodNotAllowed, "The /setup page only supports GET.")
			return
		}
		s.renderServerSetup(w)
		return
	}

	rest, ok := strings.CutPrefix(path, "/setup/v1/")
	if !ok {
		setupError(w, http.StatusForbidden, "Unsupported setup URL.")
		return
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[2] != "setup" {
		setupError(w, http.StatusForbidden, "Unsupported setup URL.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		setupError(w, http.StatusMethodNotAllowed, "The device setup page supports GET and POST.")
		return
	}

	devType := DeviceType(parts[0])
	number, err := strconv.Atoi(parts[1])
	if err != nil {
		setupError(w, http.StatusForbidden, "Invalid device number.")
		return
	}
	dev, found := s.lookup(devType, number)
	if !found {
		setupError(w, http.StatusForbidden,
			fmt.Sprintf("No %s device %d is configured on this server.", devType, number))
		return
	}
	s.renderDeviceSetup(w, r, devType, number, dev)
}

// deviceLink is one row on the server setup page.
type deviceLink struct {
	Name   string
	Type   DeviceType
	Number int
	URL    string
}

func (s *Server) renderServerSetup(w http.ResponseWriter) {
	s.mu.RLock()
	links := make([]deviceLink, 0, len(s.order))
	for _, rd := range s.order {
		links = append(links, deviceLink{
			Name:   rd.dev.Name(),
			Type:   rd.typ,
			Number: rd.num,
			URL:    fmt.Sprintf("/setup/v1/%s/%d/setup", rd.typ, rd.num),
		})
	}
	s.mu.RUnlock()

	data := struct {
		ServerName          string
		Manufacturer        string
		ManufacturerVersion string
		Location            string
		Port                int
		Devices             []deviceLink
	}{
		ServerName:          orDefault(s.cfg.ServerName, "Alpaca Server"),
		Manufacturer:        s.cfg.Manufacturer,
		ManufacturerVersion: s.cfg.ManufacturerVersion,
		Location:            s.cfg.Location,
		Port:                s.Port(),
		Devices:             links,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = serverSetupTmpl.Execute(w, data)
}

func (s *Server) renderDeviceSetup(w http.ResponseWriter, r *http.Request, devType DeviceType, number int, dev Device) {
	cfg, ok := dev.(Configurable)
	if !ok {
		// Not configurable — still a conformant 200 page, as the spec requires.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = deviceSetupTmpl.Execute(w, deviceSetupView{
			Title:          fmt.Sprintf("%s (%s #%d)", dev.Name(), devType, number),
			Description:    dev.Description(),
			Configurable:   false,
			BackURL:        "/setup",
		})
		return
	}

	var banner, bannerKind string
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			banner, bannerKind = "Could not read the submitted form.", "error"
		} else {
			values := collectValues(cfg.SettingsForm(), r)
			if err := cfg.ApplySettings(values); err != nil {
				banner, bannerKind = err.Error(), "error"
			} else if s.cfg.Settings != nil {
				if err := s.cfg.Settings.Save(dev.UniqueID(), snapshot(cfg.SettingsForm())); err != nil {
					banner, bannerKind = "Settings applied, but could not be saved: "+err.Error(), "warn"
				} else {
					banner, bannerKind = "Settings applied and saved.", "ok"
				}
			} else {
				banner, bannerKind = "Settings applied.", "ok"
			}
		}
	}

	// Re-read the form after applying so the page reflects the current state.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = deviceSetupTmpl.Execute(w, deviceSetupView{
		Title:        fmt.Sprintf("%s (%s #%d)", dev.Name(), devType, number),
		Description:  dev.Description(),
		Configurable: true,
		Fields:       cfg.SettingsForm(),
		Banner:       banner,
		BannerKind:   bannerKind,
		BackURL:      "/setup",
	})
}

// collectValues extracts one string per editable field from the posted form.
// Locked fields are skipped so a submit (even a hand-crafted one) can never
// change a host-pinned value. Checkboxes are special: an unchecked box submits
// nothing, so its value is derived from presence ("true"/"false"). Only declared
// field names are read, so stray form keys cannot reach ApplySettings.
func collectValues(fields []SettingField, r *http.Request) map[string]string {
	values := make(map[string]string, len(fields))
	for _, f := range fields {
		if f.Locked {
			continue
		}
		if f.Type == "checkbox" {
			if r.PostForm.Get(f.Name) != "" {
				values[f.Name] = "true"
			} else {
				values[f.Name] = "false"
			}
			continue
		}
		values[f.Name] = r.PostForm.Get(f.Name)
	}
	return values
}

// snapshot captures a form's editable field values as a name→value map for
// persistence; loading applies the same map back through ApplySettings. Locked
// fields are excluded: their values belong to the host (hurd.conf), so storing
// them would let a stale copy resurface if the host later releases the pin.
func snapshot(fields []SettingField) map[string]string {
	m := make(map[string]string, len(fields))
	for _, f := range fields {
		if f.Locked {
			continue
		}
		m[f.Name] = f.Value
	}
	return m
}

func setupError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = setupErrorTmpl.Execute(w, msg)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

type deviceSetupView struct {
	Title        string
	Description  string
	Configurable bool
	Fields       []SettingField
	Banner       string
	BannerKind   string // "ok" | "error"
	BackURL      string
}

const setupCSS = `
:root{color-scheme:light dark}
body{font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;max-width:44rem;margin:2rem auto;padding:0 1rem;line-height:1.5}
h1{font-size:1.4rem;margin-bottom:.2rem}
.sub{color:#888;margin-top:0}
table{border-collapse:collapse;width:100%}
td,th{text-align:left;padding:.35rem .5rem;border-bottom:1px solid #8884}
label{display:block;margin:1rem 0}
label .lab{font-weight:600;display:block;margin-bottom:.25rem}
input[type=text],input[type=number],input[type=password],select{width:100%;padding:.4rem;box-sizing:border-box;font-size:1rem}
.help{color:#888;font-size:.85rem;margin-top:.2rem}
.help.locked{color:#c9a227}
label.locked .lab{color:#888}
input:disabled,select:disabled{opacity:.6;cursor:not-allowed}
.banner{padding:.6rem .8rem;border-radius:.4rem;margin:1rem 0}
.banner.ok{background:#2e7d3222;border:1px solid #2e7d32}
.banner.error{background:#c6282822;border:1px solid #c62828}
.banner.warn{background:#f9a82522;border:1px solid #f9a825}
button{font-size:1rem;padding:.5rem 1.2rem;margin-top:1rem;cursor:pointer}
a{color:#4a9eff}
`

var (
	serverSetupTmpl = template.Must(template.New("server").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>{{.ServerName}} — Setup</title><style>` + setupCSS + `</style></head>
<body>
<h1>{{.ServerName}}</h1>
<p class="sub">{{with .Manufacturer}}{{.}}{{end}}{{with .ManufacturerVersion}} · v{{.}}{{end}}{{with .Location}} · {{.}}{{end}}</p>
<p>This Alpaca server is listening on port <strong>{{.Port}}</strong>.</p>
<h2>Devices</h2>
{{if .Devices}}
<table>
<tr><th>Name</th><th>Type</th><th>#</th><th></th></tr>
{{range .Devices}}<tr><td>{{.Name}}</td><td>{{.Type}}</td><td>{{.Number}}</td><td><a href="{{.URL}}">Configure →</a></td></tr>
{{end}}
</table>
{{else}}<p>No devices are configured.</p>{{end}}
</body></html>`))

	deviceSetupTmpl = template.Must(template.New("device").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>{{.Title}} — Setup</title><style>` + setupCSS + `</style></head>
<body>
<p><a href="{{.BackURL}}">← All devices</a></p>
<h1>{{.Title}}</h1>
{{with .Description}}<p class="sub">{{.}}</p>{{end}}
{{with .Banner}}<div class="banner {{$.BannerKind}}">{{.}}</div>{{end}}
{{if .Configurable}}
<form method="post">
{{range .Fields}}
<label{{if .Locked}} class="locked"{{end}}>
<span class="lab">{{.Label}}</span>
{{if eq .Type "checkbox"}}<input type="checkbox" name="{{.Name}}" value="true"{{if eq .Value "true"}} checked{{end}}{{if .Locked}} disabled{{end}}>
{{else if eq .Type "select"}}<select name="{{.Name}}"{{if .Locked}} disabled{{end}}>{{$v := .Value}}{{range .Options}}<option value="{{.}}"{{if eq . $v}} selected{{end}}>{{.}}</option>{{end}}</select>
{{else if eq .Type "number"}}<input type="number" step="any" name="{{.Name}}" value="{{.Value}}"{{if .Locked}} disabled{{end}}>
{{else if eq .Type "password"}}<input type="password" name="{{.Name}}" value="{{.Value}}"{{if .Locked}} disabled{{end}}>
{{else}}<input type="text" name="{{.Name}}" value="{{.Value}}"{{if .Locked}} disabled{{end}}>{{end}}
{{if .Locked}}<span class="help locked">🔒 {{with .Source}}{{.}}{{else}}pinned; not editable here{{end}}</span>{{end}}
{{with .Help}}<span class="help">{{.}}</span>{{end}}
</label>
{{end}}
<button type="submit">Apply</button>
</form>
{{else}}
<p>This device has no configurable settings.</p>
{{end}}
</body></html>`))

	setupErrorTmpl = template.Must(template.New("err").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Setup error</title><style>` + setupCSS + `</style></head>
<body><h1>Setup</h1><div class="banner error">{{.}}</div><p><a href="/setup">← Back</a></p></body></html>`))
)
