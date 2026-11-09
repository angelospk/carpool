package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"carpool/internal/api"
	"carpool/internal/live"
)

type sharedTripBody struct {
	OriginLabel      string    `json:"origin_label"`
	DestinationLabel string    `json:"destination_label"`
	DepartAt         time.Time `json:"depart_at"`
	DriverName       string    `json:"driver_name"`
	CarModel         string    `json:"car_model"`
	CarPlate         string    `json:"car_plate"`
	Opens            time.Time `json:"opens"`
	Closes           time.Time `json:"closes"`
	Position         *struct {
		Lat        float64   `json:"lat"`
		Lon        float64   `json:"lon"`
		ObservedAt time.Time `json:"observed_at"`
	} `json:"position"`
	ETAMin *int `json:"driver_eta_min"`
}

func (h *harness) shareTrip(t *testing.T, occ, token string) string {
	t.Helper()
	var got struct {
		Token string `json:"token"`
		URL   string `json:"url"`
	}
	rec := h.do(http.MethodPost, "/v1/trips/"+occ+"/share", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("sharing: %d %s", rec.Code, rec.Body.String())
	}
	h.decode(rec, &got)
	if len(got.Token) < 20 {
		t.Fatalf("token %q is too short to be unguessable", got.Token)
	}
	return got.Token
}

// A passenger hands their family a link. Before the trip it says who and
// when; once the trip is on and the driver shares, it says where the car is
// and how far from the pickup. Nobody needs an account to read it.
func TestASharedTripShowsTheCarToWhoeverHasTheLink(t *testing.T) {
	h := newHarnessDeps(t, false, nil, func(d *api.Deps) {
		d.Live = live.NewService(d.Pool, d.Clock).WithRouter(eightMinutes{})
	})
	_, passenger, occurrenceID := h.bookedTrip(t)
	occ := occurrenceID.String()

	token := h.shareTrip(t, occ, passenger)
	if again := h.shareTrip(t, occ, passenger); again != token {
		t.Fatalf("a second share made a second link: %q then %q", token, again)
	}

	var got sharedTripBody
	rec := h.do(http.MethodGet, "/v1/shared-trips/"+token, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reading the link: %d %s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	h.decode(rec, &got)
	if got.OriginLabel != "A" || got.DestinationLabel != "B" || got.DriverName != "Οδηγός" ||
		got.CarModel != "Yaris" || got.CarPlate != "ABC-1234" {
		t.Fatalf("the link says %+v", got)
	}
	if got.Position != nil {
		t.Fatalf("a day before the trip the link shows a position: %+v", got.Position)
	}
	if got.Opens.IsZero() || !got.Closes.After(got.Opens) {
		t.Fatalf("window %v .. %v", got.Opens, got.Closes)
	}

	h.clock.Set(time.Date(2026, time.July, 30, 4, 15, 0, 0, time.UTC))
	driver, _ := h.signUp("+306900000031", "Οδηγός")
	if rec := h.do(http.MethodPut, "/v1/trips/"+occ+"/position",
		map[string]any{"lat": 40.85, "lon": 25.85}, driver); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	got = sharedTripBody{}
	h.decode(h.do(http.MethodGet, "/v1/shared-trips/"+token, nil, ""), &got)
	if got.Position == nil || got.Position.Lat != 40.85 || got.Position.Lon != 25.85 {
		t.Fatalf("during the trip the link shows %+v", got.Position)
	}
	if got.ETAMin == nil || *got.ETAMin != 8 {
		t.Fatalf("eta = %v, want 8", got.ETAMin)
	}

	// The page a browser opens: the same facts, a way onto a map, and it
	// reloads itself without any script.
	rec = h.do(http.MethodGet, "/t/"+token, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("page: %d", rec.Code)
	}
	page := rec.Body.String()
	for _, want := range []string{"Οδηγός", "Yaris", "ABC-1234", "40.85", "25.85", `http-equiv="refresh"`} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(page, "<script") {
		t.Error("page carries a script")
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP = %q", csp)
	}
}

// Only a passenger shares: the link exists so somebody's family knows whose
// car they got into. Nobody else on or off the trip makes one.
func TestOnlyAPassengerCanShareTheTrip(t *testing.T) {
	h := newHarness(t)
	driver, _, occurrenceID := h.bookedTrip(t)
	occ := occurrenceID.String()

	if rec := h.do(http.MethodPost, "/v1/trips/"+occ+"/share", nil, driver); rec.Code != http.StatusForbidden ||
		h.errorCode(rec) != "passengers_only" {
		t.Fatalf("driver sharing: %d %s", rec.Code, rec.Body.String())
	}
	stranger, _ := h.signUp("+306900000799", "Άσχετος")
	if rec := h.do(http.MethodPost, "/v1/trips/"+occ+"/share", nil, stranger); rec.Code != http.StatusNotFound {
		t.Fatalf("stranger sharing: %d", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/v1/trips/"+occ+"/share", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
	if rec := h.do(http.MethodGet, "/v1/shared-trips/not-a-token", nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("made-up token: %d", rec.Code)
	}
}

// Withdrawing the link, or leaving the trip, ends it for whoever holds it.
func TestASharedTripLinkEnds(t *testing.T) {
	h := newHarness(t)
	_, passenger, occurrenceID := h.bookedTrip(t)
	occ := occurrenceID.String()

	token := h.shareTrip(t, occ, passenger)
	if rec := h.do(http.MethodDelete, "/v1/trips/"+occ+"/share", nil, passenger); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if rec := h.do(http.MethodGet, "/v1/shared-trips/"+token, nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("after revoking: %d", rec.Code)
	}
	if rec := h.do(http.MethodGet, "/t/"+token, nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("page after revoking: %d", rec.Code)
	}

	fresh := h.shareTrip(t, occ, passenger)
	if fresh == token {
		t.Fatal("a revoked token came back")
	}
	h.mustExec(`UPDATE bookings SET status = 'cancelled_by_passenger' WHERE occurrence_id = $1`, occurrenceID)
	if rec := h.do(http.MethodGet, "/v1/shared-trips/"+fresh, nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("after the booking was cancelled: %d", rec.Code)
	}
}
