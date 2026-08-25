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

const { mapPrice } = require('./migrate-mentors.js');

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

  // The passthrough branch, now canonicalised instead of returned verbatim.
  ['$50', '$50'],
  ['50', '$50'],
  ['$30 / hour', '$30'],
  ['$50.00', '$50'],
  ['$ 75', '$75'],

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

test('a respelled amount records what it was respelled from', () => {
  const notes = [];
  mapPrice('$30 / hour', notes);
  assert.equal(notes.length, 1);
  assert.match(notes[0], /"\$30 \/ hour" -> "\$30"/);
});
