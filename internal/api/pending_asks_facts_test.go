package api_test

import (
	"net/http"
	"testing"
	"time"
)

type askFacts struct {
	PassengerPhotoURL *string  `json:"passenger_photo_url"`
	PassengerRating   *float64 `json:"passenger_rating"`
	RatingCount       int      `json:"passenger_rating_count"`
	SharedGroups      []string `json:"shared_groups"`
	Luggage           string   `json:"luggage"`
	LuggageCount      int      `json:"luggage_count"`
	WindowStart       string   `json:"window_start"`
	WindowEnd         string   `json:"window_end"`
}

// The driver decides on the ask alone, so it carries who is asking: a face, a
// rating, the groups they share, what is in the boot and when they can go.
func TestAskCarriesWhatTheDriverDecidesOn(t *testing.T) {
	h := newHarness(t)
	occurrence := h.occurrenceOf("+306900000331", "Οδηγός")
	driver, _ := h.signUp("+306900000331", "Οδηγός")
	h.signUp("+306900000332", "Ελένη")
	h.signUp("+306900000333", "Κριτής")
	var passengerID, driverID, raterID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000332'`, &passengerID)
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000331'`, &driverID)
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000333'`, &raterID)

	request := h.openRequest(passengerID, "asking")
	h.mustExec(`UPDATE ride_requests SET luggage = 'suitcase', luggage_count = 2 WHERE id = $1::uuid`, request)
	h.ask(request, occurrence, "pending", 10*time.Minute)

	// A past booking of the passenger, rated 5 and 4.
	h.mustExec(`INSERT INTO ride_requests (passenger_id, origin, destination, window_start, window_end, kind, status)
		VALUES ($1::uuid, ST_SetSRID(ST_MakePoint(25.8,40.8),4326), ST_SetSRID(ST_MakePoint(26.1,40.9),4326),
		        now(), now() + interval '1 hour', 'open', 'matched')`, passengerID)
	h.mustExec(`INSERT INTO asks (request_id, occurrence_id, detour_min, detour_km, pickup_point, dropoff_point,
	                              suggested_price_eur, itinerary_version, status, expires_at)
		SELECT r.id, $2::uuid, 1, 1, ST_SetSRID(ST_MakePoint(25.8,40.8),4326), ST_SetSRID(ST_MakePoint(26.1,40.9),4326),
		       1, 1, 'accepted', now() FROM ride_requests r WHERE r.passenger_id = $1::uuid AND r.status = 'matched'`,
		passengerID, occurrence)
	h.mustExec(`INSERT INTO bookings (occurrence_id, request_id, passenger_id, ask_id, seats, price_eur, pickup_point, dropoff_point)
		SELECT $2::uuid, a.request_id, $1::uuid, a.id, 1, 1, a.pickup_point, a.dropoff_point
		FROM asks a WHERE a.status = 'accepted'`, passengerID, occurrence)
	h.mustExec(`INSERT INTO ratings (booking_id, rater_id, ratee_id, stars)
		SELECT b.id, $1::uuid, $3::uuid, 5 FROM bookings b
		UNION ALL SELECT b.id, $2::uuid, $3::uuid, 4 FROM bookings b`, driverID, raterID, passengerID)

	// A group both belong to, and one only the passenger belongs to.
	h.mustExec(`INSERT INTO groups (name, join_code) VALUES ('Ομάδα Α', 'AAAAAA'), ('Ομάδα Β', 'BBBBBB')`)
	h.mustExec(`INSERT INTO group_members (group_id, user_id, role, joined_at)
		SELECT g.id, u.id, 'member', now() FROM groups g, users u
		WHERE (g.name = 'Ομάδα Α' AND u.id IN ($1::uuid, $2::uuid)) OR (g.name = 'Ομάδα Β' AND u.id = $2::uuid)`,
		driverID, passengerID)
	h.mustExec(`INSERT INTO profile_photos (user_id, data, content_type, sha256, width, height)
		VALUES ($1::uuid, '\x00', 'image/jpeg', sha256('x'), 1, 1)`, passengerID)

	check := func(label string, a askFacts) {
		t.Helper()
		if a.PassengerPhotoURL == nil || *a.PassengerPhotoURL == "" {
			t.Errorf("%s: no passenger photo url", label)
		}
		if a.PassengerRating == nil || *a.PassengerRating != 4.5 || a.RatingCount != 2 {
			t.Errorf("%s: rating = %v (%d), want 4.5 over 2", label, a.PassengerRating, a.RatingCount)
		}
		if len(a.SharedGroups) != 1 || a.SharedGroups[0] != "Ομάδα Α" {
			t.Errorf("%s: shared groups = %v, want only the one both are in", label, a.SharedGroups)
		}
		if a.Luggage != "suitcase" || a.LuggageCount != 2 {
			t.Errorf("%s: luggage = %q x%d", label, a.Luggage, a.LuggageCount)
		}
		if a.WindowStart == "" || a.WindowEnd == "" {
			t.Errorf("%s: window = %q..%q", label, a.WindowStart, a.WindowEnd)
		}
	}

	rec := h.do(http.MethodGet, "/v1/asks/pending", nil, driver)
	if rec.Code != http.StatusOK {
		t.Fatalf("pending: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Asks []struct {
			ID string `json:"id"`
			askFacts
		} `json:"asks"`
	}
	h.decode(rec, &list)
	if len(list.Asks) != 1 {
		t.Fatalf("pending asks = %d, want 1", len(list.Asks))
	}
	check("list", list.Asks[0].askFacts)

	rec = h.do(http.MethodGet, "/v1/asks/"+list.Asks[0].ID, nil, driver)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	var one askFacts
	h.decode(rec, &one)
	check("one", one)
}

// Nobody has rated a newcomer: the field is null, not a zero that reads as one star.
func TestAskFromAnUnratedPassengerHasNoRating(t *testing.T) {
	h := newHarness(t)
	occurrence := h.occurrenceOf("+306900000341", "Οδηγός")
	driver, _ := h.signUp("+306900000341", "Οδηγός")
	h.signUp("+306900000342", "Νέος")
	var passengerID string
	h.mustQuery(`SELECT id::text FROM users WHERE phone = '+306900000342'`, &passengerID)
	h.ask(h.openRequest(passengerID, "asking"), occurrence, "pending", 10*time.Minute)

	got := h.pendingAsks(driver)
	if len(got) != 1 {
		t.Fatalf("asks = %d", len(got))
	}
	rec := h.do(http.MethodGet, "/v1/asks/"+got[0].ID, nil, driver)
	var a askFacts
	h.decode(rec, &a)
	if a.PassengerRating != nil || a.RatingCount != 0 || a.PassengerPhotoURL != nil {
		t.Errorf("newcomer facts = %+v, want null rating and photo", a)
	}
	if a.SharedGroups == nil || len(a.SharedGroups) != 0 || a.Luggage != "none" || a.LuggageCount != 0 {
		t.Errorf("newcomer facts = %+v, want empty groups and no luggage", a)
	}
}
