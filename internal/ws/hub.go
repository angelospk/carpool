// Package ws provides the in-process WebSocket hub used while an app is in the
// foreground.
//
// The socket is an optimization, never the source of truth. Push notifications
// are the channel that reaches a backgrounded app, and every client reconciles
// by fetching over REST on reconnect. Nothing here is persisted, and a dropped
// message costs a client only latency.
package ws

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

// sendBuffer is how many messages may queue for one slow client before its
// messages start being dropped.
const sendBuffer = 16

// Hub fans messages out to the subscribers of a topic. Topics are trip
// occurrence ids.
type Hub struct {
	mu   sync.RWMutex
	subs map[uuid.UUID]map[*Subscriber]struct{}
}

// Subscriber is one connected client.
type Subscriber struct {
	topic uuid.UUID
	out   chan []byte
}

// Messages is the channel a connection reads to forward frames to its client.
func (s *Subscriber) Messages() <-chan []byte { return s.out }

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[uuid.UUID]map[*Subscriber]struct{})}
}

// Subscribe registers a client for a topic. The returned function unsubscribes
// and must be called when the connection ends.
func (h *Hub) Subscribe(topic uuid.UUID) (*Subscriber, func()) {
	s := &Subscriber{topic: topic, out: make(chan []byte, sendBuffer)}

	h.mu.Lock()
	if h.subs[topic] == nil {
		h.subs[topic] = make(map[*Subscriber]struct{})
	}
	h.subs[topic][s] = struct{}{}
	h.mu.Unlock()

	return s, func() { h.unsubscribe(s) }
}

func (h *Hub) unsubscribe(s *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if peers, ok := h.subs[s.topic]; ok {
		if _, present := peers[s]; present {
			delete(peers, s)
			close(s.out)
		}
		if len(peers) == 0 {
			delete(h.subs, s.topic)
		}
	}
}

// Publish fans a payload out to a topic's subscribers.
//
// A client whose buffer is full is skipped rather than blocking the publisher:
// one stalled connection must not hold up a database-backed request handler,
// and the client will pick the message up when it reconciles over REST.
func (h *Hub) Publish(topic uuid.UUID, payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		slog.Error("encoding ws payload", "error", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for s := range h.subs[topic] {
		select {
		case s.out <- encoded:
		default:
			slog.Warn("dropping a ws frame for a slow client", "topic", topic)
		}
	}
}

// SubscriberCount reports how many clients are attached to a topic.
func (h *Hub) SubscriberCount(topic uuid.UUID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[topic])
}
