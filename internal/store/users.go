package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/domain"
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
	// Bio is the two lines somebody wrote about themselves. Rendered as plain
	// text and never as markup, which is why nothing here strips anything.
	Bio *string
	// Preferences is what their rides are like, from a closed vocabulary. It
	// describes; it does not enforce.
	Preferences []string
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
func EnsureUser(ctx context.Context, pool *pgxpool.Pool, phone, name string, regionID uuid.UUID, born domain.Date) (User, error) {
	return EnsureUserTx(ctx, pool, phone, name, regionID, born)
}

// querier is the part of pgx both a pool and a transaction satisfy, so a caller
// that has to create the account alongside other work can pass its transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// EnsureUserTx is EnsureUser inside the caller's transaction.
//
// The birth date is written on the way in and never on the way back. Somebody
// signing in again is not re-stating how old they are, and an upsert that let
// them would be an age gate anybody could walk around by signing in twice.
func EnsureUserTx(ctx context.Context, q querier, phone, name string, regionID uuid.UUID, born domain.Date) (User, error) {
	var u User
	err := q.QueryRow(ctx, `
		INSERT INTO users (phone, name, home_region_id, date_of_birth)
		VALUES ($1, $2, $3, make_date($4, $5, $6))
		ON CONFLICT (phone) DO UPDATE SET updated_at = now()
		RETURNING id, phone, name, photo_url,
		          (SELECT encode(substring(sha256 from 1 for 6), 'hex') FROM profile_photos WHERE user_id = users.id),
		          gender, home_region_id, women_only, status,
		          facebook_handle, instagram_handle`,
		phone, name, regionID, born.Year, int(born.Month), born.Day).Scan(
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
		       facebook_handle, instagram_handle, bio
		FROM users WHERE id = $1`, id).Scan(
		&u.ID, &u.Phone, &u.Name, &u.PhotoURL, &u.PhotoVersion, &u.Gender, &u.HomeRegionID, &u.WomenOnly, &u.Status,
		&u.FacebookHandle, &u.InstagramHandle, &u.Bio)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: loading user: %w", err)
	}
	if u.Preferences, err = preferencesOf(ctx, pool, id); err != nil {
		return User{}, err
	}
	return u, nil
}

// preferencesOf reads somebody's chosen tags, in the vocabulary's own order so
// that two profiles with the same preferences read the same way round.
func preferencesOf(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT tag FROM user_preferences WHERE user_id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("store: loading preferences: %w", err)
	}
	defer rows.Close()

	chosen := map[string]bool{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("store: scanning a preference: %w", err)
		}
		chosen[tag] = true
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("store: loading preferences: %w", rows.Err())
	}

	var out []string
	for _, tag := range domain.PreferenceTags() {
		if chosen[tag] {
			out = append(out, tag)
		}
	}
	return out, nil
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
		       facebook_handle, instagram_handle, bio
		FROM users WHERE phone = $1`, phone).Scan(
		&u.ID, &u.Phone, &u.Name, &u.PhotoURL, &u.PhotoVersion, &u.Gender, &u.HomeRegionID, &u.WomenOnly, &u.Status,
		&u.FacebookHandle, &u.InstagramHandle, &u.Bio)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: loading user by phone: %w", err)
	}
	// Preferences are deliberately not loaded, unlike GetUser. The one caller is
	// signing in, which needs an id and a status and returns tokens — so the
	// extra query would be on the hot path for a field nothing reads. If that
	// stops being true, load them here rather than surprising somebody with an
	// empty list.
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
	// Bio follows the handles' rule: nil leaves it, an empty string clears it.
	Bio *string
	// Preferences is a replacement, not an addition. nil leaves them alone; a
	// non-nil slice is the whole new set, because "untick the one I ticked by
	// mistake" has to be expressible and a merge cannot express it.
	Preferences *[]string
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
			bio              = CASE WHEN $8::text IS NULL THEN bio              ELSE NULLIF($8, '') END,
			updated_at       = now()
		WHERE id = $1
		RETURNING id, phone, name, photo_url,
		          (SELECT encode(substring(sha256 from 1 for 6), 'hex') FROM profile_photos WHERE user_id = users.id),
		          gender, home_region_id, women_only, status,
		          facebook_handle, instagram_handle, bio`,
		id, up.Name, up.Gender, up.WomenOnly, up.RegionID, up.FacebookHandle, up.InstagramHandle, up.Bio).Scan(
		&u.ID, &u.Phone, &u.Name, &u.PhotoURL, &u.PhotoVersion, &u.Gender, &u.HomeRegionID, &u.WomenOnly, &u.Status,
		&u.FacebookHandle, &u.InstagramHandle, &u.Bio)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: updating user: %w", err)
	}

	// Replaced wholesale rather than merged, in one statement pair, so that a
	// half-applied set is not a state anybody can observe.
	if up.Preferences != nil {
		if err := replacePreferences(ctx, pool, id, *up.Preferences); err != nil {
			return User{}, err
		}
	}
	if u.Preferences, err = preferencesOf(ctx, pool, id); err != nil {
		return User{}, err
	}
	return u, nil
}

func replacePreferences(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, tags []string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM user_preferences WHERE user_id = $1`, id); err != nil {
		return fmt.Errorf("store: clearing preferences: %w", err)
	}
	for _, tag := range tags {
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_preferences (user_id, tag) VALUES ($1, $2)`, id, tag); err != nil {
			return fmt.Errorf("store: saving preference %q: %w", tag, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
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
