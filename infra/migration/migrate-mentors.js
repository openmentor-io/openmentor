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
 *   - enum-like fields are mapped to the new data model (price RUB -> USD in
 *     $5 steps within the D87 grammar — Free | Negotiable | $1..$1000, DB-enforced;
 *     buckets per DECISIONS D3, tags RU -> EN onto the seeded tag set,
 *     experience passes through)
 *   - identity fields (email, calendar_url, privacy, sort_order,
 *     created_at) are carried over unchanged
 *   - the mentor's completed-session history is carried as a single number
 *     in mentors.legacy_sessions_count (DECISIONS D28): the count of 'done'
 *     client_requests on getmentor.dev. No request rows are copied — the
 *     API adds this number to the mentor's OpenMentor session count
 *   - a NEW legacy_id is taken from the target's mentors_legacy_id_seq and
 *     the slug keeps its text part with the id part replaced
 *     (ivan-petrov-42 -> ivan-petrov-<new id>)
 *   - the migrated profile is approved but INACTIVE (hidden from the
 *     catalog until the mentor flips visibility themselves)
 *   - profile photos are copied from Yandex Object Storage to the S3
 *     bucket under the NEW slug prefix
 *   - the worker's /jobs/profile-migrated endpoint is triggered to email
 *     the mentor
 *   - the getmentor.dev slug -> openmentor.io slug mapping is written back
 *     into the SOURCE database's openmentor_profiles table (DECISIONS D36),
 *     which getmentor.dev reads to render a cross-link from the old profile
 *     to the new one. This is the ONLY write this script makes to the
 *     getmentor.dev database, it is best-effort, and it never fails a
 *     migration whose real work (insert + images + email) already succeeded
 *
 * Idempotency: each migrated row stores `getmentor:<old legacy_id>` in the
 * (otherwise unused, never exposed) mentors.airtable_id column, which has a
 * UNIQUE constraint. Re-runs skip mentors that carry the marker, and also
 * skip when a mentor with the same email already exists (e.g. they
 * registered on openmentor.io themselves). `--resume` re-runs the image
 * copy + email steps for already-migrated mentors, refreshes
 * legacy_sessions_count (getmentor.dev stays live, so the count can grow)
 * and re-records the cross-link; no other column of an existing row is
 * touched.
 *
 * Usage (normally via ./migrate-mentors.sh, which opens the DB tunnel):
 *   node --env-file=.env migrate-mentors.js --slug ivan-petrov-42 [--slug ...]
 *   node --env-file=.env migrate-mentors.js --csv slugs.csv --dry-run
 *   node --env-file=.env migrate-mentors.js --from-intents
 *   node --env-file=.env migrate-mentors.js --sessions-only [--dry-run]
 *   node --env-file=.env migrate-mentors.js --backfill-links [--dry-run]
 *
 * Flags:
 *   --slug <slug>       mentor to migrate (repeatable)
 *   --csv <file>        bulk input: one slug per line, or a CSV whose first
 *                       column (header "slug" optional) holds the slugs
 *   --from-intents      also process every pending row of the target's
 *                       migration_intents table (filled by the public
 *                       /migrate page) and write each outcome back to it
 *   --dry-run           read + map + report only; no translation, no writes,
 *                       no image copy, no email
 *   --translate-dry-run with --dry-run: also run the Claude translation so
 *                       the full mapped record can be reviewed
 *   --sessions-only     refresh legacy_sessions_count on mentors that are
 *                       ALREADY migrated and change nothing else — no
 *                       profile insert, no translation, no image copy, no
 *                       email, and so none of those credentials are needed.
 *                       The worklist is read from the target's migration
 *                       markers, so this mode cannot create a profile; pass
 *                       --slug/--csv (old or new slugs) to narrow it, or
 *                       nothing to refresh every migrated mentor. Combine
 *                       with --dry-run to preview the changes
 *   --backfill-links    write the getmentor.dev -> openmentor.io slug mapping
 *                       for mentors migrated BEFORE this feature existed, and
 *                       change nothing else. Like --sessions-only the worklist
 *                       comes from the target's migration markers, so it can
 *                       only ever touch mentors this script created; narrow it
 *                       with --slug/--csv or pass nothing for all of them.
 *                       Combine with --dry-run to preview
 *   --resume            for already-migrated mentors, re-run image copy +
 *                       email and refresh legacy_sessions_count instead of
 *                       skipping
 *   --skip-images       don't copy profile photos
 *   --skip-email        don't trigger the profile-migrated email
 *   --skip-translation  keep the original (Russian) text verbatim
 *
 * See migration/README.md for the environment contract.
 */

const crypto = require('crypto');
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
// Maps getmentor.dev tag labels onto the openmentor taxonomy (D30). Covers
// both the Russian labels and the legacy English ones — the latter used to
// pass through unchanged, which silently dropped them once D30 renamed the
// targets (insertMentor looks tags up by exact name and drops what it can't
// find). Keep this in sync with api/migrations/000009_modernise_tags.up.sql.
const TAG_MAP = {
  // Russian labels
  Сети: 'Cloud & Infrastructure',
  Карьера: 'Career Growth',
  Собеседования: 'Interview Prep',
  Аналитика: 'Data & Analytics',
  Безопасность: 'Security',
  // Legacy English labels renamed or merged by D30
  'UX/UI/Design': 'UX/UI Design',
  iOS: 'Mobile',
  Android: 'Mobile',
  QA: 'QA & Test Automation',
  'Data Science/ML': 'Machine Learning',
  Cloud: 'Cloud & Infrastructure',
  Networking: 'Cloud & Infrastructure',
  'Team Lead/Management': 'Engineering Management',
  DevRel: 'Developer Relations',
  HR: 'People & HR',
  Agile: 'Project Management',
  'Content/Copy': 'Technical Writing',
  Marketing: 'Marketing & Growth',
  Entrepreneurship: 'Entrepreneurship & Startups',
  Career: 'Career Growth',
  'Interview prep': 'Interview Prep',
  Analytics: 'Data & Analytics',
};
const DROPPED_TAGS = new Set(['Эксперт Авито', 'Сообщество Онтико']);
// Retired by D30 with no equivalent (a task, not an expertise).
const RETIRED_TAGS = new Set(['Code Review']);

const KNOWN_EXPERIENCE = new Set(['2-5', '5-10', '10+']);

const MIGRATION_MARKER_PREFIX = 'getmentor:';

const stats = { total: 0, migrated: 0, skipped: 0, resumed: 0, failed: 0 };
const reportRows = [];

function parseArgs(argv) {
  const parsed = {
    slugs: [],
    csv: '',
    fromIntents: false,
    dryRun: false,
    translateDryRun: false,
    sessionsOnly: false,
    backfillLinks: false,
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
      case '--from-intents':
        parsed.fromIntents = true;
        break;
      case '--dry-run':
        parsed.dryRun = true;
        break;
      case '--translate-dry-run':
        parsed.translateDryRun = true;
        break;
      case '--sessions-only':
        parsed.sessionsOnly = true;
        break;
      case '--backfill-links':
        parsed.backfillLinks = true;
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
  return [...new Set(slugs)];
}

function validateConfig() {
  const problems = [];
  if (!config.sourceDatabaseUrl) problems.push('SOURCE_DATABASE_URL is required (getmentor.dev production DSN)');
  if (!config.targetDatabaseUrl) problems.push('TARGET_DATABASE_URL is required (use ./migrate-mentors.sh, which sets it via the DB tunnel)');
  // --sessions-only touches one integer column and --backfill-links one
  // mapping row: no translation, no images, no email, so none of those
  // credentials are needed for either.
  if (args.sessionsOnly || args.backfillLinks) {
    if (problems.length > 0) {
      console.error('Configuration errors:');
      problems.forEach((p) => console.error(`  - ${p}`));
      process.exit(1);
    }
    return;
  }
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
            COALESCE(array_agg(t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags,
            -- completed sessions on getmentor.dev, carried over as a single
            -- number (DECISIONS D28) — no client_requests rows are copied
            COALESCE((SELECT COUNT(*)
                        FROM client_requests cr
                       WHERE cr.mentor_id = m.id
                         AND cr.status = 'done'), 0) AS done_sessions
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

// The bounds of the canonical grammar the target column enforces
// (mentors_price_chk, api/migrations/000014): 'Free', 'Negotiable', or '$N'
// for a whole PRICE_MIN..PRICE_MAX. Every return from mapPrice must be one of
// those three shapes — anything else fails the INSERT and aborts the import
// mid-run. This function used to pass '$30 / hour' and bare '50' through
// verbatim, which the column accepted when it was free text and refuses now.
const PRICE_MIN = 1;
const PRICE_MAX = 1000;

function mapPrice(price, notes) {
  const raw = price.trim();
  if (raw === '' || /бесплатно/i.test(raw) || /^free$/i.test(raw)) {
    if (raw === '') notes.push('price: empty -> Free');
    return 'Free';
  }
  if (/договор/i.test(raw) || /negotiable/i.test(raw)) return 'Negotiable';

  // An out-of-range amount becomes Negotiable + a note, NOT a clamp: rewriting
  // 150000 руб into the $1000 cap would silently change what the mentor
  // charges by a large factor. Negotiable is this function's established
  // "could not map" placeholder — the profile arrives hidden (status
  // 'inactive', D22) and the mentor reviews it before going live, so the note
  // is what surfaces the case to a human instead of the import crashing.
  const canonical = (usd, note) => {
    if (usd < PRICE_MIN || usd > PRICE_MAX) {
      notes.push(`price: "${raw}" -> $${usd} is outside $${PRICE_MIN}..$${PRICE_MAX} -> Negotiable (mentor to set on review)`);
      return 'Negotiable';
    }
    if (note) notes.push(note);
    return `$${usd}`;
  };

  const match = raw.replace(/\s+/g, '').match(/^(\d+)(?:руб|р|₽)/i);
  if (match) {
    const rub = Number(match[1]);
    const usd = Math.max(5, Math.round(rub / config.rubToUsdRate / 5) * 5);
    return canonical(usd, `price: "${raw}" -> "$${usd}" (rate ${config.rubToUsdRate} RUB/USD)`);
  }

  // Looks like a USD amount ('$30 / hour', '50', '$50.00'): take the leading
  // whole number and respell it canonically rather than passing raw through.
  const usdMatch = raw.match(/^\$?\s*(\d+)/);
  if (usdMatch) {
    const usd = Number(usdMatch[1]);
    return canonical(usd, `$${usd}` === raw ? null : `price: "${raw}" -> "$${usd}" (canonical grammar, D87)`);
  }

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
    if (RETIRED_TAGS.has(tag)) {
      notes.push(`tag dropped (retired in D30, no equivalent): ${tag}`);
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

// Matches api repository.slugAdvisoryLockNamespace (ASCII "slug"). Taking the
// same transaction-scoped advisory lock serializes this direct writer with the
// Go registration/rename paths so a generated slug can't collide with a
// retired redirect (which would make an old shared link resolve to this new
// mentor).
const SLUG_ADVISORY_LOCK_NS = 0x736c7567;

// claimSlug locks the slug and reports whether it is already an active
// redirect in mentor_slug_history. Call inside the mentor's transaction.
async function claimSlug(target, slug) {
  // Migration may run from this release checkout BEFORE the slug-history
  // migration deploys that table (an explicit state in the cutover sequence).
  // No redirects can exist yet, so the slug is claimable — and querying the
  // missing table would roll back the whole insert.
  const { rows: chk } = await target.query(
    "SELECT to_regclass('mentor_slug_history') IS NOT NULL AS has_history"
  );
  if (!chk[0].has_history) return false;
  await target.query('SELECT pg_advisory_xact_lock($1, hashtext($2))', [SLUG_ADVISORY_LOCK_NS, slug]);
  const { rows } = await target.query(
    'SELECT EXISTS (SELECT 1 FROM mentor_slug_history WHERE slug = $1) AS taken',
    [slug]
  );
  return rows[0].taken;
}

// Refresh the carried-over session count on an already-migrated row (D28).
// getmentor.dev stays live, so a --resume run may find sessions completed
// there after the profile was migrated.
async function refreshLegacySessions(target, mentorId, doneSessions, notes) {
  const { rowCount } = await target.query(
    `UPDATE mentors SET legacy_sessions_count = $1
      WHERE id = $2 AND legacy_sessions_count <> $1`,
    [doneSessions, mentorId]
  );
  if (rowCount > 0) {
    notes.push(`legacy_sessions_count refreshed -> ${doneSessions}`);
  }
}

async function insertMentor(target, mentor, translated, mappedTags, marker, notes) {
  await target.query('BEGIN');
  try {
    const seq = await target.query(`SELECT nextval('mentors_legacy_id_seq') AS id`);
    const newLegacyId = Number(seq.rows[0].id);

    // The fresh legacy_id makes a redirect clash nearly impossible, but a
    // mentor could have squatted this exact name-<id> as a retired slug — so
    // verify under the shared lock and disambiguate rather than corrupt.
    const baseSlug = `${slugTextPart(mentor.slug)}-${newLegacyId}`;
    let newSlug = baseSlug;
    for (let attempt = 2; ; attempt++) {
      if (!(await claimSlug(target, newSlug))) break;
      if (attempt > 6) throw new Error(`could not derive a free slug for ${mentor.slug}`);
      newSlug = `${baseSlug}-${attempt}`;
    }

    const inserted = await target.query(
      `INSERT INTO mentors (
         airtable_id, legacy_id, slug, name, job_title, workplace, about,
         details, competencies, experience, price, status, email,
         preferred_contact, calendar_url, privacy, sort_order, created_at,
         legacy_sessions_count
       ) VALUES (
         $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'inactive', $12,
         $13, $14, $15, $16, $17, $18
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
        Number(mentor.done_sessions) || 0,
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

// putVerified uploads body and proves the bytes that landed are the bytes we
// read (C12).
//
// The copy is a read-into-memory then a write, and nothing checked that the two
// agreed: a truncated download or a corrupted transfer produced a profile photo
// that was silently wrong, keyed under the new mentor's UUID, with the original
// left behind on a bucket that is being decommissioned.
//
// Verification is server-side, not a second round trip. `ChecksumSHA256` makes S3
// hash what it received and reject the PUT with BadDigest if it disagrees, so a
// successful response IS the proof. The response echo is then compared as well,
// which catches a destination that ignored the header (an S3-compatible endpoint
// that does not implement additional checksums) rather than silently treating an
// unverified upload as verified.
async function putVerified(dest, bucket, key, body, contentType) {
  const digest = crypto.createHash('sha256').update(body).digest('base64');

  const response = await dest.send(
    new PutObjectCommand({
      Bucket: bucket,
      Key: key,
      Body: body,
      ContentType: contentType || 'application/octet-stream',
      ContentLength: body.length,
      ChecksumAlgorithm: 'SHA256',
      ChecksumSHA256: digest,
    })
  );

  if (response.ChecksumSHA256 !== digest) {
    throw new Error(
      `checksum mismatch after upload of ${key}: sent ${digest}, stored ${response.ChecksumSHA256 || '(none returned)'}`
    );
  }
}

// destKeyBase is the NEW mentor's UUID (mentors.id): openmentor images are
// keyed by the immutable UUID, not the slug (usernames are changeable).
async function copyImages(oldSlug, destKeyBase, notes) {
  const { s3Source: src, s3Dest: dest } = s3Clients();
  let copied = 0;
  for (const size of IMAGE_SIZES) {
    const sourceKey = `${oldSlug}/${size}`;
    const destKey = `${destKeyBase}/${size}`;
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
      await putVerified(dest, config.destS3.bucket, destKey, body, object.ContentType);
      copied++;
    } catch (error) {
      if (error.name === 'NoSuchKey' || error.$metadata?.httpStatusCode === 404) {
        notes.push(`image missing at source, skipped: ${sourceKey}`);
      } else {
        throw new Error(`image copy failed for ${sourceKey}: ${error.message}`);
      }
    }
  }
  if (copied > 0) notes.push(`images copied: ${copied}/${IMAGE_SIZES.length} (${oldSlug}/* -> ${destKeyBase}/*)`);
}

// ---------------------------------------------------------------------------
// Email trigger (worker /jobs/profile-migrated via ssh + docker exec)
// ---------------------------------------------------------------------------

async function triggerMigratedEmail(mentorId) {
  if (!/^[0-9a-f-]{36}$/i.test(mentorId)) {
    throw new Error(`unexpected mentor id format: ${mentorId}`);
  }
  // Build the remote command as ONE shell-quoted string. ssh flattens its
  // trailing argv into a single string for the remote shell, so passing
  // `-H`, `X-Worker-Token: <token>` as separate args does NOT survive: the
  // remote shell splits on the space after the colon, curl sends no token
  // header (→ 401) and treats the bare token as a hostname (→ "could not
  // resolve host"). Single-quote the header and URL so the remote shell keeps
  // each as one argument. Safe to single-quote: the token is hex and mentorId
  // is a validated UUID (no single quotes to escape).
  const remoteCmd =
    `docker exec openmentor-worker ` +
    `curl -fsS -m 15 -X POST ` +
    `-H 'X-Worker-Token: ${config.workerAuthToken}' ` +
    `'http://localhost:8090/jobs/profile-migrated?mentorId=${mentorId}'`;

  const sshArgs = ['-o', 'StrictHostKeyChecking=accept-new'];
  if (config.vmSshKeyFile) sshArgs.push('-i', config.vmSshKeyFile);
  sshArgs.push(`${config.vmSshUser}@${config.vmSshHost}`, remoteCmd);
  await execFileAsync('ssh', sshArgs, { timeout: 30000 });
}

// ---------------------------------------------------------------------------
// Migration intents (public /migrate page opt-ins in the target DB)
// ---------------------------------------------------------------------------

// Namespace for the run-level intents lock. Distinct from SLUG_ADVISORY_LOCK_NS
// (that one is shared with the Go slug writers on purpose; this one is ours).
const INTENTS_ADVISORY_LOCK_NS = 0x696e746e; // ASCII "intn"

// claimIntentsConsumer serializes the --from-intents consumer against another
// copy of itself (C12).
//
// The worklist is read, then processed, then written back. Two operators running
// --from-intents at the same time — the plausible case, since the runbook has a
// human doing this during a migration window — both read the same 'pending' rows
// and both migrate every mentor in them.
//
// This is a SESSION-scoped advisory lock over the whole run rather than a
// per-row `UPDATE ... RETURNING` claim, and that is the deliberate choice. A row
// claim would need a 'claimed' status (a migration, plus a new value in the CHECK
// constraint) and would leave rows stranded in it whenever a run died mid-pass:
// with `pg_try_advisory_lock` a dead run's lock disappears with its connection,
// so there is no stuck state to reason about and no recovery procedure to
// document. Granularity costs nothing here — the consumer is single-threaded, so
// per-row and per-run exclusion admit exactly the same amount of work.
//
// migrateMentor itself stays idempotent per slug; this stops the duplicate work,
// the duplicate Claude translation spend and the duplicate "your profile has
// been migrated" email, not a corrupt write.
async function claimIntentsConsumer(target) {
  const { rows } = await target.query('SELECT pg_try_advisory_lock($1, 0) AS acquired', [
    INTENTS_ADVISORY_LOCK_NS,
  ]);
  return rows[0].acquired;
}

async function fetchPendingIntentSlugs(target) {
  const { rows } = await target.query(
    `SELECT slug FROM migration_intents WHERE status = 'pending' ORDER BY created_at`
  );
  return rows.map((r) => r.slug);
}

// Record the mentor's outcome onto their migration_intents row (no-op when
// the slug wasn't scheduled through the /migrate page).
async function recordIntentOutcome(target, slug) {
  const row = reportRows[reportRows.length - 1];
  if (!row || row.slug !== slug || row.outcome.startsWith('would migrate')) return;

  let status;
  if (/^(migrated|resumed)/.test(row.outcome)) status = 'done';
  else if (row.outcome.startsWith('skipped')) status = 'skipped';
  else status = 'failed';

  try {
    await target.query(
      `UPDATE migration_intents
          SET status = $2, note = $3, processed_at = now()
        WHERE slug = $1`,
      [slug, status, row.outcome.slice(0, 500)]
    );
  } catch (error) {
    console.error(`  ⚠️  could not update migration_intents for ${slug}: ${error.message}`);
  }
}

// Write the getmentor.dev slug -> openmentor.io slug mapping into the SOURCE
// database so getmentor.dev can render a cross-link to the new profile (D36).
//
// This is the ONLY write this script makes to the getmentor.dev database, and
// it is deliberately best-effort: by the time it runs the mentor has already
// been inserted, their photos copied and their email sent. Letting a failure
// here bubble up would report a successful migration as failed and invite an
// operator to re-run it. So it swallows its own errors, warns, and records the
// outcome in `notes`; --backfill-links exists to repair anything it missed.
//
// Requires the SOURCE role to hold INSERT/UPDATE on openmentor_profiles, and
// getmentor-api migration 000004 to have been applied. See README.md.
async function recordCrossLink(source, getmentorSlug, openmentorSlug, notes) {
  if (args.dryRun) {
    notes.push(`cross-link: would map ${getmentorSlug} -> ${openmentorSlug} (dry run)`);
    return;
  }

  try {
    await source.query(
      `INSERT INTO openmentor_profiles (getmentor_slug, openmentor_slug)
            VALUES ($1, $2)
       ON CONFLICT (getmentor_slug) DO UPDATE
              SET openmentor_slug = EXCLUDED.openmentor_slug,
                  updated_at = now()`,
      [getmentorSlug, openmentorSlug]
    );
    notes.push(`cross-link: ${getmentorSlug} -> ${openmentorSlug}`);
  } catch (error) {
    notes.push(`cross-link NOT recorded (${error.message}) — re-run with --backfill-links`);
    console.error(`  ⚠️  could not record the cross-link for ${getmentorSlug}: ${error.message}`);
  }
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

  const doneSessions = Number(mentor.done_sessions) || 0;
  const marker = `${MIGRATION_MARKER_PREFIX}${mentor.legacy_id}`;
  const existing = await findExisting(target, marker, mentor.email);
  if (existing) {
    const reason = existing.by_marker
      ? `already migrated as ${existing.slug}`
      : `email already registered on openmentor.io (${existing.slug})`;
    if (args.resume && existing.by_marker && !args.dryRun) {
      console.log(`  🔁 ${reason} — resuming images + email`);
      await refreshLegacySessions(target, existing.id, doneSessions, notes);
      // Images are keyed by the mentor UUID, not the slug (D29).
      if (!args.skipImages) await copyImages(slug, existing.id, notes);
      if (!args.skipEmail) await triggerMigratedEmail(existing.id);
      // Re-recorded on every resume so a mapping that failed (or predates the
      // feature) heals without a separate backfill pass.
      await recordCrossLink(source, slug, existing.slug, notes);
      stats.resumed++;
      reportRows.push({ slug, outcome: `resumed (${existing.slug})`, notes });
      notes.forEach((n) => console.log(`     • ${n}`));
    } else {
      console.log(`  ⏭️  Skipped: ${reason}`);
      // The session history is not carried onto a profile this script did not
      // create (D21's reconciliation caveat) — surface it so the operator can
      // decide whether to set legacy_sessions_count by hand.
      if (!existing.by_marker && doneSessions > 0) {
        console.log(
          `     ⚠️  ${doneSessions} getmentor.dev session(s) NOT carried over to ${existing.slug}`
        );
      }
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
  notes.push(
    doneSessions > 0
      ? `sessions: ${doneSessions} done on getmentor.dev -> legacy_sessions_count (no client_requests copied)`
      : 'sessions: none completed on getmentor.dev'
  );

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

  // Images (keyed by the new mentor's UUID)
  if (!args.skipImages) {
    await copyImages(slug, mentorId, notes);
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

  // Last, so a failure here cannot shadow work that already succeeded.
  await recordCrossLink(source, slug, newSlug, notes);

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
    legacy_sessions: String(Number(mentor.done_sessions) || 0),
    created_at: mentor.created_at.toISOString(),
  };
  for (const [key, value] of Object.entries(rows)) {
    console.log(`     ${key.padEnd(18)} ${value}`);
  }
}

// ---------------------------------------------------------------------------
// --sessions-only: refresh legacy_sessions_count on already-migrated mentors
// ---------------------------------------------------------------------------

// Every already-migrated mentor, newest first. The worklist comes from the
// target's D22 markers, not from a slug list, so this mode can only ever
// touch rows this script created. An optional --slug/--csv list narrows it;
// entries may be the old getmentor.dev slug or the new openmentor.io one.
async function listMigratedMentors(target, slugFilter) {
  const { rows } = await target.query(
    `SELECT id, slug, airtable_id, legacy_sessions_count,
            split_part(airtable_id, ':', 2)::bigint AS source_legacy_id
       FROM mentors
      WHERE airtable_id LIKE $1
      ORDER BY legacy_id DESC`,
    [`${MIGRATION_MARKER_PREFIX}%`]
  );
  if (slugFilter.length === 0) return rows;

  const wanted = new Set(slugFilter);
  return rows.filter(
    (row) => wanted.has(row.slug) || wanted.has(`${slugTextPart(row.slug)}-${row.source_legacy_id}`)
  );
}

// Look the mentor up in the source by their ORIGINAL legacy_id rather than by
// slug: the marker is the stable link between the two databases, and a mentor
// may have renamed their getmentor.dev slug since the migration.
async function fetchSourceDoneSessions(source, sourceLegacyId) {
  const { rows } = await source.query(
    `SELECT COALESCE((SELECT COUNT(*)
                        FROM client_requests cr
                       WHERE cr.mentor_id = m.id
                         AND cr.status = 'done'), 0) AS done_sessions
       FROM mentors m
      WHERE m.legacy_id = $1`,
    [sourceLegacyId]
  );
  if (rows.length === 0) return null;
  return Number(rows[0].done_sessions) || 0;
}

async function refreshSessionCounts(source, target) {
  const slugFilter = loadSlugs();
  const mentors = await listMigratedMentors(target, slugFilter);

  stats.total = mentors.length;
  console.log(`   Migrated mentors to refresh: ${mentors.length}`);
  if (slugFilter.length > 0) {
    console.log(`   (narrowed by ${slugFilter.length} slug(s) from --slug/--csv)`);
  }
  if (mentors.length === 0) {
    console.log('   Nothing to do.');
    return;
  }

  for (const mentor of mentors) {
    const label = `${mentor.slug} (${mentor.airtable_id})`;
    try {
      const doneSessions = await fetchSourceDoneSessions(source, mentor.source_legacy_id);
      if (doneSessions === null) {
        stats.failed++;
        reportRows.push({ slug: mentor.slug, outcome: 'not found in source (marker legacy_id)', notes: [] });
        console.log(`  ❌ ${label}: no mentor with legacy_id ${mentor.source_legacy_id} in the source database`);
        continue;
      }

      if (doneSessions === mentor.legacy_sessions_count) {
        stats.skipped++;
        reportRows.push({ slug: mentor.slug, outcome: `unchanged (${doneSessions})`, notes: [] });
        console.log(`  ⏭️  ${label}: unchanged (${doneSessions})`);
        continue;
      }

      const change = `${mentor.legacy_sessions_count} -> ${doneSessions}`;
      if (args.dryRun) {
        stats.migrated++;
        reportRows.push({ slug: mentor.slug, outcome: `would update (${change})`, notes: [] });
        console.log(`  🔍 ${label}: would update ${change}`);
        continue;
      }

      await target.query(`UPDATE mentors SET legacy_sessions_count = $1 WHERE id = $2`, [
        doneSessions,
        mentor.id,
      ]);
      stats.migrated++;
      reportRows.push({ slug: mentor.slug, outcome: `updated (${change})`, notes: [] });
      console.log(`  ✅ ${label}: ${change}`);
    } catch (error) {
      stats.failed++;
      reportRows.push({ slug: mentor.slug, outcome: `error: ${error.message}`, notes: [] });
      console.error(`  ❌ ${label}: ${error.message}`);
    }
  }
}

// Look the mentor up in the source by their ORIGINAL legacy_id, for the same
// reason fetchSourceDoneSessions does: the marker is the stable link between
// the two databases, and a mentor may have renamed their getmentor.dev slug
// since migrating. Reconstructing the old slug from text would silently write
// a mapping keyed on a slug that no longer exists.
async function fetchSourceSlug(source, sourceLegacyId) {
  const { rows } = await source.query(`SELECT slug FROM mentors WHERE legacy_id = $1`, [
    sourceLegacyId,
  ]);
  return rows.length > 0 ? rows[0].slug : null;
}

// Backfill the cross-link mapping for mentors migrated before D36 existed.
// Worklist comes from the target's migration markers, so like --sessions-only
// this mode can only ever touch rows this script created.
async function backfillCrossLinks(source, target) {
  const slugFilter = loadSlugs();
  const mentors = await listMigratedMentors(target, slugFilter);

  stats.total = mentors.length;
  console.log(`   Migrated mentors to link: ${mentors.length}`);
  if (slugFilter.length > 0) {
    console.log(`   (narrowed by ${slugFilter.length} slug(s) from --slug/--csv)`);
  }
  if (mentors.length === 0) {
    console.log('   Nothing to do.');
    return;
  }

  for (const mentor of mentors) {
    const label = `${mentor.slug} (${mentor.airtable_id})`;
    try {
      const sourceSlug = await fetchSourceSlug(source, mentor.source_legacy_id);
      if (!sourceSlug) {
        // Reported rather than skipped quietly: it means the mentor was deleted
        // from getmentor.dev, so there is no profile left to cross-link from.
        stats.failed++;
        reportRows.push({ slug: mentor.slug, outcome: 'not found in source (marker legacy_id)', notes: [] });
        console.log(`  ❌ ${label}: no mentor with legacy_id ${mentor.source_legacy_id} in the source database`);
        continue;
      }

      const notes = [];
      if (args.dryRun) {
        stats.migrated++;
        reportRows.push({ slug: mentor.slug, outcome: `would link ${sourceSlug} -> ${mentor.slug}`, notes });
        console.log(`  🔍 ${label}: would link ${sourceSlug} -> ${mentor.slug}`);
        continue;
      }

      await recordCrossLink(source, sourceSlug, mentor.slug, notes);
      // recordCrossLink swallows its own errors, so read the note it left to
      // decide whether this row actually landed.
      const failed = notes.some((n) => n.startsWith('cross-link NOT recorded'));
      if (failed) {
        stats.failed++;
        reportRows.push({ slug: mentor.slug, outcome: `link failed (${sourceSlug})`, notes });
        continue;
      }
      stats.migrated++;
      reportRows.push({ slug: mentor.slug, outcome: `linked ${sourceSlug} -> ${mentor.slug}`, notes });
      console.log(`  ✅ ${label}: ${sourceSlug} -> ${mentor.slug}`);
    } catch (error) {
      stats.failed++;
      reportRows.push({ slug: mentor.slug, outcome: `error: ${error.message}`, notes: [] });
      console.error(`  ❌ ${label}: ${error.message}`);
    }
  }
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main() {
  validateConfig();

  if (args.sessionsOnly) {
    console.log(
      `🔢 getmentor.dev -> openmentor.io session-count refresh${args.dryRun ? ' (DRY RUN)' : ''}`
    );
    const source = await connectSource();
    const target = await connectTarget();
    try {
      await refreshSessionCounts(source, target);
    } finally {
      await source.end().catch(() => {});
      await target.end().catch(() => {});
    }
    printSummary('sessions');
    if (stats.failed > 0) process.exit(1);
    return;
  }

  if (args.backfillLinks) {
    console.log(
      `🔗 getmentor.dev -> openmentor.io cross-link backfill${args.dryRun ? ' (DRY RUN)' : ''}`
    );
    const source = await connectSource();
    const target = await connectTarget();
    try {
      await backfillCrossLinks(source, target);
    } finally {
      await source.end().catch(() => {});
      await target.end().catch(() => {});
    }
    printSummary('links');
    if (stats.failed > 0) process.exit(1);
    return;
  }

  let slugs = loadSlugs();
  if (slugs.length === 0 && !args.fromIntents) {
    fail('No slugs to migrate. Pass --slug <slug>, --csv <file> and/or --from-intents.');
  }

  console.log(`🚀 getmentor.dev -> openmentor.io mentor migration${args.dryRun ? ' (DRY RUN)' : ''}`);

  const source = await connectSource();
  const target = await connectTarget();

  try {
    if (args.fromIntents) {
      // Refuse rather than queue: a second operator wants to know that the run
      // they were about to start is already happening, not to wait for it.
      if (!(await claimIntentsConsumer(target))) {
        fail(
          'Another --from-intents run is already processing the pending intents ' +
            '(advisory lock held). Wait for it to finish, or narrow this run with --slug/--csv.'
        );
      }
      const intentSlugs = await fetchPendingIntentSlugs(target);
      console.log(`   Pending migration intents: ${intentSlugs.length}`);
      slugs = [...new Set([...slugs, ...intentSlugs])];
    }
    stats.total = slugs.length;
    console.log(`   Mentors to process: ${slugs.length}`);
    if (slugs.length === 0) {
      console.log('   Nothing to do.');
      return;
    }

    for (const slug of slugs) {
      try {
        await migrateMentor(source, target, slug);
      } catch (error) {
        stats.failed++;
        reportRows.push({ slug, outcome: `error: ${error.message}`, notes: [] });
        console.error(`  ❌ ${slug}: ${error.message}`);
      }
      if (!args.dryRun) {
        await recordIntentOutcome(target, slug);
      }
    }
  } finally {
    await source.end().catch(() => {});
    await target.end().catch(() => {});
  }

  printSummary('migration');

  if (stats.failed > 0) process.exit(1);
}

// stats.migrated counts the rows this run changed (or would change, in a dry
// run) in both modes; the wording differs because the units do.
function printSummary(mode) {
  const sessionsOnly = mode === 'sessions';
  const linksOnly = mode === 'links';
  const heading = sessionsOnly
    ? 'SESSION-COUNT REFRESH'
    : linksOnly
      ? 'CROSS-LINK BACKFILL'
      : 'MIGRATION';
  console.log('\n' + '='.repeat(60));
  console.log(`📊 ${heading} SUMMARY${args.dryRun ? ' (DRY RUN)' : ''}`);
  console.log('='.repeat(60));
  for (const row of reportRows) {
    console.log(`  ${row.slug}: ${row.outcome}`);
  }
  console.log('-'.repeat(60));

  if (sessionsOnly) {
    const verb = args.dryRun ? 'Would update' : 'Updated';
    console.log(
      `Total: ${stats.total}  ${verb}: ${stats.migrated}  Unchanged: ${stats.skipped}  Failed: ${stats.failed}`
    );
    return;
  }

  if (linksOnly) {
    const verb = args.dryRun ? 'Would link' : 'Linked';
    console.log(`Total: ${stats.total}  ${verb}: ${stats.migrated}  Failed: ${stats.failed}`);
    return;
  }

  const verb = args.dryRun ? 'Would migrate' : 'Migrated';
  console.log(
    `Total: ${stats.total}  ${verb}: ${stats.migrated}  Resumed: ${stats.resumed}  Skipped: ${stats.skipped}  Failed: ${stats.failed}`
  );
}

// Exported for mapprice.test.js (node --test), which pins every return shape
// to the grammar mentors_price_chk enforces. The require.main guard is what
// keeps `require()` from kicking off an actual import run.
module.exports = { mapPrice };

if (require.main === module) {
  main().catch((error) => {
    console.error('Unhandled error:', error);
    process.exit(1);
  });
}
