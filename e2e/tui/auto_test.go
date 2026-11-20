package tui

import (
	"strings"
	"testing"
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

	// Assert
	for _, want := range []string{"対象 ", "web-app 稼働中 ズレ 2", "orphan 停止中・宣言ファイル未検出", "api 宣言のみ（未作成）",
		"宣言（設定ファイル）と稼働中の実体（sbx）を比較", "▍宣言と実体のズレ（2 件）", "エージェント: 宣言と違うエージェントで動いている",
		"通信の許可・拒否: 宣言にない通信許可がある"} {
		s.expect(want)
	}
	s.snapshot("auto-overview-160x50", false)
}

func TestAutoDetectSwitchesTargetsByKeyAndClick(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 160, rows: 50, auto: true, files: autoFiles, sbx: autoSbx})
	s.expect("▍宣言と実体のズレ")

	// Act / Assert: > moves to the sandbox without files.
	s.press(">")
	s.expect("▍稼働中の実体（宣言ファイル未検出）")
	s.expect("ワークスペース: /srv/orphan")

	// Act / Assert: a click on a chip moves to the declaration not created.
	s.click("api 宣言のみ")
	s.expect("まだ作成されていません")

	// Act / Assert: s opens the list; a click on a row picks it.
	s.press("s")
	s.expect("対象の切替")
	s.click("web-app  [稼働中]")
	s.gone("対象の切替")
	s.expect("▍宣言と実体のズレ")

	// Act / Assert: < wraps around to the last target.
	s.press("<")
	s.expect("まだ作成されていません")
}

func TestAutoDetectShowsDeclarationAndSandboxSideBySide(t *testing.T) {
	t.Parallel()
	// Arrange
	s := start(t, launch{cols: 160, rows: 50, auto: true, files: autoFiles, sbx: autoSbx})

	// Act
	s.click("エージェント: 宣言と違うエージェント")

	// Assert
	for _, want := range []string{"一覧 · Risk", "[DRIFT] ズレ: 宣言と違うエージェントで動いている", "▍宣言（sbxenv.yaml）", "▍現在の実体（sbx）",
		"codex", "▍どうすればよいか", "sbx env rm", "▍比較の根拠", "sbx ls --json"} {
		s.expect(want)
	}
}

func TestAutoDetectWithoutSbxFallsBackToTheFiles(t *testing.T) {
	t.Parallel()
	// Arrange / Act: no sbx on PATH.
	s := start(t, launch{cols: 160, rows: 50, auto: true, files: autoFiles})

	// Assert
	s.expect("sbx コマンドが見つからないため")
	s.expect("web-app 宣言")
	s.expect("▍基本構成")
	if strings.Contains(s.text(), "ズレ") || strings.Contains(s.text(), "orphan") {
		t.Errorf("no live data without sbx:\n%s", s.text())
	}
}

func TestExplicitPathsStayOffline(t *testing.T) {
	t.Parallel()
	// Arrange / Act: sbx is available, but the files are named.
	s := start(t, launch{cols: 130, args: []string{"sbxenv.yaml"}, files: autoFiles})

	// Assert
	s.expect("▍基本構成")
	if strings.HasPrefix(s.lines()[1], "対象 ") {
		t.Errorf("named files are analysed offline, without a target bar:\n%s", s.text())
	}
}
