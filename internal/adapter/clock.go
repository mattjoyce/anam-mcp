package adapter

import (
	"fmt"
	"strings"
	"time"
)

// How a writer says its clock was read (anam cards 197 and 198). A self-report type
// whose fact time reads source_time's offset is written as a wall clock in the zone
// the person is in, with that zone, the offset the wall clock had there, and who named
// the zone: the chat client, passing zone (basis source), or this server's own zone
// setting (basis configuration). The offset is what places the fact, so a wall clock
// the zone skips or repeats is refused here, named, rather than guessed.

// sourceTimeField is the payload key anam reads how a clock was read from. Not
// configurable: it is anam's field name.
const sourceTimeField = "source_time"

// offsetPointer is where the self-report types read the offset. Not configurable: a
// Definition that reads its zone elsewhere is one this build does not write.
const offsetPointer = "/" + sourceTimeField + "/offset"

// writerClock is the zone a write names, loaded, with the name and basis it is sent
// with.
type writerClock struct {
	loc   *time.Location
	name  string
	basis string
}

// clockFor is the zone the person named, as an IANA name or an offset +HH:MM, or this
// server's own zone when they named none.
func (a *Adapter) clockFor(zone string) (writerClock, error) {
	zone = strings.TrimSpace(zone)
	if zone == "" {
		return writerClock{loc: a.zone, name: a.zone.String(), basis: "configuration"}, nil
	}
	if zone == "Local" {
		return writerClock{}, fmt.Errorf("zone %q names no zone; pass the person's IANA zone (Europe/London) or offset (+01:00)", zone)
	}
	if zone[0] == '+' || zone[0] == '-' {
		t, err := time.Parse("-07:00", zone)
		if err != nil {
			return writerClock{}, fmt.Errorf("zone %q is not an offset +HH:MM or -HH:MM", zone)
		}
		_, off := t.Zone()
		return writerClock{loc: time.FixedZone(zone, off), name: zone, basis: "source"}, nil
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return writerClock{}, fmt.Errorf("zone %q is not an IANA zone (Europe/London) or an offset (+01:00): %w", zone, err)
	}
	return writerClock{loc: loc, name: zone, basis: "source"}, nil
}

// wallLayout writes every field of a wall clock, for comparing two of them.
const wallLayout = "2006-01-02T15:04:05.999999999"

// instant is the one moment the wall clock s, in layout, names in c's zone, or an
// error naming why there is not one: the zone's clocks skip it, or repeat it.
func (c writerClock) instant(layout, s string) (time.Time, error) {
	t, err := time.ParseInLocation(layout, s, c.loc)
	if err != nil {
		return time.Time{}, err
	}
	wall, err := time.ParseInLocation(layout, s, time.UTC)
	if err != nil {
		return time.Time{}, err
	}
	want := wall.Format(wallLayout)
	if t.In(c.loc).Format(wallLayout) != want {
		return time.Time{}, fmt.Errorf("%q never happened in %s: the clocks skipped it (daylight saving); "+
			"ask the person for the time again, or pass zone as the offset they were at", s, c.name)
	}
	_, off := t.Zone()
	for _, probe := range []time.Time{t.Add(-3 * time.Hour), t.Add(3 * time.Hour)} {
		_, other := probe.Zone()
		if other == off {
			continue
		}
		if t.Add(time.Duration(off-other)*time.Second).In(c.loc).Format(wallLayout) == want {
			return time.Time{}, fmt.Errorf("%q happened twice in %s: the clocks went back (daylight saving); "+
				"ask the person which, and pass zone as that offset (%s or %s)", s, c.name,
				t.Format("-07:00"), t.Add(time.Duration(off-other)*time.Second).In(c.loc).Format("-07:00"))
		}
	}
	return t, nil
}

// sourceTime is the source_time a write sends for the wall clock value at t.
func (c writerClock) sourceTime(value string, t time.Time) map[string]any {
	return map[string]any{"value": value, "zone": c.name, "offset": t.In(c.loc).Format("-07:00"), "basis": c.basis}
}
