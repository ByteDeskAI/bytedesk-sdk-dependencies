package semver

import "testing"

func TestAtLeast(t *testing.T) {
	if !AtLeast("1.2.0", "1.0.0") {
		t.Fatal("1.2.0 should satisfy 1.0.0")
	}
	if AtLeast("0.9.0", "1.0.0") {
		t.Fatal("0.9.0 should not satisfy 1.0.0")
	}
	if !AtLeast("1.0.0", "") {
		t.Fatal("empty min always ok")
	}
}
