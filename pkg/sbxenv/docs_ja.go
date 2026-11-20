package sbxenv

import (
	"path"
	"sort"
	"strings"
)

// MergeRule says what happens when several layers set the same field.
type MergeRule string

const (
	// MergeReplace: a later layer's value replaces the earlier one (置換).
	MergeReplace MergeRule = "replace"
	// MergeMap: mappings combine key by key; each key follows its own rule (マップ結合).
	MergeMap MergeRule = "map-merge"
	// MergeKeyed: list entries with the same identity are replaced by the
	// last one, which keeps the first position (キー一致で後勝ち).
	MergeKeyed MergeRule = "keyed-last-wins"
	// MergeAppend: lists concatenate in layer order (リスト連結).
	MergeAppend MergeRule = "list-append"
	// MergeKitArgs: kit entries with the same source merge their args key by key.
	MergeKitArgs MergeRule = "kit-args-merge"
	// MergeListReplace: a later list replaces the whole earlier list.
	MergeListReplace MergeRule = "list-replace"
	// MergeArgs: argument values come from defaults, then args files, then flags.
	MergeArgs MergeRule = "arg-precedence"
	// MergeKitUnion: capabilities from different kits are unioned at composition.
	MergeKitUnion MergeRule = "kit-union"
)

// mergeLabels are the short Japanese names of each rule.
var mergeLabels = map[MergeRule]string{
	MergeReplace:     "置換（後勝ち）",
	MergeMap:         "マップ結合",
	MergeKeyed:       "キー一致で後勝ち",
	MergeAppend:      "リスト連結",
	MergeKitArgs:     "同一 source の args をキー単位で結合",
	MergeListReplace: "リスト丸ごと置換",
	MergeArgs:        "既定値 < 引数ファイル < --env-arg",
	MergeKitUnion:    "すべての Kit の要求を合算",
}

// FieldDoc is the Japanese reference for one field.
type FieldDoc struct {
	Key string `json:"key"`
	// Title is the field name as written.
	Title string `json:"title"`
	// Meaning is "要はどういう意味か".
	Meaning string `json:"meaning"`
	// Detail adds what to watch for.
	Detail string `json:"detail,omitempty"`
	// Merge is the rule applied when several layers set it.
	Merge      MergeRule `json:"merge"`
	MergeLabel string    `json:"mergeLabel"`
	// MergeDetail explains the override behaviour concretely.
	MergeDetail string `json:"mergeDetail"`
	// Recreate reports whether a change needs sbx env rm + create.
	Recreate bool `json:"recreate"`
	// Reference is the official documentation section.
	Reference string `json:"reference"`
	// OfficialNote is what the official documentation (Docker Sandboxes or
	// sandbox-kit-spec) itself says about the field's security: premises,
	// warnings, recommendations. Empty when the documentation states none;
	// it is never this tool's own judgement.
	OfficialNote string `json:"officialNote,omitempty"`
	// OfficialRef is the section OfficialNote is taken from.
	OfficialRef string `json:"officialRef,omitempty"`
}

const (
	refEnv  = "https://docs.docker.com/ai/sandboxes/configuration/environment-files/"
	refSpec = "https://github.com/docker/sandbox-kit-spec/blob/main/docs/spec/"
)

func d(key, title string, merge MergeRule, recreate bool, ref, meaning, detail, mergeDetail string) FieldDoc {
	return FieldDoc{Key: key, Title: title, Meaning: meaning, Detail: detail, Merge: merge,
		MergeLabel: mergeLabels[merge], MergeDetail: mergeDetail, Recreate: recreate, Reference: ref}
}

const (
	scalarOverride = "後から渡したファイル（右側の引数）の値が前の値を丸ごと置き換えます。~/.sbxenv.yaml はプロジェクト側ファイルより前に読まれるので、ユーザー既定値はプロジェクトの値に負けます。"
	mapOverride    = "マップはキー単位で結合されます。別のキーは両方残り、同じキーは後のレイヤーの値で置き換わります。"
	appendOverride = "各レイヤーのリスト要素が読み込み順に連結されます（重複は除かれません）。前の要素を消す手段はありません。"
)

// fieldDocs is keyed by Item.Path patterns (path.Match syntax over "." paths).
var fieldDocs = withOfficial([]FieldDoc{
	d("schemaVersion", "schemaVersion", MergeReplace, false, refEnv+"#top-level-fields",
		"このファイルの書き方のバージョン番号です。今は \"1\" しか使えず、違うと sbx はファイルを読み込みません。",
		"kit 記述子の schemaVersion（\"3\"）とは無関係です。数値の 1 ではなく引用符付きの \"1\" で書きます。",
		scalarOverride),
	d("name", "name", MergeReplace, true, refEnv+"#top-level-fields",
		"sandbox に付ける名前です。省略すると自動で付きます。名前を変えると、別の sandbox として新しく作られます。",
		"--name を渡すとファイルの値より優先されます。~/.sbxenv.yaml には書けません。create / exec / rm で同じ名前を使わないと別の sandbox として扱われます。",
		scalarOverride+" さらに --name がすべてのファイルより優先されます。"),
	d("agent", "agent", MergeReplace, true, refEnv+"#top-level-fields",
		"sandbox の中で動かす AI エージェント（claude など）です。組み込み名か、kits に並べたエージェント本体の Kit の名前を書きます。",
		"ワークロードは 1 つだけで、他の kit はすべて mixin である必要があります。組み込みエージェント自身のネットワーク許可や資格情報はこのツールからは見えません（Coverage に表示）。",
		scalarOverride+" agent を変えたら sandbox を作り直します。"),
	d("entrypoint", "entrypoint", MergeListReplace, false, refEnv+"#set-the-launch-command",
		"sbx env run で sandbox の中で最初に起動するプログラムです。sandbox の中で動くので、あなたの PC には直接影響しません。",
		"シェルとしては解釈されません。シェル式を使うなら [/bin/sh, -c] のように明示します。exec や PID 1 には影響しません。",
		"後のレイヤーのリストが前のリストを丸ごと置き換えます（連結しません）。省略または null なら前の値を引き継ぎ、[] を書くと消去します。"),
	d("cmd", "cmd", MergeListReplace, false, refEnv+"#set-the-launch-command",
		"起動するプログラム（entrypoint）に渡す既定の引数です。sbx env run -- の後ろに書いた引数があれば、そちらに置き換わります。",
		"変更は作り直さずに次の run から効きます。",
		"後のレイヤーのリストが前のリストを丸ごと置き換えます。省略または null なら引き継ぎ、[] で消去します。"),
	d("args.*", "args.<NAME>", MergeArgs, false, refEnv+"#parameterize-an-environment",
		"作成時に外から値を渡せる「引数」の宣言です。${{ env.args.NAME }} と書いた場所に値が入ります。既定値か必須かのどちらかを必ず指定します。",
		"enum（候補）と pattern（RE2 の完全一致）は同時に使えません。args ブロックの中では式は展開されません。",
		"宣言自体はマップ結合（同名なら後のレイヤーの宣言が勝つ）。値の優先順位は 既定値 < --env-args-file（後のファイルほど優先）< --env-arg です。"),
	d("workspace", "workspace", MergeReplace, true, refEnv+"#workspace",
		"エージェントが作業するフォルダです。あなたの PC のこのフォルダをそのまま共有するので、エージェントの変更は PC のファイルに直接書き込まれます（clone: true なら専用のコピーで作業）。",
		"相対パスは「その行を書いたファイル」のディレクトリ基準で解決されます。環境ファイル自体をこのマウントの中（特にサブディレクトリ）に置くと、エージェントが次回のホストコマンドを書き換えられる恐れがあります。--clone / --clone=false でその回だけ clone を上書きできます。",
		"文字列とオブジェクトのどちらで書いても、後のレイヤーが置き換えます（オブジェクト同士なら path / clone がキー単位で結合）。"),
	d("additionalWorkspaces[]", "additionalWorkspaces[]", MergeKeyed, true, refEnv+"#additionalworkspaces",
		"作業フォルダのほかに sandbox と共有するあなたの PC のフォルダです。readOnly: true がなければ、エージェントが中身を書き換えられます。",
		"メインが clone モードでも追加分は直接マウントされます。target を省略するとホストと同じパスでマウントされます。",
		"マウント先（target、省略時は解決後のホストパス）が同じ要素は、最後の要素がまるごと置き換え、最初の位置に残ります。置き換え側で readOnly を省略すると書き込み可能に戻る点に注意。"),
	d("mounts.*", "mounts.<sandboxPath>", MergeMap, true, refEnv+"#mounts",
		"Kit が「PC のフォルダを共有したい」と要求した場所に、あなたが選んだ PC のフォルダを割り当てます。割り当てたフォルダは sandbox を消しても PC に残ります。",
		"割り当てないと、その kit を使う全 sandbox が sbx 管理の 1 つのホストディレクトリを共有します。キーは kit が宣言したパスと一致している必要があります。",
		mapOverride),
	d("env.*", "env.<VAR>", MergeMap, false, refEnv+"#top-level-fields",
		"sandbox の中のエージェントに渡す環境変数です。sandbox の中から誰でも読めるので、パスワードなどの秘密は書かないでください。",
		"ホストのライフサイクルコマンドには渡されません。秘密はここに書かず secrets / credential@1 を使います。既存 sandbox でも次の sbx env run のセッションから反映されます。",
		mapOverride),
	d("sandboxOptions.template", "sandboxOptions.template", MergeReplace, true, refEnv+"#sandboxoptions",
		"sandbox の土台になるイメージ（OS と道具一式）です。中身はこのツールでは確認できないので、信頼できるものを指定します。",
		":dev のような可変タグは作成のたびに中身が変わり得ます。再現性が必要ならハッシュ値（@sha256:...）で固定します。中身はこのツールでは解析しません。",
		scalarOverride),
	d("sandboxOptions.memory", "sandboxOptions.memory", MergeReplace, true, refEnv+"#sandboxoptions",
		"sandbox が使えるメモリの上限です（例: 8g、512m）。", "cloud では cloud のサイズに合わせる必要があります。", scalarOverride),
	d("sandboxOptions.cpus", "sandboxOptions.cpus", MergeReplace, true, refEnv+"#sandboxoptions",
		"sandbox が使える CPU の数です。0 なら PC の既定（ローカルでは全 CPU）を使います。",
		"引用符なしの ${{ env.args.X }} は置換後に整数として解釈されます。", scalarOverride),
	d("sandboxOptions.pullPolicy", "sandboxOptions.pullPolicy", MergeReplace, true, refEnv+"#sandboxoptions",
		"土台のイメージをいつ取り直すかの設定です。always（既定）は毎回最新版を取りにいくので、知らないうちに中身が変わることがあります。",
		"未指定でも always として扱われるため、:dev などの可変タグと組み合わさると毎回最新版が入ります。",
		scalarOverride),
	d("sandboxOptions.profile", "sandboxOptions.profile", MergeReplace, true, refEnv+"#sandboxoptions",
		"組織が決めた制限のセット（プロファイル）の名前です。", "組織のポリシーは解析範囲外です。", scalarOverride),
	d("sandboxOptions.skills", "sandboxOptions.skills", MergeReplace, true, refEnv+"#sandboxoptions",
		"PC にある共有スキル置き場を sandbox から見せるかどうかです。readwrite にすると、この sandbox が他の sandbox と共有するスキルを書き換えられます。",
		"未指定ならデーモン既定（通常 readonly）。readwrite にすると、この sandbox がホストの共有スキルを書き換えられます。off は YAML の真偽値ではなく文字列として扱われます。",
		scalarOverride),
	d("sandboxOptions.display", "sandboxOptions.display", MergeReplace, true, refEnv+"#sandboxoptions",
		"sandbox の中の画面付きアプリを、あなたの PC の画面に表示できるようにします。", "", scalarOverride),
	d("sandboxOptions.fuse", "sandboxOptions.fuse", MergeReplace, true, refEnv+"#sandboxoptions",
		"sandbox の中で特殊なファイルシステム（FUSE）を使えるようにします。sandbox の権限が少し強くなります。", "未指定時は sbx の sandbox.fuse 設定に従います。", scalarOverride),
	d("sandboxOptions.gpu", "sandboxOptions.gpu", MergeReplace, true, refEnv+"#sandboxoptions",
		"あなたの PC の GPU を sandbox から使えるようにします。", "", scalarOverride),
	d("sandboxOptions.usb", "sandboxOptions.usb", MergeAppend, true, refEnv+"#sandboxoptions",
		"あなたの PC につないだ USB 機器を sandbox から使えるようにします。機器を直接操作できるので注意が必要です。", "", appendOverride),
	d("sandboxOptions.writableEnvFiles", "sandboxOptions.writableEnvFiles", MergeReplace, true, refEnv+"#protect-the-environment-file",
		"sandbox の中から、この環境ファイル自体を書き換えられるようにします。エージェントが、次回あなたの PC で動くコマンドを変えられてしまいます。",
		"環境ファイルはホストで実行するコマンドを決めるため、true にするとエージェントが次回以降のホスト実行内容を変えられます。",
		scalarOverride),
	d("secrets.*", "secrets.<service>", MergeMap, true, refEnv+"#secrets",
		"API キーなどの秘密をどこから取るかの指定です。ファイルに直接書く（value）・パスワード管理ツールから取る（ref）・PC でコマンドを実行して取る（command）のどれか 1 つです。直接書くとファイルを読める人に丸見えになります。",
		"sandbox スコープで保存され、プロキシが送信時に差し込みます。command は --skip-host-commands でも実行されます。value の直書きはファイルを読める人に丸見えです。",
		"サービス名ごとにマップ結合され、同じサービスのフィールドもキー単位で結合されます。そのため前のレイヤーの ref と後のレイヤーの command が同居して「どれか 1 つ」の規則に反することがあります（このツールはエラーとして表示）。"),
	d("bindings.*.apiKey", "bindings.<service>.apiKey", MergeMap, true, refEnv+"#bindings",
		"Kit が API キーを使ってよいことと、送ってよい通信先を承認する設定です。承認はあなたの PC 全体の設定に残り、他の sandbox とも共有されます。",
		"ユーザー全体の credentials.yaml に結合され、sbx env rm でも残ります（--prune-bindings で削除、ただし他の sandbox と共有）。秘密の値そのものは持ちません。",
		"マップ結合。domains リストは連結されます。"),
	d("bindings.*.oauth", "bindings.<service>.oauth", MergeMap, true, refEnv+"#bindings",
		"Kit が OAuth のログイン情報を使ってよいことと、その通信先を承認する設定です。承認はあなたの PC 全体の設定に残ります。",
		"credentials.yaml に結合され sbx env rm 後も残ります。",
		"マップ結合。domains リストは連結されます。"),
	d("registries.*", "registries.<host>", MergeMap, true, refEnv+"#registries",
		"イメージ置き場（ghcr.io など）からイメージを取ってくるときのログイン情報です。",
		"secret / username は secrets と同じく value / ref / command のどれか 1 つで指定します。",
		mapOverride),
	d("mcp.servers[]", "mcp.servers[]", MergeKeyed, false, refEnv+"#mcp",
		"エージェントが使う追加の道具（MCP サーバー）です。command 型はあなたの PC の上で、あなたの権限で動きます。url 型はインターネット上のサーバーに接続します。",
		"command 型はゲートウェイ起動のたびにあなたの権限でホスト上で動きます（--skip-host-commands で無効化）。url 型はホスト全体に登録され sbx env rm 後も残ります。",
		"name が同じ要素は最後の要素がまるごと置き換え、最初の位置に残ります。名前の違う要素は連結されます。"),
	d("ports[]", "ports[]", MergeKeyed, true, refEnv+"#ports",
		"sandbox の中で動くサービス（Web サーバーなど）を、あなたの PC のポート番号から開けるようにします。hostIP を指定しなければ、あなたの PC の中からだけ接続できます。",
		"hostIP 未指定ならあなたの PC の中からだけ接続できます。0.0.0.0 などを指定すると LAN から到達可能になります。host 省略時は空いているポートが自動で割り当てられます。",
		"sandbox ポートと実効プロトコル（省略時 tcp4、IPv6 の hostIP なら tcp6）が同じ要素は、最後の要素がまるごと置き換えます。置き換え側で host を省略すると空きポートの自動割り当てに戻ります。"),
	d("lifecycle.initialize[]", "lifecycle.initialize[]", MergeAppend, false, refEnv+"#lifecycle",
		"sandbox を作る・起動するたびに、あなたの PC の上であなたの権限で実行されるコマンドです。PC の中のファイルを自由に読み書きできます。",
		"あなたのユーザー権限で動きます。何度実行しても安全に書きます。workdir の相対パスは「最初のファイル」のディレクトリ基準です。env や秘密は渡されません。",
		appendOverride+" 1 つ失敗すると以降は止まります。"),
	d("lifecycle.postCreate[]", "lifecycle.postCreate[]", MergeAppend, false, refEnv+"#lifecycle",
		"sandbox を新しく作った直後に、あなたの PC の上であなたの権限で実行されるコマンドです。",
		"秘密投入スクリプトなどに使われます。SBX_SANDBOX_NAME などの変数を受け取ります。",
		appendOverride),
	d("lifecycle.preRemove[]", "lifecycle.preRemove[]", MergeAppend, false, refEnv+"#lifecycle",
		"sandbox を削除する直前に、あなたの PC の上であなたの権限で実行されるコマンドです。",
		"失敗しても警告だけで削除は続きます。cloud で期限切れ削除された場合は動きません。",
		appendOverride),
	d("kits[]", "kits[]", MergeKitArgs, true, refEnv+"#kits",
		"sandbox に追加する機能のセット（Kit）です。Kit は通信の許可・認証情報・PC のフォルダ共有などを要求できます。PC のフォルダにある Kit 以外は、このツールでは中身を確認できません。",
		"ローカル以外はこのツールでは展開しないため、その kit が要求する通信・資格情報・ホストマウントは見えません（Coverage）。OCI はハッシュ値（@sha256:...）、Git は ref= で固定すると再現性が上がります。--kit-arg / --kit-args-file でその回だけ kit 引数を上書きできます。",
		"source が同じ要素は 1 つにまとまり、args はキー単位で結合されます（後の値が勝ち、他の引数は残る）。source が違えば連結されます。"),
	d("cap:network-policy@1", "network-policy@1", MergeKitUnion, true, refSpec+"capabilities/com.docker.sandbox/network-policy@1.md",
		"Kit が「この通信先に接続させてほしい」と要求したものです。許可した通信先には、sandbox からデータを送れます。準備時（install）と実行中（runtime）で別々に指定します。",
		"deny は allow より優先します。*.example.com はサブドメイン 1 階層だけ、* / ** は全許可です。ホストの sbx policy（グローバル規則）も別に効きます。",
		"複数 kit の allow / deny はフェーズごとにすべての Kit の分が合算され、ある kit の deny を別の kit の allow で打ち消すことはできません。"),
	d("cap:credential@1", "credential@1", MergeKitUnion, true, refSpec+"capabilities/com.docker.sandbox/credential@1.md",
		"Kit が「このサービスの認証情報を使わせてほしい」と要求したものです。本物の値は通常 PC 側に残り、通信の途中で差し込まれます。",
		"既定は必須で、環境ファイルに bindings.<service> がないと作成が失敗します（optional: true なら黙って省略）。inject のドメインは同じ kit の network-policy@1 の allow に完全一致で載っている必要があります。proxyManaged: true なら sandbox には置き換え用の値しか入りません。",
		"すべての Kit の要求が合算されますが、同じ（サービス, フェーズ）を 2 つの kit が宣言すると合成エラーになります。"),
	d("cap:host-mount@1", "host-mount@1", MergeKitUnion, true, refSpec+"capabilities/com.docker.sandbox/host-mount@1.md",
		"Kit が「PC のフォルダを共有させてほしい」と要求したものです。書き込んだデータは sandbox を消しても PC に残ります。",
		"ホスト側の場所は sbx が決め、同じ kit を使う全 sandbox で共有されます。sandbox ごとに分けるには環境ファイルの mounts: で割り当てます。",
		"パスはすべての Kit の分が合算されます。別の kit が同じパスを宣言すると衝突（黙って統合はされない）します。"),
	d("cap:ssh-agent@1", "ssh-agent@1", MergeKitUnion, true, refSpec+"capabilities/com.docker.sandbox/ssh-agent@1.md",
		"あなたの SSH 鍵を sandbox から使えるようにします。鍵そのものは PC に残りますが、sandbox からその鍵で GitHub などにログインや署名ができます。",
		"unrestricted は既定で true で、エージェントが持つ全鍵で何にでも署名できます。unrestricted: false と sign: [git] / authenticate: [git@github.com] で絞るのが推奨です。",
		"フェーズごとに、すべての Kit の権限が合算されます。"),
	d("cap:git-identity@1", "git-identity@1", MergeKitUnion, true, refSpec+"capabilities/com.docker.sandbox/git-identity@1.md",
		"あなたの Git の名前とメールアドレスを sandbox に伝えます。コミットの作者名に使われるだけで、ログインの権限は含みません。",
		"帰属表示のためのもので、認証や署名の権限は含みません。config は書けません（空でも不可）。",
		"1 つだけ設定できる要求です。"),
	d("cap:lifecycle@1", "lifecycle@1", MergeKitUnion, true, refSpec+"capabilities/com.docker.sandbox/lifecycle@1.md",
		"Kit が sandbox の中で実行する準備用のコマンドです。あなたの PC ではなく sandbox の中で動きます。",
		"ホストではなく sandbox 内で動きます。フックが見られる環境変数は env: に宣言したものだけ（既定拒否）。到達できる先は kit の他の capability（通信・資格情報・SSH）で制限されます。",
		"各 kit のフックは依存順に実行されます（連結）。"),
	d("cap:*", "未解析の capability", MergeKitUnion, true, refSpec+"SPEC-v3.md#73-unknown-types",
		"このツールが意味を解釈できない Kit の要求です。内容はそのまま表示していますが、何が起きるかは判断していません。",
		"未知の型が必須（optional でない）なら、それを知らないホストは kit を拒否します。optional なら省略して記録されます。privileged@1 や dockerd@1 は sandbox 内での最大の昇格です。",
		"型ごとの規則に従います（各仕様ページ参照）。"),
})

var docIndex = func() map[string]FieldDoc {
	m := map[string]FieldDoc{}
	for _, f := range fieldDocs {
		m[f.Key] = f
	}
	return m
}()

// LookupDoc returns the Japanese reference for an item path such as
// "env.NODE_ENV", "ports[]", or "cap:network-policy@1".
func LookupDoc(itemPath string) (FieldDoc, bool) {
	if f, ok := docIndex[itemPath]; ok {
		return f, true
	}
	for _, f := range fieldDocs {
		if ok, _ := path.Match(f.Key, itemPath); ok && f.Key != "cap:*" {
			return f, true
		}
	}
	if strings.HasPrefix(itemPath, "cap:") {
		return docIndex["cap:*"], true
	}
	return FieldDoc{}, false
}

// AllDocs returns every reference entry sorted by key.
func AllDocs() []FieldDoc {
	out := append([]FieldDoc(nil), fieldDocs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// MergeLabel returns the Japanese label of a merge rule.
func MergeLabel(r MergeRule) string { return mergeLabels[r] }

const (
	refProtect   = refEnv + "#protect-the-environment-file"
	refHostCmds  = refEnv + "#approve-or-skip-host-commands"
	refCapPrefix = refSpec + "capabilities/com.docker.sandbox/"
)

// officialNotes are summaries of what the official documentation says,
// keyed like fieldDocs. Only statements the documentation makes belong here.
var officialNotes = map[string][2]string{
	"workspace":                       {"workspace に指定したホストのディレクトリが sandbox にマウントされ、clone の既定は false です。clone: true は sbx create --clone と同じ非公開のクローンを使います。環境ファイルは、マウントするディレクトリ（メインと additionalWorkspaces のすべて）の外に置くよう求めています。", refEnv + "#workspace"},
	"additionalWorkspaces[]":          {"readOnly の既定は false（書き込み可能）で、置き換える側で readOnly を省略すると書き込み可能になると明記しています。環境ファイルは書き込み可能なマウントの外に置くよう求めています。", refEnv + "#additionalworkspaces"},
	"sandboxOptions.skills":           {"readwrite は sandbox が共有スキルを変更できるようにする設定です。省略時はデーモンの既定で、組織が上書きしない限り readonly です。", refEnv + "#sandboxoptions"},
	"sandboxOptions.writableEnvFiles": {"環境ファイルはホストで実行するライフサイクル・秘密・MCP のコマンドを宣言できるため、書き込み可能なマウントの外に置くこと。ワークスペース直下に置いた場合は既定で読み取り専用でマウントされます。writableEnvFiles: true は、将来ホストで実行されるコマンドをエージェントが変更してよいと信頼できる場合にだけ使うこと、としています。", refProtect},
	"secrets.*":                       {"value の直書きはファイルを読める人に見えてしまうため、ref（保管庫の参照）か command（実行時に取得）を使うよう警告しています。command の補助スクリプトは書き込み可能な sandbox マウントの外に置き、絶対パスなどで指定すること。秘密の command は --skip-host-commands を付けても必要なときに実行されます。", refEnv + "#secrets"},
	"bindings.*.apiKey":               {"bindings はユーザー全体の credentials.yaml に結合され、sbx env rm でも既定では残ります。--prune-bindings はファイルに書かれたサービスの全体の承認をまとめて消すため、同じ承認を使う他の sandbox にも影響すると警告しています。", refEnv + "#bindings"},
	"bindings.*.oauth":                {"bindings はユーザー全体の credentials.yaml に結合され、sbx env rm でも既定では残ります。--prune-bindings は他の sandbox にも影響し得ると警告しています。", refEnv + "#bindings"},
	"mcp.servers[]":                   {"command 型のサーバーは、MCP ゲートウェイが起動するたびに（再起動後も）ホスト上であなたの権限で動きます。承認で固定されるのは実行ファイルのパスだけで、中身やスクリプトは固定されないため、それらは書き込み可能なマウントの外に置くこと。ホスト上のコマンドを含む計画は既定で毎回承認が必要で、--skip-host-commands で無効にできます。", refEnv + "#approve-local-mcp-commands"},
	"ports[]":                         {"hostIP の既定はループバック（あなたの PC の中からだけ接続可）、host の既定は空きポートの自動割り当てです。", refEnv + "#ports"},
	"lifecycle.initialize[]":          {"lifecycle のコマンドはホスト上であなたのユーザー権限で動きます。ホスト上のコマンドを含む計画は既定で毎回承認が必要で、--skip-host-commands で実行しないようにできます。", refHostCmds},
	"lifecycle.postCreate[]":          {"lifecycle のコマンドはホスト上であなたのユーザー権限で動きます。ホスト上のコマンドを含む計画は既定で毎回承認が必要で、--skip-host-commands で実行しないようにできます。", refHostCmds},
	"lifecycle.preRemove[]":           {"lifecycle のコマンドはホスト上であなたのユーザー権限で動きます。preRemove が失敗しても警告だけで削除は続きます。--skip-host-commands で実行しないようにできます。", refHostCmds},
	"kits[]":                          {"再現性のため、Git の Kit は ref パラメーターで、OCI の Kit は変更されないタグかハッシュ値で固定するよう勧めています。", refEnv + "#kits"},
	"cap:network-policy@1":            {"既定は全遮断（deny-by-default）で、allow に一致しない通信は拒否され、書かれていないフェーズは何も許可しません。allow と deny の両方に一致する通信先は拒否されます（deny が優先）。ある Kit の deny は別の Kit の allow では打ち消せません。", refCapPrefix + "network-policy@1.md"},
	"cap:credential@1":                {"秘密の置き場所はホストの認証情報ストアで、ホストの環境変数が自動で渡されることはありません。proxyManaged では本物の値はホストに残り、sandbox の中には置き換え用の値だけが見えます。passthrough: true は明示的なセキュリティの格下げで、ランタイムは拒否してもよいとしています。", refCapPrefix + "credential@1.md"},
	"cap:host-mount@1":                {"1 つのホストのディレクトリを、ホストおよび同じ Kit を使う他の sandbox と共有します。sandbox からの書き込みはそのディレクトリに届き、データはホストと他の sandbox から見えます。", refCapPrefix + "host-mount@1.md"},
	"cap:ssh-agent@1":                 {"秘密鍵は sandbox に置かれずホストに残ります。unrestricted の既定は true で、その場合は sandbox が求めるものに、公開されたすべての鍵で署名します（任意のサーバーへのログイン、あなたの名義の署名など）。unrestricted: false と sign / authenticate で操作を絞れます。", refCapPrefix + "ssh-agent@1.md"},
	"cap:lifecycle@1":                 {"フックが見られる環境変数は、env に宣言した名前と最小限の基本変数（PATH、HOME など）だけで、その値もホストの環境からは取られません。", refCapPrefix + "lifecycle@1.md"},
}

func withOfficial(docs []FieldDoc) []FieldDoc {
	for i := range docs {
		if n, ok := officialNotes[docs[i].Key]; ok {
			docs[i].OfficialNote, docs[i].OfficialRef = n[0], n[1]
		}
	}
	return docs
}
