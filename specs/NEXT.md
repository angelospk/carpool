# What to do next, and why it is not more features

Written 2026-08-09, after a market and regulatory review. The backend and the app
are done: `make check` is green, the twelve-step scenario runs end to end, the
stack fits in 330 MiB. Nothing in this file is a bug report. The finding is that
the next unit of engineering effort is worth less than the next conversation, and
this file exists so that a session picking the repo up cold does not spend a week
building a feature that was never the constraint.

RISKS.md already says it: "Matching tech is worthless without drivers." The review
turned that intuition into three facts.

## Fact 1 — the competitor arrived while this was being built

BlaBlaCar launched in Greece on 30 June 2026, as part of an expansion into 20
countries done with no staff on the ground.

- https://newsroom.blablacar.com/news/blablacar-announces-global-expansion-into-20-new-countries
- https://techxplore.com/news/2026-06-group-blablacar-ai-country-expansion.html

Two consequences, pulling in opposite directions.

Against: the long-distance intercity case — Αλεξανδρούπολη → Θεσσαλονίκη, the
trip somebody makes twice a term — is now taken by a platform with a decade of
liquidity and a brand people already trust. Competing there is competing on the
axis where a new app is weakest, and no amount of better matching wins it.

For: their entry is the strongest available evidence that a cost-sharing platform
can operate in Greece without being treated as a transport operator. It is also
free market education. People will learn the word συνεπιβατισμός from someone
else's advertising budget.

The strategic read is that BlaBlaCar's arrival makes the *daily, in-region*
positioning more correct, not less. It is the segment they are worst at — see
Fact 2 — and the segment this codebase was already built for. What has to change
is the messaging: this is not a smaller BlaBlaCar, it is the thing BlaBlaCar does
not do, which is the same 40 km at 07:40 every Tuesday.

**Confirmed on 2026-07-20, three weeks after the Greek launch: they are leaving
that segment entirely.** BlaBlaCar entered exclusive negotiations to sell
BlaBlaCar Daily, its home-to-work business, to Karos, and is refocusing on
long-distance. The two together will hold about 150.000 monthly active users
across 11 million annual trips.

- https://www.maddyness.com/2026/07/21/covoiturage-blablacar-veut-ceder-sa-branche-dediee-aux-trajets-domicile-travail-a-karos/
- https://www.boursorama.com/actualite-economique/actualites/blablacar-veut-arreter-le-domicile-travail-et-negocie-avec-karos-5b70959ff7515148af0920fbb7230cc7

So the competitor that just arrived in Greece brought the long-distance product
and is in the process of disposing of the commuter one. The segment this repo
was built for is being vacated by the only company with the brand to take it.

Two calibrations from those numbers, both worth holding on to. The daily segment
is small — 150.000 monthly actives is the *combined French market leader* after
years of employer contracts and public subsidy, not a rounding error on the way
to millions. And it is small for structural reasons, which is why an unsubsidised
version of it should not be expected to behave differently.

**The Greek launch has no supply, measured 2026-08-09.** blablacar.gr is fully
live and bookable — localised, with a service fee on bookings — and a search for
Αθήνα → Θεσσαλονίκη, the route their own homepage lists as most popular, returns
"Δεν υπάρχουν ακόμη διαδρομές" both for the next day and for 15 August, the
highest-traffic travel day of the Greek year. Alexandroupoli → Thessaloniki is
likewise empty. The homepage still renders those route names in French
("Athènes → Thessalonique"), which fits an entry made with no staff on the ground.

Do not read that as an opening. It says their cold-start problem in Greece is the
same as this project's, and they can spend against it whenever they choose. It is
useful as calibration, not as a strategy: an empty market is not a free one.

## Fact 2 — nobody has made daily commuter carpooling work peer-to-peer

Every European service in the daily home-to-work segment — BlaBlaCar Daily,
Klaxit, Karos, Ecov — solved cold start the same way, and it was not with product.
They signed employers and local authorities, and rides were subsidised: passengers
paid nothing or near nothing, drivers were paid roughly €1.50–3 per passenger per
trip, and the money came from the employer, the municipality, or a national scheme
rather than from the passenger. The platform's revenue was the partnership
contract, not a cut of the ride.

The consolidation is the tell. Klaxit was absorbed into BlaBlaCar Daily in 2023,
and as of mid-2026 BlaBlaCar was in exclusive talks to sell Daily to Karos. Three
funded companies with real engineering could not run this segment as a standalone
consumer marketplace.

(Reported by Grok's live search, uncited. The pattern is consistent across four
services and matches the public French subsidy schemes — forfait mobilités
durables, the €100 driver bonus — but the per-trip figures should be checked
against a primary source before anyone quotes them at a meeting.)

This is the finding that should change what happens next. A P2P launch in Evros
with no institutional partner is attempting, with one developer, the thing four
funded companies failed at. The matching engine is not what is missing.

## Fact 3 — the densest ready community is on the wrong side of a constraint

The Democritus University of Thrace has 28 departments spread across Κομοτηνή (8),
Ξάνθη (5), Αλεξανδρούπολη (5), Ορεστιάδα (2) and Διδυμότειχο (1), plus Καβάλα and
Δράμα, with more than 15.000 students.

- https://el.wikipedia.org/wiki/Δημοκρίτειο_Πανεπιστήμιο_Θράκης

That is a population that already travels those exact corridors on a repeating
weekly schedule, is young enough to install an app, and can be reached through one
institution instead of one household at a time. It is the single best pilot
community within reach of this project.

And the app cannot serve it. SPEC.md requires at least one endpoint of every trip
inside the home region, and README.md already records the consequence: "Κομοτηνή
is in Ροδόπη. RISKS 15." The rule that keeps the community local and the data
small also cuts the university's own corridor in half.

The constraint was a good decision for the reason it was made — a clipped OSRM
extract is what makes the 330 MiB deployment possible, and RISKS 3 measured that.
But note what the same measurement shows: the Evros extract with a 30 km buffer is
39 MiB serving, against 1.15 GB for all of Greece. Adding Ροδόπη and Ξάνθη to the
clip is tens of megabytes, not gigabytes. The geographic limit is a product
decision wearing an infrastructure costume.

## What follows: the ranked list

### 1. Talk to ΔΠΘ before writing any more code

Not a feature. One meeting, with the university's mobility or student-welfare
office, or whoever runs the shuttle between campuses if one exists. The question
to answer is whether an institution will put its name on a pilot and mail its own
students — because that, not the matching algorithm, is what decides whether this
app has users.

Bring: the app running on a phone with the seeded demo, the €0 marginal running
cost, and the SMARTA2 comparison. Trikala's integrated mobility app cost roughly
€25.000 to develop and €2.000–3.000 a year to maintain under EU funding (reported,
worth verifying). This one is built and runs on hardware that is already paid for.
That is the entire pitch and it is a strong one.

Fall-backs, in order, if the university says no: the Πανεπιστημιακό Γενικό
Νοσοκομείο Αλεξανδρούπολης (shift work is the ideal carpool shape — fixed times,
fixed destination, poor bus coverage at shift-change hours), then Δήμος
Αλεξανδρούπολης under its ΣΒΑΚ obligation, then a large private employer.

**Done when:** one institution has agreed to email its own people, or all four
have said no in a way that says what would change their mind.

### 2. Make the region a corridor, not a prefecture — ALREADY TRUE

**Measured 16 September 2026. Nothing was built, because every layer already
allowed it and the documents had not been told.** Kept here rather than deleted,
because the reasoning below is what somebody will re-derive otherwise.

Each of the four things this item expected to have to change:

- **The places list.** Already the whole country: 12.442 settlements, Κομοτηνή
  and Ξάνθη among them, asserted in `internal/places/national_test.go`. The
  sentence above claiming `internal/places` stops at the Evros boundary was
  stale, and so was the paragraph in README.md that repeated it. Both corrected.
- **The regions.** `REGION_FIXTURE` already defaults to
  `fixtures/regions/greece.geojson` in both compose files — all 75 περιφερειακές
  ενότητες. Κομοτηνή resolves to Ροδόπης, not to nothing.
- **The boundary rule.** Already gone from the code. `request_handlers.go`
  refuses only what is outside Greece, and carries the reasoning at the site:
  the home-region rule refused Θεσσαλονίκη → Αθήνα to somebody living in Ξάνθη
  and had quietly turned a profile field into a permission. SPEC.md still
  described the old rule as if it were live; corrected.
- **The routing extract.** This was the one expected to need a rebuild, and it
  does not. Measured against the *clipped* Evros extract — the one production
  serves, 39 MiB resident — Αλεξανδρούπολη routes to Κομοτηνή in 58,3 km, to
  Ξάνθη in 99,9 km, to Ορεστιάδα in 114,4 km and to Καβάλα in 104,2 km. The
  ~30 km buffer around the prefecture already reaches across Ροδόπη and into
  Ξάνθη, so the whole ΔΠΘ corridor is covered by the artifact already deployed.
  No osmium clip, no re-preprocessing, no new file to ship.

  **But measuring the edge of that coverage is what found the real defect, and
  it is worse than a missing corridor.** A clipped extract does not refuse a
  coordinate it does not hold — it snaps it to the nearest road it does have and
  answers `Ok`. Θεσσαλονίκη snapped 173 km away, Αθήνα 313 km, Πάτρα 408 km, and
  Θεσσαλονίκη → Αθήνα came back as a confident 186,6 km line drawn through
  Thrace. That line is what a published route is stored with, what the gross
  match locates pickups along, and what a contribution is priced from, and
  nothing downstream could tell it from a real answer.

  Fixed by bounding the snap: `geo.SnapRadiusMetres`, 20 km, sitting in the
  measured gap between legitimate places inside the corridor (≤ 6,5 km, Ξάνθη at
  the edge of the clip) and the fabrications (≥ 173 km). OSRM then answers
  `NoSegment`, which publishing and estimating surface as
  `400 outside_service_area`. So the served area is now *defined by the extract*
  and says so out loud, which is the thing this item was really asking for.

**Done, and now asserted rather than incidental.**
`internal/api/corridor_test.go` seeds two prefectures that meet at 25,6°E,
proves Αλεξανδρούπολη and Κομοτηνή land in different ones, then publishes a
route and posts a request across the boundary through the real router and a real
database — and checks that Σόφια → Φιλιππούπολη is still refused with
`outside_country`. The corridor had been working by the *absence* of a check,
which is the state in which a well-meant re-addition of a home-region guard
closes it again silently, months after anybody remembers why it was open.

The 1.15 GB figure for the whole-Greece extract still stands and is still the
reason not to serve it. It simply was never what stood between this app and
Κομοτηνή.

### 3. Verified trips, so somebody else can pay for them — BUILT

**Built 23 September 2026**, on top of groups (an organisation is a group). An
admin pulls `GET /v1/groups/{id}/trips.csv?month=YYYY-MM`: trips both sides
confirmed, where driver and passenger were both members on the day. See
`internal/groups/export.go`. The reasoning below is why.

This is the one genuinely new thing to build, and it is what makes the subsidy
model of Fact 2 reachable without breaking the rule that the app never moves
money.

The app does not need payments. It needs to be able to prove that a trip happened:
both sides confirmed, this driver, this passenger, this corridor, this date. Given
that record, an employer or a university can pay its own people directly, on its
own payroll, against an export. The platform stays a matcher, which is exactly what
keeps it outside the licensed-transport regime that ν. 4530/2018 defines by
"decisive influence" over price and service.

Both halves already exist — mutual confirmation and ratings are in the e2e path.
What is missing is the export: a per-period, per-organisation report of confirmed
trips, with the corridor and the participants' organisational identity, and
nothing else.

**Done when:** an organisation admin can pull a month of confirmed trips for their
own members as CSV, no personal data beyond what the organisation already holds,
and the endpoint refuses everything outside their own membership.

### 4. Guaranteed ride home — BUILT, the information version

**Built 23 September 2026.** A driver cancelling one seat now reopens the
passenger's request, as calling off the departure already did. The request
screen says so, lists the cars still on the road anonymously, and links the
ΚΤΕΛ Έβρου timetable. No taxi voucher: that needs a partner and money.

Cheap, and it removes the objection that kills commuter carpooling: the passenger
who is afraid of being stranded when the driver calls in sick. Every European
service ships some version of it, usually a taxi voucher.

The MVP version needs no partner and no money: when a trip a passenger relies on
is cancelled, the app immediately re-matches them against the remaining
occurrences on that corridor and, failing that, shows the KTEL departure. The
promise is "you will not be told at 07:30 and left with nothing", and it can be
kept with information alone.

**Done when:** cancelling a trip with a confirmed passenger produces an
alternative within one screen, and the cancellation notification carries it.

### 5. The insurance answer, in writing, in the terms

Grok reports that Greek compulsory motor liability cover (π.δ. 237/1986,
codifying ν. 489/1976, minimum €1.300.000 per victim since 2022) covers
passengers, and that genuine cost-sharing does not trigger the transport-for-
reward exclusion, because the vehicle stays in private use. **Uncited, and this
one must not be repeated to a user until it is checked** — it is the first
question any institutional partner will ask, and the docs/research/sources.md
discipline exists precisely because a plausible legal summary was wrong once.

Check against π.δ. 237/1986 as currently in force and the Motor Insurers' Bureau
of Greece. Then say it plainly in the terms, because "am I insured in someone
else's car" is the question that stops people from getting in.

**Done when:** `docs/research/sources.md` has it as a checked finding with the
article, and `docs/legal/terms-el.md` answers it in a sentence a passenger reads.

## Two cheap distribution experiments, on the share that already exists

Added 2026-09-23. The share button (GAPS 1) is the only distribution the app
has, and nobody knows yet whether it distributes anything. What it sends is a
sentence and no link: "Αλεξανδρούπολη → Κομοτηνή · καθημερινές 07:30 · έχω 2
θέσεις", then "Αν σε ενδιαφέρει, στείλε μου μήνυμα." The reply comes back in the
chat where the message was pasted. The recipient has no way to install the app
from it. Both experiments below answer a question to bring to the ΔΠΘ meeting
(item 1) instead of an opinion. Neither one is a feature.

### A. One corridor, posted by hand, counted by hand — no code

Take the corridor with the most reason to exist, Αλεξανδρούπολη ↔ Κομοτηνή (ΔΠΘ
has departments at both ends, 58,3 km, inside the deployed extract). Publish one
real recurring route on it: yours, or that of one driver you know who makes the
trip. Press the existing share button and post into three groups that already
exist: a ΔΠΘ student group for each city and one local Viber or Facebook group.
Post once a week for three weeks and change nothing else.

Count, in a notebook:

- replies per post, and hours until the first one;
- how many replies ask "which app?" versus "is there a seat?";
- rides that actually happen.

Cost: about 30 minutes a week. Nothing to build and nothing to review.

**What it decides.** If nobody replies in three weeks, the problem is supply
meeting demand in these groups, and neither the app nor its text is at fault. Stop
there, and say so at the meeting. If people reply "is there a seat?", the
sentence works and the demand is real. If they reply "which app?", the missing
piece is B.

### B. An install line and one counted URL — about an hour of code

Only worth doing if A produces "which app?" replies. Add a third line to
`shareRouteMessage`: a short URL on the domain Caddy already serves, e.g.
`https://{$DOMAIN}/get`. The Caddyfile gets one `redir` from that path to the
APK or TestFlight link. Caddy already writes every request to its log, so
counting the hits needs no analytics and stores no identity beyond what the log
already holds.

Run it as a before-and-after on the same groups as A. Keep the same post, the
same weekday and the same time, and add only the line. Count `/get` hits per post
from the Caddy log. Then count new accounts that file a request on that corridor
within a week, from `ride_requests` grouped by `passenger_id`.

Keep the message honest. The URL installs the app. It does not open a listing,
because no listing exists. The line must say "κατέβασε την εφαρμογή", not "δες τη
διαδρομή", and the test in `shareRoute.test.ts` should pin that down.

**What it decides.** If installs rise and requests follow, the share becomes a
loop, and GAPS 4's demand line gets the three distinct people its floor needs
before it can say anything. If installs rise and requests do not, the app loses
people between install and first request, and that becomes the next thing to
look at. If nothing moves, the one-line text was never the problem.

### Not these, yet

Referral codes, invite rewards and a public web listing of routes. Each needs
backend, and the listing needs a privacy decision (GAPS 4 set the floor at three
people for a reason). None of them is worth building before A says the channel
has anyone in it.

## What not to do

Do not build in-app payments. The €3.000 fine of άρθρο 18 ν. 1903/1990 and the
"decisive influence" test of ν. 4530/2018 both point the same way, and the cap
that stops a full car turning a profit is the safest structural position
available. Every partner model above works with the app moving no money at all.

Do not raise the two-party occurrence cap yet. RISKS 1 says decide with usage
data, and there is no usage data.

Do not add features to win against BlaBlaCar on intercity. That comparison is lost
and does not matter.

Do not copy their free pricing either, because it is not free. BlaBlaCar suggests
a per-km contribution and caps how far a driver may move it, and its terms forbid
taking any profit — the same structural position this app takes with its cap. A
driver setting their own fare is not a feature to match; it is the thing that
would make both of us transport operators.

## Where the facts came from

Live search via Grok on 2026-08-09, five queries: Greek carpooling law, the Greek
market, European commuter cold-start, motor insurance, and Greek mobility funding.
The BlaBlaCar Greece launch and the ΔΠΘ figures were verified independently
through web search; everything else in this file is Grok-reported and uncited,
and is marked as such where it matters. The legal findings on άρθρο 18 added
nothing to `docs/research/sources.md`, which was already more precise.
