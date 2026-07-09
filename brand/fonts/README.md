# Typography

The wordmark uses **Inter** (weights 500 and 700). Inter is open-source (SIL
Open Font License) and not bundled in this pack to keep it small — pull it
from Google Fonts or self-host it.

## Fastest: Google Fonts

```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
```

## Self-hosted (recommended for production / no third-party request)

1. Download the two weights you need from https://fonts.google.com/specimen/Inter
   (`Inter-Medium.woff2`, `Inter-Bold.woff2`), or use `@fontsource/inter` from npm.
2. Add:

```css
@font-face {
  font-family: 'Inter';
  font-weight: 500;
  font-style: normal;
  font-display: swap;
  src: url('/fonts/inter-medium.woff2') format('woff2');
}
@font-face {
  font-family: 'Inter';
  font-weight: 700;
  font-style: normal;
  font-display: swap;
  src: url('/fonts/inter-bold.woff2') format('woff2');
}
```

## Wordmark spec

- `openmentor` — weight 700, letter-spacing -0.025em, color `--om-ink`
- `.io` — weight 500, letter-spacing -0.01em, color `--om-ink-soft`
- Always lowercase. Never re-kern or stretch.

The wordmark SVGs in `../logo/svg/` use `<text>`, not outlined paths, so
they render crisp at any size **as long as Inter is loaded on the page**.
If you need a font-independent copy (print, a design tool that can't load
web fonts), open the SVG in Figma/Illustrator and convert the text to
outlines there.
