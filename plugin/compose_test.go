package plugin

import (
	"strings"
	"testing"
)

func TestRoleOrDefault(t *testing.T) {
	if (Manifest{ID: "x", Version: "1"}).RoleOrDefault() != RoleExtension {
		t.Fatal("omitted role is extension")
	}
	if (Manifest{ID: "x", Version: "1", Role: "system"}).RoleOrDefault() != RoleSystem {
		t.Fatal("system")
	}
}

func TestValidateRoleAndRequires(t *testing.T) {
	ok := Manifest{
		ID: "projects", Version: "1.0.0", Role: RoleExtension,
		Requires: []Requirement{{ID: "terminal-runtime"}},
		Provides: []string{"projects.desk"},
	}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	self := Manifest{ID: "a", Version: "1", Requires: []Requirement{{ID: "a"}}}
	if err := self.Validate(); err == nil {
		t.Fatal("expected self-require reject")
	}
	badRole := Manifest{ID: "a", Version: "1", Role: "kernel"}
	if err := badRole.Validate(); err == nil || !strings.Contains(err.Error(), "role") {
		t.Fatalf("expected role reject, got %v", err)
	}
	pathReq := Manifest{ID: "a", Version: "1", Requires: []Requirement{{ID: "../evil"}}}
	if err := pathReq.Validate(); err == nil {
		t.Fatal("expected path require reject")
	}
}

func TestValidateTrialPricing(t *testing.T) {
	m := Manifest{ID: "x", Version: "1", Pricing: &Pricing{Model: "trial", SKU: "sku_x", TrialDays: 14}}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	m.Pricing.SKU = ""
	if err := m.Validate(); err == nil {
		t.Fatal("trial requires sku")
	}
}

func TestMissingRequiredAndCycle(t *testing.T) {
	a := Manifest{ID: "projects", Version: "1", Requires: []Requirement{{ID: "files"}, {ID: "terminal-runtime"}}}
	installed := map[string]struct{}{"terminal-runtime": {}}
	miss := MissingRequired(a, installed)
	if len(miss) != 1 || miss[0] != "files" {
		t.Fatalf("missing=%v", miss)
	}
	g := GraphFrom([]Manifest{
		{ID: "a", Requires: []Requirement{{ID: "b"}}},
		{ID: "b", Requires: []Requirement{{ID: "a"}}},
	})
	cyc := Cycle(g)
	if len(cyc) < 2 {
		t.Fatalf("expected cycle, got %v", cyc)
	}
	dag := GraphFrom([]Manifest{
		{ID: "projects", Requires: []Requirement{{ID: "files"}}},
		{ID: "files"},
	})
	if c := Cycle(dag); c != nil {
		t.Fatalf("dag has cycle %v", c)
	}
}
