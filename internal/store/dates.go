package store

import (
	"fmt"
	"time"

	"carpool/internal/domain"
)

// parseDate reads a YYYY-MM-DD value as returned by a ::text cast on a date.
func parseDate(s string) (domain.Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return domain.Date{}, fmt.Errorf("store: parsing date %q: %w", s, err)
	}
	return domain.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, nil
}

// isoWeekday converts Go's Sunday=0 numbering to the ISO Monday=1..Sunday=7
// used by the weekdays column.
func isoWeekday(w time.Weekday) int {
	if w == time.Sunday {
		return 7
	}
	return int(w)
}

// fromISOWeekday is the inverse of isoWeekday.
func fromISOWeekday(iso int) time.Weekday {
	if iso == 7 {
		return time.Sunday
	}
	return time.Weekday(iso)
}
