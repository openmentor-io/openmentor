-- Reverses 000015. Nothing is lost: price_amount is GENERATED from price, so
-- dropping it discards a derivation, not data — re-running the up-migration
-- reproduces it exactly.
ALTER TABLE mentors DROP COLUMN IF EXISTS price_amount;
