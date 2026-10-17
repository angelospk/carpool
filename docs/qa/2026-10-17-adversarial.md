# Adversarial QA, 17 October 2026

One agent played nine accounts against the running API (`http://127.0.0.1:8188/v1`, DEV_MODE, DB `qa1017api`, psql read-only): a driver D with a trip, two passengers A and B booked on it, a third passenger P3, a stranger S, a second driver E with the opposite trip, two more passengers (C, C2) on E's trip, and Z for account deletion. Product code was not touched.

The probes are in `scripts/qa-adversarial.sh` (about 340 checks, roughly 6 minutes, prints `ok` or `FAIL` per check and carries on after a FAIL). Last run: **328 ok, 14 FAIL**. Every check asserts the secure behaviour, so a FAIL is a finding. The 14 FAIL lines map to findings 1 to 11 below (the oracle probes and the chat NUL probe print more than one line); findings 12 to 14 are printed as `note` lines because they are absences, not assertions.

Result in one paragraph: no authorization hole, and no leak of identity, phone number or location between users, was found on any route that takes an id. Every cross-user attempt on asks, requests, alternatives, bookings, trips, live positions, chat, drive control, arrangements, routes, vehicles, groups and moderation came back 403 or 404 and left the database untouched. The three important findings are about state that outlives the thing that justified it (a second pending ask that can never be accepted, a chat socket that outlives the seat) and one privacy oracle (phone-change). There is nothing critical.

## Findings

| # | Sev | Title | Where |
|---|---|---|---|
| 1 | important | A second pending ask on the same trip becomes unacceptable once the first is accepted, and stays pending | `internal/asks/accept.go:345`, `:364` |
| 2 | important | A chat WebSocket keeps delivering after the passenger cancelled his seat | `internal/api/ws_handler.go:21-37`, `internal/ws/hub.go` Publish |
| 3 | important | `POST /me/phone/request` tells any signed-in user whether a phone number has an account, unlimited | `internal/auth/phone_change.go:109-117` vs `:141` |
| 4 | minor | Raw database errors in `error.message` (block, chat) | `internal/api/trip_handlers.go:309`, `:335`, `internal/store/chat.go:42`, `:100` |
| 5 | minor | Rating `tag` has no length or content limit (40 000 characters stored) | `internal/api/trip_handlers.go` handleRateTrip, `internal/store/trips.go` RateTrip |
| 6 | minor | A co-passenger sees what the other passenger pays, and their seats | `internal/api/trip_handlers.go:119` |
| 7 | minor | 403 versus 404 or 409 tells whether an ask or booking id exists | `internal/asks/accept.go:132` vs `:140`, `internal/bookings/bookings.go:105` |
| 8 | minor | An expired ask answers "already been answered" once the sweeper has run | `internal/asks/accept.go:146` |
| 9 | minor | Per-IP limits trust a client-sent `X-Real-IP` | `internal/api/ratelimit.go:63` |
| 10 | minor | The access token outlives logout and refresh-family revocation (15 minutes) | `internal/auth/token.go:21-23` |
| 11 | minor | A cancelled passenger's last live position stays in the table until the janitor (about an hour) | `internal/bookings/bookings.go` Cancel, `internal/jobs/janitor.go:104` |
| 12 | minor | No rate limit on unauthenticated `/places` and `/places/near`, on `/estimates`, on `PUT /trips/{id}/position` | `internal/api/router.go:198-200`, `:312`, `:377` |
| 13 | minor | Chat text is stored as typed: control characters, RTL override, zero-width characters; the 2000 limit counts bytes | `internal/store/chat.go:27-34` |
| 14 | minor | `DELETE /me` needs only the access token | `internal/api/standing_handlers.go:283` |

Totals: 14 findings, 0 critical, 3 important, 11 minor.

### 1. Second pending ask becomes unacceptable (important)

Two passengers asked about the same trip close together each get a pending ask for the same driver (this is allowed: three asks were pending on a 3-seat car). The driver accepts the first. The second ask carries the old `itinerary_version`; `Accept` compares it with the occurrence and refuses with `ErrStaleItinerary`, which is mapped to 409 `itinerary_changed`: "your route changed; please reopen the request". Nothing then retires or re-quotes the ask. It stays `pending` in `asks`, stays in the driver's `/asks/pending`, and the passenger's request stays `asking` with `asking_now` true until the ask expires at the departure time. Re-posting the same journey returns the same stale ask id. The only way out is to cancel the request and post it again.

```
# D has a 3-seat trip; A and B request within a few seconds, both get ask_id
curl -s -X POST $API/asks/$AA/accept -H "Authorization: Bearer $D"
  -> 200 {"booking_id":"..."}
curl -s -X POST $API/asks/$AB/accept -H "Authorization: Bearer $D"
  -> 409 {"error":{"code":"itinerary_changed","message":"your route changed; please reopen the request"}}
psql -c "select status from asks where id='$AB'"   -> pending
curl -s -X POST $API/requests -H "Authorization: Bearer $B" -d <same journey>
  -> 200 {"ask_id":"<the same $AB>","asking":true,...}
```

Expected: accepting the first ask either leaves the others acceptable (re-verify against the new itinerary, as `Verify` already does outside the transaction) or retires them and asks the passenger's next candidate at once. Actual: a dead offer on the driver's phone and a passenger waiting for nothing. This is the two-passenger case in `2026-10-17-next-brief.md` section 2; a Go test with two accepts would have found it. The script recovers by cancelling and re-posting B.

### 2. Chat socket outlives the seat (important)

`handleTripSocket` checks participation once, when the socket is opened, and then subscribes to the hub for the occurrence. `Hub.Publish` fans out to every subscriber of the topic. Nothing closes or filters a subscriber when the booking is cancelled, the driver cancels it, the account is deleted or suspended, or the access token expires.

```
node ws.mjs ws://127.0.0.1:8188/v1/trips/$OCC/socket $B &      # B, booked: OPEN
curl -X POST $API/trips/$OCC/messages -H "Authorization: Bearer $A" -d '{"body":"before-B-left-MARK"}'
curl -X DELETE $API/bookings/$BB -H "Authorization: Bearer $B"      # 204, B's REST access now 404
curl -X POST $API/trips/$OCC/messages -H "Authorization: Bearer $A" -d '{"body":"after-B-left-SECRET"}'
  -> B's socket log: FRAME {"body":"before-B-left-MARK",...} FRAME {"body":"after-B-left-SECRET",...}
```

Expected: after the cancel, B's socket is closed (or the frame is not sent). Actual: B reads the chat of a car he left, including later messages between the driver and the remaining passengers, for as long as the socket stays up (a 75 second idle test showed it does). The same hole covers a passenger removed by a driver cancelling the whole departure, and a suspended user. The REST endpoints answer 404 correctly (checked), so only the socket is affected. A stranger cannot open a socket (404 at upgrade, checked).

### 3. Phone-change as a registry oracle (important)

`PhoneChangeService.Request` checks "is this number taken" (line 109) before it applies the per-account attempt limit (line 141), so asking for a registered number costs the caller nothing and returns 409 `phone_unavailable`, while a free number returns 200 with a challenge (and counts against the limit). Any signed-in user can therefore test any phone number for an account, without limit. `handleOTPVerify` was written specifically so that the endpoint cannot be used for this ("the lookup happens after the code is verified, never before"); this route undoes that.

```
curl -s -X POST $API/me/phone/request -H "Authorization: Bearer $A" -d '{"phone":"+306900003001"}'
  -> 409 {"error":{"code":"phone_unavailable","message":"that number cannot be used"}}      # D's number
curl -s -X POST $API/me/phone/request -H "Authorization: Bearer $A" -d '{"phone":"+306900009998"}'
  -> 200 {"challenge_id":"...","dev_code":"..."}          # free (or 429 once that number was tried a few times)
# 12 further probes of registered numbers: 12 x 409, no 429
```

Expected: the same answer for a taken and a free number (answer 200 and send nothing for a taken one, and let the confirm step fail), or at least count the taken case against the attempt limit. Actual: unlimited enumeration of who uses the app, by phone number, for the price of one account. Worth fixing before a public launch; in a small pilot the audience is a stalker checking whether a person is on the app.

### 4. Raw database errors reach the client (minor)

`handlePostMessage` and `handleBlockUser` return `err.Error()` for any error from the store.

```
curl -s -X POST $API/trips/$OCC/messages -H "Authorization: Bearer $A" -d '{"body":"nul:\u0000:end"}'
  -> 400 {"error":{"code":"invalid_request","message":"store: posting message: ERROR: invalid byte sequence for encoding \"UTF8\": 0x00 (SQLSTATE 22021)"}}
curl -s -X POST $API/users/00000000-0000-4000-8000-000000000000/block -H "Authorization: Bearer $S"
  -> 400 {"error":{"code":"invalid_request","message":"store: blocking user: ERROR: insert or update on table \"blocks\" violates foreign key constraint \"blocks_blocked_id_fkey\" (SQLSTATE 23503)"}}
```

Expected: a fixed sentence (the other handlers do this: "the report was rejected"); NUL rejected as an invalid message; blocking a missing user 404. Actual: table, constraint and SQLSTATE in the response, and a 400 for what is really a missing user. The other `err.Error()` sites (`driver_handlers.go:49`, `garage_handlers.go:89`, `moderation_handlers.go:220`, `route_handlers.go:245`) only echo validation errors and are fine.

### 5. Rating tag unbounded (minor)

```
curl -s -X POST $API/trips/$OCC/rate -H "Authorization: Bearer $A" -d '{"stars":4,"tag":"<40000 x>"}'
  -> 201          # ratings.tag is text, 40000 characters stored
```

`tag` is a free `*string` straight into `ratings.tag`. Expected: a length limit (and ideally the closed set of tags the app offers). Actual: 64 KB per rating of arbitrary text in a table moderators and the data export read. Stars (0, 6, -1, "5", 2.5), double rating (also 5 in parallel), rating yourself, rating a passenger who was never aboard or whose booking was cancelled, and rating before both sides confirmed were all refused correctly.

### 6. A co-passenger sees what the other pays (minor)

```
curl -s $API/trips/$OCC -H "Authorization: Bearer $B" | jq '.passengers[]|select(.name=="Επιβάτης Α")|{price_eur,seats,bio,rating}'
  -> {"price_eur":3.34,"seats":1,"bio":null,"rating":null}
```

Pickup and drop-off of A are correctly hidden from B (`pickup: null`), as are phone, booking id and receipt. `price_eur` and `seats` of each passenger are in the same map and sent to everyone on the trip. Expected: price and seat count only to the driver and to that passenger, the way pickup is. Actual: B sees what A pays for her seat. Low impact (prices come from a published formula), but it is the one per-passenger field that is not gated like the others.

### 7. 403 versus 404 versus 409 (minor)

UUIDv4 ids are not guessable, so this is consistency rather than exposure.

```
POST /asks/<D's ask>/accept as E            -> 403 not_yours
POST /asks/<missing id>/accept as E         -> 409 ask_closed "this request has already been answered"
DELETE /bookings/<B's booking> as S         -> 403 not_yours
DELETE /bookings/<missing id> as S          -> 404 not_found
```

Expected: the same answer for a thing that is not yours and a thing that does not exist (the request, trip, route and alternatives routes already do this, and say so in comments). Actual: three routes answer differently, and the missing ask says "already been answered", which is wrong.

### 8. Expired ask answers `ask_closed` (minor)

The code has both `ErrAskExpired` ("this request has expired") and `ErrAskNotPending` ("already answered"). Once the sweeper has flipped the row to `expired`, `Accept` takes the `preState != "pending"` exit at line 146 and answers `ask_closed`; only inside the window between the deadline and the sweep is `ask_expired` returned. Repro: leave an ask pending past the departure, accept it a minute later: 409 `ask_closed`, `asks.status='expired'`. The app shows a different sentence for each, so a driver who answers a late push is told the passenger was answered by somebody else. Expected: map `status='expired'` to `ErrAskExpired`.

### 9. `X-Real-IP` trusted from the client (minor)

```
for i in $(seq 62); do curl -s -o /dev/null -H 'X-Real-IP: 10.99.99.9'  $API/shared-routes/zzz; done   # last: 429
curl -s -o /dev/null -w '%{http_code}' -H 'X-Real-IP: 10.99.99.10' $API/shared-routes/zzz              # 404, same TCP peer
```

`perIP.middleware` prefers `X-Real-IP` over `RemoteAddr`. Behind `deploy/Caddyfile` (`header_up X-Real-IP {remote_host}`) it is overwritten and safe. It is unsafe on any directly reachable listener (`scripts/dev-lan.sh`, this QA API on `:8188`), and with `deploy/vps/Caddyfile`, which takes `Cf-Connecting-Ip`, if the origin is reachable without going through Cloudflare. The same middleware guards `POST /auth/otp/request`, so a spoofed header also removes the 20-per-hour SMS-pumping cap. Expected: trust the header only from configured proxy addresses.

### 10. Access token survives logout (minor)

Documented in `token.go` ("an access token cannot be revoked") and the 15 minute lifetime is the mitigation. Listed because the checks show it: after `POST /auth/logout`, and after a replayed refresh token burns the whole family, `GET /me` with the old access token still answers 200 until `exp`. Suspension and deletion are not affected (the middleware reads `users.status` on every request; deleted gives 403 `account_closed`, checked). Expected at most: say so in the logout response, or shorten the TTL for sensitive routes.

### 11. Position row survives the cancelled seat (minor)

`Cancel` returns the seat but does not delete the passenger's row in `trip_live_positions`; only the janitor (rows older than an hour) does. The row is not served: the driver's `/live` joins on a confirmed booking and B's own routes answer 404 (both checked). Expected: delete the row in the same transaction as the cancel, as the `live` package comment promises ("the row goes when the person stops sharing"). Actual: B's last latitude and longitude stay in the table for up to an hour (`select count(*) from trip_live_positions where occurrence_id=… and user_id=<B>` gives 1 after the cancel).

### 12. Missing rate limits (minor, noted as asked)

Measured with at most 100 requests, no 429 anywhere:

- `GET /v1/places?q=…&limit=50`, unauthenticated: 10 requests, 113 ms. A linear scan of 12k rows per call, anonymous.
- `POST /v1/places/near`, unauthenticated: 5 requests, no 429.
- `POST /v1/estimates` (an OSRM route per call; the limiter on the router covers only publishing): 10 requests, 136 ms, no 429.
- `PUT /v1/trips/{id}/position`: 15 uploads in 148 ms all accepted (204). The service stores one per 5 seconds but still runs two queries and a transaction per call.
- `POST /v1/me/phone/request` for a registered number (finding 3).

Limiters that were exercised and work: the per-address one on the shared-route pages (429 on the 61st request), and the OTP attempt cap (five wrong codes, then the right one is refused). The `/supply`, chat, publishing and report limits were not hammered (budget of 100 requests).

### 13. Chat text is not normalised (minor)

Names, labels, bios and handles go through `Normalise*` (NFC, no control or format characters, length in runes and bytes). Chat bodies do not:

```
curl -s -X POST $API/trips/$OCC/messages -H "Authorization: Bearer $A" -d '{"body":"<script>alert(1)</script> ‮ gnirts"}'
  -> 201 {"body":"<script>alert(1)</script> ‮ gnirts", ...}      # stored raw (checked in the table), echoed raw
```

A message can carry a right-to-left override or zero-width characters, so a line can render reversed or hide text from a reader. Markup is stored raw everywhere (labels, names, bio, chat); the clients must escape. This is by design for an API, but the web build of the app is the place to check. The length limit is `len(body) > 2000` on bytes, so a Greek message is cut at 1000 characters. There is no contact-detail scan on chat as there is on the bio, and the thread is one shared thread for the driver and all passengers: a phone number typed by A to the driver is read by B (checked; the script only notes it).

### 14. `DELETE /me` with a bare access token (minor)

```
curl -s -o /dev/null -w '%{http_code}' -X DELETE $API/me -H "Authorization: Bearer $Z"     # 204, no body, no OTP
```

The deletion works correctly (the account is anonymised, name and phone erased, the same token then answers 403, the refresh token 401). It is irreversible and needs no second factor, so a token lifted from a lost unlocked phone (or from finding 10) is enough. Expected for an irreversible act: a fresh OTP or a confirmation challenge.

## Things that held up

Each of these is a check in the script and passed.

IDOR and authorization (S = stranger, E = other driver, B = other passenger, A = passenger):

- `GET /asks/{id}` as S, E, B: 404. `accept` as E, A, S: 403. `decline` as S, A, B: 403. The ask stays pending. E's and S's `/asks/pending` do not carry it.
- `GET /requests/{id}/alternatives`, `DELETE /requests/{id}` as S, B, D: 404, request untouched; `/requests/mine` lists only the caller's. There is no `GET /requests/{id}` or `GET /bookings/{id}` (405).
- `DELETE /bookings/{id}` as S, A, E, P3 on B's booking: 403 and the booking stays confirmed. `POST /bookings/{id}/repeat` and `/again` as other users: 403 or 404.
- Arrangements: approve, decline and cancel as S, B, E: 403; A cannot approve her own; other users' lists do not carry it.
- Trips: GET, messages (read and post), live, position, confirm, rate, cancel as S, E, P3 (never aboard) and as B after cancelling: 404. A passenger cancelling the departure: 404. A, S, E starting, adjusting or ending D's drive: 404; D tapping «Είμαι εδώ»: 404; the drive state was untouched.
- Routes: E editing, retiring, sharing, un-sharing or nudging D's route: 404, and D's route stays active. E publishing in D's `vehicle_id`: 400; into D's group: 403; E retiring D's car or making it her default: 404; E's garage does not show D's plate. S rotating, reading stats of and exporting D's group: 404. A normal user on `/moderation/*`: 403.
- Missing trip and stranger's trip give the same 404.

Data exposure:

- No phone number anywhere except `GET /me` of the owner: not in B's view of the trip, the live view, the ask list, B's export, the share link, the live map, the arrangement list.
- A's pickup and drop-off are `null` for B and visible to D and A. B's own pickup is returned to B.
- Live positions: A never sees B's and B never sees A's (checked by coordinates and ids); both see the driver; the driver sees both; after B cancels the driver no longer sees B and B gets 404.
- `/live/map`: items have only `from`, `to`, `seats_free`, `polyline`; no driver id, plate, passenger id or street label (settlement names are derived from the ends).
- Supply answers only `"status": "several"|"unknown"`. `/demand` has no ids. Shared-route JSON and page carry no driver id, plate or passenger; a driver's markup in a label is escaped on the page.
- Photos are re-encoded server side (HTML and SVG as a photo: 400) and need a token. Any signed-in user who knows a user id can fetch that user's photo; the id is exposed only to people on a trip with them, so this is by design.

Live location abuse:

- Latitude 91, `"NaN"`, missing latitude, negative accuracy: 400. `observed_at` from 2020 or five minutes ago: 400. `observed_at` one day ahead: accepted but stored clamped to the server clock (no row with `observed_at` in the future). Upload on a trip you are not on, on another driver's trip: 404.

State machine and validation:

- Cancel a booking twice (and five at once): 204 each time, `seats_free` rises once. Cancel after completion: no change to the completed booking. Driver cancelling a departure that has left: 409.
- Seats 0, -1, 5, 100, 1.5, `"2"`: 400. A party of four against a three-seat car with one seat free asks nobody. A third party against two aboard is not asked. The ask of a third party is refused 409 at accept time (finding 1 hides which reason).
- `window_end` before, equal to `window_start`; window in the past; a year ahead; origin equal to destination; coordinates swapped, in Paris, at 1e308: all 400. Unknown luggage, `luggage_count` 99, "none" with a count, `passenger_id` or `status` smuggled in the body: 400.
- Labels: 70 KB (body limit), 500 characters, RTL override, NUL, zero-width space: 400. Names: RTL override, NUL, 300 letters: 400. Bio with a phone number: 400. `PATCH /me` with `status`, `phone`, `id`: 400.
- Rating: see finding 5 for the one gap; stars out of range, self, double, parallel, never-aboard, cancelled passenger and before confirmation all refused. Confirm and rate by S or the cancelled B: 404.
- Declined ask accepted: 409. Cancelled ask accepted: 409. Accepted ask accepted again: 409. Expired ask: 409 (finding 8).
- Third party past the cap of two gets 409, no 5xx.

`alternative_of` (new): another day joins the root, never chains (no row points at a row that has a parent); the same day again returns the original request; naming the root through its child (A→B then B→A) returns the root; someone else's request or a request id that does not exist: 404 (no difference); one's own matched request: 409; a cancelled root: 409; the eighth live day: 409 `too_many_days`, also with eight posts at once (at most seven live); `"not-a-uuid"`, `""`, `12`, `[]`: 400; the nil uuid: 404; an upper-case uuid is parsed as the same root. A live child of a cancelled root still accepts new days (201); harmless, noted.

Concurrency:

- Ten identical `POST /requests` at once: nine 200 and one 201, one request id, one open row.
- Five accepts of one ask at once: one 200, four 409, one booking, `seats_free` down by one.
- A cancels-versus-accepts race (C cancels while E accepts): cancel 204, accept 409, request `cancelled`, no booking; the two always agreed.
- Five cancels of one booking at once: one seat returned.

Authentication:

- No token, garbage, appended junk, empty bearer, Basic scheme, `alg: none`, payload swapped to another user's id with the old signature, a bad signature: all 401.
- Refresh once: 200. Reusing the spent token: 401, and the successor dies too (family revoked). Malformed `attempt_id`: 400. After logout the refresh token is dead.
- OTP: after five wrong codes the correct code is refused.
- Deleted account: access token 403 `account_closed`, every route refused, refresh token 401, the row anonymised.
- Suspended users: not driven (needs a moderator or a database write). The middleware reads `users.status` per request, and `handleOTPVerify` and `Refresh` both check it; covered by existing tests.

## Not tested

- Suspended-user behaviour, moderator decisions and the suspension cascade (needs a moderator account).
- Expired-ask accept inside the window between the deadline and the sweeper (the sweeper ran within a second in every run).
- Real SMS, push delivery, the Expo web build, the OSRM matching limits, groups beyond the IDOR checks.

## How to rerun

```
API=http://127.0.0.1:8188/v1 \
DSN='postgres://carpool:carpool@localhost:5433/qa1017api?sslmode=disable' \
  scripts/qa-adversarial.sh
```

Each run signs up nine phones in `+306900003000…+306900003099` (a counter in `/tmp/qa-adversarial.state` cycles through them) and spreads sign-ins over `127.0.0.10…250` with `curl --interface` to stay under the 20 codes an hour per address. The trip leaves four minutes after the start (`LEAD_MIN`); the script waits for it to be over before the confirm, rate and expired-ask checks. After about eleven runs the phones repeat and earlier runs' accounts are reused. Exit status is the number of FAIL lines.
