package sbxenv

import "strings"

// This file holds the plain-Japanese wording a person reads in the TUI.
// The Plan keeps its English machine-oriented fields (Reason, Message) for
// JSON consumers; these functions explain the same facts to someone who
// does not know sbx or Docker.

// DiagnosticExplanation is the Japanese wording of one diagnostic code.
type DiagnosticExplanation struct {
	// Title names the problem in a few words.
	Title string
	// What says what happens, in plain Japanese.
	What string
	// Fix says what to do about it; empty when there is nothing to do.
	Fix string
}

var diagnosticsJA = map[string]DiagnosticExplanation{
	"AGENT_MISSING":                 {"エージェントが未指定", "どのエージェント（claude など）を動かすかが、どのファイルにも書かれていません。sandbox を作れません。", "いずれかの環境ファイルに agent: を書いてください。"},
	"AGENT_NOT_WORKLOAD":            {"エージェントに指定した Kit が本体用ではない", "agent に指定した名前の Kit は、他の Kit に追加する部品（mixin）で、エージェント本体（workload）ではありません。", "組み込みのエージェント名か、workload の Kit を指定してください。"},
	"ARGS_FILE_SYNTAX":              {"引数ファイルの書き方が違う", "引数ファイルの行は「名前=値」の形でなければなりません。", "該当の行を「名前=値」の形に直してください。"},
	"ARG_DECL_INVALID":              {"引数の宣言が不正", "args の宣言は、既定値（default）か必須（required: true）のどちらか一方だけを指定する決まりです。enum と pattern も同時には使えません。", "宣言を見直してください。"},
	"ARG_NAME_INVALID":              {"引数名に使えない文字", "引数名は英字か _ で始まり、英数字・_・- だけを使えます。", "引数名を変えてください。"},
	"BINDING_DOMAIN_MISSING":        {"認証情報の差し込み先が未承認", "Kit が認証情報を差し込みたい通信先の一部が、bindings で承認されていません。作成時に承認を求められ、自動実行では失敗します。", "bindings の domains に通信先を追加してください。"},
	"BINDING_UNUSED":                {"使われていない承認設定", "bindings で承認していますが、それを使う Kit が見つかりません。設定だけが残っている可能性があります。", "不要なら削除してください。"},
	"CLOUD_SECRET_SNAPSHOT":         {"クラウドでは秘密の事前取得が必要", "クラウド環境では、ref や command で指定した秘密に snapshot: true が必要です。", "snapshot: true を追加してください。"},
	"CLOUD_UNSUPPORTED":             {"クラウドでは使えない設定", "この設定はローカルの sandbox 専用で、クラウド環境では拒否されます。", "クラウド用のファイルから外してください。"},
	"CREDENTIAL_CONFLICT":           {"同じ認証情報を複数の Kit が要求", "1 つのサービスの認証情報を、同じタイミングで 2 つの Kit が要求しています。sandbox は作れません。", "どちらか一方の Kit から要求を外してください。"},
	"CREDENTIAL_INJECT_DENIED":      {"認証情報の差し込み先が通信禁止", "認証情報を差し込む通信先が、別の Kit で通信禁止になっています。禁止が優先されるので、認証情報は送られません。", "禁止設定か差し込み先を見直してください。"},
	"CREDENTIAL_INJECT_NOT_ALLOWED": {"差し込み先への通信が許可されていない", "Kit が認証情報を差し込む通信先が、その Kit の通信許可リストに（完全一致で）書かれていません。", "Kit の network-policy に通信先を追加してください。"},
	"CREDENTIAL_INVALID":            {"認証情報の要求が不正", "認証情報の要求に必要な項目（サービス名・タイミング・方式）が欠けているか、正しくありません。", "Kit の credential@1 を見直してください。"},
	"CREDENTIAL_UNBOUND":            {"認証情報の使用が未承認", "Kit が認証情報を要求していますが、bindings で承認されていません。PC 全体の設定ですでに承認済みでなければ、作成時に承認を求められます（自動実行では失敗、任意の要求なら省略）。", "bindings にサービスを追加してください。"},
	"DUPLICATE_KEY":                 {"同じ項目が 2 回書かれている", "1 つのまとまりの中に、同じ名前の項目が 2 回あります。sbx はこのファイルを読み込めません。", "どちらか一方を消してください。"},
	"EGRESS_UNRESTRICTED":           {"すべての通信先への接続を許可", "Kit がインターネット上のすべての通信先への接続を許可しています。情報の持ち出しを止められません。", "必要な通信先だけを許可するよう Kit を見直してください。"},
	"ENV_ARG_INEFFECTIVE":           {"指定した引数が効いていない", "コマンドで指定した引数の値が、後のファイルの固定値で上書きされていて、どこにも使われていません。", "後のファイルの値を引数参照に戻すか、引数の指定をやめてください。"},
	"ENV_ARG_INVALID":               {"引数の値が条件に合わない", "引数の値が、宣言された候補（enum）や形式（pattern）に合っていません。", "条件に合う値を指定してください。"},
	"ENV_ARG_REQUIRED":              {"必須の引数が未指定", "必須の引数に値が渡されていません。", "--env-arg 名前=値 で指定してください。"},
	"ENV_ARG_SYNTAX":                {"--env-arg の書き方が違う", "--env-arg は「名前=値」の形で指定します。", "書き方を直してください。"},
	"ENV_ARG_UNDECLARED":            {"宣言されていない引数", "コマンドで渡した引数が、どの環境ファイルの args にも宣言されていません。", "args に宣言を追加するか、指定をやめてください。"},
	"ENV_ARG_UNUSED":                {"使われていない引数", "宣言された引数を指定しましたが、それを参照している設定がありません。", ""},
	"ENV_FILE_WRITABLE":             {"環境ファイルが書き換え可能な場所にある", "環境ファイルが sandbox から書き込めるフォルダの中にあります。エージェントがファイルを差し替え、次回あなたの PC で動くコマンドを変えられるおそれがあります。", "環境ファイルをワークスペースの外（またはワークスペースの直下）に置いてください。"},
	"EXPR_IN_ARGS":                  {"args の中で式は使えない", "args の宣言の中では ${{ ... }} の式を使えません。", "固定の値を書いてください。"},
	"EXPR_IN_KEY":                   {"項目名で式は使えない", "項目名（: の左側）には ${{ ... }} の式を使えません。", "項目名を固定の文字にしてください。"},
	"FLAG_REQUIRES_CLOUD":           {"クラウド専用のオプション", "--ttl や --on-timeout はクラウド環境（--cloud）でだけ使えます。", "--cloud を付けるか、オプションを外してください。"},
	"HOST_MOUNT_CONFLICT":           {"同じ場所を複数の Kit が共有しようとしている", "sandbox 内の同じ場所を、複数の Kit（または保存領域と共有フォルダ）が使おうとしています。sbx はこれを受け付けません。", "どちらかの Kit の場所を変えてください。"},
	"HOST_MOUNT_PATH_INVALID":       {"共有フォルダの場所の書き方が不正", "共有フォルダの場所は / で始まり、. や .. や // を含まない書き方が必要です。", "場所の書き方を直してください。"},
	"HOST_MOUNT_SHARED":             {"共有フォルダが他の sandbox とも共有される", "この Kit の共有フォルダには割り当て先（mounts:）が指定されていないため、同じ Kit を使う全ての sandbox が 1 つのフォルダを共有します。", "mounts: でこの環境専用のフォルダを割り当ててください。"},
	"INVALID_VALUE":                 {"使えない値", "この項目には決められた値しか書けません。", "使える値に直してください。"},
	"KIT_ARG_DECL_INVALID":          {"Kit の引数宣言が不正", "Kit の引数の宣言に誤りがあります（名前の形式や、受け渡し方法の重複など）。", "Kit の args を見直してください。"},
	"KIT_ARG_INVALID":               {"Kit の引数の値が条件に合わない", "Kit の引数の値が、宣言された候補や形式に合っていません。", "条件に合う値を指定してください。"},
	"KIT_ARG_REQUIRED":              {"Kit の必須引数が未指定", "Kit が必須としている引数に値が渡されていません。", "kits の args か --kit-arg で指定してください。"},
	"KIT_ARG_SYNTAX":                {"--kit-arg の書き方が違う", "--kit-arg は「名前=値」か「Kit名.名前=値」の形で指定します。", "書き方を直してください。"},
	"KIT_ARG_UNDECLARED":            {"Kit が知らない引数", "Kit が宣言していない引数を渡しています。", "引数名を確認してください。"},
	"KIT_ARG_UNUSED":                {"どの Kit にも効かない引数", "--kit-arg で渡した引数を宣言している Kit が見つかりません。", "引数名や Kit 名を確認してください。"},
	"KIT_CAP_GRAMMAR":               {"Kit の要求の書き方が不正", "Kit の要求（capability）の書き方に誤りがあります（種類名の形式、グループの混在や入れ子など）。sbx はこの Kit を拒否します。", "Kit の capabilities を見直してください。"},
	"KIT_CAP_TYPE_MISSING":          {"Kit の要求に種類がない", "Kit の要求（capability）に type が書かれていません。", "type を追加してください。"},
	"KIT_CONFIG_NOT_ALLOWED":        {"設定を書けない要求に設定がある", "この種類の要求には config を書けません（空でも不可）。", "config を削除してください。"},
	"KIT_GROUP_EMPTY":               {"空のグループ", "Kit の要求グループの中身が空です。", "要求を入れるか、グループを削除してください。"},
	"KIT_GROUP_MEMBER_OPTIONAL":     {"グループ内で optional は書けない", "optional はグループ全体にだけ書けます。中の要求には書けません。", "optional をグループ側へ移してください。"},
	"KIT_IN_WRITABLE_MOUNT":         {"Kit が書き換え可能な場所にある", "ローカルの Kit が sandbox から書き込めるフォルダの中にあります。エージェントが次回の作成時に要求する通信や認証情報を書き換えられるおそれがあります。", "Kit をワークスペースの外に置いてください。"},
	"KIT_PARSE":                     {"Kit の定義ファイルを読めない", "Kit の定義ファイルの YAML が壊れています。", "ファイルの書き方を直してください。"},
	"KIT_SINGLETON":                 {"1 つだけ設定できる要求が複数ある", "通信設定など「1 つだけ設定可能」な要求が、同じ場所に複数書かれています。", "1 つにまとめてください。"},
	"KIT_SOURCE_MISSING":            {"Kit の場所が未指定", "kits の項目に、Kit の場所（source）が書かれていません。", "source を書いてください。"},
	"KIT_TYPE_MISMATCH":             {"Kit の項目の形が違う", "Kit の定義の項目が、期待される形（文字・リスト・まとまり）と違います。", "書き方を直してください。"},
	"KIT_UNKNOWN_FIELD":             {"Kit に知らない項目がある", "Kit の定義に、決められていない項目名があります。sbx は厳密に読み込むので、この Kit を拒否します。", "綴りを確認してください。"},
	"KIT_UNREADABLE":                {"ローカルの Kit を読めない", "指定されたフォルダに Kit が見つからないか、読み込めません。", "Kit の場所を確認してください。"},
	"KIT_UNRESOLVED_EXPR":           {"Kit の式を埋められない", "Kit の中の ${{ ... }} の式に、使える値がありません。", "引数の宣言や値を確認してください。"},
	"LIFECYCLE_COMMAND_MISSING":     {"実行するコマンドが未指定", "lifecycle の項目に command が書かれていません。", "command を書いてください。"},
	"MCP_SOURCE_COUNT":              {"MCP サーバーの指定が不正", "MCP サーバーは、url（リモート）か command（PC 上で起動）のどちらか一方だけを指定します。", "どちらか一方にしてください。"},
	"MOUNT_TARGET_UNKNOWN":          {"割り当て先のない mounts", "mounts: に書いた場所を共有フォルダとして要求している Kit が見つかりません。", "場所の綴りや Kit を確認してください。"},
	"NOT_A_MAPPING":                 {"ファイルの形が違う", "環境ファイルは「項目: 値」の並びで書く必要があります。", "ファイルの書き方を直してください。"},
	"OAUTH_PASSTHROUGH":             {"本物のトークンが sandbox に渡る", "Kit が OAuth の本物のトークンを sandbox に渡す設定にしています。sandbox の中から本物の値が見えます。", "可能なら passthrough をやめてください。"},
	"PORT_ALL_INTERFACES":           {"ポートが外部ネットワークに公開される", "ポートがあなたの PC のすべてのネットワーク（LAN など）に公開され、他の機械から接続できます。", "hostIP を外す（あなたの PC の中だけにする）ことを検討してください。"},
	"PORT_SANDBOX_MISSING":          {"ポート番号が未指定", "ports の項目に sandbox 側のポート番号がありません。", "sandbox: を書いてください。"},
	"PRIVILEGED":                    {"最も強い権限を要求", "Kit が sandbox の中で最も強い権限（特権モード）を要求しています。", "本当に必要か確認してください。"},
	"PULL_ALWAYS_DEV":               {"中身が変わり続けるイメージを毎回取得", "テンプレートに :dev のような更新され続ける目印（タグ）を使い、作成のたびに最新版を取得します。いつの間にか中身が変わることがあります。", "ハッシュ値（@sha256:...）か固定のバージョン番号で指定してください。"},
	"REGISTRY_SECRET_MISSING":       {"レジストリの認証情報が未指定", "registries の項目に secret がありません。", "secret を書いてください。"},
	"SCHEMA_VERSION":                {"書式バージョンが不正", "schemaVersion は文字列の \"1\" でなければなりません。", "schemaVersion: \"1\" と書いてください。"},
	"SECRET_LITERAL":                {"秘密が平文で書かれている", "秘密の値がファイルに直接書かれています。ファイルを読める人なら誰でも見られます。", "ref（パスワード管理ツールの参照）か command で取得するようにしてください。"},
	"SECRET_NOT_DECLARED":           {"秘密の出どころがファイルにない", "Kit が使う認証情報の値が、この環境ファイルには書かれていません。sbx に保存済みの秘密が使われますが、このツールからは確認できません。", ""},
	"SECRET_SNAPSHOT_CONFLICT":      {"事前取得の秘密に使えない指定", "snapshot: true の秘密には refresh や noVerify を指定できません。", "どちらかを外してください。"},
	"SECRET_SOURCE_COUNT":           {"秘密の出どころが複数ある", "秘密の取得方法は value・ref・command のどれか 1 つだけです。複数のファイルを重ねた結果、2 つ以上になっています。", "1 つにしてください。"},
	"SECRET_UNUSED":                 {"使われていない秘密", "保存する秘密がありますが、それを使う承認や Kit が見つかりません。", "不要なら削除してください。"},
	"SKILLS_READWRITE":              {"共有スキルを書き換え可能", "あなたの PC の共有スキル置き場を、この sandbox から書き換えられます。他の sandbox にも影響します。", "readonly にすることを検討してください。"},
	"SSH_AGENT_INVALID":             {"SSH エージェントの要求が不正", "SSH エージェントの要求の書き方に誤りがあります（タイミング、用途の指定、ログイン先の書き方など）。", "Kit の ssh-agent@1 を見直してください。"},
	"SSH_AGENT_UNRESTRICTED":        {"SSH 鍵で何にでも署名できる", "Kit があなたの SSH 鍵を制限なしで使う設定です。sandbox から、鍵が持つ全ての権限で署名できてしまいます。", "unrestricted: false と sign / authenticate で用途を絞ってください。"},
	"TYPE_MISMATCH":                 {"項目の形が違う", "項目が、期待される形（文字・リスト・まとまり）と違います。", "書き方を直してください。"},
	"UNKNOWN_CAPABILITY_REQUIRED":   {"知らない種類の要求が必須", "Kit が、sbx の知らない種類の要求を必須にしています。対応していない sbx はこの Kit を拒否します。", "optional: true にするか、要求を見直してください。"},
	"UNKNOWN_FIELD":                 {"知らない項目がある", "環境ファイルに、決められていない項目名があります。sbx は厳密に読み込むので、このファイルを拒否します。", "綴りを確認してください。"},
	"UNRESOLVED_EXPR":               {"式を埋められない", "${{ ... }} の式に使える値がありません（引数が未指定、宣言されていない、使えない参照など）。", "引数の宣言や指定を確認してください。"},
	"USER_DEFAULTS_NAME":            {"ユーザー既定値に name は書けない", "~/.sbxenv.yaml には sandbox の名前を書けません。", "プロジェクトのファイルか --name で指定してください。"},
	"USER_DEFAULTS_WORKSPACE":       {"ユーザー既定値の workspace が不正", "~/.sbxenv.yaml の workspace には ${{ env.projectDir }}（とその下のフォルダ）しか書けません。", "書き方を直してください。"},
	"WRITABLE_ENV_FILES":            {"環境ファイルを sandbox から書き換え可能", "sandbox から環境ファイルを書き換えられる設定です。次回以降にあなたの PC で動くコマンドを変えられてしまいます。", "信頼できる場合以外は外してください。"},
}

// ExplainDiagnostic returns the plain-Japanese wording of a diagnostic.
func ExplainDiagnostic(code string) DiagnosticExplanation {
	if e, ok := diagnosticsJA[code]; ok {
		return e
	}
	return DiagnosticExplanation{Title: code, What: "この診断の日本語の説明はまだありません。"}
}

// whyNotExpanded is the common reason analysis stops at some sources.
const whyNotExpanded = "このツールは、安全のため外部への通信・イメージの取得・コマンドの実行を一切行わない静的解析ツールです。そのため、"

var coverageJA = map[CoverageKind]string{
	CoverageRemoteKit:      whyNotExpanded + "インターネット上にある Kit（OCI イメージ・Git・ZIP）の中身は読み込みません。その Kit が要求する通信・認証情報・フォルダ共有・実行コマンドは、ここには表示されていません。",
	CoverageBuiltinAgent:   whyNotExpanded + "sbx に組み込まれたエージェント（claude など）の内部の設定は読み込みません。エージェント自身が必要とする通信や認証情報は表示されていません。",
	CoverageMutableImage:   whyNotExpanded + "テンプレートイメージの中身は確認しません。:dev のような更新され続ける目印（タグ）の場合、作成のたびに中身が変わることもあります。",
	CoverageHostPolicy:     "あなたの PC 全体に効く設定（sbx policy の通信ルール、credentials.yaml の承認、組織のポリシーなど）は、この環境ファイルの外にあるため読み込みません。実際の sandbox にはそれらも適用されます。",
	CoverageUnknownCap:     "Kit が、sbx の標準にない種類の要求をしています。内容はそのまま表示していますが、意味や影響はこのツールでは判断できません。",
	CoverageUnmodeledCap:   "sbx の標準の要求ですが、このツールではまだ意味を解釈していません。内容はそのまま表示しています。",
	CoverageKitEnvRef:      "sandbox の中の環境変数（${{ kit.env.HOME }} など）は、sandbox を作るときに初めて決まるため、式のまま表示しています。",
	CoverageKitLoadFailed:  "ローカルの Kit のフォルダを読み込めなかったため、中身を表示できません。",
	CoverageLegacyKit:      "古い形式（v2 など）の Kit で、このツールでは読み込みません。",
	CoverageGlobalSecrets:  "sbx secret で保存済みの秘密は、あなたの PC の sbx の中にあり、このツールからは見えません（値を読み出すことはしません）。",
	CoverageNoKitResolver:  "ローカルの Kit を読む仕組みが設定されていないため、中身を表示していません。",
	CoverageWorkloadKitArg: whyNotExpanded + "リモートの Kit に渡す引数が正しいかは確認できません。",
}

// ExplainCoverage says in plain Japanese why something was not analysed.
func ExplainCoverage(kind CoverageKind) string {
	if s, ok := coverageJA[kind]; ok {
		return s
	}
	return "このツールの解析範囲外です。"
}

// ExplainBoundary describes in plain Japanese where something runs or
// what it reaches, and how long its effect lasts.
func ExplainBoundary(b Boundary) string {
	var what string
	switch b.Kind {
	case BoundaryHostExec:
		what = "あなたの PC（ホスト）の上で、あなたの権限でコマンドを実行します。"
	case BoundaryHostMount:
		switch b.Access {
		case "ro":
			what = "あなたの PC のフォルダを、読み取り専用で sandbox に見せます。"
		case "clone":
			what = "あなたの PC のリポジトリを sandbox 専用にコピーして使います。PC 側のファイルは直接は書き換わりません。"
		default:
			if b.Where == "sandbox" {
				what = "sandbox 専用の保存領域を使います。"
			} else {
				what = "あなたの PC のフォルダを、sandbox から読み書きできるように共有します。"
			}
		}
	case BoundaryHostPort:
		what = "sandbox の中のサービスを、あなたの PC のポートから使えるようにします。"
	case BoundaryNetwork:
		what = "sandbox から外部の通信先への接続を許可します（あなたの PC の中継を通ります）。"
	case BoundaryProxyRoute:
		what = "認証情報は sandbox に渡さず、あなたの PC の中継が通信の途中で差し込みます。"
	case BoundaryHostGlobal:
		what = "あなたの PC 全体の sbx の設定に登録され、他の sandbox とも共有されます。"
	case BoundarySSHAgent:
		what = "あなたの PC の SSH エージェントを sandbox から使えるようにします。秘密鍵は PC に残ります。"
	case BoundaryIdentity:
		what = "あなたの Git の名前とメールアドレスを sandbox に伝えます。"
	case BoundarySandboxExec:
		what = "sandbox の中でコマンドを実行します。"
	case BoundaryDevice:
		what = "あなたの PC の装置や強い権限を sandbox に渡します。"
	default:
		what = "sandbox の境界に関わる設定です。"
	}
	var notes []string
	switch b.Lifetime {
	case "host-persistent":
		notes = append(notes, "sandbox を削除しても PC に残ります。")
	case "invocation":
		notes = append(notes, "実行のたびに動きます。")
	}
	if b.Opaque {
		notes = append(notes, "コマンドの中身はこのツールでは確認していません。")
	}
	if b.Skipped {
		notes = append(notes, "ただし --skip-host-commands を付けているため実行されません。")
	}
	return strings.TrimSpace(what + " " + strings.Join(notes, " "))
}

var severityJA = map[Severity]string{
	SeverityCritical: "最重要",
	SeverityHigh:     "高",
	SeverityMedium:   "中",
	SeverityLow:      "低",
	SeverityInfo:     "情報",
}

// SeverityLabel returns the Japanese name of a risk level.
func SeverityLabel(s Severity) string {
	if l, ok := severityJA[s]; ok {
		return l
	}
	return string(s)
}

var levelJA = map[DiagLevel]string{LevelError: "エラー（このままでは作成できない）", LevelWarning: "警告（危険・要確認）", LevelInfo: "参考"}

// LevelLabel returns the Japanese name of a diagnostic level.
func LevelLabel(l DiagLevel) string {
	if s, ok := levelJA[l]; ok {
		return s
	}
	return string(l)
}

var historyOpJA = map[HistoryOp]string{
	OpSet: "設定", OpReplace: "上書き", OpAppend: "追加", OpMerge: "結合",
	OpShadow: "上書きされた（無効）", OpDefault: "既定値",
}

// HistoryOpLabel returns the Japanese name of a history step.
func HistoryOpLabel(op HistoryOp) string {
	if s, ok := historyOpJA[op]; ok {
		return s
	}
	return string(op)
}

var fieldJA = map[string]string{
	"optional": "任意の要求", "name": "名前", "description": "説明", "kind": "種類",
	"handle": "Kit 名", "status": "状態", "descriptor kind": "Kit の種類", "descriptor": "定義ファイル",
	"proxyManaged": "本物の値を PC 側に保持", "oauth.resourceHosts": "OAuth の利用先",
	"host directory": "PC 側のフォルダ", "mode": "権限", "user": "実行ユーザー", "env": "受け取る環境変数",
	"denied by": "禁止している設定", "skipped": "省略の理由", "clone": "コピーして使う", "--clone": "--clone の指定",
	"workdir": "作業フォルダ", "timeout": "時間制限", "domains": "承認した通信先", "refresh": "更新の間隔",
	"backend": "取得方法", "snapshot": "作成時に 1 回だけ取得", "noVerify": "取得の確認を省略",
	"required": "必須", "enum": "候補", "pattern": "形式",
}

// FieldLabel returns the Japanese label of an item field key.
func FieldLabel(key string) string {
	if s, ok := fieldJA[key]; ok {
		return s
	}
	switch {
	case strings.HasPrefix(key, "arg "):
		return "引数 " + strings.TrimPrefix(key, "arg ")
	case strings.HasPrefix(key, "exports "):
		return "sandbox の環境変数 " + strings.TrimPrefix(key, "exports ")
	}
	return key
}

// FieldValue rewrites machine values ("true", status words) for display.
func FieldValue(v string) string {
	switch v {
	case "true":
		return "はい"
	case "false":
		return "いいえ"
	case "expanded":
		return "中身を解析済み"
	case "not expanded":
		return "未展開（中身を確認できない）"
	case "local":
		return "ローカルのフォルダ"
	case "oci":
		return "OCI イメージ（インターネット上）"
	case "git":
		return "Git リポジトリ（インターネット上）"
	case "zip":
		return "ZIP ファイル"
	case "mixin":
		return "追加部品（mixin）"
	case "workload":
		return "エージェント本体（workload）"
	}
	return v
}

// phaseJA names a network-policy phase.
//
//declscope:shared // build.go words a deny rule's phase in a network item
func phaseJA(phase string) string {
	switch phase {
	case "install":
		return "準備時（install）"
	case "runtime":
		return "実行中（runtime）"
	}
	return phase
}

var coverageTitleJA = map[CoverageKind]string{
	CoverageRemoteKit:      "インターネット上の Kit",
	CoverageBuiltinAgent:   "組み込みエージェントの中身",
	CoverageMutableImage:   "テンプレートイメージの中身",
	CoverageHostPolicy:     "PC 全体の sbx 設定",
	CoverageUnknownCap:     "標準にない要求",
	CoverageUnmodeledCap:   "未解釈の要求",
	CoverageKitEnvRef:      "sandbox 内の環境変数",
	CoverageKitLoadFailed:  "読めなかったローカル Kit",
	CoverageLegacyKit:      "古い形式の Kit",
	CoverageGlobalSecrets:  "sbx に保存済みの秘密",
	CoverageNoKitResolver:  "未展開のローカル Kit",
	CoverageWorkloadKitArg: "リモート Kit への引数",
}

// CoverageTitle names an unanalysed area in a few Japanese words.
func CoverageTitle(kind CoverageKind) string {
	if s, ok := coverageTitleJA[kind]; ok {
		return s
	}
	return "解析範囲外"
}

var boundaryTitleJA = map[BoundaryKind]string{
	BoundaryHostExec:    "PC 上でのコマンド実行",
	BoundaryHostMount:   "PC のフォルダ共有",
	BoundaryHostPort:    "PC のポート公開",
	BoundaryNetwork:     "外部への通信",
	BoundaryProxyRoute:  "認証情報の差し込み",
	BoundaryHostGlobal:  "PC 全体の設定への登録",
	BoundarySSHAgent:    "SSH エージェント",
	BoundaryIdentity:    "Git の名前・メール",
	BoundarySandboxExec: "sandbox 内での実行",
	BoundaryDevice:      "装置・強い権限",
}

// BoundaryTitle names a boundary kind in a few Japanese words.
func BoundaryTitle(kind BoundaryKind) string {
	if s, ok := boundaryTitleJA[kind]; ok {
		return s
	}
	return string(kind)
}

var sourceKindJA = map[SourceKind]string{
	SourceEnv:          "環境ファイル",
	SourceUserDefaults: "ユーザー既定値（~/.sbxenv.yaml）",
	SourceKit:          "Kit の定義",
	SourceEnvArgsFile:  "環境の引数ファイル",
	SourceKitArgsFile:  "Kit の引数ファイル",
	SourceCLI:          "コマンドの引数",
	SourceDefault:      "sbx の既定値",
}

// SourceKindLabel names a source kind in Japanese.
func SourceKindLabel(k SourceKind) string {
	if s, ok := sourceKindJA[k]; ok {
		return s
	}
	return string(k)
}

// SourceStatusLabel rewrites a source status for display.
func SourceStatusLabel(status string) string {
	switch status {
	case "parse error":
		return "YAML が壊れていて読めない"
	case "not a mapping":
		return "ファイルの形が違う"
	case "legacy or unknown kit schema":
		return "古い形式か不明な形式の Kit"
	}
	return status
}

var badgeJA = map[string]string{
	BadgeHost:    "あなたの PC で実行",
	BadgeOpaque:  "中身を確認していないコマンド",
	BadgeRW:      "読み書きできる共有",
	BadgeRO:      "読み取り専用の共有",
	BadgePersist: "sandbox を消しても PC に残る",
	BadgeNet:     "外部への通信を許可",
	BadgeCred:    "認証情報の受け渡し",
	BadgeSSH:     "SSH 鍵を使う",
	BadgeSkip:    "実行されない",
	BadgeWarn:    "指摘あり",
	BadgeUnknown: "中身を確認できない",
	BadgeLiteral: "秘密が平文",
	"[DRIFT]":    "宣言と稼働中の実体のズレ",
}

// BadgeLabel explains a badge such as "[HOST]" in Japanese.
func BadgeLabel(b string) string {
	if s, ok := badgeJA[b]; ok {
		return s
	}
	return b
}

// CoverageFromFile reports whether a gap comes from something the files
// name (a remote kit, a mutable template, an unknown capability) rather
// than from a premise every run shares: host-wide policy, the built-in
// agent, and the sbx secret store are never read by a static analysis.
func CoverageFromFile(kind CoverageKind) bool {
	switch kind {
	case CoverageBuiltinAgent, CoverageHostPolicy, CoverageGlobalSecrets:
		return false
	}
	return true
}
