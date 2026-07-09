package registry

import (
	"encoding/json"
	"strings"
	"testing"

	server "github.com/mikefsq/goalpaca/server"
	"github.com/mikefsq/goalpaca/sim"
)

func testDriver(name string) Driver {
	return Driver{
		Name:          name,
		Type:          server.FocuserType,
		Description:   "test driver",
		ConfigExample: `{ "driver": "` + name + `" }`,
		New: func(Spec) (server.Device, error) {
			return sim.NewFocuser(), nil
		},
	}
}

func TestRegisterLookupAll(t *testing.T) {
	Register(testDriver("t-alpha"))
	Register(testDriver("t-beta"))

	if _, ok := Lookup("t-alpha"); !ok {
		t.Fatal("Lookup(t-alpha) not found")
	}
	// Lookup is case-insensitive (configs historically accepted any case).
	if _, ok := Lookup("T-Alpha"); !ok {
		t.Fatal("Lookup(T-Alpha) not found (case-insensitive)")
	}
	if _, ok := Lookup("t-missing"); ok {
		t.Fatal("Lookup(t-missing) unexpectedly found")
	}

	all := All()
	var names []string
	for _, d := range all {
		names = append(names, d.Name)
	}
	ia, ib := -1, -1
	for i, n := range names {
		if n == "t-alpha" {
			ia = i
		}
		if n == "t-beta" {
			ib = i
		}
	}
	if ia < 0 || ib < 0 {
		t.Fatalf("All() missing test drivers: %v", names)
	}
	if ia > ib {
		t.Fatalf("All() not sorted by name: %v", names)
	}
}

func TestRegisterPanics(t *testing.T) {
	mustPanic := func(name string, d Driver) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s: Register did not panic", name)
			}
		}()
		Register(d)
	}
	mustPanic("empty name", Driver{New: testDriver("x").New})
	mustPanic("nil New", Driver{Name: "t-nilnew"})

	Register(testDriver("t-dup"))
	mustPanic("duplicate", testDriver("t-dup"))
	// Duplicate detection is case-insensitive, like Lookup.
	mustPanic("duplicate (case)", testDriver("T-DUP"))
}

func TestSpecDecode(t *testing.T) {
	type cfg struct {
		Serial string `json:"serial,omitempty"`
		Index  int    `json:"index,omitempty"`
	}

	// Driver fields decode; host-owned common keys are stripped, not errors.
	spec := Spec{Driver: "t", Raw: json.RawMessage(`{
		"driver": "t", "name": "N", "enable": false, "port": 11200,
		"indi": true, "lx200Port": 4030,
		"aperture": 130, "apertureArea": 0.01, "focalLength": 1000,
		"guiderAperture": 50, "guiderFocalLength": 200, "guideRate": 0.5,
		"serial": "abc", "index": 2
	}`)}
	var c cfg
	if err := spec.Decode(&c); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if c.Serial != "abc" || c.Index != 2 {
		t.Fatalf("Decode got %+v", c)
	}

	// Common keys strip case-insensitively (Go's decoder matches them loosely,
	// so the strip must too).
	spec = Spec{Driver: "t", Raw: json.RawMessage(`{ "Driver": "t", "NAME": "N", "serial": "s" }`)}
	c = cfg{}
	if err := spec.Decode(&c); err != nil {
		t.Fatalf("Decode (mixed-case common keys): %v", err)
	}
	if c.Serial != "s" {
		t.Fatalf("Decode got %+v", c)
	}

	// A typo'd driver key is an error, not silently ignored.
	spec = Spec{Driver: "t", Raw: json.RawMessage(`{ "driver": "t", "serail": "abc" }`)}
	err := spec.Decode(&cfg{})
	if err == nil {
		t.Fatal("Decode accepted unknown key \"serail\"")
	}
	if !strings.Contains(err.Error(), "serail") {
		t.Fatalf("Decode error does not name the bad key: %v", err)
	}

	// A wrongly-typed driver key is an error.
	spec = Spec{Driver: "t", Raw: json.RawMessage(`{ "driver": "t", "index": "two" }`)}
	if err := spec.Decode(&cfg{}); err == nil {
		t.Fatal("Decode accepted wrongly-typed \"index\"")
	}

	// Empty/omitted Raw decodes as an empty entry.
	if err := (Spec{Driver: "t"}).Decode(&cfg{}); err != nil {
		t.Fatalf("Decode (nil Raw): %v", err)
	}
}

func TestCommonKeysCopy(t *testing.T) {
	ks := CommonKeys()
	if len(ks) == 0 {
		t.Fatal("CommonKeys empty")
	}
	ks[0] = "mutated"
	if CommonKeys()[0] == "mutated" {
		t.Fatal("CommonKeys returned the internal slice, not a copy")
	}
}
