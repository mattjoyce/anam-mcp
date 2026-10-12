package adapter

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mattjoyce/anam-mcp/internal/anam"
)

// TestFailed checks the words a tool fails with: the server named when it did not
// answer, the narrowings offered when a read was too large, the daemon's own words
// otherwise.
func TestFailed(t *testing.T) {
	client, err := anam.New("http://127.0.0.1:1", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	a := New(client, time.UTC, nil, "test")
	cases := []struct {
		name string
		err  error
		want []string
	}{
		{"unreachable", fmt.Errorf("%w: http://127.0.0.1:1: connection refused", anam.ErrUnreachable),
			[]string{"read_record:", "http://127.0.0.1:1", "did not answer"}},
		{"deadline", context.DeadlineExceeded, []string{"read_record:", "deadline exceeded"}},
		{"too large", &anam.Error{Status: 413, Code: anam.CodeTooLarge, Message: "too big",
			TooLarge: &anam.TooLarge{Matched: 9000, Bytes: 2000000, MaxBytes: 1000000, Narrow: []string{"type", "from"}}},
			[]string{"9000 entries", "2000000 bytes", "1000000", "narrow the read by type, from"}},
		{"daemon's words", &anam.Error{Status: 400, Message: "zone Local names the daemon's zone"},
			[]string{"read_record: zone Local names the daemon's zone"}},
		{"other", errors.New("boom"), []string{"read_record: boom"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := a.failed("read_record", c.err).Error()
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("%q lacks %q", got, w)
				}
			}
		})
	}
}

// TestQuery checks the read shapes that are refused before any call, and the one
// that is sent, zone included.
func TestQuery(t *testing.T) {
	client, err := anam.New("http://127.0.0.1:1", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	zone, _ := time.LoadLocation("Australia/Sydney")
	a := New(client, zone, nil, "test")
	refused := []struct {
		name string
		in   ReadInput
		want string
	}{
		{"no mode", ReadInput{}, "timeline or snapshot"},
		{"snapshot without at", ReadInput{Mode: "snapshot"}, "needs at"},
		{"snapshot with to", ReadInput{Mode: "snapshot", At: "2026-06-01", To: "2026-06-30"}, "to belongs to a timeline"},
		{"timeline with at", ReadInput{Mode: "timeline", At: "2026-06-01"}, "at belongs to a snapshot"},
		{"two pins", ReadInput{Mode: "timeline", AsOf: "2026-06-01", Watermark: 3}, "send one"},
		{"negative watermark", ReadInput{Mode: "timeline", Watermark: -1}, "watermark -1"},
	}
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			_, err := a.query(c.in)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want %q", err, c.want)
			}
		})
	}
	q, err := a.query(ReadInput{Mode: "snapshot", At: "2026-06-15", Type: "health.blood-pressure", Projection: "bp", Watermark: 7})
	if err != nil {
		t.Fatal(err)
	}
	want := anam.TimelineQuery{At: "2026-06-15", Type: "health.blood-pressure", Projection: "bp", W: 7, Zone: "Australia/Sydney"}
	if !reflect.DeepEqual(q, want) {
		t.Errorf("query = %+v, want %+v", q, want)
	}
	if _, err := a.query(ReadInput{Mode: "timeline", Fields: []string{"pulse"}}); err == nil || !strings.Contains(err.Error(), "JSON pointer") {
		t.Errorf("a field without a slash: %v", err)
	}
	q, err = a.query(ReadInput{Mode: "timeline", Type: "health.blood-pressure", Fields: []string{"/pulse", "/systolic_blood_pressure/value"}})
	if err != nil || strings.Join(q.Fields, " ") != "/pulse /systolic_blood_pressure/value" {
		t.Errorf("fields = %v, %v", q.Fields, err)
	}
}
