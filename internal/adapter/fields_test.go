package adapter_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattjoyce/anam-mcp/internal/adapter"
	"github.com/mattjoyce/anam-mcp/internal/anam"
)

// A read that names fields answers a frame (anam card 138): the provenance columns
// then the fields, one row per fact, nothing else of the payload, and no evidence
// entries. Published, the receipt pins the fields, verifies, and the resource shows
// the frame.
func TestFieldsAnswerAFrame(t *testing.T) {
	h := start(t, "")
	ctx := context.Background()

	var r adapter.ReadOutput
	err := call(ctx, h.session, "read_record", map[string]any{
		"mode": "timeline", "type": bpType, "from": "2026-06-01", "to": "2026-06-30",
		"fields": []string{"/systolic_blood_pressure/value", "/comment"},
	}, &r)
	if err != nil {
		t.Fatal(err)
	}
	if r.Frame == nil || len(r.Evidence) != 0 || r.Shown != 3 {
		t.Fatalf("read = frame %v, evidence %d, shown %d", r.Frame, len(r.Evidence), r.Shown)
	}
	wantCols := "facet_id type fact_time source status classification content_hash /systolic_blood_pressure/value /comment"
	if got := strings.Join(r.Frame.Columns, " "); got != wantCols {
		t.Errorf("columns = %q\nwant      %q", got, wantCols)
	}
	if len(r.Frame.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(r.Frame.Rows))
	}
	var systolic []float64
	var comments []any
	for _, row := range r.Frame.Rows {
		if len(row) != len(r.Frame.Columns) || row[1] != bpType || row[3] != bpSource {
			t.Errorf("row %v", row)
		}
		systolic = append(systolic, row[7].(float64))
		comments = append(comments, row[8])
	}
	if systolic[0] != 128 || systolic[1] != 131 || systolic[2] != 125 {
		t.Errorf("systolic = %v", systolic)
	}
	// The second reading carries the injected comment, as data; the others have
	// none, and a field a fact lacks is null.
	if comments[0] != nil || comments[1] != injected || comments[2] != nil {
		t.Errorf("comments = %v", comments)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "diastolic") {
		t.Errorf("a field not named reached the answer: %s", b)
	}

	// A field the schema does not declare is refused by name, before any read.
	err = call(ctx, h.session, "read_record", map[string]any{
		"mode": "timeline", "type": bpType, "fields": []string{"/nope"},
	}, &r)
	if err == nil || !strings.Contains(err.Error(), "/nope") {
		t.Errorf("an undeclared field: %v", err)
	}

	// Published, the frame is what the receipt names.
	var p adapter.ReadOutput
	err = call(ctx, h.session, "read_record", map[string]any{
		"mode": "timeline", "projection": bpProjection, "type": bpType, "from": "2026-06-01", "to": "2026-06-30",
		"fields": []string{"/systolic_blood_pressure/value"},
	}, &p)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Published || p.Frame == nil || p.Receipt == nil || p.Receipt.Query == nil {
		t.Fatalf("published read = %+v", p)
	}
	if strings.Join(p.Receipt.Query.Fields, " ") != "/systolic_blood_pressure/value" || len(p.Receipt.FacetIDs) != 3 {
		t.Errorf("receipt query = %+v, facets %d", p.Receipt.Query, len(p.Receipt.FacetIDs))
	}
	var v adapter.VerifyOutput
	if err := call(ctx, h.session, "verify_receipt", map[string]any{"receipt_hash": p.ReceiptHash}, &v); err != nil {
		t.Fatal(err)
	}
	if !v.Reproduced || !v.Intact {
		t.Errorf("a fields receipt did not verify: %+v", v)
	}
	res, err := h.session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "anam://receipts/" + p.ReceiptHash})
	if err != nil {
		t.Fatal(err)
	}
	var shown anam.ReceiptShown
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &shown); err != nil {
		t.Fatal(err)
	}
	if shown.Frame == nil || len(shown.Frame.Rows) != 3 || len(shown.Entries) != 0 {
		t.Errorf("resource = frame %v, %d entries", shown.Frame, len(shown.Entries))
	}
}
