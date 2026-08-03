# PostHog Dashboards as Code

This directory contains an idempotent provisioning flow for OpenMentor product dashboards in PostHog.

## What it creates

- 6 dashboards
- 54 insights
- Coverage for frontend, API, and background-worker product flows
  (getmentor-era Telegram-bot and Azure-Functions insights were removed;
  worker events use `source_system=worker`)
- Managed tags for safe re-runs and updates:
  - `managed:openmentor:posthog-dashboard`
  - `managed:openmentor:dashboard:<dashboard_key>`
  - `managed:openmentor:insight:<insight_key>`

## Files

- `spec.mjs`: declarative dashboard + insight definitions
- `sync.mjs`: upsert runner via PostHog API

## Required env vars

- `POSTHOG_PERSONAL_API_KEY`: PostHog personal API key with at least insight/dashboard read+write scopes
- `POSTHOG_PROJECT_ID`: target PostHog project ID

## Optional env vars

- `POSTHOG_HOST`:
  - default: `https://app.posthog.com`
  - use `https://eu.posthog.com` for EU cloud projects
  - use your own hostname for self-hosted PostHog
- `POSTHOG_DASHBOARD_ENVIRONMENT`:
  - default: `production`
  - injected as an event property filter into all insights
- `POSTHOG_DRY_RUN`:
  - `true` to print non-GET API operations without mutating resources

## Commands

Validate spec only:

```bash
node ./posthog/dashboards/sync.mjs --validate
```

Dry-run sync:

```bash
POSTHOG_DRY_RUN=true \
POSTHOG_PERSONAL_API_KEY=phx_xxx \
POSTHOG_PROJECT_ID=12345 \
POSTHOG_HOST=https://eu.posthog.com \
node ./posthog/dashboards/sync.mjs
```

Apply sync:

```bash
POSTHOG_PERSONAL_API_KEY=phx_xxx \
POSTHOG_PROJECT_ID=12345 \
POSTHOG_HOST=https://eu.posthog.com \
node ./posthog/dashboards/sync.mjs
```

## Required follow-up: launch funnel step 3 (insight 5138182)

**Not fixable here — see "why not the spec" below.** Tracked so the number is not
mistaken for growth.

- Insight **5138182** (`xYJKKkY6`), "Launch · Funnel: profile view → request",
  on dashboard **843674** "Launch", tile **5377367**.
- **Mechanism.** Step 3 is `mentee_contact_submitted` filtered on
  `outcome = success` only — no `source_system` — and funnels aggregate BY
  PERSON. Both the browser and the Go API emit that event. The API copy used to
  be keyed `request:<uuid>`, one isolated person per request that had fired
  neither earlier step, so it contributed nothing and step 3 was silently
  frontend-only: 468 -> 33 -> 7 (1.5%), measured 2026-08-03. Branch
  `fix/audit-p0-telemetry` re-keys it to `MentorDistinctID` — the same
  `mentor:<uuid>` that `MentorAuthContext` identifies a logged-in mentor as — so
  those events now merge onto real mentor persons, who *can* also have
  `mentor_profile_viewed` and `mentor_contact_page_viewed` from browsing the
  public catalog.
- **Magnitude.** Step 3 is expected to rise from 7 toward 12-19, roughly
  doubling the reported conversion rate. That is an instrumentation artifact, not
  growth. The exact figure depends on how many of those mentor persons have the
  two earlier steps, so re-read the funnel after the deploy rather than trusting
  the estimate.
- **Two options** (pick one; both are manual changes in the PostHog UI):
  1. Add `source_system = frontend` to step 3, matching its siblings. Keeps the
     series comparable across the deploy; the funnel then measures the browser
     journey only, which is what steps 1 and 2 already measure.
  2. Leave the filter and annotate dashboard 843674 on the deploy date so the
     jump reads as an instrumentation change. Keeps API-side submissions in the
     numerator, at the cost of a discontinuity.
- **Siblings are already insulated** and need no change: **5036965**
  (`N4x5FQSb`, "Discovery → contact funnel", dashboard 826473) and **5036958**
  (`3bFe3WJs`, "Visitor → contact conversion rate", dashboard 826472 — a trends
  ratio, not a funnel) both filter `source_system = frontend` on their
  `mentee_contact_submitted` series.
- **Why not the spec.** `spec.mjs` does not manage insight 5138182, so the fix
  cannot live there. It builds `TrendsQuery` insights only — there is no funnel
  builder — and `sync.mjs` matches by the `managed:openmentor:insight:<key>` tag,
  which 5138182 does not carry (its only tag is `launch`). Nor is dashboard
  843674 "Launch" one of the six the spec declares. `spec.mjs` is deliberately
  left untouched: adding this funnel to it would mean adopting three
  hand-made launch insights into the managed set, which is a separate decision.

## Notes

- Re-running is safe: dashboards/insights are matched by managed tags and updated in place.
- The sync preserves existing tags and adds managed tags; it does not delete dashboards or insights.
- If your production analytics uses a different `environment` value, set `POSTHOG_DASHBOARD_ENVIRONMENT` accordingly before syncing.
