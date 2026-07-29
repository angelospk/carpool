// Package push delivers notifications through the Expo Push service.
//
// Push is the primary channel to a backgrounded or killed app, but it is
// best-effort and can duplicate. Payloads carry ids and an idempotency key
// rather than state; the app reconciles by fetching over REST when opened.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"carpool/internal/outbox"
)

// DefaultEndpoint is Expo's push API.
const DefaultEndpoint = "https://exp.host/--/api/v2/push/send"

// ErrDeviceNotRegistered means the token is dead and should be pruned.
var ErrDeviceNotRegistered = errors.New("push: device is no longer registered")

// Notification is one message to one device.
type Notification struct {
	Token string
	Title string
	Body  string
	Data  map[string]any
}

// Sender delivers a notification. The provider sits behind this interface so
// tests never touch the network.
type Sender interface {
	Send(ctx context.Context, n Notification) error
}

// Expo is the production Sender.
type Expo struct {
	endpoint string
	http     *http.Client
}

// NewExpo returns a client posting to endpoint. Pass "" for Expo's own URL.
func NewExpo(endpoint string, httpClient *http.Client) *Expo {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	if httpClient == nil {
		// Comfortably shorter than the outbox lease, so a slow send cannot be
		// picked up by a second dispatcher while this one is still waiting.
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Expo{endpoint: endpoint, http: httpClient}
}

// expoMessage is Expo's send payload.
type expoMessage struct {
	To       string         `json:"to"`
	Title    string         `json:"title,omitempty"`
	Body     string         `json:"body,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Sound    string         `json:"sound,omitempty"`
	Priority string         `json:"priority,omitempty"`
}

// expoResponse is the ticket envelope Expo returns.
type expoResponse struct {
	Data []struct {
		Status  string `json:"status"`
		ID      string `json:"id"`
		Message string `json:"message"`
		Details struct {
			Error string `json:"error"`
		} `json:"details"`
	} `json:"data"`
}

// Send delivers one notification.
//
// Failures are classified: anything that can never succeed is joined with
// outbox.ErrPermanent so the dispatcher dead-letters it instead of spending the
// whole attempt budget on it.
func (e *Expo) Send(ctx context.Context, n Notification) error {
	if strings.TrimSpace(n.Token) == "" {
		return fmt.Errorf("%w: empty push token", outbox.ErrPermanent)
	}

	body, err := json.Marshal([]expoMessage{{
		To:    n.Token,
		Title: n.Title,
		Body:  n.Body,
		Data:  n.Data,
		Sound: "default",
		// Ask notifications are time-sensitive: the TTL is ticking while the
		// driver decides.
		Priority: "high",
	}})
	if err != nil {
		return fmt.Errorf("push: encoding message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("push: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := e.http.Do(req)
	if err != nil {
		// Network failures are transient by default.
		return fmt.Errorf("push: request failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		if res.StatusCode == http.StatusBadRequest {
			// Expo rejected the message itself; resending it will not help.
			return fmt.Errorf("%w: expo rejected the message (400): %s",
				outbox.ErrPermanent, strings.TrimSpace(string(snippet)))
		}
		return fmt.Errorf("push: expo returned %d: %s", res.StatusCode, strings.TrimSpace(string(snippet)))
	}

	var parsed expoResponse
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return fmt.Errorf("push: decoding response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return errors.New("push: expo returned no tickets")
	}

	ticket := parsed.Data[0]
	if ticket.Status == "ok" {
		return nil
	}

	switch ticket.Details.Error {
	case "DeviceNotRegistered":
		return fmt.Errorf("%w: %w: %s", outbox.ErrPermanent, ErrDeviceNotRegistered, ticket.Message)
	case "InvalidCredentials", "MessageTooBig":
		return fmt.Errorf("%w: %s: %s", outbox.ErrPermanent, ticket.Details.Error, ticket.Message)
	default:
		// MessageRateExceeded and anything unrecognised stay retryable: guessing
		// that an unknown error is fatal would silently drop notifications.
		return fmt.Errorf("push: expo ticket error %s: %s", ticket.Details.Error, ticket.Message)
	}
}

// Fake is a Sender that records instead of sending.
type Fake struct {
	mu   sync.Mutex
	sent []Notification
	// Err, when set, is returned by Send.
	Err error
}

// Send records the notification.
func (f *Fake) Send(_ context.Context, n Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.sent = append(f.sent, n)
	return nil
}

// Sent returns a copy of everything recorded.
func (f *Fake) Sent() []Notification {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Notification(nil), f.sent...)
}
