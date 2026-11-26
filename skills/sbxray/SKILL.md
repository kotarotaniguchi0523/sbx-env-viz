---
name: sbxray
description: Inspect Docker Sandboxes (sbxenv.yaml) effective configuration, security boundaries, provenance, and live drift using sbxray.
---

# sbxray

Use the locally installed `sbxray` binary to inspect `sbxenv*.yaml` files and local kits (`./kits/*`). Do not guess flags or search the web — load exact, version-matched instructions directly from the binary:

```sh
sbxray skills get core          # Core agent workflow and JSON output guide
sbxray skills get core --full   # Full flag and capability reference
sbxray skills list              # List all built-in skills (ci, live-drift, etc.)
```

Always pass `--json` (preferred) or `--text` when running `sbxray` as an agent so it never launches the interactive TUI.
