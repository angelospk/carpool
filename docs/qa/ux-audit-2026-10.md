# UX audit, October 2026

Done 14 October 2026, the day before Harold's two-phone test, at phone width
(390 × 844) on the web build and against the API. Three lenses: functional
(does every step work and recover), usability (Greek copy, clarity, taps,
empty and error states, onboarding) and visual (consistency, hierarchy, the
journey screens of GAPS item 2).

How it was done: a fresh database, `scripts/seed-demo.sh`, then a new account
(Ελένη, from Κομοτηνή) through sign-in and onboarding, a request
Αλεξανδρούπολη → Κομοτηνή, the driver (Γιώργος) accepting it in a second
browser profile, the trip screen and its chat on both sides, publishing and
the profile. The root URL and `/v1/readyz` were checked on production.

Severity: **critical** breaks a journey step or tells the user something
false; **important** costs a pilot user time, trust or a wrong decision;
**minor** is polish.

## Critical

| # | Lens | Finding | Status |
|---|---|---|---|
| C1 | functional, usability | The app promised notifications it cannot send. «Μπορείς να κλείσεις την εφαρμογή. Θα σου έρθει ειδοποίηση…» on the request screen, and «θα σε ειδοποιήσουμε» on search. Push is off on every build until the Expo and Firebase projects exist (docs/PUSH.md), and always off on the web. A passenger who believes it closes the app and never sees the answer; the driver's 30-minute ask lapses meanwhile. | **Fixed.** `src/push/reach.ts` knows whether this launch registered a push token. Without one the copy says the page updates itself and to check back. With one it is unchanged. The home card for a road nobody drives yet no longer says «θα ειδοποιηθείς» either (found in browser QA). |
| C2 | functional | `https://carpool.haroldpoi.dev/` answered a JSON 404. It is the address people are given, and there was nothing there to explain the app or let anybody try it. | **Fixed.** A landing page at `/`: what it is, how it works in three steps, cost sharing (no payment), the Thrace / ΔΠΘ framing (with no claim of endorsement), the pilot invite code, and links to the web app and the APK. |
| C3 | functional | No way in without an Android phone: the only client was the APK, so every iPhone user in a pilot was locked out, and a shared route link led nowhere for them. | **Fixed.** The Expo web build ships inside the API image and is served under `/app` (same origin, so no CORS, with a strict CSP). The route page `/r/{token}` gains «Συνέχισε στον browser» → `/app/r/{token}`, which already carried a signed-out visitor through sign-up and back to the route. |

## Important

| # | Lens | Finding | Status |
|---|---|---|---|
| I1 | usability | Copy hard-coded Έβρος: the first-run card said «Κοινές διαδρομές στον Έβρο» to somebody from Κομοτηνή, and the stranded-passenger fallback always linked ΚΤΕΛ Έβρου. | **Fixed.** Region-neutral first-run copy. The ΚΤΕΛ button follows the town the trip starts from (`src/design/ktel.ts`: Ροδόπης, Ξάνθης, else Έβρου; URLs checked live). |
| I2 | usability, visual | Numbers in two shapes on neighbouring screens: «2.37 €», «57.9 χλμ», «+1.7 χλμ» beside «1,00 €». | **Fixed.** `decimal`, `eur` and `km` in `src/design/format.ts`, pinned to el-GR, used on every screen that showed a decimal point. |
| I3 | visual | The time-of-day chips overflowed at 390 px: «Μεσημέρι» and «Απόγευμα» ran into each other. | **Fixed.** Band chips size to their label and wrap. |
| I4 | visual | The request and ask screens said their title twice: in the stack header and again as a large heading under it, pushing the content down. | **Fixed.** The header alone names them. |
| I5 | usability | Accepting a request dropped the driver back on the home list with no word about what had happened. | **Fixed.** Accepting opens the trip it made. |
| I6 | visual | «Δεν θα έρθω» (and the driver's «Ακύρωση») was a full-width solid red button above the chat, the heaviest thing on the trip screen. | **Fixed.** A quiet button with danger-coloured text; still confirmed. |
| I7 | usability | A passenger's next-trip card showed the driver's free seats and not who was driving. | **Fixed.** «Οδηγός: <όνομα>» for passengers; drivers keep the free seats. |
| I8 | usability | A driver with one recurring route saw every materialised departure for three weeks, fifteen rows, burying anything booked. | **Fixed.** The list keeps the coming seven days and counts the rest; a seat held as a passenger is never hidden. |
| I9 | visual | The active tab label was truncated («Δημοσίευσε διαδρ…»). | **Fixed.** Short tab labels (Αρχική, Ζήτα θέση, Δημοσίευσε, Προφίλ). |
| I10 | usability | The first-run home offered each action three times: header pills, the empty-state buttons, and the tab bar. | **Fixed.** The header pills hide while the first-run card shows. |
| I11 | functional | The code screen had one button. A wrong number or a code that never arrived left nothing to press. | **Fixed.** It shows where the code went, «Άλλος αριθμός» and «Στείλε νέο κωδικό». |
| I12 | visual | GAPS item 2: the trip screen opened in a different layout from the home card that led to it (glyph left, time right, badges under the time). | **Fixed.** The trip header now has the home card's shape: time and date, badges on the right, then the route spine with the glyph beside it. The request and ask screens already shared `JourneySummary`. |

## Minor (not done)

| # | Lens | Finding |
|---|---|---|
| M1 | visual | Tab bar icons are text glyphs (⌕ ＋ ☺) rather than drawn icons. |
| M2 | usability | On search, an empty «Τώρα στον δρόμο» card takes the top of the screen above the form. |
| M3 | usability | The fuel estimate is four paragraphs; one sentence and «Πώς βγαίνει;» would do. |
| M4 | visual | In place suggestions the name and the region can touch, and the region is named inconsistently («Περιφερειακή Ενότητα Έβρου» vs «Κομοτηνής»). |
| M5 | usability | Onboarding puts the date-of-birth note under its field and the home-town note above its field. |
| M6 | usability | The trip screen is titled «Διαδρομή» while its buttons say «ταξίδι». |
| M7 | visual | «Μικρή βαλίτσα» uses a backpack emoji. |
| M8 | visual | The profile shows the phone number twice. |
| M9 | visual | Home list rows write «A → B» on one line while every card uses the route spine. Kept on purpose: rows stay one line high. |
| M10 | visual | The web build's `index.html` says `lang="en"` (Expo's template). |

## Checked and fine

- Recovery from a dead session: after the database was reset, a reload went
  cleanly to sign-in instead of an error.
- The driver's home polls: an ask appeared within 20 s without a reload.
- The request screen's states (loading, failure, closed, empty road) were
  already separate (GAPS 3).
- The route link page, its CSP, and the signed-out path through sign-up back
  to the route.

## Not in this audit

Confirm and rate need a trip whose window has ended, so they are covered by
`scripts/e2e.sh` (steps 12–14) rather than by clicking. Push on a real phone
waits on docs/PUSH.md.
