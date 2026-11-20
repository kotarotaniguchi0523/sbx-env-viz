# sbx-env-viz

Docker Sandboxes（`sbx`）の環境ファイル `sbxenv*.yaml` と、ローカル kit（`./kits/*`）を**オフラインで静的解析**するツールです。複数レイヤーを重ねた最終構成、各値の出所（`file:line`・kit 経路）、セキュリティ境界、設定ミスの診断を、TUI・テキスト・JSON で表示します。

- `sbx` デーモンや Docker が止まっていても動く、Go の単一バイナリ（`CGO_ENABLED=0`）です。
- 表示するのは「宣言の解析」で、稼働中 sandbox の状態ではありません。緑の「安全」表示は出しません。解析できない部分（リモート kit、`:dev` テンプレート、ホスト共通ポリシー、未知の capability）は **Coverage** として常に表示します。
- 対象は `sbx` v0.54.0 の挙動です（[`.github/sbx-version`](.github/sbx-version)）。

## インストール

```sh
go install github.com/kotarotaniguchi0523/sbx-env-viz/cmd/sbx-env-viz@latest
```

## 使い方

パスとフラグは `sbx env create` と同じ形で渡します。

```sh
# ./sbxenv.yaml（なければ ~/.sbxenv.yaml との結合）を解析
sbx-env-viz

# 複数ファイルを重ねる（右ほど後勝ち）。ディレクトリは <dir>/sbxenv.yaml
sbx-env-viz base.sbxenv.yaml local.sbxenv.yaml --env-arg workspace=./app

# CI 向け: 警告以上で終了コード 1
sbx-env-viz . --text --fail-on=warning

# 機械可読な IR（既定で厳格秘匿）
sbx-env-viz . --json > plan.json
```

| フラグ | 意味 |
|---|---|
| `--env-arg NAME=VALUE`, `--env-args-file FILE` | 環境引数（優先順位: 既定値 < 引数ファイル（後ろほど優先）< `--env-arg`） |
| `--kit-arg [KIT.]NAME=VALUE`, `--kit-args-file FILE` | kit 引数の上書き |
| `--name`, `--clone[=false]`, `--skip-host-commands` | `sbx env create` と同じ意味 |
| `--cloud`, `--ttl`, `--on-timeout` | cloud 環境として検査（非対応フィールドを診断） |
| `--text` / `--json` | 出力形式。どちらもなく、stdin と stdout が TTY で `TERM!=dumb` のときは TUI |
| `--redact` | TUI / テキストでも厳格秘匿にする |
| `--fail-on none\|warning\|error` | 終了コード: `0` 正常、`1` 閾値超過、`2` 引数エラー・パース失敗 |

対応する展開式は `${{ env.args.NAME }}`・`${{ env.projectDir }}`・`${{ env.fileDir }}` です（`$${{ … }}` はリテラル、`$VAR` は展開しません）。ローカル kit では `network-policy@1`・`credential@1`・`host-mount@1`・`ssh-agent@1`・`git-identity@1`・`lifecycle@1` を展開します。それ以外の capability も設定を落とさずそのまま表示します。

## TUI

| キー | 動作 |
|---|---|
| `1` `2` `3` / `[` `]` | ビュー切替: Risk（深刻度順）/ Items / Sources |
| `j` `k` | 移動（詳細ペインではスクロール） |
| `h` `l` / `Enter` `Esc` | ペイン移動 / 詳細を開く・戻る |
| `g` | Items のグループ切替（層別 / 境界別 / kit 別） |
| `f` | レイヤー絞り込み（非表示になった警告の件数をヘッダーに表示） |
| `/` | 検索（`Esc` で解除） |
| `?` / `q` | ヘルプ / 終了（終了時に 1 行要約を表示） |

幅 120 桁以上では左右 2 ペイン、それより狭いと 1 ペインで表示します。詳細ペインには、日本語の解説（要はどういう意味か、複数レイヤーでの上書き規則）、上書き履歴、出典 `file:line`、境界の判定根拠を表示します。バッジは ASCII（`[HOST]` `[OPAQUE]` `[RW]` `[PERSIST]` `[NET]` `[CRED]` `[!]` など）です。`NO_COLOR` を設定すると色を使いません。

## ライブラリとして使う

`pkg/sbxenv` は FS・OS・sbx・TUI に依存しない純粋なライブラリです。ファイルの中身は呼び出し側が渡し、ローカル kit は `KitResolver` で供給します。

```go
plan, err := sbxenv.Resolve([]sbxenv.LayerInput{
	{Path: "/work/app/sbxenv.yaml", Content: data, Kind: sbxenv.LayerEnv},
}, sbxenv.ResolveOptions{
	ProjectDir: "/work/app",
	HomeDir:    home,
	EnvArgs:    []string{"channel=beta"},
	Kits:       myKitResolver, // nil ならローカル kit は Coverage に回る
	Redaction:  sbxenv.RedactStrict,
})
if err != nil { /* *sbxenv.ParseError など */ }
for _, d := range plan.Diagnostics {
	fmt.Println(d.Level, d.Code, d.Message)
}
doc, _ := sbxenv.LookupDoc("sandboxOptions.memory") // 日本語の解説と上書き規則
```

`Plan` は許可したフィールドだけをコピーして作ります（Allow-list DTO）。秘匿の方針は次のとおりです。

- 秘密の**実値**は読みません。`secrets.*.value` の直書きは、どのモードでも出力に含めません。
- `--json`（`RedactStrict`）では、`$HOME` を `~` に置き換え、env・引数・コマンド本文を `<redacted>` にします。`op://` 参照はプロバイダーと vault の種別だけ、URL はホストまで残します。疑似秘密が出力・診断・標準エラーに出ないことをテストで確かめています。
- TUI / `--text`（`RedactHome`）は `$HOME` → `~` の短縮だけです。`--redact` で厳格秘匿に切り替えられます。

## 開発

ツールのバージョンは [`mise.toml`](mise.toml) と `mise.lock` で固定しています（Go、hk、pkl、betterleaks、govulncheck、cyclonedx-gomod、uzomuzo、trivy）。

```sh
mise install          # 固定版を入れる
hk install            # pre-commit / pre-push フック（betterleaks・gofmt・go vet・go test -race）
mise run test         # go test -race ./...
mise run audit        # govulncheck → SBOM → uzomuzo
```

## セキュリティ監査の方針

| いつ | 何を | どこで |
|---|---|---|
| commit / push | betterleaks（差分・履歴）、gofmt、go vet、go test -race | `hk.pkl` |
| PR・main への push | テスト、macOS/Linux/Windows のクロスビルド、`govulncheck ./...`、betterleaks（全履歴） | [`ci.yml`](.github/workflows/ci.yml) |
| 毎週 | `cyclonedx-gomod app` で本番ビルド条件の SBOM を作り、`uzomuzo scan --show-transitive --fail-on eol-confirmed` でメンテ停止・EOL を検査。Trivy の結果を `trivy-to-vuls` で vuls レポートに変換し、検査対象のライブラリ数が 0 でないこと、CVE が 0 件であることを確認。`govulncheck` | [`security-audit.yml`](.github/workflows/security-audit.yml) |
| 毎日 | `docker/sbx-releases` に公開から 2 日以上経った新しい安定版が出たら、`sbx env` 系の `--help` 差分、公式 docs の変更、リリースノートをまとめた Issue（ラベル `sbx-update`）を作成 | [`sbx-watch.yml`](.github/workflows/sbx-watch.yml) |

Actions はコミット SHA で固定しています。

## ライセンス

MIT
