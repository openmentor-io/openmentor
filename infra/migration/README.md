# Migration tooling

Two scripts live here:

- **`migrate-mentors.sh` / `migrate-mentors.js`** — the getmentor.dev →
  openmentor.io mentor migration (DECISIONS D22): per-slug (or CSV-bulk)
  import with RU→EN translation via the Claude API, enum/tag/price mapping,
  new legacy ids, photo copy and a notification email through the worker.
  Idempotent, dry-run first. **Full instructions:
  [docs/runbooks/mentor-migration.md](../../docs/runbooks/mentor-migration.md).**

  ```bash
  npm install
  ./migrate-mentors.sh --slug ivan-petrov-42 --dry-run   # always dry-run first
  ./migrate-mentors.sh --csv mentors.csv                 # bulk
  ```

- **`yandex-to-s3-migration.js`** — the one-off bulk image copy, documented
  below. (For per-mentor image copies, migrate-mentors.js does its own —
  keyed to the *new* slug.)

`yandex-ca.pem` is the public Yandex Managed PostgreSQL cluster CA
(expires 2027) used by migrate-mentors.js to verify the source DB's TLS.

# Yandex Object Storage to AWS S3 Migration Script

`yandex-to-s3-migration.js` copies all objects from a Yandex Object Storage
bucket (via its S3 API at `storage.yandexcloud.net`) to an AWS S3 bucket,
preserving object keys exactly (the `mentor-images/<slug>/<size>` structure).
This is the openmentor.io image migration per DECISIONS D15 (AWS S3 for
profile pictures). TODO(P6.4): this tooling (and the Yandex account) can be
retired once the image copy has run and the registry moves to AWS ECR (D19).

> Historical note: earlier one-off scripts that lived here
> (`azure-to-yandex-migration.js` for the Azure Blob → Yandex image copy and
> `airtable-to-postgres-migration.js` for the Airtable → Postgres data
> migration) were removed after those migrations completed — openmentor.io
> starts from a fresh Postgres database and never uses Airtable or Azure.
> They remain available in git history if ever needed.

## Features

- Preserves exact object keys (`<slug>/full|large|small`)
- Idempotent: skips objects that already exist in the destination with the
  same size (and ETag, when both sides expose a plain MD5 ETag) — safe to re-run
- `--dry-run` flag: lists what would be copied without writing anything
- Progress logging per object + summary report
- Continues on individual errors; exit code 1 if any object failed
- Copy operation — the Yandex bucket is left untouched

## Configuration

Set these in `.env` (see `.env.example`) or export them:

```bash
# Source: Yandex Object Storage (static access key of a service account)
export SOURCE_S3_ACCESS_KEY="your-yandex-access-key-id"
export SOURCE_S3_SECRET_KEY="your-yandex-secret-access-key"
export SOURCE_S3_BUCKET="your-yandex-bucket-name"
# Optional (defaults shown)
export SOURCE_S3_ENDPOINT="https://storage.yandexcloud.net"
export SOURCE_S3_REGION="ru-central1"

# Destination: AWS S3
export DEST_S3_ACCESS_KEY="your-aws-access-key-id"
export DEST_S3_SECRET_KEY="your-aws-secret-access-key"
export DEST_S3_BUCKET="mentor-images"
# Optional: leave DEST_S3_ENDPOINT unset for plain AWS S3; set it only for
# another S3-compatible provider (R2, B2, ...)
export DEST_S3_ENDPOINT=""
export DEST_S3_REGION="eu-central-1"
```

The destination credentials need `s3:ListBucket`, `s3:GetObject` (for the
HeadObject existence check) and `s3:PutObject` on the destination bucket; the
source credentials need read/list access on the source bucket.

## Usage

```bash
cd migration
npm install

# 1. Dry run first — shows what would be copied, writes nothing
node --env-file=.env yandex-to-s3-migration.js --dry-run

# 2. Real run
node --env-file=.env yandex-to-s3-migration.js
# or: npm run migrate-images-s3

# 3. Re-run to verify idempotency — everything should be "Skipped"
node --env-file=.env yandex-to-s3-migration.js
```

## How it Works

1. Lists all objects in the Yandex bucket (paginated `ListObjectsV2`)
2. For each object, `HeadObject` on the AWS S3 bucket:
   - exists with matching size/ETag → skip
   - missing or different → download from Yandex, upload to S3 with the same
     key and Content-Type
3. Prints a summary (migrated / skipped / failed)
