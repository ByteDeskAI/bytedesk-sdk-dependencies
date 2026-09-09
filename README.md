# bytedesk-sdk-dependencies

**Common plugin contract** for ByteDesk hosts. Go module only.

Gateway SDK (`bytedesk-remote-gateway-plugin-sdk`) and Vault SDK
(`bytedesk-vault-sdk`) both **inherit** every type and requirement from this
module. They do not redefine Manifest, pack layout, or unix Serve. Platform
SDKs only orchestrate host env, `targets` checks, and product clients.

```text
plugin/   Manifest, targets, role (system|extension), requires, Validate, LoadDir
serve/    unix-socket HTTP (host SDKs supply socket/id)
pack/     <id>-<version>.tar.gz
bus/      Envelope
semver/   AtLeast (minCoreVersion)
```

`plugin.json` `"targets"` is `["gateway"]`, `["vault"]`, or both. Empty
targets default to gateway-only (legacy manifests).

## Versioning

This module’s SemVer (`VERSION`) is independent of the Gateway SDK and Vault
SDK. Those modules `require` a specific tag of this module in their `go.mod`.
The numbers do not have to match. Bump this repo when the common contract
changes; each SDK adopts the new tag when it is ready.

See gateway ADR 0014.

## Live plugin contracts (0.4 prerelease)

`RuntimeSnapshot` carries a host epoch and a lossless decimal-string revision; `RuntimeStatus.Available` is the authority for dispatch. Generation is an opaque string. A process restart changes the epoch, so consumers must not compare revisions across epochs. These are runtime facts, never manifest fields.

`ActivationChecker.CheckActivation` is an optional pre-publication check after `Start`. Failure prevents candidate publication and requires cleanup. Existing `Readier.Ready` remains a degraded-health report; its behavior is unchanged.

`Manifest.Protocol` declares required protocol features. `CheckProtocol` rejects unsupported major versions/features; legacy version zero may not request new features. Optional `Negotiator` adds negotiation without extending `Host`. Protocol support does not imply authority. `Permissions` requests exact publish/subscribe/request names; host policy decides the grants and state access remains owner-scoped.

`Manifest.UI` declares shell slots with owner-local panel IDs or commands. The host validates availability and authority when resolving them. An active default-view contribution replaces hard-coded product routing; the host chooses descending priority, then lexical owner/contribution ID, and supplies its generic fallback when none is available.

Generate browser declarations with `go run ./cmd/plugin-typescript -out typescript/contracts.d.ts`; `go test ./...` verifies they match the Go JSON model. The Gateway SDK distributes these declarations to UI consumers. No host implementation belongs in this module.


## Required peer versions

Use `Requirement.MatchesVersion(actual)` to evaluate `requires[].version`; do not
implement host-specific range parsers. `Validate` and `ValidateDiscover` reject
malformed constraints. An empty constraint accepts legacy versions without parsing.
Constrained versions use [Masterminds semantic-version ranges](https://github.com/Masterminds/semver/tree/v3.5.0): comparisons, AND/OR, caret, tilde and wildcards.
Short numeric versions and a leading `v` are normalized. Prereleases are excluded
unless the range includes an explicit prerelease comparator, such as
`>=1.3.0-0 <2.0.0`. Invalid constrained versions return an error, never a match.

This helper evaluates version compatibility only. The host must separately verify
installation, dependency availability, generation ownership and authority. Adoption
adds the pinned `github.com/Masterminds/semver/v3` dependency; no manifest fields or
existing Host/Plugin interfaces change.
