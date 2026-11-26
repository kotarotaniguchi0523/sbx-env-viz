# AGENTS.md: sbxray の作業ルール

このリポジトリは **Public** です。設計は [`DESIGN.md`](DESIGN.md)、使い方は [`README.md`](README.md)（日本語版: [`README.ja.md`](README.ja.md)）にあります。このファイルにはエージェント（Claude Code / Codex など）が守るルールだけを書きます。

## してはいけないこと

- **個人名、`/Users/<user>` のような実際のパス、トークンや鍵などの秘密をコミットしない。** テストの入力は一時ディレクトリに作り、例に使うパスは `~/…` か `/work/…` にする。
- **`sbx secret ls --json` の `secrets[].secret` フィールドを読まない・表示しない。** 読み込み用の構造体にこのフィールドを作らない（`internal/live/snapshot.go`）。
- **sbx の状態を変えるコマンドを実行しない。** 実行してよいのは `--json --no-input` を付けた一覧コマンド（`ls`、`policy ls`、`secret ls`、`mcp ls`）だけ。
- **公式ドキュメントや一次資料で確かめられないことを書かない。** 公式の見解（`pkg/sbxenv/docs_ja.go`）と OWASP の項目（`pkg/sbxenv/external_ja.go`）は、出典の節と項目の番号を `pkg/sbxenv/testdata/` の確認済みの一覧に足してから載せる。テストがこの一覧と照合する。
- **Homebrew を使わない。** ツールは `mise.toml`（または Nix）で宣言して入れる。

## CLI・内蔵 Skills・ドキュメントの規約

- **CLI の出力は英語、TUI は日本語で統一する。** `help` / `describe` / `skills` / exit 2 エラー（`code`, `message`, `next_step`, `candidates`）は英語で出力し、CLI の JSON 出力は `SetEscapeHTML(false)` を使う。TUI の画面表示・エラー・解説は日本語で統一し、ターゲット固有エラーや `--live` エラーが画面切替で隠れないようにする。
- **サブコマンドは Noun-Verb 記法（`sbxray <noun> <verb>`）を基本とする**（例: `sbxray skills list` / `sbxray skills get <name>`）。
- **CLI の契約を変えたら同期して更新する。** フラグ・サブコマンド・終了コードを変えたら、`cmd/sbxray/describe.go`、`cmd/sbxray/skills/*.md`（`go:embed` でバイナリ内蔵・YAML Frontmatter の `description` 付き）、`README.md`（英語）、`README.ja.md`（日本語）を揃えて更新する。
- **README と Skills は簡潔に保つ。** 利用者とエージェントに必要な情報だけに絞り、ライブラリとしての使い方・リポジトリ運用ルール・過剰な内部説明は書かない。

## 版の固定と更新

ツール・Actions・Go モジュール・nixpkgs は版を完全固定し、公開から 2 日以上たった版を PR 経由でのみ更新する。手で lock を作り直すときは次を使う。

- `mise.toml` を変えたら `tools/ci/mise-lock.sh --write`、`go.mod` / `go.sum` を変えたら `tools/ci/vendor-hash.sh --write` を実行する（CI はどちらも `--check` で確かめる）。
- `nix flake update` は使わない。`flake.lock` は `tools/ci/flake-lock-update.sh`（公開から 2 日以上たったコミットだけを使う）で更新する。
- 新しいトップレベルファイルやディレクトリを追加したら、`tools/ci/select-checks.py` の分類も確認する。

## 変更の前後に確かめること

```sh
mise run lint          # gofmt、go vet、declscope shrink、declscope（本体と e2e）
mise run test          # go test -race ./...
mise run mutation      # internal/parser を変えたら Gremlins（検出率・網羅率 100%）
mise run e2e           # 実際の端末（PTY）での TUI テスト（画面を変えたら UPDATE_SNAPSHOTS=1）
mise run lint:actions  # workflow を変えたら jactionlint と sisakulint
```

hk のフック（`hk install`）が commit と push のたびに betterleaks・gofmt・go vet・テストを実行する。フックを `--no-verify` で飛ばさない。

## コミットと公開

- コミットメッセージは英語の Conventional Commits（`feat:` `fix:` `build:` `ci:` `docs:` …）で書く。CHANGELOG は、GoReleaser がこの種類で分類して作る。
- タグ（`v*`）を push すると Release が公開され、取り消しにくい。push の前に利用者の確認を取る。署名は必ず `release.yml` から行う（署名者が変わると、署名者を固定した利用者はその版を受け付けない）。
