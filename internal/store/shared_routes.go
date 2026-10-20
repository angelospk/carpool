package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoSuchRoute means the route does not exist or is not the caller's.
var ErrNoSuchRoute = errors.New("store: no such route")

// ErrRouteRetired means the link names a route its driver stopped offering.
var ErrRouteRetired = errors.New("store: that route is no longer offered")

// SharedRoute is what a route link shows to anybody holding it: what the
// driver's own share sentence already says, and nothing more. No driver name,
// no coordinates, no price — the link is pasted into chats with people the
// driver has not met.
type SharedRoute struct {
	OriginLabel      string
	DestinationLabel string
	Kind             string
	Weekdays         []int
	OneOffDate       *string
	DepartEarliest   string
	DepartLatest     string
	Seats            int
	// ForGroup is a route offered to one group only. The group is not named:
	// that is for its members to know.
	ForGroup bool
	// DriverID is the route's owner, so the page can offer their invite code.
	// It stays out of sharedRouteJSON: the link must not name the driver.
	DriverID uuid.UUID
}

// ShareToken returns the route's link token, creating one the first time.
// Only the driver who published the route gets one.
func ShareToken(ctx context.Context, pool *pgxpool.Pool, routeID, driverID uuid.UUID) (string, error) {
	fresh, err := newShareToken()
	if err != nil {
		return "", err
	}
	var token string
	err = pool.QueryRow(ctx, `
		UPDATE route_templates SET share_token = coalesce(share_token, $3)
		WHERE id = $1 AND driver_id = $2 AND active
		RETURNING share_token`, routeID, driverID, fresh).Scan(&token)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSuchRoute
	}
	if err != nil {
		return "", fmt.Errorf("store: sharing the route: %w", err)
	}
	return token, nil
}

// RevokeShareToken withdraws the route's link. The next ShareToken makes a
// new one.
func RevokeShareToken(ctx context.Context, pool *pgxpool.Pool, routeID, driverID uuid.UUID) error {
	tag, err := pool.Exec(ctx, `
		UPDATE route_templates SET share_token = NULL WHERE id = $1 AND driver_id = $2`, routeID, driverID)
	if err != nil {
		return fmt.Errorf("store: withdrawing the link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoSuchRoute
	}
	return nil
}

// SharedRouteByToken reads the route a link names.
//
// A route that can no longer run — retired, its driver gone, a one-off whose
// day has passed, a recurring schedule past its end — answers ErrRouteRetired.
// now decides the last two, read as an Athens date like the schedule's own.
func SharedRouteByToken(ctx context.Context, pool *pgxpool.Pool, token string, now time.Time) (SharedRoute, error) {
	var (
		r      SharedRoute
		active bool
		status string
		ended  bool
	)
	err := pool.QueryRow(ctx, `
		SELECT t.origin_label, t.destination_label, s.kind,
		       coalesce(s.weekdays, '{}'), to_char(s.oneoff_date, 'YYYY-MM-DD'),
		       to_char(s.depart_earliest, 'HH24:MI'), to_char(s.depart_latest, 'HH24:MI'),
		       t.seats, t.driver_id, t.group_id IS NOT NULL, t.active, u.status,
		       (s.kind = 'oneoff' AND s.oneoff_date < ($2 AT TIME ZONE 'Europe/Athens')::date)
		    OR (s.effective_until IS NOT NULL AND s.effective_until < ($2 AT TIME ZONE 'Europe/Athens')::date)
		FROM route_templates t
		JOIN route_schedules s ON s.template_id = t.id
		JOIN users u ON u.id = t.driver_id
		WHERE t.share_token = $1
		ORDER BY s.created_at DESC LIMIT 1`, token, now).
		Scan(&r.OriginLabel, &r.DestinationLabel, &r.Kind, &r.Weekdays, &r.OneOffDate,
			&r.DepartEarliest, &r.DepartLatest, &r.Seats, &r.DriverID, &r.ForGroup, &active, &status, &ended)
	if errors.Is(err, pgx.ErrNoRows) {
		return SharedRoute{}, ErrNoSuchRoute
	}
	if err != nil {
		return SharedRoute{}, fmt.Errorf("store: reading the shared route: %w", err)
	}
	// A suspended or deleted driver's route is not on offer either, and saying
	// "retired" rather than "gone" says nothing about why.
	if !active || status != "active" || ended {
		return SharedRoute{}, ErrRouteRetired
	}
	return r, nil
}

func newShareToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("store: generating a link: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
