package aidecision

import "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"

// DeclaredCommands expands only an explicit current-version AI declaration.
// Broader cmd.gateway.> or cmd.> patterns do not qualify for automatic AI access.
// The host also checks enabled/admitted workload state, then grants only this
// returned closed list. No coding, credential or provider egress authority flows
// from it. Ordering is stable and duplicates are removed.
func DeclaredCommands(requests []bus.Pattern) []bus.Subject {
	var out []bus.Subject
	for _, command := range []string{CommandStart, CommandRead, CommandCancel, CommandModels} {
		for _, p := range requests {
			if string(p) == command || p == "cmd.gateway.ai-decision.v1.>" {
				out = append(out, bus.Subject(command))
				break
			}
		}
	}
	return out
}
