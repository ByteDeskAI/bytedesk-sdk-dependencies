# bytedesk-sdk-dependencies

**Common plugin contract** for ByteDesk hosts. Go module only.

Gateway SDK (`bytedesk-remote-gateway-plugin-sdk`) and Vault SDK
(`bytedesk-vault-sdk`) both **inherit** every type and requirement from this
module. They do not redefine Manifest, pack layout, or unix Serve. Platform
SDKs only orchestrate host env, `targets` checks, and product clients.

```text
plugin/   Manifest, targets (gateway|vault), Validate, LoadDir / LoadDirForHost
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
