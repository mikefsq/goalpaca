package devicemain

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikefsq/goalpaca/registry"
	"github.com/mikefsq/goalpaca/server"
	"github.com/mikefsq/goalpaca/sim"
)

// testCfg is a tagged config struct like a real driver's.
type testCfg struct {
	Serial  string `json:"serial,omitempty"  alpaca:"label=Serial,when=start,help=bind by serial"`
	Gain    int    `json:"gain,omitempty"    alpaca:"label=Gain,min=0,max=100"`
	Verbose bool   `json:"verbose,omitempty" alpaca:"label=Verbose"`
}

// testFocuser wraps the sim focuser and records the config it was built with
// and any Reconfigure it receives.
type testFocuser struct {
	*sim.Focuser
	built *testCfg
	live  []*testCfg
}

func (f *testFocuser) Reconfigure(v any) error {
	f.live = append(f.live, v.(*testCfg))
	return nil
}

var lastBuilt *testFocuser

func init() {
	registry.Register(registry.Driver{
		Name:          "dm-focuser",
		Type:          server.FocuserType,
		Description:   "devicemain test focuser",
		ConfigExample: `{ "driver": "dm-focuser", "serial": "F1" }`,
		Config:        func() any { return &testCfg{} },
		New: func(spec registry.Spec) (server.Device, error) {
			var cfg testCfg
			if err := spec.Decode(&cfg); err != nil {
				return nil, err
			}
			f := &testFocuser{Focuser: sim.NewFocuser(), built: &cfg}
			if spec.Name != "" {
				f.DevName = spec.Name
			}
			lastBuilt = f
			return f, nil
		},
	})
}

func TestRunUnknownDriver(t *testing.T) {
	err := RunWith("nope", Options{Args: []string{}, Stdout: io.Discard, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunCheckFromFileAndFlags(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "f1.json")
	os.WriteFile(cfg, []byte(`{
		// a comment
		"driver": "dm-focuser",
		"port": 12345,          /* block */
		"serial": "F1",
		"gain": 40
	}`), 0o644)
	var out bytes.Buffer
	err := RunWith("dm-focuser", Options{Args: []string{"-config", cfg, "-check", "-gain", "77", "-name", "Named"}, Stdout: &out, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if !strings.Contains(out.String(), "focuser/0 on port 12345") || !strings.Contains(out.String(), `"Named"`) {
		t.Errorf("check output: %s", out.String())
	}
	if lastBuilt.built.Serial != "F1" || lastBuilt.built.Gain != 77 {
		t.Errorf("built with %+v: file serial and flag gain should both apply", lastBuilt.built)
	}
	// A flag out of the tag's range is refused before New.
	err = RunWith("dm-focuser", Options{Args: []string{"-config", cfg, "-check", "-gain", "500"}, Stdout: io.Discard, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "above the maximum") {
		t.Errorf("out-of-range flag: err = %v", err)
	}
	// No file, no flags: default port.
	out.Reset()
	if err := RunWith("dm-focuser", Options{Args: []string{"-check"}, Stdout: &out, Stderr: io.Discard, DefaultPort: 4321}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "on port 4321") {
		t.Errorf("default port: %s", out.String())
	}
}

func TestRunSchema(t *testing.T) {
	var out bytes.Buffer
	if err := RunWith("dm-focuser", Options{Args: []string{"-schema", "json"}, Stdout: &out, Stderr: io.Discard}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Driver string
		Fields []struct {
			Name, Type, Constraints, Help string
			StartTime                     bool
		}
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("schema json: %v\n%s", err, out.String())
	}
	if got.Driver != "dm-focuser" || len(got.Fields) != 3 {
		t.Fatalf("schema = %+v", got)
	}
	if got.Fields[0].Name != "serial" || !got.Fields[0].StartTime || got.Fields[1].Constraints != "min 0 · max 100 · live" {
		t.Errorf("fields = %+v", got.Fields)
	}

	out.Reset()
	if err := RunWith("dm-focuser", Options{Args: []string{"-schema", "commented"}, Stdout: &out, Stderr: io.Discard, DefaultPort: 4321}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{`"driver": "dm-focuser"`, `"enable": false`, `// "port": 4321`, `// "serial": ""`, `// "gain": 0`, `// "verbose": false`, "bind by serial"} {
		if !strings.Contains(text, want) {
			t.Errorf("commented schema missing %q:\n%s", want, text)
		}
	}
	// The commented file is itself a loadable device file: driver only, no keys set.
	p := filepath.Join(t.TempDir(), "seed.json")
	os.WriteFile(p, out.Bytes(), 0o644)
	m, err := ReadDeviceFile(p)
	if err != nil {
		t.Fatalf("seed does not load: %v", err)
	}
	if string(m["driver"]) != `"dm-focuser"` || m["gain"] != nil || m["serial"] != nil {
		t.Errorf("seed decoded to %v; only driver and enable should be live", m)
	}
	// Uncommenting one line, and only that, overrides: the line carries its own
	// comma, so no other edit is needed.
	os.WriteFile(p, []byte(strings.Replace(out.String(), `// "gain": 0,`, `"gain": 9,`, 1)), 0o644)
	m, err = ReadDeviceFile(p)
	if err != nil {
		t.Fatalf("uncommented file does not load: %v", err)
	}
	if string(m["gain"]) != "9" {
		t.Errorf("uncommented gain not read: %v", m)
	}
	// Uncommenting the last commented key too (verbose, just above enable) is
	// also valid.
	txt := strings.Replace(out.String(), `// "verbose": false,`, `"verbose": true,`, 1)
	os.WriteFile(p, []byte(txt), 0o644)
	if m, err = ReadDeviceFile(p); err != nil || string(m["verbose"]) != "true" {
		t.Errorf("uncommented last key: %v %v", m, err)
	}

	if err := RunWith("dm-focuser", Options{Args: []string{"-schema", "xml"}, Stdout: io.Discard, Stderr: io.Discard}); err == nil {
		t.Error("bad -schema value accepted")
	}
}

func TestStripComments(t *testing.T) {
	in := `{"a": "http://x//y", // trailing
	/* block
	   over lines */ "b": 1, "c": "/* not a comment */"}`
	var m map[string]any
	if err := json.Unmarshal(StripComments([]byte(in)), &m); err != nil {
		t.Fatalf("stripped text does not parse: %v\n%s", err, StripComments([]byte(in)))
	}
	if m["a"] != "http://x//y" || m["b"] != 1.0 || m["c"] != "/* not a comment */" {
		t.Errorf("m = %v", m)
	}
	// Line count is preserved so decode errors point at the right line.
	if strings.Count(string(StripComments([]byte(in))), "\n") != strings.Count(in, "\n") {
		t.Error("newlines not preserved")
	}
}

func TestRunServesWithSetupForm(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("ALPACA_STATE_DIR", stateRoot)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "main.json")
	port := freePort(t)
	os.WriteFile(cfg, []byte(`{"driver":"dm-focuser","port":`+itoa(port)+`,"serial":"F9"}`), 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunWith("dm-focuser", Options{
			Args: []string{"-config", cfg, "-discovery", "off", "-quiet"}, Stdout: io.Discard, Stderr: io.Discard, Context: ctx,
		})
	}()
	base := "http://127.0.0.1:" + itoa(port)
	waitUp(t, base+"/management/apiversions")

	// Alpaca answers.
	r, err := http.Get(base + "/api/v1/focuser/0/connected")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	// Setup form: serial pinned (from the file), gain and verbose editable.
	r, _ = http.Get(base + "/setup/v1/focuser/0/setup")
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	body := string(b)
	if !strings.Contains(body, `name="serial"`) || !strings.Contains(body, "set in "+cfg) {
		t.Errorf("serial should be pinned to the file:\n%s", body)
	}
	if strings.Count(body, "set in "+cfg) != 1 {
		t.Errorf("only serial should be pinned")
	}
	// /setup names the config file.
	r, _ = http.Get(base + "/setup")
	b, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(b), cfg) {
		t.Errorf("/setup should name the config file")
	}
	// A live change reaches Reconfigure and persists under StateDir/devices/main.json.
	resp, err := http.PostForm(base+"/setup/v1/focuser/0/setup", map[string][]string{"gain": {"55"}, "verbose": {"true"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "applied and saved") {
		t.Fatalf("apply:\n%s", b)
	}
	if n := len(lastBuilt.live); n == 0 || lastBuilt.live[n-1].Gain != 55 || !lastBuilt.live[n-1].Verbose {
		t.Errorf("Reconfigure got %+v", lastBuilt.live)
	}
	stateFile := filepath.Join(stateRoot, "devices", "main.json")
	sb, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("state file at %s: %v", stateFile, err)
	}
	if !strings.Contains(string(sb), `"gain": 55`) {
		t.Errorf("state: %s", sb)
	}
	cancel()
	if err := <-done; err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("RunWith returned %v", err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func itoa(n int) string { return strconv.Itoa(n) }

func waitUp(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r, err := http.Get(url); err == nil {
			r.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s did not come up", url)
}

func TestRunReloadRereadsFile(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("ALPACA_STATE_DIR", stateRoot)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "main.json")
	port := freePort(t)
	os.WriteFile(cfg, []byte(`{"driver":"dm-focuser","port":`+itoa(port)+`,"serial":"F9"}`), 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunWith("dm-focuser", Options{
			Args: []string{"-config", cfg, "-discovery", "off", "-quiet"}, Stdout: io.Discard, Stderr: io.Discard, Context: ctx,
		})
	}()
	base := "http://127.0.0.1:" + itoa(port)
	waitUp(t, base+"/management/apiversions")
	first := lastBuilt
	if first.built.Serial != "F9" {
		t.Fatalf("built with %+v", first.built)
	}
	// Persist a live value, then change the file's start-time key.
	resp, err := http.PostForm(base+"/setup/v1/focuser/0/setup", map[string][]string{"gain": {"41"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	os.WriteFile(cfg, []byte(`{"driver":"dm-focuser","port":`+itoa(port)+`,"serial":"F10","name":"Renamed"}`), 0o644)

	resp, err = http.PostForm(base+"/setup/v1/focuser/0/setup", map[string][]string{"_form": {"reload"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "Reloaded") {
		t.Fatalf("reload page:\n%s", b)
	}
	if lastBuilt == first || lastBuilt.built.Serial != "F10" {
		t.Fatalf("reload built %+v (same object %v)", lastBuilt.built, lastBuilt == first)
	}
	// The persisted gain reached the new device.
	if n := len(lastBuilt.live); n == 0 || lastBuilt.live[n-1].Gain != 41 {
		t.Errorf("persisted gain not re-applied: %+v", lastBuilt.live)
	}
	// Same port, new name.
	r, err := http.Get(base + "/management/v1/configureddevices")
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(b), `"DeviceName":"Renamed"`) {
		t.Errorf("configureddevices after reload: %s", b)
	}
	cancel()
	<-done
}

func TestAfterRegisterHook(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "hooked.json")
	port := freePort(t)
	os.WriteFile(cfg, []byte(`{"driver":"dm-focuser","port":`+itoa(port)+`,"lx200Port":14031}`), 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var gotPort int
	var gotDev server.Device
	done := make(chan error, 1)
	go func() {
		done <- RunWith("dm-focuser", Options{
			Args: []string{"-config", cfg, "-discovery", "off", "-quiet"}, Stdout: io.Discard, Stderr: io.Discard, Context: ctx,
			AfterRegister: func(hctx context.Context, dev func() server.Device, entry map[string]json.RawMessage) error {
				_ = json.Unmarshal(entry["lx200Port"], &gotPort)
				gotDev = dev()
				cancel() // end the server as soon as it starts
				return nil
			},
		})
	}()
	if err := <-done; err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if gotPort != 14031 {
		t.Errorf("hook entry lx200Port = %d, want 14031", gotPort)
	}
	if gotDev == nil || gotDev.Name() == "" {
		t.Errorf("hook device getter returned %v", gotDev)
	}
}

// dm-fe is a test driver with a FrontEnd; the hook records what devicemain
// handed it.
var dmFELast struct {
	ctx   context.Context
	hosts []string
	entry json.RawMessage
	dev   func() server.Device
	calls int
}

func init() {
	registry.Register(registry.Driver{
		Name:          "dm-fe",
		Type:          server.FocuserType,
		Description:   "devicemain front-end test driver",
		ConfigExample: `{ "driver": "dm-fe" }`,
		New: func(spec registry.Spec) (server.Device, error) {
			return sim.NewFocuser(), nil
		},
		FrontEnd: func(ctx context.Context, dev func() server.Device, entry json.RawMessage, hosts []string) error {
			dmFELast.ctx, dmFELast.hosts, dmFELast.entry, dmFELast.dev = ctx, hosts, entry, dev
			dmFELast.calls++
			return nil
		},
	})
}

func TestFrontEndInvocation(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "fe.json")
	port := freePort(t)
	os.WriteFile(cfg, []byte(`{"driver":"dm-fe","port":`+itoa(port)+`,"lx200Port":14039}`), 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunWith("dm-fe", Options{
			Args: []string{"-config", cfg, "-discovery", "off", "-quiet"}, Stdout: io.Discard, Stderr: io.Discard, Context: ctx,
		})
	}()
	waitUp(t, "http://127.0.0.1:"+itoa(port)+"/management/apiversions")
	if dmFELast.calls != 1 {
		t.Fatalf("FrontEnd calls = %d, want 1", dmFELast.calls)
	}
	if len(dmFELast.hosts) != 0 {
		t.Fatalf("hosts = %v, want empty (every interface)", dmFELast.hosts)
	}
	if !strings.Contains(string(dmFELast.entry), `"lx200Port"`) {
		t.Fatalf("entry missing the front-end key: %s", dmFELast.entry)
	}
	if dmFELast.dev() == nil {
		t.Fatal("dev() returned nil for the served device")
	}
	if dmFELast.ctx.Err() != nil {
		t.Fatal("front-end context cancelled while serving")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if dmFELast.ctx.Err() == nil {
		t.Fatal("front-end context should end with the server")
	}
}

func TestDeviceStatePathIsTheSettingsFile(t *testing.T) {
	t.Setenv("ALPACA_STATE_DIR", "/state")
	got := deviceStatePath("rst", "rst")
	want := filepath.Join("/state", "devices", "rst.json")
	if got != want {
		t.Errorf("deviceStatePath = %q, want %q", got, want)
	}
	if deviceStatePath("rst", "") != "" {
		t.Errorf("no instance means no state file, got %q", deviceStatePath("rst", ""))
	}
}
