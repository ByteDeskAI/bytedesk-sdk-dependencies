package bus

import (
	"errors"
	"strings"
)

// Subject and Pattern are the addressing grammar of the bus. They are the one
// matcher in the system: the manifest validator, Grants.Can, the memory
// substrate and the host's grant compiler all parse and match through these
// functions, so a pattern that is accepted in a manifest means exactly what it
// means at delivery time.
//
// Grammar (deliberately narrower than NATS, because a narrower grammar can be
// widened later and a wider one cannot be narrowed):
//
//	subject  := token ("." token)*        |  reserved ("." any)*
//	pattern  := ptoken ("." ptoken)* ["." ">"]
//	token    := [a-z0-9][a-z0-9_-]*
//	ptoken   := token | "*"
//	reserved := "$"<anything>  |  "_INBOX"
//
// At most MaxTokens tokens and MaxLen bytes. "*" matches exactly one token;
// ">" matches one or more trailing tokens and may appear only last.
//
// A reserved FIRST token opens a substrate-owned namespace, and the rest of
// that subject is spelled by the substrate, not by us: "$SYS.ACCOUNT.x.CONNECT"
// is uppercase and an inbox suffix is random base62. Structure still applies
// inside it — token count, length, no empty token, ">" last — but the token
// character rule does not. The alternative was a grammar in which the
// permanently-ineligible set could not name the family it denies.
//
// Such a subject PARSES and is then refused by the manifest validator (see
// plugin.validateSubjectPatterns): the substrate must be able to name "$SYS.>"
// and "_INBOX.<id>.>" to deny or route them, while a plugin manifest may not.
// A reserved token anywhere other than first names nothing and is refused here.
const (
	// MaxTokens caps the token count of any subject or pattern.
	MaxTokens = 16
	// MaxLen caps the byte length of any subject or pattern.
	MaxLen = 255
)

// Subject is a concrete, wildcard-free address.
type Subject string

// Pattern is a subject filter. Every Subject is a valid Pattern.
type Pattern string

// ErrSubject is the class of every parse failure. Match with errors.Is.
var ErrSubject = errors.New("invalid subject")

// ParseSubject validates s as a concrete subject: no wildcards.
func ParseSubject(s string) (Subject, error) {
	if err := parse(s, false); err != nil {
		return "", err
	}
	return Subject(s), nil
}

// ParsePattern validates s as a subject filter: "*" and a trailing ">" allowed.
func ParsePattern(s string) (Pattern, error) {
	if err := parse(s, true); err != nil {
		return "", err
	}
	return Pattern(s), nil
}

// Tokens splits the subject on ".". It does not validate.
func (s Subject) Tokens() []string { return strings.Split(string(s), ".") }

// Tokens splits the pattern on ".". It does not validate.
func (p Pattern) Tokens() []string { return strings.Split(string(p), ".") }

// HasWildcard reports whether p contains "*" or ">".
func (p Pattern) HasWildcard() bool {
	for _, t := range p.Tokens() {
		if t == "*" || t == ">" {
			return true
		}
	}
	return false
}

// IsReservedToken reports whether t is reserved to the substrate: a
// "$"-prefixed token, or the inbox root. Reserved tokens parse but never
// appear in a plugin manifest.
func IsReservedToken(t string) bool {
	return strings.HasPrefix(t, "$") || t == "_INBOX"
}

// Matches reports whether p addresses the concrete subject s.
func (p Pattern) Matches(s Subject) bool {
	pt := p.Tokens()
	st := s.Tokens()
	for i, t := range pt {
		if t == ">" {
			// ">" is one-or-more trailing tokens.
			return i < len(st)
		}
		if i >= len(st) {
			return false
		}
		if t == "*" {
			continue
		}
		if t != st[i] {
			return false
		}
	}
	return len(pt) == len(st)
}

// Covers reports whether every subject q matches is also matched by p. It is
// grant containment: a grant p admits a requested pattern q only when
// p.Covers(q). Containment is deliberately conservative — "a.*" does not cover
// "a.>", because ">" spans token counts "*" cannot reach.
func (p Pattern) Covers(q Pattern) bool {
	pt := p.Tokens()
	qt := q.Tokens()
	for i, t := range pt {
		if t == ">" {
			return i < len(qt)
		}
		if i >= len(qt) {
			return false
		}
		if qt[i] == ">" {
			// q outlives every remaining token of p.
			return false
		}
		if t == "*" {
			continue
		}
		if t != qt[i] {
			return false
		}
	}
	return len(pt) == len(qt)
}

// CoveredByAny reports whether any pattern in grants covers q.
func CoveredByAny(grants []Pattern, q Pattern) bool {
	for _, g := range grants {
		if g.Covers(q) {
			return true
		}
	}
	return false
}

// MatchedByAny reports whether any pattern in grants matches s.
func MatchedByAny(grants []Pattern, s Subject) bool {
	for _, g := range grants {
		if g.Matches(s) {
			return true
		}
	}
	return false
}

func parse(s string, wildcards bool) error {
	if s == "" {
		return fault(s, "empty")
	}
	if len(s) > MaxLen {
		return fault(s, "longer than 255 bytes")
	}
	toks := strings.Split(s, ".")
	if len(toks) > MaxTokens {
		return fault(s, "more than 16 tokens")
	}
	// A reserved FIRST token opens a substrate-owned namespace, and the
	// substrate spells its own subjects: "$SYS.ACCOUNT.<acct>.CONNECT" is
	// uppercase, and an inbox suffix is random base62. Applying our token
	// grammar past that point would make the deny set unspellable — the one
	// family that most has to be nameable. Structure (token count, length,
	// wildcard placement, no empty token) still applies.
	substrate := len(toks) > 0 && IsReservedToken(toks[0])
	for i, t := range toks {
		switch {
		case t == "":
			return fault(s, "empty token")
		case t == ">":
			if !wildcards {
				return fault(s, `">" is not allowed in a concrete subject`)
			}
			if i != len(toks)-1 {
				return fault(s, `">" is only allowed as the last token`)
			}
		case t == "*":
			if !wildcards {
				return fault(s, `"*" is not allowed in a concrete subject`)
			}
		case substrate:
			// Substrate-owned: any non-empty token, already checked above.
		default:
			// A reserved token is only meaningful as the FIRST one. Anywhere
			// else it names nothing, so it is refused here rather than left
			// for the validator.
			if err := token(s, t); err != nil {
				return err
			}
		}
	}
	return nil
}

func token(s, t string) error {
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '_' || c == '-') && i > 0:
		default:
			return fault(s, "token "+quote(t)+` must match [a-z0-9][a-z0-9_-]*`)
		}
	}
	return nil
}

func fault(s, why string) error {
	return &parseError{subject: s, why: why}
}

type parseError struct {
	subject string
	why     string
}

func (e *parseError) Error() string {
	return "bus: " + quote(e.subject) + ": " + e.why
}

func (e *parseError) Is(target error) bool { return target == ErrSubject }

func quote(s string) string {
	if len(s) > 64 {
		s = s[:64] + "..."
	}
	return `"` + s + `"`
}
