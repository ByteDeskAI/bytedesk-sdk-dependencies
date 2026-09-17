package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadDir reads plugin.json from dir, validates it, and checks that a spawn
// binary exists on disk when spawn is set.
//
// v2 changes the call shape only: Validate is a function over a Manifest
// rather than a method on one, because a validator that is a method invites a
// caller to believe the manifest validated itself.
func LoadDir(dir string) (Manifest, error) {
	m, err := loadDir(dir)
	if err != nil {
		return m, err
	}
	if err := Validate(m); err != nil {
		return m, err
	}
	return m, nil
}

// LoadDirForHost is LoadDir plus Manifest.Supports(host). Platform SDKs call
// it with TargetGateway or TargetVault: a plugin that does not target this
// host is refused here rather than failing later with a confusing error from
// something it tried to use.
func LoadDirForHost(dir, host string) (Manifest, error) {
	m, err := LoadDir(dir)
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

// loadDir reads and parses, without validating. It is separate so every
// caller above validates explicitly — there is no path that reads a manifest
// and forgets to check it.
func loadDir(dir string) (Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return Manifest{}, fmt.Errorf("plugin.json: %w", err)
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return Manifest{}, fmt.Errorf("plugin.json: %w", err)
	}
	if m.Spawn {
		bin := filepath.Join(dir, filepath.Base(m.Binary))
		if _, err := os.Stat(bin); err != nil {
			return m, fmt.Errorf("spawn binary missing: %s", m.Binary)
		}
	}
	return m, nil
}
