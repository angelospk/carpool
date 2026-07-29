package domain

import (
	"fmt"
	"sort"
	"time"
)

// Date is a calendar date with no timezone attached. Occurrence identity is
// keyed on a service date rather than on a concrete instant, so that a tzdata
// update or a DST policy change cannot silently duplicate a day's trip.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// AddDays returns the date d days later.
func (d Date) AddDays(days int) Date {
	t := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, days)
	return Date{t.Year(), t.Month(), t.Day()}
}

// Weekday reports the day of the week.
func (d Date) Weekday() time.Weekday {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).Weekday()
}

// Before reports whether d falls before other.
func (d Date) Before(other Date) bool { return d.compare(other) < 0 }

// After reports whether d falls after other.
func (d Date) After(other Date) bool { return d.compare(other) > 0 }

func (d Date) compare(other Date) int {
	switch {
	case d.Year != other.Year:
		return d.Year - other.Year
	case d.Month != other.Month:
		return int(d.Month) - int(other.Month)
	default:
		return d.Day - other.Day
	}
}

// String renders the date as YYYY-MM-DD.
func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day) }

// TimeOfDay is a local wall-clock time with no date or zone attached.
type TimeOfDay struct {
	Hour   int
	Minute int
}

// ScheduleKind distinguishes a single-date schedule from a weekly recurrence.
type ScheduleKind string

const (
	ScheduleOneOff    ScheduleKind = "oneoff"
	ScheduleRecurring ScheduleKind = "recurring"
)

// Schedule is the recurrence rule attached to a route template.
type Schedule struct {
	Kind ScheduleKind

	// OneOffDate is set when Kind is ScheduleOneOff.
	OneOffDate *Date
	// Weekdays is set when Kind is ScheduleRecurring.
	Weekdays []time.Weekday

	DepartEarliest TimeOfDay
	DepartLatest   TimeOfDay

	EffectiveFrom  Date
	EffectiveUntil *Date
	Exceptions     []Date
}

// PlannedOccurrence is one concrete trip the materializer should ensure exists.
type PlannedOccurrence struct {
	ServiceDate    Date
	DepartEarliest time.Time
	DepartLatest   time.Time
}

// Plan expands a schedule into the concrete occurrences falling in the
// half-open horizon [from, to).
//
// The horizon is expressed in calendar dates rather than as a duration, because
// "now + 21*24h" is wrong across a DST transition.
//
// Dates whose local departure time does not exist (skipped by a spring-forward)
// are omitted and reported in the returned errors; the caller records them
// rather than silently shifting the trip. Where a local time is repeated by a
// fall-back, the earlier instant is chosen deterministically.
func Plan(s Schedule, loc *time.Location, from, to Date) ([]PlannedOccurrence, []error) {
	var out []PlannedOccurrence
	var errs []error

	for d := from; d.Before(to); d = d.AddDays(1) {
		if !s.covers(d) {
			continue
		}

		earliest, err := resolveOne(loc, d, s.DepartEarliest)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		latest, err := resolveOne(loc, d, s.DepartLatest)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		out = append(out, PlannedOccurrence{
			ServiceDate:    d,
			DepartEarliest: earliest,
			DepartLatest:   latest,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].DepartEarliest.Before(out[j].DepartEarliest) })
	return out, errs
}

// covers reports whether the schedule should produce an occurrence on d.
func (s Schedule) covers(d Date) bool {
	if d.Before(s.EffectiveFrom) {
		return false
	}
	if s.EffectiveUntil != nil && d.After(*s.EffectiveUntil) {
		return false
	}
	for _, ex := range s.Exceptions {
		if ex == d {
			return false
		}
	}

	switch s.Kind {
	case ScheduleOneOff:
		return s.OneOffDate != nil && *s.OneOffDate == d
	case ScheduleRecurring:
		for _, wd := range s.Weekdays {
			if wd == d.Weekday() {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// resolveOne turns a local wall time on a date into a single instant, applying
// the documented policy for skipped and repeated times.
func resolveOne(loc *time.Location, d Date, tod TimeOfDay) (time.Time, error) {
	instants := ResolveWallTime(loc, d.Year, d.Month, d.Day, tod.Hour, tod.Minute)
	if len(instants) == 0 {
		return time.Time{}, fmt.Errorf("schedule: local time %02d:%02d does not exist on %s in %s (skipped by a DST transition)",
			tod.Hour, tod.Minute, d, loc)
	}
	// ResolveWallTime sorts earliest first, so this is the earlier pass when the
	// wall time was repeated by a fall-back.
	return instants[0], nil
}
