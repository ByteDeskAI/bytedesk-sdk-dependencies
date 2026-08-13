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
}

type LauncherSpec struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
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
