package bus

import "strings"

// Fault codes. A caller distinguishes denied from budget-exhausted from
// withdrawn instead of string-matching the substrate's error text
// (ADR 0025 §8). The v1 set is kept verbatim; v2 adds the five codes a broker
// can produce that an in-process bus never could.
const (
	FaultDenied      = "denied"
	FaultBudget      = "budget"
	FaultWithdrawn   = "withdrawn"
	FaultSchema      = "schema"
	FaultUnhandled   = "unhandled"
	FaultTimeout     = "timeout"
	FaultUnavailable = "unavailable"

	// FaultNoResponders means nothing is listening on the request subject.
	// It is returned fast — it is not a timeout.
	FaultNoResponders = "no-responders"
	// FaultUnsupported means the substrate does not implement the capability.
	// Check Bus.Capabilities() first; a manifest "needs" entry fails closed at
	// enable so a running plugin should not see this.
	FaultUnsupported = "unsupported"
	// FaultConflict means an optimistic-concurrency precondition failed
	// (KV Update with a stale revision, Create over an existing key,
	// ExpectLastSeq on a stream publish).
	FaultConflict = "conflict"
	// FaultNotFound means the named stream, consumer, bucket, key or object
	// does not exist.
	FaultNotFound = "not-found"
	// FaultSlowConsumer means a subscription exceeded its PendingLimit.
	// It is delivered on the subscription's Err, never silently dropped.
	FaultSlowConsumer = "slow-consumer"
)

// Fault is a typed bus error. Match it with errors.As and switch on Code.
//
// A refusal is always attributed: Op names the subject or asset that was
// refused, and Message names the principal and the reason. A refusal that
// names neither is a bug in the substrate, and conformance property
// LoudAttributedRefusal fails on it.
type Fault struct {
	Code    string
	Op      string
	Message string
	Err     error
}

func (f Fault) Error() string {
	parts := make([]string, 0, 3)
	if f.Op != "" {
		parts = append(parts, f.Op)
	}
	parts = append(parts, f.Code)
	if f.Message != "" {
		parts = append(parts, f.Message)
	}
	return strings.Join(parts, ": ")
}

func (f Fault) Unwrap() error { return f.Err }

// Denied builds the standard attributed refusal: the subject AND the principal
// appear in the error, because an unattributed refusal is unactionable for an
// operator reading a log.
func Denied(op, principal, why string) Fault {
	msg := "principal " + principal
	if why != "" {
		msg += ": " + why
	}
	return Fault{Code: FaultDenied, Op: op, Message: msg}
}

// Unsupported builds the refusal a substrate returns for a capability it does
// not implement.
func Unsupported(op, capability string) Fault {
	return Fault{Code: FaultUnsupported, Op: op, Message: "substrate does not support " + capability}
}
