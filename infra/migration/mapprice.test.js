// node --test mapprice.test.js
//
// Pins mapPrice to the canonical grammar the target column enforces
// (mentors_price_chk, api/migrations/000014): every return must be 'Free',
// 'Negotiable' or '$N' for a whole 1..1000. The import writes straight into
// mentors.price, so a single non-canonical return aborts an import mid-run on
// a constraint violation — which is exactly what the pre-D87 passthrough
// ('$30 / hour' returned verbatim) and the uncapped RUB conversion would do.
//
// Deliberately dependency-free (node:test): this package has no test harness
// and the import tooling must stay runnable with a bare `node` on an operator
// machine.
const test = require('node:test');
const assert = require('node:assert/strict');

const { mapPrice: mapPriceRaw } = require('./price-mapping.js');

// The default rate from migrate-mentors.js config (RUB_TO_USD_RATE || 100),
// passed explicitly: the pure module deliberately has no default of its own.
const mapPrice = (raw, notes) => mapPriceRaw(raw, notes, 100);

const CANONICAL = /^(Free|Negotiable|\$([1-9][0-9]{0,2}|1000))$/;

const cases = [
  // [raw, expected]
  ['', 'Free'],
  ['Бесплатно', 'Free'],
  ['free', 'Free'],
  ['По договоренности', 'Negotiable'],
  ['negotiable', 'Negotiable'],

  ['3000 руб', '$30'],   // 3000 / default rate 100 -> $30
  ['500 руб', '$5'],     // floor: Math.max(5, ...)
  ['5000р', '$50'],
  // Separated RUB is still RUB: a digits-only match stopped at the comma and
  // the USD branch read '1,000 руб' as $1000 — a 100x rewrite (PR review).
  ['1,000 руб', '$10'],
  ['1,500 руб', '$15'],
  ['10,000 руб', '$100'],
  ['150,000 руб', 'Negotiable'], // over the cap after conversion, same as unseparated

  // The passthrough branch, now canonicalised instead of returned verbatim.
  ['$50', '$50'],
  ['50', '$50'],
  ['$30 / hour', '$30'],
  ['$50.00', '$50'],
  ['$ 75', '$75'],
  // The whole leading number token is read before deciding: a digits-only
  // prefix match stopped at the comma and imported $1,000 as $1 — a 1000x
  // rewrite that satisfies the constraint (PR review).
  ['$1,000', '$1000'],
  ['1,000', '$1000'],
  ['$1,500', 'Negotiable'], // well-formed, over the cap
  ['$1,00', 'Negotiable'], // malformed separator: no safe number to read
  ['$49.99', 'Negotiable'], // nonzero cents: truncating rewrites the price

  // Out of range -> Negotiable + note, never a clamp: rewriting what a mentor
  // charges is a decision for the mentor on review, not the importer.
  ['$1500', 'Negotiable'],
  ['$0', 'Negotiable'],
  ['0', 'Negotiable'],
  ['150000 руб', 'Negotiable'], // $1500 at the default 100 RUB/USD — over the cap

  ['ask me', 'Negotiable'],
];

for (const [raw, expected] of cases) {
  test(`mapPrice(${JSON.stringify(raw)}) -> ${expected}`, () => {
    const notes = [];
    const got = mapPrice(raw, notes);
    assert.equal(got, expected);
    assert.match(got, CANONICAL, 'every return must satisfy mentors_price_chk');
  });
}

test('an out-of-range amount leaves a note for the reviewing mentor', () => {
  const notes = [];
  mapPrice('$1500', notes);
  assert.equal(notes.length, 1);
  assert.match(notes[0], /outside \$1\.\.\$1000/);
});

// The old column was free text and notes go to the operator log, so a note
// may name what mapPrice computed and the raw value's length — never the raw
// text itself ("$30, call +7..." must not reach a log line).
test('a respelled amount is located by length, not quoted', () => {
  const notes = [];
  mapPrice('$30 / hour', notes);
  assert.equal(notes.length, 1);
  assert.match(notes[0], /leading \$30 taken from a 10-char legacy value/);
  assert.doesNotMatch(notes[0], /hour/);
});

test('no note branch ever quotes the raw value', () => {
  const dirty = '$30 / hour, call +7 999 123-45-67';
  for (const probe of [dirty, `1500 руб ${dirty}`, `ask me: ${dirty}`]) {
    const notes = [];
    mapPrice(probe, notes);
    for (const note of notes) {
      assert.ok(!note.includes('call'), `raw text leaked into a note: ${note}`);
      assert.ok(!note.includes('+7'), `raw text leaked into a note: ${note}`);
    }
  }
});
