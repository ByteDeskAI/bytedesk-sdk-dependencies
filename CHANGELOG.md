# Changelog

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
