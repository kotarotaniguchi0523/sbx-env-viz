---
name: ci
description: CI and pull request gating with --fail-on, --redact, and --cloud
---
# sbxray — CI & Pull Request Gating

Use `sbxray` in CI or automated review to fail fast on misconfigurations and high-risk boundary changes without needing Docker or `sbx`.

## Recommended Commands

```sh
# Fail on any warning or error diagnostic
sbxray . --text --fail-on=warning --redact

# Fail on high or critical risk items (diagnostics + risky boundaries)
sbxray . --json --fail-on=high

# Validate cloud sandbox compatibility
sbxray . --cloud --text --fail-on=error

# Validate layered environments with argument files
sbxray base.sbxenv.yaml prod.sbxenv.yaml --env-args-file prod.args --text --fail-on=warning --redact
```

## Choosing `--fail-on`

| Level | Evaluates | Typical Use |
|---|---|---|
| `error` | Diagnostics (`error` only) | Block invalid configs that `sbx` will reject |
| `warning` | Diagnostics (`warning` + `error`) | Standard PR gate for security warnings |
| `critical` / `high` / `medium` / `low` | Unified Risk scale (diagnostics + risky items + `--live` drift) | Enforce boundary budgets (e.g. block host exec or `0.0.0.0` ports) |

Coverage gaps (such as remote OCI kits or `:dev` templates) never trip `--fail-on` because they are premises of static analysis, not findings.
