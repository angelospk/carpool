package api_test

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// encodePolyline6 is the encoder OSRM's polyline6 geometry comes from, so a
// stub can hand back a road without a routing service.
func encodePolyline6(pts [][2]float64) string {
	var b strings.Builder
	var prevLat, prevLon int
	put := func(v int) {
		v <<= 1
		if v < 0 {
			v = ^v
		}
		for v >= 0x20 {
			b.WriteByte(byte((0x20 | (v & 0x1f)) + 63))
			v >>= 5
		}
		b.WriteByte(byte(v + 63))
	}
	for _, p := range pts {
		lat, lon := int(math.Round(p[1]*1e6)), int(math.Round(p[0]*1e6))
		put(lat - prevLat)
		put(lon - prevLon)
		prevLat, prevLon = lat, lon
	}
	return b.String()
}

func estimateRoutedAlong(t *testing.T, road [][2]float64, km float64) (*harness, string) {
	t.Helper()
	body := fmt.Sprintf(`{"code":"Ok","routes":[{"distance":%f,"duration":1800,"geometry":%q,"legs":[{"steps":[]}]}],"waypoints":[]}`,
		km*1000, encodePolyline6(road))
	osrm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(osrm.Close)

	h := newHarnessRoutedBy(t, osrm.URL)
	h.seedTheCountry()
	token, _ := h.signUp("+306903000011", "Επιβάτης")
	return h, token
}

type tollEstimate struct {
	FuelCents         int `json:"fuel_cents"`
	TollCents         int `json:"toll_cents"`
	ContributionCents int `json:"contribution_cents"`
	CeilingCents      int `json:"ceiling_cents"`
}

func askForEstimate(t *testing.T, h *harness, token string, road [][2]float64) tollEstimate {
	t.Helper()
	first, last := road[0], road[len(road)-1]
	rec := h.do(http.MethodPost, "/v1/estimates", map[string]any{
		"origin_lon": first[0], "origin_lat": first[1],
		"destination_lon": last[0], "destination_lat": last[1],
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body.String())
	}
	var got tollEstimate
	h.decode(rec, &got)
	return got
}

// QA U F4 / T F4: the search said 1,00 € for the fuel and the real ask was
// 2,38 €. The Egnatia through the Μέστης plaza (2,75 €, shared by the four in
// the car) is the barrier that was missing from the figure.
func TestTheEstimateCarriesTheTollOfARouteThatCrossesAPlaza(t *testing.T) {
	road := [][2]float64{{25.45, 41.06}, {25.5288, 41.0172}, {25.62, 40.97}, {25.70, 40.92}}
	h, token := estimateRoutedAlong(t, road, 30)

	got := askForEstimate(t, h, token, road)

	// 275 over four: 68 or 69 depending on the odd cents.
	if got.TollCents < 68 || got.TollCents > 69 {
		t.Errorf("toll = %d, want 68 or 69 (2,75 € over a car of four)", got.TollCents)
	}
	if got.ContributionCents != got.FuelCents+got.TollCents {
		t.Errorf("contribution = %d, want fuel %d + toll %d", got.ContributionCents, got.FuelCents, got.TollCents)
	}
	if got.CeilingCents < got.ContributionCents {
		t.Errorf("ceiling %d below contribution %d", got.CeilingCents, got.ContributionCents)
	}
}

func TestTheEstimateOfARouteWithoutTollsCarriesNone(t *testing.T) {
	road := [][2]float64{{25.8743, 40.8476}, {26.0, 40.87}, {26.1717, 40.8975}}
	h, token := estimateRoutedAlong(t, road, 29)

	got := askForEstimate(t, h, token, road)

	if got.TollCents != 0 {
		t.Errorf("toll = %d on a road with no barrier", got.TollCents)
	}
	if got.ContributionCents != got.FuelCents {
		t.Errorf("contribution = %d, want the fuel %d alone", got.ContributionCents, got.FuelCents)
	}
}
