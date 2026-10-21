package adapter_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mattjoyce/anam-mcp/internal/adapter"
)

// bpSelection picks every blood-pressure reading.
const bpSelection = `pick(E) :- entry(E, "health.blood-pressure", _, _, _, _, _).`

// TestSaveAndOpenSelection saves a program, gets one code for it however often it is
// saved, and opens the code to the program and the three readings it picks.
func TestSaveAndOpenSelection(t *testing.T) {
	h := start(t, "")
	ctx := context.Background()
	var saved adapter.SelectionOutput
	if err := call(ctx, h.session, "save_selection", map[string]any{"program": bpSelection}, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Code) != 8 || saved.Ref != "sel:"+saved.Code || !strings.HasPrefix(saved.Hash, "sha256:"+saved.Code) {
		t.Fatalf("saved = %+v, want eight hex of the hash, written sel:", saved)
	}
	if saved.TimelineURL != h.daemon.URL+"/?sel="+saved.Code || saved.SelectURL != h.daemon.URL+"/select?sel="+saved.Code ||
		!strings.HasSuffix(saved.HighlightURL, "&selmode=highlight") {
		t.Errorf("addresses = %s, %s, %s", saved.TimelineURL, saved.SelectURL, saved.HighlightURL)
	}
	var again adapter.SelectionOutput
	if err := call(ctx, h.session, "save_selection", map[string]any{"program": "\n" + bpSelection + "\n"}, &again); err != nil {
		t.Fatal(err)
	}
	if again != saved {
		t.Fatalf("the same program saved twice: %+v and %+v", saved, again)
	}

	var opened adapter.OpenSelectionOutput
	if err := call(ctx, h.session, "open_selection", map[string]any{"code": saved.Ref, "projection": "bp"}, &opened); err != nil {
		t.Fatal(err)
	}
	if opened.Selection != saved {
		t.Errorf("opened %+v, saved %+v", opened.Selection, saved)
	}
	if opened.Count != 3 || len(opened.Facets) != 3 || opened.Projection.Name != "bp" || opened.Watermark == 0 {
		t.Fatalf("opened = %+v, want the three readings through bp", opened)
	}
	for i, f := range opened.Facets {
		if f.Type != bpType || f.FacetID == "" || f.FactTime == "" {
			t.Errorf("facet %d = %+v", i, f)
		}
		if i > 0 && f.FactTime < opened.Facets[i-1].FactTime {
			t.Errorf("facets are not in fact-time order: %+v", opened.Facets)
		}
	}
	if !strings.Contains(opened.Note, "query_record") {
		t.Errorf("note %q does not say how to cite", opened.Note)
	}
}

// TestSelectionRefusals: an unknown code is an error, never an empty selection, and
// each refusal says what to change.
func TestSelectionRefusals(t *testing.T) {
	h := start(t, "")
	ctx := context.Background()
	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"open_selection", map[string]any{"code": "sel:00000000", "projection": "bp"}, "no saved selection has that code"},
		{"open_selection", map[string]any{"code": "sel:zz", "projection": "bp"}, "not a selection code"},
		{"open_selection", map[string]any{"code": "sel:00000000", "projection": ""}, "projection is required"},
		{"open_selection", map[string]any{"code": " ", "projection": "bp"}, "code is required"},
		{"save_selection", map[string]any{"program": "  "}, "program is required"},
		{"save_selection", map[string]any{"program": "pick(E) :- "}, "not a program"},
	} {
		err := call(ctx, h.session, c.tool, c.args, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %v = %v, want an error saying %q", c.tool, c.args, err, c.want)
		}
	}
}
