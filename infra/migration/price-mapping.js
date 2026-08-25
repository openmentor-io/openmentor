// getmentor.dev price -> canonical openmentor price (D22 mapping, D87 grammar).
//
// Separate from migrate-mentors.js so mapprice.test.js can require it without
// dragging in the import stack (pg, the AWS and Anthropic SDKs) — CI runs the
// test on a runner that has no node_modules for this package, and requiring
// the main script there is a MODULE_NOT_FOUND, not a test.
//
// Every return must satisfy mentors_price_chk (api/migrations/000014):
// 'Free', 'Negotiable', or '$N' for a whole PRICE_MIN..PRICE_MAX. Anything
// else fails the INSERT and aborts an import mid-run — this module used to
// live inline and passed '$30 / hour' and bare '50' through verbatim, which
// the column accepted as free text and refuses now.
const PRICE_MIN = 1;
const PRICE_MAX = 1000;

/**
 * @param {string} price   raw getmentor.dev price text
 * @param {string[]} notes run-note sink, surfaced to the operator per mentor
 * @param {number} rubToUsdRate  RUB per USD; the caller owns the default
 *                               (config.rubToUsdRate in migrate-mentors.js)
 */
function mapPrice(price, notes, rubToUsdRate) {
  const raw = price.trim();
  if (raw === '' || /бесплатно/i.test(raw) || /^free$/i.test(raw)) {
    if (raw === '') notes.push('price: empty -> Free');
    return 'Free';
  }
  if (/договор/i.test(raw) || /negotiable/i.test(raw)) return 'Negotiable';

  // NOTES NEVER QUOTE THE RAW VALUE. The old column was free text, a note
  // line goes to the operator log, and the logging rules keep free text out
  // of logs — a legacy price could carry anything ("$30, call +7..."). A note
  // names only what mapPrice itself computed (the matched amount, the rate)
  // plus the raw value's LENGTH, which locates the row without copying it;
  // the operator reads the actual value where it lives, with psql.
  const describeRaw = `a ${raw.length}-char legacy value`;

  // An out-of-range amount becomes Negotiable + a note, NOT a clamp: rewriting
  // 150000 руб into the $1000 cap would silently change what the mentor
  // charges by a large factor. Negotiable is this function's established
  // "could not map" placeholder — the profile arrives hidden (status
  // 'inactive', D22) and the mentor reviews it before going live, so the note
  // is what surfaces the case to a human instead of the import crashing.
  const canonical = (usd, note) => {
    if (usd < PRICE_MIN || usd > PRICE_MAX) {
      notes.push(`price: $${usd} (from ${describeRaw}) is outside $${PRICE_MIN}..$${PRICE_MAX} -> Negotiable (mentor to set on review)`);
      return 'Negotiable';
    }
    if (note) notes.push(note);
    return `$${usd}`;
  };

  const match = raw.replace(/\s+/g, '').match(/^(\d+)(?:руб|р|₽)/i);
  if (match) {
    const rub = Number(match[1]);
    const usd = Math.max(5, Math.round(rub / rubToUsdRate / 5) * 5);
    return canonical(usd, `price: ${rub} RUB -> "$${usd}" (rate ${rubToUsdRate} RUB/USD, from ${describeRaw})`);
  }

  // Looks like a USD amount ('$30 / hour', '50', '$1,000', '$50.00'): read
  // the ENTIRE leading number token before deciding, digits with separators
  // and decimals included. A digits-only prefix match stops at the first
  // comma, which read '$1,000' as $1 — a 1000x price rewrite that satisfies
  // mentors_price_chk and so is invisible to every grammar test (PR review).
  const tokenMatch = raw.match(/^\$?\s*([\d.,]+)/);
  if (tokenMatch) {
    const token = tokenMatch[1];
    let usd = null;
    if (/^\d+$/.test(token)) {
      usd = Number(token);
    } else if (/^\d{1,3}(,\d{3})+$/.test(token)) {
      // Well-formed thousands separators denote the same number.
      usd = Number(token.replace(/,/g, ''));
    } else if (/^\d+\.0+$/.test(token)) {
      // ".00" is decoration; "$49.99" is NOT — truncating nonzero cents
      // rewrites the price, so it falls through to Negotiable below.
      usd = Number(token.split('.')[0]);
    }
    if (usd !== null) {
      return canonical(usd, `$${usd}` === raw ? null : `price: leading $${usd} taken from ${describeRaw} -> "$${usd}" (canonical grammar, D87)`);
    }
    // A malformed number token ("1,0,00", "49.99"): parsing a piece of it
    // would assemble a price the mentor did not write.
    notes.push(`price: could not safely read a number out of ${describeRaw} -> Negotiable`);
    return 'Negotiable';
  }

  notes.push(`price: could not parse ${describeRaw} -> Negotiable`);
  return 'Negotiable';
}

module.exports = { mapPrice, PRICE_MIN, PRICE_MAX };
