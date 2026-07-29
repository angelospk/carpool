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
	ID           uuid.UUID
	Phone        string
	Name         string
	PhotoURL     *string
	Gender       *string
	HomeRegionID uuid.UUID
	WomenOnly    bool
	Status       string
}

// Region is a service area.
type Region struct {
	ID           uuid.UUID
	Name         string
	FuelPriceEUR float64
}

// EnsureUser returns the account for a verified phone number, creating it on
// first sign-in.
//
// Name and region are only applied at creation: a later sign-in must not let
// whatever the client happens to send overwrite the stored profile.
func EnsureUser(ctx context.Context, pool *pgxpool.Pool, phone, name string, regionID uuid.UUID) (User, error) {
	var u User
	err := pool.QueryRow(ctx, `
		INSERT INTO users (phone, name, home_region_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (phone) DO UPDATE SET updated_at = now()
		RETURNING id, phone, name, photo_url, gender, home_region_id, women_only, status`,
		phone, name, regionID).Scan(
		&u.ID, &u.Phone, &u.Name, &u.PhotoURL, &u.Gender, &u.HomeRegionID, &u.WomenOnly, &u.Status)
	if err != nil {
		return User{}, fmt.Errorf("store: ensuring user: %w", err)
	}
	return u, nil
}

// GetUser loads an account by id.
func GetUser(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (User, error) {
	var u User
	err := pool.QueryRow(ctx, `
		SELECT id, phone, name, photo_url, gender, home_region_id, women_only, status
		FROM users WHERE id = $1`, id).Scan(
		&u.ID, &u.Phone, &u.Name, &u.PhotoURL, &u.Gender, &u.HomeRegionID, &u.WomenOnly, &u.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: loading user: %w", err)
	}
	return u, nil
}

// UserUpdate carries the fields a user may change on their own profile. A nil
// field is left untouched.
type UserUpdate struct {
	Name      *string
	PhotoURL  *string
	Gender    *string
	WomenOnly *bool
	RegionID  *uuid.UUID
}

// UpdateUser applies a partial profile update and returns the new state.
func UpdateUser(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, up UserUpdate) (User, error) {
	var u User
	err := pool.QueryRow(ctx, `
		UPDATE users SET
			name           = COALESCE($2, name),
			photo_url      = COALESCE($3, photo_url),
			gender         = COALESCE($4, gender),
			women_only     = COALESCE($5, women_only),
			home_region_id = COALESCE($6, home_region_id),
			updated_at     = now()
		WHERE id = $1
		RETURNING id, phone, name, photo_url, gender, home_region_id, women_only, status`,
		id, up.Name, up.PhotoURL, up.Gender, up.WomenOnly, up.RegionID).Scan(
		&u.ID, &u.Phone, &u.Name, &u.PhotoURL, &u.Gender, &u.HomeRegionID, &u.WomenOnly, &u.Status)
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
		`SELECT id, name, fuel_price_eur FROM regions ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: listing regions: %w", err)
	}
	defer rows.Close()

	var out []Region
	for rows.Next() {
		var r Region
		if err := rows.Scan(&r.ID, &r.Name, &r.FuelPriceEUR); err != nil {
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
