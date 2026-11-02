package adapter_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattjoyce/anam-mcp/internal/adapter"
	"github.com/mattjoyce/anam-mcp/internal/anam"
	"github.com/mattjoyce/anam-mcp/internal/app"
)

// accessIssuer stands in for a Cloudflare Access team domain: it serves its keys
// at the path Access does and signs JWTs the way Access does, RS256 with a kid.
type accessIssuer struct {
	url string
	key *rsa.PrivateKey
}

func newAccessIssuer(t *testing.T) *accessIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks, err := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "k1",
		"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdn-cgi/access/certs" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	}))
	t.Cleanup(srv.Close)
	return &accessIssuer{url: srv.URL, key: key}
}

// sign makes a JWT from iss for aud that expires at exp.
func (a *accessIssuer) sign(t *testing.T, iss, aud string, exp time.Time) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"iss": iss, "aud": []string{aud}, "sub": "person",
		"iat": time.Now().Unix(), "exp": exp.Unix()})
	unsigned := b64(header) + "." + b64(claims)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + b64(sig)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// withHeader sets one header on every request a client sends, as Access does on
// the requests it lets through.
type withHeader struct {
	name, value string
	next        http.RoundTripper
}

func (h withHeader) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set(h.name, h.value)
	return h.next.RoundTrip(r)
}

// serveHTTP builds the adapter over a real daemon and serves it as the HTTP
// server does, behind an Access application with AUD aud issued by issuer.
func serveHTTP(t *testing.T, issuer *accessIssuer, aud string) string {
	t.Helper()
	d := startDaemon(t, bpSource, []string{bpType})
	client, err := anam.New(d.URL, d.TokenFile, 10*time.Second, 5*time.Second, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }
	server := adapter.New(client, time.UTC, now, "test").Server()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := app.HTTPHandler(t.Context(), server, app.Access{TeamDomain: issuer.url, AUD: aud}, log)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// A client that comes through Access reaches the tools over HTTP, and the answer
// is the daemon's.
func TestHTTPThroughAccess(t *testing.T) {
	issuer := newAccessIssuer(t)
	base := serveHTTP(t, issuer, "aud-tag")
	jwt := issuer.sign(t, issuer.url, "aud-tag", time.Now().Add(time.Hour))
	transport := &mcp.StreamableClientTransport{Endpoint: base + app.MCPPath,
		HTTPClient: &http.Client{Transport: withHeader{app.AccessHeader, jwt, http.DefaultTransport}}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()
	if names := listTools(t, cs); len(names) == 0 {
		t.Fatal("no tools over HTTP")
	}
	var got map[string]any
	if err := call(ctx, cs, "describe_record", map[string]any{}, &got); err != nil {
		t.Fatal(err)
	}
	if got["types"] == nil {
		t.Errorf("describe_record over HTTP answered %v, want the record's types", got)
	}
}

// A request that did not come through Access, or carries a JWT Access did not
// sign for this application, is refused before it reaches the server.
func TestHTTPRefusesWithoutAccess(t *testing.T) {
	issuer := newAccessIssuer(t)
	other := newAccessIssuer(t)
	base := serveHTTP(t, issuer, "aud-tag")
	cases := []struct{ name, jwt string }{
		{"no jwt", ""},
		{"other application", issuer.sign(t, issuer.url, "other-aud", time.Now().Add(time.Hour))},
		{"other issuer", issuer.sign(t, "https://other.cloudflareaccess.com", "aud-tag", time.Now().Add(time.Hour))},
		{"other key", other.sign(t, issuer.url, "aud-tag", time.Now().Add(time.Hour))},
		{"expired", issuer.sign(t, issuer.url, "aud-tag", time.Now().Add(-time.Minute))},
		{"not a jwt", "oauth:opaque"},
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+app.MCPPath, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if c.jwt != "" {
				req.Header.Set(app.AccessHeader, c.jwt)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

// cloudflared reaches the adapter on loopback with the public host name in Host.
// That is how every real request arrives, and it must be answered.
func TestHTTPPublicHostOnLoopback(t *testing.T) {
	issuer := newAccessIssuer(t)
	base := serveHTTP(t, issuer, "aud-tag")
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+app.MCPPath, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "anam-mcp.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	req.Header.Set(app.AccessHeader, issuer.sign(t, issuer.url, "aud-tag", time.Now().Add(time.Hour)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("status = %d (%s), want the request served", resp.StatusCode, b)
	}
}
