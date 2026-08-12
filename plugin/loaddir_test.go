package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePlugin(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDirForHost(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, `{"id":"both","version":"1.0.0","targets":["gateway","vault"]}`)
	m, err := LoadDirForHost(dir, TargetGateway, true)
	if err != nil || m.ID != "both" {
		t.Fatalf("gateway: m=%+v err=%v", m, err)
	}
	if _, err := LoadDirForHost(dir, TargetVault, true); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDirForHostRejectsWrongTarget(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, `{"id":"vaultonly","version":"1.0.0","targets":["vault"]}`)
	_, err := LoadDirForHost(dir, TargetGateway, true)
	if err == nil || !strings.Contains(err.Error(), "does not target gateway") {
		t.Fatalf("expected target reject, got %v", err)
	}
}

func TestLoadDirDiscoverAllowsNoVersion(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, `{"id":"legacy","spawn":false,"publisher":"bytedesk"}`)
	m, err := LoadDirDiscover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Publisher == nil || m.Publisher.ID != "bytedesk" {
		t.Fatalf("publisher=%+v", m.Publisher)
	}
	if _, err := LoadDir(dir); err == nil {
		t.Fatal("strict LoadDir should require version")
	}
}

func TestLoadDirMissingSpawnBinary(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, `{"id":"x","version":"1.0.0","spawn":true,"binary":"x"}`)
	if _, err := LoadDir(dir); err == nil {
		t.Fatal("expected missing binary")
	}
}
