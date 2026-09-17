package memory_test

import (
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus/conformance"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus/memory"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/plugin"
)

// TestMemorySatisfiesTheBusContract is the point of the conformance package.
//
// The property list is the contract for the whole substrate programme: the
// gateway runs the same list against its own in-memory substrate and against
// embedded NATS, and a plugin author runs it against this double. One list,
// several runners. A substrate that passes it is interchangeable with the
// others; one that does not is not shipped.
//
// Default is set, which is the strict reading: a property in the
// requiredForDefault set may not SKIP here. The double advertises every
// capability but Trace, and no required property is gated on Trace, so a skip
// in that set would mean a capability regressed rather than that this
// substrate was never meant to carry it.
//
// Deny comes from plugin.PermanentlyIneligible rather than a list written out
// here. The families a credential may never reach are declared once, and the
// test that proves deny wins reads the same declaration the validator and the
// host's grant compiler read — a deny set proven against a copy of itself
// proves nothing.
func TestMemorySatisfiesTheBusContract(t *testing.T) {
	deny := plugin.PermanentlyIneligible()
	if len(deny) == 0 {
		t.Fatal("PermanentlyIneligible is empty, so PermanentDenyWins would pass vacuously")
	}

	var store *memory.Store
	newStore := func() {
		store = memory.NewStore(memory.WithDeny(deny...))
	}
	newStore()
	t.Cleanup(func() { store.Close() })

	conformance.Run(t, conformance.Harness{
		New: func(t *testing.T, id bus.Identity) bus.Bus {
			return store.Connect(id)
		},
		Restart: func(t *testing.T) {
			// Restart keeps durable state and drops every connection, which
			// is what a substrate restart looks like from a plugin's side.
			store.Restart()
		},
		Revoke: func(t *testing.T, pluginID string) {
			store.Revoke(pluginID)
		},
		Caps:    store.Connect(bus.Identity{PluginID: "conformance-probe"}).Capabilities(),
		Deny:    deny,
		Default: true,
		Budget:  5 * time.Second,
	})
}
