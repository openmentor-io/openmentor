#!/usr/bin/env node

/**
 * Yandex Object Storage to AWS S3 Migration Script
 *
 * Copies all objects from a Yandex Object Storage bucket (S3 API) to an
 * AWS S3 bucket, preserving object keys exactly (e.g. the
 * mentor-images/<slug>/<size> structure). Idempotent: objects that already
 * exist in the destination with the same size (and ETag, when comparable)
 * are skipped, so the script can be re-run safely.
 *
 * Usage:
 *   node --env-file=.env yandex-to-s3-migration.js [--dry-run]
 *
 * See migration/README.md for the SOURCE_* / DEST_* environment variables.
 */

const {
  S3Client,
  ListObjectsV2Command,
  HeadObjectCommand,
  GetObjectCommand,
  PutObjectCommand,
} = require('@aws-sdk/client-s3');

const DRY_RUN = process.argv.includes('--dry-run');

// Configuration from environment variables
const config = {
  // Source: Yandex Object Storage (S3-compatible API)
  sourceAccessKey: process.env.SOURCE_S3_ACCESS_KEY,
  sourceSecretKey: process.env.SOURCE_S3_SECRET_KEY,
  sourceBucket: process.env.SOURCE_S3_BUCKET,
  sourceEndpoint: process.env.SOURCE_S3_ENDPOINT || 'https://storage.yandexcloud.net',
  sourceRegion: process.env.SOURCE_S3_REGION || 'ru-central1',

  // Destination: AWS S3 (leave DEST_S3_ENDPOINT unset for plain AWS S3)
  destAccessKey: process.env.DEST_S3_ACCESS_KEY,
  destSecretKey: process.env.DEST_S3_SECRET_KEY,
  destBucket: process.env.DEST_S3_BUCKET,
  destEndpoint: process.env.DEST_S3_ENDPOINT || '',
  destRegion: process.env.DEST_S3_REGION || 'eu-central-1',
};

// Migration statistics
const stats = {
  total: 0,
  skipped: 0,
  migrated: 0,
  failed: 0,
  errors: [],
};

/**
 * Validate configuration
 */
function validateConfig() {
  const required = [
    ['SOURCE_S3_ACCESS_KEY', config.sourceAccessKey],
    ['SOURCE_S3_SECRET_KEY', config.sourceSecretKey],
    ['SOURCE_S3_BUCKET', config.sourceBucket],
    ['DEST_S3_ACCESS_KEY', config.destAccessKey],
    ['DEST_S3_SECRET_KEY', config.destSecretKey],
    ['DEST_S3_BUCKET', config.destBucket],
  ];

  const errors = required
    .filter(([, value]) => !value)
    .map(([name]) => `${name} environment variable is required`);

  if (errors.length > 0) {
    console.error('Configuration errors:');
    errors.forEach(err => console.error(`  - ${err}`));
    console.error('\nUsage:');
    console.error('  node --env-file=.env yandex-to-s3-migration.js [--dry-run]');
    console.error('\nEnvironment variables (see README.md):');
    console.error('  SOURCE_S3_ACCESS_KEY, SOURCE_S3_SECRET_KEY, SOURCE_S3_BUCKET');
    console.error('  SOURCE_S3_ENDPOINT (default: https://storage.yandexcloud.net)');
    console.error('  SOURCE_S3_REGION (default: ru-central1)');
    console.error('  DEST_S3_ACCESS_KEY, DEST_S3_SECRET_KEY, DEST_S3_BUCKET');
    console.error('  DEST_S3_ENDPOINT (leave unset for AWS S3)');
    console.error('  DEST_S3_REGION (default: eu-central-1)');
    process.exit(1);
  }
}

/**
 * Initialize the source (Yandex Object Storage) S3 client
 */
function getSourceClient() {
  return new S3Client({
    region: config.sourceRegion,
    endpoint: config.sourceEndpoint,
    credentials: {
      accessKeyId: config.sourceAccessKey,
      secretAccessKey: config.sourceSecretKey,
    },
  });
}

/**
 * Initialize the destination (AWS S3) client.
 * When DEST_S3_ENDPOINT is unset the client targets plain AWS S3.
 */
function getDestClient() {
  return new S3Client({
    region: config.destRegion,
    ...(config.destEndpoint ? { endpoint: config.destEndpoint } : {}),
    credentials: {
      accessKeyId: config.destAccessKey,
      secretAccessKey: config.destSecretKey,
    },
  });
}

/**
 * List all objects in the source bucket (handles pagination)
 */
async function listSourceObjects(sourceClient) {
  const objects = [];
  let continuationToken;

  do {
    const response = await sourceClient.send(new ListObjectsV2Command({
      Bucket: config.sourceBucket,
      ContinuationToken: continuationToken,
    }));

    (response.Contents || []).forEach(obj => objects.push(obj));
    continuationToken = response.IsTruncated ? response.NextContinuationToken : undefined;
  } while (continuationToken);

  return objects;
}

/**
 * Check whether an ETag is a plain MD5 (multipart-uploaded objects have a
 * "-<parts>" suffix and cannot be compared across providers).
 */
function isComparableEtag(etag) {
  return typeof etag === 'string' && !etag.replace(/"/g, '').includes('-');
}

/**
 * Determine whether the destination already has an identical object.
 * Compares size always, and ETag when both sides expose a plain MD5 ETag.
 */
async function existsInDestination(destClient, sourceObject) {
  let head;
  try {
    head = await destClient.send(new HeadObjectCommand({
      Bucket: config.destBucket,
      Key: sourceObject.Key,
    }));
  } catch (error) {
    if (error.name === 'NotFound' || error.$metadata?.httpStatusCode === 404) {
      return false;
    }
    throw error;
  }

  if (head.ContentLength !== sourceObject.Size) {
    return false; // size mismatch -> re-copy
  }

  if (isComparableEtag(sourceObject.ETag) && isComparableEtag(head.ETag)) {
    return sourceObject.ETag.replace(/"/g, '') === head.ETag.replace(/"/g, '');
  }

  // ETags not comparable (multipart upload) -> trust the size match
  return true;
}

/**
 * Convert a readable stream to a buffer
 */
async function streamToBuffer(readableStream) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    readableStream.on('data', (data) => {
      chunks.push(data instanceof Buffer ? data : Buffer.from(data));
    });
    readableStream.on('end', () => {
      resolve(Buffer.concat(chunks));
    });
    readableStream.on('error', reject);
  });
}

/**
 * Copy a single object from Yandex Object Storage to AWS S3
 */
async function migrateObject(sourceClient, destClient, sourceObject) {
  const key = sourceObject.Key;

  try {
    // Idempotency: skip when an identical object already exists
    const exists = await existsInDestination(destClient, sourceObject);
    if (exists) {
      console.log(`  ⏭️  Skipped (already exists): ${key}`);
      stats.skipped++;
      return;
    }

    if (DRY_RUN) {
      console.log(`  🔍 Would migrate: ${key} (${formatBytes(sourceObject.Size)})`);
      stats.migrated++;
      return;
    }

    // Download from Yandex Object Storage
    const getResponse = await sourceClient.send(new GetObjectCommand({
      Bucket: config.sourceBucket,
      Key: key,
    }));
    const body = await streamToBuffer(getResponse.Body);

    // Upload to AWS S3 with the exact same key
    await destClient.send(new PutObjectCommand({
      Bucket: config.destBucket,
      Key: key,
      Body: body,
      ContentType: getResponse.ContentType || 'application/octet-stream',
      ContentLength: body.length,
    }));

    console.log(`  ✅ Migrated: ${key} (${formatBytes(body.length)})`);
    stats.migrated++;

  } catch (error) {
    console.error(`  ❌ Failed: ${key} - ${error.message}`);
    stats.failed++;
    stats.errors.push({ key, error: error.message });
  }
}

/**
 * Format bytes to human-readable format
 */
function formatBytes(bytes) {
  if (!bytes) return '0 Bytes';
  const k = 1024;
  const sizes = ['Bytes', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
}

/**
 * Main migration function
 */
async function migrate() {
  console.log(`🚀 Starting Yandex Object Storage to AWS S3 migration${DRY_RUN ? ' (DRY RUN)' : ''}...\n`);
  console.log('Configuration:');
  console.log(`  Source Bucket:   ${config.sourceBucket}`);
  console.log(`  Source Endpoint: ${config.sourceEndpoint}`);
  console.log(`  Source Region:   ${config.sourceRegion}`);
  console.log(`  Dest Bucket:     ${config.destBucket}`);
  console.log(`  Dest Endpoint:   ${config.destEndpoint || '(AWS S3)'}`);
  console.log(`  Dest Region:     ${config.destRegion}\n`);

  const startTime = Date.now();

  try {
    const sourceClient = getSourceClient();
    const destClient = getDestClient();

    console.log('📋 Listing objects in the source bucket...\n');
    const objects = await listSourceObjects(sourceClient);

    stats.total = objects.length;
    console.log(`Found ${stats.total} objects to process\n`);

    if (stats.total === 0) {
      console.log('⚠️  No objects found in the source bucket');
      return;
    }

    console.log('🔄 Starting migration...\n');

    for (let i = 0; i < objects.length; i++) {
      console.log(`[${i + 1}/${stats.total}]`);
      await migrateObject(sourceClient, destClient, objects[i]);
    }

  } catch (error) {
    console.error(`\n❌ Migration failed: ${error.message}`);
    console.error(error.stack);
    process.exit(1);
  }

  // Print summary
  const duration = ((Date.now() - startTime) / 1000).toFixed(2);

  console.log('\n' + '='.repeat(60));
  console.log(`📊 MIGRATION SUMMARY${DRY_RUN ? ' (DRY RUN)' : ''}`);
  console.log('='.repeat(60));
  console.log(`Total objects:    ${stats.total}`);
  console.log(`${DRY_RUN ? '🔍 Would migrate:' : '✅ Migrated:     '} ${stats.migrated}`);
  console.log(`⏭️  Skipped:       ${stats.skipped}`);
  console.log(`❌ Failed:        ${stats.failed}`);
  console.log(`⏱️  Duration:      ${duration}s`);
  console.log('='.repeat(60));

  if (stats.errors.length > 0) {
    console.log('\n❌ Errors encountered:');
    stats.errors.forEach((err, idx) => {
      console.log(`  ${idx + 1}. ${err.key}: ${err.error}`);
    });
  }

  if (stats.failed > 0) {
    console.log('\n⚠️  Migration completed with errors');
    process.exit(1);
  } else {
    console.log(`\n✅ Migration ${DRY_RUN ? 'dry run' : ''} completed successfully!`);
  }
}

// Run the migration
validateConfig();
migrate().catch(error => {
  console.error('Unhandled error:', error);
  process.exit(1);
});
