# Product

## Register

product

## Users

Primarily the author, a solo developer running the service, plus recruiters and engineering reviewers who open it as a portfolio project, usually on a laptop in a normal daylight office or at home. They have a few minutes at most. The job: understand at a glance that webhooks are flowing, find and fix what failed (retry, redeliver, pause), and set up or test a subscription without reading docs.

## Product Purpose

The admin console for a self-hosted webhook delivery service: it manages subscriptions, shows delivery activity and analytics, and works the backlog, dead-letter queue and scheduled webhooks. Success means a reviewer understands the system's state and its reliability story (retries, leases, dead letters, signing) within one screen, and every operational action is one click away and safe.

## Brand Personality

Precise, quiet, trustworthy. Closest reference: Linear. Hierarchy comes from typography and spacing, not ornament. Status color carries meaning and nothing else. Copy is terse and exact, using the system's real vocabulary (task, attempt, lease, dead letter) without jargon padding.

## Anti-references

- Generic Bootstrap or AdminLTE admin themes: blue primary buttons, gray cards, icon sidebars.
- Neon-on-black "developer tool" aesthetics: glowing green or cyan accents.
- SaaS hero-metric card rows: identical big-number cards with gradient accents and tiny labels.
- Glassmorphism and gradient-heavy decoration, including gradient text.

## Design Principles

1. **State before chrome.** The first thing on every screen is what is true now (healthy, failing, backed up), stated in words and numbers, not decoration.
2. **Every number earns an action.** A failure count links to the failures; a dead letter offers retry; nothing is display-only when it could be actionable.
3. **Safe by default.** Destructive actions confirm inline, nothing irreversible hides behind hover, and the no-JavaScript path still works.
4. **Real vocabulary, exact copy.** Use the system's own terms consistently and say precisely what happened and what to do next.
5. **Quiet until it matters.** Neutral surfaces stay calm so failure states stand out immediately.

## Accessibility & Inclusion

WCAG 2.2 AA: text and UI contrast, full keyboard operation with visible focus, status never conveyed by color alone (always a text label), `prefers-reduced-motion` honoured, and `prefers-color-scheme` supported for both light and dark.
