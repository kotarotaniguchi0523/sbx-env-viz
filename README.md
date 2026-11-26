<div align="center">

# sbxray

**X-ray vision for Docker Sandboxes (`sbxenv.yaml`)**<br>
Inspect effective config, security boundaries, provenance, and live drift before you launch.

**English** | [日本語](README.ja.md)

[![CI](https://img.shields.io/github/actions/workflow/status/kotarotaniguchi0523/sbxray/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/kotarotaniguchi0523/sbxray/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/kotarotaniguchi0523/sbxray?style=flat-square)](https://github.com/kotarotaniguchi0523/sbxray/releases)
[![Go](https://img.shields.io/badge/Go-1.27.4-00ADD8?style=flat-square&logo=go&logoColor=white)](go.mod)
[![Target sbx](https://img.shields.io/badge/sbx-v0.54.0-2496ED?style=flat-square&logo=docker&logoColor=white)](.github/sbx-version)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue?style=flat-square)](LICENSE)

</div>

---

**sbxray** statically resolves layered Docker Sandboxes environment files (`sbxenv*.yaml`) and local kits (`./kits/*`) completely offline — no running sandbox, Docker, or `sbx` daemon required.

- **Layered resolution & provenance** — Merge `sbxenv*.yaml` layers and trace every value to its exact `file:line` and kit.
- **Security boundary analysis** — Surface host execution, writable mounts, outbound network rules, credentials, SSH agent forwarding, and exposed ports — without misleading "safe" green indicators.
- **Live drift detection** — Compare declarations against existing `sbx` sandboxes using read-only commands without ever reading secret values.
- **Single static binary** — Output as an interactive TUI, plain text, or strictly redacted JSON (`--json`).

```text
╭────────────────────────────────────────────────────────────────────────────────╮
│ sbxray   1:Overview   2:Risk   3:Items   4:Sources                             │
│Host実行2 永続4 RW2 許可3 E0 W5                                                 │
│宣言の静的解析（稼働中 sandbox の状態ではない） env-args: workspace=./web-app (…│
│絞り込み  全て   L1   L2   K1   K2                                              │
│╭──────────────────────────────────────────────────────────────────────────────╮│
││● 一覧 · Risk（24 件）                                                        ││
││ HIGH  [WARNING] 認証情報の差し込み先が未承認                                 ││
││ HIGH  [WARNING] ポートが外部ネットワークに公開される                         ││
││ HIGH  [WARNING] SSH 鍵で何にでも署名できる                                   ││
││ HIGH  [CRED][HOST][OPAQUE] secrets.github = command: gh auth token           ││
││ HIGH  [!] ports[3000/tcp4] = 0.0.0.0:4000 -> sandbox:3000/tcp4               ││
││ HIGH  [HOST][OPAQUE] lifecycle.postCreate[0] = ./hooks/load-secrets.sh       ││
││ HIGH  [!][SSH][HOST] kit:egress/ssh-agent@1/runtime = phase=runtime unrestri…││
││ HIGH  [!][RW][PERSIST] kit:history/host-mount@1//home/agent/.history = /home…││
││ MED   [RW] workspace = ~/proj/web-app                                        ││
││ MED   [CRED] secrets.example = ref: op://Private/Example/credential          ││
││ MED   [NET] kit:egress/network-policy@1/runtime.allow/*.cdn.example.com = ru…││
││ LOW   [!] sandboxOptions.template = ghcr.io/example/common-claude:dev        ││
│╰──────────────────────────────────────────────────────────────────────────────╯│
```

> [!NOTE]
> Targets **`sbx` v0.54.0** ([`.github/sbx-version`](.github/sbx-version)). Items outside static analysis (remote kits, `:dev` templates, host-wide policies, unknown capabilities) are always reported under **Coverage** in `--text` / `--json` and marked `[未展開]` in `4:Sources`.

## Installation

### mise (Recommended)

```sh
mise use -g packslip:github.com/kotarotaniguchi0523/sbxray
```

Verifies Sigstore keyless signatures (`packslip.sigstore.json`) and digests automatically. To pin the signer from the first install:

```sh
packslip install github.com/kotarotaniguchi0523/sbxray --pin ps1_q5dhguvg3md2hsfxcizfdued7a
```

### Nix / Go / Manual

```sh
# Nix (macOS / Linux)
nix run github:kotarotaniguchi0523/sbxray -- --version

# Go
go install github.com/kotarotaniguchi0523/sbxray/cmd/sbxray@latest
```

Pre-built `tar.gz` archives for macOS and Windows (`amd64` / `arm64`) are available on [GitHub Releases](https://github.com/kotarotaniguchi0523/sbxray/releases) with SHA-256 checksums and GitHub build provenance attestations.

## Usage

Paths and flags mirror `sbx env create`. Passing a directory reads `<dir>/sbxenv.yaml`; later files override earlier ones.

```sh
# Interactive TUI (auto-discovers ./sbxenv.yaml, ~/.sbxenv.yaml, and live sandboxes)
sbxray

# Layer multiple files (rightmost wins)
sbxray base.sbxenv.yaml local.sbxenv.yaml --env-arg workspace=./app

# CI gate: exit 1 if any warning or error is found
sbxray . --text --fail-on=warning

# Strictly redacted JSON IR
sbxray . --json > plan.json
```

| Flag | Description |
|---|---|
| `--env-arg NAME=VALUE`, `--env-args-file FILE` | Environment arguments (later files and flags win) |
| `--kit-arg [KIT.]NAME=VALUE`, `--kit-args-file FILE` | Override kit arguments |
| `--name`, `--clone[=true\|false]`, `--skip-host-commands` | Same as `sbx env create` |
| `--cloud`, `--ttl`, `--on-timeout` | Validate against `sbx --cloud` constraints |
| `--text` / `--json` | Output format (defaults to TUI on a TTY) |
| `--redact` | Apply strict redaction to TUI and `--text` |
| `--live` / `--offline` | Force or disable read-only `sbx` live comparison |
| `--fail-on LEVEL` | `none` \| `warning` \| `error` \| `critical` \| `high` \| `medium` \| `low` |

Exit codes: `0` OK, `1` `--fail-on` threshold reached, `2` invalid arguments or parse error.

## Interactive TUI

| View | Description |
|---|---|
| `1:Overview` | Summary cards for config, kits, mounts, MCP & network, credentials, SSH, ports, and host commands |
| `2:Risk` | Diagnostics, risky items, and live drift sorted by severity |
| `3:Items` | All resolved items (`g` toggles grouping by Layer / Boundary / Kit) |
| `4:Sources` | Input files and kits with SHA-256 digests and unexpanded remote markers |

Each detail pane shows official Docker Sandboxes docs, relevant OWASP references, and sbxray's boundary assessment (currently in Japanese), along with merge history and `file:line` provenance. Supports mouse navigation and `NO_COLOR`.

| Key | Action |
|---|---|
| `1`–`4` / `[` `]` | Switch view |
| `j` `k` / `h` `l` | Navigate items / switch panes |
| `Enter` / `Esc` | Open detail / go back |
| `g` / `f` / `/` | Group (`Items`) / filter by layer / search |
| `<` `>` / `s` | Switch target sandbox / sandbox picker |
| `?` / `q` | Help / quit |

## Live Drift & Redaction

- **Read-only live comparison**: When run with no arguments in a terminal (or with `--live`), `sbxray` runs read-only `sbx` list commands (`ls`, `policy ls`, `secret ls`, `mcp ls` with `--json --no-input`) to highlight drift across agents, mounts, ports, network policies, secrets, and MCP servers. Secret values are never read.
- **Strict redaction**: Secret values and embedded URL credentials are never emitted in any mode. `--json` and `--redact` also redact env values, arg values (including interpolated segments), command bodies, `op://` details, and URL paths/queries. Control and BiDi characters are escaped before terminal output.

## For Coding Agents (Built-in Skills)

`sbxray` embeds version-matched documentation and workflows directly in the binary (no web search needed):

```sh
sbxray skills list              # list built-in skills
sbxray skills get core          # core agent workflow & JSON output guide
sbxray skills get core --full   # include full flag reference
```

A lightweight discovery stub is also included at [`skills/sbxray/SKILL.md`](skills/sbxray/SKILL.md).

## Development

```sh
mise install        # install pinned tools
mise run lint       # gofmt, go vet, declscope
mise run test       # go test -race ./...
mise run e2e        # PTY TUI tests
```

See [`DESIGN.md`](DESIGN.md) for architecture details and [`AGENTS.md`](AGENTS.md) for contributor/agent rules.

## License

[MIT](LICENSE)
