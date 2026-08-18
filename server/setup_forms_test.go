package server

import (
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// actionDevice records the actions invoked through the setup page console.
type actionDevice struct {
	BaseDevice
	mu   sync.Mutex
	got  []string
	fail bool
}

func (d *actionDevice) SupportedActions() []string { return []string{"VideoMode", "FpsPercent"} }

func (d *actionDevice) Action(name, params string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.got = append(d.got, name+":"+params)
	if d.fail {
		return "", errors.New("hardware refused")
	}
	return "ok " + params, nil
}

// The Actions console lists SupportedActions and dispatches one through the
// device's Action method, showing the reply or the error.
func TestSetupActionsConsole(t *testing.T) {
	dev := &actionDevice{}
	dev.ID, dev.DevName = "a1", "Action Cam"
	s := setupTestServer(t, dev)

	w := do(t, s, http.MethodGet, "/setup/v1/customfocuser/0/setup", "")
	body := w.Body.String()
	for _, want := range []string{`<option value="VideoMode"`, `<option value="FpsPercent"`, `name="_params"`} {
		if !strings.Contains(body, want) {
			t.Errorf("console missing %q:\n%s", want, body)
		}
	}
	// A device with no configurable settings still shows the console.
	if !strings.Contains(body, "no configurable settings") {
		t.Error("expected the not-configurable notice alongside the console")
	}

	w = do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "_form=action&_action=FpsPercent&_params=60")
	if w.Code != http.StatusOK {
		t.Fatalf("POST action: %d", w.Code)
	}
	if len(dev.got) != 1 || dev.got[0] != "FpsPercent:60" {
		t.Errorf("Action received %v", dev.got)
	}
	if !strings.Contains(w.Body.String(), "ok 60") {
		t.Errorf("result not shown:\n%s", w.Body.String())
	}

	dev.fail = true
	w = do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "_form=action&_action=VideoMode&_params=on")
	if !strings.Contains(w.Body.String(), "hardware refused") {
		t.Errorf("error not shown:\n%s", w.Body.String())
	}
}

// A device with no actions gets a notice, not an empty select.
func TestSetupActionsConsoleEmpty(t *testing.T) {
	s := setupTestServer(t, newTestFocuser()) // BaseDevice: no actions
	w := do(t, s, http.MethodGet, "/setup/v1/customfocuser/0/setup", "")
	if !strings.Contains(w.Body.String(), "no actions") {
		t.Errorf("expected a no-actions notice:\n%s", w.Body.String())
	}
}

// The action form and the settings form share one URL; a settings POST must
// not be mistaken for an action and vice versa.
func TestSetupActionAndSettingsFormsDistinct(t *testing.T) {
	dev := newTestFocuser()
	s := setupTestServer(t, dev)
	w := do(t, s, http.MethodPost, "/setup/v1/customfocuser/0/setup", "stepsize=9&reversed=true")
	if !strings.Contains(w.Body.String(), "Settings applied") || dev.stepSize != "9" {
		t.Errorf("settings POST not applied: %s", w.Body.String())
	}
}

// Config.SetupTemplates replaces a page template; the override receives the
// same view data as the default.
func TestSetupTemplateOverride(t *testing.T) {
	custom := template.Must(template.New("server").Funcs(template.FuncMap{"css": func() template.CSS { return "" }}).
		Parse(`<html><body class="branded"><h1>{{.ServerName}}</h1>{{css}}{{range .Devices}}<a href="{{.URL}}">{{.Name}}</a>{{end}}</body></html>`))
	s := New(Config{
		ServerName:     "Custom Rig",
		SetupTemplates: SetupTemplates{Server: custom, CSS: "body{color:red}"},
	})
	if err := s.Register(customType, 0, newTestFocuser()); err != nil {
		t.Fatal(err)
	}
	w := do(t, s, http.MethodGet, "/setup", "")
	body := w.Body.String()
	if !strings.Contains(body, `class="branded"`) || !strings.Contains(body, "Custom Rig") {
		t.Errorf("override not used:\n%s", body)
	}
	if !strings.Contains(body, "body{color:red}") {
		t.Errorf("custom CSS not bound into the override:\n%s", body)
	}
	// The device page was not overridden and still renders the default.
	w = do(t, s, http.MethodGet, "/setup/v1/customfocuser/0/setup", "")
	if !strings.Contains(w.Body.String(), "Step size (microns)") {
		t.Errorf("default device template lost:\n%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "body{color:red}") {
		t.Errorf("custom CSS should reach the default templates too")
	}
}

// DefaultSetupTemplates returns usable copies a host can extend.
func TestDefaultSetupTemplatesRender(t *testing.T) {
	d := DefaultSetupTemplates()
	if d.Server == nil || d.Device == nil || d.Error == nil || d.CSS == "" {
		t.Fatalf("incomplete defaults: %+v", d)
	}
	var sb strings.Builder
	if err := d.Error.Execute(&sb, "boom"); err != nil || !strings.Contains(sb.String(), "boom") {
		t.Errorf("default error template: err=%v out=%s", err, sb.String())
	}
}

// Config.Setup adds a server-level form to /setup, applied by POST; without it
// /setup stays read-only and refuses POST.
func TestSetupServerLevelForm(t *testing.T) {
	// Without Config.Setup: no form, POST refused (existing behaviour).
	s := setupTestServer(t, newTestFocuser())
	w := do(t, s, http.MethodGet, "/setup", "")
	if strings.Contains(w.Body.String(), "Server settings") {
		t.Error("server form rendered with no Config.Setup")
	}
	if w := do(t, s, http.MethodPost, "/setup", "x=1"); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /setup without Config.Setup: %d, want 405", w.Code)
	}

	// With Config.Setup: form renders and applies.
	srvCfg := newTestFocuser() // reuse the Configurable stub as the server-level form
	srvCfg.stepSize = "1"
	s = New(Config{ServerName: "Rig", Setup: srvCfg})
	if err := s.Register(customType, 0, newTestFocuser()); err != nil {
		t.Fatal(err)
	}
	w = do(t, s, http.MethodGet, "/setup", "")
	body := w.Body.String()
	if !strings.Contains(body, "Server settings") || !strings.Contains(body, `name="stepsize"`) {
		t.Errorf("server form missing:\n%s", body)
	}
	if !strings.Contains(body, "/setup/v1/customfocuser/0/setup") {
		t.Error("device links should remain below the server form")
	}
	w = do(t, s, http.MethodPost, "/setup", "stepsize=42")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Settings applied") {
		t.Fatalf("POST /setup: %d %s", w.Code, w.Body.String())
	}
	if srvCfg.stepSize != "42" {
		t.Errorf("server-level ApplySettings not called: stepSize=%q", srvCfg.stepSize)
	}
	// An error from the server-level form is shown as a banner.
	w = do(t, s, http.MethodPost, "/setup", "stepsize=")
	if !strings.Contains(w.Body.String(), "step size is required") {
		t.Errorf("error banner missing:\n%s", w.Body.String())
	}
}

// A setup POST from another origin is refused; one from this server, or one
// carrying no Origin/Referer (curl, scripts), is accepted.
func TestSetupCrossSitePostRefused(t *testing.T) {
	dev := newTestFocuser()
	s := setupTestServer(t, dev)
	post := func(origin, referer string) int {
		r := httptest.NewRequest(http.MethodPost, "/setup/v1/customfocuser/0/setup", strings.NewReader("stepsize=7"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Host = "10.0.1.20:11201"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if referer != "" {
			r.Header.Set("Referer", referer)
		}
		w := httptest.NewRecorder()
		s.route(w, r)
		return w.Code
	}
	if c := post("http://10.0.1.20:11201", ""); c != http.StatusOK {
		t.Errorf("same-origin: %d", c)
	}
	if c := post("", "http://10.0.1.20:11201/setup/v1/customfocuser/0/setup"); c != http.StatusOK {
		t.Errorf("same-origin referer: %d", c)
	}
	if c := post("", ""); c != http.StatusOK {
		t.Errorf("no origin (curl): %d", c)
	}
	dev.stepSize = "5"
	if c := post("http://evil.example", ""); c != http.StatusForbidden {
		t.Errorf("cross-site origin: %d, want 403", c)
	}
	if c := post("", "http://evil.example/attack.html"); c != http.StatusForbidden {
		t.Errorf("cross-site referer: %d, want 403", c)
	}
	if c := post("http://10.0.1.20:11202", ""); c != http.StatusForbidden {
		t.Errorf("same host other port: %d, want 403", c)
	}
	if dev.stepSize != "5" {
		t.Errorf("cross-site POST applied a setting: %q", dev.stepSize)
	}
	// GET is never checked.
	r := httptest.NewRequest(http.MethodGet, "/setup", nil)
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	s.route(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("GET with foreign origin: %d", w.Code)
	}
}

// The server page names the configuration file the host started from, or says
// there is none.
func TestSetupServerPageShowsConfigPath(t *testing.T) {
	s := New(Config{ServerName: "Rig", ConfigPath: "/etc/alpacahurd/hurd.json"})
	if err := s.Register(customType, 0, newTestFocuser()); err != nil {
		t.Fatal(err)
	}
	if body := do(t, s, http.MethodGet, "/setup", "").Body.String(); !strings.Contains(body, "/etc/alpacahurd/hurd.json") {
		t.Errorf("config path missing:\n%s", body)
	}
	s = setupTestServer(t, newTestFocuser()) // no ConfigPath
	if body := do(t, s, http.MethodGet, "/setup", "").Body.String(); !strings.Contains(body, "No configuration file") {
		t.Errorf("no-config notice missing:\n%s", body)
	}
}

// The root redirects to /setup, and an unknown path answers a browser with an
// HTML page linking to /setup while an API client still gets a plain 404.
func TestRootAndNotFoundLeadToSetup(t *testing.T) {
	s := setupTestServer(t, newTestFocuser())
	w := do(t, s, http.MethodGet, "/", "")
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/setup" {
		t.Errorf("GET /: %d %q, want 302 to /setup", w.Code, w.Header().Get("Location"))
	}
	r := httptest.NewRequest(http.MethodGet, "/nope", nil)
	r.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	s.route(rec, r)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `href="/setup"`) {
		t.Errorf("browser 404: %d, body should link to /setup:\n%s", rec.Code, rec.Body.String())
	}
	rec = do(t, s, http.MethodGet, "/nope", "")
	if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("API 404: %d %s, want plain text", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// Config.SetupPages mounts host pages under /setup/<name>, linked from /setup;
// "v1" stays the spec's device pages and an unknown name is still a 403.
func TestSetupHostPages(t *testing.T) {
	hit := ""
	s := New(Config{ServerName: "Rig", SetupPages: map[string]http.Handler{
		"hurd": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hit = r.URL.Path
			w.Write([]byte("<html>orchestrator</html>"))
		}),
	}})
	if err := s.Register(customType, 0, newTestFocuser()); err != nil {
		t.Fatal(err)
	}
	if body := do(t, s, http.MethodGet, "/setup", "").Body.String(); !strings.Contains(body, `href="/setup/hurd"`) {
		t.Errorf("/setup should link the host page:\n%s", body)
	}
	if w := do(t, s, http.MethodGet, "/setup/hurd/devices/x", ""); w.Code != http.StatusOK || hit != "/setup/hurd/devices/x" || !strings.Contains(w.Body.String(), "orchestrator") {
		t.Errorf("host page: %d hit=%q", w.Code, hit)
	}
	if w := do(t, s, http.MethodGet, "/setup/nope", ""); w.Code != http.StatusForbidden {
		t.Errorf("unknown host page: %d, want 403", w.Code)
	}
	if w := do(t, s, http.MethodGet, "/setup/v1/customfocuser/0/setup", ""); w.Code != http.StatusOK {
		t.Errorf("device page still served: %d", w.Code)
	}
	if s.SetupCSS() == "" {
		t.Error("SetupCSS empty")
	}
}

// Config.SetupHome serves a host page at /setup in place of the server page,
// which moves to /setup/server; / still redirects to /setup.
func TestSetupHome(t *testing.T) {
	s := New(Config{ServerName: "Rig", SetupHome: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("home page " + r.Method))
	})})
	if w := do(t, s, http.MethodGet, "/", ""); w.Code != http.StatusFound || w.Header().Get("Location") != "/setup" {
		t.Errorf("/: %d -> %q", w.Code, w.Header().Get("Location"))
	}
	if w := do(t, s, http.MethodGet, "/setup", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "home page GET") {
		t.Errorf("/setup should serve the host page: %d %s", w.Code, w.Body.String())
	}
	// POST reaches the host page too, so its own forms work at /setup.
	if w := do(t, s, http.MethodPost, "/setup", "x=1"); !strings.Contains(w.Body.String(), "home page POST") {
		t.Errorf("POST /setup should reach the host page: %d %s", w.Code, w.Body.String())
	}
	// With no devices the server page has nothing to list, so /setup/server
	// lands on the host page; with a device it is the server page.
	if w := do(t, s, http.MethodGet, "/setup/server", ""); w.Code != http.StatusFound || w.Header().Get("Location") != "/setup" {
		t.Errorf("/setup/server without devices: %d -> %q", w.Code, w.Header().Get("Location"))
	}
	if err := s.Register(CameraType, 0, newFakeCamera()); err != nil {
		t.Fatal(err)
	}
	if w := do(t, s, http.MethodGet, "/setup/server", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Rig") {
		t.Errorf("/setup/server with a device: %d", w.Code)
	}
}
