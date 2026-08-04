package places

import (
	_ "embed"
	"encoding/csv"
	"strconv"
	"strings"
	"sync"
)

// Where in a town, once two people have agreed on the town.
//
// A settlement node in OSM is a label position; it can sit in a field. Two
// people who have agreed on Φέρες still have to agree on where in Φέρες, and
// without a list they do it over the phone, badly.
//
// The list is landmarks OSM already carries — the ΚΤΕΛ, the station, the port,
// the petrol station — ranked by how findable they are rather than how central.
// Everyone in a Greek village can point at the petrol station.
//
// A settlement with nothing tagged gets nothing. An earlier plan filled the gap
// with a synthetic "Κέντρο <village>" at the settlement node, which is a
// coordinate wearing a confident name, and being findable is the only promise a
// meeting point makes. Roughly one settlement in five has points; every city
// does, and nine towns in ten.
//
// © OpenStreetMap contributors, ODbL.

//go:embed data/meeting-points.csv
var pointsData string

// MeetingPoint is somewhere a driver can be told to wait.
type MeetingPoint struct {
	ID string `json:"id"`
	// PlaceID is the settlement this belongs to, as Place.ID.
	PlaceID string `json:"place_id"`
	// Kind is bus_station, train_station, port, fuel or parking. The app shows
	// it, because "the petrol station" and "the port" are found differently.
	Kind   string  `json:"kind"`
	Name   string  `json:"name"`
	NameEn string  `json:"name_en,omitempty"`
	Lon    float64 `json:"lon"`
	Lat    float64 `json:"lat"`
}

var (
	pointsOnce sync.Once
	byPlace    map[string][]MeetingPoint
	allPoints  []MeetingPoint
)

func loadPoints() {
	r := csv.NewReader(strings.NewReader(pointsData))
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		// Embedded at build time, so this can only mean a broken binary.
		panic("places: meeting points unreadable")
	}

	col := map[string]int{}
	for i, h := range rows[0] {
		col[h] = i
	}

	byPlace = make(map[string][]MeetingPoint, len(rows))
	for _, row := range rows[1:] {
		lon, errLon := strconv.ParseFloat(row[col["lon"]], 64)
		lat, errLat := strconv.ParseFloat(row[col["lat"]], 64)
		if errLon != nil || errLat != nil {
			continue
		}
		p := MeetingPoint{
			ID:      "node/" + row[col["osm_id"]],
			PlaceID: row[col["place_id"]],
			Kind:    row[col["kind"]],
			Name:    row[col["name"]],
			NameEn:  row[col["name_en"]],
			Lon:     lon,
			Lat:     lat,
		}
		byPlace[p.PlaceID] = append(byPlace[p.PlaceID], p)
		allPoints = append(allPoints, p)
	}
}

// PointsFor returns the meeting points of one settlement, best first, or nil if
// it has none. Nil is a real answer here: see the note above about not
// inventing one.
func PointsFor(placeID string) []MeetingPoint {
	pointsOnce.Do(loadPoints)
	return byPlace[placeID]
}

// AllPoints returns every meeting point, for tests and diagnostics.
func AllPoints() []MeetingPoint {
	pointsOnce.Do(loadPoints)
	return allPoints
}
