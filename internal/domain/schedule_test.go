package domain

import (
	"testing"
	"time"
)

func recurring(weekdays ...time.Weekday) Schedule {
	return Schedule{
		Kind:           ScheduleRecurring,
		Weekdays:       weekdays,
		DepartEarliest: TimeOfDay{Hour: 7, Minute: 30},
		DepartLatest:   TimeOfDay{Hour: 8, Minute: 0},
		EffectiveFrom:  Date{2026, time.July, 1},
	}
}

func TestPlanGeneratesOneOccurrencePerMatchingWeekdayInTheHorizon(t *testing.T) {
	loc := athens(t)
	// 2026-07-06 is a Monday. A 21-day half-open horizon starting that Monday
	// covers Mondays on the 6th, 13th and 20th — the 27th falls outside.
	from := Date{2026, time.July, 6}
	to := from.AddDays(21)

	got, errs := Plan(recurring(time.Monday), loc, from, to)

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 3 {
		t.Fatalf("got %d occurrences, want 3: %v", len(got), got)
	}
	for i, want := range []Date{{2026, time.July, 6}, {2026, time.July, 13}, {2026, time.July, 20}} {
		if got[i].ServiceDate != want {
			t.Errorf("occurrence %d service date = %v, want %v", i, got[i].ServiceDate, want)
		}
	}
}

func TestPlanTreatsTheHorizonEndAsExclusive(t *testing.T) {
	loc := athens(t)
	from := Date{2026, time.July, 6}
	// End the horizon exactly on a Monday: that Monday must not be included.
	to := Date{2026, time.July, 13}

	got, _ := Plan(recurring(time.Monday), loc, from, to)

	if len(got) != 1 {
		t.Fatalf("got %d occurrences, want 1 (the 13th is excluded): %v", len(got), got)
	}
	if got[0].ServiceDate != (Date{2026, time.July, 6}) {
		t.Errorf("service date = %v, want 2026-07-06", got[0].ServiceDate)
	}
}

func TestPlanResolvesDepartureToConcreteInstants(t *testing.T) {
	loc := athens(t)
	from := Date{2026, time.July, 6}

	got, _ := Plan(recurring(time.Monday), loc, from, from.AddDays(1))

	if len(got) != 1 {
		t.Fatalf("got %d occurrences, want 1", len(got))
	}
	// July is EEST (UTC+3).
	wantEarliest := time.Date(2026, time.July, 6, 4, 30, 0, 0, time.UTC)
	wantLatest := time.Date(2026, time.July, 6, 5, 0, 0, 0, time.UTC)
	if !got[0].DepartEarliest.Equal(wantEarliest) {
		t.Errorf("DepartEarliest = %v, want %v", got[0].DepartEarliest.UTC(), wantEarliest)
	}
	if !got[0].DepartLatest.Equal(wantLatest) {
		t.Errorf("DepartLatest = %v, want %v", got[0].DepartLatest.UTC(), wantLatest)
	}
}

func TestPlanSkipsExceptionDates(t *testing.T) {
	loc := athens(t)
	s := recurring(time.Monday)
	s.Exceptions = []Date{{2026, time.July, 13}}
	from := Date{2026, time.July, 6}

	got, _ := Plan(s, loc, from, from.AddDays(21))

	if len(got) != 2 {
		t.Fatalf("got %d occurrences, want 2 with the 13th excepted: %v", len(got), got)
	}
	for _, o := range got {
		if o.ServiceDate == (Date{2026, time.July, 13}) {
			t.Error("the excepted date was generated anyway")
		}
	}
}

func TestPlanHonoursTheEffectiveRange(t *testing.T) {
	loc := athens(t)
	s := recurring(time.Monday)
	s.EffectiveFrom = Date{2026, time.July, 13}
	until := Date{2026, time.July, 13}
	s.EffectiveUntil = &until
	from := Date{2026, time.July, 6}

	got, _ := Plan(s, loc, from, from.AddDays(21))

	if len(got) != 1 {
		t.Fatalf("got %d occurrences, want only the one inside the effective range: %v", len(got), got)
	}
	if got[0].ServiceDate != (Date{2026, time.July, 13}) {
		t.Errorf("service date = %v, want 2026-07-13", got[0].ServiceDate)
	}
}

func TestPlanReportsAnErrorAndSkipsANonexistentLocalDepartureTime(t *testing.T) {
	loc := athens(t)
	// 2026-03-29 is the spring-forward Sunday; 03:30 local never happens.
	s := Schedule{
		Kind:           ScheduleRecurring,
		Weekdays:       []time.Weekday{time.Sunday},
		DepartEarliest: TimeOfDay{Hour: 3, Minute: 30},
		DepartLatest:   TimeOfDay{Hour: 4, Minute: 30},
		EffectiveFrom:  Date{2026, time.March, 1},
	}
	from := Date{2026, time.March, 29}

	got, errs := Plan(s, loc, from, from.AddDays(1))

	if len(got) != 0 {
		t.Fatalf("a skipped wall time must not produce an occurrence, got %v", got)
	}
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1 recording the skipped date: %v", len(errs), errs)
	}
}

func TestPlanPicksTheEarlierInstantForARepeatedLocalDepartureTime(t *testing.T) {
	loc := athens(t)
	// 2026-10-25 is the fall-back Sunday; 03:30 local happens twice.
	s := Schedule{
		Kind:           ScheduleRecurring,
		Weekdays:       []time.Weekday{time.Sunday},
		DepartEarliest: TimeOfDay{Hour: 3, Minute: 30},
		DepartLatest:   TimeOfDay{Hour: 5, Minute: 0},
		EffectiveFrom:  Date{2026, time.October, 1},
	}
	from := Date{2026, time.October, 25}

	got, errs := Plan(s, loc, from, from.AddDays(1))

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 1 {
		t.Fatalf("a repeated wall time must produce exactly one occurrence, got %d: %v", len(got), got)
	}
	// The earlier of the two passes is still EEST (UTC+3) -> 00:30 UTC.
	want := time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC)
	if !got[0].DepartEarliest.Equal(want) {
		t.Errorf("DepartEarliest = %v, want the earlier instant %v", got[0].DepartEarliest.UTC(), want)
	}
}

func TestPlanGeneratesASingleDateForAOneOffSchedule(t *testing.T) {
	loc := athens(t)
	date := Date{2026, time.July, 8}
	s := Schedule{
		Kind:           ScheduleOneOff,
		OneOffDate:     &date,
		DepartEarliest: TimeOfDay{Hour: 7, Minute: 30},
		DepartLatest:   TimeOfDay{Hour: 8, Minute: 0},
		EffectiveFrom:  Date{2026, time.July, 1},
	}
	from := Date{2026, time.July, 6}

	got, _ := Plan(s, loc, from, from.AddDays(21))

	if len(got) != 1 {
		t.Fatalf("got %d occurrences, want exactly 1: %v", len(got), got)
	}
	if got[0].ServiceDate != date {
		t.Errorf("service date = %v, want %v", got[0].ServiceDate, date)
	}
}

func TestPlanExcludesAOneOffDateOutsideTheHorizon(t *testing.T) {
	loc := athens(t)
	date := Date{2026, time.September, 1}
	s := Schedule{
		Kind:           ScheduleOneOff,
		OneOffDate:     &date,
		DepartEarliest: TimeOfDay{Hour: 7, Minute: 30},
		DepartLatest:   TimeOfDay{Hour: 8, Minute: 0},
		EffectiveFrom:  Date{2026, time.July, 1},
	}
	from := Date{2026, time.July, 6}

	got, _ := Plan(s, loc, from, from.AddDays(21))

	if len(got) != 0 {
		t.Fatalf("a one-off outside the horizon must not be generated, got %v", got)
	}
}

func TestPlanCrossesAMonthBoundary(t *testing.T) {
	loc := athens(t)
	// 2026-07-27 is a Monday; the next is 2026-08-03.
	from := Date{2026, time.July, 27}

	got, _ := Plan(recurring(time.Monday), loc, from, from.AddDays(14))

	if len(got) != 2 {
		t.Fatalf("got %d occurrences, want 2 across the month boundary: %v", len(got), got)
	}
	if got[1].ServiceDate != (Date{2026, time.August, 3}) {
		t.Errorf("second service date = %v, want 2026-08-03", got[1].ServiceDate)
	}
}

func TestPlanIsDeterministicAcrossRuns(t *testing.T) {
	loc := athens(t)
	from := Date{2026, time.July, 6}

	first, _ := Plan(recurring(time.Monday, time.Wednesday, time.Friday), loc, from, from.AddDays(21))
	second, _ := Plan(recurring(time.Monday, time.Wednesday, time.Friday), loc, from, from.AddDays(21))

	if len(first) != len(second) {
		t.Fatalf("run lengths differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ServiceDate != second[i].ServiceDate || !first[i].DepartEarliest.Equal(second[i].DepartEarliest) {
			t.Fatalf("occurrence %d differs between runs: %v vs %v", i, first[i], second[i])
		}
	}
}

func TestPlanOrdersOccurrencesChronologically(t *testing.T) {
	loc := athens(t)
	from := Date{2026, time.July, 6}

	got, _ := Plan(recurring(time.Friday, time.Monday, time.Wednesday), loc, from, from.AddDays(21))

	if len(got) < 2 {
		t.Fatalf("expected several occurrences, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if !got[i-1].DepartEarliest.Before(got[i].DepartEarliest) {
			t.Fatalf("occurrence %d (%v) does not precede %d (%v) despite unordered weekday input",
				i-1, got[i-1].DepartEarliest, i, got[i].DepartEarliest)
		}
	}
}
