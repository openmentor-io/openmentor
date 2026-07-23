# UTM conventions

How campaign links into openmentor.io are tagged. PostHog (posthog-js)
captures `utm_*` query params automatically on every pageview — they become
event properties and `$initial_utm_*` person properties. No app code is
involved; this doc only pins the vocabulary so breakdowns stay clean.

## Vocabulary

| Param | Values | Rule |
|---|---|---|
| `utm_source` | `linkedin`, `telegram`, `producthunt`, `reddit`, `mentor-share`, `mentor-email`, `founding-dm`, `spotlight`, `<newsletter-name>` | Where the click physically happened. Lowercase, hyphenated. One value per channel — never invent near-duplicates (`Linkedin`, `linked-in`). |
| `utm_medium` | `social`, `email`, `newsletter`, `community`, `dm` | The kind of channel. |
| `utm_campaign` | `launch`, `profile-share`, `spotlight` | The initiative. Evergreen product links (e.g. the dashboard share button) use `profile-share`, not a dated campaign. |

Examples:

```
https://openmentor.io?utm_source=linkedin&utm_medium=social&utm_campaign=launch
https://openmentor.io/mentor/<slug>?utm_source=mentor-share&utm_medium=social&utm_campaign=profile-share
https://openmentor.io/bementor?utm_source=founding-dm&utm_medium=dm&utm_campaign=launch
```

## Rules

- **Hacker News gets no UTMs** — HN users strip/flag tracking params;
  referrer attribution is good enough.
- Internal navigation NEVER carries UTMs (they're for inbound links only).
- The `<slug>/hero`-style share surfaces bake their own tags:
  - Dashboard "Share your profile" button and the approval email's share
    CTA → `mentor-share / social / profile-share` (see
    `web/src/components/mentor-admin/ShareProfileCard.tsx` and
    `api/internal/worker/jobs.go` `mentorProfileShareURL`).
- When adding a new source value, add it to the table above in the same PR.

## Where to look in PostHog

Project 225742 (EU). Breakdowns: any insight → breakdown by
`utm_source` / `utm_campaign` event property, or filter persons by
`$initial_utm_source` for acquisition cohorts.
