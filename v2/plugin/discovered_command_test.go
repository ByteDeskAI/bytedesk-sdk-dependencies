package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
	"testing"
)

func discoveredFixture() bus.ServiceInfo {
	name := "svc.demo.ai.decision.v1.start"
	digest := sha256.Sum256([]byte("descriptor test fixture"))
	return bus.ServiceInfo{Name: name, Version: "1.0.0", Endpoints: []bus.EndpointInfo{{Name: name, Subject: bus.Subject(name), Point: string(PointAIDecision)}}, Metadata: map[string]string{MetadataContractName: name, MetadataContractRevision: "1", MetadataContractSchema: hex.EncodeToString(digest[:])}}
}
func TestDiscoveredCommandRequiresExactDescriptorMetadata(t *testing.T) {
	v := discoveredFixture()
	if _, err := BindDiscoveredCommand[checkedValue, checkedValue](v, "demo", PointAIDecision, "start", 1); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*bus.ServiceInfo){"absent": func(v *bus.ServiceInfo) { v.Metadata = nil }, "name": func(v *bus.ServiceInfo) { v.Metadata[MetadataContractName] = "svc.other.ai.decision.v1.start" }, "revision": func(v *bus.ServiceInfo) { v.Metadata[MetadataContractRevision] = "2" }, "hash": func(v *bus.ServiceInfo) { v.Metadata[MetadataContractSchema] = "not-a-hash" }, "endpoint": func(v *bus.ServiceInfo) { v.Endpoints[0].Subject = "svc.other.ai.decision.v1.start" }, "point": func(v *bus.ServiceInfo) { v.Endpoints[0].Point = "acp.provider" }, "version": func(v *bus.ServiceInfo) { v.Version = "2.0.0" }} {
		t.Run(name, func(t *testing.T) {
			v := discoveredFixture()
			mutate(&v)
			if _, err := BindDiscoveredCommand[checkedValue, checkedValue](v, "demo", PointAIDecision, "start", 1); err == nil {
				t.Fatal("invalid discovery accepted")
			}
		})
	}
}
