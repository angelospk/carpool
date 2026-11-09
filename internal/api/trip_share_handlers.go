package api

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"carpool/internal/live"
	"carpool/internal/store"
)

// tripShareURL is the public address of a trip link. Built from
// configuration, never from the request's Host header.
func (d Deps) tripShareURL(token string) string {
	if d.PublicBaseURL == "" {
		return ""
	}
	return strings.TrimRight(d.PublicBaseURL, "/") + "/t/" + token
}

func (d Deps) handleShareTrip(w http.ResponseWriter, r *http.Request) {
	userID, _ := callerID(r.Context())
	tripID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such trip")
		return
	}
	token, err := store.ShareTrip(r.Context(), d.Pool, tripID, userID)
	switch {
	case errors.Is(err, store.ErrNotAPassenger):
		writeError(w, http.StatusForbidden, "passengers_only",
			"a trip link is for a passenger's family; the driver has none")
	case errors.Is(err, store.ErrNoSuchSharedTrip):
		writeError(w, http.StatusNotFound, "not_found", "no such trip")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not make a link")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "url": d.tripShareURL(token)})
	}
}

func (d Deps) handleRevokeTripShare(w http.ResponseWriter, r *http.Request) {
	userID, _ := callerID(r.Context())
	tripID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such trip")
		return
	}
	if err := store.RevokeTripShare(r.Context(), d.Pool, tripID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not withdraw the link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sharedTripView is a link's contents: the trip, and the car's position and
// time to the pickup as the passenger who shared it sees them. Read as the
// passenger, so the link never shows more than they could.
type sharedTripView struct {
	Trip     store.SharedTrip
	Opens    time.Time
	Closes   time.Time
	Position *live.Position
	ETAMin   *int
}

func (d Deps) sharedTrip(ctx context.Context, token string) (sharedTripView, error) {
	s, err := store.SharedTripByToken(ctx, d.Pool, token)
	if err != nil {
		return sharedTripView{}, err
	}
	v := sharedTripView{Trip: s}
	if d.Live == nil {
		return v, nil
	}
	if v.Opens, v.Closes, err = d.Live.Window(ctx, s.OccurrenceID, s.PassengerID); err != nil {
		return v, err
	}
	seen, err := d.Live.Visible(ctx, s.OccurrenceID, s.PassengerID)
	if err != nil {
		return v, err
	}
	// A passenger sees the driver only; the guard is here too so a change
	// there cannot put another passenger on somebody's family's screen.
	for i := range seen {
		if seen[i].UserID == s.DriverID {
			v.Position = &seen[i]
			break
		}
	}
	if v.Position == nil {
		return v, nil
	}
	eta, err := d.Live.DriverETA(ctx, s.OccurrenceID, s.PassengerID)
	if err != nil {
		return v, err
	}
	if eta != nil {
		m := eta.Minutes
		v.ETAMin = &m
	}
	return v, nil
}

// handleSharedTrip is a trip link's contents as JSON, for anything but a
// browser.
func (d Deps) handleSharedTrip(w http.ResponseWriter, r *http.Request) {
	publicHeaders(w)
	v, err := d.sharedTrip(r.Context(), chiURLParam(r, "token"))
	switch {
	case errors.Is(err, store.ErrNoSuchSharedTrip), errors.Is(err, live.ErrNotOnTrip):
		writeError(w, http.StatusNotFound, "not_found", "no such trip")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not read the trip")
		return
	}
	body := map[string]any{
		"origin_label": v.Trip.OriginLabel, "destination_label": v.Trip.DestinationLabel,
		"depart_at": v.Trip.DepartAt, "driver_name": v.Trip.DriverName,
		"car_model": v.Trip.CarModel, "car_color": v.Trip.CarColor, "car_plate": v.Trip.CarPlate,
		"opens": v.Opens, "closes": v.Closes, "position": nil,
	}
	if p := v.Position; p != nil {
		body["position"] = map[string]any{"lat": p.Lat, "lon": p.Lon, "observed_at": p.ObservedAt}
	}
	if v.ETAMin != nil {
		body["driver_eta_min"] = *v.ETAMin
	}
	writeJSON(w, http.StatusOK, body)
}

var tripPage = template.Must(template.New("trip").Parse(`<!doctype html>
<html lang="el"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="refresh" content="30">
<meta name="robots" content="noindex">
<title>Διαδρομή carpool</title>
<style>
body{font-family:system-ui,sans-serif;margin:0;padding:24px 16px;color:#14211C;background:#fff;max-width:520px}
h1{font-size:22px;margin:0 0 4px}
p{margin:8px 0;line-height:1.45}
.muted{color:#5F6B66}
.card{border:1px solid #DDE3E0;border-radius:12px;padding:14px;margin:16px 0}
.big{font-size:20px;font-weight:700}
.button{display:block;text-align:center;background:#1F5C45;color:#fff;text-decoration:none;
 padding:14px;border-radius:12px;font-weight:600;margin-top:12px}
</style></head><body><main>
{{if .Gone}}<h1>{{.Gone}}</h1>{{else}}
<h1>{{.Trip.OriginLabel}} → {{.Trip.DestinationLabel}}</h1>
<p class="muted">Αναχώρηση {{.Depart}}</p>
<div class="card">
<p class="big">{{.Trip.DriverName}}</p>
<p>{{.Car}}{{if .Trip.CarPlate}} · <strong>{{.Trip.CarPlate}}</strong>{{end}}</p>
</div>
{{if .Position}}
<div class="card">
<p class="big">{{if .ETAMin}}Φτάνει στο σημείο σε {{.ETAMin}}′{{else}}Το αυτοκίνητο κινείται{{end}}</p>
<p class="muted">Θέση πριν {{.Age}} · {{.Lat}}, {{.Lon}}</p>
<a class="button" href="{{.MapURL}}">Δες τη θέση στον χάρτη</a>
</div>
{{else if .Ended}}<p class="muted">Η διαδρομή τελείωσε. Η θέση δεν φαίνεται πια.</p>
{{else if .Before}}<p class="muted">Η θέση του αυτοκινήτου φαίνεται από τις {{.Opens}}.</p>
{{else}}<p class="muted">Ο οδηγός δεν μοιράζεται θέση αυτή τη στιγμή.</p>{{end}}
<p class="muted">Η σελίδα ανανεώνεται μόνη της κάθε 30″. Τη μοιράστηκε ο επιβάτης.</p>
{{end}}
</main></body></html>`))

// handleTripPage is the link as a person opens it: what the share sentence
// says, plus where the car is. It reloads itself; no script, so the strict
// policy of the route page holds.
func (d Deps) handleTripPage(w http.ResponseWriter, r *http.Request) {
	publicHeaders(w)
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	v, err := d.sharedTrip(r.Context(), chiURLParam(r, "token"))
	data := map[string]any{}
	switch {
	case errors.Is(err, store.ErrNoSuchSharedTrip), errors.Is(err, live.ErrNotOnTrip):
		w.WriteHeader(http.StatusNotFound)
		data["Gone"] = "Αυτός ο σύνδεσμος δεν δείχνει πια κάποια διαδρομή."
	case err != nil:
		slog.Warn("reading a trip link", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		data["Gone"] = "Κάτι πήγε στραβά. Δοκίμασε ξανά σε λίγο."
	default:
		now := d.now()
		loc := d.Location
		if loc == nil {
			loc = time.UTC
		}
		data["Trip"] = v.Trip
		data["Depart"] = v.Trip.DepartAt.In(loc).Format("02/01 15:04")
		data["Car"] = strings.TrimSpace(v.Trip.CarModel + " " + v.Trip.CarColor)
		data["Opens"] = v.Opens.In(loc).Format("15:04")
		data["Before"] = now.Before(v.Opens)
		data["Ended"] = !v.Closes.IsZero() && now.After(v.Closes)
		if p := v.Position; p != nil {
			data["Position"] = true
			data["Lat"] = fmt.Sprintf("%.5f", p.Lat)
			data["Lon"] = fmt.Sprintf("%.5f", p.Lon)
			data["Age"] = ageWords(now.Sub(p.ObservedAt))
			data["MapURL"] = fmt.Sprintf("https://www.google.com/maps/search/?api=1&query=%.5f,%.5f", p.Lat, p.Lon)
			if v.ETAMin != nil {
				data["ETAMin"] = *v.ETAMin
			}
		}
	}
	_ = tripPage.Execute(w, data)
}

func ageWords(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d″", int(d.Seconds()))
	}
	return fmt.Sprintf("%d′", int(d.Minutes()))
}
