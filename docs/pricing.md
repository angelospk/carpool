# How a price is arrived at, and why it is arrived at that way

Read this before changing anything that touches money. Most of what follows
looks like over-caution until you know what it is protecting against, and the
two bugs already found here both looked like fair arithmetic.

## The one sentence

A driver of a private car who carries passengers **με κόμιστρο** is fined €3.000
under άρθρο 18 ν. 1903/1990, as amended by άρθρο 36 ν. 4663/2020, and so is the
owner if they are a different person. No statute defines κόμιστρο or separates
it from sharing costs, so the only thing the driver can actually point at is
that they did not come out ahead.

Everything below exists to make "did not come out ahead" true by construction
rather than by good intentions. `docs/research/sources.md` has the provision,
its amendment history and what is still unsettled.

## The formula

```
cost    = consumption/100 × fuel_price × trip_km × wear_factor
per_pax = floor(cost / (offered_seats + 1), 0.50 €) + toll_share
```

Four inputs, and the history of this file is the history of drivers being able
to move three of them.

### The divisor is the whole car, not who is aboard

It used to be the passengers already booked, plus the new one, plus the driver.
Prices freeze at accept, so seats sold one at a time went cost/2, cost/3,
cost/4. Each figure looks like a share. On a trip costing €12.60 a four-seat car
collected €16.00, and the driver was 27% ahead of their own fuel.

Now it is every published seat plus the driver, and it does not move as the car
fills. `n` seats at `cost/(n+1)` is `n/(n+1)` of the cost: short of it by
construction, for every `n`.

The cost of that: a driver who fills one seat of four recovers a fifth of their
fuel rather than half. A driver who wants a larger share offers fewer seats,
which is honest — fewer seats genuinely does mean each passenger covers more.

### Rounding goes down

`math.Round` to the nearest 50 cents was leaking in the same direction more
quietly: €0.63 became €1.00. It floors now.

There is a subtlety worth keeping. Cents are settled **before** flooring,
because the division is binary floating point and a share of exactly €1.00
arrives as 0.9999999, which floors to €0.50.

A consequence to accept rather than fix: a share under 50 cents is suggested
free. The smallest figure the app can express is 50 cents, and quoting that for
a 32-cent share is asking half again as much as the fuel. Free is accurate. Do
not add a minimum charge.

### wear_factor is pinned at 1.0

It allowed a driver to multiply the whole fuel cost by up to 1.5. Nothing in the
API ever wrote it, so pinning it was a constraint rather than a data migration —
which was exactly the argument for doing it while it was still free.

If it ever comes back it should come back a different shape. Wear is roughly
fixed per kilometre; tying it to the fuel bill makes it swing with the price of
diesel and with how thirsty the car is, neither of which has anything to do with
tyres. An additive euro-per-kilometre rate that the app sets, not a multiplier
the driver sets.

### consumption comes from the car

This is the largest piece and has its own section.

## Where consumption comes from

`internal/vehicles`. Three sources, tried in descending order of how much each
knows about this particular car. Every answer records which one it was, because
the figure freezes into a booking and has to stay explicable afterwards.

### 1. The dataset

EU registration monitoring under Regulation 2019/631, published by the EEA under
CC-BY 4.0 with commercial reuse allowed explicitly. `scripts/harvest-vehicles.py`
builds `internal/vehicles/data/eea.csv`: 2.761 entries over 65 makes, keyed on
make, model, fuel class and a 100 cm³ displacement band.

Things learned building it, all measured rather than assumed:

**The consumption column only exists for 2021 and 2022 registrations.** Every
earlier year is null across the whole table. This is the fact that shapes
everything downstream: the dataset describes recent models, and the Έβρος fleet
is mostly older. An unmatched car is the normal case, not an edge case.

**Model alone is far too coarse.** The Toyota Yaris as one "PETROL" group runs
from 2.8 to 11.1 l/100km, because 44.000 hybrids sit on top of 32.000
conventional cars. The `Fm` column separates hybrids from mono-fuel; the
displacement band separates most of the rest.

**Petrol and diesel only.** Regulation 2019/631 permits consumption to be
reported in l/100km, m³/100km *or* kg/100km depending on the fuel. LPG did not
match the physical CO2-per-litre constant at all, which is the tell that its
unit is different. Plug-in hybrids are excluded for a different reason: their
consumption is not a property of the model, it depends how much of the trip ran
on the battery.

**Make aliases are an explicit list.** `VW`, `VOLKSWAGEN` and `VOLKSWAGEN, VW`
normalise to three different keys and split one manufacturer three ways without
ever colliding, so normalisation alone cannot find them.

**Collisions are notes, not blockers.** A merge that is actually wrong joins two
different cars, which widens the group's quartiles, which drops it at the
dispersion gate. The defence is downstream and real, so flagging every
`MERCEDES-BENZ`/`MERCEDES BENZ` fold as a problem would only train the reader to
skip the section.

The gates are `n >= 50` registrations and an interquartile range no wider than
30% of the median. Only 17 groups failed dispersion, which says the key is
tight; 5.336 failed the registration floor, which is the long tail of trim
variants.

### 2. The CO2 on the driver's own document

Greek registration certificates carry CO2 at **field V.7**, and have done for
every car first registered from 1 November 2010. That is a number the driver
reads off their own paper — not a search, not a judgement, and specific to their
car rather than to a model.

The conversion is measured, not taken from a textbook: the median of
`Ewltp ÷ Fc` over 9.0 million petrol and 4.3 million diesel registrations.

| Fuel | g CO2 per litre | quartiles | textbook |
| --- | --- | --- | --- |
| Petrol | 2270 | 22.6–22.9 | 2392 |
| Diesel | 2630 | 26.1–26.5 | 2640 |

The quartiles are that tight because the relationship is very nearly
deterministic — burning a litre of a given fuel releases a fixed amount of CO2,
and the test cycle scales both numbers together.

Diesel lands 0.4% off the textbook. Petrol lands 5% below it, and the likely
reason is that European pump petrol is E10: ethanol carries less carbon per
litre, and 90% petrol with 10% ethanol works out around 2300. Measuring beat
assuming, so the measured figures are what ship.

**The NEDC caveat, deliberately uncorrected.** A pre-2021 document carries an
NEDC figure, and NEDC understates real consumption by roughly a fifth. Dividing
it by these constants gives the NEDC consumption, so an old car is
under-estimated. That is the safe direction, and it is left alone, because a
correction factor would be a number nobody measured.

### 3. The class floor

When the model is unknown and the document has no CO2 — a car older than
November 2010 — the app falls back to the **10th percentile** of the models in
that fuel and displacement class.

Tenth, not median, and the direction is the whole point. A car absent from the
dataset is almost certainly older than the dataset, and older cars drink more,
so any figure taken from modern cars already understates it. A low percentile
understates it further, on purpose.

It is computed from the shipped dataset at load rather than written down, so a
dataset refresh moves the floor with it instead of leaving a stale constant.

The figure the floor replaced is worth remembering. `fuel_consumption_l_100km`
defaulted to a flat **7.0**, and a small petrol car drinks about 5.6. Every one
of them was quoting a quarter more than it burnt, silently, by default. There is
no neutral default; there is only a default that errs one way or the other.

### How a driver gets a profile at all

`PUT /v1/me/driver`, which is new: before it there was no way to become a driver
through the API, and `scripts/e2e.sh` inserted the row with psql.

The body carries make, model, fuel, displacement, the V.7 CO2 if the document
has one, plus colour and plate. There is no field for consumption, and the JSON
decoder rejects unknown fields, so a client that sends one gets a 400 rather
than being quietly ignored. That distinction matters: silently discarding the
field is a bug waiting for somebody to re-add it without knowing what it was
for.

`GET /v1/vehicles/makes` and `/v1/vehicles/models?make=X` back the picker.
An unknown make returns an empty list rather than a 404, because the app's next
move is to ask for the CO2 figure, not to tell the driver their car does not
exist.

### The receipt

Every price is stored with a `pricing_basis` — a jsonb record of every input the
formula used, plus the unrounded cost and the answer. It rides from the ask to
the booking, and through a standing arrangement to each week it generates.

It exists because a booking used to store only `price_eur`, and every input
lives somewhere that changes: the driver swaps cars, the vehicle dataset is
refreshed, the region's fuel price is edited, the route is re-snapped. "Why did
I pay €3.50 in March" was unanswerable, and any answer given a year later would
have been a reconstruction rather than a record.

It carries a `v`. Bump `domain.BasisVersion` whenever the arithmetic changes,
or a basis written before the change replays through the new formula, disagrees
with the price beside it, and looks exactly like a bug.

Rows that predate this carry a null basis. Filling them with today's inputs
would turn a gap into a plausible fiction, so they stay null.

## Why not just ask, or search

Two options that keep coming up, and why neither is here.

**Letting the driver type the number.** This is what the app did. A driver
entering 14 instead of 7 asks for twice the money and the passenger has no way
to tell. It is also the input the €3.000 provision cares about most.

**Looking it up on the web when a car is unknown.** For a 2008 model the web
returns forum posts, magazine tests and marketing figures. None is structured,
none is authoritative, and the app would be picking one. The same reasoning
rules out asking a language model: three times while this work was being done, a
confident and wrong answer got as far as being written down, and each was caught
by checking a primary source. Here the number would become money instead.

There is one job a model can do safely, and it is a different job: reading a
photographed document into structured fields. That is extraction, and it is
verifiable against the paper. It is not a judgement about consumption. Note that
it also drags in personal data — a registration certificate carries a name, an
address and a chassis number — which is why the current design has the driver
type the CO2 figure rather than upload the document.

## What is still open

**An unverified car does not yet price differently from a verified one.**
`consumption_source` and `Assessment.Verified()` record the difference and the
figure is already lower for an unverified car, but nothing refuses anything on
that basis. The open question is whether a `band` profile should also be capped
or barred from publishing until reviewed.

**The profiles that predate all this are marked, not fixed.** Every driver
profile created before migration 00009 carries `consumption_source = 'legacy'`
and the flat 7.0. They are deliberately not re-priced, because changing a
number without changing what the driver told us would be inventing data.
Re-assessing them is a person's job.

**Fuel price is still one number per region, updated by hand.** It now carries
`fuel_price_updated_at`, stamped only when the figure actually moves, so a stale
price is at least visible rather than silent. Automating it is gated on a
licence question in `docs/research/sources.md` — though see the note there:
ν. 4727/2020 makes public sector data open by default and requires exclusions to
be specifically justified, so the position is stronger than a reading of the
site's terms alone suggests.

**Never print the word κόμιστρο.** In any language, in any surface a user can
see, and not a synonym either. MOBILE.md already requires "προτεινόμενη
συνεισφορά". This is the reason it is a hard rule rather than a style
preference. It currently appears nowhere; keep it that way.
