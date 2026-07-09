# 00 — GetMentor Architecture (as-is reference)

Condensed from exploration of `../getmentor/` (December 2025 state). This is the system openmentor.io inherits.

## Services

```
Users → Traefik (443, Let's Encrypt) → Next.js frontend (:3000)
                                          ↓ (server-side proxy, internal Docker DNS)
                                        Go API (:8081, not public) → PostgreSQL (managed)
                                          ↓ async HTTP triggers                ↑
                                        Azure Functions (getmentor-func) ──────┘
                                          ├── emails (Yandex Postbox / SendGrid)
                                          └── Telegram (2 bots: mentor bot + admin bot)
Telegram bot for mentors (getmentor-bot, Azure Functions + telegraf) → PostgreSQL directly
```

## Frontend (`openmentor/`, from getmentor.dev)

- Next.js 16, **Pages Router**, React 19, TypeScript strict, Tailwind 3, react-hook-form + yup, Tiptap editor, FontAwesome, react-select.
- **No i18n framework — all copy hardcoded in Russian.**
- Public pages: `/` (mentor catalog + filters), `/mentor/[slug]`, `/mentor/[slug]/contact`, `/bementor` (mentor signup), `/donate`, `/ontico` (partner page), `/privacy`, `/disclaimer`, 404/error.
- Mentor portal (magic-link auth, `mentor_session` HttpOnly JWT cookie): `/mentor/login`, `/mentor` (active requests), `/mentor/past`, `/mentor/requests/[id]`, `/mentor/profile/edit`.
- Admin portal (same auth pattern, `moderators` table): `/admin/login`, `/admin/mentors/{index,pending,approved,declined,[id]}`.
- All `/api/*` routes proxy to the Go API.
- Analytics: PostHog + Mixpanel + GTM; observability: OTel, Grafana Faro, Prometheus `/api/metrics`.
- Calendar embeds: Calendly, Koalendar, Calendlab.

## Backend (`openmentor-api/`, from getmentor-api)

- Go + Gin. Layers: handlers → services → repository → PostgreSQL. In-memory caches (mentors 60s, tags 24h). Rate limiting per endpoint class. ReCAPTCHA v2 on public forms.
- Auth: magic-link (single-use token, 15 min) → JWT session cookie. Same flow for mentors and moderators. Moderator roles: `admin` (full), `moderator` (approve pending only).
- **Async side effects via env-configured webhook URLs** pointing at Azure Functions: `MentorCreatedTriggerURL`, `MentorRequestCreatedTriggerURL`, `MentorLoginEmailTriggerURL`, `ModeratorLoginEmailTriggerURL`, `MentorModerationTriggerURL`, `RequestProcessFinishedTriggerURL`, `ReviewCreatedTriggerURL`, `MentorUpdatedTriggerURL`.
- MCP endpoint `/api/internal/mcp` for Claude tool integration.

### Database schema (PostgreSQL, migrations in `openmentor-api/migrations/`)

- `mentors`: id (UUID), legacy_id, slug, name, email (CITEXT), telegram, **telegram_chat_id (BIGINT)**, **tg_secret**, job_title, workplace, details/about, price (free text, e.g. "5000 руб"), experience (`2-5|5-10|10+`), calendar_url, image, sort_order, status (`pending|active|inactive|declined`), login_token(+expiry), timestamps.
  - Partial unique index on email `WHERE status='active' AND telegram_chat_id IS NOT NULL`.
- `tags` (21 seeded; 6 Russian names) + `mentor_tags` M:N.
- `client_requests`: mentor_id, name, email, telegram, description, level (`Junior|Middle|Senior|Менеджер|Менеджер менеджеров|C-level`), status (`pending|contacted|working|done|reschedule|declined|unavailable`), decline_reason (`no_time|topic_mismatch|helping_others|on_break|other`), decline_comment, scheduled_at, status_changed_at.
- `reviews`: 1:1 with client_requests — complete, helped, one_enough, again, nps, mentor_review, platform_review, improvements.
- `moderators`: name, email, telegram, role (`admin|moderator`), login_token(+expiry).

### Critical business rule (changes in this migration)

```go
IsVisible = status == "active" && telegram_chat_id != nil
```
A mentor appears publicly **only after linking the Telegram bot**. See 02-telegram-removal.md.

## Azure Functions (`openmentor-func/`)

HTTP-triggered (called by Go API triggers): `new-mentor-watcher` (dedupe, generate tg_secret + slug, notify moderators), `new-request-watcher` (notify mentor+mentee+moderators), `adm-bot-listener` (Telegram inline-button moderation), `mentor-moderation-action` (same via HTTP — web path already exists), `mentor-login-email` (email **and** Telegram), `moderator-login-email`, `process-mentee-review`, `request-process-finished` (session done/declined emails to mentee), `tg-mass-send`, `update-mentor-image` (disabled).

Timer-triggered (daily, production-only): `sessions-watcher` (8:30, remind mentors of pending requests >1 day; warns of 30-day deactivation — **Telegram only**), `update-status-reminder` (>120h stale contacted/working — **Telegram only**), `deactivate-pending-mentors` (30-day rule, sets inactive — notification **Telegram only**), `randomize-sort-order` (shuffles catalog order; filters on `telegram_chat_id IS NOT NULL`).

Email: 11 Russian templates in `lib/postbox/templates/` sent via Yandex Cloud Postbox (SESv2-compatible), sender `hello@getmentor.dev`.

## Telegram bot (`../getmentor/getmentor-bot` — reference only, not forked)

telegraf on Azure Functions, talks to PostgreSQL directly. Auth: mentor DMs 8-char `tg_secret` → bot saves `telegram_chat_id`. Features: list active/archived requests, change request status (contacted/scheduled/done/decline/unavailable + revert), view reviews, toggle profile active/inactive, links to web profile/editor. All of this already has a web equivalent in the mentor portal **except**: nothing (the web portal covers all live bot features; profile-editing features in the bot were already disabled). Bot also sends session-complete / session-declined emails — duplicated by `request-process-finished`.

## Infrastructure (`openmentor-infra/`)

Docker Compose on a Yandex Cloud VM: traefik, frontend, backend, migrate (one-shot), Grafana Alloy, cAdvisor. Images in Yandex Container Registry (`cr.yandex`), deployed by GitHub Actions over SSH with health-check rollback. Managed PostgreSQL (Yandex). Blob storage: Azure Blob (+ Yandex Object Storage for uploads via Go API). Observability: Grafana Cloud (Prometheus/Loki/Tempo/Pyroscope). Domains: getmentor.dev, ru., www., mcp. DNS via Cloudflare.
