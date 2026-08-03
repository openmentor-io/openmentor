# Runbook: repairing what D1 and D2 find

**Trigger:** `diagnostics.sh` reported non-zero D1 or D2 counts. Read the whole
section before running anything in it — both repairs have a way to make things
worse, and both are called out in caps below.

D3 and D4 have no repair: D3 is an investigation (a link inside a person's
*name*, or a `javascript:` calendar URL, is an incident — see `diagnostics.sql`
D3), and D4 is an input to a design decision (`outstanding-decisions.md` §1).

---

## §D1 — mentors locked out (`sort_order IS NULL`)

### 1. Ship the code fix first. It is most of the repair.

`models.ScanMentor` (`api/internal/models/mentor.go:114,142`) scans `sort_order`
into a non-pointer `int` (`Mentor.SortOrder int`), so pgx rejects the **whole
row** when the column is NULL. Audit item **P2** adds
`COALESCE(sort_order, 0)` — the pattern already exists at
`mentor_repository.go:668` in `GetForModerationByID`.

**Apply it to all four query paths, not just login.** Verified in
`api/internal/repository/mentor_repository.go`:

| Query | Line | What it serves | Status filter? |
|---|---|---|---|
| `GetByEmail` | `:375-393` | magic-link login | no |
| `FetchSingleMentorFromDB` | `:542-567` | the **public profile page** at `/<slug>`, via `GetBySlug` (`:70`) | **no** |
| `FetchAllMentorsFromDB` | `:508-538` | the public catalog | `status = 'active'` |
| `fetchMentorByUUIDFromDB` | `:110-135` | mentor-by-UUID lookups | no |

`GetByLoginToken` (`:400`) scans into `*int` and is **not** affected.

So a NULL `sort_order` is worse than "cannot log in": the mentor's own public
profile page cannot be rendered either, for any status (`GetBySlug` falls back to
`mentor_slug_history`, finds nothing because the slug is current, and returns the
original error). That hits imported profiles hardest — `migrate-mentors.js`
emails the mentor a link to a page that does not load. If an **active** mentor
ever holds a NULL, the whole catalog listing fails, not just their row.
*(Verified at the repository layer by reading the code; the HTTP status the
handlers turn that error into was not exercised.)*

Once the fix ships, **every** D1 row works again — login, profile page, catalog.
No SQL, no trigger, no data change. For rows D1b classifies `imported_profile`
that is the entire repair: an imported profile inherited its NULL from
getmentor.dev (`infra/migration/migrate-mentors.js` carries `sort_order` over
verbatim and inserts `status='inactive'`); it was never a lost registration.

Verify per mentor after deploying:

```bash
# a login request for an affected mentor now issues a token
./db.sh -c "SELECT login_token IS NOT NULL AS has_token, login_token_expires_at
              FROM mentors WHERE email = 'someone@example.com'"
```

### 2. STOP — do not re-run finalization on every D1 row

Only rows D1b classifies **`stuck_registration`** may be re-finalized. Running
the finalization trigger on anything else damages the row. Verified in the code:

- `FinalizeNewMentor` (`api/internal/worker/repository.go:190-223`) writes
  `status = $4` **unconditionally**, keyed only on `id`. There is no status
  guard, and the `activated_at` hard guard lives only in `SetMentorStatus`
  (`:229-236`), which this path does not use.
- `NewMentorWatcher` (`api/internal/worker/job_new_mentor_watcher.go:52-190`)
  computes that status as `draft`, unless `CountActiveMentorsByEmail > 0`, in
  which case `declined`. That count is
  `SELECT count(*) FROM mentors WHERE email = $1 AND status = 'active'`
  (`repository.go:175-180`) and it does **not** exclude the mentor being
  processed.

| Row's current status | What the trigger actually does |
|---|---|
| `active` | Counts **itself** as a duplicate → sets `status='declined'` and sends the "duplicate profile" email. Your live mentor vanishes from the catalog. |
| `pending` / `inactive` (every imported profile) | Sets `status='draft'`, clears `login_token`, mints a new 24 h confirmation token, sends the confirm-email. An approved or imported mentor is pushed back behind the email-confirmation gate. |
| `draft`, never activated | The intended path: fills `sort_order`, mints the confirmation token, sends the confirm-email. |

It is also **re-runnable but not side-effect-free**: every run re-randomizes
`sort_order`, mints a *new* confirmation token (invalidating any link already
mailed) and sends another confirmation email. Run it once per mentor.

Get the worklist from the diagnostics report's `## D1b` section, or directly:

```sql
SELECT id, email, name, created_at
FROM mentors
WHERE sort_order IS NULL
  AND status = 'draft'
  AND activated_at IS NULL
  AND COALESCE(airtable_id, '') NOT LIKE 'getmentor:%'
ORDER BY created_at;
```

### 3. Trigger finalization, one mentor at a time

The worker is `openmentor-worker`, internal to the compose network, port 8090
(`infra/docker-compose.yml:298,304`). Job routes live under `/jobs` and require
the `X-Worker-Token` header whenever `WORKER_AUTH_TOKEN` is set — which is
mandatory in production (`api/config/config.go:415-420`). The token reaches the
container through `env_file: .env.runtime`, so read it *inside* the container
and it never touches your shell history or the host process list:

```bash
ssh <vm>
# NOTE the single quotes: $WORKER_AUTH_TOKEN is expanded by the container's
# shell, not yours. Never paste the token onto a command line.
docker exec openmentor-worker sh -c '
  curl -sS -w "\nHTTP %{http_code}\n" \
    -X POST -H "X-Worker-Token: $WORKER_AUTH_TOKEN" \
    "http://localhost:8090/jobs/new-mentor-watcher?mentorId=<MENTOR-UUID>"
'
```

Expect `HTTP 200` and a body like
`{"success":true,"mentorId":"…","status":"draft"}`. A `401` means the token is
wrong; `404` means the mentor id does not exist. Confirm afterwards:

```bash
./db.sh -c "SELECT status, sort_order, email_confirmation_expires_at
              FROM mentors WHERE id = '<MENTOR-UUID>'"
```

`sort_order` must now be non-NULL and `status` must still be `draft`. **If it
came back `declined`, you ran it on the wrong row** — the mentor had an active
row with the same email. Fix it with an explicit, single-row update and check
the duplicate situation by hand before doing anything else.

### 4. The reconciliation cron

Audit item **P2** step 4 adds a `finalize-stuck-registrations` cron job that
sweeps `status='draft'` rows whose finalization never ran, so a lost trigger,
worker downtime or a deploy restart converges on its own.

**That job does not exist yet on `main`** — `Handlers.CronJobs()`
(`api/internal/worker/cron.go:45-52`) currently returns exactly four entries:
`sessions-watcher`, `update-status-reminder`, `deactivate-pending-mentors`,
`randomize-sort-order`. Until it ships, use the per-mentor call in step 3.

`RegisterCronRoutes` (`cron.go:86-102`) exposes **every** entry of that table as
`POST /jobs/cron/<name>`, so once the job lands, forcing a run is:

```bash
docker exec openmentor-worker sh -c '
  curl -sS -X POST -H "X-Worker-Token: $WORKER_AUTH_TOKEN" \
    http://localhost:8090/jobs/cron/finalize-stuck-registrations
'
```

It returns the job's `JobSummary` as JSON (HTTP 500 with a partial summary on
error). Two cautions about that endpoint family:

- **`randomize-sort-order` will not fix a D1 row.** It selects
  `WHERE status = 'active'` (`ListActiveMentorIDs`), so a stuck `draft` or an
  imported `inactive` row is never touched. Don't reach for it.
- **`update-status-reminder` and `deactivate-pending-mentors` send email** (and
  the latter deactivates mentors). Do not trigger them to "see if it works".

### 5. Email the affected people

From their side the product was broken: they filled in a registration form,
never got a confirmation email, and every login attempt told them a link was on
its way. They will not have complained, because nothing ever told them anything
was wrong.

- Re-running finalization does send the confirmation email — but it arrives
  days or weeks late, with no explanation. A short personal note first is
  better than a mystery email.
- The list is the D1b `stuck_registration` rows. It is likely to be small.
- Keep it plain: acknowledge the delay, say their profile is now waiting for
  them, give the link, offer to help. No marketing.

Nothing in the false-success login response needs data repair — P2's logging fix
handles the operator-facing half (the current log says "Login request for
unknown email", which sends whoever debugs it looking for a typo).

---

## §D2 — prices overwritten with `Free`

### 1. Order of operations — this one is not negotiable

1. **Fix the form first** (audit item **P4**: give `ProfileForm.tsx`'s `price`
   select a placeholder option, or make it controlled) and deploy it.
2. **Then** restore values.

Backwards, and the mentor's next profile save re-clobbers everything you just
restored. Note also that restoring a price to an off-list value like `$30` puts
that mentor straight back into the D2b exposure set — the off-list value *is*
the exposure. Either the form fix landed, or restoring makes the problem
recur.

**Fix `experience` in the same change.** `diagnostics.sql` D2d covers it: the
`experience` select (`ProfileForm.tsx:404-409`) has the identical defect and
silently rewrites any off-list value to `2-5`. It is not in the original audit
plan. Everything below about recovery applies to it too, with the same sources.

### 2. Why the database cannot tell you the old value

`mentors.price` is plain `TEXT`, overwritten in place. `updated_at` is
maintained by the `trg_mentors_updated_at` trigger, so you know *when* a row
last changed, never *what* changed. There is no history table, no audit log, no
soft-delete. **D2a is a candidate list, not a list of victims** — a mentor who
genuinely picked `Free` and later edited their bio appears in it too.

### 3. Recovery source (a) — a pre-corruption `pg_dump` from S3

The best source, **if** one exists and **if** it restores.

Layout (`infra/postgres-backup/backup.sh`, and
`../postgres-backup-restore.md`): nightly `pg_dump -Fc` to
`s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/openmentor-YYYYMMDD-HHMM.dump`,
default prefix `postgres`, pruned after `BACKUP_RETENTION_DAYS` (default 30).
**Check this early:** anything older than ~30 days is already gone, so if the
corruption predates the retention window there is no dump to find.

> **CONFIRM A DUMP RESTORES BEFORE RELYING ON IT.** Audit item **P8** is that
> backup failures are completely silent — the sidecar logs a `FAILURE` line and
> nothing alerts on it. "A file is listed in S3" is not "a dump restores", and
> a truncated or aborted upload lists just fine.

```bash
# 1. What is actually there?
aws s3 ls "s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/" | sort

# 2. Pick the newest dump that PREDATES the corruption and restore it into a
#    throwaway container — never against production.
aws s3 cp "s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/openmentor-YYYYMMDD-HHMM.dump" /tmp/candidate.dump
docker run -d --name pg-pricecheck \
    -e POSTGRES_USER=openmentor -e POSTGRES_PASSWORD="$(openssl rand -hex 16)" \
    -e POSTGRES_DB=openmentor postgres:16.14-alpine
docker cp /tmp/candidate.dump pg-pricecheck:/tmp/c.dump
docker exec pg-pricecheck pg_restore -U openmentor -d openmentor /tmp/c.dump

# 3. Read the prices out of the restored copy.
docker exec pg-pricecheck psql -U openmentor -d openmentor -c \
    "SELECT id, email, price, experience, updated_at FROM mentors ORDER BY email"

# 4. Clean up when done — it holds a full copy of production personal data.
docker rm -f pg-pricecheck && rm -f /tmp/candidate.dump
```

A `pg_restore` that ends without errors and gives sane row counts is the
verification. If it does not restore, try the next-older dump; if none restores,
say so out loud — that is `outstanding-decisions.md` §2, and it makes the
backup-alerting work urgent rather than scheduled.

Then write back **only** rows where production says `Free`, the dump says
something else, and the dump's `updated_at` is older than production's. Do it
with an explicit, id-listed statement you have read line by line. Do not join
across the two databases and do not write a clever bulk `UPDATE`.

```sql
BEGIN;

UPDATE mentors AS m
   SET price = v.price
  FROM (VALUES
          ('00000000-0000-0000-0000-000000000000'::uuid, '$30'),
          ('00000000-0000-0000-0000-000000000000'::uuid, '$40')
       ) AS v(id, price)
 WHERE m.id = v.id
   AND m.price = 'Free';   -- guard: no-op if the row already changed again

-- psql prints UPDATE <n>. If n does not equal the number of rows you listed,
-- something is different from what you expected: ROLLBACK and look again.
COMMIT;
```

The `AND m.price = 'Free'` guard is what makes this safe to get wrong: a
mis-typed id, or a row a mentor has edited since you took the list, simply does
not match. `updated_at` will move — expected, the trigger does that.

### 4. Recovery source (b) — recompute for imported mentors

Applies only to rows carrying the import marker
(`airtable_id LIKE 'getmentor:%'`). `mapPrice`
(`infra/migration/migrate-mentors.js:393-410`) is deterministic, so given the
getmentor.dev source row you can recompute exactly what was imported.

**`mapPrice`, exactly, in order (read the file; this is a transcription):**

1. `raw = price.trim()`.
2. `raw === ''`, or it matches `/бесплатно/i`, or `/^free$/i` → `'Free'`.
3. Matches `/договор/i` or `/negotiable/i` → `'Negotiable'`.
4. **RUB.** After removing *all* whitespace, matches `/^(\d+)(?:руб|р|₽)/i` →
   `usd = Math.max(5, Math.round(rub / RUB_TO_USD_RATE / 5) * 5)`, returns
   `'$' + usd`. So RUB is converted and then **rounded to the nearest $5, with a
   $5 floor**.
5. **Raw pass-through.** Matches `/^\$?\d+/` → returns `raw` **verbatim**. The
   test is not anchored at the end, so anything starting with digits (or
   `$digits`) survives whole, units and all. This is why the database holds
   strings like `$30 / hour` and `75 USD`, and why those values are off-list and
   therefore exposed to the form bug.
6. Anything else → `'Negotiable'`.

Two things that make this less certain than it looks:

- **The conversion rate is not recorded in the database.**
  `RUB_TO_USD_RATE` defaults to `100` (`migrate-mentors.js:143`) and is read
  from `infra/migration/.env`. The only place the rate actually used is written
  down is the migration run output, in a note line of the form
  `price: "<raw>" -> "$<usd>" (rate <rate> RUB/USD)`. If you do not have that
  log and cannot confirm the rate, **say the recomputed value is uncertain
  rather than writing a guess into production.**
- **A recompute reproduces what was *imported*, not necessarily what was
  *lost*.** If the mentor changed their own price after import, the imported
  value is not the pre-corruption value.

Getting the source value:

```sql
-- On openmentor: which imported mentor, and which source row?
SELECT id, slug, email, price, split_part(airtable_id, ':', 2)::bigint AS source_legacy_id
FROM mentors
WHERE airtable_id LIKE 'getmentor:%'
  AND price = 'Free';
```

```sql
-- On the getmentor.dev database (SOURCE_DATABASE_URL), read-only:
SELECT legacy_id, slug, price, experience FROM mentors WHERE legacy_id = <source_legacy_id>;
```

Then apply the rules above by hand. There will be very few rows (the dev
database had three distinct off-list values), and hand-application is auditable
in a way a throwaway script is not.

Two traps worth knowing before you try to shortcut this:

- **`--dry-run` will not do it for you on an already-migrated mentor.**
  `migrateMentor` looks up the import marker first and returns at
  `⏭️ Skipped: already migrated as <slug>` (`migrate-mentors.js:828-858`)
  *before* it ever calls `mapPrice`. `--dry-run` is genuinely read-only and
  genuinely prints the mapped record with the rate note — but only for a mentor
  that has **not** been migrated yet.
- **You cannot `require()` `mapPrice`.** The script has no `module.exports` and
  no `require.main` guard, so importing it executes the migration script.

And this path needs live read access to the getmentor.dev database
(`SOURCE_DATABASE_URL`). If that access is gone, so is this option.

### 5. If neither source works: ask the mentors

For what is probably a handful of rows, emailing the affected mentors to ask
them to set their price again is honest, quick, and correct. Do it **after** the
form fix is deployed — otherwise their save re-clobbers the value and you have
spent their goodwill for nothing.

---

## Notes

- Everything in `§D1` step 1 and `§D2` step 1 is a code change, not an
  operational one. Neither repair should start before its fix is deployed.
- `./db.sh` (in `infra/`) is the shortest path to production psql from a
  workstation; `./db.sh -c "…"` for one statement, `./db.sh < file.sql` for a
  file. It runs `psql` inside `openmentor-postgres` over SSH.
- Any file you produce along the way (diagnostics report, restored dump, mentor
  worklist) contains personal data. Delete it when the repair is done, and keep
  it off shared drives — `../data-deletion.md` explains why that matters here.
