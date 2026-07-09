# 06 — Execution Checklist (work top to bottom)

Instructions for executing agents: pick the first unchecked task whose dependencies are met, do exactly what it says, run the verification command, commit in the named repo, check the box, commit this file too. One task = one commit unless stated. Details live in the referenced doc sections — read them before editing.

Verification commands per repo:
- `openmentor`: `yarn install && yarn lint && npx tsc --noEmit && yarn test`
- `openmentor-api`: `go build ./... && go vet ./... && go test ./...`
- `openmentor-func`: `npm install && npm run build`
- `openmentor-infra`: `docker compose config -q` (syntax check) where applicable

## P1 — Rebrand (no behavior change)

- [x] **P1.1** `openmentor`: replace getmentor.dev/GetMentor branding — package.json name, SEO config (`src/config/seo.ts` — also translates title, see 01 §A1), all `getmentor.dev` URLs in `src/`, README. Refs: 05 §Rebrand. Do NOT touch privacy/disclaimer pages (replaced in P5).
- [x] **P1.2** `openmentor-api`: rename Go module `go.mod` → `github.com/openmentor-io/openmentor-api`, update all import paths, replace getmentor strings in configs/README.
- [x] **P1.3** `openmentor-func`: replace branding + sender email `hello@openmentor.io` in `lib/postbox/`, `strings`/templates, package.json, README.
- [x] **P1.4** `openmentor-infra`: image names, compose labels, `O11Y_*` values, remove `ru.${DOMAIN}` Traefik router, update GitHub Actions workflow (repos, ghcr.io), README. Refs: 05.

## P2 — Translation (RU → EN)

- [x] **P2.1** `openmentor`: config & types (01 §A1) + dependent test fixtures. ⚠️ pricing options per 03 §Pricing (new USD buckets), status/decline labels per glossary.
- [x] **P2.2** `openmentor`: components (01 §A2) incl. rewrite of relative-time util & pluralize usage.
- [x] **P2.3** `openmentor`: pages (01 §A3) except privacy/disclaimer/ontico/donate (handled in P4/P5).
- [x] **P2.4** `openmentor`: `lang`/`hl` attributes, meta/OG (01 §A4); remaining test files green.
- [x] **P2.5** `openmentor-api`: error/success messages, level enum → EN (⚠️ coordinate with P2.1 contact form values), tag seed migration EN (01 §B).
- [x] **P2.6** `openmentor-func`: rewrite all email templates in English per table in 01 §C (Telegram-linking content removed here if P3.2 not yet done — coordinate).

## P3 — Telegram removal (can interleave with P2)

- [x] **P3.1** `openmentor-api`: visibility rule change + migration dropping `tg_secret`, `telegram_chat_id`, index rework; remove admin chat-id plumbing (02 §1.1). Make `telegram` optional on registration + contact request.
- [x] **P3.2** `openmentor-func`: delete `lib/telegram/`, `adm-bot-listener/`, `tg-mass-send/`; remove TG env vars; rewrite `new-mentor-approved` flow to dashboard-login onboarding (02 §1.2, §2.1–2.2).
- [x] **P3.3** `openmentor-func`: new email templates `pending-requests-reminder`, `status-update-reminder`, `profile-deactivated`; wire into the three cron functions; drop `telegram_chat_id IS NOT NULL` predicates (02 §2.2–2.3). Add `DEV_EMAIL_OVERRIDE` routing for non-prod.
- [x] **P3.4** `openmentor`: remove TG links/fields per 02 §3 (footer, admin UI chat-id, form labels optional).
- [x] **P3.5** All repos: acceptance greps from 02 §Acceptance pass; record results in commit message.

## P4 — Global-market product changes

- [x] **P4.1** `openmentor`: delete `/ontico` page, Yandex ads component, sponsor-tag UI; `openmentor-api`: remove sponsor-tag logic + seed rows (03).
- [x] **P4.2** `openmentor`: rewrite `/donate` page with new methods (per DECISIONS D4; if undecided, remove donate page + nav link and revisit).
- [x] **P4.3** `openmentor`: date formatting audit (03 §checklist); replace DD.MM.YYYY.
- [x] **P4.4** `openmentor`: logo/favicon/OG image placeholder swap (03 §Branding).

## P5 — Legal

- [x] **P5.1** `openmentor`: draft new `privacy.tsx` + `terms.tsx` in English per 04 §4.1 (mark as DRAFT pending lawyer), delete `disclaimer.tsx`, update footer.
- [x] **P5.2** `openmentor`: consent banner gating analytics init (04 §4.1.3).
- [x] **P5.3** `docs/`: `runbooks/data-deletion.md`, `legal/ropa.md` drafts (04 §4.2).

## P6 — Infrastructure (deploy)

- [x] **P6.1** `openmentor-api`: rename `pkg/yandex` → S3-generic storage config (05 §checklist item 5).
- [x] **P6.2** `openmentor-func`: email provider switch per DECISIONS D1 (SES/Resend); config only if SESv2-compatible.
- [x] **P6.3** `openmentor-infra`: env template refresh (`.env.example`, `.env.production.example`) with new variable set; remove Yandex Monitoring bits.
- [ ] **P6.4** Human/ops: provision per 05 §New-environment checklist; first staging deploy; smoke test (register mentor → approve → contact → status flow → emails).

## P7 — Verification pass

- [x] **P7.1** Full-repo Cyrillic scan: `grep -rlE '[А-Яа-яЁё]' --exclude-dir={node_modules,.git,.next,dist} openmentor openmentor-api openmentor-func openmentor-infra` → expected hits only in `pkg/slug` transliteration table, `new-mentor-watcher` transliteration map, and migration docs. Anything else = unfinished translation.
- [x] **P7.2** Full-repo brand scan: `grep -ri getmentor --exclude-dir={node_modules,.git,.next,dist} …` → expected only in migration docs/baseline commit messages.
- [ ] **P7.3** All test suites green; staging E2E walkthrough per 06 P6.4.
