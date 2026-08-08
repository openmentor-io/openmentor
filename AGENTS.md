# AGENTS.md

Instructions for any coding agent working in the openmentor monorepo. Agent-agnostic;
`CLAUDE.md` imports this file and adds Claude Code specifics.

**Per-subtree files exist and are more specific than this one.** Read the one for the
directory you are working in — `api/AGENTS.md`, `web/AGENTS.md`, `infra/AGENTS.md`. Under the
AGENTS.md convention the nearest file wins and does *not* merge with this one, so each
subtree file repeats the rules that govern it. This file holds what is true repo-wide.

## What this is

An open-source mentorship platform (AGPL-3.0), a fork of getmentor.dev, **live in production**
with real user data on a single VM. Short planned downtime is acceptable; extended downtime is
not. Treat every change as reaching real mentors and mentees.

Request path: browser → Traefik (the only service publishing :80/:443) → Next.js frontend →
`/api/*` BFF proxy → Go API → Postgres. The worker is internal-only, driven by cron and by
fire-and-forget HTTP calls from the API.

| Directory | What it is |
|---|---|
| `web/` | Next.js 16 frontend, **Pages Router only**. Acts as a BFF: all data flows through `web/src/pages/api/*` proxies to the Go API. |
| `api/` | Go backend, module `github.com/openmentor-io/openmentor/api`. Three binaries: `cmd/api`, `cmd/worker`, `cmd/migrate`. |
| `infra/` | Docker Compose + Traefik single-VM deployment, Grafana Alloy, Postgres backup sidecar. |
| `grafana/` | Dashboards and alert rules as code. `dashboards/` is Git-Synced; `alerting/` is **not**. |
| `docs/` | `docs/migration/DECISIONS.md` (decision log), `docs/runbooks/` (operator procedures), `docs/audit/` (the 2026-08 audit record). |
| `brand/` | Brand asset pack. Never redraw the logo; reference files verbatim per `brand/README.md`. Served copies live in `web/public/brand`. |

## Commands

Each subtree owns its verification. Run the `make` target, not the underlying tool — the CI
workflows call these same targets, so local and CI run the identical check.

```bash
cd api   && make ci      # golangci-lint (version pinned in api/Makefile) + tests + race + coverage floor
cd web   && make ci      # eslint + tsc --noEmit + jest + production build
cd infra && make check   # 7 suites: compose-config, env-allowlist, backup-tests,
                         # deploy-tests, rollback-tests, alert-tests, alert-fireability-tests
                         # (db-identity-tests is a separate target, NOT part of check)
```

Never inline a command in a workflow that differs from its make target. That divergence is how
CI once went green while `make lint` reported 44 issues.

## Repo-wide rules

- **Cross-cutting changes land as ONE commit/PR** touching every affected directory — API
  contract, env vars, compose services. That is the point of the monorepo.
- **Every new feature gets its own branch. Never merge to `main` without explicit permission.**
- **Product and architecture decisions get a row in `docs/migration/DECISIONS.md`.** When
  several branches run in parallel, reserve an id block per branch — four branches once all
  claimed `D39` and reconciling it cost real effort.
- **Never commit a real `.env` or a secret.** Templates are `*.example`; the root `.gitignore`
  enforces this, so don't weaken it.
- **The repository is public.** Don't commit a live capability value (a `client_requests.id`, a
  login or review token), and don't add exploitation detail for a vulnerability that is not
  fixed yet.
- `docs/migration/` is a historical record of the getmentor→openmentor fork. Don't "fix" old
  paths there. Same for `docs/audit/`: it records what an audit claimed, and §4 lists the claims
  later verification overturned — read that section before acting on the plan.

## CI

- `Checks / required-checks` runs on every PR and is the **ONLY** check that should be required.
  `CI / Web` and `CI / API` are path-filtered, so requiring their job names deadlocks any PR
  that doesn't touch that subtree.
- Keep the gate **one job**. It is ~18 steps behind 5 path filters; a second required job name
  reintroduces that deadlock.
- The gate owns the fast checks exclusively. The deep workflows keep only what it can't cheaply
  do: production build, race + coverage floor, gosec SARIF, Docker smoke test, DB-backed tests.
  Don't add a fast check to both — that duplication once made a web PR lint twice and `go vet`
  run three times.
- Some path filters deliberately cross directories: a `grafana/` change runs the infra gate
  (`alert-consistency-test.sh` pins alert thresholds against `docker-compose.yml`), a
  `deploy.yml` change runs it too (`deploy-transition-test.sh` asserts that workflow's own
  properties), and a `docs/audit/2026-08-remediation-plan.md` edit runs the runbooks gate.
  Preserve those.
- `uses:` are pinned to full 40-char SHAs with a version comment, and Dependabot's
  `github-actions` ecosystem refreshes them. Pinning without that ecosystem freezes the actions
  rather than protecting them.

## Testing

- Every fix needs a test verified to **fail without it**. Say how you checked; reverting the
  production hunk and re-running is the cheap way.
- Prefer fakes at the repository boundary over reimplementing production logic in a mock. A mock
  that silently accepted NULL into `*string` is exactly why a whole defect class survived review.
- Don't let a test pin a bug. One assertion was literally named `dry run exits 0` and protected
  the defect it described.

## Commits and pull requests

- Short, imperative commit subjects; no conventional-commit prefixes. Describe the change, not
  the ticket.
- A PR body says what changed, the evidence for each claim, and anything deliberately left
  undone with the reason. State plainly when something is unverified rather than implying it
  passed.
- Run the relevant `make ci` / `make check` before opening a PR.
