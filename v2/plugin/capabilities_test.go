package plugin

import (
	"strings"
	"testing"
)

func TestConsentCapabilitiesUsesCatalogSentences(t *testing.T) {
	m := Manifest{Capabilities: []string{
		CapabilityIngressPublish,
		"  " + CapabilityCredentialSecret + "  ",
		CapabilityCredentialSecret,
		"not-a-capability",
	}}
	got := ConsentCapabilities(m)
	if len(got) != 2 {
		t.Fatalf("ConsentCapabilities = %+v, want credential then ingress", got)
	}
	if got[0].ID != CapabilityCredentialSecret || got[1].ID != CapabilityIngressPublish {
		t.Fatalf("order = %s, %s, want catalog order", got[0].ID, got[1].ID)
	}
	if got[0].Sentence == "" || got[1].Sentence == "" {
		t.Fatal("consent line missing the host sentence")
	}
}

func TestCapabilityEnabledIsTheGrant(t *testing.T) {
	granted := []string{" " + CapabilityEgressProvider + " "}
	if !CapabilityEnabled(granted, CapabilityEgressProvider) {
		t.Fatal("consented capability was not enabled")
	}
	if CapabilityEnabled(granted, CapabilityIngressPublish) {
		t.Fatal("undeclared capability was enabled")
	}
	if CapabilityEnabled([]string{"made-up"}, "made-up") {
		t.Fatal("an id outside the catalog was enabled")
	}
	if CapabilityEnabled(nil, CapabilityCredentialSecret) {
		t.Fatal("empty grant enabled a capability")
	}
}

func TestUnknownCapabilityRefused(t *testing.T) {
	m := fullManifest()
	m.Capabilities = []string{"plugin.tunnel"}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "BDP2174") {
		t.Fatalf("Validate = %v, want BDP2174", err)
	}
}
