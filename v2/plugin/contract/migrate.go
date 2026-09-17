package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
)

// MigrateInput is what a migration cannot read off the old manifest.
//
// Three of these are decisions, not defaults. A publisher cannot be guessed
// for a package that is about to be signed; a display name and a description
// are what a human will read in a store row, and a host that invented them
// would have the operator approving text nobody wrote. Each one falls back to
// something honest — the tree, the manifest, or a diagnostic saying so — and
// never to a value that merely looks authored.
type MigrateInput struct {
	// Dir is the plugin directory, read for the files a v2 identity names
	// (README.md, CHANGELOG.md, images/icon.*) and for the kind inference
	// fallback. Empty means "the bytes only": nothing on disk is consulted.
	Dir string
	// PublisherID supplies a publisher for a manifest that has none. It is
	// required unless FirstParty says this is ByteDesk's own.
	PublisherID string
	// FirstParty declares the package ByteDesk's own, which is the only way
	// a missing publisher becomes {"bytedesk", "ByteDesk"}.
	FirstParty bool
	// DisplayName and Description fill identity. Empty falls back to
	// nav[0].label and the README's first paragraph respectively.
	DisplayName string
	Description string
	// Kind overrides the inference. Required when nothing in the tree says
	// what the package is.
	Kind string
}

// Migration shapes that are the AUTHOR's decision and are never rewritten:
//
//   - BDP2111, a permission token outside the bus grammar
//     ("event.terminal.openLink"). Renaming it would rename a subject that
//     something already publishes on, so the fix is a code change the author
//     makes and this tool reports.
//   - BDP2004, an id whose own namespace reaches a permanently ineligible
//     family. The fix is a different plugin id, which renames its routes, its
//     storage and every subject it serves.
//
// Both reach the caller as notes carrying the exact diagnostic verify will
// produce, so an author is never told one thing here and another at the gate.

var (
	// A bare major, or a major.minor, that a v1 manifest used as a version.
	versionMajorOnly = regexp.MustCompile(`^\d+$`)
	versionMajorMin  = regexp.MustCompile(`^\d+\.\d+$`)
	// The first paragraph of a README: everything up to the first blank line
	// after the first line that is neither blank, a heading, nor a badge.
	readmeBadge = regexp.MustCompile(`^\[!\[.*\)$`)
)

// MigrateV1 rewrites a v1 plugin.json into v2 deterministically.
//
// It is a pure function of (raw, in, the files in in.Dir): the same inputs
// produce byte-identical output, key order is preserved, and a subtree the
// rewrite does not touch is copied through unchanged. The rules, in order:
//
//	contract: 2 is added
//	kind is inferred (or taken from in.Kind) and spawn is deleted
//	version "1" and "1.2" are padded to semver
//	publisher as a string becomes an object; absent becomes ByteDesk's only
//	  for a first-party package, and otherwise needs in.PublisherID
//	identity is built from in, nav[0].label, the README and the files present
//	top-level homepage moves into identity.homepage
//	protocol.major 1 becomes 2
//	implements[].point loses a "host." prefix the point does not have
//	permissions entries inside the plugin's own namespace are dropped
//
// notes are the diagnostics the migrated manifest still carries: what the
// author must decide, in the same wording the verifier will use. An error
// note means the output is written but will not verify. err is returned only
// when the rewrite cannot be performed at all.
func MigrateV1(raw []byte, in MigrateInput) (out []byte, notes []plugin.Diagnostic, err error) {
	doc, err := decodeObject(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("contract: plugin.json: %w", err)
	}
	id, _ := doc.str("id")
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil, fmt.Errorf("contract: plugin.json has no id; migration cannot name the plugin")
	}

	doc.prepend("contract", mustRaw(plugin.ProtocolMajor))

	kind, err := migrateKind(doc, in)
	if err != nil {
		return nil, nil, err
	}
	doc.insertAfter("contract", "kind", mustRaw(kind))
	doc.delete("spawn")

	migrateVersion(doc)
	if err := migratePublisher(doc, in); err != nil {
		return nil, nil, err
	}
	migrateIdentity(doc, id, in)
	migrateProtocol(doc)
	migrateImplements(doc)
	migratePermissions(doc, id)

	out, err = doc.indented()
	if err != nil {
		return nil, nil, err
	}
	// The notes are the verifier's own findings on the result. Reporting
	// anything else here would let migrate say "done" about a manifest the
	// next gate refuses.
	return out, Verify(out, Options{}).Diagnostics, nil
}

// migrateKind resolves what the package IS. in.Kind wins, then a kind already
// written, then the fallback order layout.json documents.
func migrateKind(doc *jsonObject, in MigrateInput) (string, error) {
	if k := strings.TrimSpace(in.Kind); k != "" {
		return k, nil
	}
	if k, ok := doc.str("kind"); ok && strings.TrimSpace(k) != "" {
		return strings.TrimSpace(k), nil
	}
	var spawn bool
	if raw, ok := doc.get("spawn"); ok {
		_ = json.Unmarshal(raw, &spawn)
	}
	switch {
	// binary/socket before family and ui: v1 let a manifest declare a binary
	// with spawn absent or false, and the v2 verifier refuses either field on
	// any kind but process (BDP2204). Inferring anything else from a package
	// that ships an executable would migrate it into a refusal.
	case spawn, doc.has("binary"), doc.has("socket"):
		return plugin.KindProcess, nil
	case doc.has("family"):
		return plugin.KindFamily, nil
	case in.Dir != "" && fileExists(filepath.Join(in.Dir, "ui", "index.html")) && !doc.has("binary"):
		return plugin.KindUI, nil
	case in.Dir != "" && fileExists(filepath.Join(in.Dir, "plugin.go")):
		return plugin.KindBuiltin, nil
	}
	return "", fmt.Errorf("contract: cannot tell what kind of package this is; pass one of %s", strings.Join(plugin.Kinds(), ", "))
}

// migrateVersion pads a v1 version to semver. "1" and "1.2" were legal there
// and are not here; anything already three-part, or anything unrecognised, is
// left for the verifier to judge.
func migrateVersion(doc *jsonObject) {
	v, ok := doc.str("version")
	if !ok {
		return
	}
	switch v = strings.TrimSpace(v); {
	case versionMajorOnly.MatchString(v):
		doc.set("version", mustRaw(v+".0.0"))
	case versionMajorMin.MatchString(v):
		doc.set("version", mustRaw(v+".0"))
	}
}

// migratePublisher turns the deprecated string form into an object, and fills
// an absent one only from an explicit decision.
func migratePublisher(doc *jsonObject, in MigrateInput) error {
	type publisher struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if name, ok := doc.str("publisher"); ok {
		name = strings.TrimSpace(name)
		doc.set("publisher", mustRaw(publisher{ID: name, Name: name}))
		return nil
	}
	if doc.has("publisher") {
		return nil // already an object; the verifier judges its fields
	}
	switch {
	case strings.TrimSpace(in.PublisherID) != "":
		id := strings.TrimSpace(in.PublisherID)
		doc.set("publisher", mustRaw(publisher{ID: id, Name: id}))
	case in.FirstParty:
		doc.set("publisher", mustRaw(publisher{ID: "bytedesk", Name: "ByteDesk"}))
	default:
		return fmt.Errorf("contract: this manifest has no publisher; supply one, or declare the package first-party")
	}
	return nil
}

// migrateIdentity builds the human half of the manifest. Every field falls
// back to something that is true — the manifest's own nav label, the README
// the package ships, the files on disk — and never to invented text. A
// description that stays empty is left empty on purpose: the verifier refuses
// it (BDP2213) and the author writes one.
func migrateIdentity(doc *jsonObject, id string, in MigrateInput) {
	identity := &jsonObject{vals: map[string]json.RawMessage{}}
	if raw, ok := doc.get("identity"); ok {
		if existing, err := decodeObject(raw); err == nil {
			identity = existing
		}
	}
	if _, ok := identity.str("displayName"); !ok {
		identity.set("displayName", mustRaw(displayName(doc, id, in)))
	}
	if _, ok := identity.str("description"); !ok {
		identity.set("description", mustRaw(description(in)))
	}
	if home, ok := doc.str("homepage"); ok {
		if _, already := identity.str("homepage"); !already {
			identity.set("homepage", mustRaw(strings.TrimSpace(home)))
		}
	}
	doc.delete("homepage")
	if in.Dir != "" {
		if _, ok := identity.str("readme"); !ok && fileExists(filepath.Join(in.Dir, "README.md")) {
			identity.set("readme", mustRaw("README.md"))
		}
		if _, ok := identity.str("changelog"); !ok && fileExists(filepath.Join(in.Dir, "CHANGELOG.md")) {
			identity.set("changelog", mustRaw("CHANGELOG.md"))
		}
		if !identity.has("images") {
			for _, ext := range []string{".svg", ".png", ".webp"} {
				rel := "images/icon" + ext
				if fileExists(filepath.Join(in.Dir, filepath.FromSlash(rel))) {
					identity.set("images", mustRaw(map[string]string{"icon": rel}))
					break
				}
			}
		}
	}
	doc.insertAfter("version", "identity", mustRaw(identity))
}

// displayName: the caller's, then the manifest's own first nav label, then the
// id. The id is a fallback that verifies with a warning (BDP2212) rather than
// a silent pass, because an id is an address and not a name.
func displayName(doc *jsonObject, id string, in MigrateInput) string {
	if name := strings.TrimSpace(in.DisplayName); name != "" {
		return name
	}
	if raw, ok := doc.get("nav"); ok {
		var nav []struct {
			Label string `json:"label"`
		}
		if json.Unmarshal(raw, &nav) == nil && len(nav) > 0 && strings.TrimSpace(nav[0].Label) != "" {
			return strings.TrimSpace(nav[0].Label)
		}
	}
	return id
}

// description: the caller's, then the README's first paragraph, then empty.
func description(in MigrateInput) string {
	if text := strings.TrimSpace(in.Description); text != "" {
		return text
	}
	if in.Dir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(in.Dir, "README.md"))
	if err != nil {
		return ""
	}
	return firstParagraph(string(raw))
}

// firstParagraph is the first run of prose in a README: headings, badges and
// blank lines before it are skipped, and the run ends at the next blank line
// or the next heading.
func firstParagraph(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if len(lines) == 0 {
			if line == "" || strings.HasPrefix(line, "#") || readmeBadge.MatchString(line) {
				continue
			}
			lines = append(lines, line)
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, " ")
}

// migrateProtocol moves a v1 protocol major onto this schema. Only the exact
// v1 value is rewritten; any other number is a claim the verifier should
// refuse rather than a spelling this tool corrects.
func migrateProtocol(doc *jsonObject) {
	raw, ok := doc.get("protocol")
	if !ok {
		return
	}
	protocol, err := decodeObject(raw)
	if err != nil {
		return
	}
	var major int
	if m, ok := protocol.get("major"); !ok || json.Unmarshal(m, &major) != nil || major != 1 {
		return
	}
	protocol.set("major", mustRaw(plugin.ProtocolMajor))
	doc.set("protocol", mustRaw(protocol))
}

// migrateImplements drops the "host." prefix v1 wrote on point names that do
// not carry it in v2 ("host.files.s3" -> "files.s3"). A name that IS a host
// point in v2 (host.settings.section, host.session.backend) is left alone,
// which is why the rule asks the registry rather than stripping the prefix.
func migrateImplements(doc *jsonObject) {
	raw, ok := doc.get("implements")
	if !ok {
		return
	}
	var elems []json.RawMessage
	if json.Unmarshal(raw, &elems) != nil {
		return
	}
	changed := false
	for i, elem := range elems {
		provider, err := decodeObject(elem)
		if err != nil {
			continue
		}
		point, ok := provider.str("point")
		if !ok || plugin.IsKnownPoint(point) {
			continue
		}
		bare := strings.TrimPrefix(point, plugin.HostPointNamespace)
		if bare == point || !plugin.IsKnownPoint(bare) {
			continue
		}
		provider.set("point", mustRaw(bare))
		elems[i] = mustRaw(provider)
		changed = true
	}
	if changed {
		doc.set("implements", mustRaw(elems))
	}
}

// migratePermissions drops what a plugin already has. Its own namespace is
// implicit in v2, so a v1 manifest that listed "event.<id>.job" was writing
// down a grant it holds by being itself, and listing it now is refused
// (BDP2114). An entry the bus grammar cannot parse is NOT rewritten: the
// subject is already in use somewhere, so only its author can rename it.
func migratePermissions(doc *jsonObject, id string) {
	raw, ok := doc.get("permissions")
	if !ok {
		return
	}
	permissions, err := decodeObject(raw)
	if err != nil {
		return
	}
	own := plugin.OwnNamespace(id)
	granted := append(append(append([]bus.Pattern{}, own.Publish...), own.Subscribe...), own.Serves...)
	for _, verb := range []string{"publish", "subscribe", "request"} {
		listRaw, ok := permissions.get(verb)
		if !ok {
			continue
		}
		var list []string
		if json.Unmarshal(listRaw, &list) != nil {
			continue
		}
		kept := make([]string, 0, len(list))
		for _, entry := range list {
			if coveredByOwnNamespace(granted, entry) {
				continue
			}
			kept = append(kept, entry)
		}
		if len(kept) == len(list) {
			continue
		}
		if len(kept) == 0 {
			permissions.delete(verb)
			continue
		}
		permissions.set(verb, mustRaw(kept))
	}
	if permissions.empty() {
		doc.delete("permissions")
		return
	}
	doc.set("permissions", mustRaw(permissions))
}

func coveredByOwnNamespace(granted []bus.Pattern, entry string) bool {
	pattern := bus.Pattern(entry)
	if _, err := bus.ParsePattern(entry); err != nil {
		return false
	}
	for _, g := range granted {
		if g.Covers(pattern) {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
