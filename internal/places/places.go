// Package places answers "where do you want picked up" with a name instead of
// a coordinate.
//
// The dataset is 153 settlements in the Evros prefecture, harvested from
// OpenStreetMap by scripts/harvest-places.py and embedded here. At that size a
// table would buy nothing: the whole thing is searched in memory in
// microseconds, there is no migration to run, and the data changes when someone
// deliberately re-harvests it rather than at runtime.
//
// © OpenStreetMap contributors, ODbL.
package places

import (
	_ "embed"
	"encoding/csv"
	"slices"
	"strconv"
	"strings"
	"sync"
)

//go:embed data/evros.csv
var dataset string

type Place struct {
	// ID is "node/123456": the OSM type and id together, because ids are only
	// unique within a type, and because a name is not an identity — two
	// villages in this dataset can share one.
	ID     string  `json:"id"`
	Region string  `json:"region"`
	Kind   string  `json:"kind"` // city, town or village
	Name   string  `json:"name"`
	NameEn string  `json:"name_en,omitempty"`
	Lon    float64 `json:"lon"`
	Lat    float64 `json:"lat"`
}

// How a place matched, which is most of how results are ordered. A name OSM
// carries is worth more than a spelling this package invented.
type keyClass int

const (
	authoritative keyClass = iota // name, name:en, int_name, alt_name
	generated                     // romanisations produced here
)

type key struct {
	text  string
	class keyClass
	first bool // the key starts the name, rather than a later word in it
}

type indexed struct {
	Place
	keys []key
}

var (
	once  sync.Once
	index []indexed
)

func load() {
	r := csv.NewReader(strings.NewReader(dataset))
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		// Embedded at build time, so a failure here is a broken binary rather
		// than a bad input, and every caller would have to handle an error that
		// can only mean that.
		panic("places: dataset unreadable: " + err.Error())
	}

	col := map[string]int{}
	for i, h := range rows[0] {
		col[h] = i
	}

	for _, row := range rows[1:] {
		lon, errLon := strconv.ParseFloat(row[col["lon"]], 64)
		lat, errLat := strconv.ParseFloat(row[col["lat"]], 64)
		if errLon != nil || errLat != nil {
			continue
		}
		p := Place{
			ID:     row[col["osm_type"]] + "/" + row[col["osm_id"]],
			Region: row[col["region"]],
			Kind:   row[col["kind"]],
			Name:   row[col["name"]],
			NameEn: row[col["name_en"]],
			Lon:    lon,
			Lat:    lat,
		}
		index = append(index, indexed{Place: p, keys: keysFor(row, col)})
	}
}

// keysFor collects every spelling a place should be findable under: the names
// OSM carries, then romanisations of the Greek one, and for each of those the
// later words too, since half these settlements begin with Νέα or Άγιος.
func keysFor(row []string, col map[string]int) []key {
	var keys []key
	seen := map[string]bool{}

	add := func(text string, class keyClass, first bool) {
		if text == "" || seen[text] {
			return
		}
		seen[text] = true
		keys = append(keys, key{text: text, class: class, first: first})
	}

	addName := func(name string, class keyClass) {
		folded := fold(name)
		if folded == "" {
			return
		}
		add(folded, class, true)
		for _, word := range strings.Fields(folded)[1:] {
			add(word, class, false)
		}
	}

	for _, field := range []string{"name", "name_en", "int_name", "alt_name"} {
		if i, ok := col[field]; ok {
			addName(row[i], authoritative)
		}
	}
	for _, romanised := range latinKeys(fold(row[col["name"]])) {
		addName(romanised, generated)
	}
	return keys
}

func All() []Place {
	once.Do(load)
	out := make([]Place, len(index))
	for i, e := range index {
		out[i] = e.Place
	}
	return out
}

func rank(kind string) int {
	switch kind {
	case "city":
		return 0
	case "town":
		return 1
	default:
		return 2
	}
}

// score is what orders the results, lowest first. Whole-name matches come
// before prefixes so that a village typed in full is not buried under a town
// that merely starts the same way, and a name OSM carries beats one generated
// here. Settlement size only breaks ties between matches of equal quality,
// which is what makes two letters return the town.
func score(k key, q string) (int, bool) {
	switch {
	case k.text == q:
		if k.class == authoritative && k.first {
			return 0, true
		}
		return 1, true
	case strings.HasPrefix(k.text, q):
		switch {
		case k.class == authoritative && k.first:
			return 2, true
		case k.first:
			return 3, true
		default:
			return 4, true
		}
	}
	return 0, false
}

// Search returns up to limit places matching what was typed, best first.
func Search(query string, limit int) []Place {
	once.Do(load)
	return searchIn(index, query, limit)
}

// searchIn is Search over a given set, so that ranking can be tested against a
// fixture built for the case rather than against whatever OSM currently says
// about the Evros prefecture. Ranking rules outlive any particular dataset.
func searchIn(set []indexed, query string, limit int) []Place {
	q := fold(query)
	if q == "" || limit <= 0 {
		return nil
	}

	type hit struct {
		Place
		score int
	}
	var hits []hit

	for _, e := range set {
		best := -1
		for _, k := range e.keys {
			if s, ok := score(k, q); ok && (best < 0 || s < best) {
				best = s
			}
		}
		if best >= 0 {
			hits = append(hits, hit{Place: e.Place, score: best})
		}
	}

	// Sorted rather than partially sorted, and by id last, so that the same
	// query always returns the same order. Autocomplete redraws on every
	// keystroke, and a list that reshuffles under a finger is unusable.
	slices.SortFunc(hits, func(a, b hit) int {
		if a.score != b.score {
			return a.score - b.score
		}
		if r := rank(a.Kind) - rank(b.Kind); r != 0 {
			return r
		}
		if l := len(a.Name) - len(b.Name); l != 0 {
			return l
		}
		return strings.Compare(a.ID, b.ID)
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Place, len(hits))
	for i, h := range hits {
		out[i] = h.Place
	}
	return out
}
