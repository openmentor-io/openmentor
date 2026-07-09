# Redesign Assessment — "WeGrow" Figma Concept (2026-07-07)

Source: owner-exported frames in `figma-exports/` (Figma file "Untitled", early concept under the working name **WeGrow**; we keep the OpenMentor name).

## What the design contains

| Frame | Content |
|---|---|
| `Frame 2131329156.png` | **Primary homepage**: hero "Your growing journey starts here", header (logo + rounded search + About us + Become mentor + avatar), category tab bar, pastel mentor cards with **cut-out photos** |
| `Frame 2131329094.png` | Same catalog, compact crop |
| `Frame 2131329149.png` | **Dark-mode fallback** catalog (avatar chip + name, no cut-outs) |
| `Frame 2131329150.png` | **Light-gray fallback** catalog (same structure) |
| `Frame 2131329159.png`, `Frame 63-*.png` | Logo mark (orange parallelograms) and tab-bar component states |
| `Untitled.pdf` | Full canvas: desktop + mobile homepage, all three variants, ad banners. RU annotation on the fallbacks: *"if photo cut-outs ever can't be done"* — i.e. the designer planned the gray/dark cards as the fallback when background removal fails |

**Design language:** bold grotesque type, generous whitespace, rounded-full search bar, pill category tabs (Experience/Price dropdowns separated from topic tabs by a divider), 4-col card grid. Cards: pastel color blocks (blue/yellow/green/purple/pink), cut-out person photo anchored bottom, meta pill ("10y experience · $30" / "2 sessions · Free"), role-as-headline, first name, company-logo badge.

**Scope gap:** only the homepage/catalog (+ ads) is designed. Mentor profile, contact flow, registration, mentor dashboard, and admin have **no designs** — the design language must be extrapolated.

## Fit with the current codebase

Stack fit is good: Tailwind + custom components, no UI framework to fight. The redesign is a restyle + card/filters rework, not a rebuild.

| Design element | Today | Effort |
|---|---|---|
| Header with search + CTA | Search lives on page body | S — move/restyle |
| Category tabs | Multi-select tag filters (same taxonomy: Development, Management, DevOps, HR, Marketing, Data Science, Design…) | M — MentorsFilters/useMentors rework to tabs + 2 dropdowns |
| Card: role headline, name, experience/price pill | All data exists (job_title, experience, price) | S–M — new card component |
| "N sessions" on card | Data exists (done requests per mentor) but not exposed on list API | S backend + S frontend |
| Pastel card colors | — | S — deterministic palette by index/hash |
| **Cut-out photos** | Plain uploaded photos | **L** — needs a background-removal pipeline at upload (e.g. rembg in the Go API/func, or an external API) + quality fallback. Designer already provided the fallback design for exactly this case |
| Company logo badge | No such data (workplace is free text) | M–L — needs logo sourcing (Clearbit-style API) or mentor upload; skip v1 |
| Dark mode | — | M — Tailwind `dark:` pass, best done after tokens settle |
| Typography/tokens | Open Sans, indigo/blue | S — font swap (e.g. Inter) + Tailwind config |
| Mobile layout | Responsive already | S — verify against mobile frame |

## Recommended phasing

- **Phase A — tokens + fallback design (small, do first):** new font/colors/radii, header with search, pill tab filters, new card in the **gray fallback** style with regular photos. Gets ~70% of the look with zero new infrastructure. Entirely frontend; 1 focused PR.
  - ✅ **Implemented 2026-07-07** on branch `redesign-phase-a` (openmentor repo), commits `210d289..eea1bfa` (tokens → header → hero → tab bar → cards → next/font fix). Inter self-hosted via next/font; ink `#111113` / surface `#F2F3F5` tokens; header with centered pill search (drives mentors search on the homepage) + dark "Become a mentor" CTA + Log in link; two-line hero "Your mentorship journey starts here" with the catalog directly below; Experience/Price dropdown pills + single-select topic tabs (Development, Management, DevOps, HR, Marketing, Data Science, Design, Others — mapped 1:1 onto the existing tag taxonomy, `filters.categories`); gray fallback cards (avatar chip + first name, role headline, "{experience} years · {price}" meta). Deferred within scope: "New mentors"/"No sessions yet"/"Reset all" filter pills dropped from the UI (hook state kept), mentee count removed from cards (sessions count is Phase B). Verified: lint, tsc, 95 Jest tests, production build.
- **Phase B — polish + coverage (medium):** pastel card variants, avatar chip, sessions count (small API addition), mobile pass, extrapolate the language to profile/contact/bementor pages (needs light design judgment since no frames exist).
  - ✅ **Implemented 2026-07-08** on branch `redesign-phase-b` (openmentor repo, not merged), commits `762b562..c56b784` (pastel cards → sessions meta → mobile pass → profile page → contact/fields → bementor+donate → portal touch-up).
    - **Pastel palette** — Figma hues (`#BDEEFF/#FFE7BD/#BDFFBD/#C2BDFF/#FFBDDF`) desaturated/warmed toward `--om-paper-dim` to harmonize with Fresh Signal: sky `#C9E6F2`, sand `#F4E3C0`, sage `#CDE8C6`, lavender `#DDD9F4`, rose `#F6D4E2`; hover deepens to `#B7DCEE/#EED8AA/#BCE0B4/#CFC9EE/#F0C3D6`. Assignment: FNV-1a hash of the mentor slug → 1 of 5 (`src/lib/mentor-pastel.ts`), stable across pages/reloads. The Phase A gray `surface` card is kept as the **fallback** (empty key), not as a 6th rotation color, to match the fully-pastel grid. Contrast verified: ink ≥ 11:1, card meta text (`ink-mute #4A5160`) ≥ 5.0:1 on every base+hover pastel, "New" pill (white/75 + navy) ≥ 12.7:1.
    - **Sessions meta** — `sessionsCount?: number` added to `MentorBase` (optional; Go API populates it separately); cards show "{n} session(s) · {price}" when > 0, else "{experience} years · {price}".
    - **Mobile pass** — hero left-aligned on small screens per the PDF mobile frame (centered from md), shorter single-column cards, edge-to-edge category tab scroll with hidden scrollbar (Experience/Price dropdowns stay outside the overflow container so menus never clip).
    - **Inner pages (extrapolated, no frames)** — mentor profile: two-column with a sticky rounded-2xl photo/meta card + full-width navy CTA, quiet tag pills, bold ink section headings; contact: mentor header chip + form in a rounded-2xl surface card; bementor: form sectioned (Contact details / Your profile / Your expertise / Scheduling) in a surface card; donate: token-aligned card; portal ProfileForm picked up the shared `.field` styling + navy select chips.
    - **Deferred:** mobile floating "Filters" sheet from the mobile frame (kept the scrollable tab bar), role-first card content shift (owner decision pending), everything in Phase C.
    - Verified: lint, tsc, 120 Jest tests, production build.
- **Phase C — signature features (large, optional):** photo cut-out pipeline with automatic fallback to Phase-A cards, company badges, dark mode.

**Bottom line: medium complexity overall.** Phase A is cheap and safe to do now; the expensive parts (cut-outs, badges, missing inner-page designs) are separable and each has a designed fallback or can be deferred.

## Notes

- The logo mark (orange parallelograms) conflicts with nothing — we could adapt it for OpenMentor (current placeholder is a text wordmark), pending owner preference.
- Card headline in the concept is the **role/value proposition**, not the person's name — a real content-model shift (today cards lead with name). Worth confirming the owner wants role-first cards.
- Ad banners in the PDF are marketing collateral, not product scope.
