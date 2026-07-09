# 02 — Telegram Bot Removal & Web-First Mentor Experience

Goal: openmentor.io has **no Telegram dependency**. The web mentor portal (which already exists) becomes the only working place for mentors; every Telegram notification gets an email equivalent; account activation no longer requires linking a bot.

## Why this is feasible

The web mentor portal already covers 100% of the bot's live features:

| Bot feature | Web equivalent (already exists) |
|---|---|
| List active requests | `/mentor` page |
| List archived requests | `/mentor/past` |
| View single request | `/mentor/requests/[id]` |
| Set status contacted/working/done | `POST /api/v1/mentor/requests/:id/status` (state machine in Go API) |
| Decline with reason | `POST /api/v1/mentor/requests/:id/decline` + DeclineModal |
| Revert unavailable → contacted | same status endpoint |
| View mentee review | request detail page |
| Toggle profile active/inactive | **Did NOT exist at migration time** (this table originally claimed it did). Added post-migration on 2026-07-07: "Profile visibility" toggle on `/mentor/profile/edit` backed by `POST /api/v1/mentor/profile/status` |
| Edit profile | `/mentor/profile/edit` |
| Auth | magic-link email login (bot used tg_secret instead) |

The bot's profile-editing menus were already disabled in its code. Nothing functional is lost.

## 1. Database / backend changes (`openmentor-api/`)

### 1.1 Visibility rule (the critical change)

- `internal/models/mentor.go`: `IsVisible = status == "active" && telegramChatID != nil` → `IsVisible = status == "active"`.
- Migration (new file `migrations/0000XX_remove_telegram.up.sql`):
  - Drop partial unique index on `mentors.email` (`WHERE status='active' AND telegram_chat_id IS NOT NULL`); recreate as `WHERE status='active'` (or plain unique on email — check for legacy duplicates first; fresh DB so plain unique is fine).
  - Drop columns: `mentors.tg_secret`, `mentors.telegram_chat_id`.
  - Keep `mentors.telegram` and `client_requests.telegram` **as optional contact handles?** → NO for global market: generalize. Rename concept to a free-form "preferred contact" or keep field but make optional (see §4). Decision recorded in DECISIONS.md; default plan: keep column `telegram`, make optional, relabel in UI as "Telegram (optional)" and add nothing else for v1.
  - Keep `moderators.telegram` column or drop — drop (moderator notifications go to email).
- Remove `tg_secret` generation/handling anywhere in API and in `openmentor-func/new-mentor-watcher`.
- Search codebase for `telegram_chat_id`, `tg_secret`, `TelegramChatID` and remove all admin set/unset plumbing (`internal/services/admin_mentors_service.go`, `internal/models/admin_moderation.go`, frontend admin types/UI).

### 1.2 Registration & activation flow (new)

Old: register → pending → moderator approves → email with tg_secret → mentor links bot → visible.
New: register → pending → moderator approves → **approval email with magic-link to dashboard** → visible immediately (status=active). Email deliverability is the implicit "link check" — the mentor already receives login links by email.

- `new-mentor-approved` email template rewritten: congratulate, link to `https://openmentor.io/mentor/login` (or embed a magic link — reuse `mentor-login-email` flow), explain dashboard.
- Optional hardening (v2, not in scope): require first login before listing.

## 2. Functions changes (`openmentor-func/`)

### 2.1 Delete Telegram sending entirely

- Delete `lib/telegram/` (notificator + all message classes), `adm-bot-listener/`, `tg-mass-send/`.
- Env vars removed: `TELEGRAM_BOT_TOKEN`, `ADMIN_BOT_TOKEN`, `MODERATOR_TG_CHANNEL`, `DEV_TG_CHANNEL_ID`, `TG_MENTORS_CHAT_LINK`.

### 2.2 Replace each Telegram notification with email

| Was (Telegram) | Becomes (email) | Function |
|---|---|---|
| New request → mentor (with action buttons) | Already has email `new-request-mentor` — make it the primary channel; add prominent "Open request" button linking `/mentor/requests/{id}` | `new-request-watcher` |
| New request → moderators channel | Email to moderators list **or** nothing (admin dashboard shows pending) — default: daily digest is overkill; send per-event email to a moderators alias `moderators@openmentor.io` | `new-request-watcher` |
| New mentor → moderators channel (with approve/decline buttons) | Email to moderators alias with link to `/admin/mentors/pending` (web moderation already exists via `mentor-moderation-action`) | `new-mentor-watcher` |
| Moderation result → moderators channel | Skip (visible in admin UI) | — |
| Pending-requests reminder (sessions-watcher) | **New email template** `pending-requests-reminder`: list of stale requests + dashboard link + 30-day warning | `sessions-watcher` |
| Stale-status reminder (update-status-reminder) | **New email template** `status-update-reminder` | `update-status-reminder` |
| Deactivation notice (deactivate-pending-mentors) | **New email template** `profile-deactivated`: why + reactivation steps (log in → resolve requests → toggle active in profile edit) | `deactivate-pending-mentors` |
| Review received → mentor | Already has email `ReviewNotificationEmailMessage` — keep, drop TG twin | `process-mentee-review` |
| Login link → mentor via TG | Drop TG twin; email only | `mentor-login-email` |

### 2.3 Cron query changes

`sessions-watcher`, `deactivate-pending-mentors`, `randomize-sort-order` filter mentors by `telegram_chat_id IS NOT NULL` — remove that predicate (filter by `status='active'` only).

## 3. Frontend changes (`openmentor/`)

- Remove Telegram community link in `Footer.tsx`; remove bot references anywhere (`grep -ri 't.me\|telegram_bot\|getmentor_bot' src/`).
- Admin moderation UI: remove Telegram Chat ID display/edit fields (`src/types/admin-moderation.ts`, moderation detail page).
- Contact form (`ContactMentorForm.tsx`): `telegramUsername` field — make optional, relabel "Telegram (optional)"; helper text becomes "Optional — add your Telegram handle if you prefer to chat there. Your mentor will reach out by email otherwise." (Matches API change §1.1.)
- Register form: `telegram` field → optional.
- Mentor portal is now the primary surface — add an onboarding hint on first dashboard visit (nice-to-have, v2).

## 4. Bot repo

`getmentor-bot` is **not forked** into openmentor-io. No work needed beyond ensuring nothing references it (grep all repos for `getmentor-bot`, `t.me/getmentor`, `tg_secret`).

## Acceptance criteria

1. `grep -ri 'telegram' openmentor-api/ --include='*.go'` returns only the optional contact-handle field (model + normalization) and nothing about chat IDs, secrets, or bots.
2. Fresh DB migration applies cleanly; `mentors` has no `tg_secret`/`telegram_chat_id`.
3. A mentor approved by a moderator appears in the public catalog without any further step.
4. All 3 daily cron functions run without Telegram env vars and send emails (verifiable in non-prod by routing to a dev inbox — replicate the existing `DEV_TG_CHANNEL_ID` pattern with a `DEV_EMAIL_OVERRIDE`).
5. `openmentor-func` has no `lib/telegram` directory and builds (`npm run build`).
