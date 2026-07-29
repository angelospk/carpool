package auth

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// SMSSender delivers a one-time code to a phone number. The provider is a
// deployment decision, so it sits behind this interface.
type SMSSender interface {
	Send(ctx context.Context, phone, message string) error
}

// ConsoleSender prints the message instead of sending it. It is the DEV_MODE
// provider and must never be selected in production — main logs a warning when
// it is active.
type ConsoleSender struct{}

// Send writes the message to the log.
func (ConsoleSender) Send(_ context.Context, phone, message string) error {
	slog.Warn("DEV_MODE otp (not sent by SMS)", "phone", phone, "message", message)
	return nil
}

// SentSMS records one delivery made through FakeSMS.
type SentSMS struct {
	Phone   string
	Message string
	// Code is the six-digit code extracted from the message, for convenience in
	// tests.
	Code string
}

// FakeSMS captures messages for assertions. It is exported so tests in other
// packages can drive the auth flow end to end.
type FakeSMS struct {
	mu   sync.Mutex
	sent []SentSMS
	// Err, when set, is returned by Send.
	Err error
}

// Send records the message, or returns Err when one is configured.
func (f *FakeSMS) Send(_ context.Context, phone, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.sent = append(f.sent, SentSMS{Phone: phone, Message: message, Code: extractCode(message)})
	return nil
}

// Sent returns a copy of everything sent so far.
func (f *FakeSMS) Sent() []SentSMS {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SentSMS(nil), f.sent...)
}

// LastCode returns the code from the most recent message, or "" if none.
func (f *FakeSMS) LastCode() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return ""
	}
	return f.sent[len(f.sent)-1].Code
}

// Reset discards recorded messages.
func (f *FakeSMS) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = nil
}

// extractCode pulls the first run of otpDigits digits out of a message.
func extractCode(message string) string {
	run := make([]rune, 0, otpDigits)
	for _, r := range message {
		if r >= '0' && r <= '9' {
			run = append(run, r)
			if len(run) == otpDigits {
				return string(run)
			}
		} else {
			run = run[:0]
		}
	}
	return ""
}

// otpMessage is the SMS body. Kept plain so it survives any provider's
// transliteration rules.
func otpMessage(code string) string {
	return fmt.Sprintf("%s is your carpool verification code. It expires in %d minutes.",
		code, int(OTPTTL.Minutes()))
}
