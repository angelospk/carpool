package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func (h *harness) openPage(path, userAgent string) int {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("User-Agent", userAgent)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec.Code
}

func (h *harness) linkOpens(token, routeID string) int {
	h.t.Helper()
	rec := h.do(http.MethodGet, "/v1/me/routes", nil, token)
	var body struct {
		Routes []struct {
			ID        string `json:"id"`
			LinkOpens int    `json:"link_opens"`
		} `json:"routes"`
	}
	h.decode(rec, &body)
	for _, r := range body.Routes {
		if r.ID == routeID {
			return r.LinkOpens
		}
	}
	h.t.Fatalf("route %s not in my routes", routeID)
	return 0
}

// Whether the share distributes anything was a question nobody could answer
// without reading the proxy's logs. The driver now sees how many times the
// link was opened — opened by a person, not fetched by a chat app building its
// preview, which happens once per chat and would count the paste as a reader.
func TestADriverSeesHowOftenTheirLinkWasOpened(t *testing.T) {
	h := newHarness(t)
	driver, routeID := driverWithRoute(t, h, "+306900000821")
	link := h.shareRoute(driver, routeID)

	if got := h.linkOpens(driver, routeID); got != 0 {
		t.Fatalf("before anybody opened it: %d", got)
	}

	phone := "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/128 Mobile Safari/537.36"
	for _, ua := range []string{
		phone,
		phone,
		// Previews and crawlers, which fetch the page without anybody reading it.
		"facebookexternalhit/1.1",
		"WhatsApp/2.23.20.0",
		"Viber/21.0 LinkPreview",
		"TelegramBot (like TwitterBot)",
		"Mozilla/5.0 (compatible; Googlebot/2.1)",
		"",
	} {
		if code := h.openPage("/r/"+link.Token, ua); code != http.StatusOK {
			t.Fatalf("open with %q: %d", ua, code)
		}
	}
	// HEAD is how a preview checks the link exists.
	h.do(http.MethodHead, "/r/"+link.Token, nil, "")

	if got := h.linkOpens(driver, routeID); got != 2 {
		t.Errorf("link_opens = %d, want the two opens from a phone", got)
	}

	// A token that is no link counts against nothing.
	if code := h.openPage("/r/AAAAAAAAAAAAAAAAAAAAAAAAAA", phone); code != http.StatusNotFound {
		t.Errorf("unknown token: %d", code)
	}
}
