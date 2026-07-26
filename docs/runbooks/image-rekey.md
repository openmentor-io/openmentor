# Runbook: re-keying profile images to mentor-UUID paths

Part of custom usernames (DECISIONS **D28**). Profile photos used to live in
S3 under the mentor's slug (`<slug>/{full,large,small}`). Because usernames are
now changeable, images are keyed by the immutable mentor UUID
(`mentors.id` → `<uuid>/{full,large,small}`) so a rename never has to move an
object. `infra/migration/rekey-images.js` is the **one-time** copy that moves
every existing photo to its new key.

New registrations and picture uploads already write UUID keys — this script
only backfills photos that predate the change.

## What the script does

For every mentor (`SELECT id, slug FROM mentors`), for each size:

1. Skip if the destination `<uuid>/<size>` already exists (idempotent).
2. Skip if the source `<slug>/<size>` is missing (nothing to copy).
3. Otherwise server-side `CopyObject` from `<slug>/<size>` to `<uuid>/<size>`.

Old slug-keyed objects are **left in place** (they simply become unused). The
script prints a summary of copied / skipped / missing counts.

## Prerequisites

Same stack and env as `migrate-mentors.js` — it reuses the `migrate-mentors.sh`
wrapper for the DB tunnel and credentials:

- `TARGET_DATABASE_URL` — set by the wrapper (openmentor.io production Postgres).
- `DEST_S3_ACCESS_KEY` / `DEST_S3_SECRET_KEY` / `DEST_S3_BUCKET`
  [`/ DEST_S3_ENDPOINT / DEST_S3_REGION`] — from `infra/migration/.env`.

## Running it

Always dry-run first (read-only: lists what would be copied, touches nothing):

```bash
cd infra/migration
MIGRATE_SCRIPT=rekey-images.js ./migrate-mentors.sh --dry-run
```

Then the real run:

```bash
MIGRATE_SCRIPT=rekey-images.js ./migrate-mentors.sh
```

## Deploy order

The UUID-keyed reads ship in the same release as the copy. Run the script
around the deploy so no photo is missing:

1. **Run the re-key script** — copies all existing photos to UUID keys while
   the app is still serving slug-keyed reads.
2. **Deploy** the `custom-username` release (`deploy.sh`). Migrations
   (`000006_slug_history`, `000007_slug_changed_at`) run automatically before
   api/worker. From here the app reads/writes images at `<uuid>/…`.
3. **Re-run the re-key script once more** — copies photos uploaded in the gap
   whose UUID destination doesn't exist yet. This rerun never overwrites an
   existing destination, so it can't clobber a live upload. It reports any
   destination whose slug source is *newer* (a photo replaced during the gap)
   as "stale (need --refresh)".
4. **Only if step 3 reported stale destinations:** briefly disable
   profile-picture uploads (quiesce), then run once with `--refresh` to
   re-copy those replaced photos, and re-enable uploads:
   ```bash
   MIGRATE_SCRIPT=rekey-images.js ./migrate-mentors.sh --refresh
   ```
   `--refresh` is the only mode that overwrites, and its HEAD→copy step can
   race a concurrent upload — hence the quiesce. In practice a mentor replacing
   their photo in the short deploy gap is rare, so step 4 is usually a no-op.

## Verifying

```bash
# A migrated photo is reachable under the UUID key
../db.sh -c "SELECT id, slug FROM mentors WHERE status IN ('active','inactive') LIMIT 5"
curl -sI https://cdn.openmentor.io/<uuid>/large | head -1   # expect 200
```

Spot-check a couple of profile pages in the browser — images should load, and
the network tab should show `cdn.openmentor.io/<uuid>/...` rather than
`/<slug>/...`.

## Notes

- Safe to re-run any time: the default mode only fills in missing destinations
  and never overwrites an existing one, so it can't clobber a live upload.
  Overwriting a replaced photo requires the explicit `--refresh` mode, which
  must run with uploads quiesced (see step 4 above).
- **Sweep the legacy slug-keyed copies once the cutover is verified** — don't
  leave them indefinitely. After a mentor renames, a leftover `<old-slug>/…`
  object is only reachable via `mentor_slug_history`, and that row is trimmed
  after 2 more changes (and cascade-deleted if the mentor is erased), so a
  stale copy can become an unreferenced blob of personal data that the
  right-to-erasure flow (`data-deletion.md`) can no longer locate. Going
  forward images are keyed by the immutable mentor UUID, which erasure can
  always find; the one-time job here is to delete the pre-cutover `<slug>/…`
  objects after confirming every `<uuid>/…` copy resolves. Until that sweep
  runs, follow `data-deletion.md`'s "every current/retired slug prefix" step
  when erasing a mentor.
