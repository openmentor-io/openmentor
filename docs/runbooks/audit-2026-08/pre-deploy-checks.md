# Pre-deploy checks for the 2026-08 remediation

**Run these before the remediation deploy, not after.** Each one is a case where
the fix is stricter than what production already holds, so the count tells you
how many real people will meet the new rule on their next save. None of them
blocks the deploy; all of them decide whether you warn someone first.

Read-only. No writes, no migration, nothing here changes code.

---

## §1 — mentors holding a legacy `http://` calendar link

**Introduced by:** audit item **P6** (`https_url` binding tag,
`api/internal/models/validation.go`), applied to all three calendar bindings —
mentor registration, mentor profile save, and admin moderation.

### What changes

The old binding was validator/v10's `url` tag, which does not restrict the
scheme: `http://`, `javascript:`, `data:` and `mailto:` all passed it. The new
`https_url` tag requires an absolute **https** URL with a host, no userinfo, and
none of the characters that would break out of an HTML attribute.

Nothing is rewritten and there is no migration. Stored rows keep rendering
exactly as before — the public profile page and the email template both go
through `safeHttpUrl`, which deliberately stays looser for that reason. **Only
new writes are validated.**

The consequence is therefore not a display change, it is a save path: a mentor
whose stored `calendar_url` begins `http://` cannot save their profile at all —
including an edit to a completely unrelated field — until they also fix that
field. The same applies to a moderator editing that mentor.

### The check

```sql
SELECT count(*), min(created_at) FROM mentors WHERE calendar_url LIKE 'http://%';
```

Run it the same way as the diagnostics:

```bash
infra/db.sh -c "SELECT count(*), min(created_at) FROM mentors WHERE calendar_url LIKE 'http://%';"
```

`min(created_at)` is there to tell you which population you are looking at: a
date at or before the getmentor.dev import means these are inherited rows, not
something mentors typed on this platform.

### If it returns 0

Nothing to do. Deploy.

### If it returns rows

Deploy anyway — but know that each of those mentors hits a blocked save on their
next profile edit, and decide which of these you want:

1. **Do nothing (acceptable).** The block is self-serve and visible: the profile
   form shows *"This must be a valid https:// URL"* against that field, and the
   admin moderation editor shows the field-level reason inline and marks the
   input. The mentor changes `http` to `https` and saves. This is the deliberate
   design — P6 landed the client-side mirror (`web/src/lib/safe-url.ts`) in the
   same change precisely so the failure is a named field error rather than the
   opaque 400 that `mentor/profile/edit.tsx` renders as *"Something went
   wrong"*.

2. **Warn them first (if the count is small).** The affected rows are cheap to
   list, and a mentor told in advance will not open a support ticket:

   ```sql
   SELECT id, slug, email, calendar_url
     FROM mentors
    WHERE calendar_url LIKE 'http://%'
    ORDER BY created_at;
   ```

   That output contains personal data — treat it like the diagnostics report
   (see the README's "Before you start").

3. **Do NOT bulk-rewrite `http://` to `https://`.** Tempting and wrong: nothing
   guarantees the booking host serves https on the same path, and a silently
   rewritten link that 404s is worse for the mentee than a link the mentor was
   asked to confirm. If a host is known-good you can fix that one row by hand,
   but there is no safe blanket UPDATE here.

### Related

`diagnostics.sql` **D3** already reports every non-https `calendar_url` as part
of the email-injection sweep, with a wider predicate (`!~* '^https://'`, so it
also catches `javascript:` and `data:`). If you have run the diagnostics, cross
-check against it: a `javascript:` or `data:` row there is an incident, not a
pre-deploy nuisance — follow D3, not this section.
