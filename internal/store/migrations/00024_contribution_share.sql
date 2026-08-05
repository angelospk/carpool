-- +goose Up

-- The app stops setting the figure and starts capping it.
--
-- Until now it computed a contribution and both sides saw that number. That is
-- the app deciding, however transparently, and άρθρο 12 ν. 4530/2018 makes
-- *influencing the price* relevant to how an electronic intermediary is
-- characterised — see RISKS.md. Transparency answers a different question.
--
-- BlaBlaCar's own Greek terms describe the shape this migration moves towards:
-- "το ποσό της Συνεισφοράς στα Έξοδα καθορίζεται από εσάς, ως Οδηγό, υπό την
-- αποκλειστική σας ευθύνη", with the platform suggesting an amount and
-- restricting how far it can be adjusted. Our own terms already say the
-- contribution is a private arrangement between the two people; this is the
-- code catching up with them.
--
-- So: the computed share of the fuel becomes a ceiling, and the driver picks
-- what fraction of it they ask for. The same number comes out most of the
-- time — most drivers will leave it at all of it — but the decision is theirs
-- and the app's job becomes refusing anything above cost.
--
-- Strictly above zero. "I ask for nothing" already has a representation in
-- pricing_mode = 'free', and a share whose zero value means "charge nothing"
-- is a caller that forgets one line away from publishing a route that quietly
-- asks for nothing — the same landmine the detour budget had, recorded in
-- PROGRESS.md, and not one worth building twice.
ALTER TABLE route_templates
    ADD COLUMN contribution_share numeric(3,2) NOT NULL DEFAULT 1.00
        CHECK (contribution_share > 0 AND contribution_share <= 1);

-- Copied onto each departure like everything else the price is made from, so
-- that a driver changing their mind does not reprice a booked seat.
ALTER TABLE trip_occurrences
    ADD COLUMN contribution_share numeric(3,2) NOT NULL DEFAULT 1.00
        CHECK (contribution_share > 0 AND contribution_share <= 1);

UPDATE trip_occurrences o SET contribution_share = t.contribution_share
  FROM route_templates t WHERE t.id = o.template_id;

-- Every route that exists was published under the old rule, where the app's
-- figure was the figure. One is what they were implicitly asking for, so
-- nothing reprices on the day of this migration.

-- +goose Down

ALTER TABLE trip_occurrences DROP COLUMN IF EXISTS contribution_share;
ALTER TABLE route_templates  DROP COLUMN IF EXISTS contribution_share;
