package conformance

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// The vacuity check proves the properties can FAIL. This file proves the other
// half: that they can PASS. A property nobody can satisfy is as useless as one
// nobody can fail, and it is not discovered until integration.
//
// The substrate here is core-only on purpose — publish, subscribe, request,
// queue groups, grants, the permanent deny set, caller stamping, the payload
// ceiling, bounded overflow and revoke. It declares no capability at all, so
// every capability-gated property (17 onwards) skips. It is NOT a second
// memory substrate and nothing outside this file may use it: it exists so that
// the sixteen core properties, where all the timing and assertion logic lives,
// are known to be satisfiable before anyone wires a real substrate to them.

func TestCorePropertiesAreSatisfiable(t *testing.T) {
	c := newCore(4096, []bus.Pattern{"deny.everything.>"})
	Run(t, Harness{
		New:     func(_ *testing.T, id bus.Identity) bus.Bus { return c.bind(id) },
		Revoke:  func(_ *testing.T, pluginID string) { c.revoke(pluginID) },
		Caps:    bus.Capabilities{MaxPayload: 4096},
		Deny:    []bus.Pattern{"deny.everything.>"},
		Budget:  2 * time.Second,
		Default: false,
	})
}

// ------------------------------------------------------------------- substrate

type core struct {
	mu         sync.Mutex
	subs       []*coreSub
	revoked    map[string]bool
	deny       []bus.Pattern
	maxPayload int
	inbox      atomic.Int64
	rr         map[string]int
}

func newCore(maxPayload int, deny []bus.Pattern) *core {
	return &core{revoked: map[string]bool{}, deny: deny, maxPayload: maxPayload, rr: map[string]int{}}
}

func (c *core) bind(id bus.Identity) bus.Bus { return &coreBus{c: c, id: id} }

func (c *core) revoke(pluginID string) {
	c.mu.Lock()
	var hit []*coreSub
	for _, s := range c.subs {
		if s.owner.PluginID == pluginID {
			hit = append(hit, s)
			c.revoked[key(s.owner)] = true
		}
	}
	c.mu.Unlock()
	for _, s := range hit {
		s.finish(bus.Fault{Code: bus.FaultWithdrawn, Op: string(s.pattern), Message: "principal " + s.owner.String() + ": credential revoked"})
	}
}

func key(id bus.Identity) string { return id.PluginID + "@" + id.Generation }

func (c *core) isRevoked(id bus.Identity) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.revoked[key(id)]
}

// recipients picks the subscriptions one publish reaches: every individual
// match, plus exactly one member of each queue group.
func (c *core) recipients(subject bus.Subject) []*coreSub {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*coreSub
	groups := map[string][]*coreSub{}
	for _, s := range c.subs {
		if s.dead() || !s.pattern.Matches(subject) {
			continue
		}
		if s.group == "" {
			out = append(out, s)
			continue
		}
		groups[s.group] = append(groups[s.group], s)
	}
	for name, members := range groups {
		i := c.rr[name] % len(members)
		c.rr[name] = i + 1
		out = append(out, members[i])
	}
	return out
}

type coreBus struct {
	c      *core
	id     bus.Identity
	closed atomic.Bool
}

func (b *coreBus) check(kind bus.GrantKind, s bus.Subject) error {
	if b.closed.Load() || b.c.isRevoked(b.id) {
		return bus.Fault{Code: bus.FaultWithdrawn, Op: string(s), Message: "principal " + b.id.String() + ": credential is no longer valid"}
	}
	if bus.MatchedByAny(b.c.deny, s) {
		return bus.Denied(string(s), b.id.String(), "permanently ineligible family")
	}
	if !b.id.Grants.Can(kind, s) {
		return bus.Denied(string(s), b.id.String(), "no "+string(kind)+" grant covers this subject")
	}
	return nil
}

func (b *coreBus) Publish(_ context.Context, subject bus.Subject, data []byte, opts ...bus.PublishOpt) error {
	if err := b.check(bus.GrantPublish, subject); err != nil {
		return err
	}
	if len(data) > b.c.maxPayload {
		return bus.Denied(string(subject), b.id.String(), "payload of "+strconv.Itoa(len(data))+" bytes exceeds the "+strconv.Itoa(b.c.maxPayload)+"-byte ceiling")
	}
	o := bus.ResolvePublish(opts)
	b.fanout(subject, "", o.Headers, data, nil)
	return nil
}

// fanout enqueues without ever blocking the publisher. An overflowing
// subscription is ended with FaultSlowConsumer rather than dropped in silence.
func (b *coreBus) fanout(subject, reply bus.Subject, h bus.Headers, data []byte, respond func(bus.Headers, []byte) error) {
	stamped := bus.StripReserved(h)
	if stamped == nil {
		stamped = bus.Headers{}
	}
	stamped[bus.HeaderCaller] = b.id.PluginID
	stamped[bus.HeaderGeneration] = b.id.Generation
	if !b.id.Lease.IsZero() {
		stamped[bus.HeaderSubject] = b.id.Lease.Token()
	}
	for _, s := range b.c.recipients(subject) {
		s.enqueue(bus.NewMsg(subject, reply, stamped, data, respond))
	}
}

func (b *coreBus) Subscribe(_ context.Context, pattern bus.Pattern, h bus.Handler, opts ...bus.SubOpt) (bus.Subscription, error) {
	if b.closed.Load() || b.c.isRevoked(b.id) {
		return nil, bus.Fault{Code: bus.FaultWithdrawn, Op: string(pattern), Message: "principal " + b.id.String() + ": credential is no longer valid"}
	}
	for _, d := range b.c.deny {
		if d.Covers(pattern) {
			return nil, bus.Denied(string(pattern), b.id.String(), "permanently ineligible family")
		}
	}
	if !b.id.Grants.CanPattern(bus.GrantSubscribe, pattern) {
		return nil, bus.Denied(string(pattern), b.id.String(), "no subscribe grant covers this pattern")
	}
	o := bus.ResolveSub(opts)
	s := &coreSub{
		pattern: pattern,
		group:   o.QueueGroup,
		owner:   b.id,
		queue:   make(chan *bus.Msg, o.PendingLimit),
		done:    make(chan struct{}),
		h:       h,
	}
	b.c.mu.Lock()
	b.c.subs = append(b.c.subs, s)
	b.c.mu.Unlock()
	go s.pump()
	return s, nil
}

func (b *coreBus) Request(ctx context.Context, subject bus.Subject, data []byte, opts ...bus.ReqOpt) (*bus.Msg, error) {
	if err := b.check(bus.GrantRequest, subject); err != nil {
		return nil, err
	}
	o := bus.ResolveReq(opts)
	if len(b.c.recipients(subject)) == 0 {
		return nil, bus.Fault{Code: bus.FaultNoResponders, Op: string(subject), Message: "principal " + b.id.String() + ": nothing is listening"}
	}
	reply := bus.Subject("_INBOX." + strconv.FormatInt(b.c.inbox.Add(1), 10))
	answer := make(chan *bus.Msg, 1)
	respond := func(h bus.Headers, d []byte) error {
		select {
		case answer <- &bus.Msg{Subject: reply, Headers: h.Clone(), Data: d}:
		default:
		}
		return nil
	}
	b.fanout(subject, reply, o.Headers, data, respond)

	wait, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	select {
	case m := <-answer:
		return m, nil
	case <-wait.Done():
		return nil, bus.Fault{Code: bus.FaultTimeout, Op: string(subject), Message: "principal " + b.id.String() + ": no reply", Err: wait.Err()}
	}
}

func (b *coreBus) Streams() bus.Streams    { return brokenStreams{} }
func (b *coreBus) KV() bus.KV              { return brokenKV{} }
func (b *coreBus) Objects() bus.Objects    { return brokenObjects{} }
func (b *coreBus) Services() bus.Services  { return brokenServices{} }
func (b *coreBus) Schedule() bus.Scheduler { return brokenScheduler{} }
func (b *coreBus) Trace() bus.Trace        { return brokenTrace{} }
func (b *coreBus) Close() error            { b.closed.Store(true); return nil }

func (b *coreBus) Capabilities() bus.Capabilities {
	return bus.Capabilities{MaxPayload: b.c.maxPayload}
}

type coreSub struct {
	pattern bus.Pattern
	group   string
	owner   bus.Identity
	queue   chan *bus.Msg
	done    chan struct{}
	h       bus.Handler

	mu       sync.Mutex
	err      error
	finished bool
}

func (s *coreSub) dead() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finished
}

func (s *coreSub) finish(err error) {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.finished, s.err = true, err
	s.mu.Unlock()
	close(s.done)
}

func (s *coreSub) enqueue(m *bus.Msg) {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	select {
	case s.queue <- m:
	default:
		s.finish(bus.Fault{Code: bus.FaultSlowConsumer, Op: string(s.pattern), Message: "principal " + s.owner.String() + ": pending limit exceeded"})
	}
}

// pump delivers one message at a time, in order, so a blocked handler delays
// only its own subscription.
func (s *coreSub) pump() {
	ctx := context.Background()
	for {
		select {
		case m := <-s.queue:
			s.h(ctx, m)
		case <-s.done:
			return
		}
	}
}

func (s *coreSub) Cancel()                     { s.finish(nil) }
func (s *coreSub) Drain(context.Context) error { s.finish(nil); return nil }
func (s *coreSub) Done() <-chan struct{}       { return s.done }

func (s *coreSub) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
