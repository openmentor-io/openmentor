-- phase: contract
-- (narrowing: an image that predates pkg/price still writes free text like
-- '$100 / hour', which this constraint rejects — so a rollback across this
-- boundary would leave the old image unable to save a profile)
--
-- Close mentors.price to the canonical grammar (D87, supersedes the free-text
-- price of D3): 'Free', 'Negotiable', or '$N' for a whole 1..1000.
--
-- WHY $1 AND NOT $0
-- A mentor who charges nothing has price 'Free'. That is a different statement
-- from charging zero dollars — it is the one the catalog filters, the profile
-- badge and the mentee confirmation email all read as "this session is free" —
-- and allowing both would give it two spellings. pkg/price.MinAmount is the
-- same 1.
--
-- WHY NOT ALSO `SET NOT NULL`
-- Deliberately out of scope. A CHECK is satisfied by NULL, so this constrains
-- every non-NULL value and leaves the column's nullability exactly as D50 found
-- it — which matters, because D50's COALESCE in mentorSelect and the four other
-- mentor SELECTs is what keeps a NULL from taking out login and the catalog,
-- and that stays true either way. No write path can produce a NULL price (every
-- one binds a `required` field), and production held none on 2026-08-13.
--
-- PRODUCTION STATE AT AUTHORING TIME (2026-08-13, 519 rows, all conforming):
--   Negotiable 221 · $50 89 · Free 70 · $100 39 · $30 30 · $150 14 · $20 14
--   $10 13 · $40 8 · $70 5 · $60 5 · $200 5 · $90 4 · $120 1 · $80 1
-- so the normalisation below is a no-op there. It exists for rows that
-- infra/migration/migrate-mentors.js could write between now and the deploy:
-- its mapPrice passes an unrecognised '$?\d+' prefix through verbatim, which is
-- how '$100 / hour' and a bare '50' would get in.

-- 1. Shape normalisation.
--
-- This deliberately does NOT clamp an out-of-range amount. Rewriting a mentor's
-- '$1500' to '$1000' would change what they charge by a third, silently and
-- unrecoverably; such a value falls through to the assertion in step 2 and
-- stops the deploy instead, which is loud and fixable.
UPDATE mentors SET price = btrim(price) WHERE price IS NOT NULL AND price <> btrim(price);

UPDATE mentors SET price = 'Free'       WHERE lower(price) = 'free'       AND price <> 'Free';
UPDATE mentors SET price = 'Negotiable' WHERE lower(price) = 'negotiable' AND price <> 'Negotiable';

-- '$100 / hour' -> '$100', '50' -> '$50', '$050' -> '$50'. The `0*` outside the
-- capture is what drops a leading zero; '$0' survives it (the capture needs one
-- digit, so it backtracks onto the zero) and is caught in step 2, because a
-- zero price is a decision only the mentor can make.
UPDATE mentors
   SET price = '$' || (regexp_match(price, '^\$?0*([0-9]+)'))[1]
 WHERE price ~ '^\$?[0-9]'
   AND price !~ '^\$([1-9][0-9]{0,2}|1000)$';

-- 2. Assert before constraining, so the operator reads what is wrong rather
-- than a bare constraint violation. A failing migration takes the whole deploy
-- down (backend and worker gate on service_completed_successfully), so the
-- message has to carry enough to act on — but NOT the offending values
-- themselves: this error lands in the migrate container's log and from there
-- in Loki, and the old column was free text, which is exactly the class the
-- logging rules keep out of log lines. The COUNT locates the problem; the
-- values are read where they live, with psql.
DO $$
DECLARE offending_count bigint;
BEGIN
    SELECT count(*)
      INTO offending_count
      FROM mentors
     WHERE price IS NOT NULL
       AND price !~ '^(Free|Negotiable|\$([1-9][0-9]{0,2}|1000))$';

    IF offending_count > 0 THEN
        RAISE EXCEPTION
            '% mentor row(s) hold prices outside the canonical grammar (values not printed here: the old column was free text and this message is a log line). List them with docs/runbooks/audit-2026-08/diagnostics.sql section D2c, set each by hand to Free, Negotiable, or $N for a whole 1..1000, then re-run the deploy. This migration will not guess what a mentor meant to charge.',
            offending_count;
    END IF;
END $$;

-- 3. The constraint. Mirrored by api/pkg/price.Parse and web/src/lib/price.ts;
-- api/internal/repository/mentor_price_constraint_db_test.go is what pins this
-- regexp and that parser to the same grammar.
ALTER TABLE mentors
    ADD CONSTRAINT mentors_price_chk
    CHECK (price ~ '^(Free|Negotiable|\$([1-9][0-9]{0,2}|1000))$');
