// Package weekend keeps the plans of people who leave town most weekends, and
// turns them into requests week by week (specs/WEEKEND.md).
//
// A plan says once: from here to there, Friday at 15:00, back on Sunday at
// 18:00. An hourly job makes the requests of the coming weekend from it. Those
// requests are never put to a driver by the matcher; the driver says yes first,
// and only then is the passenger told.
package weekend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/clock"
	"carpool/internal/domain"
	"carpool/internal/match"
	"carpool/internal/notices"
	"carpool/internal/outbox"
	"carpool/internal/store"
)

// ErrNotFound means there is no such plan for this person. Somebody else's plan
// and one that never existed get the same error, so asking cannot be used to
// find out who has a plan.
var ErrNotFound = errors.New("weekend: no such plan")

// InvalidError is a plan that cannot be made, with a code the app can branch on
// and a sentence a person can act on.
type InvalidError struct{ Code, Message string }

func (e *InvalidError) Error() string { return "weekend: " + e.Message }

func invalid(code, message string) error { return &InvalidError{Code: code, Message: message} }

// stopAfterDeclines is how many «όχι» in a row make the app offer to stop.
const stopAfterDeclines = 3

// Plan is a weekend plan: what the passenger sets once, and, when read back,
// how the coming weekend stands.
type Plan struct {
	ID uuid.UUID

	OriginLon, OriginLat           float64
	DestinationLon, DestinationLat float64
	OriginLabel, DestinationLabel  string

	// DepartDow is 5 for Friday, 6 for Saturday, 0 for Sunday. Times are
	// "15:04" in Athens.
	DepartDow  int
	DepartTime string
	ReturnDow  *int
	ReturnTime *string

	Seats        int
	Luggage      string
	LuggageCount int
	EveryWeek    bool

	// Read back only.
	DeclinesInRow int
	// ThisWeek is how the coming weekend stands: none, open, matched or
	// cancelled.
	ThisWeek  string
	AskToStop bool
}

// Service keeps weekend plans.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
	loc   *time.Location
}

// NewService returns a service reasoning about days in loc, Europe/Athens in
// production.
func NewService(pool *pgxpool.Pool, c clock.Clock, loc *time.Location) *Service {
	return &Service{pool: pool, clock: c, loc: loc}
}

// offset is a weekend day's distance from the Friday: 5, 6 and 0 are 0, 1 and
// 2. Anything else is not a weekend day.
func offset(dow int) (int, bool) {
	if dow != 5 && dow != 6 && dow != 0 {
		return 0, false
	}
	return (dow + 2) % 7, true
}

func parseClock(in string) (time.Time, bool) {
	t, err := time.Parse("15:04", in)
	return t, err == nil
}

func (p Plan) validate() error {
	out, ok := offset(p.DepartDow)
	if !ok {
		return invalid("invalid_day", "the outbound day has to be Friday, Saturday or Sunday")
	}
	depart, ok := parseClock(p.DepartTime)
	if !ok {
		return invalid("invalid_time", "the outbound time has to be hours and minutes, like 15:00")
	}
	if (p.ReturnDow == nil) != (p.ReturnTime == nil) {
		return invalid("invalid_return", "a return needs both a day and a time")
	}
	if p.ReturnDow != nil {
		back, ok := offset(*p.ReturnDow)
		if !ok {
			return invalid("invalid_day", "the return day has to be Friday, Saturday or Sunday")
		}
		at, ok := parseClock(*p.ReturnTime)
		if !ok {
			return invalid("invalid_time", "the return time has to be hours and minutes, like 18:00")
		}
		if back < out || (back == out && !at.After(depart)) {
			return invalid("invalid_return", "the return has to come after the outbound")
		}
	}
	if p.Seats < 1 || p.Seats > 4 {
		return invalid("invalid_seats", "seats must be between 1 and 4")
	}
	luggage, err := domain.ParseLuggage(p.Luggage)
	if err != nil {
		return invalid("invalid_luggage", "that is not a luggage size this app knows")
	}
	if (luggage == domain.LuggageNone) != (p.LuggageCount == 0) || p.LuggageCount < 0 || p.LuggageCount > 4 {
		return invalid("invalid_luggage",
			"say how many items you are bringing, between one and four, or say you have none")
	}
	return nil
}

// Create stores a plan for passenger and makes its requests for the coming
// weekend at once, so it is not an hour before the app shows them.
func (s *Service) Create(ctx context.Context, passenger uuid.UUID, p Plan) (Plan, error) {
	if err := p.validate(); err != nil {
		return Plan{}, err
	}
	var err error
	if p.OriginLabel, err = domain.NormalisePlaceLabel(p.OriginLabel); err != nil {
		return Plan{}, invalid("invalid_request", "the pickup needs a name somebody can read")
	}
	if p.DestinationLabel, err = domain.NormalisePlaceLabel(p.DestinationLabel); err != nil {
		return Plan{}, invalid("invalid_request", "the destination needs a name somebody can read")
	}
	for _, end := range []struct {
		what     string
		lon, lat float64
	}{
		{"pickup", p.OriginLon, p.OriginLat},
		{"destination", p.DestinationLon, p.DestinationLat},
	} {
		_, err := store.RegionAt(ctx, s.pool, end.lon, end.lat)
		if errors.Is(err, store.ErrOutsideGreece) {
			return Plan{}, invalid("outside_country",
				"the "+end.what+" is outside Greece, which is as far as this app goes")
		}
		if err != nil {
			return Plan{}, fmt.Errorf("weekend: locating the %s: %w", end.what, err)
		}
	}

	now := s.clock.Now()
	err = s.pool.QueryRow(ctx, `
		INSERT INTO weekend_plans (passenger_id, origin, destination, origin_label, destination_label,
		                           depart_dow, depart_time, return_dow, return_time,
		                           seats, luggage, luggage_count, every_week, created_at, updated_at)
		VALUES ($1, ST_SetSRID(ST_MakePoint($2, $3), 4326), ST_SetSRID(ST_MakePoint($4, $5), 4326),
		        $6, $7, $8, $9::time, $10, $11::time, $12, $13::luggage_size, $14, $15, $16, $16)
		RETURNING id`,
		passenger, p.OriginLon, p.OriginLat, p.DestinationLon, p.DestinationLat,
		p.OriginLabel, p.DestinationLabel, p.DepartDow, p.DepartTime, p.ReturnDow, p.ReturnTime,
		p.Seats, p.Luggage, p.LuggageCount, p.EveryWeek, now).Scan(&p.ID)
	if err != nil {
		return Plan{}, fmt.Errorf("weekend: saving the plan: %w", err)
	}
	if _, err := s.generateFor(ctx, p.ID, now); err != nil {
		// The plan stands and the hourly job will make the requests.
		slog.Warn("making the first weekend's requests", "plan", p.ID, "error", err)
	}
	views, err := s.read(ctx, s.pool, passenger, &p.ID, now)
	if err != nil || len(views) != 1 {
		return Plan{}, fmt.Errorf("weekend: reading the plan back: %w", err)
	}
	return views[0], nil
}

// List is the caller's plans that are still on, with how the coming weekend
// stands for each.
func (s *Service) List(ctx context.Context, passenger uuid.UUID) ([]Plan, error) {
	return s.read(ctx, s.pool, passenger, nil, s.clock.Now())
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (s *Service) read(ctx context.Context, q querier, passenger uuid.UUID, id *uuid.UUID, now time.Time) ([]Plan, error) {
	rows, err := q.Query(ctx, `
		SELECT p.id, ST_X(p.origin), ST_Y(p.origin), ST_X(p.destination), ST_Y(p.destination),
		       COALESCE(p.origin_label, ''), COALESCE(p.destination_label, ''),
		       p.depart_dow, to_char(p.depart_time, 'HH24:MI'),
		       p.return_dow, to_char(p.return_time, 'HH24:MI'),
		       p.seats, p.luggage::text, p.luggage_count, p.every_week, p.declines_in_row,
		       (SELECT CASE WHEN bool_or(r.status = 'matched') THEN 'matched'
		                    WHEN bool_or(r.status IN ('open', 'asking')) THEN 'open'
		                    WHEN bool_or(r.status = 'cancelled') THEN 'cancelled'
		                    ELSE 'none' END
		          FROM ride_requests r
		         WHERE r.weekend_plan_id = p.id AND r.weekend_of = $3)
		  FROM weekend_plans p
		 WHERE p.passenger_id = $1 AND p.active AND ($2::uuid IS NULL OR p.id = $2)
		 ORDER BY p.created_at, p.id`,
		passenger, id, s.comingFriday(now))
	if err != nil {
		return nil, fmt.Errorf("weekend: listing plans: %w", err)
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.ID, &p.OriginLon, &p.OriginLat, &p.DestinationLon, &p.DestinationLat,
			&p.OriginLabel, &p.DestinationLabel, &p.DepartDow, &p.DepartTime, &p.ReturnDow, &p.ReturnTime,
			&p.Seats, &p.Luggage, &p.LuggageCount, &p.EveryWeek, &p.DeclinesInRow, &p.ThisWeek); err != nil {
			return nil, fmt.Errorf("weekend: reading a plan: %w", err)
		}
		p.AskToStop = p.DeclinesInRow >= stopAfterDeclines
		out = append(out, p)
	}
	return out, rows.Err()
}

// Stop ends a plan and calls off the requests of it that are still looking. A
// seat already booked is a booking, and is cancelled as one.
func (s *Service) Stop(ctx context.Context, passenger, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("weekend: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The passenger's matching lock first, as cancelling a request takes it.
	if _, err := tx.Exec(ctx, match.PassengerLockSQL, passenger); err != nil {
		return fmt.Errorf("weekend: locking the passenger: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE weekend_plans SET active = false, updated_at = $3
		 WHERE id = $1 AND passenger_id = $2`, id, passenger, s.clock.Now())
	if err != nil {
		return fmt.Errorf("weekend: stopping the plan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := s.cancelLooking(ctx, tx, id, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("weekend: commit: %w", err)
	}
	return nil
}

// cancelLooking cancels the plan's requests that are open or being asked, with
// their live offers, and wakes whoever waits behind the seats those offers
// held. weekendOf limits it to one weekend; nil takes them all. It returns how
// many requests it cancelled.
func (s *Service) cancelLooking(ctx context.Context, tx pgx.Tx, plan uuid.UUID, weekendOf *time.Time) (int, error) {
	var cancelled int
	var freed []uuid.UUID
	err := tx.QueryRow(ctx, `
		WITH cancelled AS (
			UPDATE ride_requests SET status = 'cancelled', updated_at = now()
			 WHERE weekend_plan_id = $1 AND status IN ('open', 'asking')
			   AND ($2::date IS NULL OR weekend_of = $2)
			RETURNING id
		), killed AS (
			UPDATE asks SET status = 'cancelled', responded_at = now()
			 WHERE request_id IN (SELECT id FROM cancelled) AND status = 'pending'
			RETURNING occurrence_id
		)
		SELECT (SELECT count(*) FROM cancelled), ARRAY(SELECT DISTINCT occurrence_id FROM killed)`,
		plan, weekendOf).Scan(&cancelled, &freed)
	if err != nil {
		return 0, fmt.Errorf("weekend: cancelling requests: %w", err)
	}
	if err := outbox.WakeAll(ctx, tx, freed, "request_cancelled", s.clock.Now()); err != nil {
		return 0, fmt.Errorf("weekend: waking the seats: %w", err)
	}
	return cancelled, nil
}

// ThisWeek is the Thursday answer. Going leaves things as they are and starts
// the count of «όχι» over. Not going calls off this weekend's requests, with
// any live offers on them, and counts one more «όχι».
//
// The count rises only when there was something to call off, so the same
// answer sent twice, by a double tap or a retry, is one answer.
func (s *Service) ThisWeek(ctx context.Context, passenger, id uuid.UUID, going bool) (Plan, error) {
	now := s.clock.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Plan{}, fmt.Errorf("weekend: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, match.PassengerLockSQL, passenger); err != nil {
		return Plan{}, fmt.Errorf("weekend: locking the passenger: %w", err)
	}
	var active bool
	err = tx.QueryRow(ctx,
		`SELECT active FROM weekend_plans WHERE id = $1 AND passenger_id = $2 FOR UPDATE`,
		id, passenger).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("weekend: reading the plan: %w", err)
	}

	declines := 0
	if !going {
		friday := s.comingFriday(now)
		n, err := s.cancelLooking(ctx, tx, id, &friday)
		if err != nil {
			return Plan{}, err
		}
		if n > 0 {
			declines = 1
		}
		if _, err := tx.Exec(ctx, `
			UPDATE weekend_plans SET declines_in_row = declines_in_row + $2, updated_at = $3 WHERE id = $1`,
			id, declines, now); err != nil {
			return Plan{}, fmt.Errorf("weekend: counting the answer: %w", err)
		}
	} else if _, err := tx.Exec(ctx,
		`UPDATE weekend_plans SET declines_in_row = 0, updated_at = $2 WHERE id = $1`, id, now); err != nil {
		return Plan{}, fmt.Errorf("weekend: counting the answer: %w", err)
	}

	views, err := s.read(ctx, tx, passenger, &id, now)
	if err != nil || len(views) != 1 {
		return Plan{}, fmt.Errorf("weekend: reading the plan back: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Plan{}, fmt.Errorf("weekend: commit: %w", err)
	}
	return views[0], nil
}

// comingFriday is the Friday, as a calendar date, of the weekend that has not
// ended yet at now: on a Saturday or a Sunday it is the one just gone.
func (s *Service) comingFriday(now time.Time) time.Time {
	local := now.In(s.loc)
	ahead := 5 - int(local.Weekday())
	if local.Weekday() == time.Sunday {
		ahead = -2
	}
	return time.Date(local.Year(), local.Month(), local.Day()+ahead, 0, 0, 0, 0, time.UTC)
}

// at is the instant it is hh:mm in Athens on a calendar date. A wall time the
// spring-forward skipped is the hour after it.
func (s *Service) at(date time.Time, hh, mm int) time.Time {
	got := domain.ResolveWallTime(s.loc, date.Year(), date.Month(), date.Day(), hh, mm)
	if len(got) == 0 {
		got = domain.ResolveWallTime(s.loc, date.Year(), date.Month(), date.Day(), hh+1, mm)
	}
	return got[0]
}

// Run is one hourly pass: make the coming weekend's requests, then ask the
// Thursday question.
func (s *Service) Run(ctx context.Context) error {
	now := s.clock.Now()
	_, genErr := s.Generate(ctx, now)
	_, askErr := s.AskThursday(ctx, now)
	return errors.Join(genErr, askErr)
}

// Generate makes, for every active plan that has none yet, the requests of the
// coming weekend, and returns how many it made. A plan for this weekend only
// is finished once the weekend is over.
func (s *Service) Generate(ctx context.Context, now time.Time) (int, error) {
	friday := s.comingFriday(now)
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.every_week, p.created_at
		  FROM weekend_plans p
		  JOIN users u ON u.id = p.passenger_id
		 WHERE p.active AND u.status = 'active'
		 ORDER BY p.created_at, p.id`)
	if err != nil {
		return 0, fmt.Errorf("weekend: listing plans: %w", err)
	}
	type due struct {
		id        uuid.UUID
		everyWeek bool
		createdAt time.Time
	}
	var plans []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.everyWeek, &d.createdAt); err != nil {
			rows.Close()
			return 0, fmt.Errorf("weekend: reading a plan: %w", err)
		}
		plans = append(plans, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("weekend: listing plans: %w", err)
	}

	made := 0
	var failed error
	for _, d := range plans {
		// A plan for one weekend is for the first one that was coming when it
		// was made; once that one is over, so is the plan.
		if !d.everyWeek && s.comingFriday(d.createdAt).Before(friday) {
			if _, err := s.pool.Exec(ctx,
				`UPDATE weekend_plans SET active = false, updated_at = $2 WHERE id = $1`, d.id, now); err != nil {
				failed = errors.Join(failed, fmt.Errorf("weekend: ending a plan: %w", err))
			}
			continue
		}
		n, err := s.generateFor(ctx, d.id, now)
		made += n
		if err != nil {
			failed = errors.Join(failed, err)
		}
	}
	return made, failed
}

// generateFor makes the requests of the coming weekend for one plan that has
// none. Days that are already over are left out.
func (s *Service) generateFor(ctx context.Context, plan uuid.UUID, now time.Time) (int, error) {
	friday := s.comingFriday(now)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("weekend: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var passenger uuid.UUID
	var departDow int
	var departTime string
	var returnDow *int
	var returnTime *string
	err = tx.QueryRow(ctx, `
		SELECT passenger_id, depart_dow, to_char(depart_time, 'HH24:MI'),
		       return_dow, to_char(return_time, 'HH24:MI')
		  FROM weekend_plans WHERE id = $1`, plan).
		Scan(&passenger, &departDow, &departTime, &returnDow, &returnTime)
	if err != nil {
		return 0, fmt.Errorf("weekend: reading the plan: %w", err)
	}
	// Under the passenger's lock, which Stop and the Thursday answer also
	// take, so a plan stopped a moment ago is not given requests after the
	// ones it cancelled; and the plan is read again once it is held.
	if _, err := tx.Exec(ctx, match.PassengerLockSQL, passenger); err != nil {
		return 0, fmt.Errorf("weekend: locking the passenger: %w", err)
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT active FROM weekend_plans WHERE id = $1`, plan).Scan(&active); err != nil {
		return 0, fmt.Errorf("weekend: reading the plan: %w", err)
	}
	if !active {
		return 0, nil
	}

	made := 0
	// insert makes one request from the plan, the other way round for a
	// return, and answers its id. The unique index is the last word on
	// doing it twice.
	insert := func(leg string, from, to time.Time, root *uuid.UUID) (*uuid.UUID, error) {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO ride_requests (passenger_id, origin, destination, origin_label, destination_label,
			                           window_start, window_end, seats, kind, status, luggage, luggage_count,
			                           alternative_of, weekend_plan_id, weekend_of, weekend_leg)
			SELECT p.passenger_id,
			       CASE WHEN $2 = 'back' THEN p.destination ELSE p.origin END,
			       CASE WHEN $2 = 'back' THEN p.origin ELSE p.destination END,
			       CASE WHEN $2 = 'back' THEN p.destination_label ELSE p.origin_label END,
			       CASE WHEN $2 = 'back' THEN p.origin_label ELSE p.destination_label END,
			       $3, $4, p.seats, 'open', 'open', p.luggage, p.luggage_count, $5, p.id, $6, $2
			  FROM weekend_plans p WHERE p.id = $1
			ON CONFLICT DO NOTHING
			RETURNING id`, plan, leg, from, to, root, friday).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("weekend: making a request: %w", err)
		}
		made++
		return &id, nil
	}
	has := func(leg string) (bool, error) {
		var yes bool
		err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM ride_requests
			                WHERE weekend_plan_id = $1 AND weekend_of = $2 AND weekend_leg = $3)`,
			plan, friday, leg).Scan(&yes)
		return yes, err
	}
	hhmm := func(in string) (int, int) {
		t, _ := parseClock(in)
		return t.Hour(), t.Minute()
	}

	if done, err := has("out"); err != nil {
		return 0, fmt.Errorf("weekend: looking for requests: %w", err)
	} else if !done {
		first, _ := offset(departDow)
		var root *uuid.UUID
		for day := first; day <= 2; day++ {
			date := friday.AddDate(0, 0, day)
			h, m := 6, 0
			if day == first {
				h, m = hhmm(departTime)
			}
			from, to := s.at(date, h, m), s.at(date, 23, 59)
			if !to.After(now) {
				continue
			}
			id, err := insert("out", from, to, root)
			if err != nil {
				return 0, err
			}
			if root == nil {
				root = id
			}
		}
	}
	if returnDow != nil {
		if done, err := has("back"); err != nil {
			return 0, fmt.Errorf("weekend: looking for requests: %w", err)
		} else if !done {
			day, _ := offset(*returnDow)
			date := friday.AddDate(0, 0, day)
			h, m := hhmm(*returnTime)
			if to := s.at(date, 23, 59); to.After(now) {
				if _, err := insert("back", s.at(date, h, m), to, nil); err != nil {
					return 0, err
				}
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("weekend: commit: %w", err)
	}
	return made, nil
}

// AskThursday sends «Φεύγεις αυτό το Σαββατοκύριακο;» to every active plan that
// still has a request looking for a driver this weekend, once per plan and
// weekend, from Thursday 18:00 until the outbound window opens. It returns how
// many it asked.
//
// A plan made after Thursday 18:00 is not asked: the passenger has only just
// said they are going. One with a seat already booked is not asked either.
func (s *Service) AskThursday(ctx context.Context, now time.Time) (int, error) {
	friday := s.comingFriday(now)
	thursday := s.at(friday.AddDate(0, 0, -1), 18, 0)
	if now.Before(thursday) {
		return 0, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.passenger_id
		  FROM weekend_plans p
		  JOIN users u ON u.id = p.passenger_id
		 WHERE p.active AND u.status = 'active'
		   AND p.created_at <= $2
		   AND p.last_asked_for IS DISTINCT FROM $1::date
		   AND NOT EXISTS (SELECT 1 FROM ride_requests r
		                    WHERE r.weekend_plan_id = p.id AND r.weekend_of = $1 AND r.status = 'matched')
		   AND (SELECT min(r.window_start) FROM ride_requests r
		         WHERE r.weekend_plan_id = p.id AND r.weekend_of = $1 AND r.weekend_leg = 'out'
		           AND r.status IN ('open', 'asking')) > $3
		 ORDER BY p.created_at, p.id`, friday, thursday, now)
	if err != nil {
		return 0, fmt.Errorf("weekend: finding plans to ask: %w", err)
	}
	type target struct{ plan, passenger uuid.UUID }
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.plan, &t.passenger); err != nil {
			rows.Close()
			return 0, fmt.Errorf("weekend: reading a plan: %w", err)
		}
		targets = append(targets, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("weekend: finding plans to ask: %w", err)
	}

	asked := 0
	var failed error
	for _, t := range targets {
		ok, err := s.ask(ctx, t.plan, t.passenger, friday, now)
		if err != nil {
			failed = errors.Join(failed, err)
		}
		if ok {
			asked++
		}
	}
	return asked, failed
}

// ask marks the plan asked for this weekend and queues the push in one
// transaction, so a crash between the two neither loses the question nor sends
// it twice.
func (s *Service) ask(ctx context.Context, plan, passenger uuid.UUID, friday, now time.Time) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("weekend: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE weekend_plans SET last_asked_for = $2
		 WHERE id = $1 AND active AND last_asked_for IS DISTINCT FROM $2::date`, plan, friday)
	if err != nil {
		return false, fmt.Errorf("weekend: marking the plan asked: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	msg, err := notices.WeekendQuestion(passenger, plan, friday)
	if err != nil {
		return false, err
	}
	if err := outbox.Enqueue(ctx, tx, msg, now); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("weekend: commit: %w", err)
	}
	return true, nil
}
