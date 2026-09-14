package plugin

import "testing"

func TestContributionRoleEligibility(t *testing.T) {
	want := map[string]bool{SlotDefaultView: true, SlotMainNavigation: false, SlotSubNavigation: false, SlotPrimaryAction: false, SlotSecondaryActions: false, SlotStatusIndicator: false, SlotObjectActions: true, SlotLauncher: false, SlotSettings: true, SlotCommand: false}
	roles := ContributionRoles()
	if len(roles) != len(want) {
		t.Fatal("role vocabulary and eligibility differ")
	}
	seen := map[string]bool{}
	for _, role := range roles {
		allowed, ok := want[role.Slot]
		if !ok || seen[role.Slot] {
			t.Fatalf("unexpected or duplicate role %+v", role)
		}
		seen[role.Slot] = true
		for _, compiled := range []bool{false, true} {
			for _, consent := range []bool{false, true} {
				if got := ContributionRoleAllowed(role.Slot, compiled, consent); got != (compiled || consent || allowed) {
					t.Fatalf("%s compiled=%v consent=%v allowed=%v", role.Slot, compiled, consent, got)
				}
			}
		}
	}
	for _, unknown := range []string{"", "toolbar", "MAIN-NAVIGATION", "*", "unknown"} {
		for _, compiled := range []bool{false, true} {
			for _, consent := range []bool{false, true} {
				if ContributionRoleAllowed(unknown, compiled, consent) {
					t.Fatalf("accepted unknown %q", unknown)
				}
			}
		}
	}
	roles[1].Eligibility = ContributionInstalledAllowed
	if ContributionRoleAllowed(SlotMainNavigation, false, false) {
		t.Fatal("returned table mutated authority")
	}
}
