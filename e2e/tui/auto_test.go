package tui

import (
	"strings"
	"testing"

	tuitest "github.com/microsoft/tui-test/bindings/go"
)

// The project declares web-app (running, but with another agent and a
// host allowed by hand) and api (not created); sbx also lists orphan, a
// sandbox no file declares.
var autoFiles = map[string]string{
	"sbxenv.yaml": strings.Join([]string{
		`schemaVersion: "1"`, `name: web-app`, `agent: claude`, `workspace: ./web-app`,
		`ports:`, `  - {sandbox: 3000, host: 4000}`, ``,
	}, "\n"),
	"sbxenv.api.yaml": "schemaVersion: \"1\"\nname: api\nagent: codex\n",
}

var autoSbx = map[string]string{
	"ls --json": `[
  {"name": "web-app", "id": "sbx_1", "agent": "codex", "status": "running",
   "mounts": [], "workspaces": ["{{PROJ}}/web-app"],
   "ports": [{"host_ip": "127.0.0.1", "host_port": 4000, "sandbox_port": 3000, "protocol": "tcp"}],
   "created_at": "2026-11-01T09:00:00Z", "last_used_at": "2026-11-19T09:00:00Z"},
  {"name": "orphan", "id": "sbx_2", "agent": "claude", "status": "stopped",
   "mounts": [], "workspaces": ["/srv/orphan"], "ports": [],
   "created_at": "2026-10-01T09:00:00Z", "last_used_at": "2026-10-01T09:00:00Z"}
]`,
	"policy ls web-app --json":           `{"rules": [{"id": "r1", "name": "manual", "policy_id": "p1", "scope": "sandbox", "applies_to": "web-app", "resource_type": "network", "decision": "allow", "resources": ["manual.example.com"], "origin": "local", "layer": "local", "status": "active", "editable": true, "provenance": {"created_via": "added"}, "actions": []}]}`,
	"policy ls orphan --json":            `{"rules": []}`,
	"secret ls --sandbox web-app --json": `{"secrets": [], "custom_secrets": [], "shadowed_services": [], "env_only_count": 0}`,
	"secret ls --sandbox orphan --json":  `{"secrets": [], "custom_secrets": [], "shadowed_services": [], "env_only_count": 0}`,
	"mcp ls --json":                      `[]`,
}

func TestAutoDetectListsEverySandboxAndDeclaration(t *testing.T) {
	t.Parallel()
	// Arrange / Act
	s := start(t, launch{cols: 160, rows: 50, auto: true, files: autoFiles, sbx: autoSbx})

	// Assert: every target on one line, and the manifest gains an 実体 column.
	for _, want := range []string{" 対象  web-app（稼働中・ズレ 2）  orphan（停止中・宣言ファイル未検出）  api（宣言のみ（未作成））",
		" 宣言 1 ファイルと稼働中の sandbox（sbx の読み取り専用コマンド）を照合", "宣言にない実体・その他のズレ"} {
		s.expect(want)
	}
	for _, row := range []string{`ズレ 2\s+印なしでも問題がないとは限りません`, `項目\s+宣言（sbxenv\.yaml）\s+実体（sbx）`,
		`名前\s+web-app\s+照合なし`, `エージェント\s+claude\s+codex`, `ワークスペース\s+~/proj/web-app（読み書き）\s+パスは一致`,
		`通信の許可・拒否\s+（宣言なし）\s+許可: manual\.example\.com`} {
		s.expectMatch(row)
	}
	// A drift is told by bold underlined text in the 実体 column, not by a
	// mark or colour alone; matches and areas not compared stay plain.
	header := s.matches(`項目\s+宣言（sbxenv\.yaml）`)[0].Start.Row
	var drifted []string
	for _, l := range s.styled(tuitest.TextStyle{Bold: tuitest.Ptr(true), UnderlineStyle: tuitest.Ptr(tuitest.UnderlineSingle)}) {
		if l.Start.Row > header {
			drifted = append(drifted, strings.TrimSpace(l.Text))
		}
	}
	if got := strings.Join(drifted, " "); got != "codex 許可: manual.example.com" {
		t.Errorf("bold underlined values %q, want the two drifts only", drifted)
	}
	s.snapshot("auto-overview-160x50", false)
}

func TestAutoDetectSwitchesTargetsByKeyAndClick(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 160, rows: 50, auto: true, files: autoFiles, sbx: autoSbx})
	s.expect("宣言にない実体・その他のズレ")

	// Act / Assert: > moves to the sandbox without files.
	s.press(">")
	s.expect(" sbxray  orphan  停止中・宣言ファイル未検出")
	s.expect("稼働中の実体（宣言ファイル未検出）")
	s.expect("ワークスペース: /srv/orphan")

	// Act / Assert: a click on a target name moves to the declaration not created.
	s.click("api（宣言のみ")
	s.expect("まだ作成されていません")

	// Act / Assert: s opens the list; a click on a row picks it.
	s.press("s")
	s.expect("対象の sandbox を選ぶ")
	s.clickMatch(`web-app\s+稼働中\s+2\s+~/proj/sbxenv\.yaml`)
	s.gone("対象の sandbox を選ぶ")
	s.expect("宣言にない実体・その他のズレ")

	// Act / Assert: < wraps around to the last target.
	s.press("<")
	s.expect("まだ作成されていません")
}

func TestAutoDetectShowsDeclarationAndSandboxSideBySide(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 160, rows: 50, auto: true, files: autoFiles, sbx: autoSbx})

	// Act: Enter on a drifted Overview row (the second, the agent) opens
	// that drift in Risk, declaration and sandbox side by side.
	s.press("j", "Enter")

	// Assert
	s.expectMatch(`\|\s+ズレ: 宣言と違うエージェントで動いている`)
	for _, want := range []string{"稼働中とのズレ（2 件）", "エージェント: 宣言と違うエージェントで動いている",
		"宣言（sbxenv.yaml）", "現在の実体（sbx）", "codex", "どうすればよいか", "sbx env rm", "比較の根拠", "sbx ls --json"} {
		s.expect(want)
	}

	// Act: a click on another drifted row opens its drift, not the first one.
	s.press("1")
	s.clickMatch(`通信の許可・拒否\s+（宣言なし）`)

	// Assert
	s.expectMatch(`\|\s+ズレ: 宣言にない通信許可がある`)
	s.expect("許可: manual.example.com")
	if got := activeView(t, s); !strings.HasPrefix(got, "2 リスク") {
		t.Errorf("active view = %q", got)
	}
}

func TestAutoDetectWithoutSbxFallsBackToTheFiles(t *testing.T) {
	t.Parallel()
	// Arrange / Act: no sbx on PATH.
	s := start(t, launch{cols: 160, rows: 50, auto: true, files: autoFiles})

	// Assert
	s.expect("sbx コマンドが見つからないため")
	s.expect("web-app（宣言）")
	s.expectMatch(`項目\s+宣言（sbxenv\.yaml）`)
	if strings.Contains(s.text(), "ズレ") || strings.Contains(s.text(), "orphan") || strings.Contains(s.text(), "実体（sbx）") {
		t.Errorf("no live data without sbx:\n%s", s.text())
	}
}

func TestExplicitPathsStayOffline(t *testing.T) {
	t.Parallel()
	// Arrange / Act: sbx is available, but the files are named.
	s := start(t, launch{cols: 130, args: []string{"sbxenv.yaml"}, files: autoFiles})

	// Assert
	s.expectMatch(`項目\s+宣言（sbxenv\.yaml）`)
	if strings.HasPrefix(strings.TrimSpace(s.lines()[1]), "対象") {
		t.Errorf("named files are analysed offline, without a target bar:\n%s", s.text())
	}
}

func TestAutoRedactsMatchedNameThroughExit(t *testing.T) {
	files := map[string]string{"sbxenv.yaml": "schemaVersion: \"1\"\nname: ${{ env.args.n }}\nagent: claude\nargs:\n  n:\n    default: web-app\n"}
	s := start(t, launch{cols: 160, rows: 50, auto: true, args: []string{"--redact"}, files: files, sbx: autoSbx})
	s.expect("稼働中")
	s.gone("web-app")
	s.press("s")
	s.expect("対象の sandbox を選ぶ")
	s.gone("web-app")
	s.press("Escape")
	full := s.quit()
	if !strings.Contains(full, "sbxray: ") || strings.Contains(full, "web-app") {
		t.Fatalf("unexpected summary: %q", full)
	}
}
