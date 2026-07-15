# Runbook: migrating mentors from getmentor.dev

`infra/migration/migrate-mentors.sh` imports mentor profiles from the
getmentor.dev production database into openmentor.io (DECISIONS D22). It is
an opt-in, per-mentor flow: you feed it slugs (or a CSV of slugs), it does
the rest.

## What one migration does

1. Reads the mentor (+ tags) from the getmentor.dev production Postgres by slug.
2. Translates `name` (romanized), `job_title`, `workplace`, `about`,
   `details` (HTML preserved) and `competencies` RU→EN with the Claude API.
3. Maps enum-like fields to the new data model:
   - price: `Бесплатно` → `Free`, `По договоренности` → `Negotiable`,
     `N руб` → `$N/RUB_TO_USD_RATE` rounded to $5 steps (default rate 100)
   - tags: `Сети`→`Networking`, `Карьера`→`Career`,
     `Собеседования`→`Interview prep`, `Аналитика`→`Analytics`,
     `Безопасность`→`Security`; sponsor tags (`Эксперт Авито`,
     `Сообщество Онтико`) are dropped; English tags pass through
   - experience passes through (`2-5` / `5-10` / `10+`)
   - `telegram` → `preferred_contact` as `Telegram: @handle`
4. Keeps identity fields unchanged: email, calendar_url, privacy,
   sort_order, created_at.
5. Takes a **new legacy_id** from `mentors_legacy_id_seq` and builds the
   slug from the old slug's text part + the new id
   (`ivan-petrov-42` → `ivan-petrov-107`).
6. Inserts with **status = inactive**: approved, can log in via magic link,
   but hidden from the catalog until the mentor flips visibility.
7. Copies the profile photos (`<old slug>/{full,large,small}`) from Yandex
   Object Storage to the S3 images bucket under the **new** slug.
8. Triggers the worker's `POST /jobs/profile-migrated` (over SSH +
   `docker exec`, like the manual cron triggers), which emails the mentor:
   "your profile moved, log in, review the translation, switch it on".

**Idempotency:** each migrated row stores `getmentor:<old legacy_id>` in
`mentors.airtable_id` (unused by the app, UNIQUE). Re-runs skip migrated
mentors; mentors whose email already exists on openmentor.io (they signed up
themselves) are skipped too. `--resume` re-runs steps 7–8 for
already-migrated mentors (e.g. after an image/email failure).

## Prerequisites (one-time)

- The worker image containing the `profile-migrated` template is deployed
  (`infra/deploy.sh backend`). The email trigger 404s otherwise.
- `infra/.env.production` in place (as for deploy.sh/db.sh) — provides VM
  SSH access, `POSTGRES_PASSWORD` and `WORKER_AUTH_TOKEN`.
- `cd infra/migration && npm install`.
- `infra/migration/.env` (gitignored) with:

  ```bash
  # getmentor.dev production DSN (getmentor-infra/.env.production DATABASE_URL).
  # sslmode/sslrootcert params are stripped automatically; TLS is verified
  # against the committed yandex-ca.pem.
  SOURCE_DATABASE_URL=postgres://getmentor:...@rc1b-....mdb.yandexcloud.net:6432/getmentor

  # Translation (or export it in the shell)
  ANTHROPIC_API_KEY=sk-ant-...

  # Photos: Yandex Object Storage -> S3 (same vars as the image-copy script)
  SOURCE_S3_ACCESS_KEY=...
  SOURCE_S3_SECRET_KEY=...
  SOURCE_S3_BUCKET=mentor-images
  DEST_S3_ACCESS_KEY=...
  DEST_S3_SECRET_KEY=...
  DEST_S3_BUCKET=mentor-images

  # Optional
  # RUB_TO_USD_RATE=100
  # ANTHROPIC_MODEL=claude-opus-4-8
  ```

## Running it

```bash
cd infra/migration

# 1. Always dry-run first — read + map + report, no writes, no translation
./migrate-mentors.sh --slug ivan-petrov-42 --dry-run

# (optional) include the actual translation in the dry-run output
./migrate-mentors.sh --slug ivan-petrov-42 --dry-run --translate-dry-run

# 2. Migrate for real
./migrate-mentors.sh --slug ivan-petrov-42

# Bulk: CSV with one slug per line (header "slug" optional)
./migrate-mentors.sh --csv mentors.csv --dry-run
./migrate-mentors.sh --csv mentors.csv

# Re-run images + email for already-migrated mentors
./migrate-mentors.sh --slug ivan-petrov-42 --resume

# Process everything mentors scheduled via the /migrate page (see below)
./migrate-mentors.sh --from-intents
```

Useful flags: `--skip-images`, `--skip-email`, `--skip-translation`
(keeps the Russian text verbatim).

## Self-service opt-ins (the /migrate page)

Mentors schedule their own migration at

```
https://openmentor.io/migrate?slug=<getmentor-slug>
```

e.g. `https://openmentor.io/migrate?slug=ivan-petrov-42` — this is the link
to template into the Telegram announcement, one per mentor, using their
getmentor.dev slug. The page shows what migration does and a
"Schedule migration" button (Turnstile-protected). Clicking it records the
slug in the `migration_intents` table (API endpoint
`POST /api/v1/migration/intents`), idempotently — repeat clicks show
"already scheduled".

To process the queue (run this daily while the announcement is hot):

```bash
./migrate-mentors.sh --from-intents
```

It picks up every `pending` intent, migrates each mentor, and writes the
outcome back to the row (`done` / `skipped` / `failed` + a note), so the
next run only sees new opt-ins. Peek at the queue any time:

```bash
../db.sh -c "SELECT slug, status, note, created_at FROM migration_intents ORDER BY created_at DESC LIMIT 20"
```

A `failed` intent (bad slug, no email) stays recorded with its reason —
fix the cause and reset it with
`UPDATE migration_intents SET status='pending' WHERE slug='...'`, or handle
that mentor manually with `--slug`.

Cost note: translation runs on `claude-opus-4-8`; a typical profile is
~1.5–4k input tokens + a similar output, i.e. **a few cents per mentor**.

## Verifying a migration

```bash
# Row landed, marker set, status inactive
../db.sh -c "SELECT slug, legacy_id, status, airtable_id FROM mentors WHERE airtable_id LIKE 'getmentor:%' ORDER BY legacy_id DESC LIMIT 10"

# Photo reachable under the new slug
curl -sI https://cdn.openmentor.io/<new-slug>/full | head -1
```

Then check the mentor received the email (DEV_EMAIL_OVERRIDE reroutes it off
production) and can log in at https://openmentor.io/mentor/login with their
email; the profile edit page should show the translated text and the
visibility card should show "hidden".

## Failure modes

| Symptom | Cause / fix |
| --- | --- |
| `Skipped: email already registered` | Mentor signed up on openmentor.io themselves (D21 caveat). Migration intentionally refuses; reconcile manually if their getmentor profile is richer. |
| Insert ok, images/email failed | Fix the cause, re-run with `--resume`. |
| Email trigger 404 | Worker image predates the `profile-migrated` template — deploy backend first. |
| `Mentor has no email` | Not migratable: magic-link login and notification are impossible. |
| Source connect fails | Check `SOURCE_DATABASE_URL`; the Yandex cluster requires TLS (CA committed as `yandex-ca.pem`, expires 2027 — override with `SOURCE_CA_CERT_FILE`). |
