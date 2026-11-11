package registry

import (
	"encoding/json"
	"testing"

	server "github.com/mikefsq/goalpaca/server"
)

// An Extra kind inherits the entry's driver keys, its blocks override them, the primary's
// name stays out, and a non-Default kind serves nothing unless the file names its key.
func TestExtraBlocks(t *testing.T) {
	newDev := func(Spec) (server.Device, error) { return nil, nil }
	oc := Kind{Type: server.ObservingConditionsType, MultiKey: "observingconditions", New: newDev, Default: true}
	sm := Kind{Type: server.SafetyMonitorType, MultiKey: "safetymonitors", New: newDev}
	d := Driver{Name: "hub", Type: server.SwitchType, New: newDev, Extra: []Kind{oc, sm}}

	flat := json.RawMessage(`{"driver":"hub","name":"Hub","serial":"A1","port":11217}`)
	got, err := ExtraBlocks(d, oc, flat)
	if err != nil || len(got) != 1 || got[0].Name != "" {
		t.Fatalf("flat default kind: %+v %v", got, err)
	}
	var m map[string]any
	_ = json.Unmarshal(got[0].Raw, &m)
	if m["serial"] != "A1" || m["name"] != nil {
		t.Errorf("flat block raw = %s, want serial kept and name dropped", got[0].Raw)
	}
	if got, _ := ExtraBlocks(d, sm, flat); len(got) != 0 {
		t.Errorf("opt-in kind on a flat entry: %d blocks, want 0", len(got))
	}

	arr := json.RawMessage(`{"driver":"hub","serial":"A1","observingconditions":[{"name":"Weather","enable":false},{"serial":"B2"}],"safetymonitors":[{}]}`)
	got, err = ExtraBlocks(d, oc, arr)
	if err != nil || len(got) != 2 {
		t.Fatalf("array: %+v %v", got, err)
	}
	if got[0].Name != "Weather" || got[0].Enable == nil || *got[0].Enable {
		t.Errorf("block 0 = %+v, want name Weather, enable false", got[0])
	}
	_ = json.Unmarshal(got[1].Raw, &m)
	if m["serial"] != "B2" || m["safetymonitors"] != nil {
		t.Errorf("block 1 raw = %s, want serial overridden and other kind's array dropped", got[1].Raw)
	}
	if ks := d.Kinds(); len(ks) != 3 || ks[0].Type != server.SwitchType || d.StateSuffix(ks[0]) != "" || d.StateSuffix(oc) != ".observingconditions" {
		t.Errorf("Kinds/StateSuffix wrong: %+v", ks)
	}
}
