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
func Validate(m Manifest) error { return validate(m, true) }

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
func ValidateDiscover(m Manifest) error { return validate(m, false) }

func validate(m Manifest, requireVersion bool) error {
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
	if role := strings.ToLower(strings.TrimSpace(m.Role)); role != "" && role != RoleSystem && role != RoleExtension {
		return fmt.Errorf("role must be system or extension")
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
		if bin == "" || strings.ContainsAny(bin, `/\`) || strings.Contains(bin, "..") {
			return fmt.Errorf("spawn binary must be a relative basename")
		}
	}
	if sock := strings.TrimSpace(m.Socket); sock != "" {
		if strings.ContainsAny(sock, `/\`) || strings.Contains(sock, "..") {
			return fmt.Errorf("socket must be a relative basename")
		}
	}
	for _, t := range m.Targets {
		if t = strings.ToLower(strings.TrimSpace(t)); t != TargetGateway && t != TargetVault {
			return fmt.Errorf("targets: unknown %q (gateway|vault)", t)
		}
	}
	for _, point := range m.Extends {
		if err := ValidateExtendsName(m.Publisher, strings.TrimSpace(point.Name)); err != nil {
			return err
		}
	}
	for _, impl := range m.Implements {
		if err := validatePointName("implements", m.Publisher, impl.Point); err != nil {
			return err
		}
	}
	if err := validateDocumentPaths(m); err != nil {
		return err
	}
	if err := validatePublicRoutes(m); err != nil {
		return err
	}
	if err := validateConfig(m.Config); err != nil {
		return err
	}
	if err := validateProtocol(m.Protocol); err != nil {
		return err
	}
	if err := validateSubjectPatterns(m); err != nil {
		return err
	}
	if err := validateServes(m); err != nil {
		return err
	}
	if err := validateAssets(m); err != nil {
		return err
	}
	if err := validateNeeds(m); err != nil {
		return err
	}
	return validateUI(m)
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
func validateSubjectPatterns(m Manifest) error {
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
				return fmt.Errorf("plugin id %q implicitly Serves %q, which reaches the permanently ineligible family %q; choose a different id", m.ID, string(g), string(d))
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
		for _, p := range list.patterns {
			if seen[p] {
				return fmt.Errorf("%s contains duplicate %q", list.label, string(p))
			}
			seen[p] = true
			if _, err := bus.ParsePattern(string(p)); err != nil {
				return fmt.Errorf("%s: %w", list.label, err)
			}
			if err := refuseReservedTokens(list.label, p); err != nil {
				return err
			}
			for _, g := range own {
				if g.Covers(p) {
					return fmt.Errorf("%s: %q is this plugin's own namespace, which is implicit and must not be listed", list.label, string(p))
				}
			}
			for _, d := range denied {
				if patternsOverlap(d, p) {
					return fmt.Errorf("%s: %q reaches the permanently ineligible family %q; it is refused rather than narrowed", list.label, string(p), string(d))
				}
			}
		}
	}
	return nil
}

// refuseReservedTokens refuses what the grammar lets through on purpose.
// bus.IsReservedToken covers the substrate's own "$"-prefixed names and the
// inbox root; the inter-gateway mesh prefix is this package's, because the bus
// does not know gateways exist.
func refuseReservedTokens(label string, p bus.Pattern) error {
	for i, tok := range p.Tokens() {
		if bus.IsReservedToken(tok) {
			return fmt.Errorf("%s: %q uses the reserved token %q", label, string(p), tok)
		}
		if i == 0 && tok == MeshPrefix {
			return fmt.Errorf("%s: %q starts with the reserved mesh prefix %q", label, string(p), MeshPrefix+".")
		}
	}
	return nil
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
func validateServes(m Manifest) error {
	id := strings.TrimSpace(m.ID)
	prefixes := []string{"svc." + id + ".", "cmd." + id + "."}
	services := map[string]bool{}
	for _, svc := range m.Serves {
		name := strings.TrimSpace(svc.Name)
		if name == "" {
			return fmt.Errorf("serves.name required")
		}
		if services[name] {
			return fmt.Errorf("serves: duplicate service name %q", name)
		}
		services[name] = true
		endpoints := map[string]bool{}
		for _, ep := range svc.Endpoints {
			epName := strings.TrimSpace(ep.Name)
			if epName == "" {
				return fmt.Errorf("serves %s: endpoint name required", name)
			}
			if endpoints[epName] {
				return fmt.Errorf("serves %s: duplicate endpoint name %q", name, epName)
			}
			endpoints[epName] = true
			if _, err := bus.ParseSubject(string(ep.Subject)); err != nil {
				return fmt.Errorf("serves %s.%s: %w", name, epName, err)
			}
			if err := refuseReservedTokens("serves "+name+"."+epName, bus.Pattern(ep.Subject)); err != nil {
				return err
			}
			inOwn := false
			for _, prefix := range prefixes {
				if strings.HasPrefix(string(ep.Subject), prefix) {
					inOwn = true
					break
				}
			}
			if !inOwn {
				return fmt.Errorf("serves %s.%s: subject %q is outside this plugin's own namespace (%s or %s)",
					name, epName, string(ep.Subject), prefixes[0], prefixes[1])
			}
			if strings.TrimSpace(ep.Point) == "" {
				continue
			}
			if err := validatePointName("serves "+name+"."+epName, m.Publisher, ep.Point); err != nil {
				return err
			}
		}
	}
	return nil
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
func validatePointName(label string, publisher *Publisher, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%s: extension point required", label)
	}
	if IsKnownPoint(name) {
		return nil
	}
	if suggestion, ok := SuggestPoint(name); ok {
		return fmt.Errorf("%s: unknown extension point %q — did you mean %q?", label, name, string(suggestion))
	}
	if err := ValidateExtendsName(publisher, name); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if strings.HasPrefix(name, HostPointNamespace) {
		return fmt.Errorf("%s: %q is not a host extension point; the host's points are %s",
			label, name, strings.Join(knownPointNames(), ", "))
	}
	return nil
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
func validateAssets(m Manifest) error {
	streams := map[string]bool{}
	for _, s := range m.Streams {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			return fmt.Errorf("streams.name required")
		}
		if streams[name] {
			return fmt.Errorf("streams: duplicate stream name %q", name)
		}
		streams[name] = true
		if s.MaxBytes <= 0 {
			return fmt.Errorf("streams %s: maxBytes is required and must be positive", name)
		}
		if s.MaxAgeSeconds <= 0 {
			return fmt.Errorf("streams %s: maxAgeSeconds is required and must be positive", name)
		}
		if s.MaxMsgs < 0 {
			return fmt.Errorf("streams %s: maxMsgs must be >= 0", name)
		}
		for _, subject := range s.Subjects {
			if _, err := bus.ParsePattern(string(subject)); err != nil {
				return fmt.Errorf("streams %s: %w", name, err)
			}
			if err := refuseReservedTokens("streams "+name, subject); err != nil {
				return err
			}
		}
	}
	kv := map[string]bool{}
	for _, b := range m.KV {
		name := strings.TrimSpace(b.Name)
		if name == "" {
			return fmt.Errorf("kv.name required")
		}
		if kv[name] {
			return fmt.Errorf("kv: duplicate bucket name %q", name)
		}
		kv[name] = true
		if b.MaxBytes < 0 || b.History < 0 {
			return fmt.Errorf("kv %s: maxBytes and history must be >= 0", name)
		}
	}
	objects := map[string]bool{}
	for _, b := range m.Objects {
		name := strings.TrimSpace(b.Name)
		if name == "" {
			return fmt.Errorf("objects.name required")
		}
		if objects[name] {
			return fmt.Errorf("objects: duplicate bucket name %q", name)
		}
		objects[name] = true
		if b.MaxBytes < 0 {
			return fmt.Errorf("objects %s: maxBytes must be >= 0", name)
		}
	}
	return nil
}

func validateNeeds(m Manifest) error {
	vocabulary := bus.CapabilityNames()
	seen := map[string]bool{}
	for _, need := range m.Needs {
		need = strings.TrimSpace(need)
		if need == "" {
			return fmt.Errorf("needs: empty capability")
		}
		if seen[need] {
			return fmt.Errorf("needs: duplicate capability %q", need)
		}
		seen[need] = true
		if !slices.Contains(vocabulary, need) {
			return fmt.Errorf("needs: unknown capability %q (known: %s)", need, strings.Join(vocabulary, " "))
		}
	}
	return nil
}

// validateProtocol keeps v1's exact-name rules for the fields that are still
// exact names. Feature and hook identifiers are not subjects and never were.
func validateProtocol(p *ProtocolRequirements) error {
	if p == nil {
		return nil
	}
	if p.Major != 0 && p.Major != ProtocolMajor {
		return fmt.Errorf("protocol.major %d is not this manifest schema (%d)", p.Major, ProtocolMajor)
	}
	if p.Major == 0 && len(p.Required) != 0 {
		return fmt.Errorf("protocol.required needs an explicit major version")
	}
	if err := validateExactNames("protocol.required", p.Required); err != nil {
		return err
	}
	if p.Major == 0 && len(p.Hooks) != 0 {
		return fmt.Errorf("protocol.hooks needs an explicit major version")
	}
	if err := validateExactNames("protocol.hooks", p.Hooks); err != nil {
		return err
	}
	for _, hook := range p.Hooks {
		if !slices.Contains(lifecycleHookVocabulary, hook) {
			return fmt.Errorf("protocol.hooks: unknown lifecycle hook %q", hook)
		}
	}
	return nil
}

// lifecycleHookVocabulary is the closed set of hooks a manifest may advertise.
// Every hook runs on entry or reports health; there is deliberately no stop
// hook, so a plugin cannot declare an exit veto.
var lifecycleHookVocabulary = []string{"activation.check", "ready", "health"}

func validateUI(m Manifest) error {
	seen := map[string]bool{}
	for _, item := range m.UI {
		if err := validateIDSegment("ui.id", item.ID); err != nil {
			return err
		}
		if seen[item.ID] {
			return fmt.Errorf("duplicate ui.id %q", item.ID)
		}
		seen[item.ID] = true
		// A vocabulary addition without its eligibility policy is not valid.
		if _, ok := ContributionRoleFor(item.Slot); !ok {
			return fmt.Errorf("unknown ui slot %q", item.Slot)
		}
		switch item.Slot {
		case SlotCommand:
			if item.Command == "" || item.PanelID != "" {
				return fmt.Errorf("ui command requires only command")
			}
			if err := validateExactNames("ui.command", []string{item.Command}); err != nil {
				return err
			}
		default:
			if item.PanelID == "" || item.Command != "" {
				return fmt.Errorf("ui slot %q requires only panelId", item.Slot)
			}
			owned := false
			for _, panel := range m.Panels {
				if panel.ID == item.PanelID {
					owned = true
					break
				}
			}
			if !owned {
				return fmt.Errorf("ui panelId %q is not owned by this manifest", item.PanelID)
			}
		}
		if err := validateBindings(item.ID, item.Bindings); err != nil {
			return err
		}
	}
	return nil
}

// validateBindings checks the shape only. Whether the plugin may subscribe to
// the event it names is the host's question, asked at registration, and no
// manifest can answer it for itself.
func validateBindings(contributionID string, bindings []UIBinding) error {
	seen := map[string]bool{}
	for _, bind := range bindings {
		switch bind.Kind {
		case BindCount, BindBadge, BindLiveness, BindToggle:
		default:
			return fmt.Errorf("ui %q: unknown binding kind %q", contributionID, bind.Kind)
		}
		if seen[bind.Kind] {
			return fmt.Errorf("ui %q: duplicate binding kind %q", contributionID, bind.Kind)
		}
		seen[bind.Kind] = true
		if err := validateExactNames("ui.bindings.event", []string{bind.Event}); err != nil {
			return err
		}
		// A field selects one value out of the payload, so it is one JSON key,
		// not a path. A path would be a query language nobody asked for, and the
		// host would have to evaluate it against a payload a plugin controls.
		if bind.Field != "" {
			if err := validateIDSegment("ui.bindings.field", bind.Field); err != nil {
				return err
			}
			if strings.ContainsAny(bind.Field, ".[]") {
				return fmt.Errorf("ui %q: binding field %q must be one JSON key, not a path", contributionID, bind.Field)
			}
		}
	}
	return nil
}

// validatePublicRoutes holds the one rule that makes the field safe to trust:
// a plugin may only waive authentication on a route it already declared.
//
// Without it, a manifest could name another plugin's route, or a host route, as
// public and open a hole in a surface it does not own.
func validatePublicRoutes(m Manifest) error {
	if len(m.PublicRoutes) == 0 {
		return nil
	}
	declared := make(map[string]bool, len(m.Routes))
	for _, route := range m.Routes {
		declared[strings.TrimSpace(route)] = true
	}
	seen := map[string]bool{}
	for _, route := range m.PublicRoutes {
		route = strings.TrimSpace(route)
		if route == "" {
			return fmt.Errorf("publicRoutes entry is empty")
		}
		if seen[route] {
			return fmt.Errorf("duplicate publicRoutes entry %q", route)
		}
		seen[route] = true
		if !declared[route] {
			return fmt.Errorf("publicRoutes %q is not one of this manifest's routes", route)
		}
	}
	return nil
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

func validateDocumentPaths(m Manifest) error {
	var claimed []string
	panelIDs := map[string]int{}
	for _, panel := range m.Panels {
		panelIDs[panel.ID]++
	}
	for _, panel := range m.Panels {
		if len(panel.DocumentPaths) == 0 {
			continue
		}
		if m.Protocol == nil || m.Protocol.Major == 0 || !slices.Contains(m.Protocol.Required, FeatureDocumentPaths) {
			return fmt.Errorf("document paths require explicit protocol negotiation for %s", FeatureDocumentPaths)
		}
		if err := validateIDSegment("panel.id", panel.ID); err != nil {
			return err
		}
		if panel.ID != strings.TrimSpace(panel.ID) || panelIDs[panel.ID] != 1 || strings.TrimSpace(panel.URL) == "" {
			return fmt.Errorf("document paths require a unique panel id and document URL")
		}
		for _, pattern := range panel.DocumentPaths {
			if err := ValidateDocumentPath(pattern); err != nil {
				return err
			}
			for _, previous := range claimed {
				if overlap, _ := DocumentPathsOverlap(previous, pattern); overlap {
					return fmt.Errorf("overlapping document paths %q and %q", previous, pattern)
				}
			}
			claimed = append(claimed, pattern)
		}
	}
	return nil
}

// validateConfig checks the declared sections: ids present and unique, and every
// field well formed with a key unique within its section.
func validateConfig(c *Config) error {
	if c == nil {
		return nil
	}
	sections := map[string]bool{}
	for _, s := range c.Sections {
		id := strings.TrimSpace(s.ID)
		if id == "" || sections[id] {
			return fmt.Errorf("config.sections: id %q is empty or repeated", s.ID)
		}
		sections[id] = true
		keys := map[string]bool{}
		for _, f := range s.Fields {
			if err := validateConfigField(f); err != nil {
				return fmt.Errorf("config.sections %s: %w", id, err)
			}
			if keys[f.Key] {
				return fmt.Errorf("config.sections %s: field key %q repeated", id, f.Key)
			}
			keys[f.Key] = true
		}
	}
	return nil
}

func validateConfigField(f ConfigField) error {
	if strings.TrimSpace(f.Key) == "" {
		return fmt.Errorf("config field key required")
	}
	kinds := []string{ConfigKindBool, ConfigKindInt, ConfigKindString, ConfigKindStringList, ConfigKindEnum, ConfigKindSecret, ConfigKindProvider}
	if !slices.Contains(kinds, f.Kind) {
		return fmt.Errorf("config field %s: unknown kind %q", f.Key, f.Kind)
	}
	if f.Kind == ConfigKindProvider {
		parts := strings.Split(f.Point, ".")
		if len(parts) < 2 || (strings.HasPrefix(f.Point, HostPointNamespace) && len(parts) < 3) {
			return fmt.Errorf("config field %s: provider needs an exact extension point", f.Key)
		}
		for _, part := range parts {
			if part == "" || strings.Trim(part, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
				return fmt.Errorf("config field %s: invalid provider point %q", f.Key, f.Point)
			}
		}
		if err := validateExactNames("config provider requires", f.Requires); err != nil {
			return err
		}
		if f.Default != "" {
			if err := validateExactNames("config provider default", []string{f.Default}); err != nil {
				return err
			}
		}
	} else if f.Point != "" || len(f.Requires) != 0 {
		return fmt.Errorf("config field %s: point/requires apply only to provider", f.Key)
	}
	if (f.Min != nil || f.Max != nil) && f.Kind != ConfigKindInt {
		return fmt.Errorf("config field %s: min/max apply only to int", f.Key)
	}
	if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
		return fmt.Errorf("config field %s: min %d exceeds max %d", f.Key, *f.Min, *f.Max)
	}
	if (len(f.Choices) > 0) != (f.Kind == ConfigKindEnum) || slices.Contains(f.Choices, "") {
		return fmt.Errorf("config field %s: an enum needs non-empty choices, and only an enum has them", f.Key)
	}
	if f.Default == "" {
		return nil
	}
	switch f.Kind {
	case ConfigKindBool:
		if _, err := strconv.ParseBool(f.Default); err != nil {
			return fmt.Errorf("config field %s: default %q is not a bool", f.Key, f.Default)
		}
	case ConfigKindInt:
		n, err := strconv.Atoi(f.Default)
		if err != nil || (f.Min != nil && n < *f.Min) || (f.Max != nil && n > *f.Max) {
			return fmt.Errorf("config field %s: default %q is not an int within bounds", f.Key, f.Default)
		}
	case ConfigKindEnum:
		if !slices.Contains(f.Choices, f.Default) {
			return fmt.Errorf("config field %s: default %q is not one of its choices", f.Key, f.Default)
		}
	case ConfigKindStringList, ConfigKindSecret:
		return fmt.Errorf("config field %s: a %s field takes no default", f.Key, f.Kind)
	}
	return nil
}

func validateIDSegment(what, s string) error {
	if s = strings.TrimSpace(s); s == "" {
		return fmt.Errorf("%s required", what)
	}
	if strings.ContainsAny(s, `/\ `) || strings.Contains(s, "..") {
		return fmt.Errorf("%s must be a single path segment", what)
	}
	return nil
}

// validateExactNames guards the fields that are still exact names rather than
// subjects: negotiated protocol features, lifecycle hooks, the command a UI
// contribution invokes and the event a binding follows.
func validateExactNames(label string, values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "*?\r\n\t ") {
			return fmt.Errorf("%s must contain nonempty exact names", label)
		}
		if seen[value] {
			return fmt.Errorf("%s contains duplicate %q", label, value)
		}
		seen[value] = true
	}
	return nil
}
