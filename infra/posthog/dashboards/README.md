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

## Notes

- Re-running is safe: dashboards/insights are matched by managed tags and updated in place.
- The sync preserves existing tags and adds managed tags; it does not delete dashboards or insights.
- If your production analytics uses a different `environment` value, set `POSTHOG_DASHBOARD_ENVIRONMENT` accordingly before syncing.
