---
name: live-drift
description: Read-only comparison against running sbx sandboxes with --live
---
# sbxray — Live Sandbox Drift Comparison

Compare declarative `sbxenv*.yaml` files against existing `sbx` sandboxes safely.

## Commands

```sh
# Machine-readable plan + live drift section
sbxray . --json --live

# Human-readable summary + live drift
sbxray . --text --live

# Fail CI / check if drift reaches medium severity or higher
sbxray . --json --live --fail-on=medium
```

## Safety Guarantees

- Runs only read-only `sbx` commands with `--json --no-input`:
  - `sbx ls --json --no-input`
  - `sbx policy ls <name> --json --no-input`
  - `sbx secret ls --sandbox <name> --json --no-input`
  - `sbx mcp ls --json --no-input`
- Secret values (`secrets[].secret`) are never deserialized or read.
- If `sbx` is not installed or stopped, `sbxray` reports the reason in `live.problem` and still outputs the static plan.
- Under `--json` (and `--redact`), every value `sbx` reports (names, agents, paths, ports, hosts, secret names) appears as `<redacted>` in `live.drift[].actual`, since it can carry a hidden argument; the drift `kind` and `title` still say what differs.

## What Is Compared vs. Skipped

- **Compared**: agent, workspaces & mounts (missing/extra), port exposure, network allow/deny rules, secret registration, MCP server registration/status.
- **Not compared** (reported under `live.skipped`): memory, CPU, lifecycle execution history, and mount `RW`/`RO` mode (`sbx ls --json` only emits mount paths).
