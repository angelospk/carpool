package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"carpool/internal/account"
)

// What somebody who shared a trip can still see of a person who has left.
//
// Erasure now takes the photograph, the bio and the preferences, so in an
// unbroken system there is nothing here to find. This is the other half: the
// endpoints themselves refusing a deleted account, so that a row erasure missed
// — one written by hand, one added by a migration nobody thought about — is not
// served to the first person who asks for it by id.
//
// The fixture is deliberately inconsistent for exactly that reason. The account
// is marked deleted while the photograph is still in the table, which is a state
// the erasure cannot produce. A test that deleted the account properly would
// pass with this check removed, because there would be no bytes to serve.

func TestAPhotographIsNotServedForAnAccountThatHasGone(t *testing.T) {
	h := newHarness(t)
	_, passenger, _ := h.bookedTrip(t)

	var driverID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000031'`, &driverID)
	h.mustExec(`
		INSERT INTO profile_photos (user_id, data, content_type, sha256, width, height)
		VALUES ($1, '\x89504e470d0a1a0a'::bytea, 'image/jpeg', sha256('x'::bytea), 400, 400)`, driverID)

	// Visible first, so the refusal below is the status doing it and not a
	// missing row.
	if rec := h.do(http.MethodGet, "/v1/users/"+driverID.String()+"/photo", nil, passenger); rec.Code != http.StatusOK {
		t.Fatalf("the photo of an ordinary driver is %d, want 200", rec.Code)
	}

	h.mustExec(`UPDATE users SET status = 'deleted' WHERE id = $1`, driverID)

	rec := h.do(http.MethodGet, "/v1/users/"+driverID.String()+"/photo", nil, passenger)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("the photo of a closed account is %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() > 200 {
		t.Fatalf("%d bytes came back for a closed account", rec.Body.Len())
	}
}

// A suspension is not an erasure and must not behave like one: the person is
// coming back, and the people they are already carrying still need to recognise
// them at the kerb.
func TestAPhotographSurvivesASuspension(t *testing.T) {
	h := newHarness(t)
	_, passenger, _ := h.bookedTrip(t)

	var driverID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000031'`, &driverID)
	h.mustExec(`
		INSERT INTO profile_photos (user_id, data, content_type, sha256, width, height)
		VALUES ($1, '\x89504e470d0a1a0a'::bytea, 'image/jpeg', sha256('x'::bytea), 400, 400)`, driverID)
	h.mustExec(`UPDATE users SET status = 'suspended' WHERE id = $1`, driverID)

	if rec := h.do(http.MethodGet, "/v1/users/"+driverID.String()+"/photo", nil, passenger); rec.Code != http.StatusOK {
		t.Fatalf("the photo of a suspended driver is %d, want 200", rec.Code)
	}
}

// A ride that already happened, read by the person who took it after the driver
// closed their account.
//
// This is the case that stays reachable. A future departure is called off when
// its driver leaves and the cancelled booking drops out of both the history list
// and the trip endpoint, so there is no dead link to follow; a completed ride
// stays in the passenger's history for as long as the retention rules keep it,
// and opening it has to answer rather than fall over, with nothing left in it
// that names the person who left.
func TestARideAlreadyTakenSaysNothingAboutTheDriverWhoLeft(t *testing.T) {
	h := newHarness(t)
	_, passenger, occurrence := h.bookedTrip(t)

	var driverID uuid.UUID
	h.mustQuery(`SELECT id FROM users WHERE phone = '+306900000031'`, &driverID)
	h.mustExec(`UPDATE users SET bio = 'Δουλεύω στο νοσοκομείο',
	                             facebook_handle = 'giannis.odigos',
	                             instagram_handle = 'giannis_ig'
	             WHERE id = $1`, driverID)
	h.mustExec(`INSERT INTO user_preferences (user_id, tag) VALUES ($1, 'no_smoking')`, driverID)

	// The ride happened: it left, and the seat was taken.
	h.mustExec(`UPDATE trip_occurrences SET depart_earliest = $2::timestamptz - interval '2 hours',
	                                        depart_latest   = $2::timestamptz - interval '1 hour'
	             WHERE id = $1`, occurrence, h.clock.Now())
	h.mustExec(`UPDATE bookings SET status = 'completed' WHERE occurrence_id = $1`, occurrence)

	// Everything the erasure does, through the service that does it.
	if err := account.NewService(h.pool, h.clock).Delete(context.Background(), driverID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	rec := h.do(http.MethodGet, "/v1/trips/"+occurrence.String()+"/", nil, passenger)
	if rec.Code != http.StatusOK {
		t.Fatalf("reading the trip after the driver left is %d, want 200: %s", rec.Code, rec.Body.String())
	}
	for _, leak := range []string{
		"νοσοκομείο", "+306900000031", "ΑΒΓ-1234", "ABC-1234",
		"giannis.odigos", "giannis_ig", "no_smoking",
	} {
		if body := rec.Body.String(); strings.Contains(body, leak) {
			t.Fatalf("the trip still names the person who left: %q appears in %s", leak, body)
		}
	}
}
