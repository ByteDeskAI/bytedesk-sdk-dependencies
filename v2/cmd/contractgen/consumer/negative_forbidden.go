//go:build compilefail

// These call sites MUST NOT compile. TestConsumerUnionRefusesUnclassifiedTypes
// builds this file with -tags compilefail and asserts the compiler rejects all
// of them — the classification guarantee is only real if it is a build failure
// in the consumer's OWN package, not just inside plugin.
//
// It matters more in v2 than it did in v1. plugin's own generics are now
// [Req, Resp any], because the mechanism has to be shareable across packages
// that each own a different union. That makes this file the only thing standing
// between a consumer and sending an unclassified type: if the generated
// wrappers stopped being constrained by Payload, everything here would start
// compiling and nothing else would notice.
package consumer

import (
	"context"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

func mustNotCompileUnclassified(ctx context.Context, b bus.Bus) {
	_, _ = Call(ctx, b, Command[Unclassified, Pong]{}, Unclassified{})
}

func mustNotCompileForgedByEmbedding(ctx context.Context, b bus.Bus) {
	_, _ = Call(ctx, b, Command[Forged, Pong]{}, Forged{})
}

func mustNotCompileUnclassifiedEvent(ctx context.Context, b bus.Bus) {
	_ = Emit(ctx, b, Event[Unclassified]{}, Unclassified{})
}

func mustNotCompileForgedBucket(ctx context.Context, b bus.Bus) {
	_, _ = OpenBucket(ctx, b, Bucket[Forged]{})
}
