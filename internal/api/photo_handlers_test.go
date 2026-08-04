package api_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"
)

func aJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// upload sends raw bytes, which is what the endpoint takes.
func (h *harness) upload(method, path string, raw []byte, token string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/octet-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func TestAPhotoCanBeUploadedAndReadBack(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000801", "Γιώργος")
	var me struct {
		ID string `json:"id"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me", nil, token), &me)

	rec := h.upload(http.MethodPut, "/v1/me/photo", aJPEG(t, 300, 300), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		PhotoURL string `json:"photo_url"`
	}
	h.decode(rec, &out)
	if out.PhotoURL == "" {
		t.Fatal("no photo_url came back")
	}

	got := h.do(http.MethodGet, "/v1/users/"+me.ID+"/photo", nil, token)
	if got.Code != http.StatusOK {
		t.Fatalf("fetch: %d", got.Code)
	}
	// The type is the server's own, and nosniff stops a browser deciding these
	// bytes are something else.
	if ct := got.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("content type %q", ct)
	}
	if got.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff missing")
	}
	if got.Header().Get("ETag") == "" {
		t.Error("no ETag, so every view re-downloads the image")
	}
	if got.Body.Len() == 0 {
		t.Error("empty body")
	}
}

func TestAnUnchangedPhotoIsNotSentTwice(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000802", "Γιώργος")
	var me struct {
		ID string `json:"id"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me", nil, token), &me)
	h.upload(http.MethodPut, "/v1/me/photo", aJPEG(t, 200, 200), token)

	first := h.do(http.MethodGet, "/v1/users/"+me.ID+"/photo", nil, token)
	etag := first.Header().Get("ETag")

	req := httptest.NewRequest(http.MethodGet, "/v1/users/"+me.ID+"/photo", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Errorf("status %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Error("304 carried a body")
	}
}

func TestReplacingAPhotoChangesItsAddress(t *testing.T) {
	// Otherwise a cache keeps serving the picture somebody just took down.
	h := newHarness(t)
	token, _ := h.signUp("+306900000803", "Γιώργος")

	var first, second struct {
		PhotoURL string `json:"photo_url"`
	}
	h.decode(h.upload(http.MethodPut, "/v1/me/photo", aJPEG(t, 200, 200), token), &first)
	h.decode(h.upload(http.MethodPut, "/v1/me/photo", aJPEG(t, 240, 200), token), &second)

	if first.PhotoURL == second.PhotoURL {
		t.Errorf("both photos are at %s", first.PhotoURL)
	}
}

func TestWhatIsNotAnImageIsRefusedByTheEndpoint(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000804", "Γιώργος")

	for name, raw := range map[string][]byte{
		"text":   []byte("hello"),
		"an SVG": []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`),
		"empty":  {},
	} {
		if rec := h.upload(http.MethodPut, "/v1/me/photo", raw, token); rec.Code != http.StatusBadRequest {
			t.Errorf("%s gave %d, want 400", name, rec.Code)
		}
	}
}

func TestAPhotoCanBeTakenDown(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000805", "Γιώργος")
	var me struct {
		ID string `json:"id"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me", nil, token), &me)
	h.upload(http.MethodPut, "/v1/me/photo", aJPEG(t, 200, 200), token)

	if rec := h.do(http.MethodDelete, "/v1/me/photo", nil, token); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := h.do(http.MethodGet, "/v1/users/"+me.ID+"/photo", nil, token); rec.Code != http.StatusNotFound {
		t.Errorf("after deleting, fetch gave %d, want 404", rec.Code)
	}
	// Twice is not an error: the caller wanted no photo and there is none.
	if rec := h.do(http.MethodDelete, "/v1/me/photo", nil, token); rec.Code != http.StatusNoContent {
		t.Errorf("second delete: %d", rec.Code)
	}
}

func TestPhotosAreNotPublic(t *testing.T) {
	// These are photographs of people who agreed to share a car in one
	// prefecture, not images on the open web.
	h := newHarness(t)
	token, _ := h.signUp("+306900000806", "Γιώργος")
	var me struct {
		ID string `json:"id"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me", nil, token), &me)
	h.upload(http.MethodPut, "/v1/me/photo", aJPEG(t, 200, 200), token)

	if rec := h.do(http.MethodGet, "/v1/users/"+me.ID+"/photo", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous fetch gave %d, want 401", rec.Code)
	}
	if rec := h.upload(http.MethodPut, "/v1/me/photo", aJPEG(t, 200, 200), ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous upload gave %d, want 401", rec.Code)
	}
}

func TestAPhotoURLCannotBeSetByHand(t *testing.T) {
	// It used to be an ordinary field on PATCH /me, which let anybody point
	// their avatar at any URL on the internet: an origin they control, serving
	// whatever they like, fetched by every other user's device.
	h := newHarness(t)
	token, _ := h.signUp("+306900000807", "Γιώργος")

	// Refused outright rather than accepted and ignored, because the decoder
	// rejects fields it does not know. A client sending this is a client with a
	// wrong idea about where photos come from, and hearing so is more use than
	// a silent no-op.
	rec := h.do(http.MethodPatch, "/v1/me", map[string]any{
		"photo_url": "https://attacker.example/track.jpg",
	}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("patch gave %d, want 400: %s", rec.Code, rec.Body.String())
	}

	var out struct {
		PhotoURL *string `json:"photo_url"`
	}
	h.decode(h.do(http.MethodGet, "/v1/me", nil, token), &out)
	if out.PhotoURL != nil {
		t.Errorf("photo_url ended up as %q", *out.PhotoURL)
	}
}
