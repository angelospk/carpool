package api

import (
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"carpool/internal/store"
)

// shareURL is the public address of a route link. Built from configuration,
// never from the request's Host header, which the client chooses.
func (d Deps) shareURL(token string) string {
	if d.PublicBaseURL == "" {
		return ""
	}
	return strings.TrimRight(d.PublicBaseURL, "/") + "/r/" + token
}

func (d Deps) handleShareRoute(w http.ResponseWriter, r *http.Request) {
	userID, _ := callerID(r.Context())
	routeID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such route")
		return
	}
	token, err := store.ShareToken(r.Context(), d.Pool, routeID, userID)
	if errors.Is(err, store.ErrNoSuchRoute) {
		writeError(w, http.StatusNotFound, "not_found", "no such route")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not make a link")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "url": d.shareURL(token)})
}

func (d Deps) handleRevokeRouteShare(w http.ResponseWriter, r *http.Request) {
	userID, _ := callerID(r.Context())
	routeID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such route")
		return
	}
	err = store.RevokeShareToken(r.Context(), d.Pool, routeID, userID)
	if errors.Is(err, store.ErrNoSuchRoute) {
		writeError(w, http.StatusNotFound, "not_found", "no such route")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not withdraw the link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// publicHeaders go on everything a route link answers. The page is about
// somebody's routine, so it is not cached by anybody, not indexed, and does
// not tell the next site it came from here.
func publicHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
}

func sharedRouteJSON(s store.SharedRoute) map[string]any {
	return map[string]any{
		"origin_label": s.OriginLabel, "destination_label": s.DestinationLabel,
		"kind": s.Kind, "weekdays": s.Weekdays, "oneoff_date": s.OneOffDate,
		"depart_earliest": s.DepartEarliest, "depart_latest": s.DepartLatest,
		"seats": s.Seats, "for_group": s.ForGroup,
	}
}

// handleSharedRoute is the link's contents for the app.
func (d Deps) handleSharedRoute(w http.ResponseWriter, r *http.Request) {
	publicHeaders(w)
	s, err := store.SharedRouteByToken(r.Context(), d.Pool, chiURLParam(r, "token"), d.now())
	switch {
	case errors.Is(err, store.ErrNoSuchRoute):
		writeError(w, http.StatusNotFound, "not_found", "no such route")
	case errors.Is(err, store.ErrRouteRetired):
		writeError(w, http.StatusGone, "route_retired", "that route is no longer offered")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not read the route")
	default:
		writeJSON(w, http.StatusOK, sharedRouteJSON(s))
	}
}

var weekdayShort = [...]string{"", "Δευ", "Τρι", "Τετ", "Πεμ", "Παρ", "Σαβ", "Κυρ"}

// scheduleWords is the route's days in words, the same presets the app's
// share sentence uses.
func scheduleWords(s store.SharedRoute) string {
	if s.Kind == "oneoff" {
		if s.OneOffDate != nil {
			return *s.OneOffDate
		}
		return ""
	}
	days := map[int]bool{}
	for _, d := range s.Weekdays {
		days[d] = true
	}
	is := func(want ...int) bool {
		if len(days) != len(want) {
			return false
		}
		for _, d := range want {
			if !days[d] {
				return false
			}
		}
		return true
	}
	switch {
	case is(1, 2, 3, 4, 5):
		return "καθημερινές"
	case is(6, 7):
		return "Σαββατοκύριακα"
	case is(1, 2, 3, 4, 5, 6, 7):
		return "κάθε μέρα"
	}
	var out []string
	for d := 1; d <= 7; d++ {
		if days[d] {
			out = append(out, weekdayShort[d])
		}
	}
	return strings.Join(out, " ")
}

// routePage is deliberately plain: no scripts, no fonts, no images from
// anywhere, so the CSP can forbid everything but its own inline style.
var routePage = template.Must(template.New("route").Parse(`<!doctype html>
<html lang="el"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
{{if .Route}}<meta property="og:title" content="{{.Route.OriginLabel}} → {{.Route.DestinationLabel}}">
<meta property="og:description" content="{{.Schedule}} · {{.Route.DepartEarliest}}–{{.Route.DepartLatest}} · {{.Route.Seats}} θέσεις">{{end}}
<title>{{if .Route}}{{.Route.OriginLabel}} → {{.Route.DestinationLabel}}{{else}}Διαδρομή{{end}}</title>
<style>
body{font-family:system-ui,sans-serif;background:#f6f5f1;color:#1d2521;margin:0;padding:24px;line-height:1.5}
main{max-width:28rem;margin:auto;background:#fff;border-radius:16px;padding:24px}
h1{font-size:1.4rem;margin:0 0 8px}p{margin:8px 0}.muted{color:#6b716d}
a.button{display:block;text-align:center;background:#2f5d50;color:#fff;border-radius:999px;padding:12px;text-decoration:none;margin-top:16px}
a.quiet{background:#e7e5df;color:#1d2521}
</style></head><body><main>
{{if .Route}}
<h1>{{.Route.OriginLabel}} → {{.Route.DestinationLabel}}</h1>
<p>{{.Schedule}} · {{.Route.DepartEarliest}}–{{.Route.DepartLatest}}</p>
<p>{{.Route.Seats}} θέσεις προσφέρονται{{if .Route.ForGroup}}, μόνο για τα μέλη μιας ομάδας{{end}}.</p>
<p class="muted">Ο οδηγός μοιράζεται τα έξοδα, δεν πουλάει διαδρομή. Για να ζητήσεις θέση, άνοιξε την εφαρμογή.</p>
<a class="button" href="{{.AppLink}}">Άνοιγμα στην εφαρμογή</a>
{{if .InstallURL}}<a class="button quiet" href="{{.InstallURL}}">Κατέβασε την εφαρμογή</a>{{end}}
{{else}}
<h1>{{.Gone}}</h1>
{{end}}
</main></body></html>`))

// handleRoutePage is the link itself, for whoever taps it in a chat: a page
// that says what the share sentence said and how to get the app.
func (d Deps) handleRoutePage(w http.ResponseWriter, r *http.Request) {
	publicHeaders(w)
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	token := chiURLParam(r, "token")
	s, err := store.SharedRouteByToken(r.Context(), d.Pool, token, d.now())
	data := map[string]any{"InstallURL": d.safeInstallURL()}
	switch {
	case errors.Is(err, store.ErrNoSuchRoute):
		w.WriteHeader(http.StatusNotFound)
		data["Gone"] = "Αυτός ο σύνδεσμος δεν οδηγεί σε κάποια διαδρομή."
	case errors.Is(err, store.ErrRouteRetired):
		w.WriteHeader(http.StatusGone)
		data["Gone"] = "Αυτή η διαδρομή δεν προσφέρεται πια."
	case err != nil:
		w.WriteHeader(http.StatusInternalServerError)
		data["Gone"] = "Κάτι πήγε στραβά. Δοκίμασε ξανά σε λίγο."
	default:
		data["Route"] = s
		data["Schedule"] = scheduleWords(s)
		// A custom scheme, which html/template would otherwise refuse as an
		// unsafe URL. The token is base64url, so it cannot carry anything but
		// itself.
		data["AppLink"] = template.URL("carpool://r/" + token)
	}
	_ = routePage.Execute(w, data)
}

// safeInstallURL is INSTALL_URL if it is an https address, and nothing
// otherwise: a misconfigured value must not become a javascript: link on a
// page anybody can open.
func (d Deps) safeInstallURL() string {
	if strings.HasPrefix(d.InstallURL, "https://") {
		return d.InstallURL
	}
	return ""
}
