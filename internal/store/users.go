package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("store: not found")

// User is an account.
type User struct {
	ID       uuid.UUID
	Phone    string
	Name     string
	PhotoURL *string
	// PhotoVersion is a prefix of the stored photo's hash, present only when
	// there is one. The API composes the URL from it, so a replaced photo lands
	// at a new address and no cache goes on serving the old one.
	PhotoVersion    *string
	Gender          *string
	HomeRegionID    uuid.UUID
	WomenOnly       bool
	Status          string
	FacebookHandle  *string
	InstagramHandle *string
}

// Region is a περιφερειακή ενότητα: the geography a fuel price is quoted for
// and the one a person's home town resolves to.
//
// It carries no price. One number per region could not tell petrol from diesel,
// and would have meant seventy-five copies of a national figure wearing local
// badges. See fuel_prices.go.
type Region struct {
	ID   uuid.UUID
	Name string
}

// EnsureUser returns the account for a verified phone number, creating it on
// first sign-in.
//
// Name and region are only applied at creation: a later sign-in must not let
// whatever the client happens to send overwrite the stored profile.
func EnsureUser(ctx context.Context, pool *pgxpool.Pool, phone, name string, regionID uuid.UUID) (User, error) {
	return EnsureUserTx(ctx, pool, phone, name, regionID)
}

// querier is the part of pgx both a pool and a transaction satisfy, so a caller
// that has to create the account alongside other work can pass its transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// EnsureUserTx is EnsureUser inside the caller's transaction.
func EnsureUserTx(ctx context.Context, q querier, phone, name string, regionID uuid.UUID) (User, error) {
	var u User
	err := q.QueryRow(ctx, `
		INSERT INTO users (phone, name, home_region_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (phone) DO UPDATE SET updated_at = now()
		RETURNING id, phone, name, photo_url,
		          (SELECT encode(substring(sha256 from 1 for 6), 'hex') FROM profile_photos WHERE user_id = users.id),
		          gender, home_region_id, women_only, status,
		          facebook_handle, instagram_handle`,
		phone, name, regionID).Scan(
		&u.ID, &u.Phone, &u.Name, &u.PhotoURL, &u.PhotoVersion, &u.Gender, &u.HomeRegionID, &u.WomenOnly, &u.Status,
		&u.FacebookHandle, &u.InstagramHandle)
	if err != nil {
		return User{}, fmt.Errorf("store: ensuring user: %w", err)
	}
	return u, nil
}

// GetUser loads an account by id.
func GetUser(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (User, error) {
	var u User
	err := pool.QueryRow(ctx, `
		SELECT id, phone, name, photo_url,
		       (SELECT encode(substring(sha256 from 1 for 6), 'hex') FROM profile_photos WHERE user_id = users.id),
		       gender, home_region_id, women_only, status,
		       facebook_handle, instagram_handle
		FROM users WHERE id = $1`, id).Scan(
		&u.ID, &u.Phone, &u.Name, &u.PhotoURL, &u.PhotoVersion, &u.Gender, &u.HomeRegionID, &u.WomenOnly, &u.Status,
		&u.FacebookHandle, &u.InstagramHandle)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: loading user: %w", err)
	}
	return u, nil
}

// UserByPhone loads the account for a verified number, or ErrNotFound.
//
// Signing in asks this before anything else: a number that is already an
// account gets a session, and one that is not gets sent to finish joining.
// Nothing outside a verified OTP flow may call it — the answer is whether a
// given number is a member, which is not a question a stranger gets to ask.
func UserByPhone(ctx context.Context, pool *pgxpool.Pool, phone string) (User, error) {
	var u User
	err := pool.QueryRow(ctx, `
		SELECT id, phone, name, photo_url,
		       (SELECT encode(substring(sha256 from 1 for 6), 'hex') FROM profile_photos WHERE user_id = users.id),
		       gender, home_region_id, women_only, status,
		       facebook_handle, instagram_handle
		FROM users WHERE phone = $1`, phone).Scan(
		&u.ID, &u.Phone, &u.Name, &u.PhotoURL, &u.PhotoVersion, &u.Gender, &u.HomeRegionID, &u.WomenOnly, &u.Status,
		&u.FacebookHandle, &u.InstagramHandle)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: loading user by phone: %w", err)
	}
	return u, nil
}

// UserUpdate carries the fields a user may change on their own profile. A nil
// field is left untouched.
type UserUpdate struct {
	Name *string
	// The handles follow the same nil-means-untouched rule as the rest, with
	// one addition: an empty string clears one. A link you can add and never
	// remove is not a link you would add.
	FacebookHandle  *string
	InstagramHandle *string
	Gender          *string
	WomenOnly       *bool
	RegionID        *uuid.UUID
}

// UpdateUser applies a partial profile update and returns the new state.
func UpdateUser(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, up UserUpdate) (User, error) {
	var u User
	err := pool.QueryRow(ctx, `
		UPDATE users SET
			name             = COALESCE($2, name),
			gender           = COALESCE($3, gender),
			women_only       = COALESCE($4, women_only),
			home_region_id   = COALESCE($5, home_region_id),
			facebook_handle  = CASE WHEN $6::text IS NULL THEN facebook_handle  ELSE NULLIF($6, '') END,
			instagram_handle = CASE WHEN $7::text IS NULL THEN instagram_handle ELSE NULLIF($7, '') END,
			updated_at       = now()
		WHERE id = $1
		RETURNING id, phone, name, photo_url,
		          (SELECT encode(substring(sha256 from 1 for 6), 'hex') FROM profile_photos WHERE user_id = users.id),
		          gender, home_region_id, women_only, status,
		          facebook_handle, instagram_handle`,
		id, up.Name, up.Gender, up.WomenOnly, up.RegionID, up.FacebookHandle, up.InstagramHandle).Scan(
		&u.ID, &u.Phone, &u.Name, &u.PhotoURL, &u.PhotoVersion, &u.Gender, &u.HomeRegionID, &u.WomenOnly, &u.Status,
		&u.FacebookHandle, &u.InstagramHandle)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: updating user: %w", err)
	}
	return u, nil
}

// ListRegions returns every region, alphabetically.
func ListRegions(ctx context.Context, pool *pgxpool.Pool) ([]Region, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, name FROM regions ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: listing regions: %w", err)
	}
	defer rows.Close()

	var out []Region
	for rows.Next() {
		var r Region
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return nil, fmt.Errorf("store: scanning region: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// isUniqueViolation reports whether err is a Postgres unique-constraint error,
// which is how the store distinguishes "already exists" from a real failure.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
