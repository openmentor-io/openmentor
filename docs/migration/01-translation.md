# 01 — Russian → English Translation Inventory

Goal: openmentor.io ships **English-only** (no i18n framework needed initially — keep strings hardcoded but in English, mirroring current architecture; adding i18n later is a separate project and must NOT be started as part of this migration).

## Rules

1. Translate only human-visible strings. Never rename variables, CSS classes, DB columns, API fields, or status codes.
2. Some Russian strings are **data contracts** (stored in DB or validated by the backend). These must be changed in *both* frontend and backend in the same phase, with a DB migration for seed data. They are marked ⚠️CONTRACT below.
3. Tone: friendly, professional, concise. Use "you" (the RU informal/formal distinction disappears). Avoid literal translation of idioms; write natural product copy.
4. Emojis in current copy (🍩, 👋, ✅) may be kept where they fit; drop them in legal/error text.
5. Tests asserting Russian strings must be updated in the same commit as the code they test.

## Glossary (use consistently)

| Russian | English |
|---|---|
| ментор / менти | mentor / mentee |
| заявка | request |
| Личный кабинет | Dashboard |
| Стать ментором | Become a mentor |
| Донат | Support us / Donate |
| Ожидает / Связались / В работе / Завершено / Отклонено / Недоступен | Pending / Contacted / In progress / Completed / Declined / Unavailable |
| Собеседования | Interview prep |
| Карьера | Career |
| Аналитика | Analytics |
| Сети | Networking (computer networks — check context; the tag means network engineering) |
| По договоренности | Negotiable |
| Бесплатно | Free |

## A. Frontend (`openmentor/`) — 43 files

### A1. Config & types (do these first; other files reference them)

| File | What to translate | Notes |
|---|---|---|
| `src/config/seo.ts` | Site title + description | New copy: "OpenMentor — an open community of tech mentors" (adjust freely) |
| `src/config/filters.ts` | Tag labels, price options | ⚠️CONTRACT prices ("Бесплатно", "1000 руб"…"15000 руб", "По договоренности") are stored in `mentors.price`. See 03-design-global.md §Pricing before translating — pricing model changes to USD/negotiable. Tags ⚠️CONTRACT with `tags` table seed (see §B). |
| `src/config/donates.ts` | 3 donation labels | Donation methods change entirely (03 §Donations) |
| `src/types/mentor.ts` | Price enum, tag unions, sponsor tags (`Эксперт Авито`, `Сообщество Онтико`) | ⚠️CONTRACT. Sponsor tags are **removed** for openmentor (02/03), not translated. |
| `src/types/mentor-requests.ts` | Status labels (6), decline reason labels (5) | Labels only; the underlying codes (`pending`, `no_time`…) stay. |

### A2. Components

| File | Strings |
|---|---|
| `src/components/forms/ContactMentorForm.tsx` | ~15: labels ("Ваша почта", "Ваше имя и фамилия", "О чём хотите поговорить?"…), validation errors, submit button, Telegram-field helper (this field changes per 02 §Contact-form) |
| `src/components/forms/RegisterMentorForm.tsx` | ~5 validation messages (image size/type) + all field labels |
| `src/components/forms/ProfileForm.tsx` | ~5 field labels |
| `src/components/layout/NavHeader.tsx` | Nav items: "✍️ Наш блог", "➕ Стать ментором", "🍩 Донат" (blog link — decide whether a blog exists; see DECISIONS) |
| `src/components/layout/Footer.tsx` | Legal link titles, community links (Telegram community link removed per 02) |
| `src/components/mentors/MentorsSearch.tsx` | Search placeholder |
| `src/components/mentors/MentorsList.tsx` | Empty states, pluralized counts |
| `src/components/mentors/MentorsListAd.tsx` | Ad-block fallback text (Yandex ads removed entirely per 03 — likely delete component) |
| `src/components/mentor-admin/MentorAdminLayout.tsx` | "Личный кабинет" → "Dashboard", nav labels |
| `src/components/mentor-admin/SearchInput.tsx` | "Поиск...", "Очистить поиск" |
| `src/components/mentor-admin/SortToggle.tsx` | "Сначала новые/старые" → "Newest first/Oldest first" |
| `src/components/mentor-admin/DeclineModal.tsx` | Modal title, reasons, errors |
| `src/components/mentor-admin/utils.ts` | Relative-time formatter with Russian plural rules (~30 strings). Rewrite in English ("just now", "5 minutes ago", "yesterday"…) — English needs only singular/plural, simplify accordingly. Consider `Intl.RelativeTimeFormat`. |
| `src/lib/pluralize.ts` | Russian 3-form pluralizer — replace usages with simple English pluralization; keep or delete the helper. |
| `src/lib/admin-moderation-api.ts` | Client-side error strings |

### A3. Pages

| File | Strings |
|---|---|
| `src/pages/index.tsx` | Hero "Найди своего ментора", tagline, feature blurbs |
| `src/pages/404.tsx`, `src/pages/_error.tsx` | Error copy |
| `src/pages/bementor.tsx` | Registration page copy + submit errors |
| `src/pages/donate.tsx` | Full page (~10 strings incl. supporter tiers) — rewritten per 03 §Donations |
| `src/pages/ontico.tsx` | **Delete page** — Ontico is an RU partner (03) |
| `src/pages/privacy.tsx` | **Do not translate** — replaced with new GDPR policy (04) |
| `src/pages/disclaimer.tsx` | **Do not translate** — replaced with new Terms/Disclaimer (04) |
| `src/pages/mentor/login.tsx`, `index.tsx`, `past.tsx`, `profile/edit.tsx`, `requests/[id].tsx` | Portal copy, page titles, error strings |
| `src/pages/mentor/[slug]/index.tsx` + `contact.tsx` | Profile page + contact flow copy |
| `src/pages/reviews/new.tsx` | Review form copy + errors |
| Admin pages `src/pages/admin/**` | Login, lists, moderation detail copy |

### A4. Other frontend items

- ReCAPTCHA `hl="ru"` → `hl="en"` (grep for `hl=`).
- `<html lang="ru">` (check `_document.tsx`) → `lang="en"`.
- Any OpenGraph/meta strings in `_app.tsx`/`_document.tsx`/next-seo config.
- Tests (7 files listed below) — update fixtures/assertions:
  `src/__tests__/server/mentors-data.test.ts`, `src/__tests__/components/{MentorsList,ContactMentorForm,RegisterMentorForm,MentorsListAd}.test.tsx`, `src/__tests__/pages/api/register-mentor.test.ts`, `src/components/hooks/__tests__/useMentors.test.tsx`.

## B. Backend (`openmentor-api/`)

| File | What | Action |
|---|---|---|
| `migrations/000002_populate_tags.up.sql` | 6 Russian tag names: Сети, Карьера, Собеседования, Аналитика, Эксперт Авито, Сообщество Онтико | ⚠️CONTRACT. New install: edit seed directly (fresh DB for openmentor). Translate 4 (Networking, Career, Interview prep, Analytics); **delete** the 2 sponsor tags. |
| `internal/models/contact.go` | Level validation enum: `Менеджер`, `Менеджер менеджеров` | ⚠️CONTRACT with frontend contact form. Change to `Manager`, `Manager of managers` (or `Engineering Manager`/`Director+` — see DECISIONS) in both repos simultaneously. |
| `internal/models/mentor.go` | Sponsor-tag map (`Эксперт Авито`, `Сообщество Онтико`) | Remove sponsor-tag logic entirely (03). |
| `internal/services/review_service.go` | "Заявка не найдена", "Отзыв уже оставлен…" | Translate ("Request not found", "Review already submitted or the request is not completed yet"). |
| `internal/services/mentor_auth_service.go`, `admin_auth_service.go` | "Ссылка для входа отправлена на вашу почту" | "We've sent a login link to your email." |
| `pkg/slug/slug.go` | RU→Latin transliteration | **Keep** (harmless; still useful for Cyrillic names). |
| `test/**` | Russian fixtures ("Иван Иванов", "5000 руб"…) | Update alongside the code they exercise. |

## C. Azure Functions (`openmentor-func/`) — email templates & messages

All 11 templates in `lib/postbox/templates/` are Russian. **Rewrite** (not literal-translate) in English; strip Telegram-linking instructions per 02; strip donation pitches per 03; sender becomes `hello@openmentor.io`.

| Template | Current subject | New subject (suggested) |
|---|---|---|
| `new-mentor.ts` | Заявку приняли, скоро отпишемся | "We received your mentor application" |
| `new-mentor-approved.ts` | Вы приняты в менторы GetMentor.dev | "Welcome aboard — you're now an OpenMentor mentor" ⚠️ body currently centers on Telegram bot linking + tg_secret — replace with dashboard login instructions (02) |
| `new-mentor-declined.ts` | 😢Заявка отклонена | "Update on your mentor application" |
| `new-mentor-duplicate.ts` | Внимание – Регистрация… | "You already have a profile on OpenMentor" (remove Telegram reactivation path) |
| `new-request.ts` | Ментор получил твою заявку | "Your mentor has received your request" |
| `new-request-calendly.ts` | (same) | same + booking link paragraph |
| `new-request-mentor.ts` | Новая заявка на менторство! | "New mentorship request" |
| `mentor-login.ts` | Вход в личный кабинет GetMentor | "Your OpenMentor login link" |
| session-complete / session-declined (also in bot repo) | — | "How was your session?" / "Your mentor couldn't take this request" |
| review notification | — | "You've received a new review" |

Plus: `moderator-login-email/index.ts` default name `'модератор'` → `'moderator'`; `new-mentor-watcher/index.ts` transliteration map — keep. All Telegram message classes in `lib/telegram/` are deleted, not translated (02).

## D. Infra (`openmentor-infra/`)

No user-facing Russian; check README/docs for RU comments — translate or leave (docs-only, low priority).

## Suggested execution (per repo, one PR/commit each)

1. `openmentor`: A1 → A2 → A3 → A4, run `yarn test && yarn lint && npx tsc --noEmit`.
2. `openmentor-api`: B, run `go test ./... && go vet ./...`.
3. `openmentor-func`: C, run `npm run build`.
Coordinate ⚠️CONTRACT items (price options, level enum, tags) across repos in the same phase.
