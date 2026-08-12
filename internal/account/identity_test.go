package account_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// Erasure has to reach the face and the person's own words.
//
// "Erase me" rewrites the phone, replaces the name with a placeholder and nulls
// the handles and the gender. It never touched three things that identify
// somebody at least as well:
//
//   - profile_photos, which holds the actual image. The users row loses its
//     photo_url, but the bytes live in their own table, and the endpoint that
//     serves them reads that table by id. Their face survived.
//   - users.bio, five hundred characters somebody wrote about themselves. It is
//     shown to the driver and to every passenger on the trip detail.
//   - user_preferences, the tags they chose.
//
// This is the third time this class has been found here: the registration plate
// went the same way, and before that the export was missing what the erasure
// had scrubbed. The pattern is that erasure was written against the users row,
// and everything a person owns that lives in a table of its own was missed.

func (w *world) seedIdentity(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := w.pool.Exec(ctx, `
		UPDATE users SET bio = 'Δουλεύω στο νοσοκομείο, κάνω τη διαδρομή κάθε πρωί'
		 WHERE id = $1`, w.driverID); err != nil {
		t.Fatalf("seeding the bio: %v", err)
	}
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO user_preferences (user_id, tag) VALUES ($1, 'no_smoking'), ($1, 'quiet_ride')`,
		w.driverID); err != nil {
		t.Fatalf("seeding the preferences: %v", err)
	}
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO profile_photos (user_id, data, content_type, sha256, width, height)
		VALUES ($1, '\x89504e470d0a1a0a'::bytea, 'image/jpeg', sha256('x'::bytea), 400, 400)`,
		w.driverID); err != nil {
		t.Fatalf("seeding the photo: %v", err)
	}
}

func TestErasureTakesTheFaceAndTheirOwnWords(t *testing.T) {
	w := newWorld(t)
	w.seedIdentity(t)

	if err := w.svc.Delete(context.Background(), w.driverID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	ctx := context.Background()
	var photos int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM profile_photos WHERE user_id = $1`, w.driverID).Scan(&photos); err != nil {
		t.Fatalf("counting photos: %v", err)
	}
	if photos != 0 {
		t.Error("their photograph survived the erasure, and the endpoint that serves it reads by id")
	}

	if got := w.column(t, "bio", w.driverID); got != nil {
		t.Errorf("the bio survived as %v", got)
	}

	var prefs int
	if err := w.pool.QueryRow(ctx,
		`SELECT count(*) FROM user_preferences WHERE user_id = $1`, w.driverID).Scan(&prefs); err != nil {
		t.Fatalf("counting preferences: %v", err)
	}
	if prefs != 0 {
		t.Errorf("%d preferences survived the erasure", prefs)
	}
}

// And the copy somebody asks for has to contain them, which is the same gap
// from the other side: what erasure scrubs is exactly what a subject access
// request is for.
func TestACopyIncludesTheProfileTheyWrote(t *testing.T) {
	w := newWorld(t)
	w.seedIdentity(t)

	body, err := w.svc.Export(context.Background(), w.driverID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("the export is not readable JSON: %v", err)
	}

	profile, ok := out["profile"].(map[string]any)
	if !ok {
		t.Fatal("the copy has no profile section")
	}
	if profile["bio"] == nil || profile["bio"] == "" {
		t.Error("the copy leaves out the bio they wrote")
	}
	prefs, _ := profile["preferences"].([]any)
	if len(prefs) != 2 || prefs[0] != "no_smoking" || prefs[1] != "quiet_ride" {
		t.Errorf("the copy leaves out their preferences: %v", profile["preferences"])
	}

	// The photograph itself, not a note that one is held. An avatar is tens of
	// kilobytes, and answering "we have a file" turns a right into an errand.
	if profile["has_photo"] != true {
		t.Error("the copy does not say a photograph is held")
	}
	if got := profile["photo_content_type"]; got != "image/jpeg" {
		t.Errorf("the copy calls the photograph %v", got)
	}
	encoded, _ := profile["photo_base64"].(string)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("the photograph in the copy is not readable: %v", err)
	}
	var stored []byte
	if err := w.pool.QueryRow(context.Background(),
		`SELECT data FROM profile_photos WHERE user_id = $1`, w.driverID).Scan(&stored); err != nil {
		t.Fatalf("reading the stored photograph: %v", err)
	}
	if !bytes.Equal(decoded, stored) {
		t.Errorf("the copy holds %d bytes of photograph, the account holds %d", len(decoded), len(stored))
	}
}
