package auth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"carpool/internal/auth"
)

// The provider is not chosen yet, so the sender is configured rather than
// coded: whatever gateway wins on price gets described by a template.
func newGateway(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestTheCodeReachesTheGatewayInTheShapeItWasConfiguredWith(t *testing.T) {
	var got struct {
		Body    map[string]any
		Auth    string
		Content string
	}
	srv := newGateway(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.Body)
		got.Auth = r.Header.Get("Authorization")
		got.Content = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	})

	sender, err := auth.NewHTTPSMS(auth.HTTPSMSConfig{
		URL:      srv.URL + "/messages",
		Token:    "secret-token",
		Sender:   "CARPOOL",
		Template: `{"from":"{{sender}}","to":"{{phone}}","text":"{{message}}"}`,
	})
	if err != nil {
		t.Fatalf("NewHTTPSMS: %v", err)
	}

	if err := sender.Send(context.Background(), "+306900000001", "123456 is your code"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.Body["to"] != "+306900000001" {
		t.Errorf("gateway saw to=%v", got.Body["to"])
	}
	if got.Body["text"] != "123456 is your code" {
		t.Errorf("gateway saw text=%v", got.Body["text"])
	}
	if got.Body["from"] != "CARPOOL" {
		t.Errorf("gateway saw from=%v, want the configured sender id", got.Body["from"])
	}
	if got.Auth != "Bearer secret-token" {
		t.Errorf("Authorization = %q", got.Auth)
	}
	if got.Content != "application/json" {
		t.Errorf("Content-Type = %q", got.Content)
	}
}

// A code with a quote or a newline in it must not be able to break out of the
// JSON body and rewrite the request. The message is not user input today, but a
// template that concatenates strings is a hole waiting for the day it is.
func TestAMessageCannotBreakOutOfTheTemplate(t *testing.T) {
	var raw []byte
	srv := newGateway(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	sender, err := auth.NewHTTPSMS(auth.HTTPSMSConfig{
		URL:      srv.URL,
		Template: `{"to":"{{phone}}","text":"{{message}}"}`,
	})
	if err != nil {
		t.Fatalf("NewHTTPSMS: %v", err)
	}

	if err := sender.Send(context.Background(), "+30690", `x","admin":true,"y":"`); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("the gateway was sent something that is not JSON: %s", raw)
	}
	if _, injected := body["admin"]; injected {
		t.Fatalf("a message injected a field into the request: %s", raw)
	}
}

// A gateway that refuses has to fail loudly. A swallowed error means a user
// staring at a code screen that will never receive a code.
func TestARefusedMessageIsAnError(t *testing.T) {
	srv := newGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":"out of credit"}`))
	})

	sender, err := auth.NewHTTPSMS(auth.HTTPSMSConfig{
		URL: srv.URL, Template: `{"to":"{{phone}}","text":"{{message}}"}`,
	})
	if err != nil {
		t.Fatalf("NewHTTPSMS: %v", err)
	}

	err = sender.Send(context.Background(), "+30690", "123456")

	if err == nil {
		t.Fatal("a refused send reported success")
	}
	if !strings.Contains(err.Error(), "402") {
		t.Errorf("the error does not say what the gateway answered: %v", err)
	}
}

// The code must never travel to the logs or to an error string: an error that
// quotes the body would put a live credential in the log file.
func TestAFailureDoesNotQuoteTheCode(t *testing.T) {
	srv := newGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	sender, err := auth.NewHTTPSMS(auth.HTTPSMSConfig{
		URL: srv.URL, Template: `{"to":"{{phone}}","text":"{{message}}"}`,
	})
	if err != nil {
		t.Fatalf("NewHTTPSMS: %v", err)
	}

	err = sender.Send(context.Background(), "+306900000001", "911911 is your code")

	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "911911") {
		t.Errorf("the one-time code is in the error text: %v", err)
	}
}

func TestAMisconfiguredSenderIsRefusedAtStartup(t *testing.T) {
	cases := map[string]auth.HTTPSMSConfig{
		"no url":      {Template: `{"to":"{{phone}}"}`},
		"no template": {URL: "https://gateway.example/send"},
		"a template naming neither the phone nor the message": {
			URL: "https://gateway.example/send", Template: `{"hello":"world"}`,
		},
		"plaintext http to a public gateway": {
			URL: "http://gateway.example/send", Template: `{"to":"{{phone}}","text":"{{message}}"}`,
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := auth.NewHTTPSMS(cfg); err == nil {
				t.Fatal("the sender was accepted; it would fail on the first real sign-in instead")
			}
		})
	}
}
