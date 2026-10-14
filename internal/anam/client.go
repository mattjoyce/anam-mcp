// Package anam is the adapter's client of the Anam daemon: the /v1 routes it calls,
// the values they answer with, and the errors they can fail with. It holds no state
// but the server's address and the token, and it computes nothing of the record.
package anam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// timeoutHeader carries the caller's deadline to the daemon, which works to the
// shorter of it and its own, so it answers before the caller gives up. Not
// configurable: the daemon reads this name, and docs/API.md fixes it.
const timeoutHeader = "Anam-Timeout"

// CodeTooLarge is the daemon's code for a read refused for its size. Not
// configurable: the daemon sends it, and docs/API.md fixes it.
const CodeTooLarge = "result_too_large"

// ErrUnreachable marks a call that never got an answer: the daemon is down, the
// address is wrong, or the deadline passed first.
var ErrUnreachable = errors.New("anam: server unreachable")

// Error is an answer that was not a success, with what the daemon said.
type Error struct {
	Status   int
	Message  string
	Code     string
	TooLarge *TooLarge
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("anam: %s (%s, HTTP %d)", e.Message, e.Code, e.Status)
	}
	return fmt.Sprintf("anam: %s (HTTP %d)", e.Message, e.Status)
}

// Client calls one daemon.
type Client struct {
	server  string
	token   []byte
	timeout time.Duration
	margin  time.Duration
	maxBody int64
	http    *http.Client
}

// New makes a client of the daemon at server. tokenFile may be empty for a daemon
// on loopback that asks for no token; otherwise its content, trimmed, is sent as a
// bearer token and never logged. timeout bounds each call on the daemon's side;
// margin is the time allowed beyond it for the answer to travel, so a daemon that
// answers at its bound is still heard. maxBody bounds what the client reads of one
// answer.
func New(server, tokenFile string, timeout, margin time.Duration, maxBody int64) (*Client, error) {
	u, err := url.Parse(server)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("anam: server %q: want a URL such as http://127.0.0.1:8787", server)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("anam: timeout %s: want a positive duration", timeout)
	}
	if margin < 0 {
		return nil, fmt.Errorf("anam: network margin %s: want zero or a positive duration", margin)
	}
	if maxBody < 1 {
		return nil, fmt.Errorf("anam: max response bytes %d: want a positive number", maxBody)
	}
	// The http.Client has no timeout of its own: each call's context carries the
	// deadline, so there is one bound per call and not two.
	c := &Client{server: strings.TrimRight(server, "/"), timeout: timeout, margin: margin, maxBody: maxBody, http: &http.Client{}}
	if tokenFile != "" {
		b, err := os.ReadFile(tokenFile) // #nosec G304 -- the operator names the file in configuration.
		if err != nil {
			return nil, fmt.Errorf("anam: token file: %w", err)
		}
		c.token = bytes.TrimSpace(b)
		if len(c.token) == 0 {
			return nil, fmt.Errorf("anam: token file %s is empty", tokenFile)
		}
	}
	return c, nil
}

// Server is the daemon's base URL, for an error that names it.
func (c *Client) Server() string { return c.server }

// Timeout is the deadline each call carries.
func (c *Client) Timeout() time.Duration { return c.timeout }

// Status answers GET /v1/status.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var s Status
	err := c.get(ctx, "/v1/status", nil, &s)
	return s, err
}

// Definitions answers GET /v1/definitions.
func (c *Client) Definitions(ctx context.Context) (Definitions, error) {
	var d Definitions
	err := c.get(ctx, "/v1/definitions", nil, &d)
	return d, err
}

// Projections answers GET /v1/projections: every template a read may select.
func (c *Client) Projections(ctx context.Context) (Projections, error) {
	var p Projections
	err := c.get(ctx, "/v1/projections", nil, &p)
	return p, err
}

// Definition answers GET /v1/definitions/{type} or, with version > 0,
// /{type}/{version}: the manifest as published.
func (c *Client) Definition(ctx context.Context, typ string, version int) (Definition, error) {
	path := "/v1/definitions/" + url.PathEscape(typ)
	if version > 0 {
		path += "/" + strconv.Itoa(version)
	}
	var d Definition
	err := c.get(ctx, path, nil, &d)
	return d, err
}

// Timeline answers GET /v1/timeline: the read q describes, without a receipt.
func (c *Client) Timeline(ctx context.Context, q TimelineQuery) (Timeline, error) {
	var t Timeline
	err := c.get(ctx, "/v1/timeline", q.values(), &t)
	return t, err
}

// Publish answers POST /v1/receipts: the read q describes, filed with a receipt. The
// daemon requires a projection.
func (c *Client) Publish(ctx context.Context, q TimelineQuery) (Published, error) {
	var p Published
	err := c.do(ctx, http.MethodPost, "/v1/receipts", q.values(), &p)
	return p, err
}

// Receipt answers GET /v1/receipts/{hash}: the receipt and the entries it names.
func (c *Client) Receipt(ctx context.Context, hash string) (ReceiptShown, error) {
	var r ReceiptShown
	err := c.get(ctx, "/v1/receipts/"+url.PathEscape(hash), nil, &r)
	return r, err
}

// VerifyReceipt answers GET /v1/receipts/{hash}/verify.
func (c *Client) VerifyReceipt(ctx context.Context, hash string) (ReceiptVerified, error) {
	var v ReceiptVerified
	err := c.get(ctx, "/v1/receipts/"+url.PathEscape(hash)+"/verify", nil, &v)
	return v, err
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, q, out)
}

// do makes one call, bounded by the client's timeout and the caller's context, and
// decodes a 2xx into out. Any other status is an *Error; no answer is ErrUnreachable.
func (c *Client) do(ctx context.Context, method, path string, q url.Values, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout+c.margin)
	defer cancel()
	u := c.server + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, http.NoBody)
	if err != nil {
		return fmt.Errorf("anam: %s %s: %w", method, path, err)
	}
	req.Header.Set(timeoutHeader, c.timeout.String())
	req.Header.Set("Accept", "application/json")
	if c.token != nil {
		req.Header.Set("Authorization", "Bearer "+string(c.token))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrUnreachable, c.server, unwrapURL(err))
	}
	defer resp.Body.Close() //nolint:errcheck // nothing to do with a close error on a read body
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBody+1))
	if err != nil {
		return fmt.Errorf("%w: %s: reading the answer: %w", ErrUnreachable, c.server, err)
	}
	if int64(len(body)) > c.maxBody {
		return fmt.Errorf("anam: %s %s: the answer is over %d bytes (ANAM_MCP_MAX_RESPONSE_BYTES); narrow the read",
			method, path, c.maxBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return failure(resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("anam: %s %s: the answer is not the JSON expected: %w", method, path, err)
	}
	return nil
}

// failure reads the daemon's error body; one that is not JSON is quoted as text.
func failure(status int, body []byte) error {
	var e errorBody
	if err := json.Unmarshal(body, &e); err != nil || e.Error == "" {
		e.Error = strings.TrimSpace(string(body))
		if e.Error == "" {
			e.Error = http.StatusText(status)
		}
	}
	return &Error{Status: status, Message: e.Error, Code: e.Code, TooLarge: e.TooLarge}
}

// unwrapURL strips the *url.Error wrapper, which repeats the URL the message
// already names.
func unwrapURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// values encodes q for the daemon, under the names its query parser reads.
func (q TimelineQuery) values() url.Values {
	v := url.Values{}
	for name, val := range map[string]string{
		"from": q.From, "to": q.To, "at": q.At, "as_of": q.AsOf,
		"type": q.Type, "projection": q.Projection, "zone": q.Zone,
	} {
		if val != "" {
			v.Set(name, val)
		}
	}
	if q.W != 0 {
		v.Set("w", strconv.FormatInt(q.W, 10))
	}
	for _, f := range q.Fields {
		v.Add("fields", f)
	}
	return v
}
