package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
	"carpool/internal/geo"
	"carpool/internal/groups"
	"carpool/internal/invites"
	"carpool/internal/live"
	"carpool/internal/moderation"
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
	devMode  bool
}

func newHarness(t *testing.T) *harness { return newHarnessWith(t, false) }

// newDevHarness builds the router as `DEV_MODE=true` does, which relaxes two
// things that must stay strict elsewhere: the browser may call across origins,
// and the OTP comes back in the response.
func newDevHarness(t *testing.T) *harness { return newHarnessWith(t, true) }

func newHarnessWith(t *testing.T, devMode bool) *harness {
	t.Helper()
	ctx := context.Background()

	pool := testdb.Fresh(t)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var regionID uuid.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO regions (name, boundary)
		VALUES ('Έβρος',
		        ST_Multi(ST_GeomFromText('POLYGON((25.3 40.3, 26.7 40.3, 26.7 41.8, 25.3 41.8, 25.3 40.3))', 4326)))
		RETURNING id`).Scan(&regionID)
	if err != nil {
		t.Fatalf("seeding region: %v", err)
	}
	// Without these nothing can suggest a contribution to anybody, which is
	// exactly what production would look like if a deployment forgot them.
	if _, err := pool.Exec(ctx, `
		INSERT INTO fuel_prices (region_id, fuel, price_eur, source)
		VALUES (NULL, 'petrol', 1.859, 'test'), (NULL, 'diesel', 1.729, 'test')`); err != nil {
		t.Fatalf("seeding fuel prices: %v", err)
	}

	c := clock.NewFake(time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC))
	sms := &auth.FakeSMS{}
	issuer := auth.NewTokenIssuer(testSecret, c)
	sessions := auth.NewSessionService(pool, c, issuer)

	router := api.NewRouter(api.Deps{
		DevMode:     devMode,
		Ready:       pool.Ping,
		Pool:        pool,
		Clock:       c,
		Location:    testLocation(),
		Issuer:      issuer,
		OTP:         auth.NewOTPService(pool, c, sms),
		Signup:      auth.NewSignupService(pool, c),
		Sessions:    sessions,
		PhoneChange: auth.NewPhoneChangeService(pool, c, sms, sessions, []byte(testSecret)),
		Asks:        asks.NewService(pool, c, nil),
		Standing:    standing.NewService(pool, c),
		Bookings:    bookings.NewService(pool, c),
		Invites:     invites.NewService(pool, c, false),
		Groups:      groups.NewService(pool, c),
		Live:        live.NewService(pool, c),
		Account:     account.NewService(pool, c),
		Drivers:     drivers.NewService(pool, c),
		Moderation:  moderation.NewService(pool, c),
	})

	return &harness{t: t, router: router, pool: pool, clock: c, sms: sms, regionID: regionID, devMode: devMode}
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

	// 200 with the code in the body on a DEV_MODE harness, 204 otherwise, and
	// exactly one of the two. Accepting either regardless of mode would let the
	// rule that keeps the code out of production responses break unnoticed.
	want := http.StatusNoContent
	if h.devMode {
		want = http.StatusOK
	}
	rec := h.do(http.MethodPost, "/v1/auth/otp/request", map[string]any{"phone": phone}, "")
	if rec.Code != want {
		h.t.Fatalf("otp/request: status %d, want %d, body %s", rec.Code, want, rec.Body.String())
	}

	rec = h.do(http.MethodPost, "/v1/auth/otp/verify", map[string]any{
		"phone": phone,
		"code":  h.sms.LastCode(),
	}, "")
	if rec.Code != http.StatusOK {
		h.t.Fatalf("otp/verify: status %d, body %s", rec.Code, rec.Body.String())
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		SignupToken  string `json:"signup_token"`
	}
	h.decode(rec, &body)

	// A number that is already an account is signed in and done. A new one is
	// handed a ticket and has to say who it is.
	if body.SignupToken != "" {
		rec = h.do(http.MethodPost, "/v1/auth/signup/complete", map[string]any{
			"signup_token":  body.SignupToken,
			"name":          name,
			"home_place_id": feres,
			// Old enough, and fixed rather than computed: every test that is not
			// about age wants an ordinary adult, and a date derived from the
			// clock would make the age gate untestable from here.
			"date_of_birth": "1990-01-01",
		}, "")
		if rec.Code != http.StatusOK {
			h.t.Fatalf("signup/complete: status %d, body %s", rec.Code, rec.Body.String())
		}
		h.decode(rec, &body)
	}
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
		case *string, *int, *bool, *uuid.UUID, *float64, *time.Time:
			scan = append(scan, d)
		default:
			args = append(args, d)
		}
	}
	if err := h.pool.QueryRow(context.Background(), sql, args...).Scan(scan...); err != nil {
		h.t.Fatalf("query %q: %v", sql, err)
	}
}

// OSRMEnv names the environment variable holding a routing service for the
// handful of tests that cannot be written without one.
const OSRMEnv = "CARPOOL_TEST_OSRM"

// newRoutingHarness is the harness with a real router wired in.
//
// A real one rather than a fake because Deps.Router is a concrete *geo.OSRM,
// and the alternative — turning it into an interface so a test can substitute a
// stub — would be changing production code to suit a test. The same trade the
// database harness already makes.
//
// Skipped when no service is configured, so a unit-only run stays fast.
func newRoutingHarness(t *testing.T) *harness {
	t.Helper()

	base := os.Getenv(OSRMEnv)
	if base == "" {
		t.Skipf("%s is not set; skipping the routing test", OSRMEnv)
	}

	return newHarnessRoutedBy(t, base)
}

// newHarnessRoutedBy is newRoutingHarness with the routing service named
// outright, so a test that needs one specific answer out of OSRM can stand up
// an httptest server and hand over its URL.
//
// Deps.Router stays a concrete *geo.OSRM either way. The stub speaks HTTP, not
// Go, so nothing in production has to become an interface for a test's benefit
// — and what is exercised is the real decoder, the real ferry check and the
// real WKT, which a Go-level fake would have replaced.
func newHarnessRoutedBy(t *testing.T, base string) *harness {
	t.Helper()

	h := newHarness(t)
	h.router = api.NewRouter(api.Deps{
		Ready: h.pool.Ping, Pool: h.pool, Clock: h.clock, Location: testLocation(),
		Issuer:   auth.NewTokenIssuer(testSecret, h.clock),
		OTP:      auth.NewOTPService(h.pool, h.clock, h.sms),
		Signup:   auth.NewSignupService(h.pool, h.clock),
		Sessions: auth.NewSessionService(h.pool, h.clock, auth.NewTokenIssuer(testSecret, h.clock)),
		Invites:  invites.NewService(h.pool, h.clock, false),
		Groups:   groups.NewService(h.pool, h.clock),
		Drivers:  drivers.NewService(h.pool, h.clock),
		Router:   geo.NewOSRM(base, nil),
	})
	return h
}

// countRows counts every table an estimate might touch if it wrote anything,
// which is the point: it should touch none of them.
func (h *harness) countRows() map[string]int {
	h.t.Helper()
	out := map[string]int{}
	for _, table := range []string{"ride_requests", "trip_occurrences", "asks", "bookings", "route_templates"} {
		var n int
		if err := h.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM `+table).Scan(&n); err != nil {
			h.t.Fatalf("counting %s: %v", table, err)
		}
		out[table] = n
	}
	return out
}

// testLocation is the same civil timezone the service runs in. Tests that reason
// about days must not quietly use UTC, because that is exactly the bug the
// Location field exists to prevent.
func testLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Athens")
	if err != nil {
		// A container without tzdata. UTC gets the arithmetic right for every
		// case except the couple of hours either side of midnight, which is
		// better than refusing to run the suite at all.
		return time.UTC
	}
	return loc
}
