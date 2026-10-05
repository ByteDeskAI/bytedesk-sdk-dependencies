package projecttasks

import "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"

// DeclaredCommands expands only exact current-version declarations. Broad
// cmd.gateway.> grants do not qualify for automatic project authority.
func DeclaredCommands(requests []bus.Pattern) []bus.Subject {
	var out []bus.Subject
	for _, command := range []string{CommandDescribe, CommandChanges, CommandRecover, CommandTransfer, CommandMigration, CommandSchema, CommandQuery, CommandRead, CommandWrite, CommandClaim} {
		for _, p := range requests {
			if string(p) == command || p == "cmd.gateway.project-tasks.v1.>" {
				out = append(out, bus.Subject(command))
				break
			}
		}
	}
	return out
}
