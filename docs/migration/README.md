# OpenMentor.io Migration Plan — Master Document

> Note (2026-07-09): the component repos described here were consolidated into the openmentor monorepo (web/, api/, infra/); paths below reflect the pre-monorepo layout.

**Source project:** getmentor.dev (`../getmentor/`) — Russian-language IT mentorship marketplace.
**Target project:** openmentor.io (this folder, `openmentor-io/`) — English-first, global-market fork.
**Constraint:** getmentor.dev keeps running unchanged. openmentor.io is a separate fork with its own repos, domain, and infrastructure. Never edit files under `../getmentor/`.

## Repository map

| Repo (this folder) | Forked from | Purpose | Status |
|---|---|---|---|
| `openmentor/` | `getmentor.dev` | Next.js 16 frontend (Pages Router, Tailwind) | baseline imported |
| `openmentor-api/` | `getmentor-api` | Go/Gin REST API + PostgreSQL | baseline imported |
| `openmentor-func/` | `getmentor-func` | Azure Functions: email, notifications, cron jobs | baseline imported |
| `openmentor-infra/` | `getmentor-infra` | Docker Compose + Traefik deployment | baseline imported |
| *(none)* | `getmentor-bot` | Telegram bot for mentors | **NOT forked — being removed** (see 02) |

Each repo has a fresh git history starting from an "Import … as migration baseline" commit. Secrets (`.env`, `local.settings.json`) were deliberately NOT copied; `.env.example` files were.

## Plan documents (read in order)

1. **[00-architecture.md](00-architecture.md)** — how getmentor works today: services, data model, workflows. Read this first for context.
2. **[01-translation.md](01-translation.md)** — every file containing Russian text, per repo, with translation instructions, glossary, and tone guide.
3. **[02-telegram-removal.md](02-telegram-removal.md)** — full inventory of Telegram functionality and its web/email replacement, including the activation-rule change and new email notifications.
4. **[03-design-global.md](03-design-global.md)** — design/UX review for a global audience: currency, payments, RU-specific elements, branding.
5. **[04-legal-compliance.md](04-legal-compliance.md)** — privacy policy, terms, GDPR/CCPA, cookie consent. (HIPAA assessed as not applicable — see doc.)
6. **[05-infrastructure.md](05-infrastructure.md)** — replacing RU-region services (Yandex Cloud, Postbox, cr.yandex), domain/env renames, deployment for openmentor.io.
7. **[06-execution-checklist.md](06-execution-checklist.md)** — the ordered, atomic task list. **If you are an LLM executing this migration, work from this file.** Each task states the repo, files, and acceptance criteria.

## Phases

| Phase | Content | Depends on |
|---|---|---|
| **P0 Baseline** | Fork repos, fresh git, no secrets | done |
| **P1 Rebrand** | getmentor.dev → openmentor.io strings, domains, emails, package names | — |
| **P2 Translation** | All RU copy → EN (frontend, backend, emails, tests) | P1 (avoids double edits) |
| **P3 Telegram removal** | Drop bot, replace notifications with email, change visibility rule, web-first mentor portal | can run parallel to P2 |
| **P4 Global-market changes** | Currency/pricing, payment/donation methods, RU tags/partners removed, design polish | P2 |
| **P5 Legal** | New privacy policy, terms, cookie consent, GDPR mechanics | P2 |
| **P6 Infra** | New cloud/email/storage providers, CI/CD, domain | can start anytime; deploy last |

## Rules for executing agents (important)

- Work **only** inside `openmentor-io/`. The `../getmentor/` tree is read-only reference.
- One logical change per commit; commit messages explain *what and why*; run the repo's tests/lints before committing (`yarn test`/`yarn lint`/`npx tsc --noEmit` in `openmentor`, `go test ./...`/`go vet ./...` in `openmentor-api`, `npm run build` in `openmentor-func`).
- When translating, do not change logic, markup structure, or variable names — only human-visible strings (and enum *values* only where 01/02 explicitly says so, because some Russian strings are stored in the database).
- Database enum values, status codes, and API field names stay in English as-is (they already are); Russian **seed data** (tags) changes per 01-translation.md §Backend.
- If a step is ambiguous, stop and record the question in `docs/migration/DECISIONS.md` instead of guessing.

## Open decisions (need product-owner input)

Tracked in [DECISIONS.md](DECISIONS.md). Highlights: email provider, hosting provider/region, pricing currency model, whether to keep Azure Functions or fold jobs into the Go API, Figma file access.
