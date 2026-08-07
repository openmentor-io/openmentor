/**
 * @jest-environment node
 */

// The filter used to set `mappings: ''` on every map it stripped, with the
// comment "Clear mappings as they reference old indices". The comment was right
// about the cause and wrong about the fix: the maps still uploaded, and Faro
// resolved nothing from them, so every stack frame stayed unsymbolicated.
//
// So these tests resolve generated positions back to original ones THROUGH a
// stripped map. That is the only assertion that distinguishes a working map from
// a well-formed empty one — a schema check passes on both.

import { originalPositionFor, TraceMap } from '@jridgewell/trace-mapping'
import { encode } from '@jridgewell/sourcemap-codec'

// eslint-disable-next-line @typescript-eslint/no-require-imports
const { filterMap, isAppSource } = require('../../../scripts/filter-sourcemaps.js')

const VENDOR = 'turbopack:///[project]/node_modules/react-dom/index.js'
const APP_A = 'turbopack:///[project]/src/components/Widget.tsx'
const APP_B = 'turbopack:///[project]/src/lib/format.ts'

type RawMap = {
  version: number
  file?: string
  sourceRoot?: string
  sources: string[]
  sourcesContent?: (string | null)[]
  names: string[]
  mappings: string
  ignoreList?: number[]
}

/**
 * One generated line with three mapped columns, one per source, so a correct
 * filter has to drop the vendor segment AND renumber the two survivors.
 *
 * Segments are [generatedColumn, sourceIndex, originalLine, originalColumn,
 * nameIndex] with a ZERO-based original line, which is what the wire format
 * stores; trace-mapping reports lines one-based, hence the off-by-one between the
 * fixture below and the expectations.
 */
function buildMap(): RawMap {
  return {
    version: 3,
    file: 'chunk.js',
    sources: [VENDOR, APP_A, APP_B],
    sourcesContent: ['vendor source', 'app A source', 'app B source'],
    names: ['vendorSymbol', 'renderWidget', 'formatPrice'],
    mappings: encode([
      [
        [0, 0, 5, 2, 0],
        [10, 1, 12, 4, 1],
        [40, 2, 30, 8, 2],
      ],
    ]),
    ignoreList: [0],
  }
}

function resolve(map: RawMap, column: number) {
  return originalPositionFor(new TraceMap(map as never), { line: 1, column })
}

describe('filter-sourcemaps', () => {
  it('classifies app and vendor sources', () => {
    expect(isAppSource(APP_A)).toBe(true)
    expect(isAppSource(APP_B)).toBe(true)
    expect(isAppSource(VENDOR)).toBe(false)
  })

  it('resolves app frames through a map whose vendor source was stripped', () => {
    const filtered = filterMap(buildMap(), [1, 2])

    // Generated column 10 was source index 1 (APP_A) and is index 0 now.
    expect(resolve(filtered, 10)).toEqual({
      source: APP_A,
      line: 13,
      column: 4,
      name: 'renderWidget',
    })

    // Column 40 was index 2 (APP_B) and is index 1 now. If the segments were
    // kept without renumbering, this frame would resolve to APP_A or to nothing.
    expect(resolve(filtered, 40)).toEqual({
      source: APP_B,
      line: 31,
      column: 8,
      name: 'formatPrice',
    })
  })

  it('drops the segments that pointed at a removed source', () => {
    const filtered = filterMap(buildMap(), [1, 2])

    // Column 0 was the vendor frame. It must resolve to nothing rather than to
    // whichever source inherited index 0.
    expect(resolve(filtered, 0).source).toBeNull()
  })

  it('keeps sources, sourcesContent and names aligned after filtering', () => {
    const filtered = filterMap(buildMap(), [1, 2])

    expect(filtered.sources).toEqual([APP_A, APP_B])
    expect(filtered.sourcesContent).toEqual(['app A source', 'app B source'])
    // Only the names the surviving segments still refer to, renumbered.
    expect(filtered.names).toEqual(['renderWidget', 'formatPrice'])
    expect(filtered.mappings).not.toBe('')
  })

  it('preserves the fields the rewrite is not about', () => {
    const filtered = filterMap(buildMap(), [1, 2])

    // The previous version rebuilt the object from a hardcoded field list, so
    // `file` and `sourceRoot` were silently dropped along with the mappings.
    expect(filtered.version).toBe(3)
    expect(filtered.file).toBe('chunk.js')
  })

  it('renumbers ignoreList and drops entries for removed sources', () => {
    const map = buildMap()
    map.ignoreList = [0, 2] // vendor + APP_B

    const filtered = filterMap(map, [1, 2])

    // APP_B moved from index 2 to 1; the vendor entry has no index any more.
    expect(filtered.ignoreList).toEqual([1])
  })

  it('keeps source-less segments, which mark "no original position here"', () => {
    const map = buildMap()
    map.mappings = encode([
      [
        [10, 1, 12, 4, 1],
        [20], // generated column only
      ],
    ])

    const filtered = filterMap(map, [1, 2])

    expect(resolve(filtered, 10).source).toBe(APP_A)
    // Without the 1-length segment, column 20 would keep resolving to APP_A and
    // a frame in unmapped generated code would be blamed on a real source file.
    expect(resolve(filtered, 20).source).toBeNull()
  })
})
