# 04 — Legal & Compliance

> ⚠️ Everything here is engineering-side preparation. **A qualified lawyer must review the final documents before launch.** Do not treat generated legal text as final.

## Current state (getmentor)

- `src/pages/privacy.tsx` — Russian privacy policy referencing RU law FZ-152, operator named personally (Георгий Могелашвили).
- `src/pages/disclaimer.tsx` — Russian liability disclaimer (platform is a technical intermediary; disputes are mentor↔mentee).
- No cookie consent banner; PostHog/Mixpanel/GTM load unconditionally.
- No terms of service as such; no data-deletion self-service.

## Applicability assessment

| Regulation | Applies? | Why |
|---|---|---|
| **GDPR** (EU/EEA) | **Yes** | Global site will serve EU users; processes names, emails, Telegram handles, request texts, reviews, analytics. |
| **UK GDPR / PECR** | Yes | Same, UK users; PECR drives cookie consent. |
| **CCPA/CPRA** (California) | Likely below thresholds initially (revenue/volume), but the privacy policy should include CCPA-style disclosures anyway. |
| **HIPAA** | **No.** HIPAA covers US healthcare "covered entities" and their business associates handling PHI. A tech-mentorship platform handles no PHI. Mentioned here because the brief asked; the correct posture is: don't claim HIPAA compliance, and state in ToS that the service is not for exchanging medical information. |
| FZ-152 (RU) | No — openmentor.io does not target RU; that stays getmentor.dev's concern. |

## Work items

### 4.1 New legal pages (frontend `openmentor/`)

1. **Privacy Policy** (`src/pages/privacy.tsx`, rewritten in English):
   - Controller identity (DECISION: person vs. legal entity — currently the RU policy names the owner personally; for openmentor.io decide entity + contact address).
   - Data collected: account data (mentor profiles), request data (mentee name/email/handle/message), reviews, technical/analytics data.
   - Purposes & lawful bases (contract performance for matching; legitimate interest/consent for analytics).
   - Processors/sub-processors list: hosting provider, email provider, PostHog/Mixpanel, Google (ReCAPTCHA), Calendly et al. embeds, storage provider. (Fill in after infra decisions in 05.)
   - International transfers, retention periods, data-subject rights + contact channel (`privacy@openmentor.io`), complaint rights.
2. **Terms of Service** (`src/pages/terms.tsx`, new): platform-as-intermediary model, no payment processing, code of conduct, moderation rights, disclaimers/liability (absorbs the old disclaimer page), governing law (DECISION).
3. **Cookie/consent banner**: gate PostHog/Mixpanel/GTM behind consent for EU visitors (simplest v1: consent-gate analytics for everyone; e.g. a lightweight self-built banner storing a consent cookie, analytics initialized only after accept). ReCAPTCHA disclosure in policy.
4. Footer links updated: Privacy, Terms; remove old disclaimer link.

### 4.2 GDPR mechanics (backend `openmentor-api/` + process)

- **Right to erasure**: v1 = documented manual process (support email → operator deletes mentor/requests rows + blob images). v2 = self-service delete in mentor dashboard. Add a `docs/runbooks/data-deletion.md` runbook.
- **Data export**: v1 manual (SQL per-subject dump).
- **Retention**: define & document (e.g., declined requests purged after N months — DECISION); implement as cron later.
- **Email consent**: transactional emails are fine without opt-in; `tg-mass-send`-style broadcast is deleted — any future newsletter needs explicit opt-in.
- **DPAs**: sign/accept DPAs with hosting, email, analytics providers (human task, checklist in DECISIONS).
- Records of processing (simple ROPA table) in `docs/legal/ropa.md` (human+LLM drafted).

### 4.3 Security posture already in place (mention in policy)

HttpOnly session cookies, magic-link auth (no passwords stored), rate limiting, TLS everywhere, ReCAPTCHA on public forms. Verify cookie `Secure`+`SameSite` flags in production config.

## Execution order

Legal pages depend on infra provider choices (processor list) → draft after 05 decisions, lawyer review before launch. The consent banner and footer changes can be built immediately.
