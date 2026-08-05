# Runbook: deploying the seven Phase 1 audit PRs

**Status:** written 2026-08-05, before any of the seven deployed.
**Scope:** PRs #72–#78, deployed **one at a time** to the live single-VM stack.

This is the list of everything `./deploy.sh` will **not** do for you. Read §0
first so you know what the list is the complement of, then work down the order
in §2, doing one PR per sitting.

Related material, linked rather than duplicated:
[`infra/DEPLOYMENT.md`](../../infra/DEPLOYMENT.md) ·
[`infra/ENVIRONMENT_VARIABLES.md`](../../infra/ENVIRONMENT_VARIABLES.md) ·
[`grafana/README.md`](../../grafana/README.md) ·
[`audit-2026-08/`](audit-2026-08/) ·
[`secret-rotation.md`](secret-rotation.md) ·
[`postgres-backup-restore.md`](postgres-backup-restore.md)

---

## §0 — What `./deploy.sh` does on its own

`./deploy.sh [frontend|backend|infra|all] [--tag T] [--yes] [--dry-run] [--staging]`
— **default target is `frontend backend`, which does NOT sync `infra/`.**

| Step | Automatic |
|---|---|
| 1 | ECR login locally (`aws sts get-caller-identity` must succeed) |
| 2–3 | `docker build --platform linux/amd64` for `web/` and/or `api/`; tag = short git SHA |
| 4 | reads the VM's current tags when a target is skipped |
| 5 | `docker push` |
| 6 | **`infra` target only:** `rsync` `infra/` to `/opt/openmentor/infra` (excluding `.env*`, `logs/`, `alloy-secrets/`, no `--delete`), and restart alloy / rebuild the backup sidecar if their files changed |
| 7 | **always:** uploads `.env.production` + the image tags to the VM's `.env` (mode 600), backing up the old one |
| 7b | writes `alloy-secrets/postgres_secret_openmentor` if `POSTGRES_OBS_DSN` is set |
| 8 | ECR login on the VM, then runs `deploy-remote.sh`: `docker compose pull` → `up -d` → **`migrate` runs to completion, and `backend`/`worker` are held behind `depends_on: service_completed_successfully`** → 20 s settle → health checks (frontend, backend, worker, postgres, backup sidecar) → auto-rollback to the previous `.env` if an app check fails |
| 9 | one public probe through Traefik: `https://$DOMAIN/api/healthcheck`, 12 × 10 s, fatal |

**Migrations are automatic.** Every converge runs `migrate`, which calls
golang-migrate's `Up()` — nothing else. It never runs a `.down.sql`, and it
applies only versions **greater** than the one in `schema_migrations`.

**`./deploy.sh` does NOT:** apply Grafana alert rules or notification policies ·
sync `infra/` unless you ask for it · run any diagnostics or repair SQL · take a
pre-deploy backup · verify a backup restores · rotate any secret · change a
Postgres role or password · check anything outside the VM beyond that one probe.

---

## §1 — Cross-cutting facts, true for every PR below

1. **The `grafana/` diff on six of the seven branches is a mirage.** #72, #73,
   #75, #76, #77 and #78 were cut from `d6d7fc8`, before PR #79
   (`alert-folder-relocation`) landed on `main`. `git log <merge-base>..<branch> -- grafana/`
   is **empty on all six** — they contain no grafana commit, so a normal merge
   keeps main's version and nothing is reverted. **Do not squash-apply a raw
   diff** from these branches, and after each merge confirm
   `grafana/alerting/alert-rules.yaml` still says `folder: OpenMentor Alerts`
   (uid `openmentor-alerts`). #74 is based on current `main` and is the only PR
   with a real alert change.

2. **Alert rules are not Git-Synced.** Only `grafana/dashboards` is (repo
   `repository-7b3d712`, branch `main`, hourly). Rules live in folder
   **OpenMentor Alerts / uid `openmentor-alerts`**, group `openmentor`, and are
   applied by hand:
   ```
   PUT /api/v1/provisioning/folder/openmentor-alerts/rule-groups/openmentor
   Header: X-Disable-Provenance: true
   Body:   alert-rules.yaml's group as JSON {title, folderUid, interval: 60, rules}
   ```
   Deleting the folder silently deletes every rule in it — that is how all 14
   were lost on 2026-08-04. Verify with `GET /api/v1/provisioning/alert-rules`,
   never by assuming.

3. **Migration order is a hard constraint: #75 (`000010`) → #77 (`000011`) →
   #78 (`000012`).** `Up()` only walks forward, so if a higher number deploys
   first, production records that version and the skipped migration is **never
   applied in production** — with `migrate` exiting 0 and a green deploy. The
   divergence only surfaces later as a runtime error against a schema that never
   got the table. Recovery afterwards is renumbering or forcing the version by
   hand. #73 adds the CI guard that catches this mechanically, which is why it
   goes first.

4. **Run the preflight before #76.** [`infra/preflight-phase1.sh`](../../infra/preflight-phase1.sh)
   re-implements every per-binary startup condition #76 introduces. See §4.

5. **`infra/rollback.sh` is touched by both #73 and #74.** Expect a merge
   conflict on the second of the two; both changes must survive (the `flock` +
   `.env.lastgood` promotion from #73, and the migration-boundary guard from
   #74).

---

## §2 — Recommended deploy order

| # | PR | Why here |
|---|---|---|
| 1 | **#73** CI + deploy serialization | Lands the CI guard that mechanically enforces the `000010→000011→000012` order **before** any of those three PRs can merge out of sequence. Also puts the deploy `flock` and the health-verified `.env.lastgood` rollback target in place before six more deploys. No app code, no schema. |
| 2 | **#76** per-binary config validation | The highest startup risk in the set, and it deserves a deploy of its own so a failure is unambiguous rather than confounded with a schema change. Deploying it *early* also shrinks `migrate`'s environment to `DATABASE_URL` alone, removing the exact coupling that took production down on 2026-08-04 — worth having before three migration deploys. |
| 3 | **#72** replay-safe worker callbacks | No env, no migration, no schema, no wire-format change. A clean low-risk app deploy that exercises the new pipeline. |
| 4 | **#74** rollback guard + recovery runbooks + alerts | Must precede #78 (see below), and you want the migration-boundary guard in `rollback.sh` **before** the first migration deploy — that is precisely when a rollback becomes dangerous. |
| 5 | **#75** `000010_session_hardening` | First of the ordered three. |
| 6 | **#77** `000011_review_invitations` | Second. |
| 7 | **#78** `000012_split_database_identities` | Third, and the largest infra change in the set. |

**Why #74 before #78.** #78 trims cAdvisor's mounts — it removes `/:/rootfs:ro`
and narrows `/var/run` to two explicit sockets. cAdvisor's docker factory needs
`containerd.sock` to register, and if it fails to, every `name=` and
`container_label_*` label disappears. `ContainerHighCPU` and
`ContainerHighMemory` both select on `name=~"openmentor-.*"` with
`noDataState: OK`, so the series would vanish and the rules would go quietly to
Normal — monitoring that looks healthy because it is measuring nothing. Getting
#74's corrected rules live first means the post-#78 label check is protecting
something real.

> **Note on a claim you may have seen:** #74's own `grafana/README.md` says "No
> cAdvisor mount or flag change is needed" — that is about **#74**, which
> changes no mounts. It is not a statement about #78, which does. Both are true;
> they are about different PRs.

---

## §3 — PR #73 · `fix/audit-h5-h6-h9-ci` — CI supply chain, deploy serialization

**Migration:** none. **Schema effect:** none. **App behaviour:** unchanged.

### 3.1 Environment variables
**None.** No change to `api/config/config.go`, `infra/.env*.example`,
`infra/env-allowlist.txt` or `infra/docker-compose.yml`.

New *tooling* knobs, all optional, none reaching a container:
`DEPLOY_LOCK_WAIT` (default `900`, read on the VM), `MIGRATION_TEST_DATABASE_URL`,
`MIGRATION_TEST_PORT` (`55445`), `MIGRATIONS_DIR`, `MIGRATION_BASE_REF`
(`origin/main`), `MIGRATION_DOWN_EXEMPT`, `SHELLCHECK_SEVERITY` (`warning`).

### 3.2 Startup-blocking validation
**None.**

### 3.3 Migrations
**None.**

### 3.4 SQL to run manually
**None.**

### 3.5 Other manual setup

| When | Action |
|---|---|
| **Before** | **Confirm `flock` (util-linux) is installed on the VM.** A missing `flock` is **fatal** in all three writers (`deploy-remote.sh`, `rollback.sh`, `deploy.sh`'s env-swap block): `"flock (util-linux) is not installed on this VM — refusing to deploy unserialized."` + `exit 1`. Check with `ssh <vm> 'command -v flock'`. This is the one thing that can brick every subsequent deploy in this list. |
| **Before** | **Re-confirm `VM_SSH_HOST_KEY` is not just set but *correct*.** It exists (set 2026-07-18) and `gh secret list` proves presence, not correctness. The `ssh-keyscan` fallback is now removed and the secret is checked twice — in `validate-inputs` before any credential is minted, and again at the sync step. If the VM was ever rebuilt since 2026-07-18, every `Deploy` run dies at the first `ssh` with `Host key verification failed`. Re-capture with `ssh-keyscan -H <VM_SSH_HOST>` from a trusted network if in doubt. Only the manually-triggered `Deploy` workflow is affected — no PR can deadlock on it. |
| — | **Nothing to change in GitHub settings.** Verified against the live repo: the required check context is `required-checks` on ruleset `protected-main`, and the job name is **unchanged** — H12 only adds *steps* inside that job. `security-events: write` is already granted job-scoped in `ci-api.yml`, code scanning is already configured, and the repo is public so no Advanced Security purchase applies. H9's serialization is a workflow `concurrency: {group: production-deploy, cancel-in-progress: false}` plus the VM-side `flock` — **not** an environment protection rule. |
| **After** | Expect new Dependabot traffic: the `github-actions` ecosystem and `/infra/postgres-backup` are now watched weekly. |
| **After** | Know that `api/gosec-baseline.txt` fails on **stale** entries too, so a gosec version bump that stops emitting one of its 4 findings turns `CI / API → Security Checks` red until the file is pruned. That job is not a required check, so it cannot block a merge. |

### 3.6 User-visible behaviour
**None.** But the *operator* recovery command changes: `cp .env.backup .env`
becomes **`cp .env.lastgood .env && docker compose up -d`**. `.env.lastgood` is
written only after every application health check passes, so it is a target
known to have worked; `.env.backup.<epoch>` keeps the 5 newest snapshots.

### 3.7 Rollback
`./rollback.sh <sha>` is safe — no migration boundary exists yet. If the new
deploy scripts themselves misbehave, `./deploy.sh infra` from the previous
commit restores them; the VM-side files (`.deploy.lock`, `.env.lastgood`) are
inert to the old scripts.

---

## §4 — PR #76 · `fix/audit-h14-config-tests` — ⚠️ per-binary config validation

**Migration:** none. **This is the highest-risk deploy in the set.**

`config.Load()`/`Validate()` are gone. Each binary now calls
`config.LoadFor(...)` and **exits 1 at startup** if its own contract fails.
A `cmd/migrate` failure blocks the entire stack behind
`service_completed_successfully` — that is the 2026-08-04 outage shape, and it
is exactly what this PR exists to prevent recurring. But the PR also adds checks
a long-lived `.env` has never been held to.

### 4.0 ⚠️ RUN THIS FIRST

```bash
cd infra
./preflight-phase1.sh                                        # workstation, .env.production
./preflight-phase1.sh --env-file /opt/openmentor/infra/.env  # or on the VM
```

Exit `0` = safe · `1` = a binary will refuse to start · `2` = no startup
problem but read the warnings. It reports a verdict per key and **never prints a
value**. Do not deploy #76 until it exits 0 or 2.

### 4.1 ⚠️ Every startup-blocking condition, per binary

`IsProduction()` is `APP_ENV == "production"`, and **`APP_ENV` defaults to
`production`** — an unset `APP_ENV` gets the strict path. Validation is
fail-fast on the *first* error, so fixing one may reveal another.

Two coercion traps that make this sharper than it looks:
- **`GetInt` is `cast.ToInt`:** a non-numeric string silently becomes `0`. So
  `SESSION_TTL_HOURS=24h` is not "24 hours", it is `0`, and it now fails.
- **`GetBool` is `cast.ToBool`:** anything `strconv.ParseBool` cannot read
  silently becomes `false`. So `COOKIE_SECURE=yes` reads as **false** and fails
  the production check.
- An **empty** value (`KEY=`) is treated as *unset* by viper, so the
  `SetDefault` wins. Only a non-empty malformed value is dangerous.

#### `cmd/api` — `ValidateForAPI()`

| Key(s) | Rule | Gate | New? |
|---|---|---|---|
| `DATABASE_URL` | non-empty unless `DB_WORK_OFFLINE` is true | all | — |
| `ANALYTICS_PROVIDER` | resolves to `none` or `posthog` | all | — |
| `POSTHOG_API_KEY` | required when provider is `posthog` | all | — |
| `POSTHOG_HOST` **or** `POSTHOG_CAPTURE_ENDPOINT` | at least one, when `posthog` | all | — |
| `INTERNAL_MENTORS_API` | non-empty | all | — |
| `MENTORS_API_LIST_AUTH_TOKEN` | non-empty | all | — |
| `TURNSTILE_SECRET_KEY` | non-empty | all | — |
| `TURNSTILE_EXPECTED_HOSTNAME` | if set, a **bare** hostname — no `:`, `/` or space | all | **NEW** |
| `PORT` | non-empty (default `8081`) | all | — |
| `BASE_URL` | non-empty (default `https://openmentor.io`) | all | — |
| `BASE_URL` | absolute **https**, non-empty host, **no userinfo**, none of `"'<>`` \ space tab CR LF` | **prod** | **NEW** |
| `ALLOWED_CORS_ORIGINS` | ≥1 non-blank origin | all | — |
| `ALLOWED_CORS_ORIGINS` | no element equal to `*` | all | — |
| `JWT_SECRET` | non-empty | **prod** | — |
| `JWT_SECRET` | ≥ 32 bytes when set | all | — |
| `SESSION_TTL_HOURS` | integer **1–720** (720 = 30 d), default `24` | **all** | **NEW** |
| `LOGIN_TOKEN_TTL_MINUTES` | integer **1–120**, default `15` | **all** | **NEW** |
| `JWT_ISSUER` | non-empty, default `openmentor-api` | all | **NEW** |
| `COOKIE_SECURE` | must not be false (default `true`) | **prod** | **NEW** |
| `WORKER_AUTH_TOKEN` | non-empty | **prod** | — |
| **7 × `*_TRIGGER_URL`** (below) | all non-empty | **prod** | **NEW** |
| **8 × `*_TRIGGER_URL`** | if set, absolute **http or https** with a host, no userinfo, no unsafe chars | all | **NEW** |
| `MENTOR_CREATED_TRIGGER_URL` | must contain `new-mentor-watcher` | all | **NEW** |
| `S3_STORAGE_*` (5 keys) | 1–4 blank (partial) is rejected | **all** | — |
| `S3_STORAGE_*` (5 keys) | all 5 blank is rejected | **prod** | — |
| `O11Y_PROFILING_ENDPOINT` | required when `O11Y_PROFILING_ENABLED` | all | — |

**The seven required triggers:** `MENTOR_CREATED_TRIGGER_URL`,
`MENTOR_REQUEST_CREATED_TRIGGER_URL`, `MENTOR_LOGIN_EMAIL_TRIGGER_URL`,
`MODERATOR_LOGIN_EMAIL_TRIGGER_URL`, `MENTOR_MODERATION_TRIGGER_URL`,
`REQUEST_PROCESS_FINISHED_TRIGGER_URL`, `REVIEW_CREATED_TRIGGER_URL`.
The eighth, `MENTOR_UPDATED_TRIGGER_URL`, is **deliberately unset** in production
(the worker serves no such job) and is format-checked only.

**The derivation rule.** Two further triggers are not env vars at all — they are
built from `MENTOR_CREATED_TRIGGER_URL` by replacing the path segment
`new-mentor-watcher`:
`mentor-confirmed` and `mentor-confirm-email`. Derivation returns `""` (= "skip
the trigger") when the segment is absent, so a `MENTOR_CREATED` URL pointing at
any other job **silently disables both mentor confirmation emails**. #76 makes
that a startup failure instead of a silence.

#### `cmd/worker` — `ValidateForWorker()`

| Key(s) | Rule | Gate | New? |
|---|---|---|---|
| `DATABASE_URL`, analytics block | as `cmd/api` above | all | — |
| `WORKER_PORT` | non-empty (default `8090`) | all | **NEW** |
| `WORKER_AUTH_TOKEN` | non-empty | **prod** | — |
| `BASE_URL` | non-empty — it is rendered into **every** email link | all | **NEW** |
| `BASE_URL` | absolute https with a host | **prod** | **NEW** |
| `SES_REGION` / `SES_ACCESS_KEY_ID` / `SES_SECRET_ACCESS_KEY` | all 3 blank is rejected | **prod** | **NEW** |
| same 3 | 1–2 blank (partial) is rejected | **all** | **NEW** |
| `MODERATORS_EMAIL` | non-blank (default `moderators@openmentor.io`) | all | **NEW** |
| **`DEV_EMAIL_OVERRIDE`** | **must be empty** | **prod** | **NEW** |
| `DISCORD_MENTORS_PRIVATE_INVITE_LINK` | if set, absolute **https** | **all envs** | **NEW** |
| `O11Y_PROFILING_ENDPOINT` | required when profiling enabled | all | — |

> **`DEV_EMAIL_OVERRIDE` is the sneakiest one.** If any non-empty value is left
> in the production `.env`, `cmd/worker` **will not start** where it previously
> booted fine and merely rerouted every mentor's magic link to one mailbox.

#### `cmd/migrate` — `ValidateForMigrate()`

**Exactly one condition:** `DATABASE_URL` non-blank.
`DB_WORK_OFFLINE` is deliberately ignored — it cannot make migrations run
against nothing. `validateShared()` is not called, so migrate no longer
validates analytics either.

### 4.2 Environment variables

**Added — `backend` only** (both are optional, both default to empty/off):

| Key | Required | Default | If absent | Service |
|---|---|---|---|---|
| `TURNSTILE_EXPECTED_HOSTNAME` | optional | `""` (off) | nothing — hostname pinning stays off | backend |
| `TURNSTILE_EXPECTED_ACTION` | optional | `""` (off) | nothing | backend |

> ⚠️ **`TURNSTILE_EXPECTED_ACTION` ships documented but INERT — leave it unset.**
> The forms render the Turnstile widget with no `action`, so siteverify echoes an
> empty action back and **any** non-empty value rejects **every** public write
> (contact form, review submission, mentor registration). There is no config
> guard against this; the preflight script warns on it.

**Removed from `[migrate]` — 19 keys** (compose block *and* allowlist). These
were "validation-only", required solely because migrate called the shared
`Validate()`. Migrate's section is now exactly `APP_ENV`, `LOG_LEVEL`, `LOG_DIR`,
`DATABASE_URL` — **no credential of any kind**:

`DB_WORK_OFFLINE`, `S3_STORAGE_ACCESS_KEY`, `S3_STORAGE_SECRET_KEY`,
`S3_STORAGE_BUCKET`, `S3_STORAGE_ENDPOINT`, `S3_STORAGE_REGION`, `BASE_URL`,
`ALLOWED_CORS_ORIGINS`, `INTERNAL_MENTORS_API`, `MENTORS_API_LIST_AUTH_TOKEN`,
`TURNSTILE_SECRET_KEY`, `JWT_SECRET`, `WORKER_AUTH_TOKEN`, `ANALYTICS_PROVIDER`,
`ANALYTICS_EVENT_VERSION`, `POSTHOG_ENABLED`, `POSTHOG_API_KEY`, `POSTHOG_HOST`,
`POSTHOG_CAPTURE_ENDPOINT`

**Removed from `[worker]` — 10 keys:** `S3_STORAGE_ACCESS_KEY`,
`S3_STORAGE_SECRET_KEY`, `S3_STORAGE_BUCKET`, `S3_STORAGE_ENDPOINT`,
`S3_STORAGE_REGION`, `ALLOWED_CORS_ORIGINS`, `INTERNAL_MENTORS_API`,
`MENTORS_API_LIST_AUTH_TOKEN`, `TURNSTILE_SECRET_KEY`, `JWT_SECRET`.
(`BASE_URL` is **kept** — the worker genuinely reads it.)

> **Leaving those lines in `.env.production` is harmless** — they simply stop
> being passed to those containers. Removing them is optional tidying. What
> would fail is the reverse: a key in `docker-compose.yml` that is not in
> `env-allowlist.txt`. `cd infra && ./check-service-env.sh` reports the drift.

`check-service-env.sh`'s secret-owner map narrows in the same commit:
`JWT_SECRET`, `INTERNAL_MENTORS_API`, `MENTORS_API_LIST_AUTH_TOKEN`,
`TURNSTILE_SECRET_KEY`, `S3_STORAGE_ACCESS_KEY`, `S3_STORAGE_SECRET_KEY` →
`backend` only; `WORKER_AUTH_TOKEN`, `POSTHOG_API_KEY` → `backend worker`.

### 4.3 Migrations
**None.**

### 4.4 SQL to run manually
**None.**

### 4.5 Other manual setup
- Run the preflight (§4.0). That is the step.
- After deploying, confirm `docker logs openmentor-migrate` exits 0 and that
  backend and worker actually left `Created`. `deploy.sh` step 9 covers the
  public path, but migrate's exit is the one to read first.
- `cd infra && ./check-service-env.sh` after the merge, to confirm no allowlist
  drift.

### 4.6 User-visible behaviour

| Change | Old | New |
|---|---|---|
| Global request body cap | **none** on mentor-profile and admin-moderation POSTs | `512 KiB` (`DefaultMaxBodyBytes`) — sized for the worst-case legal profile save, because `validator`'s `max=` counts **runes** and JSON can spend 12 wire bytes on one |
| Image upload body cap | `10 MiB` | **`14 MiB`** (`MaxImageBodyBytes`) — an 8–10 MB photo used to pass the picker and `ValidateImage` and then die at the body reader with a generic error |
| Concurrent uploads in flight | `4` | **`3`** (`MaxUploadsInFlight`) — pays for the larger bodies; peak upload memory is unchanged at ~254 MiB of the 400 MiB `GOMEMLIMIT` |
| Next.js proxy `bodyParser.sizeLimit` | `'10mb'` | `'14mb'` on all three upload routes |
| **Advertised file limit** | 10 MB | **still 10 MB** — only the wire budget moved |
| Captcha token length | unbounded | `max=2048` → 400 on a longer token (Cloudflare's documented ceiling, should be inert) |
| Captcha outage messaging | a Cloudflare 5xx HTML page decoded to `Success:false` → users told "your captcha is invalid" | non-2xx is now a distinct error; users are no longer told to fix something they cannot |

**Support should expect:** slightly fewer simultaneous uploads at peak (3 rather
than 4, with the same 5 s queue wait), and photo uploads in the 8–10 MB range
now succeeding where they used to fail confusingly.

### 4.7 Rollback
`./rollback.sh <previous-sha>` is **safe** — no migration boundary. The rolled-back
image reverts to the shared validator, which demands the keys #76 removed from
migrate's compose block. **If you deleted those lines from `.env.production`,
put them back before rolling back**, or `migrate` fails on the old image and
blocks the stack. This is the strongest argument for leaving the stale lines in
place until all seven PRs are deployed.

---

## §5 — PR #72 · `fix/audit-h3-replay` — replay-safe worker callbacks

**Migration:** none. **Env vars:** none. **Schema:** none.

### 5.1–5.4 Env / validation / migrations / SQL
**All none.** The replay guards are compare-and-set predicates over columns that
already exist — `status_changed_at IS NULL` marks an unclaimed request, and
`client_requests.status_changed_at` has existed since `000001`. No idempotency
table, no nonce, no replay window, and therefore no migration.

No wire-format change either: `X-Worker-Token` / `WORKER_AUTH_TOKEN` are
untouched, and `./deploy.sh backend` rolls migrate + backend + worker from **one
image**, so there is no version-skew window. Every timestamp is server-side
Postgres `NOW()` in the same transaction as the guard, so **there is no clock or
NTP dependency to verify on the VM.**

### 5.5 Other manual setup

**One transitional behaviour to know about, once.** Requests announced *before*
this deploy never had `status_changed_at` stamped, so if the mentor has not
acted they still read as unclaimed. From `data-repair.md` on this branch:

> The first post-deploy `POST /jobs/new-request-watcher` against such a request
> **wins the claim and re-sends all three announcement emails** (mentee, mentor,
> moderators). It self-heals from there — but don't replay a pre-deploy
> `pending` request expecting a no-op.

Nothing needs doing; just don't manually replay old pending requests to "test"
the deploy.

### 5.6 User-visible behaviour
Invisible to users. Visible to **operators**: manual `POST /jobs/*` triggers
against an entity that has moved on now answer **`200` with `"superseded":true`**
(or count into `mentors_superseded`), write nothing and email nobody. That is
success, not failure. Applies to `new-mentor-watcher` on a non-`draft` mentor,
`new-request-watcher` on an advanced request, `mentor-moderation-action`
contradicted by current status, and `deactivate-pending-mentors` over a
non-`active` mentor.

The bug this fixes was live: a replayed `new-request-watcher` **reopened a
request the mentor had already declined or completed** and emailed all three
parties that a new request was waiting.

### 5.7 Rollback
`./rollback.sh <sha>` is safe. No schema change, no data written that the old
code cannot read — `status_changed_at` being populated earlier than before is
inert to the old binary (both reminder queries filter on
`status IN ('contacted','working')`).

---

## §6 — PR #74 · `fix/audit-h10-h11-h13-recovery` — rollback guard, runbooks, alerts

**Migration:** adds `000002_populate_tags.down.sql`. **Net DB effect of this
deploy: none.**

### 6.1 Environment variables
**None.** No compose change, no allowlist change. The new rollback guard reads
only `POSTGRES_USER`, `POSTGRES_DB` and `ECR_REGISTRY`, all already required.

### 6.2 Startup-blocking validation
**None.**

### 6.3 Migrations

- **`000002_populate_tags.down.sql` (new) — applies to nothing at deploy time.**
  `Up()` never executes a `.down.sql`, and production is already at version 9 so
  `Up()` returns `ErrNoChange`. It exists to close the last gap in the
  "every migration ships a `.down.sql`" rule that `rollback-migration-guard-test.sh`
  now enforces. It only ever runs when a human applies it by hand while unwinding
  to version 1. It is **lossy**: `tags.id` is referenced by `mentor_tags` with
  `ON DELETE CASCADE`, so deleting a seeded tag deletes every mentor's
  association with it, unrecoverably.
- The nine existing `*.up.sql` files each gain a line-1 `-- phase: expand` or
  `-- phase: contract` marker. golang-migrate does not checksum migration bodies,
  so this is inert on an applied database.

### 6.4 SQL to run manually
**None required.**

### 6.5 Other manual setup

| When | Action |
|---|---|
| **After — REQUIRED** | **Re-apply the alert rule group.** Four rules were rewritten (`ContainerHighCPU`, `ContainerHighMemory`, `DBErrorRate`, `DBLatencyP95`) and the YAML header marks them **PENDING RE-APPLY**. **Verified against the live stack on 2026-08-05: the OLD versions are what is evaluating** — the live `ContainerHighCPU` annotation still reads "threshold 90%" and `ContainerHighMemory` still reads "threshold 1GiB". All 14 rules are present and Normal in folder `openmentor-alerts`. Re-apply with the group `PUT` in §1.2, then diff the response against the file. |
| **After — before that PUT** | **Run each of the four new expressions as an instant query in Grafana Explore first.** Both container rules carry `execErrState: Error`, and Error state notifies through the live fan-out (telegram/slack/Discord, `repeat_interval: 4h`). `ContainerHighCPU` newly joins `on (instance) group_left () machine_cpu_cores`; if that metric is aggregated or `instance`-stripped on this tenant, the join errors and pages. The file's assurance that "nothing pages on arrival" is about **NoData**, not ExecError. |
| **After** | Note the four new rules' semantics changed, not just their thresholds: CPU is now a share of the whole VM at **>75%** (matcher widened to include `traefik|cadvisor`), memory is now a percentage of **each container's own `mem_limit`** at **>85%**, and both DB rules are now `sum by (service_name)` scoped to `openmentor-.*` so one service can trip them alone. |
| — | `cd infra && make check` now also runs `rollback-migration-guard-test.sh` and `alert-fireability-test.sh`. The latter is the **first infra check to require `yq`** — install it locally or `make check` fails. |

### 6.6 User-visible behaviour
**None.**

### 6.7 Rollback — and what this PR changes about rollback forever after

`./rollback.sh <sha>` is safe for this PR itself. What changes is the tool:

**`rollback.sh` now REFUSES a backend rollback that would cross a migration
boundary**, before it edits `.env` and before anything is pulled. It reads
`schema_migrations.version` from the running Postgres, reads the *target image's*
highest migration by `docker create` + `docker cp` (a container created and never
started), and compares. It refuses when the schema version exceeds what the
target image carries, and also when `schema_migrations.dirty = 't'` or when
`docker pull` of the target fails **for any reason** (including an ECR outage or
an expired token — not just a missing tag).

Why a refusal rather than a warning, in the script's own words: migrate shares
the backend tag, so golang-migrate would look for a version the image does not
contain, print `no migration found for version N`, exit 1,
`service_completed_successfully` would never complete, `set -e` would abort the
script **half-way, leaving the bad version live**. The old behaviour was worse
than no rollback at all.

**The manual procedure when it refuses** (`infra/DEPLOYMENT.md` §"Rolling back
across a migration boundary"):

```bash
cd /opt/openmentor/infra
BACKEND_TAG=$(grep '^BACKEND_IMAGE_TAG=' .env | cut -d= -f2)
ECR=$(grep '^ECR_REGISTRY=' .env | cut -d= -f2)

# 1. Get the down-migrations out of the RUNNING image (created, never started).
CID=$(docker create "$ECR/openmentor-backend:$BACKEND_TAG")
docker cp "$CID:/app/migrations/." ./migrations-tmp/
docker rm -f "$CID"

# 2. READ THE HEADER FIRST. "-- phase: contract" plus a LOSSY note means a
#    restore is the honest answer, not this procedure.
head -20 ./migrations-tmp/000010_session_hardening.down.sql

# 3. Apply it (ON_ERROR_STOP so a failure doesn't half-apply)
docker compose exec -T postgres \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 \
  < ./migrations-tmp/000010_session_hardening.down.sql

# 4. Point schema_migrations at the previous version. golang-migrate does not
#    notice the change on its own.
docker compose exec -T postgres \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -c "UPDATE schema_migrations SET version = 9, dirty = false;"

rm -rf ./migrations-tmp

# 5. Now it is no longer a crossing, and rollback.sh will run it.
```

Two gaps the runbook itself records or that follow from it:

- **`deploy-remote.sh`'s *automatic* health-check rollback has no such guard.**
  If a failed deploy applied a migration first, the auto-rollback restores the
  previous `.env`, points `migrate` at an image without that migration, and fails
  the same way. Documented as a "Known gap" in `DEPLOYMENT.md`.
- **`DEPLOYMENT.md`'s "Manual fallback on the VM"** (`sed -i` the tag then
  `docker-compose pull && up -d`) bypasses the guard entirely and is left
  unqualified ~40 lines after the section explaining why the crossing is fatal.
  Treat it as unsafe after #75.
- **The phase markers are read out of the *deployed* image**, so until a backend
  image built from #74 is running, orphaned migrations print as `[unmarked]` and
  are conservatively all counted as contract-phase. Fail-safe, but the message
  will over-state the problem on the first rollback window.

---

## §7 — PR #75 · `fix/audit-h1-h2-auth` — atomic magic links, JWT contract, revocation

**Migration:** `000010_session_hardening`. **First of the three ordered ones.**

### 7.1 Environment variables
**None added, removed, renamed, or re-defaulted.** `api/config/config.go`'s only
change is a comment. `infra/` is not in the diff at all.

Two **existing** keys become newly load-bearing:

| Key | Default | Why it matters now |
|---|---|---|
| `JWT_ISSUER` | `openmentor-api` | `iss` is now **validated** (`jwt.WithIssuer`) where it previously was not. See §7.6. |
| `SESSION_TTL_HOURS` | `24` | Unchanged by this PR — no session is shortened. |

### 7.2 Startup-blocking validation
**None new.** (#76's checks apply once #76 is deployed, whichever order.)

### 7.3 Migrations

`000010_session_hardening.up.sql` — four statements, all additive:

```sql
CREATE UNIQUE INDEX IF NOT EXISTS mentors_login_token_uniq
  ON mentors (login_token) WHERE login_token IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS moderators_login_token_uniq
  ON moderators (login_token) WHERE login_token IS NOT NULL;
ALTER TABLE mentors     ADD COLUMN IF NOT EXISTS session_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE moderators  ADD COLUMN IF NOT EXISTS session_version INTEGER NOT NULL DEFAULT 1;
```

Applied **automatically** by the deploy. No backfill, no data rewritten.
`ADD COLUMN NOT NULL DEFAULT` is metadata-only on PG 11+; the two index builds
are deliberately **not** `CONCURRENTLY` and take a `SHARE` lock (blocks writes,
not reads) for milliseconds on identity tables of this size.

Down (reversible, idempotent): drops both columns then both indexes. Losing the
counter values is harmless — a re-applied `000010` resets everyone to 1, which
is a valid version for tokens minted before the rollback.

> **Ordering hazard the down file does not cover:** the new binary *requires*
> `session_version` (`RETURNING ... session_version`, `SELECT status, session_version`).
> Rolling the **migration** back while the new **image** is still running breaks
> every magic-link login (500) and 503s every mentor mutation. Correct order is
> always **image first, then migration.**

### 7.4 SQL to run manually

**Optional, before deploy — costs nothing, and the index build is the only thing
in this PR that can fail outright:**

```sql
SELECT login_token, count(*) FROM mentors
 WHERE login_token IS NOT NULL GROUP BY 1 HAVING count(*) > 1;

SELECT login_token, count(*) FROM moderators
 WHERE login_token IS NOT NULL GROUP BY 1 HAVING count(*) > 1;
```

Returns: nothing, on a healthy database. Any row means the `CREATE UNIQUE INDEX`
will fail, `migrate` exits 1, and the stack stays down. These are SHA-256 digests
of 256-bit randoms so a collision is not a practical risk — but it is an
unguarded precondition and this check is free.

**Not required by this PR:** the `UPDATE ... SET session_version = session_version + 1`
statements in [`telemetry-leak-token-invalidation.md`](telemetry-leak-token-invalidation.md)
are *incident response*, not deploy steps. Do not run them as part of this deploy —
they would log people out for no reason.

### 7.5 Other manual setup

| When | Action |
|---|---|
| **Before** | **Confirm `JWT_ISSUER` in `/opt/openmentor/infra/.env`.** If unset, or set to `openmentor-api`, you are fine. If it is set to anything else, that value must be the one live tokens were minted with — `iss` is now validated. The preflight script (§4.0) reports this. |
| **After, +24 h** | **Remove the three grandfather branches** (issue #80): the missing-`aud` allowance, the missing-`session_version` allowance, and the `mcf_` plaintext-confirmation fallback. Until then, a pre-D57 plaintext confirmation token is still an accepted credential shape. This is a follow-up code change, not a config flip. |

### 7.6 ⚠️ Does anyone get logged out? — verified against the branch, not the PR body

**No mass logout. The claim holds.** Checked in code rather than taken on trust:

- The new parser requires **HS256 exactly**, plus `iss`, `exp`, `iat`, a UUID
  subject consistent with `mentor_uuid`, and the realm's `token_type`. Every
  live token already satisfies all of these — `generateToken` has set
  `Issuer`, `Subject == MentorUUID`, `exp`/`iat`/`nbf` and HS256 in both
  historical versions of `pkg/jwt`, and `mentors.id`/`moderators.id` are
  `UUID PRIMARY KEY DEFAULT gen_random_uuid()`.
- The **two genuinely new claims are both optional by explicit code.** A
  *missing* `aud` passes (`if len(aud) > 0 && !slices.Contains(...)` — only a
  *wrong* audience is rejected; `jwt.WithAudience`, which would require it, is
  deliberately not used). A missing `session_version` decodes to `0` and
  `sessionVersionCurrent` returns true for `0`.
- Pre-M13 tokens with no `token_type` are still accepted.
- **`JWT_SECRET` is untouched** — no code reads or rotates it differently.
- Cookie names and attributes are unchanged (`mentor_session`, `admin_session`,
  `HttpOnly`, `SameSite=Lax`). `SESSION_TTL_HOURS` is unchanged. A new 30 s
  `jwt.WithLeeway` slightly *extends* acceptance and never shortens it.

**Who does get signed out — a handful of people, all intentionally:**
1. A moderator whose DB `role` differs from their token's role (promotions too, not just demotions) → 401.
2. A moderator whose row was deleted → 401.
3. A mentor whose status is now `declined` → 403 on mutations and on the requests inbox.
4. **Everybody, during a database blip** — mutations and admin requests return **503**, not 401, and the cookie is deliberately *not* cleared. New failure mode: the admin panel is fully unusable during a DB outage.

**Other user-visible changes support should expect:**
- **Logout is now global across devices.** `RevokeSession` bumps `session_version`, invalidating every token for that identity. Logging out on a laptop logs you out on a phone. Intended, but a real product change.
- Double-clicking a magic link: the second click now 401s. Previously it could mint a second 24-hour session — measured at 7 sessions from 16 concurrent verifications.
- **"Expired" and "already used" are no longer distinguishable.** Both are `invalid_token` and the client sees the same 401. Accepted consequence of moving the expiry check into the SQL.
- A single-use token presented by an ineligible account is now **spent**, not handed back.
- The `expired` label on `MentorAuthVerifyRequests` stops being emitted; a `consume_failed` label appears. No Grafana rule references either.

### 7.7 In-flight confirmation links — they survive

Confirmation tokens (`mentors.email_confirmation_token`, `mcf_` prefix) move from
**plaintext to hashed** at rest. Magic-link tokens (`mtk_`, `atk_`) were *already*
hashed and are byte-identically unaffected.

A confirmation link emailed before the deploy and clicked after it **still works**:
every read predicate is `(email_confirmation_token = $hash OR email_confirmation_token = $legacy)`,
where `$legacy` is the submitted plaintext only when it carries the `mcf_` prefix.
The prefix scoping is the security property — an unscoped "submitted == stored"
fallback would let anyone holding a database dump submit the stored digest.
There is no backfill (plaintext cannot be recovered from a hash) and none is
needed; the tokens live 24 h. Delete the fallback one day after the deploy.

The worker can no longer read a link back out of the row, so the confirm URL now
travels in the `mentor-confirm-email` trigger payload. Resend refuses with 409
(`outcome=no_confirmation_token`) rather than emailing a digest.

### 7.8 Rollback

**After this deploy, `./rollback.sh` across #75 is deliberately blocked** by #74's
guard (assuming #74 is deployed first, per §2), and it is blocked *even though
`000010` is purely additive*.

That is worth being clear about, because the instinct is wrong. The schema being
harmless to the old code is not the constraint — `migrate` shares
`BACKEND_IMAGE_TAG`, so a pre-#75 image carries migrations only up to 9 while
`schema_migrations` says 10. golang-migrate's `Up()` cannot find version 10 in
that image, prints `no migration found for version 10`, exits 1,
`service_completed_successfully` never completes, and the old `set -e` behaviour
aborted the rollback **half-way with the bad version still live**. So there is no
"just roll the image back and leave the schema alone" option through the tool.

**The only path is the manual procedure in §6.7**, with
`000010_session_hardening.down.sql` and `version = 9`, after which the crossing no
longer exists and `rollback.sh` will run. Order matters within it: **image first,
then migration** (§7.3) — dropping `session_version` under a still-running new
binary breaks every login.

Read the guard's own refusal message rather than reasoning from here; it names the
orphaned migrations and prints their phases.

---

## §8 — PR #77 · `fix/audit-h4-capability` — review capability token

**Migration:** `000011_review_invitations`. **Second of the three ordered ones —
deploy only after #75 is applied in production.**

### 8.1 Environment variables

| Key | Required | Default | If absent | Service |
|---|---|---|---|---|
| `REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED` | **optional** | `true` | Nothing. Compose declares it as a bare `- KEY`, which renders away when unresolvable, so the binary falls back to the viper default. Absent == present-and-true. No fail-closed risk. | **backend only** |

Added to `.env.example`, `.env.production.example`, `env-allowlist.txt`,
`docker-compose.yml` and `ENVIRONMENT_VARIABLES.md`. Setting it to `false` is
**the cutover**, which is a separate, later, gated decision (§8.5) — not part of
this deploy.

Not a new variable but newly load-bearing: the review email link host now comes
from `BASE_URL` (fallback `https://openmentor.io`) where it was previously
hardcoded in the template JSON. If production's `BASE_URL` is not exactly the
canonical origin, review link hosts change with this deploy.

### 8.2 Startup-blocking validation
**None new.**

### 8.3 Migrations

`000011_review_invitations.up.sql` — applied **automatically**:

```sql
CREATE TABLE IF NOT EXISTS review_invitations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  client_request_id UUID NOT NULL REFERENCES client_requests(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  consumed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT review_invitations_token_hash_len_chk CHECK (char_length(token_hash) = 64)
);
CREATE UNIQUE INDEX IF NOT EXISTS review_invitations_token_hash_key
  ON review_invitations (token_hash);
CREATE INDEX IF NOT EXISTS review_invitations_client_request_id_idx
  ON review_invitations (client_request_id);
ALTER TABLE client_requests
  ADD COLUMN IF NOT EXISTS review_legacy_link_revoked_at TIMESTAMPTZ;
```

**Additive and safe to deploy ahead of the code.** No backfill, no `NOT NULL` on
an existing column, nothing dropped. Both indexes are on a table created empty in
the same transaction (no `CONCURRENTLY` needed, and none was written — it cannot
run inside a transaction anyway). The one touch to an existing table is a
nullable `ADD COLUMN` with no default: metadata-only on PG 11+.

Down: `DROP TABLE review_invitations;` + `DROP COLUMN review_legacy_link_revoked_at`.

> ⚠️ **The down file understates the damage.** It warns that dropping the table
> discards every outstanding capability. It does **not** warn that dropping
> `review_legacy_link_revoked_at` **destroys the revocation records** — so a
> rollback silently **un-revokes the 12 leaked request ids** and makes those
> legacy links live again. If §8.4 has been run and you roll back, **§8.4 must be
> re-run.**

### 8.4 SQL to run manually — revoking the 12 leaked request ids

**AFTER deploy** (the column does not exist until `000011` applies). The runbook
calls this "the only remedy available for them"; nothing enforces it, and
skipping it leaves 12 live bearer capabilities readable by anyone with PostHog
read access for up to 12 months. **Treat it as required.**

The 12 UUIDs are deliberately not in the repository — it is public, and
committing them would publish the leak instead of closing it.

**Step 1 — recover the list from PostHog** (HogQL, not our schema):

```sql
SELECT DISTINCT
  extract(properties.$current_url, 'request_id=([0-9a-f-]{36})') AS request_id
FROM events
WHERE properties.$current_url LIKE '%request_id=%'
  AND timestamp > now() - INTERVAL 400 DAY
```
```sql
SELECT DISTINCT distinct_id FROM events
WHERE distinct_id LIKE 'request:%' AND timestamp > now() - INTERVAL 400 DAY
```

Union the two, strip the `request:` prefix, keep the list in a scratch file you
delete afterwards. **Do not paste it into a ticket, a chat message or a commit.**
The `[0-9a-f-]{36}` class also matches non-UUID text of the right length — treat
the output as a candidate list.

**Step 2 — revoke.** Run via `docker compose exec -T postgres psql -U openmentor -d openmentor`:

```sql
BEGIN;

UPDATE client_requests
   SET review_legacy_link_revoked_at = now()
 WHERE id = ANY(ARRAY[
         '00000000-0000-0000-0000-000000000000'  -- replace with the real uuids
       ]::uuid[])
   AND review_legacy_link_revoked_at IS NULL
RETURNING id, status,
          EXISTS(SELECT 1 FROM reviews r WHERE r.client_request_id = client_requests.id)
            AS already_reviewed;

COMMIT;
```

Returns one row per revoked request. The count must equal the number of ids you
pasted, **minus any no longer in the table and minus any already revoked** (the
`IS NULL` guard excludes those too — the runbook omits the second subtraction).
Effect: the legacy path now answers for that request exactly as for an unknown
one — 404, **no mentor name** — while a properly issued token for the same
request still works.

> 🐛 **This differs from the runbook on purpose.** The version in
> `review-capability-cutover.md` §1 writes the subquery as
> `EXISTS(SELECT 1 FROM reviews r WHERE r.client_request_id = id)` with **`id`
> unqualified**. SQL binds the inner scope first and `reviews` has its own `id`
> column, so the predicate degenerates to `r.client_request_id = r.id` and
> `already_reviewed` is **always `false`**. No error is raised. Step 3 then says
> "a row whose `already_reviewed` came back `true` needs nothing" — with the
> column always false, you would reissue for every revoked request, including
> ones already reviewed, sending needless duplicate emails for invitations that
> can never be spent. **Use the qualified form above.**

**Step 3 — the reissue replay**, for rows that are *not* already reviewed. Safe
for a request in status `done`: it appends a new invitation row and sends one
`session-complete` email. It does **not** invalidate any invitation already
outstanding.

> 🐛 **The runbook's command does not work as written**, on two counts:
> `-X POST` (the route is registered **GET-only**, so gin returns 404 and `curl -fsS`
> exits non-zero with no body), and `http://openmentor-worker:8090` (the worker
> publishes no ports, so that name resolves only from inside the compose
> network). Use:

```bash
docker exec openmentor-worker sh -c '
  curl -fsS -w "\nHTTP %{http_code}\n" \
    -H "X-Worker-Token: $WORKER_AUTH_TOKEN" \
    "http://localhost:8090/jobs/request-process-finished?requestId=<uuid>"
'
```

Single quotes matter: `$WORKER_AUTH_TOKEN` expands in the container's shell, not
yours, so the token never reaches your shell history or the host process list.
Do not paste this into a shared terminal recording — the `requestId` is itself
the leaked capability.

**Also relevant, unchanged and still outstanding:** the pre-existing
[`audit-2026-08/`](audit-2026-08/) diagnostics. `D4` (completed requests with no
review) is the question this PR was sized against, and `outstanding-decisions.md`
§1 is now answered by the `legacy_only_outstanding` query in §8.5 rather than by
`D4`. The `pre-deploy-checks.md` §1 `http://` calendar-link count and the D1/D2
repairs in `data-repair.md` are unrelated to these seven PRs and neither gate nor
are gated by them.

### 8.5 Other manual setup — and what is *not* part of this deploy

**The cutover is NOT in this PR.** Flipping
`REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED=false` (which makes the legacy path
answer 410 Gone) is a later operator action gated on two things, per
`review-capability-cutover.md` §2:

1. `sum(increase(openmentor_review_legacy_link_uses_total{outcome="accepted"}[30d]))`
   at zero for longer than the 30-day token TTL.
   > The runbook says "any non-zero value is a real mentee using a real old
   > link." **It overstates it** — the counter increments at the *top* of
   > `CheckReview`/`SubmitReview`, before any lookup, so it also counts unknown
   > UUIDs, crawlers and probes. And **no alert rule ships for this metric**; the
   > gate is a manual dashboard read.
2. The `legacy_only_outstanding` count — small enough to accept losing, or
   reissued first. This query is schema-verified correct:

```sql
SELECT count(*) AS legacy_only_outstanding
  FROM client_requests cr
 WHERE cr.status = 'done'
   AND cr.review_legacy_link_revoked_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM reviews r WHERE r.client_request_id = cr.id)
   AND NOT EXISTS (SELECT 1 FROM review_invitations i
                    WHERE i.client_request_id = cr.id
                      AND i.consumed_at IS NULL
                      AND i.expires_at > now());
```

> 🐛 **The runbook's post-cutover verification is also wrong.** It suggests
> `curl -X POST .../api/v1/reviews/<uuid> -d '{}'` expecting 410. The handler
> binds the JSON **before** reaching the service, and the request struct requires
> `mentorReview` (`min=10`) and `captchaToken` — so `-d '{}'` returns
> **400 Validation failed**, before and after the flip, and the gate is never
> reached. An operator following it literally concludes the cutover did not take.
> Probe the **check** endpoint instead:
> ```bash
> docker exec openmentor-backend curl -si \
>   http://localhost:8081/api/v1/reviews/<any-uuid>/check | head -1
> ```
> 410 after the flip; 404 or 200 before.

### 8.6 User-visible behaviour — the review link format changes

| | |
|---|---|
| **Old** | `https://openmentor.io/reviews/new?request_id={{request_id}}` |
| **New** | `https://openmentor.io/reviews/new#review_token=rvw_<43 base64url chars>` |

The token is 32 bytes from `crypto/rand`, stored **only** as a hex sha256 in
`review_invitations`, single-use, expiring after 30 days. It travels in the URL
**fragment**, so it is never sent to the server in a request line and never lands
in an access log or in `$current_url` telemetry.

**Already-emailed old links keep working.** This is an expand → dual-read
release: the legacy `GET /api/v1/reviews/:requestId/check` and
`POST /api/v1/reviews/:requestId` are retained, the web page still reads
`router.query.request_id` when there is no fragment token, and both BFF routes
keep an explicit legacy branch. An old link stops working in exactly three cases:
the operator flips the switch (410), that request was revoked in §8.4 (404, no
mentor name), or a review already exists (409).

**What support should say:**
- *Default state:* the old link still works — click it.
- *One of the 12 revoked ids ("Request not found"):* dead by design. Remedy is
  the §8.4 step-3 reissue; tell the mentee to use the **newest** email.
- *After the cutover (410, message names `hello@openmentor.io`):* same remedy.
- *Already reviewed:* nothing needed, the link was already dead.
- Note these links go to the **mentee**, not the mentor.

**One monitoring change:** the legacy submit race previously surfaced the unique
violation as an untyped error → **500 "Failed to save review"**. It is now
`ErrReviewIneligible` → **409**. Anything keyed on that 500 will go quiet.

### 8.7 ⚠️ Deploy both images in one run

New public endpoints `POST /api/v1/reviews/check` and
`POST /api/v1/reviews/submit`; the BFF's `/api/reviews/check` changes **GET →
POST**. Neither deploy order is fully safe:

- **Backend/worker first:** the worker immediately starts mailing
  `#review_token=` links, and the old frontend does not read `location.hash` at
  all → the page renders *"Invalid link — the request ID is missing"*. **Every
  review email sent in that window is a dead link to the mentee**, until the
  frontend ships (the token itself stays valid for 30 days, so re-clicking later
  works).
- **Frontend first:** the token path cannot trigger yet (no new emails exist), so
  the practical break is limited; legacy links still work end to end.

**Use a single `./deploy.sh` covering both**, and confirm `000011` actually
applied before the API takes traffic. If you must split, frontend first.

### 8.8 Rollback

`./rollback.sh` across #77 is **blocked** by #74's guard, for the same reason as
§7.8: a pre-#77 image carries migrations only to 10 while `schema_migrations` says
11, so `migrate` would fail at the gate. D61's "safe to leave in place if the code
is rolled back" is a statement about the *schema* being harmless to old code — it
is not a statement that the tool will let you move the image backward.

Use the §6.7 manual procedure with `000011_review_invitations.down.sql` and
`version = 10`, then **re-run §8.4** — the down migration drops
`review_legacy_link_revoked_at` and therefore **un-revokes the 12 leaked ids**.

Before reaching for a rollback at all, consider the cheaper undo: most of what
could go wrong in #77 is reversible with the feature flag rather than the image.
Setting `REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED` is a config flip and a restart,
and the legacy code path is still present — that is exactly why D61 rejected
making the cutover a Go constant.

---

## §9 — PR #78 · `fix/audit-h7-h8-infra` — socket proxy, container hardening, DB identities

**Migration:** `000012_split_database_identities`. **Last of the three ordered
ones — deploy only after #75 and #77 are applied in production.**

This is the largest infra change in the set and the only one that recreates every
container, including `postgres` (~10–20 s of database downtime).

### 9.1 Environment variables

**Five new keys, all optional, all with a fallback to today's value.** Deploying
#78 without setting any of them reproduces pre-H8 behaviour byte for byte.

| Key | Consumed by | Required | Default | If absent | If wrong |
|---|---|---|---|---|---|
| `MIGRATE_DATABASE_URL` | `migrate` → its `DATABASE_URL` | optional | `${DATABASE_URL}` | pre-H8 behaviour | `migrate` exits non-zero → backend + worker never start → **full outage** |
| `API_DATABASE_URL` | `backend` | optional | `${DATABASE_URL}` | pre-H8 | backend won't start; `deploy.sh` step 9 fails the deploy |
| `WORKER_DATABASE_URL` | `worker` | optional | `${DATABASE_URL}` | pre-H8 | worker unhealthy; async jobs + transactional email stop, site keeps serving |
| `BACKUP_POSTGRES_USER` | `postgres-backup` → `POSTGRES_USER` | optional | `${POSTGRES_USER:-openmentor}` | pre-H8 | `pg_dump` fails; sidecar stays healthy until `BACKUP_MAX_AGE_HOURS` then goes unhealthy → deploy/rollback exit 2 |
| `BACKUP_POSTGRES_PASSWORD` | `postgres-backup` → `POSTGRES_PASSWORD` | optional | `${POSTGRES_PASSWORD}` | pre-H8 | same; if both are empty the sidecar exits immediately |

Also new: a `[docker-socket-proxy]` section in `env-allowlist.txt` (required —
`check-service-env.sh` fails on a compose service with no section). The proxy's
four switches (`VERSION`, `PING`, `CONTAINERS`, `EVENTS`, all `=1`) are
**hardcoded in compose**, not read from `.env`. Removing any of them makes
Traefik's docker provider supply no configuration and **every request 404s**.

No `om_*` role name ever appears in an env *key* — the roles live only inside the
DSN *values* the operator writes.

### 9.2 Startup-blocking validation
**None new** from this PR.

### 9.3 Migrations

`000012_split_database_identities` is applied **automatically** and **changes
nothing about how anything authenticates.** It creates five roles —
`om_migrate` (owns the schema, CREATE on the database so trusted extensions still
work), `om_api` and `om_worker` (SELECT/INSERT/UPDATE/DELETE only; no TRUNCATE,
REFERENCES, TRIGGER or CREATE, and read-only on `schema_migrations`), `om_backup`
(`pg_read_all_data`, which is what `pg_dump` needs and carries no
`COPY ... FROM/TO PROGRAM`), and `om_monitor_ro` (a grant-only group role) —
**`NOLOGIN` and passwordless**. It reassigns object ownership to `om_migrate`,
grants the DML sets, and sets default privileges for both creators. It is
idempotent and **never touches LOGIN/PASSWORD on re-run**, so it cannot lock out
a live service.

The switch is the operator's, in §9.4 and §9.5.

Down migration: `REASSIGN OWNED BY om_migrate TO CURRENT_USER`, then per role
`DROP OWNED BY` / `REVOKE ALL ON DATABASE` / `DROP ROLE`. Its own header:
*"DANGER: this drops the roles the services may already be authenticating as. Run
it ONLY after every DSN has been pointed back at `POSTGRES_USER` and the
containers recreated."* It requires the superuser, and applying it by hand leaves
`schema_migrations` at 12 so the up never re-runs — **neither runbook gives a
procedure for it.**

**Side effect to know about:** dumps now carry `OWNER TO om_migrate`, so a
restore into a fresh cluster must apply `000012` first or use `--no-owner`.
`postgres-backup-restore.md` is updated accordingly.

### 9.4 SQL to run manually — REQUIRED before any DSN change

**Step 1 (after deploy, before adding any `*_DATABASE_URL` line).** The roles
exist but cannot log in:

```bash
cd infra && ./db.sh
```
```
ALTER ROLE om_migrate LOGIN;
ALTER ROLE om_api     LOGIN;
ALTER ROLE om_worker  LOGIN;
ALTER ROLE om_backup  LOGIN;
\password om_migrate
\password om_api
\password om_worker
\password om_backup
```

(`om_monitor_ro` stays `NOLOGIN` by design. A bare `./db.sh` with a TTY runs
`psql` interactively inside the container over SSH, so `\password` works — which
matters, because it never puts the password on a command line.)

> ⚠️ **Do not generate these with `openssl rand -base64 32`.** The runbook says
> to, and then pastes the result into a **URL** DSN
> (`postgres://om_api:<pw>@postgres:5432/...`). base64 emits `/`, `+` and `=`;
> a `/` in the userinfo breaks URI parsing (≈50 % chance in 43 characters) and
> you get an obscure parse or auth failure that looks like a wrong password.
> **Use `openssl rand -hex 32`** (or `-base64 32 | tr -d '/+='`).
> `BACKUP_POSTGRES_PASSWORD` is exempt — it goes into `PGPASSWORD`, not a URL.

**Verification:**

```bash
./db.sh -c "SELECT rolname, rolcanlogin FROM pg_roles WHERE rolname LIKE 'om\_%' ORDER BY 1"
```
Expect the five roles, with `rolcanlogin = t` for the four you altered.

**Pre-deploy sanity check** (expect the table count):
```bash
./db.sh -c "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r'"
```
> The runbook says "9 at the time of writing". **That is stale once #77 lands** —
> `review_invitations` makes it **10**. Since #77 deploys before #78 in this
> order, expect 10.

**Step 6 — optional, a separate decision, after everything else.** Narrow the
live Grafana monitoring role:

```bash
cd infra
./db.sh -c "GRANT om_monitor_ro TO grafana_monitoring"
./db.sh -c "REVOKE pg_read_all_data FROM grafana_monitoring"
```
Rollback: `./db.sh -c "GRANT pg_read_all_data TO grafana_monitoring"`.
**Expect to lose:** EXPLAIN plans and schema sampling on the four PII-carrying
tables (`mentors`, `client_requests`, `moderators`, `reviews`). `pg_monitor`
keeps stats and normalized query text.

### 9.5 Other manual setup

**Before deploy, on the VM — this one is a hard gate:**

```bash
ls -l /var/run/docker.sock /run/containerd/containerd.sock
docker info --format '{{.ServerVersion}}'
```

If `/run/containerd/containerd.sock` does not exist at that path, Docker will
create it as an **empty directory** at bind time, cAdvisor's docker factory
silently fails to register, and every `name=` and `container_label_*` label
disappears. Both container alert rules use `noDataState: OK`, so they would go
quietly Normal — monitoring that looks healthy because it measures nothing. This
is why #74 goes first.

Also confirm nobody else is deploying, and `cp .env.production .env.production.pre-h8`
into the password manager (not the repo).

**The DSN cutover — five separate deploys, in ascending blast radius, `migrate`
last.** Each step is one line added to `.env.production` followed by
`cd infra && ./deploy.sh infra --yes`:

| # | Line | Verify |
|---|---|---|
| 2 | `BACKUP_POSTGRES_USER=om_backup` + `BACKUP_POSTGRES_PASSWORD=<pw>` | `docker exec openmentor-postgres-backup backup.sh once` → `SUCCESS`; sidecar `healthy` |
| 3 | `WORKER_DATABASE_URL=postgres://om_worker:<pw>@postgres:5432/openmentor?sslmode=disable` | `docker logs --since 2m openmentor-worker`; health `healthy` |
| 4 | `API_DATABASE_URL=postgres://om_api:<pw>@postgres:5432/openmentor?sslmode=disable` | deploy step 9 gate, then load the catalog and a profile in a browser |
| 5 | `MIGRATE_DATABASE_URL=postgres://om_migrate:<pw>@postgres:5432/openmentor?sslmode=disable` | `docker logs openmentor-migrate` ends with `Database migrations completed successfully`, exit 0 |

Between steps: `./db.sh -c "SELECT usename, count(*) FROM pg_stat_activity WHERE datname = current_database() GROUP BY 1 ORDER BY 1"`.

**Post-deploy verification (the container-hardening half):**

```bash
# Socket proxy denies writes
docker run --rm --network openmentor-docker-api curlimages/curl:8.11.1 \
  -s -o /dev/null -w '%{http_code}\n' -X POST \
  http://openmentor-docker-socket-proxy:2375/v1.51/containers/create      # expect 403

# cAdvisor still has Docker metadata — this is the containerd check's real payoff
docker run --rm --network openmentor-network curlimages/curl:8.11.1 \
  -s http://cadvisor:8080/metrics | grep -c 'container_label_com_docker_compose_service'
```
and in Grafana Explore:
```promql
container_cpu_usage_seconds_total{name=~"openmentor-.*"}
sum by (device) (container_fs_usage_bytes{id="/"})
```
The first must return series — **that is what breaks if the containerd socket
path is wrong**. The second confirms the *Host Filesystem Usage* panel survived
the removal of `/:/rootfs:ro` (it does, because `/var/lib/docker` is on the same
filesystem — which stops being true if `/var/lib/docker` ever moves to its own
volume).

> 🐛 **One verification in `container-hardening.md` will mislead you.** Check 1 is
> `docker logs --since 5m traefik | grep -i "provider connection established"`.
> That message is emitted at **DEBUG**, Traefik's static default log level is
> `ERROR`, and no `--log.level` flag exists in either compose file — so it will
> print nothing **even when everything is fine**. Use check 3 (the proxy returning
> 200 on `/v1.51/containers/json`) plus a real request through the edge instead.

**Also verify after deploy,** because the hardening flags are broad:
`docker compose ps` shows all 10 services up and nothing restarting ·
`openmentor-postgres` came back (its five `cap_add`s — CHOWN, DAC_OVERRIDE,
FOWNER, SETGID, SETUID — are the highest-consequence guess in the diff; a missing
one means the database will not start, which blocks everything behind `migrate`) ·
`grafana-alloy` is up and not exiting 1 (`cap_drop: [ALL]` without `DAC_OVERRIDE`
fails at its first mkdir) · `cadvisor` is up under `read_only: true` with **no
tmpfs**, the only service given `read_only` without a writable `/tmp` ·
`openmentor-postgres-backup` is `healthy` and `backup.sh once` succeeds ·
`docker inspect -f '{{.State.Health.Status}}' openmentor-docker-socket-proxy` ·
and, slowest to fail, `docker logs traefik | grep -i acme` after the next
certificate renewal.

**Rebuilding a Postgres volume after this — read this before you ever need it.**
`000012` cannot be applied by `om_migrate` (its `CREATE ROLE`/`ALTER ROLE` and
`GRANT pg_read_all_data` need CREATEROLE or superuser, which the migrator
correctly does not have). So once `MIGRATE_DATABASE_URL` is set, **any new
Postgres volume deadlocks the whole stack on the first converge** — `migrate`
fails with `role "om_migrate" does not exist` and backend/worker never start.
The bring-up path is: comment out all five new lines for the first converge so
the superuser fallback runs the migration set and creates the roles, then redo
step 1 (passwords) and re-add them.

### 9.6 User-visible behaviour
**None** — provided nothing breaks. The risk profile is availability, not
semantics: an unreachable socket proxy means Traefik has no configuration and
every request 404s, and a full-stack recreate includes ~10–20 s of database
downtime.

### 9.7 Rollback

`./rollback.sh` across #78 is **blocked** by #74's guard. Per-layer rollback,
smallest blast radius first:

| Layer | Undo |
|---|---|
| DSN cutover | Remove the `*_DATABASE_URL` / `BACKUP_POSTGRES_*` lines from `.env.production`, `./deploy.sh infra`. Everything falls back to `${DATABASE_URL}` and the superuser. **Do this before anything else.** |
| Monitoring narrowing | `./db.sh -c "GRANT pg_read_all_data TO grafana_monitoring"` |
| Role logins | `./db.sh -c "ALTER ROLE om_api NOLOGIN"` (etc.) |
| Socket proxy | Restore `/var/run/docker.sock` to `traefik`'s volumes and delete `--providers.docker.endpoint=...` from **both** `docker-compose.yml` **and** `docker-compose.dev.yml` |
| Migration `000012` | §6.7 manual procedure with `version = 11`. Requires the superuser. **Only after every DSN is back on `POSTGRES_USER` and the containers recreated.** |

Because the roles are inert until an operator points a DSN at them, the migration
itself almost never needs undoing — reverting the `.env` lines is the real
rollback.

---

## §10 — Corrections to the branches' own runbooks

These are things I checked against the code and found wrong. They are **not**
fixed in the files themselves, because each lives on an unmerged branch and
editing it here would conflict with the PR that introduces it. Fix them in the
owning PR, or immediately after it merges.

| PR | File | Defect |
|---|---|---|
| #77 | `audit-2026-08/review-capability-cutover.md` §1 | `already_reviewed` uses an **unqualified `id`** in the `RETURNING` subquery; SQL binds `reviews.id`, so the column is always `false` and the filter it feeds is useless. Corrected form in §8.4. |
| #77 | same, §1 step 3 | The replay `curl` uses **`-X POST` on a GET-only route** (404) and **`openmentor-worker:8090`**, which does not resolve from a host shell. Corrected form in §8.4. |
| #77 | same, §2.5 | The post-cutover 410 probe posts `-d '{}'` to a handler that binds and validates the body first → **400, not 410**, before and after the flip. Corrected probe in §8.5. |
| #77 | same, §2.1 | "Any non-zero value is a real mentee using a real old link" — the counter increments before any lookup, so it also counts unknown UUIDs and crawlers. |
| #77 | `000011_..._down.sql` | Warns that dropping the table kills outstanding capabilities; does **not** warn that dropping `review_legacy_link_revoked_at` **un-revokes the 12 leaked ids**. |
| #78 | `database-identities.md` step 1 | `openssl rand -base64 32` for passwords that are then pasted into **URL** DSNs. Use `-hex 32`. |
| #78 | same, precondition 2 | Expected table count "9" is stale once #77 lands — it becomes **10**. |
| #78 | `container-hardening.md` check 1 | Greps for a **DEBUG-level** Traefik message while the image runs at `ERROR`; prints nothing even when healthy. |
| #78 | both new runbooks | No procedure for applying `000012_..._down.sql`, which needs the superuser and leaves `schema_migrations` at 12. |
| #75 | `telemetry-leak-token-invalidation.md` | Says bumping `session_version` "invalidates every issued cookie". **For mentors that is false** — the live re-read is scoped to mutations plus the requests inbox, so a revoked mentor cookie still returns 200 on `GET /mentor/profile`, `/mentor/username`, `/auth/mentor/session`. The branch's own tests assert this. The admin side *is* checked on every request. |
| #75 | same | No `000010` rollback-ordering note, despite the new binary hard-requiring `session_version`. |
| #74 | `infra/DEPLOYMENT.md` | Says the guard "also refuses a tag that is not in the registry"; it refuses on **any** `docker pull` failure, including an ECR outage. |
| #74 | `infra/DEPLOYMENT.md` | "Manual fallback on the VM" (`sed -i` the tag, `pull && up -d`) bypasses the new guard entirely and is left unqualified 40 lines below the section explaining why the crossing is fatal. |
| pre-existing | `grafana/README.md` § Notifications | Lists contact points as telegram/slack/**email** with `(catch-all) → email`, while the authoritative `notification-policies.yaml` has children telegram → slack → **Discord** and `email` as the *parent* receiver. |

---

## §11 — What could not be verified from the repository

- **The live `.env.production`.** Every #76 startup risk depends on values not in
  the tree. That is exactly what `infra/preflight-phase1.sh` is for — run it.
- **`flock` on the VM** (#73), and whether `VM_SSH_HOST_KEY` still matches the
  host's current key.
- **`/run/containerd/containerd.sock`** existing at that path on the VM, and
  whether `/var/lib/docker` is on the root filesystem (#78's *Host Filesystem
  Usage* claim depends on it).
- **`JWT_ISSUER`** in production versus the value live tokens were minted with.
- Whether `tecnativa/docker-socket-proxy:0.3.0` ships `wget` for its healthcheck
  (cosmetic if not — Traefik's `depends_on` is start-order only).
- Traefik v3.7's shipped default log level — the FLAG in §10 is a strong
  inference from the absence of any `--log.level` flag, not a live observation.
- Whether `machine_cpu_cores` carries a usable `instance` label on this tenant.
  If it does not, #74's new `ContainerHighCPU` join errors and **Error state
  pages**. Run it in Explore before the group PUT.
- The exact count of leaked request ids (#77 §8.4) — the PostHog queries return a
  candidate list, and 12 is the number the audit reported, not something this
  repository can confirm.

**Verified live on 2026-08-05 via the Grafana API** (so *not* on this list):
folder `openmentor-alerts` exists, holds all 14 rules, all Normal — and
`ContainerHighCPU` / `ContainerHighMemory` are still the **old** versions
("threshold 90%", "threshold 1GiB"), confirming #74's four rewritten rules are
genuinely not applied yet.
