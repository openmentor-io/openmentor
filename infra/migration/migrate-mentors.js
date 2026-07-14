#!/usr/bin/env node

/**
 * getmentor.dev -> openmentor.io mentor migration
 *
 * Migrates mentor profiles by slug from the getmentor.dev production
 * Postgres into the openmentor.io production Postgres:
 *
 *   - profile text (about/details/competencies/job title/workplace) is
 *     translated RU -> EN with the Claude API; the mentor's name is
 *     romanized; HTML markup is preserved
 *   - enum-like fields are mapped to the new data model (price RUB -> USD
 *     buckets per DECISIONS D3, tags RU -> EN onto the seeded tag set,
 *     experience passes through)
 *   - identity fields (email, calendar_url, privacy, sort_order,
 *     created_at) are carried over unchanged
 *   - a NEW legacy_id is taken from the target's mentors_legacy_id_seq and
 *     the slug keeps its text part with the id part replaced
 *     (ivan-petrov-42 -> ivan-petrov-<new id>)
 *   - the migrated profile is approved but INACTIVE (hidden from the
 *     catalog until the mentor flips visibility themselves)
 *   - profile photos are copied from Yandex Object Storage to the S3
 *     bucket under the NEW slug prefix
 *   - the worker's /jobs/profile-migrated endpoint is triggered to email
 *     the mentor
 *
 * Idempotency: each migrated row stores `getmentor:<old legacy_id>` in the
 * (otherwise unused, never exposed) mentors.airtable_id column, which has a
 * UNIQUE constraint. Re-runs skip mentors that carry the marker, and also
 * skip when a mentor with the same email already exists (e.g. they
 * registered on openmentor.io themselves). `--resume` re-runs the image
 * copy + email steps for already-migrated mentors without touching the row.
 *
 * Usage (normally via ./migrate-mentors.sh, which opens the DB tunnel):
 *   node --env-file=.env migrate-mentors.js --slug ivan-petrov-42 [--slug ...]
 *   node --env-file=.env migrate-mentors.js --csv slugs.csv --dry-run
 *
 * Flags:
 *   --slug <slug>       mentor to migrate (repeatable)
 *   --csv <file>        bulk input: one slug per line, or a CSV whose first
 *                       column (header "slug" optional) holds the slugs
 *   --dry-run           read + map + report only; no translation, no writes,
 *                       no image copy, no email
 *   --translate-dry-run with --dry-run: also run the Claude translation so
 *                       the full mapped record can be reviewed
 *   --resume            for already-migrated mentors, re-run image copy +
 *                       email instead of skipping
 *   --skip-images       don't copy profile photos
 *   --skip-email        don't trigger the profile-migrated email
 *   --skip-translation  keep the original (Russian) text verbatim
 *
 * See migration/README.md for the environment contract.
 */

const fs = require('fs');
const { execFile } = require('child_process');
const { promisify } = require('util');
const { Client } = require('pg');
const {
  S3Client,
  HeadObjectCommand,
  GetObjectCommand,
  PutObjectCommand,
} = require('@aws-sdk/client-s3');
const Anthropic = require('@anthropic-ai/sdk');

const execFileAsync = promisify(execFile);

// ---------------------------------------------------------------------------
// CLI + configuration
// ---------------------------------------------------------------------------

const args = parseArgs(process.argv.slice(2));

const config = {
  sourceDatabaseUrl: process.env.SOURCE_DATABASE_URL,
  // Yandex Managed PostgreSQL cluster CA (public cert, committed alongside
  // this script) — override with SOURCE_CA_CERT_FILE if it ever rotates.
  sourceCaCertFile:
    process.env.SOURCE_CA_CERT_FILE ||
    (fs.existsSync(`${__dirname}/yandex-ca.pem`) ? `${__dirname}/yandex-ca.pem` : ''),
  targetDatabaseUrl: process.env.TARGET_DATABASE_URL,

  // Image copy (same variable names as yandex-to-s3-migration.js)
  sourceS3: {
    accessKey: process.env.SOURCE_S3_ACCESS_KEY,
    secretKey: process.env.SOURCE_S3_SECRET_KEY,
    bucket: process.env.SOURCE_S3_BUCKET,
    endpoint: process.env.SOURCE_S3_ENDPOINT || 'https://storage.yandexcloud.net',
    region: process.env.SOURCE_S3_REGION || 'ru-central1',
  },
  destS3: {
    accessKey: process.env.DEST_S3_ACCESS_KEY,
    secretKey: process.env.DEST_S3_SECRET_KEY,
    bucket: process.env.DEST_S3_BUCKET,
    endpoint: process.env.DEST_S3_ENDPOINT || '',
    region: process.env.DEST_S3_REGION || 'eu-central-1',
  },

  // Email trigger: ssh to the VM and curl the worker's internal port,
  // exactly like the manual cron triggers in the runbooks.
  vmSshHost: process.env.VM_SSH_HOST || '',
  vmSshUser: process.env.VM_SSH_USER || '',
  vmSshKeyFile: process.env.VM_SSH_KEY_FILE || '',
  workerAuthToken: process.env.WORKER_AUTH_TOKEN || '',

  rubToUsdRate: Number(process.env.RUB_TO_USD_RATE || 100),

  anthropicModel: process.env.ANTHROPIC_MODEL || 'claude-opus-4-8',
};

// Old Russian tags -> seeded openmentor tag names (api/migrations/000002).
// Sponsor tags have no counterpart on openmentor.io and are dropped.
const TAG_MAP = {
  Сети: 'Networking',
  Карьера: 'Career',
  Собеседования: 'Interview prep',
  Аналитика: 'Analytics',
  Безопасность: 'Security',
};
const DROPPED_TAGS = new Set(['Эксперт Авито', 'Сообщество Онтико']);

const KNOWN_EXPERIENCE = new Set(['2-5', '5-10', '10+']);

const MIGRATION_MARKER_PREFIX = 'getmentor:';

const stats = { total: 0, migrated: 0, skipped: 0, resumed: 0, failed: 0 };
const reportRows = [];

function parseArgs(argv) {
  const parsed = {
    slugs: [],
    csv: '',
    dryRun: false,
    translateDryRun: false,
    resume: false,
    skipImages: false,
    skipEmail: false,
    skipTranslation: false,
  };
  for (let i = 0; i < argv.length; i++) {
    switch (argv[i]) {
      case '--slug':
        parsed.slugs.push(requireValue(argv, ++i, '--slug'));
        break;
      case '--csv':
        parsed.csv = requireValue(argv, ++i, '--csv');
        break;
      case '--dry-run':
        parsed.dryRun = true;
        break;
      case '--translate-dry-run':
        parsed.translateDryRun = true;
        break;
      case '--resume':
        parsed.resume = true;
        break;
      case '--skip-images':
        parsed.skipImages = true;
        break;
      case '--skip-email':
        parsed.skipEmail = true;
        break;
      case '--skip-translation':
        parsed.skipTranslation = true;
        break;
      default:
        fail(`Unknown argument: ${argv[i]} (see the header of this file for usage)`);
    }
  }
  return parsed;
}

function requireValue(argv, index, flag) {
  if (index >= argv.length || argv[index].startsWith('--')) {
    fail(`${flag} needs a value`);
  }
  return argv[index];
}

function fail(message) {
  console.error(`❌ ${message}`);
  process.exit(1);
}

function loadSlugs() {
  const slugs = [...args.slugs];
  if (args.csv) {
    if (!fs.existsSync(args.csv)) fail(`CSV file not found: ${args.csv}`);
    const lines = fs.readFileSync(args.csv, 'utf8').split(/\r?\n/);
    for (const line of lines) {
      const value = line.split(',')[0].trim();
      if (!value || value.startsWith('#') || value.toLowerCase() === 'slug') continue;
      slugs.push(value);
    }
  }
  const unique = [...new Set(slugs)];
  if (unique.length === 0) {
    fail('No slugs to migrate. Pass --slug <slug> and/or --csv <file>.');
  }
  return unique;
}

function validateConfig() {
  const problems = [];
  if (!config.sourceDatabaseUrl) problems.push('SOURCE_DATABASE_URL is required (getmentor.dev production DSN)');
  if (!config.targetDatabaseUrl) problems.push('TARGET_DATABASE_URL is required (use ./migrate-mentors.sh, which sets it via the DB tunnel)');
  if (!args.dryRun && !args.skipTranslation && !process.env.ANTHROPIC_API_KEY) {
    problems.push('ANTHROPIC_API_KEY is required for translation (or pass --skip-translation)');
  }
  if (!args.dryRun && !args.skipImages) {
    for (const [name, value] of [
      ['SOURCE_S3_ACCESS_KEY', config.sourceS3.accessKey],
      ['SOURCE_S3_SECRET_KEY', config.sourceS3.secretKey],
      ['SOURCE_S3_BUCKET', config.sourceS3.bucket],
      ['DEST_S3_ACCESS_KEY', config.destS3.accessKey],
      ['DEST_S3_SECRET_KEY', config.destS3.secretKey],
      ['DEST_S3_BUCKET', config.destS3.bucket],
    ]) {
      if (!value) problems.push(`${name} is required for the image copy (or pass --skip-images)`);
    }
  }
  if (!args.dryRun && !args.skipEmail) {
    if (!config.vmSshHost || !config.vmSshUser) problems.push('VM_SSH_HOST/VM_SSH_USER are required for the email trigger (or pass --skip-email)');
    if (!config.workerAuthToken) problems.push('WORKER_AUTH_TOKEN is required for the email trigger (or pass --skip-email)');
  }
  if (!Number.isFinite(config.rubToUsdRate) || config.rubToUsdRate <= 0) {
    problems.push('RUB_TO_USD_RATE must be a positive number');
  }
  if (problems.length > 0) {
    console.error('Configuration errors:');
    problems.forEach((p) => console.error(`  - ${p}`));
    process.exit(1);
  }
}

// ---------------------------------------------------------------------------
// Database clients
// ---------------------------------------------------------------------------

function sourceSslConfig() {
  // Yandex Managed PostgreSQL requires TLS. With the cluster CA we do full
  // verification; without it we still encrypt but skip verification (the
  // DSN is operator-supplied, so this is an accepted trade-off — see README).
  if (config.sourceCaCertFile) {
    return { ca: fs.readFileSync(config.sourceCaCertFile, 'utf8'), rejectUnauthorized: true };
  }
  return { rejectUnauthorized: false };
}

async function connectSource() {
  // Strip sslmode/sslrootcert from the DSN: node-postgres derives its own
  // ssl settings from them (pointing at server-side cert paths from the
  // production env file) and they override the explicit `ssl` option below.
  const url = new URL(config.sourceDatabaseUrl);
  url.searchParams.delete('sslmode');
  url.searchParams.delete('sslrootcert');
  const client = new Client({ connectionString: url.toString(), ssl: sourceSslConfig() });
  await client.connect();
  return client;
}

async function connectTarget() {
  // The target is reached through the SSH tunnel (localhost) — no TLS.
  const client = new Client({ connectionString: config.targetDatabaseUrl });
  await client.connect();
  return client;
}

// ---------------------------------------------------------------------------
// Source read
// ---------------------------------------------------------------------------

async function fetchSourceMentor(source, slug) {
  const { rows } = await source.query(
    `SELECT m.id, m.legacy_id, m.slug, m.name,
            COALESCE(m.job_title, '')    AS job_title,
            COALESCE(m.workplace, '')    AS workplace,
            COALESCE(m.about, '')        AS about,
            COALESCE(m.details, '')      AS details,
            COALESCE(m.competencies, '') AS competencies,
            COALESCE(m.experience, '')   AS experience,
            COALESCE(m.price, '')        AS price,
            m.status,
            COALESCE(m.email::text, '')  AS email,
            COALESCE(m.telegram, '')     AS telegram,
            COALESCE(m.calendar_url, '') AS calendar_url,
            COALESCE(m.privacy, false)   AS privacy,
            m.sort_order,
            m.created_at,
            COALESCE(array_agg(t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags
       FROM mentors m
       LEFT JOIN mentor_tags mt ON mt.mentor_id = m.id
       LEFT JOIN tags t ON t.id = mt.tag_id
      WHERE m.slug = $1
      GROUP BY m.id`,
    [slug]
  );
  return rows[0] || null;
}

// ---------------------------------------------------------------------------
// Field mapping (enum-like fields -> new data model)
// ---------------------------------------------------------------------------

function mapPrice(price, notes) {
  const raw = price.trim();
  if (raw === '' || /бесплатно/i.test(raw) || /^free$/i.test(raw)) {
    if (raw === '') notes.push('price: empty -> Free');
    return 'Free';
  }
  if (/договор/i.test(raw) || /negotiable/i.test(raw)) return 'Negotiable';
  const match = raw.replace(/\s+/g, '').match(/^(\d+)(?:руб|р|₽)/i);
  if (match) {
    const rub = Number(match[1]);
    const usd = Math.max(5, Math.round(rub / config.rubToUsdRate / 5) * 5);
    notes.push(`price: "${raw}" -> "$${usd}" (rate ${config.rubToUsdRate} RUB/USD)`);
    return `$${usd}`;
  }
  if (/^\$?\d+/.test(raw)) return raw; // already looks like a USD amount
  notes.push(`price: could not parse "${raw}" -> Negotiable`);
  return 'Negotiable';
}

function mapExperience(experience, notes) {
  const raw = experience.trim();
  if (raw === '' || KNOWN_EXPERIENCE.has(raw)) return raw;
  notes.push(`experience: unexpected value "${raw}" kept verbatim`);
  return raw;
}

function mapTags(tags, notes) {
  const mapped = [];
  for (const tag of tags) {
    if (DROPPED_TAGS.has(tag)) {
      notes.push(`tag dropped (sponsor): ${tag}`);
      continue;
    }
    mapped.push(TAG_MAP[tag] || tag);
  }
  return [...new Set(mapped)];
}

function mapPreferredContact(telegram) {
  const handle = telegram.trim().replace(/^@/, '');
  return handle ? `Telegram: @${handle}` : null;
}

// "ivan-petrov-42" -> "ivan-petrov"; slugs without a numeric suffix keep
// their full text part.
function slugTextPart(slug) {
  return slug.replace(/-\d+$/, '');
}

// ---------------------------------------------------------------------------
// Translation (Claude API)
// ---------------------------------------------------------------------------

const TRANSLATION_FIELDS = ['name', 'job_title', 'workplace', 'about', 'details', 'competencies'];

const TRANSLATION_SCHEMA = {
  type: 'object',
  properties: Object.fromEntries(TRANSLATION_FIELDS.map((f) => [f, { type: 'string' }])),
  required: TRANSLATION_FIELDS,
  additionalProperties: false,
};

const TRANSLATION_SYSTEM = `You translate mentor profiles from a Russian IT-mentorship marketplace into English for its international sister platform.

You receive a JSON object with the fields name, job_title, workplace, about, details and competencies. Return the same JSON object with every field translated into natural, professional English, following these rules:

- "about" and "details" contain HTML. Preserve every tag and attribute exactly as-is; translate only the human-readable text between tags.
- "competencies" is a plain-text list of skills; keep its separators (commas/newlines) as they are.
- "name" is a person's name: romanize it (standard Latin transliteration), never translate it. E.g. "Иван Петров" -> "Ivan Petrov". If it is already in Latin script, return it unchanged.
- Keep company names, product names and technology terms as they are conventionally written in English (e.g. Яндекс -> Yandex).
- Text that is already in English must be returned unchanged.
- Empty fields stay empty.
- Do not add, remove, summarize or embellish content — translate faithfully.`;

let anthropicClient = null;

async function translateProfile(mentor, notes) {
  if (!anthropicClient) anthropicClient = new Anthropic();

  const payload = Object.fromEntries(TRANSLATION_FIELDS.map((f) => [f, mentor[f] || '']));

  const response = await anthropicClient.messages.create({
    model: config.anthropicModel,
    max_tokens: 16000,
    thinking: { type: 'adaptive' },
    system: TRANSLATION_SYSTEM,
    messages: [{ role: 'user', content: JSON.stringify(payload) }],
    output_config: { format: { type: 'json_schema', schema: TRANSLATION_SCHEMA } },
  });

  if (response.stop_reason === 'refusal') {
    throw new Error('translation request was refused by the model');
  }
  if (response.stop_reason === 'max_tokens') {
    throw new Error('translation output hit max_tokens — profile too large?');
  }

  const textBlock = response.content.find((block) => block.type === 'text');
  if (!textBlock) throw new Error('translation response contained no text block');
  const translated = JSON.parse(textBlock.text);

  const usage = response.usage || {};
  notes.push(`translated with ${config.anthropicModel} (${usage.input_tokens ?? '?'} in / ${usage.output_tokens ?? '?'} out tokens)`);
  return translated;
}

// ---------------------------------------------------------------------------
// Target write
// ---------------------------------------------------------------------------

async function findExisting(target, marker, email) {
  const { rows } = await target.query(
    `SELECT id, slug, airtable_id,
            (airtable_id = $1) AS by_marker
       FROM mentors
      WHERE airtable_id = $1 OR ($2 <> '' AND lower(email::text) = lower($2))
      ORDER BY (airtable_id = $1) DESC
      LIMIT 1`,
    [marker, email]
  );
  return rows[0] || null;
}

async function insertMentor(target, mentor, translated, mappedTags, marker, notes) {
  await target.query('BEGIN');
  try {
    const seq = await target.query(`SELECT nextval('mentors_legacy_id_seq') AS id`);
    const newLegacyId = Number(seq.rows[0].id);
    const newSlug = `${slugTextPart(mentor.slug)}-${newLegacyId}`;

    const inserted = await target.query(
      `INSERT INTO mentors (
         airtable_id, legacy_id, slug, name, job_title, workplace, about,
         details, competencies, experience, price, status, email,
         preferred_contact, calendar_url, privacy, sort_order, created_at
       ) VALUES (
         $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'inactive', $12,
         $13, $14, $15, $16, $17
       ) RETURNING id`,
      [
        marker,
        newLegacyId,
        newSlug,
        translated.name || mentor.name,
        translated.job_title,
        translated.workplace,
        translated.about,
        translated.details,
        translated.competencies,
        mentor.mappedExperience,
        mentor.mappedPrice,
        mentor.email,
        mapPreferredContact(mentor.telegram),
        mentor.calendar_url || null,
        mentor.privacy,
        mentor.sort_order,
        mentor.created_at,
      ]
    );
    const mentorId = inserted.rows[0].id;

    if (mappedTags.length > 0) {
      const { rows: tagRows } = await target.query(`SELECT id, name FROM tags WHERE name = ANY($1)`, [mappedTags]);
      const found = new Set(tagRows.map((r) => r.name));
      for (const name of mappedTags) {
        if (!found.has(name)) notes.push(`tag not in target tag set, dropped: ${name}`);
      }
      if (tagRows.length > 0) {
        await target.query(
          `INSERT INTO mentor_tags (mentor_id, tag_id)
           SELECT $1, id FROM tags WHERE name = ANY($2)
           ON CONFLICT DO NOTHING`,
          [mentorId, tagRows.map((r) => r.name)]
        );
      }
    }

    await target.query('COMMIT');
    return { mentorId, newLegacyId, newSlug };
  } catch (error) {
    await target.query('ROLLBACK');
    throw error;
  }
}

// ---------------------------------------------------------------------------
// Image copy (old slug prefix -> new slug prefix)
// ---------------------------------------------------------------------------

const IMAGE_SIZES = ['full', 'large', 'small'];

let s3Source = null;
let s3Dest = null;

function s3Clients() {
  if (!s3Source) {
    s3Source = new S3Client({
      region: config.sourceS3.region,
      endpoint: config.sourceS3.endpoint,
      credentials: { accessKeyId: config.sourceS3.accessKey, secretAccessKey: config.sourceS3.secretKey },
    });
    s3Dest = new S3Client({
      region: config.destS3.region,
      ...(config.destS3.endpoint ? { endpoint: config.destS3.endpoint } : {}),
      credentials: { accessKeyId: config.destS3.accessKey, secretAccessKey: config.destS3.secretKey },
    });
  }
  return { s3Source, s3Dest };
}

async function streamToBuffer(readableStream) {
  const chunks = [];
  for await (const chunk of readableStream) {
    chunks.push(chunk instanceof Buffer ? chunk : Buffer.from(chunk));
  }
  return Buffer.concat(chunks);
}

async function copyImages(oldSlug, newSlug, notes) {
  const { s3Source: src, s3Dest: dest } = s3Clients();
  let copied = 0;
  for (const size of IMAGE_SIZES) {
    const sourceKey = `${oldSlug}/${size}`;
    const destKey = `${newSlug}/${size}`;
    try {
      // Idempotency: skip when the destination object already exists.
      try {
        await dest.send(new HeadObjectCommand({ Bucket: config.destS3.bucket, Key: destKey }));
        notes.push(`image already present, skipped: ${destKey}`);
        continue;
      } catch (error) {
        if (error.name !== 'NotFound' && error.$metadata?.httpStatusCode !== 404) throw error;
      }

      const object = await src.send(new GetObjectCommand({ Bucket: config.sourceS3.bucket, Key: sourceKey }));
      const body = await streamToBuffer(object.Body);
      await dest.send(
        new PutObjectCommand({
          Bucket: config.destS3.bucket,
          Key: destKey,
          Body: body,
          ContentType: object.ContentType || 'application/octet-stream',
          ContentLength: body.length,
        })
      );
      copied++;
    } catch (error) {
      if (error.name === 'NoSuchKey' || error.$metadata?.httpStatusCode === 404) {
        notes.push(`image missing at source, skipped: ${sourceKey}`);
      } else {
        throw new Error(`image copy failed for ${sourceKey}: ${error.message}`);
      }
    }
  }
  if (copied > 0) notes.push(`images copied: ${copied}/${IMAGE_SIZES.length} (${oldSlug}/* -> ${newSlug}/*)`);
}

// ---------------------------------------------------------------------------
// Email trigger (worker /jobs/profile-migrated via ssh + docker exec)
// ---------------------------------------------------------------------------

async function triggerMigratedEmail(mentorId) {
  if (!/^[0-9a-f-]{36}$/i.test(mentorId)) {
    throw new Error(`unexpected mentor id format: ${mentorId}`);
  }
  const sshArgs = ['-o', 'StrictHostKeyChecking=no'];
  if (config.vmSshKeyFile) sshArgs.push('-i', config.vmSshKeyFile);
  sshArgs.push(
    `${config.vmSshUser}@${config.vmSshHost}`,
    'docker', 'exec', 'openmentor-worker',
    'curl', '-fsS', '-m', '15', '-X', 'POST',
    '-H', `X-Worker-Token: ${config.workerAuthToken}`,
    `http://localhost:8090/jobs/profile-migrated?mentorId=${mentorId}`
  );
  await execFileAsync('ssh', sshArgs, { timeout: 30000 });
}

// ---------------------------------------------------------------------------
// Per-mentor pipeline
// ---------------------------------------------------------------------------

async function migrateMentor(source, target, slug) {
  const notes = [];
  console.log(`\n── ${slug} ${'─'.repeat(Math.max(1, 60 - slug.length))}`);

  const mentor = await fetchSourceMentor(source, slug);
  if (!mentor) {
    stats.failed++;
    reportRows.push({ slug, outcome: 'not found in source', notes });
    console.log('  ❌ Not found in the getmentor.dev database');
    return;
  }
  if (!mentor.email) {
    stats.failed++;
    reportRows.push({ slug, outcome: 'no email (cannot log in or be notified)', notes });
    console.log('  ❌ Mentor has no email — magic-link login and notification are impossible; not migrating');
    return;
  }

  const marker = `${MIGRATION_MARKER_PREFIX}${mentor.legacy_id}`;
  const existing = await findExisting(target, marker, mentor.email);
  if (existing) {
    const reason = existing.by_marker
      ? `already migrated as ${existing.slug}`
      : `email already registered on openmentor.io (${existing.slug})`;
    if (args.resume && existing.by_marker && !args.dryRun) {
      console.log(`  🔁 ${reason} — resuming images + email`);
      if (!args.skipImages) await copyImages(slug, existing.slug, notes);
      if (!args.skipEmail) await triggerMigratedEmail(existing.id);
      stats.resumed++;
      reportRows.push({ slug, outcome: `resumed (${existing.slug})`, notes });
    } else {
      console.log(`  ⏭️  Skipped: ${reason}`);
      stats.skipped++;
      reportRows.push({ slug, outcome: `skipped: ${reason}`, notes });
    }
    return;
  }

  // Map enum-like fields
  mentor.mappedPrice = mapPrice(mentor.price, notes);
  mentor.mappedExperience = mapExperience(mentor.experience, notes);
  const mappedTags = mapTags(mentor.tags, notes);
  notes.push(`tags: [${mentor.tags.join(', ')}] -> [${mappedTags.join(', ')}]`);

  // Translate
  let translated = {
    name: mentor.name,
    job_title: mentor.job_title,
    workplace: mentor.workplace,
    about: mentor.about,
    details: mentor.details,
    competencies: mentor.competencies,
  };
  const shouldTranslate = !args.skipTranslation && (!args.dryRun || args.translateDryRun);
  if (shouldTranslate) {
    translated = await translateProfile(mentor, notes);
  } else if (!args.skipTranslation) {
    notes.push('translation skipped in dry run (use --translate-dry-run to include it)');
  } else {
    notes.push('translation skipped (--skip-translation): original text kept');
  }

  if (args.dryRun) {
    console.log('  🔍 Dry run — would insert:');
    printMappedRecord(mentor, translated, mappedTags, marker);
    notes.forEach((n) => console.log(`     • ${n}`));
    stats.migrated++;
    reportRows.push({ slug, outcome: 'would migrate (dry run)', notes });
    return;
  }

  // Insert
  const { mentorId, newLegacyId, newSlug } = await insertMentor(target, mentor, translated, mappedTags, marker, notes);
  console.log(`  ✅ Inserted: ${newSlug} (legacy_id ${mentor.legacy_id} -> ${newLegacyId}, status=inactive)`);

  // Images
  if (!args.skipImages) {
    await copyImages(slug, newSlug, notes);
  } else {
    notes.push('image copy skipped (--skip-images)');
  }

  // Email
  if (!args.skipEmail) {
    await triggerMigratedEmail(mentorId);
    console.log(`  📧 profile-migrated email triggered for ${mentor.email}`);
  } else {
    notes.push('email skipped (--skip-email)');
  }

  notes.forEach((n) => console.log(`     • ${n}`));
  stats.migrated++;
  reportRows.push({ slug, outcome: `migrated -> ${newSlug}`, notes });
}

function printMappedRecord(mentor, translated, mappedTags, marker) {
  const preview = (text) => {
    const oneLine = String(text || '').replace(/\s+/g, ' ').trim();
    return oneLine.length > 100 ? `${oneLine.slice(0, 100)}…` : oneLine;
  };
  const rows = {
    marker,
    slug: `${slugTextPart(mentor.slug)}-<new legacy_id>`,
    name: translated.name || mentor.name,
    job_title: preview(translated.job_title),
    workplace: preview(translated.workplace),
    about: preview(translated.about),
    details: preview(translated.details),
    competencies: preview(translated.competencies),
    experience: mentor.mappedExperience,
    price: `${mentor.price || '(empty)'} -> ${mentor.mappedPrice}`,
    status: `${mentor.status} -> inactive`,
    email: mentor.email,
    preferred_contact: mapPreferredContact(mentor.telegram) || '(none)',
    calendar_url: mentor.calendar_url || '(none)',
    tags: mappedTags.join(', ') || '(none)',
    created_at: mentor.created_at.toISOString(),
  };
  for (const [key, value] of Object.entries(rows)) {
    console.log(`     ${key.padEnd(18)} ${value}`);
  }
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main() {
  validateConfig();
  const slugs = loadSlugs();
  stats.total = slugs.length;

  console.log(`🚀 getmentor.dev -> openmentor.io mentor migration${args.dryRun ? ' (DRY RUN)' : ''}`);
  console.log(`   Mentors to process: ${slugs.length}`);

  const source = await connectSource();
  const target = await connectTarget();

  try {
    for (const slug of slugs) {
      try {
        await migrateMentor(source, target, slug);
      } catch (error) {
        stats.failed++;
        reportRows.push({ slug, outcome: `error: ${error.message}`, notes: [] });
        console.error(`  ❌ ${slug}: ${error.message}`);
      }
    }
  } finally {
    await source.end().catch(() => {});
    await target.end().catch(() => {});
  }

  console.log('\n' + '='.repeat(60));
  console.log(`📊 MIGRATION SUMMARY${args.dryRun ? ' (DRY RUN)' : ''}`);
  console.log('='.repeat(60));
  for (const row of reportRows) {
    console.log(`  ${row.slug}: ${row.outcome}`);
  }
  console.log('-'.repeat(60));
  console.log(`Total: ${stats.total}  ${args.dryRun ? 'Would migrate' : 'Migrated'}: ${stats.migrated}  Resumed: ${stats.resumed}  Skipped: ${stats.skipped}  Failed: ${stats.failed}`);

  if (stats.failed > 0) process.exit(1);
}

main().catch((error) => {
  console.error('Unhandled error:', error);
  process.exit(1);
});
