# Product

<!-- impeccable:product-schema 1 -->

## Platform

Terminal UI (Bubble Tea v2 / Lip Gloss v2), plus plain text and JSON output. Not a web or native app.

## Users

The primary user runs `sbxray --live` to check sandboxes that already exist: they compare what the `sbxenv*.yaml` files declare with what `sbx` actually created, and look for drift. Developers checking their own file before `sbx env create`, and reviewers reading someone else's file, use the same screens.

## Product Purpose

sbxray resolves layered Docker Sandboxes environment files (`sbxenv*.yaml`) and local kits offline, and shows the effective configuration, the security boundaries it crosses, where every value came from, and how a running sandbox drifts from its declaration. Success: within the first few seconds of opening the TUI, the user has the whole picture of the configuration (name, agent, kits, mounts, network, credentials, host execution), and can reach the detail and the source line of any part of it.

## Positioning

Fully offline, read-only static resolution with `file:line` provenance and live comparison through read-only `sbx` list commands. It never reads secret values and never shows a green "safe" state.

## Operating Context

- Used in a terminal, often about 80 columns wide (split panes), on a light background. Must also work at 130+ columns, on dark backgrounds, and under `NO_COLOR`.
- UI text in the TUI is Japanese (wide CJK glyphs); CLI output is English.
- Keyboard first, with mouse clicks and wheel scrolling.
- Screens: Overview (the default), Risk, Items, Sources, plus a target picker when several sandboxes are discovered.

## Capabilities and Constraints

- Strict redaction (`--redact`) must hold on every screen.
- The risk level is the tool's own yardstick, kept separate from the official Docker documentation's view and from OWASP references.
- Static analysis does not see remote kits, built-in agents, host-wide `sbx` policy or stored secrets; these are stated, not hidden.

## Product Principles

1. The whole picture first; detail on demand.
2. Never imply safety: no green "safe" state, and gaps in the analysis are stated.
3. Every value is traceable to its file, line, and kit.
4. Read-only: sbxray never changes a sandbox and never reads secret values.

## Accessibility & Inclusion

Meaning must never depend on colour alone: it has to read under `NO_COLOR`, with 16-colour palettes, and on light and dark backgrounds.
