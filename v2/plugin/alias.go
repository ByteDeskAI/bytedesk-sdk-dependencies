package plugin

import "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"

// The SDK's public re-export.
//
// A plugin author imports one package. Everything the bus contract declares is
// reachable as plugin.X, so an author writes plugin.Identity and plugin.Fault
// without ever naming the bus package — and the gateway, which aliases these
// again, ends up pointing at the same declarations rather than at a parallel
// set of its own.
//
// Every entry below is an ALIAS (=), which makes plugin.Grants and bus.Grants
// the SAME type rather than two convertible ones. That distinction is the whole
// point of the file. A local definition would compile, satisfy the same call
// sites and drift silently the first time the bus contract changed; the
// gateway's TestManifestTypesComeFromSDK exists because exactly that already
// happened once. Nothing in this package may DECLARE a bus type.

// Addressing and messages.
type (
	// Bus is the single messaging surface a plugin has.
	Bus = bus.Bus
	// Subject is a concrete, wildcard-free address.
	Subject = bus.Subject
	// Pattern is a subject filter.
	Pattern = bus.Pattern
	// Msg is one message as a handler sees it.
	Msg = bus.Msg
	// Headers are message headers, lowercase-keyed on ingress.
	Headers = bus.Headers
	// Handler receives one message.
	Handler = bus.Handler
	// Subscription is a live subscription.
	Subscription = bus.Subscription
	// PublishOpt configures one Publish.
	PublishOpt = bus.PublishOpt
)

// Identity, grants and refusals.
type (
	// Identity is who a bound bus belongs to, for one generation.
	Identity = bus.Identity
	// Grants are the effective patterns for one principal in one generation.
	Grants = bus.Grants
	// GrantKind names which list of the effective grants Can reads.
	GrantKind = bus.GrantKind
	// Lease is an opaque subject lease.
	Lease = bus.Lease
	// Role names the class of principal a connection belongs to.
	Role = bus.Role
	// Capabilities are the substrate's features, not the principal's grants.
	Capabilities = bus.Capabilities
	// Fault is a typed bus error. Match it with errors.As and switch on Code.
	Fault = bus.Fault
	// Caller is the principal pair a handler sees.
	Caller = bus.Caller
)

// Durable messaging, storage and scheduling.
type (
	// Seq is a stream sequence number.
	Seq = bus.Seq
	// Revision is a KV revision.
	Revision = bus.Revision
	// Cursor is an opaque replay position.
	Cursor = bus.Cursor
	// Streams is durable, replayable messaging.
	Streams = bus.Streams
	// StreamMsg is a stored message plus its position and acknowledgement.
	StreamMsg = bus.StreamMsg
	// Consumer is a live stream subscription.
	Consumer = bus.Consumer
	// ConsumerSpec declares a consumer.
	ConsumerSpec = bus.ConsumerSpec
	// KV is the key/value store.
	KV = bus.KV
	// Bucket is one key/value bucket.
	Bucket = bus.Bucket
	// Objects stores blobs by reference.
	Objects = bus.Objects
	// Services is request/reply with discovery, versioning and stats.
	Services = bus.Services
	// Service is a mounted service.
	Service = bus.Service
	// ServiceSpec declares a service.
	ServiceSpec = bus.ServiceSpec
	// EndpointSpec is one operation of a service.
	EndpointSpec = bus.EndpointSpec
	// Scheduler is durable scheduled publishing.
	Scheduler = bus.Scheduler
	// Trace propagates correlation across calls.
	Trace = bus.Trace
)

// Fault codes. A caller switches on these instead of string-matching the
// substrate's error text.
const (
	FaultDenied       = bus.FaultDenied
	FaultBudget       = bus.FaultBudget
	FaultWithdrawn    = bus.FaultWithdrawn
	FaultSchema       = bus.FaultSchema
	FaultUnhandled    = bus.FaultUnhandled
	FaultTimeout      = bus.FaultTimeout
	FaultUnavailable  = bus.FaultUnavailable
	FaultNoResponders = bus.FaultNoResponders
	FaultUnsupported  = bus.FaultUnsupported
	FaultConflict     = bus.FaultConflict
	FaultNotFound     = bus.FaultNotFound
	FaultSlowConsumer = bus.FaultSlowConsumer
)

// Reserved headers. The bd- prefix belongs to the host: a plugin cannot set
// one, and the substrate strips any it is handed.
const (
	HeaderPrefix      = bus.HeaderPrefix
	HeaderSchema      = bus.HeaderSchema
	HeaderCaller      = bus.HeaderCaller
	HeaderGeneration  = bus.HeaderGeneration
	HeaderSubject     = bus.HeaderSubject
	HeaderFault       = bus.HeaderFault
	HeaderCorrelation = bus.HeaderCorrelation
	HeaderTraceparent = bus.HeaderTraceparent
	HeaderMsgID       = bus.HeaderMsgID
)

// Grant kinds, for Grants.Can.
const (
	GrantPublish   = bus.GrantPublish
	GrantSubscribe = bus.GrantSubscribe
	GrantRequest   = bus.GrantRequest
	GrantServe     = bus.GrantServe
)

// Principal roles.
const (
	RoleHost          = bus.RoleHost
	RolePlugin        = bus.RolePlugin
	RoleUI            = bus.RoleUI
	RoleOrchestration = bus.RoleOrchestration
)

// Go has no function alias, so the three functions an author needs are thin
// wrappers rather than package variables. A variable would be re-assignable,
// and a re-assignable ParseSubject is a way to make the one matcher in the
// system disagree with itself at runtime.

// ParseSubject validates s as a concrete subject: no wildcards.
func ParseSubject(s string) (Subject, error) { return bus.ParseSubject(s) }

// ParsePattern validates s as a subject filter: "*" and a trailing ">".
func ParsePattern(s string) (Pattern, error) { return bus.ParsePattern(s) }

// CallerOf reads the caller identity the SUBSTRATE stamped on m. It is a read
// of an already-verified field, not a verification: the transport re-derives
// caller identity from the connection's own credential on ingress.
func CallerOf(m *Msg) Caller { return bus.CallerOf(m) }
