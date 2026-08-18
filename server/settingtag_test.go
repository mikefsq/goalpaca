package server

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func fp(f float64) *float64 { return &f }

func TestParseSettingTag(t *testing.T) {
	cases := []struct {
		name string
		tag  string
		want SettingTag
	}{
		{"empty", "", SettingTag{When: WhenLive}},
		{"label only", "label=Serial", SettingTag{Label: "Serial", When: WhenLive}},
		{"help", "help=Factory serial (hex)", SettingTag{Help: "Factory serial (hex)", When: WhenLive}},
		{"type", "type=password", SettingTag{Type: "password", When: WhenLive}},
		{"type is case-insensitive", "type=Number", SettingTag{Type: "number", When: WhenLive}},
		{"min max", "min=40,max=100", SettingTag{Min: fp(40), Max: fp(100), When: WhenLive}},
		{"negative min", "min=-10.5", SettingTag{Min: fp(-10.5), When: WhenLive}},
		{"options", "options=raw16|raw8", SettingTag{Options: []string{"raw16", "raw8"}, When: WhenLive}},
		{"options trims blanks", "options= a | b |", SettingTag{Options: []string{"a", "b"}, When: WhenLive}},
		{"when start", "when=start", SettingTag{When: WhenStart}},
		{"when live explicit", "when=live", SettingTag{When: WhenLive}},
		{"secret bare", "secret", SettingTag{Secret: true, When: WhenLive}},
		{"secret false", "secret=false", SettingTag{When: WhenLive}},
		{"hidden bare", "hidden", SettingTag{Hidden: true, When: WhenLive}},
		{"whitespace tolerated", " label = FPS percent , min = 1 ", SettingTag{Label: "FPS percent", Min: fp(1), When: WhenLive}},
		{"quoted comma single", "help='a, b',label=X", SettingTag{Help: "a, b", Label: "X", When: WhenLive}},
		{"quoted comma double", `help="a, b"`, SettingTag{Help: "a, b", When: WhenLive}},
		{"key case-insensitive", "LABEL=x,WHEN=START", SettingTag{Label: "x", When: WhenStart}},
		{"everything", "label=L,help=H,type=select,min=0,max=9,options=a|b,when=start,secret",
			SettingTag{Label: "L", Help: "H", Type: "select", Min: fp(0), Max: fp(9), Options: []string{"a", "b"}, When: WhenStart, Secret: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseSettingTag(c.tag)
			if err != nil {
				t.Fatalf("ParseSettingTag(%q): %v", c.tag, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseSettingTag(%q)\n got %s\nwant %s", c.tag, tagString(got), tagString(c.want))
			}
		})
	}
}

func TestParseSettingTagErrors(t *testing.T) {
	cases := []struct {
		tag, wantErr string
	}{
		{"bogus=1", "unknown key"},
		{"=1", "empty key"},
		{"type=slider", "not text|number"},
		{"min=abc", "not a number"},
		{"max=", "not a number"},
		{"options=", "options is empty"},
		{"options=|", "options is empty"},
		{"when=later", "not start|live"},
		{"secret=maybe", "not a boolean"},
		{"min=10,max=1", "min 10 exceeds max 1"},
		{"help='unterminated", "unterminated quote"},
	}
	for _, c := range cases {
		t.Run(c.tag, func(t *testing.T) {
			_, err := ParseSettingTag(c.tag)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("ParseSettingTag(%q): err = %v, want mention of %q", c.tag, err, c.wantErr)
			}
		})
	}
}

// tagString renders a SettingTag with pointer values dereferenced, for
// readable test failures.
func tagString(t SettingTag) string {
	var b strings.Builder
	b.WriteString("{Label:" + t.Label + " Help:" + t.Help + " Type:" + t.Type)
	if t.Min != nil {
		b.WriteString(" Min:" + strconv.FormatFloat(*t.Min, 'g', -1, 64))
	}
	if t.Max != nil {
		b.WriteString(" Max:" + strconv.FormatFloat(*t.Max, 'g', -1, 64))
	}
	b.WriteString(" Options:" + strings.Join(t.Options, "|") + " When:" + t.When)
	if t.Secret {
		b.WriteString(" Secret")
	}
	if t.Hidden {
		b.WriteString(" Hidden")
	}
	b.WriteString("}")
	return b.String()
}
