# Runbook: GDPR Data Deletion (Right to Erasure)

**Trigger:** A request to `privacy@openmentor.io` (or `hello@openmentor.io`) asking to delete personal data, from a mentor or a mentee. Respond within 30 days (GDPR Art. 12(3)).

**Scope of personal data held per subject:**

| Subject | Where | Data |
|---|---|---|
| Mentor | `mentors` table | name, email, preferred contact, job/workplace, about/details, photo (blob storage), slug |
| Mentor | blob storage (S3 bucket) | profile photo(s) keyed by slug/id |
| Mentee | `client_requests` table | name, email, preferred contact, request description, level |
| Mentee | `reviews` table | review text (linked to a request) |
| Both | analytics (PostHog) | events keyed by distinct_id |
| Both | email provider logs (SES) | delivery metadata (auto-expires) |
| Both | server logs (Grafana Loki) | IPs/emails may appear in logs (retention-limited) |

## Procedure

1. **Verify identity**: reply from the address on file; for mentors, ask them to trigger a magic-link login or reply from the registered email. Never delete on a third-party's word.
2. **Mentor deletion — prefer the product flow (D70), not SQL.** Since profile
   deletion shipped, the mentor can delete their own profile from their profile
   page, and an admin can delete it from the moderation panel. That path does
   everything this runbook's manual steps do, plus the parts that are easy to
   forget by hand: it revokes sessions and outstanding review invitations,
   moves the profile images — UUID-keyed **and** legacy slug-keyed — into the
   bucket's `deleted/` trash prefix (erased by the bucket lifecycle rule), and
   the worker's nightly purge erases the rows, requests and reviews after the
   retention window (`WORKER_PROFILE_PURGE_RETENTION_DAYS`). For a GDPR
   deadline shorter than the retention window, delete via the product flow,
   then backdate `deleted_at` so the next nightly sweep erases the rows:
   `UPDATE mentors SET deleted_at = NOW() - make_interval(days => <retention>+1) WHERE id = $MENTOR_ID;`
   The manual SQL below remains for cases the product flow cannot reach (e.g.
   a mentor row in a broken state).
   **Mentor deletion, manual path** (SQL, run in a transaction):
   ```sql
   -- find the mentor
   SELECT id, slug, name, email FROM mentors WHERE email = $1;
   -- Capture EVERY slug the mentor has used, BEFORE deleting the row: images
   -- may be keyed by the current UUID and — for legacy/pre-cutover copies — by
   -- any current or retired slug. mentor_slug_history cascade-deletes with the
   -- mentor row, so grab these first or the mapping is lost (D29).
   SELECT slug FROM mentors WHERE id = $MENTOR_ID
   UNION
   SELECT slug FROM mentor_slug_history WHERE mentor_id = $MENTOR_ID;
   -- requests referencing the mentor keep working (mentor_id is ON DELETE SET NULL)
   DELETE FROM mentor_tags WHERE mentor_id = $MENTOR_ID;
   DELETE FROM mentors WHERE id = $MENTOR_ID; -- also cascades mentor_slug_history
   ```
   Then delete profile images from the storage bucket for **every** prefix: the
   mentor UUID (`<id>/`, the canonical key since D29) **and** each current/retired
   slug from the query above (`<slug>/`, covers legacy pre-cutover copies). The
   API reads mentors directly from the database (no cache), so the deletion is
   reflected immediately.
3. **Mentee deletion**:
   ```sql
   SELECT id FROM client_requests WHERE email = $1;
   DELETE FROM reviews WHERE client_request_id IN (SELECT id FROM client_requests WHERE email = $1);
   DELETE FROM client_requests WHERE email = $1;
   ```
   If the mentor should retain evidence a session happened, replace PII with placeholders instead of row deletion (`UPDATE client_requests SET name='[deleted]', email=NULL, preferred_contact=NULL, description='[deleted]' ...`) — prefer full deletion unless there's an active dispute.
4. **Analytics**: delete the person in PostHog (Persons → delete, incl. events) using their distinct_id/email.
5. **Confirm** to the requester in writing; note the date. Keep a minimal log (date, subject hash, operator) in the ops tracker — not the deleted data itself.

## Notes

- **Retention model (D13, amended by D70):** service data is retained while relevant and deleted on request, with one scheduled exception — DELETED mentor profiles are automatically erased by the worker's nightly purge once `WORKER_PROFILE_PURGE_RETENTION_DAYS` has passed, and their images by the bucket lifecycle rule on the `deleted/` prefix. For everything else this runbook remains the primary erasure mechanism; handle requests promptly and completely. (Login tokens, sessions, and observability/log data expire on their own TTLs.)
- Backups: managed-Postgres backups age out on their retention schedule; deletion from backups is not immediate — the privacy policy states this (section 6, Backups).
- Self-service deletion shipped with D70 (profile page → Delete profile); this runbook now mainly covers mentee erasure, analytics, and mentor rows the product flow cannot reach.
