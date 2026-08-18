package server

import (
	"fmt"
	"strconv"
	"strings"
)

// SettingTag is the parsed form of an `alpaca:"..."` struct tag: the metadata
// a driver attaches to a config field so the framework can render it on the
// device setup page (FieldsFromStruct) and validate a submission
// (ApplyToStruct).
//
// The tag is a comma-separated list of key=value pairs, in any order:
//
//	Serial     string `json:"serial"     alpaca:"label=Serial,help=Factory serial (hex),when=start"`
//	FpsPercent int    `json:"fpsPercent" alpaca:"label=FPS percent,min=40,max=100,when=live"`
//	Mode       string `json:"mode"       alpaca:"options=raw16|raw8"`
//
// Keys:
//
//	label     control label; defaults to the field's json name
//	help      help text rendered under the control
//	type      control override: text, number, checkbox, select, password;
//	          otherwise derived from the Go kind
//	min, max  numeric bounds, rendered and validated
//	options   choices for a select, separated by |
//	when      start or live (default live); a start field applies at the next
//	          start and renders read-only
//	secret    render as a password control and never echo the value
//	hidden    omit the field from the form
//
// A value may contain a comma when the pair is written key='value with, comma'
// or key="..."; both quote styles are stripped. Whitespace around keys and
// values is trimmed.
type SettingTag struct {
	Label   string
	Help    string
	Type    string
	Min     *float64
	Max     *float64
	Options []string
	When    string
	Secret  bool
	Hidden  bool
}

// Values for SettingTag.When.
const (
	WhenLive  = "live"  // applies immediately through Reconfigure
	WhenStart = "start" // applies at the next start; renders read-only
)

// ParseSettingTag parses the value of an `alpaca` struct tag. An empty tag is
// valid and yields the zero SettingTag with When defaulted to live. Unknown keys
// and malformed pairs are errors, so a typo in a driver's tag is caught by that
// driver's tests rather than silently ignored.
func ParseSettingTag(tag string) (SettingTag, error) {
	t := SettingTag{When: WhenLive}
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return t, nil
	}
	pairs, err := splitTagPairs(tag)
	if err != nil {
		return t, err
	}
	for _, p := range pairs {
		key, val, hasVal := strings.Cut(p, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		val = unquote(strings.TrimSpace(val))
		switch key {
		case "label":
			t.Label = val
		case "help":
			t.Help = val
		case "type":
			switch strings.ToLower(val) {
			case "text", "number", "checkbox", "select", "password":
				t.Type = strings.ToLower(val)
			default:
				return t, fmt.Errorf("alpaca tag: type %q is not text|number|checkbox|select|password", val)
			}
		case "min", "max":
			f, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return t, fmt.Errorf("alpaca tag: %s=%q is not a number", key, val)
			}
			if key == "min" {
				t.Min = &f
			} else {
				t.Max = &f
			}
		case "options":
			for _, o := range strings.Split(val, "|") {
				if o = strings.TrimSpace(o); o != "" {
					t.Options = append(t.Options, o)
				}
			}
			if len(t.Options) == 0 {
				return t, fmt.Errorf("alpaca tag: options is empty")
			}
		case "when":
			switch strings.ToLower(val) {
			case WhenLive, WhenStart:
				t.When = strings.ToLower(val)
			default:
				return t, fmt.Errorf("alpaca tag: when=%q is not start|live", val)
			}
		case "secret", "hidden":
			b := true
			if hasVal {
				var err error
				if b, err = strconv.ParseBool(val); err != nil {
					return t, fmt.Errorf("alpaca tag: %s=%q is not a boolean", key, val)
				}
			}
			if key == "secret" {
				t.Secret = b
			} else {
				t.Hidden = b
			}
		case "":
			return t, fmt.Errorf("alpaca tag: empty key in %q", p)
		default:
			return t, fmt.Errorf("alpaca tag: unknown key %q", key)
		}
	}
	if t.Min != nil && t.Max != nil && *t.Min > *t.Max {
		return t, fmt.Errorf("alpaca tag: min %g exceeds max %g", *t.Min, *t.Max)
	}
	return t, nil
}

// splitTagPairs splits on commas that are not inside single or double quotes.
func splitTagPairs(s string) ([]string, error) {
	var (
		pairs []string
		cur   strings.Builder
		quote rune
	)
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			cur.WriteRune(r)
		case r == ',':
			pairs = append(pairs, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("alpaca tag: unterminated quote in %q", s)
	}
	pairs = append(pairs, cur.String())
	return pairs, nil
}

// unquote strips one matching pair of single or double quotes.
func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '\'' && v[len(v)-1] == '\'') || (v[0] == '"' && v[len(v)-1] == '"') {
			return v[1 : len(v)-1]
		}
	}
	return v
}
