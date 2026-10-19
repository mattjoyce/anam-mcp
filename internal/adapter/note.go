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

// noteType is the type save_note writes. Not configurable: the tool's description
// and its answer describe this type, and the conversation token is scoped to it.
const noteType = "record.note"

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
	At    string   `json:"at,omitempty" jsonschema:"when the words were said, in the layout and zone save_note answers with (a Sydney wall clock to the minute, for example 2026-10-14 21:30): left out for words said just now, which take the present moment; given for words the person relays from earlier, never in the future; required with id, as save_note answered it"`
}

// SaveNoteOutput is what save_note answers: the note as saved, and what became of it.
type SaveNoteOutput struct {
	Outcome string   `json:"outcome" jsonschema:"admitted (a new note), superseded (a correction, the earlier version kept) or no-op (already saved, unchanged)"`
	ID      string   `json:"id" jsonschema:"the note's id; pass it with at to correct the note"`
	At      string   `json:"at" jsonschema:"when the words were said, in the zone named"`
	Zone    string   `json:"zone"`
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
	loc, err := time.LoadLocation(ft.Zone)
	if err != nil {
		return nil, SaveNoteOutput{}, fmt.Errorf("save_note: the %s Definition's zone %q: %w", noteType, ft.Zone, err)
	}
	// Words said now take the present moment; words relayed from earlier take the
	// moment given (card 162). Either way the moment is the person's, in the
	// Definition's zone, and it cannot be ahead of now: words are not said before
	// they are saved, and a future at would be a guess, not a time the person gave.
	now := a.now().In(loc)
	at := in.At
	if at == "" {
		at = now.Format(ft.Layout)
	} else {
		said, err := time.ParseInLocation(ft.Layout, at, loc)
		if err != nil {
			return nil, SaveNoteOutput{}, fmt.Errorf("save_note: at %q: want the layout %s, in %s", at, ft.Layout, ft.Zone)
		}
		if said.After(now) {
			return nil, SaveNoteOutput{}, fmt.Errorf("save_note: at %q is in the future (it is %s in %s); at is when the words were said, never later than now",
				at, now.Format(ft.Layout), ft.Zone)
		}
	}
	id := in.ID
	if id == "" {
		id = noteID(at, in.Text)
	}
	payload := map[string]any{"id": id, "at": at, "text": in.Text}
	if len(in.About) > 0 {
		payload["about"] = in.About
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
	out := SaveNoteOutput{Outcome: r.Outcome, ID: id, At: at, Zone: ft.Zone, Text: in.Text, About: in.About, FacetID: r.FacetID, W: s.W}
	switch r.Outcome {
	case "superseded":
		out.Saved = fmt.Sprintf("Corrected your note of %s; the earlier words are kept as its previous version.", at)
	case "no-op":
		out.Saved = fmt.Sprintf("Already saved, unchanged: your note of %s.", at)
	default:
		out.Saved = fmt.Sprintf("Saved your words as a note at %s (%s).", at, ft.Zone)
	}
	return nil, out, nil
}

// noteID names a note taken down in conversation by the moment and the words, so
// the same words sent twice are one note.
func noteID(at, text string) string {
	sum := sha256.Sum256([]byte(at + "\n" + text))
	return noteIDPrefix + hex.EncodeToString(sum[:8])
}

// writesOnce marks a tool that adds to the record and destroys nothing, and that
// changes nothing more when called again with the same input.
func writesOnce() *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &f, IdempotentHint: true, OpenWorldHint: &f}
}
