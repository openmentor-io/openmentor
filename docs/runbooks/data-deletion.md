# Runbook: GDPR Data Deletion (Right to Erasure)

**Trigger:** A request to `privacy@openmentor.io` (or `hello@openmentor.io`) asking to delete personal data, from a mentor or a mentee. Respond within 30 days (GDPR Art. 12(3)).

**Scope of personal data held per subject:**

| Subject | Where | Data |
|---|---|---|
| Mentor | `mentors` table | name, email, telegram handle, job/workplace, about/details, photo (blob storage), slug |
| Mentor | blob storage (S3 bucket) | profile photo(s) keyed by slug/id |
| Mentee | `client_requests` table | name, email, telegram handle, request description, level |
| Mentee | `reviews` table | review text (linked to a request) |
| Both | analytics (PostHog) | events keyed by distinct_id |
| Both | email provider logs (SES) | delivery metadata (auto-expires) |
| Both | server logs (Grafana Loki) | IPs/emails may appear in logs (retention-limited) |

## Procedure

1. **Verify identity**: reply from the address on file; for mentors, ask them to trigger a magic-link login or reply from the registered email. Never delete on a third-party's word.
2. **Mentor deletion** (SQL, run in a transaction):
   ```sql
   -- find the mentor
   SELECT id, slug, name, email FROM mentors WHERE email = $1;
   -- requests referencing the mentor keep working (mentor_id is ON DELETE SET NULL)
   DELETE FROM mentor_tags WHERE mentor_id = $MENTOR_ID;
   DELETE FROM mentors WHERE id = $MENTOR_ID;
   ```
   Then delete profile images from the storage bucket (prefix = slug or mentor id), and trigger frontend revalidation / cache reset (`?force_reset_cache=true` on the internal mentors API).
3. **Mentee deletion**:
   ```sql
   SELECT id FROM client_requests WHERE email = $1;
   DELETE FROM reviews WHERE client_request_id IN (SELECT id FROM client_requests WHERE email = $1);
   DELETE FROM client_requests WHERE email = $1;
   ```
   If the mentor should retain evidence a session happened, replace PII with placeholders instead of row deletion (`UPDATE client_requests SET name='[deleted]', email=NULL, telegram=NULL, description='[deleted]' ...`) — prefer full deletion unless there's an active dispute.
4. **Analytics**: delete the person in PostHog (Persons → delete, incl. events) using their distinct_id/email.
5. **Confirm** to the requester in writing; note the date. Keep a minimal log (date, subject hash, operator) in the ops tracker — not the deleted data itself.

## Notes

- Backups: managed-Postgres backups age out on their retention schedule; deletion from backups is not immediate — state this in the privacy policy.
- v2: self-service deletion from the mentor dashboard is tracked as a post-migration improvement.
