package adapter_test

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattjoyce/anam-mcp/internal/adapter"
)

// weightType is a bundled type with a Sydney wall-clock fact time, so a fact
// saved at noon there is on 1 July in UTC too, where the harness reads days.
const weightType = "health.weight"

func weightsOn(t *testing.T, cs *mcp.ClientSession) []adapter.Evidence {
	t.Helper()
	var r adapter.ReadOutput
	if err := call(context.Background(), cs, "read_record", map[string]any{
		"mode": "timeline", "type": weightType, "from": "2026-07-01", "to": "2026-07-02",
	}, &r); err != nil {
		t.Fatal(err)
	}
	return r.Evidence
}

// A fact the person asserts lands as a facet of its type under the conversation's
// source, with its fields verbatim; the same fact again is a no-op, and changed
// fields under the same identity are a new version (anam card 177).
func TestSaveFact_SavesATypedFactOnce(t *testing.T) {
	_, cs := startNotes(t)
	ctx := context.Background()
	payload := map[string]any{"id": "conversation:weight/2026-07-01", "date": "2026-07-01 12:00", "kg": 80.5}
	var first adapter.SaveFactOutput
	if err := call(ctx, cs, "save_fact", map[string]any{"type": weightType, "payload": payload}, &first); err != nil {
		t.Fatal(err)
	}
	if first.Outcome != "admitted" || first.Type != weightType || first.DefinitionVersion == 0 ||
		first.FacetID == "" || first.ArtifactVersion != 1 || first.W == 0 || first.Saved == "" {
		t.Fatalf("first save = %+v", first)
	}
	var again adapter.SaveFactOutput
	if err := call(ctx, cs, "save_fact", map[string]any{"type": weightType, "payload": payload}, &again); err != nil {
		t.Fatal(err)
	}
	if again.Outcome != "no-op" || again.FacetID != first.FacetID {
		t.Errorf("the same fact again = %+v, want a no-op on %s", again, first.FacetID)
	}
	got := weightsOn(t, cs)
	if len(got) != 1 {
		t.Fatalf("%d weights, want 1", len(got))
	}
	e := got[0]
	p, _ := e.Payload.(map[string]any)
	if e.Source != noteSource || e.FacetID != first.FacetID || p["kg"] != 80.5 || p["date"] != "2026-07-01 12:00" {
		t.Errorf("fact = source %s, payload %v", e.Source, e.Payload)
	}

	payload["kg"] = 81.0
	var fixed adapter.SaveFactOutput
	if err := call(ctx, cs, "save_fact", map[string]any{"type": weightType, "payload": payload}, &fixed); err != nil {
		t.Fatal(err)
	}
	if fixed.Outcome != "superseded" || fixed.FacetID != first.FacetID || fixed.ArtifactVersion != 2 {
		t.Fatalf("correction = %+v, want superseded on %s at version 2", fixed, first.FacetID)
	}
	if got := weightsOn(t, cs); len(got) != 1 || got[0].ArtifactVersion != 2 {
		t.Fatalf("weights = %+v, want one at its second version", got)
	}
}

// What the tool cannot save is refused with the reason, naming what to ask the
// person for, and nothing is written.
func TestSaveFact_Refusals(t *testing.T) {
	_, cs := startNotes(t)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"no type":         {map[string]any{"type": " ", "payload": map[string]any{"kg": 80}}, "type is required"},
		"no payload":      {map[string]any{"type": weightType, "payload": map[string]any{}}, "payload is required"},
		"an unknown type": {map[string]any{"type": "health.mood", "payload": map[string]any{"id": "x"}}, "admits no type health.mood"},
		"a missing field": {map[string]any{"type": weightType, "payload": map[string]any{"id": "x", "date": "2026-07-01 12:00"}}, "needs kg; ask the person"},
		"a date in another layout": {map[string]any{"type": weightType,
			"payload": map[string]any{"id": "x", "date": "1 July", "kg": 80}}, "want the layout 2006-01-02 15:04, in Australia/Sydney"},
		"a field the schema refuses": {map[string]any{"type": weightType,
			"payload": map[string]any{"id": "x", "date": "2026-07-01 12:00", "kg": "eighty"}}, "envelope 0"},
	} {
		err := call(ctx, cs, "save_fact", tc.args, nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to say %q", name, err, tc.want)
		}
	}
	if got := weightsOn(t, cs); len(got) != 0 {
		t.Errorf("%d weights written by refused calls", len(got))
	}
}

// describe_type shows a type's handling guidance where it has one, so a writer
// reads it before save_fact (anam card 166).
func TestDescribeType_ShowsTheGuidance(t *testing.T) {
	h := start(t, "")
	ctx := context.Background()
	var med adapter.TypeDescription
	if err := call(ctx, h.session, "describe_type", map[string]any{"type": "health.medication-started"}, &med); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(med.Guidance, "generic name") {
		t.Errorf("health.medication-started guidance = %q, want the drug naming rule", med.Guidance)
	}
	whole, _ := med.Definition.(map[string]any)
	if _, has := whole["handling"]; !has {
		t.Errorf("the Definition as published lacks handling: %v", whole)
	}
	var weight adapter.TypeDescription
	if err := call(ctx, h.session, "describe_type", map[string]any{"type": weightType}, &weight); err != nil {
		t.Fatal(err)
	}
	if weight.Guidance != "" {
		t.Errorf("health.weight guidance = %q, want none", weight.Guidance)
	}
}
