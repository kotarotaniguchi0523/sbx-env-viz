package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/kotarotaniguchi0523/sbxray/internal/live"
	"github.com/kotarotaniguchi0523/sbxray/internal/render/model"
	"github.com/kotarotaniguchi0523/sbxray/pkg/sbxenv"
)

// docKind is the role of one line in the detail pane.
type docKind int

const (
	docTitle docKind = iota
	docHeading
	docField
	docText
	docBlank
)

// docLine is a detail-pane line before wrapping and styling.
type docLine struct {
	kind   docKind
	label  string
	text   string
	indent int
}

// doc is the detail pane's content before wrapping and styling.
//
//declscope:shared // view.go builds the detail and help panes from it
type doc []docLine

func (d *doc) title(s string) { *d = append(*d, docLine{kind: docTitle, text: s}) }
func (d *doc) heading(s string) {
	*d = append(*d, docLine{kind: docBlank}, docLine{kind: docHeading, text: s})
}
func (d *doc) field(label, s string) {
	*d = append(*d, docLine{kind: docField, label: label, text: s})
}

// text adds a paragraph indented by indent cells.
//
//declscope:shared // view.go's detailLines writes the empty-list note with it
func (d *doc) text(s string, indent int) {
	*d = append(*d, docLine{kind: docText, text: s, indent: indent})
}

// render wraps every line to width w at spaces and path separators, so a
// long path breaks after a "/" instead of in the middle of a name, and
// continuation lines hang under their own text.
//
//declscope:shared // view.go draws the detail and help panes with it
func (d doc) render(p palette, w int) []string {
	if w < 8 {
		w = 8
	}
	var out []string
	for _, l := range d {
		switch l.kind {
		case docBlank:
			out = append(out, "")
		case docTitle:
			ind := strings.Repeat(" ", l.indent)
			for _, s := range wrap(l.text, w-l.indent) {
				out = append(out, ind+p.title.Render(s))
			}
		case docHeading:
			out = append(out, strings.Repeat(" ", l.indent)+p.heading.Render(l.text))
		case docField:
			ind := strings.Repeat(" ", l.indent)
			lead := l.label + ": "
			pad := l.indent + ansi.StringWidth(lead)
			for i, s := range wrap(l.text, w-pad) {
				if i == 0 {
					out = append(out, ind+p.muted.Render(lead)+s)
				} else {
					out = append(out, strings.Repeat(" ", pad)+s)
				}
			}
		case docText:
			lead := strings.Repeat(" ", l.indent)
			for _, s := range wrap(l.text, w-l.indent) {
				out = append(out, lead+s)
			}
		}
	}
	return out
}

// origin formats where a value came from, in Japanese.
func (m *Model) origin(o sbxenv.Origin) string {
	label := o.SourceID
	if s, ok := m.idx[o.SourceID]; ok {
		label = s.Path
	}
	if o.SourceID == "host" {
		label = "あなたの PC 全体の sbx 設定"
	}
	if o.Line > 0 {
		label = fmt.Sprintf("%s の %d 行目", label, o.Line)
	}
	if len(o.Via) > 0 {
		label += "（" + strings.Join(o.Via, " -> ") + " 経由）"
	}
	return label
}

// judgementBasis says the risk level is this tool's own yardstick.
const judgementBasis = "※ 危険度は、sandbox の隔離境界（PC 上での実行・PC のファイルの書き換え・外部への公開・sandbox 削除後も残るもの）に基づくこのツール独自の基準で、公式の評価ではありません。"

// riskLine is "高（HIGH）" style text for a severity.
func riskLine(s sbxenv.Severity) string {
	return sbxenv.SeverityLabel(s) + "（" + strings.ToUpper(string(s)) + "）"
}

// itemDoc describes a setting: its value, the official note, this tool's
// judgement, the override history, and the boundaries it crosses.
//
//declscope:shared // view.go's detailLines shows it for a selected item
func (m *Model) itemDoc(d *doc, it *sbxenv.Item) {
	d.title(it.ID)
	d.field("値", it.Value)
	if it.Kit != "" {
		d.field("Kit", it.Kit)
	}
	for _, f := range it.Fields {
		d.field("  "+sbxenv.FieldLabel(f.Key), sbxenv.FieldValue(f.Value))
	}
	if doc := it.Doc; doc != nil {
		d.heading("公式の見解（Docker Sandboxes / sandbox-kit-spec）")
		if doc.OfficialNote != "" {
			d.text(doc.OfficialNote, 2)
			for _, ref := range doc.OfficialRefs {
				d.field("  出典", ref)
			}
		} else {
			d.text("公式ドキュメントに特段の警告記載なし", 2)
			d.field("  出典", doc.Reference)
		}
		if len(doc.External) > 0 {
			d.heading("外部セキュリティ基準の観点（OWASP 等）")
		}
		for _, x := range doc.External {
			d.text("・"+strings.Join(slices.DeleteFunc([]string{x.Standard, x.ID, x.Title}, func(s string) bool { return s == "" }), " "), 2)
			d.text(x.Why, 4)
			d.field("    出典", x.URL)
		}
	}
	d.heading("このツールの判定（独自基準）")
	d.field("  危険度", riskLine(it.Risk))
	reason := it.RiskReason
	if reason == "" {
		reason = "一般的な設定値で、sandbox の外には影響しません。"
	}
	d.field("  理由", reason)
	for _, b := range it.Badges {
		d.text("・"+sbxenv.BadgeLabel(b), 2)
	}
	d.text(judgementBasis, 2)
	if doc := it.Doc; doc != nil {
		d.heading("解説（" + doc.Title + "）")
		d.field("要は", doc.Meaning)
		if doc.Detail != "" {
			d.field("注意", doc.Detail)
		}
		d.heading("複数ファイルでの上書き規則")
		d.field("規則", doc.MergeLabel)
		d.text(doc.MergeDetail, 2)
		if doc.Recreate {
			d.field("反映", "既存 sandbox には効かず、sbx env rm → create で作り直したときに反映されます。")
		}
		d.field("参照", doc.Reference)
	}
	d.heading("上書きの履歴（上から順に適用）")
	for _, h := range it.History {
		d.text(sbxenv.HistoryOpLabel(h.Op)+": "+h.Value, 2)
		d.text("場所: "+m.origin(h.Origin), 4)
	}
	d.heading("どこで設定されたか")
	for _, o := range it.Origins {
		d.text(m.origin(o), 2)
	}
	if bs := model.BoundariesFor(m.plan, it.ID); len(bs) > 0 {
		d.heading("sandbox の外との関わり")
		for _, x := range bs {
			d.text("・"+sbxenv.BoundaryTitle(x.Kind), 2)
			d.text(sbxenv.ExplainBoundary(x), 4)
		}
	}
	if ds := model.DiagnosticsFor(m.plan, it.ID); len(ds) > 0 {
		d.heading("このツールからの指摘")
		for _, x := range ds {
			e := sbxenv.ExplainDiagnostic(x.Code)
			d.text("・"+e.Title+"（"+sbxenv.LevelLabel(x.Level)+"）", 2)
			d.text(e.What, 4)
			if e.Fix != "" {
				d.text("対処: "+e.Fix, 4)
			}
		}
	}
}

// diagDoc describes a diagnostic and, when it names one, its setting.
//
//declscope:shared // view.go's detailLines shows it for a selected diagnostic
func (m *Model) diagDoc(d *doc, x *sbxenv.Diagnostic, sev sbxenv.Severity) {
	e := sbxenv.ExplainDiagnostic(x.Code)
	d.title(e.Title)
	d.field("危険度", riskLine(sev))
	d.field("種類", sbxenv.LevelLabel(x.Level))
	d.heading("何が起きるか")
	d.text(e.What, 2)
	if e.Fix != "" {
		d.heading("どうすればよいか")
		d.text(e.Fix, 2)
	}
	if len(x.Origins) > 0 {
		d.heading("場所")
		for _, o := range x.Origins {
			d.text(m.origin(o), 2)
		}
	}
	if x.ItemID != "" {
		for i := range m.plan.Items {
			if m.plan.Items[i].ID == x.ItemID {
				d.heading("対象の設定")
				// The setting reads as part of this section: every line of
				// its own document is indented under the heading.
				var sub doc
				m.itemDoc(&sub, &m.plan.Items[i])
				for _, l := range sub {
					l.indent += 2
					*d = append(*d, l)
				}
				break
			}
		}
	}
}

// coverageDoc describes a part this static analysis did not expand.
//
//declscope:shared // view.go's detailLines shows it for a selected coverage gap
func (m *Model) coverageDoc(d *doc, c *sbxenv.Coverage) {
	d.title("未展開: " + sbxenv.CoverageTitle(c.Kind))
	d.field("対象", c.Ref)
	d.heading("なぜ中身を確認していないか")
	d.text(sbxenv.ExplainCoverage(c.Kind), 2)
	d.text("これは静的解析の前提で、問題が見つかったという意味ではありません。sbx が実際に作成するときには、この部分も適用されます。", 2)
	d.heading("どこで指定されたか")
	d.text(m.origin(c.Origin), 2)
}

// sourceDoc describes an input file and the settings it writes.
//
//declscope:shared // view.go's detailLines shows it for a selected source
func (m *Model) sourceDoc(d *doc, s *sbxenv.Source) {
	d.title(s.Path)
	d.field("種類", sbxenv.SourceKindLabel(s.Kind))
	d.field("識別名", s.ID)
	if s.SHA256 != "" {
		d.field("ハッシュ値", s.SHA256)
	}
	if s.Layer > 0 {
		d.field("重ねる順番", fmt.Sprintf("%d 番目（後のファイルほど優先）", s.Layer))
	}
	state := "読み込んで解析済み"
	if !s.Expanded {
		state = "未展開（中身を確認していない）"
	}
	if s.Status != "" {
		state += "：" + sbxenv.SourceStatusLabel(s.Status)
	}
	d.field("状態", state)
	n := 0
	var lines []string
	for i := range m.plan.Items {
		it := &m.plan.Items[i]
		if model.TouchesSource(it, s.ID) {
			n++
		}
		for _, h := range it.History {
			if h.Origin.SourceID == s.ID && h.Origin.Line > 0 {
				lines = append(lines, fmt.Sprintf("%5d 行目  %s", h.Origin.Line, it.ID))
				break
			}
		}
	}
	d.field("関わる設定", fmt.Sprintf("%d 件（f で絞り込み）", n))
	if len(lines) > 0 {
		d.heading("このファイルで書かれている設定")
		for _, l := range lines {
			d.text(l, 2)
		}
	}
}

// driftDoc describes a difference between the declaration and the sandbox.
//
//declscope:shared // view.go's detailLines shows it for a selected drift
func driftDoc(d *doc, x *live.Drift) {
	d.title("ズレ: " + x.Title)
	d.field("分類", x.Kind)
	d.heading("宣言（sbxenv.yaml）")
	d.text(x.Declared, 2)
	d.heading("現在の実体（sbx）")
	d.text(x.Actual, 2)
	d.heading("どうすればよいか")
	d.text(x.Fix, 2)
	d.heading("このツールの判定（独自基準）")
	d.field("  危険度", riskLine(x.Severity))
	d.text(judgementBasis, 2)
	d.heading("比較の根拠")
	d.text("宣言: 設定ファイルの静的解析", 2)
	d.text("実体: "+x.Source+" の出力（読み取り専用のコマンド）", 2)
	if x.ItemID != "" {
		d.text("関連する設定: "+x.ItemID+"（「3 設定」で詳細を確認できます）", 2)
	}
}

// helpDoc is the content of the help panel.
//
//declscope:shared // view.go's helpLines draws it
func helpDoc() doc {
	var d doc
	d.title("このツールについて")
	d.text("設定ファイルの静的解析です。緑の「安全」表示は出しません。", 2)
	d.text("静的解析の前提: PC 全体の sbx 設定（sbx policy・credentials.yaml）、sbx secret に保存した秘密、組み込みエージェントやリモート Kit の中身は読みません（ファイルに書いたリモート Kit などは「4 ファイル」に表示）。", 2)
	d.text("危険度（最重要 / 高 / 中 / 低）は隔離境界に基づくこのツール独自の基準です。公式の記述は詳細の「公式の見解」に分けて表示します。", 2)
	d.heading("キー操作")
	for _, kv := range [][2]string{
		{"1 / 2 / 3 / 4, [ / ]", "画面切替（概要 / リスク / 設定 / ファイル）"},
		{"< / >, s", "対象の sandbox を切替 / 一覧から選ぶ（自動検出時）"},
		{"j / k, ↑ / ↓", "移動（詳細ペインではスクロール）"},
		{"h / l", "ペイン移動（狭い画面では戻る / 詳細）"},
		{"Enter / Esc", "詳細を開く（概要では詳細へ移動）/ 戻る・検索解除"},
		{"g", "設定のグループ切替（層別 / 境界別 / kit別）"},
		{"f", "レイヤー絞り込み（入力ファイル・kit を順に切替）"},
		{"/", "検索（Enter で確定、Esc で解除）"},
		{"?", "このヘルプ（j / k でスクロール、ほかのキーで閉じる）"},
		{"q", "終了（1 行要約を出力）"},
	} {
		d.text(fmt.Sprintf("%-20s %s", kv[0], kv[1]), 2)
	}
	d.heading("マウス")
	d.text("タブ・グループ・絞り込みの項目をクリックで切替", 2)
	d.text("一覧の行をクリックで選択（狭い画面では詳細を開く）", 2)
	d.text("概要の行をクリックで詳細へ移動", 2)
	d.text("ペインをクリックでフォーカス、ホイールでカーソル下のペインをスクロール", 2)
	d.heading("一覧の印")
	d.text("✗  このツールの基準で危険度が最重要・高", 2)
	d.text("!  危険度が中", 2)
	d.text("概要の「実体（sbx）」の列で太字・下線の値は、宣言と違う（ズレている）ことを示します。", 2)
	d.text("印のない行も、問題がないという意味ではありません。理由は詳細の「このツールの判定」に出ます。", 2)
	d.heading("詳細に出る目印")
	for _, b := range []string{sbxenv.BadgeHost, sbxenv.BadgeOpaque, sbxenv.BadgeRW, sbxenv.BadgeRO, sbxenv.BadgePersist, sbxenv.BadgeNet, sbxenv.BadgeCred, sbxenv.BadgeSSH, sbxenv.BadgeSkip, sbxenv.BadgeWarn, sbxenv.BadgeUnknown, sbxenv.BadgeLiteral, "[DRIFT]"} {
		d.text("・"+sbxenv.BadgeLabel(b), 2)
	}
	return d
}
