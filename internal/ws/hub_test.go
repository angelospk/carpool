package ws_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"carpool/internal/ws"
)

func TestSubscriberReceivesAPublishedMessage(t *testing.T) {
	h := ws.NewHub()
	topic := uuid.New()
	sub, unsubscribe := h.Subscribe(topic)
	defer unsubscribe()

	h.Publish(topic, map[string]any{"type": "message", "body": "γεια"})

	select {
	case raw := <-sub.Messages():
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("frame is not JSON: %v", err)
		}
		if got["body"] != "γεια" {
			t.Fatalf("body = %v, want γεια", got["body"])
		}
	case <-time.After(time.Second):
		t.Fatal("no frame arrived")
	}
}

func TestPublishReachesEverySubscriberOfATopic(t *testing.T) {
	h := ws.NewHub()
	topic := uuid.New()
	a, closeA := h.Subscribe(topic)
	defer closeA()
	b, closeB := h.Subscribe(topic)
	defer closeB()

	h.Publish(topic, map[string]any{"n": 1})

	for name, sub := range map[string]*ws.Subscriber{"a": a, "b": b} {
		select {
		case <-sub.Messages():
		case <-time.After(time.Second):
			t.Fatalf("subscriber %s received nothing", name)
		}
	}
}

func TestPublishDoesNotLeakAcrossTopics(t *testing.T) {
	h := ws.NewHub()
	mine, closeMine := h.Subscribe(uuid.New())
	defer closeMine()
	other := uuid.New()

	h.Publish(other, map[string]any{"secret": true})

	select {
	case raw := <-mine.Messages():
		t.Fatalf("received another trip's frame: %s", raw)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	h := ws.NewHub()
	topic := uuid.New()
	sub, unsubscribe := h.Subscribe(topic)

	unsubscribe()
	h.Publish(topic, map[string]any{"n": 1})

	// The channel is closed on unsubscribe, so a receive returns immediately
	// with the zero value rather than a frame.
	select {
	case raw, open := <-sub.Messages():
		if open {
			t.Fatalf("received %s after unsubscribing", raw)
		}
	case <-time.After(time.Second):
		t.Fatal("the subscriber channel was not closed")
	}
	if got := h.SubscriberCount(topic); got != 0 {
		t.Fatalf("subscriber count = %d, want 0", got)
	}
}

func TestUnsubscribeIsSafeToCallTwice(t *testing.T) {
	h := ws.NewHub()
	_, unsubscribe := h.Subscribe(uuid.New())

	unsubscribe()
	unsubscribe() // must not panic on a double close
}

// One stalled client must not block a request handler that is publishing.
func TestPublishDoesNotBlockOnASlowSubscriber(t *testing.T) {
	h := ws.NewHub()
	topic := uuid.New()
	_, unsubscribe := h.Subscribe(topic)
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		// Far more than the send buffer, and nothing is reading.
		for range 1000 {
			h.Publish(topic, map[string]any{"n": 1})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a subscriber that was not reading")
	}
}

func TestPublishToATopicWithNoSubscribersIsANoOp(t *testing.T) {
	ws.NewHub().Publish(uuid.New(), map[string]any{"n": 1})
}
