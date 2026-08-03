# Contributing to OpenMentor

Thanks for being here. OpenMentor is a community project — donation-funded, zero
commission, no ads — and it only works because people give it their time. That
includes mentoring, and it includes code.

This guide covers both. If anything here is wrong, unclear, or out of date,
that's a bug: open an issue or send a PR fixing it.

## Ways to help

**Become a mentor.** Honestly, this is the most valuable thing most people
reading this can do. If you've worked in tech for a few years, someone is
looking for exactly what you know. Mentoring for free is encouraged and
first-class here — [sign up at openmentor.io/bementor](https://openmentor.io/bementor).

**Tell someone about it.** A mentee who never hears about the platform can't use
it. Sharing it with someone who's stuck is real help.

**Report a bug.** Something broken, confusing, or wrong? [Open an issue](https://github.com/openmentor-io/openmentor/issues/new/choose).
Bugs you hit as a real user are more useful than most code contributions.

**Improve the docs.** Every directory has a README, and they drift. Fixes to
wording, broken links, and stale instructions are always welcome and always
merged quickly.

**Donate.** Running costs are covered by donations — see
[openmentor.io/donate](https://openmentor.io/donate).

**Write code.** The rest of this document.

## Before you write code

For **typos, docs, broken links, and obvious bug fixes** — just open a pull
request. No ceremony needed.

For **anything else**, please [open an issue first](https://github.com/openmentor-io/openmentor/issues/new/choose)
and let's agree on the approach before you start. That especially means changes
that touch:

- the design system or any UI (there's an established system — see below)
- the database schema or a migration
- the API contract between `web/` and `api/`
- infrastructure, deployment, or CI
- product behaviour — what the site does, not just how it does it

This isn't gatekeeping. OpenMentor is maintained by one person, and the codebase
carries a lot of decisions that aren't obvious from reading it. A quick issue
saves you from spending a weekend on something that turns out to conflict with a
decision already recorded in [`docs/migration/DECISIONS.md`](docs/migration/DECISIONS.md).
Ask, and you'll get a real answer.

## Getting set up

### Prerequisites

| Tool | Version | Needed for |
|---|---|---|
| Go | 1.25+ (CI runs 1.26) | `api/` |
| Node.js | 26.x | `web/` |
| npm | ≥ 10.9.0 | `web/` |
| Docker + Compose | 20.10+ / 2.24.4+ | full stack, `infra/` (the dev override uses `!override`) |
| PostgreSQL | 16 | running the API for real (Docker is fine) |

### Fork, clone, branch

```bash
# Fork the repo on GitHub first, then:
git clone https://github.com/<your-username>/openmentor.git
cd openmentor
git remote add upstream https://github.com/openmentor-io/openmentor.git
git checkout -b your-change-name
```

Branch names are descriptive kebab-case — `tiptap-v3`, `switch-to-npm`,
`modernise-tags`. No prefix convention to memorise.

### Working on the API (`api/`)

The Go test suite runs with **no database and no `.env`** — the fastest possible
first loop:

```bash
cd api
make install-tools   # once: installs the pinned golangci-lint
make test            # passes on a fresh clone
make lint
```

To actually run the API you need Postgres and config:

```bash
docker run -d --name openmentor-pg \
  -e POSTGRES_USER=openmentor -e POSTGRES_PASSWORD=password -e POSTGRES_DB=openmentor \
  -p 5432:5432 postgres:16-alpine

cp .env.example .env    # its DATABASE_URL already matches this container
go run ./cmd/migrate && go run ./cmd/api
go run ./cmd/worker     # optional: background jobs + email
```

Set `APP_ENV=development` in `.env` before running anything — the template
defaults to `production`, which refuses to start without `JWT_SECRET` and
`WORKER_AUTH_TOKEN`. Development needs neither, though the mentor and admin
login flows disable themselves until `JWT_SECRET` is set to 32+ characters.

Running the worker this way also needs the `*_TRIGGER_URL` values in `.env`
repointed from `http://worker:8090` — a Compose-internal hostname — to
`http://localhost:8090`, or the API can't fire its background jobs. The Compose
stack below needs no such edit.

Details — auth model, endpoints, worker jobs, cron schedules — are in
[`api/README.md`](api/README.md).

### Working on the web app (`web/`)

```bash
cd web
cp .env.example .env    # GO_API_INTERNAL_TOKEN must match the API's INTERNAL_MENTORS_API
npm ci
npm run dev             # http://localhost:3000
```

The frontend is a thin client: every data operation goes through the Go API, so
you'll usually want the API running too. Conventions — path aliases, strict
TypeScript, component layout — are in [`web/CLAUDE.md`](web/CLAUDE.md).

### The whole stack at once

```bash
cd infra
./deploy-dev.sh all --yes
```

This builds both images and brings up the full production service set locally
(Traefik, frontend, api, worker, migrate, Postgres) with dev overrides — HTTP-only
on `localhost`, disposable database on port 5433, no backups. It creates `.env`
from `.env.example` on first run with generated secrets. See
[`infra/README.md`](infra/README.md).

## How the repo is laid out

| Directory | What it is | Start here |
|---|---|---|
| `web/` | Next.js 16 frontend | [`web/README.md`](web/README.md), [`web/CLAUDE.md`](web/CLAUDE.md) |
| `api/` | Go backend: API, worker, migrations | [`api/README.md`](api/README.md) |
| `infra/` | Compose + Traefik deployment, observability | [`infra/README.md`](infra/README.md) |
| `docs/` | Decisions log, runbooks, design reference, legal | [`docs/migration/DECISIONS.md`](docs/migration/DECISIONS.md) |
| `brand/` | Brand asset pack — logos, colour tokens | [`brand/README.md`](brand/README.md) |

## House rules

These are the ones that aren't obvious and that reviewers actually enforce.

**Cross-cutting changes land as one commit and one PR.** If a change touches the
API contract, an environment variable, or a Compose service, the changes to
`api/`, `web/`, and `infra/` go together in a single commit. That's the point of
the monorepo — don't split it across PRs.

**Environment contracts stay in sync.** `infra/.env.example` and
`infra/.env.production.example` must match what `api/config/config.go` and
`web/` actually read. Adding a variable means adding it to the templates in the
same commit. For `web/`, also update `src/types/env.d.ts`.

**Never commit a real `.env` or any secret.** Templates are `*.example`, and the
root `.gitignore` enforces this — please don't weaken it. If you think you've
committed a secret, say so immediately rather than quietly force-pushing; it
needs rotating, not just removing.

**Read the design system before touching UI.**
[`docs/design-reference/design-system.md`](docs/design-reference/design-system.md)
maps the type system, tokens, button and field classes, card states and motion
spec to their source files. Hard rules: no Tailwind `gray-*` (use the
ink/surface/line families), one button system (the `.button*` classes), and radii,
shadows and pastels only via the tokens in `tailwind.config.js`.

**Never redraw the logo.** Reference the files in `brand/` verbatim — see
[`brand/README.md`](brand/README.md) for the rules.

**Product and architecture decisions get a row in the decisions log.** If your
change decides something — a provider, a data model, a workflow, a policy — add
a row to [`docs/migration/DECISIONS.md`](docs/migration/DECISIONS.md) explaining
what was chosen and why. That file is the reason the codebase is
understandable, and future-you is one of the people it helps.

**`docs/migration/` is history, not documentation.** It records the
getmentor.dev → openmentor fork as it happened. Old paths and superseded plans in
there are correct as a record — please don't "fix" them.

## Check your work before pushing

Both halves of the monorepo expose the same make targets, and **the workflows
call those same targets** — `make lint` on your machine is the identical check
CI runs, at the same pinned linter version.

```bash
cd web && make pre-commit    # lint + typecheck + tests
cd api && make pre-commit    # fmt + vet + tests
```

Before opening the PR, run the heavier targets for whatever you touched:

```bash
cd web && make ci    # lint + typecheck + tests + production build
cd api && make ci    # golangci-lint + race tests
```

Neither target is the complete gate. `CI / Web` additionally builds the Docker
image, checks it ships the Node version `engines.node` declares, and boots it
against the health endpoint; `CI / API` adds a coverage floor, builds all three
binaries, and runs its own Docker smoke test. A green `make ci` is a strong
signal, not a guarantee.

For `infra/` changes, validate both Compose files together — the dev override
isn't loaded by default:

```bash
cd infra && docker compose -f docker-compose.yml -f docker-compose.dev.yml config -q
```

(That needs `.env` present — copy it from `.env.example`, then delete it
afterwards. `./deploy-dev.sh` does this for you.) If you touched a service's
`environment:` block, also run `cd infra && ./check-service-env.sh`.

A note on the Go linter: `make lint` refuses to run if the `golangci-lint` on
your `PATH` isn't the pinned version, because different builds report different
issues. `make install-tools` installs the right one.

## Commits and pull requests

**Commit messages.** Write an imperative subject line that says what the change
does — "Stop the editor toolbar from painting over the tags dropdown", not
"toolbar fix". An area prefix (`fix(web):`, `ci:`, `docs(runbook):`) is welcome
but optional; there's no tooling enforcing a format. Use the body to explain
*why*, especially if the reason isn't obvious from the diff. The best commit
messages in this repo read like a short note to whoever hits this code next.

**Pull requests.** Fill in the template — it asks what changed, why, and how you
tested it. Link the issue if there is one. Keep the PR focused on one thing;
a drive-by refactor bundled with a bug fix makes both harder to review.

**What happens next.** `Checks / required-checks` runs on every PR and is the
only required check — it runs the fast gates for whatever you changed. Deeper
workflows (`CI / Web`, `CI / API`) run only when their subtree changed and cover
the production build, race detector, coverage floor, security scanning and a
Docker smoke test.

Review is done by one person around a full-time job, so expect a few days rather
than a few hours — and a nudge on the PR after a week is completely fair, not
rude. Every PR gets a real answer, including the ones that get declined.

Nothing merges to `main` directly; everything goes through a PR.

## AI-assisted contributions

Use whatever tools help you — AI assistance is genuinely welcome here, and it
would be hypocritical to say otherwise given how much of this repo was built
with it (that's what the `CLAUDE.md` and `AGENTS.md` files are for).

One rule, and it's not negotiable: **you are the author, and you're accountable
for every line you submit.** Understand what your change does, run the checks,
and be able to explain the reasoning in review. Generated output that the
submitter clearly hasn't read gets closed — not because a machine wrote it, but
because nobody reviewed it.

If you use the agent config files, keep in mind they're written for this
repository's conventions, not general advice.

## Reporting security issues

**Please don't open a public issue for a security vulnerability.** See
[SECURITY.md](SECURITY.md) for how to report privately.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md) — it covers
the repository, the issue tracker, pull requests, and discussions. Reports go to
hello@openmentor.io.

## Licensing

The whole repository is [AGPL-3.0](LICENSE), and contributions are accepted
under the same licence. There's no CLA and no copyright assignment — you keep
ownership of what you write; opening a PR licenses it under AGPL-3.0 like the
rest of the project.

This matters more than usual here: `web/` began as a fork of
[getmentor.dev](https://getmentor.dev), whose contributors licensed their work
AGPL-3.0. Keeping the entire monorepo under the same licence is a deliberate
decision, not an accident of history.
