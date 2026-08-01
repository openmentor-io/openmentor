import nextJest from 'next/jest.js'

const createJestConfig = nextJest({ dir: './' })

// Transitive dependencies that ship ESM only, with no CommonJS entry point.
// Jest runs our tests as CommonJS, so these have to go through the SWC
// transform or `require()`ing them throws "Cannot use import statement outside
// a module".
//
// The whole htmlparser2 v12 family is here because sanitize-html moved onto it:
// sanitize-html 2.17.0 (what yarn.lock had pinned) used the CommonJS
// htmlparser2 v8, and 2.17.6 — which any fresh resolution picks up — uses v12,
// which is ESM-only. So this is not a package-manager difference; regenerating
// the lockfile with yarn would have surfaced exactly the same failure.
const esmOnlyDeps = ['htmlparser2', 'domhandler', 'domutils', 'dom-serializer', 'entities', 'domelementtype']

const customJestConfig = {
  testEnvironment: 'jsdom',
  setupFilesAfterEnv: ['<rootDir>/jest.setup.ts'],
  moduleNameMapper: {
    '^@/(.*)$': '<rootDir>/src/$1',
  },
  // Raise the default 5s per-test timeout: the form tests drive many sequential
  // userEvent interactions that run comfortably under 5s locally but exceed it
  // on slower CI runners. 15s gives ample headroom without masking real hangs.
  testTimeout: 15000,
}

// next/jest hard-codes '/node_modules/' into transformIgnorePatterns and merges
// a custom `transformIgnorePatterns` by APPENDING to it ("Custom config can
// append to transformIgnorePatterns but not modify it" — next/dist/build/jest/
// jest.js). So neither setting it above nor listing the packages in
// `transpilePackages` can lift that blanket ignore, and the config has to be
// rewritten after next/jest has produced it.
//
// Next's own `transpilePackages` pattern would not have worked anyway: it emits
// `/node_modules/(?!(pkg)/)`, which only matches a package at the FIRST
// node_modules segment. These live nested at
// node_modules/sanitize-html/node_modules/htmlparser2/, where that first
// segment is `sanitize-html/` — the lookahead passes, the path is ignored, and
// the file never gets transformed. The `(?:.*/)?` below is what makes it match
// at any depth.
export default async function jestConfig() {
  const config = await createJestConfig(customJestConfig)()

  // Widen every "ignore node_modules" rule with one more exception rather than
  // replacing it, so Next's own exemptions (transpilePackages, its client
  // runtime) survive untouched. next/jest emits either a bare '/node_modules/'
  // or '/node_modules/(?!.pnpm)(?!(<transpiled>)/)' depending on whether
  // transpilePackages is set — prefixing the lookahead works for both.
  const exception = `(?!(?:.*/)?(${esmOnlyDeps.join('|')})/)`
  const prefix = '/node_modules/'
  let patched = 0

  config.transformIgnorePatterns = config.transformIgnorePatterns.map((pattern) => {
    if (!pattern.startsWith(prefix)) return pattern
    patched++
    return prefix + exception + pattern.slice(prefix.length)
  })

  // This depends on the internal shape of a config next/jest generates, so fail
  // loudly if a Next upgrade changes it. Silently matching nothing would put
  // the ESM-only transitive deps back behind the blanket ignore, and the only
  // symptom would be "Cannot use import statement outside a module" in tests
  // that have nothing to do with jest config.
  if (patched === 0) {
    throw new Error(
      'jest.config.mjs: no transformIgnorePatterns entry started with "/node_modules/". ' +
        'next/jest changed its output shape — re-check how the ESM-only deps get transformed.'
    )
  }

  return config
}
