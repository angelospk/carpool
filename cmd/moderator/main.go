// Command moderator grants and revokes moderation from the server.
//
// There is no way to become a moderator through the app, and there should not
// be: the first one has to be made by somebody with the database, and a role
// that can be handed out over the wire is a role that can be taken by anyone
// who finds a way to send that request.
//
//	moderator -grant  <user-uuid> -note "runs the Evros pilot"
//	moderator -revoke <user-uuid>
//	moderator -list
//
// Identified by uuid rather than by phone number, because a phone number can be
// given up and reissued and this is a decision about a person.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"carpool/internal/config"
	"carpool/internal/store"
)

func main() {
	grant := flag.String("grant", "", "user id to make a moderator")
	revoke := flag.String("revoke", "", "user id to stop being a moderator")
	list := flag.Bool("list", false, "list the current moderators")
	note := flag.String("note", "", "why, for the record")
	flag.Parse()

	if err := run(*grant, *revoke, *list, *note); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(grant, revoke string, list bool, note string) error {
	ctx := context.Background()

	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer pool.Close()

	switch {
	case list:
		return listModerators(ctx, pool)
	case grant != "":
		return set(ctx, pool, grant, true, note)
	case revoke != "":
		return set(ctx, pool, revoke, false, note)
	default:
		flag.Usage()
		return fmt.Errorf("nothing to do")
	}
}

func set(ctx context.Context, pool *pgxpool.Pool, raw string, granted bool, note string) error {
	id, err := uuid.Parse(raw)
	if err != nil {
		return fmt.Errorf("%q is not a user id", raw)
	}

	// Idempotent: running it twice is not an error, and the second run records
	// that somebody ran it twice.
	if err := store.SetModerator(ctx, pool, id, granted, uuid.Nil, note); err != nil {
		return err
	}

	var name, phone string
	if err := pool.QueryRow(ctx, `SELECT name, phone FROM users WHERE id = $1`, id).Scan(&name, &phone); err != nil {
		return fmt.Errorf("reading the user back: %w", err)
	}
	verb := "is now a moderator"
	if !granted {
		verb = "is no longer a moderator"
	}
	fmt.Printf("%s (%s, %s) %s\n", name, phone, id, verb)
	return nil
}

func listModerators(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx,
		`SELECT id, name, phone FROM users WHERE is_moderator ORDER BY name`)
	if err != nil {
		return fmt.Errorf("listing: %w", err)
	}
	defer rows.Close()

	var n int
	for rows.Next() {
		var id uuid.UUID
		var name, phone string
		if err := rows.Scan(&id, &name, &phone); err != nil {
			return err
		}
		fmt.Printf("%s  %-20s %s\n", id, name, phone)
		n++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if n == 0 {
		fmt.Println("nobody is a moderator yet")
	}
	return nil
}
