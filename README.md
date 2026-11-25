# sbxray

sbxray は Docker Sandboxes（`sbx`）の環境ファイル `sbxenv*.yaml` とローカルの kit（`./kits/*`）を読み、sandbox を作る前に中身を確かめるためのツールです。名前は「sandbox の X 線写真」から付けました。sandbox を起動せず、ネットワークにもつながずにファイルだけを解析します。

次のことが分かります。

- 複数のファイル（レイヤー）を重ねたあとの最終的な設定
- 各値をどのファイルの何行目、どの kit から設定したか
- ホストのファイルやネットワークにどこまで触れられるか（セキュリティ上の境界）
- 設定の誤りと、その直し方

結果はターミナルの対話画面（TUI）、テキスト、JSON のどれかで表示します。

- Go の単一バイナリ（`CGO_ENABLED=0`）で、`sbx` のデーモンや Docker が止まっていても動きます。
- 表示するのはファイルに書かれた宣言の解析結果で、稼働中の sandbox の状態ではありません。「安全」を示す緑の表示は出しません。
- 解析できない部分（リモートの kit、`:dev` テンプレート、ホスト共通のポリシー、未知の capability）は、`--json` と `--text` の出力の **Coverage**（解析範囲の外にあるもの）に毎回載せます。TUI ではこれらが毎回の前提にすぎないため、Overview・Risk・ヘッダーには出しません。ファイルに書かれたもの（リモートの kit など）だけを `4:Sources` 画面に `[未展開]` として並べます。
- `sbx` v0.54.0 の挙動を前提にしています（[`.github/sbx-version`](.github/sbx-version)）。

## インストール

配布しているのは macOS と Windows（amd64 / arm64）向けの `tar.gz` です。Linux ではソースからビルドしてください。

### mise で入れる（おすすめ）

```sh
mise use -g packslip:github.com/kotarotaniguchi0523/sbxray
```

mise の [`packslip:` backend](https://mise.jdx.dev/dev-tools/backends/packslip.html) は、Release に添付した `packslip.sigstore.json` を検証してからアーカイブを展開します。このファイルは各アーカイブのダイジェスト（内容のハッシュ）の一覧で、このリポジトリの `release.yml` が Sigstore の keyless 署名（鍵を保管しない署名）で署名しています。署名・透明性ログ・ダイジェスト・サイズのどれかが合わなければ、インストールは止まります。`cosign` などのツールを別に入れる必要はありません。

信頼する署名者を最初から固定したいときは、次のフィンガープリントを使います。v0.1.0 と v0.2.0 を `packslip pin` で検証して得た値で、リポジトリの名前を変えたあとも同じです。

```text
ps1_q5dhguvg3md2hsfxcizfdued7a
```

```sh
packslip install github.com/kotarotaniguchi0523/sbxray --pin ps1_q5dhguvg3md2hsfxcizfdued7a
```

mise に `minimum_release_age` を設定している場合、新しい版は公開からその期間が過ぎるまで入りません。

### Nix で入れる

flake からソースをビルドします（macOS・Linux）。

```sh
nix run github:kotarotaniguchi0523/sbxray -- --version
nix profile install github:kotarotaniguchi0523/sbxray/v0.2.0   # タグを指定して版を固定する
```

Nix では署名ではなく内容のハッシュで中身を固定します。`flake.lock` が nixpkgs のコミットと内容のハッシュを、`nix/package.nix` の `vendorHash` が Go の依存一式を固定しており、1 バイトでも違えばビルドは止まります。flake の入力は nixpkgs だけです。go.mod が指定する Go 1.27.4 が nixpkgs にまだないあいだは、Google が配布する Go のソース（ハッシュで固定）からビルドするため、初回は時間がかかります。nixpkgs に入れば、自動でそちらを使います。

### 手動で入れる

[Releases](https://github.com/kotarotaniguchi0523/sbxray/releases) からアーカイブをダウンロードし、チェックサムと build provenance（GitHub Actions がこのリポジトリのソースからビルドしたことの証明）を確かめます。Windows 10 以降は標準の `tar -xf` で展開できます。

```sh
shasum -a 256 -c sbxray_<版>_checksums.txt --ignore-missing
gh attestation verify sbxray_<版>_darwin_arm64.tar.gz -R kotarotaniguchi0523/sbxray
```

Go があれば、ソースから入れることもできます。

```sh
go install github.com/kotarotaniguchi0523/sbxray/cmd/sbxray@latest
```

## 使い方

パスとフラグは `sbx env create` と同じ形で渡します。

```sh
# ./sbxenv.yaml（なければ ~/.sbxenv.yaml と重ねたもの）を解析する
sbxray

# 複数のファイルを重ねる（右にあるファイルほど優先）。ディレクトリを渡すと <dir>/sbxenv.yaml を読む
sbxray base.sbxenv.yaml local.sbxenv.yaml --env-arg workspace=./app

# CI 向け: 警告かエラーが 1 件でもあれば終了コード 1 で終わる
sbxray . --text --fail-on=warning

# プログラムで読むための JSON（既定で値を厳しく伏せる）
sbxray . --json > plan.json
```

| フラグ | 意味 |
|---|---|
| `--env-arg NAME=VALUE`, `--env-args-file FILE` | 環境引数（優先順位: 既定値 < 引数ファイル（後ろほど優先）< `--env-arg`） |
| `--kit-arg [KIT.]NAME=VALUE`, `--kit-args-file FILE` | kit 引数の上書き |
| `--name`, `--clone[=false]`, `--skip-host-commands` | `sbx env create` と同じ意味 |
| `--cloud`, `--ttl`, `--on-timeout` | cloud 環境として検査する（cloud で使えないフィールドを指摘する） |
| `--text` / `--json` | 出力の形式。どちらも付けず、stdin と stdout が端末で `TERM!=dumb` のときは TUI |
| `--redact` | TUI とテキストでも、JSON と同じく値を厳しく伏せる |
| `--fail-on none\|warning\|error\|critical\|high\|medium\|low` | 終了コード: `0` 正常、`1` 基準を超えた、`2` 引数の誤りか解析の失敗。`warning` と `error` は診断だけを数えます。`critical`〜`low` は Risk 画面と同じ尺度で、診断、リスクのある項目、`--live` で見つかったズレのうち最も重いものと比べます。Coverage は毎回の前提なので数えません |

使える展開式は `${{ env.args.NAME }}`・`${{ env.projectDir }}`・`${{ env.fileDir }}` です（`$${{ … }}` は文字どおりの文字列として扱い、`$VAR` は展開しません）。ローカルの kit では `network-policy@1`・`credential@1`・`host-mount@1`・`ssh-agent@1`・`git-identity@1`・`lifecycle@1` を展開します。それ以外の capability も設定を落とさずにそのまま表示します。

## 稼働中の sandbox と比べる

引数を付けずにターミナルで `sbxray` を実行すると、次のものを自動で集め、sandbox ごとに切り替えて表示します。

- `sbx ls --json`、sandbox ごとの `sbx policy ls <name> --json` と `sbx secret ls --sandbox <name> --json`、`sbx mcp ls --json` の結果。どれも読み取り専用の一覧コマンドで、`--no-input` を付けて実行します。シークレットは名前だけを読み、値は取得しません。
- カレントディレクトリと各 sandbox のワークスペースの近くにある `sbxenv.yaml` / `sbxenv.*.yaml`（`sbxenv.X.local.yaml` は `sbxenv.X.yaml` に重ねます）と `~/.sbxenv.yaml`

宣言の `name`（なければメインのワークスペースのパス）で sandbox と結び付けます。画面上部の「対象」バーには `稼働中` / `停止中` / `宣言のみ（未作成）` / `宣言ファイル未検出` のどれかと `ズレ N` を表示します。`<` `>` かクリックで切り替え、`s` で一覧から選べます。

宣言と実際の sandbox の両方があるときは、次の点を比べ、違いを「ズレ」として `1:Overview` と `2:Risk` に表示します。

- エージェント
- ワークスペースとマウント（足りないもの、余分なもの）
- ポートの公開
- 通信の許可と拒否（手で足したルール、宣言したのに反映されていないルール）
- シークレットを登録したかどうか
- MCP サーバーの登録と状態

詳細ペインでは「宣言（sbxenv.yaml）」と「現在の実体（sbx）」を並べ、作り直しなどの対処を示します。メモリや CPU、lifecycle の実行履歴のように sbx の一覧から取れないものは比べず、比べなかったことを表示します。

`sbx` が入っていないときやデーモンが止まっているときは、設定ファイルだけを解析します。パスを渡したとき、`--text` か `--json` を付けたとき、`--offline` を付けたときは、sbx を実行しません。`--live` を付けると、これらの出力にも比較の結果を加えます（`--json` では `live` キー）。

各コマンドの JSON は sbx v0.54.0 が実際に出力する構造に合わせて読みます。`sbx secret ls` の各行にある `secret` フィールドは読み込み用の構造体に対応するフィールドがないため、値はプログラムに取り込まれません（名前・スコープ・種類だけを読みます）。`sbx ls --json` はマウントのパスだけを出力し、読み取り専用かどうかを出力しないので、書き込みの可否（RW / RO）の違いは比べません。

## TUI の操作

| キー | 動作 |
|---|---|
| `1` `2` `3` `4` / `[` `]` | 画面の切り替え: Overview（全体のカード）/ Risk（深刻度の順）/ Items / Sources |
| `Enter`（Overview で） | 選んだ項目の Items か Risk の詳細へ移る |
| `j` `k` | 移動（詳細ペインではスクロール） |
| `h` `l` / `Enter` `Esc` | ペインの移動 / 詳細を開く・戻る |
| `g` | Items のまとめ方を切り替える（レイヤー別 / 境界別 / kit 別） |
| `f` | レイヤーで絞り込む（隠れた警告の件数をヘッダーに表示） |
| `/` | 検索（`Esc` で解除） |
| `<` `>` / `s` | 対象の sandbox を切り替える / 一覧から選ぶ（自動検出のとき） |
| `?` / `q` | ヘルプ / 終了（終了時に 1 行の要約を表示） |

| マウス | 動作 |
|---|---|
| タブ・チップのクリック | 画面（`1:Overview` など）、まとめ方（レイヤー別 / 境界別 / kit 別）、絞り込み（全て / `L1` / `K1` …）を切り替える |
| Overview のカードの項目のクリック | その項目の Items か Risk の詳細へ移る |
| 一覧の行のクリック | その項目を選ぶ（狭い画面では詳細を開き、`← 戻る` で一覧へ戻る） |
| ペインのクリック | フォーカスを移す（フォーカス中の枠はアクセント色と `●` で示す） |
| ホイール | ポインタの下のペインをスクロールする（一覧では選択を動かす） |

起動すると Overview を表示します。基本の構成、適用する kit（中身を確かめたか、リモートか）、ワークスペースとマウント、MCP と通信の許可・禁止、資格情報・SSH・ポート・ホストでの実行を角丸のカードで並べ、項目を選ぶとその詳細へ移ります。

画面は上から、タイトルバー（タブ）、要約のチップ（`Host実行` `Host永続` `RWマウント` `外への許可` `エラー` `警告`）、まとめ方と絞り込みのチップ、角丸の枠のペイン、キーの一覧を並べたステータスバーの順に並びます。幅が 120 桁以上なら一覧と詳細を左右に並べ、それより狭ければ 1 つのペインで表示します。

詳細ペインの説明はすべて日本語で、次の見出しに分けて表示します。

- 公式の見解: Docker Sandboxes のドキュメントと sandbox-kit-spec が実際に述べているセキュリティ上の前提・警告・推奨と、出典の節の URL。書かれていなければ「公式ドキュメントに特段の警告記載なし」と表示します。引用する節のアンカーは公開ページから記録した一覧とテストで照合しています。
- 外部セキュリティ基準の観点（OWASP 等）: 重要な項目だけに表示します。OWASP MCP Top 10（ベータ版）、OWASP Top 10 for Agentic Applications 2026、OWASP AI Agent Security Cheat Sheet のうち、原文で確かめた項目の番号と名前、なぜ危ないとされるかの 1〜2 行、出典の URL を載せます。引用した項目は確認済みの一覧（`pkg/sbxenv/testdata/external-standards.txt`）とテストで照合しています。
- このツールの判定: 隔離の境界をもとにした独自の基準での危険度と、その理由

ほかに、その設定が要するに何を意味するか、複数のレイヤーで上書きされたときの規則、上書きの履歴、設定した場所（ファイルと行番号）、sandbox の外との関わり、指摘と対処、展開しなかった理由を見出しを付けて表示します。長いパスは `/` の直後で折り返します。

重要度とバッジ（`[HOST]` `[OPAQUE]` `[RW]` `[PERSIST]` `[NET]` `[CRED]` `[!]` など）は色分けしますが、「安全」を示す緑は使いません。`NO_COLOR` を設定すると色を使わず、枠・太字・反転表示で同じ構造を保ちます。

## ライブラリとして使う

`pkg/sbxenv` は、ファイルシステム・OS・sbx・TUI のどれにも依存しないライブラリです。ファイルの中身は呼び出す側が渡し、ローカルの kit は `KitResolver` で渡します。

```go
plan, err := sbxenv.Resolve([]sbxenv.LayerInput{
	{Path: "/work/app/sbxenv.yaml", Content: data, Kind: sbxenv.LayerEnv},
}, sbxenv.ResolveOptions{
	ProjectDir: "/work/app",
	HomeDir:    home,
	EnvArgs:    []string{"channel=beta"},
	Kits:       myKitResolver, // nil ならローカルの kit は Coverage に回る
	Redaction:  sbxenv.RedactStrict,
})
if err != nil { /* *sbxenv.ParseError など */ }
for _, d := range plan.Diagnostics {
	fmt.Println(d.Level, d.Code, d.Message)
}
doc, _ := sbxenv.LookupDoc("sandboxOptions.memory") // 日本語の解説と上書きの規則
```

`Plan` は、許可したフィールドだけをコピーして作ります（allow-list 方式）。値の伏せ方は次のとおりです。

- 秘密の**実際の値**は読みません。`secrets.*.value` に直接書いた値と、`secrets` に文字列だけを書いた値は、どのモードでも出力に含めません。URL や kit の参照に埋め込まれた認証情報（`user:token@`）も、どのモードでも `<credentials>@` に置き換えます。
- `--json`（`RedactStrict`）が伏せるのは「値」です。env の値、`--env-arg` と kit 引数の値、コマンドの本文、`op://` 参照の中身（プロバイダーと vault の種類だけ残す）、URL のパスとクエリ（ホストまでは残す）が対象です。引数の値が `${{ env.args.X }}` や `${{ kit.args.X }}` を通じて `workspace`・ポート・通信先のドメイン・host-mount のパスなどに入ったときも、その部分だけを `<redacted>` にします（例: `~/proj/<redacted>`）。ファイルに直接書かれたパス・ポート・ドメイン・名前は、レビューに要る構造なので表示します。値が出力・診断・標準エラーに出ないことは、テストとファジング（毎週 5 分）で確かめています。
- TUI と `--text`（`RedactHome`）は `$HOME` を `~` に縮めるだけです。`--redact` で厳しく伏せるモードに切り替えられます。
- 端末に出す前に、値に含まれる制御文字・改行・双方向制御文字を `\x1b` `\n` `\u202e` のようにエスケープします。解析するファイルが画面を操作したり、出典の行を偽ったりできないようにするためです。

### 診断の重さ

- `error`: sbx が受け付けない構成か、仕様上必ず失敗する構成です（未知の必須 capability、重複したキー、未知のフィールド、capability の書式の誤り、host-mount のパスの衝突など）。
- `warning`: 動くかもしれないが危ない構成か、画面から見えない状態によって失敗する構成です。たとえば `bindings` のない credential@1 は、ホストの `~/.config/sbx/credentials.yaml` に承認がすでにあれば動くので、`warning` にしています。
- 通信の許可は kit をまたいで deny を優先します。deny された許可は「外への許可」に数えず、`denied by` を表示します。

## 開発

ツールはすべて [`mise.toml`](mise.toml) で版を完全に固定し、`mise.lock` にプラットフォームごとのハッシュと署名者を記録しています。Homebrew は使いません。

```sh
mise install          # mise.lock のとおりの版を入れる（locked = true）
hk install            # pre-commit / pre-push のフックを入れる
mise run test         # go test -race ./...
mise run e2e          # 実際の端末（PTY）での TUI テスト（UPDATE_SNAPSHOTS=1 でスナップショットを更新）
mise run lint         # gofmt と go vet
mise run lint:actions # GitHub Actions の workflow を jactionlint（shellcheck つき）と sisakulint で検査する
mise run audit        # govulncheck → SBOM → uzomuzo
mise run snapshot     # 公開せずにリリース用のアーカイブを作る（GoReleaser）
betterleaks git --redact --no-banner .   # 全履歴から秘密情報を探す
nix flake check       # Nix でビルドし、--version が動くかを確かめる
```

TUI のテスト（`e2e/tui`）はビルドしたバイナリを実際の端末（PTY）の上で操作して確かめます。[Microsoft tui-test](https://github.com/microsoft/tui-test) の Go バインディングを使い、独立した Go モジュールとして `go.mod` / `go.sum` で版を固定しています。80 桁と 130 桁の画面のスナップショット（`e2e/tui/__snapshots__/`）、`1`/`2`/`3`・`g`・`f`・`/`・`?`・`q` の操作、日本語の文字幅、`NO_COLOR`、制御文字のエスケープを確かめます。

テストは古典派のブラックボックステストです（AAA 形式で、モックは使いません）。相手にするのは公開 API（`pkg/sbxenv`）とビルドしたバイナリだけで、入力はケースごとに作る一時ディレクトリ（HOME・プロジェクト・kit）の実際のファイルです。すべてのケースを `t.Parallel()` で並列に動かすので、ケースのあいだに依存があれば失敗として表に出ます。

エージェント（Claude Code など）向けの作業ルールは [`AGENTS.md`](AGENTS.md) にあります。

## 自動チェックと監査

| いつ | 何をするか | どこで |
|---|---|---|
| commit / push | betterleaks（差分と push する履歴）、gofmt、go vet、go test -race、workflow を変えたときは jactionlint と sisakulint | [`hk.pkl`](hk.pkl) |
| PR と main への push | macOS・Linux でのテストと実際の端末での TUI テスト、6 プラットフォームへのクロスビルド、`nix flake check`（macOS・Linux）と `vendorHash` の確認、`mise.lock` の整合、jactionlint と sisakulint（`--online`）、GoReleaser の `--snapshot`、`govulncheck`、betterleaks（全履歴） | [`ci.yml`](.github/workflows/ci.yml) |
| タグ `v*` の push | テストのあと、GoReleaser で macOS・Windows 向けの 4 種類の `tar.gz`、チェックサム、SBOM（CycloneDX）、ソースのアーカイブを作って Release を公開する。続いて書き込み権限のないジョブで `jdx/packslip` が各アーカイブに build provenance を付け、アーカイブの一覧に keyless 署名した `packslip.sigstore.json` を作る。最後に別のジョブがそれを Release に添付する | [`release.yml`](.github/workflows/release.yml)、[`.goreleaser.yaml`](.goreleaser.yaml) |
| 毎週 | 本番と同じ条件で作った SBOM を `uzomuzo` で調べ、メンテナンスが止まった依存やサポートの切れた依存を探す。Trivy の結果を vuls のレポートに変えて CVE が 0 件かを確かめる。`govulncheck` と、値の伏せ方のファジング（5 分） | [`security-audit.yml`](.github/workflows/security-audit.yml) |
| 毎週 | `flake.lock` を、公開から 2 日以上たった nixpkgs のコミットに上げる PR を作る | [`flake-lock.yml`](.github/workflows/flake-lock.yml) |
| 毎日 | `docker/sbx-releases` に公開から 2 日以上たった新しい安定版が出たら、`sbx env` 系の `--help` の差分、公式ドキュメントの変更、リリースノートをまとめた Issue（ラベル `sbx-update`）を作る | [`sbx-watch.yml`](.github/workflows/sbx-watch.yml) |

## リポジトリの運用ルール

[mac-host-setup](https://github.com/kotarotaniguchi0523/mac-host-setup) と同じルールで運用しています。

### 版の固定と 2 日ルール

ツール・Actions・Go モジュール・nixpkgs は、すべて版を完全に固定します（Actions はコミット SHA で固定）。新しい版は公開から 2 日以上たってから使います（mise の `minimum_release_age`、Renovate の `minimumReleaseAge`、`flake-lock.yml`）。

### 更新は PR だけで行う

Renovate が mise のツール・Actions・Go モジュールの更新をまとめて PR にし、minor と patch の更新は必須のチェックが通れば自動でマージします。脆弱性のアラートによる更新は 2 日を待たずに PR を作ります。Renovate が作り直せない `mise.lock` と Nix の `vendorHash` は、[`relock.yml`](.github/workflows/relock.yml) が PR のブランチで作り直し、1 つのコミットにして push します。途中で Renovate が push していたら、上書きしません。

### main ブランチの保護

必須のチェックが通るまでマージできません。履歴は一直線に保ち（merge commit を作らず、squash か rebase でマージ）、force push と削除は禁止しています。マージしたブランチは自動で削除します。

### 自動 PR のための設定

`flake-lock.yml` が PR を作って自動マージを予約するので、Settings → Actions の「Allow GitHub Actions to create and approve pull requests」と、Settings → General の「Allow auto-merge」を有効にしています。

### 使ってよい Actions

GitHub 製の Actions のほかは、`jdx/mise-action`・`jdx/packslip`・`cachix/install-nix-action` だけを許可し、どれもコミット SHA で指定することを必須にしています。`GITHUB_TOKEN` の既定の権限は読み取りだけです。

### キャッシュ

Go のモジュールとビルド結果は、[`go-cache`](.github/actions/go-cache/action.yml)（`actions/cache` を使う共通部品）ですべてのジョブが復元し、保存は main への push のときだけ行います。PR が保存したキャッシュはその PR の中でしか使われないので、容量を使わないよう保存しません。リリース用のアーカイブを作るジョブ（`release.yml` 全体と GoReleaser の snapshot）と、キャッシュを使わない workflow・ジョブは [`cache-mode: none`](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching#controlling-cache-access-with-cache-mode) を宣言しており、GitHub のキャッシュサービスの側でキャッシュの読み書きを禁止しています。

### 公開リポジトリとしての注意

個人名、`/Users/<user>` のような実際のパス、秘密情報はコミットしません。betterleaks が commit・push・CI のたびに検査します。

## ライセンス

MIT
