package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"carpool/internal/api"
	"carpool/internal/invites"
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

// newInviteOnlyHarness is the pilot as deployed: a new account needs a code.
// The seeded code lets the test's own people in.
func newInviteOnlyHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarnessDeps(t, false, nil, func(d *api.Deps) {
		d.Invites = invites.NewService(d.Pool, d.Clock, true)
	})
	h.mustExec(`INSERT INTO invite_codes (code, max_uses) VALUES ('SEEDLINK', 30)`)
	h.invite = "SEEDLINK"
	return h
}

// When the pilot is invite-only, a stranger who opens a route link would stop
// at signup for want of a code. The page shows the driver's own code: the
// driver is already inside and handed the link over, which is what a code
// stands for. A code with no uses left, or on a route that is no longer
// offered, is not shown.
func TestAnInviteOnlyLinkCarriesTheDriversCode(t *testing.T) {
	h := newInviteOnlyHarness(t)
	driver, routeID := driverWithRoute(t, h, "+306900000841")
	link := h.shareRoute(driver, routeID)

	page := h.do(http.MethodGet, "/r/"+link.Token, nil, "")
	if page.Code != http.StatusOK {
		t.Fatalf("page: %d %s", page.Code, page.Body.String())
	}
	var code string
	h.mustQuery(`SELECT c.code FROM invite_codes c JOIN users u ON u.id = c.created_by
	              WHERE u.phone = '+306900000841'`, &code)
	if !strings.Contains(page.Body.String(), code) {
		t.Fatalf("page does not show the driver's code %q:\n%s", code, page.Body.String())
	}

	// The code on the page lets a stranger in.
	h.invite = code
	if passenger, _ := h.signUp("+306900000842", "Επιβάτης"); passenger == "" {
		t.Fatal("signing up with the code from the page gave no session")
	}
	var used int
	h.mustQuery(`SELECT used_count FROM invite_codes WHERE code = $1`, &used, code)
	if used != 1 {
		t.Fatalf("used_count = %d, want 1", used)
	}

	h.mustExec(`UPDATE invite_codes SET used_count = max_uses WHERE code = $1`, code)
	spent := h.do(http.MethodGet, "/r/"+link.Token, nil, "").Body.String()
	if strings.Contains(spent, code) {
		t.Errorf("a spent code is still shown")
	}
	if !strings.Contains(spent, "Οι προσκλήσεις του οδηγού τελείωσαν") {
		t.Errorf("a spent code is not explained:\n%s", spent)
	}

	h.mustExec(`UPDATE invite_codes SET used_count = 0, expires_at = $2 WHERE code = $1`,
		code, h.clock.Now().Add(-time.Minute))
	if expired := h.do(http.MethodGet, "/r/"+link.Token, nil, "").Body.String(); strings.Contains(expired, code) {
		t.Errorf("an expired code is still shown")
	}

	h.mustExec(`UPDATE invite_codes SET expires_at = NULL WHERE code = $1`, code)
	h.mustExec(`UPDATE route_templates SET active = false WHERE id = $1::uuid`, routeID)
	retired := h.do(http.MethodGet, "/r/"+link.Token, nil, "")
	if retired.Code != http.StatusGone || strings.Contains(retired.Body.String(), code) {
		t.Errorf("a retired route's page: %d, shows the code: %v",
			retired.Code, strings.Contains(retired.Body.String(), code))
	}
}

// Without the invite gate there is nothing to hand over, and opening a link
// makes no code for the driver.
func TestAnOpenLinkCarriesNoCode(t *testing.T) {
	h := newHarness(t)
	driver, routeID := driverWithRoute(t, h, "+306900000851")
	link := h.shareRoute(driver, routeID)

	html := h.do(http.MethodGet, "/r/"+link.Token, nil, "").Body.String()
	if strings.Contains(html, "πρόσκληση") || strings.Contains(html, "προσκλήσεις") {
		t.Errorf("an open pilot's page talks about invites:\n%s", html)
	}
	var n int
	h.mustQuery(`SELECT count(*) FROM invite_codes`, &n)
	if n != 0 {
		t.Errorf("opening the link made %d invite codes", n)
	}
}
