// Package plugin is the canonical plugin.json / contribution model.
package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Manifest is the CDM for plugin.json (nav, panels, spawn, commercial fields).
type Manifest struct {
	ID               string         `json:"id" bd:"public"`
	Version          string         `json:"version,omitempty" bd:"public"`
	Nav              []NavItem      `json:"nav,omitempty" bd:"public"`
	Panels           []PanelSpec    `json:"panels,omitempty" bd:"public"`
	Launchers        []LauncherSpec `json:"launchers,omitempty" bd:"public"`
	Scopes           []string       `json:"scopes,omitempty" bd:"public"`
	Routes           []string       `json:"routes,omitempty" bd:"public"`
	Spawn            bool           `json:"spawn,omitempty" bd:"public"`
	Binary           string         `json:"binary,omitempty" bd:"public"`
	Socket           string         `json:"socket,omitempty" bd:"public"`
	MinCoreVersion   string         `json:"minCoreVersion,omitempty" bd:"public"`
	Targets          []string       `json:"targets,omitempty" bd:"public"` // gateway, vault
	Role             string         `json:"role,omitempty" bd:"public"`    // system | extension
	Provides         []string       `json:"provides,omitempty" bd:"public"`
	Requires         []Requirement  `json:"requires,omitempty" bd:"public"`
	RequiresProvides []string       `json:"requiresProvides,omitempty" bd:"public"`
	Pricing          *Pricing       `json:"pricing,omitempty" bd:"public"`
	Publisher        *Publisher     `json:"publisher,omitempty" bd:"public"`

	// Family declares platform members the host selects between (ADR 0020).
	// Until now this lived only in the gateway's private parser, so an SDK
	// author could not express a family at all.
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
	Permissions *Permissions          `json:"permissions,omitempty" bd:"public"`
	UI          []UIContribution      `json:"ui,omitempty" bd:"public"`

	// Config declares the settings sections this plugin contributes. It is
	// SCHEMA, never values: the host owns values and reads its own record, so a
	// plugin describes what it configures and the operator's choices never live
	// in a file the plugin writes (ADR 0026 C0). A section is served at the
	// host.settings.section extension point, which the plugin must also name in
	// Implements; this field is what lets the host list and label a section
	// before the plugin is asked for anything.
	Config *Config `json:"config,omitempty" bd:"public"`
}

// HostPointNamespace prefixes every extension point the host owns. Only the
// host and plugins compiled into it may declare a point under it. A manifest
// cannot prove where it runs, so the host enforces that half at admission;
// Validate only checks that a name is well formed and namespaced.
const HostPointNamespace = "host."

// SettingsSectionPoint is the extension point a plugin implements to contribute
// a settings section. Its operations are cmd.host.settings.section.v1.snapshot
// and cmd.host.settings.section.v1.patch.
const SettingsSectionPoint = HostPointNamespace + "settings.section"

// ValidateExtendsName checks one name a plugin declares in Extends. A point
// name is lowercase letters, digits and hyphens in at least three dot-separated
// segments, and it starts with HostPointNamespace or with the publisher id and a
// dot: "acme.widgets.panel" for publisher "acme". A nil or empty publisher has
// no namespace. The version is not part of the name; it belongs in Interface.
func ValidateExtendsName(publisher *Publisher, name string) error {
	segments := strings.Split(name, ".")
	if len(segments) < 3 {
		return fmt.Errorf("extends: extension point %q needs at least three dot-separated segments", name)
	}
	for _, segment := range segments {
		if segment == "" || strings.Trim(segment, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			return fmt.Errorf("extends: extension point %q must be lowercase letters, digits and hyphens between dots", name)
		}
	}
	if strings.HasPrefix(name, HostPointNamespace) {
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
// equal the contribution ID the plugin registers at SettingsSectionPoint. Fields
// is the typed schema a host renders controls from; ConfigFieldsFromStruct
// derives it from a tagged struct.
type ConfigSection struct {
	ID          string        `json:"id" bd:"public"`
	Title       string        `json:"title,omitempty" bd:"public"`
	Description string        `json:"description,omitempty" bd:"public"`
	Fields      []ConfigField `json:"fields,omitempty" bd:"public"`
}

// When constrains a plugin or family member to a set of operating systems,
// matched against runtime.GOOS.
type When struct {
	OS []string `json:"os,omitempty" bd:"public"`
}

// FamilyMember is one platform member of a family.
type FamilyMember struct {
	ID   string `json:"id" bd:"public"`
	When When   `json:"when,omitzero" bd:"public"`
}

// Family declares the platform members a host selects between (ADR 0020).
// Selector is "runtime.os" in v1.
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

type NavItem struct {
	ID    string `json:"id" bd:"public"`
	Label string `json:"label" bd:"public"`
	Icon  string `json:"icon,omitempty" bd:"public"`
	Href  string `json:"href" bd:"public"`
	Order int    `json:"order,omitempty" bd:"public"`
}

type PanelSpec struct {
	ID   string `json:"id" bd:"public"`
	Kind string `json:"kind" bd:"public"`
	URL  string `json:"url" bd:"public"`
	// Module is an optional ES module the shell mounts in-page instead of
	// iframing URL (ADR 0024). URL stays required and remains the fallback:
	// the host renders the iframe when Module is absent, when the bundle fails
	// to load, or when the plugin is not trusted to mount in-page. Like URL, it
	// is rewritten to /p/<id>/... for an out-of-process plugin, so declare it
	// relative to the plugin root.
	Module string `json:"module,omitempty" bd:"public"`
	// DocumentPaths maps friendly shell document paths to this panel. These
	// are not plugin HTTP/API handlers; the host admits and serves its shell
	// for the current owner generation. See ValidateDocumentPath. Root / is
	// host composition (the default-view slot), never a plugin document claim.
	DocumentPaths []string `json:"documentPaths,omitempty" bd:"public"`
}

type LauncherSpec struct {
	Kind        string `json:"kind" bd:"public"`
	Title       string `json:"title" bd:"public"`
	Description string `json:"description,omitempty" bd:"public"`
	// Group buckets launchers in the shell's launch menu.
	Group string `json:"group,omitempty" bd:"public"`
}

type Pricing struct {
	Model     string `json:"model" bd:"public"` // free | trial | paid
	SKU       string `json:"sku,omitempty" bd:"public"`
	TrialDays int    `json:"trialDays,omitempty" bd:"public"`
}

type Publisher struct {
	ID   string `json:"id" bd:"public"`
	Name string `json:"name" bd:"public"`
	URL  string `json:"url,omitempty" bd:"public"`
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

// ParseManifest decodes plugin.json bytes.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Validate checks id/version/spawn basename rules (authoring + pack).
func (m Manifest) Validate() error {
	return m.validate(true)
}

// ValidateDiscover is the host enable/scan gate: version is optional so
// historical plugin.json files (id + spawn only) still load.
func (m Manifest) ValidateDiscover() error {
	return m.validate(false)
}

func (m Manifest) validate(requireVersion bool) error {
	id := strings.TrimSpace(m.ID)
	if id == "" {
		return fmt.Errorf("plugin id required")
	}
	if err := validateIDSegment("plugin id", id); err != nil {
		return err
	}
	if requireVersion && strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("plugin version required")
	}
	if m.Pricing != nil {
		model := strings.ToLower(strings.TrimSpace(m.Pricing.Model))
		if model != "free" && model != "trial" && model != "paid" {
			return fmt.Errorf("pricing.model must be free, trial, or paid")
		}
		if (model == "paid" || model == "trial") && strings.TrimSpace(m.Pricing.SKU) == "" {
			return fmt.Errorf("pricing.sku required for %s plugins", model)
		}
		if model == "trial" && m.Pricing.TrialDays < 0 {
			return fmt.Errorf("pricing.trialDays must be >= 0")
		}
	}
	role := strings.ToLower(strings.TrimSpace(m.Role))
	if role != "" && role != RoleSystem && role != RoleExtension {
		return fmt.Errorf("role must be system or extension")
	}
	if err := validateTokenList("provides", m.Provides); err != nil {
		return err
	}
	if err := validateTokenList("requiresProvides", m.RequiresProvides); err != nil {
		return err
	}
	for _, req := range m.Requires {
		rid := strings.TrimSpace(req.ID)
		if rid == "" {
			return fmt.Errorf("requires.id required")
		}
		if err := validateIDSegment("requires.id", rid); err != nil {
			return err
		}
		if rid == id {
			return fmt.Errorf("requires.id cannot be self")
		}
		if err := req.ValidateVersionConstraint(); err != nil {
			return err
		}
	}
	if m.Spawn {
		bin := strings.TrimSpace(m.Binary)
		if bin == "" || strings.Contains(bin, "/") || strings.Contains(bin, "\\") || strings.Contains(bin, "..") {
			return fmt.Errorf("spawn binary must be a relative basename")
		}
	}
	if sock := strings.TrimSpace(m.Socket); sock != "" {
		if strings.Contains(sock, "/") || strings.Contains(sock, "\\") || strings.Contains(sock, "..") {
			return fmt.Errorf("socket must be a relative basename")
		}
	}
	for _, t := range m.Targets {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != TargetGateway && t != TargetVault {
			return fmt.Errorf("targets: unknown %q (gateway|vault)", t)
		}
	}
	for _, point := range m.Extends {
		if err := ValidateExtendsName(m.Publisher, strings.TrimSpace(point.Name)); err != nil {
			return err
		}
	}
	if err := m.validateDocumentPaths(); err != nil {
		return err
	}
	if err := m.Config.validate(); err != nil {
		return err
	}
	return m.validateRuntimeContract()
}

// TargetsOrDefault returns declared targets, or ["gateway"] for historical
// manifests that omit the field.
func (m Manifest) TargetsOrDefault() []string {
	if len(m.Targets) == 0 {
		return []string{TargetGateway}
	}
	out := make([]string, 0, len(m.Targets))
	for _, t := range m.Targets {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
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

// RoleOrDefault returns system|extension. Omitted role is extension (legacy
// Store packs). Host reserved-ID lists still treat core IDs as system.
func (m Manifest) RoleOrDefault() string {
	r := strings.ToLower(strings.TrimSpace(m.Role))
	if r == RoleSystem {
		return RoleSystem
	}
	return RoleExtension
}

func validateIDSegment(what, s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("%s required", what)
	}
	if strings.Contains(s, "/") || strings.Contains(s, "\\") || strings.Contains(s, "..") || strings.Contains(s, " ") {
		return fmt.Errorf("%s must be a single path segment", what)
	}
	return nil
}

func validateTokenList(field string, vals []string) error {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			return fmt.Errorf("%s: empty token", field)
		}
		if strings.Contains(v, "/") || strings.Contains(v, "\\") || strings.Contains(v, "..") || strings.Contains(v, " ") {
			return fmt.Errorf("%s: %q must be a single path segment", field, v)
		}
	}
	return nil
}
