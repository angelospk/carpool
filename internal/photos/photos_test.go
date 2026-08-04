package photos

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func jpegOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAPhotoComesBackAsAPhoto(t *testing.T) {
	for name, raw := range map[string][]byte{
		"jpeg": jpegOf(t, 400, 300),
		"png":  pngOf(t, 400, 300),
	} {
		got, err := Canonicalise(bytes.NewReader(raw))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got.Width != 400 || got.Height != 300 {
			t.Errorf("%s: %dx%d, want 400x300", name, got.Width, got.Height)
		}
		if len(got.SHA256) != 32 {
			t.Errorf("%s: hash is %d bytes", name, len(got.SHA256))
		}
		// The format is chosen by the server from what it decoded, never from
		// what the request claimed.
		if got.ContentType != "image/"+name {
			t.Errorf("%s: content type %q", name, got.ContentType)
		}
	}
}

func TestABigPhotoIsScaledDown(t *testing.T) {
	got, err := Canonicalise(bytes.NewReader(jpegOf(t, 1600, 900)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != MaxSide {
		t.Errorf("width %d, want %d", got.Width, MaxSide)
	}
	// Proportions kept: 1600x900 is 16:9, so the height follows.
	if want := 900 * MaxSide / 1600; got.Height != want {
		t.Errorf("height %d, want %d", got.Height, want)
	}
}

func TestASmallPhotoIsNotEnlarged(t *testing.T) {
	// Scaling a 64px avatar up to 512 produces a blurry 512px avatar and a
	// bigger file. Leaving it alone is better on both counts.
	got, err := Canonicalise(bytes.NewReader(jpegOf(t, 64, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 64 || got.Height != 64 {
		t.Errorf("%dx%d, want 64x64", got.Width, got.Height)
	}
}

func TestWhatIsNotAnImageIsRefused(t *testing.T) {
	cases := map[string][]byte{
		"empty":            {},
		"text":             []byte("hello"),
		"an SVG":           []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`),
		"a GIF":            {'G', 'I', 'F', '8', '9', 'a', 1, 0, 1, 0},
		"a truncated JPEG": jpegOf(t, 400, 300)[:120],
	}
	for name, raw := range cases {
		if _, err := Canonicalise(bytes.NewReader(raw)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestTheLimitIsOnBytesAndOnPixels(t *testing.T) {
	// A byte limit alone protects nothing. This image is a few kilobytes on the
	// wire and gigabytes decoded, which is the whole trick.
	bomb := pngOf(t, 8000, 8000)
	if len(bomb) > MaxUpload {
		t.Fatalf("the fixture is %d bytes, so this would be refused for its size instead", len(bomb))
	}
	if _, err := Canonicalise(bytes.NewReader(bomb)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a decompression bomb gave %v", err)
	}

	// And the byte limit still applies on its own.
	huge := strings.NewReader(strings.Repeat("x", MaxUpload+1))
	if _, err := Canonicalise(huge); err == nil {
		t.Error("an oversized upload was accepted")
	}
}

func TestNothingOfTheOriginalFileSurvives(t *testing.T) {
	// The reason re-encoding is not optional. A JPEG carries an EXIF block that
	// routinely holds the coordinates the photo was taken at, which for a
	// profile picture is usually somebody's home.
	raw := jpegOf(t, 400, 300)
	marker := []byte("GPSLatitudeSECRETHOUSE")

	// APP1 is where EXIF lives: right after the two-byte start-of-image.
	segment := append([]byte{0xFF, 0xE1, 0x00, byte(len(marker) + 2)}, marker...)
	withExif := append(append(append([]byte{}, raw[:2]...), segment...), raw[2:]...)
	// Plus a payload stuck on the end, which every image viewer ignores and
	// every naive "store what you were sent" keeps.
	withExif = append(withExif, []byte("APPENDED-PAYLOAD")...)

	got, err := Canonicalise(bytes.NewReader(withExif))
	if err != nil {
		t.Fatalf("the fixture did not decode: %v", err)
	}
	if bytes.Contains(got.Data, marker) {
		t.Error("the EXIF block survived into the stored image")
	}
	if bytes.Contains(got.Data, []byte("APPENDED-PAYLOAD")) {
		t.Error("data appended after the image survived into the stored image")
	}
}

func TestTheSameImageHashesTheSameAndADifferentOneDoesNot(t *testing.T) {
	// The hash is the ETag and the cache-busting part of the URL, so a replaced
	// photo that hashed the same would be served from an old cache forever.
	a, _ := Canonicalise(bytes.NewReader(jpegOf(t, 200, 200)))
	b, _ := Canonicalise(bytes.NewReader(jpegOf(t, 200, 200)))
	c, _ := Canonicalise(bytes.NewReader(jpegOf(t, 201, 200)))

	if !bytes.Equal(a.SHA256, b.SHA256) {
		t.Error("the same image hashed differently twice")
	}
	if bytes.Equal(a.SHA256, c.SHA256) {
		t.Error("two different images hashed the same")
	}
}
