package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"carpool/internal/api"
	"carpool/internal/ws"
)

// QA 2026-10-17 A 2: a passenger who cancels their seat stops reading the
// car's chat. The socket checked who was on the trip once, when it opened, and
// went on delivering every later message to somebody who had left.
func TestTheChatSocketStopsWhenTheSeatIsCancelled(t *testing.T) {
	h := newHarnessDeps(t, false, nil, func(d *api.Deps) { d.Hub = ws.NewHub() })
	driver, passenger, occ := h.bookedTrip(t)
	srv := httptest.NewServer(h.router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/trips/" + occ.String() + "/socket"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + passenger}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	post := func(body string) {
		t.Helper()
		if rec := h.do(http.MethodPost, "/v1/trips/"+occ.String()+"/messages",
			map[string]any{"body": body}, driver); rec.Code != http.StatusCreated {
			t.Fatalf("posting: %d %s", rec.Code, rec.Body.String())
		}
	}

	post("before")
	_, frame, err := conn.Read(ctx)
	if err != nil || !strings.Contains(string(frame), "before") {
		t.Fatalf("first frame: %q, %v", frame, err)
	}

	var booking string
	h.mustQuery(`SELECT id::text FROM bookings WHERE occurrence_id = $1`, &booking, occ)
	if rec := h.do(http.MethodDelete, "/v1/bookings/"+booking, nil, passenger); rec.Code != http.StatusNoContent {
		t.Fatalf("cancelling: %d %s", rec.Code, rec.Body.String())
	}

	post("after-SECRET")
	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	_, frame, err = conn.Read(readCtx)
	if err == nil && strings.Contains(string(frame), "SECRET") {
		t.Fatalf("a passenger who left read %q", frame)
	}
}
