package plugin

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type documentSegment struct {
	value string
	kind  byte // literal (0), named parameter (:), or terminal one-or-more catch-all (*)
}

// ValidateDocumentPath checks a shell document pattern. Patterns contain literal
// ASCII segments, :name parameters, and an optional terminal *name matching one
// or more segments. Root, empty segments, trailing slashes, escaping, queries and
// fragments are forbidden. Host-reserved namespaces are a host admission policy.
func ValidateDocumentPath(pattern string) error {
	_, err := parseDocumentPath(pattern)
	return err
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
			if len(name) == 0 || !documentLetter(name[0]) || names[name] || (segment.kind == '*' && i != len(parts)-1) {
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
		if err != nil || decoded == "" || decoded == "." || decoded == ".." || !utf8.ValidString(decoded) || strings.ContainsAny(decoded, "/\\") || strings.ContainsFunc(decoded, unicode.IsControl) {
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
// Hosts use this before atomically publishing claims, without route precedence.
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

func (m Manifest) validateDocumentPaths() error {
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
