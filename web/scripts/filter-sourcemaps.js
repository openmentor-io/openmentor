#!/usr/bin/env node
/**
 * Filter source maps to keep only application sources, removing third-party code.
 * This cuts the upload size for Faro while keeping stack traces resolvable.
 *
 * Dropping a source from `sources` renumbers every later index, and every segment
 * in `mappings` is a reference INTO that array. This script used to set
 * `mappings: ''` instead of renumbering, with the comment "Clear mappings as they
 * reference old indices" — which is true, and which made every stripped map
 * resolve nothing at all. Faro then symbolicated nothing, so the upload was paid
 * for and delivered no file, line or column anywhere. The remapping below is the
 * whole point of the file: decode the mappings, drop the segments that pointed at
 * a removed source, renumber the ones that survive, re-encode.
 */

const fs = require('fs')
const path = require('path')
const { decode, encode } = require('@jridgewell/sourcemap-codec')

const STATIC_DIR = path.join(process.cwd(), '.next/static')

function findMapFiles(dir, files = []) {
  const entries = fs.readdirSync(dir, { withFileTypes: true })
  for (const entry of entries) {
    const fullPath = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      findMapFiles(fullPath, files)
    } else if (entry.name.endsWith('.map')) {
      files.push(fullPath)
    }
  }
  return files
}

// Check if a source path is from our application code
function isAppSource(source) {
  // App sources in Turbopack start with turbopack:///[project]/src/
  if (source.includes('[project]/src/')) return true
  // Also include project root files
  if (source.includes('[project]/') && !source.includes('node_modules')) return true
  return false
}

/**
 * Rewrite one parsed source map to keep only `keptIndices` of `sources`.
 *
 * Returns the new map. Preserves every field the input carried (`file`,
 * `sourceRoot`, `debugId`, …) — the previous version rebuilt the object from a
 * hardcoded field list, which silently dropped `file` and `sourceRoot` too.
 *
 * A segment with no source (a 1-field segment: generated column only) is kept
 * as-is; it marks "no original position here", which is information Faro uses to
 * stop attributing a frame to the previous mapping.
 */
function filterMap(map, keptIndices) {
  const sources = map.sources || []
  const newIndexOf = new Map()
  keptIndices.forEach((oldIndex, newIndex) => newIndexOf.set(oldIndex, newIndex))

  const decoded = map.mappings ? decode(map.mappings) : []

  // Names are indexed independently of sources; a name only survives if some
  // surviving segment still refers to it, and then it too has to be renumbered.
  const names = map.names || []
  const keptNameIndices = []
  const newNameIndexOf = new Map()

  const remapped = decoded.map((line) =>
    line.reduce((segments, segment) => {
      // [generatedColumn] — no original position, nothing to renumber.
      if (segment.length === 1) {
        segments.push([segment[0]])
        return segments
      }

      const newSourceIndex = newIndexOf.get(segment[1])
      if (newSourceIndex === undefined) return segments // pointed at a dropped source

      if (segment.length === 4) {
        segments.push([segment[0], newSourceIndex, segment[2], segment[3]])
        return segments
      }

      // 5 fields: the segment also names the original symbol.
      let newNameIndex = newNameIndexOf.get(segment[4])
      if (newNameIndex === undefined) {
        newNameIndex = keptNameIndices.length
        newNameIndexOf.set(segment[4], newNameIndex)
        keptNameIndices.push(segment[4])
      }
      segments.push([segment[0], newSourceIndex, segment[2], segment[3], newNameIndex])
      return segments
    }, [])
  )

  const filtered = {
    ...map,
    sources: keptIndices.map((i) => sources[i]),
    names: keptNameIndices.map((i) => names[i]),
    mappings: encode(remapped),
  }

  if (map.sourcesContent) {
    filtered.sourcesContent = keptIndices.map((i) => map.sourcesContent[i])
  }

  // ignoreList is a list of indices into `sources`, so it needs the same
  // renumbering; an unremapped one would mark the wrong files as third-party.
  if (Array.isArray(map.ignoreList)) {
    filtered.ignoreList = map.ignoreList
      .map((i) => newIndexOf.get(i))
      .filter((i) => i !== undefined)
  }

  return filtered
}

async function filterSourceMaps() {
  const mapFiles = findMapFiles(STATIC_DIR)

  // Loud, not silent (C12). Finding no maps means the upload that follows will
  // succeed while uploading nothing, which is exactly how the symbolication
  // feature was paid for and delivered nothing for as long as it did. Failing
  // the build is the only signal that cannot be missed.
  if (mapFiles.length === 0) {
    throw new Error(
      `No .map files under ${STATIC_DIR}. Source map upload would be a no-op. ` +
        'Check next.config.js productionBrowserSourceMaps and that nothing deleted ' +
        'the maps earlier in the build.'
    )
  }

  let kept = 0
  let removed = 0
  let stripped = 0
  let totalSizeBefore = 0
  let totalSizeAfter = 0

  for (const fullPath of mapFiles) {
    const mapFile = path.relative(STATIC_DIR, fullPath)
    const sizeBefore = fs.statSync(fullPath).size
    totalSizeBefore += sizeBefore

    try {
      const content = JSON.parse(fs.readFileSync(fullPath, 'utf8'))
      const sources = content.sources || []

      // Find indices of app sources
      const appSourceIndices = sources
        .map((source, index) => (isAppSource(source) ? index : -1))
        .filter((index) => index !== -1)

      if (appSourceIndices.length === 0) {
        // No app sources - remove the file entirely
        fs.unlinkSync(fullPath)
        removed++
        if (process.env.VERBOSE) {
          console.log(`Removed: ${mapFile} (no app sources)`)
        }
      } else if (appSourceIndices.length < sources.length) {
        // Has mix of app and third-party - strip third-party sources
        fs.writeFileSync(fullPath, JSON.stringify(filterMap(content, appSourceIndices)))
        stripped++

        const sizeAfter = fs.statSync(fullPath).size
        totalSizeAfter += sizeAfter

        if (process.env.VERBOSE) {
          console.log(
            `Stripped: ${mapFile} (${sources.length} -> ${appSourceIndices.length} sources, ${(sizeBefore / 1024).toFixed(0)}KB -> ${(sizeAfter / 1024).toFixed(0)}KB)`
          )
        }
      } else {
        // All sources are app sources - keep as is
        kept++
        totalSizeAfter += sizeBefore
        if (process.env.VERBOSE) {
          console.log(`Kept: ${mapFile} (all app sources)`)
        }
      }
    } catch (err) {
      console.error(`Error processing ${mapFile}:`, err.message)
      kept++
      totalSizeAfter += sizeBefore
    }
  }

  console.log(`Source maps: ${kept} kept, ${stripped} stripped, ${removed} removed`)
  console.log(`Size: ${(totalSizeBefore / 1024 / 1024).toFixed(1)}MB -> ${(totalSizeAfter / 1024 / 1024).toFixed(1)}MB`)
}

module.exports = { filterMap, isAppSource }

// Only sweep .next/static when run as a script, so the remapping above can be
// unit-tested without a build.
if (require.main === module) {
  filterSourceMaps().catch((err) => {
    console.error('Failed to filter source maps:', err)
    process.exit(1)
  })
}
