package adapter_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mattjoyce/anam-mcp/internal/adapter"
	"github.com/mattjoyce/anam-mcp/internal/anam"
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

// Anam card 247: a type whose Definition requires source_time and reads its fact time
// in its own zone (a visit, a lab result, a blood pressure reading) is sent with the
// block saying so, so a chat write is not refused for a field the person never gives.
// The block follows the Definition in force: against a daemon whose Definition does not
// require it (before the records window of card 198) nothing is added, which a closed
// schema would refuse.
func TestSaveFact_ATypeThatRequiresSourceTimeCarriesIt(t *testing.T) {
	d, cs := startNotes(t)
	ctx := context.Background()
	client, err := anam.New(d.URL, d.TokenFile, 10*time.Second, 5*time.Second, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	required := requiresSourceTime(t, client, "health.encounter")

	visit := func(id, date, zone string) map[string]any {
		args := map[string]any{"type": "health.encounter", "payload": map[string]any{"id": id, "date": date, "provider": "A GP"}}
		if zone != "" {
			args["zone"] = zone
		}
		return args
	}
	var plain, named adapter.SaveFactOutput
	if err := call(ctx, cs, "save_fact", visit("gp:1", "2026-06-30 14:00", ""), &plain); err != nil {
		t.Fatal(err)
	}
	if err := call(ctx, cs, "save_fact", visit("gp:2", "2026-06-30 15:00", "Australia/Sydney"), &named); err != nil {
		t.Fatal(err)
	}
	var r adapter.ReadOutput
	if err := call(ctx, cs, "read_record", map[string]any{"mode": "timeline", "type": "health.encounter", "from": "2026-06-30", "to": "2026-07-02"}, &r); err != nil {
		t.Fatal(err)
	}
	got := map[string]adapter.Evidence{}
	for _, e := range r.Evidence {
		got[e.FacetID] = e
	}
	if factTime(t, got[plain.FacetID]).Format(time.RFC3339) != "2026-06-30T04:00:00Z" {
		t.Errorf("a visit said at 14:00 sits at %s, want 04:00Z", got[plain.FacetID].FactTime)
	}
	if !required {
		for id, e := range got {
			if st := sourceTime(t, e); st != nil {
				t.Errorf("%s carries source_time %v against a Definition that does not require it", id, st)
			}
		}
		return
	}
	for name, want := range map[string]struct {
		e            adapter.Evidence
		value, basis string
	}{
		"no zone given":         {got[plain.FacetID], "2026-06-30 14:00", "configuration"},
		"the Definition's zone": {got[named.FacetID], "2026-06-30 15:00", "source"},
	} {
		st := sourceTime(t, want.e)
		if st["value"] != want.value || st["zone"] != "Australia/Sydney" || st["basis"] != want.basis {
			t.Errorf("%s: source_time %v, want %s in Australia/Sydney, basis %s", name, st, want.value, want.basis)
		}
		if _, has := st["offset"]; has {
			t.Errorf("%s: source_time %v carries an offset, which this type's shape has none of", name, st)
		}
	}

	// Another zone would make source_time and the fact time name two instants: refused,
	// naming both zones. So is a wall clock Sydney skips or repeats.
	err = call(ctx, cs, "save_fact", visit("gp:3", "2026-06-30 14:00", "Europe/London"), nil)
	if err == nil || !strings.Contains(err.Error(), "Australia/Sydney") || !strings.Contains(err.Error(), "Europe/London") {
		t.Errorf("a visit in London: %v, want a refusal naming both zones", err)
	}
	for at, word := range map[string]string{"2026-10-04 02:30": "skipped", "2026-04-05 02:30": "twice"} {
		err = call(ctx, cs, "save_fact", visit("gp:4", at, ""), nil)
		if err == nil || !strings.Contains(err.Error(), word) {
			t.Errorf("a visit at %s: %v, want a refusal saying %q", at, err, word)
		}
	}

	// A draw whose report printed no time is given its date alone, as the report printed it.
	var draw adapter.SaveFactOutput
	if err := call(ctx, cs, "save_fact", map[string]any{"type": "health.lab-result", "payload": map[string]any{
		"id": "acl:1/sodium", "collected": "2026-06-30 00:00", "time_unknown": true, "analyte": "Sodium", "value": "137", "lab_number": "1"}}, &draw); err != nil {
		t.Fatal(err)
	}
	if err := call(ctx, cs, "read_record", map[string]any{"mode": "timeline", "type": "health.lab-result", "from": "2026-06-29", "to": "2026-07-02"}, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Evidence) != 1 || sourceTime(t, r.Evidence[0])["value"] != "2026-06-30" {
		t.Errorf("an untimed draw: %+v, want source_time value 2026-06-30", r.Evidence)
	}
}

// requiresSourceTime reports whether the daemon's current Definition of typ lists
// source_time as required.
func requiresSourceTime(t *testing.T, client *anam.Client, typ string) bool {
	t.Helper()
	def, err := client.Definition(context.Background(), typ, 0)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(def.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	return slices.Contains(schema.Required, "source_time")
}

// A server set to another city still reads these types in the Definition's zone, and says
// nothing stands behind it but the Definition: assumption, not the server's setting.
func TestSaveFact_ARequiredSourceTimeNamesTheDefinitionsZoneFromAServerElsewhere(t *testing.T) {
	d, cs := startNotesIn(t, "Europe/London")
	ctx := context.Background()
	client, err := anam.New(d.URL, d.TokenFile, 10*time.Second, 5*time.Second, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !requiresSourceTime(t, client, "health.donation") {
		t.Skip("the daemon's health.donation does not require source_time yet")
	}
	var out adapter.SaveFactOutput
	if err := call(ctx, cs, "save_fact", map[string]any{"type": "health.donation", "payload": map[string]any{
		"id": "lifeblood:1", "date": "2026-06-30 08:00", "outcome": "donated"}}, &out); err != nil {
		t.Fatal(err)
	}
	var r adapter.ReadOutput
	if err := call(ctx, cs, "read_record", map[string]any{"mode": "timeline", "type": "health.donation", "from": "2026-06-29", "to": "2026-07-02"}, &r); err != nil {
		t.Fatal(err)
	}
	if st := sourceTime(t, r.Evidence[0]); st["zone"] != "Australia/Sydney" || st["basis"] != "assumption" {
		t.Errorf("source_time %v, want Australia/Sydney, basis assumption", st)
	}
}
