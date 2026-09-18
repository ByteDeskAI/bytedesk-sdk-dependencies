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
	// one it already has (BDP6004). Empty accepts any. It is a per-plugin
	// release floor and must not be confused with MinAccepted below, which
	// is a floor over the contract itself.
	MinPluginVersion string `json:"minPluginVersion,omitempty"`
	// HostReservations are the route prefixes THIS host serves itself, which
	// a plugin's alias may not claim (BDP6011). A host passes its own routing
	// table; the SDK holds no copy, because a list of one host's routes kept
	// in a shared library is a guess that goes stale silently — see
	// plugin.ReservedAliasPrefixes for the two prefixes that really are
	// universal. Empty means no host-specific reservation is applied.
	HostReservations []string `json:"hostReservations,omitempty"`
	// MinAccepted is the lowest manifest contract this gate accepts. A
	// manifest below it is refused with BDP1007 and nothing else about it is
	// judged, because a document written against an older contract read with
	// this one's meanings is the failure the whole 1xxx band exists to
	// prevent. 0 accepts any contract, which is what keeps the dual-loading
	// window open: the host migrates a v1 document in memory and verifies the
	// result, while the Store publish gate sets 2 from day one. Raising this
	// to ContractV2 at the host is the single flip that closes the window.
	MinAccepted int `json:"minAccepted,omitempty"`
}

// The contract numbers a manifest may carry. A manifest with no contract field
// is a v1 document — v1 predates the field, so its absence IS the version.
const (
	ContractV1 = 1
	ContractV2 = plugin.ProtocolMajor
)

// documentContract reads the contract a raw document declares, defaulting an
// absent, zero or unreadable field to v1. It works off the document rather
// than the decoded Manifest because the floor is judged BEFORE the schema: a
// v1 document carries v1 fields, and running the v2 schema over it first would
// bury the one finding that matters under a page of them.
func documentContract(doc any) int {
	obj, ok := doc.(map[string]any)
	if !ok {
		return ContractV1
	}
	switch n := obj["contract"].(type) {
	case json.Number:
		if i, err := n.Int64(); err == nil && i > 0 {
			return int(i)
		}
	case float64:
		if n > 0 {
			return int(n)
		}
	}
	return ContractV1
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
//
// TWO THINGS EMBED CANNOT CARRY, and a consumer running the corpus from here
// rather than from a checkout will diverge on exactly the fixtures that need
// them:
//
//   - file modes. Go's embed reports every file as 0444, so a fixture whose
//     verdict depends on the executable bit (a package declaring a binary)
//     cannot be reproduced from Assets.
//   - symlinks. A fixture that needs one ships links.json instead, as data the
//     consumer recreates; the reference suite and any consumer reading Assets
//     both have to act on it.
//
// Found when Vault ran the corpus through this package (TM-403) and two
// binary fixtures disagreed. A consumer that vendors the tree from git — as
// the Toolbox does, because Rust cannot import a Go package — gets both and
// needs neither workaround.
func Assets() fs.FS {
	sub, err := fs.Sub(assets, "v2")
	if err != nil {
		panic("contract: embedded assets: " + err.Error())
	}
	return sub
}

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
	// The contract floor comes before every other judgement: the rest of this
	// verifier reads the document with THIS contract's meanings, and applying
	// them to an older document is exactly the misreading the band prevents.
	// So a document below the floor is refused on that alone, with no second
	// page of findings that only describe the version mismatch.
	if opts.MinAccepted > 0 && documentContract(doc) < opts.MinAccepted {
		r.add("BDP1007", "contract", documentContract(doc), opts.MinAccepted)
		return plugin.Manifest{}, doc, false
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
	// The deprecations only the raw document can still see. Both of these are
	// decoded away by the time a Manifest exists: a string publisher becomes
	// a Publisher, and a top-level homepage becomes nothing at all, because
	// the field moved into identity and no Go field claims the old name.
	//
	// ponytail: a top-level homepage therefore draws BDP1006 (unknown
	// property) as well as BDP5004. Both are true and the pair is cheaper
	// than teaching the schema walk about names it must not warn on.
	if obj, isObj := doc.(map[string]any); isObj {
		if _, isString := obj["publisher"].(string); isString {
			r.add("BDP5001", "publisher")
		}
		if _, present := obj["homepage"]; present {
			r.add("BDP5004", "homepage")
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
	for i, raw := range m.Aliases {
		alias := "/" + strings.Trim(strings.TrimSpace(raw), "/")
		for _, reservation := range opts.HostReservations {
			reservation = "/" + strings.Trim(strings.TrimSpace(reservation), "/")
			if reservation == "/" {
				continue
			}
			if alias == reservation || strings.HasPrefix(alias, reservation+"/") {
				r.add("BDP6011", fmt.Sprintf("aliases[%d]", i), raw, hostName(opts.Host), reservation)
				break
			}
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

// hostName is what a host-gate diagnostic calls the host when the caller
// supplied reservations but no name. "this host" is honest; inventing one
// would put a product name in a refusal that nothing checked.
func hostName(host string) string {
	if h := strings.TrimSpace(host); h != "" {
		return h
	}
	return "this host"
}
