# AGENTS.md: sbxray の作業ルール

このリポジトリは **Public** です。設計は [`DESIGN.md`](DESIGN.md)、使い方と自動チェックの一覧は [`README.md`](README.md) にあります。このファイルにはエージェント（Claude Code など）が守るルールだけを書きます。

## してはいけないこと

- **個人名、`/Users/<user>` のような実際のパス、トークンや鍵などの秘密をコミットしない。** テストの入力は一時ディレクトリに作り、例に使うパスは `~/…` か `/work/…` にする。
- **`sbx secret ls --json` の `secrets[].secret` フィールドを読まない・表示しない。** 読み込み用の構造体にこのフィールドを作らない（`internal/live/snapshot.go`）。
- **sbx の状態を変えるコマンドを実行しない。** 実行してよいのは `--json --no-input` を付けた一覧コマンド（`ls`、`policy ls`、`secret ls`、`mcp ls`）だけ。
- **公式ドキュメントや一次資料で確かめられないことを書かない。** 公式の見解（`pkg/sbxenv/docs_ja.go`）と OWASP の項目（`pkg/sbxenv/external_ja.go`）は、出典の節と項目の番号を `pkg/sbxenv/testdata/` の確認済みの一覧に足してから載せる。テストがこの一覧と照合する。
- **Homebrew を使わない。** ツールは `mise.toml`（または Nix）で宣言して入れる。

## 版の固定と更新

ルールは README の「[リポジトリの運用ルール](README.md#リポジトリの運用ルール)」のとおり（版の完全な固定、2 日ルール、更新は PR だけで行う）。手で lock を作り直すときは次を使う。

- `mise.toml` を変えたら `tools/ci/mise-lock.sh --write`、`go.mod` / `go.sum` を変えたら `tools/ci/vendor-hash.sh --write` を実行する（CI はどちらも `--check` で確かめる）。
- `nix flake update` は使わない。`flake.lock` は `tools/ci/flake-lock-update.sh`（公開から 2 日以上たったコミットだけを使う）で更新する。

## 変更の前後に確かめること

```sh
mise run lint          # gofmt と go vet
mise run test          # go test -race ./...
mise run e2e           # 実際の端末（PTY）での TUI テスト（画面を変えたら UPDATE_SNAPSHOTS=1）
mise run lint:actions  # workflow を変えたら jactionlint と sisakulint
```

hk のフック（`hk install`）が commit と push のたびに betterleaks・gofmt・go vet・テストを実行する。フックを `--no-verify` で飛ばさない。

## コミットと公開

- コミットメッセージは英語の Conventional Commits（`feat:` `fix:` `build:` `ci:` `docs:` …）で書く。CHANGELOG は、GoReleaser がこの種類で分類して作る。
- タグ（`v*`）を push すると Release が公開され、取り消しにくい。push の前に利用者の確認を取る。署名は必ず `release.yml` から行う（署名者が変わると、署名者を固定した利用者はその版を受け付けない）。
