package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadDir reads plugin.json, runs Validate (version required), and checks
// that a spawn binary exists on disk when spawn is true.
func LoadDir(dir string) (Manifest, error) {
	return loadDir(dir, true)
}

// LoadDirDiscover is LoadDir with ValidateDiscover (version optional).
func LoadDirDiscover(dir string) (Manifest, error) {
	return loadDir(dir, false)
}

// LoadDirForHost is LoadDir plus Manifest.Supports(host). Platform SDKs
// call this with TargetGateway or TargetVault.
func LoadDirForHost(dir, host string, requireVersion bool) (Manifest, error) {
	m, err := loadDir(dir, requireVersion)
	if err != nil {
		return m, err
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return m, fmt.Errorf("host target required")
	}
	if !m.Supports(host) {
		return m, fmt.Errorf("plugin %s does not target %s", m.ID, host)
	}
	return m, nil
}

func loadDir(dir string, requireVersion bool) (Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return Manifest{}, fmt.Errorf("plugin.json: %w", err)
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return Manifest{}, fmt.Errorf("plugin.json: %w", err)
	}
	if requireVersion {
		if err := m.Validate(); err != nil {
			return m, err
		}
	} else if err := m.ValidateDiscover(); err != nil {
		return m, err
	}
	if m.Spawn {
		bin := filepath.Join(dir, filepath.Base(m.Binary))
		if _, err := os.Stat(bin); err != nil {
			return m, fmt.Errorf("spawn binary missing: %s", m.Binary)
		}
	}
	return m, nil
}
