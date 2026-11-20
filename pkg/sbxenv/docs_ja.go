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
	MergeKitUnion:    "kit 間で和集合（合成時）",
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
var fieldDocs = []FieldDoc{
	d("schemaVersion", "schemaVersion", MergeReplace, false, refEnv+"#top-level-fields",
		"環境ファイルの書式バージョン。現在サポートされる値は文字列 \"1\" だけです。",
		"kit 記述子の schemaVersion（\"3\"）とは無関係です。数値の 1 ではなく引用符付きの \"1\" で書きます。",
		scalarOverride),
	d("name", "name", MergeReplace, true, refEnv+"#top-level-fields",
		"作られる sandbox の名前。省略すると自動生成されます。",
		"--name を渡すとファイルの値より優先されます。~/.sbxenv.yaml には書けません。create / exec / rm で同じ名前を使わないと別の sandbox として扱われます。",
		scalarOverride+" さらに --name がすべてのファイルより優先されます。"),
	d("agent", "agent", MergeReplace, true, refEnv+"#top-level-fields",
		"sandbox で動かすエージェント。組み込み名（claude・codex など）か、kits に並べた workload kit のハンドル（リポジトリ末尾の名前やディレクトリ名）を書きます。",
		"ワークロードは 1 つだけで、他の kit はすべて mixin である必要があります。組み込みエージェント自身のネットワーク許可や資格情報はこのツールからは見えません（Coverage に表示）。",
		scalarOverride+" agent を変えたら sandbox を作り直します。"),
	d("entrypoint", "entrypoint", MergeListReplace, false, refEnv+"#set-the-launch-command",
		"sbx env run が sandbox 内で起動する実行ファイルと固定引数（文字列のリスト）。",
		"シェルとしては解釈されません。シェル式を使うなら [/bin/sh, -c] のように明示します。exec や PID 1 には影響しません。",
		"後のレイヤーのリストが前のリストを丸ごと置き換えます（連結しません）。省略または null なら前の値を引き継ぎ、[] を書くと消去します。"),
	d("cmd", "cmd", MergeListReplace, false, refEnv+"#set-the-launch-command",
		"entrypoint の後ろに付く既定の引数。`sbx env run -- 引数` で渡した引数がこのリストを置き換えます。",
		"変更は作り直さずに次の run から効きます。",
		"後のレイヤーのリストが前のリストを丸ごと置き換えます。省略または null なら引き継ぎ、[] で消去します。"),
	d("args.*", "args.<NAME>", MergeArgs, false, refEnv+"#parameterize-an-environment",
		"環境ファイルの入力パラメーター。値は ${{ env.args.NAME }} で参照します。default か required: true のどちらか一方を必ず設定します。",
		"enum（候補）と pattern（RE2 の完全一致）は同時に使えません。args ブロックの中では式は展開されません。",
		"宣言自体はマップ結合（同名なら後のレイヤーの宣言が勝つ）。値の優先順位は 既定値 < --env-args-file（後のファイルほど優先）< --env-arg です。"),
	d("workspace", "workspace", MergeReplace, true, refEnv+"#workspace",
		"sandbox にマウントするメインの作業ディレクトリ。エージェントの変更はこのホストディレクトリに直接書き込まれます（clone: true なら非公開の Git クローン）。",
		"相対パスは「その行を書いたファイル」のディレクトリ基準で解決されます。環境ファイル自体をこのマウントの中（特にサブディレクトリ）に置くと、エージェントが次回のホストコマンドを書き換えられる恐れがあります。--clone / --clone=false でその回だけ clone を上書きできます。",
		"文字列とオブジェクトのどちらで書いても、後のレイヤーが置き換えます（オブジェクト同士なら path / clone がキー単位で結合）。"),
	d("additionalWorkspaces[]", "additionalWorkspaces[]", MergeKeyed, true, refEnv+"#additionalworkspaces",
		"メインのワークスペースに加えてマウントする追加ディレクトリ。readOnly: true で読み取り専用になります。",
		"メインが clone モードでも追加分は直接マウントされます。target を省略するとホストと同じパスでマウントされます。",
		"マウント先（target、省略時は解決後のホストパス）が同じ要素は、最後の要素がまるごと置き換え、最初の位置に残ります。置き換え側で readOnly を省略すると書き込み可能に戻る点に注意。"),
	d("mounts.*", "mounts.<sandboxPath>", MergeMap, true, refEnv+"#mounts",
		"kit の host-mount@1 が要求した sandbox 内パスに、自分で選んだホストディレクトリを割り当てます（ローカルのみ）。",
		"割り当てないと、その kit を使う全 sandbox が sbx 管理の 1 つのホストディレクトリを共有します。キーは kit が宣言したパスと一致している必要があります。",
		mapOverride),
	d("env.*", "env.<VAR>", MergeMap, false, refEnv+"#top-level-fields",
		"sandbox 内のエージェントに渡す環境変数。",
		"ホストのライフサイクルコマンドには渡されません。秘密はここに書かず secrets / credential@1 を使います。既存 sandbox でも次の sbx env run のセッションから反映されます。",
		mapOverride),
	d("sandboxOptions.template", "sandboxOptions.template", MergeReplace, true, refEnv+"#sandboxoptions",
		"sandbox の元になるカスタムテンプレートイメージ。",
		":dev のような可変タグは作成のたびに中身が変わり得ます。再現性が必要ならダイジェストで固定します。中身はこのツールでは解析しません。",
		scalarOverride),
	d("sandboxOptions.memory", "sandboxOptions.memory", MergeReplace, true, refEnv+"#sandboxoptions",
		"メモリ上限（例: 8g、512m）。", "cloud では cloud のサイズに合わせる必要があります。", scalarOverride),
	d("sandboxOptions.cpus", "sandboxOptions.cpus", MergeReplace, true, refEnv+"#sandboxoptions",
		"CPU 数。0 はホストの既定（ローカルでは全 CPU、Linux arm64 は 16 が上限）。",
		"引用符なしの ${{ env.args.X }} は置換後に整数として解釈されます。", scalarOverride),
	d("sandboxOptions.pullPolicy", "sandboxOptions.pullPolicy", MergeReplace, true, refEnv+"#sandboxoptions",
		"テンプレートイメージの取得方針: always（既定）/ missing / never。",
		"未指定でも always として扱われるため、:dev などの可変タグと組み合わさると毎回最新版が入ります。",
		scalarOverride),
	d("sandboxOptions.profile", "sandboxOptions.profile", MergeReplace, true, refEnv+"#sandboxoptions",
		"ガバナンスプロファイル名。", "組織のポリシーは解析範囲外です。", scalarOverride),
	d("sandboxOptions.skills", "sandboxOptions.skills", MergeReplace, true, refEnv+"#sandboxoptions",
		"共有エージェントスキル置き場へのアクセス: off（マウントしない）/ readonly / readwrite。",
		"未指定ならデーモン既定（通常 readonly）。readwrite にすると、この sandbox がホストの共有スキルを書き換えられます。off は YAML の真偽値ではなく文字列として扱われます。",
		scalarOverride),
	d("sandboxOptions.display", "sandboxOptions.display", MergeReplace, true, refEnv+"#sandboxoptions",
		"GUI アプリ用のディスプレイソケットを用意します。", "", scalarOverride),
	d("sandboxOptions.fuse", "sandboxOptions.fuse", MergeReplace, true, refEnv+"#sandboxoptions",
		"FUSE ファイルシステムのマウントを許可します（ローカルのみ）。", "未指定時は sbx の sandbox.fuse 設定に従います。", scalarOverride),
	d("sandboxOptions.gpu", "sandboxOptions.gpu", MergeReplace, true, refEnv+"#sandboxoptions",
		"ホストの GPU を sandbox に渡します。", "", scalarOverride),
	d("sandboxOptions.usb", "sandboxOptions.usb", MergeAppend, true, refEnv+"#sandboxoptions",
		"sandbox に渡す USB デバイスのセレクター一覧。", "", appendOverride),
	d("sandboxOptions.writableEnvFiles", "sandboxOptions.writableEnvFiles", MergeReplace, true, refEnv+"#protect-the-environment-file",
		"sandbox から環境ファイルを書き換えられるようにします（ローカルのみ）。",
		"環境ファイルはホストで実行するコマンドを決めるため、true にするとエージェントが次回以降のホスト実行内容を変えられます。",
		scalarOverride),
	d("secrets.*", "secrets.<service>", MergeMap, true, refEnv+"#secrets",
		"サービスの資格情報の出どころ。value（直書き）/ ref（op:// などのボルト参照）/ command（ホストで実行し標準出力を秘密にする）のどれか 1 つを指定します。",
		"sandbox スコープで保存され、プロキシが送信時に差し込みます。command は --skip-host-commands でも実行されます。value の直書きはファイルを読める人に丸見えです。",
		"サービス名ごとにマップ結合され、同じサービスのフィールドもキー単位で結合されます。そのため前のレイヤーの ref と後のレイヤーの command が同居して「どれか 1 つ」の規則に反することがあります（このツールはエラーとして表示）。"),
	d("bindings.*.apiKey", "bindings.<service>.apiKey", MergeMap, true, refEnv+"#bindings",
		"kit の credential@1（apiKey 方式）が資格情報を差し込んでよいドメインの承認。",
		"ユーザー全体の credentials.yaml に結合され、sbx env rm でも残ります（--prune-bindings で削除、ただし他の sandbox と共有）。秘密の値そのものは持ちません。",
		"マップ結合。domains リストは連結されます。"),
	d("bindings.*.oauth", "bindings.<service>.oauth", MergeMap, true, refEnv+"#bindings",
		"kit の credential@1（OAuth 方式）が使うドメインの承認。",
		"credentials.yaml に結合され sbx env rm 後も残ります。",
		"マップ結合。domains リストは連結されます。"),
	d("registries.*", "registries.<host>", MergeMap, true, refEnv+"#registries",
		"レジストリ（ghcr.io など）からイメージを pull するための資格情報。secret は必須、username は任意。",
		"secret / username は secrets と同じく value / ref / command のどれか 1 つで指定します。",
		mapOverride),
	d("mcp.servers[]", "mcp.servers[]", MergeKeyed, false, refEnv+"#mcp",
		"MCP ゲートウェイに登録するサーバー。url（リモート）か command（ホストで動く stdio サーバー）のどちらか一方。",
		"command 型はゲートウェイ起動のたびにあなたの権限でホスト上で動きます（--skip-host-commands で無効化）。url 型はホスト全体に登録され sbx env rm 後も残ります。",
		"name が同じ要素は最後の要素がまるごと置き換え、最初の位置に残ります。名前の違う要素は連結されます。"),
	d("ports[]", "ports[]", MergeKeyed, true, refEnv+"#ports",
		"sandbox のポートをホストに公開します。",
		"hostIP 未指定はループバックのみ。0.0.0.0 などを指定すると LAN から到達可能になります。host 省略時は空いているポートが自動で割り当てられます。",
		"sandbox ポートと実効プロトコル（省略時 tcp4、IPv6 の hostIP なら tcp6）が同じ要素は、最後の要素がまるごと置き換えます。置き換え側で host を省略するとエフェメラルポートに戻ります。"),
	d("lifecycle.initialize[]", "lifecycle.initialize[]", MergeAppend, false, refEnv+"#lifecycle",
		"create / run のたびに、他の処理より前にホストで実行されるコマンド。",
		"あなたのユーザー権限で動きます。何度実行しても安全に書きます。workdir の相対パスは「最初のファイル」のディレクトリ基準です。env や秘密は渡されません。",
		appendOverride+" 1 つ失敗すると以降は止まります。"),
	d("lifecycle.postCreate[]", "lifecycle.postCreate[]", MergeAppend, false, refEnv+"#lifecycle",
		"新しい sandbox を作った直後にホストで実行されるコマンド（既存 sandbox への接続時は動かない）。",
		"秘密投入スクリプトなどに使われます。SBX_SANDBOX_NAME などの変数を受け取ります。",
		appendOverride),
	d("lifecycle.preRemove[]", "lifecycle.preRemove[]", MergeAppend, false, refEnv+"#lifecycle",
		"削除を承認した後、リソースを消す前にホストで実行されるコマンド。",
		"失敗しても警告だけで削除は続きます。cloud で期限切れ削除された場合は動きません。",
		appendOverride),
	d("kits[]", "kits[]", MergeKitArgs, true, refEnv+"#kits",
		"作成時に入れる kit。ローカルディレクトリ（./ や ../ で始まる）、ZIP、OCI 参照、git+https:// / git+ssh:// の Git URL を指定できます。",
		"ローカル以外はこのツールでは展開しないため、その kit が要求する通信・資格情報・ホストマウントは見えません（Coverage）。OCI はダイジェスト、Git は ref= で固定すると再現性が上がります。--kit-arg / --kit-args-file でその回だけ kit 引数を上書きできます。",
		"source が同じ要素は 1 つにまとまり、args はキー単位で結合されます（後の値が勝ち、他の引数は残る）。source が違えば連結されます。"),
	d("cap:network-policy@1", "network-policy@1", MergeKitUnion, true, refSpec+"capabilities/com.docker.sandbox/network-policy@1.md",
		"kit が要求する外向き通信の許可。install（インストールフック中だけ）と runtime（エージェント稼働中）でフェーズが分かれ、書かれていないフェーズは何も許可しません。",
		"deny は allow より優先します。*.example.com はサブドメイン 1 階層だけ、* / ** は全許可です。ホストの sbx policy（グローバル規則）も別に効きます。",
		"複数 kit の allow / deny はフェーズごとに和集合になり、ある kit の deny を別の kit の allow で打ち消すことはできません。"),
	d("cap:credential@1", "credential@1", MergeKitUnion, true, refSpec+"capabilities/com.docker.sandbox/credential@1.md",
		"kit が 1 つのサービスへの認証を要求する宣言。秘密の置き場所ではなく、どの環境変数名・どのドメインのどのヘッダーに差し込むかを決めます。",
		"既定は必須で、環境ファイルに bindings.<service> がないと作成が失敗します（optional: true なら黙って省略）。inject のドメインは同じ kit の network-policy@1 の allow に完全一致で載っている必要があります。proxyManaged: true なら sandbox には置き換え用の値しか入りません。",
		"kit をまたいで和集合ですが、同じ（サービス, フェーズ）を 2 つの kit が宣言すると合成エラーになります。"),
	d("cap:host-mount@1", "host-mount@1", MergeKitUnion, true, refSpec+"capabilities/com.docker.sandbox/host-mount@1.md",
		"kit が sandbox 内パスにホストのディレクトリを共有するよう要求します。データは sandbox を消しても残り、ホストから見えます。",
		"ホスト側の場所は sbx が決め、同じ kit を使う全 sandbox で共有されます。sandbox ごとに分けるには環境ファイルの mounts: で割り当てます。",
		"パスは kit 間で和集合。別の kit が同じパスを宣言すると衝突（黙って統合はされない）します。"),
	d("cap:ssh-agent@1", "ssh-agent@1", MergeKitUnion, true, refSpec+"capabilities/com.docker.sandbox/ssh-agent@1.md",
		"ホストの SSH エージェントを sandbox から使えるようにします。秘密鍵はホストに残りますが、署名は sandbox から依頼できます。",
		"unrestricted は既定で true で、エージェントが持つ全鍵で何にでも署名できます。unrestricted: false と sign: [git] / authenticate: [git@github.com] で絞るのが推奨です。",
		"フェーズごとに権限が和集合になります。"),
	d("cap:git-identity@1", "git-identity@1", MergeKitUnion, true, refSpec+"capabilities/com.docker.sandbox/git-identity@1.md",
		"ホスト側で選ばれた Git の user.name / user.email を sandbox の既定値として渡します。",
		"帰属表示のためのもので、認証や署名の権限は含みません。config は書けません（空でも不可）。",
		"単一（シングルトン）の要求です。"),
	d("cap:lifecycle@1", "lifecycle@1", MergeKitUnion, true, refSpec+"capabilities/com.docker.sandbox/lifecycle@1.md",
		"kit のセットアップ処理。install フックは作成時に 1 回、startup フックは起動のたびに sandbox の中で動きます。files は起動時に書かれるファイルです。",
		"ホストではなく sandbox 内で動きます。フックが見られる環境変数は env: に宣言したものだけ（既定拒否）。到達できる先は kit の他の capability（通信・資格情報・SSH）で制限されます。",
		"各 kit のフックは依存順に実行されます（連結）。"),
	d("cap:*", "未解析の capability", MergeKitUnion, true, refSpec+"SPEC-v3.md#73-unknown-types",
		"このツールが意味を解釈していない capability。設定は取りこぼさずそのまま表示しています。",
		"未知の型が必須（optional でない）なら、それを知らないホストは kit を拒否します。optional なら省略して記録されます。privileged@1 や dockerd@1 は sandbox 内での最大の昇格です。",
		"型ごとの規則に従います（各仕様ページ参照）。"),
}

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
