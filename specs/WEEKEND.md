# «Απόδραση το Σαββατοκύριακο»

A passenger who leaves town most weekends says so once. Each week the server
asks drivers on that road, the driver says yes first, and only then is the
passenger told. Decided in docs/qa/2026-10-17-next-brief.md §5.

## What the passenger sets once

A **weekend plan**:

- origin and destination (points with labels, as a request has);
- the outbound day and time, «από Παρασκευή 15:00» (`depart_dow`, `depart_time`,
  Athens time; 5 = Friday, 6 = Saturday, 0 = Sunday);
- optionally a return day and time (`return_dow`, `return_time`);
- seats and luggage, as on a request;
- `every_week` (true) or this weekend only (false).

## What the server does each week

**Generation.** A job runs hourly and, for each active plan without requests for
the coming weekend, creates them. The coming weekend is the Friday to Sunday
that has not ended yet (on a Saturday it is this weekend). Outbound: one request
on the outbound day, window from `depart_time` to 23:59 Athens, and one request
per later day of the same weekend up to Sunday, window 06:00–23:59, each an
`alternative_of` the first. Accepting any day withdraws the others, as for any
group of days. Return, when set: one request on `return_dow` from `return_time`
to 23:59. Requests carry `weekend_plan_id` and `weekend_of` (the Friday's date)
and are generated once per plan, weekend and direction (unique index).

**No ordinary matching.** The matcher does not ask drivers about a weekend
request (`AskNext` answers `ErrNoCandidates` for one). The driver decides first,
from the publish screen, so nobody is asked a question the passenger has not
yet seen the answer to.

**The driver's offer.** After a publish, for each departure of the route that
falls on a weekend, the app asks `GET /v1/trips/{occurrence}/waiting`: open
weekend requests that this departure could carry, by the same gross filter the
matcher uses (`match.Gross` with the occurrence fixed). The screen says «2
άτομα περιμένουν σε αυτόν τον δρόμο την Παρασκευή. Τους παίρνεις;» with «Ναι»
per passenger. «Ναι» is `POST /v1/trips/{occurrence}/offers {request_id}`: an
ask with `offered_by = 'driver'`, quoted like any other (detour, price), that
lives until two hours before the departure or 24 hours, whichever is sooner.
The passenger gets a push.

**The passenger's answer.** `GET /v1/offers` lists pending driver offers on the
caller's requests. `POST /v1/offers/{ask}/accept` books the seat through the
same accept path a driver's acceptance uses (all its checks under its locks).
`POST /v1/offers/{ask}/decline` declines it and leaves the request open for
another driver.

**Thursday 18:00.** A job sends each active plan with an unmatched request for
this weekend a push «Φεύγεις αυτό το Σαββατοκύριακο;». The app shows the same
question on the home screen from Thursday 18:00 until the outbound window
opens. `POST /v1/weekend-plans/{id}/this-week {"going": true|false}`:

- `true`: nothing changes; `declines_in_row` goes back to 0.
- `false`: this weekend's requests are cancelled (any live offers with them),
  `declines_in_row` goes up by one. At 3 the plan says `ask_to_stop: true` and
  the app asks «Να το σταματήσω;».

A plan with `every_week = false` becomes inactive once its weekend is over.

## What it does not do

No background location to guess whether the passenger is in town: intrusive,
costs battery, needs Google Play's special review, and a wrong guess cancels a
real trip. The Thursday question gives the same answer with consent.

## API

| Method | Path | Who |
|---|---|---|
| POST | /v1/weekend-plans | passenger: create |
| GET | /v1/weekend-plans | passenger: own plans, with `this_week` state and `ask_to_stop` |
| DELETE | /v1/weekend-plans/{id} | passenger: stop the plan, cancel its open requests |
| POST | /v1/weekend-plans/{id}/this-week | passenger: Thursday answer |
| GET | /v1/trips/{occurrence}/waiting | driver of the occurrence |
| POST | /v1/trips/{occurrence}/offers | driver of the occurrence |
| GET | /v1/offers | passenger |
| POST | /v1/offers/{ask}/accept, /decline | passenger who owns the request |
