//go:build compilefail

// These call sites MUST NOT compile. TestConsumerUnionRefusesUnclassifiedTypes
// builds this file with -tags compilefail and asserts the compiler rejects both
// — the classification guarantee is only real if it is a build failure in the
// consumer's own package, not just in plugin's.
package consumer

import (
	"context"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/plugin"
)

func mustNotCompileUnclassified(ctx context.Context, h plugin.Host) {
	_, _ = Call(ctx, h, Command[Unclassified, Pong]{}, Unclassified{})
}

func mustNotCompileForgedByEmbedding(ctx context.Context, h plugin.Host) {
	_, _ = Call(ctx, h, Command[Forged, Pong]{}, Forged{})
}
