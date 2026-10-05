package projectmanagement

import (
	"strings"
	"testing"
)

func testScope() Scope {
	return Scope{ProjectID: "project-1", EnvironmentID: "dev", TenantID: "tenant-1", BindingID: "binding-1", BindingRevision: "rev-1", ProviderGeneration: "generation-1", CapabilityRevision: "catalog-1"}
}
func testMutation() Mutation {
	return Mutation{OperationID: "operation-1", SourceBindingRevision: "rev-1", Origin: "terminal", CreatedAt: "2026-10-05T12:00:00Z"}
}
func testRef() ResourceRef {
	return ResourceRef{ProviderID: "tasks", InstallationID: "tenant/install", Kind: "item", ID: "opaque/path?id=1"}
}
func testCapabilities() Capabilities {
	return Capabilities{Revision: "catalog-1", Operations: []Capability{{Operation: "item.patch", Revision: 1, Supported: true, Authorized: true, Concurrency: "revision", Offline: "queue"}}, Transport: Transport{Kind: "mcp", CatalogRevision: "remote-1", Discovery: "dynamic"}, Recovery: RecoveryPolicy{Idempotency: "durable", LookupByOperationID: true, ReconcileBeforeReplay: true}}
}

func TestProviderInvocationRequiresExactFence(t *testing.T) {
	r := ProviderDescribeRequest{Invocation: Invocation{ID: "invocation", Scope: testScope(), Actor: ActingContext{ActorID: "actor", ConnectionID: "connection", ExecutionMode: "interactive"}, ExpiresAt: "2026-10-05T12:01:00Z"}, Request: DescribeRequest{Scope: testScope()}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Scope){"project": func(s *Scope) { s.ProjectID = "other" }, "environment": func(s *Scope) { s.EnvironmentID = "prod" }, "tenant": func(s *Scope) { s.TenantID = "other" }, "binding": func(s *Scope) { s.BindingID = "other" }, "revision": func(s *Scope) { s.BindingRevision = "rev-2" }, "generation": func(s *Scope) { s.ProviderGeneration = "new" }, "capability": func(s *Scope) { s.CapabilityRevision = "new" }} {
		t.Run(name, func(t *testing.T) {
			q := r
			change(&q.Request.Scope)
			if q.Validate() == nil {
				t.Fatal("accepted mismatched authority")
			}
		})
	}
}

func TestCapabilitySupportAuthorizationAndOfflineAreIndependent(t *testing.T) {
	s, c := testScope(), testCapabilities()
	if err := RequireOperation(s, c, "item.patch", true); err != nil {
		t.Fatal(err)
	}
	c.Operations[0].Authorized = false
	if RequireOperation(s, c, "item.patch", true) == nil {
		t.Fatal("accepted unauthorized operation")
	}
	c = testCapabilities()
	c.Revision = "new"
	if RequireOperation(s, c, "item.patch", false) == nil {
		t.Fatal("accepted stale capabilities")
	}
	for _, op := range []string{"claim.acquire", "claim.renew", "agent.complete", "permissions.set", "admin.permissions", "public-link.enable"} {
		c = testCapabilities()
		c.Operations[0].Operation = op
		if RequireOperation(s, c, op, true) == nil {
			t.Fatalf("queued authority change %s", op)
		}
		if err := RequireOperation(s, c, op, false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCachedReadCapabilityCannotQueueMutation(t *testing.T) {
	c := testCapabilities()
	c.Operations[0].Offline = "read-cache"
	if RequireOperation(testScope(), c, "item.patch", true) == nil {
		t.Fatal("cached-read policy admitted an offline mutation")
	}
	if err := RequireOperation(testScope(), c, "item.patch", false); err != nil {
		t.Fatal("online operation lost its independent authorization:", err)
	}
}

func TestLocalReceiptCannotClaimRemoteAcceptance(t *testing.T) {
	r := Receipt{OperationID: "operation-1", State: "saved-local", ObservedAt: "2026-10-05T12:00:00Z"}
	if err := r.ValidateFor(testMutation()); err != nil {
		t.Fatal(err)
	}
	r.RemoteAccepted = true
	if r.Validate() == nil {
		t.Fatal("saved-local claimed remote acceptance")
	}
	if r.ValidateFor(testMutation()) == nil {
		t.Fatal("request correspondence bypassed receipt semantics")
	}
	r.RemoteAccepted = false
	r.State = "unknown"
	if r.Validate() == nil {
		t.Fatal("unknown outcome without recovery")
	}
	r.RecoveryToken = "lookup-1"
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.OperationID = "other"
	if r.ValidateFor(testMutation()) == nil {
		t.Fatal("accepted another operation receipt")
	}
	r.OperationID = "operation-1"
	r.State = "conflicted"
	if r.Validate() == nil {
		t.Fatal("conflict discarded data")
	}
	r.Conflict = &Conflict{BaseRevision: "before", RemoteRevision: "after", PayloadRef: "both-sides", ResolutionOptions: []string{"review"}}
	if err := Validate(r); err != nil {
		t.Fatal(err)
	}
}

func TestReplayDoesNotOutliveRemoteReceipts(t *testing.T) {
	p := RecoveryPolicy{Idempotency: "windowed", DeduplicationSeconds: 86400, ReceiptRetentionSeconds: 86400, LookupByOperationID: true, ReconcileBeforeReplay: true}
	if err := p.SupportsReplay(60); err != nil {
		t.Fatal(err)
	}
	if p.SupportsReplay(86400) == nil {
		t.Fatal("expired remote receipt allowed replay")
	}
	p.Idempotency = "durable"
	p.DeduplicationSeconds = 0
	p.ReceiptRetentionSeconds = 0
	if err := p.SupportsReplay(86400 * 365); err != nil {
		t.Fatal(err)
	}
	p.LookupByOperationID = false
	if p.SupportsReplay(1) == nil {
		t.Fatal("unknown outcome retried without lookup")
	}
}

func TestOfflineRecordPreservesAuthorityAndDisallowsQueuedClaims(t *testing.T) {
	o := OfflineOperation{Scope: testScope(), Actor: ActingContext{ActorID: "actor", ConnectionID: "connection", ExecutionMode: "interactive"}, Mutation: testMutation(), Operation: "item.patch", PayloadRef: "payload-1", PayloadChecksum: "sha256:original", Receipt: Receipt{OperationID: "operation-1", State: "saved-local", ObservedAt: "2026-10-05T12:00:00Z"}, PersistedAt: "2026-10-05T12:00:00Z", SessionID: "session-1", NativeID: "todo-1"}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	o.Operation = "claim.acquire"
	if o.Validate() == nil {
		t.Fatal("queued claim")
	}
	o.Operation = "item.patch"
	o.Mutation.SourceBindingRevision = "old"
	if o.Validate() == nil {
		t.Fatal("queued stale binding")
	}
	o.Mutation = testMutation()
	o.NativeID = ""
	if o.Validate() == nil {
		t.Fatal("accepted incomplete native mapping")
	}
}

func TestMigrationRequiresIdentityLosslessnessAndPausedWrites(t *testing.T) {
	m := Migration{ID: "migration-1", Source: testScope(), Destination: testScope(), State: "verified", SnapshotRevision: "snapshot-1", InventoryChecksum: "sha256:inventory", Lossless: true, WritesPaused: true}
	m.Destination.BindingID = "destination"
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Migration){"lossy": func(m *Migration) { m.Lossless = false }, "writers": func(m *Migration) { m.WritesPaused = false }, "project": func(m *Migration) { m.Destination.ProjectID = "another" }, "environment": func(m *Migration) { m.Destination.EnvironmentID = "prod" }, "tenant": func(m *Migration) { m.Destination.TenantID = "another" }} {
		t.Run(name, func(t *testing.T) {
			q := m
			change(&q)
			if q.Validate() == nil {
				t.Fatal("accepted unsafe migration")
			}
		})
	}
}

func TestVerifiedMigrationRequiresRecoverableEntityRecords(t *testing.T) {
	ref := testRef()
	m := Migration{ID: "migration-1", Source: testScope(), Destination: testScope(), State: "verified", SnapshotRevision: "snapshot-1", InventoryChecksum: "sha256:inventory", Lossless: true, WritesPaused: true, Entities: []MigrationEntity{{Source: ref, Destination: &ref}}}
	r := MigrationRequest{Scope: testScope(), Mutation: testMutation(), Action: "verify", Migration: m, Page: PageRequest{Limit: 1}}
	if r.Validate() == nil {
		t.Fatal("verified entity discarded original revision, checksum, and record")
	}
}

func TestMigrationPreservesDuplicateNativeIDsAsDistinctSourceRecords(t *testing.T) {
	ref := testRef()
	destination := ref
	destination.ProviderID = "new-tasks"
	entity := MigrationEntity{SourceRecordID: "tasks/TM-001.md", Source: ref, Destination: &destination, Revision: "source-revision", Checksum: "sha256:original", Record: &Transfer{ID: "transfer-1", Resource: ref, Direction: "download", Method: "mcp-resource", URI: "resource://migration/record-1", ExpiresAt: "2026-10-05T12:00:00Z", Size: 123, MediaType: "application/octet-stream", Checksum: "sha256:original", Provenance: "original source file", Verified: true}}
	m := Migration{ID: "migration-1", Source: testScope(), Destination: testScope(), State: "verified", SnapshotRevision: "snapshot-1", InventoryChecksum: "sha256:inventory", Lossless: true, WritesPaused: true, Entities: []MigrationEntity{entity, entity}}
	m.Entities[1].SourceRecordID = "tasks/TM-001-historical.md"
	validate := func(m Migration) error {
		return (MigrationRequest{Scope: testScope(), Mutation: testMutation(), Action: "verify", Migration: m, Page: PageRequest{Limit: 2}}).Validate()
	}
	if err := validate(m); err != nil {
		t.Fatal("distinct original records with the same native ID must survive:", err)
	}
	for name, change := range map[string]func(*MigrationEntity){
		"missing source record":   func(e *MigrationEntity) { e.SourceRecordID = "" },
		"duplicate source record": func(e *MigrationEntity) { e.SourceRecordID = entity.SourceRecordID },
		"missing revision":        func(e *MigrationEntity) { e.Revision = "" },
		"missing checksum":        func(e *MigrationEntity) { e.Checksum = "" },
		"missing bytes":           func(e *MigrationEntity) { e.Record = nil },
		"unverified bytes":        func(e *MigrationEntity) { e.Record.Verified = false },
		"different bytes":         func(e *MigrationEntity) { e.Record.Checksum = "sha256:changed" },
		"different source":        func(e *MigrationEntity) { e.Record.Resource.ID = "different" },
		"missing provenance":      func(e *MigrationEntity) { e.Record.Provenance = "" },
		"unmapped":                func(e *MigrationEntity) { e.Destination = nil },
		"failure": func(e *MigrationEntity) {
			e.Problems = []Problem{{Code: "validation", Message: "unpreserved data", Retry: "never"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			q := m
			q.Entities = append([]MigrationEntity(nil), m.Entities...)
			record := *q.Entities[1].Record
			q.Entities[1].Record = &record
			change(&q.Entities[1])
			if validate(q) == nil {
				t.Fatal("verified migration accepted incomplete or inconsistent preservation")
			}
		})
	}
}

func TestTransfersContainNoExecutableCommandsOrCredentialURLs(t *testing.T) {
	x := Transfer{ID: "transfer-1", Resource: testRef(), Direction: "download", Method: "authorized-https", URI: "https://bytes.example.test/object", ExpiresAt: "2026-10-05T12:00:00Z", Size: 123, MediaType: "application/octet-stream", Checksum: "sha256:bytes", Provenance: "item-attachment"}
	if err := x.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, uri := range []string{"https://user:password@bytes.example.test/object", "https://bytes.example.test/object?signature=secret", "https://bytes.example.test/object#secret", "http://bytes.example.test/object", "file:///tmp/object", "sh:download"} {
		q := x
		q.URI = uri
		if q.Validate() == nil {
			t.Fatalf("accepted unsafe transfer %s", uri)
		}
	}
	x.Method = "mcp-resource"
	x.URI = "resource://attachments/123"
	if err := x.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestNestedWireValidationAndFieldTypes(t *testing.T) {
	for _, f := range []FieldValue{{Name: "custom:score", Type: "number", JSON: "12.5"}, {Name: "custom:status", Type: "string", JSON: `"open"`}, {Name: "custom:unknown", Type: "object", JSON: `{"source":true}`}} {
		if err := f.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []FieldValue{{Name: "custom:score", Type: "number", JSON: `"12"`}, {Name: "custom:flag", Type: "boolean", JSON: "null"}, {Name: "", Type: "object", JSON: `{}`}, {Name: "custom:bad", Type: "object", JSON: `{`}} {
		if f.Validate() == nil {
			t.Fatal("accepted malformed typed extension")
		}
	}
	r := ChangesRequest{Scope: testScope(), Page: PageRequest{Limit: 201}}
	if r.Validate() == nil {
		t.Fatal("accepted unbounded page")
	}
	errResult := DescribeResult{Problem: &Problem{Code: "unavailable", Message: strings.Repeat("x", 70000), Retry: "after-delay"}}
	if errResult.Validate() == nil {
		t.Fatal("error response bypassed wire limit")
	}
	if err := testRef().Validate(); err != nil {
		t.Fatal("native identifier must remain opaque:", err)
	}
}
