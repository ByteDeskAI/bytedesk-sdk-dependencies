package plugin

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// Validate is the author-time and pack-time gate for a v2 manifest.
//
// It refuses; it never repairs. Every rule below could in principle be
// "corrected" — drop the reserved token, trim the pattern down to the part that
// is allowed, ignore the unknown capability. None of those are done, anywhere,
// because a plugin that asked for a family and silently received a slice of it
// has no way to notice, and the operator who approved the manifest approved the
// text they read, not the host's improvement on it.
//
// The error returned is the first error-severity Diagnostic, in the order the
// rules run, so a caller that only ever read the error sees what it always
// saw. Diagnostics returns the whole list.
func Validate(m Manifest) error { return FirstError(Diagnostics(m, true)) }

// ValidateDiscover is Validate with the version requirement lifted, for reading
// a manifest off disk before a version has been assigned.
//
// The two entry points exist because they answer different questions. Validate
// asks "may this be published?", where an unversioned artifact is unresolvable
// and a missing version is the author's bug. ValidateDiscover asks "may this be
// loaded?", and at that point the version is the loader's business, not the
// manifest's — refusing there would strand a plugin the host installed itself
// over a field nothing had yet filled in. The version check is the ONLY
// difference; every authority rule below applies identically, because a
// manifest read off disk is exactly the one that gets enabled.
func ValidateDiscover(m Manifest) error { return FirstError(Diagnostics(m, false)) }

// Diagnostics runs every semantic rule (the BDP2xxx band), plus the
// deprecations a decoded manifest can see on its own (BDP5003), and returns
// all of its findings, in rule order. The deprecations that need the raw
// document — a string publisher, a top-level homepage — belong to the contract
// package, because by the time a Manifest exists both have been decoded away.
//
// It is a function over a Manifest for the same reason Validate is: a
// validator that is a method invites a caller to believe the manifest
// validated itself. Ordering is the rules' own; callers that need a stable
// wire order call SortDiagnostics.
func Diagnostics(m Manifest, requireVersion bool) []Diagnostic {
	var c collector
	id := strings.TrimSpace(m.ID)
	if id == "" {
		c.add("BDP2001", "id")
	} else if _, ok := idSegment("plugin id", id); !ok {
		// idSegment's message duplicates BDP2002's own template, and passing
		// it as an argument to a template with no verb rendered a Go
		// formatting artifact into author-facing output — see
		// TestNoDiagnosticRendersAFormattingArtifact.
		c.add("BDP2002", "id")
	}
	if requireVersion && strings.TrimSpace(m.Version) == "" {
		c.add("BDP2003", "version")
	}
	if m.Pricing != nil {
		model := strings.ToLower(strings.TrimSpace(m.Pricing.Model))
		if model != "free" && model != "trial" && model != "paid" {
			c.add("BDP2010", "pricing.model")
		}
		if (model == "paid" || model == "trial") && strings.TrimSpace(m.Pricing.SKU) == "" {
			c.add("BDP2011", "pricing.sku", model)
		}
		if model == "trial" && m.Pricing.TrialDays < 0 {
			c.add("BDP2012", "pricing.trialDays")
		}
	}
	if role := strings.ToLower(strings.TrimSpace(m.Role)); role != "" && role != RoleSystem && role != RoleExtension {
		c.add("BDP2020", "role")
	}
	for i, req := range m.Requires {
		path := fmt.Sprintf("requires[%d].id", i)
		rid := strings.TrimSpace(req.ID)
		if rid == "" {
			c.add("BDP2030", path)
			continue
		}
		if _, ok := idSegment("requires.id", rid); !ok {
			c.add("BDP2031", path)
		}
		if rid == id {
			c.add("BDP2032", path)
		}
	}
	if m.Spawn {
		bin := strings.TrimSpace(m.Binary)
		if bin == "" || strings.ContainsAny(bin, `/\`) || strings.Contains(bin, "..") {
			c.add("BDP2040", "binary")
		}
	}
	if sock := strings.TrimSpace(m.Socket); sock != "" {
		if strings.ContainsAny(sock, `/\`) || strings.Contains(sock, "..") {
			c.add("BDP2041", "socket")
		}
	}
	for i, t := range m.Targets {
		if t = strings.ToLower(strings.TrimSpace(t)); t != TargetGateway && t != TargetVault {
			c.add("BDP2021", fmt.Sprintf("targets[%d]", i), t)
		}
	}
	for i, point := range m.Extends {
		if err := ValidateExtendsName(m.Publisher, strings.TrimSpace(point.Name)); err != nil {
			c.add("BDP2050", fmt.Sprintf("extends[%d].name", i), err.Error())
		}
	}
	for i, impl := range m.Implements {
		validatePointName(&c, fmt.Sprintf("implements[%d].point", i), "implements", m.Publisher, impl.Point)
	}
	validateKind(&c, m)
	validateIdentity(&c, m)
	validatePublisher(&c, m)
	validateStatic(&c, m)
	validateAliases(&c, m)
	validateDocumentPaths(&c, m)
	validatePublicRoutes(&c, m)
	validateConfig(&c, m.Config)
	validateProtocol(&c, m.Protocol)
	validateSubjectPatterns(&c, m)
	validateServes(&c, m)
	validateAssets(&c, m)
	validateNeeds(&c, m)
	validateCapabilities(&c, m)
	validateUI(&c, m)
	validateProjectContributions(&c, m)
	return c.list
}

// validateKind checks the closed kind vocabulary and the two places where a
// manifest can say the same thing twice and disagree with itself.
//
// Spawn is the deprecated spelling of "kind": "process" and Family is the
// evidence for "kind": "family". Where both are written, they must agree: a
// verifier that picked one would be choosing which half of the manifest the
// operator actually approved. Where only the old spelling is written, the
// manifest is accepted with a deprecation warning, which is what makes the
// window a window.
func validateKind(c *collector, m Manifest) {
	kind := strings.ToLower(strings.TrimSpace(m.Kind))
	if kind != "" && !slices.Contains(Kinds(), kind) {
		c.add("BDP2200", "kind", m.Kind, strings.Join(Kinds(), ", "))
		return
	}
	if m.Spawn {
		c.add("BDP5003", "spawn")
	}
	if kind == "" {
		return
	}
	if m.Spawn && kind != KindProcess {
		c.add("BDP2201", "kind", m.Kind)
	}
	if kind == KindFamily && m.Family == nil {
		c.add("BDP2202", "kind")
	}
	if kind != KindFamily && m.Family != nil {
		c.add("BDP2203", "family", m.Kind)
	}
	// binary and socket describe a process the host spawns and talks to. On
	// any other kind they are inert text that reads like a capability, which
	// is the shape an operator approves by mistake.
	//
	// Skipped when spawn already disagreed with kind: the manifest has one
	// problem, and repeating it per field buries the sentence that says which
	// two declarations contradict each other.
	if kind != KindProcess && !m.Spawn {
		if strings.TrimSpace(m.Binary) != "" {
			c.add("BDP2204", "binary", "binary", kind)
		}
		if strings.TrimSpace(m.Socket) != "" {
			c.add("BDP2204", "socket", "socket", kind)
		}
	}
}

// validateIdentity requires the human half of every manifest, for every kind.
//
// Nothing here is host-defaultable. An identity the host filled in would make
// the store row, the settings list and the consent prompt all render text
// nobody wrote, and the gate that checks identity exists would pass for every
// package whether or not its author ever described it.
func validateIdentity(c *collector, m Manifest) {
	if m.Identity == nil {
		c.add("BDP2210", "identity")
		return
	}
	name := strings.TrimSpace(m.Identity.DisplayName)
	switch {
	case name == "":
		c.add("BDP2211", "identity.displayName")
	case name == strings.TrimSpace(m.ID):
		// A warning, not a refusal: it is honest, just useless to a reader.
		c.add("BDP2212", "identity.displayName", name)
	}
	if strings.TrimSpace(m.Identity.Description) == "" {
		c.add("BDP2213", "identity.description")
	}
}

// validatePublisher requires a publisher for everything the host did not
// compile in, and checks that the two identifiers a registry looks up are the
// shape it can look up.
//
// Neither check is a trust decision. A publisher block proves nothing; the
// signature over the package and the key registry do. What is refused here is
// a package that could not be attributed even if it were signed.
func validatePublisher(c *collector, m Manifest) {
	kind := m.KindOrInferred()
	if m.Publisher == nil {
		// An unrecognised kind has already been refused by BDP2200; asking it
		// for a publisher as well would report the same mistake twice.
		if kind != KindBuiltin && slices.Contains(Kinds(), kind) {
			c.add("BDP2220", "publisher", kind)
		}
		return
	}
	if id := strings.TrimSpace(m.Publisher.ID); !validPublisherID(id) {
		c.add("BDP2221", "publisher.id", m.Publisher.ID)
	}
	if kid := strings.TrimSpace(m.Publisher.KID); kid != "" && !validKID(kid) {
		c.add("BDP2222", "publisher.kid", m.Publisher.KID)
	}
}

// validateStatic checks the directories a host is asked to serve.
//
// Both halves are one safe path segment. Dir addresses the extracted package
// and Mount addresses the URL space under /p/<id>/, so ".." or a leading
// slash in either is a request to serve somewhere the package does not own.
// Refused, never normalised: a mount the verifier had to correct is not the
// one the author read.
func validateStatic(c *collector, m Manifest) {
	seenDir, seenMount := map[string]bool{}, map[string]bool{}
	for i, s := range m.Static {
		dir, mount := strings.TrimSpace(s.Dir), strings.TrimSpace(s.Mount)
		if !safeSegment(dir) {
			c.add("BDP2230", fmt.Sprintf("static[%d].dir", i), s.Dir)
		} else if seenDir[dir] {
			c.add("BDP2232", fmt.Sprintf("static[%d].dir", i), "static[].dir", dir)
		}
		seenDir[dir] = true
		if !safeSegment(mount) {
			c.add("BDP2231", fmt.Sprintf("static[%d].mount", i), s.Mount)
		} else if seenMount[mount] {
			c.add("BDP2232", fmt.Sprintf("static[%d].mount", i), "static[].mount", mount)
		}
		seenMount[mount] = true
	}
}

// safeSegment reports whether s is exactly one path segment that addresses
// something inside the tree it is relative to.
func safeSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`) && !strings.Contains(s, "..")
}

// validateAliases checks the pretty paths the host's reverse proxy maps.
//
// An alias is an absolute path of non-empty segments that the host does not
// already serve itself. Collisions BETWEEN plugins are the host's refusal, not
// this one: only the host sees two manifests at once.
func validateAliases(c *collector, m Manifest) {
	seen := map[string]bool{}
	for i, raw := range m.Aliases {
		path := fmt.Sprintf("aliases[%d]", i)
		alias := strings.TrimSpace(raw)
		if !absolutePath(alias) {
			c.add("BDP2240", path, raw)
			continue
		}
		normal := "/" + strings.Trim(alias, "/")
		if reserved := reservedAlias(normal); reserved != "" {
			c.add("BDP2241", path, raw, reserved)
			continue
		}
		if seen[normal] {
			c.add("BDP2232", path, "aliases", normal)
		}
		seen[normal] = true
	}
}

// absolutePath reports whether s is a leading slash followed by at least one
// non-empty segment, with no "." or ".." anywhere. A trailing slash is allowed
// because an alias names a prefix.
func absolutePath(s string) bool {
	if !strings.HasPrefix(s, "/") || strings.Contains(s, `\`) {
		return false
	}
	segments := strings.Split(strings.Trim(s, "/"), "/")
	if len(segments) == 0 || segments[0] == "" {
		return false
	}
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// reservedAlias returns the reserved prefix alias claims, or "".
func reservedAlias(alias string) string {
	for _, prefix := range ReservedAliasPrefixes() {
		if alias == prefix || strings.HasPrefix(alias, prefix+"/") {
			return prefix
		}
	}
	return ""
}

// validateSubjectPatterns is the v2 rule v1 could not express. v1's Permissions
// were exact names, so "may this plugin see the whole event.files family" had no
// spelling and every author wrote out the names they happened to know about.
//
// Four refusals, in the order they are cheapest to explain to the author:
//
//  1. It must parse under bus's grammar, which is the same parser that matches
//     at delivery time.
//  2. It must not contain a reserved token. The parser ACCEPTS these on purpose
//     — the substrate has to be able to name "$SYS.>" to deny it — so the
//     manifest is where they are refused.
//  3. It must not restate the plugin's own namespace, which is implicit.
//  4. It must not overlap a permanently ineligible family. Overlap, not
//     containment: "cmd.>" reaches "cmd.auth.>" and is refused whole rather
//     than quietly reduced to the rest of "cmd.".
func validateSubjectPatterns(c *collector, m Manifest) {
	own := ownNamespacePatterns(strings.TrimSpace(m.ID))
	denied := PermanentlyIneligible()
	// own is implicit and never appears in a declared Permissions list, so the
	// loop below -- which walks Publish/Subscribe/Request -- cannot see it and
	// cannot catch an id whose OWN NAMESPACE reaches an ineligible family. A
	// plugin id of "auth" implicitly Serves cmd.auth.>, exactly a permanently
	// ineligible family, entirely without declaring any permission at all.
	// Checked here, before the declared-permission loop, so the manifest is
	// refused by the id that caused it rather than passing validation on the
	// technicality that nothing was written down.
	for _, g := range own {
		for _, d := range denied {
			if patternsOverlap(d, g) {
				c.add("BDP2004", "id", m.ID, string(g), string(d))
				break
			}
		}
	}
	for _, list := range []struct {
		label    string
		patterns []bus.Pattern
	}{
		{"permissions.publish", m.Permissions.Publish},
		{"permissions.subscribe", m.Permissions.Subscribe},
		{"permissions.request", m.Permissions.Request},
	} {
		seen := map[bus.Pattern]bool{}
		for i, p := range list.patterns {
			path := fmt.Sprintf("%s[%d]", list.label, i)
			if seen[p] {
				c.add("BDP2110", path, list.label, string(p))
			}
			seen[p] = true
			if _, err := bus.ParsePattern(string(p)); err != nil {
				c.add("BDP2111", path, list.label, err.Error())
				continue
			}
			if refuseReservedTokens(c, path, list.label, p) {
				continue
			}
			for _, g := range own {
				if g.Covers(p) {
					c.add("BDP2114", path, list.label, string(p))
				}
			}
			// One finding per pattern: the first family it reaches is the
			// evidence, and "cmd.>" reaching all nine is not nine defects.
			for _, d := range denied {
				if patternsOverlap(d, p) {
					c.add("BDP2115", path, list.label, string(p), string(d))
					break
				}
			}
		}
	}
}

// refuseReservedTokens refuses what the grammar lets through on purpose.
// bus.IsReservedToken covers the substrate's own "$"-prefixed names and the
// inbox root; the inter-gateway mesh prefix is this package's, because the bus
// does not know gateways exist. It reports whether anything was refused.
func refuseReservedTokens(c *collector, path, label string, p bus.Pattern) bool {
	for i, tok := range p.Tokens() {
		if bus.IsReservedToken(tok) {
			c.add("BDP2112", path, label, string(p), tok)
			return true
		}
		if i == 0 && tok == MeshPrefix {
			c.add("BDP2113", path, label, string(p), MeshPrefix+".")
			return true
		}
	}
	return false
}

// patternsOverlap reports whether any concrete subject matches both patterns.
//
// It is deliberately not an intersection. Overlap is a yes/no question a
// refusal can be written against; an intersection is a new, smaller pattern
// nobody declared and nobody approved, handed to a plugin that will behave as
// though it got what it asked for.
func patternsOverlap(a, b bus.Pattern) bool {
	at, bt := a.Tokens(), b.Tokens()
	for i := 0; ; i++ {
		if i >= len(at) || i >= len(bt) {
			return len(at) == len(bt)
		}
		x, y := at[i], bt[i]
		if x == ">" || y == ">" {
			// ">" is one-or-more trailing tokens, and the other side still has
			// at least one token left, so a common subject exists.
			return true
		}
		if x == "*" || y == "*" {
			continue
		}
		if x != y {
			return false
		}
	}
}

// validateServes checks the export surface. An endpoint outside the plugin's own
// "svc.<id>." / "cmd.<id>." trees is the one that matters: mounting elsewhere is
// a plugin answering calls addressed to somebody it is not, and the caller has
// no way to tell.
func validateServes(c *collector, m Manifest) {
	id := strings.TrimSpace(m.ID)
	prefixes := []string{"svc." + id + ".", "cmd." + id + "."}
	services := map[string]bool{}
	for si, svc := range m.Serves {
		name := strings.TrimSpace(svc.Name)
		if name == "" {
			c.add("BDP2120", fmt.Sprintf("serves[%d].name", si))
			continue
		}
		if services[name] {
			c.add("BDP2121", fmt.Sprintf("serves[%d].name", si), name)
		}
		services[name] = true
		endpoints := map[string]bool{}
		for ei, ep := range svc.Endpoints {
			epPath := fmt.Sprintf("serves[%d].endpoints[%d]", si, ei)
			epName := strings.TrimSpace(ep.Name)
			if epName == "" {
				c.add("BDP2122", epPath+".name", name)
				continue
			}
			if endpoints[epName] {
				c.add("BDP2123", epPath+".name", name, epName)
			}
			endpoints[epName] = true
			label := "serves " + name + "." + epName
			if _, err := bus.ParseSubject(string(ep.Subject)); err != nil {
				c.add("BDP2111", epPath+".subject", label, err.Error())
			} else if !refuseReservedTokens(c, epPath+".subject", label, bus.Pattern(ep.Subject)) {
				inOwn := false
				for _, prefix := range prefixes {
					if strings.HasPrefix(string(ep.Subject), prefix) {
						inOwn = true
						break
					}
				}
				if !inOwn {
					c.add("BDP2124", epPath+".subject", name, epName, string(ep.Subject), prefixes[0], prefixes[1])
				}
			}
			if strings.TrimSpace(ep.Point) == "" {
				continue
			}
			validatePointName(c, epPath+".point", label, m.Publisher, ep.Point)
		}
	}
}

// validatePointName resolves one extension point name a manifest registers
// INTO, as opposed to one it owns.
//
// A known point passes. A near miss is answered with the suggestion, because
// "settings.section" is the mistake every author makes once and an error that
// only says "unknown" makes them read the source to find the prefix. An unknown
// name under the host namespace is refused outright: the host's points are
// enumerable, so a name that is not among them is not one — "host.auth.method"
// is a command, not a seam. A well formed name in the publisher's own namespace
// is allowed through, since a peer plugin's point cannot be enumerated here.
func validatePointName(c *collector, path, label string, publisher *Publisher, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		c.add("BDP2051", path, label)
		return
	}
	if IsKnownPoint(name) {
		return
	}
	if suggestion, ok := SuggestPoint(name); ok {
		c.add("BDP2052", path, label, name, string(suggestion))
		return
	}
	if err := ValidateExtendsName(publisher, name); err != nil {
		c.add("BDP2053", path, label, err.Error())
		return
	}
	if strings.HasPrefix(name, HostPointNamespace) {
		c.add("BDP2054", path, label, name, strings.Join(knownPointNames(), ", "))
	}
}

func knownPointNames() []string {
	points := KnownPoints()
	out := make([]string, 0, len(points))
	for _, p := range points {
		out = append(out, string(p))
	}
	slices.Sort(out)
	return out
}

// validateAssets checks the provisioned storage a plugin asks the host to
// create. A stream without both limits is refused: an unbounded stream is how a
// gateway fills a disk quietly, and the first symptom is the whole host out of
// space rather than one plugin failing.
func validateAssets(c *collector, m Manifest) {
	streams := map[string]bool{}
	for i, s := range m.Streams {
		path := fmt.Sprintf("streams[%d]", i)
		name := strings.TrimSpace(s.Name)
		if name == "" {
			c.add("BDP2130", path+".name")
			continue
		}
		if streams[name] {
			c.add("BDP2131", path+".name", name)
		}
		streams[name] = true
		if s.MaxBytes <= 0 {
			c.add("BDP2132", path+".maxBytes", name)
		}
		if s.MaxAgeSeconds <= 0 {
			c.add("BDP2133", path+".maxAgeSeconds", name)
		}
		if s.MaxMsgs < 0 {
			c.add("BDP2134", path+".maxMsgs", name)
		}
		for j, subject := range s.Subjects {
			sp := fmt.Sprintf("%s.subjects[%d]", path, j)
			if _, err := bus.ParsePattern(string(subject)); err != nil {
				c.add("BDP2111", sp, "streams "+name, err.Error())
				continue
			}
			refuseReservedTokens(c, sp, "streams "+name, subject)
		}
	}
	kv := map[string]bool{}
	for i, b := range m.KV {
		path := fmt.Sprintf("kv[%d]", i)
		name := strings.TrimSpace(b.Name)
		if name == "" {
			c.add("BDP2135", path+".name")
			continue
		}
		if kv[name] {
			c.add("BDP2136", path+".name", name)
		}
		kv[name] = true
		if b.MaxBytes < 0 || b.History < 0 {
			c.add("BDP2137", path, name)
		}
	}
	objects := map[string]bool{}
	for i, b := range m.Objects {
		path := fmt.Sprintf("objects[%d]", i)
		name := strings.TrimSpace(b.Name)
		if name == "" {
			c.add("BDP2138", path+".name")
			continue
		}
		if objects[name] {
			c.add("BDP2139", path+".name", name)
		}
		objects[name] = true
		if b.MaxBytes < 0 {
			c.add("BDP2140", path+".maxBytes", name)
		}
	}
}

func validateCapabilities(c *collector, m Manifest) {
	seen := map[string]bool{}
	for i, id := range m.Capabilities {
		path := fmt.Sprintf("capabilities[%d]", i)
		id = strings.TrimSpace(id)
		if id == "" {
			c.add("BDP2172", path)
			continue
		}
		if seen[id] {
			c.add("BDP2173", path, id)
		}
		seen[id] = true
		if !knownCapability(id) {
			c.add("BDP2174", path, id, strings.Join(CapabilityIDs(), " "))
		}
	}
}

func validateNeeds(c *collector, m Manifest) {
	vocabulary := bus.CapabilityNames()
	seen := map[string]bool{}
	for i, need := range m.Needs {
		path := fmt.Sprintf("needs[%d]", i)
		need = strings.TrimSpace(need)
		if need == "" {
			c.add("BDP2150", path)
			continue
		}
		if seen[need] {
			c.add("BDP2151", path, need)
		}
		seen[need] = true
		if !slices.Contains(vocabulary, need) {
			c.add("BDP2152", path, need, strings.Join(vocabulary, " "))
		}
	}
}

// validateProtocol keeps v1's exact-name rules for the fields that are still
// exact names. Feature and hook identifiers are not subjects and never were.
func validateProtocol(c *collector, p *ProtocolRequirements) {
	if p == nil {
		return
	}
	if p.Major != 0 && p.Major != ProtocolMajor {
		c.add("BDP2100", "protocol.major", p.Major, ProtocolMajor)
	}
	if p.Major == 0 && len(p.Required) != 0 {
		c.add("BDP2101", "protocol.required")
	}
	if msg, ok := exactNames("protocol.required", p.Required); !ok {
		c.add("BDP2102", "protocol.required", msg)
	}
	if p.Major == 0 && len(p.Hooks) != 0 {
		c.add("BDP2103", "protocol.hooks")
	}
	if msg, ok := exactNames("protocol.hooks", p.Hooks); !ok {
		c.add("BDP2104", "protocol.hooks", msg)
	}
	for i, hook := range p.Hooks {
		if !slices.Contains(lifecycleHookVocabulary, hook) {
			c.add("BDP2105", fmt.Sprintf("protocol.hooks[%d]", i), hook)
		}
	}
}

// lifecycleHookVocabulary is the closed set of hooks a manifest may advertise.
// Every hook runs on entry or reports health; there is deliberately no stop
// hook, so a plugin cannot declare an exit veto.
var lifecycleHookVocabulary = []string{"activation.check", "ready", "health"}

func validateUI(c *collector, m Manifest) {
	seen := map[string]bool{}
	for i, item := range m.UI {
		path := fmt.Sprintf("ui[%d]", i)
		if msg, ok := idSegment("ui.id", item.ID); !ok {
			c.add("BDP2160", path+".id", msg)
			continue
		}
		if seen[item.ID] {
			c.add("BDP2161", path+".id", item.ID)
		}
		seen[item.ID] = true
		// A vocabulary addition without its eligibility policy is not valid.
		if _, ok := ContributionRoleFor(item.Slot); !ok {
			c.add("BDP2162", path+".slot", item.Slot)
			continue
		}
		switch item.Slot {
		case SlotCommand:
			if item.Command == "" || item.PanelID != "" {
				c.add("BDP2163", path+".command")
			} else if msg, ok := exactNames("ui.command", []string{item.Command}); !ok {
				c.add("BDP2164", path+".command", msg)
			}
		default:
			if item.PanelID == "" || item.Command != "" {
				c.add("BDP2165", path+".panelId", item.Slot)
				break
			}
			owned := false
			for _, panel := range m.Panels {
				if panel.ID == item.PanelID {
					owned = true
					break
				}
			}
			if !owned {
				c.add("BDP2166", path+".panelId", item.PanelID)
			}
		}
		validateBindings(c, path, item.ID, item.Bindings)
	}
}

func validateProjectContributions(c *collector, m Manifest) {
	panels := make(map[string]struct{}, len(m.Panels))
	for _, panel := range m.Panels {
		panels[panel.ID] = struct{}{}
	}
	views := make(map[string]struct{}, len(m.ProjectViews))
	for i, view := range m.ProjectViews {
		path := fmt.Sprintf("projectViews[%d]", i)
		if msg, ok := idSegment("projectViews.id", view.ID); !ok {
			c.add("BDP2190", path+".id", msg)
			continue
		}
		if _, exists := views[view.ID]; exists {
			c.add("BDP2191", path+".id", view.ID)
		}
		views[view.ID] = struct{}{}
		if strings.TrimSpace(view.Label) == "" {
			c.add("BDP2192", path+".label")
		}
		if strings.TrimSpace(view.Icon) == "" {
			c.add("BDP2193", path+".icon")
		}
		if _, owned := panels[view.PanelID]; !owned || view.PanelID == "" {
			c.add("BDP2194", path+".panelId", view.PanelID)
		}
	}
	actions := make(map[string]struct{}, len(m.DirectoryContextActions))
	for i, action := range m.DirectoryContextActions {
		path := fmt.Sprintf("directoryContextActions[%d]", i)
		if msg, ok := idSegment("directoryContextActions.id", action.ID); !ok {
			c.add("BDP2195", path+".id", msg)
			continue
		}
		if _, exists := actions[action.ID]; exists {
			c.add("BDP2196", path+".id", action.ID)
		}
		actions[action.ID] = struct{}{}
		if strings.TrimSpace(action.Label) == "" {
			c.add("BDP2197", path+".label")
		}
		if _, owned := panels[action.WizardPanelID]; !owned || action.WizardPanelID == "" {
			c.add("BDP2198", path+".wizardPanelId", action.WizardPanelID)
		}
	}
}

// validateBindings checks the shape only. Whether the plugin may subscribe to
// the event it names is the host's question, asked at registration, and no
// manifest can answer it for itself.
func validateBindings(c *collector, itemPath, contributionID string, bindings []UIBinding) {
	seen := map[string]bool{}
	for i, bind := range bindings {
		path := fmt.Sprintf("%s.bindings[%d]", itemPath, i)
		switch bind.Kind {
		case BindCount, BindBadge, BindLiveness, BindToggle:
		default:
			c.add("BDP2167", path+".kind", contributionID, bind.Kind)
			continue
		}
		if seen[bind.Kind] {
			c.add("BDP2168", path+".kind", contributionID, bind.Kind)
		}
		seen[bind.Kind] = true
		if msg, ok := exactNames("ui.bindings.event", []string{bind.Event}); !ok {
			c.add("BDP2169", path+".event", msg)
		}
		// A field selects one value out of the payload, so it is one JSON key,
		// not a path. A path would be a query language nobody asked for, and the
		// host would have to evaluate it against a payload a plugin controls.
		if bind.Field != "" {
			if msg, ok := idSegment("ui.bindings.field", bind.Field); !ok {
				c.add("BDP2170", path+".field", msg)
			} else if strings.ContainsAny(bind.Field, ".[]") {
				c.add("BDP2171", path+".field", contributionID, bind.Field)
			}
		}
	}
}

// validatePublicRoutes holds the one rule that makes the field safe to trust:
// a plugin may only waive authentication on a route it already declared.
//
// Without it, a manifest could name another plugin's route, or a host route, as
// public and open a hole in a surface it does not own.
func validatePublicRoutes(c *collector, m Manifest) {
	if len(m.PublicRoutes) == 0 {
		return
	}
	declared := make(map[string]bool, len(m.Routes))
	for _, route := range m.Routes {
		declared[strings.TrimSpace(route)] = true
	}
	seen := map[string]bool{}
	for i, route := range m.PublicRoutes {
		path := fmt.Sprintf("publicRoutes[%d]", i)
		route = strings.TrimSpace(route)
		if route == "" {
			c.add("BDP2070", path)
			continue
		}
		if seen[route] {
			c.add("BDP2071", path, route)
		}
		seen[route] = true
		if !declared[route] {
			c.add("BDP2072", path, route)
		}
	}
}

// ValidateDocumentPath checks a shell document pattern. Patterns contain literal
// ASCII segments, :name parameters, and an optional terminal *name matching one
// or more segments. Root, empty segments, trailing slashes, escaping, queries and
// fragments are forbidden. Host-reserved namespaces are a host admission policy.
func ValidateDocumentPath(pattern string) error {
	_, err := parseDocumentPath(pattern)
	return err
}

type documentSegment struct {
	value string
	kind  byte // literal (0), named parameter (:), or terminal catch-all (*)
}

func parseDocumentPath(pattern string) ([]documentSegment, error) {
	bad := func() ([]documentSegment, error) {
		return nil, fmt.Errorf("invalid document path %q", pattern)
	}
	if !strings.HasPrefix(pattern, "/") || pattern == "/" {
		return bad()
	}
	parts := strings.Split(pattern[1:], "/")
	segments := make([]documentSegment, 0, len(parts))
	names := map[string]bool{}
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return bad()
		}
		segment := documentSegment{value: part}
		if part[0] == ':' || part[0] == '*' {
			segment.kind, segment.value = part[0], part[1:]
			name := segment.value
			if name == "" || !documentLetter(name[0]) || names[name] || (segment.kind == '*' && i != len(parts)-1) {
				return bad()
			}
			for j := 1; j < len(name); j++ {
				if !documentLetter(name[j]) && !(name[j] >= '0' && name[j] <= '9') && name[j] != '_' {
					return bad()
				}
			}
			names[name] = true
		} else {
			for j := 0; j < len(part); j++ {
				c := part[j]
				if !documentLetter(c) && !(c >= '0' && c <= '9') && !strings.ContainsRune("._~-", rune(c)) {
					return bad()
				}
			}
		}
		segments = append(segments, segment)
	}
	return segments, nil
}

func documentLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// MatchDocumentPath matches an escaped URL pathname (Go URL.EscapedPath or
// browser location.pathname), never a whole URL. It decodes each segment exactly
// once and rejects malformed UTF-8, separators, controls and dot traversal.
// Returned values are data: consumers must not decode or clean them again.
// A catch-all joins validated segments with /. Invalid input never matches.
func MatchDocumentPath(pattern, escapedPath string) (map[string]string, bool) {
	segments, err := parseDocumentPath(pattern)
	if err != nil || !strings.HasPrefix(escapedPath, "/") || strings.ContainsAny(escapedPath, "?#\\") {
		return nil, false
	}
	parts := strings.Split(escapedPath[1:], "/")
	if len(parts) < len(segments) || (segments[len(segments)-1].kind != '*' && len(parts) != len(segments)) {
		return nil, false
	}
	for i, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil || decoded == "" || decoded == "." || decoded == ".." || !utf8.ValidString(decoded) ||
			strings.ContainsAny(decoded, `/\`) || strings.ContainsFunc(decoded, unicode.IsControl) {
			return nil, false
		}
		parts[i] = decoded
	}
	params := map[string]string{}
	for i, segment := range segments {
		switch segment.kind {
		case ':':
			params[segment.value] = parts[i]
		case '*':
			params[segment.value] = strings.Join(parts[i:], "/")
		default:
			if segment.value != parts[i] {
				return nil, false
			}
		}
	}
	return params, true
}

// DocumentPathsOverlap reports whether two valid patterns can match the same
// pathname. Invalid declarations return an error, never a false claim of safety.
func DocumentPathsOverlap(a, b string) (bool, error) {
	left, err := parseDocumentPath(a)
	if err != nil {
		return false, err
	}
	right, err := parseDocumentPath(b)
	if err != nil {
		return false, err
	}
	if left[len(left)-1].kind != '*' && len(left) < len(right) || right[len(right)-1].kind != '*' && len(right) < len(left) {
		return false, nil
	}
	for i := 0; i < len(left) && i < len(right); i++ {
		if left[i].kind == '*' || right[i].kind == '*' {
			return true, nil
		}
		if left[i].kind == 0 && right[i].kind == 0 && left[i].value != right[i].value {
			return false, nil
		}
	}
	return true, nil
}

func validateDocumentPaths(c *collector, m Manifest) {
	var claimed []string
	panelIDs := map[string]int{}
	for _, panel := range m.Panels {
		panelIDs[panel.ID]++
	}
	for i, panel := range m.Panels {
		if len(panel.DocumentPaths) == 0 {
			continue
		}
		path := fmt.Sprintf("panels[%d]", i)
		if m.Protocol == nil || m.Protocol.Major == 0 || !slices.Contains(m.Protocol.Required, FeatureDocumentPaths) {
			c.add("BDP2060", path+".documentPaths", FeatureDocumentPaths)
			// Without the negotiated feature nothing below is reachable at
			// runtime; one finding per manifest says it.
			return
		}
		if msg, ok := idSegment("panel.id", panel.ID); !ok {
			c.add("BDP2061", path+".id", msg)
			continue
		}
		if panel.ID != strings.TrimSpace(panel.ID) || panelIDs[panel.ID] != 1 || strings.TrimSpace(panel.URL) == "" {
			c.add("BDP2062", path)
			continue
		}
		for j, pattern := range panel.DocumentPaths {
			pp := fmt.Sprintf("%s.documentPaths[%d]", path, j)
			if err := ValidateDocumentPath(pattern); err != nil {
				c.add("BDP2063", pp, pattern)
				continue
			}
			for _, previous := range claimed {
				if overlap, _ := DocumentPathsOverlap(previous, pattern); overlap {
					c.add("BDP2064", pp, previous, pattern)
				}
			}
			claimed = append(claimed, pattern)
		}
	}
}

// validateConfig checks the declared sections: ids present and unique, and every
// field well formed with a key unique within its section.
func validateConfig(c *collector, cfg *Config) {
	if cfg == nil {
		return
	}
	sections := map[string]bool{}
	for i, s := range cfg.Sections {
		path := fmt.Sprintf("config.sections[%d]", i)
		id := strings.TrimSpace(s.ID)
		if id == "" || sections[id] {
			c.add("BDP2080", path+".id", s.ID)
			continue
		}
		sections[id] = true
		keys := map[string]bool{}
		for j, f := range s.Fields {
			fp := fmt.Sprintf("%s.fields[%d]", path, j)
			validateConfigField(c, fp, id, f)
			if keys[f.Key] {
				c.add("BDP2081", fp+".key", id, f.Key)
			}
			keys[f.Key] = true
		}
	}
}

func validateConfigField(c *collector, path, section string, f ConfigField) {
	if strings.TrimSpace(f.Key) == "" {
		c.add("BDP2082", path+".key", section)
		return
	}
	kinds := []string{ConfigKindBool, ConfigKindInt, ConfigKindString, ConfigKindStringList, ConfigKindEnum, ConfigKindSecret, ConfigKindProvider}
	if !slices.Contains(kinds, f.Kind) {
		c.add("BDP2083", path+".kind", section, f.Key, f.Kind)
		return
	}
	if f.Kind == ConfigKindProvider {
		parts := strings.Split(f.Point, ".")
		if len(parts) < 2 || (strings.HasPrefix(f.Point, HostPointNamespace) && len(parts) < 3) {
			c.add("BDP2084", path+".point", section, f.Key)
		} else {
			for _, part := range parts {
				if part == "" || strings.Trim(part, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
					c.add("BDP2085", path+".point", section, f.Key, f.Point)
					break
				}
			}
		}
		if msg, ok := exactNames("config provider requires", f.Requires); !ok {
			c.add("BDP2086", path+".requires", section, msg)
		}
		if f.Default != "" {
			if msg, ok := exactNames("config provider default", []string{f.Default}); !ok {
				c.add("BDP2086", path+".default", section, msg)
			}
		}
	} else if f.Point != "" || len(f.Requires) != 0 {
		c.add("BDP2087", path, section, f.Key)
	}
	if (f.Min != nil || f.Max != nil) && f.Kind != ConfigKindInt {
		c.add("BDP2088", path, section, f.Key)
	}
	if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
		c.add("BDP2089", path, section, f.Key, *f.Min, *f.Max)
	}
	if (len(f.Choices) > 0) != (f.Kind == ConfigKindEnum) || slices.Contains(f.Choices, "") {
		c.add("BDP2090", path+".choices", section, f.Key)
	}
	if f.Default == "" {
		return
	}
	switch f.Kind {
	case ConfigKindBool:
		if _, err := strconv.ParseBool(f.Default); err != nil {
			c.add("BDP2091", path+".default", section, f.Key, fmt.Sprintf("default %q is not a bool", f.Default))
		}
	case ConfigKindInt:
		n, err := strconv.Atoi(f.Default)
		if err != nil || (f.Min != nil && n < *f.Min) || (f.Max != nil && n > *f.Max) {
			c.add("BDP2091", path+".default", section, f.Key, fmt.Sprintf("default %q is not an int within bounds", f.Default))
		}
	case ConfigKindEnum:
		if !slices.Contains(f.Choices, f.Default) {
			c.add("BDP2091", path+".default", section, f.Key, fmt.Sprintf("default %q is not one of its choices", f.Default))
		}
	case ConfigKindStringList, ConfigKindSecret:
		c.add("BDP2091", path+".default", section, f.Key, fmt.Sprintf("a %s field takes no default", f.Kind))
	}
}

// idSegment reports whether s is one non-empty path segment. The message is the
// pre-diagnostic wording, returned rather than formatted so each caller keeps
// its own code.
func idSegment(what, s string) (string, bool) {
	if s = strings.TrimSpace(s); s == "" {
		return what + " required", false
	}
	if strings.ContainsAny(s, `/\ `) || strings.Contains(s, "..") {
		return what + " must be a single path segment", false
	}
	return "", true
}

// exactNames guards the fields that are still exact names rather than
// subjects: negotiated protocol features, lifecycle hooks, the command a UI
// contribution invokes and the event a binding follows.
func exactNames(label string, values []string) (string, bool) {
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "*?\r\n\t ") {
			return label + " must contain nonempty exact names", false
		}
		if seen[value] {
			return fmt.Sprintf("%s contains duplicate %q", label, value), false
		}
		seen[value] = true
	}
	return "", true
}
