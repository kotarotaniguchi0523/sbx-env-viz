# sbx-env-viz

Docker Sandboxes（`sbx`）の環境ファイル `sbxenv*.yaml` と、ローカル kit（`./kits/*`）を**オフラインで静的解析**するツールです。複数レイヤーを重ねた最終構成、各値の出所（`file:line`・kit 経路）、セキュリティ境界、設定ミスの診断を、TUI・テキスト・JSON で表示します。

- `sbx` デーモンや Docker が止まっていても動く、Go の単一バイナリ（`CGO_ENABLED=0`）です。
- 表示するのは「宣言の解析」で、稼働中 sandbox の状態ではありません。緑の「安全」表示は出しません。解析できない部分（リモート kit、`:dev` テンプレート、ホスト共通ポリシー、未知の capability）は `--json` / `--text` の **Coverage** に必ず含めます。TUI では毎回の前提にすぎないため Overview・Risk・ヘッダーには出さず、ファイルに書かれたもの（リモート kit など）だけを `4:Sources` に `[未展開]` として並べます。
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
| `1` `2` `3` `4` / `[` `]` | 画面切替: Overview（全体のカード）/ Risk（深刻度順）/ Items / Sources |
| `Enter`（Overview） | 選んだ項目の Items または Risk の詳細へ移動 |
| `j` `k` | 移動（詳細ペインではスクロール） |
| `h` `l` / `Enter` `Esc` | ペイン移動 / 詳細を開く・戻る |
| `g` | Items のグループ切替（層別 / 境界別 / kit 別） |
| `f` | レイヤー絞り込み（非表示になった警告の件数をヘッダーに表示） |
| `/` | 検索（`Esc` で解除） |
| `?` / `q` | ヘルプ / 終了（終了時に 1 行要約を表示） |

| マウス | 動作 |
|---|---|
| タブ・チップのクリック | 画面（`1:Overview` など）、グループ（層別 / 境界別 / kit 別）、絞り込み（全て / `L1` / `K1` …）を切替 |
| Overview のカードの項目のクリック | その項目の Items または Risk の詳細へ移動 |
| 一覧の行のクリック | その項目を選択（狭い画面では詳細を開き、`← 戻る` で一覧へ） |
| ペインのクリック | フォーカスを移動（フォーカス中の枠はアクセント色と `●` で表示） |
| ホイール | ポインタの下のペインをスクロール（一覧では選択を移動） |

起動すると Overview を表示します。基本構成、適用される Kits（中身を確認済みか、リモートか）、ワークスペース・マウント、MCP と通信の許可・禁止、資格情報・SSH・ポート・ホスト実行を角丸のカードで並べ、項目を選ぶと詳細へ移動します。

画面は、タイトルバー（タブ）、サマリーのチップ（`Host実行` `Host永続` `RWマウント` `外への許可` `エラー` `警告`）、グループと絞り込みのチップ、角丸の枠のペイン、キーのバッジを並べたステータスバーで構成します。幅 120 桁以上では一覧と詳細を左右に並べ、それより狭いと 1 ペインで表示します。詳細ペインはすべて日本語です。「公式の見解」（Docker Sandboxes のドキュメントと sandbox-kit-spec が実際に述べているセキュリティ上の前提・警告・推奨と、その出典 URL。記載がなければ「公式ドキュメントに特段の警告記載なし」）と、「このツールの判定」（隔離境界に基づく独自基準の危険度とその理由）を分けて表示します。ほかに、解説（要はどういう意味か、複数レイヤーでの上書き規則）、上書きの履歴、設定した場所（ファイルと行番号）、sandbox の外との関わり、指摘と対処、未展開の理由を見出し付きで表示し、長いパスは `/` の直後で折り返します。重要度とバッジ（`[HOST]` `[OPAQUE]` `[RW]` `[PERSIST]` `[NET]` `[CRED]` `[!]` など）は色分けしますが、「安全」を示す緑は使いません。`NO_COLOR` を設定すると色を使わず、枠・太字・反転表示で同じ構造を保ちます。

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

- 秘密の**実値**は読みません。`secrets.*.value` の直書きと、`secrets` に文字列だけを書いた値は、どのモードでも出力に含めません。URL や kit 参照に埋め込まれた認証情報（`user:token@`）も、どのモードでも `<credentials>@` に置き換えます。
- `--json`（`RedactStrict`）が隠すのは「値」です。env の値、`--env-arg`・kit 引数の値、コマンド本文、`op://` 参照の中身（プロバイダーと vault の種別だけ残す）、URL のパスとクエリ（ホストまで残す）が対象です。引数の値が `${{ env.args.X }}` や `${{ kit.args.X }}` 経由で `workspace`・ポート・通信先ドメイン・host-mount パスなどに入った場合も、その部分だけを `<redacted>` にします（例: `~/proj/<redacted>`）。ファイルに直接書かれたパス・ポート・ドメイン・名前は、レビューに必要な構造なので表示します。値が出力・診断・標準エラーに出ないことを、テストとファジング（毎週 5 分）で確かめています。
- TUI / `--text`（`RedactHome`）は `$HOME` → `~` の短縮だけです。`--redact` で厳格秘匿に切り替えられます。
- 端末に出す前に、値に含まれる制御文字・改行・双方向制御文字を `\x1b` `\n` `\u202e` のようなエスケープにします。解析対象のファイルが画面を操作したり、出典行を偽装したりできないようにするためです。

### 診断の重さの考え方

- `error`: sbx が拒否する、または仕様上必ず失敗する構成です（未知の必須 capability、重複キー、未知フィールド、capability の文法違反、host-mount のパス衝突など）。
- `warning`: 動くかもしれないが危険、または見えない状態次第で失敗する構成です。たとえば `bindings` がない credential@1 は、ホストの `~/.config/sbx/credentials.yaml` に承認が既にあれば動くので、`warning` にしています。
- 通信許可は kit をまたいで deny が優先します。deny された許可は「外への許可」に数えず、`denied by` を表示します。

## 開発

ツールのバージョンは [`mise.toml`](mise.toml) と `mise.lock` で固定しています（Go、hk、pkl、betterleaks、govulncheck、cyclonedx-gomod、uzomuzo、trivy）。

```sh
mise install          # 固定版を入れる
hk install            # pre-commit / pre-push フック（betterleaks・gofmt・go vet・go test -race）
mise run test         # go test -race ./...
mise run audit        # govulncheck → SBOM → uzomuzo
```

TUI は `e2e/tui` で、ビルドした実バイナリを実 PTY 上で操作して検証します（[Microsoft tui-test](https://github.com/microsoft/tui-test) の Go バインディング。独立した Go モジュールで `go.mod` / `go.sum` に固定）。80 桁と 130 桁の描画スナップショット（`e2e/tui/__snapshots__/`）、`1`/`2`/`3`・`g`・`f`・`/`・`?`・`q` の操作、日本語の文字幅、`NO_COLOR`、制御文字のエスケープを確かめます。

```sh
mise run e2e                                   # 実 PTY テスト
UPDATE_SNAPSHOTS=1 mise run e2e                # 画面を意図して変えたときにスナップショットを更新
```

テストは古典派のブラックボックステストです（AAA 形式、モックなし）。公開 API（`pkg/sbxenv`）とビルドした実バイナリだけを相手にします。入力は、ケースごとに作る一時ディレクトリ（HOME・プロジェクト・kit）の実ファイルです。全ケースを `t.Parallel()` で並列に動かすため、ケース間の依存があれば表に出ます。

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
