package consumer

import (
	"os/exec"
	"strings"
	"testing"
)

// TestConsumerUnionRefusesUnclassifiedTypes is the whole point of the closed
// union, and it is a compiler test because that is the only place the
// guarantee actually lives.
//
// negative_forbidden.go is behind a build tag so the ordinary build ignores it.
// This test builds it on purpose and asserts the compiler REJECTS every call
// site in it. If the generated wrappers ever stopped being constrained by this
// package's Payload, every one of those call sites would start compiling, the
// classification guarantee would be gone, and nothing else in the suite would
// notice — the union would still exist, it would simply no longer be load
// bearing.
func TestConsumerUnionRefusesUnclassifiedTypes(t *testing.T) {
	out, err := exec.Command("go", "build", "-tags", "compilefail", ".").CombinedOutput()
	if err == nil {
		t.Fatal("negative_forbidden.go compiled; an unclassified or forged payload must not reach a call site")
	}
	for _, want := range []string{
		"Unclassified does not satisfy Payload",
		"Forged does not satisfy Payload",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("compiler output missing %q:\n%s", want, out)
		}
	}
}

// TestUnionMembershipIsExactlyTheClassifiedTypes pins what the generator put in
// the union. Forged embeds Ping and adds an unclassified field; a union without
// ~ matches exactly the named types, so Forged is not one of them however much
// it looks like one.
func TestUnionMembershipIsExactlyTheClassifiedTypes(t *testing.T) {
	// A compile-time assertion per member: if the generator dropped one, this
	// file stops building.
	accepts(Ping{})
	accepts(Pong{})
}

func accepts[T Payload](T) {}
