# AI contract release review

Date: 2026-09-25. Gateway task: TM-475 / EP-028.

Independent review approved the rc14 contract change after checking invocation
binding, provider payload and egress scope, exact discovered descriptor identity,
strict validated messages, host-only provider ingress and concrete provider hash
generation. Discovery metadata proves compatibility, not workload authority.

The host must enforce admitted owner/generation, isolated provider ingress and
invocation lifetimes. Explicit individual decision commands or the exact v1
decision family qualify for automatic admission; broader wildcards do not.
Credential, egress and coding authority do not follow from decision admission.

Verification passed:

- Root and v2 `go test ./...`.
- Race tests for the five new contract packages, plugin and contractgen.
- Generated Go, TypeScript and schema drift checks.
- Independent rerun of plugin, aidecision, payloads, provideraccess,
  hostsettings and contractgen tests.

This release defines contracts. It does not claim Gateway runtime implementation,
Store delivery or live Jev/ACP acceptance.
