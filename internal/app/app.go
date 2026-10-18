// Package app runs the adapter: configuration from the environment, the daemon
// client, the MCP server on stdio. stdout carries protocol traffic only; everything
// said about the run goes to stderr.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
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
	return c, nil
}

// Run serves the adapter on stdio until the client closes the stream or ctx ends.
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
	log.Info("anam-mcp: serving on stdio", "server", cfg.Server, "zone", cfg.Zone.String(), "version", version,
		"notes", cfg.NoteTokenFile != "")
	err = server.Run(ctx, &mcp.StdioTransport{})
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("anam-mcp: %w", err)
	}
	return nil
}
