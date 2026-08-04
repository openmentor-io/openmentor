# Runbook: Production Secret Rotation

**Trigger:** suspected or confirmed credential exposure, an operator leaving, a
scheduled rotation, or the one-off rotation that closes out SECURITY P10 (until
this change every container held every secret, so any of them being compromised
meant all of them were).

**Who can run this:** someone with `.env.production` and SSH access to the VM.
Every step is executed by a human; nothing here is automated.

> **Order matters.** Rotate in the sequence below. Rotating credentials before
> the P10 containment change would just redistribute new secrets to the same six
> containers, and `JWT_SECRET` last because it is the only one that logs
> everybody out.

## Standing decision: the one-off P10 rotation was declined (2026-08-03)

**This runbook stays valid. What follows applies only to the one-off rotation
listed as a trigger above** — a future exposure, a departing operator or a
scheduled rotation still runs it verbatim.

The owner considered that one-off rotation and **deliberately declined it**
(2026-08-03, see `docs/migration/DECISIONS.md` D56). P10 is therefore closed by
the **containment** change — the per-service `environment:` allowlists, enforced
by `infra/check-service-env.sh` — and not by replacing any credential.

The reasoning, so it can be re-examined rather than re-litigated: what P10
documents is **co-location**, not disclosure. Every container received every
secret, so a compromise of any one of them would have exposed all of them; no
compromise, and no disclosure of any value, was demonstrated. The audit's
build-ARG concern is weaker than it was written: `web/Dockerfile:62` sets
`ENV POSTHOG_PERSONAL_API_KEY` inside the **builder** stage, and the `runner`
stage begins at line 92, so the pushed image does not carry it.

**What reverses this decision.** Evidence that the pre-P12 "Information to
Gather" procedure in `infra/docs/troubleshooting.md` was ever actually run. That
version piped `docker exec openmentor-backend env | grep -v SECRET` into
`debug-info.txt` — a denylist that filtered five key names and passed everything
else, including the full `DATABASE_URL` with its password, `POSTGRES_PASSWORD`,
`CLOUDFLARE_DNS_API_TOKEN`, `GCLOUD_RW_API_KEY` and `WORKER_AUTH_TOKEN` — and the
section ended with "Send debug-info.txt to support". A single execution of that
bundle turns co-location into a real disclosure to whatever inbox or issue
tracker received the file, and the correct response is then a **full** rotation
in the order below. So: any `debug-info.txt` in a mailbox, an issue, a chat
attachment or a support thread, or any operator recollection of having produced
one, means run this runbook. (The procedure itself was rewritten under P12 and is
now allowlist + presence-only, so newly produced bundles are safe.)

## Preconditions

| # | Check | Why |
|---|---|---|
| 1 | P10 landed (per-service `environment:` allowlists; `cd infra && ./check-service-env.sh` passes) | otherwise the new secrets end up in every container again |
| 2 | P9 landed (`.env.production.example` no longer leaves `BACKUP_AWS_*` empty with a bucket set) | a maintenance restart with the old template restart-loops the backup sidecar, which `deploy-remote.sh` reads as unhealthy → **automatic rollback** |
| 3 | P15 landed (deploy-safety fix) | same reason: the database-password step below needs a deliberate restart that must not auto-roll-back |
| 4 | A fresh backup exists: `docker exec openmentor-postgres-backup backup.sh once` shows `SUCCESS`, and `docker inspect -f '{{.State.Health.Status}}' openmentor-postgres-backup` says `healthy` | you are about to change the credentials the backup path uses |
| 5 | `cp .env.production .env.production.pre-rotation` (outside the repo, in your password manager) | rollback of the *config*, not of the images |

Generate every new value with `openssl rand -base64 32` (or `-hex 32` where a
hex token is expected). Never paste a value into chat, an issue or an email —
transport them in the password manager, and edit `.env.production` locally.

Verify presence, never content:

```bash
docker exec openmentor-backend sh -c 'test -n "$JWT_SECRET" && echo SET || echo UNSET'
```

## Rotation order

### 1. Database passwords

Two moving parts: the cluster's own role password and every DSN that uses it
(`DATABASE_URL`, plus `POSTGRES_OBS_DSN` for the separate `grafana_monitoring`
role). The Postgres image applies `POSTGRES_PASSWORD` **only on first
initialization**, so the running cluster keeps the old password until you change
it with SQL.

```bash
# On the VM
docker exec -it openmentor-postgres psql -U openmentor -c \
    "ALTER USER openmentor WITH PASSWORD '<new>';"
docker exec -it openmentor-postgres psql -U openmentor -c \
    "ALTER USER grafana_monitoring WITH PASSWORD '<new-monitoring>';"
```

Then, locally, update `.env.production`: `POSTGRES_PASSWORD`, the password
inside `DATABASE_URL`, and the password inside `POSTGRES_OBS_DSN`. Re-deploy —
`api`, `worker`, `migrate` and `postgres-backup` all need to restart to pick up
the new DSN, and `alloy` needs the new monitoring DSN plus a rewrite of
`alloy-secrets/postgres_secret_openmentor` (`deploy.sh` writes it):

```bash
./deploy.sh infra
```

**Expect a coordinated restart.** This is the step that requires precondition 3:
`migrate` runs first, and `backend`/`worker` wait on it. Watch the deploy output
for the health checks rather than assuming.

Verify: `docker exec openmentor-postgres pg_isready -U openmentor -d openmentor`,
the backend healthcheck, and one real request through the site.

### 2. S3, SES and backup keys

Three separate IAM identities (do not collapse them):

| Key pair | Identity | Scope |
|---|---|---|
| `S3_STORAGE_ACCESS_KEY` / `S3_STORAGE_SECRET_KEY` | app | profile-image bucket |
| `SES_ACCESS_KEY_ID` / `SES_SECRET_ACCESS_KEY` | worker | SESv2 send only |
| `BACKUP_AWS_ACCESS_KEY_ID` / `BACKUP_AWS_SECRET_ACCESS_KEY` | backups | backup bucket, **no** `s3:DeleteObject` on the images bucket (SECURITY M12) |

For each: create a *second* access key in IAM, put it in `.env.production`,
`./deploy.sh infra`, verify, then delete the old key. AWS allows two active keys
per user precisely so rotation needs no downtime.

Verify:

- S3: upload a profile picture through the mentor edit form.
- SES: trigger a magic-link email to yourself (mentor login).
- Backups: `docker exec openmentor-postgres-backup backup.sh once` →
  `SUCCESS ... dest=s3://...`, then confirm the object exists.

### 3. `WORKER_AUTH_TOKEN` and the internal API tokens

`WORKER_AUTH_TOKEN` (`X-Worker-Token`), `INTERNAL_MENTORS_API` /
`GO_API_INTERNAL_TOKEN` (**these two must hold the same value**),
`MENTORS_API_LIST_AUTH_TOKEN`, `METRICS_AUTH_TOKEN`.

All of these are shared secrets between two containers, and neither side
tolerates a mismatch — so they flip together in one deploy, with a few seconds
where in-flight internal calls can 401:

```bash
./deploy.sh infra          # or a full ./deploy.sh if images also changed
```

Verify: the worker healthcheck, a mentor-registration email end to end (API →
worker trigger), the mentors list endpoint, and that Grafana still receives
frontend metrics (`METRICS_AUTH_TOKEN` guards `/api/metrics`).

### 4. `CLOUDFLARE_DNS_API_TOKEN`

Used **only** by Traefik, for the DNS-01 ACME challenge. Create a new
"Edit zone DNS" token scoped to the zone, update `.env.production`, deploy, then
revoke the old one in the Cloudflare dashboard.

Verify: `docker logs traefik | tail -50` shows no ACME errors. Certificates only
renew every ~60 days, so a broken token stays quiet for weeks — force the issue
by checking the ACME account/registration lines at startup, or renew a staging
cert first.

### 5. `GCLOUD_RW_API_KEY`

Grafana Cloud access policy token (metrics/logs/traces/profiles push). Create a
new token in Grafana Cloud → Access Policies, update `.env.production`, deploy
(`alloy` restarts), then delete the old token.

Verify: `docker logs grafana-alloy | grep -i "remote_write\|error"` is clean and
new data points appear in the `om-overview` dashboard within a couple of
minutes.

### 6. `JWT_SECRET` — LAST, and scheduled

**Rotating this logs out every mentor and admin.** One secret signs both session
types; changing it invalidates every issued session cookie, and everyone must
request a new magic link.

Do this deliberately:

1. Pick a low-traffic window (check the `om-overview` traffic panel).
2. Pair it with the P14 token-invalidation work so the disruption is absorbed
   **once** rather than twice.
3. Announce it to the moderators beforehand — they will be logged out mid-task
   otherwise.
4. Minimum 32 bytes: `config.ValidateForAPI()` rejects anything shorter, and the
   API refuses to start (not just refuses logins). Since D62 only the backend
   holds `JWT_SECRET` — migrate and worker no longer need it, so there is
   nothing to rotate there.

```bash
openssl rand -base64 48    # comfortably over the 32-byte floor
```

Update `.env.production`, `./deploy.sh infra`, then verify a fresh magic-link
login for a mentor **and** for an admin before you close the window.

## After every step

```bash
cd infra && ./check-service-env.sh   # no key drifted into a service that should not hold it
```

Once all six groups are done:

- Delete `.env.production.pre-rotation` from the password manager.
- Confirm the old credentials are actually revoked at the provider (AWS IAM,
  Cloudflare, Grafana Cloud) — replacing a value in `.env.production` does not
  disable the old one.
- On the VM, confirm no stale full-secret copy remains:
  `ls -la /opt/openmentor/infra/.env*` should show `.env` (mode 600),
  `.env.backup`, and **no** `.env.runtime`. If that file is still there the VM
  has not yet received the P10 `docker-compose.yml`: run `./deploy.sh infra`,
  which is the deploy that removes it (`infra/DEPLOYMENT.md`).
- Record the date and which groups were rotated in the ops tracker.

## If something breaks

`infra/rollback.sh` rolls back **image tags**, not secrets — and it is inoperable
across a migration boundary. For a bad secret, restore the value from
`.env.production.pre-rotation` and re-deploy. That is why step 1 changes the
cluster password with SQL: the config and the cluster can be brought back into
agreement in either direction.

Common failure: `password authentication failed for user "openmentor"` from
backend/worker/migrate means `DATABASE_URL` and the cluster disagree — see the
failure table in [`postgres-backup-restore.md`](postgres-backup-restore.md).
