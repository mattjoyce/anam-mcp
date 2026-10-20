package adapter_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mattjoyce/anam-mcp/internal/adapter"
)

// pagedBound is the daemon's max_read_bytes for the paging tests: a few blood
// pressure entries a page, so a month of daily readings takes several.
const pagedBound = "ANAM_MAX_READ_BYTES=2500"

// startPaged runs a daemon under a small read bound with a reading on each of the
// twelve days of June from the 2nd (the 1st, a Sydney morning, is May in UTC),
// and connects a client.
func startPaged(t *testing.T) *harness {
	t.Helper()
	d := startDaemon(t, bpSource, []string{bpType}, pagedBound)
	v := d.CurrentVersion(t, bpType)
	var readings []Envelope
	for day := 2; day <= 13; day++ {
		readings = append(readings, reading(v, fmt.Sprintf("2026-6-%d 06:30:00", day), 1, float64(120+day), 80, ""))
	}
	d.Submit(t, readings...)
	return &harness{daemon: d, version: v, session: connect(t, d.URL, d.TokenFile, "")}
}

// A published read over the daemon's bound comes back as a first page that says
// so, with the cursor; read_receipt follows the cursor to the end, and the pages
// together are exactly the receipt's facet_ids, in order (anam card 92).
func TestPagedReadIsNamedAndFollowed(t *testing.T) {
	h := startPaged(t)
	ctx := context.Background()

	var r adapter.ReadOutput
	err := call(ctx, h.session, "read_record", map[string]any{
		"mode": "timeline", "projection": bpProjection, "type": bpType, "from": "2026-06-01", "to": "2026-06-30",
	}, &r)
	if err != nil {
		t.Fatal(err)
	}
	if r.Page == nil || r.Page.NextCursor == "" {
		t.Fatalf("a read over the bound answered without a next page: page %+v", r.Page)
	}
	if r.Shown != 12 || r.Page.TotalCount != 12 || r.Page.Offset != 0 ||
		r.Page.ReturnedCount != len(r.Evidence) || r.Page.ReturnedCount >= 12 || r.Page.ReturnedCount == 0 {
		t.Fatalf("first page: shown %d, evidence %d, page %+v", r.Shown, len(r.Evidence), r.Page)
	}
	want := fmt.Sprintf("facts 1 to %d of 12", r.Page.ReturnedCount)
	for _, w := range []string{"partial", want, "read_receipt", r.ReceiptHash, r.Page.NextCursor} {
		if !strings.Contains(r.PageNotice, w) {
			t.Errorf("page notice %q lacks %q", r.PageNotice, w)
		}
	}

	got := facetIDs(r.Evidence)
	cursor, pages := r.Page.NextCursor, 1
	for cursor != "" {
		var p adapter.ReceiptOutput
		if err := call(ctx, h.session, "read_receipt", map[string]any{"receipt_hash": r.ReceiptHash, "cursor": cursor}, &p); err != nil {
			t.Fatalf("page %d: %v", pages+1, err)
		}
		pages++
		if p.Page == nil || p.Page.Offset != len(got) || p.Page.ReturnedCount != len(p.Evidence) || p.Receipt != nil {
			t.Fatalf("page %d: page %+v, evidence %d, receipt %v", pages, p.Page, len(p.Evidence), p.Receipt)
		}
		if p.Page.NextCursor == "" && !strings.HasPrefix(p.PageNotice, "last page") {
			t.Errorf("last page notice = %q", p.PageNotice)
		}
		got = append(got, facetIDs(p.Evidence)...)
		cursor = p.Page.NextCursor
		if pages > 12 {
			t.Fatal("the cursor never ran out")
		}
	}
	if !slices.Equal(got, r.Receipt.FacetIDs) {
		t.Errorf("pages hold %v\nreceipt names %v", got, r.Receipt.FacetIDs)
	}

	// Without a cursor, read_receipt answers the receipt and its first page.
	var first adapter.ReceiptOutput
	if err := call(ctx, h.session, "read_receipt", map[string]any{"receipt_hash": r.ReceiptHash}, &first); err != nil {
		t.Fatal(err)
	}
	if first.Receipt == nil || first.Page == nil || first.Page.Offset != 0 ||
		!slices.Equal(facetIDs(first.Evidence), facetIDs(r.Evidence)) || !strings.Contains(first.PageNotice, "partial") {
		t.Errorf("first page again: receipt %v, page %+v, notice %q", first.Receipt, first.Page, first.PageNotice)
	}
}

// A cursor another receipt issued is refused, and the error says what to pass
// instead.
func TestForeignCursorIsRefused(t *testing.T) {
	h := startPaged(t)
	ctx := context.Background()
	read := func(from, to string) adapter.ReadOutput {
		t.Helper()
		var r adapter.ReadOutput
		err := call(ctx, h.session, "read_record", map[string]any{
			"mode": "timeline", "projection": bpProjection, "type": bpType, "from": from, "to": to,
		}, &r)
		if err != nil {
			t.Fatal(err)
		}
		if r.Page == nil || r.Page.NextCursor == "" {
			t.Fatalf("read %s to %s answered whole; the test needs a second page", from, to)
		}
		return r
	}
	a, b := read("2026-06-01", "2026-06-30"), read("2026-06-02", "2026-06-30")
	if a.ReceiptHash == b.ReceiptHash {
		t.Fatal("two different reads filed one receipt")
	}
	err := call(ctx, h.session, "read_receipt", map[string]any{"receipt_hash": a.ReceiptHash, "cursor": b.Page.NextCursor}, nil)
	if err == nil {
		t.Fatal("another receipt's cursor was accepted")
	}
	for _, w := range []string{"read_receipt:", "not one this receipt issued", "next_cursor unchanged", "omit cursor"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("%q lacks %q", err, w)
		}
	}
}

// An unpublished read over the bound is still refused whole, and the refusal says
// a projection would page it.
func TestUnpublishedReadOverTheBoundNamesPaging(t *testing.T) {
	h := startPaged(t)
	err := call(context.Background(), h.session, "read_record", map[string]any{"mode": "timeline", "type": bpType}, nil)
	if err == nil || !strings.Contains(err.Error(), "too large") || !strings.Contains(err.Error(), "name a projection") {
		t.Errorf("an unpublished read over the bound: %v", err)
	}
}

// One fact over the bound alone cannot be paged: a publish holding it files
// nothing, and the error names the facet and what to do.
func TestOneFactOverTheBoundIsNamed(t *testing.T) {
	h := startPaged(t)
	h.daemon.Submit(t, reading(h.version, "2026-7-2 06:30:00", 1, 140, 90, strings.Repeat("a long comment ", 300)))
	err := call(context.Background(), h.session, "read_record", map[string]any{
		"mode": "timeline", "projection": bpProjection, "type": bpType, "from": "2026-07-01", "to": "2026-07-31",
	}, nil)
	if err == nil {
		t.Fatal("a read holding one fact over the bound was answered")
	}
	for _, w := range []string{"read_record:", "one fact, facet ", "no page can carry it", "nothing was published", "Name fields"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("%q lacks %q", err, w)
		}
	}
}

func facetIDs(ev []adapter.Evidence) []string {
	ids := make([]string, 0, len(ev))
	for _, e := range ev {
		ids = append(ids, e.FacetID)
	}
	return ids
}
