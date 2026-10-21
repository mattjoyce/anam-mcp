package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattjoyce/anam-mcp/internal/anam"
)

// factInstructions joins the server's instructions, after noteInstructions, when
// save_fact is offered.
const factInstructions = ` When the person asks you to put a fact on the record as a type (a dose, a reading, an event, a correction), save_fact saves it as a facet of that type under their own source. Call describe_type first and read its guidance, write the fields the person gave or confirmed, and ask for a field you would otherwise have to guess. Never save as their fact something you inferred, read in a document or found in the record; their words go to save_note.`

var saveFactTool = &mcp.Tool{
	Name:  "save_fact",
	Title: "Save a fact the person asserts, as a typed facet",
	Description: "Save a fact the person has asked you to record, as a facet of the type they mean, under their own " +
		"source: a medicine started, a weight, a one-off event, their correction of what a device recorded. The " +
		"payload is the type's fields exactly as describe_type names them, in the layouts its Definition declares; " +
		"read the type's guidance first, since it says how writers of the type agree on names and ids. Use only " +
		"values the person gave or confirmed, and ask for a missing one rather than guess it. The fact is validated " +
		"against the Definition and refused with the field named when it does not fit. Its identity is composed " +
		"from the fields the Definition names, so the same fact saved again changes nothing, and changed fields " +
		"under the same identity are a new version, the earlier one kept. It is marked as asserted by the person. " +
		"Tell the person what was saved.",
	Annotations: writesOnce(),
}

// SaveFactInput is the type and the fields of the fact the person asserts.
type SaveFactInput struct {
	Type    string         `json:"type" jsonschema:"the facet type, dotted and namespaced, as describe_record lists it"`
	Payload map[string]any `json:"payload" jsonschema:"the fields of the facet, exactly as the type's Definition names them (describe_type), holding the values the person gave or confirmed"`
	Zone    string         `json:"zone,omitempty" jsonschema:"the zone the person is in, as your client knows it: an IANA name (Europe/London) or an offset (+01:00); pass it whenever you know it. A type timed by a wall clock is read in it, and the server adds source_time saying so; left out, the server's own zone is used"`
}

// SaveFactOutput is what save_fact answers: the fact as saved, and what became of it.
type SaveFactOutput struct {
	Outcome           string `json:"outcome" jsonschema:"admitted (a new facet), superseded (a new version of one saved before, the earlier kept) or no-op (already saved, unchanged)"`
	Type              string `json:"type"`
	DefinitionVersion int    `json:"definition_version" jsonschema:"the Definition version the fact was shaped to"`
	FacetID           string `json:"facet_id"`
	ArtifactVersion   int    `json:"artifact_version" jsonschema:"the facet's version after the save; 1 for a new facet"`
	W                 int64  `json:"w" jsonschema:"the ledger position after the save; a read at it or later sees the fact"`
	Saved             string `json:"saved" jsonschema:"one line to tell the person"`
}

func (a *Adapter) saveFact(ctx context.Context, _ *mcp.CallToolRequest, in SaveFactInput) (*mcp.CallToolResult, SaveFactOutput, error) {
	if strings.TrimSpace(in.Type) == "" {
		return nil, SaveFactOutput{}, errors.New("save_fact: type is required: a facet type, as describe_record lists them")
	}
	if len(in.Payload) == 0 {
		return nil, SaveFactOutput{}, fmt.Errorf("save_fact: payload is required: the fields of a %s, as describe_type names them", in.Type)
	}
	d, err := a.client.Definition(ctx, in.Type, 0)
	var ae *anam.Error
	switch {
	case errors.As(err, &ae) && ae.Status == http.StatusNotFound:
		return nil, SaveFactOutput{}, fmt.Errorf("save_fact: the record admits no type %s; describe_record lists the types it does", in.Type)
	case err != nil:
		return nil, SaveFactOutput{}, a.failed("save_fact", err)
	}
	// source_time is this server's to add, from the zone, not the person's to give.
	if err := a.addSourceTime(d, in.Payload, in.Zone); err != nil {
		return nil, SaveFactOutput{}, fmt.Errorf("save_fact: %w", err)
	}
	// Two checks the gate would make anyway, made here so the refusal names what to
	// ask the person for, before anything travels.
	if missing := missingRequired(d, in.Payload); len(missing) > 0 {
		return nil, SaveFactOutput{}, fmt.Errorf("save_fact: a %s needs %s; ask the person, do not guess", in.Type, strings.Join(missing, ", "))
	}
	if err := checkFactTime(d, in.Payload); err != nil {
		return nil, SaveFactOutput{}, fmt.Errorf("save_fact: %w", err)
	}
	person, err := a.assertedPerson(ctx, "save_fact")
	if err != nil {
		return nil, SaveFactOutput{}, err
	}
	env := anam.Envelope{Type: in.Type, DefinitionVersion: d.Meta.DefinitionVersion, Payload: in.Payload, AssertedPerson: person}
	s, err := a.notes.Submit(ctx, []anam.Envelope{env})
	if err != nil {
		// The identity is the Definition's, from the fields, so the same call again
		// is safe: a fact that landed before the answer was lost is a no-op.
		return nil, SaveFactOutput{}, fmt.Errorf("%w; the same call again is safe, the identity is composed from the fields",
			a.failed("save_fact", err))
	}
	if len(s.Results) != 1 {
		return nil, SaveFactOutput{}, fmt.Errorf("save_fact: the daemon answered %d outcomes for one fact", len(s.Results))
	}
	r := s.Results[0]
	out := SaveFactOutput{Outcome: r.Outcome, Type: in.Type, DefinitionVersion: d.Meta.DefinitionVersion,
		FacetID: r.FacetID, ArtifactVersion: r.ArtifactVersion, W: s.W}
	switch r.Outcome {
	case "superseded":
		out.Saved = fmt.Sprintf("Saved a new version of the %s you recorded before; the earlier version is kept.", in.Type)
	case "no-op":
		out.Saved = fmt.Sprintf("Already in the record, unchanged: that %s.", in.Type)
	default:
		out.Saved = fmt.Sprintf("Saved the %s as your own fact.", in.Type)
	}
	return nil, out, nil
}

// missingRequired names the schema's required fields the payload lacks, in the
// schema's order, but two a writer need not give: source_time, which addSourceTime
// gives a payload it can and the gate names when it could not, and the id a type's
// identity_template composes, which the gate fills (anam card 188). A schema without
// required names none.
func missingRequired(d anam.Definition, payload map[string]any) []string {
	var s struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(d.Schema, &s); err != nil {
		return nil
	}
	composed := ""
	if d.Meta.IdentityTemplate != "" && len(d.Meta.IdentityKey) == 1 {
		composed = strings.TrimPrefix(d.Meta.IdentityKey[0], "/")
	}
	var missing []string
	for _, f := range s.Required {
		if _, ok := payload[f]; !ok && f != sourceTimeField && f != composed {
			missing = append(missing, f)
		}
	}
	return missing
}

// addSourceTime gives a payload of a type whose fact time reads the offset from the
// fact (anam card 198) the source_time that says how its wall clock was read: the
// value as given, the person's zone or this server's, and the offset that wall clock
// had there, refusing one the zone skips or repeats. A payload that already says, or
// a type that reads no zone from the fact, is left as it is; a zone such a type cannot
// take is refused, since the fact would be placed in the Definition's zone instead.
func (a *Adapter) addSourceTime(d anam.Definition, payload map[string]any, zone string) error {
	ft := d.Meta.TimeSemantics.FactTime
	field, top := strings.CutPrefix(ft.Pointer, "/")
	if ft.ZonePointer != offsetPointer {
		if z := strings.TrimSpace(zone); z != "" && z != ft.Zone && ft.ZonePointer == "" && zoneless(ft.Layout) {
			return fmt.Errorf("the daemon's %s v%d reads %s in %s and cannot take zone %s; leave zone out, or update the daemon",
				d.Meta.FacetType, d.Meta.DefinitionVersion, field, ft.Zone, z)
		}
		return nil
	}
	if _, said := payload[sourceTimeField]; said || !top || strings.Contains(field, "/") {
		return nil
	}
	value, ok := payload[field].(string)
	if !ok {
		return nil // checkFactTime names what is wanted
	}
	c, err := a.clockFor(zone)
	if err != nil {
		return err
	}
	t, err := c.instant(ft.Layout, value)
	if err != nil {
		if _, perr := time.ParseInLocation(ft.Layout, value, time.UTC); perr != nil {
			return fmt.Errorf("%s %q: want the layout %s, in %s", field, value, ft.Layout, c.name)
		}
		return fmt.Errorf("%s %w", field, err)
	}
	payload[sourceTimeField] = c.sourceTime(value, t)
	return nil
}

// zoneless reports whether a Go time layout writes no offset and no zone name.
func zoneless(layout string) bool {
	return !strings.Contains(layout, "-07") && !strings.Contains(layout, "Z07") && !strings.Contains(layout, "MST")
}

// checkFactTime reads the payload's fact-time field the way the gate will, by the
// Definition's pointer, layout and zone, and says what is wanted when it cannot.
// Only a top-level pointer is checked here; a deeper one is left to the gate.
func checkFactTime(d anam.Definition, payload map[string]any) error {
	ft := d.Meta.TimeSemantics.FactTime
	field, ok := strings.CutPrefix(ft.Pointer, "/")
	if !ok || strings.Contains(field, "/") || ft.Layout == "" {
		return nil
	}
	v, present := payload[field]
	if !present {
		return fmt.Errorf("a %s is placed in time by %s, which is missing; want the layout %s, in %s",
			d.Meta.FacetType, field, ft.Layout, ft.Zone)
	}
	text, isText := v.(string)
	if !isText {
		return fmt.Errorf("%s must be text in the layout %s, in %s", field, ft.Layout, ft.Zone)
	}
	loc, err := time.LoadLocation(ft.Zone)
	if err != nil {
		return fmt.Errorf("the %s Definition's zone %q: %w", d.Meta.FacetType, ft.Zone, err)
	}
	// The digits are what is checked here; the zone they are read in is the one
	// source_time names, when the type reads it, and the gate holds the instant.
	if _, err := time.ParseInLocation(ft.Layout, text, loc); err != nil {
		return fmt.Errorf("%s %q: want the layout %s, in %s", field, text, ft.Layout, ft.Zone)
	}
	return nil
}
