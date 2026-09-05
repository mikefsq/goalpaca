package server

import (
	"strings"
	"testing"
)

// everyKind exercises each supported Go kind plus the tag features that change
// rendering: label, help, min/max, options, when=start, secret, hidden, json:"-",
// an untagged field, and an embedded struct.
type everyKind struct {
	embedded

	Enabled bool    `json:"enabled"    alpaca:"label=Enabled,help=Turns it on"`
	Name    string  `json:"name"       alpaca:"label=Name"`
	Mode    string  `json:"mode"       alpaca:"options=raw16|raw8"`
	Count   int     `json:"count"      alpaca:"min=1,max=10"`
	Small   int8    `json:"small"`
	Big     uint64  `json:"big"`
	Ratio   float64 `json:"ratio"      alpaca:"min=0"`
	Serial  string  `json:"serial"     alpaca:"when=start"`
	Token   string  `json:"token"      alpaca:"secret"`
	Ghost   string  `json:"ghost"      alpaca:"hidden"`
	Skipped string  `json:"-"`
	NoTag   string
	private int
}

type embedded struct {
	Nested int `json:"nested"`
}

func TestFieldsFromStruct(t *testing.T) {
	cfg := &everyKind{
		embedded: embedded{Nested: 7},
		Enabled:  true, Name: "cam", Mode: "raw8", Count: 3, Small: -2, Big: 1 << 40,
		Ratio: 0.5, Serial: "abc", Token: "s3cret", Ghost: "boo", NoTag: "plain",
	}
	fields, err := FieldsFromStruct(cfg, map[string]bool{"name": true}, "set in hurd.conf")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]SettingField{}
	var order []string
	for _, f := range fields {
		by[f.Name] = f
		order = append(order, f.Name)
	}
	wantOrder := []string{"nested", "enabled", "name", "mode", "count", "small", "big", "ratio", "serial", "token", "NoTag"}
	if strings.Join(order, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("field order = %v, want %v", order, wantOrder)
	}
	if _, ok := by["ghost"]; ok {
		t.Error("hidden field rendered")
	}
	if _, ok := by["Skipped"]; ok {
		t.Error("json:\"-\" field rendered")
	}
	if _, ok := by["private"]; ok {
		t.Error("unexported field rendered")
	}

	check := func(name, typ, value string) {
		t.Helper()
		f := by[name]
		if f.Type != typ || f.Value != value {
			t.Errorf("%s: type %q value %q, want %q %q", name, f.Type, f.Value, typ, value)
		}
	}
	check("nested", "number", "7")
	check("enabled", "checkbox", "true")
	check("name", "text", "cam")
	check("mode", "select", "raw8")
	check("count", "number", "3")
	check("small", "number", "-2")
	check("big", "number", "1099511627776")
	check("ratio", "number", "0.5")
	check("serial", "text", "abc")
	check("token", "password", "") // secret never echoed
	check("NoTag", "text", "plain")

	if by["enabled"].Label != "Enabled" || by["enabled"].Help != "Turns it on" {
		t.Errorf("label/help not carried: %+v", by["enabled"])
	}
	if by["count"].Label != "count" {
		t.Errorf("default label should be the json name, got %q", by["count"].Label)
	}
	if got := by["count"].Constraints; got != "min 1 · max 10 · live" {
		t.Errorf("count constraints = %q", got)
	}
	if got := by["ratio"].Constraints; got != "min 0 · live" {
		t.Errorf("ratio constraints = %q", got)
	}
	if got := by["mode"].Constraints; got != "one of raw16 | raw8 · live" {
		t.Errorf("mode constraints = %q", got)
	}
	if got := by["serial"].Constraints; got != "start" {
		t.Errorf("serial constraints = %q", got)
	}
	if by["enabled"].Help != "Turns it on" {
		t.Errorf("help must stay separate from constraints: %q", by["enabled"].Help)
	}
	if got := by["mode"].Options; strings.Join(got, "|") != "raw16|raw8" {
		t.Errorf("options = %v", got)
	}
	if f := by["name"]; !f.Locked || f.Source != "set in hurd.conf" {
		t.Errorf("host-locked field: %+v", f)
	}
	if f := by["serial"]; !f.Locked || !strings.Contains(f.Source, "next start") {
		t.Errorf("when=start field should be locked with a start note: %+v", f)
	}
	if by["count"].Locked {
		t.Error("live field should not be locked")
	}
}

func TestFieldsFromStructRejectsBadInput(t *testing.T) {
	if _, err := FieldsFromStruct(everyKind{}, nil, ""); err == nil {
		t.Error("non-pointer accepted")
	}
	var nilp *everyKind
	if _, err := FieldsFromStruct(nilp, nil, ""); err == nil {
		t.Error("nil pointer accepted")
	}
	type badTag struct {
		X int `alpaca:"bogus=1"`
	}
	if _, err := FieldsFromStruct(&badTag{}, nil, ""); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("bad tag: err = %v", err)
	}
	type badKind struct {
		X []int `json:"x"`
	}
	if _, err := FieldsFromStruct(&badKind{}, nil, ""); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("bad kind: err = %v", err)
	}
}

func TestApplyToStruct(t *testing.T) {
	cfg := &everyKind{Name: "old", Count: 5}
	err := ApplyToStruct(cfg, map[string]string{
		"nested":  "9",
		"enabled": "true",
		"name":    "new",
		"mode":    "raw16",
		"count":   "10",
		"small":   "-128",
		"big":     "18446744073709551615",
		"ratio":   "1.25",
		"serial":  "def",
		"token":   "t",
		"NoTag":   "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Nested != 9 || !cfg.Enabled || cfg.Name != "new" || cfg.Mode != "raw16" ||
		cfg.Count != 10 || cfg.Small != -128 || cfg.Big != 1<<64-1 || cfg.Ratio != 1.25 ||
		cfg.Serial != "def" || cfg.Token != "t" || cfg.NoTag != "x" {
		t.Errorf("struct after apply: %+v", cfg)
	}
}

func TestApplyToStructPartial(t *testing.T) {
	cfg := &everyKind{Name: "keep", Count: 5}
	if err := ApplyToStruct(cfg, map[string]string{"count": "7"}); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "keep" || cfg.Count != 7 {
		t.Errorf("partial apply changed more than asked: %+v", cfg)
	}
}

func TestApplyToStructRejects(t *testing.T) {
	cases := []struct {
		name    string
		values  map[string]string
		wantErr string
	}{
		{"unknown key", map[string]string{"nope": "1"}, "unknown setting"},
		{"bad bool", map[string]string{"enabled": "maybe"}, "not a boolean"},
		{"bad int", map[string]string{"count": "x"}, "not an integer"},
		{"below min", map[string]string{"count": "0"}, "below the minimum 1"},
		{"above max", map[string]string{"count": "11"}, "above the maximum 10"},
		{"int8 overflow", map[string]string{"small": "200"}, "overflows int8"},
		{"negative uint", map[string]string{"big": "-1"}, "non-negative"},
		{"bad float", map[string]string{"ratio": "abc"}, "not a number"},
		{"float below min", map[string]string{"ratio": "-0.1"}, "below the minimum 0"},
		{"not an option", map[string]string{"mode": "raw12"}, "not one of raw16|raw8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &everyKind{Name: "keep", Count: 5, Mode: "raw8"}
			err := ApplyToStruct(cfg, c.values)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want mention of %q", err, c.wantErr)
			}
			if cfg.Name != "keep" || cfg.Count != 5 || cfg.Mode != "raw8" {
				t.Errorf("struct changed on error: %+v", cfg)
			}
		})
	}
}

func TestApplyToStructAtomic(t *testing.T) {
	cfg := &everyKind{Name: "keep", Count: 5}
	err := ApplyToStruct(cfg, map[string]string{"name": "changed", "count": "999"})
	if err == nil {
		t.Fatal("expected range error")
	}
	if cfg.Name != "keep" || cfg.Count != 5 {
		t.Errorf("partial write on error: %+v", cfg)
	}
}
