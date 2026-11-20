package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/live"
	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/model"
	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
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

type doc []docLine

func (d *doc) title(s string) { *d = append(*d, docLine{kind: docTitle, text: s}) }
func (d *doc) heading(s string) {
	*d = append(*d, docLine{kind: docBlank}, docLine{kind: docHeading, text: s})
}
func (d *doc) field(label, s string) {
	*d = append(*d, docLine{kind: docField, label: label, text: s})
}
func (d *doc) text(s string, indent int) {
	*d = append(*d, docLine{kind: docText, text: s, indent: indent})
}

// render wraps every line to width w at spaces and path separators, so a
// long path breaks after a "/" instead of in the middle of a name, and
// continuation lines hang under their own text.
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
			for _, s := range wrap(l.text, w) {
				out = append(out, p.title.Render(s))
			}
		case docHeading:
			out = append(out, p.heading.Render("▍"+l.text))
		case docField:
			lead := l.label + ": "
			pad := ansi.StringWidth(lead)
			for i, s := range wrap(l.text, w-pad) {
				if i == 0 {
					out = append(out, p.label.Render(lead)+s)
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

// wrap breaks plain text into lines at most w cells wide. Words break at
// spaces; a token wider than the line (a path, a URL, a run of Japanese)
// breaks after its last "/" that fits, and only then at the width itself.
func wrap(s string, w int) []string {
	if w < 4 {
		w = 4
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		flush := func() {
			out = append(out, strings.TrimRight(line, " "))
			line = ""
		}
		for _, word := range strings.SplitAfter(para, " ") {
			if ansi.StringWidth(line)+ansi.StringWidth(strings.TrimRight(word, " ")) <= w {
				line += word
				continue
			}
			long := ansi.StringWidth(strings.TrimRight(word, " ")) > w
			room := w - ansi.StringWidth(line)
			// Japanese has no spaces between words, so a run that holds
			// wide characters may break anywhere instead of leaving the
			// rest of the line empty.
			if line != "" && wide(word) && room >= 4 {
				head := kinsoku(word, keepASCIIWord(word, cutAt(word, room)))
				out = append(out, line+head)
				line, word = "", word[len(head):]
			} else if line != "" && (!long || room < w/3) {
				flush()
			}
			for ansi.StringWidth(line)+ansi.StringWidth(strings.TrimRight(word, " ")) > w {
				head := kinsoku(word, keepASCIIWord(word, cutAt(word, w-ansi.StringWidth(line))))
				out = append(out, line+head)
				line, word = "", word[len(head):]
			}
			line += word
		}
		flush()
	}
	return out
}

// keepASCIIWord moves a cut that would split an ASCII word such as
// "--skip-host-commands" back to the start of that word.
func keepASCIIWord(word, head string) string {
	if head == "" || len(head) >= len(word) || !asciiWord(word[len(head)]) || !asciiWord(head[len(head)-1]) {
		return head
	}
	i := len(head)
	for i > 0 && asciiWord(head[i-1]) {
		i--
	}
	if i == 0 {
		return head
	}
	return head[:i]
}

func asciiWord(b byte) bool {
	return b == '-' || b == '_' || b == '.' || b == '/' || b == ':' || (b >= '0' && b <= '9') || (b|0x20 >= 'a' && b|0x20 <= 'z')
}

// kinsoku moves the last character of head to the next line when the
// next line would otherwise start with closing punctuation such as "。".
func kinsoku(word, head string) string {
	rest := word[len(head):]
	r, _ := utf8.DecodeRuneInString(rest)
	if rest == "" || !strings.ContainsRune("、。，．）」』】・ー", r) {
		return head
	}
	_, size := utf8.DecodeLastRuneInString(head)
	if size >= len(head) {
		return head
	}
	return head[:len(head)-size]
}

// wide reports whether s holds a character two cells wide.
func wide(s string) bool {
	for _, r := range s {
		if ansi.StringWidth(string(r)) > 1 {
			return true
		}
	}
	return false
}

// cutAt returns the longest prefix of s that fits in w cells, ending just
// after a "/" when one falls in the second half of that prefix.
func cutAt(s string, w int) string {
	width, end, slash := 0, 0, -1
	for i, r := range s {
		rw := ansi.StringWidth(string(r))
		if width+rw > w {
			break
		}
		width += rw
		end = i + len(string(r))
		if r == '/' && width > w/2 {
			slash = end
		}
	}
	if end == 0 {
		_, size := utf8.DecodeRuneInString(s)
		return s[:size]
	}
	if slash > 0 {
		return s[:slash]
	}
	return s[:end]
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
		label += "（" + strings.Join(o.Via, " → ") + " 経由）"
	}
	return label
}

// judgementBasis says the risk level is this tool's own yardstick.
const judgementBasis = "※ 危険度は、sandbox の隔離境界（PC 上での実行・PC のファイルの書き換え・外部への公開・sandbox 削除後も残るもの）に基づくこのツール独自の基準で、公式の評価ではありません。"

// riskLine is "高（HIGH）" style text for a severity.
func riskLine(s sbxenv.Severity) string {
	return sbxenv.SeverityLabel(s) + "（" + strings.ToUpper(string(s)) + "）"
}

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
	}
	d.heading("このツールの判定（独自基準）")
	d.field("  危険度", riskLine(it.Risk))
	reason := it.RiskReason
	if reason == "" {
		reason = "一般的な設定値で、sandbox の外には影響しません。"
	}
	d.field("  理由", reason)
	for _, b := range it.Badges {
		d.text(b+" "+sbxenv.BadgeLabel(b), 2)
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

func (m *Model) diagDoc(d *doc, x *sbxenv.Diagnostic) {
	e := sbxenv.ExplainDiagnostic(x.Code)
	d.title(e.Title)
	d.field("重要度", sbxenv.LevelLabel(x.Level))
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
				*d = append(*d, docLine{kind: docBlank})
				m.itemDoc(d, &m.plan.Items[i])
				break
			}
		}
	}
}

func (m *Model) coverageDoc(d *doc, c *sbxenv.Coverage) {
	d.title("未展開: " + sbxenv.CoverageTitle(c.Kind))
	d.field("対象", c.Ref)
	d.heading("なぜ中身を確認していないか")
	d.text(sbxenv.ExplainCoverage(c.Kind), 2)
	d.text("これは静的解析の前提で、問題が見つかったという意味ではありません。sbx が実際に作成するときには、この部分も適用されます。", 2)
	d.heading("どこで指定されたか")
	d.text(m.origin(c.Origin), 2)
}

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
		d.text("関連する設定: "+x.ItemID+"（3: Items で詳細を確認できます）", 2)
	}
}

func helpDoc() doc {
	var d doc
	d.title("このツールについて")
	d.text("設定ファイルの静的解析です。緑の「安全」表示は出しません。", 2)
	d.text("静的解析の前提: PC 全体の sbx 設定（sbx policy・credentials.yaml）、sbx secret に保存した秘密、組み込みエージェントやリモート Kit の中身は読みません（ファイルに書いたリモート Kit などは 4: Sources に表示）。", 2)
	d.text("危険度（HIGH / MEDIUM / LOW）は隔離境界に基づくこのツール独自の基準です。公式の記述は詳細の「公式の見解」に分けて表示します。", 2)
	d.heading("キー操作")
	for _, kv := range [][2]string{
		{"1 / 2 / 3 / 4, [ / ]", "画面切替（Overview / Risk / Items / Sources）"},
		{"< / >, s", "対象の sandbox を切替 / 一覧から選ぶ（自動検出時）"},
		{"j / k, ↑ / ↓", "移動（詳細ペインではスクロール）"},
		{"h / l", "ペイン移動（狭い画面では戻る / 詳細）"},
		{"Enter / Esc", "詳細を開く（Overview では詳細へ移動）/ 戻る・検索解除"},
		{"g", "Items のグループ切替（層別 / 境界別 / kit別）"},
		{"f", "レイヤー絞り込み（入力ファイル・kit を順に切替）"},
		{"/", "検索（Enter で確定、Esc で解除）"},
		{"?", "このヘルプ（どのキーでも閉じる）"},
		{"q", "終了（1 行要約を出力）"},
	} {
		d.text(fmt.Sprintf("%-20s %s", kv[0], kv[1]), 2)
	}
	d.heading("マウス")
	d.text("タブ・グループ・絞り込みのチップをクリックで切替", 2)
	d.text("一覧の行をクリックで選択（狭い画面では詳細を開く）", 2)
	d.text("Overview のカードの項目をクリックで詳細へ移動", 2)
	d.text("ペインをクリックでフォーカス、ホイールでカーソル下のペインをスクロール", 2)
	d.heading("目印（バッジ）")
	for _, b := range []string{sbxenv.BadgeHost, sbxenv.BadgeOpaque, sbxenv.BadgeRW, sbxenv.BadgeRO, sbxenv.BadgePersist, sbxenv.BadgeNet, sbxenv.BadgeCred, sbxenv.BadgeSSH, sbxenv.BadgeSkip, sbxenv.BadgeWarn, sbxenv.BadgeUnknown, sbxenv.BadgeLiteral, "[DRIFT]"} {
		d.text(fmt.Sprintf("%-10s %s", b, sbxenv.BadgeLabel(b)), 2)
	}
	return d
}
