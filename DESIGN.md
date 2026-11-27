# sbxray の設計

sbxray は Docker Sandboxes（`sbx`）の複数レイヤーの `sbxenv*.yaml` とローカルの kit（`./kits/*`）をオフラインで静的に解析する Go のライブラリと CLI です。最終的な構成、値の出所（ファイル:行番号・kit の経路）、セキュリティ上の境界、設定の誤りの診断を、Bubble Tea の TUI、端末でないときのテキスト、安定した JSON の IR（中間表現）として出力します。使い方は [`README.md`](README.md) にあります。

---

## 1. 設計の方針

### ホストの上だけで動く

Go の単一バイナリ（`CGO_ENABLED=0`）で、外部のランタイムを必要としません。`sbx` のデーモンや Docker Desktop が止まっていても動きます。配布するのは macOS と Windows 向けのバイナリで、Linux ではソースからビルドします。

### ライブラリとして使える

外部に依存しない中心部分を `pkg/sbxenv` として公開し、構文木、ファイルの探索、表示（TUI / テキスト / JSON）は `internal/` の中に閉じます。

### 誤った安心感を与えない

- 「安全」「OK」を示す緑の表示を使いません。
- 未知の capability、展開していない OCI のリモート kit、`:dev` テンプレート、ホスト共通の `local-policy` などは `Coverage`（解析範囲の外にあるもの）に分け、いつも明示します。
- ホストで実行するコマンド（`lifecycle.*`、stdio の MCP など）は中身を推測せず、`Opaque` として警告を表示します。

### 値を伏せることを仕組みで強制する

- あとから `Redact` を呼び忘れることを防ぐため、公開する `Plan` を作るときに、許可したフィールドだけをコピーします（allow-list 方式）。
- `--json` は既定で値を厳しく伏せます（`$HOME` を `~` に、`op://` はプロバイダーと vault の種類だけに）。秘密に似た値が出力に出ないことをテストで確かめています。手元の端末で使う TUI と `--text` は `$HOME` を `~` に縮めるだけで、`--redact` で厳しく伏せるモードに切り替えられます。
- 秘密の実際の値は一切読みません。

### 日本語で詳しく説明する

設定のフィールドごとに、要するにどういう意味かと、複数のレイヤーで上書きされたときの動き（置き換え / マップの結合 / キーが一致したら後勝ち / リストの連結）を、詳細ペインと `--json` のメタデータに日本語で示します。

---

## 2. パッケージの構成

```text
sbxray/
├── pkg/
│   └── sbxenv/            # 公開ライブラリ（ファイルシステム・OS・sbx・TUI に依存しない）
│       ├── types.go       # Plan（IR）、Effective、Boundaries、Coverage、Diagnostics、出所
│       ├── resolve.go     # Resolve(layers []LayerInput, opts ResolveOptions) (*Plan, error)
│       ├── merge.go       # レイヤーの結合と上書きの履歴
│       ├── vnode.go       # 結合した値の木（ほぼすべてのファイルが読む共有のモデル）と表示用の値
│       ├── args.go        # env-arg・kit-arg の宣言と --*-args-file の読み込み
│       ├── keys.go        # キーのパスの組み立てと並べ替え
│       ├── expand.go      # ${{ … }} の展開
│       ├── kits.go        # ローカルの kit の展開
│       ├── netpolicy.go   # network-policy の allow / deny
│       ├── analyze.go     # 診断（効いていない env-arg、host-mount の mounts: 未指定、credential・bindings・secrets の照合など）
│       ├── redact.go      # 値を Plan に入れる唯一の入口
│       ├── declared.go    # 稼働中の sandbox と比べる部分の宣言
│       ├── docs_ja.go     # 公式の仕様にもとづく日本語のフィールド解説と上書きの規則
│       ├── explain_ja.go  # TUI で読む日本語の説明文
│       └── external_ja.go # OWASP など外部の基準の項目（確認済みの一覧とテストで照合）
├── internal/
│   ├── parser/            # go.yaml.in/yaml/v3（1 文書に限る、重複キーの検出、行番号の取得など）
│   ├── loader/            # ファイルの読み込み、./kits/* の解決、--env-args-file / --kit-args-file、~/.sbxenv.yaml
│   ├── live/              # sbx の読み取り専用の一覧コマンドで既存の sandbox を読み、宣言とのズレを出す
│   └── render/
│       ├── model/         # TUI とテキストが共有する表示用のデータ（リスクの一覧、出所の表示、端末向けのエスケープ）
│       ├── tui/           # charm.land/bubbletea/v2（概要 / リスク / 設定 / ファイル の 4 画面）
│       ├── text/          # 端末でないとき向けの要約（high 以上は省略せずに全件を表示）
│       └── jsonout/       # schemaVersion "1" の安定した JSON の IR（並び順を固定し、allow-list で値を伏せる）
└── cmd/
    └── sbxray/            # 薄い CLI の入口（sbx env create と同じ引数の形）
```

---

## 3. 対応する `sbx env` の引数と展開式

- 入力ファイル: パスを並べて渡します（ディレクトリなら `<dir>/sbxenv.yaml`）。渡さなければ、`~/.sbxenv.yaml` を既定値として使います。
- CLI のフラグ: `--env-arg`, `--env-args-file`, `--kit-arg`, `--kit-args-file`, `--name`, `--clone`, `--skip-host-commands`, `--cloud`, `--ttl`, `--on-timeout`
- 展開式: `${{ env.args.<NAME> }}`, `${{ env.projectDir }}`, `${{ env.fileDir }}`（それ以外の `$` は文字どおりに扱う）
- 出力と終了コード
  - 出力の形式は `--json` / `--text` があればそれを使い、なければ stdin と stdout がどちらも端末で `TERM!=dumb` のときに TUI、それ以外はテキストにします。
  - `--fail-on=none|warning|error|critical|high|medium|low`（既定は `none`）。`critical`〜`low` は Risk 画面の尺度で、診断・項目・`--live` のズレを数え、Coverage は数えません。終了コードは `0` が正常、`1` が基準を超えた、`2` が引数の誤りか解析の失敗です。

---

## 4. IR（`Plan`）の要点

- `Meta`: `SchemaVersion: "1"`、`TargetSbxVersion`（前提にした `sbx` の版）、`Sources[]`（ID・論理パス・sha256）
- `Effective`: フィールド単位の正規化した最終構成と、`Origins[]`（`SourceID`, `Line`, `Col`, `Via[]`）、`History[]`（`set / replace / append / merge / shadow / default`）
- `Boundaries`: 項目の `ItemID` と判定の根拠（実行する場所 `host|sandbox|unknown`、寿命 `host-persistent|sandbox-scoped`）
  - ホストでの実行（`initialize` / `postCreate` / `preRemove` / stdio の MCP / コマンド型の secret）
  - ホストのマウント（`rw` / `ro`、`host-mount@1` で `mounts:` を指定しないときの共有のリスク）
  - ネットワークの許可（`network-policy@1`）と、プロキシ・Gateway の経路（`bindings` / `credential@1` / `secrets` / リモートの MCP）は分けて扱う
- `Coverage`: 展開していないリモートの kit、`:dev` テンプレート、ホスト共通の `local-policy`、未知の capability などを別に持ち、いつも件数と理由を表示します。
- `Diagnostics`: `error` / `warning` / `info`。たとえば次のものを診断します。
  - 渡した `--env-arg` が後ろのレイヤーの固定値で上書きされて効いていない
  - `credential@1`・`bindings`・`secrets` の 3 つが食い違っている（重複、未登録、片方だけ）
  - `host-mount@1` で `mounts:` を指定していない共有のリスク、`pullPolicy: always` と `:dev` の組み合わせ、解決できない式、重複したキー

---

## 5. TUI の設計（Bubble Tea v2）

画面の見た目の方針は [`.impeccable/surfaces/internal-render-tui.md`](.impeccable/surfaces/internal-render-tui.md)（通関の申告書と検査）にあります。

- 1 行目に `sbxray`、対象の名前と状態、番号付きのタブ（件数つき）を置きます。2 行目は出どころ（静的解析か、稼働中との照合か）と、一覧の画面ではまとめ方・絞り込みの切り替えです。枠線やチップは使いません。
- 画面は 4 つです。
  - `1 概要`（起動時の画面）: 構成の申告書。項目・宣言（`sbxenv.yaml`）・実体（`sbx`）の列に並べ、実体の列は、`sbx` と照らし合わせた項目に `一致`（`sbx` がホストのパスしか返さないフォルダは `パスは一致`、名前しか返さないシークレットは `名前は一致`）か太字・下線の実際の値を、照らし合わせていない項目に `照合なし` を示します。行を選ぶと詳細へ移ります。
  - `2 リスク`: 深刻度の順に並べた 1 本の一覧（ホストでの実行、`rw` のマウント、永続する変更、警告、ズレを隠さずに表示）
  - `3 設定`: すべての項目の一覧。`g` キーで、レイヤー別 / 境界別 / kit 別のまとめ方を切り替えます。
  - `4 ファイル`: 入力ファイルと kit の一覧（sha256、展開したかどうか、行番号）。展開していないリモートの kit は `未展開` と表示します。
- 行の印は左端に 1 つだけです（`✗` 重大・高、`!` 中）。境界の目印（PC 上で実行、書き込み可、残る、など）は詳細ペインに言葉で並べます。
- 色は ANSI の 16 色だけを使い、背景は塗りません（選択行の反転表示を除く）。明るい背景・暗い背景のどちらでも端末のテーマが色を決めます。`NO_COLOR` では色を落とし、太字・下線・反転は残します。印は記号なので、色がなくても意味が伝わります。
- レイアウトとキー
  - 幅が 120 桁以上なら左右の 2 ペイン（間に縦線 1 本）、それより狭ければ 1 ペイン（`Enter` で詳細、`Esc` で戻る）です。詳細ペインには日本語の解説、上書きの動き、出典の `file:line` を表示します。
  - `1-4` / `[` `]` で画面を切り替え、`h` `l` でペイン、`j` `k` で項目を移ります。`g` でまとめ方、`f` でレイヤーの絞り込み、`/` で検索（`Esc` で解除）、`?` でヘルプ、`q` で終了します（終了時に 1 行の要約を stdout に出力）。最下行には、いまの状態で使うキーだけを出します。マウスでも操作できます。
- 稼働中の sandbox が複数ある場合は、2 行目の「対象」で sandbox を切り替えます（`s` で一覧から選択）。宣言とのズレは概要の実体の列とリスクに表示します（`internal/live`）。

---

## 6. 依存の監査と `sbx` の更新への追従

### 依存の監査

1. `govulncheck`: 実際に呼ばれる経路にある CVE を調べます（CI で必須）。
2. OSS の `future-architect/vuls`（Trivy と連携）: ライブラリの CVE を調べます（調べた件数が 0 より多いことも確かめる）。
3. `future-architect/uzomuzo-oss`: `cyclonedx-gomod app` で本番と同じ条件の SBOM を作り、`uzomuzo scan --sbom ... --show-transitive --format json --fail-on eol-confirmed` で、メンテナンスが止まった依存やサポートの切れた依存を調べます。
4. betterleaks: commit と push のとき（hk）と CI（全履歴と作業ツリー）で秘密情報を探します。mise で入る単体のバイナリだけを使い、Action は使いません。
5. jactionlint と sisakulint: workflow の正しさとセキュリティ（キャッシュの汚染、多すぎる権限、テンプレートへの注入、SHA で固定していない参照、偽のコミットの参照など）を CI と hk で検査します。jactionlint は shellcheck で `run:` の中も検査し、sisakulint がまだ持たないルールを補います。

### 配布と署名

- GoReleaser: macOS・Windows（amd64 / arm64）の `tar.gz`、チェックサム、CycloneDX の SBOM、ソースのアーカイブを作ります。Linux ではソース（`go install` / Nix）から入れます。
- packslip: 書き込み権限のないジョブが各アーカイブに build provenance を付け、ダイジェストの一覧に keyless 署名した `packslip.sigstore.json` を作ります。mise の `packslip:` backend はこれを検証してから展開します。署名者はタグの上の `release.yml` に固定されます。
- Nix: `flake.nix`（入力は nixpkgs だけ）がソースからビルドします。中身が正しいことは、署名ではなく `flake.lock`（rev と narHash）と `vendorHash` のハッシュで確かめます。

### `sbx` の新しい版の検知

`sbx-watch.yml` は毎日、次の順に動きます。

1. `docker/sbx-releases` に、公開から 2 日以上たった新しい安定版が出ていないかを調べます。
2. 新しい版の `sbx env` / `env create` / `env plan` / `env run` / `env rm` の `--help` を、保存してあるスナップショット（`.github/sbx-snapshots/`）と比べ、公式ドキュメント（`docker/docs` の `content/manuals/ai/sandboxes`）の変更を集めます。
3. リリースノートと差分をまとめた GitHub の Issue（ラベル `sbx-update`）を作ります。

対応の実装は自動では始まりません。Issue にはコメントでエージェント（例: `@claude`）を呼ぶか、T3 Code のスケジュールタスクの webhook から起動するよう案内を載せています。
