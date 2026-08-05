package domain

import (
	"errors"
	"fmt"
	"time"
)

// AdultYears is how old somebody must be to use this app at all.
//
// Eighteen, which is where Greek law puts adulthood and where every carpooling
// service in the country puts its own line. The alternative — minors travelling
// with an adult family member, which is what GoRide allows — needs the app to
// know that the adult beside them is a family member, and nothing in a booking
// can tell it that. A rule the app cannot check is a rule it should not claim
// to have.
const AdultYears = 18

// ErrImplausibleBirthDate means the text was not a date, or was a date nobody
// could have been born on.
var ErrImplausibleBirthDate = errors.New("domain: that is not a date somebody was born on")

// oldestPlausibleDate bounds the other end.
//
// The point is not demography — it is that a typo in the year is far more
// likely than a supercentenarian, and a birth date of 1808 should be refused
// rather than stored.
//
// A fixed date rather than "a hundred and thirty years ago", and the same fixed
// date as the CHECK on users.date_of_birth in migration 00025. A relative bound
// moves: it would let 1899 past this function next year to be refused by the
// database instead, which is a 500 where a 400 was meant.
var oldestPlausibleDate = Date{1900, time.January, 1}

// DateIn is the civil date at an instant, in a given place.
//
// Which place matters. Half past eleven at night in Athens is already tomorrow
// in UTC, and a rule about somebody's birthday must not turn on where the
// server happens to be.
func DateIn(t time.Time, loc *time.Location) Date {
	local := t.In(loc)
	return Date{local.Year(), local.Month(), local.Day()}
}

// IsAdultOn reports whether somebody born on the given date has completed
// AdultYears by the given day.
//
// The birthday itself counts: eighteen years completed on the fifteenth means
// the fifteenth is the first eligible day, not the sixteenth.
//
// Somebody born on 29 February has no birthday at all in a common year. Adding
// eighteen years to it lands on 1 March, which is what Go's calendar
// arithmetic gives and also the answer that is never wrong in the dangerous
// direction: it is the later of the two candidate days, so nobody travels a day
// before they are eighteen.
func IsAdultOn(born, on Date) bool {
	eighteenth := born.asTime().AddDate(AdultYears, 0, 0)
	return !on.asTime().Before(eighteenth)
}

// ParseBirthDate reads a YYYY-MM-DD date and checks that a person could have
// been born on it.
//
// "Could have been born on it" is a different question from "is old enough",
// and they are kept apart so that the caller can say which one failed. Being
// born this morning is entirely plausible; it is just not enough to sign up.
func ParseBirthDate(in string, today Date) (Date, error) {
	// time.Parse with a fixed layout rejects "2008-3-15" and "2008-02-30" both,
	// the second because it refuses to roll a date over into March.
	t, err := time.Parse("2006-01-02", in)
	if err != nil {
		return Date{}, fmt.Errorf("%w: %q", ErrImplausibleBirthDate, in)
	}
	born := Date{t.Year(), t.Month(), t.Day()}

	if born.asTime().After(today.asTime()) {
		return Date{}, fmt.Errorf("%w: %s is in the future", ErrImplausibleBirthDate, in)
	}
	if born.asTime().Before(oldestPlausibleDate.asTime()) {
		return Date{}, fmt.Errorf("%w: %s is before %04d-%02d-%02d",
			ErrImplausibleBirthDate, in,
			oldestPlausibleDate.Year, oldestPlausibleDate.Month, oldestPlausibleDate.Day)
	}
	return born, nil
}

// asTime pins a civil date to midnight UTC purely so that the calendar
// arithmetic in this file has something to work with. It is never a real
// instant and must not be treated as one — see DateIn for that.
func (d Date) asTime() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}
