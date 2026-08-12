# bytedesk-sdk-dependencies

Shared types for ByteDesk gateway process plugins, the plugin SDK, and the
Vault SDK. **Go module only** — no process, no MCP.

Gateway and extracted plugins import this so `plugin.json` and bus envelopes
do not drift.

```text
github.com/ByteDeskAI/bytedesk-sdk-dependencies/plugin
github.com/ByteDeskAI/bytedesk-sdk-dependencies/bus
github.com/ByteDeskAI/bytedesk-sdk-dependencies/semver
```

See `bytedesk-remote-gateway` [ADR 0014](https://github.com/ByteDeskAI/bytedesk-remote-gateway/blob/develop/docs/adr/0014-plugin-sdk-and-extract.md).
