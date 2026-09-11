package plugin

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// RuntimeStatus describes host-observed availability, not package metadata.
// Generation is an opaque string so transports cannot lose integer precision.
// Available is authoritative; a health state alone never grants access.
type RuntimeStatus struct {
	ID            string `json:"id" bd:"public"`
	Installed     bool   `json:"installed" bd:"public"`
	DesiredState  string `json:"desiredState" bd:"public"`
	ObservedState string `json:"observedState" bd:"public"`
	Available     bool   `json:"available" bd:"public"`
	Generation    string `json:"generation" bd:"public"`
	Reason        string `json:"reason,omitempty" bd:"public"`
}

// RuntimeSnapshot is published atomically with contributions at Revision.
// Epoch changes across host restarts; revisions are comparable only within it.
type RuntimeSnapshot struct {
	Epoch    string          `json:"epoch" bd:"public"`
	Revision uint64          `json:"revision,string" bd:"public"`
	Plugins  []RuntimeStatus `json:"plugins" bd:"public"`
}

const (
	// DesiredUnknown reports unreadable or unverified intent. It never grants availability.
	DesiredUnknown     = "unknown"
	DesiredAbsent      = "absent"
	DesiredDisabled    = "disabled"
	DesiredEnabled     = "enabled"
	OperationPending   = "pending"
	OperationCompleted = "completed"
	OperationFailed    = "failed"
)

// LifecycleOperation remains queryable when draining or removal is incomplete.
// A pending/failed operation must not be reported as successful removal.
type LifecycleOperation struct {
	ID         string `json:"id" bd:"public"`
	PluginID   string `json:"pluginId" bd:"public"`
	Action     string `json:"action" bd:"public"`
	State      string `json:"state" bd:"public"`
	Generation string `json:"generation,omitempty" bd:"public"`
	Reason     string `json:"reason,omitempty" bd:"public"`
}

// ActivationChecker is optional and runs after Start, before publication.
// Failure rolls the candidate generation back. Unlike Readier, this is an
// admission condition, not a degraded-health report for an available plugin.
type ActivationChecker interface {
	CheckActivation(context.Context) error
}

// Permissions requests authority. Only host policy can grant it. Topic names
// are exact; wildcard interpretation is deliberately not part of this contract.
// StateDir always addresses the caller's own state, regardless of permissions.
type Permissions struct {
	Publish   []string `json:"publish,omitempty" bd:"public"`
	Subscribe []string `json:"subscribe,omitempty" bd:"public"`
	Request   []string `json:"request,omitempty" bd:"public"`
}

// ProtocolRequirements declares required features of the host protocol.
// Major zero is the legacy protocol, with no new required features.
type ProtocolRequirements struct {
	Major    uint32   `json:"major" bd:"public"`
	Required []string `json:"required,omitempty" bd:"public"`
	// Hooks lists the lifecycle hooks the plugin implements. ServePlugin fills
	// it with DeclaredHooks; it is advertisement, not a requirement.
	Hooks []string `json:"hooks,omitempty" bd:"public"`
}

// HostCapabilities is scoped to one admitted plugin generation. Features
// describe protocol support; Grants describe authority. Neither implies the other.
type HostCapabilities struct {
	Major      uint32      `json:"major" bd:"public"`
	Features   []string    `json:"features" bd:"public"`
	PluginID   string      `json:"pluginId" bd:"public"`
	Generation string      `json:"generation" bd:"public"`
	Grants     Permissions `json:"grants" bd:"public"`
	// Hooks is the subset of the requested hooks the host will call over the
	// lifecycle hook verb. A plugin must not also run those hooks itself.
	Hooks []string `json:"hooks,omitempty" bd:"public"`
}

// Lifecycle hooks a plugin advertises through negotiation. Across a process
// boundary the host cannot type-assert, so it calls each acknowledged hook over
// one generic verb instead. Every hook runs on entry or reports health; there
// is deliberately no stop hook, so a plugin cannot declare an exit veto.
const (
	HookActivationCheck = "activation.check"
	HookReady           = "ready"
	HookHealth          = "health"

	// LifecycleHookCommand is the one host-to-plugin verb for acknowledged
	// hooks: POST /cmd.lifecycle.v1.hook {"hook":"<name>"} answered
	// {"error":"<reason or empty>","result":<hook result or null>}.
	LifecycleHookCommand = "cmd.lifecycle.v1.hook"
)

// DeclaredHooks returns the lifecycle hooks p implements, found by local type
// assertion, in a stable order.
func DeclaredHooks(p any) []string {
	var hooks []string
	if _, ok := p.(ActivationChecker); ok {
		hooks = append(hooks, HookActivationCheck)
	}
	if _, ok := p.(Readier); ok {
		hooks = append(hooks, HookReady)
	}
	if _, ok := p.(HealthContributor); ok {
		hooks = append(hooks, HookHealth)
	}
	return hooks
}

func validateHooks(major uint32, hooks []string) error {
	if major == 0 && len(hooks) != 0 {
		return fmt.Errorf("protocol.hooks needs an explicit major version")
	}
	if err := validateExactNames("protocol.hooks", hooks); err != nil {
		return err
	}
	for _, hook := range hooks {
		switch hook {
		case HookActivationCheck, HookReady, HookHealth:
		default:
			return fmt.Errorf("protocol.hooks: unknown lifecycle hook %q", hook)
		}
	}
	return nil
}

// Negotiator is an optional Host interface; legacy Host implementations keep
// their method set. Negotiation must complete before a plugin uses new features.
type Negotiator interface {
	Negotiate(context.Context, ProtocolRequirements) (HostCapabilities, error)
}

// CheckProtocol fails closed for an unsupported required feature or version.
// A legacy request can never opt into new features by omitting its version.
func CheckProtocol(have HostCapabilities, need ProtocolRequirements) error {
	if err := validateExactNames("protocol.required", need.Required); err != nil {
		return err
	}
	if err := validateHooks(need.Major, need.Hooks); err != nil {
		return err
	}
	for _, hook := range have.Hooks {
		if !slices.Contains(need.Hooks, hook) {
			return fmt.Errorf("host acknowledged undeclared lifecycle hook %q", hook)
		}
	}
	if need.Major == 0 {
		if len(need.Required) != 0 {
			return fmt.Errorf("required protocol features need an explicit major version")
		}
		return nil
	}
	if need.Major != have.Major {
		return fmt.Errorf("plugin protocol major %d is unsupported (host %d)", need.Major, have.Major)
	}
	for _, feature := range need.Required {
		if feature == "" || !slices.Contains(have.Features, feature) {
			return fmt.Errorf("required plugin protocol feature %q is unsupported", feature)
		}
	}
	return nil
}

const (
	ProtocolMajor             = 1
	FeatureRuntimeSnapshot    = "runtime.snapshot.v1"
	FeatureScopedHost         = "host.scoped.v1"
	FeatureActivationCheck    = "activation.check.v1"
	FeatureLifecycleHooks     = "lifecycle.hooks.v1"
	FeatureShellContributions = "ui.contributions.v1"
	FeatureDocumentPaths      = "ui.document-paths.v1"
	FeatureUIModuleMount      = "ui.mount.v1"
)

// UIContribution names the role a contribution plays in the shell. The host
// resolves PanelID and Command within the owner's declared contributions and
// granted authority.
// Priority selects a default view; ties resolve by owner ID then contribution ID.
type UIContribution struct {
	ID       string `json:"id" bd:"public"`
	Slot     string `json:"slot" bd:"public"`
	PanelID  string `json:"panelId,omitempty" bd:"public"`
	Command  string `json:"command,omitempty" bd:"public"`
	Label    string `json:"label,omitempty" bd:"public"`
	Icon     string `json:"icon,omitempty" bd:"public"`
	Priority int    `json:"priority,omitempty" bd:"public"`
}

// Contribution roles (gateway ADR 0026 D1). A slot names what a contribution
// is, never where it sits: the shell owns the role-to-region map, so a redesign
// changes one mapping and no plugin. Settings and command already named a
// function and keep their names; SlotSettings is the settings-section role.
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

func (m Manifest) validateRuntimeContract() error {
	if p := m.Protocol; p != nil {
		if p.Major == 0 && len(p.Required) != 0 {
			return fmt.Errorf("protocol.required needs an explicit major version")
		}
		if err := validateExactNames("protocol.required", p.Required); err != nil {
			return err
		}
		if err := validateHooks(p.Major, p.Hooks); err != nil {
			return err
		}
	}
	if p := m.Permissions; p != nil {
		for _, list := range []struct {
			name   string
			values []string
		}{
			{"permissions.publish", p.Publish}, {"permissions.subscribe", p.Subscribe}, {"permissions.request", p.Request},
		} {
			if err := validateExactNames(list.name, list.values); err != nil {
				return err
			}
		}
	}
	seen := map[string]bool{}
	for _, item := range m.UI {
		if err := validateIDSegment("ui.id", item.ID); err != nil {
			return err
		}
		if seen[item.ID] {
			return fmt.Errorf("duplicate ui.id %q", item.ID)
		}
		seen[item.ID] = true
		switch item.Slot {
		case SlotDefaultView, SlotMainNavigation, SlotSubNavigation, SlotPrimaryAction, SlotSecondaryActions,
			SlotStatusIndicator, SlotObjectActions, SlotLauncher, SlotSettings:
			if item.PanelID == "" || item.Command != "" {
				return fmt.Errorf("ui slot %q requires only panelId", item.Slot)
			}
			found := false
			for _, panel := range m.Panels {
				if panel.ID == item.PanelID {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("ui panelId %q is not owned by this manifest", item.PanelID)
			}
		case SlotCommand:
			if item.Command == "" || item.PanelID != "" {
				return fmt.Errorf("ui command requires only command")
			}
			if err := validateExactNames("ui.command", []string{item.Command}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown ui slot %q", item.Slot)
		}
	}
	return nil
}

func validateExactNames(label string, values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "*?\r\n\t ") {
			return fmt.Errorf("%s must contain nonempty exact names", label)
		}
		if seen[value] {
			return fmt.Errorf("%s contains duplicate %q", label, value)
		}
		seen[value] = true
	}
	return nil
}
