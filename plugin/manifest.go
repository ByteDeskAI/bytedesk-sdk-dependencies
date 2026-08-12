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
	Pricing        *Pricing       `json:"pricing,omitempty"`
	Publisher      *Publisher     `json:"publisher,omitempty"`
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
	Model string `json:"model"` // free | paid
	SKU   string `json:"sku,omitempty"`
}

type Publisher struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// ParseManifest decodes plugin.json bytes.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Validate checks id/version/spawn basename rules (host + pack share this).
func (m Manifest) Validate() error {
	id := strings.TrimSpace(m.ID)
	if id == "" {
		return fmt.Errorf("plugin id required")
	}
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") || strings.Contains(id, " ") {
		return fmt.Errorf("plugin id must be a single path segment")
	}
	if strings.TrimSpace(m.Version) == "" {
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
	return nil
}
