# Legal texts

Drafts produced by a legal AI tool from a written brief, then edited for
publication. They are not reviewed by a lawyer and must not ship as they are.

- `terms-el.md` — Όροι Χρήσης
- `privacy-el.md` — Πολιτική Απορρήτου

The editing pass fixed a dozen transcription typos (`Οηγών`, `αράνομους`,
`διαικασία`, `κλή πίστη`, `επιββαση`, `Τηρούε` and others), cut the padding, and
turned the retention section into a table. No legal wording was softened; terms
of art are untouched.

## What the code cannot back up yet

These are promises the software does not currently keep. Each one is either a
gap to close before publishing or a sentence to change.

**Deletion and access have no endpoints.** The policy offers deletion within 30
days and rights of access, portability and correction. There is no
`DELETE /me`, no export, and no admin path that performs any of it. Today those
requests could only be honoured by hand, in psql. Closing this is real work: the
schema has foreign keys from bookings, asks, ratings, messages and invite
redemptions to `users`, and deletion has to decide, per table, between removal
and anonymisation. Ratings in particular cannot simply vanish without rewriting
other people's history.

**The chat retention promise is unimplemented.** Nothing deletes messages after
twelve months. There is no retention job at all, for any table.

**The liability cap is blank and the service is free.** `[ΠΟΣΟ]` in §13 of the
Terms needs a figure, and a cap on a free service is exactly the clause a lawyer
should look at rather than a generator.

**The processors are unnamed.** The SMS gateway is not chosen, and the VPS
country is not filled in. Both feed §6 and §7 of the policy, and the second one
decides whether there are international transfers to declare at all.

## What was asked for and did not come back

The brief asked for a short legal memo on six questions. Only the two documents
arrived. The memo is the part worth chasing, because it is where the answers
that a template cannot supply would be:

1. Where Greek law draws the line between cost sharing and carrying passengers
   for reward, and what wording and ceiling keep the app on the safe side.
2. Whether ordinary private motor insurance covers a passenger who contributes
   to costs, and what drivers must be told.
3. Whether a DPIA is required.
4. What has to exist technically to satisfy access, deletion and portability.
5. What retention and access is lawful for messages between users.
6. Whether minors must be excluded outright.

Question 2 is the one that matters most in practice and the one nobody asks
until after an accident.
