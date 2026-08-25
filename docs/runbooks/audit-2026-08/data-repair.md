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

That last point is why **the D1b list is a snapshot, not a work queue**.
Registration commits the mentor row with `sort_order` NULL and *then* fires
finalization from a goroutine — `trigger.CallAsync`
(`api/pkg/trigger/trigger.go`), called at
`api/internal/services/registration_service.go:203` after `CreateMentor`
returns. A registration that committed seconds before you ran the diagnostics
is in the report while its own finalization call is still in flight (the HTTP
client timeout is 30 s, `api/pkg/httpclient/client.go:33`, and the worker may
be restarting or backlogged on top of that). If that call lands after the
report is written, triggering from the static list runs finalization a **second**
time: it replaces the confirmation token the mentor was just emailed with a new
one, so the link in their inbox is already dead when they click it, and sends a
duplicate email.

Two guards, both cheap:

**(a) Only act on rows old enough that their own trigger has certainly
finished.** 15 minutes is comfortably past the 30 s client timeout plus a worker
restart.

**(b) Classify same-email duplicates before you trigger anything** — see the
`has_active_duplicate` column below and step 2a.

```sql
SELECT
    m.id,
    m.email,
    m.name,
    m.created_at,
    EXISTS (
        SELECT 1 FROM mentors a
         WHERE a.email = m.email AND a.status = 'active'
    ) AS has_active_duplicate
FROM mentors m
WHERE m.sort_order IS NULL
  AND m.status = 'draft'
  AND m.activated_at IS NULL
  AND COALESCE(m.airtable_id, '') NOT LIKE 'getmentor:%'
  -- (a): exclude registrations whose own fire-and-forget finalization may
  -- still be in flight. Do not lower this.
  AND m.created_at < now() - interval '15 minutes'
ORDER BY m.created_at;
```

`mentors.email` is `citext`, so `a.email = m.email` is case-insensitive — the
same comparison `CountActiveMentorsByEmail`
(`api/internal/worker/repository.go:175-180`) makes. A `draft` row never counts
itself, because that query filters `status = 'active'`.

### 2a. `has_active_duplicate = true` → do not trigger. `declined` is correct.

For those rows the worker will find the active sibling, set
`status = 'declined'` and send the "duplicate profile" email — and **that is the
right answer, not a mistake**. Someone already has a live profile on that
address; the second registration is the duplicate the check exists to catch.
`declined` is its correct terminal state.

So there is nothing to repair on those rows beyond the `COALESCE` fix in step 1,
which is what makes them loadable again. Options, in order of preference:

1. **Leave them.** Once P2 ships they no longer break anything. A `draft` row
   with a NULL `sort_order` is invisible to the catalog and harmless.
2. **Trigger it anyway** if you want the state written down explicitly. Expect
   `{"status":"declined"}` and a duplicate-profile email to the mentor — decide
   whether you want that email sent before you do it.

**Do not "fix" a `declined` duplicate with an UPDATE.** Setting it back to
`draft` re-opens exactly the duplicate the check closed, and setting it to
`active` is rejected by the database anyway: `mentors_active_email_uniq`
(`api/migrations/000001_initial_schema.up.sql`) is a UNIQUE index on `email`
`WHERE status = 'active'`. If the person genuinely needs the *new* row and not
the old one, that is a merge decision — deactivate the old profile first,
deliberately, and record why.

### 3. Trigger finalization, one mentor at a time

The worker is `openmentor-worker`, internal to the compose network, port 8090
(the `worker` service in `infra/docker-compose.yml`). Job routes live under
`/jobs` and require the `X-Worker-Token` header whenever `WORKER_AUTH_TOKEN` is
set — which is mandatory in production (`api/config/config.go:415-420`). The
token reaches the container through the worker's `environment:` allowlist, so
read it *inside* the container and it never touches your shell history or the
host process list.

**Step 3a — re-check the row, immediately before the call. On your workstation.**
The worklist is minutes or hours old by now: the reconciliation cron may have
shipped, the mentor's own trigger may have landed late, or a moderator may have
touched the row. One query, and it is the difference between a safe procedure and
one that mails a dead confirmation link.

```bash
# Must print exactly one row. NO ROW -> skip this mentor, do not call the
# worker: something already finalized it. Calling anyway would re-randomize
# sort_order, invalidate the confirmation link already sitting in their inbox,
# and send a second email.
./db.sh -c "SELECT id, email, status FROM mentors
             WHERE id = '<MENTOR-UUID>'
               AND sort_order IS NULL
               AND status = 'draft'
               AND activated_at IS NULL
               AND NOT EXISTS (SELECT 1 FROM mentors a
                                WHERE a.email = mentors.email
                                  AND a.status = 'active')"
```

**Step 3b — call the worker. On the VM.** Only for a mentor that just returned a
row.

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
wrong; `404` means the mentor id does not exist.

Confirm afterwards — **still in the same SSH session, so query Postgres
directly.** `./db.sh` is a workstation tool: it reads `VM_SSH_HOST`/`VM_SSH_USER`
from `infra/.env.production`, which does not exist on the VM (deployment writes
`.env` there — and, since P10, nothing else), so running it inside the SSH session exits
`❌ .../.env.production not found` instead of verifying anything.

```bash
docker exec openmentor-postgres psql -U openmentor -d openmentor -c \
    "SELECT status, sort_order, email_confirmation_expires_at
       FROM mentors WHERE id = '<MENTOR-UUID>'"
```

`sort_order` must now be non-NULL and `status` must still be `draft`.

**If it came back `declined`,** the mentor has an active row on the same email
and the duplicate check fired. That is the intended outcome, not an error, and
**it is not something to undo** — step 3a should have caught it, so the sibling
row went active in between. Read step 2a before touching anything.

### 4. The reconciliation cron

Audit item **P2** step 4 adds a `finalize-stuck-registrations` cron job that
sweeps `status='draft'` rows whose finalization never ran, so a lost trigger,
worker downtime or a deploy restart converges on its own.

It is registered in `Handlers.CronJobs()` (`api/internal/worker/cron.go`) on the
remediation branch, at `0 */10 * * * *` — every 10 minutes, because it is the
retry for a lost registration trigger and a locked-out new mentor must not wait
for a daily pass. **It only runs once the worker image carrying it is deployed**;
until then, use the per-mentor call in step 3.

`RegisterCronRoutes` exposes **every** entry of that table as
`POST /jobs/cron/<name>`, so forcing a run is:

```bash
docker exec openmentor-worker sh -c '
  curl -sS -X POST -H "X-Worker-Token: $WORKER_AUTH_TOKEN" \
    http://localhost:8090/jobs/cron/finalize-stuck-registrations
'
```

It returns the job's `JobSummary` as JSON (HTTP 500 with a partial summary on
error). Three cautions about that endpoint family:

- **`randomize-sort-order` will not fix a D1 row.** It selects
  `WHERE status = 'active'` (`ListActiveMentorIDs`), so a stuck `draft` or an
  imported `inactive` row is never touched. Don't reach for it.
- **`update-status-reminder` and `deactivate-pending-mentors` send email** (and
  the latter deactivates mentors). Do not trigger them to "see if it works".
- **Since `H3`, a manual trigger against an entity that has moved on is a
  no-op, not a redo.** Every worker write is a guarded claim, so
  `POST /jobs/new-mentor-watcher` on a mentor who is no longer `draft`,
  `POST /jobs/new-request-watcher` on a request that has already been processed
  or advanced past its first `pending`, `POST /jobs/mentor-moderation-action`
  whose action the mentor's current status contradicts, and a
  `deactivate-pending-mentors` pass over a mentor who left `active` all answer
  **200 with `"superseded":true`** (or count into `mentors_superseded`), write
  nothing and email nobody. That is the intended answer, not a failure — it is
  what stops a replay from reopening a closed request or delisting a live
  mentor. If you genuinely need the notification resent, fix the row's state
  first, or send the mail by hand.

  **One exception, once per request: requests announced BEFORE `H3` deploys.**
  The old write never stamped `status_changed_at`, so a request it already
  announced and whose mentor has not acted still reads as unclaimed — the first
  post-deploy `POST /jobs/new-request-watcher` against it wins the claim and
  re-sends all three announcement emails. It self-heals from there (that claim
  holds, and any mentor action stamps the column anyway), so there is nothing to
  repair — but don't replay a pre-deploy `pending` request expecting a no-op.

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
restored.

> **The price half of step 1 has shipped (D87).** `ProfileForm.tsx` no longer
> renders price as a `<select>` at all — it is a pill group over a closed
> grammar (`Free` / `Negotiable` / any whole `$1`–`$1000`), a radio group has no
> first option to fall into, and `mentors_price_chk` refuses anything outside
> the grammar. Two consequences for this runbook:
>
> - **Restoring a price no longer re-arms the bug.** `$30` is a value the form
>   renders and round-trips, so it is not "exposure" any more; the old warning
>   that restoring an off-list value put the mentor straight back at risk no
>   longer applies. D2b was repointed to match — it now flags rows the *grammar*
>   rejects, and expects zero.
> - **A restore must be canonical.** `$30 / hour`, `75 USD` and `''` are not
>   storable; the write-back will be refused by the constraint rather than
>   silently accepted. Convert to `$30`, `$75`, or ask the mentor.
>
> **`experience` is unchanged and still exposed** — see the paragraph below.
> Everything in §D2 continues to apply to it verbatim.

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

**There is no `aws` credential on the VM, so the S3 calls run inside the backup
sidecar.** `.env.production.example` states it outright ("The VM needs NO AWS
credentials"), and the backup keys are handed *only* to
`openmentor-postgres-backup` (`BACKUP_AWS_ACCESS_KEY_ID` /
`BACKUP_AWS_SECRET_ACCESS_KEY` / `BACKUP_AWS_REGION` in
`infra/docker-compose.yml`) — a deliberate split, since those keys can delete
every backup. That container is the one place with both the keys and `aws-cli`
(`infra/postgres-backup/Dockerfile` installs it), so run `aws` there. `backup.sh`
exports `AWS_*` from the `BACKUP_AWS_*` names inside its own process only, so a
`docker exec` has to do that mapping itself.

Everything below runs **on the VM** (`ssh <vm>`). Check `df -h` first: this
lands three copies of the dump on the VM (sidecar volume, host `/tmp`, restored
container).

```bash
# 1. What is actually there? Single quotes matter: the variables expand in the
#    container, so no key ever reaches your shell history or the host process list.
docker exec openmentor-postgres-backup sh -c '
  export AWS_ACCESS_KEY_ID="$BACKUP_AWS_ACCESS_KEY_ID" \
         AWS_SECRET_ACCESS_KEY="$BACKUP_AWS_SECRET_ACCESS_KEY" \
         AWS_DEFAULT_REGION="${BACKUP_AWS_REGION:-eu-central-1}"
  aws s3 ls "s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/"
' | sort

# 2. Pick the newest dump that PREDATES the corruption and pull it into the
#    sidecar's /backups volume. The name deliberately does NOT match
#    'openmentor-*.dump', so the retention pruner leaves it alone.
docker exec openmentor-postgres-backup sh -c '
  export AWS_ACCESS_KEY_ID="$BACKUP_AWS_ACCESS_KEY_ID" \
         AWS_SECRET_ACCESS_KEY="$BACKUP_AWS_SECRET_ACCESS_KEY" \
         AWS_DEFAULT_REGION="${BACKUP_AWS_REGION:-eu-central-1}"
  aws s3 cp "s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/openmentor-YYYYMMDD-HHMM.dump" \
            /backups/restore-candidate.dump
'
# 2b. Copy it out to the host — but NOT with a bare `docker cp`. `docker cp`
#     behaves like `cp -a` and applies the mode from the tar header it reads out
#     of the container, so the sidecar's 0644 (its `aws s3 cp` runs under the
#     ordinary 022 umask) lands on the host copy: a complete production database
#     dump readable by every local user on the VM. Setting `umask 077` does NOT
#     fix that — measured: `docker cp` of a 0644 file produced 0644 on the host
#     under both umask 022 and umask 077, and only the explicit chmod gave 600.
#     So refuse a path we did not create, create it under 077, and set the mode
#     ourselves before the dump exists at a readable one.
if [ -e /tmp/candidate.dump ] || [ -L /tmp/candidate.dump ]; then
  echo "refusing: /tmp/candidate.dump already exists — its mode and owner are not ours" >&2
else
  ( umask 077
    docker cp openmentor-postgres-backup:/backups/restore-candidate.dump /tmp/candidate.dump )
  chmod 600 /tmp/candidate.dump
  ls -l /tmp/candidate.dump   # must print -rw------- before you continue
fi

# 3. Restore into a throwaway container — never against production. It gets no
#    --network, so it cannot reach the production database.
docker run -d --name pg-pricecheck \
    -e POSTGRES_USER=openmentor -e POSTGRES_PASSWORD="$(openssl rand -hex 16)" \
    -e POSTGRES_DB=openmentor postgres:16.14-alpine
until docker exec pg-pricecheck pg_isready -U openmentor -q; do sleep 1; done
docker cp /tmp/candidate.dump pg-pricecheck:/tmp/c.dump
docker exec pg-pricecheck pg_restore -U openmentor -d openmentor /tmp/c.dump

# 4. Read the prices out of the restored copy.
docker exec pg-pricecheck psql -U openmentor -d openmentor -c \
    "SELECT id, email, price, experience, updated_at FROM mentors ORDER BY email"

# 5. Clean up when done — all three copies hold production personal data.
docker rm -f pg-pricecheck
rm -f /tmp/candidate.dump
docker exec openmentor-postgres-backup rm -f /backups/restore-candidate.dump
```

If you would rather do this on a workstation, that needs **your own** read
credentials for the backup bucket — do not copy the container's keys out. The
backup identity is deliberately separate from the app's S3 keys (SECURITY M12),
and it holds delete rights on every dump.

A `pg_restore` that ends without errors and gives sane row counts is the
verification. If it does not restore, try the next-older dump; if none restores,
say so out loud — that is `outstanding-decisions.md` §2, and it makes the
backup-alerting work urgent rather than scheduled.

#### Which rows may be written back — and it is fewer than you think

"Production says `Free`, the dump says `$100`, the dump is older" is **not**
evidence the bug did it. A mentor who deliberately switched to `Free` after the
dump was taken matches all three conditions exactly. §D2.2 above says D2a is a
candidate list, not a list of victims; that applies here too.

There is one hard filter, and it comes straight from the mechanism. **This
describes the form as it was when the clobbering happened** — the six-option
select is gone (D87), but the historical signature below is still how you read a
dump taken while it was live. The select offered `Free, $50, $100, $150, $200,
Negotiable` and was rendered
`<select {...register('price')} defaultValue={mentor.price}>`. If the stored
price **was** one of those six, the browser selected that option and the value
round-tripped correctly. The clobber can only have happened when the stored
price was **off-list**.

| Dump's price | Can the select bug explain production's `Free`? | Action |
|---|---|---|
| Off-list (`$30 / hour`, `75 USD`, `''`, …) | Yes — this is the bug's signature | Candidate for write-back |
| On-list and not `Free` (`$100`, `Negotiable`) | **No.** The option existed and would have been preselected | **Do not write back** — this was a deliberate change |
| `Free` | Nothing was lost | Nothing to do |

Even for the first row of that table, an off-list dump value is consistent with
the bug, not proof of it — the mentor could have set `Free` on purpose from an
off-list value. So require **one** of these before writing:

- **Mentor confirmation.** Ask: "our records show you charged `<X>` — is that
  still right?" One email, and it removes all doubt. Do this for anything you
  are not sure about; it is cheaper than getting it wrong.
- **Corroborating evidence that the save was not a price change** — for example
  the mentor emailed support about editing their bio around the `updated_at`
  timestamp, or a moderation note records the edit.

Write back **nothing** you cannot put in one of those two boxes. A price left
wrong is a bug the mentor can fix in ten seconds; a price you overwrote against
their intent is you editing someone's public listing without asking.

#### The statement — two steps, with the commit as a separate decision

Explicit, id-listed, read line by line. Do not join across the two databases and
do not write a clever bulk `UPDATE`. **No paste-able block below both opens a
transaction and commits it**, because the whole point of the guards is the count
you read in between.

Carry the `updated_at` you observed during triage into the `VALUES` list and
compare it. Without that, the statement races the mentor: if they edit their
profile after you take the list and the result is still `Free` (they edited their
bio, or they chose `Free` on purpose), `AND m.price = 'Free'` still matches and
you overwrite the newer value. Verified on a scratch database — with only the
price guard the stale write lands (`UPDATE 1`); with the `updated_at` guard it
does not (`UPDATE 0`).

**Step 1 — open the transaction and run the `UPDATE`. This block deliberately
ends without `COMMIT`.** Paste it whole; you will be left inside an open
transaction, which is where the decision gets made.

```sql
BEGIN;

UPDATE mentors AS m
   SET price = v.price
  FROM (VALUES
          -- id, price to restore, updated_at EXACTLY as triage printed it
          ('00000000-0000-0000-0000-000000000000'::uuid, '$30',
           '2026-08-01 09:14:22.518731+00'::timestamptz),
          ('00000000-0000-0000-0000-000000000001'::uuid, '$40',
           '2026-07-28 16:02:07.994612+00'::timestamptz)
       ) AS v(id, price, observed_updated_at)
 WHERE m.id = v.id
   AND m.price = 'Free'                        -- still the value you saw
   AND m.updated_at = v.observed_updated_at;   -- and nobody has touched it since
```

**Step 2 — read the count before you type anything else.** psql prints
`UPDATE <n>`. Compare `n` with the number of rows you listed in the `VALUES` list
(two, in the example above). Nothing is committed yet — but the rows the `UPDATE`
matched are locked until you decide, so decide now rather than walking away.

| `UPDATE <n>` | What it means | What to type |
|---|---|---|
| `n` = rows you listed | Every guard matched. This is the expected case | `COMMIT;` |
| `n` < rows you listed | A row moved since triage, an id is mis-typed, or somebody already repaired it. **A `COMMIT` here commits a partial repair** | `ROLLBACK;` then re-read those rows |
| `n` = 0 | None of the guards matched — your list is stale | `ROLLBACK;` and start from triage |

```sql
-- Only after the count above is what you expected:
COMMIT;
```

```sql
-- Anything else — including "n is smaller but the rows it did update look fine":
ROLLBACK;
```

`COMMIT` and `ROLLBACK` are in separate blocks on purpose. They used to sit at
the bottom of the `UPDATE` block with the "ROLLBACK and look again" instruction
as a SQL comment above them; pasting that block ran the `COMMIT` before the
operator had read the count, which is exactly the partial repair the guards exist
to prevent.

Copy `observed_updated_at` from psql's own output verbatim, including all six
fractional digits — `timestamptz` is microsecond-precision and a truncated value
simply will not match. Get it in the same query that gives you the price:

```sql
SELECT id, email, price, updated_at FROM mentors WHERE id IN (…);
```

The two guards turn a wrong id or a moved row into a non-match rather than an
overwrite — but they only protect you if you stop at step 2. A short
`UPDATE <n>` is the procedure working; `ROLLBACK`, re-read those rows, and do not
remove the guard to make the number bigger. `updated_at` moves on the rows you do
write; expected, the `trg_mentors_updated_at` trigger does that.

Step 2 holds row locks on whatever the `UPDATE` matched for as long as it takes
you to read one number — seconds, on at most a handful of rows, and a mentor
saving an unmatched profile is unaffected. If you would rather pin the whole id
list before deciding, the equivalent is `SELECT … FOR UPDATE` on it inside the
same transaction, re-checking `price = 'Free'` on what comes back before you run
the `UPDATE`. Either way the decision point is the same, and it is yours to make
explicitly.

### 4. Recovery source (b) — recompute for imported mentors

Applies only to rows carrying the import marker
(`airtable_id LIKE 'getmentor:%'`). `mapPrice`
(`infra/migration/migrate-mentors.js`) is deterministic, so given the
getmentor.dev source row you can recompute exactly what was imported.

> **The function below is the one that ran BEFORE D87 — keep using it here.**
> A pre-D87 import wrote its output verbatim, so these are the rules that
> reproduce what the corrupted row *used to hold*. The live `mapPrice` has
> since been rewritten to emit only the canonical grammar (leading amount
> respelled `$N`, out-of-range → `Negotiable`; pinned by
> `infra/migration/mapprice.test.js`), so re-running today's importer does NOT
> reproduce a pre-D87 value.

**Pre-D87 `mapPrice`, exactly, in order (historical transcription):**

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

**Before feeding the result into the §D2.3 write-back, make it canonical** —
`mentors_price_chk` refuses anything else, so this is enforced, not advisory:
a recomputed `$30 / hour` is written back as `$30`, `75 USD` as `$75`, and an
amount over `$1000` (or a value you cannot confidently convert) goes to the
mentor as a question, not into the column. The recomputed *historical* value
still belongs in your notes and in the mentor email ("our records show you
charged `$30 / hour`"); only the write-back is constrained.

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
  file. It runs `psql` inside `openmentor-postgres` over SSH. **Workstation only**
  — it needs `infra/.env.production` for `VM_SSH_HOST`/`VM_SSH_USER`, which the
  VM does not have. Once you are inside an SSH session, use
  `docker exec openmentor-postgres psql -U openmentor -d openmentor` instead.
- Any file you produce along the way (diagnostics report, restored dump, mentor
  worklist) contains personal data. Delete it when the repair is done, and keep
  it off shared drives — `../data-deletion.md` explains why that matters here.
