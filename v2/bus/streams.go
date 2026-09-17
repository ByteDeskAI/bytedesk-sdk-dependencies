package bus

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

// Seq is a stream sequence number. It marshals as a DECIMAL STRING, not a JSON
// number: a uint64 past 2^53 loses precision in every JSON parser that backs a
// browser, and a sequence that silently rounds is a replay that silently skips.
type Seq uint64

// MarshalJSON writes the sequence as a decimal string.
func (s Seq) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatUint(uint64(s), 10) + `"`), nil
}

// UnmarshalJSON accepts a decimal string, and a JSON number for hand-written
// fixtures.
func (s *Seq) UnmarshalJSON(b []byte) error {
	u, err := unmarshalUint(b, "sequence")
	if err != nil {
		return err
	}
	*s = Seq(u)
	return nil
}

func (s Seq) String() string { return strconv.FormatUint(uint64(s), 10) }

// Revision is a KV revision. It marshals as a decimal string for the same
// reason as Seq.
type Revision uint64

// MarshalJSON writes the revision as a decimal string.
func (r Revision) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatUint(uint64(r), 10) + `"`), nil
}

// UnmarshalJSON accepts a decimal string, and a JSON number for hand-written
// fixtures.
func (r *Revision) UnmarshalJSON(b []byte) error {
	u, err := unmarshalUint(b, "revision")
	if err != nil {
		return err
	}
	*r = Revision(u)
	return nil
}

func (r Revision) String() string { return strconv.FormatUint(uint64(r), 10) }

func unmarshalUint(b []byte, what string) (uint64, error) {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		u, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return 0, errors.New("bus: " + what + " is not a decimal string")
		}
		return u, nil
	}
	var n uint64
	if err := json.Unmarshal(b, &n); err != nil {
		return 0, errors.New("bus: " + what + " must be a decimal string")
	}
	return n, nil
}

// Cursor is an opaque replay position. It is minted by the substrate and
// verified by the substrate; a caller may store it, send it to a browser and
// hand it back, and may do nothing else with it. A fabricated cursor is
// refused rather than interpreted, so a cursor is never a way to read a
// position the holder was not given.
type Cursor struct {
	token string
}

// NewCursor wraps a substrate-minted token. Substrates call it.
func NewCursor(token string) Cursor { return Cursor{token: token} }

// Token returns the opaque token for transport. It is not a sequence number
// and must not be parsed.
func (c Cursor) Token() string { return c.token }

// IsZero reports whether the cursor is unset.
func (c Cursor) IsZero() bool { return c.token == "" }

// MarshalJSON writes the opaque token.
func (c Cursor) MarshalJSON() ([]byte, error) { return json.Marshal(c.token) }

// UnmarshalJSON reads an opaque token.
func (c *Cursor) UnmarshalJSON(b []byte) error { return json.Unmarshal(b, &c.token) }

// Retention is how a stream discards.
type Retention string

const (
	// RetentionLimits discards only by the declared limits. It is the only
	// retention a browser or an ephemeral ordered consumer may resume from,
	// because an interest stream with no live consumer discards immediately
	// and nothing survives a cutover (R6).
	RetentionLimits Retention = "limits"
	// RetentionInterest discards once every durable consumer has acked. Use
	// it only where every consumer is durable.
	RetentionInterest Retention = "interest"
	// RetentionWorkQueue discards on the single ack.
	RetentionWorkQueue Retention = "workqueue"
)

// AckPolicy is how a consumer acknowledges.
type AckPolicy string

const (
	// AckNone means delivery is fire-and-forget.
	AckNone AckPolicy = "none"
	// AckAll means acking a sequence acks everything before it.
	AckAll AckPolicy = "all"
	// AckExplicit means every message is acked individually.
	AckExplicit AckPolicy = "explicit"
)

// StartPolicy is where a consumer begins.
type StartPolicy string

const (
	// StartAll replays from the beginning.
	StartAll StartPolicy = "all"
	// StartNew delivers only messages published after the consumer exists.
	StartNew StartPolicy = "new"
	// StartCursor resumes from ConsumerSpec.Cursor.
	StartCursor StartPolicy = "cursor"
	// StartTime resumes from ConsumerSpec.StartTime.
	StartTime StartPolicy = "time"
)

// StreamSpec declares a stream. Declare is idempotent, and the host provisions
// only streams the manifest lists.
//
// MaxBytes and MaxAge are both REQUIRED. An unbounded stream is how a gateway
// fills a disk quietly; the declaration is refused rather than defaulted.
type StreamSpec struct {
	Name     string
	Subjects []Pattern
	// Retention defaults to RetentionLimits when empty.
	Retention Retention
	MaxAge    time.Duration
	MaxBytes  int64
	MaxMsgs   int64
	// MaxMsgsPerSubject bounds per-subject history; 1 makes the stream a
	// last-value cache.
	MaxMsgsPerSubject int64
	// Dedupe is the window in which a repeated MsgID is discarded.
	Dedupe time.Duration
	// Compression asks the substrate to compress at rest where it can.
	Compression bool
	// AllowTTL, AllowSchedules, AllowCounters and AllowBatch opt into the
	// capabilities of the same name for this stream.
	AllowTTL       bool
	AllowSchedules bool
	AllowCounters  bool
	AllowBatch     bool
}

// ConsumerSpec declares a consumer. A consumer is durable exactly when Name is
// set; an unnamed consumer is ephemeral and disappears with its subscription.
type ConsumerSpec struct {
	Name string
	// Ordered asks for a single-threaded, gap-detecting ephemeral consumer.
	Ordered bool
	// Filter narrows the stream's subjects.
	Filter Pattern
	// Ack defaults to AckExplicit when empty.
	Ack           AckPolicy
	AckWait       time.Duration
	MaxDeliver    int
	MaxAckPending int
	// Start defaults to StartAll when empty.
	Start     StartPolicy
	Cursor    Cursor
	StartTime time.Time
	// PriorityGroup, Priority and Overflow select the substrate's priority
	// scheduling where it has any.
	PriorityGroup string
	Priority      int
	Overflow      string
}

// StreamInfo is a stream's observed state.
type StreamInfo struct {
	Name     string
	Subjects []Pattern
	Msgs     uint64
	Bytes    uint64
	FirstSeq Seq
	LastSeq  Seq
	Created  time.Time
}

// Streams is durable, replayable messaging.
type Streams interface {
	// Declare creates or confirms a stream. It is idempotent, and refuses a
	// stream the manifest does not list, a spec without MaxBytes and MaxAge,
	// and a subject outside the caller's own namespace.
	Declare(ctx context.Context, spec StreamSpec) error
	// Publish appends one message and returns its sequence.
	Publish(ctx context.Context, subject Subject, data []byte, opts ...PublishOpt) (Seq, error)
	// PublishBatch appends every message or none.
	PublishBatch(ctx context.Context, msgs []BatchMsg) ([]Seq, error)
	// Counter atomically adds delta to the counter at subject and returns the
	// new value.
	Counter(ctx context.Context, subject Subject, delta int64) (int64, error)
	// Consume delivers to h until the Consumer is cancelled.
	Consume(ctx context.Context, stream string, spec ConsumerSpec, h StreamHandler) (Consumer, error)
	// Fetch pulls up to n messages, blocking until one arrives or ctx expires.
	Fetch(ctx context.Context, stream string, spec ConsumerSpec, n int) ([]*StreamMsg, error)
	// Purge discards messages matching filter, or the whole stream when
	// filter is empty.
	Purge(ctx context.Context, stream string, filter Pattern) error
	// Info reports a stream's state.
	Info(ctx context.Context, stream string) (StreamInfo, error)
	// Delete removes a stream and everything in it.
	Delete(ctx context.Context, stream string) error
}

// BatchMsg is one message in an atomic batch.
type BatchMsg struct {
	Subject Subject
	Data    []byte
	Opts    []PublishOpt
}

// StreamHandler receives one stored message. It must Ack, Nak or Term under
// AckExplicit; a handler that returns without acking will be redelivered.
type StreamHandler func(ctx context.Context, m *StreamMsg)

// StreamMsg is a stored message plus its position and acknowledgement.
type StreamMsg struct {
	Msg
	// Seq is this message's position in the stream.
	Seq Seq
	// Time is when the substrate stored it.
	Time time.Time
	// Delivered counts how many times it has been delivered, starting at 1.
	Delivered int

	ack func(kind string, delay time.Duration, reason string) error
}

// NewStreamMsg builds a stored message with its acknowledgement installed.
// Substrates call it.
func NewStreamMsg(m Msg, seq Seq, at time.Time, delivered int, ack func(kind string, delay time.Duration, reason string) error) *StreamMsg {
	return &StreamMsg{Msg: m, Seq: seq, Time: at, Delivered: delivered, ack: ack}
}

// Ack confirms the message is handled and will not be redelivered.
func (m *StreamMsg) Ack() error { return m.acknowledge("ack", 0, "") }

// Nak asks for redelivery after delay. Zero delay means immediately.
func (m *StreamMsg) Nak(delay time.Duration) error { return m.acknowledge("nak", delay, "") }

// Term refuses the message permanently, with a reason for the operator.
func (m *StreamMsg) Term(reason string) error { return m.acknowledge("term", 0, reason) }

// InProgress extends the ack deadline for a handler that is still working.
func (m *StreamMsg) InProgress() error { return m.acknowledge("progress", 0, "") }

func (m *StreamMsg) acknowledge(kind string, delay time.Duration, reason string) error {
	if m == nil || m.ack == nil {
		return Fault{Code: FaultUnhandled, Message: "message carries no acknowledgement"}
	}
	return m.ack(kind, delay, reason)
}

// ConsumerInfo is a consumer's observed state.
type ConsumerInfo struct {
	Stream        string
	Name          string
	Delivered     Seq
	AckFloor      Seq
	NumPending    uint64
	NumAckPending int
}

// Consumer is a live stream subscription.
type Consumer interface {
	Subscription
	// Cursor returns an opaque position to resume from. Store it, hand it
	// back to Consume with StartCursor; do not parse it.
	Cursor() Cursor
	// Reset moves the consumer back to its spec's start position.
	Reset(ctx context.Context) error
	// Info reports the consumer's state.
	Info(ctx context.Context) (ConsumerInfo, error)
}
