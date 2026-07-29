package push_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"carpool/internal/outbox"
	"carpool/internal/push"
)

func stub(t *testing.T, handler http.HandlerFunc) *push.Expo {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return push.NewExpo(srv.URL, srv.Client())
}

func TestSendDeliversTheNotification(t *testing.T) {
	var got struct {
		To    string         `json:"to"`
		Title string         `json:"title"`
		Body  string         `json:"body"`
		Data  map[string]any `json:"data"`
	}
	c := stub(t, func(w http.ResponseWriter, r *http.Request) {
		var batch []json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Errorf("request body is not a JSON array: %v", err)
		}
		if len(batch) == 1 {
			_ = json.Unmarshal(batch[0], &got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"status":"ok","id":"ticket-1"}]}`))
	})

	err := c.Send(context.Background(), push.Notification{
		Token: "ExponentPushToken[abc]",
		Title: "Νέο αίτημα",
		Body:  "+6 λεπτά",
		Data:  map[string]any{"ask_id": "a1"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.To != "ExponentPushToken[abc]" {
		t.Errorf("to = %q, want the push token", got.To)
	}
	if got.Title != "Νέο αίτημα" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Data["ask_id"] != "a1" {
		t.Errorf("data = %v, want the ask id carried through", got.Data)
	}
}

// An unregistered device will never accept a notification, so retrying wastes
// the queue. It must be reported as permanent so the outbox dead-letters it.
func TestDeviceNotRegisteredIsAPermanentFailure(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"status":"error","message":"not registered",
			"details":{"error":"DeviceNotRegistered"}}]}`))
	})

	err := c.Send(context.Background(), push.Notification{Token: "ExponentPushToken[dead]"})

	if !errors.Is(err, outbox.ErrPermanent) {
		t.Fatalf("error = %v, want it to wrap outbox.ErrPermanent", err)
	}
	if !errors.Is(err, push.ErrDeviceNotRegistered) {
		t.Fatalf("error = %v, want it to wrap ErrDeviceNotRegistered so the token can be pruned", err)
	}
}

// A transient Expo-side error should be retried, not dead-lettered.
func TestATransientTicketErrorIsRetryable(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"status":"error","message":"try later",
			"details":{"error":"MessageRateExceeded"}}]}`))
	})

	err := c.Send(context.Background(), push.Notification{Token: "ExponentPushToken[abc]"})

	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, outbox.ErrPermanent) {
		t.Fatal("a rate-limit error must stay retryable, not be dead-lettered")
	}
}

func TestAServerErrorIsRetryable(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	err := c.Send(context.Background(), push.Notification{Token: "ExponentPushToken[abc]"})

	if err == nil {
		t.Fatal("expected an error for a 500 from Expo")
	}
	if errors.Is(err, outbox.ErrPermanent) {
		t.Fatal("a 500 must stay retryable")
	}
}

func TestSendRejectsAnEmptyToken(t *testing.T) {
	c := stub(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("Expo should not be called for an empty token")
		w.WriteHeader(http.StatusOK)
	})

	err := c.Send(context.Background(), push.Notification{Token: ""})

	if err == nil {
		t.Fatal("expected an error for an empty token")
	}
	if !errors.Is(err, outbox.ErrPermanent) {
		t.Fatal("an empty token can never succeed, so it must be permanent")
	}
}

func TestFakeRecordsWhatWasSent(t *testing.T) {
	f := &push.Fake{}

	if err := f.Send(context.Background(), push.Notification{Token: "t1", Title: "hello"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	sent := f.Sent()
	if len(sent) != 1 || sent[0].Title != "hello" {
		t.Fatalf("fake recorded %+v, want one notification titled hello", sent)
	}
}

// Compile-time proof both implementations satisfy the interface the outbox
// handler depends on.
var (
	_ push.Sender = (*push.Expo)(nil)
	_ push.Sender = (*push.Fake)(nil)
)
