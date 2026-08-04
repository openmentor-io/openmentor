# Verification harness — reproducing the audit findings

This directory lets you **independently re-verify** every high-severity finding in
[`../2026-08-remediation-plan.md`](../2026-08-remediation-plan.md) without trusting the report.
Everything here was executed on 2026-08-03 against commit `9b63e73`.

Nothing in this directory is compiled or run by CI. The Go files carry a `.go.txt` extension
because they live outside the `api/` Go module and several of them **intentionally assert current
buggy behaviour** — they are evidence, not tests. See "Turning proofs into regression tests" below.

---

## 0. Prerequisites

| Tool | Version used | Notes |
|---|---|---|
| Docker | 29.4.0 | Compose v5.1.2 |
| Go | 1.25.5 | `api/go.mod` declares `go 1.25.0`; CI/Docker use 1.26 |
| Node | 26.5.1 | matches `web/package.json` `engines.node: 26.x` |
| Python 3 | any | only to generate image-bomb fixtures |

**Safety.** `infra/.env` in this repo is **development** configuration (`APP_ENV=development`,
`DOMAIN=localhost`, `DATABASE_URL` pointing at the compose-internal `postgres` host). Verify that
before use:

```bash
cd infra
grep -E '^(APP_ENV|DOMAIN)=' .env
grep -oE '@[a-zA-Z0-9._-]+:[0-9]+' .env | sort -u   # expect only @postgres:5432
```

Never run these procedures with `.env.production`. Do not print secret values into logs or issues.

---

## 1. Bring up an isolated database

```bash
cd infra
docker compose --env-file .env -f docker-compose.yml -f docker-compose.dev.yml up -d postgres
docker exec openmentor-postgres-dev pg_isready -U openmentor
```

Binds `127.0.0.1:5433` only. Credentials are the dev defaults (`openmentor` / `password`).
A convenience shell alias used throughout:

```bash
P() { docker exec openmentor-postgres-dev psql -U openmentor -d openmentor -tAc "$1"; }
P "SELECT version, dirty FROM schema_migrations;"    # expect: 9|f
```

If the database is empty, apply migrations first: `cd api && DATABASE_URL='postgres://openmentor:password@localhost:5433/openmentor?sslmode=disable' go run ./cmd/migrate`

---

## 2. Run the API from source

Running from source (rather than the Docker image) is what makes the crash tests possible — you
control exactly which environment variables are absent.

```bash
cd api
go build -o /tmp/om-api ./cmd/api

cat > /tmp/om-dev.env <<'EOF'
APP_ENV=development
LOG_LEVEL=warn
GIN_MODE=release
PORT=8099
DATABASE_URL=postgres://openmentor:password@localhost:5433/openmentor?sslmode=disable
JWT_SECRET=dev-only-jwt-secret-not-a-real-secret-000000000000
WORKER_AUTH_TOKEN=dev-worker-token
BASE_URL=http://localhost:3000
ALLOWED_CORS_ORIGINS=http://localhost:3000
TURNSTILE_SECRET_KEY=1x0000000000000000000000000000000AA
INTERNAL_MENTORS_API=dev-internal-token
MENTORS_API_LIST_AUTH_TOKEN=dev-list-token
GO_API_INTERNAL_TOKEN=dev-internal-token
METRICS_AUTH_TOKEN=dev-metrics-token
EOF

set -a; . /tmp/om-dev.env; set +a
/tmp/om-api > /tmp/om-api.log 2>&1 &
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8099/api/healthcheck   # expect 200
```

Notes learned the hard way:
- The health endpoint is **`/api/healthcheck`**, not `/health` or `/healthz`.
- `TURNSTILE_SECRET_KEY=1x0000000000000000000000000000000AA` is Cloudflare's public
  **always-passes** test secret, so any non-empty token satisfies the captcha. Requires outbound
  network access to Cloudflare.
- **S3 variables are deliberately omitted** above — that is the precondition for finding `P1`.

Clean up when done: `pkill -f /tmp/om-api; rm -f /tmp/om-api /tmp/om-dev.env /tmp/om-api.log`

---

## 3. Reproduce `P1` — nil S3 client kills the whole process

With the API running under the env above (no S3 credentials):

```bash
PNG=$(python3 -c "
import base64,zlib,struct
def chunk(t,d):
    c=t+d
    return struct.pack('>I',len(d))+c+struct.pack('>I',zlib.crc32(c)&0xffffffff)
png=(b'\x89PNG\r\n\x1a\n'
     + chunk(b'IHDR',struct.pack('>IIBBBBB',1,1,8,2,0,0,0))
     + chunk(b'IDAT',zlib.compress(b'\x00\xff\xff\xff'))
     + chunk(b'IEND',b''))
print(base64.b64encode(png).decode())")

TAG=$(docker exec openmentor-postgres-dev psql -U openmentor -d openmentor -tAc "SELECT name FROM tags LIMIT 1;")

cat > /tmp/reg.json <<JSON
{"name":"Audit Verify","email":"audit-verify-$(date +%s)@example.com","job":"Engineer",
"workplace":"Acme","experience":"5-10","price":"\$75","tags":["$TAG"],
"about":"About text here","description":"Desc","competencies":"Comp",
"profilePicture":{"image":"$PNG","fileName":"a.png","contentType":"image/png"},
"captchaToken":"XXXX.DUMMY.TOKEN.XXXX"}
JSON

pgrep -f /tmp/om-api                       # note the pid
curl -s -w '\nHTTP %{http_code}\n' -X POST http://localhost:8099/api/v1/register-mentor \
  -H 'Content-Type: application/json' --data-binary @/tmp/reg.json
sleep 2
pgrep -f /tmp/om-api || echo "PROCESS IS GONE"
grep -A8 'panic' /tmp/om-api.log
```

**Expected:** `HTTP 000` (connection dropped), the process gone, and a SIGSEGV traceback through
`s3storage.(*StorageClient).UploadImage(0x0, ...)` at `storage.go:97`, created by
`UploadImageAllSizesAsync` at `storage.go:244`.

Then confirm the row was committed anyway — this is the link to `P2`:

```bash
P "SELECT status, COALESCE(sort_order::text,'NULL'), email FROM mentors WHERE email LIKE 'audit-verify-%';"
# expect: draft | NULL | audit-verify-...
```

`proofs/nilclient_auditverify_test.go.txt` and `proofs/ginrecovery_auditverify_test.go.txt` prove the
same thing in-process, including that Gin's recovery middleware **cannot** catch it (a control route
with an inline panic is recovered and the process survives; the async path returns
`HTTP 200 {"success":true}` and then dies).

`proofs/s3validate_auditverify_test.go.txt` proves `config.Validate()` returns `nil` for production
with empty *and partial* S3 configuration, while correctly rejecting an empty `JWT_SECRET`.

---

## 4. Reproduce `P2` — a mentor is permanently locked out

Restart the API **with** dummy S3 credentials so the client is non-nil and the crash no longer
masks the login behaviour:

```bash
cat >> /tmp/om-dev.env <<'EOF'
S3_STORAGE_ACCESS_KEY=dummy
S3_STORAGE_SECRET_KEY=dummy
S3_STORAGE_BUCKET=dev-bucket
S3_STORAGE_ENDPOINT=https://s3.example.invalid
S3_STORAGE_REGION=us-east-1
EOF
set -a; . /tmp/om-dev.env; set +a
/tmp/om-api > /tmp/om-api2.log 2>&1 &
sleep 5

EMAIL=$(P "SELECT email FROM mentors WHERE sort_order IS NULL LIMIT 1;")
curl -s -w '\nHTTP %{http_code}\n' -X POST http://localhost:8099/api/v1/auth/mentor/request-login \
  -H 'Content-Type: application/json' -d "{\"email\":\"$EMAIL\",\"captchaToken\":\"XXXX.DUMMY.TOKEN.XXXX\"}"
tail -3 /tmp/om-api2.log
```

**Expected:** `HTTP 200 {"success":true,"message":"If an account exists for that email, a login link
has been sent."}` — a false success — while the log shows:

```
WARN services/mentor_auth_service.go:80  Login request for unknown email
     error="can't scan into dest[15] (col: sort_order): cannot scan NULL into *int"
```

Prove causality and that the proposed fix is sufficient:

```bash
P "UPDATE mentors SET sort_order=0 WHERE email='$EMAIL';"
curl -s -o /dev/null -w 'HTTP %{http_code}\n' -X POST http://localhost:8099/api/v1/auth/mentor/request-login \
  -H 'Content-Type: application/json' -d "{\"email\":\"$EMAIL\",\"captchaToken\":\"XXXX.DUMMY.TOKEN.XXXX\"}"
P "SELECT CASE WHEN login_token IS NULL THEN 'NO TOKEN' ELSE 'TOKEN ISSUED' END FROM mentors WHERE email='$EMAIL';"
# expect: TOKEN ISSUED
```

Clean up the test rows:

```bash
P "DELETE FROM mentor_tags WHERE mentor_id IN (SELECT id FROM mentors WHERE email LIKE 'audit-verify-%');
   DELETE FROM mentors WHERE email LIKE 'audit-verify-%';"
```

---

## 5. Reproduce `P3` — image decompression bomb

```bash
python3 docs/audit/verification/proofs/make_image_bombs.py    # writes /tmp/av/bomb_*.png
ls -l /tmp/av/bomb_*.png
```

Then copy `proofs/bomb_auditverify_test.go.txt` to `api/pkg/imageclass/bomb_test.go` and run:

```bash
cd api && go test ./pkg/imageclass/ -run TestAuditBomb -v 2>&1 | tail -40
/usr/bin/time -l go test ./pkg/imageclass/ -run TestAuditBombClassifyMemory 2>&1 | grep 'maximum resident'
```

**Expected** (measured 2026-08-03):

| Bomb | File size | HeapAlloc | Peak RSS | Amplification |
|---|---|---|---|---|
| 10000² | 11.9 KiB | 95.4 MiB | 110 MiB | 8,185× |
| 20000² | 47.5 KiB | 381.5 MiB | 397 MiB | 8,219× |
| 40000² | 189.9 KiB | 1,525.8 MiB | 1,543 MiB | 8,227× |

Also expected: all four validators return `nil` for every bomb, and `image.DecodeConfig` returns the
true dimensions in ~5.6 µs versus ~2.49 s for a full decode.

**Both photo endpoints must be checked**, not just registration:

```bash
grep -n 'classifyPhotoStyle' api/internal/services/*.go
# registration_service.go:146  (captcha checked first, at :89)
# profile_service.go:192       (session auth only — NO captcha)
grep -rn 'DecodeConfig' api/ | wc -l    # expect 0
```

Remember to delete `api/pkg/imageclass/bomb_test.go` afterwards, or convert it per the last section.

---

## 6. Reproduce `P6` — SES email template injection, and test the counter-proposal

Copy `proofs/injection_auditverify_test.go.txt` to `api/pkg/email/injection_test.go`:

```bash
cd api && go test ./pkg/email/ -run TestAudit -v 2>&1 | tail -80
```

Five sub-tests:

| Test | Proves |
|---|---|
| `TestAuditSESTemplateDataRaw` | the payload reaches `TemplateData` unescaped and renders as live markup |
| `TestAuditEscapeCounterProposal` | escaping props puts HTML entities into subject lines and plaintext bodies |
| `TestAuditTemplateInventory` | 19/19 templates have a text part; 6 interpolate a name into the subject; no block helpers; no `{{{raw}}}` |
| `TestAuditPairedProps` | the existing `x` / `x_text` convention, and where those values are produced |
| `TestAuditHTMLTemplateNeutralizesJavascriptURL` | `html/template` renders `javascript:`/`data:` as `#ZgotmplZ` while preserving benign URLs |

A quick inventory without Go:

```bash
cd api/pkg/email/templates/assets
python3 -c "
import json,glob,re
for f in sorted(glob.glob('*.json')):
    d=json.load(open(f))
    tv=sorted(set(re.findall(r'{{(\w+)}}', d.get('text',''))))
    sv=sorted(set(re.findall(r'{{(\w+)}}', d.get('subject',''))))
    print(f'{f:34} text_vars={len(tv):2} subject_vars={sv}')"
grep -l '{{#\|{{/\|{{{' *.json || echo 'no block helpers, no triple-brace'
```

---

## 7. Reproduce `P8`/`P9` — backup silence and the restart loop

```bash
cd infra/postgres-backup

# P9: the production template's exact defaults (bucket set, BACKUP_AWS_* empty),
# with app S3 keys present to test the fallback the template comment promises
docker run --rm \
  -e BACKUP_S3_BUCKET=openmentor-db-backups \
  -e BACKUP_AWS_ACCESS_KEY_ID= -e BACKUP_AWS_SECRET_ACCESS_KEY= \
  -e S3_STORAGE_ACCESS_KEY=appkey -e S3_STORAGE_SECRET_KEY=appsecret \
  -e POSTGRES_HOST=postgres -e POSTGRES_USER=openmentor -e POSTGRES_DB=openmentor \
  -e POSTGRES_PASSWORD=password \
  -v "$PWD/backup.sh:/tmp/backup.sh:ro" \
  --entrypoint /bin/sh postgres:16.14-alpine -c 'sh /tmp/backup.sh; echo "EXIT=$?"'
# expect: FATAL ... Refusing to start.  EXIT=1   (app keys are ignored: no fallback exists)

# P8: does a failed backup propagate?
docker run --rm \
  -e BACKUP_AWS_ACCESS_KEY_ID=bogus -e BACKUP_AWS_SECRET_ACCESS_KEY=bogus \
  -e POSTGRES_HOST=unreachable.invalid -e POSTGRES_USER=openmentor -e POSTGRES_DB=openmentor \
  -e POSTGRES_PASSWORD=password \
  -v "$PWD/backup.sh:/tmp/backup.sh:ro" \
  --entrypoint /bin/sh postgres:16.14-alpine -c 'sh /tmp/backup.sh once; echo "RUN_BACKUP_EXIT=$?"'
# expect: FAILURE ... error=pg_dump_failed   RUN_BACKUP_EXIT=1

grep -n 'run_backup || true' backup.sh                    # line 167 discards that exit code
grep -c 'last_success\|touch ' backup.sh                  # expect 0 — no freshness marker
awk '/^  postgres-backup:/,/^  [a-z]/' ../docker-compose.yml | grep -c healthcheck   # expect 0
grep -ri backup ../../grafana/ | wc -l                    # expect 0 — no alerting
grep -c 'loki.source' ../alloy/config.alloy               # 3 file sources, none for this sidecar
```

---

## 8. Reproduce `P11` — resend rate limiter is one global bucket

With the API running:

```bash
for i in 1 2 3 4; do
  code=$(curl -s -o /dev/null -w '%{http_code}' -X POST http://localhost:8099/api/v1/mentors/confirm/resend \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"distinct-user-$i@example.com\",\"captchaToken\":\"XXXX.DUMMY.TOKEN.XXXX\"}")
  echo "request $i (unique email) -> HTTP $code"
done
# expect: 400, 400, 429, 429
```

Four *different* addresses share one bucket (proving IP keying), and the payload is deliberately
malformed — the endpoint wants `token`, not `email` — proving the limiter runs **before** validation,
so invalid requests consume the global budget.

---

## 9. Reproduce `P13` — unbounded metric cardinality

Create `web/src/lib/__tests__/cardinality.tmp.test.ts`:

```ts
import { withObservability, normalizeRoute } from '../with-observability'
import { register } from 'prom-client'
import type { NextApiRequest, NextApiResponse } from 'next'

const mkRes = () => {
  const res: Record<string, unknown> = { statusCode: 200 }
  res.end = jest.fn(() => res); res.setHeader = jest.fn()
  return res as unknown as NextApiResponse
}

it('measures http_route cardinality', async () => {
  const handler = withObservability(async (_req, res) => { res.end() })
  for (let i = 0; i < 500; i++) {
    await handler({ url: `/api/mentor/requests/junk-${i}`, method: 'GET',
      headers: {}, socket: {} } as unknown as NextApiRequest, mkRes())
  }
  const m = await register.getMetricsAsJSON()
  const t = m.find((x) => x.name === 'http_server_request_total') as
    { values?: { labels: Record<string, string> }[] }
  const routes = new Set((t?.values ?? []).map((v) => v.labels.http_route))
  console.log('distinct http_route series:', routes.size)
  console.log('UUID control:', normalizeRoute('/api/mentor/requests/aaaaaaaa-0000-4000-8000-000000000000'))
  expect(routes.size).toBeGreaterThan(0)
})
```

```bash
cd web && npx jest src/lib/__tests__/cardinality.tmp.test.ts 2>&1 | grep -E 'distinct|control'
# expect: distinct http_route series: 500
#         UUID control: /api/mentor/requests/:id
```

The metric is `http_server_request_total` (see `web/src/lib/metrics.ts:96`) — not
`http_requests_total`. Mock requests need `headers: {}` and `socket: {}` because
`logger.ts:178` reads `req.headers['user-agent']`. Delete the file afterwards.

---

## 10. Reproduce `P5` — deploy workflow shell injection

No GitHub Actions run needed; the defect is textual interpolation into a shell line
(`.github/workflows/deploy.yml:244`):

```bash
MALICIOUS='x"; touch /tmp/PWNED; echo "'
LINE="bash -c 'echo SERVICE=\$1 TAG=\$2' -- both \"$MALICIOUS\""
echo "$LINE"
rm -f /tmp/PWNED; eval "$LINE" >/dev/null 2>&1
[ -f /tmp/PWNED ] && echo "INJECTION SUCCEEDED" || echo "safe"; rm -f /tmp/PWNED
```

Compare `deploy.yml:170-184` (which correctly passes the input via `env:`) against `:244` (which
interpolates `${{ steps.tag.outputs.TAG }}` straight into the `ssh ... "bash -s" --` argument list).

---

## 11. Reproduce `P4` — price corruption, and measure exposure

Data exposure (dev database, 14 mentors → 5 at risk):

```bash
P "SELECT COALESCE(price,'<null>')||' x'||count(*) FROM mentors GROUP BY price ORDER BY 1;"
P "SELECT count(*) FROM mentors WHERE price IS NOT NULL
     AND price NOT IN ('Free','\$50','\$100','\$150','\$200','Negotiable');"
P "SELECT count(*) FROM mentors WHERE price='Free' AND updated_at > created_at;"
```

The component-level proof (render `ProfileForm` with `price: '$75'`, assert the select reads `Free`,
submit an unrelated edit and capture `price: 'Free'` in the payload) is in the plan under `P4`, with
a control for `$100` and comparisons against the registration and admin forms.

---

## Turning proofs into regression tests

The four highest-value tests to land during implementation. Each currently **fails**, which is the
point — flip the assertion to the desired behaviour and fix the code until it passes.

| Test | Source | Assert |
|---|---|---|
| Image dimension gate | `proofs/bomb_auditverify_test.go.txt` | a 40000×40000 PNG is rejected by validation before `image.Decode`; generate the fixture in-process rather than reading `/tmp` |
| Email escaping | `proofs/injection_auditverify_test.go.txt` | no user-controlled prop yields raw `<` in the rendered HTML; subject and text stay entity-free |
| Async upload safety | `proofs/nilclient_auditverify_test.go.txt` | `UploadImageAllSizesAsync` on a nil client does not terminate the process |
| Production config | `proofs/s3validate_auditverify_test.go.txt` | `Validate()` rejects production with empty **or partial** S3 configuration |

Plus, from the plan: a NULL-`sort_order` mentor can request a login link (`P2`); an out-of-list price
round-trips through `ProfileForm` (`P4`); an upstream 400 reaches the client as 400 (`P7`); the
Turnstile widget resets after a failed submit (`P7`).

`proofs/s3validate_auditverify_test.go.txt` contains a `productionBaseline()` helper that is directly
reusable as a fixture for any production-config test.
