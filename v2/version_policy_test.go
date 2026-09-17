package sdkv2

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The release policy for this module, as tests rather than as prose in a
// CHANGELOG nobody reads before tagging. It is the v1 policy
// (bytedesk-remote-gateway-plugin-sdk/version_policy_test.go) carried forward,
// plus the pseudo-version gate v1 never had.

// pseudoVersion matches every form the Go toolchain mints for an untagged
// commit: vX.0.0-<timestamp>-<hash>, vX.Y.Z-pre.0.<timestamp>-<hash> and
// vX.Y.Z-0.<timestamp>-<hash>. All three end the same way.
var pseudoVersion = regexp.MustCompile(`-[0-9]{14}-[0-9a-f]{12}(\+incompatible)?$`)

var requireLine = regexp.MustCompile(`(?m)^\s*(?:require\s+)?([^\s/][^\s]*)\s+(v[^\s]+)`)

// TestModuleIsV2 pins the import path. A /v2 module whose go.mod says /v1
// resolves to the wrong code with no error anywhere: Go treats the path as the
// identity, and a consumer requiring /v2 would silently get v1's API.
func TestModuleIsV2(t *testing.T) {
	mod := readGoMod(t)
	want := "module github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2"
	if !strings.Contains(mod, want) {
		t.Fatalf("go.mod must declare %q", want)
	}
}

// TestNoReplaceDirective is the rule the sibling repository enforces
// deliberately: a replace papers over an untagged dependency and makes the
// module build for the author and nobody else. If a dependency is not
// releasable, that is the problem to fix.
func TestNoReplaceDirective(t *testing.T) {
	mod := readGoMod(t)
	if strings.Contains(mod, "\nreplace ") || strings.HasPrefix(mod, "replace ") {
		t.Fatal("go.mod must not contain a replace directive")
	}
}

// TestNoPseudoVersionRequires refuses a dependency pinned to a commit.
//
// A pseudo-version means the dependency was consumed before it was released.
// Tagging this module on top of one publishes a release whose own inputs were
// never published, and the first consumer to build from a clean cache is the
// one who finds out.
func TestNoPseudoVersionRequires(t *testing.T) {
	for _, m := range requireLine.FindAllStringSubmatch(readGoMod(t), -1) {
		path, version := m[1], m[2]
		if path == "go" || path == "toolchain" || path == "module" {
			continue
		}
		if pseudoVersion.MatchString(version) {
			t.Errorf("%s is required at pseudo-version %s: release it and require the tag", path, version)
		}
	}
}

// TestRequiresV1AtATag records the one intentional dependency: v2/plugin/v1compat
// implements the v1 Host over the v2 bus, so v1 is a normal, tagged require of
// v2. v1 keeps building beside v2; it is not vendored, copied or replaced.
func TestRequiresV1AtATag(t *testing.T) {
	mod := readGoMod(t)
	const v1 = "github.com/ByteDeskAI/bytedesk-sdk-dependencies "
	i := strings.Index(mod, v1)
	if i < 0 {
		t.Skip("v1 is not yet required; v1compat has not landed")
	}
	rest := mod[i+len(v1):]
	version := strings.Fields(rest)[0]
	if pseudoVersion.MatchString(version) {
		t.Fatalf("v1 is required at pseudo-version %s", version)
	}
	if !strings.HasPrefix(version, "v0.") && !strings.HasPrefix(version, "v1.") {
		t.Fatalf("the v1 require must name a v0/v1 tag, got %s", version)
	}
}

// TestVersionIsMajorTwo keeps VERSION and the module path from drifting apart.
// The file is what the release chain reads when it tags.
func TestVersionIsMajorTwo(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	v := strings.TrimSpace(string(raw))
	if v == "" {
		t.Fatal("VERSION is empty")
	}
	if !strings.HasPrefix(v, "2.") {
		t.Fatalf("VERSION is %q; a /v2 module releases as 2.x", v)
	}
}

// TestVersionsAreIndependent records the policy explicitly so nobody adds a
// lockstep assertion later: this module's VERSION is not required to equal the
// version of anything it requires, and the plugin SDK's version is not
// required to equal this one.
func TestVersionsAreIndependent(t *testing.T) {
	t.Log("VERSION here and in any consumer move independently by design; do not add a lockstep assertion")
}

func readGoMod(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
