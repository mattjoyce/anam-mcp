package app

import (
	"strings"
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr string
	}{
		{"complete", map[string]string{EnvServer: "http://127.0.0.1:8787", EnvTokenFile: "/run/token", EnvZone: "Australia/Sydney", EnvTimeout: "5s", EnvMargin: "2s"},
			Config{Server: "http://127.0.0.1:8787", TokenFile: "/run/token", Timeout: 5 * time.Second, Margin: 2 * time.Second, MaxBody: DefaultMaxBody}, ""},
		{"defaults", map[string]string{EnvServer: "http://127.0.0.1:8787", EnvZone: "UTC"},
			Config{Server: "http://127.0.0.1:8787", Timeout: DefaultTimeout, Margin: DefaultMargin, MaxBody: DefaultMaxBody}, ""},
		{"zero margin", map[string]string{EnvServer: "http://127.0.0.1:8787", EnvZone: "UTC", EnvMargin: "0s"},
			Config{Server: "http://127.0.0.1:8787", Timeout: DefaultTimeout, MaxBody: DefaultMaxBody}, ""},
		{"max body", map[string]string{EnvServer: "http://127.0.0.1:8787", EnvZone: "UTC", EnvMaxBody: "1048576"},
			Config{Server: "http://127.0.0.1:8787", Timeout: DefaultTimeout, Margin: DefaultMargin, MaxBody: 1 << 20}, ""},
		{"max body too small", map[string]string{EnvServer: "http://127.0.0.1:8787", EnvZone: "UTC", EnvMaxBody: "10"}, Config{}, EnvMaxBody + "=\"10\""},
		{"max body not a number", map[string]string{EnvServer: "http://127.0.0.1:8787", EnvZone: "UTC", EnvMaxBody: "lots"}, Config{}, EnvMaxBody + "=\"lots\""},
		{"no server", map[string]string{EnvZone: "UTC"}, Config{}, EnvServer + " is not set"},
		{"no zone", map[string]string{EnvServer: "http://127.0.0.1:8787"}, Config{}, EnvZone + " is not set"},
		{"local zone", map[string]string{EnvServer: "http://127.0.0.1:8787", EnvZone: "Local"}, Config{}, EnvZone + " is not set"},
		{"bad zone", map[string]string{EnvServer: "http://127.0.0.1:8787", EnvZone: "Mars/Olympus"}, Config{}, "Mars/Olympus"},
		{"bad timeout", map[string]string{EnvServer: "http://127.0.0.1:8787", EnvZone: "UTC", EnvTimeout: "soon"}, Config{}, EnvTimeout + "=\"soon\""},
		{"zero timeout", map[string]string{EnvServer: "http://127.0.0.1:8787", EnvZone: "UTC", EnvTimeout: "0s"}, Config{}, EnvTimeout + "=\"0s\""},
		{"bad margin", map[string]string{EnvServer: "http://127.0.0.1:8787", EnvZone: "UTC", EnvMargin: "later"}, Config{}, EnvMargin + "=\"later\""},
		{"negative margin", map[string]string{EnvServer: "http://127.0.0.1:8787", EnvZone: "UTC", EnvMargin: "-1s"}, Config{}, EnvMargin + "=\"-1s\""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ConfigFromEnv(func(k string) string { return c.env[k] })
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("got %v, want an error containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Server != c.want.Server || got.TokenFile != c.want.TokenFile || got.Timeout != c.want.Timeout || got.Margin != c.want.Margin {
				t.Errorf("config = %+v, want %+v", got, c.want)
			}
			if got.Zone == nil || got.Zone.String() != c.env[EnvZone] {
				t.Errorf("zone = %v, want %s", got.Zone, c.env[EnvZone])
			}
		})
	}
}
