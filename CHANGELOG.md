# Changelog

## [Unreleased]

### Added

- **Lease-scoped host session contexts (`v2/sessioncontext`).** Three typed
  commands — `cmd.host.session-context.v1.open`, `.refresh`, and `.action` —
  define an opaque, bounded interaction context for a plugin. The substrate
  stamps workload identity, generation, and subject lease; those are never
  request fields. Contexts expose only a purpose, opaque identifiers, derived
  state, expiry, revision, and a closed host-approved action set. They expose
  no project path, terminal CWD, port, process, proxy, private URL, credential,
  or arbitrary metadata. `task_dashboard` is the first purpose.

- **`v2/`: the substrate-neutral SDK generation** (module
  `github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2`, released independently of
  v1, which keeps building beside it).
  - `v2/bus` — the whole messaging surface as SDK-owned types: `Subject`/`Pattern`
    with one shared matcher (`ParseSubject`, `ParsePattern`, `Matches`, `Covers`),
    `Headers`, `Msg`, `Bus`, `Subscription`, `Capabilities`, `Fault`, and
    `Streams`/`KV`/`Objects`/`Services`/`Scheduler`/`Trace`. `Identity` and
    `Grants` live here too, so a substrate can tell who is calling without
    importing the plugin package. `Seq` and `Revision` marshal as decimal
    strings; `Cursor` is opaque and substrate-minted.
  - `v2/bus/memory` — the in-memory substrate, shipped as the plugin author's
    test double so a full test suite runs with no broker.
  - `v2/bus/conformance` — the 24 properties every substrate must satisfy, with a
    `requiredForDefault` subset that fails hard rather than skipping. One list,
    several runners.
  - `v2/plugin` — `Base`, embedded and accessor-only (`Bus`, `Logger`,
    `Profiling`, `StateDir`, `Identity`), bound once per generation by `Bind`;
    manifest v2 with `serves`/`streams`/`kv`/`objects`/`needs`;
    `validateSubjectPatterns`; `PermanentlyIneligible`; `GrantsDigest`; the typed
    layer over `bus.Bus`; the extension-point registry.
  - `v2/plugin/v1compat` — a v1 `plugin.Host` over the v2 bus, so v1 plugins run
    unchanged and the host converts one at a time rather than on a flag day.
  - `v2/cmd/contractgen` — v1's `plugin-typescript`, with five operation kinds
    (`command|event|stream|bucket|service`) instead of two, the operation's
    ADDRESS inside the schema digest, and two more emitters (`descriptors_gen.go`,
    `descriptors.js`). The digest algorithm is bumped to `bd.schema-id.v2`
    because the canonical form changed.
  - `v2/messaging` and `v2/pack` carried forward; `pack.Result` now reports the
    manifest's `GrantsDigest`.
  - Module gates: no `github.com/nats-io/*` import or require anywhere, and a
    reflect walk over every exported `v2/bus` and `v2/plugin` type. Each gate
    ships with a companion test that breaks it on purpose.

### Notes for implementers

- **`bd-schema` is stamped with `bus.WithSchema`, not `bus.WithHeaders`.**
  `StripReserved` strips only the IDENTITY triple — `bd-caller`,
  `bd-generation`, `bd-subject` — and not the whole `bd-` namespace. Stripping
  everything also removed the typed layer's schema stamp, so the receive-side
  check could never fire; a check that cannot fire is worse than no check.
  Forging `bd-schema` or `bd-corr` buys a caller nothing. Forging identity buys
  authority, which is why only those three are unforgeable.
- **A reserved FIRST token opens a substrate-owned namespace.** `$SYS.ACCOUNT.x`
  and `_INBOX.<id>.<random>` parse, because the permanently-ineligible set has
  to be able to name the family it denies and the broker spells those subjects
  in its own case convention. Structure still applies inside them; a manifest
  naming one is still refused.
- **The digest covers the address.** Two operations with identical payloads on
  different subjects now get different hashes, so a descriptor cannot be
  re-pointed without the schema check noticing.
- `contractgen` has no `plugin` target yet. v1's carried the host-exposed
  command contracts (terminal presentation, desktop applications, tmux); those
  become bus services in wave 2, and porting them now only to re-address them
  then would be work done twice.

### Changed

- Nothing in v1. v1 continues on `0.4.x` for fixes; `v2/plugin/v1compat` imports it.

## [0.4.0-rc.17] - 2026-09-16

### Added

- `host.session.backend`: the extension point the host's terminal runtime asks for a session instead of driving local tmux itself (agent-fabric ADR 0002). `SessionBackend` is `ID`, `Create`, `Close`, `List`, `Attach`, `SendText` and `Capture`; `Attach` returns a `Stream`, an `io.ReadWriteCloser` plus `Resize`, carrying PTY bytes unchanged. The existing local tmux code becomes the `local` backend without moving. The point is in-process only and has no wire schema, because a live byte stream does not fit a command envelope; a spawned plugin can implement it once the host can grant byte streams.

## [0.4.0-rc.16] - 2026-09-16

### Added

- `Host.Profiling()` returns this plugin's host-owned profiler switch (`Enabled` / `Set`). Off is the default. The switch is independent per plugin and takes effect on the next request, command, subscription or tick without a restart. Adding the method is a breaking change for Host implementations; bump consumers together. `NopProfiler()` is the always-off stand-in for tests and unscoped hosts.

## [0.4.0-rc.15] - 2026-09-15

### Added

- Four typed, read-only tmux host commands for availability, sessions, windows, and panes. Availability is public host metadata; inventory output is subject-classified, fixed-format, NUL-free, and capped at 48 KiB after JSON escaping. Requests accept no caller-controlled argv, target, format, or mutation.

## [0.4.0-rc.14] - 2026-09-15

### Added

- `LoggerWithCorrelationID` adds a host-minted request correlation id to plugin log entries as structured data without changing the `Logger` interface. Existing hosts and plugins remain source compatible, and transport-specific SDKs retain responsibility for trusting their request boundary.

## [0.4.0-rc.13] - 2026-09-15

### Fixed

- Component snapshot responses and change events carry a host-owned monotonic revision within their component incarnation, so clients can reject stale events that race the initial snapshot read. Live browser revisions must remain nonnegative JavaScript safe integers; withdrawal may use zero.

## [0.4.0-rc.12] - 2026-09-15

### Added

- Component identities, explicit host assignments, additive extension descriptors, and readonly snapshots for workspace, session list, session tab, terminal, stage, Tasks, file tree, and project tools. Browser declarations and validators are generated from these canonical Go contracts. Identities do not grant authority; hosts must validate each assignment and invalidate it on owner or generation changes.

## [0.4.0-rc.11] - 2026-09-15

### Added

- **Resolved Applications executable identity (gateway TM-331).** Optional `DesktopApplication.executablePath` carries an absolute host-resolved binary path separately from the launcher path and arguments. It remains subject-classified data. The added field changes exact typed-schema hashes for scan and registration payloads, so host and plugin consumers must adopt the release together; JSON optionality alone does not preserve schema-hash compatibility.
- **Provider settings declarations and contribution-role eligibility (gateway TM-258/TM-259).** Provider fields declare an exact extension point and required capabilities, with order-independent struct tags; the host must resolve and validate live choices. Each canonical contribution role now carries installed-artifact eligibility. Trusted chrome and operator-tool roles require explicit per-point consent, and unknown roles fail closed. These declarations do not grant authority or supply Gateway enforcement by themselves.

### Fixed

- Generated messaging artifacts now participate in regeneration checks. Descriptor checks evaluate literal and named constants without weakening schema identity checks. Generated consumer wrappers reject nil handlers before registration, preserving messaging's existing fail-fast behavior.

## [0.4.0-rc.10] - 2026-09-13

### Added

- **Asynchronous, paged Applications discovery (gateway TM-331).** Adds `cmd.desktop-applications.v2.scan` without changing the v1 scan command. An empty scan id starts or joins discovery; callers poll by scan id and page one immutable completed snapshot with bounded cursors and a default page size of 100 (maximum 200). Results distinguish `scanning`, `complete`, and `failed`, carry no desktop-availability state, require a non-nil application page, and are capped at 48 KiB on the wire.

## [0.4.0-rc.9] - 2026-09-13

### Changed

- **Breaking: UI contribution slots name a role, never a position (gateway ADR 0026 D1, TM-255).** `SlotToolbar`, `SlotOverlay` and `SlotBadge` are removed, and manifest validation refuses `toolbar`, `overlay` and `badge` as unknown slots. The roles are `SlotMainNavigation` (`main-navigation`), `SlotSubNavigation` (`sub-navigation`), `SlotPrimaryAction` (`primary-action`), `SlotSecondaryActions` (`secondary-actions`), `SlotStatusIndicator` (`status-indicator`), `SlotObjectActions` (`object-actions`) and `SlotLauncher` (`launcher`). Like the old panel slots, each requires only `panelId`. `SlotDefaultView`, `SlotSettings` (the settings-section role) and `SlotCommand` already named a function and are unchanged. A contribution declares what it is, and the shell decides where that role renders. To migrate, replace `toolbar` with `primary-action` and `badge` with `status-indicator`. `overlay` has no replacement role.

- **Breaking: extension point names are namespaced.** `HostPointNamespace` (`host.`) and `ValidateExtendsName`. Manifest validation refuses an `extends` name that is not lowercase dot-separated segments under either `host.` (followed by an area and a name, e.g. `host.settings.section`) or the plugin's publisher id (at least two segments, e.g. `acme.widgets`); `implements` entries are left to the host, so installed packages using the old bare names still pass discovery. `SettingsSectionPoint` is now `host.settings.section` and `TerminalPresentationPoint` is now `host.terminal.presentation`; the terminal presentation interface and command names are unchanged.

### Added

- **Applications catalog metadata (gateway TM-331).** `DesktopApplication` now carries optional `launcherPath`, `installedAt`, and `installedAtEstimated` fields alongside `iconUrl`. Launcher paths are absolute host paths for authorized catalog display, install times are RFC3339, and an estimated flag is valid only when an install time is present. Executables and desktop credentials remain host-private.

- **Typed desktop Applications host service (gateway TM-326).** Adds bounded, validated status, scan, register, open, refresh, viewer-ticket and quit commands. Scan results expose bounded catalog records and opaque session identities without returning executables or desktop credentials.

- **Reactive contribution bindings (gateway TM-256).** `UIContribution.Bindings []Binding` declares that one aspect of a contribution follows a bus event the plugin already publishes. `Binding` carries `Kind` — `BindCount`, `BindBadge`, `BindLiveness` or `BindToggle` — the exact `Event`, and an optional `Field` naming one JSON key of that event's payload; an empty field means the payload is the value. The plugin writes no frontend code, and the host never hands it the shell to draw a badge itself. A contribution may carry one binding of each kind, so a nav item can show an unread count and a liveness dot at once. A binding is a request, not authority: the host resolves the named event through the same conjuncts as any other subscribe, so one declared for an event the plugin was never granted is refused rather than obeyed. Manifest validation checks shape only — known kind, no duplicate kind, exact event name, and a field that is one JSON key rather than a path, because a path would be a query language evaluated by the host against a payload the plugin controls. Additive: every contribution that exists today declares none and is unchanged.

- **Optional interfaces beside `Host` and `Plugin` (gateway ADR 0026 B2, B3, B4, E1; TM-268).** `Host`'s method set is pinned, because every deployed plugin links its own copy, so these arrive alongside it and a plugin type-asserts for what it wants. An older host implements none of them and keeps working.
  - `ObservableRegistrar` — `SubscribeErr` and `EveryErr`. `Host.Subscribe` and `Host.Every` return only a cancel, so a registration refused past the per-generation cap, or refused for a missing grant, is a silent no-op and the plugin believes it is subscribed. These forms return the refusal.
  - `Draining` — a plugin's `OnDrain(ctx)`, called before the host revokes the generation, while the bus, timers and state dir still work. A hook next to `Stop` would be dead on arrival. It may not veto: an error is reported and teardown continues.
  - `DataVersion` and `DataVersioned` — the version that last wrote this plugin's state dir, scoped to the calling plugin. An upgrade hook has no `from` to pass until the host records this.

- **The plugin Kit.** `Kit`, `NewKitFrom(h Host, caps HostCapabilities)`, `Kit.Host`, `Kit.Capabilities` and `Kit.Can(kind GrantKind, operation string)`, with `GrantPublish`, `GrantSubscribe` and `GrantRequest`. A plugin holds a Kit as a field and calls through it. It is never embedded, following `Registrar`. `NewKitFrom` does not negotiate: it copies the capabilities the plugin already negotiated. `Can` answers from that copy of the grants, by exact operation name, as the host matches them, and never calls the host. The host still enforces authority on every call, so a stale or fabricated cache can only mislead its own plugin and never widens what the host allows. `true` from `Can` is necessary but not sufficient, because a host resource rule can still refuse one envelope.

- **Typed settings field schema.** `ConfigSection.Fields []ConfigField` with `key`, `kind` (`bool`, `int`, `string`, `stringList`, `enum`, `secret`), `label`, `description`, `default`, `min`, `max`, `nullable`, `choices`, `readOnly` and `requiresRestart`. `ConfigFieldsFromStruct` derives fields from `json` tags plus a `config:"label=…,enum=a|b,min=,max=,default=,readonly,secret,restart"` tag; unknown tag keys are errors. Manifest validation checks declared sections. The TypeScript declarations, JS validators and Go payload union are regenerated.

- **Lifecycle hooks through negotiation.** `Hooks` on `ProtocolRequirements` (declared by the plugin) and `HostCapabilities` (acknowledged by the host), `DeclaredHooks` to build the set by local type assertion, the constants `HookActivationCheck`, `HookReady` and `HookHealth`, `LifecycleHookCommand` and `FeatureLifecycleHooks`. `CheckProtocol` and manifest validation refuse unknown hook names and a host acknowledging a hook the plugin did not declare. There is no stop hook.

- **`Manifest.Config` and `SettingsSectionPoint`.** A plugin declares the settings sections it contributes as manifest data — `Config.Sections[]` with `id`, `title` and `description` — so the host can list and label a section before asking the plugin for anything. It is schema, never values: the host owns values and reads its own record (gateway ADR 0026 C0). `SettingsSectionPoint` (`settings.section`) is the extension point the plugin implements; its operations are `cmd.settings.section.v1.snapshot` and `cmd.settings.section.v1.patch`. Typed field schema is added to `ConfigSection` before the SDK is tagged. The TypeScript declarations, JS validators and Go payload union are regenerated.

### Fixed

- **Terminal presentation admission compares incarnations by value.** `sameTerminalIncarnation` compared `PresentationTerminal` with `==`, and the tmux context is a pointer, so a freshly re-resolved tmux binding never matched the requested one and every tmux-bound terminal failed admission. It now compares the terminal id, the binding kind and the tmux fields by value.

- **TypeScript declarations for the runtime shape guards.** `cmd/plugin-typescript` emitted an `is<Type>` guard per declaration into the paired `.js` module but never declared them in `contracts.d.ts`, so `import { isManifest }` was a TS2305 error against a symbol that resolves at runtime. The emitter now declares every guard as a narrowing predicate (`value is <Type>`), and the generator tests assert the declarations stay in step with the exports. Guards check structure only, not the host's security invariants.

## [0.4.0-rc.7] - 2026-09-10

### Added

- Typed capability layer (ADR 0025): `Descriptor`, `NewDescriptor`, `Invoke`, `Observe`, `Publish` and `HandleRaw` as the cross-package mechanism, with `Command`/`Event`/`Call`/`Emit`/`On`/`Handle` implemented on top so there is one code path. Consumer packages generate their own `Payload` union and typed wrappers over `plugin.Descriptor`; `plugin` never imports a consumer, so no import cycle is possible.
- Classification: a `bd` struct tag (`public` | `subject` | `secret`) on every JSON-visible field, and a generated `Payload` union type set per package. A union without `~` matches exactly the named types, so an unclassified type cannot be passed to a typed call and cannot be smuggled in by embedding a member — a marker method would have been promoted through embedding and is deliberately not used.
- `bd.schema-id.v1`: SHA-256 over a length-delimited canonical AST covering operation kind, name, revision and request/response schemas, with fields sorted by JSON name and classification included. Type and package names deliberately do not affect the hash.
- Deterministic `typescript/schemas.json` sidecar carrying the canonical AST beside every hash, so a mismatch is a diff rather than two opaque hex strings.
- `messaging` contract package and generated TypeScript runtime validators.

### Changed

- The generator classifies by reachability from flagged seed roots instead of a type-name prefix. Output is byte-identical: `typescript/contracts.d.ts` is unchanged.
- Unsupported field kinds, unclassified fields, secret fields reachable from an exposed type, 64-bit integers without `,string`, and cyclic payload graphs are now named generator errors rather than a bare panic.

### Notes

- Classification is purely additive: tagging the shipped `Manifest`, `RuntimeSnapshot` and `PresentationRequest`/`Result` types produced a zero-byte `.d.ts` diff and no behavioural change.
- `terminal.presentation.project.v1` keeps schema id `bf4dbefbc9cd9832e1803f4c91e34802ab02a41ff793e9ae3ba9a3926294c0d7`, so no consumer renegotiates.

## [0.4.0-rc.5] - 2026-09-09

### Added

- Canonical panel document-path contributions, explicit feature negotiation, validated named parameters and one-or-more catch-alls, overlap checks, and decode-once matching with shared browser parity vectors.
- Feature identifiers for document routing and framework-independent UI mounting. Hosts advertise them only after implementation.

## [0.4.0-rc.4] - 2026-09-09

### Added

- Canonical Requirement version-range validation and matching, with explicit prerelease behavior and compatibility for unconstrained legacy versions. Manifest authoring and discovery reject malformed constraints.

## [0.4.0-rc.3] - 2026-09-08

### Added

- Explicit unknown desired state for unreadable operator intent. Recovery reports remain valid snapshots and never grant availability.

## [0.4.0-rc.2] - 2026-09-08

### Fixed

- Generate browser declarations with a single terminating newline so whitespace checks pass.

## [0.4.0-rc.1] - 2026-09-08

### Added

- Host-owned runtime snapshots, generation identity and lifecycle operation contracts.
- Optional activation checks and protocol capability negotiation without changing Plugin or Host method sets.
- Exact requested permissions and validated declarative shell contributions.
- Browser declarations generated from canonical Go JSON contracts with a drift test.

## [Unreleased]

## [0.4.0-rc.6] - 2026-09-09

### Added

- Additive `terminal.presentation.v1` types, identifiers, bounded strict wire
  validation, principal/incarnation lease checks, shared fixtures, and generated
  readonly TypeScript discriminated-union parity. The contract uses existing
  extension declarations and command transport; it adds no manifest field or
  mandatory host/plugin method.

## [0.3.0] - 2026-09-06

### Added

- `plugin.Plugin` and `plugin.Host` — the plugin contract itself now lives here
  rather than inside the gateway (ADR 0024). Before this the SDK shipped the
  nouns but not the verbs, so nothing a plugin author imported described what a
  plugin is, and first-party and third-party plugins were two different
  programming models. Both deployment modes — linked in-process, or spawned and
  reached over a unix socket — implement the same interfaces.
- `plugin.Logger`, and the segregated optional capabilities `HTTPPlugin`,
  `HealthContributor`, `CommandHandler`, `Validator`, `Readier`.
- `PanelSpec.Module` — an optional ES module the shell mounts in-page instead of
  iframing `URL`, which stays required and remains the fallback.

### Changed

- Every `Host` method is constrained to be marshallable, so the in-process and
  out-of-process modes stay interchangeable. `TestHostMethodSetIsPinned` makes
  adding one a deliberate, versioned act: external plugins link their own copy
  of the interface, so a new method breaks every deployed one.

### Added

- `role` (`system`|`extension`), `provides`, `requires`, `requiresProvides` (ADR 0015)
- `Pricing.Model` accepts `trial`; `TrialDays`; `plugin.MissingRequired` / `GraphFrom` / `Cycle`

### Changed

- docs: this module’s SemVer is independent of the Gateway / Vault SDK versions

## [0.1.2] - 2026-08-12

### Added

- `plugin.Targets` / `Supports` / `TargetsOrDefault` (`gateway`|`vault`; empty defaults to gateway-only)
- `plugin.LoadDir`, `LoadDirDiscover`, `LoadDirForHost` (parse + validate + spawn binary + host target)
- `serve` package: host-neutral unix-socket HTTP
- `pack` package: common `<id>-<version>.tar.gz` layout

### Changed

- README: this module is the common ABI both product SDKs inherit
