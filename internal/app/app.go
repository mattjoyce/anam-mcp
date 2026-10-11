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
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mattjoyce/anam-mcp/internal/adapter"
	"github.com/mattjoyce/anam-mcp/internal/anam"
)

// The environment the adapter reads. ANAM_SERVER and ANAM_TOKEN_FILE are the
// variables the anam CLI reads too, so one shell serves both.
const (
	envPrefix    = "ANAM_"
	EnvServer    = envPrefix + "SERVER"
	EnvTokenFile = envPrefix + "TOKEN_FILE"
	EnvZone      = envPrefix + "ZONE"
	EnvTimeout   = envPrefix + "MCP_TIMEOUT"
)

// DefaultTimeout bounds each call on the daemon when ANAM_MCP_TIMEOUT is unset.
const DefaultTimeout = 30 * time.Second

// Config is one run's configuration, resolved and checked.
type Config struct {
	Server    string
	TokenFile string
	Zone      *time.Location
	Timeout   time.Duration
}

// ConfigFromEnv reads the configuration from the environment and refuses to start
// without the server or a zone it can resolve. The zone must be named: a bare date
// in a read is a day in it, and guessing one would misplace facts.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	c := Config{Server: getenv(EnvServer), TokenFile: getenv(EnvTokenFile), Timeout: DefaultTimeout}
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
	return c, nil
}

// Run serves the adapter on stdio until the client closes the stream or ctx ends.
func Run(ctx context.Context, version string) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	client, err := anam.New(cfg.Server, cfg.TokenFile, cfg.Timeout)
	if err != nil {
		return err
	}
	server := adapter.New(client, cfg.Zone, time.Now, version).Server()
	log.Info("anam-mcp: serving on stdio", "server", cfg.Server, "zone", cfg.Zone.String(), "version", version)
	err = server.Run(ctx, &mcp.StdioTransport{})
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("anam-mcp: %w", err)
	}
	return nil
}
