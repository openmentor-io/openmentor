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

    // Redesign type system (docs/design-reference — component sheet):
    // Archivo = display (CAPS headlines), Schibsted Grotesk = names/numbers,
    // Inter = body, IBM Plex Mono = metadata. All self-hosted variable
    // fonts via next/font (CSS variables set in _app.tsx).
    fontFamily: {
      sans: ['var(--font-inter)', 'Inter', 'system-ui', 'sans-serif'],
      display: ['var(--font-archivo)', 'Archivo', 'system-ui', 'sans-serif'],
      name: ['var(--font-schibsted)', 'Schibsted Grotesk', 'system-ui', 'sans-serif'],
      mono: ['var(--font-plex-mono)', 'IBM Plex Mono', 'ui-monospace', 'monospace'],
    },

    extend: {
      // "Fresh Signal" brand palette. Hex values mirror the --om-* custom
      // properties in src/styles/brand-tokens.css (copied from the brand
      // asset pack in ../brand, which is the source of truth) plus the
      // redesign token deltas in src/styles/design-tokens.css.
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
        // Disabled text/icons only, never body copy — --om-ink-faint
        'ink-faint': '#B9BDC7',
        // Card / control surface — --om-paper-dim
        surface: '#F7F6F2',
        // Deep paper (skeletons, neutral gradient stop) — --om-paper-deep
        'surface-deep': '#EDEBE4',
        // Hairline borders on brand surfaces — --om-line
        line: '#DEDBD1',
        // Errors / destructive actions (AA on white) — --om-danger
        danger: '#E5484D',
        // Mint-family text on light surfaces (AA 5.2:1) — --om-mint-ink
        'mint-ink': '#0E7A70',
        brand: {
          // Deep navy (the open ring) — --om-ring
          navy: '#132A52',
          // Cobalt (the directional stroke) — --om-bar
          cobalt: '#2F5EFF',
          // Mint (the arrival node) — --om-dot
          mint: '#17C3B2',
        },
        // Mentor card pastels. `deep` variants are the gradient bottom
        // stop / hover tints. Assignment logic: src/lib/mentor-pastel.ts.
        pastel: {
          sky: { DEFAULT: '#C9E6F2', deep: '#B7DCEE' },
          sand: { DEFAULT: '#F4E3C0', deep: '#EED8AA' },
          sage: { DEFAULT: '#CDE8C6', deep: '#BCE0B4' },
          lavender: { DEFAULT: '#DDD9F4', deep: '#CFC9EE' },
          rose: { DEFAULT: '#F6D4E2', deep: '#F0C3D6' },
        },
      },

      // Redesign radius scale — replaces the old mixed rounded-lg/xl/full.
      borderRadius: {
        field: '12px',
        card: '14px',
        btn: '14px',
        panel: '16px',
      },

      // Redesign shadows (component sheet token deltas).
      boxShadow: {
        btn: '2px 2px 0 rgb(47 94 255 / 0.4)',
        'btn-hover': '4px 4px 0 rgb(47 94 255 / 0.4)',
        'card-hover': '0 12px 26px -10px rgb(19 42 82 / 0.22)',
        dropdown: '0 16px 36px -12px rgb(19 42 82 / 0.25)',
        'focus-field': '0 0 0 3px rgb(47 94 255 / 0.13)',
        'focus-ring': '0 0 0 3px #fff, 0 0 0 5.5px #2F5EFF',
      },

      // Pastel blocks render as top→bottom gradients (base → deep).
      backgroundImage: {
        'pastel-sky-grad': 'linear-gradient(180deg,#C9E6F2 0%,#B7DCEE 100%)',
        'pastel-sand-grad': 'linear-gradient(180deg,#F4E3C0 0%,#EED8AA 100%)',
        'pastel-sage-grad': 'linear-gradient(180deg,#CDE8C6 0%,#BCE0B4 100%)',
        'pastel-lavender-grad': 'linear-gradient(180deg,#DDD9F4 0%,#CFC9EE 100%)',
        'pastel-rose-grad': 'linear-gradient(180deg,#F6D4E2 0%,#F0C3D6 100%)',
        'pastel-neutral-grad': 'linear-gradient(180deg,#F7F6F2 0%,#EDEBE4 100%)',
        shimmer: 'linear-gradient(90deg,#F7F6F2 25%,#EDEBE4 50%,#F7F6F2 75%)',
      },

      // Motion spec: 120ms focus/color, 150–180ms hover/press, 200–240ms
      // entrances/crossfades, 300ms max one-off pops.
      transitionDuration: {
        120: '120ms',
        180: '180ms',
        240: '240ms',
      },
      transitionTimingFunction: {
        pop: 'cubic-bezier(.34,1.56,.64,1)',
      },
      keyframes: {
        'rise-in': {
          from: { opacity: '0', transform: 'translateY(8px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
        shimmer: {
          from: { backgroundPosition: '200% 0' },
          to: { backgroundPosition: '-200% 0' },
        },
        shake: {
          '0%, 100%': { transform: 'translateX(0)' },
          '20%, 60%': { transform: 'translateX(-3px)' },
          '40%, 80%': { transform: 'translateX(3px)' },
        },
        'toast-in': {
          from: { opacity: '0', transform: 'translateY(16px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
        'dropdown-in': {
          from: { opacity: '0', transform: 'translateY(-6px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
      },
      animation: {
        'rise-in': 'rise-in 240ms ease-out both',
        shimmer: 'shimmer 1.4s linear infinite',
        shake: 'shake 200ms ease-in-out',
        'toast-in': 'toast-in 240ms ease-out both',
        'dropdown-in': 'dropdown-in 140ms ease-out both',
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
