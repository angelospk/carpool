package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/account"
	"carpool/internal/api"
	"carpool/internal/asks"
	"carpool/internal/auth"
	"carpool/internal/bookings"
	"carpool/internal/clock"
	"carpool/internal/drivers"
	"carpool/internal/invites"
	"carpool/internal/standing"
	"carpool/internal/store"
	"carpool/internal/store/testdb"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// harness is a full API stack wired to a real database.
type harness struct {
	t        *testing.T
	router   http.Handler
	pool     *pgxpool.Pool
	clock    *clock.Fake
	sms      *auth.FakeSMS
	regionID uuid.UUID
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()

	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var regionID uuid.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary, fuel_price_eur)
		VALUES ('Έβρος',
		        ST_Multi(ST_GeomFromText('POLYGON((25.3 40.3, 26.7 40.3, 26.7 41.8, 25.3 41.8, 25.3 40.3))', 4326)),
		        1.800)
		RETURNING id`).Scan(&regionID)
	if err != nil {
		t.Fatalf("seeding region: %v", err)
	}

	c := clock.NewFake(time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC))
	sms := &auth.FakeSMS{}
	issuer := auth.NewTokenIssuer(testSecret, c)

	router := api.NewRouter(api.Deps{
		Ready:    pool.Ping,
		Pool:     pool,
		Clock:    c,
		Issuer:   issuer,
		OTP:      auth.NewOTPService(pool, c, sms),
		Sessions: auth.NewSessionService(pool, c, issuer),
		Asks:     asks.NewService(pool, c, nil),
		Standing: standing.NewService(pool, c),
		Bookings: bookings.NewService(pool, c),
		Invites:  invites.NewService(pool, c, false),
		Account:  account.NewService(pool, c),
		Drivers:  drivers.NewService(pool, c),
	})

	return &harness{t: t, router: router, pool: pool, clock: c, sms: sms, regionID: regionID}
}

// do issues a request against the router. token may be empty.
func (h *harness) do(method, path string, body any, token string) *httptest.ResponseRecorder {
	h.t.Helper()

	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshalling request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

// decode unmarshals a response body, failing the test if it is not JSON.
func (h *harness) decode(rec *httptest.ResponseRecorder, into any) {
	h.t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		h.t.Fatalf("response is not JSON: %v (body %q)", err, rec.Body.String())
	}
}

// errorCode extracts the machine-readable code from an error envelope.
func (h *harness) errorCode(rec *httptest.ResponseRecorder) string {
	h.t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	h.decode(rec, &body)
	return body.Error.Code
}

// signUp runs the whole OTP flow and returns a usable token pair.
func (h *harness) signUp(phone, name string) (accessToken, refreshToken string) {
	h.t.Helper()

	rec := h.do(http.MethodPost, "/v1/auth/otp/request", map[string]any{"phone": phone}, "")
	if rec.Code != http.StatusNoContent {
		h.t.Fatalf("otp/request: status %d, body %s", rec.Code, rec.Body.String())
	}

	rec = h.do(http.MethodPost, "/v1/auth/otp/verify", map[string]any{
		"phone":     phone,
		"code":      h.sms.LastCode(),
		"name":      name,
		"region_id": h.regionID.String(),
	}, "")
	if rec.Code != http.StatusOK {
		h.t.Fatalf("otp/verify: status %d, body %s", rec.Code, rec.Body.String())
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	h.decode(rec, &body)
	return body.AccessToken, body.RefreshToken
}

// mustExec runs a statement, failing the test on error.
func (h *harness) mustExec(sql string, args ...any) {
	h.t.Helper()
	if _, err := h.pool.Exec(context.Background(), sql, args...); err != nil {
		h.t.Fatalf("exec %q: %v", sql, err)
	}
}

// mustQuery scans a single row into dest. Any trailing args are query
// parameters, so callers can write mustQuery(sql, &out, param).
func (h *harness) mustQuery(sql string, dest ...any) {
	h.t.Helper()

	// Split dest into scan targets and query args: a target is a pointer we
	// created in the test, an arg is a plain value.
	var scan []any
	var args []any
	for _, d := range dest {
		switch d.(type) {
		case *string, *int, *bool, *uuid.UUID, *float64:
			scan = append(scan, d)
		default:
			args = append(args, d)
		}
	}
	if err := h.pool.QueryRow(context.Background(), sql, args...).Scan(scan...); err != nil {
		h.t.Fatalf("query %q: %v", sql, err)
	}
}
