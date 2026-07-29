package geo_test

import (
	"math"
	"testing"

	"carpool/internal/geo"
)

// The encoding is the Google/OSRM standard at precision 5. This is the example
// from the specification, which pins the sign and chunking rules.
func TestDecodesTheReferenceExample(t *testing.T) {
	got, err := geo.DecodePolyline("_p~iF~ps|U_ulLnnqC_mqNvxq`@")
	if err != nil {
		t.Fatalf("DecodePolyline: %v", err)
	}

	want := [][2]float64{{38.5, -120.2}, {40.7, -120.95}, {43.252, -126.453}}
	if len(got) != len(want) {
		t.Fatalf("%d points, want %d", len(got), len(want))
	}
	for i, p := range got {
		if math.Abs(p.Lat-want[i][0]) > 1e-6 || math.Abs(p.Lon-want[i][1]) > 1e-6 {
			t.Fatalf("point %d = %v, want %v", i, p, want[i])
		}
	}
}

func TestDecodesAnEmptyPolylineAsNoPoints(t *testing.T) {
	got, err := geo.DecodePolyline("")
	if err != nil {
		t.Fatalf("DecodePolyline: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("%d points, want 0", len(got))
	}
}

// A truncated chunk would otherwise run off the end of the string and either
// panic or return a point built from half its bits.
func TestRejectsATruncatedPolyline(t *testing.T) {
	if _, err := geo.DecodePolyline("_p~iF~ps|U_ulL"[:12]); err == nil {
		t.Fatal("a truncated polyline decoded without error")
	}
}

func TestRejectsCharactersOutsideTheAlphabet(t *testing.T) {
	if _, err := geo.DecodePolyline("_p~iF\x01~ps|U"); err == nil {
		t.Fatal("an invalid character decoded without error")
	}
}

// The app stores geometry at precision 6, and the toll matcher reads the same
// column. Decoding a precision-6 line as precision 5 puts every point ten times
// too far from the origin — off the map, silently.
func TestDecodesPrecisionSix(t *testing.T) {
	// Alexandroupoli then Komotini, encoded at precision 6.
	got, err := geo.DecodePolylinePrecision("gw_|lA_dfjp@gnyOnmq[", 6)
	if err != nil {
		t.Fatalf("DecodePolylinePrecision: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("%d points, want 2", len(got))
	}
	if math.Abs(got[0].Lat-40.8457) > 1e-6 || math.Abs(got[0].Lon-25.8740) > 1e-6 {
		t.Fatalf("first point = %v, want Alexandroupoli", got[0])
	}
	if math.Abs(got[1].Lat-41.1214) > 1e-6 || math.Abs(got[1].Lon-25.4058) > 1e-6 {
		t.Fatalf("second point = %v, want Komotini", got[1])
	}
}
