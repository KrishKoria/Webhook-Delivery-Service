---
name: Webhook Delivery
description: Admin console for a self-hosted webhook delivery service. Precise, quiet, state-first.
colors:
  canvas: "oklch(0.982 0.003 300)"
  surface: "oklch(0.996 0.002 300)"
  surface-raised: "oklch(0.962 0.005 300)"
  hairline: "oklch(0.905 0.006 300)"
  hairline-strong: "oklch(0.83 0.008 300)"
  ink: "oklch(0.23 0.012 300)"
  ink-secondary: "oklch(0.44 0.012 300)"
  ink-tertiary: "oklch(0.52 0.01 300)"
  plum-ink: "oklch(0.46 0.13 330)"
  plum-ink-deep: "oklch(0.40 0.13 330)"
  plum-wash: "oklch(0.955 0.025 330)"
  on-plum: "oklch(0.99 0.004 330)"
  focus-plum: "oklch(0.55 0.16 330)"
  delivered-green: "oklch(0.47 0.12 152)"
  delivered-wash: "oklch(0.955 0.035 152)"
  retry-amber: "oklch(0.50 0.11 70)"
  retry-wash: "oklch(0.96 0.045 85)"
  failure-red: "oklch(0.51 0.18 27)"
  failure-wash: "oklch(0.955 0.03 27)"
  info-blue: "oklch(0.48 0.09 245)"
  in-flight-violet: "oklch(0.48 0.13 292)"
typography:
  headline:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Helvetica Neue, sans-serif"
    fontSize: "1.75rem"
    fontWeight: 650
    lineHeight: 1.2
    letterSpacing: "-0.015em"
  verdict:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Helvetica Neue, sans-serif"
    fontSize: "1.375rem"
    fontWeight: 640
    lineHeight: 1.3
    letterSpacing: "-0.01em"
  title:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Helvetica Neue, sans-serif"
    fontSize: "1.125rem"
    fontWeight: 620
    lineHeight: 1.3
    letterSpacing: "-0.005em"
  body:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Helvetica Neue, sans-serif"
    fontSize: "0.9375rem"
    fontWeight: 400
    lineHeight: 1.5
    fontFeature: "tnum"
  label:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Helvetica Neue, sans-serif"
    fontSize: "0.8125rem"
    fontWeight: 560
    lineHeight: 1.4
  data:
    fontFamily: "ui-monospace, SF Mono, Cascadia Mono, JetBrains Mono, Menlo, Consolas, monospace"
    fontSize: "0.92em"
    fontWeight: 400
rounded:
  sm: "4px"
  md: "6px"
  lg: "10px"
  pill: "999px"
spacing:
  s1: "0.25rem"
  s2: "0.5rem"
  s3: "0.75rem"
  s4: "1rem"
  s5: "1.5rem"
  s6: "2rem"
  s7: "3rem"
components:
  button-primary:
    backgroundColor: "{colors.plum-ink}"
    textColor: "{colors.on-plum}"
    typography: "{typography.label}"
    rounded: "{rounded.md}"
    padding: "0 1rem"
    height: "2.25rem"
  button-primary-hover:
    backgroundColor: "{colors.plum-ink-deep}"
    textColor: "{colors.on-plum}"
  button-default:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.label}"
    rounded: "{rounded.md}"
    padding: "0 1rem"
    height: "2.25rem"
  button-default-hover:
    backgroundColor: "{colors.surface-raised}"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.ink-secondary}"
    rounded: "{rounded.md}"
  button-danger:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.failure-red}"
    rounded: "{rounded.md}"
  button-danger-hover:
    backgroundColor: "{colors.failure-wash}"
    textColor: "{colors.failure-red}"
  badge-delivered:
    backgroundColor: "{colors.delivered-wash}"
    textColor: "{colors.delivered-green}"
    rounded: "{rounded.pill}"
    padding: "0.125rem 0.5rem"
  badge-failed:
    backgroundColor: "{colors.failure-wash}"
    textColor: "{colors.failure-red}"
    rounded: "{rounded.pill}"
    padding: "0.125rem 0.5rem"
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: "0.5rem 0.75rem"
    height: "2.5rem"
  verdict:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.verdict}"
    rounded: "{rounded.lg}"
    padding: "1.5rem"
  nav-link-active:
    backgroundColor: "{colors.surface-raised}"
    textColor: "{colors.ink}"
    typography: "{typography.label}"
    rounded: "{rounded.md}"
    padding: "0.5rem 0.75rem"
---

# Design System: Webhook Delivery

## 1. Overview

**Creative North Star: "The Operations Ledger"**

This console reads like a well-kept ledger, not a dashboard. Every screen opens with a plain statement of what is true right now (the verdict), then lists the figures behind it in grouped rows you can act on. Hierarchy comes from type weight, size and spacing on calm, near-white (or near-black) surfaces. Colour is spent almost entirely on meaning: one plum-ink accent for actions and the current selection, and green, amber and red only for delivery state. It is a tool someone opens for a few minutes to answer "is it working, and what failed?", so it is dense where data is dense and quiet everywhere else.

The system follows PRODUCT.md: "Precise, quiet, trustworthy. Closest reference: Linear." It explicitly rejects generic Bootstrap or AdminLTE admin themes (blue primary buttons, gray cards, icon sidebars), neon-on-black "developer tool" aesthetics, SaaS hero-metric card rows, and glassmorphism or gradient-heavy decoration.

Layout is a single centred column (max 76rem) under a slim top bar with text navigation. Figures sit in a responsive ledger grid (`auto-fit`, 15rem minimum per group), tables scroll inside their own bordered wrapper, and below 40rem the gutters tighten and key-value lists stack. Motion is limited to 150 to 220ms state feedback with an ease-out-quart curve; nothing animates on page load, and `prefers-reduced-motion` removes it all.

**Key Characteristics:**
- State first: a verdict sentence with a meter opens every overview screen.
- Ledger groups with hairline rules instead of cards.
- Restrained colour: tinted neutrals (hue 300) plus one plum-ink accent (hue 330).
- Status colour always paired with a text label and a dot, never colour alone.
- System sans for everything; monospace only for data (URLs, IDs, payloads, HTTP codes).
- Light and dark themes follow the OS (`prefers-color-scheme`).

## 2. Colors

A restrained palette: violet-tinted neutrals, one plum-ink accent, and a strict set of state colours that only ever mean delivery status. All values are OKLCH; the frontmatter lists the light theme, and the dark theme redefines the same tokens (lighter accents and state colours on deep neutral surfaces).

### Primary

- **Plum Ink** (`{colors.plum-ink}`): primary buttons, links, the active nav underline, the brand mark and success-rate meters. It marks "what you can do" and "where you are", nothing else.
- **Deep Plum** (`{colors.plum-ink-deep}`): hover and pressed state for primary actions and links.
- **Plum Wash** (`{colors.plum-wash}`): rare selected-row or soft emphasis backgrounds.
- **Focus Plum** (`{colors.focus-plum}`): 2px focus outlines and the 3px input focus halo (25% mix).

### Neutral

- **Canvas** (`{colors.canvas}`): page background.
- **Surface** (`{colors.surface}`): tables, inputs, verdict panels, toasts, default buttons.
- **Raised Surface** (`{colors.surface-raised}`): hover fills, active nav item, segmented control track, badges without state.
- **Hairline / Strong Hairline** (`{colors.hairline}`, `{colors.hairline-strong}`): row dividers and control borders; strong hairline also underlines ledger group headings.
- **Ink / Secondary Ink / Tertiary Ink** (`{colors.ink}`, `{colors.ink-secondary}`, `{colors.ink-tertiary}`): primary text, supporting text and labels, metadata. All meet WCAG AA on Canvas and Surface in both themes.

### State

- **Delivered Green** (`{colors.delivered-green}` on `{colors.delivered-wash}`): delivered, active, reachable.
- **Retry Amber** (`{colors.retry-amber}` on `{colors.retry-wash}`): retrying, degraded, warnings.
- **Failure Red** (`{colors.failure-red}` on `{colors.failure-wash}`): failed attempts, dead letters, destructive actions.
- **Info Blue** (`{colors.info-blue}`) and **In-flight Violet** (`{colors.in-flight-violet}`): neutral notices and tasks currently being delivered.

### Named Rules

**The Status-Only Rule.** Green, amber and red are reserved for delivery state. A decorative use of a state colour is a bug.

**The One Accent Rule.** Plum ink covers well under 10% of any screen: primary actions, links, current selection, focus. If a screen feels plum, something is decorated that should not be.

**The Label-Always Rule.** Every state colour ships with a word ("failed attempt", "dead", "paused") and a dot. Colour never carries meaning alone.

## 3. Typography

**Body Font:** system-ui stack (`system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", sans-serif`)
**Label/Mono Font:** `ui-monospace, "SF Mono", "Cascadia Mono", "JetBrains Mono", Menlo, Consolas, monospace`

**Character:** One native sans carries everything, tuned with weight (560 to 650) and tight negative tracking on headings. Monospace appears only where the content is machine text. Tabular numerals are on globally so figures line up in every column.

### Hierarchy

- **Headline** (650, 1.75rem, 1.2, -0.015em): the single page `h1`. Subscription pages use a monospace URL title (560, 1.375rem) instead, because the URL is the subscription's name.
- **Verdict** (640, 1.375rem, 1.3): the state sentence at the top of overview screens.
- **Title** (620, 1.125rem, 1.3): section headings (`h2`).
- **Body** (400, 0.9375rem, 1.5): running text and table cells. Ledes and verdict details cap at 68 to 70ch.
- **Label** (560, 0.8125rem): buttons, nav, form labels, badges at 0.75rem. Ledger group headings are uppercase labels (620, 0.8125rem, 0.06em tracking) in secondary ink.
- **Data** (monospace, 0.92em): URLs, IDs, event types, HTTP codes, payloads and code blocks.

### Named Rules

**The Machine Text Rule.** Monospace means "this is literally what the system sees". Never use it for prose, labels or decoration.

**The Tight Scale Rule.** Steps stay between 1.15 and 1.25 apart (0.75, 0.8125, 0.9375, 1.125, 1.375, 1.75rem). No display sizes, no fluid type.

## 4. Elevation

The system is flat. Depth comes from tonal steps (Canvas, Surface, Raised Surface) and hairline borders, not shadows. Exactly one shadow exists, for content that floats above the page.

### Shadow Vocabulary

- **Pop** (`box-shadow: 0 1px 2px oklch(0.2 0.02 300 / 0.06), 0 8px 24px oklch(0.2 0.02 300 / 0.12)`; dark: `0 1px 2px oklch(0 0 0 / 0.3), 0 8px 24px oklch(0 0 0 / 0.45)`): toasts and the row action menu only.

### Named Rules

**The Flat-By-Default Rule.** Anything that sits in the page flow is flat. Only toasts and open menus, which float over content, get the Pop shadow.

## 5. Components

### Buttons

Restrained and exact: they look like controls, not banners.

- **Shape:** gently rounded (6px), 2.25rem tall (1.875rem small; 2.75rem on touch screens).
- **Primary:** plum ink fill with near-white text. One per view, for the main action.
- **Default:** surface fill, strong hairline border, ink text; hover moves to Raised Surface.
- **Ghost:** no border or fill, secondary ink; for low-emphasis row actions (Pause, Redeliver, More).
- **Danger:** surface fill with failure-red text; hover adds the failure wash and a red border. Always confirms before acting.
- **States:** 150ms colour transitions; 2px Focus Plum outline on `:focus-visible`; disabled at 45% opacity; 60% opacity while an htmx request is in flight.

### Badges

- **Style:** pill (999px), 0.75rem label weight, a 6px dot in the current colour, then the word.
- **Variants:** delivered/active (green), retrying/paused (amber), failed/dead (red), in flight (violet), neutral (raised surface, secondary ink).

### Verdict (signature)

A single bordered panel (10px radius, 1.5rem padding) holding a state badge, one sentence in the Verdict style, a 6px meter, and a detail line with the next action as a link. Failing state tints the panel with a failure wash and a red-mixed border; warning state only tints the border.

### Ledger (signature)

Grouped figures instead of metric cards. Each group has an uppercase label heading over a strong hairline, a link to the full list on the right, then rows of term and number separated by hairlines. Alert rows colour only the number (failure red or retry amber). Every number links to the filtered list it counts.

### Tables

Inside a bordered wrapper (10px radius) on Surface that scrolls horizontally on narrow screens. Uppercase label headers, hairline row dividers, tabular numbers, monospace for data cells, row actions right-aligned as ghost buttons.

### Inputs / Fields

- **Style:** Surface fill, strong hairline border, 6px radius, 2.5rem tall, 0.9375rem text.
- **Hover:** border darkens to tertiary ink.
- **Focus:** border becomes Focus Plum plus a 3px 25% halo.
- **Error:** `aria-invalid` field with failure-red border and a message under it; an error summary at the top of the form takes focus.

### Navigation

A slim top bar: brand mark plus name, text nav links (label size, secondary ink). The active link gets Raised Surface and a 2px plum underline. Subscription pages add a tab row with the same treatment. The nav scrolls horizontally on narrow screens; the brand name hides below 40rem.

### Toasts

Bottom-right stack, Surface with Pop shadow, 10px radius, a coloured dot (info, ok, error) and a dismiss button. Success and info auto-dismiss after about 6s; errors stay until dismissed.

## 6. Do's and Don'ts

### Do:

- **Do** open every overview screen with the verdict: what is true now, in words and a number.
- **Do** make every figure a link to the rows it counts.
- **Do** pair every state colour with its word and a dot.
- **Do** keep plum ink for actions, links, current selection and focus only.
- **Do** use monospace for URLs, IDs, event types, HTTP codes and payloads, and nowhere else.
- **Do** confirm destructive actions and keep them in the danger style (failure-red text, wash on hover).
- **Do** keep transitions between 150 and 250ms with the ease-out-quart curve, and honour `prefers-reduced-motion`.
- **Do** define colours as OKLCH tokens and redefine them for dark mode; never hard-code a colour in a component.

### Don't:

- **Don't** build a generic Bootstrap or AdminLTE admin theme: blue primary buttons, gray cards, icon sidebars.
- **Don't** use neon-on-black "developer tool" aesthetics: glowing green or cyan accents.
- **Don't** build SaaS hero-metric card rows: identical big-number cards with gradient accents and tiny labels.
- **Don't** use glassmorphism or gradient-heavy decoration, including gradient text.
- **Don't** put a coloured stripe on one side of a card, row or alert (`border-left` or `border-right` over 1px as an accent).
- **Don't** use pure black or white; neutrals stay tinted toward hue 300.
- **Don't** add shadows to anything in the page flow; only toasts and menus float.
- **Don't** animate layout properties (width, height, top, left) or choreograph page loads.
- **Don't** use inline styles or scripts; the CSP (`style-src 'self'; script-src 'self'`) forbids them. Widths come from the `w-0` to `w-100` utility classes.
- **Don't** use em dashes in interface copy.
