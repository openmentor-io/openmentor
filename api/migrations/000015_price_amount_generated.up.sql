-- phase: expand
-- (additive: one generated column no existing code selects; an older image
-- neither reads nor writes it, so rollback may cross it)
--
-- Derived numeric view of mentors.price (D88): the dollar amount when the
-- price is fixed, NULL for 'Free', 'Negotiable' and a NULL price. Exists so
-- the first SQL consumer of a price number — a server-side catalog filter, a
-- sort, an analytics query — gets an integer instead of reinventing
-- substring-and-cast at the call site.
--
-- GENERATED, not a real column, on purpose:
--   - it cannot drift from price: Postgres recomputes it on every write, so
--     there is no backfill, no dual-write, and no second source of truth;
--   - it cannot be written, so no application path needs to know it exists —
--     nothing in api/ selects it today, and that is deliberate. Pulling it
--     into a mentor SELECT later puts it under the D50 rule: it is nullable,
--     so it must be COALESCEd or scanned into a pointer. Note that
--     dbtest.NullableColumns deliberately EXCLUDES generated columns (they
--     cannot be filled or blanked by an UPDATE), so the nullable-column test
--     will NOT catch a miss on this one — the reviewer has to.
--
-- The guard CANNOT lean on mentors_price_chk (000014), although it is
-- tempting: Postgres computes generated columns for the CANDIDATE row before
-- it checks constraints, so on an INSERT of '$50.00' a bare
-- substring-and-cast fails with a cast error (SQLSTATE 22P02) before the
-- constraint gets to reject the row by name — the write is still refused, but
-- by the wrong layer with a worse message, and
-- repository.TestPriceConstraintAgreesWithTheParser fails on exactly that.
-- Hence the expression matches the full canonical digit shape itself: any
-- '$'-prefixed value outside it yields NULL here and is then rejected by the
-- constraint, which stays the one that speaks. {1,4} also caps the cast input
-- at 9999, so a '$' followed by twenty digits cannot overflow integer
-- (22003) — same pre-constraint trap, different error. A NULL price yields
-- NULL (CASE with no ELSE), matching the constraint's own NULL-passes
-- semantics.
ALTER TABLE mentors
    ADD COLUMN price_amount integer
    GENERATED ALWAYS AS (
        CASE WHEN price ~ '^\$[0-9]{1,4}$' THEN substring(price FROM 2)::integer END
    ) STORED;

COMMENT ON COLUMN mentors.price_amount IS
    'Derived from price (D88): the dollar amount for a fixed price, NULL for Free/Negotiable. Generated — never write it; COALESCE or pointer-scan it if it ever enters a Go SELECT (D50).';
