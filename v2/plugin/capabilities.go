package plugin

import "strings"

// Side-effect capabilities. The host enacts them. A plugin declares the id.
// That id is the enablement: the consent screen lists it, and the call that
// turns the feature on is denied unless the host granted it.
//
// Requires does not copy these. A peer's capabilities are shown on the
// install sheet as that peer's own lines. They are not inherited.
const (
	CapabilityCredentialSecret  = "credential.secret"
	CapabilityEgressProvider    = "egress.provider"
	CapabilityProcessSupervised = "process.supervised"
	CapabilityIngressPublish    = "ingress.publish"
)

// ConsentCapability is one line on the install sheet. Sentence is the host's
// text. A plugin does not supply it.
type ConsentCapability struct {
	ID       string
	Sentence string
}

// CapabilityCatalog is the closed set, in the order the consent screen shows
// it. Enablement and the sheet both read this list. An id that is not here is
// not a capability, even if a manifest writes it.
func CapabilityCatalog() []ConsentCapability {
	return []ConsentCapability{
		{ID: CapabilityCredentialSecret, Sentence: "Stores a secret for this plugin. The host holds the value. The plugin receives a handle, not the secret."},
		{ID: CapabilityEgressProvider, Sentence: "Calls a provider API on the host allowlist, using the secret this plugin stored. The plugin names an operation, not a URL."},
		{ID: CapabilityProcessSupervised, Sentence: "Runs one program from the host allowlist, outside the plugin sandbox. The plugin does not choose the executable."},
		{ID: CapabilityIngressPublish, Sentence: "Publishes a local origin through that program. The operator confirms the destination. The plugin cannot substitute another."},
	}
}

// CapabilityIDs returns the closed ids, in catalog order.
func CapabilityIDs() []string {
	cat := CapabilityCatalog()
	out := make([]string, len(cat))
	for i, c := range cat {
		out[i] = c.ID
	}
	return out
}

// ConsentCapabilities returns the install-sheet lines for the capabilities
// this manifest declares, in catalog order. Unknown ids are omitted; Validate
// reports them and the package is not installed. Duplicate declarations
// produce one line.
func ConsentCapabilities(m Manifest) []ConsentCapability {
	declared := map[string]bool{}
	for _, id := range m.Capabilities {
		declared[strings.TrimSpace(id)] = true
	}
	var out []ConsentCapability
	for _, line := range CapabilityCatalog() {
		if declared[line.ID] {
			out = append(out, line)
		}
	}
	return out
}

// CapabilityEnabled reports whether the host grant turns id on. granted is
// the subset the operator consented, already intersected with the declaration.
// An id outside the catalog is never enabled.
func CapabilityEnabled(granted []string, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || !knownCapability(id) {
		return false
	}
	for _, g := range granted {
		if strings.TrimSpace(g) == id {
			return true
		}
	}
	return false
}

func knownCapability(id string) bool {
	for _, line := range CapabilityCatalog() {
		if line.ID == id {
			return true
		}
	}
	return false
}
