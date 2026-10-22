package adapter

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattjoyce/anam-mcp/internal/anam"
)

// The selection tools (anam card 206): save_selection files a selection program under
// a short code, sel:<code>, that the person's timeline, select page and link form
// open; open_selection reads a code back to its program and the facets it selects
// now. The daemon saves, opens and evaluates; the adapter translates.

var saveSelectionTool = &mcp.Tool{
	Name:  "save_selection",
	Title: "Save a selection",
	Description: "Save a selection program and get its short code, sel: and eight hex digits, to hand the person " +
		"instead of a program or a list of ids. A selection program defines pick/1, the facets it selects, over " +
		"the base relations describe_query gives, for example pick(E) :- entry(E, \"health.weight\", _, _, _, _, _). " +
		"or pick(\"<facet id>\"). for one facet by hand. The person opens the code on their timeline, which draws " +
		"only what it selects or highlights it among the rest, on the select page, and in the link form; " +
		"open_selection reads it back. The code is the program's own hash: the same program always has the same " +
		"code, so saving it again is harmless, and a code never changes meaning. It names the program, not its " +
		"results, so as the record grows the same code may select more. Saving files the program and nothing " +
		"else: it records no fact. The program must parse; whether it selects anything is not checked here, so " +
		"try it with open_selection or query_record (relation pick) first.",
	Annotations: &mcp.ToolAnnotations{IdempotentHint: true, OpenWorldHint: new(bool), DestructiveHint: new(bool)},
}

var openSelectionTool = &mcp.Tool{
	Name:  "open_selection",
	Title: "Open a saved selection",
	Description: "Read a selection code, such as sel:1a2b3c4d that the person copied from their select page or " +
		"that save_selection gave, back to its program and the facets it selects now. The facets are run through " +
		"the projection named, as query_record runs a program, and answered with each one's facet_id, type and " +
		"fact_time, not their payloads: read them with read_record, or run the program with query_record " +
		"(relation pick) for an answer with a receipt to cite. The run files nothing. The server bounds the " +
		"answer: a selection too large to answer whole is refused with what to narrow, and the program itself, " +
		"which this answers in any case, can be narrowed and saved again under a new code. An unknown code is an " +
		"error, never an empty selection.",
	Annotations: readOnly(),
}

// selectionRelation is the relation open_selection asks for, a rule it adds to the
// saved program to answer each picked facet's type and fact time. Not configurable:
// the name is chosen to leave a selection program's own relations alone.
const selectionRelation = "anam_mcp_opened"

// selectionRule joins pick/1 to the envelope of each facet it picks.
const selectionRule = "\n" + selectionRelation + "(E, T, At) :- pick(E), entry(E, T, At, _, _, _, _).\n"

// SaveSelectionInput is the program to save.
type SaveSelectionInput struct {
	Program string `json:"program" jsonschema:"the selection program, Anam's Datalog defining pick/1, the facets selected, in the syntax describe_query gives"`
}

// SelectionOutput is a saved selection as the tools answer it.
type SelectionOutput struct {
	Code         string `json:"code" jsonschema:"the short code, eight hex digits, or more when another saved selection shares them"`
	Ref          string `json:"ref" jsonschema:"the code as the person reads and pastes it: sel:<code>"`
	Hash         string `json:"hash" jsonschema:"the SHA-256 of the program as saved, which the code is the start of"`
	Program      string `json:"program" jsonschema:"the program as saved: a program the select page's criteria could make is saved in their one layout"`
	TimelineURL  string `json:"timeline_url" jsonschema:"the person's timeline drawing only what the selection picks; it opens in their browser with their own login"`
	HighlightURL string `json:"highlight_url" jsonschema:"the timeline drawing every facet with the selection's picked out"`
	SelectURL    string `json:"select_url" jsonschema:"the select page opened at the code, its criteria drawn when the program is one they make"`
}

// OpenSelectionInput is a code and the projection its facets are read through.
type OpenSelectionInput struct {
	Code       string `json:"code" jsonschema:"the selection code, with or without sel:, such as sel:1a2b3c4d"`
	Projection string `json:"projection" jsonschema:"the projection the program runs through, one of the names describe_record lists; it decides which whole facets the selection may pick"`
}

// SelectedFacet is one facet a selection picks.
type SelectedFacet struct {
	FacetID  string `json:"facet_id"`
	Type     string `json:"type"`
	FactTime string `json:"fact_time"`
}

// OpenSelectionOutput is the saved selection and what it selects now.
type OpenSelectionOutput struct {
	Selection  SelectionOutput `json:"selection"`
	Projection ProjectionRead  `json:"projection" jsonschema:"the projection the program ran through and how many facets it served"`
	Watermark  int64           `json:"watermark" jsonschema:"the ledger position the facets were read at"`
	Count      int             `json:"count" jsonschema:"how many facets the selection picks through the projection"`
	Facets     []SelectedFacet `json:"facets" jsonschema:"each facet picked, in fact-time order, with its id, type and fact time; read the facts with read_record"`
	Note       string          `json:"note" jsonschema:"what the answer is and is not"`
}

// openNote says what open_selection's facets are. Not configurable: it describes
// the tool.
const openNote = "The facets the saved program picks now, through the projection named; a dry run, so nothing was filed and there is no receipt to cite. Run the program with query_record (relation pick) to cite the answer. Facets the projection does not serve are not listed, though the program may pick them."

func (a *Adapter) saveSelection(ctx context.Context, _ *mcp.CallToolRequest, in SaveSelectionInput) (*mcp.CallToolResult, SelectionOutput, error) {
	if strings.TrimSpace(in.Program) == "" {
		return nil, SelectionOutput{}, fmt.Errorf("save_selection: program is required: a Datalog program defining pick/1; describe_query gives the syntax")
	}
	s, err := a.client.SaveSelection(ctx, in.Program)
	if err != nil {
		return nil, SelectionOutput{}, a.failed("save_selection", err)
	}
	return nil, a.selectionOutput(s), nil
}

func (a *Adapter) openSelection(ctx context.Context, _ *mcp.CallToolRequest, in OpenSelectionInput) (*mcp.CallToolResult, OpenSelectionOutput, error) {
	switch {
	case strings.TrimSpace(in.Code) == "":
		return nil, OpenSelectionOutput{}, fmt.Errorf("open_selection: code is required, such as sel:1a2b3c4d")
	case in.Projection == "":
		return nil, OpenSelectionOutput{}, fmt.Errorf("open_selection: projection is required; the program runs over what it serves, and describe_record lists the names")
	}
	s, err := a.client.Selection(ctx, strings.TrimSpace(in.Code))
	if err != nil {
		return nil, OpenSelectionOutput{}, a.failed("open_selection", err)
	}
	ans, err := a.client.Query(ctx, anam.QueryRequest{
		Projection: anam.Named(in.Projection), Text: s.Program + selectionRule, Relation: selectionRelation, DryRun: true, Zone: a.zone.String(),
	})
	if err != nil {
		return nil, OpenSelectionOutput{}, a.failed("open_selection: running "+s.Code, err)
	}
	tuples, err := tuplesOutput(ans.Answer)
	if err != nil {
		return nil, OpenSelectionOutput{}, err
	}
	facets := make([]SelectedFacet, 0, len(tuples))
	for i, t := range tuples {
		f, ok := selectedFacet(t)
		if !ok {
			return nil, OpenSelectionOutput{}, fmt.Errorf("open_selection: tuple %d is not a facet id, a type and a fact time: %v", i, t)
		}
		facets = append(facets, f)
	}
	sortFacets(facets)
	return nil, OpenSelectionOutput{
		Selection: a.selectionOutput(s),
		Projection: ProjectionRead{Name: ans.Projection.Name, Version: ans.Projection.Version,
			Served: ans.Projection.Served, Hash: ans.Projection.Hash},
		Watermark: ans.W, Count: len(facets), Facets: facets, Note: openNote,
	}, nil
}

// selectedFacet reads one tuple of the added rule: three strings.
func selectedFacet(t []any) (SelectedFacet, bool) {
	if len(t) != 3 {
		return SelectedFacet{}, false
	}
	id, ok1 := t[0].(string)
	typ, ok2 := t[1].(string)
	at, ok3 := t[2].(string)
	return SelectedFacet{FacetID: id, Type: typ, FactTime: at}, ok1 && ok2 && ok3
}

// sortFacets orders facets by fact time, then id; a fact-time lexeme sorts as its
// instant does.
func sortFacets(fs []SelectedFacet) {
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].FactTime != fs[j].FactTime {
			return fs[i].FactTime < fs[j].FactTime
		}
		return fs[i].FacetID < fs[j].FacetID
	})
}

// selectionOutput is a saved selection with the person's pages addressed on the
// daemon this adapter calls.
func (a *Adapter) selectionOutput(s anam.Selection) SelectionOutput {
	base := a.client.Server()
	code := url.QueryEscape(s.Code)
	return SelectionOutput{
		Code: s.Code, Ref: "sel:" + s.Code, Hash: s.Hash, Program: s.Program,
		TimelineURL:  base + s.URL,
		HighlightURL: base + "/?sel=" + code + "&selmode=highlight",
		SelectURL:    base + "/select?sel=" + code,
	}
}
