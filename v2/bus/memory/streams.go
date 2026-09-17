package memory

import (
	"context"
	"sync"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// defaultAckWait is the redelivery deadline applied when ConsumerSpec.AckWait
// is zero. It matches the broker default a plugin author will meet in
// production, so a handler that forgets to ack behaves the same in both.
const defaultAckWait = 30 * time.Second

type storedMsg struct {
	seq     bus.Seq
	subject bus.Subject
	headers bus.Headers
	data    []byte
	at      time.Time
	expires time.Time
}

func (m *storedMsg) size() uint64 { return uint64(len(m.data)) }

type dedupeEntry struct {
	seq bus.Seq
	at  time.Time
}

type stream struct {
	spec     bus.StreamSpec
	created  time.Time
	msgs     []*storedMsg
	lastSeq  bus.Seq
	bytes    uint64
	counters map[bus.Subject]int64
	dedupe   map[string]dedupeEntry
	durables map[string]*consumerState
}

func (st *stream) firstSeq() bus.Seq {
	if len(st.msgs) == 0 {
		return st.lastSeq + 1
	}
	return st.msgs[0].seq
}

func (st *stream) covers(subject bus.Subject) bool {
	for _, p := range st.spec.Subjects {
		if p.Matches(subject) {
			return true
		}
	}
	return false
}

// expire drops messages past MaxAge or past their own TTL. It runs on publish
// and before every read, so an expired message is never delivered.
func (st *stream) expire(now time.Time) {
	if len(st.msgs) == 0 {
		return
	}
	kept := st.msgs[:0]
	for _, m := range st.msgs {
		dead := false
		if st.spec.MaxAge > 0 && now.Sub(m.at) > st.spec.MaxAge {
			dead = true
		}
		if !m.expires.IsZero() && now.After(m.expires) {
			dead = true
		}
		if dead {
			st.bytes -= m.size()
			continue
		}
		kept = append(kept, m)
	}
	for i := len(kept); i < len(st.msgs); i++ {
		st.msgs[i] = nil
	}
	st.msgs = kept
}

func (st *stream) trim() {
	if st.spec.MaxMsgsPerSubject > 0 {
		counts := map[bus.Subject]int64{}
		for _, m := range st.msgs {
			counts[m.subject]++
		}
		over := map[bus.Subject]int64{}
		for s, n := range counts {
			if n > st.spec.MaxMsgsPerSubject {
				over[s] = n - st.spec.MaxMsgsPerSubject
			}
		}
		if len(over) > 0 {
			kept := st.msgs[:0]
			for _, m := range st.msgs {
				if over[m.subject] > 0 {
					over[m.subject]--
					st.bytes -= m.size()
					continue
				}
				kept = append(kept, m)
			}
			for i := len(kept); i < len(st.msgs); i++ {
				st.msgs[i] = nil
			}
			st.msgs = kept
		}
	}
	for st.spec.MaxMsgs > 0 && int64(len(st.msgs)) > st.spec.MaxMsgs {
		st.bytes -= st.msgs[0].size()
		st.msgs[0] = nil
		st.msgs = st.msgs[1:]
	}
	for st.spec.MaxBytes > 0 && int64(st.bytes) > st.spec.MaxBytes && len(st.msgs) > 0 {
		st.bytes -= st.msgs[0].size()
		st.msgs[0] = nil
		st.msgs = st.msgs[1:]
	}
}

// ---------------------------------------------------------------- consumers

type consumerState struct {
	name       string
	stream     string
	spec       bus.ConsumerSpec
	next       bus.Seq
	ackFloor   bus.Seq
	acked      map[bus.Seq]bool
	pending    map[bus.Seq]time.Time
	deliveries map[bus.Seq]int
	started    bus.Seq
}

func newConsumerState(name, stream string, spec bus.ConsumerSpec, start bus.Seq) *consumerState {
	return &consumerState{
		name:       name,
		stream:     stream,
		spec:       spec,
		next:       start,
		started:    start,
		ackFloor:   start - 1,
		acked:      map[bus.Seq]bool{},
		pending:    map[bus.Seq]time.Time{},
		deliveries: map[bus.Seq]int{},
	}
}

func (cs *consumerState) markAcked(seq bus.Seq) {
	delete(cs.pending, seq)
	cs.acked[seq] = true
	for cs.acked[cs.ackFloor+1] {
		cs.ackFloor++
		delete(cs.acked, cs.ackFloor)
		delete(cs.deliveries, cs.ackFloor)
	}
}

func (cs *consumerState) ackAllThrough(seq bus.Seq) {
	for s := cs.ackFloor + 1; s <= seq; s++ {
		cs.markAcked(s)
	}
}

func (cs *consumerState) numPending(st *stream) uint64 {
	var n uint64
	for _, m := range st.msgs {
		if m.seq >= cs.next {
			n++
		}
	}
	return n + uint64(len(cs.pending))
}

// ------------------------------------------------------------------ Streams

type streams struct{ c *conn }

func (c *conn) Streams() bus.Streams { return streams{c: c} }

func (s streams) off(op string) error {
	if !s.c.store.cfg.caps.Durable {
		return unsupported(op, "durable")
	}
	return s.c.alive()
}

func (s streams) deny(name string) error {
	if !s.c.id.Grants.CanUse(bus.AssetStream, name) {
		return bus.Denied(name, s.c.id.String(), "stream is not provisioned for this principal")
	}
	return nil
}

func (s streams) Declare(ctx context.Context, spec bus.StreamSpec) error {
	if err := s.off("stream:" + spec.Name); err != nil {
		return err
	}
	if spec.Name == "" {
		return bus.Fault{Code: bus.FaultSchema, Op: "stream:", Message: "principal " + s.c.id.String() + ": stream name is empty"}
	}
	if err := s.deny(spec.Name); err != nil {
		return err
	}
	if spec.MaxBytes == 0 || spec.MaxAge == 0 {
		return bus.Fault{
			Code:    bus.FaultSchema,
			Op:      "stream:" + spec.Name,
			Message: "principal " + s.c.id.String() + ": MaxBytes and MaxAge are both required; an unbounded stream is refused, not defaulted",
		}
	}
	for _, p := range spec.Subjects {
		if _, err := bus.ParsePattern(string(p)); err != nil {
			return bus.Fault{Code: bus.FaultSchema, Op: "stream:" + spec.Name, Message: err.Error(), Err: err}
		}
		if s.c.store.denyPattern(p) {
			return bus.Denied("stream:"+spec.Name, s.c.id.String(), "subject "+string(p)+" is permanently ineligible")
		}
	}
	if spec.Retention == "" {
		spec.Retention = bus.RetentionLimits
	}
	st := s.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.streams[spec.Name]; ok {
		// Idempotent: a repeat declaration confirms the stream and leaves the
		// stored messages alone.
		return nil
	}
	st.streams[spec.Name] = &stream{
		spec:     spec,
		created:  nowUTC(),
		counters: map[bus.Subject]int64{},
		dedupe:   map[string]dedupeEntry{},
		durables: map[string]*consumerState{},
	}
	return nil
}

// lookup finds the stream by name, enforcing the principal's asset grant.
func (s streams) lookup(name string) (*stream, error) {
	if err := s.deny(name); err != nil {
		return nil, err
	}
	st, ok := s.c.store.streams[name]
	if !ok {
		return nil, bus.Fault{Code: bus.FaultNotFound, Op: "stream:" + name, Message: "principal " + s.c.id.String() + ": no such stream"}
	}
	return st, nil
}

// routeLocked finds the one stream this principal may use whose subjects cover
// the subject.
func (s streams) routeLocked(subject bus.Subject) (*stream, error) {
	for name, st := range s.c.store.streams {
		if !s.c.id.Grants.CanUse(bus.AssetStream, name) {
			continue
		}
		if st.covers(subject) {
			return st, nil
		}
	}
	return nil, bus.Fault{
		Code:    bus.FaultNotFound,
		Op:      string(subject),
		Message: "principal " + s.c.id.String() + ": no stream this principal may use covers this subject",
	}
}

func (s streams) checkPublish(subject bus.Subject, data []byte) error {
	if _, err := bus.ParseSubject(string(subject)); err != nil {
		return bus.Fault{Code: bus.FaultSchema, Op: string(subject), Message: err.Error(), Err: err}
	}
	if s.c.store.denySubject(subject) {
		return bus.Denied(string(subject), s.c.id.String(), "subject is permanently ineligible")
	}
	if max := s.c.store.cfg.caps.MaxPayload; max > 0 && len(data) > max {
		return bus.Fault{
			Code:    bus.FaultBudget,
			Op:      string(subject),
			Message: "principal " + s.c.id.String() + ": payload " + formatUint(uint64(len(data))) + " bytes exceeds MaxPayload " + formatUint(uint64(max)),
		}
	}
	return nil
}

func (s streams) Publish(ctx context.Context, subject bus.Subject, data []byte, opts ...bus.PublishOpt) (bus.Seq, error) {
	if err := s.off(string(subject)); err != nil {
		return 0, err
	}
	if err := s.checkPublish(subject, data); err != nil {
		return 0, err
	}
	o := bus.ResolvePublish(opts)
	store := s.c.store
	store.mu.Lock()
	defer store.mu.Unlock()
	st, err := s.routeLocked(subject)
	if err != nil {
		return 0, err
	}
	seq, err := s.appendLocked(ctx, st, subject, data, o)
	if err != nil {
		return 0, err
	}
	st.trim()
	return seq, nil
}

func (s streams) appendLocked(ctx context.Context, st *stream, subject bus.Subject, data []byte, o bus.PublishOptions) (bus.Seq, error) {
	now := nowUTC()
	st.expire(now)
	if o.MsgID != "" && st.spec.Dedupe > 0 {
		if prev, ok := st.dedupe[o.MsgID]; ok && now.Sub(prev.at) <= st.spec.Dedupe {
			// A repeat inside the window is accepted and not stored twice.
			return prev.seq, nil
		}
	}
	if o.ExpectLastSeq != 0 && o.ExpectLastSeq != st.lastSeq {
		return 0, bus.Fault{
			Code: bus.FaultConflict,
			Op:   string(subject),
			Message: "principal " + s.c.id.String() + ": expected last sequence " + o.ExpectLastSeq.String() +
				" but stream " + st.spec.Name + " is at " + st.lastSeq.String(),
		}
	}
	st.lastSeq++
	m := &storedMsg{
		seq:     st.lastSeq,
		subject: subject,
		headers: s.c.stamp(ctx, o.Headers),
		data:    clone(data),
		at:      now,
	}
	if o.MsgID != "" {
		m.headers[bus.HeaderMsgID] = o.MsgID
		st.dedupe[o.MsgID] = dedupeEntry{seq: m.seq, at: now}
	}
	if o.TTL > 0 {
		m.expires = now.Add(o.TTL)
	}
	st.msgs = append(st.msgs, m)
	st.bytes += m.size()
	return m.seq, nil
}

// PublishBatch appends every message or none: the whole batch is validated
// against the payload ceiling, the deny list and ExpectLastSeq before any of
// it is stored.
func (s streams) PublishBatch(ctx context.Context, msgs []bus.BatchMsg) ([]bus.Seq, error) {
	if err := s.off("stream:batch"); err != nil {
		return nil, err
	}
	if !s.c.store.cfg.caps.Batch {
		return nil, unsupported("stream:batch", "batch")
	}
	if len(msgs) == 0 {
		return nil, nil
	}
	for _, m := range msgs {
		if err := s.checkPublish(m.Subject, m.Data); err != nil {
			return nil, err
		}
	}
	store := s.c.store
	store.mu.Lock()
	defer store.mu.Unlock()

	type plan struct {
		st   *stream
		msg  bus.BatchMsg
		opts bus.PublishOptions
	}
	plans := make([]plan, 0, len(msgs))
	// Pre-flight: resolve every stream and every precondition against the
	// sequence the batch WILL have reached, so a batch that would conflict
	// halfway is refused whole.
	projected := map[*stream]bus.Seq{}
	for _, m := range msgs {
		st, err := s.routeLocked(m.Subject)
		if err != nil {
			return nil, err
		}
		if _, ok := projected[st]; !ok {
			projected[st] = st.lastSeq
		}
		o := bus.ResolvePublish(m.Opts)
		if o.ExpectLastSeq != 0 && o.ExpectLastSeq != projected[st] {
			return nil, bus.Fault{
				Code: bus.FaultConflict,
				Op:   string(m.Subject),
				Message: "principal " + s.c.id.String() + ": expected last sequence " + o.ExpectLastSeq.String() +
					" but stream " + st.spec.Name + " is at " + projected[st].String(),
			}
		}
		projected[st]++
		plans = append(plans, plan{st: st, msg: m, opts: o})
	}
	out := make([]bus.Seq, 0, len(plans))
	touched := map[*stream]struct{}{}
	for _, p := range plans {
		seq, err := s.appendLocked(ctx, p.st, p.msg.Subject, p.msg.Data, bus.PublishOptions{
			Headers: p.opts.Headers,
			MsgID:   p.opts.MsgID,
			TTL:     p.opts.TTL,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, seq)
		touched[p.st] = struct{}{}
	}
	for st := range touched {
		st.trim()
	}
	return out, nil
}

func (s streams) Counter(ctx context.Context, subject bus.Subject, delta int64) (int64, error) {
	if err := s.off(string(subject)); err != nil {
		return 0, err
	}
	if !s.c.store.cfg.caps.Counters {
		return 0, unsupported(string(subject), "counters")
	}
	if err := s.checkPublish(subject, nil); err != nil {
		return 0, err
	}
	store := s.c.store
	store.mu.Lock()
	defer store.mu.Unlock()
	st, err := s.routeLocked(subject)
	if err != nil {
		return 0, err
	}
	st.counters[subject] += delta
	return st.counters[subject], nil
}

func (s streams) Purge(ctx context.Context, name string, filter bus.Pattern) error {
	if err := s.off("stream:" + name); err != nil {
		return err
	}
	store := s.c.store
	store.mu.Lock()
	defer store.mu.Unlock()
	st, err := s.lookup(name)
	if err != nil {
		return err
	}
	if filter == "" {
		st.bytes = 0
		st.msgs = nil
		return nil
	}
	if _, err := bus.ParsePattern(string(filter)); err != nil {
		return bus.Fault{Code: bus.FaultSchema, Op: "stream:" + name, Message: err.Error(), Err: err}
	}
	kept := st.msgs[:0]
	for _, m := range st.msgs {
		if filter.Matches(m.subject) {
			st.bytes -= m.size()
			continue
		}
		kept = append(kept, m)
	}
	for i := len(kept); i < len(st.msgs); i++ {
		st.msgs[i] = nil
	}
	st.msgs = kept
	return nil
}

func (s streams) Info(ctx context.Context, name string) (bus.StreamInfo, error) {
	if err := s.off("stream:" + name); err != nil {
		return bus.StreamInfo{}, err
	}
	store := s.c.store
	store.mu.Lock()
	defer store.mu.Unlock()
	st, err := s.lookup(name)
	if err != nil {
		return bus.StreamInfo{}, err
	}
	st.expire(nowUTC())
	return bus.StreamInfo{
		Name:     st.spec.Name,
		Subjects: append([]bus.Pattern(nil), st.spec.Subjects...),
		Msgs:     uint64(len(st.msgs)),
		Bytes:    st.bytes,
		FirstSeq: st.firstSeq(),
		LastSeq:  st.lastSeq,
		Created:  st.created,
	}, nil
}

func (s streams) Delete(ctx context.Context, name string) error {
	if err := s.off("stream:" + name); err != nil {
		return err
	}
	store := s.c.store
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, err := s.lookup(name); err != nil {
		return err
	}
	delete(store.streams, name)
	return nil
}

// startSeqLocked resolves a ConsumerSpec's start policy to a sequence.
func (s streams) startSeqLocked(st *stream, spec bus.ConsumerSpec) (bus.Seq, error) {
	switch spec.Start {
	case "", bus.StartAll:
		return st.firstSeq(), nil
	case bus.StartNew:
		return st.lastSeq + 1, nil
	case bus.StartCursor:
		seq, err := s.c.store.readCursor(spec.Cursor, st.spec.Name)
		if err != nil {
			return 0, err
		}
		return seq + 1, nil
	case bus.StartTime:
		for _, m := range st.msgs {
			if !m.at.Before(spec.StartTime) {
				return m.seq, nil
			}
		}
		return st.lastSeq + 1, nil
	}
	return 0, bus.Fault{Code: bus.FaultSchema, Op: "stream:" + st.spec.Name, Message: "principal " + s.c.id.String() + ": unknown start policy " + string(spec.Start)}
}

// stateLocked resolves the consumer state a Consume or Fetch should use:
// a durable one stored on the stream, or an ephemeral one owned by the caller.
func (s streams) stateLocked(st *stream, spec bus.ConsumerSpec, ephemeralKey string) (*consumerState, error) {
	explicitCursor := spec.Start == bus.StartCursor && !spec.Cursor.IsZero()
	if spec.Name != "" {
		if cs, ok := st.durables[spec.Name]; ok && !explicitCursor {
			cs.spec = spec
			return cs, nil
		}
		start, err := s.startSeqLocked(st, spec)
		if err != nil {
			return nil, err
		}
		cs := newConsumerState(spec.Name, st.spec.Name, spec, start)
		st.durables[spec.Name] = cs
		return cs, nil
	}
	if ephemeralKey != "" && !explicitCursor {
		if cs, ok := s.c.fetchers[ephemeralKey]; ok {
			cs.spec = spec
			return cs, nil
		}
	}
	start, err := s.startSeqLocked(st, spec)
	if err != nil {
		return nil, err
	}
	cs := newConsumerState(s.c.store.newIDLocked("eph"), st.spec.Name, spec, start)
	if ephemeralKey != "" {
		if s.c.fetchers == nil {
			s.c.fetchers = map[string]*consumerState{}
		}
		s.c.fetchers[ephemeralKey] = cs
	}
	return cs, nil
}

func (s streams) Consume(ctx context.Context, name string, spec bus.ConsumerSpec, h bus.StreamHandler) (bus.Consumer, error) {
	if err := s.off("stream:" + name); err != nil {
		return nil, err
	}
	if h == nil {
		return nil, bus.Fault{Code: bus.FaultSchema, Op: "stream:" + name, Message: "principal " + s.c.id.String() + ": handler is nil"}
	}
	if spec.Filter != "" {
		if _, err := bus.ParsePattern(string(spec.Filter)); err != nil {
			return nil, bus.Fault{Code: bus.FaultSchema, Op: "stream:" + name, Message: err.Error(), Err: err}
		}
	}
	store := s.c.store
	store.mu.Lock()
	st, err := s.lookup(name)
	if err != nil {
		store.mu.Unlock()
		return nil, err
	}
	cs, err := s.stateLocked(st, spec, "")
	if err != nil {
		store.mu.Unlock()
		return nil, err
	}
	store.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	cons := &consumer{
		c:       s.c,
		stream:  name,
		st:      cs,
		h:       h,
		ctx:     ctx,
		durable: spec.Name != "",
		doneCh:  make(chan struct{}),
		drained: make(chan struct{}),
		wake:    make(chan struct{}, 1),
	}
	s.c.mu.Lock()
	s.c.consumers = append(s.c.consumers, cons)
	s.c.mu.Unlock()
	go cons.run()
	return cons, nil
}

func (s streams) Fetch(ctx context.Context, name string, spec bus.ConsumerSpec, n int) ([]*bus.StreamMsg, error) {
	if err := s.off("stream:" + name); err != nil {
		return nil, err
	}
	if n <= 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := name + "\x00" + string(spec.Filter) + "\x00" + string(spec.Start) + "\x00" + string(spec.Ack) + "\x00" + spec.Cursor.Token()
	store := s.c.store
	for {
		store.mu.Lock()
		st, err := s.lookup(name)
		if err != nil {
			store.mu.Unlock()
			return nil, err
		}
		cs, err := s.stateLocked(st, spec, key)
		if err != nil {
			store.mu.Unlock()
			return nil, err
		}
		var out []*bus.StreamMsg
		for len(out) < n {
			m := s.nextLocked(st, cs, nowUTC())
			if m == nil {
				break
			}
			out = append(out, s.buildLocked(st, cs, m))
		}
		store.mu.Unlock()
		if len(out) > 0 {
			return out, nil
		}
		select {
		case <-ctx.Done():
			return nil, bus.Fault{Code: bus.FaultTimeout, Op: "stream:" + name, Message: "principal " + s.c.id.String() + ": " + ctx.Err().Error(), Err: ctx.Err()}
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// nextLocked picks the next message to hand a consumer: an expired redelivery
// first, then the next unread one that passes the filter.
func (s streams) nextLocked(st *stream, cs *consumerState, now time.Time) *storedMsg {
	st.expire(now)
	maxDeliver := cs.spec.MaxDeliver
	for seq, deadline := range cs.pending {
		if now.Before(deadline) {
			continue
		}
		if maxDeliver > 0 && cs.deliveries[seq] >= maxDeliver {
			// Out of attempts: terminate it so the consumer can move on, and
			// let the ack floor advance.
			cs.markAcked(seq)
			continue
		}
		if m := st.find(seq); m != nil {
			return m
		}
		cs.markAcked(seq)
	}
	if cs.spec.MaxAckPending > 0 && len(cs.pending) >= cs.spec.MaxAckPending {
		return nil
	}
	for {
		if cs.next <= st.firstSeq() {
			cs.next = st.firstSeq()
		}
		if cs.next > st.lastSeq {
			return nil
		}
		m := st.find(cs.next)
		cs.next++
		if m == nil {
			continue
		}
		if cs.spec.Filter != "" && !cs.spec.Filter.Matches(m.subject) {
			cs.markAcked(m.seq)
			continue
		}
		return m
	}
}

func (st *stream) find(seq bus.Seq) *storedMsg {
	for _, m := range st.msgs {
		if m.seq == seq {
			return m
		}
	}
	return nil
}

// buildLocked turns a stored message into the StreamMsg a handler sees, with
// its acknowledgement wired to this consumer's state.
func (s streams) buildLocked(st *stream, cs *consumerState, m *storedMsg) *bus.StreamMsg {
	cs.deliveries[m.seq]++
	delivered := cs.deliveries[m.seq]
	policy := cs.spec.Ack
	if policy == "" {
		policy = bus.AckExplicit
	}
	if policy == bus.AckNone {
		cs.markAcked(m.seq)
	} else {
		wait := cs.spec.AckWait
		if wait <= 0 {
			wait = defaultAckWait
		}
		cs.pending[m.seq] = nowUTC().Add(wait)
	}
	seq := m.seq
	store := s.c.store
	ack := func(kind string, delay time.Duration, reason string) error {
		store.mu.Lock()
		defer store.mu.Unlock()
		switch kind {
		case "ack":
			if policy == bus.AckAll {
				cs.ackAllThrough(seq)
			} else {
				cs.markAcked(seq)
			}
		case "nak":
			cs.pending[seq] = nowUTC().Add(delay)
		case "term":
			cs.markAcked(seq)
		case "progress":
			wait := cs.spec.AckWait
			if wait <= 0 {
				wait = defaultAckWait
			}
			cs.pending[seq] = nowUTC().Add(wait)
		default:
			return bus.Fault{Code: bus.FaultUnhandled, Op: string(m.subject), Message: "unknown acknowledgement " + kind + " " + reason}
		}
		return nil
	}
	base := bus.NewMsg(m.subject, "", m.headers.Clone(), clone(m.data), nil)
	return bus.NewStreamMsg(*base, m.seq, m.at, delivered, ack)
}

// ----------------------------------------------------------------- consumer

type consumer struct {
	c       *conn
	stream  string
	st      *consumerState
	h       bus.StreamHandler
	ctx     context.Context
	durable bool

	mu        sync.Mutex
	err       error
	finished  bool
	draining  bool
	doneCh    chan struct{}
	drained   chan struct{}
	wake      chan struct{}
	once      sync.Once
	drainOnce sync.Once
}

var _ bus.Consumer = (*consumer)(nil)

func (k *consumer) run() {
	for {
		if k.isDone() {
			return
		}
		store := k.c.store
		store.mu.Lock()
		var sm *bus.StreamMsg
		st, ok := store.streams[k.stream]
		if ok {
			if m := (streams{c: k.c}).nextLocked(st, k.st, nowUTC()); m != nil {
				sm = (streams{c: k.c}).buildLocked(st, k.st, m)
			}
		}
		store.mu.Unlock()
		if sm != nil {
			k.call(sm)
			continue
		}
		k.mu.Lock()
		draining := k.draining
		k.mu.Unlock()
		if draining {
			k.drainOnce.Do(func() { close(k.drained) })
		}
		select {
		case <-k.wake:
		case <-k.doneCh:
			return
		case <-k.ctx.Done():
			k.finish(nil)
			return
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func (k *consumer) call(m *bus.StreamMsg) {
	defer func() {
		if r := recover(); r != nil {
			k.finish(bus.Fault{Code: bus.FaultUnhandled, Op: "stream:" + k.stream, Message: "principal " + k.c.id.String() + ": handler panicked"})
		}
	}()
	ctx := traceOf(k.c).Extract(k.ctx, m.Headers)
	k.h(ctx, m)
}

func (k *consumer) Cancel() { k.finish(nil) }

func (k *consumer) Drain(ctx context.Context) error {
	k.mu.Lock()
	if k.finished {
		k.mu.Unlock()
		return nil
	}
	k.draining = true
	k.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-k.drained:
		k.finish(nil)
		return nil
	case <-k.doneCh:
		return k.Err()
	case <-ctx.Done():
		k.finish(nil)
		return bus.Fault{Code: bus.FaultTimeout, Op: "stream:" + k.stream, Message: "principal " + k.c.id.String() + ": drain did not finish: " + ctx.Err().Error(), Err: ctx.Err()}
	}
}

func (k *consumer) Done() <-chan struct{} { return k.doneCh }

func (k *consumer) Err() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.err
}

func (k *consumer) isDone() bool {
	select {
	case <-k.doneCh:
		return true
	default:
		return false
	}
}

func (k *consumer) finish(err error) {
	k.mu.Lock()
	if k.finished {
		k.mu.Unlock()
		return
	}
	k.finished = true
	if err != nil {
		k.err = err
	}
	k.mu.Unlock()
	k.once.Do(func() {
		close(k.doneCh)
		k.drainOnce.Do(func() { close(k.drained) })
	})
}

// Cursor returns an opaque, signed position covering everything this consumer
// has acknowledged. Hand it back as ConsumerSpec{Start: StartCursor, Cursor:c}
// and delivery resumes at the next message.
func (k *consumer) Cursor() bus.Cursor {
	store := k.c.store
	store.mu.Lock()
	seq := k.st.ackFloor
	store.mu.Unlock()
	return store.mintCursor(k.stream, k.st.name, seq)
}

// Reset moves the consumer back to its spec's start position.
func (k *consumer) Reset(ctx context.Context) error {
	store := k.c.store
	store.mu.Lock()
	defer store.mu.Unlock()
	st, ok := store.streams[k.stream]
	if !ok {
		return bus.Fault{Code: bus.FaultNotFound, Op: "stream:" + k.stream, Message: "principal " + k.c.id.String() + ": no such stream"}
	}
	start, err := (streams{c: k.c}).startSeqLocked(st, k.st.spec)
	if err != nil {
		return err
	}
	k.st.next = start
	k.st.started = start
	k.st.ackFloor = start - 1
	k.st.acked = map[bus.Seq]bool{}
	k.st.pending = map[bus.Seq]time.Time{}
	k.st.deliveries = map[bus.Seq]int{}
	select {
	case k.wake <- struct{}{}:
	default:
	}
	return nil
}

func (k *consumer) Info(ctx context.Context) (bus.ConsumerInfo, error) {
	store := k.c.store
	store.mu.Lock()
	defer store.mu.Unlock()
	st, ok := store.streams[k.stream]
	if !ok {
		return bus.ConsumerInfo{}, bus.Fault{Code: bus.FaultNotFound, Op: "stream:" + k.stream, Message: "principal " + k.c.id.String() + ": no such stream"}
	}
	return bus.ConsumerInfo{
		Stream:        k.stream,
		Name:          k.st.name,
		Delivered:     k.st.next - 1,
		AckFloor:      k.st.ackFloor,
		NumPending:    k.st.numPending(st),
		NumAckPending: len(k.st.pending),
	}, nil
}
