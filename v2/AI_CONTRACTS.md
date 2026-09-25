# Decision providers and durable coding sessions

These public v2 contracts describe host-owned operations. They do not implement
HTTP, credential storage, payload storage, ACP processes, routing or a UI. Hosts
must implement and authorize them before advertising availability.

| Package | Boundary |
| --- | --- |
| `aidecision` | Mixed Choice/Score/Noul decision jobs and immediate model-cache snapshots |
| `payloads` | Host-owned, scoped upload/read/revoke handles |
| `provideraccess` | Opaque credential handles and allowlisted HTTP jobs |
| `codingsessions` | Catalog, task lifecycle, durable events, approvals and shared dock attachment |
| `hostsettings` | Redacted settings validation and owner-only nonsecret snapshots |

All host commands use `cmd.gateway.*`. Plugins may request granted operations;
they may not publish, subscribe or serve that namespace, and may not use the ID
`gateway`. Workload identity and generation come from the authenticated transport,
not request fields, browser headers or manifest claims. Enabled declaring
background consumers can use the decision facade without a human subject lease.
Coding-session authorization is separate and does not follow from AI access.

Automatic decision admission requires an enabled admitted plugin and explicit
`Permissions.Request` entries for the exact current commands or precisely
`cmd.gateway.ai-decision.v1.>`. `aidecision.DeclaredCommands` expands that declaration
to a closed list. Broader patterns do not qualify. No `Needs` entry, new capability
ID or publisher allowlist is involved. Payload operations still need explicit
declarations and owner checks; coding and provider privileges remain separate.

## Provider services and generated schemas

Providers declare `ai.decision`, or `host.settings.section` for settings, using
existing manifest service/implements fields. Consumers call the stable host
facade; only the host discovers and resolves providers. Generate provider-specific
descriptors from the released generator, for example:

```sh
go run github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/cmd/contractgen@VERSION -package=aidecision -provider-id=jev -go-package=jevdecision -emit=go -out decision_gen.go
go run github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/cmd/contractgen@VERSION -package=hostsettings -provider-id=jev -go-package=jevsettings -emit=go -out settings_gen.go
```

Use separate Go packages for those two outputs. Generate `-emit=json` as well to
record the provider's canonical schema sidecar. Check generated files into the
provider package. Schema digests include the concrete address: never retarget a
descriptor or reuse a host hash. `plugin.ServeAtPoint` mounts a generated provider
command with its public point metadata; grants and host admission still apply.
It also publishes the reserved `bd.contract.name`, `bd.contract.revision` and
`bd.contract.schema` metadata. The host uses `BindDiscoveredCommand` only after
authenticating and admitting the live provider; the helper checks exact concrete
namespace, operation, revision, endpoint point and metadata. Missing or mismatched
metadata fails closed. These fields establish compatibility, never authority.
Plugins cannot request or publish to `svc.*.ai.decision.>` and cannot subscribe
outside their own service namespace. The host broker must enforce these same
rules even against a plugin that bypasses SDK manifest validation.

All new command descriptors use `NewValidatedCommand`. Typed calls and handlers
validate requests and responses, with exact JSON keys, no duplicate/case-folded
keys, no null scalars/objects and the existing 64 KiB message ceiling. Legacy
descriptors keep their previous decoding behavior. Browser validators check wire
shape; the serving Go host is authoritative for semantic and authorization checks.

## Decision values and asynchronous work

Decision values preserve the text/object/array inputs and Jev primitives; they do
not promise chat generation, embeddings or media. Questions and answers use
ordered ID-bearing arrays; adapters preserve IDs when translating upstream maps.
Choice criteria may have a null description. Score levels use zero-based IDs.
Noul has a value but no confidence. `BatchResult.ValidateFor` also checks request
correspondence, full option coverage, score bounds and probability sums within
one millionth. Upstream adapters must reject unknown/duplicate response keys and
malformed JSON before constructing these normalized values.
`RequestedModel` echoes the requested ID; `Model` records the actual resolved ID.
An adapter must check exact pinned model identity or an explicitly enabled known
alias resolution. Generic correspondence checking does not invent alias policy.

The generator's portable numeric contract excludes floating-point fields.
Probability, confidence, score and Noul therefore use nonnegative decimal strings
with at most 64 bytes. Use `DecimalFromJSONNumber` with `Decoder.UseNumber` to
preserve upstream precision, or `DecimalFromFloat` if a value is already a float.
`ParseDecimal` returns an exact rational. NaN, infinity, negative values and
out-of-bound precision are rejected; clients need not invent decimal formatting.
Token counts are uint64 decimal strings. Successful Jev results require both
usage fields, including genuine zero; absent telemetry is an invalid successful
result, not zero usage. Failed/cancelled jobs may still have unknown provider cost,
which the host must record separately and conservatively for quota accounting.

Decision Start/Read/Cancel and provider EgressStart/EgressRead/EgressCancel return
promptly. They never hold the five-second command bus open for provider HTTP.
Host-owned jobs survive a caller disconnect, enforce deadlines and are cancelled
explicitly. Routing has a 10-second total budget; ordinary decisions and egress
have at most 30 seconds. Retries and Retry-After must fit the original deadline.
Models returns an immediate cache snapshot and marks an in-progress refresh;
a cache miss does not wait synchronously for HTTP. Cancel does not promise a
refund for upstream work already accepted.

## Payload and credential ownership

Assembled payloads are at most 8 MiB. Upload chunks are at most 24 KiB decoded,
using canonical padded base64, so encoded requests fit inside 64 KiB. Uploads
declare total size and lowercase SHA-256; commit verifies both. Identical chunk
retries are idempotent; conflicting offsets are refused. TTL defaults to 300
seconds and may not exceed 600 seconds. Inline text is bounded at 32 KiB.

The host binds each handle to caller identity/generation, designated provider
identity/generation and purpose. It never accepts caller identity in payload
requests. On dispatch the host alone grants the chosen provider generation a
job-limited read delegation for consumer input handles. The provider cannot choose
a different caller, transfer that delegation, or read another job's payloads.
Result bytes are rebound by the host to the original consumer. Normal completion
revokes input/delegated provider access, but keeps consumer output readable until
its TTL or explicit release. Cancellation revokes the job's handles and callback
scope. Disable, uninstall, rekey and generation changes revoke affected scopes.

`ProviderStartRequest` carries a host-minted `InvocationID` alongside the consumer's
batch. The token binds original consumer identity/generation, chosen provider
identity/generation, job and deadline. Providers forward it on all nested payload
and egress calls; the host verifies it against the authenticated provider on every
call. It is separate from the credential handle and idempotency key. Provider
read/cancel/model operations also carry this invocation. Consumers cannot mint or
choose one. Revoking the original job cancels nested egress and delegations; normal
completion preserves the rebound consumer output until TTL or explicit release.

Credentials stay in host-owned secret settings. Providers receive opaque handles
for their own configured slot, never plaintext keys. Egress names `evaluate` or
`models`; no arbitrary URLs, methods, headers or credential values enter this
contract. The host maps operations to admitted provider destinations. Requiring a
provider does not inherit `credential.secret` or `egress.provider` authority.

External settings opt into host persistence by advertising the generated typed
`validate` operation. The host strips secrets before sending merged values and
persists only after successful validation. `read-owner` returns only the owner's
nonsecret values plus secret-presence slot names. DTO validation cannot identify
arbitrary secret values: schema filtering and redaction are mandatory host checks.

## Coding lifecycle and recovery

Each task selects one actual available ACP provider, model and advertised config
values. Unknown model/effort metadata stays unknown; provider-specific config IDs,
categories, values and extensions are preserved. After a config update the full
option list replaces the old one. Balanced, Economy, Fastest and Maximum quality
are routing policies, not invented provider model IDs. Ask, Auto-edit and Full
access appear only when the host can enforce them for that provider. Overrides
apply to the next task, never silently reroute a followup.

Preview starts a bounded routing-only job through the same policy/eligibility path,
with explicit read/cancel operations. It reports the selected route or a pending
reason, confidence when known, and cost/latency knowledge flags. It creates no
session, worktree or native process and does not consume next-task overrides.

Create/NewTask use the selected checkout's committed state in a fresh host-owned
worktree and report exclusion of dirty changes. Non-Git projects fail clearly.
Create and NewTask may explicitly name `workUnit: {taskId: "TM-475"}`. The selected
project and checkout identify the host-authorized Task Management store; no
caller-supplied path, URL, port, board or binding ID is accepted. The host validates
the exact authoritative task, persists its immutable private store/task identity,
and returns `Session.workUnit: {taskId, bindingId}`. That opaque output binding is
not input authority. The coding `Session.taskId` remains distinct from the
originating `Session.workUnit.taskId`. When the bound work unit completes, the host
ends its shared coding session without impersonating a plugin or user. Missing,
replaced or unavailable task state must not be treated as completion.

Omitting workUnit keeps an unlinked task; explicit `null` is invalid in both Go
and browser decoders. NewTask omission creates an unlinked new
task; it never inherits the prior link. A prompt, active epic, task title or ACP
end_turn cannot create or complete this binding. Input IDs use `TM-` followed by a
positive ASCII integer, at most 64 bytes. Preserve zero padding exactly: `TM-001`
must not be rewritten to `TM-1`; an all-zero number is invalid. DTO validation
checks syntax, not task existence or permission. Hosts must enforce those checks.
This additive DTO extension changes generated schema hashes: regenerate and pin
the matching released host/client contracts; do not patch hashes by hand.

Complete/NewTask must honor authoritative linked-task completion gates; an ACP
end_turn is only the end of a prompt. Stop cancels that prompt and retains a
healthy connection. End/Complete retire the task session without deleting the
worktree or history. Recover explicitly attempts capability-gated load/resume;
uncertain prompts are never replayed automatically.

Events have strictly increasing per-session sequence numbers. Live Changed events
are hints; clients recover through Events and deduplicate. The host persists event
bytes, not expiring handle IDs, and remints authorized viewer handles on reads.
ACP approvals remain pending until an allowed advertised option is selected.

Projects and the dock attach to the same durable session. OpenSurface opens or
focuses that reference and does not create a PTY or agent. Explicit dock close
calls End. Navigation, refresh, detach and network disconnect do not end it.
