# Changelog

## [Unreleased]

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
