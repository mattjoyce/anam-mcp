package anam_test

import (
	"encoding/json"
	"testing"

	"github.com/mattjoyce/anam-mcp/internal/anam"
)

// TestProjectionRefWire: a name travels as a string, an inline template as an
// object, and each reads back as it was written.
func TestProjectionRefWire(t *testing.T) {
	cases := []struct {
		name string
		ref  anam.ProjectionRef
		wire string
	}{
		{"named", anam.Named("bp"), `"bp"`},
		{"inline", anam.ProjectionRef{Inline: &anam.InlineProjection{
			Types: []string{"health.weight"}, Statuses: []string{"admitted"}, MaxClassification: "Normal", Shape: "log",
		}}, `{"types":["health.weight"],"statuses":["admitted"],"max_classification":"Normal","shape":"log"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := json.Marshal(c.ref)
			if err != nil || string(b) != c.wire {
				t.Fatalf("marshal = %s (%v), want %s", b, err, c.wire)
			}
			var back anam.ProjectionRef
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatal(err)
			}
			again, _ := json.Marshal(back)
			if string(again) != c.wire {
				t.Errorf("round trip = %s, want %s", again, c.wire)
			}
		})
	}
	both := anam.ProjectionRef{Name: "bp", Inline: &anam.InlineProjection{}}
	if _, err := json.Marshal(both); err == nil {
		t.Error("a reference that is named and inline marshalled")
	}
	var r anam.ProjectionRef
	if err := json.Unmarshal([]byte(`7`), &r); err == nil {
		t.Error("a number read as a projection")
	}
}
