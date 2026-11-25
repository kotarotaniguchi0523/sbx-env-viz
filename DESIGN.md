# sbx-env-viz 詳細設計書（v2）

Docker Sandboxes（`sbx`）の複数レイヤー `sbxenv*.yaml` とローカル kit（`./kits/*`）を**オフライン静的解析**し、最終構成・出所（ファイル:行番号・kit 経路）・セキュリティ境界・設定ミス診断を **Bubble Tea TUI**・**非 TTY テキスト**・**安定 JSON IR** として出力する Go ライブラリ＆CLI。

---

## 1. 設計原則

- **環境制約ゼロ（ホスト単体実行）**: Go 単一バイナリ（`CGO_ENABLED=0`）。対象 OS（macOS / Windows / Linux）で外部ランタイム不要、`sbx` デーモンや Docker Desktop が停止中でも動作。
- **ライブラリ品質**: 純粋コアを `pkg/sbxenv` として公開し、AST・FS 探索・Renderer（TUI / text / JSON）は `internal/` に閉じる。
- **Fail-visible（誤った安心感を与えない）**:
  - 緑の「安全／OK」表示を使わない。
  - 未知の capability や未展開の OCI remote kit・`:dev` template・ホスト共通 `local-policy` を `Coverage`（解析範囲外）として常時明示。
  - ホスト実行コマンド（`lifecycle.*`・stdio MCP 等）は中身を推測せず `Opaque` として警告表示。
- **秘匿の強制（Allow-list DTO）**:
  - 後処理の `Redact` 忘れを防ぐため、公開 `Plan` 生成時に許可フィールドだけをコピーする。
  - `--json` はデフォルト厳格秘匿（`$HOME` → `~`、`op://` はプロバイダー＋vault 種別、疑似秘密の非露出をテスト保証）。ローカル TTY の TUI / `--text` は `$HOME` → `~` 短縮のみ（`--redact` で厳格秘匿へ切替可）。秘密の**実値**は一切読まない。
- **日本語での詳細解説**:
  - 各設定フィールドについて「**要はどういう意味か**」「**複数レイヤーで上書きされたときの挙動**（置換 / マップ結合 / キー一致で後勝ち / リスト連結）」を詳細ペインおよび `--json` メタデータに日本語で提示。

---

## 2. パッケージ構成

```text
sbx-env-viz/
├── pkg/
│   └── sbxenv/            # 公開ライブラリ（純粋関数・FS/OS/sbx/TUI 非依存）
│       ├── types.go       # Plan IR, Effective, Boundaries, Coverage, Diagnostics, Provenance
│       ├── resolve.go     # Resolve(layers []LayerInput, opts ResolveOptions) (*Plan, error)
│       ├── analyze.go     # 診断エンジン（無効 env-arg, host-mount 未指定, credential/binding/secret 三者照合等）
│       └── docs_ja.go     # 公式仕様ベースの日本語フィールド解説・上書き規則辞書
├── internal/
│   ├── parser/            # go.yaml.in/yaml/v3（1文書制限・重複キー走査・skills: off 文字列保持・行番号抽出）
│   ├── loader/            # ローカル FS 読込・./kits/* 解決・--env-args-file / --kit-args-file・~/.sbxenv.yaml
│   └── render/
│       ├── tui/           # charm.land/bubbletea/v2（3ビュー: Risk / Items / Sources）
│       ├── text/          # 非 TTY 向けサマリー（high 以上は省略せず全件表示）
│       └── jsonout/       # schemaVersion: "1" 安定 JSON IR（決定論的ソート・Allow-list 秘匿）
└── cmd/
    └── sbx-env-viz/       # 薄い CLI エントリポイント（sbx env create と同一引数形）
```

---

## 3. 対応する `sbx env` 引数・展開式

- **入力ファイル**: 明示パス列（ディレクトリ指定は `<dir>/sbxenv.yaml`）、無指定時は `~/.sbxenv.yaml` 既定値フォールバック
- **CLI フラグ**: `--env-arg`, `--env-args-file`, `--kit-arg`, `--kit-args-file`, `--name`, `--clone`, `--skip-host-commands`, `--cloud`, `--ttl`, `--on-timeout`
- **展開式**: `${{ env.args.<NAME> }}`, `${{ env.projectDir }}`, `${{ env.fileDir }}`（それ以外の `$` はリテラル扱い）
- **出力・終了コード**:
  - 判定順: `--json` / `--text` 優先 → stdin & stdout ともに TTY かつ `TERM!=dumb` なら TUI → それ以外は text
  - `--fail-on=none|warning|error|critical|high|medium|low`（既定 `none`。`critical`〜`low` は Risk 画面の尺度で、診断・項目・`--live` のズレを数え、Coverage は数えない）: `0`=正常、`1`=閾値超過、`2`=引数エラー/パース失敗

---

## 4. IR（`Plan`）の設計要点

- `Meta`: `SchemaVersion: "1"`, `TargetSbxVersion`（前提とした `sbx` 版）, `Sources[]`（ID・論理パス・sha256）
- `Effective`: フィールド単位の正規化済み最終構成＋`Origins[]`（`SourceID`, `Line`, `Col`, `Via[]`）＋`History[]`（`set / replace / append / shadow / default`）
- `Boundaries`: `ItemID` 参照＋判定根拠（実行場所 `host|sandbox|unknown`・寿命 `host-persistent|sandbox-scoped`）
  - ホスト実行（`initialize` / `postCreate` / `preRemove` / stdio MCP / コマンド型 secret）
  - ホストマウント（`rw` / `ro`、`host-mount@1` の `mounts:` 未指定共有リスク）
  - ネットワーク許可（`network-policy@1`）とプロキシ／Gateway 経路（`bindings` / `credential@1` / `secrets` / リモート MCP）を分離
- `Coverage`: 未展開の OCI remote kit・`:dev` template・ホスト共通 `local-policy` を独立管理し、常に件数と理由を表示
- `Diagnostics`: `error` / `warning` / `info`
  - 指定した `--env-arg` が後段レイヤーの固定値で上書きされて無効化されている
  - `credential@1`・`bindings`・`secrets` の三者不整合（重複・未投入・片落ち）
  - `host-mount@1` の `mounts:` 未指定共有リスク、`pullPolicy: always` + `:dev`、未解決式、重複キー

---

## 5. Bubble Tea（v2）TUI 設計

- **固定ヘッダー**: 1 行要約（`Host実行 N | Host永続 N | RWマウント N | 外への許可 N | 警告 N | 未展開 N`）＋ 適用中の `env-args` 前提 ＋ 「宣言の静的解析（稼働中 sandbox の状態ではない）」明記 ＋ 絞り込み中の非表示警告件数
- **3 ビュー**:
  - `1: Risk`（起動時）: 深刻度順の 1 本リスト（ホスト実行・`rw` マウント・永続変更・警告・未展開を隠さず表示）
  - `2: Items`: 全項目リスト。`g` キーで **「層別 / 境界別 / kit 別」** のグルーピング切替
  - `3: Sources`: 入力ファイル・kit 一覧（sha256・展開状態・行番号）
- **レイアウト・キーバインド**:
  - 幅 120 桁以上は左右 2 ペイン、80 桁以下は 1 ペイン（`Enter` 詳細 / `Esc` 戻る）。詳細ペインに日本語仕様解説・上書き挙動・出典 `file:line` を表示
  - `1-3` / `[` `]`: ビュー切替、`h/l`: ペイン移動、`j/k`: 移動、`g`: グループ切替、`f`: レイヤー絞り込み、`/`（検索モード、`Esc` 解除）、`?`: ヘルプ、`q`: 終了（終了時に 1 行要約＋警告件数を stdout 出力）
  - バッジは ASCII（`[!]`, `[RW]`, `[HOST]`, `[OPAQUE]`）、`NO_COLOR` 対応

---

## 6. サプライチェーン監査 & `sbx` 更新自動追従

### 依存監査（導入前 & CI ゲート）
1. **`govulncheck`**: 到達可能 CVE 検査（PR 必須）
2. **OSS `future-architect/vuls`（Trivy 連携）**: ライブラリ CVE 検査（検査対象件数 > 0 を確認）
3. **`future-architect/uzomuzo-oss`**: `cyclonedx-gomod app` で本番ビルド条件の SBOM を生成し、`uzomuzo scan --sbom ... --show-transitive --format json --fail-on eol-confirmed` でメンテ停滞・EOL・ゾンビ依存を検査

4. **betterleaks**: commit・push（hk）と CI（全履歴・作業ツリー）で秘密情報を検査。mise で入る単体バイナリだけを使い、Action は使わない
5. **jactionlint + sisakulint**: workflow の正しさとセキュリティ（キャッシュ汚染、過剰な権限、テンプレート注入、SHA 未固定、偽のコミット参照など）を CI と hk で検査。jactionlint は shellcheck で `run:` も検査する。jactionlint は sisakulint がまだ持たないルールを補う

### 配布と署名
- **GoReleaser**: macOS・Windows（amd64 / arm64）の `tar.gz`、checksums、CycloneDX SBOM、ソースアーカイブ。Linux はソース（`go install` / Nix）から
- **packslip**: 書き込み権限のないジョブが各アーカイブに build provenance を付け、ダイジェスト一覧に keyless 署名した `packslip.sigstore.json` を作る。mise の `packslip:` backend がこれを検証してから展開する。署名者はタグ上の `release.yml` に固定される
- **Nix**: `flake.nix`（入力は nixpkgs だけ）がソースからビルドする。完全性は署名ではなく `flake.lock`（rev + narHash）と `vendorHash` のハッシュで担保する

### `sbx` 更新の自動検知 → エージェント実装パイプライン
- **GitHub Actions（日次 cron）**:
  1. `docker/sbx-releases` の新安定版（公開 2 日以上経過）を監視
  2. 新版の `sbx env --help` / `create --help` / `plan --help` および公式 docs の差分をスナップショットと比較
  3. 差分がある場合のみ、リリースノート＋差分付き **GitHub Issue** を自動起票
- **エージェント起動（差分があるときだけ発火）**:
  - **リモートエージェント経路**: Issue から `@claude`（Claude Code リモート）または Codex Cloud を起動し、`pkg/sbxenv`・`docs_ja.go`・テストを更新して PR 作成
  - **T3 Code サブスク軍団経路**: Actions から T3 Code の `webhook` スケジュールタスクを発火して自動実装→PR 作成
