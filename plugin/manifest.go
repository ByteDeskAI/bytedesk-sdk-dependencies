// Package plugin is the canonical plugin.json / contribution model.
package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Manifest is the CDM for plugin.json (nav, panels, spawn, commercial fields).
type Manifest struct {
	ID               string         `json:"id"`
	Version          string         `json:"version,omitempty"`
	Nav              []NavItem      `json:"nav,omitempty"`
	Panels           []PanelSpec    `json:"panels,omitempty"`
	Launchers        []LauncherSpec `json:"launchers,omitempty"`
	Scopes           []string       `json:"scopes,omitempty"`
	Routes           []string       `json:"routes,omitempty"`
	Spawn            bool           `json:"spawn,omitempty"`
	Binary           string         `json:"binary,omitempty"`
	Socket           string         `json:"socket,omitempty"`
	MinCoreVersion   string         `json:"minCoreVersion,omitempty"`
	Targets          []string       `json:"targets,omitempty"` // gateway, vault
	Role             string         `json:"role,omitempty"`    // system | extension
	Provides         []string       `json:"provides,omitempty"`
	Requires         []Requirement  `json:"requires,omitempty"`
	RequiresProvides []string       `json:"requiresProvides,omitempty"`
	Pricing          *Pricing       `json:"pricing,omitempty"`
	Publisher        *Publisher     `json:"publisher,omitempty"`

	// Family declares platform members the host selects between (ADR 0020).
	// Until now this lived only in the gateway's private parser, so an SDK
	// author could not express a family at all.
	Family *Family `json:"family,omitempty"`
	// When constrains this plugin to a set of operating systems.
	When When `json:"when,omitzero"`

	// Extends names extension points this plugin owns; Implements registers it
	// into points other plugins own (ADR 0023). The host mediates: a plugin
	// never loads, execs or proxies another.
	Extends    []ExtensionPoint `json:"extends,omitempty"`
	Implements []Provider       `json:"implements,omitempty"`

	// Critical marks a role:system plugin the host must not serve without. A
	// critical plugin that fails to start halts the host rather than running
	// with the capability missing. Meaningless for extensions.
	Critical bool `json:"critical,omitempty"`
}

// When constrains a plugin or family member to a set of operating systems,
// matched against runtime.GOOS.
type When struct {
	OS []string `json:"os,omitempty"`
}

// FamilyMember is one platform member of a family.
type FamilyMember struct {
	ID   string `json:"id"`
	When When   `json:"when,omitzero"`
}

// Family declares the platform members a host selects between (ADR 0020).
// Selector is "runtime.os" in v1.
type Family struct {
	Selector string         `json:"selector,omitempty"`
	Members  []FamilyMember `json:"members,omitempty"`
}

// ExtensionPoint is a named seam a plugin owns and others register into.
type ExtensionPoint struct {
	Name        string `json:"name"`
	Interface   string `json:"interface,omitempty"`
	Description string `json:"description,omitempty"`
}

// Provider registers a plugin into someone else's extension point. Higher
// Priority wins when a point takes a single provider.
type Provider struct {
	Point    string `json:"point"`
	ID       string `json:"id"`
	Priority int    `json:"priority,omitempty"`
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
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
}

type NavItem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
	Href  string `json:"href"`
	Order int    `json:"order,omitempty"`
}

type PanelSpec struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
	// Module is an optional ES module the shell mounts in-page instead of
	// iframing URL (ADR 0024). URL stays required and remains the fallback:
	// the host renders the iframe when Module is absent, when the bundle fails
	// to load, or when the plugin is not trusted to mount in-page. Like URL, it
	// is rewritten to /p/<id>/... for an out-of-process plugin, so declare it
	// relative to the plugin root.
	Module string `json:"module,omitempty"`
}

type LauncherSpec struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// Group buckets launchers in the shell's launch menu.
	Group string `json:"group,omitempty"`
}

type Pricing struct {
	Model     string `json:"model"` // free | trial | paid
	SKU       string `json:"sku,omitempty"`
	TrialDays int    `json:"trialDays,omitempty"`
}

type Publisher struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
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
	return nil
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
