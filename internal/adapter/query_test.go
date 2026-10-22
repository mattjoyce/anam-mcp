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

// highProgram is the question "which readings were over 126, and when", as one
// program over the base relations.
const highProgram = `high(T, S) :- entry(E, "health.blood-pressure", T, _, _, _, _), attr(E, "/systolic_blood_pressure/value", S), S > 126.`

// TestDescribeQuery is the catalogue: the relations and the parameter, the lexeme,
// the type's pointers at its versions, and the bundled rules with their text.
func TestDescribeQuery(t *testing.T) {
	h := start(t, "")
	var cat adapter.CatalogueOutput
	if err := call(context.Background(), h.session, "describe_query", nil, &cat); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, r := range cat.Relations {
		kinds[r.Name] = r.Kind
	}
	if kinds["entry"] != "base" || kinds["attr"] != "base" || kinds["instant"] != "parameter" {
		t.Errorf("relations = %+v", cat.Relations)
	}
	if !strings.HasSuffix(cat.Instant.Example, "Z") || cat.Instant.Layout == "" || !strings.Contains(cat.HowTo, "instant/1") || !strings.Contains(cat.Syntax, ":-") {
		t.Errorf("instant %+v, how_to %q", cat.Instant, cat.HowTo)
	}
	var bp *anam.QueryType
	for i := range cat.Types {
		if cat.Types[i].Type == bpType {
			bp = &cat.Types[i]
		}
	}
	if bp == nil || bp.Current != h.version || len(bp.Versions) == 0 {
		t.Fatalf("types do not describe %s v%d: %+v", bpType, h.version, cat.Types)
	}
	declared := false
	for _, v := range bp.Versions {
		if v.Version == h.version {
			declared = strings.Contains(strings.Join(v.Pointers, " "), "/systolic_blood_pressure/value")
		}
	}
	if !declared {
		t.Errorf("the current version does not list the systolic pointer: %+v", bp.Versions)
	}
	if cat.Rules.Name == "" || cat.Rules.Hash == "" || !strings.Contains(cat.Rules.Text, "current(E, At)") {
		t.Errorf("rules = %s v%d %s", cat.Rules.Name, cat.Rules.Version, cat.Rules.Hash)
	}
}

// TestQueryRecord runs the question as one query: the two high readings come back
// as tuples with a receipt that verifies, the resource shows the answer, and a dry
// run derives the same receipt without filing it.
func TestQueryRecord(t *testing.T) {
	h := start(t, "")
	ctx := context.Background()
	args := map[string]any{"projection": bpProjection, "program": highProgram, "relation": "high"}
	var q adapter.QueryOutput
	if err := call(ctx, h.session, "query_record", args, &q); err != nil {
		t.Fatal(err)
	}
	if q.Count != 2 || len(q.Tuples) != 2 || q.Relation != "high" || !q.Published || q.ReceiptHash == "" || q.Receipt == nil {
		t.Fatalf("query = %+v", q)
	}
	if q.Tuples[0][1] != 128.0 || q.Tuples[1][1] != 131.0 {
		t.Errorf("tuples = %v, want the 128 and 131 readings in time order", q.Tuples)
	}
	if q.Receipt.Program == nil || q.Receipt.Program.Relation != "high" || q.Receipt.Program.RuleSetHash != q.Rules.Hash || q.Receipt.ServedHash != q.Hash {
		t.Errorf("receipt = %+v", q.Receipt)
	}
	if q.Projection.Name != bpProjection || q.Projection.Served != 3 || q.EvidenceNotice != adapter.QueryNotice || !strings.Contains(q.Citation, q.ReceiptHash) {
		t.Errorf("projection %+v, notice %q, citation %q", q.Projection, q.EvidenceNotice, q.Citation)
	}

	var v adapter.VerifyOutput
	if err := call(ctx, h.session, "verify_receipt", map[string]any{"receipt_hash": q.ReceiptHash}, &v); err != nil {
		t.Fatal(err)
	}
	if !v.Reproduced || !v.Intact || v.Receipt.Program == nil {
		t.Errorf("a query receipt did not verify: %+v", v)
	}
	res, err := h.session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "anam://receipts/" + q.ReceiptHash})
	if err != nil {
		t.Fatal(err)
	}
	var shown anam.ReceiptShown
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &shown); err != nil {
		t.Fatal(err)
	}
	if shown.Answer == nil || len(shown.Answer.Tuples) != 2 || len(shown.Entries) != 0 {
		t.Errorf("resource = %+v", shown)
	}

	args["dry_run"] = true
	var dry adapter.QueryOutput
	if err := call(ctx, h.session, "query_record", args, &dry); err != nil {
		t.Fatal(err)
	}
	if dry.Published || dry.Count != 2 || !strings.Contains(dry.Citation, "dry run") {
		t.Errorf("dry run = %+v", dry)
	}

	// Pinned to the first answer's watermark, a later fact is not seen.
	h.daemon.Submit(t, reading(h.version, "2026-6-20 06:30:00", 1, 140, 90, ""))
	var pinned, head adapter.QueryOutput
	if err := call(ctx, h.session, "query_record", map[string]any{"projection": bpProjection, "program": highProgram, "relation": "high", "watermark": q.Watermark}, &pinned); err != nil {
		t.Fatal(err)
	}
	if err := call(ctx, h.session, "query_record", map[string]any{"projection": bpProjection, "program": highProgram, "relation": "high", "dry_run": true}, &head); err != nil {
		t.Fatal(err)
	}
	if pinned.Count != 2 || pinned.Hash != q.Hash || head.Count != 3 {
		t.Errorf("pinned %d tuples (%s), head %d; want 2 (%s) and 3", pinned.Count, pinned.Hash, head.Count, q.Hash)
	}
}

// TestQueryBundledRules is the card's shape: current/2 over instants the program
// defines, and an injected comment read back as data in a tuple.
func TestQueryBundledRules(t *testing.T) {
	h := start(t, "")
	ctx := context.Background()
	program := `instant(At) :- entry(_, "health.blood-pressure", At, _, _, _, _).
	            said(T, C) :- current(E, T), entry(E, _, T, _, _, _, _), attr(E, "/comment", C).`
	var q adapter.QueryOutput
	if err := call(ctx, h.session, "query_record", map[string]any{"projection": bpProjection, "program": program, "relation": "said", "dry_run": true}, &q); err != nil {
		t.Fatal(err)
	}
	if q.Count != 1 || q.Tuples[0][1] != injected {
		t.Errorf("said = %v, want the injected comment as data", q.Tuples)
	}
}

// TestQueryRefusals: what cannot run is refused with a message that says what to
// change, the adapter's own checks first and the daemon's words after.
func TestQueryRefusals(t *testing.T) {
	h := start(t, "")
	ctx := context.Background()
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"no projection", map[string]any{"program": highProgram, "relation": "high"}, "send projection (a name describe_record lists) or projection_inline"},
		{"both projections", map[string]any{"projection": bpProjection, "projection_inline": inlineBP(), "program": highProgram, "relation": "high"}, "not both"},
		{"bad inline", map[string]any{"projection_inline": map[string]any{"types": []string{bpType}, "statuses": []string{"admitted"}, "max_classification": "Secret"}, "program": highProgram, "relation": "high"}, "max_classification"},
		{"no program", map[string]any{"projection": bpProjection, "program": " ", "relation": "high"}, "program is required"},
		{"no relation", map[string]any{"projection": bpProjection, "program": highProgram, "relation": ""}, "relation is required"},
		{"two pins", map[string]any{"projection": bpProjection, "program": highProgram, "relation": "high", "as_of": "2026-06-01", "watermark": 1}, "send one"},
		{"unknown projection", map[string]any{"projection": "nope", "program": highProgram, "relation": "high"}, "unknown projection"},
		{"syntax", map[string]any{"projection": bpProjection, "program": `high(E) :- entry(E, _, _, _, _, _, _)`, "relation": "high"}, `expected "."`},
		{"unsafe", map[string]any{"projection": bpProjection, "program": `high(E, X) :- entry(E, _, _, _, _, _, _).`, "relation": "high"}, "not bound"},
		{"undeclared pointer", map[string]any{"projection": bpProjection, "program": `a(E) :- attr(E, "/nope", _).`, "relation": "a"}, "no Definition declares a field at /nope"},
		{"instant lexeme", map[string]any{"projection": bpProjection, "program": `instant("2026-06-15"). a(E) :- current(E, _).`, "relation": "a"}, "write the whole instant"},
		{"wrong relation", map[string]any{"projection": bpProjection, "program": highProgram, "relation": "low"}, "no relation low"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := call(ctx, h.session, "query_record", c.args, nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want an error mentioning %q", err, c.want)
			}
		})
	}
}

// inlineBP is the blood-pressure projection, written in the call.
func inlineBP() map[string]any {
	return map[string]any{
		"types": []string{bpType}, "statuses": []string{"admitted", "endorsed"},
		"max_classification": "Sensitive", "shape": "log",
	}
}

// TestQueryInlineProjection asks the same question as TestQueryRecord with no
// projection named: the types travel in the call, the answer is the same tuples,
// the projection reads as inline v1, and the receipt verifies. A named projection
// asked beside it still answers as before.
func TestQueryInlineProjection(t *testing.T) {
	h := start(t, "")
	ctx := context.Background()
	var named adapter.QueryOutput
	if err := call(ctx, h.session, "query_record", map[string]any{"projection": bpProjection, "program": highProgram, "relation": "high", "dry_run": true}, &named); err != nil {
		t.Fatal(err)
	}
	if named.Projection.Name != bpProjection || named.Projection.Inline || named.Receipt.TemplateInline {
		t.Errorf("a named projection reads as %+v, receipt inline %v", named.Projection, named.Receipt.TemplateInline)
	}

	args := map[string]any{"projection_inline": inlineBP(), "program": highProgram, "relation": "high"}
	var q adapter.QueryOutput
	if err := call(ctx, h.session, "query_record", args, &q); err != nil {
		t.Fatal(err)
	}
	if q.Count != 2 || !q.Published || q.ReceiptHash == "" || q.Receipt == nil {
		t.Fatalf("query = %+v", q)
	}
	if q.Projection.Name != "inline" || q.Projection.Version != 1 || !q.Projection.Inline || q.Projection.Served != 3 || !q.Receipt.TemplateInline {
		t.Errorf("projection %+v, receipt inline %v; want inline v1 serving 3, marked template_inline", q.Projection, q.Receipt.TemplateInline)
	}
	if q.Hash != named.Hash || !strings.Contains(q.Citation, "inline projection") {
		t.Errorf("answer hash %s, named %s; citation %q", q.Hash, named.Hash, q.Citation)
	}
	var v adapter.VerifyOutput
	if err := call(ctx, h.session, "verify_receipt", map[string]any{"receipt_hash": q.ReceiptHash}, &v); err != nil {
		t.Fatal(err)
	}
	if !v.Reproduced || !v.Intact {
		t.Errorf("an inline query receipt did not verify: %+v", v)
	}

	// A window in the call narrows what it serves.
	narrow := inlineBP()
	narrow["from"] = "2026-06-10T00:00:00.000000000Z"
	var w adapter.QueryOutput
	if err := call(ctx, h.session, "query_record", map[string]any{"projection_inline": narrow, "program": highProgram, "relation": "high", "dry_run": true}, &w); err != nil {
		t.Fatal(err)
	}
	if w.Projection.Served >= 3 {
		t.Errorf("from in the call served %d facets, want fewer than 3", w.Projection.Served)
	}
}
