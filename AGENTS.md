# AGENTS.md: sbx-env-viz の作業ルール

このリポジトリは **Public** です。設計は [`DESIGN.md`](DESIGN.md)、使い方と自動チェックの一覧は [`README.md`](README.md) にあります。ここにはエージェント（Claude Code など）が守るルールだけを書きます。

## 禁止

- **個人名、`/Users/<user>` のような実パス、トークンや鍵などの秘密をコミットしない。** テストの入力は一時ディレクトリに作り、例のパスは `~/…` か `/work/…` にする。
- **`sbx secret ls --json` の `secrets[].secret` フィールドを読まない・表示しない。** 読み込み用の構造体にこのフィールドを作らない（`internal/live/snapshot.go`）。
- **sbx を書き換えるコマンドを実行しない。** 実行してよいのは `--json --no-input` 付きの一覧コマンド（`ls`、`policy ls`、`secret ls`、`mcp ls`）だけ。
- **公式ドキュメントや一次資料で確認できないことを書かない。** 公式の見解（`pkg/sbxenv/docs_ja.go`）と OWASP の項目（`pkg/sbxenv/external_ja.go`）は、出典の節・項目番号を `pkg/sbxenv/testdata/` の確認済み一覧に足してから載せる。テストが照合する。
- **Homebrew を使わない。** ツールは `mise.toml`（または Nix）で宣言して入れる。

## 版の固定と更新

ルールは README の「[リポジトリの運用ルール](README.md#リポジトリの運用ルール)」のとおり（完全一致の固定、2 日ルール、更新は PR だけ）。手で lock を作り直すときは次を使う。

- `mise.toml` を変えたら `tools/ci/mise-lock.sh --write`、`go.mod` / `go.sum` を変えたら `tools/ci/vendor-hash.sh --write`（CI はどちらも `--check`）。
- `nix flake update` は使わない。`flake.lock` は `tools/ci/flake-lock-update.sh`（2 日以上前のコミットだけ）で更新する。

## 変更の前後に確かめること

```sh
mise run lint          # gofmt と go vet
mise run test          # go test -race ./...
mise run e2e           # 実 PTY の TUI テスト（画面を変えたら UPDATE_SNAPSHOTS=1）
mise run lint:actions  # workflow を変えたら jactionlint と sisakulint
```

hk のフック（`hk install`）が commit と push のたびに betterleaks・gofmt・go vet・テストを実行する。フックを `--no-verify` で飛ばさない。

## コミットと公開

- コミットメッセージは英語の Conventional Commits（`feat:` `fix:` `build:` `ci:` `docs:` …）。CHANGELOG は GoReleaser がこれを分類して作る。
- タグ（`v*`）の push は Release を公開して取り消しにくいので、利用者の確認を取ってから行う。署名は必ず `release.yml` から行う（署名者が変わると、固定した利用者がその版を拒否する）。
