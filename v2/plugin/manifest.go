package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// Manifest v2 is the CDM for plugin.json.
//
// It differs from v1 in one idea: authority is addressed in the bus grammar
// rather than in exact names. v1's Permissions listed exact topic names, which
// meant a plugin with ten commands listed ten names and a host could not tell a
// family from a coincidence of prefixes. Here every request is a bus.Pattern,
// parsed by the same parser that matches at delivery time, so a pattern that
// validates here means at enable time exactly what it meant to its author.
//
// Two v1 fields are gone rather than deprecated. Provides and RequiresProvides
// were a second, untyped composition mechanism beside Requires and Extends /
// Implements; keeping both meant two answers to "who resolves this", and only
// one of them was host-mediated.
type Manifest struct {
	// Contract is the manifest contract this document is written against
	// (ProtocolMajor). It is the field a host reads BEFORE deciding how to
	// read the rest, which is why it is a number and not inferred: a v1
	// document read with v2 meanings is the failure the whole band exists to
	// prevent. The per-gate floor over it is contract.Options.MinAccepted,
	// which refuses a lower number with BDP1007; this package itself judges
	// one document and has no host to refuse it on behalf of.
	Contract int `json:"contract,omitempty" bd:"public"`

	// Kind is what this package IS, from the closed set below. Until it
	// existed the verifier inferred a kind from the tree, which meant a
	// package's rules depended on which files happened to be in it. The
	// explicit field is preferred everywhere; inference survives only as the
	// fallback for a manifest written before this field.
	Kind string `json:"kind,omitempty" bd:"public"`

	ID      string `json:"id" bd:"public"`
	Version string `json:"version,omitempty" bd:"public"`

	// Identity is who this plugin is to a human: the name and description a
	// store row, a settings list and an operator prompt all render. It is
	// REQUIRED for every kind and is never host-defaulted — a default would
	// make the gate that checks it pass without anyone writing anything.
	Identity *ManifestIdentity `json:"identity,omitempty" bd:"public"`

	Nav       []NavItem      `json:"nav,omitempty" bd:"public"`
	Panels    []PanelSpec    `json:"panels,omitempty" bd:"public"`
	Launchers []LauncherSpec `json:"launchers,omitempty" bd:"public"`
	Scopes    []string       `json:"scopes,omitempty" bd:"public"`
	Routes    []string       `json:"routes,omitempty" bd:"public"`

	// PublicRoutes are the routes above that the host's OPERATOR SESSION GATE
	// does not apply to. Anything not named here requires a session, so the
	// safe answer is the default and a plugin has to ask deliberately.
	//
	// It does NOT mean the route is unprotected, and reading it that way is the
	// mistake this comment exists to prevent. A route named here may still
	// enforce its own credential: a guest share link is authorised by the token
	// in its URL, and a deploy probe by coming from loopback. What the
	// declaration says is "do not ask this route for a session cookie", because
	// for these routes a session cannot exist yet or never will.
	//
	// Every entry must also appear in Routes. Declaring a public route this
	// manifest does not own would let a plugin open a hole in another's surface.
	PublicRoutes []string `json:"publicRoutes,omitempty" bd:"public"`
	// Spawn is DEPRECATED by Kind: "spawn": true is "kind": "process" said
	// twice. It is still read through the migration window (BDP5003 warns),
	// and a manifest whose two answers disagree is refused rather than having
	// one of them picked for it.
	Spawn  bool   `json:"spawn,omitempty" bd:"public"`
	Binary string `json:"binary,omitempty" bd:"public"`
	Socket string `json:"socket,omitempty" bd:"public"`

	// Helpers, Scripts and Services name the other executables and templates
	// this package ships beside Binary: helpers under bin/, shell shims under
	// scripts/, unit templates under services/. Scripts and services are
	// consent-gated at install; declaring one is how a package ships them
	// honestly instead of a host owning them on its behalf.
	Helpers  []string `json:"helpers,omitempty" bd:"public"`
	Scripts  []string `json:"scripts,omitempty" bd:"public"`
	Services []string `json:"services,omitempty" bd:"public"`

	// Static are directories in the extracted package the host serves at
	// /p/<id>/<mount>/. Aliases are pretty paths the host's reverse proxy maps
	// onto them at start. Both are declarations the HOST acts on: a plugin
	// never mounts its own files into the shell's URL space.
	Static  []StaticMount `json:"static,omitempty" bd:"public"`
	Aliases []string      `json:"aliases,omitempty" bd:"public"`

	// Trust is reserved for the package attestation (publisher signature,
	// Store countersignature, key id). It is OPAQUE here on purpose: the
	// verifier never reads it, so a manifest cannot talk its way into a trust
	// tier, and the shape stays free until the attestation format is released.
	Trust json.RawMessage `json:"trust,omitempty" bd:"public"`

	MinCoreVersion string        `json:"minCoreVersion,omitempty" bd:"public"`
	Targets        []string      `json:"targets,omitempty" bd:"public"` // gateway, vault
	Role           string        `json:"role,omitempty" bd:"public"`    // system | extension
	Requires       []Requirement `json:"requires,omitempty" bd:"public"`
	Pricing        *Pricing      `json:"pricing,omitempty" bd:"public"`
	Publisher      *Publisher    `json:"publisher,omitempty" bd:"public"`

	// Family declares platform members the host selects between (ADR 0020).
	Family *Family `json:"family,omitempty" bd:"public"`
	// When constrains this plugin to a set of operating systems.
	When When `json:"when,omitzero" bd:"public"`

	// Extends names extension points this plugin owns; Implements registers it
	// into points other plugins own (ADR 0023). The host mediates: a plugin
	// never loads, execs or proxies another.
	Extends    []ExtensionPoint `json:"extends,omitempty" bd:"public"`
	Implements []Provider       `json:"implements,omitempty" bd:"public"`

	// Critical marks a role:system plugin the host must not serve without. A
	// critical plugin that fails to start halts the host rather than running
	// with the capability missing. Meaningless for extensions.
	Critical bool `json:"critical,omitempty" bd:"public"`

	// Protocol and Permissions request host capabilities; they do not grant
	// authority. UI declares shell slots owned by this plugin generation.
	Protocol    *ProtocolRequirements `json:"protocol,omitempty" bd:"public"`
	Permissions Permissions           `json:"permissions,omitzero" bd:"public"`
	UI          []UIContribution      `json:"ui,omitempty" bd:"public"`

	// Serves are the services this plugin EXPORTS. Permissions say what a
	// plugin may reach; this says what it answers, which until now only the
	// running process knew. A host that can read the export surface from the
	// manifest can show an operator what a plugin offers before enabling it,
	// and can refuse a second plugin claiming the same service name.
	Serves []ServiceDecl `json:"serves,omitempty" bd:"public"`

	// Streams, KV and Objects are the assets the host PROVISIONS inside this
	// plugin's own principal at enable time, with the limits declared here.
	// A plugin never creates its own storage: an asset created at runtime has
	// no declared ceiling and no operator who ever saw it.
	Streams []StreamDecl `json:"streams,omitempty" bd:"public"`
	KV      []KVDecl     `json:"kv,omitempty" bd:"public"`
	Objects []ObjectDecl `json:"objects,omitempty" bd:"public"`

	// Needs are the substrate capabilities this plugin cannot run without, from
	// the closed vocabulary bus.CapabilityNames(). At enable time the host
	// compares them against Bus.Capabilities() and fails CLOSED, the same way
	// OSAllowed and When.Matches do — a plugin that silently degrades because
	// the substrate lacks durable storage is a data-loss report later.
	Needs []string `json:"needs,omitempty" bd:"public"`

	// Config declares the settings sections this plugin contributes. It is
	// SCHEMA, never values: the host owns values and reads its own record, so a
	// plugin describes what it configures and the operator's choices never live
	// in a file the plugin writes (ADR 0026 C0). A section is served at the
	// host.settings.section extension point, which the plugin must also name in
	// Implements; this field is what lets the host list and label a section
	// before the plugin is asked for anything.
	Config *Config `json:"config,omitempty" bd:"public"`
}

// Manifest.Kind values. The set is CLOSED: a host that meets a kind it does
// not know refuses the package rather than guessing which rules apply to it,
// the same way an unknown config field kind refuses a section.
//
//   - builtin — compiled into the host binary; ships a manifest, docs and images.
//   - process — the host spawns Binary; may ship every deliverable class.
//   - ui      — static front end only, ui/index.html required, no executable.
//   - family  — a platform selector with Family members and no payload of its own.
const (
	KindBuiltin = "builtin"
	KindProcess = "process"
	KindUI      = "ui"
	KindFamily  = "family"
)

// Kinds returns the closed set, in the order a human reads it.
func Kinds() []string { return []string{KindBuiltin, KindProcess, KindUI, KindFamily} }

// ReservedAliasPrefixes are the first path segments a manifest may not claim in
// Aliases, because the host already serves them. An alias is a pretty path the
// host's reverse proxy maps onto a plugin's static mount, so an alias that
// shadowed /api would take the host's own surface away from it — and the host
// would have no way to tell that from a plugin that simply started first.
//
// Collisions between two plugins' aliases are the HOST's refusal (BDP6010),
// not this one: only the host can see both manifests at once.
func ReservedAliasPrefixes() []string {
	return []string{"/api", "/p", "/v1", "/healthz", "/readyz", "/assets", "/static", "/.well-known"}
}

// ManifestIdentity is the human half of a manifest: what a store row, a settings list
// and a consent prompt show. Everything here describes the plugin to a person;
// nothing here grants it anything.
//
// It is required for every kind, and the host never fills it in. A defaulted
// display name would read as an authored one, and the operator approving a
// package would be approving text the host wrote.
type ManifestIdentity struct {
	// DisplayName is the human name. It must not merely repeat ID: an id is
	// an address and a display name is a name, and a store listing that shows
	// "tmux-manager" tells a reader nothing the URL did not.
	DisplayName string `json:"displayName" bd:"public"`
	// Description is one or two sentences: what this plugin does.
	Description string   `json:"description" bd:"public"`
	Categories  []string `json:"categories,omitempty" bd:"public"`
	Keywords    []string `json:"keywords,omitempty" bd:"public"`
	// Images are package-relative paths to files the package ships under
	// images/. They are NOT the icon tokens NavItem and UIContribution carry:
	// those are names the shell resolves from its own icon set.
	Images *Images `json:"images,omitempty" bd:"public"`
	// Homepage replaces the deprecated top-level "homepage" property.
	Homepage string `json:"homepage,omitempty" bd:"public"`
	License  string `json:"license,omitempty" bd:"public"`
	// Readme and Changelog are package-relative paths to shipped documents.
	Readme    string   `json:"readme,omitempty" bd:"public"`
	Changelog string   `json:"changelog,omitempty" bd:"public"`
	Support   *Support `json:"support,omitempty" bd:"public"`
	// ThirdPartyNotices is a package-relative path under notices/.
	ThirdPartyNotices string `json:"thirdPartyNotices,omitempty" bd:"public"`
}

// Images are the package-relative image paths a store listing renders.
type Images struct {
	Icon        string   `json:"icon,omitempty" bd:"public"`
	Logo        string   `json:"logo,omitempty" bd:"public"`
	Banner      string   `json:"banner,omitempty" bd:"public"`
	Screenshots []string `json:"screenshots,omitempty" bd:"public"`
}

// Support is where a human takes a problem with this plugin. It is the
// publisher's channel for THIS package; Publisher.SupportEmail is the
// publisher's own, which outlives any one release.
type Support struct {
	Email string `json:"email,omitempty" bd:"public"`
	URL   string `json:"url,omitempty" bd:"public"`
}

// StaticMount asks the host to serve one directory of the extracted package at
// /p/<id>/<Mount>/.
//
// Both halves are a SINGLE safe path segment. Dir is a directory in the
// package, so "../../etc" or an absolute path is an attempt to serve somewhere
// the package does not own; Mount is a URL segment, so the same spelling would
// escape the plugin's own prefix. Neither is normalised into something legal —
// a mount that had to be corrected is not the one the author reviewed.
type StaticMount struct {
	Dir   string `json:"dir" bd:"public"`
	Mount string `json:"mount" bd:"public"`
}

// ProtocolMajor is the protocol this manifest schema belongs to. A manifest
// declaring any other non-zero major is refused rather than downgraded: the
// fields below mean different things in v1, and a host that accepted the
// version but read the v2 meaning would be the worst of both.
const ProtocolMajor = 2

// FeatureDocumentPaths is the negotiated feature a manifest must require before
// a panel may claim shell document paths.
const FeatureDocumentPaths = "ui.document-paths.v1"

// MeshPrefix is the first token reserved to inter-gateway mesh routing. Like the
// substrate's own "$"-prefixed and _INBOX tokens it parses, because the host has
// to be able to name it; a manifest may not.
const MeshPrefix = "bd"

// OwnNamespace is the grant every plugin has by virtue of being that plugin. It
// is IMPLICIT and is never written in a manifest — a manifest that lists any of
// it is refused, because two sources for the same authority is one source that
// can disagree with itself.
//
// The host's grant compiler and the manifest validator both call this, so
// "what a plugin owns" is computed one way or not at all.
func OwnNamespace(id string) bus.Grants {
	id = strings.TrimSpace(id)
	if id == "" {
		return bus.Grants{}
	}
	return bus.Grants{
		// Its own events, and its own timer ticks in both directions.
		Publish:   []bus.Pattern{bus.Pattern("event." + id + ".>"), bus.Pattern("tick." + id + ".>")},
		Subscribe: []bus.Pattern{bus.Pattern("tick." + id + ".>"), bus.Pattern("_INBOX." + id + ".>")},
		// Its own command and service trees, which only it may mount on.
		Serves: []bus.Pattern{bus.Pattern("cmd." + id + ".>"), bus.Pattern("svc." + id + ".>")},
	}
}

// ownNamespacePatterns flattens OwnNamespace into one list, because the refusal
// does not care which verb a plugin used to restate what it already has.
func ownNamespacePatterns(id string) []bus.Pattern {
	g := OwnNamespace(id)
	out := make([]bus.Pattern, 0, len(g.Publish)+len(g.Subscribe)+len(g.Serves))
	seen := map[bus.Pattern]bool{}
	for _, list := range [][]bus.Pattern{g.Publish, g.Subscribe, g.Serves} {
		for _, p := range list {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// Permissions requests authority over subjects OUTSIDE this plugin's own
// namespace. Only host policy can grant it, and the grant is the intersection
// of this request with the ceiling and the operator's consent.
//
// Entries are bus patterns, so a family is one line rather than a list that
// goes stale. Reserved tokens are refused here even though the parser accepts
// them: the substrate must be able to name "$SYS.>" to deny it, a manifest
// must not be able to name it at all.
type Permissions struct {
	Publish   []bus.Pattern `json:"publish,omitempty" bd:"public"`
	Subscribe []bus.Pattern `json:"subscribe,omitempty" bd:"public"`
	Request   []bus.Pattern `json:"request,omitempty" bd:"public"`
}

// ServiceDecl is one service this plugin exports. QueueGroup names the load
// balancing group; members of the same group share the work, so two generations
// of the same plugin do not each answer the same call.
type ServiceDecl struct {
	Name       string         `json:"name" bd:"public"`
	Version    string         `json:"version,omitempty" bd:"public"`
	QueueGroup string         `json:"queueGroup,omitempty" bd:"public"`
	Endpoints  []EndpointDecl `json:"endpoints,omitempty" bd:"public"`
}

// EndpointDecl is one callable operation of a service. Subject must sit under
// this plugin's own "svc.<id>." or "cmd.<id>." tree: an endpoint mounted
// anywhere else is a plugin answering for somebody it is not.
//
// Point, when set, names the extension point this endpoint implements, so the
// host can route a point's traffic to it without a second declaration.
type EndpointDecl struct {
	Name    string      `json:"name" bd:"public"`
	Subject bus.Subject `json:"subject" bd:"public"`
	Point   string      `json:"point,omitempty" bd:"public"`
}

// StreamDecl asks the host to provision one durable stream inside this plugin's
// principal, with the limits stated here.
//
// MaxBytes and MaxAgeSeconds are REQUIRED and must be positive. An unbounded
// stream is how a gateway fills a disk quietly: nothing fails, nothing is
// logged, and the first symptom is the whole host out of space. A declaration
// that cannot say what it costs is refused.
type StreamDecl struct {
	Name          string        `json:"name" bd:"public"`
	Subjects      []bus.Pattern `json:"subjects,omitempty" bd:"public"`
	MaxBytes      int64         `json:"maxBytes" bd:"public"`
	MaxAgeSeconds int64         `json:"maxAgeSeconds" bd:"public"`
	MaxMsgs       int64         `json:"maxMsgs,omitempty" bd:"public"`
}

// MaxAge is MaxAgeSeconds as a duration. The field is seconds because a
// manifest is JSON a human edits, and a Go duration marshals as nanoseconds.
func (s StreamDecl) MaxAge() time.Duration { return time.Duration(s.MaxAgeSeconds) * time.Second }

// KVDecl asks for one key/value bucket. History is the number of revisions kept
// per key; zero lets the host choose its default.
type KVDecl struct {
	Name       string `json:"name" bd:"public"`
	MaxBytes   int64  `json:"maxBytes,omitempty" bd:"public"`
	TTLSeconds int64  `json:"ttlSeconds,omitempty" bd:"public"`
	History    int    `json:"history,omitempty" bd:"public"`
}

// TTL is TTLSeconds as a duration.
func (k KVDecl) TTL() time.Duration { return time.Duration(k.TTLSeconds) * time.Second }

// ObjectDecl asks for one object (blob) bucket.
type ObjectDecl struct {
	Name       string `json:"name" bd:"public"`
	MaxBytes   int64  `json:"maxBytes,omitempty" bd:"public"`
	TTLSeconds int64  `json:"ttlSeconds,omitempty" bd:"public"`
}

// TTL is TTLSeconds as a duration.
func (o ObjectDecl) TTL() time.Duration { return time.Duration(o.TTLSeconds) * time.Second }

// ProtocolRequirements declares required features of the host protocol.
// Major zero is the legacy protocol, with no new required features.
type ProtocolRequirements struct {
	Major    uint32   `json:"major" bd:"public"`
	Required []string `json:"required,omitempty" bd:"public"`
	// Hooks lists the lifecycle hooks the plugin implements. It is
	// advertisement, not a requirement.
	Hooks []string `json:"hooks,omitempty" bd:"public"`
}

// HostPointNamespace prefixes every extension point the host owns. Only the
// host and plugins compiled into it may declare a point under it. A manifest
// cannot prove where it runs, so the host enforces that half at admission;
// validation only checks that a name is well formed and namespaced.
const HostPointNamespace = "host."

// ValidateExtendsName checks one name a plugin declares in Extends. A point
// name is lowercase letters, digits and hyphens between dots. A host name has an
// area and a name after HostPointNamespace ("host.settings.section"); any other
// name starts with the publisher id and a dot ("acme.widgets" or
// "acme.widgets.panel" for publisher "acme"). A nil or empty publisher has no
// namespace. The version is not part of the name; it belongs in Interface.
func ValidateExtendsName(publisher *Publisher, name string) error {
	segments := strings.Split(name, ".")
	if len(segments) < 2 {
		return fmt.Errorf("extends: extension point %q needs at least two dot-separated segments", name)
	}
	for _, segment := range segments {
		if segment == "" || strings.Trim(segment, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			return fmt.Errorf("extends: extension point %q must be lowercase letters, digits and hyphens between dots", name)
		}
	}
	if strings.HasPrefix(name, HostPointNamespace) {
		if len(segments) < 3 {
			return fmt.Errorf("extends: host extension point %q needs an area and a name after %q", name, HostPointNamespace)
		}
		return nil
	}
	if publisher != nil {
		if namespace := strings.ToLower(strings.TrimSpace(publisher.ID)); namespace != "" && strings.HasPrefix(name, namespace+".") {
			return nil
		}
	}
	return fmt.Errorf("extends: extension point %q must start with the publisher id or %q", name, HostPointNamespace)
}

// Config is a plugin's declared configuration surface.
type Config struct {
	Sections []ConfigSection `json:"sections,omitempty" bd:"public"`
}

// ConfigSection names one settings section and the fields it shows. ID must
// equal the contribution ID the plugin registers at the settings section point.
type ConfigSection struct {
	ID          string        `json:"id" bd:"public"`
	Title       string        `json:"title,omitempty" bd:"public"`
	Description string        `json:"description,omitempty" bd:"public"`
	Fields      []ConfigField `json:"fields,omitempty" bd:"public"`
}

// Config field kinds. The set is closed: a renderer that meets a kind it does
// not know refuses the section rather than guessing a control.
const (
	ConfigKindBool       = "bool"
	ConfigKindInt        = "int"
	ConfigKindString     = "string"
	ConfigKindStringList = "stringList"
	ConfigKindEnum       = "enum"
	ConfigKindSecret     = "secret"
	ConfigKindProvider   = "provider"
)

// ConfigField describes one value a settings section shows. Like the section it
// belongs to it is schema and never a value: the host renders a control from it
// and reads the value from the section snapshot.
//
// Key is a dot-separated path into the section's snapshot and patch body, so a
// value nested under an object ("prefs.muteToasts") is addressable without a
// second structure. Default is the value as text, read by Kind; stringList and
// secret fields take none. A secret field's value is never rendered.
type ConfigField struct {
	Key         string   `json:"key" bd:"public"`
	Kind        string   `json:"kind" bd:"public"`
	Label       string   `json:"label,omitempty" bd:"public"`
	Description string   `json:"description,omitempty" bd:"public"`
	Default     string   `json:"default,omitempty" bd:"public"`
	Min         *int     `json:"min,omitempty" bd:"public"`
	Max         *int     `json:"max,omitempty" bd:"public"`
	Nullable    bool     `json:"nullable,omitempty" bd:"public"`
	Choices     []string `json:"choices,omitempty" bd:"public"`
	// Provider options are resolved by the host from the live point registry,
	// filtered by Requires. They are never static Choices.
	Point           string   `json:"point,omitempty" bd:"public"`
	Requires        []string `json:"requires,omitempty" bd:"public"`
	ReadOnly        bool     `json:"readOnly,omitempty" bd:"public"`
	RequiresRestart bool     `json:"requiresRestart,omitempty" bd:"public"`
}

// When constrains a plugin or family member to a set of operating systems,
// matched against runtime.GOOS.
type When struct {
	OS []string `json:"os,omitempty" bd:"public"`
}

// Matches reports whether goos satisfies the constraint. An empty constraint
// matches every OS.
func (w When) Matches(goos string) bool {
	if len(w.OS) == 0 {
		return true
	}
	goos = strings.TrimSpace(strings.ToLower(goos))
	for _, v := range w.OS {
		if strings.EqualFold(strings.TrimSpace(v), goos) {
			return true
		}
	}
	return false
}

// OSAllowed reports whether this plugin may run on goos. It is the shape the
// Needs check follows at enable time: an unmet declaration refuses the plugin,
// it never starts it anyway.
func (m Manifest) OSAllowed(goos string) bool { return m.When.Matches(goos) }

// MissingNeeds returns the declared capabilities this substrate does not
// provide, in the order declared. Enable-time capability checking is this call
// plus a refusal; there is deliberately no "degrade" branch.
func (m Manifest) MissingNeeds(have bus.Capabilities) []string { return have.Missing(m.Needs) }

// FamilyMember is one platform member of a family.
type FamilyMember struct {
	ID   string `json:"id" bd:"public"`
	When When   `json:"when,omitzero" bd:"public"`
}

// Family declares the platform members a host selects between (ADR 0020).
// Selector is "runtime.os".
type Family struct {
	Selector string         `json:"selector,omitempty" bd:"public"`
	Members  []FamilyMember `json:"members,omitempty" bd:"public"`
}

// ExtensionPoint is a named seam a plugin owns and others register into.
type ExtensionPoint struct {
	Name        string `json:"name" bd:"public"`
	Interface   string `json:"interface,omitempty" bd:"public"`
	Description string `json:"description,omitempty" bd:"public"`
}

// Provider registers a plugin into someone else's extension point. Higher
// Priority wins when a point takes a single provider.
type Provider struct {
	Point    string `json:"point" bd:"public"`
	ID       string `json:"id" bd:"public"`
	Priority int    `json:"priority,omitempty" bd:"public"`
}

// Host identifiers used in Manifest.Targets.
const (
	TargetGateway = "gateway"
	TargetVault   = "vault"
)

// Manifest.Role values (ADR 0015). Host reserved-ID lists still win.
const (
	RoleSystem    = "system"
	RoleExtension = "extension"
)

// Requirement is a host-resolved peer (id + optional version constraint).
type Requirement struct {
	ID      string `json:"id" bd:"public"`
	Version string `json:"version,omitempty" bd:"public"`
}

// NavItem is one entry in the shell's navigation.
type NavItem struct {
	ID    string `json:"id" bd:"public"`
	Label string `json:"label" bd:"public"`
	Icon  string `json:"icon,omitempty" bd:"public"`
	Href  string `json:"href" bd:"public"`
	Order int    `json:"order,omitempty" bd:"public"`
}

// PanelSpec is one mountable surface this plugin owns.
type PanelSpec struct {
	ID   string `json:"id" bd:"public"`
	Kind string `json:"kind" bd:"public"`
	URL  string `json:"url" bd:"public"`
	// Module is an optional ES module the shell mounts in-page instead of
	// iframing URL (ADR 0024). URL stays required and remains the fallback:
	// the host renders the iframe when Module is absent, when the bundle fails
	// to load, or when the plugin is not trusted to mount in-page.
	Module string `json:"module,omitempty" bd:"public"`
	// DocumentPaths maps friendly shell document paths to this panel. These
	// are not plugin HTTP/API handlers; the host admits and serves its shell
	// for the current owner generation. Root / is host composition, never a
	// plugin document claim.
	DocumentPaths []string `json:"documentPaths,omitempty" bd:"public"`
}

// LauncherSpec is one entry in the shell's launch menu.
type LauncherSpec struct {
	Kind        string `json:"kind" bd:"public"`
	Title       string `json:"title" bd:"public"`
	Description string `json:"description,omitempty" bd:"public"`
	// Group buckets launchers in the shell's launch menu.
	Group string `json:"group,omitempty" bd:"public"`
}

// Pricing is the commercial term the store presents. The host never charges;
// it only refuses to run what was not paid for.
type Pricing struct {
	Model     string `json:"model" bd:"public"` // free | trial | paid
	SKU       string `json:"sku,omitempty" bd:"public"`
	TrialDays int    `json:"trialDays,omitempty" bd:"public"`
}

// Publisher identifies who signed the package, and owns the namespace a
// non-host extension point must start with.
type Publisher struct {
	ID   string `json:"id" bd:"public"`
	Name string `json:"name" bd:"public"`
	// LegalName is the entity a contract or a licence names, when it differs
	// from the trading name.
	LegalName    string `json:"legalName,omitempty" bd:"public"`
	URL          string `json:"url,omitempty" bd:"public"`
	SupportEmail string `json:"supportEmail,omitempty" bd:"public"`
	// KID identifies the publisher key a release is signed with: the hex
	// sha256 of the raw ed25519 public key, 64 lowercase hex characters.
	//
	// It is an IDENTIFIER, never a grant. Writing a kid here proves nothing —
	// the signature over the package and the key registry decide trust — so
	// the only thing checked at this gate is that the value is the shape a
	// verifier can look up.
	KID string `json:"kid,omitempty" bd:"public"`
}

// publisherIDPattern is the charset a publisher id must match: a lowercase
// alphanumeric first character, then lowercase alphanumerics and hyphens, at
// most 64 characters. It is the same shape a DNS label and an R2 key prefix
// both accept, because the id is used as both.
func validPublisherID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}

// validKID reports whether s is exactly 64 lowercase hex characters.
func validKID(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// KindOrInferred returns the declared Kind, or the kind implied by the manifest
// alone when the field is absent.
//
// Inference from the MANIFEST can only see two of the four: a family block
// means family and spawn means process. It cannot tell a ui package from a
// builtin, because that difference is in the tree (ui/index.html) and not in
// the manifest — contract.VerifyDir completes the inference with the files it
// walked. Callers that have no tree get "builtin", which is the conservative
// answer: it allows the fewest deliverable classes.
func (m Manifest) KindOrInferred() string {
	if k := strings.ToLower(strings.TrimSpace(m.Kind)); k != "" {
		return k
	}
	switch {
	case m.Family != nil:
		return KindFamily
	case m.Spawn:
		return KindProcess
	default:
		return KindBuiltin
	}
}

// UnmarshalJSON accepts either a publisher object or a legacy string id
// (StorePackageManifest / fixture catalogs use "publisher":"bytedesk").
func (p *Publisher) UnmarshalJSON(raw []byte) error {
	if p == nil {
		return fmt.Errorf("nil publisher")
	}
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil
	}
	if len(raw) > 0 && raw[0] == '"' {
		var id string
		if err := json.Unmarshal(raw, &id); err != nil {
			return err
		}
		p.ID = id
		if p.Name == "" {
			p.Name = id
		}
		return nil
	}
	type plain Publisher
	var v plain
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*p = Publisher(v)
	return nil
}

// UIContribution names the role a contribution plays in the shell. The host
// resolves PanelID and Command within the owner's declared contributions and
// granted authority. Priority selects a default view; ties resolve by owner ID
// then contribution ID.
type UIContribution struct {
	ID       string `json:"id" bd:"public"`
	Slot     string `json:"slot" bd:"public"`
	PanelID  string `json:"panelId,omitempty" bd:"public"`
	Command  string `json:"command,omitempty" bd:"public"`
	Label    string `json:"label,omitempty" bd:"public"`
	Icon     string `json:"icon,omitempty" bd:"public"`
	Priority int    `json:"priority,omitempty" bd:"public"`
	// Bindings make the contribution reactive. At most one per kind.
	Bindings []UIBinding `json:"bindings,omitempty" bd:"public"`
}

// UIBinding declares that one aspect of a contribution follows a bus event.
//
// The plugin declares the event it already publishes; the host subscribes on
// the plugin's behalf, under the subscribe grant that plugin already has to
// hold, and the shell re-renders. The author writes no frontend code, and the
// host never hands a plugin the shell to do it themselves.
//
// A binding is a request, not authority. The host resolves it through the same
// conjuncts as any other subscribe, so declaring one for an event the plugin
// was never granted is refused rather than obeyed — and refused observably,
// because a silently dead badge is indistinguishable from a quiet one.
type UIBinding struct {
	// Kind is what the shell renders: BindCount, BindBadge, BindLiveness or
	// BindToggle.
	Kind string `json:"kind" bd:"public"`
	// Event is the exact bus event carrying the value.
	Event string `json:"event" bd:"public"`
	// Field is the JSON field of the event payload to read. Empty means the
	// payload is the value itself.
	Field string `json:"field,omitempty" bd:"public"`
}

// Binding kinds. Each says what the shell does with the value, not where the
// contribution sits — the same separation the slot roles make.
const (
	// BindCount is a number beside the label, hidden at zero.
	BindCount = "count"
	// BindBadge is a short string, such as a status word.
	BindBadge = "badge"
	// BindLiveness is a boolean shown as present or absent, for a dot.
	BindLiveness = "liveness"
	// BindToggle is a boolean the contribution renders as on or off.
	BindToggle = "toggle"
)

// Contribution roles name function, never position (ADR 0026 D1). The shell
// owns the role-to-region map; eligibility travels with the vocabulary, so a
// slot cannot be added without saying who may fill it.
const (
	SlotDefaultView      = "default-view"
	SlotMainNavigation   = "main-navigation"
	SlotSubNavigation    = "sub-navigation"
	SlotPrimaryAction    = "primary-action"
	SlotSecondaryActions = "secondary-actions"
	SlotStatusIndicator  = "status-indicator"
	SlotObjectActions    = "object-actions"
	SlotLauncher         = "launcher"
	SlotSettings         = "settings"
	SlotCommand          = "command"
)

// ContributionEligibility describes admission, not execution authorization.
// The host still enforces scopes, availability and command permissions.
type ContributionEligibility string

// Contribution eligibility classes.
const (
	ContributionInstalledAllowed ContributionEligibility = "installed-allowed"
	ContributionConsentRequired  ContributionEligibility = "consent-required"
	ContributionCompiledOnly     ContributionEligibility = "compiled-only"
)

// ContributionRole keeps each semantic slot beside its installed-artifact policy.
type ContributionRole struct {
	Slot        string
	Eligibility ContributionEligibility
}

// ContributionRoles returns a fresh table: callers cannot mutate shared policy.
// Trusted chrome and operator-tool roles require explicit per-point consent.
func ContributionRoles() []ContributionRole {
	return []ContributionRole{
		{SlotDefaultView, ContributionInstalledAllowed},
		{SlotMainNavigation, ContributionConsentRequired},
		{SlotSubNavigation, ContributionConsentRequired},
		{SlotPrimaryAction, ContributionConsentRequired},
		{SlotSecondaryActions, ContributionConsentRequired},
		{SlotStatusIndicator, ContributionConsentRequired},
		{SlotObjectActions, ContributionInstalledAllowed},
		{SlotLauncher, ContributionConsentRequired},
		{SlotSettings, ContributionInstalledAllowed},
		{SlotCommand, ContributionConsentRequired},
	}
}

// ContributionRoleFor returns the reviewed role for slot. Unknown slots fail
// closed: a vocabulary addition without its eligibility policy is not valid.
func ContributionRoleFor(slot string) (ContributionRole, bool) {
	for _, role := range ContributionRoles() {
		if role.Slot == slot {
			return role, true
		}
	}
	return ContributionRole{}, false
}

// TargetsOrDefault returns declared targets, or ["gateway"] for historical
// manifests that omit the field.
func (m Manifest) TargetsOrDefault() []string {
	if len(m.Targets) == 0 {
		return []string{TargetGateway}
	}
	out := make([]string, 0, len(m.Targets))
	for _, t := range m.Targets {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return []string{TargetGateway}
	}
	return out
}

// Supports reports whether this plugin may run on host (gateway|vault).
func (m Manifest) Supports(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, t := range m.TargetsOrDefault() {
		if t == host {
			return true
		}
	}
	return false
}

// RoleOrDefault returns system|extension. An omitted role is extension. Host
// reserved-ID lists still treat core IDs as system.
func (m Manifest) RoleOrDefault() string {
	if strings.ToLower(strings.TrimSpace(m.Role)) == RoleSystem {
		return RoleSystem
	}
	return RoleExtension
}

// PublicRoute reports whether path is served anonymously by this manifest.
//
// A declared route ending in "/" is a prefix, matching how the host mounts
// routes; anything else is exact. The default is false, so a caller asking
// about an unknown path is told to authenticate.
func (m Manifest) PublicRoute(path string) bool {
	if path = strings.TrimSpace(path); path == "" {
		return false
	}
	for _, route := range m.PublicRoutes {
		route = strings.TrimSpace(route)
		if route == "" {
			continue
		}
		if strings.HasSuffix(route, "/") {
			if strings.HasPrefix(path, route) {
				return true
			}
			continue
		}
		if path == route {
			return true
		}
	}
	return false
}

// RequiredIDs returns requires[].id (trimmed, non-empty).
func (m Manifest) RequiredIDs() []string {
	out := make([]string, 0, len(m.Requires))
	for _, req := range m.Requires {
		if id := strings.TrimSpace(req.ID); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// ParseManifest decodes plugin.json bytes and validates them. v1 decoded
// without checking, which meant every caller was the last line of defence and
// most of them were not one.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, err
	}
	if err := Validate(m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}
