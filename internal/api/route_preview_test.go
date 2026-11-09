package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"carpool/internal/geo"
)

// Two roads from A to B: the main one along y=40.85, and a longer one that
// bends north through (25.90, 40.95). Polyline6 for each, as OSRM sends them
// with geometries=polyline6.
const twoRoads = `{"code":"Ok","routes":[` +
	`{"distance":20000,"duration":1200,"geometry":"_dh|lA_suep@?_ibE?_ibE","legs":[{"steps":[]}]},` +
	`{"distance":26000,"duration":1500,"geometry":"_dh|lA_suep@_ibE_ibE~hbE_ibE","legs":[{"steps":[]}]}` +
	`],"waypoints":[]}`

type osrmSpy struct {
	mu   sync.Mutex
	urls []string
}

func (s *osrmSpy) server(t *testing.T, body string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.urls = append(s.urls, r.URL.String())
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (s *osrmSpy) last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.urls) == 0 {
		return ""
	}
	return s.urls[len(s.urls)-1]
}

type previewBody struct {
	Routes []struct {
		Polyline string     `json:"polyline"`
		Km       float64    `json:"km"`
		Min      int        `json:"min"`
		Via      *[]float64 `json:"via"`
	} `json:"routes"`
}

// Before publishing, the driver sees the roads OSRM knows between the two
// ends. The first is the one the server would take anyway; every other one
// comes with a point on it, which is what publishing sends back to say "this
// one". Nothing is stored.
func TestPreviewOffersTheRoadsWithAPointOnEachAlternative(t *testing.T) {
	// Sanity: the fixture decodes to what the comment says.
	main, _ := geo.DecodePolylinePrecision("_dh|lA_suep@?_ibE?_ibE", 6)
	alt, _ := geo.DecodePolylinePrecision("_dh|lA_suep@_ibE_ibE~hbE_ibE", 6)
	if len(main) != 3 || len(alt) != 3 || alt[1].Lat < 40.94 {
		t.Fatalf("fixture decodes to %v and %v", main, alt)
	}

	spy := &osrmSpy{}
	h := newHarnessRoutedBy(t, spy.server(t, twoRoads).URL)
	token, _ := h.signUp("+306901000041", "Οδηγός")
	before := h.countRows()

	ends := map[string]any{"origin_lon": 25.80, "origin_lat": 40.85, "destination_lon": 26.00, "destination_lat": 40.85}
	rec := h.do(http.MethodPost, "/v1/routes/preview", ends, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(spy.last(), "alternatives=") {
		t.Errorf("OSRM was not asked for alternatives: %s", spy.last())
	}
	var got previewBody
	h.decode(rec, &got)
	if len(got.Routes) != 2 {
		t.Fatalf("routes = %+v, want 2", got.Routes)
	}
	first, second := got.Routes[0], got.Routes[1]
	if first.Via != nil || first.Polyline == "" || first.Km != 20 || first.Min != 20 {
		t.Errorf("first route = %+v", first)
	}
	if second.Via == nil || len(*second.Via) != 2 || second.Km != 26 || second.Min != 25 {
		t.Fatalf("second route = %+v", second)
	}
	// The point has to be on the alternative and off the main road, or routing
	// through it gives the main road back.
	if v := *second.Via; v[0] != 25.90 || v[1] < 40.94 {
		t.Errorf("via = %v, want the bend at (25.90, 40.95)", v)
	}

	if after := h.countRows(); len(after) != len(before) {
		t.Errorf("a preview wrote rows: %v then %v", before, after)
	} else {
		for k, n := range before {
			if after[k] != n {
				t.Errorf("a preview wrote to %s: %d then %d", k, n, after[k])
			}
		}
	}

	if rec := h.do(http.MethodPost, "/v1/routes/preview", ends, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", rec.Code)
	}
}

// Publishing with a via routes start → via → end, so the stored road is the
// one the driver picked.
func TestPublishingThroughAViaRoutesThroughIt(t *testing.T) {
	spy := &osrmSpy{}
	h := newHarnessRoutedBy(t, spy.server(t, `{"code":"Ok","routes":[{"distance":26000,"duration":1500,`+
		`"geometry":{"coordinates":[[25.80,40.85],[25.90,40.95],[26.00,40.85]]},"legs":[{"steps":[]}]}],"waypoints":[]}`).URL)
	token, _ := h.signUp("+306901000042", "Οδηγός")
	h.becomeDriver(token)

	rec := h.do(http.MethodPost, "/v1/routes", map[string]any{
		"origin_lon": 25.80, "origin_lat": 40.85,
		"destination_lon": 26.00, "destination_lat": 40.85,
		"via":          []float64{25.90, 40.95},
		"origin_label": "A", "destination_label": "B",
		"seats": 3, "kind": "oneoff",
		"oneoff_date":     h.clock.Now().Add(48 * time.Hour).Format("2006-01-02"),
		"depart_earliest": "07:30", "depart_latest": "08:00",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish: %d %s", rec.Code, rec.Body.String())
	}
	if u := spy.last(); !strings.Contains(u, "25.8,40.85;25.9,40.95;26,40.85") {
		t.Fatalf("OSRM was asked %s, want start;via;end", u)
	}
}

// A via has to be a place in the country, like either end.
func TestAViaOutsideTheCountryIsRefused(t *testing.T) {
	spy := &osrmSpy{}
	h := newHarnessRoutedBy(t, spy.server(t, twoRoads).URL)
	h.seedTheCountry()
	token, _ := h.signUp("+306901000043", "Οδηγός")
	h.becomeDriver(token)

	rec := h.do(http.MethodPost, "/v1/routes", map[string]any{
		"origin_lon": 25.87, "origin_lat": 40.85,
		"destination_lon": 26.50, "destination_lat": 41.50,
		"via":          []float64{26.55, 41.68}, // Edirne
		"origin_label": "A", "destination_label": "B",
		"seats": 3, "kind": "oneoff",
		"oneoff_date":     h.clock.Now().Add(48 * time.Hour).Format("2006-01-02"),
		"depart_earliest": "07:30", "depart_latest": "08:00",
	}, token)
	if rec.Code != http.StatusBadRequest || h.errorCode(rec) != "outside_country" {
		t.Fatalf("via in Turkey: %d %s", rec.Code, rec.Body.String())
	}
	if rec := h.do(http.MethodPost, "/v1/routes", map[string]any{
		"origin_lon": 25.87, "origin_lat": 40.85, "destination_lon": 26.50, "destination_lat": 41.50,
		"via": []float64{26.0}, "origin_label": "A", "destination_label": "B",
		"seats": 3, "kind": "oneoff", "oneoff_date": h.clock.Now().Add(48 * time.Hour).Format("2006-01-02"),
		"depart_earliest": "07:30", "depart_latest": "08:00",
	}, token); rec.Code != http.StatusBadRequest {
		t.Fatalf("a via with one coordinate: %d", rec.Code)
	}
}
