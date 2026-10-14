package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var describeRecordTool = &mcp.Tool{
	Name:  "describe_record",
	Title: "Describe the record",
	Description: "Learn what the record holds before choosing what to read: whose record it is, its ledger identity and " +
		"current watermark, the current time and the zone bare dates are read in, which sources the server has seen and " +
		"when, every facet type it admits with its current definition version, every projection a read may go through " +
		"(the names read_record takes), what this server can do, and what it cannot do yet. Call it first. It " +
		"publishes nothing. Its answer is small: a few hundred bytes plus one line per source, per type and per projection.",
	Annotations: readOnly(),
}

var describeTypeTool = &mcp.Tool{
	Name:  "describe_type",
	Title: "Describe a facet type",
	Description: "The exact shape of one facet type, as its published Definition: the JSON Schema every payload of that " +
		"type satisfies, with each field's description and examples, and the meta that says how a fact is placed in " +
		"time and identified. Use it to read a payload correctly, never to guess at fields a schema does not name. " +
		"It publishes nothing. Its answer is the Definition as published, usually a few KB.",
	Annotations: readOnly(),
}

// DescribeTypeInput names a type and, optionally, which version of its Definition.
type DescribeTypeInput struct {
	Type    string `json:"type" jsonschema:"the facet type, dotted and namespaced, such as health.blood-pressure"`
	Version int    `json:"version,omitempty" jsonschema:"a Definition version to read instead of the current one; omit for the current"`
}

// RecordDescription is what describe_record answers.
type RecordDescription struct {
	Person       string              `json:"person" jsonschema:"the person whose record this is, as the server labels them"`
	LedgerID     string              `json:"ledger_id" jsonschema:"the ledger's identity; a receipt names it, and a watermark means nothing without it"`
	Watermark    int64               `json:"watermark" jsonschema:"the ledger's current position; a read at this watermark sees exactly this knowledge"`
	Now          string              `json:"now" jsonschema:"the current instant, RFC 3339 UTC; resolve relative dates against it"`
	Zone         string              `json:"zone" jsonschema:"the IANA zone a bare date in a read is a day in"`
	AnamVersion  string              `json:"anam_version" jsonschema:"the server's build"`
	AdapterVer   string              `json:"adapter_version" jsonschema:"this adapter's build"`
	ServerSince  string              `json:"server_since,omitempty" jsonschema:"when the server started; source freshness below covers only what it has seen since"`
	Sources      []SourceFreshness   `json:"sources" jsonschema:"each source whose importer has sent a batch since the server started, and when; a quiet source may be current without recent facts"`
	Types        []TypeSummary       `json:"types" jsonschema:"every facet type the server admits"`
	Projections  []ProjectionSummary `json:"projections" jsonschema:"every projection a read may go through; read_record's projection takes one of these names"`
	Capabilities []string            `json:"capabilities" jsonschema:"what read_record can do on this server"`
	Limitations  []string            `json:"limitations" jsonschema:"what this server cannot do yet; do not work around these by other means"`
}

// SourceFreshness is when a source was last read and last changed the record.
type SourceFreshness struct {
	Source          string `json:"source"`
	LastScanAt      string `json:"last_scan_at" jsonschema:"when this source's importer last sent a batch"`
	LastAdmissionAt string `json:"last_admission_at,omitempty" jsonschema:"when a read of this source last changed the record; empty if none has since the server started"`
}

// TypeSummary is one admitted type and the Definition versions held for it.
type TypeSummary struct {
	Type           string `json:"type"`
	Origin         string `json:"origin" jsonschema:"bundled with the server, or registered by an importer"`
	CurrentVersion int    `json:"current_version"`
	Versions       []int  `json:"versions" jsonschema:"every Definition version held, oldest first"`
}

// ProjectionSummary is one projection a read may select: what it serves, its shape,
// and whether it ships with the server or the operator registered it.
type ProjectionSummary struct {
	Name           string   `json:"name"`
	Origin         string   `json:"origin" jsonschema:"bundled with the server, or registered by the operator"`
	CurrentVersion int      `json:"current_version" jsonschema:"the version a read goes through"`
	Versions       []int    `json:"versions" jsonschema:"every version held, oldest first; a receipt names the one that served it"`
	Description    string   `json:"description" jsonschema:"what the projection is for, in the operator's words"`
	Types          []string `json:"types" jsonschema:"the facet types it serves; everything else is pruned"`
	Shape          string   `json:"shape" jsonschema:"log (every surviving facet) or snapshot (the latest per series)"`
}

// TypeDescription is what describe_type answers: the Definition as published.
type TypeDescription struct {
	Type           string `json:"type"`
	Version        int    `json:"version"`
	Classification string `json:"classification,omitempty" jsonschema:"the type's classification, as the Definition declares it"`
	Definition     any    `json:"definition" jsonschema:"the whole Definition as published: meta (time semantics, identity key) and schema (JSON Schema 2020-12)"`
}

// capabilities and limitations say what this build can and cannot do. Not
// configurable: they describe the code, and a list that differed from it would
// mislead the model.
var capabilities = []string{
	"timeline: live facts in fact-time order, narrowed by from, to and type",
	"snapshot: the latest fact per series at or before a fact-time, with at",
	"as_of: what the record knew at an earlier anam-time",
	"watermark: a read pinned to a ledger position a receipt names, so two reads see the same knowledge",
	"projection: a read through a named projection, published with a receipt",
	"fields: a read answered as a frame of only the payload fields named, JSON pointers from describe_type, a fraction of the size",
	"verify: a receipt checked against the ledger and the stored artifact",
}

var limitations = []string{
	"no counts or date ranges per type: coverage is found by reading, not by discovery",
	"no paging: a read is answered whole or refused as too large, with what to narrow by",
	"no single facet by reference: cite a read's receipt and the facet_id within it",
	"no writes: nothing here records, corrects or retracts a fact",
}

func (a *Adapter) describeRecord(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, RecordDescription, error) {
	st, err := a.client.Status(ctx)
	if err != nil {
		return nil, RecordDescription{}, a.failed("describe_record", err)
	}
	defs, err := a.client.Definitions(ctx)
	if err != nil {
		return nil, RecordDescription{}, a.failed("describe_record", err)
	}
	projs, err := a.client.Projections(ctx)
	if err != nil {
		return nil, RecordDescription{}, a.failed("describe_record", err)
	}
	d := RecordDescription{
		Person: st.Person, LedgerID: st.LedgerID, Watermark: st.W,
		Now: a.now().UTC().Format(time.RFC3339), Zone: a.zone.String(),
		AnamVersion: st.Version, AdapterVer: a.version, ServerSince: st.Since,
		Sources:      make([]SourceFreshness, 0, len(st.Sources)),
		Types:        make([]TypeSummary, 0, len(defs.Definitions)),
		Projections:  make([]ProjectionSummary, 0, len(projs.Projections)),
		Capabilities: capabilities, Limitations: limitations,
	}
	for _, s := range st.Sources {
		d.Sources = append(d.Sources, SourceFreshness{Source: s.Source, LastScanAt: s.LastScanAt, LastAdmissionAt: s.LastAdmissionAt})
	}
	for _, t := range defs.Definitions {
		d.Types = append(d.Types, TypeSummary{Type: t.Type, Origin: t.Origin, CurrentVersion: t.Current, Versions: t.Versions})
	}
	for _, p := range projs.Projections {
		d.Projections = append(d.Projections, ProjectionSummary{
			Name: p.Name, Origin: p.Origin, CurrentVersion: p.Current, Versions: p.Versions,
			Description: p.Description, Types: p.Types, Shape: p.Shape,
		})
	}
	return nil, d, nil
}

func (a *Adapter) describeType(ctx context.Context, _ *mcp.CallToolRequest, in DescribeTypeInput) (*mcp.CallToolResult, TypeDescription, error) {
	if in.Type == "" {
		return nil, TypeDescription{}, fmt.Errorf("describe_type: type is required")
	}
	if in.Version < 0 {
		return nil, TypeDescription{}, fmt.Errorf("describe_type: version %d: want a positive integer, or none", in.Version)
	}
	def, err := a.client.Definition(ctx, in.Type, in.Version)
	if err != nil {
		return nil, TypeDescription{}, a.failed("describe_type", err)
	}
	// The whole manifest travels as published; Meta is only what this side read.
	var whole any
	b, err := json.Marshal(def)
	if err == nil {
		err = json.Unmarshal(b, &whole)
	}
	if err != nil {
		return nil, TypeDescription{}, fmt.Errorf("describe_type: %w", err)
	}
	return nil, TypeDescription{
		Type: def.Meta.FacetType, Version: def.Meta.DefinitionVersion,
		Classification: def.Meta.Classification, Definition: whole,
	}, nil
}
