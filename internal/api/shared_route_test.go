package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

type shareBody struct {
	Token string `json:"token"`
	URL   string `json:"url"`
}

func (h *harness) shareRoute(token, routeID string) shareBody {
	h.t.Helper()
	rec := h.do(http.MethodPost, "/v1/me/routes/"+routeID+"/share", nil, token)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("share: %d %s", rec.Code, rec.Body.String())
	}
	var b shareBody
	h.decode(rec, &b)
	return b
}

// The link is the driver's to make, is the same link each time they ask, and
// is built on the configured address rather than whatever Host the request
// claimed.
func TestADriverGetsOneStableLinkForTheirRoute(t *testing.T) {
	h := newHarness(t)
	driver, routeID := driverWithRoute(t, h, "+306900000801")

	first := h.shareRoute(driver, routeID)
	if len(first.Token) < 22 || first.URL != "https://carpool.test/r/"+first.Token {
		t.Fatalf("share = %+v", first)
	}
	if again := h.shareRoute(driver, routeID); again.Token != first.Token {
		t.Fatalf("asking again made a new link: %q then %q", first.Token, again.Token)
	}

	other, _ := h.signUp("+306900000802", "Άλλος")
	if rec := h.do(http.MethodPost, "/v1/me/routes/"+routeID+"/share", nil, other); rec.Code != http.StatusNotFound {
		t.Fatalf("somebody else sharing the route: %d", rec.Code)
	}
}

// Anybody holding the link sees what the share sentence says, without an
// account, and nothing that would identify or locate the driver.
func TestALinkShowsTheRouteAndNothingAboutTheDriver(t *testing.T) {
	h := newHarness(t)
	driver, routeID := driverWithRoute(t, h, "+306900000811")
	link := h.shareRoute(driver, routeID)

	rec := h.do(http.MethodGet, "/v1/shared-routes/"+link.Token, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("shared route: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Αλεξανδρούπολη", "Φέρες", "07:30", `"seats":3`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q: %s", want, body)
		}
	}
	for _, leak := range []string{"Οδηγός", "ΑΒΓ-1234", "+3069", "25.87", "40.84", "driver", "price"} {
		if strings.Contains(body, leak) {
			t.Errorf("the link carries %q: %s", leak, body)
		}
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Robots-Tag") == "" {
		t.Errorf("headers: %v", rec.Header())
	}

	page := h.do(http.MethodGet, "/r/"+link.Token, nil, "")
	if page.Code != http.StatusOK {
		t.Fatalf("page: %d %s", page.Code, page.Body.String())
	}
	html := page.Body.String()
	for _, want := range []string{"Αλεξανδρούπολη → Φέρες", "Δευ Τρι Τετ", "carpool://r/" + link.Token, "https://carpool.test/get",
		// Somebody without the app, on an iPhone, continues in the browser.
		`href="/app/r/` + link.Token + `"`} {
		if !strings.Contains(html, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if !strings.Contains(html, `<meta property="og:title" content="Αλεξανδρούπολη → Φέρες">`) {
		t.Errorf("no preview title for a chat app")
	}
	if head := h.do(http.MethodHead, "/r/"+link.Token, nil, ""); head.Code != http.StatusOK {
		t.Errorf("HEAD: %d", head.Code)
	}
	if csp := page.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP = %q", csp)
	}
}

// A label is typed by a driver and rendered on a page anybody can open.
func TestALabelCannotBecomeMarkupOnThePage(t *testing.T) {
	h := newHarness(t)
	driver, routeID := driverWithRoute(t, h, "+306900000821")
	h.mustExec(`UPDATE route_templates SET origin_label = '<script>alert(1)</script>' WHERE id = $1::uuid`, routeID)
	link := h.shareRoute(driver, routeID)

	html := h.do(http.MethodGet, "/r/"+link.Token, nil, "").Body.String()
	if strings.Contains(html, "<script>") {
		t.Fatalf("a label became markup: %s", html)
	}
}

// Withdrawing the link kills it; a new one is a different link. A route the
// driver retired, or a one-off whose day has passed, says so rather than
// advertising a trip that will not run.
func TestALinkStopsWorkingWhenWithdrawnOrWhenTheRouteCannotRun(t *testing.T) {
	h := newHarness(t)
	driver, routeID := driverWithRoute(t, h, "+306900000831")
	link := h.shareRoute(driver, routeID)

	if rec := h.do(http.MethodDelete, "/v1/me/routes/"+routeID+"/share", nil, driver); rec.Code != http.StatusNoContent {
		t.Fatalf("withdraw: %d", rec.Code)
	}
	if rec := h.do(http.MethodGet, "/v1/shared-routes/"+link.Token, nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("a withdrawn link: %d", rec.Code)
	}
	fresh := h.shareRoute(driver, routeID)
	if fresh.Token == link.Token {
		t.Fatal("the new link is the withdrawn one")
	}

	h.mustExec(`UPDATE route_schedules SET kind = 'oneoff', weekdays = NULL, oneoff_date = $2
	            WHERE template_id = $1::uuid`, routeID, h.clock.Now().Add(-48*time.Hour).Format("2006-01-02"))
	if rec := h.do(http.MethodGet, "/v1/shared-routes/"+fresh.Token, nil, ""); rec.Code != http.StatusGone {
		t.Fatalf("a one-off that already ran: %d %s", rec.Code, rec.Body.String())
	}

	h.mustExec(`UPDATE route_templates SET active = false WHERE id = $1::uuid`, routeID)
	if rec := h.do(http.MethodGet, "/r/"+fresh.Token, nil, ""); rec.Code != http.StatusGone {
		t.Fatalf("a retired route's page: %d", rec.Code)
	}
	if rec := h.do(http.MethodGet, "/r/nosuchtokenatallxxxxxx", nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown link: %d", rec.Code)
	}
}

// Anybody can call the public routes, so one address gets a minute's worth
// and then waits.
func TestThePublicRoutesAreLimitedPerAddress(t *testing.T) {
	h := newHarness(t)
	var last int
	for i := 0; i < 61; i++ {
		last = h.do(http.MethodGet, "/v1/shared-routes/nosuchtokenatallxxxxxx", nil, "").Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("the 61st request in a minute: %d, want 429", last)
	}
}
