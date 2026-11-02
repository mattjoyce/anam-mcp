package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPPath is where the adapter answers MCP over HTTP. It is part of the URL a client
// is given, so changing it breaks every connector already configured.
const MCPPath = "/mcp"

// AccessHeader carries the JWT Cloudflare Access signs for each request it lets
// through. Access names it; the adapter cannot choose another.
const AccessHeader = "Cf-Access-Jwt-Assertion"

// readHeaderTimeout bounds how long a client may take to send its headers. It is
// not configurable: it guards the listener against a client that never finishes,
// and the only client on the loopback address is cloudflared, which sends its
// headers at once. The tool call itself is bounded by ANAM_MCP_TIMEOUT.
const readHeaderTimeout = 10 * time.Second

// Access names the Cloudflare Access application that fronts the adapter: its
// team domain, which issues and signs the JWT, and the application's AUD tag,
// which the JWT must be for.
type Access struct {
	TeamDomain string
	AUD        string
}

// HTTPHandler serves server as stateless Streamable HTTP at MCPPath and refuses
// any request that does not carry a valid Access JWT for access. Access does the
// OAuth (discovery, client registration, login, tokens) at Cloudflare's edge; the
// adapter only checks what Access signed, so a request that reaches it by any
// other way than through Access is refused. ctx bounds the fetching of keys.
func HTTPHandler(ctx context.Context, server *mcp.Server, access Access, log *slog.Logger) http.Handler {
	keys := oidc.NewRemoteKeySet(ctx, strings.TrimSuffix(access.TeamDomain, "/")+"/cdn-cgi/access/certs")
	verifier := oidc.NewVerifier(strings.TrimSuffix(access.TeamDomain, "/"), keys, &oidc.Config{ClientID: access.AUD})
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:                    true,
		PropagateRequestCancellation: true,
		// cloudflared reaches the adapter on loopback with the public host name in
		// Host, which the SDK's DNS-rebinding guard would refuse. The guard is not
		// needed here: a rebound browser request carries no Access JWT and is
		// refused before it reaches the server.
		DisableLocalhostProtection: true,
	})
	mux := http.NewServeMux()
	mux.Handle(MCPPath, mcpHandler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get(AccessHeader)
		if token == "" {
			http.Error(w, "anam-mcp answers only through Cloudflare Access", http.StatusUnauthorized)
			return
		}
		if _, err := verifier.Verify(r.Context(), token); err != nil {
			log.Warn("anam-mcp: refused a request with an invalid Access JWT", "error", err)
			http.Error(w, "anam-mcp answers only through Cloudflare Access", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// serveHTTP serves h on addr until ctx ends, then gives in-flight calls the
// adapter's own deadline to finish.
func serveHTTP(ctx context.Context, addr string, h http.Handler, grace time.Duration) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("%s=%q: %w", EnvHTTPAddr, addr, err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: readHeaderTimeout}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		return err
	}
	if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
