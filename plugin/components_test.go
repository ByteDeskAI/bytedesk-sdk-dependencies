package plugin

import "testing"

func TestComponentContract(t *testing.T) {
	v := ComponentIdentity{ID: "a", Family: "terminal", OwnerID: "projects", Generation: "1"}
	if err := ValidateComponentIdentity(v); err != nil {
		t.Fatal(err)
	}
	v.Generation = ""
	if ValidateComponentIdentity(v) == nil {
		t.Fatal("missing generation accepted")
	}
	v.Generation = "1"
	if ValidateComponentExtension(ComponentExtension{ID: "x", Target: v, Point: "replace", Label: "X"}) == nil {
		t.Fatal("replacement accepted")
	}
	if err := ValidateComponentExtension(ComponentExtension{ID: "x", Target: v, Point: "badge", Label: "X"}); err != nil {
		t.Fatal(err)
	}
}
