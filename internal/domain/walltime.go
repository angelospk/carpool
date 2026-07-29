// Package domain holds entities and business rules. It must not import
// infrastructure packages.
package domain

import (
	"sort"
	"time"
)

// ResolveWallTime returns every UTC instant that presents as the given local
// wall time in loc.
//
// The length of the result carries the meaning:
//
//	0 — the wall time does not exist (skipped by a spring-forward transition)
//	1 — the ordinary case
//	2 — the wall time happens twice (repeated by a fall-back transition)
//
// Results are sorted earliest first.
//
// time.Date is deliberately not trusted here: Go documents that for skipped or
// repeated wall times the zone it picks is not guaranteed, which would make
// recurrence generation silently implementation-defined twice a year. Instead
// each plausible UTC offset around the date is applied and the candidate is kept
// only if it round-trips back to exactly the requested local components.
func ResolveWallTime(loc *time.Location, year int, month time.Month, day, hour, minute int) []time.Time {
	// The requested components read as if they were UTC. Subtracting a zone
	// offset from this turns them into a candidate real instant.
	naive := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)

	// Probe a day either side so both the pre- and post-transition offsets are
	// considered on a transition date.
	probes := []time.Time{
		naive.Add(-24 * time.Hour),
		naive,
		naive.Add(24 * time.Hour),
	}

	seen := make(map[int64]struct{}, len(probes))
	var out []time.Time

	for _, probe := range probes {
		_, offset := probe.In(loc).Zone()
		candidate := naive.Add(-time.Duration(offset) * time.Second)

		local := candidate.In(loc)
		if local.Year() != year || local.Month() != month || local.Day() != day ||
			local.Hour() != hour || local.Minute() != minute {
			continue
		}
		if _, dup := seen[candidate.Unix()]; dup {
			continue
		}
		seen[candidate.Unix()] = struct{}{}
		out = append(out, candidate)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}
