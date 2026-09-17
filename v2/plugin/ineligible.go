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
// Host-internal families live under "cmd.host." rather than "cmd.<name>.", and
// the prefix is load-bearing rather than tidy. A plugin implicitly serves
// "cmd.<id>.>", so while these families sat at "cmd.identity.>" and
// "cmd.cutover.>" the two shipped plugins named identity and cutover were
// refused by their NAMES alone, before declaring a single permission — nothing
// about either manifest was wrong and no migration could fix it (TM-422). A
// bare "cmd.<word>." reservation is a collision waiting for anyone who names a
// plugin after what it does. "cmd.host." cannot collide, because no plugin id
// is "host": the host's own extension points already own that word.
//
// The move is safe to make because none of these families has a registered
// operation yet — they are reservations, denied by absence and named here so a
// future declaration cannot reach for them. A host surface added under one of
// these names must therefore be spelled "cmd.host.<family>.v1.<verb>"; spelling
// it "cmd.<family>.v1.<verb>" puts it in a namespace a plugin may legitimately
// own. The gateway pins that in TestHostInternalFamiliesStayUnderCmdHost.
//
// Two entries that look like families deliberately are not, and stay put:
// cmd.plugin.v1.disable / cmd.plugin.v1.announce (see below) and
// cmd.terminal.v1.write.
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
		"cmd.host.auth.>",
		// Session cookie signing keys.
		"cmd.host.session.>",
		// Store and Vault credentials, TLS pins.
		"cmd.host.identity.>",
		// SETUP_TOKEN, control.env, config.json.
		"cmd.host.secrets.>",
		// The cutover whitelist runner is arbitrary command execution.
		"cmd.host.cutover.>",
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
		"cmd.host.statedir.>",
		// Raw PTY write to a terminal the caller does not own.
		"cmd.terminal.v1.write",
	}
}
