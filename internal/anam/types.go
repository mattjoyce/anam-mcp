package anam

import "encoding/json"

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
	Ingest   IngestState    `json:"ingest"`
}

// IngestState names the daemon's running and pending ingest runs, if any.
type IngestState struct {
	Running string `json:"running,omitempty"`
	Pending string `json:"pending,omitempty"`
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

// Definition is one manifest as published: its meta and its JSON Schema, kept as
// the daemon sent them.
type Definition struct {
	Meta   DefinitionMeta  `json:"meta"`
	Schema json.RawMessage `json:"schema"`
}

// DefinitionMeta is the part of a manifest the adapter reads for itself; the rest
// of the meta travels on in Raw.
type DefinitionMeta struct {
	FacetType         string `json:"facet_type"`
	DefinitionVersion int    `json:"definition_version"`
	Classification    string `json:"classification,omitempty"`
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

// Published is a read filed with its receipt (POST /v1/receipts).
type Published struct {
	Timeline
	ReceiptHash string  `json:"receipt_hash"`
	Receipt     Receipt `json:"receipt"`
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
}

// ReceiptQuery is how a read narrowed the template's value, resolved to instants.
type ReceiptQuery struct {
	From   string   `json:"from,omitempty"`
	Before string   `json:"before,omitempty"`
	At     string   `json:"at,omitempty"`
	Type   string   `json:"type,omitempty"`
	Fields []string `json:"fields,omitempty"`
}

// ReceiptShown is a receipt and the served value it names (GET /v1/receipts/{hash}).
type ReceiptShown struct {
	Hash    string  `json:"hash"`
	Receipt Receipt `json:"receipt"`
	Entries []Entry `json:"entries"`
	Frame   *Frame  `json:"frame,omitempty"`
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

// TooLarge measures a read the daemon refused to answer whole.
type TooLarge struct {
	Matched  int      `json:"matched"`
	Bytes    int      `json:"bytes"`
	MaxBytes int      `json:"max_bytes"`
	Narrow   []string `json:"narrow,omitempty"`
}

// errorBody is the body of every response that is not a success.
type errorBody struct {
	Error    string    `json:"error"`
	Code     string    `json:"code,omitempty"`
	TooLarge *TooLarge `json:"too_large,omitempty"`
}
