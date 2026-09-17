package bus

import "testing"

// TestReservedFirstTokenOpensASubstrateNamespace records why the token
// grammar stops at a reserved first token.
//
// The permanently-ineligible set has to be able to NAME "$SYS.>" and the
// transport has to be able to name "_INBOX.<id>.<random>". Both are spelled by
// the broker, in the broker's case convention, not ours. A grammar that
// refused them would leave the deny set unspellable, which is the one family
// that most has to be spellable.
func TestReservedFirstTokenOpensASubstrateNamespace(t *testing.T) {
	for _, s := range []string{
		"$SYS.ACCOUNT.PLUGIN_files.CONNECT",
		"$JS.API.STREAM.INFO.EVENTS_UI",
		"_INBOX.files.hK3zQ9XbT2",
	} {
		if _, err := ParseSubject(s); err != nil {
			t.Errorf("ParseSubject(%q) = %v, want ok: the substrate spells its own subjects", s, err)
		}
	}
	for _, p := range []string{"$SYS.>", "_INBOX.files.>", "$JS.API.*"} {
		if _, err := ParsePattern(p); err != nil {
			t.Errorf("ParsePattern(%q) = %v, want ok", p, err)
		}
	}

	// The relaxation is scoped to a reserved FIRST token. It does not leak
	// into ordinary subjects, and structural rules still apply inside it.
	for _, s := range []string{
		"event.FILES.changed",                  // uppercase in an ordinary namespace
		"event.$SYS.changed",                   // reserved token that is not first
		"$SYS..CONNECT",                        // empty token
		"$SYS.A.B.C.D.E.F.G.H.I.J.K.L.M.N.O.P", // 17 tokens
	} {
		if _, err := ParseSubject(s); err == nil {
			t.Errorf("ParseSubject(%q) = ok, want refusal", s)
		}
	}
	if _, err := ParsePattern("$SYS.>.x"); err == nil {
		t.Error(`ParsePattern("$SYS.>.x") = ok: ">" is still last-only inside a substrate namespace`)
	}
}
