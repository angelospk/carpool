package domain

import (
	"errors"
	"testing"
	"time"
)

// Eighteen on the day counts. The rule is "has completed eighteen years", not
// "is older than eighteen", so the birthday itself is the first eligible day.
func TestEighteenOnTheDayIsEighteen(t *testing.T) {
	born := Date{2008, time.March, 15}

	for _, tt := range []struct {
		on    Date
		adult bool
		why   string
	}{
		{Date{2026, time.March, 14}, false, "the day before the eighteenth birthday"},
		{Date{2026, time.March, 15}, true, "the eighteenth birthday itself"},
		{Date{2026, time.March, 16}, true, "the day after"},
		{Date{2025, time.December, 31}, false, "a few months short"},
		{Date{2040, time.January, 1}, true, "long after"},
	} {
		if got := IsAdultOn(born, tt.on); got != tt.adult {
			t.Errorf("born %v, on %v: adult = %v, want %v (%s)", born, tt.on, got, tt.adult, tt.why)
		}
	}
}

// Born on a leap day, in a year that has no 29 February.
//
// 2008 was a leap year and 2026 is not, so there is no eighteenth birthday to
// land on. The rule adds eighteen years and takes what the calendar gives,
// which is 1 March: the later of the two candidate days, and so the one that
// never lets somebody travel a day before they are eighteen.
func TestALeapDayBirthdayFallsOnTheFirstOfMarch(t *testing.T) {
	born := Date{2008, time.February, 29}

	if IsAdultOn(born, Date{2026, time.February, 28}) {
		t.Error("adult on 28 February, the day before any candidate birthday")
	}
	if IsAdultOn(born, Date{2026, time.March, 1}) != true {
		t.Error("not adult on 1 March, which is the eighteenth birthday in a common year")
	}
}

// The comparison is between two civil dates and nothing else. A birth date is
// the day printed on somebody's ID, not an instant, and reading it as midnight
// somewhere turns "born on the 15th" into "born on the 14th" for anybody east
// or west of whoever wrote the code.
func TestTheRuleIsAboutCalendarDaysNotInstants(t *testing.T) {
	born := Date{2008, time.March, 15}

	// 23:30 in Athens on the day before the birthday is already the next day in
	// UTC. The Athens date is the one that counts.
	athens, err := time.LoadLocation("Europe/Athens")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	late := time.Date(2026, time.March, 14, 23, 30, 0, 0, athens)

	if IsAdultOn(born, DateIn(late, athens)) {
		t.Error("half past eleven on the 14th in Athens counted as the 15th")
	}
	if !IsAdultOn(born, DateIn(late.Add(time.Hour), athens)) {
		t.Error("half past midnight on the 15th in Athens did not count as the 15th")
	}
}

func TestABirthDateFromNowhereIsRefused(t *testing.T) {
	today := Date{2026, time.August, 5}

	for _, in := range []string{
		"", "15/03/2008", "2008-3-15", "2008-13-01", "2008-02-30", "yesterday",
		"2027-01-01", // the future
		"1899-12-31", // the day before the bound the database enforces
	} {
		if _, err := ParseBirthDate(in, today); !errors.Is(err, ErrImplausibleBirthDate) {
			t.Errorf("ParseBirthDate(%q) gave %v, want ErrImplausibleBirthDate", in, err)
		}
	}

	got, err := ParseBirthDate("2008-03-15", today)
	if err != nil {
		t.Fatalf("ParseBirthDate on a real date: %v", err)
	}
	if got != (Date{2008, time.March, 15}) {
		t.Errorf("parsed %v", got)
	}
}

// The bound is a fixed date and it is the same fixed date the database has.
//
// It used to be "a hundred and thirty years ago", which moves, and which let
// 1899 through here to be refused by a CHECK constraint one layer down — a 500
// where a 400 was meant. Two bounds that agree today and disagree next year are
// worse than either bound on its own.
func TestTheOldestPlausibleDateIsTheOneTheDatabaseAgreesWith(t *testing.T) {
	today := Date{2026, time.August, 5}

	if _, err := ParseBirthDate("1900-01-01", today); err != nil {
		t.Errorf("the boundary date itself was refused: %v", err)
	}
	if _, err := ParseBirthDate("1899-12-31", today); !errors.Is(err, ErrImplausibleBirthDate) {
		t.Errorf("the day before the boundary was accepted: %v", err)
	}
}

// Today is a real birth date: somebody born this morning is a plausible person,
// just not one who can sign up. The two questions stay separate so that the
// error can say which one was the problem.
func TestBeingBornTodayIsPlausibleButNotAdult(t *testing.T) {
	today := Date{2026, time.August, 5}

	got, err := ParseBirthDate("2026-08-05", today)
	if err != nil {
		t.Fatalf("today was rejected as implausible: %v", err)
	}
	if IsAdultOn(got, today) {
		t.Error("somebody born today is an adult")
	}
}
