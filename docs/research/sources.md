# Outside sources the pricing and the legal texts depend on

Four questions were open. Three now have answers; one of those answers changes
what the app has to say to drivers. Each entry records what was checked and what
was only reported, because the two are not the same and the memo that started
this got caught out on exactly that distinction.

## 1. No statute authorises carpooling, but one penalises the thing next to it

Nothing in this review found a Greek act that permits or licences carpooling, and
none of the sources consulted names one. That is a negative finding from a
non-exhaustive search, not a proof of absence. What the practice rests on is that
absence: a driver making a trip they would make anyway, sharing real costs,
taking no profit, is not carrying passengers for reward.

The provision on the other side of that line is real and is the most consequential
fact in this file. It is also the one this file got wrong on the first pass.

**Άρθρο 18 ν. 1903/1990** (ΦΕΚ Α΄ 142/23.10.1990), which penalises the driver of
an Ε.Ι.Χ. vehicle carrying passengers **με κόμιστρο** where that transport work
is assigned to another operator, and the owner too when they are a different
person.

Read the version before relying on any figure. The chain: repealed by άρθρο 50
ν. 2963/2001, restored by παρ. 3 άρθρου 11 ν. 3082/2002, replaced by παρ. 8
άρθρου 17 ν. 3534/2007, and **amended by άρθρο 36 ν. 4663/2020** (ΦΕΚ Α΄ 30/
12.02.2020, "Διενέργεια μεταφορικού έργου που έχει ανατεθεί σε άλλον φορέα ή
επαγγελματική ομάδα"), which is the operative version.

As it stands, the sanction is a **€3.000 administrative fine imposed by the
police**, not imprisonment. ΔΠρΘεσ 6197/2024 applies it in exactly those terms,
citing "άρθρο 18 ν. 1903/1990, όπως τροποποιήθηκε με το άρθρο 36 ν. 4663/2020".

This file previously said at least six months' imprisonment, from a consolidated
text at poas.gr that stops at the 2007 amendment and a summary of ν. 4530/2018.
Both describe superseded versions. That is the whole reason this section exists:
a stale consolidation looks exactly like a current one.

What still holds:

- It reaches the **owner as well as the driver** when they differ.
- The trigger word is **κόμιστρο**, a fare. Neither statute defines it or
  separates it from sharing costs. That undefined gap is the space this app
  operates in, and nothing found here closes it.
- Whether any criminal limb survives alongside the administrative fine was not
  established, and a lawyer should say. What is established is that the 2024
  enforcement route is a fine.

Consequences for the code:

- The price ceiling is a **risk control we chose**, not compliance with a rule
  and not a safe harbour. No article hands down a number, and keeping the
  suggested contribution under the trip's real cost does not guarantee any
  particular legal characterisation. It is simply the strongest fact available if
  the question is ever asked, which is why the driver-typed consumption and
  `wear_factor` of RISKS 13 carry more weight than their size suggests.
- Never the word **κόμιστρο**, anywhere a user can see it, in any language, and
  never a synonym that means the same thing. MOBILE.md already requires
  "προτεινόμενη συνεισφορά"; this is the reason, and it is a harder rule than a
  style preference. Checked: the word appears nowhere in the app.

Sources, with retrieval date 2 August 2026: e-nomothesia.gr and kodiko.gr for
ν. 1903/1990 (ΦΕΚ Α΄ 142/23.10.1990); taxheaven.gr and e-nomothesia.gr for
ν. 4663/2020 (ΦΕΚ Α΄ 30/12.02.2020); lawspot.gr for ΔΠρΘεσ 6197/2024. The
gazette texts themselves were not read; these are consolidations, which is what
went wrong the first time.

Still unverified, and not to be repeated until someone reads it: a reference to
**ΠΔ 50/2012**, asserted without a citation.

## 2. Motor insurance: the premise was wrong

Five sets of general terms were read directly (Generali, Syndea, Ατλαντική
Ένωση, Ευρωπαϊκή Πίστη, Intersalonica). **None of those five excludes carrying a
passenger who shares costs.** The word κόμιστρο does not appear in any of them.

Five policies are not the market, and what applies to any given driver is their
own policy, their declared vehicle use and their permitted passenger count. The
finding below is that the exclusion everyone assumes is there was not in the
sample, not that no insurer has one.

What every one of them has instead is an exclusion for **use other than the use
declared in the policy and the registration document**. That is a different
thing in three ways that matter:

- It is a right of **recourse against the driver**, not a refusal to pay the
  injured party. Ευρωπαϊκή Πίστη says so outright: the insurer cannot raise its
  exclusions against injured third parties.
- Three of the five (Ατλαντική Ένωση, Ευρωπαϊκή Πίστη, Intersalonica) require
  **αιτιώδης συνάφεια** between that use and the accident. Having had a passenger
  aboard is not enough on its own.
- It turns on whether the private car became, in fact, a commercial one. Which is
  the same question as §1: cost sharing, no profit.

So the honest line to a driver is neither "your insurance may not cover you" nor
"you are covered." It is that the exclusion turns on declared use, that no policy
in the sample named cost sharing, and that they should check their own. Terms §6
now says that, having previously implied an exclusion that the sampled policies
do not contain.

The app's ceiling supports the argument that the car stayed in private use. It
does not settle it, and it is not a guarantee to make to a driver.

Second axis, still open: ν. 2496/1997 on misrepresenting the risk. Somebody doing
four priced trips a day is describing a different vehicle use to their insurer
than the one on the policy. An argument for a frequency limit, not only a price
one.

## 3. Fuel prices: parse the ministry's PDF bulletins, do not scrape the site

`fuelprices.gr` is the source of truth and publishes per-prefecture averages for
Αμόλυβδη 95/100, Diesel κίνησης and Autogas, on working days, at no fixed hour.
It is also behind anti-bot protection and its terms limit use to consumer
information, so scraping the HTML is both fragile and arguably out of bounds. The
data.gov.gr entry points back at it rather than serving the data.

Two community datasets exist. `iMEdD-Lab/open-data` is CC BY-NC, which rules it
out for anything but backfill and cross-checking. `mavroprovato/fuelpricesgr` is
MIT and, importantly, parses the **PDF bulletins** rather than the site, which
sidesteps the anti-bot layer and is the more stable surface anyway.

Shape to build, matching how tolls already work: harvest into a checked-in
dataset, review the diff, ship it. Not a live feed.

**Release gate, not a footnote.** The site's terms limit use to consumer
information and grant no redistribution right, and a checked-in dataset derived
from the bulletins is redistribution. Reading them again will not settle it.
Written clearance from `fuelprices@mindev.gov.gr` is required before any derived
dataset ships, and the exact terms relied on go in this file when it arrives.
That the MIT parser is freely licensed says nothing about the data it parses.

## 4. Vehicle consumption: EEA, and the field is there

`[CO2Emission].[latest].[co2cars]` on `discodata.eea.europa.eu` is the EU
registration monitoring data under Regulation 2019/631. CC-BY 4.0, commercial use
allowed explicitly.

Queried and confirmed, rather than taken on trust:

- The consumption field exists. It is called **`Fc`**, in l/100km, alongside
  `Mk` (make), `Cn` (model), `Ft` (fuel), `Ec` (cm³), `Ep` (kW) and
  `Ewltp (g/km)`.
- **`Fc` is only populated for registration years 2021 and 2022.** Every earlier
  year is null across the whole table. Within those two years coverage is 65–76%.
- Greece is `GR`, 1.21M rows, of which only 2021 carries `Fc` (50,102 rows).

That last point is the catch, and it is not small. The dataset is new
registrations, and `Fc` starts in 2021, while the Greek fleet a pilot will
actually meet is far older. Matching on make and model EU-wide rather than
Greek-only widens it a lot, since a Berlingo burns the same in Spain, but a 2008
car is simply not in this table at all.

The fallback is deriving consumption from CO2, which is present from 2010, **for
petrol and diesel only**. On the rows that carry both, the ratio comes out at:

| Fuel | `Ewltp` ÷ `Fc`, mean | implied g CO2 per litre | textbook |
| --- | --- | --- | --- |
| Petrol | 23.59 | 2359 | 2392 |
| Diesel | 27.35 | 2735 | 2640 |
| LPG | 20.05 | 2005 | ~1665 |

Petrol and diesel land within a few percent, so the derivation is sound for
those. LPG does not. Regulation 2019/631 allows reported consumption in
l/100km, m³/100km **or kg/100km**, which is the likely explanation, and it means
`Fc` cannot be treated as litres without checking the fuel type first.

Rules for whatever gets built:

- Derive from CO2 only for fuels with a validated factor, which today means
  petrol and diesel. No LPG, no NG, no hydrogen.
- Never emit a litre-based figure for electric vehicles, and keep plug-in
  hybrids out until both fuel and electricity are modelled.
- Enforce a plausible band on the ratio as well as on `Fc`; a believable `Fc`
  can still sit next to a mismatched CO2 value.
- Medians, not means. The figures in the table above are means, quoted to show
  the derivation is not wildly off, and must be recomputed as filtered medians
  before anything depends on them.
- An unsupported fuel gets no derived number. It falls through to whatever the
  no-match path is, the same as a car that is not in the table.
- Derived numbers must never be presented as manufacturer figures.
