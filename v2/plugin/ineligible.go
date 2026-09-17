package plugin

import "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"

// PermanentlyIneligible is the set of subject families no principal may ever
// reach, whatever a manifest asks for and whatever an operator consents to.
// Deny wins over every allow. The validator refuses a manifest naming one, and
// the host's grant compiler compiles the same list into every credential, so
// the two cannot drift.
//
// The list supersedes the gateway's hostFixedObjectFamilies table
// (src/host_read_rules.go:72), which expressed the same rule as an object class
// on a host operation name. That worked while every such operation was a host
// command; once a plugin addresses the bus directly, the rule has to live in
// the addressing grammar or it only covers the names somebody remembered to
// register.
//
// Two families that look like they belong here deliberately do not:
//
//   - "_INBOX.>" is not denied, because every principal has its own inbox under
//     "_INBOX.<id>.>" (see OwnNamespace). Denying the root would deny reply
//     delivery to everyone. A manifest still cannot name it: the reserved-token
//     rule in the validator refuses it before this list is consulted.
//   - "bd.>", the inter-gateway mesh prefix, is refused in manifests by the same
//     validator rule but is not denied here, because the host's own mesh
//     credential has to carry it. A deny compiled into every credential would
//     switch off mesh routing rather than protect it.
func PermanentlyIneligible() []bus.Pattern {
	return []bus.Pattern{
		// Substrate internals: monitoring, accounts, JetStream API.
		"$SYS.>",
		// Session minting and isAuthed itself.
		"cmd.auth.>",
		// Session cookie signing keys.
		"cmd.session.>",
		// Store and Vault credentials, TLS pins.
		"cmd.identity.>",
		// SETUP_TOKEN, control.env, config.json.
		"cmd.secrets.>",
		// The cutover whitelist runner is arbitrary command execution.
		"cmd.cutover.>",
		// Lifecycle MUTATION is Control Bus, operator-only. These are the two
		// operations the gateway actually has under cmd.plugin.: the operator's
		// disable verb (src/kernel_host_grants_test.go:459,
		// src/host_read_rules_test.go:34) and the ring-0 lifecycle announce
		// (privLifecycleAnnounce, src/kernel_ring0_ineligible_test.go:28).
		//
		// They are enumerated rather than denied as the family "cmd.plugin.>",
		// which is what v1 did (src/host_read_rules.go:78) and what this list
		// prefers everywhere else. The family cannot be denied whole because
		// cmd.plugin.v1.negotiate lives inside it and is NOT operator-only: it
		// is the spawned plugin's own handshake, requested after Dial and
		// before Bind (phase-2 plan, "Spawned wire v2"). PermanentlyIneligible
		// is compiled into every credential as deny, and deny wins, so denying
		// the family would refuse every spawned plugin at its first call —
		// invisibly, because the memory substrate has no handshake and the
		// conformance suite would stay green.
		//
		// The cost is that family closure is lost HERE: a cmd.plugin.v1.<verb>
		// added to the gateway later is grantable until someone adds it below.
		// TestSpawnedHandshakeIsNotDenied pins the exclusion so the next person
		// widening this entry back to "cmd.plugin.>" fails a test instead of
		// shipping a gateway that cannot spawn. The structural fix is to move
		// the handshake out of this family; that is a wire-contract decision,
		// not one this file may take.
		"cmd.plugin.v1.disable",
		"cmd.plugin.v1.announce",
		// Another plugin's StateDir.
		"cmd.statedir.>",
		// Raw PTY write to a terminal the caller does not own.
		"cmd.terminal.v1.write",
	}
}
