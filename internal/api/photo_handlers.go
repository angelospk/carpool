package api

import (
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"carpool/internal/photos"
)

// handlePutPhoto takes an upload and stores what the server made of it.
//
// The body is the image itself rather than a multipart form. There is one file
// and no other fields, so multipart would add a parser, a boundary, a
// per-part limit and the gap between "the request is 2 MB" and "the file is
// 2 MB" — all to carry one thing.
func (d Deps) handlePutPhoto(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	// Capped before anything reads it, so a body that never ends cannot occupy
	// a goroutine and a connection indefinitely.
	body := http.MaxBytesReader(w, r.Body, photos.MaxUpload+1)
	defer body.Close()

	photo, err := photos.Canonicalise(body)
	switch {
	case errors.Is(err, photos.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "that image is too large")
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, "invalid_image", "that is not a JPEG or PNG")
		return
	}

	if _, err := d.Pool.Exec(r.Context(), `
		INSERT INTO profile_photos (user_id, data, content_type, sha256, width, height)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id) DO UPDATE SET
			data = EXCLUDED.data, content_type = EXCLUDED.content_type,
			sha256 = EXCLUDED.sha256, width = EXCLUDED.width, height = EXCLUDED.height,
			updated_at = now()`,
		userID, photo.Data, photo.ContentType, photo.SHA256, photo.Width, photo.Height); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "the photo could not be saved")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"photo_url": photoURL(userID, photo.SHA256)})
}

func (d Deps) handleDeletePhoto(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	// No row is not a failure: the caller wanted no photo and there is none.
	if _, err := d.Pool.Exec(r.Context(),
		`DELETE FROM profile_photos WHERE user_id = $1`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "the photo could not be removed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetPhoto serves a stored photo.
//
// Behind the same authentication as the rest: these are photographs of people
// who agreed to share a car in one prefecture, not public images. Cache-Control
// is private for the same reason.
func (d Deps) handleGetPhoto(w http.ResponseWriter, r *http.Request) {
	if _, ok := callerID(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	subject, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no photo")
		return
	}

	var (
		data        []byte
		contentType string
		sum         []byte
	)
	err = d.Pool.QueryRow(r.Context(),
		`SELECT data, content_type, sha256 FROM profile_photos WHERE user_id = $1`, subject).
		Scan(&data, &contentType, &sum)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "no photo")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "the photo could not be read")
		return
	}
	// The connection is back in the pool before a byte is written, so a phone
	// on a slow train cannot hold one open while it reads.

	etag := `"` + hex.EncodeToString(sum) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	// The content type is the server's, from what it decoded. nosniff stops a
	// browser deciding for itself that these bytes are something executable.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// photoURL includes a prefix of the hash, so replacing a photo changes its
// address and no cache anywhere can go on serving the old one.
func photoURL(userID uuid.UUID, sum []byte) string {
	return "/v1/users/" + userID.String() + "/photo?v=" + hex.EncodeToString(sum)[:12]
}
