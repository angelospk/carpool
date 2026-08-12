// Package account carries out the two requests a person can make about their
// own data: give me a copy, and erase me.
//
// Erasure here means anonymisation rather than DELETE, and the reason is that a
// carpool account is not only its owner's data. A rating is somebody else's
// judgement, a message is half of somebody else's conversation, and a report is
// the record that protected a third person. Cascading a DELETE through users
// would rewrite all of it, and the reports table refuses outright: its foreign
// keys carry no ON DELETE at all, so a hard delete fails rather than quietly
// destroying evidence.
//
// So the person becomes unidentifiable and everything that was about other
// people stays standing.
package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/outbox"
)

// ErrNotFound means there is no such account.
var ErrNotFound = errors.New("account: no such account")

// Service exports and erases accounts.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewService returns a service reading time from c.
func NewService(pool *pgxpool.Pool, c clock.Clock) *Service {
	return &Service{pool: pool, clock: c}
}

// Delete erases the person and leaves the record.
//
// It is idempotent: a second call on an account already erased changes nothing
// and reports success, because the answer to "erase me" is the same either way.
func (s *Service) Delete(ctx context.Context, userID uuid.UUID) error {
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("account: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx,
		`SELECT status FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("account: loading the account: %w", err)
	}
	if status == "deleted" {
		return nil
	}

	// Seats first, while the account can still be identified. A deleted person
	// holding next Tuesday's seat is a seat nobody can use and a driver who
	// stops for nobody.
	rows, err := tx.Query(ctx, `
		WITH released AS (
			UPDATE bookings b
			SET status = 'cancelled_by_passenger', updated_at = now()
			FROM trip_occurrences o
			WHERE b.passenger_id = $1
			  AND o.id = b.occurrence_id
			  AND b.status = 'confirmed'
			  AND o.depart_earliest > $2
			RETURNING b.occurrence_id, b.seats
		)
		UPDATE trip_occurrences o
		SET seats_free = o.seats_free + r.seats, updated_at = now()
		FROM released r WHERE o.id = r.occurrence_id
		RETURNING o.id`, userID, now)
	if err != nil {
		return fmt.Errorf("account: releasing seats: %w", err)
	}
	var freed []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("account: reading a released seat: %w", err)
		}
		freed = append(freed, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("account: releasing seats: %w", err)
	}

	// Somebody leaving gives their seats back, and those trips may be exactly
	// what another passenger has been waiting on.
	if err := outbox.WakeAll(ctx, tx, freed, "seat_returned", now); err != nil {
		return err
	}

	// Stop offering rides that nobody will now turn up to drive, and withdraw
	// anything still waiting for an answer.
	//
	// Every statement takes the same two parameters whether it needs both or
	// not, because a list where the placeholders differ per line is a list that
	// breaks the first time somebody adds to it.
	windDown := []string{
		`UPDATE route_templates SET active = false, updated_at = now()
		 WHERE driver_id = $1 AND $2::timestamptz IS NOT NULL`,
		`UPDATE ride_requests SET status = 'cancelled', updated_at = now()
		 WHERE passenger_id = $1 AND status IN ('open','asking') AND $2::timestamptz IS NOT NULL`,
		`UPDATE standing_arrangements SET status = 'cancelled', updated_at = now()
		 WHERE passenger_id = $1 AND status IN ('pending','active') AND $2::timestamptz IS NOT NULL`,
		// Offers still sitting with a driver, and offers sitting with this
		// person about somebody else. Cancelling the request above is not enough
		// on its own: an ask is what a driver's phone is showing, it holds a seat
		// against the trip's admission count until its half hour is up, and a
		// tap on it is how a seat gets given to somebody who has left.
		`UPDATE asks a SET status = 'cancelled', responded_at = $2
		   FROM ride_requests rr, trip_occurrences o, route_templates t
		  WHERE rr.id = a.request_id AND o.id = a.occurrence_id AND t.id = o.template_id
		    AND a.status = 'pending'
		    AND (rr.passenger_id = $1 OR t.driver_id = $1)`,
		// Credentials and reachability go outright: they identify a living
		// person and serve no purpose once the account is closed.
		`DELETE FROM push_tokens WHERE user_id = $1 AND $2::timestamptz IS NOT NULL`,
		`DELETE FROM refresh_tokens WHERE user_id = $1 AND $2::timestamptz IS NOT NULL`,
		`UPDATE invite_codes SET disabled_at = $2 WHERE created_by = $1 AND disabled_at IS NULL`,
	}
	for _, stmt := range windDown {
		if _, err := tx.Exec(ctx, stmt, userID, now); err != nil {
			return fmt.Errorf("account: winding down: %w", err)
		}
	}

	// Their departures, called off in their own statement so that the ones this
	// wind-down cancelled are known by id rather than inferred afterwards from
	// their status. "Cancelled and in the future" would also name a departure
	// something else called off last week, and would make the clean-up below
	// depend on nothing having left a confirmed booking behind on one of those.
	rows, err = tx.Query(ctx, `
		UPDATE trip_occurrences o SET status = 'cancelled', updated_at = now()
		 FROM route_templates t
		 WHERE t.id = o.template_id AND t.driver_id = $1
		   AND o.status = 'scheduled' AND o.depart_earliest > $2
		RETURNING o.id`, userID, now)
	if err != nil {
		return fmt.Errorf("account: calling off their departures: %w", err)
	}
	var calledOff []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("account: reading a called-off departure: %w", err)
		}
		calledOff = append(calledOff, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("account: calling off their departures: %w", err)
	}

	// The people holding a seat in one of the departures just called off.
	//
	// Cancelling the departure is not what tells them: their booking went on
	// saying 'confirmed', so the app went on showing a trip with a meeting point
	// and a time for a driver who no longer exists, and the first they would
	// have known is standing at the kerb. Suspension does all three of these —
	// cancel the booking, send the push, put the request back in the queue —
	// because from the passenger's side the two are the same event.
	//
	// The seats are not given back: the departure is gone, so there is nothing
	// left to take a seat in.
	if err := s.tellThePassengers(ctx, tx, calledOff, now); err != nil {
		return err
	}

	// Whoever was waiting on one of the offers just withdrawn, who is not the
	// person leaving. Their request sits at 'asking' waiting on an answer that
	// has gone, and nothing looks at a request in that state again — not the
	// matcher, which wants 'open', and not the reconciler that exists to catch
	// what the events miss.
	//
	// Asked again through the outbox rather than left for that hourly sweep,
	// and a wake on the occurrence would not do it: the departures of the person
	// leaving have just been cancelled, so there is nothing there to wake.
	rows, err = tx.Query(ctx, `
		UPDATE ride_requests rr SET status = 'open', updated_at = now()
		 WHERE rr.status = 'asking'
		   AND rr.passenger_id <> $1
		   AND EXISTS (
		         SELECT 1 FROM asks a
		          WHERE a.request_id = rr.id AND a.status = 'cancelled' AND a.responded_at = $2)
		   AND NOT EXISTS (
		         SELECT 1 FROM asks a
		          WHERE a.request_id = rr.id AND a.status = 'pending')
		RETURNING rr.id`, userID, now)
	if err != nil {
		return fmt.Errorf("account: putting the other passengers back in the queue: %w", err)
	}
	var stranded []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("account: reading the reopened requests: %w", err)
		}
		stranded = append(stranded, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("account: putting the other passengers back in the queue: %w", err)
	}
	for _, id := range stranded {
		payload, err := json.Marshal(map[string]any{"request_id": id})
		if err != nil {
			return fmt.Errorf("account: encoding the next ask: %w", err)
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Message{
			Kind:           "ask_next",
			IdempotencyKey: "ask_next:closed:" + userID.String() + ":" + id.String(),
			Payload:        payload,
		}, now); err != nil {
			return err
		}
	}

	// The phone is the identifier, and it is unique, so it cannot simply be
	// blanked. Replacing it with the account's own id keeps the column's
	// promise while making the number unrecoverable.
	if _, err := tx.Exec(ctx, `
		UPDATE users SET
			phone      = 'deleted:' || id::text,
			name       = 'Διαγραμμένος χρήστης',
			photo_url  = NULL,
			facebook_handle  = NULL,
			instagram_handle = NULL,
			gender     = NULL,
			status     = 'deleted',
			updated_at = now()
		WHERE id = $1`, userID); err != nil {
		return fmt.Errorf("account: anonymising: %w", err)
	}

	// The car identifies the driver as surely as the phone does, so the plate
	// and the model go. The row itself stays: route_templates points at
	// driver_profiles with ON DELETE CASCADE, so deleting it would take every
	// route this person ever published, and with them the occurrences, the
	// bookings other people held and the ratings attached to those bookings.
	// Erasing one account is not licence to erase everyone else's history.
	if _, err := tx.Exec(ctx, `
		UPDATE driver_profiles
		SET car_model = '—', car_color = NULL, car_plate = '—', updated_at = now()
		WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("account: clearing the vehicle: %w", err)
	}

	// And the garage, which is where the car has actually lived since a driver
	// could have more than one of them. The scrubbing above was written when
	// driver_profiles held the only copy, and the plate stayed readable here.
	//
	// It is the sharpest identifier in the database. A phone number needs
	// somebody to already have it; a plate is on the outside of the car, and
	// anybody standing next to it can look the owner up.
	//
	// Rows kept for the same reason as the profile: trip_occurrences names the
	// vehicle each departure ran with, and route_templates points at the pair.
	// The consumption figures stay too — they are what a past trip's suggested
	// contribution was worked out from, and a receipt that no longer adds up is
	// a worse thing to leave behind than an anonymous engine size.
	if _, err := tx.Exec(ctx, `
		UPDATE vehicles
		SET make = '—', model = '—', plate = '—', colour = NULL, label = NULL, updated_at = now()
		WHERE owner_id = $1`, userID); err != nil {
		return fmt.Errorf("account: clearing the garage: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO data_requests (user_id, kind, handled_at)
		VALUES ($1, 'erasure', $2)`, userID, now); err != nil {
		return fmt.Errorf("account: recording the request: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("account: commit: %w", err)
	}
	return nil
}

// Export returns everything held about the person, as JSON.
//
// Article 20 asks for a commonly readable, machine-readable format, which JSON
// is. Messages are included as sent and as received, because a conversation the
// person took part in is theirs to read even though it is not theirs alone.
func (s *Service) Export(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	now := s.clock.Now()

	out := map[string]any{"exported_at": now.UTC().Format(time.RFC3339)}

	profile := map[string]any{}
	var (
		phone, name         string
		photo, gender       *string
		facebook, instagram *string
		womenOnly           bool
		status              string
		createdAt           time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT phone, name, photo_url, facebook_handle, instagram_handle,
		       gender, women_only, status, created_at
		FROM users WHERE id = $1`, userID).
		Scan(&phone, &name, &photo, &facebook, &instagram, &gender, &womenOnly, &status, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("account: loading the profile: %w", err)
	}
	profile["phone"] = phone
	profile["name"] = name
	profile["photo_url"] = photo
	profile["facebook_handle"] = facebook
	profile["instagram_handle"] = instagram
	profile["gender"] = gender
	profile["women_only"] = womenOnly
	profile["status"] = status
	profile["created_at"] = createdAt.UTC().Format(time.RFC3339)
	out["profile"] = profile

	sections := []struct {
		name string
		sql  string
	}{
		{"routes_published", `
			SELECT origin_label, destination_label, route_km, seats, active, created_at
			FROM route_templates WHERE driver_id = $1 ORDER BY created_at`},
		{"requests_made", `
			SELECT origin_label, destination_label, window_start, window_end, seats, status, created_at
			FROM ride_requests WHERE passenger_id = $1 ORDER BY created_at`},
		{"trips_taken", `
			SELECT t.origin_label, t.destination_label, o.service_date, b.price_eur, b.seats, b.status
			FROM bookings b
			JOIN trip_occurrences o ON o.id = b.occurrence_id
			JOIN route_templates  t ON t.id = o.template_id
			WHERE b.passenger_id = $1 ORDER BY o.service_date`},
		{"messages", `
			SELECT body, created_at, (sender_id = $1) AS sent_by_me
			FROM trip_messages m
			WHERE m.sender_id = $1
			   OR m.occurrence_id IN (SELECT occurrence_id FROM bookings WHERE passenger_id = $1)
			ORDER BY created_at`},
		{"ratings_given", `
			SELECT stars, tag, created_at FROM ratings WHERE rater_id = $1 ORDER BY created_at`},
		{"ratings_received", `
			SELECT stars, tag, created_at FROM ratings WHERE ratee_id = $1 ORDER BY created_at`},
		{"invite_codes", `
			SELECT code, max_uses, used_count, created_at
			FROM invite_codes WHERE created_by = $1 ORDER BY created_at`},
		// The car and the settings behind it. Copied off the άδεια κυκλοφορίας
		// and held against their name, and until now the only way to see it was
		// to open the garage screen — which is not what a copy of your data
		// means. The plate above all: it identifies a vehicle and, through it, a
		// person, on a document they handed us.
		{"vehicles", `
			SELECT make, model, label, plate, fuel, engine_cc, co2_g_km,
			       consumption_l_100km, consumption_source, is_default, retired_at, created_at
			FROM vehicles WHERE owner_id = $1 ORDER BY created_at`},
		{"driver_settings", `
			SELECT detour_budget_min, notify_radius_km, notifications_muted,
			       wear_factor, free_below_km, created_at
			FROM driver_profiles WHERE user_id = $1`},
		// Choices they made about other people. Theirs to have a copy of, and
		// deliberately without the other person: somebody is entitled to know
		// they blocked two people, not to be handed the pair of identifiers.
		{"people_blocked", `
			SELECT created_at FROM blocks WHERE blocker_id = $1 ORDER BY created_at`},
		// What they reported, never what was reported about them. The second
		// would name whoever complained, and telling somebody who reported them
		// is how a report becomes a reason not to make one.
		{"reports_made", `
			SELECT reason, status, created_at FROM reports
			WHERE reporter_id = $1 ORDER BY created_at`},
		{"meeting_points_proposed", `
			SELECT place_id, name, kind, action, status, created_at
			FROM meeting_point_requests WHERE proposed_by = $1 ORDER BY created_at`},
	}
	for _, section := range sections {
		rows, err := s.pool.Query(ctx, section.sql, userID)
		if err != nil {
			return nil, fmt.Errorf("account: exporting %s: %w", section.name, err)
		}
		items, err := collect(rows)
		if err != nil {
			return nil, fmt.Errorf("account: exporting %s: %w", section.name, err)
		}
		out[section.name] = items
	}

	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("account: encoding the export: %w", err)
	}

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO data_requests (user_id, kind, handled_at)
		VALUES ($1, 'access', $2)`, userID, now); err != nil {
		return nil, fmt.Errorf("account: recording the request: %w", err)
	}
	return body, nil
}

// collect turns rows into maps keyed by column name, so a section can be added
// by writing one query rather than one query and one struct.
func collect(rows pgx.Rows) ([]map[string]any, error) {
	defer rows.Close()

	names := make([]string, 0, len(rows.FieldDescriptions()))
	for _, f := range rows.FieldDescriptions() {
		names = append(names, string(f.Name))
	}

	out := []map[string]any{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		item := make(map[string]any, len(names))
		for i, name := range names {
			if t, ok := values[i].(time.Time); ok {
				item[name] = t.UTC().Format(time.RFC3339)
				continue
			}
			item[name] = values[i]
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// tellThePassengers cancels the seats booked in the departures a closing
// account has just called off, tells each passenger, and puts the request
// behind each one back in the matching queue.
//
// It takes the departures by id, from the statement that cancelled them, so it
// acts only on what this closure did.
func (s *Service) tellThePassengers(
	ctx context.Context, tx pgx.Tx, calledOff []uuid.UUID, now time.Time,
) error {
	if len(calledOff) == 0 {
		return nil
	}

	rows, err := tx.Query(ctx, `
		UPDATE bookings SET status = 'cancelled_by_driver', updated_at = now()
		 WHERE occurrence_id = ANY($1) AND status = 'confirmed'
		RETURNING id, passenger_id, request_id`, calledOff)
	if err != nil {
		return fmt.Errorf("account: cancelling the seats in their departures: %w", err)
	}
	type stranded struct {
		booking, passenger uuid.UUID
		// Null for a seat that came from a standing arrangement, which is
		// cancelled in its own right and carries its own standing permission.
		request *uuid.UUID
	}
	var affected []stranded
	for rows.Next() {
		var a stranded
		if err := rows.Scan(&a.booking, &a.passenger, &a.request); err != nil {
			rows.Close()
			return fmt.Errorf("account: reading the cancelled seats: %w", err)
		}
		affected = append(affected, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("account: cancelling the seats in their departures: %w", err)
	}

	// Ordered by the request they are about, so that two closures running at
	// once reach the same rows in the same sequence rather than each holding
	// what the other is waiting for. See RISKS.md.
	slices.SortFunc(affected, func(x, y stranded) int {
		return strings.Compare(x.booking.String(), y.booking.String())
	})

	for _, a := range affected {
		payload, err := json.Marshal(map[string]any{
			"event": "booking_cancelled", "recipient_id": a.passenger, "booking_id": a.booking,
		})
		if err != nil {
			return fmt.Errorf("account: encoding the notification: %w", err)
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Message{
			Kind:           "push",
			Payload:        payload,
			IdempotencyKey: "booking_cancelled:" + a.booking.String(),
		}, now); err != nil {
			return err
		}
		if a.request == nil {
			continue
		}
		// The window is re-read because a journey whose time has passed has no
		// ride left to find.
		tag, err := tx.Exec(ctx, `
			UPDATE ride_requests SET status = 'open', updated_at = now()
			 WHERE id = $1 AND status = 'matched' AND window_end > $2`, *a.request, now)
		if err != nil {
			return fmt.Errorf("account: reopening the stranded request: %w", err)
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		next, err := json.Marshal(map[string]any{"request_id": *a.request, "reason": "driver_left"})
		if err != nil {
			return fmt.Errorf("account: encoding the next ask: %w", err)
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Message{
			Kind: "ask_next",
			// Keyed on the seat that was lost rather than on the request. The
			// same request can be stranded more than once — rematched after
			// this driver leaves, and stranded again when the next one does —
			// and a key naming only the request would make the second event a
			// duplicate of the first and swallow it, leaving that passenger
			// waiting for an ask nobody was going to send.
			IdempotencyKey: "ask_next:driver_left:" + a.booking.String(),
			Payload:        next,
		}, now); err != nil {
			return err
		}
	}
	return nil
}
