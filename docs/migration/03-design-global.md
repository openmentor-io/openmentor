# 03 — Design & Product Review for a Global Audience

## Current design assessment (from code review)

The UI is modern and in good shape technically: Next.js 16 + Tailwind, mobile-first, custom components (no heavy UI kit), Open Sans, blue/indigo primary palette, card grid catalog, status-badge system, Tiptap rich text. Nothing structurally dated. The problems for a global audience are **content and market assumptions**, not CSS.

### RU-market elements to remove/replace

| Item | Where | Action |
|---|---|---|
| Prices in rubles ("5000 руб", "По договоренности") | `src/config/filters.ts`, `src/types/mentor.ts`, mentor cards, emails | New pricing model (below) |
| Donation methods: Tinkoff card transfer, Patreon, PayPal | `src/config/donates.ts`, `src/pages/donate.tsx` | Replace with GitHub Sponsors / Ko-fi / Stripe Payment Link (DECISIONS) |
| Yandex context ads | `MentorsListAd.tsx` | Delete component + config |
| Sponsor tags "Эксперт Авито", "Сообщество Онтико" | types, models, seed, moderation "Accept as Avito" button | Delete feature |
| `/ontico` partner landing | `src/pages/ontico.tsx` | Delete |
| Blog link "Наш блог" (points to RU blog) | `NavHeader.tsx` | Remove until an EN blog exists |
| Telegram community link | `Footer.tsx` | Remove (or replace with Discord/Slack later) |
| `ru.` subdomain, `hl="ru"`, `lang="ru"` | infra + `_document.tsx` | Remove/EN |
| Experience/level enums mixing EN+RU (`Менеджер`…) | contact form + API validation | English set: Junior / Middle / Senior / Manager / Manager of managers / C-level (final naming in DECISIONS) |
| RU legal pages | privacy/disclaimer | Replaced (04) |

## Pricing model (proposal — confirm in DECISIONS)

`mentors.price` is free text today. For global market:
- v1 (minimal): keep free text, but registration/profile forms suggest "$/hour or 'Free' or 'Negotiable'"; filter options become `Free | ≤$50 | $50–100 | $100–200 | $200+ | Negotiable`.
- v2 (better, out of scope): structured `price_amount` + `price_currency` + `price_period`.

## Session-flow UX (payments stay off-platform)

getmentor takes no payments — mentor and mentee settle directly. Keep this for v1 (huge scope savings; also simplifies legal). State it clearly on mentor profiles and in FAQ copy.

## Branding

- Name: **OpenMentor**, domain openmentor.io, sender `hello@openmentor.io`.
- Replace logo assets (`public/` — audit `public/images`), favicon, OG images. Placeholder wordmark is acceptable for v1; design pass later.
- Keep the existing color system initially (it's clean); rebrand palette can come with the Figma-driven redesign.

## Figma reference

`https://www.figma.com/design/uffqoXgnpw0TV9K2l19XKI/…` — **not publicly accessible** (fetch returns the Figma login shell). Blocked: need the owner to either share the file publicly / grant access, or export key frames as PNGs into `docs/design-reference/`. Once available: extract palette, typography, layout ideas; compare against current UI; produce a redesign backlog. Until then, design work is limited to content-level changes above.

## Global-audience checklist (copy & UX conventions)

- Dates: use unambiguous formats (e.g. "6 Jul 2026" or locale-aware `Intl.DateTimeFormat`), not DD.MM.YYYY (bot used this; check web `utils.ts` formatter).
- Times/timezones: sessions are scheduled via Calendly-class tools — fine, they handle TZ.
- Currency symbol before amount ($50), no non-breaking-space conventions from RU typography.
- Copy tone: first-person plural community voice ("we"), short sentences; avoid RU-style long compound sentences when rewriting.
- Accessibility quick pass: form labels/aria already decent; verify color contrast of status badges.
- SEO: new EN meta/OG, `hreflang` not needed (single locale), update sitemap/robots for openmentor.io.

## User feedback (user-research step from the brief)

Gathering feedback from a sample of global users is a human task. Plan:
1. Ship the EN v1 to a staging URL.
2. Recruit 5–8 English-speaking tech folks (Twitter/X, LinkedIn, communities) for 20-min moderated walkthroughs: find a mentor → contact → (as mentor) manage a request.
3. Capture findings in `docs/design-reference/user-feedback.md`; fold into redesign backlog.
This plan can be executed later; it does not block P1–P6.
