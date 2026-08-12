// Package plugin is the canonical plugin.json / contribution model.
package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Manifest is the CDM for plugin.json (nav, panels, spawn, commercial fields).
type Manifest struct {
	ID             string         `json:"id"`
	Version        string         `json:"version,omitempty"`
	Nav            []NavItem      `json:"nav,omitempty"`
	Panels         []PanelSpec    `json:"panels,omitempty"`
	Launchers      []LauncherSpec `json:"launchers,omitempty"`
	Scopes         []string       `json:"scopes,omitempty"`
	Routes         []string       `json:"routes,omitempty"`
	Spawn          bool           `json:"spawn,omitempty"`
	Binary         string         `json:"binary,omitempty"`
	Socket         string         `json:"socket,omitempty"`
	MinCoreVersion string         `json:"minCoreVersion,omitempty"`
	Targets        []string       `json:"targets,omitempty"` // gateway, vault
	Pricing        *Pricing       `json:"pricing,omitempty"`
	Publisher      *Publisher     `json:"publisher,omitempty"`
}

// Host identifiers used in Manifest.Targets.
const (
	TargetGateway = "gateway"
	TargetVault   = "vault"
)

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
	Model string `json:"model"` // free | paid
	SKU   string `json:"sku,omitempty"`
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
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") || strings.Contains(id, " ") {
		return fmt.Errorf("plugin id must be a single path segment")
	}
	if requireVersion && strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("plugin version required")
	}
	if m.Pricing != nil {
		model := strings.ToLower(strings.TrimSpace(m.Pricing.Model))
		if model != "free" && model != "paid" {
			return fmt.Errorf("pricing.model must be free or paid")
		}
		if model == "paid" && strings.TrimSpace(m.Pricing.SKU) == "" {
			return fmt.Errorf("pricing.sku required for paid plugins")
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
