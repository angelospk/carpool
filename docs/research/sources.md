# Outside sources the pricing and the legal texts depend on

Four questions were open. Three now have answers; one of those answers changes
what the app has to say to drivers. Each entry records what was checked and what
was only reported, because the two are not the same and the memo that started
this got caught out on exactly that distinction.

## 1. No statute authorises carpooling, but one criminalises the thing next to it

There is no Greek act that permits or licences carpooling. What holds it up is an
absence: a driver making a trip they would make anyway, sharing real costs,
taking no profit, is not carrying passengers for reward.

The provision on the other side of that line is real, was checked, and is the
most consequential fact in this file.

**Άρθρο 18 παρ. 1 ν. 1903/1990** (ΦΕΚ Α΄ 142/23.10.1990), as it now stands after
άρθρο 22 ν. 4530/2018:

> Ο οδηγός κάθε Επιβατικού Ιδιωτικής Χρήσης (Ε.Ι.Χ.) οχήματος, ο οποίος
> διενεργεί επιβατικές μεταφορές **με κόμιστρο**, οι οποίες έχουν ανατεθεί σε
> άλλο φορέα ή πρόσωπο σύμφωνα με τις ισχύουσες διατάξεις, καθώς και ο ιδιοκτήτης
> αυτού, εφόσον είναι πρόσωπο διαφορετικό του οδηγού, τιμωρούνται με **φυλάκιση
> τουλάχιστον έξι (6) μηνών** και χρηματική ποινή **τουλάχιστον τριών χιλιάδων
> (3.000) ευρώ**.

Its history, because the version matters more than the number: repealed by άρθρο
50 ν. 2963/2001, brought back by παρ. 3 άρθρου 11 ν. 3082/2002, replaced by παρ.
8 άρθρου 17 ν. 3534/2007, replaced again by άρθρο 22 ν. 4530/2018. The 1990
original said *up to* three months. It now says *at least* six. Any text still
quoting drachmas is quoting a repealed version.

Four things follow, and they are not the ones an unsourced summary would suggest:

- It is **criminal, not administrative**. Prison, not a ticket. Παρ. 3 sends it
  through the summary procedure of άρθρα 417 ΚΠΔ.
- It reaches the **owner as well as the driver** when they are different people.
- The trigger word is **κόμιστρο**, a fare. The statute does not define it and
  does not distinguish it from sharing costs. That undefined gap is the entire
  space this app operates in.
- So the earlier reading that ν. 4530/2018 is only about taxis was half right.
  The law as a whole is, but its άρθρο 22 is precisely the provision that
  criminalises a private car carrying passengers for a fare.

Consequences for the code:

- The price ceiling is a **product decision**, not compliance with a rule. No
  article hands down a number. What keeps the app on the safe side is that the
  suggested contribution cannot exceed the trip's real cost, which is why the
  driver-typed consumption and `wear_factor` of RISKS 13 carry more weight than
  their size suggests.
- Never the word **κόμιστρο**, anywhere a user can see it, in any language, and
  never a synonym that means the same thing. MOBILE.md already requires
  "προτεινόμενη συνεισφορά"; this is the reason, and it is a harder rule than a
  style preference. Checked: the word appears nowhere in the app.

Still unverified, and not to be repeated until someone reads it: a reference to
**ΠΔ 50/2012**, asserted without a citation.

## 2. Motor insurance: the premise was wrong

Five sets of general terms were read directly (Generali, Syndea, Ατλαντική
Ένωση, Ευρωπαϊκή Πίστη, Intersalonica). **None excludes carrying a passenger who
shares costs.** The word κόμιστρο does not appear.

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

So the honest line to a driver is not "your insurance may not cover you." It is:
*you are sharing costs, not earning, which is why the car stays in private use.*
The app's own ceiling is the driver's insurance argument. Terms §6 said the
weaker, scarier thing and has been corrected.

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

Worth an email to `fuelprices@mindev.gov.gr` for a written answer on reuse. The
terms do not resolve it and no amount of reading will.

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

The fallback is deriving consumption from CO2, which is present from 2010. On the
rows that carry both, the ratio comes out at:

| Fuel | `Ewltp` ÷ `Fc`, mean | implied g CO2 per litre | textbook |
| --- | --- | --- | --- |
| Petrol | 23.59 | 2359 | 2392 |
| Diesel | 27.35 | 2735 | 2640 |
| LPG | 20.05 | 2005 | ~1665 |

Petrol and diesel land within a few percent, so the derivation is sound for
those. LPG does not, which suggests the field is not reported in the same unit
for it. The extremes are junk either way (a ratio of 1590 exists in the data), so
any harvest has to filter on a plausible band and take medians, not means.

If it gets built: derived numbers must not be presented as manufacturer figures.
