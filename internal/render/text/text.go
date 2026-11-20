// Package text renders a Plan for non-TTY output. Entries of high severity
// or above are always listed in full; lower ones are capped per section.
package text

import (
	"fmt"
	"io"
	"strings"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/render/model"
	"github.com/kotarotaniguchi0523/sbx-env-viz/pkg/sbxenv"
)

// LowCap is how many entries below high severity are listed in the Risk section.
const LowCap = 15

// Write renders p to w.
func Write(w io.Writer, p *sbxenv.Plan) error {
	p = model.ForTerminal(p)
	b := &strings.Builder{}
	idx := model.SourceIndex(p)
	fmt.Fprintf(b, "sbx-env-viz — %s\n", "宣言の静的解析（稼働中 sandbox の状態ではない）")
	fmt.Fprintf(b, "対象 sbx: %s | 秘匿: %s | schemaVersion: %s\n", p.Meta.TargetSbxVersion, p.Meta.Redaction, p.Meta.SchemaVersion)
	fmt.Fprintln(b, sbxenv.SummaryLine(p.Summary))
	fmt.Fprintln(b, model.ArgsLine(p))
	if p.Invocation.SkipHostCommands {
		fmt.Fprintln(b, "--skip-host-commands: lifecycle と local MCP コマンドは実行されない前提（secrets の command は実行される）")
	}

	section(b, "Risk（high 以上は全件表示）")
	risks := model.RiskList(p)
	shown, hidden := 0, 0
	for _, r := range risks {
		if r.Severity.Rank() < sbxenv.SeverityHigh.Rank() {
			if shown >= LowCap {
				hidden++
				continue
			}
			shown++
		}
		fmt.Fprintf(b, "  %s %s %s\n", model.SeverityTag(r.Severity), strings.Join(r.Badges, ""), r.Label)
		if r.Item != nil {
			fmt.Fprintf(b, "       ← %s\n", model.FormatOrigin(idx, model.LastOrigin(r.Item)))
		}
		if r.Diag != nil {
			for _, o := range r.Diag.Origins {
				fmt.Fprintf(b, "       ← %s\n", model.FormatOrigin(idx, o))
			}
		}
	}
	if hidden > 0 {
		fmt.Fprintf(b, "  … medium 以下 %d 件を省略（--json か TUI で全件確認できます）\n", hidden)
	}
	if len(risks) == 0 {
		fmt.Fprintln(b, "  (該当なし)")
	}

	section(b, fmt.Sprintf("Coverage（解析範囲外 %d 件）", len(p.Coverage)))
	for _, c := range p.Coverage {
		fmt.Fprintf(b, "  [%s] %s — %s\n", c.Kind, c.Ref, c.Reason)
	}

	section(b, "Items（実効値）")
	for i := range p.Items {
		it := &p.Items[i]
		badges := strings.Join(it.Badges, "")
		if badges != "" {
			badges += " "
		}
		fmt.Fprintf(b, "  %s%s = %s  ← %s\n", badges, it.ID, it.Value, model.FormatOrigin(idx, model.LastOrigin(it)))
		for _, h := range it.History {
			if h.Op == sbxenv.OpShadow {
				fmt.Fprintf(b, "      (上書き済み) %s @ %s\n", h.Value, model.FormatOrigin(idx, h.Origin))
			}
		}
	}

	section(b, "Sources")
	for _, s := range p.Meta.Sources {
		sha := s.SHA256
		if len(sha) > 12 {
			sha = sha[:12]
		}
		state := "展開済み"
		if !s.Expanded {
			state = "未展開"
		}
		if s.Status != "" {
			state += "（" + s.Status + "）"
		}
		fmt.Fprintf(b, "  %-7s %-14s %s %s %s\n", s.ID, s.Kind, s.Path, sha, state)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func section(b *strings.Builder, title string) {
	fmt.Fprintf(b, "\n== %s ==\n", title)
}
