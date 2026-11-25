# sbx-env-viz

Docker Sandboxes（`sbx`）の環境ファイル `sbxenv*.yaml` と、ローカル kit（`./kits/*`）を**オフラインで静的解析**するツールです。複数レイヤーを重ねた最終構成、各値の出所（`file:line`・kit 経路）、セキュリティ境界、設定ミスの診断を、TUI・テキスト・JSON で表示します。

- `sbx` デーモンや Docker が止まっていても動く、Go の単一バイナリ（`CGO_ENABLED=0`）です。
- 表示するのは「宣言の解析」で、稼働中 sandbox の状態ではありません。緑の「安全」表示は出しません。解析できない部分（リモート kit、`:dev` テンプレート、ホスト共通ポリシー、未知の capability）は `--json` / `--text` の **Coverage** に必ず含めます。TUI では毎回の前提にすぎないため Overview・Risk・ヘッダーには出さず、ファイルに書かれたもの（リモート kit など）だけを `4:Sources` に `[未展開]` として並べます。
- 対象は `sbx` v0.54.0 の挙動です（[`.github/sbx-version`](.github/sbx-version)）。

## インストール

配布物は macOS と Windows（amd64 / arm64）向けの `tar.gz` です。Linux ではソースから入れてください。

### mise（推奨）

```sh
mise use -g packslip:github.com/kotarotaniguchi0523/sbx-env-viz
```

mise の [`packslip:` backend](https://mise.jdx.dev/dev-tools/backends/packslip.html) は、Release に添付された `packslip.sigstore.json` を検証してから展開します。これは各アーカイブのダイジェストを並べた一覧で、このリポジトリの `release.yml` が鍵を持たずに（Sigstore の keyless 署名で）署名しています。署名・透明性ログ・ダイジェスト・サイズのどれかが合わなければインストールは止まります。外部の `cosign` などは要りません。

最初に信頼する署名者を固定したい場合は、次のフィンガープリントを使います（v0.1.0 を検証して得たもの）。

```text
ps1_q5dhguvg3md2hsfxcizfdued7a
```

```sh
packslip install github.com/kotarotaniguchi0523/sbx-env-viz --pin ps1_q5dhguvg3md2hsfxcizfdued7a
```

mise に `minimum_release_age` を設定している場合、公開からその期間が過ぎるまで新しい版は入りません。

### Nix

flake からソースをビルドします（macOS・Linux）。

```sh
nix run github:kotarotaniguchi0523/sbx-env-viz -- --version
nix profile install github:kotarotaniguchi0523/sbx-env-viz/v0.1.0   # タグを指定して固定
```

Nix は署名ではなく内容のハッシュで固定します。`flake.lock` が nixpkgs のコミットと内容ハッシュを、`nix/package.nix` の `vendorHash` が Go の依存一式を固定し、どれか1バイトでも違えばビルドは止まります。依存は nixpkgs だけです。go.mod の Go 1.27.4 が nixpkgs にまだ無い間は、Google の配布物（ハッシュ固定）からビルドするので初回は時間がかかります。nixpkgs が追いつけば自動的にそちらを使います。

### 手動

[Releases](https://github.com/kotarotaniguchi0523/sbx-env-viz/releases) からアーカイブを取得し、checksums と build provenance（GitHub Actions がこのリポジトリからビルドした証明）を確かめます。Windows 10 以降は標準の `tar -xf` で展開できます。

```sh
shasum -a 256 -c sbx-env-viz_<版>_checksums.txt --ignore-missing
gh attestation verify sbx-env-viz_<版>_darwin_arm64.tar.gz -R kotarotaniguchi0523/sbx-env-viz
```

Go があればソースから入れることもできます。

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
| `--fail-on none\|warning\|error\|critical\|high\|medium\|low` | 終了コード: `0` 正常、`1` 閾値超過、`2` 引数エラー・パース失敗。`warning` / `error` は診断だけを数えます。`critical`〜`low` は Risk 画面と同じ尺度で、診断・リスクのある項目・`--live` で見つかったズレのうち最も重いものと比べます。解析範囲外（Coverage）は毎回の前提なので数えません |

対応する展開式は `${{ env.args.NAME }}`・`${{ env.projectDir }}`・`${{ env.fileDir }}` です（`$${{ … }}` はリテラル、`$VAR` は展開しません）。ローカル kit では `network-policy@1`・`credential@1`・`host-mount@1`・`ssh-agent@1`・`git-identity@1`・`lifecycle@1` を展開します。それ以外の capability も設定を落とさずそのまま表示します。

## 自動検出と稼働中の sandbox との比較

ターミナルで引数なしで `sbx-env-viz` を実行すると、次のものを自動で集めて切り替えながら表示します。

- `sbx ls --json`、sandbox ごとの `sbx policy ls <name> --json` と `sbx secret ls --sandbox <name> --json`、`sbx mcp ls --json`（どれも読み取り専用の一覧コマンドで、`--no-input` 付きで実行します。シークレットは名前だけを読み、値は取得しません）
- カレントディレクトリと各 sandbox のワークスペースの近くにある `sbxenv.yaml` / `sbxenv.*.yaml`（`sbxenv.X.local.yaml` は `sbxenv.X.yaml` に重ねます）と `~/.sbxenv.yaml`

宣言の `name`（なければメインのワークスペースのパス）で sandbox と紐付けます。画面上部の「対象」バーに、`稼働中` / `停止中` / `宣言のみ（未作成）` / `宣言ファイル未検出` と `ズレ N` を表示します。`<` `>` かクリックで切り替え、`s` で一覧から選べます。

宣言と実体の両方がある sandbox では、エージェント、ワークスペースとマウント（欠落・追加）、ポート公開、通信の許可と拒否（手動で追加したルール、宣言したのに反映されていないルール）、シークレットの登録有無、MCP サーバーの登録と状態を比べ、`1:Overview` と `2:Risk` に「ズレ」として表示します。詳細ペインでは「宣言（sbxenv.yaml）」と「現在の実体（sbx）」を並べ、作り直しなどの対処を案内します。メモリや CPU、lifecycle の実行履歴のように sbx の一覧から取得できないものは比較せず、比較しなかったことを表示します。

`sbx` がない場合やデーモンが停止している場合は、設定ファイルだけを解析します。パスを指定したとき、`--text` / `--json` のとき、`--offline` のときは従来どおり sbx を実行しません。`--live` を付けると、それらの出力でも比較結果を加えます（`--json` では `live` キー）。

各コマンドの JSON は sbx v0.54.0 の実際の出力の構造に合わせて読みます。`sbx secret ls` の各行にある `secret` フィールドは読み込み用の構造体に対応するフィールドを持たないため、値はプログラムに取り込まれません（名前・スコープ・種類だけを読みます）。`sbx ls --json` はマウントのパスだけを出力し、読み取り専用かどうかは出力しないため、書き込みの可否（RW / RO）の違いは比較しません。

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
| `<` `>` / `s` | 対象の sandbox を切替 / 一覧から選ぶ（自動検出時） |
| `?` / `q` | ヘルプ / 終了（終了時に 1 行要約を表示） |

| マウス | 動作 |
|---|---|
| タブ・チップのクリック | 画面（`1:Overview` など）、グループ（層別 / 境界別 / kit 別）、絞り込み（全て / `L1` / `K1` …）を切替 |
| Overview のカードの項目のクリック | その項目の Items または Risk の詳細へ移動 |
| 一覧の行のクリック | その項目を選択（狭い画面では詳細を開き、`← 戻る` で一覧へ） |
| ペインのクリック | フォーカスを移動（フォーカス中の枠はアクセント色と `●` で表示） |
| ホイール | ポインタの下のペインをスクロール（一覧では選択を移動） |

起動すると Overview を表示します。基本構成、適用される Kits（中身を確認済みか、リモートか）、ワークスペース・マウント、MCP と通信の許可・禁止、資格情報・SSH・ポート・ホスト実行を角丸のカードで並べ、項目を選ぶと詳細へ移動します。

画面は、タイトルバー（タブ）、サマリーのチップ（`Host実行` `Host永続` `RWマウント` `外への許可` `エラー` `警告`）、グループと絞り込みのチップ、角丸の枠のペイン、キーのバッジを並べたステータスバーで構成します。幅 120 桁以上では一覧と詳細を左右に並べ、それより狭いと 1 ペインで表示します。詳細ペインはすべて日本語です。「公式の見解」（Docker Sandboxes のドキュメントと sandbox-kit-spec が実際に述べているセキュリティ上の前提・警告・推奨と、各記述の出典となる節の URL。引用する節のアンカーは公開ページから記録した一覧と照合するテストがあります。記載がなければ「公式ドキュメントに特段の警告記載なし」）、重要な項目では「外部セキュリティ基準の観点（OWASP 等）」（OWASP MCP Top 10〔ベータ版〕、OWASP Top 10 for Agentic Applications 2026、OWASP AI Agent Security Cheat Sheet のうち、原文で確認した項目番号・名称と、なぜ危険とされるかの 1〜2 行、出典 URL）、そして「このツールの判定」（隔離境界に基づく独自基準の危険度とその理由）を分けて表示します。引用した OWASP の項目は確認済みの一覧（`pkg/sbxenv/testdata/external-standards.txt`）と照合するテストがあります。ほかに、解説（要はどういう意味か、複数レイヤーでの上書き規則）、上書きの履歴、設定した場所（ファイルと行番号）、sandbox の外との関わり、指摘と対処、未展開の理由を見出し付きで表示し、長いパスは `/` の直後で折り返します。重要度とバッジ（`[HOST]` `[OPAQUE]` `[RW]` `[PERSIST]` `[NET]` `[CRED]` `[!]` など）は色分けしますが、「安全」を示す緑は使いません。`NO_COLOR` を設定すると色を使わず、枠・太字・反転表示で同じ構造を保ちます。

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

ツールはすべて [`mise.toml`](mise.toml) で完全一致の版に固定し、`mise.lock` にプラットフォームごとのハッシュと署名者を記録しています。Homebrew は使いません。

```sh
mise install          # mise.lock どおりの版を入れる（locked = true）
hk install            # pre-commit / pre-push フック
mise run test         # go test -race ./...
mise run e2e          # 実 PTY での TUI テスト（UPDATE_SNAPSHOTS=1 でスナップショット更新）
mise run lint         # gofmt と go vet
mise run lint:actions # GitHub Actions を jactionlint（shellcheck つき）と sisakulint で検査
mise run audit        # govulncheck → SBOM → uzomuzo
mise run snapshot     # 公開せずにリリース用アーカイブを作る（GoReleaser）
betterleaks git --redact --no-banner .   # 全履歴の秘密情報スキャン
nix flake check       # Nix でのビルドと --version の確認
```

TUI は `e2e/tui` で、ビルドした実バイナリを実 PTY 上で操作して検証します（[Microsoft tui-test](https://github.com/microsoft/tui-test) の Go バインディング。独立した Go モジュールで `go.mod` / `go.sum` に固定）。80 桁と 130 桁の描画スナップショット（`e2e/tui/__snapshots__/`）、`1`/`2`/`3`・`g`・`f`・`/`・`?`・`q` の操作、日本語の文字幅、`NO_COLOR`、制御文字のエスケープを確かめます。

テストは古典派のブラックボックステストです（AAA 形式、モックなし）。公開 API（`pkg/sbxenv`）とビルドした実バイナリだけを相手にします。入力は、ケースごとに作る一時ディレクトリ（HOME・プロジェクト・kit）の実ファイルです。全ケースを `t.Parallel()` で並列に動かすため、ケース間の依存があれば表に出ます。

エージェント（Claude Code など）向けの作業ルールは [`AGENTS.md`](AGENTS.md) にあります。

## 自動チェックと監査

| いつ | 何を | どこで |
|---|---|---|
| commit / push | betterleaks（差分・push する履歴）、gofmt、go vet、go test -race、workflow を変えたときは jactionlint と sisakulint | [`hk.pkl`](hk.pkl) |
| PR・main への push | macOS・Linux でのテストと実 PTY の TUI テスト、6 プラットフォームのクロスビルド、`nix flake check`（macOS・Linux）と `vendorHash` の確認、`mise.lock` の整合、jactionlint・sisakulint（`--online`）、GoReleaser の `--snapshot`、`govulncheck`、betterleaks（全履歴） | [`ci.yml`](.github/workflows/ci.yml) |
| タグ `v*` の push | テスト後に GoReleaser で macOS・Windows 向けの 4 種類の `tar.gz`、checksums、SBOM（CycloneDX）、ソースアーカイブを作って Release を公開する。続く書き込み権限のないジョブで `jdx/packslip` が各アーカイブの build provenance を付け、アーカイブ一覧に keyless 署名した `packslip.sigstore.json` を作り、別ジョブがそれを Release に添付する | [`release.yml`](.github/workflows/release.yml)、[`.goreleaser.yaml`](.goreleaser.yaml) |
| 毎週 | 本番ビルド条件の SBOM を `uzomuzo` でメンテ停止・EOL 検査、Trivy の結果を vuls レポートに変換して CVE 0 件を確認、`govulncheck`、秘匿のファジング（5 分） | [`security-audit.yml`](.github/workflows/security-audit.yml) |
| 毎週 | `flake.lock` を、公開から 2 日以上経った nixpkgs のコミットへ更新する PR を作る | [`flake-lock.yml`](.github/workflows/flake-lock.yml) |
| 毎日 | `docker/sbx-releases` に公開から 2 日以上経った新しい安定版が出たら、`sbx env` 系の `--help` 差分、公式 docs の変更、リリースノートをまとめた Issue（ラベル `sbx-update`）を作る | [`sbx-watch.yml`](.github/workflows/sbx-watch.yml) |

## リポジトリの運用ルール

[mac-host-setup](https://github.com/kotarotaniguchi0523/mac-host-setup) と同じルールです。

- **版の固定と 2 日ルール**: ツール・Actions・Go モジュール・nixpkgs はすべて完全一致で固定し（Actions はコミット SHA）、新しい版は公開から 2 日以上経ってから採用します（mise の `minimum_release_age`、Renovate の `minimumReleaseAge`、`flake-lock.yml`）。
- **更新は PR だけ**: Renovate が mise のツール・Actions・Go モジュールをまとめて PR にし、minor / patch は必須チェックが通れば自動でマージします。脆弱性アラート由来の更新は待たずに PR を作ります。Renovate が再生成できない `mise.lock` と Nix の `vendorHash` は、[`relock.yml`](.github/workflows/relock.yml) が PR ブランチで作り直し、1 つのコミットで push します（途中で Renovate が push していたら上書きしません）。
- **main の保護**: 必須チェックが通るまでマージできず、履歴は直線（merge commit なし。squash か rebase）、force push と削除は禁止です。マージ後のブランチは自動で削除します。
- **自動 PR の前提**: `flake-lock.yml` が PR を作って自動マージを予約するため、Settings → Actions の「Allow GitHub Actions to create and approve pull requests」と、Settings → General の「Allow auto-merge」を有効にしています。
- **Actions の許可**: GitHub 製のほかは、`jdx/mise-action`・`jdx/packslip`・`cachix/install-nix-action` だけを許可し、すべてコミット SHA での指定を必須にしています。`GITHUB_TOKEN` の既定の権限は読み取りのみです。
- **キャッシュ**: Go のモジュールとビルド結果は [`go-cache`](.github/actions/go-cache/action.yml)（`actions/cache`）で全ジョブが復元し、保存は main への push のときだけです。PR は main のキャッシュを読めますが、main やリリースが後で読むキャッシュを書けません（キャッシュ汚染の防止）。リリース用アーカイブを作るジョブ（`release.yml`、GoReleaser の snapshot）はキャッシュを使いません。
- **公開リポジトリ**: 個人名や `/Users/<user>` のような実パス、秘密情報はコミットしません。betterleaks が commit・push・CI のたびに検査します。

## ライセンス

MIT
