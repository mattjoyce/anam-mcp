// Package app runs the adapter: configuration from the environment, the daemon
// client, the MCP server on stdio. stdout carries protocol traffic only; everything
// said about the run goes to stderr.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattjoyce/anam-mcp/internal/adapter"
	"github.com/mattjoyce/anam-mcp/internal/anam"
)

// The environment the adapter reads. ANAM_SERVER and ANAM_TOKEN_FILE are the
// variables the anam CLI reads too, so one shell serves both. The names are not
// configurable; they are how configuration is found.
const (
	envPrefix    = "ANAM_"
	EnvServer    = envPrefix + "SERVER"
	EnvTokenFile = envPrefix + "TOKEN_FILE"
	EnvZone      = envPrefix + "ZONE"
	EnvTimeout   = envPrefix + "MCP_TIMEOUT"
	EnvMargin    = envPrefix + "MCP_NETWORK_MARGIN"
	EnvMaxBody   = envPrefix + "MCP_MAX_RESPONSE_BYTES"
	// EnvNoteTokenFile names the token of the importer the person's words and
	// facts are admitted as (source conversation). Set, the adapter offers
	// save_note and save_fact; unset, it writes nothing (anam cards 157, 177).
	EnvNoteTokenFile = envPrefix + "MCP_NOTE_TOKEN_FILE"
	// EnvHTTPAddr is the address to serve MCP over HTTP on, such as
	// 127.0.0.1:8790. Unset, the adapter speaks stdio. Set, it answers only
	// requests Cloudflare Access let through, so the two Access variables are
	// required with it.
	EnvHTTPAddr = envPrefix + "MCP_HTTP_ADDR"
	// EnvAccessTeamDomain is the Access team domain that signs the JWT, such as
	// https://example.cloudflareaccess.com.
	EnvAccessTeamDomain = envPrefix + "MCP_ACCESS_TEAM_DOMAIN"
	// EnvAccessAUD is the AUD tag of the Access application fronting the adapter.
	EnvAccessAUD = envPrefix + "MCP_ACCESS_AUD"
)

// The opinionated defaults. Each is a config key; see AGENTS.md and anam card 151.
const (
	// DefaultTimeout bounds each call on the daemon when ANAM_MCP_TIMEOUT is unset.
	DefaultTimeout = 30 * time.Second
	// DefaultMargin is the time allowed beyond the daemon's deadline for its answer
	// to travel, when ANAM_MCP_NETWORK_MARGIN is unset. A daemon across a slow link
	// may want more.
	DefaultMargin = 5 * time.Second
	// DefaultMaxBody bounds what the adapter reads of one answer when
	// ANAM_MCP_MAX_RESPONSE_BYTES is unset: 64 MiB, the anam CLI's own bound. The
	// daemon's max_read_bytes refuses a read far smaller than this first.
	DefaultMaxBody = 64 << 20
)

// Config is one run's configuration, resolved and checked.
type Config struct {
	Server    string
	TokenFile string
	// NoteTokenFile is the conversation importer's token; empty, no note is saved.
	NoteTokenFile string
	Zone          *time.Location
	Timeout       time.Duration
	Margin        time.Duration
	MaxBody       int64
	// HTTPAddr is where to serve HTTP; empty, the adapter speaks stdio.
	HTTPAddr string
	// Access fronts the HTTP server; set whenever HTTPAddr is.
	Access Access
}

// ConfigFromEnv reads the configuration from the environment and refuses to start
// without the server or a zone it can resolve. The zone must be named: a bare date
// in a read is a day in it, and guessing one would misplace facts.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	c := Config{Server: getenv(EnvServer), TokenFile: getenv(EnvTokenFile), NoteTokenFile: getenv(EnvNoteTokenFile), Timeout: DefaultTimeout, Margin: DefaultMargin, MaxBody: DefaultMaxBody}
	if c.Server == "" {
		return Config{}, fmt.Errorf("%s is not set: the daemon's base URL, such as http://127.0.0.1:8787", EnvServer)
	}
	zone := getenv(EnvZone)
	if zone == "" || zone == "Local" {
		return Config{}, fmt.Errorf("%s is not set: an IANA zone name such as Australia/Sydney, the one bare dates are read in", EnvZone)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return Config{}, fmt.Errorf("%s=%q: %w", EnvZone, zone, err)
	}
	c.Zone = loc
	if v := getenv(EnvTimeout); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("%s=%q: want a positive duration such as 30s", EnvTimeout, v)
		}
		c.Timeout = d
	}
	if v := getenv(EnvMargin); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return Config{}, fmt.Errorf("%s=%q: want zero or a positive duration such as 5s", EnvMargin, v)
		}
		c.Margin = d
	}
	if v := getenv(EnvMaxBody); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1<<10 || n > 1<<30 {
			return Config{}, fmt.Errorf("%s=%q: want a number of bytes from 1024 to 1073741824", EnvMaxBody, v)
		}
		c.MaxBody = n
	}
	if err := c.httpFromEnv(getenv); err != nil {
		return Config{}, err
	}
	return c, nil
}

// httpFromEnv reads the HTTP address and the Access application in front of it.
// There is no HTTP without Access: the record is a person's own, and an address
// that answered without it would answer anyone who reached it.
func (c *Config) httpFromEnv(getenv func(string) string) error {
	c.HTTPAddr = getenv(EnvHTTPAddr)
	team, aud := getenv(EnvAccessTeamDomain), getenv(EnvAccessAUD)
	if c.HTTPAddr == "" {
		if team != "" || aud != "" {
			return fmt.Errorf("%s and %s apply only to HTTP: set %s too, or unset them", EnvAccessTeamDomain, EnvAccessAUD, EnvHTTPAddr)
		}
		return nil
	}
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		return fmt.Errorf("%s=%q: want host:port such as 127.0.0.1:8790", EnvHTTPAddr, c.HTTPAddr)
	}
	u, err := url.Parse(team)
	if team == "" || err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("%s=%q: HTTP needs the Access team domain, such as https://example.cloudflareaccess.com", EnvAccessTeamDomain, team)
	}
	if aud == "" {
		return fmt.Errorf("%s is not set: HTTP needs the AUD tag of the Access application in front of it", EnvAccessAUD)
	}
	c.Access = Access{TeamDomain: strings.TrimSuffix(team, "/"), AUD: aud}
	return nil
}

// Run serves the adapter until ctx ends: on stdio until the client closes the
// stream, or over HTTP behind Cloudflare Access when ANAM_MCP_HTTP_ADDR is set.
func Run(ctx context.Context, version string) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	client, err := anam.New(cfg.Server, cfg.TokenFile, cfg.Timeout, cfg.Margin, cfg.MaxBody)
	if err != nil {
		return err
	}
	a := adapter.New(client, cfg.Zone, time.Now, version)
	if cfg.NoteTokenFile != "" {
		notes, err := anam.New(cfg.Server, cfg.NoteTokenFile, cfg.Timeout, cfg.Margin, cfg.MaxBody)
		if err != nil {
			return fmt.Errorf("%s: %w", EnvNoteTokenFile, err)
		}
		a.WithNotes(notes)
	}
	server := a.Server()
	if cfg.HTTPAddr != "" {
		log.Info("anam-mcp: serving HTTP behind Cloudflare Access", "addr", cfg.HTTPAddr, "path", MCPPath,
			"access", cfg.Access.TeamDomain, "server", cfg.Server, "zone", cfg.Zone.String(), "version", version,
			"notes", cfg.NoteTokenFile != "")
		return serveHTTP(ctx, cfg.HTTPAddr, HTTPHandler(ctx, server, cfg.Access, log), cfg.Timeout+cfg.Margin)
	}
	log.Info("anam-mcp: serving on stdio", "server", cfg.Server, "zone", cfg.Zone.String(), "version", version,
		"notes", cfg.NoteTokenFile != "")
	err = server.Run(ctx, &mcp.StdioTransport{})
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("anam-mcp: %w", err)
	}
	return nil
}
