package plugin

import "strings"

// The typed extension-point registry.
//
// An extension point is a SERVICE in v2. A provider mounts
// svc.<provider>.<point>.v1.<op> and the host DISCOVERS it through
// Bus().Services().Discover; there is no registration callback, no host-held
// table of providers and no adapter per point. What is left for the SDK to own
// is the vocabulary — which point names exist at all — and that is this file.
//
// The registry is CLOSED. A manifest naming a point that is not here fails
// validation rather than being accepted and then never dispatched to, because
// a provider that is silently never called looks exactly like a provider whose
// point simply has no consumers today. Adding a name here is an SDK release.
//
// Two names are deliberately ABSENT and must stay absent: host.auth.method and
// host.system.action. Authentication methods and system-level actions are the
// host's own, not an extension surface — a plugin that could supply either
// could substitute itself for the gateway's authentication or run host actions
// under the host's authority. A manifest naming one FAILS validation, which is
// the point; TestHostOwnedPointsAreNotExtensible holds it.
type Point string

const (
	// PointMCPTool supplies tools to the Model Context Protocol surface.
	PointMCPTool Point = "mcp.tool"
	// PointFilesS3 supplies an S3-compatible file backend.
	PointFilesS3 Point = "files.s3"
	// PointACPProvider supplies an Agent Client Protocol provider.
	PointACPProvider Point = "acp.provider"
	// PointAIDecision provides typed Choice, Score and Noul decision jobs.
	// Only the host resolves and dispatches providers. Consumers call the stable
	// aidecision host facade, never a provider's endpoint directly.
	PointAIDecision Point = "ai.decision"
	// PointTerminalPresentation projects a terminal list into the shell.
	PointTerminalPresentation Point = "terminal.presentation"
	// PointSettingsSection contributes one section to the settings surface.
	// Its operations are svc.<provider>.host.settings.section.v1.snapshot and
	// .patch. A typed .validate endpoint opts into host-owned persistence and
	// receives redacted merged values only (package hostsettings).
	PointSettingsSection Point = "host.settings.section"
	// PointSessionBackend supplies terminal sessions. The host owns it, so only
	// the host and plugins compiled into it may declare it. The name keeps its
	// v1 spelling (plugin.SessionBackendPoint), host-namespaced, so a manifest
	// that named it under v1 still names it under v2.
	PointSessionBackend Point = "host.session.backend"
)

// knownPoints is the closed registry, in the order KnownPoints reports and
// SuggestPoint searches. Order is stable so a near-miss that could resolve to
// two points always resolves to the same one.
var knownPoints = []Point{
	PointMCPTool,
	PointFilesS3,
	PointACPProvider,
	PointAIDecision,
	PointTerminalPresentation,
	PointSettingsSection,
	PointSessionBackend,
}

// KnownPoints returns every point name the SDK recognises. The slice is a copy,
// so a caller cannot extend the closed registry by appending to it.
func KnownPoints() []Point {
	out := make([]Point, len(knownPoints))
	copy(out, knownPoints)
	return out
}

// IsKnownPoint reports whether s is one of the registered point names, matched
// exactly. An unknown name is false — including host.auth.method and
// host.system.action, which are host-owned and not extension points at all.
func IsKnownPoint(s string) bool {
	for _, p := range knownPoints {
		if string(p) == s {
			return true
		}
	}
	return false
}

// SuggestPoint returns the correct spelling for a near miss, so a validator can
// say "settings.section: did you mean host.settings.section?" instead of
// "unknown extension point". It resolves two kinds of mistake and no others:
//
//   - a bare name that is the tail of a namespaced point
//     ("settings.section" to "host.settings.section"), which is the mistake
//     everyone makes with the host namespace; and
//   - a single-character typo — one insertion, deletion or substitution.
//
// Anything further away returns false. A suggestion that guesses too eagerly is
// worse than none: an author who takes a wrong suggestion ships a manifest that
// validates and implements a point nobody calls.
//
// SuggestPoint never widens what IsKnownPoint accepts. It produces a message,
// not an admission.
func SuggestPoint(s string) (Point, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", false
	}
	if IsKnownPoint(s) {
		return Point(s), true
	}
	for _, p := range knownPoints {
		if strings.HasSuffix(string(p), "."+s) {
			return p, true
		}
	}
	for _, p := range knownPoints {
		if withinOneEdit(string(p), s) {
			return p, true
		}
	}
	return "", false
}

// withinOneEdit reports whether a and b differ by at most one insertion,
// deletion or substitution. It is bounded rather than a general edit distance
// because the answer only has to separate "one typo" from "something else":
// a full distance-2 search starts matching mcp.tool to files.s3 in spirit if
// not in fact, and every extra match is a wrong suggestion an author may take.
func withinOneEdit(a, b string) bool {
	switch d := len(a) - len(b); {
	case d == 0:
		diff := 0
		for i := range a {
			if a[i] != b[i] {
				diff++
				if diff > 1 {
					return false
				}
			}
		}
		return diff == 1
	case d == 1:
		return isOneDeletionFrom(a, b)
	case d == -1:
		return isOneDeletionFrom(b, a)
	default:
		return false
	}
}

// isOneDeletionFrom reports whether removing exactly one byte from long yields
// short. len(long) == len(short)+1 is the caller's guarantee.
func isOneDeletionFrom(long, short string) bool {
	for i := 0; i < len(short); i++ {
		if long[i] != short[i] {
			return long[i+1:] == short[i:]
		}
	}
	return true
}
