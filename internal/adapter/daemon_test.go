// A real anam daemon for the adapter's tests: a new ledger
// under the test's own directory, an operator token, one importer that may submit
// the types a test needs. ANAM_BIN names the anam binary; without it the tests that
// need a daemon are skipped, and scripts/check.sh sets it.
package adapter_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// EnvBin names the anam binary the tests run.
const EnvBin = "ANAM_BIN"

// Daemon is one running anam daemon on a scratch ledger.
type Daemon struct {
	// URL is the daemon's base URL and TokenFile the operator token's path: what the
	// adapter is configured with.
	URL       string
	TokenFile string
	// Source is the importer the daemon admits envelopes from, and importerToken
	// what it presents.
	Source        string
	importerToken string
	cmd           *exec.Cmd
	stderr        bytes.Buffer
}

// Start runs a daemon for t that admits types from source, and stops it when the
// test ends. It skips t when ANAM_BIN is unset.
func startDaemon(t *testing.T, source string, types []string) *Daemon {
	t.Helper()
	bin := os.Getenv(EnvBin)
	if bin == "" {
		t.Skipf("%s names no anam binary; scripts/check.sh sets it", EnvBin)
	}
	dir := t.TempDir()
	d := &Daemon{Source: source, TokenFile: filepath.Join(dir, "token"), importerToken: random(32)}
	if err := os.WriteFile(d.TokenFile, []byte(random(24)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(d.importerToken))
	importers := fmt.Sprintf("importers:\n  - source: %s\n    token_sha256: %s\n    types: [%s]\n",
		source, hex.EncodeToString(sum[:]), strings.Join(types, ", "))
	if err := os.WriteFile(filepath.Join(dir, "importers.yaml"), []byte(importers), 0o600); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	d.URL = fmt.Sprintf("http://127.0.0.1:%d", port)
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir,
		"ANAM_PERSON_ID=person-a",
		"ANAM_LEDGER_PATH=" + filepath.Join(dir, "ledger.db"),
		"ANAM_DB_PATH=" + filepath.Join(dir, "tracking.db"),
		"ANAM_ARTIFACT_DIR=" + filepath.Join(dir, "artifacts"),
		"ANAM_LISTEN=" + fmt.Sprintf("127.0.0.1:%d", port),
		"ANAM_TOKEN_FILE=" + d.TokenFile,
		"ANAM_IMPORTERS_FILE=" + filepath.Join(dir, "importers.yaml"),
		"ANAM_ZONE=UTC",
	}
	initCmd := exec.CommandContext(context.Background(), bin, "init") // #nosec G204 -- the test names the binary.
	initCmd.Env = env
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("anam init: %v\n%s", err, out)
	}
	d.cmd = exec.CommandContext(context.Background(), bin, "daemon") // #nosec G204 -- the test names the binary.
	d.cmd.Env = env
	d.cmd.Stderr = &d.stderr
	if err := d.cmd.Start(); err != nil {
		t.Fatalf("anam daemon: %v", err)
	}
	t.Cleanup(d.stop)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if d.healthy() {
			return d
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("anam daemon did not become ready\n%s", d.stderr.String())
	return nil
}

func (d *Daemon) healthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL+"/healthz", http.NoBody)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (d *Daemon) stop() {
	if d.cmd == nil || d.cmd.Process == nil {
		return
	}
	_ = d.cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _ = d.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = d.cmd.Process.Kill()
		<-done
	}
}

// Envelope is one fact offered to the daemon, shaped to a type's current Definition.
type Envelope struct {
	Type              string `json:"type"`
	DefinitionVersion int    `json:"definition_version"`
	Payload           any    `json:"payload"`
}

// Submitted is the daemon's answer to an admitted batch.
type Submitted struct {
	W       int64 `json:"w"`
	Changed int   `json:"changed"`
	Results []struct {
		Index           int    `json:"index"`
		Outcome         string `json:"outcome"`
		FacetID         string `json:"facet_id"`
		ArtifactVersion int    `json:"artifact_version"`
	} `json:"results"`
}

// Submit posts one batch of envelopes under the importer's token and fails t on
// anything but an admitted batch.
func (d *Daemon) Submit(t *testing.T, envelopes ...Envelope) Submitted {
	t.Helper()
	body, err := json.Marshal(map[string]any{"envelopes": envelopes})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL+"/v1/envelopes", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.importerToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // a test body
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit: HTTP %d: %s", resp.StatusCode, out)
	}
	var s Submitted
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatalf("submit: %v: %s", err, out)
	}
	return s
}

// CurrentVersion reads the current Definition version of typ from the daemon, so a
// test shapes its payloads to what the binary bundles rather than to a constant.
func (d *Daemon) CurrentVersion(t *testing.T, typ string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL+"/v1/definitions", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+d.importerToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // a test body
	var list struct {
		Definitions []struct {
			Type    string `json:"type"`
			Current int    `json:"current"`
		} `json:"definitions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	for _, def := range list.Definitions {
		if def.Type == typ {
			return def.Current
		}
	}
	t.Fatalf("the daemon holds no Definition for %s", typ)
	return 0
}

// FreeAddr is an address nothing listens on, for a test of a daemon that is down.
func freeAddr(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("http://127.0.0.1:%d", freePort(t))
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close() //nolint:errcheck // a probe listener
	return l.Addr().(*net.TCPAddr).Port
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
