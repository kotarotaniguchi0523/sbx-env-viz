package sbxenv

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kotarotaniguchi0523/sbx-env-viz/internal/parser"
)

// builtinServices are services sbx ships credential handling for, which do
// not need a credential@1 declaration from a kit (see `sbx secret set --help`).
var builtinServices = map[string]bool{
	"anthropic": true, "openai": true, "github": true, "google": true, "gemini": true,
	"mistral": true, "groq": true, "xai": true, "openrouter": true, "aws": true,
}

func (rs *resolver) analyze() {
	rs.checkEnvArgUse()
	rs.checkValues()
	rs.checkCredentialTriad()
	rs.checkHostMounts()
	rs.checkTemplate()
	rs.checkCloud()
	rs.checkWritableLocations()
	rs.checkSSHAndOthers()
	rs.coverage(CoverageHostPolicy, "sbx policy / settings / credentials.yaml",
		"host-wide network rules (sbx policy), kit.allowedSources, global bindings already in credentials.yaml, and organization governance profiles also apply but live outside these files", Origin{SourceID: "host"})
}

func (rs *resolver) checkEnvArgUse() {
	final := map[string]bool{}
	rs.root.collectArgRefs(final)
	for _, a := range rs.envArgs {
		if a.From != "flag" && a.From != "env-args-file" {
			continue
		}
		if final[a.Name] {
			continue
		}
		var lost []lostRef
		for _, l := range rs.lost {
			if l.arg == a.Name {
				lost = append(lost, l)
			}
		}
		flag := "--env-arg"
		if a.From == "env-args-file" {
			flag = "--env-args-file"
		}
		if len(lost) > 0 {
			origins := []Origin{a.Origin}
			var where []string
			for _, l := range lost {
				origins = append(origins, l.shadowed, l.overridden)
				where = append(where, fmt.Sprintf("%s (%s:%d)", l.path, l.overridden.SourceID, l.overridden.Line))
			}
			rs.diag("ENV_ARG_INEFFECTIVE", LevelWarning, "args",
				fmt.Sprintf("%s %s has no effect: every field that referenced it is overridden by a later layer with a fixed value at %s", flag, a.Name, strings.Join(where, ", ")),
				"args."+a.Name, origins...)
			continue
		}
		rs.diag("ENV_ARG_UNUSED", LevelInfo, "args", fmt.Sprintf("%s %s is declared but no field references ${{ env.args.%s }}", flag, a.Name, a.Name), "args."+a.Name, a.Origin)
	}
}

func (rs *resolver) checkValues() {
	so := rs.root.get("sandboxOptions")
	enum := func(key string, allowed ...string) {
		v := so.get(key)
		if v != nil && v.kind == parser.Scalar && !contains(allowed, v.val) {
			rs.diag("INVALID_VALUE", LevelError, "schema", fmt.Sprintf("sandboxOptions.%s must be one of %s", key, strings.Join(allowed, ", ")), "sandboxOptions."+key, v.origin())
		}
	}
	enum("pullPolicy", "always", "missing", "never")
	enum("skills", "off", "readonly", "readwrite")
	if v := so.get("writableEnvFiles"); v != nil && v.val == "true" {
		rs.diag("WRITABLE_ENV_FILES", LevelWarning, "boundary", "sandboxOptions.writableEnvFiles lets the agent rewrite the environment files that decide which host commands later invocations run", "sandboxOptions.writableEnvFiles", v.origin())
	}
	if v := so.get("skills"); v != nil && v.val == "readwrite" {
		rs.diag("SKILLS_READWRITE", LevelWarning, "boundary", "sandboxOptions.skills: readwrite lets this sandbox modify the host's shared skills store, which other sandboxes and agents read", "sandboxOptions.skills", v.origin())
	}
	for _, e := range rs.root.get("ports").list() {
		key := "ports[" + listKey("ports", e) + "]"
		if p := strings.ToLower(e.str("protocol")); p != "" && !contains([]string{"tcp", "tcp4", "tcp6", "udp", "udp4", "udp6"}, p) {
			rs.diag("INVALID_VALUE", LevelError, "schema", "ports[].protocol must be tcp, tcp4, tcp6, udp, udp4, or udp6", key, e.origin())
		}
		if !e.has("sandbox") {
			rs.diag("PORT_SANDBOX_MISSING", LevelError, "schema", "ports[] entries require sandbox", "", e.origin())
		}
		ip := e.str("hostIP")
		if ip == "0.0.0.0" || ip == "::" || ip == "[::]" {
			rs.diag("PORT_ALL_INTERFACES", LevelWarning, "boundary", fmt.Sprintf("%s binds every host interface, so other machines on the network can reach the sandbox service", key), key, e.origin())
		}
	}
	for _, e := range rs.root.get("mcp").get("servers").list() {
		n := 0
		for _, k := range []string{"url", "command"} {
			if e.has(k) {
				n++
			}
		}
		if n != 1 {
			rs.diag("MCP_SOURCE_COUNT", LevelError, "schema", fmt.Sprintf("mcp.servers[%s] must set exactly one of url or command", e.str("name")), "mcp.servers["+e.str("name")+"]", e.origin())
		}
	}
	checkSource := func(id string, v *vnode) {
		n := 0
		for _, k := range []string{"value", "ref", "command"} {
			if v.has(k) {
				n++
			}
		}
		if n != 1 {
			rs.diag("SECRET_SOURCE_COUNT", LevelError, "credentials", fmt.Sprintf("%s must set exactly one of value, ref, or command (merged layers can combine two)", id), id, v.origin())
		}
		if v.has("value") {
			rs.diag("SECRET_LITERAL", LevelWarning, "credentials", fmt.Sprintf("%s holds a literal value; anyone who can read the file can read it. Use ref or command", id), id, v.get("value").origin())
		}
		if v.str("snapshot") == "true" && (v.has("refresh") || v.has("noVerify")) {
			rs.diag("SECRET_SNAPSHOT_CONFLICT", LevelError, "credentials", fmt.Sprintf("%s: a snapshot cannot set refresh or noVerify", id), id, v.origin())
		}
	}
	for _, svc := range rs.root.get("secrets").mapKeys() {
		checkSource("secrets."+svc, rs.root.get("secrets").get(svc))
	}
	for _, host := range rs.root.get("registries").mapKeys() {
		r := rs.root.get("registries").get(host)
		if s := r.get("secret"); s != nil {
			checkSource("registries."+host, s)
		} else {
			rs.diag("REGISTRY_SECRET_MISSING", LevelError, "credentials", fmt.Sprintf("registries.%s requires secret", host), "registries."+host, r.origin())
		}
	}
	for _, phase := range []string{"initialize", "postCreate", "preRemove"} {
		for i, e := range rs.root.get("lifecycle").get(phase).list() {
			if !e.has("command") {
				rs.diag("LIFECYCLE_COMMAND_MISSING", LevelError, "schema", fmt.Sprintf("lifecycle.%s[%d] requires command", phase, i), fmt.Sprintf("lifecycle.%s[%d]", phase, i), e.origin())
			}
		}
	}
	if sv := rs.root.get("schemaVersion"); sv == nil {
		rs.diag("SCHEMA_VERSION", LevelError, "schema", "schemaVersion is required (\"1\")", "")
	}
}

type credUse struct {
	cap       *capState
	service   string
	phases    []string
	apiKey    bool
	oauth     bool
	domains   []string
	oauthDoms []string
}

func (rs *resolver) checkCredentialTriad() {
	var uses []credUse
	owner := map[string]*capState{}
	for _, k := range rs.kits {
		allow := map[string][]string{}
		for _, c := range k.caps {
			if c.short == "network-policy@1" {
				for _, ph := range []string{"install", "runtime"} {
					allow[ph] = append(allow[ph], c.config.get(ph).get("allow").scalars()...)
				}
			}
		}
		for _, c := range k.caps {
			if c.short != "credential@1" {
				continue
			}
			cfg := c.config
			u := credUse{cap: c, service: cfg.str("service"), phases: phases(cfg.get("phase"))}
			if ak := cfg.get("apiKey"); ak != nil {
				u.apiKey = true
				for _, inj := range ak.get("inject").list() {
					u.domains = append(u.domains, inj.str("domain"))
				}
				if ak.str("name") == "" && len(ak.get("inject").list()) == 0 {
					rs.diag("CREDENTIAL_INVALID", LevelError, "credentials", fmt.Sprintf("kit %s credential %s: apiKey needs a name or inject rules", k.handle, u.service), "", c.origin)
				}
			}
			if oa := cfg.get("oauth"); oa != nil {
				u.oauth = true
				u.oauthDoms = append(u.oauthDoms, oa.get("resourceHosts").scalars()...)
				if h := oa.get("tokenEndpoint").str("host"); h != "" {
					u.oauthDoms = append(u.oauthDoms, h)
				}
				if oa.str("passthrough") == "true" {
					rs.diag("OAUTH_PASSTHROUGH", LevelWarning, "credentials", fmt.Sprintf("kit %s credential %s sets oauth.passthrough, returning the real token to the sandbox", k.handle, u.service), "", c.origin)
				}
			}
			if !u.apiKey && !u.oauth {
				rs.diag("CREDENTIAL_INVALID", LevelError, "credentials", fmt.Sprintf("kit %s credential %s declares neither apiKey nor oauth", k.handle, u.service), "", c.origin)
			}
			if u.service == "" || len(u.phases) == 0 {
				rs.diag("CREDENTIAL_INVALID", LevelError, "credentials", fmt.Sprintf("kit %s credential@1 requires service and phase", k.handle), "", c.origin)
			}
			for _, ph := range u.phases {
				key := u.service + "/" + ph
				if prev, ok := owner[key]; ok {
					rs.diag("CREDENTIAL_CONFLICT", LevelError, "credentials",
						fmt.Sprintf("credential %s (%s phase) is declared by both kit %s and kit %s; one credential has one owner", u.service, ph, prev.kit.handle, k.handle), "", prev.origin, c.origin)
				} else {
					owner[key] = c
				}
				for _, d := range u.domains {
					if !domainAllowed(d, allow[ph]) {
						rs.diag("CREDENTIAL_INJECT_NOT_ALLOWED", LevelError, "credentials",
							fmt.Sprintf("kit %s credential %s injects into %s, but the kit's network-policy@1 %s allow list does not list it exactly (wildcards do not count)", k.handle, u.service, d, ph), "", c.origin)
					}
				}
			}
			uses = append(uses, u)
		}
	}
	bindings := rs.root.get("bindings")
	secrets := rs.root.get("secrets")
	anyRemote := false
	for _, k := range rs.kits {
		if !k.expanded {
			anyRemote = true
		}
	}
	used := map[string]bool{}
	missingSecret := false
	for _, u := range uses {
		used[u.service] = true
		b := bindings.get(u.service)
		itemID := "kit:" + u.cap.kit.handle + "/credential@1/" + u.service + "/" + strings.Join(u.phases, ",")
		check := func(mech string, need bool, doms []string) {
			if !need {
				return
			}
			bm := b.get(mech)
			if bm == nil {
				if u.cap.optional {
					rs.diag("CREDENTIAL_UNBOUND", LevelWarning, "credentials",
						fmt.Sprintf("kit %s requests credential %s (%s) as optional, but bindings.%s.%s is missing; sbx silently skips it and the kit runs unauthenticated", u.cap.kit.handle, u.service, mech, u.service, mech), itemID, u.cap.origin)
				} else {
					rs.diag("CREDENTIAL_UNBOUND", LevelError, "credentials",
						fmt.Sprintf("kit %s requires credential %s (%s), but bindings.%s.%s is missing; sbx env create fails", u.cap.kit.handle, u.service, mech, u.service, mech), itemID, u.cap.origin)
				}
				return
			}
			have := bm.get("domains").scalars()
			var missing []string
			for _, d := range doms {
				if !contains(have, d) {
					missing = append(missing, d)
				}
			}
			if len(missing) > 0 {
				rs.diag("BINDING_DOMAIN_MISSING", LevelWarning, "credentials",
					fmt.Sprintf("bindings.%s.%s.domains does not approve %s used by kit %s; sbx asks for approval at create time (and fails unattended runs)", u.service, mech, strings.Join(missing, ", "), u.cap.kit.handle), "bindings."+u.service+"."+mech, bm.origin(), u.cap.origin)
			}
		}
		check("apiKey", u.apiKey, u.domains)
		check("oauth", u.oauth, u.oauthDoms)
		if secrets.get(u.service) == nil {
			missingSecret = true
			rs.diag("SECRET_NOT_DECLARED", LevelInfo, "credentials",
				fmt.Sprintf("credential %s has no secrets.%s entry; it must already exist in the sbx secret store (sbx secret ls), which this tool cannot see", u.service, u.service), itemID, u.cap.origin)
		}
	}
	if missingSecret {
		rs.coverage(CoverageGlobalSecrets, "sbx secret store", "secrets stored globally or per sandbox with sbx secret set are not visible to static analysis", Origin{SourceID: "host"})
	}
	for _, svc := range bindings.mapKeys() {
		if used[svc] || builtinServices[svc] {
			continue
		}
		level := LevelWarning
		msg := fmt.Sprintf("bindings.%s approves injection, but no expanded kit declares credential@1 for %s and it is not a built-in service", svc, svc)
		if anyRemote {
			level = LevelInfo
			msg += " (a remote kit may declare it)"
		}
		rs.diag("BINDING_UNUSED", level, "credentials", msg, "", bindings.get(svc).origin())
	}
	for _, svc := range secrets.mapKeys() {
		if used[svc] || builtinServices[svc] || bindings.get(svc) != nil {
			continue
		}
		level := LevelWarning
		msg := fmt.Sprintf("secrets.%s is stored but nothing consumes it: no binding, no expanded kit credential@1, and not a built-in service", svc)
		if anyRemote {
			level = LevelInfo
			msg += " (a remote kit may declare it)"
		}
		rs.diag("SECRET_UNUSED", level, "credentials", msg, "secrets."+svc, secrets.get(svc).origin())
	}
}

func domainAllowed(d string, allow []string) bool {
	for _, a := range allow {
		if a == "*" || a == "**" {
			return true
		}
		host := a
		if i := strings.LastIndex(a, ":"); i > 0 && !strings.Contains(a[i:], "]") {
			host = a[:i]
		}
		if host == d {
			return true
		}
	}
	return false
}

func (rs *resolver) checkHostMounts() {
	mounts := rs.root.get("mounts")
	declared := map[string]bool{}
	allExpanded := true
	for _, k := range rs.kits {
		if !k.expanded {
			allExpanded = false
		}
		for _, c := range k.caps {
			if c.short != "host-mount@1" {
				continue
			}
			p := c.config.str("path")
			declared[p] = true
			if p == "" || !strings.HasPrefix(p, "/") || strings.Contains(p, "/../") || strings.HasSuffix(p, "/") || strings.Contains(p, "//") || strings.Contains(p, "/./") {
				rs.diag("HOST_MOUNT_PATH_INVALID", LevelError, "boundary", fmt.Sprintf("kit %s host-mount@1 path must be absolute and canonical", k.handle), "", c.origin)
			}
			if mounts.get(p) == nil {
				rs.diag("HOST_MOUNT_SHARED", LevelWarning, "boundary",
					fmt.Sprintf("kit %s host-mount@1 %s has no mounts: entry, so every sandbox composing this kit shares one host directory; add mounts: { %s: <dir> } to give this environment its own", k.handle, p, p),
					"kit:"+k.handle+"/host-mount@1/"+p, c.origin)
			}
		}
	}
	for _, p := range mounts.mapKeys() {
		if declared[p] {
			continue
		}
		v := mounts.get(p)
		if allExpanded {
			rs.diag("MOUNT_TARGET_UNKNOWN", LevelError, "boundary", fmt.Sprintf("mounts.%s does not match a host-mount@1 path of any selected kit", p), "mounts."+p, v.origin())
		} else {
			rs.diag("MOUNT_TARGET_UNKNOWN", LevelInfo, "boundary", fmt.Sprintf("mounts.%s does not match a host-mount@1 path of an expanded kit (a remote kit may declare it)", p), "mounts."+p, v.origin())
		}
	}
	if rs.opts.Cloud && len(mounts.mapKeys()) > 0 {
		rs.diag("CLOUD_UNSUPPORTED", LevelError, "cloud", "mounts applies to local sandboxes only", "mounts", mounts.origin())
	}
}

func (rs *resolver) checkTemplate() {
	so := rs.root.get("sandboxOptions")
	t := so.get("template")
	if t == nil || t.val == "" {
		return
	}
	ref := t.val
	tag := ""
	if !strings.Contains(ref, "@sha256:") {
		last := ref[strings.LastIndex(ref, "/")+1:]
		if i := strings.LastIndex(last, ":"); i >= 0 {
			tag = last[i+1:]
		} else {
			tag = "latest"
		}
	}
	pp := so.get("pullPolicy")
	policy := "always"
	origins := []Origin{t.origin()}
	if pp != nil {
		policy = pp.val
		origins = append(origins, pp.origin())
	} else {
		it := rs.addItem(Item{ID: "sandboxOptions.pullPolicy", Path: "sandboxOptions.pullPolicy", Section: SectionSandbox, Value: "always",
			Risk: SeverityInfo, Origins: []Origin{{SourceID: "default"}},
			History: []HistoryEntry{{Op: OpDefault, Value: "always", Origin: Origin{SourceID: "default"}}}}, nil, nil)
		_ = it
		rs.ensureDefaultSource()
	}
	isDev := tag == "dev" || strings.HasSuffix(tag, "-dev") || strings.HasPrefix(tag, "dev-")
	if isDev && policy == "always" {
		rs.diag("PULL_ALWAYS_DEV", LevelWarning, "supply-chain",
			fmt.Sprintf("sandboxOptions.template uses the mutable :%s tag with pullPolicy always (explicit or default), so every create installs whatever was pushed last; pin a digest or release tag", tag),
			"sandboxOptions.template", origins...)
	}
	if tag != "" {
		reason := "the template image is not digest-pinned; its contents (tools, users, entrypoint) are not analysed"
		if isDev {
			reason = "the :dev template is a moving tag; its contents are not analysed and change between creates"
		}
		rs.coverage(CoverageMutableImage, ref, reason, t.origin())
	}
}

func (rs *resolver) ensureDefaultSource() {
	for _, s := range rs.sources {
		if s.ID == "default" {
			return
		}
	}
	rs.addSource(Source{ID: "default", Kind: SourceDefault, Path: "sbx documented default", Expanded: true})
}

func (rs *resolver) checkCloud() {
	o := rs.opts
	if !o.Cloud {
		if o.TTL != "" {
			rs.diag("FLAG_REQUIRES_CLOUD", LevelError, "cloud", "--ttl applies only to cloud sandboxes (sbx --cloud env create)", "", Origin{SourceID: "cli", Via: []string{"--ttl"}})
		}
		if o.OnTimeout != "" {
			rs.diag("FLAG_REQUIRES_CLOUD", LevelError, "cloud", "--on-timeout applies only to cloud sandboxes (sbx --cloud env create)", "", Origin{SourceID: "cli", Via: []string{"--on-timeout"}})
		}
		return
	}
	if o.OnTimeout != "" && !contains([]string{"stop", "restart", "delete"}, o.OnTimeout) {
		rs.diag("INVALID_VALUE", LevelError, "cloud", "--on-timeout must be stop, restart, or delete", "", Origin{SourceID: "cli", Via: []string{"--on-timeout"}})
	}
	if o.OnTimeout == "restart" && o.TTL != "" {
		if d, err := time.ParseDuration(strings.ToLower(o.TTL)); err == nil && d < time.Hour {
			rs.diag("INVALID_VALUE", LevelError, "cloud", "--on-timeout restart requires --ttl of at least 1h", "", Origin{SourceID: "cli", Via: []string{"--ttl"}})
		}
	}
	unsupported := []string{"workspace", "additionalWorkspaces", "ports", "registries"}
	for _, k := range unsupported {
		if v := rs.root.get(k); v != nil && v.kind != parser.Null {
			rs.diag("CLOUD_UNSUPPORTED", LevelError, "cloud", fmt.Sprintf("%s is not supported by cloud environments and is rejected before provisioning", k), k, v.origin())
		}
	}
	if rs.opts.Clone != nil {
		rs.diag("CLOUD_UNSUPPORTED", LevelError, "cloud", "--clone is not supported by cloud environments", "", Origin{SourceID: "cli", Via: []string{"--clone"}})
	}
	if v := rs.root.get("mcp").get("servers"); v != nil && len(v.items) > 0 {
		rs.diag("CLOUD_UNSUPPORTED", LevelError, "cloud", "mcp.servers is not supported by cloud environments; load MCP servers with cloud commands", "", v.origin())
	}
	so := rs.root.get("sandboxOptions")
	for _, k := range []string{"template", "profile", "pullPolicy", "skills", "display", "gpu", "usb", "writableEnvFiles", "fuse"} {
		if v := so.get(k); v != nil {
			rs.diag("CLOUD_UNSUPPORTED", LevelError, "cloud", fmt.Sprintf("sandboxOptions.%s is not supported by cloud environments", k), "sandboxOptions."+k, v.origin())
		}
	}
	for _, svc := range rs.root.get("secrets").mapKeys() {
		s := rs.root.get("secrets").get(svc)
		if (s.has("ref") || s.has("command")) && s.str("snapshot") != "true" {
			rs.diag("CLOUD_SECRET_SNAPSHOT", LevelWarning, "cloud", fmt.Sprintf("secrets.%s: cloud environments need snapshot: true for ref or command sources", svc), "secrets."+svc, s.origin())
		}
	}
	rs.coverage(CoverageHostPolicy, "cloud account credentials and policy", "inherited cloud credentials and cloud network policy are managed by the service and not visible here", Origin{SourceID: "host"})
}

// checkWritableLocations flags environment files or local kits that the
// sandbox itself could rewrite through a writable workspace mount.
func (rs *resolver) checkWritableLocations() {
	type mount struct {
		dir string
		id  string
	}
	var writable []mount
	ws := rs.root.get("workspace")
	clone := ws.str("clone") == "true"
	if rs.opts.Clone != nil {
		clone = *rs.opts.Clone
	}
	if ws != nil && !clone && !rs.opts.Cloud {
		p := ws
		if ws.kind == parser.Map {
			p = ws.get("path")
		}
		if p != nil && p.kind == parser.Scalar && p.val != "" {
			writable = append(writable, mount{filepath.Clean(resolveHostPath(p.fileDir, p.val)), "workspace"})
		}
	}
	for _, e := range rs.root.get("additionalWorkspaces").list() {
		if e.str("readOnly") == "true" || e.str("path") == "" {
			continue
		}
		writable = append(writable, mount{filepath.Clean(resolveHostPath(e.fileDir, e.str("path"))), "additionalWorkspaces"})
	}
	if rs.root.get("sandboxOptions").str("writableEnvFiles") == "true" {
		return // already reported as WRITABLE_ENV_FILES
	}
	within := func(dir, p string) (bool, bool) {
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false, false
		}
		return true, rel == "."
	}
	for _, ls := range rs.layers {
		for _, m := range writable {
			in, root := within(m.dir, ls.dir)
			if in && !root {
				rs.diag("ENV_FILE_WRITABLE", LevelWarning, "boundary",
					fmt.Sprintf("environment file %s sits in a subdirectory of the writable %s mount; the agent can replace it by renaming its parent directory and change the host commands of the next invocation", rs.r.path(ls.in.Path), m.id),
					"", Origin{SourceID: ls.id, Line: 1})
			} else if in && root && m.id == "additionalWorkspaces" {
				rs.diag("ENV_FILE_WRITABLE", LevelWarning, "boundary",
					fmt.Sprintf("environment file %s sits inside a writable additional workspace", rs.r.path(ls.in.Path)), "", Origin{SourceID: ls.id, Line: 1})
			}
		}
	}
	for _, k := range rs.kits {
		if k.kind != kitLocal {
			continue
		}
		for _, m := range writable {
			if in, _ := within(m.dir, filepath.Clean(k.dir)); in {
				rs.diag("KIT_IN_WRITABLE_MOUNT", LevelWarning, "boundary",
					fmt.Sprintf("local kit %s lives inside the writable %s mount; the agent can change the network policy and credentials it requests on the next create", k.source, m.id),
					"kits["+rs.r.path(k.source)+"]", k.origin)
			}
		}
	}
}

func (rs *resolver) checkSSHAndOthers() {
	for _, k := range rs.kits {
		for _, c := range k.caps {
			switch c.short {
			case "ssh-agent@1":
				cfg := c.config
				if cfg == nil || cfg.str("unrestricted") != "false" {
					rs.diag("SSH_AGENT_UNRESTRICTED", LevelWarning, "boundary",
						fmt.Sprintf("kit %s requests ssh-agent@1 unrestricted: the sandbox can sign anything with every key your agent holds; set unrestricted: false with sign/authenticate", k.handle),
						"kit:"+k.handle+"/ssh-agent@1/"+strings.Join(phases(cfg.get("phase")), ","), c.origin)
				}
			case "privileged@1", "dockerd@1":
				rs.diag("PRIVILEGED", LevelWarning, "boundary", fmt.Sprintf("kit %s requests %s, the widest elevation inside the sandbox VM", k.handle, c.short), "kit:"+k.handle+"/"+c.short, c.origin)
			case "network-policy@1":
				for _, ph := range []string{"install", "runtime"} {
					for _, a := range c.config.get(ph).get("allow").scalars() {
						if (a == "*" || a == "**") && ph == "runtime" {
							rs.diag("EGRESS_UNRESTRICTED", LevelWarning, "boundary", fmt.Sprintf("kit %s allows all runtime egress (%s)", k.handle, a), "kit:"+k.handle+"/network-policy@1/runtime.allow/"+a, c.origin)
						}
					}
				}
			}
			if !strings.HasPrefix(c.typ, capNamespace) && !c.optional {
				rs.diag("UNKNOWN_CAPABILITY_REQUIRED", LevelWarning, "kits", fmt.Sprintf("kit %s requires unknown capability %s; a host that does not recognise it refuses the kit", k.handle, c.typ), "kit:"+k.handle+"/"+c.short, c.origin)
			}
		}
	}
}

// finish sorts everything, computes the summary, and attaches docs.
func (rs *resolver) finish() *Plan {
	flagged := map[string]bool{}
	for _, d := range rs.diags {
		if d.ItemID != "" && d.Level != LevelInfo {
			flagged[d.ItemID] = true
		}
	}
	for i := range rs.items {
		it := &rs.items[i]
		if flagged[it.ID] && !contains(it.Badges, BadgeWarn) {
			it.Badges = append([]string{BadgeWarn}, it.Badges...)
		}
		if doc, ok := LookupDoc(it.Path); ok {
			d := doc
			it.Doc = &d
		}
	}
	sortItems(rs.items)
	sort.SliceStable(rs.diags, func(i, j int) bool {
		a, b := rs.diags[i], rs.diags[j]
		if a.Level.Rank() != b.Level.Rank() {
			return a.Level.Rank() > b.Level.Rank()
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return originKey(a.Origins) < originKey(b.Origins)
	})
	sort.SliceStable(rs.bounds, func(i, j int) bool {
		if rs.bounds[i].ItemID != rs.bounds[j].ItemID {
			return rs.bounds[i].ItemID < rs.bounds[j].ItemID
		}
		return rs.bounds[i].Kind < rs.bounds[j].Kind
	})
	sort.SliceStable(rs.cover, func(i, j int) bool {
		if rs.cover[i].Kind != rs.cover[j].Kind {
			return rs.cover[i].Kind < rs.cover[j].Kind
		}
		return rs.cover[i].Ref < rs.cover[j].Ref
	})
	p := &Plan{
		Meta: Meta{
			SchemaVersion: SchemaVersion, TargetSbxVersion: TargetSbxVersion, Redaction: rs.r.mode,
			Notice:  "static analysis of declared configuration; not the state of a running sandbox",
			Sources: rs.sources,
		},
		Invocation: Invocation{
			Name: rs.r.text(rs.opts.Name), ProjectDir: rs.r.path(rs.opts.ProjectDir), Clone: rs.opts.Clone,
			SkipHostCommands: rs.opts.SkipHostCommands, Cloud: rs.opts.Cloud, TTL: rs.opts.TTL, OnTimeout: rs.opts.OnTimeout,
			EnvArgs: nonNil(rs.envArgs), KitArgs: nonNil(rs.kitArgs),
		},
		Items:       nonNil(rs.items),
		Boundaries:  nonNil(rs.bounds),
		Coverage:    nonNil(rs.cover),
		Diagnostics: nonNil(rs.diags),
	}
	p.Summary = Summarize(p)
	return p
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func originKey(os []Origin) string {
	if len(os) == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%06d:%04d", os[0].SourceID, os[0].Line, os[0].Col)
}

// Summarize computes the header counts for a plan.
func Summarize(p *Plan) Summary {
	var s Summary
	for _, b := range p.Boundaries {
		if b.Kind == BoundaryHostExec && !b.Skipped {
			s.HostExec++
		}
		if b.Lifetime == "host-persistent" {
			s.HostPersistent++
		}
		if b.Kind == BoundaryHostMount && b.Access == "rw" && b.Where == "host" {
			s.RWMounts++
		}
		if b.Kind == BoundaryNetwork {
			s.EgressAllows++
		}
	}
	for _, d := range p.Diagnostics {
		switch d.Level {
		case LevelError:
			s.Errors++
		case LevelWarning:
			s.Warnings++
		}
	}
	s.Unexpanded = len(p.Coverage)
	return s
}

// SummaryLine renders the one-line header used by every renderer.
func SummaryLine(s Summary) string {
	return fmt.Sprintf("Host実行 %d | Host永続 %d | RWマウント %d | 外への許可 %d | エラー %d | 警告 %d | 未展開 %d",
		s.HostExec, s.HostPersistent, s.RWMounts, s.EgressAllows, s.Errors, s.Warnings, s.Unexpanded)
}
