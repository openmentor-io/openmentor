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
    const usd = Math.max(5, Math.round(rub / rubToUsdRate / 5) * 5);
    return canonical(usd, `price: "${raw}" -> "$${usd}" (rate ${rubToUsdRate} RUB/USD)`);
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

module.exports = { mapPrice, PRICE_MIN, PRICE_MAX };
