package plugin

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Severity is how a host treats a Diagnostic. An error refuses the manifest;
// a warning is reported and the manifest proceeds. There is no third level:
// a rule that is neither refused nor reported is not a rule.
type Severity string

// Diagnostic severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is one finding about a manifest or a package tree.
//
// Code is BDPnnnn, banded by the thousands digit (1 schema, 2 semantic,
// 3 layout, 4 references, 5 deprecation, 6 host gate, 9 trust). Codes are
// stable across releases and shared by every binding, so a fixture written
// against the Go verifier is the same fixture for the TypeScript and Rust
// ones. Path names the manifest field ("serves[0].endpoints[1].subject") or
// the package-relative file the finding is about. Message is human text in the
// wording the pre-diagnostic validator used, so an operator who learnt the old
// errors reads the same ones.
//
// Diagnostic lives in package plugin rather than plugin/contract because the
// semantic rules are here and contract imports this package; the other
// direction would be a cycle.
type Diagnostic struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path,omitempty"`
	Message  string   `json:"message"`
}

// Error makes a Diagnostic usable wherever the pre-diagnostic API returned an
// error: Validate and ParseManifest return the first error-severity finding as
// this value, so errors.As still reaches the code.
func (d Diagnostic) Error() string { return d.Code + ": " + d.Message }

// NewDiagnostic builds a Diagnostic from the shared table in
// contract/v2/diagnostics.json. The table is the single source of severity and
// wording: the Go rules pass only the code, the path and the format arguments,
// so a message cannot drift from what the bindings and the docs carry.
//
// A code the table does not know is reported as such rather than silently
// accepted, so a typo in a rule shows up in the first fixture that trips it.
func NewDiagnostic(code, path string, args ...any) Diagnostic {
	entry, ok := diagnosticTable()[code]
	if !ok {
		return Diagnostic{Code: code, Severity: SeverityError, Path: path,
			Message: fmt.Sprintf("unknown diagnostic %s (args %v)", code, args)}
	}
	return Diagnostic{Code: code, Severity: entry.Severity, Path: path,
		Message: fmt.Sprintf(entry.Message, args...)}
}

// DiagnosticCodes lists every code the table defines, sorted. The contract
// corpus test uses it to prove each code has a fixture that produces it.
func DiagnosticCodes() []string {
	table := diagnosticTable()
	out := make([]string, 0, len(table))
	for code := range table {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

// SortDiagnostics orders findings by (path, code, severity, message) so a
// report is byte-identical whichever binding produced it.
func SortDiagnostics(list []Diagnostic) {
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		return a.Message < b.Message
	})
}

// FirstError returns the first error-severity finding in list as an error, or
// nil. It is how the error-returning entry points keep their signatures over
// a collector.
func FirstError(list []Diagnostic) error {
	for _, d := range list {
		if d.Severity == SeverityError {
			return d
		}
	}
	return nil
}

//go:embed contract/v2/diagnostics.json
var diagnosticsJSON []byte

type diagnosticEntry struct {
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

var (
	diagnosticOnce  sync.Once
	diagnosticCodes map[string]diagnosticEntry
)

func diagnosticTable() map[string]diagnosticEntry {
	diagnosticOnce.Do(func() {
		var doc struct {
			Codes map[string]diagnosticEntry `json:"codes"`
		}
		if err := json.Unmarshal(diagnosticsJSON, &doc); err != nil {
			panic("plugin: contract/v2/diagnostics.json is not valid: " + err.Error())
		}
		diagnosticCodes = doc.Codes
	})
	return diagnosticCodes
}

// collector accumulates diagnostics while the rules run. It exists so a rule
// reads as "report and carry on" rather than "return the first thing wrong",
// which is what an author fixing a manifest needs and what the old
// return-on-first-error validator could not give them.
type collector struct{ list []Diagnostic }

func (c *collector) add(code, path string, args ...any) {
	c.list = append(c.list, NewDiagnostic(code, path, args...))
}
