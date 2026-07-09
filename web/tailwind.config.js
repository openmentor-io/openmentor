module.exports = {
  content: [
    './src/components/**/*.{js,ts,jsx,tsx}',
    './src/pages/**/*.{js,ts,jsx,tsx}',
    './src/lib/**/*.{js,ts,jsx,tsx}',
  ],
  darkMode: 'media', // or 'media' or 'class'
  theme: {
    container: {
      center: true,
      padding: {
        DEFAULT: '1rem',
        sm: '2rem',
      },
    },

    fontFamily: {
      sans: ['var(--font-inter)', 'Inter', 'system-ui', 'sans-serif'],
    },

    extend: {
      // "Fresh Signal" brand palette. Hex values mirror the --om-* custom
      // properties in src/styles/brand-tokens.css (copied from the brand
      // asset pack in ../brand, which is the source of truth).
      // Hex literals (rather than var() references) keep Tailwind's
      // opacity modifiers (e.g. ring-ink/10, bg-brand-navy/90) working.
      colors: {
        // Primary text — --om-ink
        ink: '#161A20',
        // Secondary text / captions — --om-ink-soft
        'ink-soft': '#5B6270',
        // Darker secondary text for tinted (pastel) surfaces — keeps
        // WCAG AA (>= 5.0:1) on every pastel incl. the deepened hover tints
        'ink-mute': '#4A5160',
        // Card / control surface — --om-paper-dim
        surface: '#F7F6F2',
        // Hairline borders on brand surfaces — --om-line
        line: '#DEDBD1',
        brand: {
          // Deep navy (the open ring) — --om-ring
          navy: '#132A52',
          // Cobalt (the directional stroke) — --om-bar
          cobalt: '#2F5EFF',
          // Mint (the arrival node) — --om-dot
          mint: '#17C3B2',
        },
        // Mentor card pastels (redesign Phase B). Figma concept hues
        // (Frame 2131329156) desaturated/warmed toward --om-paper-dim so
        // they harmonize with the Fresh Signal brand. `deep` variants are
        // the hover tints. Assignment logic: src/lib/mentor-pastel.ts.
        pastel: {
          sky: { DEFAULT: '#C9E6F2', deep: '#B7DCEE' },
          sand: { DEFAULT: '#F4E3C0', deep: '#EED8AA' },
          sage: { DEFAULT: '#CDE8C6', deep: '#BCE0B4' },
          lavender: { DEFAULT: '#DDD9F4', deep: '#CFC9EE' },
          rose: { DEFAULT: '#F6D4E2', deep: '#F0C3D6' },
        },
      },
    },
  },
  variants: {
    extend: {},
  },
  plugins: [
    require('@tailwindcss/forms'),
    require('@tailwindcss/typography'),
    require('@tailwindcss/aspect-ratio'),
  ],
}
