package adapter_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mattjoyce/anam-mcp/internal/adapter"
)

// Anam card 198: the chat client passes the zone the person is in, and a note or a
// fact is placed at the offset that wall clock had there, saying so in source_time.
// Without one, the server's own zone is used and said to be configuration. The
// harness clock is 2026-07-01 00:00 UTC: 10:00 in Sydney, 01:00 in London.

func factTime(t *testing.T, e adapter.Evidence) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, e.FactTime)
	if err != nil {
		t.Fatalf("fact time %q: %v", e.FactTime, err)
	}
	return at.UTC()
}

func sourceTime(t *testing.T, e adapter.Evidence) map[string]any {
	t.Helper()
	p, _ := e.Payload.(map[string]any)
	st, _ := p["source_time"].(map[string]any)
	return st
}

func TestSaveNote_TheZoneThePersonIsIn(t *testing.T) {
	_, cs := startNotes(t)
	ctx := context.Background()

	var here adapter.SaveNoteOutput
	if err := call(ctx, cs, "save_note", map[string]any{"text": "In the garden.", "zone": "Europe/London"}, &here); err != nil {
		t.Fatal(err)
	}
	if here.At != "2026-07-01 01:00" || here.Zone != "Europe/London" || here.Offset != "+01:00" || here.Basis != "source" {
		t.Fatalf("a note said now in London = %+v", here)
	}
	var relayed adapter.SaveNoteOutput
	if err := call(ctx, cs, "save_note", map[string]any{"text": "Dinner at six.", "zone": "Europe/London", "at": "2026-06-30 18:00"}, &relayed); err != nil {
		t.Fatal(err)
	}
	var plain adapter.SaveNoteOutput
	if err := call(ctx, cs, "save_note", map[string]any{"text": "No zone given."}, &plain); err != nil {
		t.Fatal(err)
	}
	if plain.At != noteAt || plain.Zone != "Australia/Sydney" || plain.Offset != "+10:00" || plain.Basis != "configuration" {
		t.Fatalf("a note with no zone = %+v, want the server's zone, configuration", plain)
	}
	var r adapter.ReadOutput
	if err := call(ctx, cs, "read_record", map[string]any{"mode": "timeline", "type": noteType, "from": "2026-06-30", "to": "2026-07-02"}, &r); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		at string
		st map[string]any
	}{
		here.FacetID:    {"2026-07-01T00:00:00Z", map[string]any{"value": "2026-07-01 01:00", "zone": "Europe/London", "offset": "+01:00", "basis": "source"}},
		relayed.FacetID: {"2026-06-30T17:00:00Z", map[string]any{"value": "2026-06-30 18:00", "zone": "Europe/London", "offset": "+01:00", "basis": "source"}},
		plain.FacetID:   {"2026-07-01T00:00:00Z", map[string]any{"value": noteAt, "zone": "Australia/Sydney", "offset": "+10:00", "basis": "configuration"}},
	}
	seen := 0
	for _, e := range r.Evidence {
		w, ok := want[e.FacetID]
		if !ok {
			continue
		}
		seen++
		if got := factTime(t, e).Format(time.RFC3339); got != w.at {
			t.Errorf("%s at %s, want %s", e.FacetID, got, w.at)
		}
		st := sourceTime(t, e)
		for k, v := range w.st {
			if st[k] != v {
				t.Errorf("%s source_time %v, want %v", e.FacetID, st, w.st)
				break
			}
		}
	}
	if seen != 3 {
		t.Fatalf("read %d of the 3 notes", seen)
	}

	// One local minute in two zones is two notes.
	var sydney, london adapter.SaveNoteOutput
	_ = call(ctx, cs, "save_note", map[string]any{"text": "Same words.", "at": "2026-06-30 18:00"}, &sydney)
	_ = call(ctx, cs, "save_note", map[string]any{"text": "Same words.", "at": "2026-06-30 18:00", "zone": "Europe/London"}, &london)
	if sydney.ID == "" || sydney.ID == london.ID {
		t.Errorf("the same words at 18:00 in Sydney and London share id %q", sydney.ID)
	}

	// London's clocks went back at 02:00 on 26 October 2025: 01:30 happened twice, and
	// the offset says which.
	err := call(ctx, cs, "save_note", map[string]any{"text": "Late.", "at": "2025-10-26 01:30", "zone": "Europe/London"}, nil)
	if err == nil || !strings.Contains(err.Error(), "happened twice in Europe/London") {
		t.Errorf("a repeated hour with the zone's name: %v, want refused", err)
	}
	var settled adapter.SaveNoteOutput
	if err := call(ctx, cs, "save_note", map[string]any{"text": "Late.", "at": "2025-10-26 01:30", "zone": "+00:00"}, &settled); err != nil || settled.Offset != "+00:00" {
		t.Errorf("the same hour by its offset = %+v, %v", settled, err)
	}
	err = call(ctx, cs, "save_note", map[string]any{"text": "x", "zone": "Mars/Olympus_Mons"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not an IANA zone") {
		t.Errorf("a zone that is none: %v", err)
	}
}

func TestSaveFact_TheZoneThePersonIsIn(t *testing.T) {
	_, cs := startNotes(t)
	ctx := context.Background()
	dose := func(zone string) map[string]any {
		args := map[string]any{"type": "health.medication.dose", "payload": map[string]any{
			"taken_at": "2026-06-30 22:00", "drug": "melatonin", "amount": 5, "unit": "mg"}}
		if zone != "" {
			args["zone"] = zone
		}
		return args
	}
	var london, sydney adapter.SaveFactOutput
	if err := call(ctx, cs, "save_fact", dose("Europe/London"), &london); err != nil {
		t.Fatal(err)
	}
	if err := call(ctx, cs, "save_fact", dose(""), &sydney); err != nil {
		t.Fatal(err)
	}
	if london.FacetID == sydney.FacetID {
		t.Fatalf("22:00 in London and in Sydney are one dose: %s", london.FacetID)
	}
	var r adapter.ReadOutput
	if err := call(ctx, cs, "read_record", map[string]any{"mode": "timeline", "type": "health.medication.dose", "from": "2026-06-30", "to": "2026-07-02"}, &r); err != nil {
		t.Fatal(err)
	}
	got := map[string]adapter.Evidence{}
	for _, e := range r.Evidence {
		got[e.FacetID] = e
	}
	l, s := got[london.FacetID], got[sydney.FacetID]
	if factTime(t, l).Format(time.RFC3339) != "2026-06-30T21:00:00Z" || factTime(t, s).Format(time.RFC3339) != "2026-06-30T12:00:00Z" {
		t.Errorf("London dose at %s, Sydney dose at %s; want 21:00Z and 12:00Z", l.FactTime, s.FactTime)
	}
	if st := sourceTime(t, l); st["zone"] != "Europe/London" || st["offset"] != "+01:00" || st["basis"] != "source" {
		t.Errorf("London dose source_time %v", st)
	}
	if st := sourceTime(t, s); st["zone"] != "Australia/Sydney" || st["offset"] != "+10:00" || st["basis"] != "configuration" {
		t.Errorf("Sydney dose source_time %v", st)
	}
	if p, _ := l.Payload.(map[string]any); !strings.HasSuffix(p["id"].(string), "2026-06-30T22:00+01:00") {
		t.Errorf("London dose id %v, want it to end in its offset", p["id"])
	}
	// A type read only in its Definition's zone cannot be told another.
	err := call(ctx, cs, "save_fact", map[string]any{"type": "health.medication.started", "zone": "Europe/London",
		"payload": map[string]any{"id": "conversation:x", "date": "2026-06-30", "drug": "x", "course": 1}}, nil)
	if err != nil && !strings.Contains(err.Error(), "cannot take zone") && !strings.Contains(err.Error(), "envelope") {
		t.Errorf("a date type with a zone: %v", err)
	}
}
