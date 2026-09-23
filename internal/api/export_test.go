package api_test

import (
	"encoding/csv"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// exportWorld is a booked trip whose driver runs a group, with a second
// passenger on the same departure who is not in it.
type exportWorld struct {
	h                           *harness
	driver, passenger, outsider string
	group                       groupBody
	occurrenceID                uuid.UUID
}

func newExportWorld(t *testing.T) *exportWorld {
	h := newHarness(t)
	driver, passenger, occurrenceID := h.bookedTrip(t)
	outsider := h.addSecondPassenger(t, occurrenceID)
	g := h.createGroup(driver, "Νοσοκομείο")
	h.joinGroup(passenger, *g.Code)
	// Both sides confirmed for everybody, so what decides a row is membership.
	h.mustExec(`UPDATE bookings SET driver_confirmed = true, passenger_confirmed = true`)
	return &exportWorld{h: h, driver: driver, passenger: passenger, outsider: outsider, group: g, occurrenceID: occurrenceID}
}

func (w *exportWorld) export(t *testing.T, token, month string) (int, [][]string) {
	t.Helper()
	rec := w.h.do(http.MethodGet, "/v1/groups/"+w.group.ID+"/trips.csv?month="+month, nil, token)
	if rec.Code != http.StatusOK {
		return rec.Code, nil
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q", ct)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("missing cache or sniffing headers: %v", rec.Header())
	}
	body := strings.TrimPrefix(rec.Body.String(), "\uFEFF")
	rows, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("not CSV: %v\n%s", err, body)
	}
	return rec.Code, rows
}

// The one trip between two members, and not the seat beside it taken by
// somebody who never joined: the export must not tell an employer where their
// employee went with a stranger who never agreed to be counted.
func TestTheExportListsOnlyConfirmedTripsBetweenMembers(t *testing.T) {
	w := newExportWorld(t)

	code, rows := w.export(t, w.driver, "2026-07")
	if code != http.StatusOK {
		t.Fatalf("export: %d", code)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want header + 1:\n%v", len(rows), rows)
	}
	want := []string{"2026-07-30", "07:30", "A", "B", "17.0", "Οδηγός", "Επιβάτης", "1"}
	for i := range want {
		if rows[1][i] != want[i] {
			t.Fatalf("row = %v, want %v", rows[1], want)
		}
	}
	for _, row := range rows {
		for _, cell := range row {
			if strings.Contains(cell, "Δεύτερος") || strings.Contains(cell, "+30") {
				t.Fatalf("the export carries %q", cell)
			}
		}
	}

	// Another month is empty.
	if _, rows := w.export(t, w.driver, "2026-08"); len(rows) != 1 {
		t.Fatalf("August has %d rows, want only the header", len(rows))
	}
}

// One side's word is not a trip that happened.
func TestATripOnlyOneSideConfirmedIsNotExported(t *testing.T) {
	w := newExportWorld(t)
	w.h.mustExec(`UPDATE bookings SET passenger_confirmed = false`)
	if _, rows := w.export(t, w.driver, "2026-07"); len(rows) != 1 {
		t.Fatalf("%d rows for a trip only the driver confirmed", len(rows)-1)
	}
}

// Membership on the day, not today: somebody who joined after the trip ran was
// not a colleague on it.
func TestSomebodyWhoJoinedAfterTheTripIsNotCountedForIt(t *testing.T) {
	w := newExportWorld(t)
	w.h.mustExec(`UPDATE group_members SET joined_at = '2026-08-01' WHERE role = 'member'`)
	if _, rows := w.export(t, w.driver, "2026-07"); len(rows) != 1 {
		t.Fatalf("%d rows for a trip taken before the passenger joined", len(rows)-1)
	}
}

func TestOnlyAnAdminCanExport(t *testing.T) {
	w := newExportWorld(t)
	if code, _ := w.export(t, w.passenger, "2026-07"); code != http.StatusForbidden {
		t.Errorf("a member exporting: %d, want 403", code)
	}
	if code, _ := w.export(t, w.outsider, "2026-07"); code != http.StatusNotFound {
		t.Errorf("an outsider exporting: %d, want 404", code)
	}
	if code, _ := w.export(t, w.driver, "July"); code != http.StatusBadRequest {
		t.Errorf("a malformed month: %d, want 400", code)
	}
}

// A name is typed by a member and opened by the admin in a spreadsheet, which
// runs a cell that starts with = as a formula on the admin's own machine.
func TestANameThatIsAFormulaArrivesAsText(t *testing.T) {
	w := newExportWorld(t)
	w.h.mustExec(`UPDATE users SET name = ' =HYPERLINK("http://x","y")' WHERE phone = '+306900000032'`)
	_, rows := w.export(t, w.driver, "2026-07")
	if got := rows[1][6]; !strings.HasPrefix(got, "'") {
		t.Fatalf("passenger cell = %q, want it defused with a leading apostrophe", got)
	}
}
