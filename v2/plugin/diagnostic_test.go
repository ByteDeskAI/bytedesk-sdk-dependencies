package plugin

import "testing"

// TestBDP6020IsUsableOutsideVerifyDir: this code is reachable only through a
// host's own attestation check (TM-407's direct-upload route), never through
// VerifyDir judging a manifest alone — so it has no fixture, and this test is
// what proves plugin.NewDiagnostic still produces it correctly for a caller
// that has no reason to import the contract package.
func TestBDP6020IsUsableOutsideVerifyDir(t *testing.T) {
	d := NewDiagnostic("BDP6020", "publisher", "bytedesk")
	if d.Severity != SeverityError {
		t.Fatalf("severity = %v, want error", d.Severity)
	}
	if d.Message != `publisher "bytedesk" is reserved; only ByteDesk may claim it, and only with a Store countersignature` {
		t.Fatalf("message = %q", d.Message)
	}
}
