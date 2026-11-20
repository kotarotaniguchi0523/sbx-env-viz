// Package sbxenv statically resolves layered Docker Sandboxes environment
// files (sbxenv.yaml) and local kits into a provenance-carrying plan.
//
// The package is pure: it never touches the file system, the OS environment,
// a running sbx daemon, or a terminal. Callers pass file contents in
// LayerInput values and supply local kits through a KitResolver.
//
// The resulting Plan is built from an allow-list: only fields this package
// chooses to copy appear in it, and every value passes through the redactor
// selected in ResolveOptions.Redaction. Literal secret values are never
// copied, in any mode.
package sbxenv

// SchemaVersion is the version of the Plan IR.
const SchemaVersion = "1"

// TargetSbxVersion is the sbx release whose documented behavior this package
// models. It is bumped when sbx-watch reports a new stable release and the
// rules have been checked against it.
const TargetSbxVersion = "v0.54.0"

// LayerKind tells where an environment file came from.
type LayerKind string

const (
	// LayerEnv is an environment file passed on the command line (or found
	// in the project directory).
	LayerEnv LayerKind = "env"
	// LayerUserDefaults is ~/.sbxenv.yaml.
	LayerUserDefaults LayerKind = "user-defaults"
)

// LayerInput is one environment file in merge order.
type LayerInput struct {
	// Path is the absolute host path of the file. It is used for
	// ${{ env.fileDir }}, for resolving relative paths, and (after
	// redaction) for display.
	Path    string
	Content []byte
	Kind    LayerKind
}

// ArgFile is the content of an --env-args-file or --kit-args-file.
type ArgFile struct {
	Path    string
	Content []byte
}

// KitInput is a local kit descriptor read by the caller.
type KitInput struct {
	// DescriptorPath is the absolute path of the descriptor YAML.
	DescriptorPath string
	Content        []byte
}

// KitResolver loads local kit directories. Implementations live outside
// this package so that it stays free of file-system access.
type KitResolver interface {
	// LoadKit returns the descriptor for the kit directory dir (absolute).
	LoadKit(dir string) (KitInput, error)
}

// Redaction selects how much the Plan hides.
type Redaction string

const (
	// RedactHome shortens $HOME to ~ and nothing else. Literal secret values
	// are still never copied. Intended for a local TTY.
	RedactHome Redaction = "home"
	// RedactStrict additionally hides environment values, argument values,
	// host command text, and vault reference paths. It is the default for
	// machine-readable output.
	RedactStrict Redaction = "strict"
)

// ResolveOptions mirrors the `sbx env create` flags plus analysis settings.
type ResolveOptions struct {
	// ProjectDir is ${{ env.projectDir }}: the first path's directory, or the
	// working directory when no path was given.
	ProjectDir string
	// HomeDir is used only to shorten paths to ~ in output.
	HomeDir string
	// HomeAliases are other spellings of HomeDir that paths may use, such as
	// its symlink-resolved form (/private/var/... on macOS).
	HomeAliases []string

	EnvArgsFiles []ArgFile
	// EnvArgs are --env-arg NAME=VALUE flags in command-line order.
	EnvArgs      []string
	KitArgsFiles []ArgFile
	// KitArgs are --kit-arg flags: NAME=VALUE (every kit declaring NAME) or
	// HANDLE.NAME=VALUE (one kit).
	KitArgs []string

	// Name is --name; empty means not given.
	Name string
	// Clone is --clone / --clone=false; nil means not given.
	Clone            *bool
	SkipHostCommands bool
	Cloud            bool
	TTL              string
	OnTimeout        string

	// Kits loads local ./kits directories. Nil leaves local kits unexpanded
	// (they are listed under Coverage).
	Kits KitResolver

	// Redaction defaults to RedactStrict when empty.
	Redaction Redaction
}

// Severity orders risks and diagnostics. Higher is worse.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Rank returns a sortable rank (critical = 4 … info = 0).
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	}
	return 0
}

// DiagLevel is a diagnostic level.
type DiagLevel string

const (
	LevelError   DiagLevel = "error"
	LevelWarning DiagLevel = "warning"
	LevelInfo    DiagLevel = "info"
)

// Rank returns error = 2, warning = 1, info = 0.
func (l DiagLevel) Rank() int {
	switch l {
	case LevelError:
		return 2
	case LevelWarning:
		return 1
	}
	return 0
}

// Plan is the resolved, redacted intermediate representation.
type Plan struct {
	Meta        Meta         `json:"meta"`
	Invocation  Invocation   `json:"invocation"`
	Items       []Item       `json:"items"`
	Boundaries  []Boundary   `json:"boundaries"`
	Coverage    []Coverage   `json:"coverage"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Summary     Summary      `json:"summary"`
}

// Meta describes the plan and its inputs.
type Meta struct {
	SchemaVersion    string    `json:"schemaVersion"`
	TargetSbxVersion string    `json:"targetSbxVersion"`
	Redaction        Redaction `json:"redaction"`
	// Notice states that this is a static analysis of declarations.
	Notice  string   `json:"notice"`
	Sources []Source `json:"sources"`
}

// SourceKind classifies a Source.
type SourceKind string

const (
	SourceEnv          SourceKind = "env"
	SourceUserDefaults SourceKind = "user-defaults"
	SourceKit          SourceKind = "kit"
	SourceEnvArgsFile  SourceKind = "env-args-file"
	SourceKitArgsFile  SourceKind = "kit-args-file"
	SourceCLI          SourceKind = "cli"
	SourceDefault      SourceKind = "default"
)

// Source is one input that contributed to the plan.
type Source struct {
	ID   string     `json:"id"`
	Kind SourceKind `json:"kind"`
	// Path is the (redacted) logical path.
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
	// Layer is the merge order for environment files (1-based), 0 otherwise.
	Layer int `json:"layer,omitempty"`
	// Expanded reports whether the source was parsed and analysed.
	Expanded bool   `json:"expanded"`
	Status   string `json:"status,omitempty"`
}

// Invocation records the command-line inputs that shaped the plan.
type Invocation struct {
	Name             string     `json:"name,omitempty"`
	ProjectDir       string     `json:"projectDir,omitempty"`
	Clone            *bool      `json:"clone,omitempty"`
	SkipHostCommands bool       `json:"skipHostCommands"`
	Cloud            bool       `json:"cloud"`
	TTL              string     `json:"ttl,omitempty"`
	OnTimeout        string     `json:"onTimeout,omitempty"`
	EnvArgs          []ArgValue `json:"envArgs"`
	KitArgs          []ArgValue `json:"kitArgs"`
}

// ArgValue is a resolved environment or kit argument.
type ArgValue struct {
	// Kit is the kit handle for kit arguments.
	Kit   string `json:"kit,omitempty"`
	Name  string `json:"name"`
	Value string `json:"value"`
	// From is "default", "env-args-file", "flag", "kit-entry", or "unset".
	From   string `json:"from"`
	Origin Origin `json:"origin"`
}

// Origin points at the place that set a value.
type Origin struct {
	SourceID string `json:"source"`
	Line     int    `json:"line,omitempty"`
	Col      int    `json:"col,omitempty"`
	// Via lists the chain that led here, such as the env file that
	// referenced a kit.
	Via []string `json:"via,omitempty"`
}

// HistoryOp is what one layer did to a value.
type HistoryOp string

const (
	OpSet     HistoryOp = "set"
	OpReplace HistoryOp = "replace"
	OpAppend  HistoryOp = "append"
	OpMerge   HistoryOp = "merge"
	OpShadow  HistoryOp = "shadow"
	OpDefault HistoryOp = "default"
)

// HistoryEntry is one step in a value's override history.
type HistoryEntry struct {
	Op     HistoryOp `json:"op"`
	Value  string    `json:"value"`
	Origin Origin    `json:"origin"`
}

// Section groups items by where they live in the files.
type Section string

const (
	SectionTop          Section = "top"
	SectionArgs         Section = "args"
	SectionWorkspace    Section = "workspace"
	SectionMounts       Section = "mounts"
	SectionEnv          Section = "env"
	SectionSandbox      Section = "sandboxOptions"
	SectionSecrets      Section = "secrets"
	SectionBindings     Section = "bindings"
	SectionRegistries   Section = "registries"
	SectionMCP          Section = "mcp"
	SectionPorts        Section = "ports"
	SectionLifecycle    Section = "lifecycle"
	SectionKits         Section = "kits"
	SectionCapabilities Section = "capabilities"
)

// Item is one effective setting.
type Item struct {
	ID      string  `json:"id"`
	Path    string  `json:"path"`
	Section Section `json:"section"`
	// Kit is the kit handle for capability items.
	Kit   string `json:"kit,omitempty"`
	Value string `json:"value"`
	// Fields holds a structured view for compound items (key → display value).
	Fields  []Field        `json:"fields,omitempty"`
	Origins []Origin       `json:"origins"`
	History []HistoryEntry `json:"history"`
	Risk    Severity       `json:"risk"`
	// RiskReason says in plain Japanese why the item got its risk level.
	RiskReason string    `json:"riskReason,omitempty"`
	Badges     []string  `json:"badges,omitempty"`
	Doc        *FieldDoc `json:"doc,omitempty"`
}

// Field is a key/value pair inside a compound item.
type Field struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// BoundaryKind classifies what crosses the sandbox boundary.
type BoundaryKind string

const (
	BoundaryHostExec    BoundaryKind = "host-exec"
	BoundaryHostMount   BoundaryKind = "host-mount"
	BoundaryHostPort    BoundaryKind = "host-port"
	BoundaryNetwork     BoundaryKind = "network-egress"
	BoundaryProxyRoute  BoundaryKind = "proxy-route"
	BoundaryHostGlobal  BoundaryKind = "host-global"
	BoundarySSHAgent    BoundaryKind = "ssh-agent"
	BoundaryIdentity    BoundaryKind = "identity"
	BoundarySandboxExec BoundaryKind = "sandbox-exec"
	BoundaryDevice      BoundaryKind = "device"
)

// Boundary explains why an item crosses (or stays inside) the sandbox.
type Boundary struct {
	ItemID string       `json:"itemId"`
	Kind   BoundaryKind `json:"kind"`
	// Where is host, sandbox, or unknown.
	Where string `json:"where"`
	// Lifetime is host-persistent, sandbox-scoped, or invocation.
	Lifetime string `json:"lifetime"`
	// Access is rw or ro for mounts.
	Access  string `json:"access,omitempty"`
	Opaque  bool   `json:"opaque"`
	Skipped bool   `json:"skipped,omitempty"`
	Reason  string `json:"reason"`
}

// CoverageKind classifies what the analysis could not see.
type CoverageKind string

const (
	CoverageRemoteKit      CoverageKind = "remote-kit"
	CoverageBuiltinAgent   CoverageKind = "builtin-agent"
	CoverageMutableImage   CoverageKind = "mutable-template"
	CoverageHostPolicy     CoverageKind = "host-local-policy"
	CoverageUnknownCap     CoverageKind = "unknown-capability"
	CoverageUnmodeledCap   CoverageKind = "unmodeled-capability"
	CoverageKitEnvRef      CoverageKind = "kit-env-reference"
	CoverageKitLoadFailed  CoverageKind = "kit-unreadable"
	CoverageLegacyKit      CoverageKind = "legacy-kit"
	CoverageGlobalSecrets  CoverageKind = "global-secret-store"
	CoverageNoKitResolver  CoverageKind = "local-kit-unexpanded"
	CoverageWorkloadKitArg CoverageKind = "remote-kit-args"
)

// Coverage is something outside the analysis, always shown.
type Coverage struct {
	Kind   CoverageKind `json:"kind"`
	Ref    string       `json:"ref"`
	Reason string       `json:"reason"`
	Origin Origin       `json:"origin"`
}

// Diagnostic is a finding about the configuration.
type Diagnostic struct {
	Code     string    `json:"code"`
	Level    DiagLevel `json:"level"`
	Message  string    `json:"message"`
	ItemID   string    `json:"itemId,omitempty"`
	Origins  []Origin  `json:"origins,omitempty"`
	Category string    `json:"category,omitempty"`
}

// Summary is the one-line header.
type Summary struct {
	HostExec       int `json:"hostExec"`
	HostPersistent int `json:"hostPersistent"`
	RWMounts       int `json:"rwMounts"`
	EgressAllows   int `json:"egressAllows"`
	Errors         int `json:"errors"`
	Warnings       int `json:"warnings"`
	Unexpanded     int `json:"unexpanded"`
}
