package plugin

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

const (
	MetadataContractName     = "bd.contract.name"
	MetadataContractRevision = "bd.contract.revision"
	MetadataContractSchema   = "bd.contract.schema"
)

var descriptorDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func descriptorMetadata(d Descriptor, point string) map[string]string {
	if point == "" {
		return nil
	}
	return map[string]string{MetadataContractName: d.name, MetadataContractRevision: strconv.FormatUint(uint64(d.rev), 10), MetadataContractSchema: d.schemaHash}
}

// BindDiscoveredCommand binds the exact concrete descriptor advertised by an
// admitted provider's ServeAtPoint service. It never retargets a host hash.
// Before calling, the host must resolve and authenticate the live service owner
// and verify current manifest/grant/generation admission. Discovery metadata is
// compatibility information, NOT identity or authority. Missing/mismatched
// metadata fails closed; payload validation remains mandatory in both directions.
func BindDiscoveredCommand[Req interface{ Validate() error }, Resp interface{ Validate() error }](service bus.ServiceInfo, providerID string, point Point, operation string, revision uint32) (Command[Req, Resp], error) {
	var zero Command[Req, Resp]
	if _, ok := idSegment("provider id", providerID); !ok {
		return zero, fmt.Errorf("invalid provider id")
	}
	if _, ok := idSegment("operation", operation); !ok {
		return zero, fmt.Errorf("invalid provider operation")
	}
	if providerID == "gateway" || providerID == "host" || !IsKnownPoint(string(point)) || revision == 0 {
		return zero, fmt.Errorf("invalid provider contract identity")
	}
	want := fmt.Sprintf("svc.%s.%s.v%d.%s", providerID, point, revision, operation)
	if service.Name != want || service.Version != serviceVersion(revision) || len(service.Endpoints) != 1 {
		return zero, fmt.Errorf("discovered service identity mismatch")
	}
	e := service.Endpoints[0]
	if e.Name != want || string(e.Subject) != want || e.Point != string(point) {
		return zero, fmt.Errorf("discovered endpoint identity mismatch")
	}
	meta := service.Metadata
	if meta[MetadataContractName] != want || meta[MetadataContractRevision] != strconv.FormatUint(uint64(revision), 10) || !descriptorDigest.MatchString(meta[MetadataContractSchema]) {
		return zero, fmt.Errorf("missing or mismatched provider descriptor metadata")
	}
	return NewValidatedCommand[Req, Resp](want, revision, meta[MetadataContractSchema], e.Subject), nil
}
