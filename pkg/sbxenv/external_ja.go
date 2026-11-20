package sbxenv

// ExternalNote is how an external security standard frames a field's risk.
// Only identifiers, titles, and statements read in the standard itself are
// recorded; see testdata/external-standards.txt for what was checked.
type ExternalNote struct {
	// Standard names the document, e.g. "OWASP MCP Top 10（ベータ版）".
	Standard string `json:"standard"`
	// ID and Title are exactly as the standard writes them.
	ID    string `json:"id"`
	Title string `json:"title"`
	// Why says in plain Japanese why the standard calls it dangerous.
	Why string `json:"why"`
	URL string `json:"url"`
}

const (
	stdMCP     = "OWASP MCP Top 10（ベータ版）"
	stdAgentic = "OWASP Top 10 for Agentic Applications 2026"
	stdCheat   = "OWASP AI Agent Security Cheat Sheet"

	urlMCP     = "https://github.com/OWASP/www-project-mcp-top-10/blob/main/2025/"
	urlAgentic = "https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026/"
	urlCheat   = "https://github.com/OWASP/CheatSheetSeries/blob/master/cheatsheets/AI_Agent_Security_Cheat_Sheet.md"
)

var (
	mcp01 = ExternalNote{stdMCP, "MCP01:2025", "Token Mismanagement & Secret Exposure",
		"設定ファイルや環境変数に埋め込まれた秘密は、ログや会話の文脈から取り出され、つながったシステムの乗っ取りにつながるとしています。",
		urlMCP + "MCP01-2025-Token-Mismanagement-and-Secret-Exposure.md"}
	mcp02 = ExternalNote{stdMCP, "MCP02:2025", "Privilege Escalation via Scope Creep",
		"一時的・あいまいに与えた権限が時間とともに広がり、エージェントが必要以上の操作をできるようになる危険を挙げ、最小権限と権限の期限を勧めています。",
		urlMCP + "MCP02-2025%E2%80%93Privilege-Escalation-via-Scope-Creep.md"}
	mcp03 = ExternalNote{stdMCP, "MCP03:2025", "Tool Poisoning",
		"エージェントが頼るツールやプラグイン、その出力が乗っ取られると、悪意のある内容でモデルの振る舞いを操られるとしています。",
		urlMCP + "MCP03-2025%E2%80%93Tool-Poisoning.md"}
	mcp04 = ExternalNote{stdMCP, "MCP04:2025", "Software Supply Chain Attacks & Dependency Tampering",
		"改ざんされた依存部品はエージェントの振る舞いを変えたり裏口を仕込んだりできるとし、「latest」を避けてバージョンを固定するよう勧めています。",
		urlMCP + "MCP04-2025%E2%80%93Software-Supply-Chain-Attacks%26Dependency-Tampering.md"}
	asi02 = ExternalNote{stdAgentic, "ASI02", "Tool Misuse and Exploitation",
		"対策として、ツールやコードの実行を隔離された sandbox で行い、外向きの通信は許可リストにある宛先だけにして、それ以外はすべて拒否するよう求めています。",
		urlAgentic}
	asi03 = ExternalNote{stdAgentic, "ASI03", "Identity and Privilege Abuse",
		"エージェントが受け継いだ認証情報や権限が悪用されると、本来の範囲を超えた操作ができてしまうとし、タスク単位で短命・最小の権限にすることや、セッションごとに分けた sandbox を勧めています。",
		urlAgentic}
	asi04 = ExternalNote{stdAgentic, "ASI04", "Agentic Supply Chain Vulnerabilities",
		"第三者が提供するツール・プラグイン・MCP などに、悪意のあるものや改ざんされたものが紛れ込む危険を挙げ、許可リストとバージョン固定、出どころの確認を勧めています。",
		urlAgentic}
	asi05 = ExternalNote{stdAgentic, "ASI05", "Unexpected Code Execution (RCE)",
		"意図しないコードの実行がホストやコンテナの乗っ取り、sandbox からの脱出につながるとし、root で動かさず、制限した sandbox の中で実行するよう求めています。",
		urlAgentic}
	cheatLeast = ExternalNote{stdCheat, "", "Tool Security & Least Privilege",
		"エージェントには作業に必要な最小限の道具だけを与え、読み取り専用か書き込み可能かなど、権限を道具ごとに絞るよう求めています。",
		urlCheat}
	cheatData = ExternalNote{stdCheat, "", "Key Risks: Data Exfiltration / Sensitive Data Exposure",
		"ツールの呼び出しや外部への通信を通じた情報の持ち出しと、認証情報がエージェントの文脈やログに入り込むことを主要なリスクに挙げています。",
		urlCheat}
	cheatSandbox = ExternalNote{stdCheat, "", "Do's and Don'ts",
		"「sandbox なしでエージェントに任意のコードを実行させない」ことを、してはいけないことに挙げています。",
		urlCheat}
)

// externalNotes maps a doc key to the standards that frame its risk. A
// field is left out when no checked statement fits it (ports[], for one).
var externalNotes = map[string][]ExternalNote{
	"secrets.*":                       {mcp01, cheatData},
	"env.*":                           {mcp01},
	"bindings.*.apiKey":               {asi03, mcp02},
	"bindings.*.oauth":                {asi03, mcp02},
	"cap:credential@1":                {mcp01, asi03},
	"mcp.servers[]":                   {mcp03, mcp04},
	"kits[]":                          {asi04, mcp04},
	"sandboxOptions.template":         {asi04},
	"lifecycle.initialize[]":          {asi05, cheatSandbox},
	"lifecycle.postCreate[]":          {asi05, cheatSandbox},
	"lifecycle.preRemove[]":           {asi05, cheatSandbox},
	"sandboxOptions.writableEnvFiles": {asi05},
	"cap:ssh-agent@1":                 {asi03, mcp02},
	"workspace":                       {cheatLeast},
	"additionalWorkspaces[]":          {cheatLeast},
	"mounts.*":                        {asi03},
	"sandboxOptions.skills":           {cheatLeast},
	"cap:host-mount@1":                {asi03},
	"cap:network-policy@1":            {asi02, cheatData},
}

func withExternal(docs []FieldDoc) []FieldDoc {
	for i := range docs {
		docs[i].External = externalNotes[docs[i].Key]
	}
	return docs
}
