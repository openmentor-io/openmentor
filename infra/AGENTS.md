# AGENTS.md — `infra/`

Instructions for any coding agent working in `infra/` (and in `grafana/`, which the infra checks
own). **Read the repo-root `AGENTS.md` too** — it holds the repo-wide rules. Under the AGENTS.md
convention the nearest file wins and does *not* merge with the root one, so the rules below that
also appear there are repeated deliberately.

This directory deploys and operates a **live production single-VM stack**: Docker Compose behind
Traefik (the only service publishing :80/:443), a Postgres backup sidecar, and Grafana Alloy.
Scripts here run as root on that VM. A quoting slip is an incident, not a stack trace.

## Commands

```bash
cd infra && make check
```

`check` is seven suites, and CI's required gate calls these same targets:

| Target | What it proves |
|---|---|
| `compose-config` | `docker-compose.yml` renders, and so does the prod + `docker-compose.dev.yml` overlay |
| `env-allowlist` | `check-service-env.sh --self-test` — per-service env allowlist *and* secret ownership, plus that the check still bites |
| `backup-tests` | `postgres-backup/backup-test.sh` — sidecar behaviour |
| `deploy-tests` | `deploy-transition-test.sh` — the blocks `deploy-remote.sh` and `rollback.sh` share |
| `rollback-tests` | `rollback-migration-guard-test.sh` — the migration-boundary guard and the expand/contract policy, reading `../api/migrations` |
| `alert-tests` | `alert-consistency-test.sh` — `../grafana/alerting` and `../grafana/dashboards` against `docker-compose.yml` |
| `alert-fireability-tests` | `alert-fireability-test.sh` — every rule's labels exist and its threshold is reachable |

Not in `check`, run them when relevant: `make shellcheck`, `make db-identity-tests`.

`ENV_FILE` falls back to `.env.example` when the machine has no `.env`. Only key names and
structure are ever read, so placeholder values are fine and nothing is copied over a real `.env`.

## No shared env file

**There is no `env_file:` anywhere in the compose files, and there must not be.** A shared file
gives every container every secret — the internet-facing frontend would hold `DATABASE_URL`. Each
service declares an explicit `environment:` list, and `check-service-env.sh` asserts, per service,
that every key it declares appears in that service's section of `env-allowlist.txt`, and that the
keys in its `SECRET_OWNERS` table appear *only* in the services entitled to them.

Adding an env var to a service therefore means adding it to `env-allowlist.txt` as well, in that
service's section, or `make check` fails.

**The trap:** a bare `- KEY` entry (no `=value`) is dropped by compose when it cannot resolve the
key, so a variable that exists only in the production environment would render away — passing both
checks while reaching no container. `check-service-env.sh` parses the compose file for *declared*
keys and seeds every bare entry before rendering, precisely so that cannot hide. Don't defeat that
by "simplifying" the declared-keys pass.

## Deploy and rollback

- `./deploy.sh` defaults to targets **`frontend backend`** — it does **not** sync `infra/`. A
  compose, allowlist or Alloy change reaches the VM only via `./deploy.sh infra` (or `all`).
  Merging a compose change and running a normal deploy changes nothing on the VM.
- **Migrations apply themselves.** The `migrate` service runs from the same image as backend and
  worker, and `backend`/`worker` depend on it with `condition: service_completed_successfully`. A
  migration that fails to validate its config takes the whole deploy down — that is what happened
  on 2026-08-04.
- **The rollback target is `.env.lastgood`, not `.env.backup`.** `.env.lastgood` is written only
  *after* health checks pass, so it always names a version known good. The `.env.backup.<epoch>`
  files are timestamped snapshots of what a deploy replaced (a single `.env.backup` slot would be
  overwritten by a concurrent converge); the newest of them is not necessarily healthy.
- **`rollback.sh` refuses a rollback that crosses a migration boundary**, before it edits `.env`.
  It reads the target image's highest migration version and the phase markers out of that image
  and compares them with `schema_migrations.version`. It never runs a down-migration; a crossing is
  a human procedure documented in `DEPLOYMENT.md` § "Rolling back across a migration boundary".
- **A service that needs to shut down gracefully needs `stop_grace_period`.** Compose's default is
  10s and then SIGKILL, so a drain, an in-flight cron wait or a connection-pool close that takes
  longer never completes — and the shutdown code that implements it is dead code that reads as
  live. If you add a drain, add the grace period in the same change.

## Shell scripts

Every `.sh` file must pass `shellcheck` (`--severity=warning`) and `/bin/bash -n`. Operators run
these from macOS, whose `/bin/bash` is **3.2**, so:

- **`case` is forbidden inside a block that travels through an unquoted heredoc.** bash 3.2 counts
  parentheses to find the end of the enclosing `$( )`, so a `case` pattern's `)` truncates the
  script. Use `if`/`elif`, and keep every paren in such a block balanced.
- The blocks `deploy-remote.sh` and `rollback.sh` share are compared **byte for byte** by
  `deploy-transition-test.sh`, which also renders them through the heredoc and asserts nothing
  expanded. Edit both copies, or the test fails.
- `scripts/shellcheck-all.sh` is the one definition of the file list and severity; `infra/Makefile`
  and the merge gate both call it, with scopes `infra` and `outside-infra`. Don't hand-write a glob
  in either — the list comes from `git ls-files '*.sh'` so a new script is covered the day it lands.

## Observability as code (`grafana/`)

The two halves ship completely differently, and mixing them up is how alerting goes silent:

- **`grafana/dashboards/` IS Git-Synced**, hourly, from `main`, into folder uid `repository-7b3d712`.
  A merge changes live Grafana with no operator action.
- **`grafana/alerting/` is NOT synced by anything.** The YAML is the versioned source of record;
  the rules are *desired state* until an operator PUTs the group
  (`PUT /api/v1/provisioning/folder/openmentor-alerts/rule-groups/openmentor`, header
  `X-Disable-Provenance: true`). **If a PR changes an alert rule, say so in the PR body** — nothing
  else will tell the operator that a re-apply is owed.
- The rules live in folder uid **`openmentor-alerts`**, deliberately not in the Git Sync dashboards
  folder: Grafana refuses to store alert rules in a Git-Sync-managed folder. **Deleting a Grafana
  folder silently deletes every alert rule in it.** That has happened here — the first apply used a
  folder that looked like a duplicate of the dashboards folder, someone tidied it, and all 14 rules
  went with it, leaving a period with zero alerting.
- **Validate new PromQL against the live tenant before shipping it.** Grafana Cloud Adaptive
  Metrics aggregates some series on this tenant and strips labels from them — `container_spec_memory_limit_bytes`
  has no `name` label here, so the obvious `/ on (name) group_left ()` join errors out, which is
  why the memory limits are written out as literals and pinned against `docker-compose.yml` by
  `alert-fireability-test.sh`. And a rule whose query returns nothing while carrying
  `noDataState: OK` sits permanently green: it is not alerting, it is decoration.
- SLO burn alerts in `grafana/slo/slos.yaml` materialise as their own rules in the separate
  `grafana-slo` folder, so availability and latency page from two independent places. Check both
  before concluding nothing alerts on something; silence both when you silence one.

## Branching

Feature branches only — never merge to `main` without explicit permission. An env var or compose
service change lands as one PR across `infra/`, `api/` and `web/`. Never commit a real `.env`;
templates are `.env.example` and `.env.production.example`.
