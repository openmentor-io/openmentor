# OpenMentor design system (2026-07 redesign)

The working reference for anyone — developer or agent — building or
changing interfaces. The visual system was designed in Claude Design and
implemented across `web/` and the email templates in July 2026.

**Design sources (authoritative):** `docs/design-reference/redesign/*.dc.html`
— self-contained HTML mockups with inline styles, desktop (1440) + mobile
(390) frames, and MOTION annotation blocks. Open them in a browser. The
component sheet (`10 Component Sheet.dc.html`) is the system spec; screens
01–09 are per-page truth; `11 Email Templates.dc.html` defines the email
system. The Claude Design project:
https://claude.ai/design/p/a11d8ee8-dfe1-4690-83a0-85abc4325b63

**Where the system lives in code** (single sources of truth):

| What | File |
|---|---|
| Brand hues (never change) | `web/src/styles/brand-tokens.css` ← copied from `brand/colors/tokens.css` |
| Redesign token deltas | `web/src/styles/design-tokens.css` |
| Tailwind mapping (fonts/colors/radii/shadows/gradients/keyframes) | `web/tailwind.config.js` |
| Button + field classes, heading defaults, focus, reduced-motion | `web/src/styles/globals.css` |
| Fonts (next/font, self-hosted) | `web/src/pages/_app.tsx` |
| Pastel assignment (deterministic per mentor) | `web/src/lib/mentor-pastel.ts` |
| Card/portrait treatments | `web/src/components/mentors/MentorsList.tsx`, `web/src/components/ui/MentorPortrait.tsx` |
| Price pills | `web/src/components/ui/PriceBadge.tsx` |
| Toasts | `web/src/components/ui/Notification.tsx` |
| Page chrome | `web/src/components/layout/NavHeader.tsx`, `Footer.tsx` |

## Type system

All OFL variable fonts, self-hosted via `next/font` (CSS variables set in
`_app.tsx`), consumed as Tailwind families:

| Tailwind class | Face | Role | Key styles |
|---|---|---|---|
| `font-display` | Archivo 800 | CAPS headlines, section headers, eyebrows | display-xl 72/0.98 −3%; display-l 34/1.05 −2%; labels 13–15/1 +3% — always uppercase |
| `font-name` | Schibsted Grotesk 700 | People names, big numbers | name-l 40/1.05 −2%; card title 17/1.15 −1.5% |
| `font-sans` | Inter 400–700 | Body, UI copy | 14–15/1.6 — deliberately quiet |
| `font-mono` | IBM Plex Mono 500 | Metadata rows, counts, sort control | 10–12/1 +4% CAPS — use the `.meta-mono` utility |

`h1`/`h2` default to the display treatment globally (globals.css).

## Color

Brand hues are fixed (`brand-navy #132A52`, `brand-cobalt #2F5EFF`,
`brand-mint #17C3B2`). Neutrals come ONLY from the ink/paper family:
`ink` `ink-soft` `ink-mute` (AA on pastels) `ink-faint` (disabled only) /
`surface` `surface-deep` `line`. **Never use Tailwind `gray-*`.** Extras:
`danger #E5484D` (errors/destructive), `mint-ink #0E7A70` (mint-family
text on light surfaces, e.g. FREE price).

Pastels (sky/sand/sage/lavender/rose + neutral) render as top→bottom
gradients — `bg-pastel-*-grad` — assigned deterministically per mentor by
FNV-1a slug hash (`mentorPastelGradClass`). Don't pick pastels manually
for mentor surfaces.

## Radii · shadows · buttons · fields

Radius scale: `rounded-field` 12 · `rounded-card` 14 · `rounded-btn` 14 ·
`rounded-panel` 16. Pills are `rounded-full`; nothing else uses ad-hoc
radii.

One button system (never mix with legacy patterns):
`.button` (primary: navy fill + `shadow-btn` cobalt offset; hover shifts
−1,−1 and grows the shadow) · `.button-secondary` (white, 1.5px
cobalt/45% border) · `.button-ghost` · `.button-destructive`. Disabled
comes from the `[disabled]` attribute.

Fields: `.field` (+ `.field-error`) — 1.5px `line` border, cobalt border +
`shadow-focus-field` ring on focus. Focus-visible everywhere is the global
double ring (white gap + cobalt); never `outline: none` without it.

## Mentor card / portrait state set

Four states (component sheet), chosen by data:

1. **hero** — `photoStyle === 'hero'`: cut-out look; portrait bottom-anchored,
   `mix-blend-multiply` + `contrast(1.03)` on the pastel gradient. Only
   photos with light plain backgrounds qualify — classified at upload by
   `api/pkg/imageclass` (border mean luminance > .78, std < .12) into
   `mentors.photo_style`.
2. **frame** — everything else with a photo: uncut tile, arch mask
   (`rounded-t-panel`), 3px white/75 keyline, `object-position: center 20%`.
3. **initials** — photo missing/failed to load: navy or cobalt circle (hash
   parity), Schibsted 700. Cards always *attempt* the slug-keyed photo and
   fall back on `onError`.
4. **loading** — `bg-shimmer animate-shimmer` skeleton, paper tones only.

Badges on the photo block: `NEW` (cobalt) wins over `N SESSIONS`
(white/90). Meta row is `.meta-mono`: `8Y EXP · $50` — price colored FREE
`mint-ink` / `$N` navy / NEGOTIABLE `ink-mute` (parsing in
`config/filters.ts` + `PriceBadge`).

## Motion (all CSS-only)

Durations: 120ms focus/color · 150–180ms hover/press · 200–240ms
entrances/crossfades · 300ms max one-off pops (`ease-pop`). Prebuilt:
`animate-rise-in` (catalog entrance, 40ms stagger via `animationDelay`,
first 12 only), `animate-shimmer`, `animate-shake` (form errors),
`animate-toast-in`, `animate-dropdown-in`. Card hover: −3px translate +
`shadow-card-hover`, portrait scales 1.03 — transforms only, no layout.
Everything degrades under `prefers-reduced-motion` (global kill-switch in
globals.css) — don't add JS-driven animation.

## Page chrome

`NavHeader` (default nav incl. GitHub link, or `backLink` variant) and the
navy `Footer`. The `_app` wrapper is a `min-h-screen` flex column and the
footer carries `mt-auto` — pages must NOT wrap themselves in their own
min-height containers or the footer pinning breaks.

## Emails

Table-based, 600px white card, radius 14, **inline styles only**,
Arial stack (no webfonts), bulletproof padded-table-cell buttons, hidden
preheader div, accent-bar callouts (4px colored td + surface td), hairline
footer. Every template has a matching plain-text body. Templates:
`api/pkg/email/templates/assets/*.json`, registry pinned by
`templates_test.go`. Follow `11 Email Templates.dc.html` for anything new.

## Rules of thumb

- The `.dc.html` files win over this document; this document wins over
  guessing. Deviations from the mockups (and why) are recorded in the
  redesign commit messages on the `redesign` branch.
- No `gray-*`, no second button style, no ad-hoc radii/shadows — extend
  the tokens instead (tailwind.config.js + design-tokens.css together).
- Anything mentor-visual (pastel, portrait, price, initials) goes through
  the shared helpers, never re-derived locally.
- AA contrast minimum; `ink-mute` is the darkest text allowed on pastels.
- Historical context: `redesign-assessment.md` (the pre-redesign WeGrow
  phases) and `claude-design-brief.md` (the brief that produced these
  designs).
