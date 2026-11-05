package adapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mattjoyce/anam-mcp/internal/adapter"
)

// Anam card 270a renamed the record's person to its subject, on the wire as in
// config. The adapter meets three daemons: the rollback image from before 270a,
// which answers person and refuses asserted_subject as an unknown field; the 270a
// daemon, which answers both and reads either; and the alias-removal release, which
// will answer subject alone. A wire shape is one of those faces, put on a real
// daemon of whichever build ANAM_BIN names, so every shape is tested on every build.
type wireShape struct {
	// status is the names GET /v1/status answers the subject under.
	status []string
	// reads is the envelope fields POST /v1/envelopes reads; any other is refused
	// the way a strict decoder refuses an unknown field.
	reads []string
}

var (
	rollbackWire   = wireShape{status: []string{"person"}, reads: []string{"asserted_person"}}
	transitionWire = wireShape{status: []string{"subject", "person"}, reads: []string{"asserted_subject", "asserted_person"}}
	removalWire    = wireShape{status: []string{"subject"}, reads: []string{"asserted_subject"}}
	unnamedWire    = wireShape{reads: []string{"asserted_subject", "asserted_person"}}
)

// shapedDaemon is a server in front of a real daemon that wears a wire shape, and
// the asserted field each envelope sent through it carried.
type shapedDaemon struct {
	URL      string
	mu       sync.Mutex
	asserted []string
}

func (s *shapedDaemon) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.asserted)
}

// shape puts w on d. A status is rewritten to answer the subject under w's names
// only. An envelope's asserted field is recorded, refused when w does not read it,
// and otherwise passed on under the name d itself reads, so the gate behind still
// binds the fact.
func shape(t *testing.T, d *Daemon, w wireShape) *shapedDaemon {
	t.Helper()
	target, err := url.Parse(d.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	upstream := "asserted_person"
	if _, ok := d.status(t)["subject"]; ok {
		upstream = "asserted_subject"
	}
	s := &shapedDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/status":
			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, r)
			var st map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
				http.Error(rw, err.Error(), http.StatusBadGateway)
				return
			}
			id, ok := st["subject"]
			if !ok {
				id = st["person"]
			}
			delete(st, "subject")
			delete(st, "person")
			for _, name := range w.status {
				st[name] = id
			}
			reply(rw, rec.Code, st)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/envelopes":
			var batch struct {
				Envelopes []map[string]any `json:"envelopes"`
			}
			dec := json.NewDecoder(r.Body)
			dec.UseNumber()
			if err := dec.Decode(&batch); err != nil {
				http.Error(rw, err.Error(), http.StatusBadRequest)
				return
			}
			for _, e := range batch.Envelopes {
				var names []string
				for _, name := range []string{"asserted_subject", "asserted_person"} {
					if _, ok := e[name]; ok {
						names = append(names, name)
					}
				}
				s.mu.Lock()
				s.asserted = append(s.asserted, names...)
				s.mu.Unlock()
				for _, name := range names {
					if !slices.Contains(w.reads, name) {
						reply(rw, http.StatusBadRequest, map[string]string{
							"error": fmt.Sprintf("submission: json: unknown field %q", name)})
						return
					}
				}
				for _, name := range names {
					v := e[name]
					delete(e, name)
					e[upstream] = v
				}
			}
			body, err := json.Marshal(batch)
			if err != nil {
				http.Error(rw, err.Error(), http.StatusInternalServerError)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			proxy.ServeHTTP(rw, r)
		default:
			proxy.ServeHTTP(rw, r)
		}
	}))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

func reply(rw http.ResponseWriter, code int, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(code)
	_ = json.NewEncoder(rw).Encode(v)
}

// status reads d's own GET /v1/status as the daemon wrote it.
func (d *Daemon) status(t *testing.T) map[string]any {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, d.URL+"/v1/status", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(d.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // a test body
	var st map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

// Whichever name the daemon answers the subject under, describe_record answers it
// as subject, and save_note and save_fact assert it under the name that daemon
// reads: asserted_subject when its status says subject, asserted_person otherwise.
func TestSubject_EveryWireShape(t *testing.T) {
	cases := map[string]struct {
		wire wireShape
		want string
	}{
		"the rollback daemon, person only":        {rollbackWire, "asserted_person"},
		"the 270a daemon, subject and person":     {transitionWire, "asserted_subject"},
		"the alias-removal release, subject only": {removalWire, "asserted_subject"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d := startDaemon(t, noteSource, nil)
			sd := shape(t, d, c.wire)
			cs := notesSession(t, d, sd.URL, "Australia/Sydney")
			ctx := context.Background()

			var desc adapter.RecordDescription
			if err := call(ctx, cs, "describe_record", nil, &desc); err != nil {
				t.Fatal(err)
			}
			if desc.Subject != subjectID {
				t.Errorf("describe_record subject = %q, want %q", desc.Subject, subjectID)
			}
			var note adapter.SaveNoteOutput
			if err := call(ctx, cs, "save_note", map[string]any{"text": "Walked to the station."}, &note); err != nil {
				t.Fatalf("save_note: %v", err)
			}
			var fact adapter.SaveFactOutput
			payload := map[string]any{"id": "conversation:weight/2026-07-01", "date": "2026-07-01 12:00", "kg": 80.5}
			if err := call(ctx, cs, "save_fact", map[string]any{"type": weightType, "payload": payload}, &fact); err != nil {
				t.Fatalf("save_fact: %v", err)
			}
			if note.Outcome != "admitted" || fact.Outcome != "admitted" {
				t.Errorf("note %s, fact %s, want both admitted", note.Outcome, fact.Outcome)
			}
			if got := sd.sent(); !slices.Equal(got, []string{c.want, c.want}) {
				t.Errorf("asserted fields sent = %v, want %s for the note and the fact", got, c.want)
			}
		})
	}
}

// A daemon that names no subject under either name is an error, and nothing is
// sent: the person's own words and facts are what these tools save, never a quiet
// medium binding.
func TestSubject_NoneNamedSavesNothing(t *testing.T) {
	d := startDaemon(t, noteSource, nil)
	sd := shape(t, d, unnamedWire)
	cs := notesSession(t, d, sd.URL, "Australia/Sydney")
	ctx := context.Background()
	err := call(ctx, cs, "save_note", map[string]any{"text": "Walked to the station."}, nil)
	if err == nil || !strings.Contains(err.Error(), "names no subject") {
		t.Errorf("save_note on a daemon that names no subject: err = %v", err)
	}
	payload := map[string]any{"id": "conversation:weight/2026-07-01", "date": "2026-07-01 12:00", "kg": 80.5}
	err = call(ctx, cs, "save_fact", map[string]any{"type": weightType, "payload": payload}, nil)
	if err == nil || !strings.Contains(err.Error(), "names no subject") {
		t.Errorf("save_fact on a daemon that names no subject: err = %v", err)
	}
	if got := sd.sent(); len(got) != 0 {
		t.Errorf("envelopes sent = %v, want none", got)
	}
}
