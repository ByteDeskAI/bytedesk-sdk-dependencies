package plugin

// Contribution roles name function, never position (gateway ADR 0026 D1).
// The shell owns the role-to-region map; eligibility travels with vocabulary.
const (
	SlotDefaultView      = "default-view"
	SlotMainNavigation   = "main-navigation"
	SlotSubNavigation    = "sub-navigation"
	SlotPrimaryAction    = "primary-action"
	SlotSecondaryActions = "secondary-actions"
	SlotStatusIndicator  = "status-indicator"
	SlotObjectActions    = "object-actions"
	SlotLauncher         = "launcher"
	SlotSettings         = "settings"
	SlotCommand          = "command"
)

// ContributionEligibility describes admission, not execution authorization.
// The host still enforces scopes, availability and command permissions.
type ContributionEligibility string

const (
	ContributionInstalledAllowed ContributionEligibility = "installed-allowed"
	ContributionConsentRequired  ContributionEligibility = "consent-required"
	ContributionCompiledOnly     ContributionEligibility = "compiled-only"
)

// ContributionRole keeps each semantic slot beside its installed-artifact policy.
type ContributionRole struct {
	Slot        string
	Eligibility ContributionEligibility
}

// ContributionRoles returns a fresh table: callers cannot mutate shared policy.
// Trusted chrome and operator-tool roles require explicit per-point consent.
func ContributionRoles() []ContributionRole {
	return []ContributionRole{
		{SlotDefaultView, ContributionInstalledAllowed},
		{SlotMainNavigation, ContributionConsentRequired},
		{SlotSubNavigation, ContributionConsentRequired},
		{SlotPrimaryAction, ContributionConsentRequired},
		{SlotSecondaryActions, ContributionConsentRequired},
		{SlotStatusIndicator, ContributionConsentRequired},
		{SlotObjectActions, ContributionInstalledAllowed},
		{SlotLauncher, ContributionConsentRequired},
		{SlotSettings, ContributionInstalledAllowed},
		{SlotCommand, ContributionConsentRequired},
	}
}

func ContributionRoleFor(slot string) (ContributionRole, bool) {
	for _, role := range ContributionRoles() {
		if role.Slot == slot {
			return role, true
		}
	}
	return ContributionRole{}, false
}

// ContributionRoleAllowed accepts only host-owned trust inputs. compiledIn is
// verified build provenance, NEVER Manifest.Role, publisher name or a signature.
// pointConsent is an explicit operator grant for this exact plugin and slot;
// broad module trust does not qualify. The host must re-evaluate on revocation.
// Unknown roles fail closed even for compiled-in code or a supplied grant.
func ContributionRoleAllowed(slot string, compiledIn, pointConsent bool) bool {
	role, ok := ContributionRoleFor(slot)
	if !ok {
		return false
	}
	if compiledIn {
		return true
	}
	switch role.Eligibility {
	case ContributionInstalledAllowed:
		return true
	case ContributionConsentRequired:
		return pointConsent
	default:
		return false
	}
}
