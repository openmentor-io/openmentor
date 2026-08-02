# Security Policy

OpenMentor holds real personal data — mentor profiles, and the contact details
and messages mentees send to mentors. Vulnerability reports are taken seriously
and are always welcome.

## Reporting a vulnerability

**Please don't open a public issue, PR, or discussion for a security problem.**

Report it privately, either way:

1. **GitHub private vulnerability reporting** (preferred) — go to the
   [Security tab](https://github.com/openmentor-io/openmentor/security) and
   click **Report a vulnerability**. This keeps the report, the discussion and
   the fix in one private thread.
2. **Email** — security@openmentor.io.

### What to include

The more of this you can provide, the faster it gets fixed:

- What the issue is and what an attacker could do with it
- Steps to reproduce, or a proof of concept
- The affected component — `web/`, `api/`, the worker, or the infrastructure
- Any conditions needed (a specific role, a particular state, a race)

## What to expect

This project is maintained by one person, funded by donations, with no security
team and no bug bounty. Being honest about that up front:

- **Acknowledgement within 5 days.** If you haven't heard back by then, please
  chase — assume the message got lost, not ignored.
- **An assessment within about two weeks**, including whether it's being fixed
  and roughly when.
- **Credit if you want it.** Tell us how you'd like to be named, or say you'd
  rather stay anonymous.
- **No payment.** There's no bounty programme.

Please give a reasonable window to ship a fix before disclosing publicly. If a
fix is taking too long, say so and we'll agree on a date rather than letting it
drift.

## Scope

**In scope:** openmentor.io and its subdomains, and everything in this
repository — the web app, the Go API, the background worker, and the deployment
configuration in `infra/`.

**Out of scope:**

- Denial-of-service and volumetric attacks — please don't test these against
  production; a single VM serves the whole site
- Social engineering of the maintainer, mentors, or mentees
- Vulnerabilities in third-party services (AWS, Cloudflare, Grafana Cloud) —
  report those to the provider
- Missing hardening headers or best-practice findings with no demonstrated
  impact — still useful, but open a normal issue for those
- Automated scanner output submitted without verification

## Testing responsibly

If you're probing for issues, please:

- **Use your own accounts and your own data.** Never access, modify, or retain
  another person's profile, request, or message. If you stumble into someone
  else's data, stop, and tell us what you saw so we can assess the exposure.
- Don't degrade the service for real users.
- Prefer a local stack for anything invasive — `cd infra && ./deploy-dev.sh all --yes`
  brings up the full production service set on your machine.

Good-faith research that follows this policy is welcome, and we won't pursue or
support action against anyone who reports in good faith and follows the rules
above.

## Supported versions

OpenMentor is a hosted service rather than a released library. The only
supported version is what's currently deployed from `main`; fixes ship forward
and there are no backported release branches.

If you self-host from this repository under the AGPL, you're responsible for
your own deployment and for keeping it current. Reports about vulnerabilities in
the code itself are still very much wanted — that benefits everyone running it.
