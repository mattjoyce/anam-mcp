package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattjoyce/anam-mcp/internal/anam"
)

var verifyReceiptTool = &mcp.Tool{
	Name:  "verify_receipt",
	Title: "Verify a receipt",
	Description: "Check a receipt a read returned: whether the artifact on disk still hashes to what the receipt " +
		"claims (intact), and whether re-reading the ledger at the receipt's watermark reproduces exactly the facets " +
		"it names (reproduced), with any facet missing or unexpected listed by id. Use it before relying on a cited " +
		"read, or when a person asks whether an answer can still be checked. It publishes nothing. Its answer is " +
		"the receipt plus a few lines.",
	Annotations: readOnly(),
}

// VerifyInput names the receipt to check.
type VerifyInput struct {
	ReceiptHash string `json:"receipt_hash" jsonschema:"the receipt hash a read_record answer gave as its citation"`
}

// VerifyOutput is what verify_receipt answers.
type VerifyOutput struct {
	ReceiptHash string       `json:"receipt_hash"`
	Receipt     anam.Receipt `json:"receipt"`
	Reproduced  bool         `json:"reproduced" jsonschema:"re-deriving the read at the receipt's watermark gave exactly the facets it names"`
	Intact      bool         `json:"intact" jsonschema:"the stored artifact still hashes to the receipt's served_hash"`
	StoredHash  string       `json:"stored_hash,omitempty" jsonschema:"what the artifact on disk hashes to; empty when it is absent"`
	DerivedHash string       `json:"derived_hash" jsonschema:"what re-deriving at the watermark produced"`
	Missing     []string     `json:"missing,omitempty" jsonschema:"facet ids the receipt names that re-deriving did not"`
	Unexpected  []string     `json:"unexpected,omitempty" jsonschema:"facet ids re-deriving produced that the receipt does not name"`
	Notes       []string     `json:"notes,omitempty"`
	Verdict     string       `json:"verdict" jsonschema:"one line: whether the citation holds"`
}

func (a *Adapter) verifyReceipt(ctx context.Context, _ *mcp.CallToolRequest, in VerifyInput) (*mcp.CallToolResult, VerifyOutput, error) {
	if in.ReceiptHash == "" {
		return nil, VerifyOutput{}, fmt.Errorf("verify_receipt: receipt_hash is required")
	}
	v, err := a.client.VerifyReceipt(ctx, in.ReceiptHash)
	if err != nil {
		return nil, VerifyOutput{}, a.failed("verify_receipt", err)
	}
	out := VerifyOutput{
		ReceiptHash: v.Hash, Receipt: v.Receipt,
		Reproduced: v.Result.Reproduced, Intact: v.Result.Intact,
		StoredHash: v.Result.StoredHash, DerivedHash: v.Result.DerivedHash,
		Missing: v.Result.Missing, Unexpected: v.Result.Unexpected, Notes: v.Result.Notes,
	}
	switch {
	case out.Reproduced && out.Intact:
		out.Verdict = "the citation holds: the artifact is intact and the ledger reproduces it"
	case out.Intact:
		out.Verdict = "the artifact is intact, but the ledger no longer reproduces it at that watermark"
	case out.Reproduced:
		out.Verdict = "the ledger reproduces the read, but the stored artifact is altered or absent"
	default:
		out.Verdict = "the citation does not hold: neither the artifact nor the ledger agrees with the receipt"
	}
	return nil, out, nil
}

// receiptPrefix is where the receipt resource's URIs live.
const receiptPrefix = "anam://receipts/"

var receiptResource = &mcp.ResourceTemplate{
	// A hash is sha256:<hex>; the reserved expansion lets the colon through.
	URITemplate: receiptPrefix + "{+hash}",
	Name:        "receipt",
	Title:       "A published read",
	Description: "A receipt by its hash, with the entries it names: the immutable artifact a read_record citation points at. Reading it enforces the same bounds as a read; a receipt too large to show still verifies.",
	MIMEType:    "application/json",
}

// readReceiptResource answers anam://receipts/{hash} with the receipt and the
// entries it names, as JSON text.
func (a *Adapter) readReceiptResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	hash, ok := strings.CutPrefix(uri, receiptPrefix)
	if !ok || hash == "" || strings.Contains(hash, "/") {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	shown, err := a.client.Receipt(ctx, hash)
	if err != nil {
		var ae *anam.Error
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		return nil, a.failed("receipt "+hash, err)
	}
	b, err := json.Marshal(shown)
	if err != nil {
		return nil, fmt.Errorf("receipt %s: %w", hash, err)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(b)}}}, nil
}
