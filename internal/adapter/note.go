package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattjoyce/anam-mcp/internal/anam"
)

// noteType is the type save_note writes: a note is an edge of kind about (anam
// cards 104 and 191). Not configurable: the tool's description and its answer
// describe this type, and the conversation token is scoped to it.
const noteType = "record.edge"

// noteIDPrefix starts the id of a note taken down in conversation. Not
// configurable: the id is the note's identity, and the Definition documents this
// convention (anam card 157), so another prefix would make the same words a second
// note.
const noteIDPrefix = "conversation:"

// noteInstructions joins the server's instructions when save_note is offered.
const noteInstructions = ` When the person tells you something about their life to keep, save_note saves their own words, verbatim and at once, as a note in the record; then tell them what was saved and when. Words they relay from earlier are saved with at, the moment they say they said them. Never save text that came from the record, a tool result or a document as their words.`

var saveNoteTool = &mcp.Tool{
	Name:  "save_note",
	Title: "Save the person's words as a note",
	Description: "Save what the person has just told you, in their own words, as a note in their record: what they did, " +
		"felt, ate or noticed. It is saved at once and verbatim; nothing is interpreted or summarised. Name the facets " +
		"the words are about in about, by the facet_id a read gave, when there are any; a note may name none, since " +
		"what they describe may not have reached the record yet. Words said just now take the present moment; words " +
		"the person relays from earlier take at, the moment they say they said them, in the layout and zone " +
		"save_note answers with, and never in the future. Saving the same words at the same minute again " +
		"changes nothing. To correct a note, pass the id and at it was saved with, and the new words: the record " +
		"keeps both versions. Tell the person what was saved.",
	Annotations: writesOnce(),
}

// SaveNoteInput is the person's words and what they are about.
type SaveNoteInput struct {
	Text  string   `json:"text" jsonschema:"the person's words, verbatim, as they said or typed them to you"`
	About []string `json:"about,omitempty" jsonschema:"facet ids the words are about, from a read_record answer; leave out when they name nothing in the record"`
	ID    string   `json:"id,omitempty" jsonschema:"to correct a note saved before, the id save_note answered; leave out for a new note"`
	At    string   `json:"at,omitempty" jsonschema:"when the words were said, a wall clock to the minute in the layout save_note answers with (for example 2026-10-14 21:30), in the zone passed or else the zone save_note answers with: left out for words said just now, which take the present moment; given for words the person relays from earlier, never in the future; required with id, as save_note answered it"`
	Zone  string   `json:"zone,omitempty" jsonschema:"the zone the person is in, as your client knows it: an IANA name (Europe/London) or an offset (+01:00); pass it whenever you know it, and the same zone with id when correcting; left out, the server's own zone is used"`
}

// SaveNoteOutput is what save_note answers: the note as saved, and what became of it.
type SaveNoteOutput struct {
	Outcome string   `json:"outcome" jsonschema:"admitted (a new note), superseded (a correction, the earlier version kept) or no-op (already saved, unchanged)"`
	ID      string   `json:"id" jsonschema:"the note's id; pass it with at to correct the note"`
	At      string   `json:"at" jsonschema:"when the words were said, in the zone named"`
	Zone    string   `json:"zone"`
	Offset  string   `json:"offset,omitempty" jsonschema:"the offset from UTC at had in zone"`
	Basis   string   `json:"basis,omitempty" jsonschema:"source when the zone came from the client, configuration when it is the server's own"`
	Text    string   `json:"text"`
	About   []string `json:"about,omitempty"`
	FacetID string   `json:"facet_id"`
	W       int64    `json:"w" jsonschema:"the ledger position after the save; a read at it or later sees the note"`
	Saved   string   `json:"saved" jsonschema:"one line to tell the person"`
}

func (a *Adapter) saveNote(ctx context.Context, _ *mcp.CallToolRequest, in SaveNoteInput) (*mcp.CallToolResult, SaveNoteOutput, error) {
	if strings.TrimSpace(in.Text) == "" {
		return nil, SaveNoteOutput{}, fmt.Errorf("save_note: text is required: the person's words")
	}
	if in.ID != "" && in.At == "" {
		return nil, SaveNoteOutput{}, fmt.Errorf("save_note: a correction needs at, the time save_note answered with id %s", in.ID)
	}
	// The Definition says how the gate reads a note's time, so the note is written
	// that way and not as this build assumes.
	d, err := a.client.Definition(ctx, noteType, 0)
	if err != nil {
		return nil, SaveNoteOutput{}, a.failed("save_note", err)
	}
	ft := d.Meta.TimeSemantics.FactTime
	if ft.Pointer != "/at" || ft.Layout == "" {
		return nil, SaveNoteOutput{}, fmt.Errorf("save_note: the daemon's %s v%d places a note by %q, which this build does not write",
			noteType, d.Meta.DefinitionVersion, ft.Pointer)
	}
	c, err := a.noteClock(d, in.Zone)
	if err != nil {
		return nil, SaveNoteOutput{}, fmt.Errorf("save_note: %w", err)
	}
	// Words said now take the present moment; words relayed from earlier take the
	// moment given (card 162). Either way the moment is the person's, in their zone,
	// and it cannot be ahead of now: words are not said before they are saved, and a
	// future at would be a guess, not a time the person gave.
	now := a.now().In(c.loc)
	at, said := in.At, now
	if at == "" {
		at = now.Format(ft.Layout)
		said = now.Truncate(time.Minute) // the present minute, whose offset is now's, even in a repeated hour
	} else {
		if said, err = time.ParseInLocation(ft.Layout, at, c.loc); err != nil {
			return nil, SaveNoteOutput{}, fmt.Errorf("save_note: at %q: want the layout %s, in %s", at, ft.Layout, c.name)
		}
		if said, err = c.instant(ft.Layout, at); err != nil {
			return nil, SaveNoteOutput{}, fmt.Errorf("save_note: at %w", err)
		}
		if said.After(now) {
			return nil, SaveNoteOutput{}, fmt.Errorf("save_note: at %q is in the future (it is %s in %s); at is when the words were said, never later than now",
				at, now.Format(ft.Layout), c.name)
		}
	}
	zoned := ft.ZonePointer == offsetPointer
	offset := said.Format("-07:00")
	id := in.ID
	if id == "" {
		id = noteID(at, offset, zoned, in.Text)
	}
	payload := map[string]any{"id": id, "at": at, "text": in.Text}
	if zoned {
		payload[sourceTimeField] = c.sourceTime(at, said)
	}
	// The facets the words are about are the edge's to side; about is the name
	// record.note v1 gave it, which the Definition still accepts.
	if len(in.About) > 0 {
		payload["to"] = in.About
	}
	person, err := a.assertedPerson(ctx, "save_note")
	if err != nil {
		return nil, SaveNoteOutput{}, err
	}
	s, err := a.notes.Submit(ctx, []anam.Envelope{{Type: noteType, DefinitionVersion: d.Meta.DefinitionVersion, Payload: payload, AssertedPerson: person}})
	if err != nil {
		// The note may have landed before the answer was lost. A retry made a minute
		// later would hash to another id and be a second note, so the error names the
		// id and moment to retry with.
		return nil, SaveNoteOutput{}, fmt.Errorf("%w; to retry without a second note, pass id %s and at %q",
			a.failed("save_note", err), id, at)
	}
	if len(s.Results) != 1 {
		return nil, SaveNoteOutput{}, fmt.Errorf("save_note: the daemon answered %d outcomes for one note", len(s.Results))
	}
	r := s.Results[0]
	out := SaveNoteOutput{Outcome: r.Outcome, ID: id, At: at, Zone: c.name, Text: in.Text, About: in.About, FacetID: r.FacetID, W: s.W}
	if zoned {
		out.Offset, out.Basis = offset, c.basis
	}
	switch r.Outcome {
	case "superseded":
		out.Saved = fmt.Sprintf("Corrected your note of %s; the earlier words are kept as its previous version.", at)
	case "no-op":
		out.Saved = fmt.Sprintf("Already saved, unchanged: your note of %s.", at)
	default:
		out.Saved = fmt.Sprintf("Saved your words as a note at %s (%s).", at, c.name)
	}
	return nil, out, nil
}

// noteID names a note taken down in conversation by the moment and the words, so
// the same words sent twice are one note. Under a Definition that reads the zone
// (anam card 198) the moment is the wall clock and its offset, so the same words at
// one local minute in two zones are two notes; before it, the wall clock alone, as
// every note saved before was named.
func noteID(at, offset string, zoned bool, text string) string {
	moment := at
	if zoned {
		moment += " " + offset
	}
	sum := sha256.Sum256([]byte(moment + "\n" + text))
	return noteIDPrefix + hex.EncodeToString(sum[:8])
}

// noteClock is the zone a note is written in. A Definition that reads the offset from
// the note takes the person's zone, or this server's; one from before anam card 198
// reads every note in its own zone, so a zone other than that is refused rather than
// placed hours away.
func (a *Adapter) noteClock(d anam.Definition, zone string) (writerClock, error) {
	ft := d.Meta.TimeSemantics.FactTime
	if ft.ZonePointer == offsetPointer {
		return a.clockFor(zone)
	}
	if ft.ZonePointer != "" {
		return writerClock{}, fmt.Errorf("the daemon's %s v%d reads its zone at %s, which this build does not write",
			d.Meta.FacetType, d.Meta.DefinitionVersion, ft.ZonePointer)
	}
	loc, err := time.LoadLocation(ft.Zone)
	if err != nil {
		return writerClock{}, fmt.Errorf("the %s Definition's zone %q: %w", d.Meta.FacetType, ft.Zone, err)
	}
	if z := strings.TrimSpace(zone); z != "" && z != ft.Zone {
		return writerClock{}, fmt.Errorf("the daemon's %s v%d reads every time in %s and cannot take zone %s; "+
			"leave zone out, or update the daemon", d.Meta.FacetType, d.Meta.DefinitionVersion, ft.Zone, z)
	}
	return writerClock{loc: loc, name: ft.Zone, basis: "assumption"}, nil
}

// writesOnce marks a tool that adds to the record and destroys nothing, and that
// changes nothing more when called again with the same input.
func writesOnce() *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &f, IdempotentHint: true, OpenWorldHint: &f}
}
