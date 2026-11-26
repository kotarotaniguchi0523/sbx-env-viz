---
version: 1
slug: "internal-render-tui"
primary_target: "internal/render/tui"
related_targets: []
---

# TUI (sbxray interactive view)

Scope: the Bubble Tea TUI in `internal/render/tui` (Overview, Risk, Items, Sources, target picker, help). Visitor mode: Operate.

Audience and job: someone running `sbxray --live` checks existing sandboxes against their `sbxenv*.yaml`; on opening they want the whole configuration at a glance, then the detail and source line of any part. Usually an ~80-column terminal on a light background; must also hold at 130+ columns, dark backgrounds and `NO_COLOR`.

User feedback that started this redesign: too much information on one screen; warning tags stuck together (`[!][CRED][HOST]`) are unreadable.

## Direction contract

THESIS: The Overview is a customs declaration under inspection: what the files declare, what `sbx` actually has, and a stamp only where they disagree. It refuses the category default of a dashboard of bordered cards and rows of coloured tag chips.

OWN-WORLD: Ink on the terminal's own paper: no painted backgrounds, ANSI-16 only. Default ink for content, muted ink for labels and context, one official blue for focus, the active tab and keys, and stamp red for findings and drift. Ruled form: column headers underlined, groups separated by space, never boxes. At most one mark per row in a narrow left margin (`✗` high, `!` medium); the long tag list lives in the detail.

STORY: The user sees what is declared, what is running, and where they differ; picks a row; reads why it matters and where it was set; fixes the file or recreates the sandbox.

FIRST VIEWPORT: Line 1: `sbxray`, the target and its state, then numbered tabs with counts. Line 2: muted provenance (files, kits, static or live). Then one summary line of marks and drift counts. Then the manifest table: margin mark, item, declared value, and with live data an `実体（sbx）` column reading `一致`, `照合なし`, or the actual value in stamp red, bold and underlined where it differs (the margin `✗` stays the only mark on the row). A muted `ほか N 行` line ends any clipped list. Bottom: one line of at most six key hints.

FORM: Customs declaration and inspection manifest, candidate 1 of my ordered list (offered as the pick). The user chose it over the dealt direction (candidate 3, a plan-checker's redline sheet). seed:0026d8d08d9f487b (degraded roll: no challengers)

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
