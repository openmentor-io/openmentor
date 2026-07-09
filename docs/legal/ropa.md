# Records of Processing Activities (ROPA)

> In-house review pass completed 2026-07-09 (D7/D13). NOT professional legal advice — a lawyer review is still recommended before significant scale (see docs/legal/review-2026-07-09.md).

**Controller:** Georgiy Mogelashvili, an individual (sole operator of openmentor.io), based in the Netherlands (D7)
**Supervisory authority:** Autoriteit Persoonsgegevens (NL)
**Contact:** privacy@openmentor.io (general: hello@openmentor.io)

| # | Activity | Data subjects | Personal data | Purpose | Lawful basis | Recipients / processors | Retention | Transfers |
|---|---|---|---|---|---|---|---|---|
| 1 | Mentor profiles | Mentors | Name, email, preferred contact (optional free text), job title, workplace, bio, photo, price | Operate the mentor catalog; matching | Contract (Art. 6(1)(b)) | Hosting (Hetzner, EU), storage bucket (AWS S3 eu-central-1), email (AWS SES eu-central-1) | While profile active; deleted on erasure request (D13) | EU hosting; AWS EU regions |
| 2 | Mentee contact requests | Mentees | Name, email, preferred contact (optional free text), request text, self-assessed level | Deliver the request to the chosen mentor; session coordination | Contract | Hosting, email provider; the chosen mentor | While relevant to the service; deleted/anonymized on erasure request (D13 — no fixed expiry) | as above |
| 3 | Session reviews | Mentees | Review text, ratings, NPS | Quality feedback to mentors and platform | Legitimate interest | Hosting; the reviewed mentor | While relevant to the service; deleted/anonymized on erasure request (D13 — no fixed expiry) | as above |
| 4 | Authentication | Mentors, moderators | Email, single-use login tokens, session cookies | Account access (magic links) | Contract | Hosting, email provider | Tokens 15 min; sessions per TTL | as above |
| 5 | Product analytics | All visitors | Pseudonymous events, device data | Product improvement | Consent (cookie banner, P5.2) | PostHog (EU cloud) — Mixpanel DROPPED (D18, 2026-07-09) | Provider defaults | per provider |
| 6 | Anti-abuse | All visitors | Turnstile signals, IPs, rate-limit counters | Spam/bot protection | Legitimate interest | Cloudflare Turnstile | Transient / log retention | Cloudflare (US, SCCs/DPF) |
| 7 | Transactional email | Mentors, mentees, moderators | Email, name, message content | Notifications (requests, approvals, reminders, login links) | Contract | AWS SES | Provider log retention | SES EU region (eu-central-1, per infra env templates) |
| 8 | Server logs & observability | All visitors | IPs, user agents, request metadata | Security, debugging, uptime | Legitimate interest | Grafana Cloud (choose EU region) | ~30 days (configure) | EU region |

**Sub-processor list (for privacy policy):** Hetzner (hosting), AWS SES (email), AWS S3 (profile-image storage — D15), PostHog (EU cloud, consent-gated), Google Tag Manager (tag loading, consent-gated), Grafana Labs (observability), Cloudflare (DNS/CDN, Turnstile captcha), Calendly/Koalendar/Calendlab (only when a mentor embeds them — data goes subject→provider directly).

**Open items:** confirm Grafana Cloud stack is in an EU region; DPAs to accept with each provider (checklist in 04-legal-compliance.md §4.2). Resolved: D7 controller & governing law (NL, 2026-07-09); D13 retention = retain-while-relevant + erasure-on-request (2026-07-09); SES/S3 EU regions (eu-central-1); Mixpanel keep/drop — DROPPED (D18, 2026-07-09).
