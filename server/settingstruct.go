package server

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// structField is one exported field of a config struct, with its json name and
// parsed alpaca tag, ready for form rendering or write-back.
type structField struct {
	name  string // form key: the json tag name, else the Go field name
	tag   SettingTag
	value reflect.Value // addressable field value
	kind  reflect.Kind
}

// walkConfigStruct enumerates the exported, non-hidden fields of the struct
// that v points to. v must be a non-nil pointer to a struct. Fields tagged
// json:"-" are skipped. Anonymous (embedded) structs are flattened.
func walkConfigStruct(v any) ([]structField, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("config: want a non-nil pointer to a struct, got %T", v)
	}
	var out []structField
	if err := walkFields(rv.Elem(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func walkFields(sv reflect.Value, out *[]structField) error {
	st := sv.Type()
	for i := 0; i < st.NumField(); i++ {
		sf := st.Field(i)
		fv := sv.Field(i)
		// An embedded struct is flattened whether or not its type name is
		// exported: its promoted exported fields are what the form shows.
		if sf.Anonymous && fv.Kind() == reflect.Struct {
			if err := walkFields(fv, out); err != nil {
				return err
			}
			continue
		}
		if !sf.IsExported() {
			continue
		}
		jsonName, skip := jsonFieldName(sf)
		if skip {
			continue
		}
		tag, err := ParseSettingTag(sf.Tag.Get("alpaca"))
		if err != nil {
			return fmt.Errorf("config field %s: %w", sf.Name, err)
		}
		if tag.Hidden {
			continue
		}
		switch fv.Kind() {
		case reflect.Bool, reflect.String,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
		default:
			return fmt.Errorf("config field %s: kind %s is not supported (bool, string, integer, float)", sf.Name, fv.Kind())
		}
		*out = append(*out, structField{name: jsonName, tag: tag, value: fv, kind: fv.Kind()})
	}
	return nil
}

// jsonFieldName returns the form key for a struct field: the json tag name
// when present, else the Go field name. skip is set for json:"-".
func jsonFieldName(sf reflect.StructField) (name string, skip bool) {
	tag := sf.Tag.Get("json")
	if tag == "-" {
		return "", true
	}
	if n, _, _ := strings.Cut(tag, ","); n != "" {
		return n, false
	}
	return sf.Name, false
}

// FieldsFromStruct renders the exported fields of the config struct v points to
// as setup-form fields, in declaration order. Each field's json tag supplies its
// form key and its alpaca tag supplies label, help, control type, bounds,
// options, and start/live timing (ParseSettingTag).
//
// The control type is derived from the Go kind when the tag does not set it.
// A bool renders as a checkbox, an integer or float as a number, a string with
// options as a select, and any other string as text.
//
// A field named in locked is rendered Locked with lockSource as its Source, so
// a host can pin values it owns (a host config key) and the framework will refuse
// to change them. A field tagged when=start is rendered Locked too, with a
// note that it applies at the next start: the setup page changes live values
// only, and start-time values belong to the config file.
//
// A secret field renders as a password control with an empty value, so the
// current secret is never sent to the browser.
func FieldsFromStruct(v any, locked map[string]bool, lockSource string) ([]SettingField, error) {
	fields, err := walkConfigStruct(v)
	if err != nil {
		return nil, err
	}
	out := make([]SettingField, 0, len(fields))
	for _, f := range fields {
		sf := SettingField{
			Name:    f.name,
			Label:   f.tag.Label,
			Help:    f.tag.Help,
			Type:    f.tag.Type,
			Options: f.tag.Options,
			Value:   fieldValueString(f.value),
		}
		if sf.Label == "" {
			sf.Label = f.name
		}
		if sf.Type == "" {
			sf.Type = controlTypeFor(f.kind, len(f.tag.Options) > 0)
		}
		if f.tag.Secret {
			sf.Type = "password"
			sf.Value = ""
		}
		sf.Constraints = constraintsNote(f.tag)
		switch {
		case locked[f.name]:
			sf.Locked, sf.Source = true, lockSource
		case f.tag.When == WhenStart:
			sf.Locked, sf.Source = true, "applies at the next start; set it in the config file"
		}
		out = append(out, sf)
	}
	return out, nil
}

// ApplyToStruct writes values into the config struct v points to, keyed by the
// same form names FieldsFromStruct produces, and reports the first invalid one.
// Keys not present in values are left untouched, so a caller can apply a
// partial submission. Unknown keys are errors, since a typo would otherwise
// vanish silently.
//
// Values are parsed by the field's Go kind: strconv.ParseBool for bool, base-10
// integers with overflow checking, ParseFloat for floats, and the string as-is.
// A numeric value outside the tag's min/max, or a select value not among the
// tag's options, is rejected before anything is written; on error the struct is
// unchanged.
func ApplyToStruct(v any, values map[string]string) error {
	return applyToStruct(v, values, true)
}

// applyToStruct is ApplyToStruct with range and option validation optional.
// The adapter rebuilds a struct from values it already holds, which came from
// the config file rather than a submission and may sit outside a tag's range
// (a zero meaning "unset" under min=40, say); those must round-trip unchanged.
func applyToStruct(v any, values map[string]string, validate bool) error {
	fields, err := walkConfigStruct(v)
	if err != nil {
		return err
	}
	byName := make(map[string]structField, len(fields))
	for _, f := range fields {
		byName[f.name] = f
	}
	// Validate everything first so a bad value leaves the struct untouched.
	type write struct {
		f   structField
		val reflect.Value
	}
	writes := make([]write, 0, len(values))
	for name, s := range values {
		f, ok := byName[name]
		if !ok {
			return fmt.Errorf("config: unknown setting %q", name)
		}
		val, err := parseFieldValue(f, s, validate)
		if err != nil {
			return err
		}
		writes = append(writes, write{f, val})
	}
	for _, w := range writes {
		w.f.value.Set(w.val)
	}
	return nil
}

func parseFieldValue(f structField, s string, validate bool) (reflect.Value, error) {
	s = strings.TrimSpace(s)
	t := f.value.Type()
	switch f.kind {
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("config: %s: %q is not a boolean", f.name, s)
		}
		return reflect.ValueOf(b).Convert(t), nil
	case reflect.String:
		if validate && len(f.tag.Options) > 0 && !contains(f.tag.Options, s) {
			return reflect.Value{}, fmt.Errorf("config: %s: %q is not one of %s", f.name, s, strings.Join(f.tag.Options, "|"))
		}
		return reflect.ValueOf(s).Convert(t), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("config: %s: %q is not an integer", f.name, s)
		}
		if err := checkRange(f, float64(n), validate); err != nil {
			return reflect.Value{}, err
		}
		nv := reflect.New(t).Elem()
		if nv.OverflowInt(n) {
			return reflect.Value{}, fmt.Errorf("config: %s: %d overflows %s", f.name, n, t)
		}
		nv.SetInt(n)
		return nv, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("config: %s: %q is not a non-negative integer", f.name, s)
		}
		if err := checkRange(f, float64(n), validate); err != nil {
			return reflect.Value{}, err
		}
		nv := reflect.New(t).Elem()
		if nv.OverflowUint(n) {
			return reflect.Value{}, fmt.Errorf("config: %s: %d overflows %s", f.name, n, t)
		}
		nv.SetUint(n)
		return nv, nil
	case reflect.Float32, reflect.Float64:
		x, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("config: %s: %q is not a number", f.name, s)
		}
		if err := checkRange(f, x, validate); err != nil {
			return reflect.Value{}, err
		}
		nv := reflect.New(t).Elem()
		nv.SetFloat(x)
		return nv, nil
	}
	return reflect.Value{}, fmt.Errorf("config: %s: unsupported kind %s", f.name, f.kind)
}

func checkRange(f structField, x float64, validate bool) error {
	if !validate {
		return nil
	}
	if f.tag.Min != nil && x < *f.tag.Min {
		return fmt.Errorf("config: %s: %g is below the minimum %g", f.name, x, *f.tag.Min)
	}
	if f.tag.Max != nil && x > *f.tag.Max {
		return fmt.Errorf("config: %s: %g is above the maximum %g", f.name, x, *f.tag.Max)
	}
	return nil
}

func fieldValueString(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	}
	return ""
}

func controlTypeFor(k reflect.Kind, hasOptions bool) string {
	switch k {
	case reflect.Bool:
		return "checkbox"
	case reflect.String:
		if hasOptions {
			return "select"
		}
		return "text"
	default:
		return "number"
	}
}

// constraintsNote renders a tag's rules as one short line: the numeric bounds,
// the allowed options, and the start/live timing. It is shown separately from
// the help so a reader sees the rule and the explanation as two things.
func constraintsNote(t SettingTag) string {
	var parts []string
	if t.Min != nil {
		parts = append(parts, fmt.Sprintf("min %g", *t.Min))
	}
	if t.Max != nil {
		parts = append(parts, fmt.Sprintf("max %g", *t.Max))
	}
	if len(t.Options) > 0 {
		parts = append(parts, "one of "+strings.Join(t.Options, " | "))
	}
	if t.Secret {
		parts = append(parts, "secret")
	}
	parts = append(parts, t.When)
	return strings.Join(parts, " · ")
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
