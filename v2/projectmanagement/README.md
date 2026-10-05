# Project Management contracts

Project Management has two independent provider slots: `project.tasks` and
`project.knowledge`. The SDK owns their wire models and validation. The host
owns provider selection, identity, credentials, lifecycle, durable operations,
and admission. This release adds contracts; it does not implement a provider,
an offline database, or remote product acceptance.

Use `projecttasks` and `projectknowledge` from UI adapters, CLI, MCP, hooks, and
native terminal bridges. Both packages alias shared types from
`projectmanagement`; do not copy these structs into a host or plugin. A
Project Management container calls the host facade. It neither discovers nor
invokes a peer plugin. A provider may call a remote product's MCP interface;
that is an external transport, not a Gateway plugin-to-plugin call.

## Commands and provider discovery

The host command prefixes are `cmd.gateway.project-tasks.v1` and
`cmd.gateway.project-knowledge.v1`. Both expose:

| Wire suffix | Go descriptor | Purpose |
| --- | --- | --- |
| `describe` | `Describe` | Capabilities, current authorization, shared binding |
| `schema` | `Schema` | Current operation fields, types, formats, allowed actions |
| `query` | `Search` | Common filters, opaque cursor, optional native query |
| `read` | `Read` | Resource and explicitly requested related data |
| `write` | `Write` | Typed action union, mutation identity and expected revision |
| `changes` | `Changes` | Resumable changes where the provider supports them |
| `recover` | `Recover` | Reconcile an operation with an uncertain outcome |
| `transfer` | `ManageTransfer` | Prepare, complete, or cancel authorized byte transfer |
| `migration` | `Migrate` | Plan, export, import, verify, or abort lossless migration |

Tasks also exposes `claim` (`Claim`) for server-issued exclusive leases.
Mutation actions have separate capability entries; support for `write` does
not authorize every action. For example, `workflow.transition`,
`agent.complete`, and `admin.permissions` are separate decisions.

Task records cover projects, typed work items, workflow actions, hierarchy,
links, dependencies, knowledge references, comments, attachments, history,
watchers, worklogs, boards, backlogs, iterations, rank, releases, components,
saved views, custom fields, and authorized administration. Agent work adds
goals, requirements, acceptance evidence, readiness reasons, dispatch/pool/
worktree references, review, claims, and native mappings. Workflow status
changes use discovered actions rather than a field patch.

Knowledge records cover spaces, typed content, native rich bodies,
draft/published states, versions and diffs, ordered hierarchy, comments and
anchors, reactions, watchers, inline tasks, labels, properties, templates,
attachments, effective permissions and explicit grants. Restoring a version
creates a new version against the expected current revision. Inline tasks
remain document checkboxes unless explicitly linked to managed work. A
rendered export and a lossless archive advertise different capabilities.

Generate provider descriptors before compiling the provider:

```sh
go run ./cmd/contractgen -package=projecttasks -provider-id=example-tasks \
  -go-package=contracts -emit=go -out /tmp/example-task-contracts.go
go run ./cmd/contractgen -package=projectknowledge -provider-id=example-knowledge \
  -go-package=contracts -emit=json -out /tmp/example-knowledge-schemas.json
```

The generator binds and hashes the exact
`svc.<provider>.project.tasks.v1.<operation>` or
`svc.<provider>.project.knowledge.v1.<operation>` address. Providers use
`plugin.ServeAtPoint`; hosts use discovery plus
`plugin.BindDiscoveredCommand`. Do not retarget a host descriptor or calculate
schema hashes at runtime. Manifest permission validation reserves provider
ingress to the host and the provider's own subscriptions. `DeclaredCommands`
expands only exact commands or the matching versioned project facade pattern;
a broad Gateway wildcard never grants implicit project authority.

## Identity and admission

`Scope` carries portable logical project ID, environment and tenant, binding
ID/revision, provider generation, and capability revision. A checkout path and
a Gateway Projects registration are not portable project identity. `Binding`
names the selected slot, provider and remote resource. A machine's global
default or project override cannot silently replace shared project authority.

Public requests contain scope references, not an actor or credential.
The host derives `ActingContext` from the authenticated caller and authorized
account connection, then mints an expiring `Invocation`. Provider envelopes
must exactly match the request scope. The JSON records themselves confer no
authority: the host must verify admission, invocation ownership/expiry,
current binding and generation, provider instance, project/resource access,
and the user's current connection. Providers must recheck current remote
authorization. Comment, reaction, and worklog inputs cannot set a historical
author. Migration provenance is retained separately from the acting user.

Capability support and current authorization are distinct. `RequireOperation`
checks the capability revision and operation policy; it does not authenticate
the actor or replace the host checks above. A dynamic MCP catalog revision
must be refreshed and fenced before dispatch. Capabilities report what that
transport can perform; discovering tools does not establish REST or CLI parity.
Provider-specific fields are namespaced typed JSON values with valid JSON and
explicit types, not raw remote requests.

`RequireOperation` with `offline: true` admits queued mutations only when the
capability advertises `queue`. A `read-cache` policy permits no queued write.
Hosts check cached-read policy separately when serving already stored data.

## Offline operations, claims, and provider changes

Persist the exact typed request, `OfflineOperation`, actor, scope, native
mapping, dependencies and payload checksum before returning `saved-local`.
That receipt has `remoteAccepted: false`. Only `accepted` has
`remoteAccepted: true`. Preserve the operation ID and payload across retries.
Unknown outcomes require a recovery token; recover them before any replay.
`ValidateFor` checks receipt/request correspondence when the host has both.

Reconnection must reauthorize the actor and reconcile the active shared
binding. `RecoveryPolicy.SupportsReplay` refuses operations beyond remote
deduplication or receipt retention windows. A 24-hour remote receipt does not
support an unbounded offline promise. Zero retention means indefinite only
with durable idempotency, lookup, and reconciliation support. Conflicts retain
both versions or an opaque handle to both payloads for explicit resolution.

New claims and renewals, governed completion, and permission/public-link
changes require online authority even if a provider mistakenly advertises
them as queueable. Leases carry a server-issued fence and expiration. Local
time or persistence cannot extend one. Writable `AgentWorkInput` excludes the
read model's `Claim`; supplying a lease through an agent metadata write is
rejected. Review and evidence inputs still require provider-side policy checks.
Native mappings retain binding
revision, provider generation, resource, and operation ID for bidirectional
feedback suppression. Hosts must also deduplicate incoming native events.

A provider switch pauses every writer, snapshots and exports all entities,
relations, versions, provenance and bytes, imports them, verifies inventory
and checksums, then atomically replaces the shared binding and reconnects all
writers. `verified` migration requires `lossless` and `writesPaused`; the host
must establish that those claims are true. The SDK exposes no activation
operation for a provider to switch itself. Never activate with dropped data
or outstanding unhandled operations.

Migration retains the logical project, environment, and tenant; a provider's
remote account is identified separately by the resource installation. Every
entity retains its original native resource ID, source revision, checksum, and
a separate stable `sourceRecordId`. Distinct source files or historical versions
with duplicate native IDs remain distinct records. Verified entities require
mapped destinations and verified source bytes with matching identity, checksum,
and provenance. Hosts must check source-record uniqueness and full inventory
coverage across all pages; a valid manifest page alone does not prove losslessness.

## Wire limits and byte transfer

Validated descriptors enforce the bounded Go model at dispatch and response.
The total JSON message is limited to 64 KiB, collections to 256 entries, and
requested pages to 200 records. Responses use opaque cursors and explicitly
state whether totals are unknown, estimated, or exact and whether reads are
snapshot, live, eventual, or cached. Large bodies, attachments, and migration
records use the transfer protocol rather than increasing bus message limits.
Generated browser validators enforce shape; host semantic validation remains
authoritative.

`Transfer` identifies MCP resource/chunk or authorized HTTPS bytes, direction,
expiry, media type, size, checksum, and provenance. It carries an opaque
transfer ID and a credential-free resource URI. Credentials and signed URLs
remain in the host's protected authorization lookup; query strings, fragments
and URL user information are rejected in the public URI. Providers must bind
the transfer to the acting user/project and approved remote destination. The
host verifies completion and checksums. A returned URI is never a local path
or shell command to execute.

## Regeneration and validation

From `v2`, regenerate each package with `contractgen` using `-emit=go`, `dts`,
`js`, and `descriptors-js`; outputs are `payload_gen.go` and
`typescript/{contracts.d.ts,validators.js,descriptors.js}`. Regenerate the
combined `typescript/schemas.json` with `-emit=json`. Artifact equality,
concrete provider hashes, semantic boundaries, and host-only ingress have
focused tests. Run `go test ./...` in both modules before releasing.

These are additive v2 public contracts and require a versioned SDK release,
then the Gateway SDK consumer release and host/provider pin updates. No
untagged module replacement is needed or permitted. Contract tests establish
wire behavior; they do not establish authenticated Agent Boards, Jira, or
Confluence acceptance.
