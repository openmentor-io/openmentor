# Records of Processing Activities (ROPA) — DRAFT

> DRAFT for lawyer review. Controller identity pending DECISIONS D7.

**Controller:** TBD (D7 — person or legal entity operating openmentor.io)
**Contact:** privacy@openmentor.io

| # | Activity | Data subjects | Personal data | Purpose | Lawful basis | Recipients / processors | Retention | Transfers |
|---|---|---|---|---|---|---|---|---|
| 1 | Mentor profiles | Mentors | Name, email, telegram handle, job title, workplace, bio, photo, price | Operate the mentor catalog; matching | Contract (Art. 6(1)(b)) | Hosting (Hetzner, EU), storage bucket, email (AWS SES) | While profile active + 12 mo after deletion request grace | EU hosting; SES region TBD |
| 2 | Mentee contact requests | Mentees | Name, email, telegram handle (optional), request text, self-assessed level | Deliver the request to the chosen mentor; session coordination | Contract | Hosting, email provider; the chosen mentor | 24 months (D13, pending) | as above |
| 3 | Session reviews | Mentees | Review text, ratings, NPS | Quality feedback to mentors and platform | Legitimate interest | Hosting; the reviewed mentor | 24 months (D13, pending) | as above |
| 4 | Authentication | Mentors, moderators | Email, single-use login tokens, session cookies | Account access (magic links) | Contract | Hosting, email provider | Tokens 15 min; sessions per TTL | as above |
| 5 | Product analytics | All visitors | Pseudonymous events, device data | Product improvement | Consent (cookie banner, P5.2) | PostHog (EU cloud) — Mixpanel DROPPED (D18, 2026-07-09) | Provider defaults | per provider |
| 6 | Anti-abuse | All visitors | ReCAPTCHA signals, IPs, rate-limit counters | Spam/bot protection | Legitimate interest | Google ReCAPTCHA | Transient / log retention | Google (US, SCCs) |
| 7 | Transactional email | Mentors, mentees, moderators | Email, name, message content | Notifications (requests, approvals, reminders, login links) | Contract | AWS SES | Provider log retention | SES region TBD (choose EU: eu-west-1/eu-central-1) |
| 8 | Server logs & observability | All visitors | IPs, user agents, request metadata | Security, debugging, uptime | Legitimate interest | Grafana Cloud (choose EU region) | ~30 days (configure) | EU region |

**Sub-processor list (for privacy policy):** Hetzner (hosting), AWS SES (email), storage provider (R2/S3 — TBD P6), PostHog, Google (ReCAPTCHA), Grafana Labs (observability), Cloudflare (DNS/CDN), Calendly/Koalendar/Calendlab (only when a mentor embeds them — data goes subject→provider directly).

**Open items:** D7 controller identity & governing law; D13 retention numbers; pick EU regions for SES/Grafana; DPAs to accept with each provider (checklist in 04-legal-compliance.md §4.2). Resolved: Mixpanel keep/drop — DROPPED (D18, 2026-07-09).
