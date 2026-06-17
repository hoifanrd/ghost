package output

import (
	"encoding/json"
	"testing"
)

func TestCapabilitiesCurrent(t *testing.T) {
	c := Current()
	if c.SchemaMax != TrailerSchema {
		t.Errorf("schema_max = %d, want %d", c.SchemaMax, TrailerSchema)
	}
	want := map[string]bool{"supervise": true, "result-trailer": true}
	for _, f := range c.Features {
		delete(want, f)
	}
	if len(want) != 0 {
		t.Errorf("missing required features: %v", want)
	}
}

func TestCapabilitiesJSON(t *testing.T) {
	data, err := json.Marshal(Current())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["schema_max"]; !ok {
		t.Errorf("missing schema_max key: %s", data)
	}
	if _, ok := m["features"]; !ok {
		t.Errorf("missing features key: %s", data)
	}
}
