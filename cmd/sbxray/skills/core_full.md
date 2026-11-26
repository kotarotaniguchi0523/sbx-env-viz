## Full Flag Reference

Paths and flags mirror `sbx env create`. Passing a directory reads `<dir>/sbxenv.yaml`. With no path, `./sbxenv.yaml` is merged over `~/.sbxenv.yaml`. Use `--` before a path if a filename matches a subcommand (`help`, `skills`).

### Output & Control Flags

| Flag | Default | Description |
|---|---|---|
| `--json` | `false` | Emit `schemaVersion: "1"` JSON IR (strictly redacted) |
| `--text` | `false` | Emit plain-text summary (default when stdout is not a TTY) |
| `--redact` | `false` | Apply strict redaction to `--text` / TUI as well |
| `--fail-on LEVEL` | `none` | Exit `1` when reached: `none`, `warning`, `error`, `critical`, `high`, `medium`, `low` |
| `--live` | `false` | Also compare against existing `sbx` sandboxes (read-only) |
| `--offline` | `false` | Never invoke `sbx` (already the default for `--json`, `--text`, or explicit paths) |
| `--version` | `false` | Print version, IR schema version, and target `sbx` version |
| `-h`, `--help` | `false` | Show CLI usage |

### `sbx env create` Compatible Flags

| Flag | Repeatable | Description |
|---|---|---|
| `--env-arg NAME=VALUE` | Yes | Environment argument (highest priority) |
| `--env-args-file FILE` | Yes | Load `NAME=VALUE` lines (later files win) |
| `--kit-arg [KIT.]NAME=VALUE` | Yes | Override kit argument |
| `--kit-args-file FILE` | Yes | Load kit argument lines |
| `--name NAME` | No | Override sandbox name |
| `--clone[=true\|false]` | No | Override `workspace.clone` |
| `--skip-host-commands` | No | Treat lifecycle and local stdio MCP commands as skipped |
| `--cloud` | No | Validate for `sbx --cloud` (flags unsupported fields) |
| `--ttl DURATION` | No | Cloud expiration (with --cloud) |
| `--on-timeout ACTION` | No | Cloud expiration action (with --cloud) |

## Expressions & Local Kits

- Supported expressions: `${{ env.args.NAME }}`, `${{ env.projectDir }}`, `${{ env.fileDir }}` (`$${{ ... }}` is literal; `$VAR` is not expanded).
- Local kits (`./kits/*`) expand: `network-policy@1`, `credential@1`, `host-mount@1`, `ssh-agent@1`, `git-identity@1`, `lifecycle@1`.
- Network policy `deny` always overrides `allow` across kits.

## Redaction Guarantee

- Secret values (`secrets.*.value`, string-form `secrets`) and URL credentials (`user:token@`) are never emitted in any mode.
- `--json` and `--redact` additionally redact env values, arg values (including interpolated path/port/domain segments), command bodies, `op://` vault details, and URL paths/queries.

## Troubleshooting Common Exit 2 Errors

- **`no environment file`**: No `./sbxenv.yaml` or `~/.sbxenv.yaml` was found. Pass an explicit file or directory path (e.g. `sbxray path/to/sbxenv.yaml --json`).
- **`multiple YAML documents`**: `sbxenv*.yaml` must contain a single YAML document (`---` document separators are rejected).
- **Filename matches subcommand**: Use `--` before positional paths (e.g. `sbxray --json -- help`).
- **`--text and --json cannot be combined`** / **`--live and --offline cannot be combined`**: Pass only one flag from each pair.
