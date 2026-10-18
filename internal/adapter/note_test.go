package adapter_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattjoyce/anam-mcp/internal/adapter"
	"github.com/mattjoyce/anam-mcp/internal/anam"
)

const (
	noteType   = "note.annotation"
	noteSource = "conversation"
	// noteAt is the harness clock, 2026-07-01 00:00 UTC, as the note Definition
	// writes it: a Sydney wall-clock to the minute.
	noteAt = "2026-07-01 10:00"
)

// startNotes runs a daemon whose one importer is the conversation, allowed any
// type as the deployed one is, and connects an adapter that saves notes and facts
// through its token.
func startNotes(t *testing.T) (*Daemon, *mcp.ClientSession) {
	t.Helper()
	d := startDaemon(t, noteSource, nil)
	noteToken := filepath.Join(t.TempDir(), "note-token")
	if err := os.WriteFile(noteToken, []byte(d.importerToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := anam.New(d.URL, d.TokenFile, 10*time.Second, 5*time.Second, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := anam.New(d.URL, noteToken, 10*time.Second, 5*time.Second, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }
	server := adapter.New(client, time.UTC, now, "test").WithNotes(notes).Server()
	return d, session(t, server)
}

// session connects a client to server over in-memory transports.
func session(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// notesOn reads every note on 1 July.
func notesOn(t *testing.T, cs *mcp.ClientSession) []adapter.Evidence {
	t.Helper()
	var r adapter.ReadOutput
	if err := call(context.Background(), cs, "read_record", map[string]any{
		"mode": "timeline", "type": noteType, "from": "2026-07-01", "to": "2026-07-02",
	}, &r); err != nil {
		t.Fatal(err)
	}
	return r.Evidence
}

// The person's words land at once, verbatim, as a note of their own under the
// conversation's source, naming no facet; sending them again changes nothing.
func TestSaveNote_SavesTheWordsOnce(t *testing.T) {
	_, cs := startNotes(t)
	ctx := context.Background()
	words := "Put up the sunshades in the garden at 31 degrees, then a Hydralyte."
	var first adapter.SaveNoteOutput
	if err := call(ctx, cs, "save_note", map[string]any{"text": words}, &first); err != nil {
		t.Fatal(err)
	}
	if first.Outcome != "admitted" || first.At != noteAt || first.Zone != "Australia/Sydney" ||
		!strings.HasPrefix(first.ID, "conversation:") || first.FacetID == "" || first.Saved == "" {
		t.Fatalf("first save = %+v", first)
	}
	var again adapter.SaveNoteOutput
	if err := call(ctx, cs, "save_note", map[string]any{"text": words}, &again); err != nil {
		t.Fatal(err)
	}
	if again.Outcome != "no-op" || again.ID != first.ID || again.FacetID != first.FacetID {
		t.Errorf("the same words again = %+v, want a no-op on %s", again, first.ID)
	}
	got := notesOn(t, cs)
	if len(got) != 1 {
		t.Fatalf("%d notes, want 1", len(got))
	}
	e := got[0]
	p, _ := e.Payload.(map[string]any)
	if e.Source != noteSource || e.FacetID != first.FacetID || p["text"] != words || p["at"] != noteAt {
		t.Errorf("note = source %s, payload %v", e.Source, e.Payload)
	}
	if _, named := p["about"]; named {
		t.Errorf("a note that names nothing carries about: %v", p["about"])
	}
}

// A correction is a new version of the same note: the words change, the identity
// and the moment do not.
func TestSaveNote_CorrectsByANewVersion(t *testing.T) {
	_, cs := startNotes(t)
	ctx := context.Background()
	var first, fixed adapter.SaveNoteOutput
	if err := call(ctx, cs, "save_note", map[string]any{"text": "Rowed 5 km."}, &first); err != nil {
		t.Fatal(err)
	}
	if err := call(ctx, cs, "save_note", map[string]any{"text": "Rowed 6 km.", "id": first.ID, "at": first.At}, &fixed); err != nil {
		t.Fatal(err)
	}
	if fixed.Outcome != "superseded" || fixed.FacetID != first.FacetID {
		t.Fatalf("correction = %+v, want superseded on %s", fixed, first.FacetID)
	}
	got := notesOn(t, cs)
	if len(got) != 1 || got[0].ArtifactVersion != 2 {
		t.Fatalf("notes = %+v, want one at its second version", got)
	}
	if p, _ := got[0].Payload.(map[string]any); p["text"] != "Rowed 6 km." {
		t.Errorf("the corrected note reads %v", p["text"])
	}
}

// What the tool cannot save is refused with the reason, and nothing is written.
func TestSaveNote_Refusals(t *testing.T) {
	_, cs := startNotes(t)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"no words":                {map[string]any{"text": "  "}, "text is required"},
		"a correction with no at": {map[string]any{"text": "x", "id": "conversation:0011223344556677"}, "needs at"},
		"at in another layout":    {map[string]any{"text": "x", "id": "conversation:0011223344556677", "at": "1 July"}, "want the layout"},
		"about that is not an id": {map[string]any{"text": "x", "about": []string{"the heart rate alert"}}, "envelope 0"},
		"names the retry":         {map[string]any{"text": "x", "about": []string{"the heart rate alert"}}, "pass id conversation:"},
	} {
		err := call(ctx, cs, "save_note", tc.args, nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to say %q", name, err, tc.want)
		}
	}
	if got := notesOn(t, cs); len(got) != 0 {
		t.Errorf("%d notes written by refused calls", len(got))
	}
}

// save_note is offered, and described, only when the adapter has the note token.
func TestSaveNote_OfferedOnlyWithItsToken(t *testing.T) {
	_, cs := startNotes(t)
	want := append(slices.Clone(toolNames), "save_note", "save_fact")
	slices.Sort(want)
	if got := listTools(t, cs); !slices.Equal(got, want) {
		t.Errorf("tools with notes = %v", got)
	}
	var d adapter.RecordDescription
	if err := call(context.Background(), cs, "describe_record", nil, &d); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(d.Capabilities, func(s string) bool { return strings.HasPrefix(s, "save_note") }) ||
		slices.ContainsFunc(d.Limitations, func(s string) bool { return strings.HasPrefix(s, "no writes:") }) {
		t.Errorf("with notes, capabilities %v limitations %v", d.Capabilities, d.Limitations)
	}
	h := start(t, "")
	if got := listTools(t, h.session); slices.Contains(got, "save_note") {
		t.Errorf("save_note offered without the note token: %v", got)
	}
}
