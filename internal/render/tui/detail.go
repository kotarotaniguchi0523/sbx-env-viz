package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

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
			if room := w - ansi.StringWidth(line); line != "" && (!long || room < w/3) {
				flush()
			}
			for ansi.StringWidth(line)+ansi.StringWidth(strings.TrimRight(word, " ")) > w {
				head := cutAt(word, w-ansi.StringWidth(line))
				out = append(out, line+head)
				line, word = "", word[len(head):]
			}
			line += word
		}
		flush()
	}
	return out
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

func (m *Model) itemDoc(d *doc, it *sbxenv.Item) {
	d.title(it.ID)
	d.field("値", it.Value)
	d.field("リスク", fmt.Sprintf("%s  %s", it.Risk, strings.Join(it.Badges, " ")))
	if it.Kit != "" {
		d.field("kit", it.Kit)
	}
	for _, f := range it.Fields {
		d.field("  "+f.Key, f.Value)
	}
	if doc := it.Doc; doc != nil {
		d.heading("解説（" + doc.Title + "）")
		d.field("要は", doc.Meaning)
		if doc.Detail != "" {
			d.field("注意", doc.Detail)
		}
		d.heading("上書き規則")
		d.field(doc.MergeLabel, doc.MergeDetail)
		if doc.Recreate {
			d.field("反映", "既存 sandbox には効かず、sbx env rm → create で作り直したときに反映されます。")
		}
		d.field("参照", doc.Reference)
	}
	d.heading("上書き履歴")
	for _, h := range it.History {
		d.text(fmt.Sprintf("%-7s %s", h.Op, h.Value), 2)
		d.text("@ "+model.FormatOrigin(m.idx, h.Origin), 10)
	}
	d.heading("出典")
	for _, o := range it.Origins {
		d.text(model.FormatOrigin(m.idx, o), 2)
	}
	if bs := model.BoundariesFor(m.plan, it.ID); len(bs) > 0 {
		d.heading("境界")
		for _, x := range bs {
			flags := ""
			if x.Opaque {
				flags += " [OPAQUE]"
			}
			if x.Skipped {
				flags += " [SKIP]"
			}
			access := ""
			if x.Access != "" {
				access = " " + x.Access
			}
			d.text(fmt.Sprintf("%s 実行/所在=%s 寿命=%s%s%s", x.Kind, x.Where, x.Lifetime, access, flags), 2)
			d.text(x.Reason, 4)
		}
	}
	if ds := model.DiagnosticsFor(m.plan, it.ID); len(ds) > 0 {
		d.heading("診断")
		for _, x := range ds {
			d.text(fmt.Sprintf("[%s] %s: %s", x.Level, x.Code, x.Message), 2)
		}
	}
}

func (m *Model) diagDoc(d *doc, x *sbxenv.Diagnostic) {
	d.title(x.Code)
	d.field("レベル", string(x.Level))
	d.field("分類", x.Category)
	d.text(x.Message, 0)
	if len(x.Origins) > 0 {
		d.heading("場所")
		for _, o := range x.Origins {
			d.text(model.FormatOrigin(m.idx, o), 2)
		}
	}
	if x.ItemID != "" {
		for i := range m.plan.Items {
			if m.plan.Items[i].ID == x.ItemID {
				*d = append(*d, docLine{kind: docBlank})
				m.itemDoc(d, &m.plan.Items[i])
				break
			}
		}
	}
}

func (m *Model) coverageDoc(d *doc, c *sbxenv.Coverage) {
	d.title("解析範囲外（Coverage）")
	d.field("種別", string(c.Kind))
	d.field("対象", c.Ref)
	d.field("理由", c.Reason)
	d.field("出典", model.FormatOrigin(m.idx, c.Origin))
	*d = append(*d, docLine{kind: docBlank})
	d.text("この部分は推測で埋めず、未解析として常に表示しています。", 0)
}

func (m *Model) sourceDoc(d *doc, s *sbxenv.Source) {
	d.title(s.ID)
	d.field("種別", string(s.Kind))
	d.field("パス", s.Path)
	if s.SHA256 != "" {
		d.field("sha256", s.SHA256)
	}
	if s.Layer > 0 {
		d.field("マージ順", fmt.Sprintf("%d（大きいほど後勝ち）", s.Layer))
	}
	state := "展開済み"
	if !s.Expanded {
		state = "未展開"
	}
	d.field("状態", strings.TrimSpace(state+" "+s.Status))
	n := 0
	var lines []string
	for i := range m.plan.Items {
		it := &m.plan.Items[i]
		if model.TouchesSource(it, s.ID) {
			n++
		}
		for _, h := range it.History {
			if h.Origin.SourceID == s.ID && h.Origin.Line > 0 {
				lines = append(lines, fmt.Sprintf("%5d  %s", h.Origin.Line, it.ID))
				break
			}
		}
	}
	d.field("関わる項目", fmt.Sprintf("%d 件（f で絞り込み）", n))
	if len(lines) > 0 {
		d.heading("行番号")
		for _, l := range lines {
			d.text(l, 2)
		}
	}
}

func helpDoc() doc {
	var d doc
	d.title("キー操作")
	for _, kv := range [][2]string{
		{"1 / 2 / 3, [ / ]", "ビュー切替（Risk / Items / Sources）"},
		{"j / k, ↑ / ↓", "移動（詳細ペインではスクロール）"},
		{"h / l", "ペイン移動（狭い画面では戻る / 詳細）"},
		{"Enter / Esc", "詳細を開く / 戻る・検索解除"},
		{"g", "Items のグループ切替（層別 / 境界別 / kit別）"},
		{"f", "レイヤー絞り込み（入力ファイル・kit を順に切替）"},
		{"/", "検索（Enter で確定、Esc で解除）"},
		{"?", "このヘルプ（どのキーでも閉じる）"},
		{"q", "終了（1 行要約を出力）"},
	} {
		d.text(fmt.Sprintf("%-18s %s", kv[0], kv[1]), 2)
	}
	d.heading("マウス")
	d.text("タブ・グループ・絞り込みのチップをクリックで切替", 2)
	d.text("一覧の行をクリックで選択（狭い画面では詳細を開く）", 2)
	d.text("ペインをクリックでフォーカス、ホイールでカーソル下のペインをスクロール", 2)
	d.heading("バッジ")
	d.text("[HOST] ホストで実行   [OPAQUE] 中身を解析しないコマンド   [RW]/[RO] マウント", 2)
	d.text("[PERSIST] ホストに残る   [NET] 外向き通信の許可   [CRED] 資格情報経路", 2)
	d.text("[SSH] SSH エージェント   [SKIP] 実行されない   [!] 診断あり", 2)
	d.text("[UNKNOWN] 未解析   [LITERAL] 平文の秘密", 2)
	d = append(d, docLine{kind: docBlank})
	d.text("これは宣言の静的解析です。緑の「安全」表示は出しません。", 0)
	d.text("未展開の kit・テンプレート・ホスト共通ポリシーは Coverage として常に表示します。", 0)
	return d
}
