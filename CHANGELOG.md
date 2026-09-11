# Changelog

## [Unreleased]

### Changed

- **Breaking: extension point names are namespaced.** `HostPointNamespace` (`host.`) and `ValidateExtendsName`. Manifest validation refuses an `extends` name that is not lowercase dot-separated segments under either `host.` (followed by an area and a name, e.g. `host.settings.section`) or the plugin's publisher id (at least two segments, e.g. `acme.widgets`); `implements` entries are left to the host, so installed packages using the old bare names still pass discovery. `SettingsSectionPoint` is now `host.settings.section` and `TerminalPresentationPoint` is now `host.terminal.presentation`; the terminal presentation interface and command names are unchanged.

### Added

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
