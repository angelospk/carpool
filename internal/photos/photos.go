// Package photos turns whatever somebody uploaded into one canonical image.
//
// Nothing a user sends is stored as it arrived. It is decoded with a real
// decoder, checked, scaled down and re-encoded, and the output of that is what
// goes in the database. Re-encoding is not about file size: it is what removes
// the EXIF block with the GPS coordinates of the person's house, anything
// appended after the image data, unusual PNG chunks, and the various tricks
// that make one file parse as two formats.
//
// The client also downscales before sending. That is a courtesy to the network,
// not a check: everything here has to hold for a request written by hand.
package photos

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/draw"
)

var (
	// ErrNotAnImage covers everything from a text file to a truncated JPEG to a
	// format we do not accept. They are one answer on purpose: a caller can act
	// on "send a JPEG or PNG" and cannot act on which of eleven ways it failed.
	ErrNotAnImage = errors.New("that is not a JPEG or PNG")
	// ErrTooLarge is separate because it is the one the person can do something
	// about by choosing a different photo.
	ErrTooLarge = errors.New("that image is too large")
)

const (
	// MaxUpload is the most that will be read from a request body. A byte more
	// and it is refused without decoding.
	MaxUpload = 2 << 20 // 2 MiB
	// MaxPixels bounds the decoded image rather than the compressed one. A
	// 30,000 by 30,000 PNG of one colour is a few hundred kilobytes on the wire
	// and 3.6 GB in memory, so a byte limit alone does not protect anything.
	MaxPixels = 4 << 20 // 4 million
	// MaxSide is what an avatar is scaled into. Generous for a face on a phone.
	MaxSide = 512
	// jpegQuality: 82 is where the difference stops being visible at avatar
	// size while the file stays small.
	jpegQuality = 82
)

// Photo is a stored image.
type Photo struct {
	Data        []byte
	ContentType string
	SHA256      []byte
	Width       int
	Height      int
}

// Canonicalise reads an upload and returns what should be stored.
func Canonicalise(r io.Reader) (Photo, error) {
	// One byte over the limit is read on purpose, so that a file exactly at the
	// limit is accepted and one above it is refused rather than silently cut
	// short and rejected later as corrupt.
	raw, err := io.ReadAll(io.LimitReader(r, MaxUpload+1))
	if err != nil {
		return Photo{}, ErrNotAnImage
	}
	if len(raw) > MaxUpload {
		return Photo{}, ErrTooLarge
	}
	if len(raw) == 0 {
		return Photo{}, ErrNotAnImage
	}

	// The dimensions are read before the pixels are, which is the whole point:
	// a decompression bomb is refused from its header rather than after it has
	// been allocated.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return Photo{}, ErrNotAnImage
	}
	if format != "jpeg" && format != "png" {
		// GIF, WebP, BMP and anything else a decoder happens to be registered
		// for. Not because they are dangerous, but because two accepted formats
		// are two code paths to be sure of.
		return Photo{}, ErrNotAnImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return Photo{}, ErrNotAnImage
	}
	// Multiplied as int64 because on a 32-bit build the product of two large
	// dimensions overflows into a small positive number that passes the check.
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return Photo{}, ErrTooLarge
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		// Reached by a file whose header parsed and whose body did not: a
		// truncated upload, or one crafted to disagree with itself.
		return Photo{}, ErrNotAnImage
	}

	dst := scale(src)

	var buf bytes.Buffer
	contentType := "image/jpeg"
	if format == "png" {
		// PNG stays PNG so that a logo or a drawing with sharp edges does not
		// come back blurred, and so that transparency survives.
		contentType = "image/png"
		err = png.Encode(&buf, dst)
	} else {
		err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality})
	}
	if err != nil {
		return Photo{}, fmt.Errorf("photos: encoding: %w", err)
	}

	// A PNG of noise can re-encode larger than it arrived. Refused rather than
	// stored, because the column has a limit and a constraint violation here
	// would surface as an internal error.
	if buf.Len() > MaxUpload/2 {
		return Photo{}, ErrTooLarge
	}

	sum := sha256.Sum256(buf.Bytes())
	b := dst.Bounds()
	return Photo{
		Data:        buf.Bytes(),
		ContentType: contentType,
		SHA256:      sum[:],
		Width:       b.Dx(),
		Height:      b.Dy(),
	}, nil
}

// scale fits the image inside MaxSide without changing its proportions, and
// leaves a smaller image alone rather than enlarging it into blur.
func scale(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= MaxSide && h <= MaxSide {
		return src
	}

	if w > h {
		h = h * MaxSide / w
		w = MaxSide
	} else {
		w = w * MaxSide / h
		h = MaxSide
	}
	// Integer division can land on zero for a very long thin image, and a
	// zero-sided image encodes to nothing.
	w, h = max(w, 1), max(h, 1)

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// CatmullRom rather than the cheaper kernels: this runs once per upload,
	// and a face is what the result is judged on.
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}
