# sbxray — Core Workflow for Coding Agents

`sbxray` statically inspects Docker Sandboxes (`sbxenv*.yaml` and `./kits/*`) offline and optionally compares declarations against running `sbx` sandboxes using read-only commands.

## Rules for Agents

1. **Always use `--json` (preferred) or `--text`**. Never launch the interactive TUI (no-arg TTY mode).
2. **Never run mutating `sbx` commands** when using `sbxray` for inspection. `sbxray --live` only runs read-only list commands (`--json --no-input`) and never reads secret values.
3. **Trust exit codes**:
   - `0`: success (threshold not reached)
   - `1`: `--fail-on` threshold reached
   - `2`: invalid arguments, missing file, or YAML parse error

## Standard Inspection Loop

```sh
# 1. Inspect the current project as strictly redacted JSON (safe to read in full)
sbxray --json

# 2. Inspect specific layered files (rightmost file wins)
sbxray base.sbxenv.yaml local.sbxenv.yaml --env-arg workspace=./app --json

# 3. Gate on warnings or risk severity
sbxray . --text --fail-on=warning
sbxray . --json --fail-on=high

# 4. Compare declarations against running sandboxes (read-only)
sbxray . --json --live
```

## Reading `--json` Output

Key top-level fields in `schemaVersion: "1"`:

- `meta`: `schemaVersion`, `targetSbxVersion`, `redaction` (`strict`), `sources[]` (id, path, sha256)
- `summary`: counts (`hostExec`, `hostPersistent`, `rwMounts`, `egressAllows`, `errors`, `warnings`)
- `diagnostics[]`: `level` (`error` | `warning` | `info`), `code`, `message`, `origin`
- `items[]`: resolved effective settings with `id`, `value`, `severity`, `badges`, `origins[]`, `history[]`
- `coverage[]`: items outside static analysis (remote OCI kits, `:dev` templates, host-wide policies, unknown capabilities). Always mention these as unverified scope — never report a config as "completely safe".
- `live` (only with `--live`): `state`, `sandbox`, `drift[]`, `checked[]`, `skipped[]`, `problem`

## Need More Detail?

- Full flag & workflow reference: `sbxray skills get core --full`
- CI gating patterns: `sbxray skills get ci`
- Live drift comparison: `sbxray skills get live-drift`
- List all built-in skills: `sbxray skills list`
