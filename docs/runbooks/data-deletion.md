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
| Both | server logs (Grafana Loki) | client IP, user agent, masked email, hashed ids — see [Retention](#retention-what-expires-when); step 6 is a *verification* step, not a deletion |
| Both | traces (Grafana Tempo) | HTTP method, redacted path/query, hashed ids — no addresses or names |

## Retention: what expires when

Every claim below is checkable, and where it is a plan setting rather than a
number in this repo, the check is spelled out. Before C11 this section did not
exist: the only statement about logs was "IPs/emails may appear in logs
(retention-limited)" — no period, no mechanism, no procedure, which is not an
answer to an erasure request.

| Store | Retention | Where it is set / how to verify |
|---|---|---|
| `mentors` row of a deleted profile | `WORKER_PROFILE_PURGE_RETENTION_DAYS`, default **30 days** from `deleted_at` | `infra/.env.example`; default in `api/internal/worker/jobs.go`. Verify the sweep ran: `openmentor_profile_purge_last_success_timestamp_seconds` in Prometheus |
| Profile images | bucket lifecycle rule on the `deleted/` prefix | S3 provider console → bucket → lifecycle rules. Record the configured day count in the erasure note |
| `client_requests` / `reviews` of a deleted mentor | erased by the same nightly purge | `api/internal/worker/repository_purge.go` |
| App logs on the VM | **7 days**, 100 MB/file, 7 rotations, gzipped | `api/pkg/logger/logger.go` (lumberjack) and `web/src/lib/logger.ts` (`MAX_FILES: '7d'`) |
| Logs in Grafana Cloud (Loki) | **plan setting — read it, do not assume** | Grafana Cloud portal → the stack → **Logs** → retention. Record the number in the erasure note: it is the only window in which any of the subject's log lines can still exist |
| Traces in Grafana Cloud (Tempo) | plan setting, same place | Grafana Cloud portal → the stack → **Traces** |
| Postgres backups | `BACKUP_RETENTION_DAYS`, default **30 days**; managed snapshots age out on the provider's schedule | `infra/.env.example`, `postgres-backup-restore.md` |
| PostHog | until deleted; the person delete in step 4 is immediate | PostHog project → Persons |
| SES | provider-managed delivery metadata, not configurable | nothing to do — no message bodies are retained by us |

**Why logs get a verification step and not a deletion step.** Since C11 the
telemetry pipeline is not supposed to hold subject-identifying personal data at
all, so there is nothing to selectively erase:

- Addresses are masked to `j***@example.com` before they are written —
  `api/pkg/redact.Email` at the call sites, `api/pkg/logger`'s redacting core for
  every field that reaches an encoder, and `loki.process "redact_pii"` in
  `infra/alloy/config.alloy` as a last resort on the way out of the host.
- Ids are hashed (`sha256:…`), never carried (P14).
- Mentor and moderator names are not logged; the id is.
- What remains that is personal data is **client IP and user agent** on request
  lines (`internal/middleware/observability.go`). Those are kept deliberately —
  they are what abuse and rate-limit investigations run on — they are not keyed by
  subject, and they expire with the Loki window above. Selective erasure of
  security logs is not offered; the privacy policy's retention section covers it.

So the log obligation is: confirm nothing subject-identifying is present, and
state the window in which the non-erasable remainder expires. That is step 6.

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
5. **Verify the telemetry stores hold nothing subject-identifying.** Run these
   three queries in Grafana Explore over the **full Loki retention window** (read
   the number from the portal per the Retention table; do not use the default
   picker). Each must return zero results. Substitute the subject's real address
   only in the query box — never paste it into a ticket, a commit, or this file.

   ```logql
   # 1. no raw address anywhere. Also catches the local part on its own.
   {namespace="openmentor"} |= "<local-part>@"
   # 2. no line reached Loki un-redacted: this marker is written ONLY by the
   #    Alloy last-resort stage, so a hit is a bug in the app-level redaction
   #    and has to be fixed, not just noted.
   {namespace="openmentor"} |= "[REDACTED_EMAIL]"
   # 3. the masked form is expected and is NOT a finding — record the count so
   #    the erasure note says what is there rather than implying nothing is.
   {namespace="openmentor"} |= `"recipient":"<first-char>***@<domain>"`
   ```

   Query 1 or 3 returning hits with a *raw* address means a sink was missed:
   open a bug, fix the sink, and note in the erasure record that the lines expire
   with the Loki window. Query 2 returning hits means the Alloy net caught
   something the application should have — same, but the app-level fix is
   mandatory because the net is not guaranteed to cover every future shape.
   Repeat query 1 against Tempo (Explore → Tempo → free-text search) for the same
   window.
6. **Confirm** to the requester in writing; note the date.
7. **Record the erasure.** Append one row to the ops tracker's erasure log. This
   is the evidence that the request was handled — GDPR Art. 5(2) accountability —
   and it must contain no personal data itself, so the subject is identified by a
   salted hash, never by address or name.

   ```
   date_received:      2026-08-07
   date_completed:     2026-08-07
   subject_ref:        sha256(<address> + <the ops-tracker salt>)  # first 12 hex
   subject_type:       mentor | mentee
   operator:           <who ran it>
   db_rows:            mentors=<uuid or n/a> client_requests=<n> reviews=<n>
   purge_verified:     openmentor_profile_purge_last_success_timestamp_seconds=<ts>
   images:             deleted/ prefix, lifecycle=<days> days
   posthog:            person deleted (distinct_id=<mentor:uuid|n/a>)
   loki:               queries 1-3 run over <N>-day window, raw hits=<0|n>
   tempo:              queried over <N>-day window, raw hits=<0|n>
   backups:            ages out with BACKUP_RETENTION_DAYS=<n> (stated in policy)
   notes:              <anything the product flow could not reach>
   ```

   The `subject_ref` salt lives with the ops tracker, not in this repo: an
   unsalted hash of an address is trivially reversible against a guessed candidate
   list, which would make the erasure log itself a record of who asked to be
   erased. Same reasoning as `redact.Email` masking rather than hashing addresses
   in logs (D80).

## Notes

- **Retention model (D13, amended by D70):** service data is retained while relevant and deleted on request, with one scheduled exception — DELETED mentor profiles are automatically erased by the worker's nightly purge once `WORKER_PROFILE_PURGE_RETENTION_DAYS` has passed, and their images by the bucket lifecycle rule on the `deleted/` prefix. For everything else this runbook remains the primary erasure mechanism; handle requests promptly and completely. (Login tokens, sessions, and observability/log data expire on their own TTLs.)
- Backups: managed-Postgres backups age out on their retention schedule; deletion from backups is not immediate — the privacy policy states this (section 6, Backups).
- Self-service deletion shipped with D70 (profile page → Delete profile); this runbook now mainly covers mentee erasure, analytics, and mentor rows the product flow cannot reach.
- **A soft-deleted profile stops producing personal data immediately, and that is
  checked, not assumed (C11).** Between `deleted_at` being stamped and the nightly
  purge erasing the row, the profile is out of the catalog and out of the login
  path — `MentorRepository.GetByEmail` excludes deleted rows in the query itself,
  so a magic link cannot even be minted for one — and the only lines the deletion
  flow writes are `mentor_id` plus counts (`job_purge_deleted_profiles.go`,
  `job_profile_deletion_emails.go`). The one email that still goes out is the
  deletion confirmation the subject asked for, and its recipient is masked.
  What deletion does **not** do is reach back into telemetry: log lines written
  while the profile was live outlive the row by the difference between the Loki
  window and the purge window. Since C11 those lines carry no address and no name,
  which is what makes step 5 a verification rather than an erasure — but it is the
  reason step 5 exists and is run over the **full** retention window.
