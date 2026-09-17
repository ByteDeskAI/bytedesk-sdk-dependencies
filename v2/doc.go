// Package sdkv2 is the root of the ByteDesk SDK v2 module. It declares no API.
//
// Its only contents are the module's own gates, which are tests:
//
//   - boundary_test.go     no broker type reaches an SDK-public signature
//   - inventory_test.go    every exported type is actually walked by that gate
//   - version_policy_test.go   the module is released, not pinned to a checkout
//
// The API lives in bus, bus/memory, bus/conformance, plugin, plugin/v1compat,
// messaging, pack and cmd/contractgen.
package sdkv2
