// Package contract is the one plugin contract, verified the same way at every
// gate: the author's CLI, the in-tree CI ratchet, Store publish, and host
// install and boot all call Verify or VerifyDir and read the same Report.
//
// The contract is data first. plugin.schema.json is the structural half,
// layout.json says what may live where in a package, diagnostics.json is the
// code table, and the fixture corpus under v2/fixtures is the proof: a binding
// in another language is conformant when it produces the same (code, severity,
// path) tuples for every fixture. This package is the Go reference binding.
//
// It imports only the plugin package, semver, the JSON Schema validator and
// the standard library, and TestContractImportsAreClosed keeps it that way, so
// a host, the Store and a standalone CLI can all depend on it without pulling
// in each other.
package contract

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
	"github.com/Masterminds/semver/v3"
)

// Contract is the manifest contract this package verifies, reported on every
// Report so a reader knows which rules produced it.
const Contract = plugin.ProtocolMajor

// Mode selects how much of a directory is judged. See layout.json "modes".
type Mode string

// Verification modes.
const (
	// ModePackage judges an extracted package: layout, limits, references.
	// It is the default and the host's gate.
	ModePackage Mode = "package"
	// ModeSource judges a source tree: the manifest and the symlink rule only.
	ModeSource Mode = "source"
)

// Options selects the gates beyond the manifest itself. The zero value verifies
// the manifest and the package layout for no host in particular.
type Options struct {
	// Host, when set, applies the host gate: the manifest must target it
	// (BDP6001). Values are plugin.TargetGateway and plugin.TargetVault.
	Host string `json:"host,omitempty"`
	// Mode is ModePackage unless ModeSource is asked for.
	Mode Mode `json:"mode,omitempty"`
	// CoreVersion is the verifying host's own version. When set, a manifest
	// whose minCoreVersion exceeds it is refused (BDP6002).
	CoreVersion string `json:"coreVersion,omitempty"`
	// MinPluginVersion is the lowest manifest version the host accepts for
	// this plugin, for a host that must not load an older release than the
	// one it already has (BDP6004). Empty accepts any.
	//
	// The name MinAccepted is RESERVED for the contract-major floor TM-396
	// adds as an int: a manifest whose contract field is below it is refused
	// with BDP1007, and raising that floor from 1 to 2 in one SDK release is
	// what closes the v1 dual-loading window. It is a different rule from
	// this per-plugin version floor and must not share its name.
	MinPluginVersion string `json:"minPluginVersion,omitempty"`
}

// Report is the wire shape. `bytedesk plugin verify --json` prints it and a
// host API returns it unchanged. Diagnostics are sorted by (path, code) so two
// bindings verifying the same tree produce byte-identical output.
type Report struct {
	Contract    int                 `json:"contract"`
	Root        string              `json:"root,omitempty"`
	OK          bool                `json:"ok"`
	Manifest    *plugin.Manifest    `json:"manifest,omitempty"`
	Diagnostics []plugin.Diagnostic `json:"diagnostics"`
}

func (r *Report) add(code, path string, args ...any) {
	r.Diagnostics = append(r.Diagnostics, plugin.NewDiagnostic(code, path, args...))
}

func (r *Report) hasErrors() bool { return plugin.FirstError(r.Diagnostics) != nil }

// finish puts the report in wire order and settles OK. A nil list marshals as
// null, and a consumer reading "diagnostics": null has to special-case it, so
// the empty list is spelled out.
func (r *Report) finish() {
	if r.Diagnostics == nil {
		r.Diagnostics = []plugin.Diagnostic{}
	}
	plugin.SortDiagnostics(r.Diagnostics)
	r.OK = !r.hasErrors()
}

//go:embed all:v2
var assets embed.FS

// Assets is the contract as data: plugin.schema.json, layout.json,
// diagnostics.json and the fixture corpus, rooted at "." so a binding in
// another language reads exactly what this one embeds.
func Assets() fs.FS {
	sub, err := fs.Sub(assets, "v2")
	if err != nil {
		panic("contract: embedded assets: " + err.Error())
	}
	return sub
}

// ErrNotImplemented is returned by MigrateV1 until TM-396 lands the migration.
var ErrNotImplemented = errors.New("contract: not implemented")

// MigrateV1 rewrites a v1 plugin.json into v2 deterministically. It is the
// entry point `bytedesk plugin migrate` calls; the rewrite itself is TM-396.
func MigrateV1(raw []byte) ([]byte, error) { return nil, ErrNotImplemented }

// Verify judges plugin.json bytes on their own: the schema band, the semantic
// band, deprecations, and the host gate when Options asks for one. It cannot
// see a tree, so layout and reference findings need VerifyDir.
func Verify(raw []byte, opts Options) Report {
	r := Report{Contract: Contract}
	verifyManifest(&r, raw, opts)
	r.finish()
	return r
}

// VerifyDir judges a plugin directory: everything Verify does, then the tree
// against layout.json for the inferred kind in opts.Mode. The error is for a
// root that cannot be read at all; a missing or broken plugin.json is a
// diagnostic, because "this package is not a package" is a verdict.
func VerifyDir(root string, opts Options) (Report, error) {
	r := Report{Contract: Contract, Root: root}
	if opts.Mode == "" {
		opts.Mode = ModePackage
	}
	info, err := os.Stat(root)
	if err != nil {
		return r, err
	}
	if !info.IsDir() {
		return r, fmt.Errorf("contract: %s is not a directory", root)
	}
	raw, err := os.ReadFile(filepath.Join(root, layoutSpec().Manifest))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return r, err
	}
	var (
		m   plugin.Manifest
		doc any
		ok  bool
	)
	if err != nil {
		r.add("BDP3003", layoutSpec().Manifest, layoutSpec().Manifest)
	} else {
		m, doc, ok = verifyManifest(&r, raw, opts)
	}
	if err := verifyTree(&r, root, m, doc, ok, opts.Mode); err != nil {
		return r, err
	}
	r.finish()
	return r, nil
}

// verifyManifest runs the bands that need only the bytes. It returns the
// decoded manifest, the raw document, and whether decoding succeeded; the
// tree pass needs all three.
func verifyManifest(r *Report, raw []byte, opts Options) (plugin.Manifest, any, bool) {
	doc, err := decodeDocument(raw)
	if err != nil {
		r.add("BDP1001", "", err.Error())
		return plugin.Manifest{}, nil, false
	}
	schemaDiagnostics(r, doc)
	var m plugin.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		// A type the schema already refused decodes badly for the same
		// reason; only an unexplained decode failure gets its own line.
		if !r.hasErrors() {
			r.add("BDP1002", "", err.Error())
		}
		return plugin.Manifest{}, doc, false
	}
	r.Manifest = &m
	r.Diagnostics = append(r.Diagnostics, plugin.Diagnostics(m, true)...)
	if obj, isObj := doc.(map[string]any); isObj {
		if _, isString := obj["publisher"].(string); isString {
			r.add("BDP5001", "publisher")
		}
	}
	hostGate(r, m, opts)
	return m, doc, true
}

// hostGate is the BDP6xxx band: what a particular host, at a particular
// version, may load. It runs only for the options a caller set, so Verify
// with zero Options is host-neutral.
func hostGate(r *Report, m plugin.Manifest, opts Options) {
	if host := strings.ToLower(strings.TrimSpace(opts.Host)); host != "" && !m.Supports(host) {
		r.add("BDP6001", "targets", m.ID, host)
	}
	if core := strings.TrimSpace(opts.CoreVersion); core != "" && strings.TrimSpace(m.MinCoreVersion) != "" {
		have, err := semver.NewVersion(core)
		if err != nil {
			r.add("BDP6003", "", "coreVersion", core)
		} else if need, err := semver.NewVersion(strings.TrimSpace(m.MinCoreVersion)); err != nil {
			r.add("BDP6003", "minCoreVersion", "minCoreVersion", m.MinCoreVersion)
		} else if need.GreaterThan(have) {
			r.add("BDP6002", "minCoreVersion", m.MinCoreVersion, core)
		}
	}
	if floor := strings.TrimSpace(opts.MinPluginVersion); floor != "" && strings.TrimSpace(m.Version) != "" {
		least, err := semver.NewVersion(floor)
		if err != nil {
			r.add("BDP6003", "", "minPluginVersion", floor)
		} else if have, err := semver.NewVersion(strings.TrimSpace(m.Version)); err != nil {
			r.add("BDP6003", "version", "version", m.Version)
		} else if have.LessThan(least) {
			r.add("BDP6004", "version", m.Version, floor)
		}
	}
}
