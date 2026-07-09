# openmentor.io — brand asset pack

Everything needed to implement the logo and brand colors on the website.
Start with `manifest.json` if you're an agent — it indexes every file with
its purpose and exact use case. This README is the narrative version of
the same information.

## Quick start (most common tasks)

**Site header / footer:**
```html
<img src="/brand/logo/svg/logo-horizontal.svg" alt="openmentor.io" height="32">
```
Use `logo-horizontal-dark.svg` instead on any dark-background section.

**Favicon (`<head>`):**
```html
<link rel="icon" href="/favicon.ico" sizes="any">
<link rel="icon" type="image/png" sizes="32x32" href="/brand/logo/png/favicon/favicon-32.png">
<link rel="icon" type="image/png" sizes="16x16" href="/brand/logo/png/favicon/favicon-16.png">
<link rel="apple-touch-icon" href="/brand/logo/png/icon-app/icon-app-180.png">
```

**PWA manifest.json:**
```json
"icons": [
  { "src": "/brand/logo/png/icon-app/icon-app-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any maskable" },
  { "src": "/brand/logo/png/icon-app/icon-app-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any maskable" }
]
```

**Brand colors:** import `colors/tokens.css` globally, use the `--om-*`
custom properties instead of hardcoded hex values anywhere the brand shows
up (buttons, links, dark-mode surfaces).

**Social / org avatar** (GitHub, LinkedIn, X, Slack): `logo/png/social/social-avatar-400.png`

## Folder structure

```
openmentor-brand-assets/
├── README.md                  ← you are here
├── manifest.json               ← machine-readable index of every asset
├── logo/
│   ├── svg/                    ← source of truth, use these first
│   │   ├── logomark.svg                    icon only, full color
│   │   ├── logomark-mono-black.svg
│   │   ├── logomark-mono-white.svg
│   │   ├── logomark-tile-brand.svg         icon on brand-gradient tile (icon source)
│   │   ├── wordmark.svg                    text only, dark
│   │   ├── wordmark-white.svg              text only, white
│   │   ├── logo-horizontal.svg             ★ primary logo, light bg
│   │   ├── logo-horizontal-dark.svg        ★ primary logo, dark bg
│   │   ├── logo-horizontal-mono-black.svg
│   │   └── logo-horizontal-mono-white.svg
│   ├── png/
│   │   ├── logomark/            transparent-bg raster mark, 16 → 1024px + mono
│   │   ├── icon-app/            mark on brand tile, app-icon sizes (128–1024)
│   │   ├── favicon/             16 / 32 / 48px flat favicon PNGs
│   │   └── social/              400px square avatar for profile photos
│   └── ico/
│       └── favicon.ico          multi-res (16/32/48) — drop at site root
├── colors/
│   ├── tokens.css               CSS custom properties
│   └── tokens.json              same tokens, for JS/build tooling
├── fonts/
│   └── README.md                Inter setup (Google Fonts + self-host)
└── explorations/                alternate color directions, NOT shipped —
    ├── warm-momentum/           reference only, see note below
    ├── indigo-amber/
    └── midnight-native/
```

## The mark, in one paragraph

Three shapes, always: an **open ring** (the gate — mentorship without
gatekeeping), a **diagonal stroke that exits precisely through the gap**
(a mentor-guided path, not a drift), and a **small node where it lands**
(arrival, momentum). Don't close the ring, don't add a fourth shape, don't
recolor the three pieces independently outside the provided variants.

## Naming convention

`{subject}-{variant}-{modifier?}-{size?}.{ext}`
Examples: `logomark-mono-black.svg`, `logo-horizontal-dark.svg`,
`icon-app-512.png`. Sizes are always pixel width of a square canvas.

## What NOT to do

- Don't recreate the mark from scratch in code (no re-implementing the SVG
  paths by hand) — always reference these files directly.
- Don't use `explorations/` on the live site without being explicitly told
  to switch brand direction; they're kept for future reference only.
- Don't stretch, skew, rotate, drop-shadow, or outline the mark.
- Don't put the mark on a background color it wasn't designed for — use
  `logomark.svg` on light/neutral surfaces, `logomark-tile-brand.svg` where
  a filled icon tile is expected (app icons, avatars), and the `-white` /
  `-dark` variants for dark surfaces.
- Don't render `wordmark.svg` or `logo-horizontal*.svg` before Inter has
  loaded — see `fonts/README.md`.

## Shipped color direction: "Fresh Signal"

| Token | Hex | Role |
|---|---|---|
| `--om-ring` | `#132A52` | deep navy, the open ring |
| `--om-bar` | `#2F5EFF` | cobalt, the directional stroke |
| `--om-dot` | `#17C3B2` | mint, the arrival node |
| `--om-ink` | `#161A20` | primary text |
| `--om-ink-soft` | `#5B6270` | secondary text, `.io` |

Full token set including dark-mode values is in `colors/tokens.css` /
`colors/tokens.json`.
