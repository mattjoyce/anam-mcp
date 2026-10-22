package anam

import (
	"bytes"
	"encoding/json"
	"errors"
)

// The values the daemon's /v1 routes answer with, as the adapter reads them. They
// mirror the anam repo's internal/api package field for field; the adapter cannot
// import it, so this is the contract spelled on this side of the boundary. Every
// wire key is snake_case.

// Status is the answer to GET /v1/status.
type Status struct {
	Version  string         `json:"version"`
	Person   string         `json:"person"`
	LedgerID string         `json:"ledger_id"`
	W        int64          `json:"w"`
	Since    string         `json:"since,omitempty"`
	Sources  []SourceStatus `json:"sources"`
}

// SourceStatus is what the daemon has seen of one source since it started.
type SourceStatus struct {
	Source          string `json:"source"`
	LastScanAt      string `json:"last_scan_at"`
	LastAdmissionAt string `json:"last_admission_at,omitempty"`
}

// Definitions is the answer to GET /v1/definitions.
type Definitions struct {
	Definitions []DefinitionSummary `json:"definitions"`
}

// DefinitionSummary is one type's Definitions: where it comes from, the version
// envelopes must be shaped to, and every version held.
type DefinitionSummary struct {
	Type     string `json:"type"`
	Origin   string `json:"origin"`
	Current  int    `json:"current"`
	Versions []int  `json:"versions"`
}

// Coverage is the answer to GET /v1/coverage (anam card 90): what the record holds
// at W, type by type. It says what is recorded, never that a source is complete.
type Coverage struct {
	W        int64          `json:"w"`
	LedgerID string         `json:"ledger_id"`
	Live     int            `json:"live"`
	Types    []TypeCoverage `json:"types"`
}

// TypeCoverage is one type's live count and fact-time range, and each source's share.
type TypeCoverage struct {
	Type     string           `json:"type"`
	Count    int              `json:"count"`
	Earliest string           `json:"earliest"`
	Latest   string           `json:"latest"`
	Sources  []SourceCoverage `json:"sources"`
}

// SourceCoverage is what one source contributed to one type.
type SourceCoverage struct {
	Source   string `json:"source"`
	Count    int    `json:"count"`
	Earliest string `json:"earliest"`
	Latest   string `json:"latest"`
}

// Projections is the answer to GET /v1/projections.
type Projections struct {
	Projections []ProjectionSummary `json:"projections"`
}

// ProjectionSummary is one template as a consumer selects it: bundled with the
// server or registered by the operator, its newest version and every version held.
type ProjectionSummary struct {
	Name        string   `json:"name"`
	Origin      string   `json:"origin"`
	Current     int      `json:"current"`
	Versions    []int    `json:"versions"`
	Description string   `json:"description"`
	Types       []string `json:"types"`
	Shape       string   `json:"shape"`
	Hash        string   `json:"hash"`
}

// Definition is one manifest as published: its meta, its JSON Schema and, when
// the type carries one, its handling block, kept as the daemon sent them. The
// handling is what every writer of the type must agree on, served verbatim; by
// convention its guidance key is prose (anam card 166).
type Definition struct {
	Meta     DefinitionMeta  `json:"meta"`
	Schema   json.RawMessage `json:"schema"`
	Handling json.RawMessage `json:"handling,omitempty"`
}

// DefinitionMeta is the part of a manifest the adapter reads for itself; the rest
// of the meta travels on in Raw.
type DefinitionMeta struct {
	FacetType         string        `json:"facet_type"`
	DefinitionVersion int           `json:"definition_version"`
	Classification    string        `json:"classification,omitempty"`
	TimeSemantics     TimeSemantics `json:"time_semantics"`
	// IdentityKey and IdentityTemplate say how the facet's id is made. A type with a
	// template has its id field composed by the gate from the rest (anam card 188),
	// so a writer may leave it out.
	IdentityKey      []string `json:"identity_key,omitempty"`
	IdentityTemplate string   `json:"identity_template,omitempty"`
}

// TimeSemantics says where a type's fact time is in the payload and how it is
// written, so a writer renders a moment the way the gate will read it.
type TimeSemantics struct {
	FactTime FactTime `json:"fact_time"`
}

// FactTime is the payload field that places a facet, its Go time layout, and the
// zone a wall-clock value in it is read in.
type FactTime struct {
	Pointer string `json:"pointer"`
	Layout  string `json:"layout"`
	Zone    string `json:"zone"`
	// ZonePointer, when set, is where the payload names the zone its wall clock was
	// said in (anam card 198): source_time's offset for the self-report types. A
	// payload that names one is read in it, not in Zone.
	ZonePointer string `json:"zone_pointer,omitempty"`
}

// TimelineQuery is a timeline read as the reader asks for it. It travels as URL
// query parameters under these names.
type TimelineQuery struct {
	From       string
	To         string
	At         string
	AsOf       string
	W          int64
	Type       string
	Projection string
	Zone       string
	// Fields names the payload fields wanted, as JSON pointers into the type's
	// schema (anam card 138). With any, the daemon answers a Frame.
	Fields []string
}

// Timeline is one read of the person: what it was read at and through, the hashes
// that identify it, and the entries that survived the narrowing.
type Timeline struct {
	W          int64           `json:"w"`
	Live       int             `json:"live"`
	Shown      int             `json:"shown"`
	AsOf       string          `json:"as_of,omitempty"`
	At         string          `json:"at,omitempty"`
	AtHash     string          `json:"at_hash,omitempty"`
	Canonical  string          `json:"canonical"`
	Projection *ProjectionRead `json:"projection,omitempty"`
	Entries    []Entry         `json:"entries,omitempty"`
	Versions   map[string]int  `json:"versions,omitempty"`
	// Frame is the answer when the query named fields: Entries is then empty.
	Frame *Frame `json:"frame,omitempty"`
}

// Frame is a read cut to its fields: columns, the envelope's attributes
// (facet_id, type, fact_time, source, status, classification, content_hash) then
// the pointers, and one row per facet in timeline order. A cell is the value's own
// JSON; a pointer a row lacks is null.
type Frame struct {
	Columns []string            `json:"columns"`
	Rows    [][]json.RawMessage `json:"rows"`
}

// ProjectionRead names the projection a read went through and what came out of it.
type ProjectionRead struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Served  int    `json:"served"`
	Hash    string `json:"hash"`
}

// Entry is one live facet at its folded state: its identity, provenance and
// verbatim payload.
type Entry struct {
	FacetID        string          `json:"facet_id"`
	Type           string          `json:"type"`
	FactTime       string          `json:"fact_time"`
	Series         string          `json:"series,omitempty"`
	Source         string          `json:"source"`
	SourceRef      string          `json:"source_ref"`
	Status         string          `json:"status"`
	Classification string          `json:"classification"`
	ContentHash    string          `json:"content_hash"`
	Payload        json.RawMessage `json:"payload"`
}

// Published is a read filed with its receipt (POST /v1/receipts). A read too large
// for one answer is filed whole and answered a page at a time (anam card 92):
// Entries, Versions or the frame's rows are then the first page, Shown stays the
// whole count, and Page says where the rest is. Page is nil from a daemon older
// than card 92, which answered whole or refused.
type Published struct {
	Timeline
	ReceiptHash string  `json:"receipt_hash"`
	Receipt     Receipt `json:"receipt"`
	Page        *Page   `json:"page,omitempty"`
}

// Page is which portion of a receipted value an answer carries (anam card 92): the
// receipt's facets from Offset, ReturnedCount of them, of TotalCount in all, cut
// from the filed artifact and never a new read. NextCursor asks for the page after
// this one (GET /v1/receipts/{hash}?cursor=); empty on the last page.
type Page struct {
	Offset        int    `json:"offset"`
	ReturnedCount int    `json:"returned_count"`
	TotalCount    int    `json:"total_count"`
	NextCursor    string `json:"next_cursor,omitempty"`
}

// Receipt is the daemon's claim about one served value: what was served, at which
// watermark, through which template, narrowed how.
type Receipt struct {
	ServedHash           string        `json:"served_hash"`
	FacetIDs             []string      `json:"facet_ids"`
	Watermark            int64         `json:"watermark"`
	Template             string        `json:"template"`
	TemplateVersion      int           `json:"template_version"`
	DefinitionSetVersion string        `json:"definition_set_version"`
	ServedAt             string        `json:"served_at"`
	Span                 string        `json:"span"`
	LedgerID             string        `json:"ledger_id,omitempty"`
	Query                *ReceiptQuery `json:"query,omitempty"`
	// TemplateHash is the content hash of the template version that pruned the
	// value (anam card 125); receipts from before 2026-10-13 have none.
	TemplateHash string `json:"template_hash,omitempty"`
	// TemplateInline is true when the template was an inline projection a query
	// carried (anam card 211): Template is then "inline", TemplateVersion 1, and
	// TemplateHash the address of the body filed beside the receipt.
	TemplateInline bool `json:"template_inline,omitempty"`
	// Program is the query a receipt answers (anam card 173): the template is then
	// the projection the query ran through and ServedHash the answer's. Nil for a
	// timeline receipt.
	Program *ReceiptProgram `json:"program,omitempty"`
}

// ReceiptProgram is the query half of a query receipt: the bundled rule set it
// stood on, the text it ran and the relation answered.
type ReceiptProgram struct {
	RuleSet        string `json:"rule_set"`
	RuleSetVersion int    `json:"rule_set_version"`
	RuleSetHash    string `json:"rule_set_hash"`
	Text           string `json:"text"`
	Relation       string `json:"relation"`
}

// ReceiptQuery is how a read narrowed the template's value, resolved to instants.
type ReceiptQuery struct {
	From   string   `json:"from,omitempty"`
	Before string   `json:"before,omitempty"`
	At     string   `json:"at,omitempty"`
	Type   string   `json:"type,omitempty"`
	Fields []string `json:"fields,omitempty"`
}

// ReceiptShown is a receipt and a page of the served value it names (GET
// /v1/receipts/{hash}). Receipt rides the first page only: a page asked for by
// cursor carries Hash and Page with its entries (anam card 92).
type ReceiptShown struct {
	Hash    string   `json:"hash"`
	Receipt *Receipt `json:"receipt,omitempty"`
	Entries []Entry  `json:"entries"`
	Frame   *Frame   `json:"frame,omitempty"`
	// Answer is the served value of a query receipt; Entries is then empty. An
	// answer is one relation and is not paged, so Page is then nil.
	Answer *Answer `json:"answer,omitempty"`
	Page   *Page   `json:"page,omitempty"`
}

// SelectionRequest is POST /v1/selections's body (anam card 206).
type SelectionRequest struct {
	Program string `json:"program"`
}

// Selection is a saved selection: its short code, the hash of the program as saved,
// the timeline's path to it on the daemon, and the program.
type Selection struct {
	Code    string `json:"code"`
	Hash    string `json:"hash"`
	URL     string `json:"url"`
	Program string `json:"program"`
}

// QueryRequest is a Datalog query as POST /v1/query takes it (anam card 173).
type QueryRequest struct {
	Projection ProjectionRef `json:"projection"`
	Text       string        `json:"text"`
	Relation   string        `json:"relation"`
	AsOf       string        `json:"as_of,omitempty"`
	W          int64         `json:"w,omitempty"`
	DryRun     bool          `json:"dry_run,omitempty"`
	Zone       string        `json:"zone,omitempty"`
}

// InlineProjection is a projection template a query carries itself (anam card 211):
// the fields of a manifest without its name, version and description. The daemon's
// loader checks it, so a mistake comes back as a 400 naming the field.
type InlineProjection struct {
	Types             []string `json:"types"`
	Statuses          []string `json:"statuses"`
	MaxClassification string   `json:"max_classification"`
	From              string   `json:"from,omitempty"`
	To                string   `json:"to,omitempty"`
	Shape             string   `json:"shape,omitempty"`
}

// ProjectionRef is how a query says which projection it reads through: a name, which
// travels as a JSON string, or an InlineProjection, which travels as an object. It
// mirrors the daemon's reference; it holds one of the two.
type ProjectionRef struct {
	Name   string
	Inline *InlineProjection
}

// Named is a reference to the projection called name.
func Named(name string) ProjectionRef { return ProjectionRef{Name: name} }

// IsZero reports whether r names no projection.
func (r ProjectionRef) IsZero() bool { return r.Name == "" && r.Inline == nil }

// MarshalJSON writes a name as a string and an inline template as its object.
func (r ProjectionRef) MarshalJSON() ([]byte, error) {
	switch {
	case r.Name != "" && r.Inline != nil:
		return nil, errors.New("a projection is named or inline, not both")
	case r.Inline != nil:
		return json.Marshal(r.Inline)
	}
	return json.Marshal(r.Name)
}

// UnmarshalJSON reads a name from a string and an inline template from an object.
func (r *ProjectionRef) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case bytes.Equal(b, []byte("null")):
		*r = ProjectionRef{}
	case len(b) > 0 && b[0] == '"':
		var name string
		if err := json.Unmarshal(b, &name); err != nil {
			return err
		}
		*r = ProjectionRef{Name: name}
	case len(b) > 0 && b[0] == '{':
		var in InlineProjection
		if err := json.Unmarshal(b, &in); err != nil {
			return err
		}
		*r = ProjectionRef{Inline: &in}
	default:
		return errors.New("a projection is a name (a string) or an inline template (an object)")
	}
	return nil
}

// Answer is a query's answer: one relation's tuples, sorted. A cell is a JSON
// string, number or boolean.
type Answer struct {
	Relation string              `json:"relation"`
	Tuples   [][]json.RawMessage `json:"tuples"`
}

// RuleSetRef names the bundled rule set an answer stood on.
type RuleSetRef struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Hash    string `json:"hash"`
}

// QueryAnswer is the answer to POST /v1/query: the knowledge it stood on, the
// projection and rules it went through, the tuples and their hash, and the receipt.
type QueryAnswer struct {
	W                    int64          `json:"w"`
	LedgerID             string         `json:"ledger_id"`
	AsOf                 string         `json:"as_of,omitempty"`
	Live                 int            `json:"live"`
	Canonical            string         `json:"canonical"`
	Projection           ProjectionRead `json:"projection"`
	DefinitionSetVersion string         `json:"definition_set_version"`
	Rules                RuleSetRef     `json:"rules"`
	Answer               Answer         `json:"answer"`
	Hash                 string         `json:"hash"`
	Warnings             []string       `json:"warnings,omitempty"`
	ReceiptHash          string         `json:"receipt_hash"`
	Receipt              Receipt        `json:"receipt"`
	Written              bool           `json:"written"`
}

// QueryCatalogue is the answer to GET /v1/query: what a program is written against.
type QueryCatalogue struct {
	Syntax    string          `json:"syntax"`
	Relations []QueryRelation `json:"relations"`
	Instant   QueryInstant    `json:"instant"`
	Types     []QueryType     `json:"types"`
	Rules     QueryRules      `json:"rules"`
}

// QueryRelation describes one relation: base, parameter or rule.
type QueryRelation struct {
	Name  string   `json:"name"`
	Arity int      `json:"arity"`
	Args  []string `json:"args"`
	Kind  string   `json:"kind"`
	Doc   string   `json:"doc,omitempty"`
}

// QueryInstant is how a fact-time is written in a program.
type QueryInstant struct {
	Layout  string `json:"layout"`
	Example string `json:"example"`
}

// QueryType is one type's attributes: the pointers each held Definition version
// declares, its series family and whether it closes one.
type QueryType struct {
	Type     string         `json:"type"`
	Current  int            `json:"current"`
	Series   string         `json:"series,omitempty"`
	Closes   bool           `json:"closes"`
	Versions []QueryVersion `json:"versions"`
}

// QueryVersion lists the pointers one Definition version declares.
type QueryVersion struct {
	Version  int      `json:"version"`
	Pointers []string `json:"pointers"`
}

// QueryRules describes the bundled rule set: identity, hash, text and relations.
type QueryRules struct {
	Name      string          `json:"name"`
	Version   int             `json:"version"`
	Hash      string          `json:"hash"`
	Text      string          `json:"text"`
	Relations []QueryRelation `json:"relations"`
}

// ReceiptVerified is a receipt and the two proofs of it (GET /v1/receipts/{hash}/verify).
type ReceiptVerified struct {
	Hash    string       `json:"hash"`
	Receipt Receipt      `json:"receipt"`
	Result  VerifyResult `json:"result"`
}

// VerifyResult is what verifying a receipt found: re-derived from the ledger, and
// re-hashed on disk.
type VerifyResult struct {
	Reproduced  bool     `json:"reproduced"`
	Intact      bool     `json:"intact"`
	StoredHash  string   `json:"stored_hash,omitempty"`
	DerivedHash string   `json:"derived_hash"`
	Missing     []string `json:"missing,omitempty"`
	Unexpected  []string `json:"unexpected,omitempty"`
	Notes       []string `json:"notes,omitempty"`
}

// TooLarge measures a read the daemon refused to answer whole. A published read
// pages rather than refuse (anam card 92), so there it is refused only for one facet
// over the bound alone: FacetID names it, and a page refused so carries NextCursor,
// the page after that facet.
type TooLarge struct {
	Matched    int      `json:"matched"`
	Bytes      int      `json:"bytes"`
	MaxBytes   int      `json:"max_bytes"`
	Narrow     []string `json:"narrow,omitempty"`
	FacetID    string   `json:"facet_id,omitempty"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// errorBody is the body of every response that is not a success.
type errorBody struct {
	Error    string    `json:"error"`
	Code     string    `json:"code,omitempty"`
	TooLarge *TooLarge `json:"too_large,omitempty"`
	Refused  []refusal `json:"refused,omitempty"`
}

// refusal is one envelope of a batch the gate refused, and why.
type refusal struct {
	Index  int    `json:"index"`
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// Envelope is one fact offered to the gate: its type, the Definition version the
// payload is shaped to (the type's current one), and the payload. The source is the
// token's, never a field. AssertedPerson, set to the record's person, says the
// source scopes the fact to them explicitly, which the gate binds with high
// assurance (anam card 177).
type Envelope struct {
	Type              string `json:"type"`
	DefinitionVersion int    `json:"definition_version"`
	Payload           any    `json:"payload"`
	AssertedPerson    string `json:"asserted_person,omitempty"`
}

// Submitted is the daemon's answer to an admitted batch: the watermark after it, how
// many envelopes changed the record, and each one's outcome.
type Submitted struct {
	W       int64          `json:"w"`
	Changed int            `json:"changed"`
	Results []SubmitResult `json:"results"`
}

// SubmitResult is one envelope's fate: admitted, superseded or no-op.
type SubmitResult struct {
	Index           int    `json:"index"`
	Outcome         string `json:"outcome"`
	FacetID         string `json:"facet_id"`
	ArtifactVersion int    `json:"artifact_version"`
}
