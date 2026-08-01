package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPSMSConfig describes an SMS gateway well enough to talk to it.
//
// The provider is not chosen yet — the Greek gateways quote rather than
// publish, and the decision belongs to whoever compares those quotes. So the
// request is configuration rather than code: almost every gateway takes a JSON
// POST with a bearer token, and the ones that differ, differ only in field
// names.
type HTTPSMSConfig struct {
	// URL is the gateway's send endpoint.
	URL string
	// Token is sent as a bearer credential when set.
	Token string
	// Sender is the alphanumeric sender id, where the gateway supports one.
	Sender string
	// Template is the JSON request body, with {{phone}}, {{message}} and
	// {{sender}} substituted. Values are JSON-encoded on the way in, so a
	// message cannot rewrite the request around it.
	Template string
	// Timeout bounds one attempt. Zero means ten seconds.
	Timeout time.Duration
}

// HTTPSMS delivers codes through a JSON gateway.
type HTTPSMS struct {
	cfg  HTTPSMSConfig
	http *http.Client
}

// NewHTTPSMS validates the configuration and returns a sender.
//
// It refuses at startup rather than at the first sign-in: a gateway that is
// wrong in the environment file fails when somebody is standing at a code
// screen, and by then nobody is reading logs.
func NewHTTPSMS(cfg HTTPSMSConfig) (*HTTPSMS, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("auth: the sms gateway needs a url")
	}
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("auth: the sms gateway url is not a url: %w", err)
	}
	// A one-time code in cleartext across the internet is the whole credential.
	// Loopback stays allowed so a local stub can be pointed at.
	if u.Scheme != "https" && !isLoopback(u.Hostname()) {
		return nil, fmt.Errorf("auth: the sms gateway must be https, got %q", u.Scheme)
	}
	if cfg.Template == "" {
		return nil, fmt.Errorf("auth: the sms gateway needs a body template")
	}
	if !strings.Contains(cfg.Template, "{{phone}}") || !strings.Contains(cfg.Template, "{{message}}") {
		return nil, fmt.Errorf("auth: the body template names neither the phone nor the message")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &HTTPSMS{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}, nil
}

// Send posts one message to the gateway.
func (s *HTTPSMS) Send(ctx context.Context, phone, message string) error {
	body := s.cfg.Template
	for placeholder, value := range map[string]string{
		"{{phone}}":   phone,
		"{{sender}}":  s.cfg.Sender,
		"{{message}}": message,
	} {
		// Encoded, then stripped of its surrounding quotes, so the value lands
		// inside the template's own quotes already escaped. Pasting it raw would
		// let a quote in the text close the field and add another.
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("auth: encoding an sms field: %w", err)
		}
		body = strings.ReplaceAll(body, placeholder, string(encoded[1:len(encoded)-1]))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.URL, bytes.NewReader([]byte(body)))
	if err != nil {
		return fmt.Errorf("auth: building the sms request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if s.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	}

	res, err := s.http.Do(req)
	if err != nil {
		// Deliberately without the body: it carries the live code.
		return fmt.Errorf("auth: the sms gateway could not be reached: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		// The gateway's answer is quoted, the request is not — the response says
		// why it failed, and the code was in what we sent.
		snippet, _ := io.ReadAll(io.LimitReader(res.Body, 256))
		return fmt.Errorf("auth: the sms gateway returned %d: %s",
			res.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
