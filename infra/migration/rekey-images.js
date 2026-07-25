#!/usr/bin/env node
/**
 * rekey-images.js — one-time copy of profile images from slug-based S3 keys
 * to mentor-UUID-based keys.
 *
 * Context: images were historically stored under `<slug>/{full,large,small}`.
 * With user-changeable usernames, objects are now keyed by the immutable
 * mentor UUID (`mentors.id`) instead: `<uuid>/{full,large,small}`. This
 * script copies every existing photo to its new key (server-side S3 copy;
 * old objects are left in place and become unused).
 *
 * Run via the migrate-mentors.sh wrapper (it provides the DB tunnel and env):
 *
 *   MIGRATE_SCRIPT=rekey-images.js ./migrate-mentors.sh --dry-run
 *   MIGRATE_SCRIPT=rekey-images.js ./migrate-mentors.sh
 *
 * Idempotent: objects whose destination key already exists are skipped, so
 * re-running (e.g. once more after the deploy, to catch photos uploaded in
 * between) is safe and fast.
 *
 * Env (same contract as migrate-mentors.js): TARGET_DATABASE_URL (set by the
 * wrapper), DEST_S3_ACCESS_KEY / DEST_S3_SECRET_KEY / DEST_S3_BUCKET
 * [/ DEST_S3_ENDPOINT / DEST_S3_REGION] from infra/migration/.env.
 */

const { Client } = require('pg');
const { S3Client, HeadObjectCommand, CopyObjectCommand } = require('@aws-sdk/client-s3');

const IMAGE_SIZES = ['full', 'large', 'small'];

const config = {
  targetDatabaseUrl: process.env.TARGET_DATABASE_URL,
  destS3: {
    accessKey: process.env.DEST_S3_ACCESS_KEY,
    secretKey: process.env.DEST_S3_SECRET_KEY,
    bucket: process.env.DEST_S3_BUCKET,
    endpoint: process.env.DEST_S3_ENDPOINT || '',
    region: process.env.DEST_S3_REGION || 'eu-central-1',
  },
};

const dryRun = process.argv.includes('--dry-run');

function assertConfig() {
  const problems = [];
  if (!config.targetDatabaseUrl) {
    problems.push('TARGET_DATABASE_URL is required (run via migrate-mentors.sh — it opens the DB tunnel)');
  }
  for (const key of ['accessKey', 'secretKey', 'bucket']) {
    if (!config.destS3[key]) problems.push(`DEST_S3_* incomplete: missing ${key} (see infra/migration/.env)`);
  }
  if (problems.length > 0) {
    console.error('❌ Configuration problems:');
    for (const p of problems) console.error(`   - ${p}`);
    process.exit(1);
  }
}

function s3Client() {
  return new S3Client({
    region: config.destS3.region,
    ...(config.destS3.endpoint ? { endpoint: config.destS3.endpoint } : {}),
    credentials: {
      accessKeyId: config.destS3.accessKey,
      secretAccessKey: config.destS3.secretKey,
    },
  });
}

async function objectExists(s3, key) {
  try {
    await s3.send(new HeadObjectCommand({ Bucket: config.destS3.bucket, Key: key }));
    return true;
  } catch (error) {
    if (error.name === 'NotFound' || error.$metadata?.httpStatusCode === 404) return false;
    throw error;
  }
}

async function main() {
  assertConfig();
  const s3 = s3Client();
  const db = new Client({ connectionString: config.targetDatabaseUrl });
  await db.connect();

  const { rows: mentors } = await db.query(
    `SELECT id, slug FROM mentors WHERE slug IS NOT NULL AND slug <> '' ORDER BY created_at`
  );
  console.log(`Found ${mentors.length} mentors. Mode: ${dryRun ? 'DRY-RUN' : 'LIVE'}\n`);

  let copied = 0;
  let skippedExisting = 0;
  let noSource = 0;
  let failed = 0;

  for (const mentor of mentors) {
    for (const size of IMAGE_SIZES) {
      const sourceKey = `${mentor.slug}/${size}`;
      const destKey = `${mentor.id}/${size}`;
      try {
        if (await objectExists(s3, destKey)) {
          skippedExisting++;
          continue;
        }
        if (!(await objectExists(s3, sourceKey))) {
          noSource++;
          continue;
        }
        if (dryRun) {
          console.log(`  would copy ${sourceKey} -> ${destKey}`);
          copied++;
          continue;
        }
        await s3.send(
          new CopyObjectCommand({
            Bucket: config.destS3.bucket,
            // CopySource must be URL-encoded (slugs are URL-safe, but be strict).
            CopySource: encodeURIComponent(`${config.destS3.bucket}/${sourceKey}`),
            Key: destKey,
          })
        );
        copied++;
        console.log(`  ✅ ${sourceKey} -> ${destKey}`);
      } catch (error) {
        failed++;
        console.error(`  ❌ ${sourceKey} -> ${destKey}: ${error.message}`);
      }
    }
  }

  await db.end();
  console.log(
    `\nDone: ${copied} ${dryRun ? 'to copy' : 'copied'}, ` +
      `${skippedExisting} already at destination, ${noSource} without a source photo, ${failed} failed.`
  );
  process.exit(failed > 0 ? 1 : 0);
}

main().catch((error) => {
  console.error(`❌ ${error.message}`);
  process.exit(1);
});
