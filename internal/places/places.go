// Package places answers "where do you want picked up" with a name instead of
// a coordinate.
//
// The dataset is every city, town, village and hamlet in Greece — 12,442 of
// them — harvested from the same OpenStreetMap extract the router runs on by
// scripts/harvest-places.py, and embedded here.
//
// Still embedded rather than in a table, and still searched by walking the
// whole list. That was obviously right at 153 rows and is worth restating at
// twelve thousand: measured, a search costs 0.85 ms and does not grow with how
// broad the query is. One that has to correct a typo costs about 3 ms. A table would add a migration, a round trip and a second
// place for the ranking to live, and buy nothing back.
//
// © OpenStreetMap contributors, ODbL.
package places

import (
	"cmp"
	_ "embed"
	"encoding/csv"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
)

//go:embed data/greece.csv
var dataset string

type Place struct {
	// ID is "node/123456": the OSM type and id together, because ids are only
	// unique within a type, and because a name is not an identity — two
	// villages in this dataset can share one.
	ID   string `json:"id"`
	Kind string `json:"kind"` // city, town, village or hamlet
	Name string `json:"name"`
	// Area is the municipality, and it is not decoration. Greece has dozens of
	// Άγιος Γεώργιος; a list of identical rows is no better than no list.
	Area   string  `json:"area"`
	AreaEn string  `json:"area_en,omitempty"`
	NameEn string  `json:"name_en,omitempty"`
	Lon    float64 `json:"lon"`
	Lat    float64 `json:"lat"`

	// Population orders places of the same kind. OSM has it for 50 of the 54
	// cities and about half the villages; where it is missing it is zero, which
	// puts an untagged place behind a tagged one of the same kind rather than
	// ahead of it.
	Population int `json:"-"`
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
			// Not skipped. A settlement dropped quietly is one nobody can be
			// picked up from, and no test over the loaded list can see the
			// difference between a row that was never harvested and one that
			// was thrown away here.
			panic("places: settlement with an unreadable coordinate: " + row[col["osm_id"]])
		}
		population, _ := strconv.Atoi(row[col["population"]])
		p := Place{
			ID:         row[col["osm_type"]] + "/" + row[col["osm_id"]],
			Area:       row[col["area"]],
			AreaEn:     row[col["area_en"]],
			Kind:       row[col["kind"]],
			Name:       row[col["name"]],
			NameEn:     row[col["name_en"]],
			Lon:        lon,
			Lat:        lat,
			Population: population,
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
	case "village":
		return 2
	default:
		// hamlet, and anything OSM invents later. Last on purpose: there are
		// 4,832 of them against 54 cities, and a search that led with them
		// would be complete and useless in the same breath.
		return 3
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

// MinQuery is the shortest thing that counts as a search.
//
// One character is not a search, it is a sample of the dataset: 4,832 hamlets
// start with alpha and eight of them arrive in no order anyone asked for. Two
// rather than three because the shortest Greek place names are exactly three
// letters — Κως, Ίος, Λία — and a floor of three would mean typing the whole
// name before seeing it.
const MinQuery = 2

// MaxQuery is the longest thing that counts as a search. The longest name in
// the dataset is under fifty letters; anything longer is not a place, and on
// a public endpoint it would be paid for on every keystroke.
const MaxQuery = 64

// Search returns up to limit places matching what was typed, best first.
func Search(query string, limit int) []Place {
	return SearchNear(query, limit, nil)
}

// Near is where the person is going from or to, when they have said. Among
// equally good matches, places within NearKm of it come first.
type Near struct{ Lon, Lat float64 }

// NearKm is how close counts as near: a prefecture, give or take.
const NearKm = 100

// SearchNear is Search, with places near near ahead of equally good matches
// elsewhere. A nil near is plain Search. "Νέα…" typed in Evros led with Νέα
// Μουδανιά (QA 2026-10-16, U12).
func SearchNear(query string, limit int, near *Near) []Place {
	once.Do(load)
	return searchNear(index, query, limit, near)
}

// searchIn is Search over a given set, so that ranking can be tested against a
// fixture built for the case rather than against whatever OSM currently says
// about the Evros prefecture. Ranking rules outlive any particular dataset.
func searchIn(set []indexed, query string, limit int) []Place {
	return searchNear(set, query, limit, nil)
}

func searchNear(set []indexed, query string, limit int, near *Near) []Place {
	// "Ορεστιάδα, Έβρος" is a name and then its area, the way an address is
	// written. The name is what is searched.
	if name, _, found := strings.Cut(query, ","); found && strings.TrimSpace(name) != "" {
		query = name
	}
	q := fold(query)
	if n := len([]rune(q)); n < MinQuery || n > MaxQuery || limit <= 0 {
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

	// Nothing, or not enough, spelled that way: a letter missing, doubled,
	// swapped or wrong is still the town. «Αλεξανδρούπλη» and «Kommotini»
	// found nothing (QA 2026-10-16, U12). After every exact and prefix match,
	// so a correct spelling always wins.
	if len(hits) < limit {
		matched := make(map[string]bool, len(hits))
		for _, h := range hits {
			matched[h.ID] = true
		}
		for _, e := range set {
			if matched[e.ID] {
				continue
			}
			if d, ok := typo(e.keys, q); ok {
				hits = append(hits, hit{Place: e.Place, score: fuzzyScore + d})
			}
		}
	}

	// Kilometres beyond NearKm, or 0 for anything near enough to count as
	// here. Within NearKm the kind and size decide, as before; beyond it the
	// nearer of two homonyms wins even if the farther one is a bigger place.
	// «Αύρα» listed the one in Άρτα, 351 km away, ahead of a closer one (F20).
	away := func(p Place) float64 {
		if near == nil {
			return 0
		}
		if d := geoKm(near.Lon, near.Lat, p.Lon, p.Lat); d > NearKm {
			return d
		}
		return 0
	}

	// Sorted rather than partially sorted, and by id last, so that the same
	// query always returns the same order. Autocomplete redraws on every
	// keystroke, and a list that reshuffles under a finger is unusable.
	slices.SortFunc(hits, func(a, b hit) int {
		if a.score != b.score {
			return a.score - b.score
		}
		if c := cmp.Compare(away(a.Place), away(b.Place)); c != 0 {
			return c
		}
		if r := rank(a.Kind) - rank(b.Kind); r != 0 {
			return r
		}
		// Larger first. This is what puts Αλεξανδρούπολη ahead of Αλεξάνδρεια
		// for "alexandr": both are cities, so the tie used to fall to the
		// shorter name, which is not a fact about either place.
		if p := b.Population - a.Population; p != 0 {
			return p
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

// fuzzyScore is where a corrected spelling ranks: after every prefix match
// (score returns at most 4), and further back the more letters it took.
const fuzzyScore = 5

// typo reports how many edits make q the start of one of keys, if few enough.
//
// One edit up to seven letters, two from eight. Under four letters nothing is
// corrected: «Βέρ» one edit from «Φέρ» is half the country. The first letter
// has to match, as it almost always does in a typo, which also keeps this to a
// fraction of the twelve thousand places.
func typo(keys []key, q string) (int, bool) {
	qr := []rune(q)
	if len(qr) < 4 {
		return 0, false
	}
	allowed := 1
	if len(qr) >= 8 {
		allowed = 2
	}
	best := allowed + 1
	for _, k := range keys {
		kr := []rune(k.text)
		// Too short to be within reach of q, or not starting the same way.
		if len(kr) < len(qr)-allowed || kr[0] != qr[0] {
			continue
		}
		if d := prefixDistance(qr, kr, allowed); d < best {
			best = d
		}
	}
	return best, best <= allowed
}

// prefixDistance is the fewest edits (insert, delete, substitute, or swap two
// neighbours) that turn q into some prefix of k, from len(q)-allowed to
// len(q)+allowed letters long.
func prefixDistance(q, k []rune, allowed int) int {
	cols := min(len(k), len(q)+allowed)
	// Three rows: the swap looks two back.
	prev2 := make([]int, cols+1)
	prev := make([]int, cols+1)
	cur := make([]int, cols+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(q); i++ {
		cur[0] = i
		for j := 1; j <= cols; j++ {
			cost := 1
			if q[i-1] == k[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && q[i-1] == k[j-2] && q[i-2] == k[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	best := allowed + 1
	for j := max(0, len(q)-allowed); j <= cols; j++ {
		best = min(best, prev[j])
	}
	return best
}

// geoKm is the great-circle distance between two points, in kilometres.
func geoKm(lon1, lat1, lon2, lat2 float64) float64 {
	const r = 6371.0
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}
