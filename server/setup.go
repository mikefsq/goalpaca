package server

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
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

	// Constraints is a short line of the field's rules, rendered beside the
	// help: numeric bounds, the allowed options, and whether the value applies
	// live or at the next start. FieldsFromStruct fills it from the alpaca tag;
	// a hand-written form may set it or leave it empty.
	Constraints string

	// Locked marks a field whose value is pinned by a higher-precedence source
	// (typically a host config override) and therefore cannot be changed here. The
	// framework renders it disabled, ignores it on submit, and never persists or
	// restores it, so precedence stays default < persisted < host override. The
	// device sets this for keys its host supplied; Source names that source for
	// the note shown beside the control (e.g. "set in host config").
	Locked bool
	Source string
}

// Configurable supplies the device's browser setup form. ApplySettings runs
// concurrently with device operations and must provide its own synchronization.
// An error rejects the submission; success refreshes the form.
//
// The server excludes locked fields from submissions and persisted settings.
// ApplySettings must update only the supplied keys. Settings precedence is
// code defaults, then persisted values, then host overrides.
type Configurable interface {
	SettingsForm() []SettingField
	ApplySettings(values map[string]string) error
}

// handleSetup serves the Alpaca browser setup interface:
//
//	GET  /setup                                        server-wide page and device links
//	GET  /setup/v1/{device_type}/{device_number}/setup  per-device form, or "not configurable"
//	POST /setup/v1/{device_type}/{device_number}/setup  apply submitted settings
//
// Per the spec, an invalid /setup URL returns 403 with an HTML message.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && !sameOriginPost(r) {
		s.setupError(w, http.StatusForbidden, "Cross-site form submission refused.")
		return
	}
	path := r.URL.Path
	if path == "/setup" || path == "/setup/server" {
		if path == "/setup" && s.cfg.SetupHome != nil {
			s.cfg.SetupHome.ServeHTTP(w, r)
			return
		}
		if path == "/setup/server" && s.cfg.SetupHome != nil && s.deviceCount() == 0 {
			// A server that exists to carry a host page has no devices to
			// list; the host page is the page.
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
		switch {
		case r.Method == http.MethodGet:
		case r.Method == http.MethodPost && s.cfg.Setup != nil:
		default:
			s.setupError(w, http.StatusMethodNotAllowed, "The /setup page only supports GET.")
			return
		}
		s.renderServerSetup(w, r)
		return
	}

	rest, ok := strings.CutPrefix(path, "/setup/v1/")
	if !ok {
		// A host page: /setup/<name> or anything below it.
		if name, _, _ := strings.Cut(strings.TrimPrefix(path, "/setup/"), "/"); name != "" && name != "v1" {
			if h, ok := s.cfg.SetupPages[name]; ok {
				h.ServeHTTP(w, r)
				return
			}
		}
		s.setupError(w, http.StatusForbidden, "Unsupported setup URL.")
		return
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[2] != "setup" {
		s.setupError(w, http.StatusForbidden, "Unsupported setup URL.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		s.setupError(w, http.StatusMethodNotAllowed, "The device setup page supports GET and POST.")
		return
	}

	devType := DeviceType(parts[0])
	number, err := strconv.Atoi(parts[1])
	if err != nil {
		s.setupError(w, http.StatusForbidden, "Invalid device number.")
		return
	}
	rd, found := s.lookupRegistered(devType, number)
	if !found {
		s.setupError(w, http.StatusForbidden,
			fmt.Sprintf("No %s device %d is configured on this server.", devType, number))
		return
	}
	s.renderDeviceSetup(w, r, devType, number, rd)
}

// sameOriginPost rejects submissions whose Origin or Referer names another host.
// Requests without either header are accepted for non-browser clients.
func sameOriginPost(r *http.Request) bool {
	src := r.Header.Get("Origin")
	if src == "" {
		src = r.Header.Get("Referer")
	}
	if src == "" {
		return true
	}
	u, err := url.Parse(src)
	if err != nil {
		return false
	}
	// Compare host:port. r.Host is what the browser addressed, and for a page
	// served by this server the Origin names the same authority.
	return strings.EqualFold(u.Host, r.Host)
}

// deviceLink is one row on the server setup page.
type deviceLink struct {
	Name   string
	Type   DeviceType
	Number int
	URL    string
}

// serverSetupView is the data the server setup template renders.
type serverSetupView struct {
	ServerName          string
	Manufacturer        string
	ManufacturerVersion string
	Location            string
	Port                int
	ConfigPath          string
	Devices             []deviceLink

	// Server-level form, present when Config.Setup is set.
	Configurable bool
	Fields       []SettingField
	Banner       string
	BannerKind   string

	// Host pages from Config.SetupPages, linked below the devices.
	Pages []pageLink
}

// pageLink is one host page on the server setup page.
type pageLink struct{ Name, URL string }

func (s *Server) renderServerSetup(w http.ResponseWriter, r *http.Request) {
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

	view := serverSetupView{
		ServerName:          orDefault(s.cfg.ServerName, "Alpaca Server"),
		Manufacturer:        s.cfg.Manufacturer,
		ManufacturerVersion: s.cfg.ManufacturerVersion,
		Location:            s.cfg.Location,
		Port:                s.Port(),
		ConfigPath:          s.cfg.ConfigPath,
		Devices:             links,
	}
	if cfg := s.cfg.Setup; cfg != nil {
		view.Configurable = true
		if r.Method == http.MethodPost {
			view.Banner, view.BannerKind = s.applyForm(r, cfg, "")
		}
		view.Fields = cfg.SettingsForm()
	}
	names := make([]string, 0, len(s.cfg.SetupPages))
	for n := range s.cfg.SetupPages {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		view.Pages = append(view.Pages, pageLink{Name: n, URL: "/setup/" + n})
	}
	ext := make([]string, 0, len(s.cfg.SetupLinks))
	for n := range s.cfg.SetupLinks {
		ext = append(ext, n)
	}
	sort.Strings(ext)
	for _, n := range ext {
		view.Pages = append(view.Pages, pageLink{Name: n, URL: s.cfg.SetupLinks[n]})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.tmpl().server.Execute(w, view)
}

// deviceSetupView is the data the device setup template renders.
type deviceSetupView struct {
	Title        string
	Description  string
	Configurable bool
	Fields       []SettingField
	Banner       string
	BannerKind   string // "ok" | "error" | "warn"
	BackURL      string

	// Actions console: every device has SupportedActions and Action, so the
	// console is present on every device page. Result holds the last reply.
	Actions      []string
	ActionName   string
	ActionParams string
	ActionResult string
	ActionError  string

	// Reloadable is set when the host gave the device a Reloader, so the page
	// offers a reload: configuration re-read, hardware closed and reopened,
	// port kept.
	Reloadable bool
}

func (s *Server) renderDeviceSetup(w http.ResponseWriter, r *http.Request, devType DeviceType, number int, rd *registeredDevice) {
	dev, cfg, configurable := s.current(rd)
	view := deviceSetupView{
		Title:       fmt.Sprintf("%s (%s #%d)", dev.Name(), devType, number),
		Description: dev.Description(),
		BackURL:     "/setup",
		Actions:     dev.SupportedActions(),
		Reloadable:  s.reloadable(rd),
	}
	view.Configurable = configurable

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			view.Banner, view.BannerKind = "Could not read the submitted form.", "error"
		} else if r.PostForm.Get("_form") == "action" {
			view.ActionName = r.PostForm.Get("_action")
			view.ActionParams = r.PostForm.Get("_params")
			res, err := dev.Action(view.ActionName, view.ActionParams)
			if err != nil {
				view.ActionError = err.Error()
			} else {
				view.ActionResult = res
			}
		} else if r.PostForm.Get("_form") == "reload" {
			if err := s.Reload(r.Context(), devType, number); err != nil {
				view.Banner, view.BannerKind = "Reload failed: "+err.Error(), "error"
			} else {
				view.Banner, view.BannerKind = "Reloaded: configuration re-read and hardware reopened.", "ok"
			}
			// The page describes the device now serving.
			dev, cfg, configurable = s.current(rd)
			view.Title = fmt.Sprintf("%s (%s #%d)", dev.Name(), devType, number)
			view.Description = dev.Description()
			view.Actions = dev.SupportedActions()
			view.Configurable = configurable
		} else if configurable {
			view.Banner, view.BannerKind = s.applyForm(r, cfg, s.settingsKey(rd))
		}
	}
	if configurable {
		// Re-read the form after applying so the page reflects the current state.
		view.Fields = cfg.SettingsForm()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.tmpl().device.Execute(w, view)
}

// applyForm applies and persists a settings submission, returning its banner.
// Persistence overlays submitted values on the form's prior editable values;
// it does not read back driver rounding or clamping.
func (s *Server) applyForm(r *http.Request, cfg Configurable, saveKey string) (banner, kind string) {
	if r.PostForm == nil {
		if err := r.ParseForm(); err != nil {
			return "Could not read the submitted form.", "error"
		}
	}
	before := cfg.SettingsForm()
	values := collectValues(before, r)
	if err := cfg.ApplySettings(values); err != nil {
		return err.Error(), "error"
	}
	if s.cfg.Settings == nil || saveKey == "" {
		return "Settings applied.", "ok"
	}
	var persist map[string]any
	if tv, ok := cfg.(interface{ PersistValues() map[string]any }); ok {
		// A generated form knows its field types and persists typed JSON, so the
		// file decodes under the driver's config struct at the next start.
		persist = tv.PersistValues()
	} else {
		merged := snapshot(before)
		for k, v := range values {
			merged[k] = v
		}
		persist = make(map[string]any, len(merged))
		for k, v := range merged {
			persist[k] = v
		}
	}
	if err := s.cfg.Settings.Save(saveKey, persist); err != nil {
		return "Settings applied, but could not be saved: " + err.Error(), "warn"
	}
	return "Settings applied and saved.", "ok"
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
// fields are excluded: their values belong to the host (host config), so storing
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

func (s *Server) setupError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = s.tmpl().errPage.Execute(w, msg)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// SetupTemplates lets a host replace the setup pages' templates and stylesheet
// (Config.SetupTemplates). Each template receives the same view data the
// default does; DefaultSetupTemplates returns the defaults as a starting point.
// A zero-value field keeps the corresponding default.
//
// Templates are parsed with html/template. The CSS is inlined into each page
// under {{.CSS}} by the default templates; a custom template may link its own.
type SetupTemplates struct {
	Server *template.Template // renders serverSetupView
	Device *template.Template // renders deviceSetupView
	Error  *template.Template // renders a string message
	CSS    string             // stylesheet, inlined by the default templates
}

// DefaultSetupTemplates returns copies of the built-in templates and CSS.
func DefaultSetupTemplates() SetupTemplates {
	return SetupTemplates{
		Server: template.Must(defaultServerTmpl.Clone()),
		Device: template.Must(defaultDeviceTmpl.Clone()),
		Error:  template.Must(defaultErrorTmpl.Clone()),
		CSS:    defaultSetupCSS,
	}
}

// resolvedTemplates is the effective template set: overrides where given,
// defaults elsewhere, with the CSS bound in.
type resolvedTemplates struct {
	server, device, errPage *template.Template
}

// SetupCSS returns the stylesheet the setup pages use, so a host page under
// Config.SetupPages can inline the same one and match.
func (s *Server) SetupCSS() string { return orDefault(s.cfg.SetupTemplates.CSS, defaultSetupCSS) }

// tmpl resolves Config.SetupTemplates once and caches the result.
func (s *Server) tmpl() *resolvedTemplates {
	s.tmplOnce.Do(func() {
		o := s.cfg.SetupTemplates
		css := orDefault(o.CSS, defaultSetupCSS)
		pick := func(over, def *template.Template) *template.Template {
			t := def
			if over != nil {
				t = over
			}
			// Bind the CSS by wrapping the template's data with a css func.
			return template.Must(t.Clone()).Funcs(template.FuncMap{"css": func() template.CSS { return template.CSS(css) }})
		}
		s.tmplCache = &resolvedTemplates{
			server:  pick(o.Server, defaultServerTmpl),
			device:  pick(o.Device, defaultDeviceTmpl),
			errPage: pick(o.Error, defaultErrorTmpl),
		}
	})
	return s.tmplCache
}

const defaultSetupCSS = `
:root{color-scheme:light dark}
body{font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;max-width:44rem;margin:2rem auto;padding:0 1rem;line-height:1.5}
h1{font-size:1.4rem;margin-bottom:.2rem}
h2{font-size:1.1rem;margin-top:2rem}
.sub{color:#888;margin-top:0}
table{border-collapse:collapse;width:100%}
td,th{text-align:left;padding:.35rem .5rem;border-bottom:1px solid #8884}
label{display:block;margin:1rem 0}
label .lab{font-weight:600;display:block;margin-bottom:.25rem}
input[type=text],input[type=number],input[type=password],select{width:100%;padding:.4rem;box-sizing:border-box;font-size:1rem}
.help{color:#888;font-size:.85rem;margin-top:.2rem}
.help.locked{color:#c9a227}
.help.constraints{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.8rem}
label.locked .lab{color:#888}
input:disabled,select:disabled{opacity:.6;cursor:not-allowed}
.banner{padding:.6rem .8rem;border-radius:.4rem;margin:1rem 0}
.banner.ok{background:#2e7d3222;border:1px solid #2e7d32}
.banner.error{background:#c6282822;border:1px solid #c62828}
.banner.warn{background:#f9a82522;border:1px solid #f9a825}
button{font-size:1rem;padding:.5rem 1.2rem;margin-top:1rem;cursor:pointer}
pre.result{background:#8881;padding:.6rem;border-radius:.4rem;white-space:pre-wrap;word-break:break-all}
a{color:#4a9eff}
`

// formFields is the shared control-rendering fragment, defined once and used
// by both page templates.
const formFieldsTmpl = `{{define "fields"}}{{range .}}
<label{{if .Locked}} class="locked"{{end}}>
<span class="lab">{{.Label}}</span>
{{if eq .Type "checkbox"}}<input type="checkbox" name="{{.Name}}" value="true"{{if eq .Value "true"}} checked{{end}}{{if .Locked}} disabled{{end}}>
{{else if eq .Type "select"}}<select name="{{.Name}}"{{if .Locked}} disabled{{end}}>{{$v := .Value}}{{range .Options}}<option value="{{.}}"{{if eq . $v}} selected{{end}}>{{.}}</option>{{end}}</select>
{{else if eq .Type "number"}}<input type="number" step="any" name="{{.Name}}" value="{{.Value}}"{{if .Locked}} disabled{{end}}>
{{else if eq .Type "password"}}<input type="password" name="{{.Name}}" value="{{.Value}}"{{if .Locked}} disabled{{end}}>
{{else}}<input type="text" name="{{.Name}}" value="{{.Value}}"{{if .Locked}} disabled{{end}}>{{end}}
{{if .Locked}}<span class="help locked">🔒 {{with .Source}}{{.}}{{else}}pinned; not editable here{{end}}</span>{{end}}
{{with .Constraints}}<span class="help constraints">{{.}}</span>{{end}}
{{with .Help}}<span class="help">{{.}}</span>{{end}}
</label>
{{end}}{{end}}`

var (
	defaultServerTmpl = template.Must(template.New("server").Funcs(template.FuncMap{"css": func() template.CSS { return "" }}).Parse(formFieldsTmpl + `<!doctype html>
<html><head><meta charset="utf-8"><title>{{.ServerName}} — Setup</title><style>{{css}}</style></head>
<body>
<h1>{{.ServerName}}</h1>
<p class="sub">{{with .Manufacturer}}{{.}}{{end}}{{with .ManufacturerVersion}} · {{.}}{{end}}{{with .Location}} · {{.}}{{end}}</p>
<p>This Alpaca server is listening on port <strong>{{.Port}}</strong>.</p>
{{with .ConfigPath}}<p>Configuration file: <code>{{.}}</code></p>{{else}}<p class="sub">No configuration file; this server is configured by command-line flags.</p>{{end}}
{{if .Configurable}}
<h2>Server settings</h2>
{{with .Banner}}<div class="banner {{$.BannerKind}}">{{.}}</div>{{end}}
<form method="post">
{{template "fields" .Fields}}
<button type="submit">Apply</button>
</form>
{{end}}
<h2>Devices</h2>
{{if .Devices}}
<table>
<tr><th>Name</th><th>Type</th><th>#</th><th></th></tr>
{{range .Devices}}<tr><td>{{.Name}}</td><td>{{.Type}}</td><td>{{.Number}}</td><td><a href="{{.URL}}">Configure →</a></td></tr>
{{end}}
</table>
{{else}}<p>No devices are configured.</p>{{end}}
{{if .Pages}}<h2>More</h2><ul>{{range .Pages}}<li><a href="{{.URL}}">{{.Name}}</a></li>{{end}}</ul>{{end}}
</body></html>`))

	defaultDeviceTmpl = template.Must(template.New("device").Funcs(template.FuncMap{"css": func() template.CSS { return "" }}).Parse(formFieldsTmpl + `<!doctype html>
<html><head><meta charset="utf-8"><title>{{.Title}} — Setup</title><style>{{css}}</style></head>
<body>
<p><a href="{{.BackURL}}">← All devices</a></p>
<h1>{{.Title}}</h1>
{{with .Description}}<p class="sub">{{.}}</p>{{end}}
{{with .Banner}}<div class="banner {{$.BannerKind}}">{{.}}</div>{{end}}
{{if .Configurable}}
<form method="post">
{{template "fields" .Fields}}
<button type="submit">Apply</button>
</form>
{{else}}
<p>This device has no configurable settings.</p>
{{end}}
<h2>Actions</h2>
{{if .Actions}}
<form method="post">
<input type="hidden" name="_form" value="action">
<label><span class="lab">Action</span>
<select name="_action">{{$a := .ActionName}}{{range .Actions}}<option value="{{.}}"{{if eq . $a}} selected{{end}}>{{.}}</option>{{end}}</select></label>
<label><span class="lab">Parameters</span><input type="text" name="_params" value="{{.ActionParams}}"></label>
<button type="submit">Invoke</button>
</form>
{{with .ActionError}}<div class="banner error">{{.}}</div>{{end}}
{{if .ActionName}}{{if not .ActionError}}<pre class="result">{{.ActionResult}}</pre>{{end}}{{end}}
{{else}}<p>This device has no actions.</p>{{end}}
{{if .Reloadable}}
<h2>Reload</h2>
<form method="post">
<input type="hidden" name="_form" value="reload">
<button type="submit">Reload</button>
<span class="help">Re-reads the device's configuration, closes the hardware, and reopens it. The port does not change; a port change needs a restart.</span>
</form>
{{end}}
</body></html>`))

	defaultErrorTmpl = template.Must(template.New("err").Funcs(template.FuncMap{"css": func() template.CSS { return "" }}).Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Setup error</title><style>{{css}}</style></head>
<body><h1>Setup</h1><div class="banner error">{{.}}</div><p><a href="/setup">← Back</a></p></body></html>`))
)

// deviceCount is the number of registered devices.
func (s *Server) deviceCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.order)
}
