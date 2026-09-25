# bytedesk-sdk-dependencies/v2

The substrate-neutral plugin contract. A separate Go module, living in the `v2/`
subdirectory so **v1 keeps building beside it** and consumers migrate one at a time.

```text
bus/              Subject/Pattern, Msg, Bus, Streams/KV/Objects/Services/Scheduler/Trace,
                  Identity and Grants. The whole messaging surface.
bus/memory/       In-memory substrate: the plugin author's test double.
bus/conformance/  The 24 properties every substrate must satisfy. One list, several runners.
plugin/           Base (embedded, accessor-only), Bind, Plugin, manifest v2, the typed layer.
plugin/v1compat/  A v1 plugin.Host implemented over the v2 bus, so v1 plugins run unchanged.
webapps/          Typed discovery, coding, service, log, preview, and replay contracts.
aidecision/       Host-resolved decision jobs; generated cross-publisher providers.
payloads/         Scoped bounded payload uploads and reads.
provideraccess/   Opaque credentials and named asynchronous provider egress.
codingsessions/   Durable task sessions, route preview, approvals and dock attachment.
hostsettings/     External redacted validation with host-owned persistence.
```

See [AI_CONTRACTS.md](AI_CONTRACTS.md) for decision/coding ownership rules,
provider generation commands, limits and host implementation obligations.

## The three rules this module exists to enforce

**1. The broker is abstracted, not wrapped.** No `nats` type appears in any
public signature here, and this module does not depend on `github.com/nats-io/*`
at all. There is deliberately no `Raw()` escape hatch: one would put the broker in
every plugin's dependency graph and make the substrate unswappable. "All the
power" is instead a completeness obligation — every broker capability has an
SDK-owned type. `boundary_test.go` and `inventory_test.go` are the enforcement,
and each ships with a companion test that breaks it on purpose, because a fence
that cannot detect its own vacuity is not a fence.

**2. The substrate is pluggable, and it is proven rather than asserted.**
`bus/conformance` is the property list. The gateway runs it against its in-memory
substrate and against embedded NATS; a plugin author runs it against
`bus/memory`. A substrate that passes is interchangeable; one that does not is
not shipped.

**3. Every plugin gets the bus by embedding `plugin.Base`.** No wiring, no
constructor argument, no registration call:

```go
type MyPlugin struct{ plugin.Base }

func (p *MyPlugin) Start(ctx context.Context) error {
    _, err := p.Bus().Subscribe(ctx, "event.files.>", p.onChange)
    return err
}
```

`Base` carries accessors only — `Bus`, `Logger`, `Profiling`, `StateDir`,
`Identity` — and its method set is disjoint from every optional interface, so
embedding it cannot accidentally make a plugin claim a lifecycle hook it did not
write. It is bound once per generation by the host, and a zero `Base` returns
refusing doubles rather than nil.

`plugin.Base` is **not** `kernel.Base`. The SDK base confers capabilities under
grants; the gateway's kernel base confers membership in ring 0. A third party
embeds the first freely and cannot name the second.

## Subjects

```text
event.<id>.<name>     a plugin's own events         (own namespace, implicit)
cmd.<svc>.v1.<op>     a command with an answer
svc.<id>.<point>...   a service endpoint            (own namespace, implicit)
tick.<id>.<name>      scheduled work
_INBOX.<id>.>         replies                       (own namespace, implicit)
```

A plugin's own namespace is implicit and is never listed in its manifest; listing
it is a validation error. `$`-prefixed subjects and `_INBOX` belong to the
substrate: they parse, so the deny set can name them, and a manifest naming one
is refused.

## Manifest v2

New: `serves` (service exports), `streams`, `kv`, `objects` (assets the host
provisions inside the plugin's own principal at enable time), `needs` (substrate
capabilities, a closed vocabulary, fail-closed at enable). `permissions` becomes
pattern-based. Removed: `provides`, `requiresProvides`. `protocol.major` is `2`.

`GrantsDigest` fingerprints everything an operator consents to, so widening any
of it re-asks rather than inheriting the old approval.

## Host-owned session contexts

`sessioncontext` defines the generic interaction boundary for a plugin that
needs derived state about an operator-scoped host resource. Its only commands
are `cmd.session-context.v1.open`, `.refresh`, and `.action`. They are served by
the host, while `cmd.host.*` remains reserved for host-internal operations that
plugins can never invoke. A plugin asks
for an opaque target and purpose; the host derives the caller's principal from
the substrate-stamped lease, then returns an opaque context, revision, expiry,
bounded state, and closed action set. A context action is named and
permission-gated. The contract never carries a working directory, project root,
port, process, proxy handle, private URL, credential, principal, or lease.

## Host-owned Web Apps runtime

`webapps` defines the typed `cmd.web-apps.v1.*` boundary used by a project
panel. Requests bind project, checkout, app, configuration revision, and run
identity. Results expose structured conversation events, service state, logs,
preview capability, and opaque replay cursors. The package contains no command
arrays, process handles, passwords, environment values, or direct app-server
credentials. Creation is the only operation that carries the existing
host-resolved `ProjectDirectoryContext`; its paths select agent working context
and do not grant filesystem authority.

## Migrating from v1

`v1compat.Host(base)` returns a v1 `plugin.Host` over the v2 bus. A v1 plugin
runs unchanged through it; the gateway type-switches on `plugin.Bound` and
converts one plugin at a time. Behaviour that deliberately changed:

- `Start` takes no host argument — the embedded base is already bound.
- Subscribing to the literal `"*"` becomes `">"`.
- Delivery is bounded and asynchronous; a publish no longer returns after the
  handler ran. Tests that assumed it need a wait.
- `Every` becomes at-least-once where the substrate is durable.
- Refusals are loud and attributed. Code that ignored a `Publish` error will now
  see one.

## Releasing

`VERSION` is the module's own version and moves independently of v1's. The module
requires v1 at a tag; a pseudo-version or a `replace` directive fails
`version_policy_test.go` on purpose.

## Hierarchical navigation

`Manifest.Nav` supports links (the default), non-navigating `group` entries, and
root `section` entries. Existing flat links keep their meaning. `NavReference`
addresses an owner and its local navigation ID; URL and label are not identity.
`section`, `parent`, `placement` (main, pinned, footer), and `newTab` are optional.
Groups and sections keep an empty `href`.

A parent opens a cross-plugin seam by setting `childrenPoint` to its own
`Extends` declaration with interface `navigation.children.v1`. The child owner
registers its navigation ID in `Implements` at that point. The host validates
this relationship, resolves availability and authorization, and publishes a
`NavigationSnapshot` with flat nodes and effective parent/section references.
Declared references remain on `node.item`; effective references live on the
node. This supports arbitrary tree depth without recursive wire payloads.

Host projections must retain ownership checks, reject ambiguous or cyclic
attachments, and never restore unavailable or unauthorized links while
promoting a child past an unavailable parent. The resolved snapshot accompanies
the contribution/runtime consistency boundary; endpoint authorization remains
independent. Legacy consumers may continue receiving the flat link projection.
