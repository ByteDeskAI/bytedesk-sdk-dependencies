package memory

import (
	"context"
	"sync"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// conn is one principal's connection. It is the bus.Bus a plugin holds.
type conn struct {
	store *Store
	id    bus.Identity
	gen   uint64

	mu        sync.Mutex
	closed    bool
	subs      []*subscription
	services  []*service
	consumers []*consumer
	watchers  []stopper
	fetchers  map[string]*consumerState
	inboxN    uint64
}

// stopper is anything a connection must shut down with it: a KV or object
// watcher.
type stopper interface{ stopWith(error) }

var _ bus.Bus = (*conn)(nil)

// Identity reports the principal this bus is bound to.
//
// bus.Bus does not declare Identity(), although package bus's own
// documentation tells callers to use "Identity().Grants().Can" as a preflight.
// The method lives on the concrete connection so the preflight the contract
// describes is reachable; see the report for the contract gap.
func (c *conn) Identity() bus.Identity { return c.id.Clone() }

func (c *conn) Capabilities() bus.Capabilities { return c.store.cfg.caps }

// alive reports that this connection still belongs to the current generation.
func (c *conn) alive() error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return withdrawn("connection closed")
	}
	c.store.mu.Lock()
	dead := c.store.closed || c.gen != c.store.gen
	c.store.mu.Unlock()
	if dead {
		return withdrawn("connection belongs to a previous generation")
	}
	return nil
}

func (c *conn) markDead() {
	c.mu.Lock()
	c.closed = true
	c.subs = nil
	c.services = nil
	cons := c.consumers
	watchers := c.watchers
	c.consumers = nil
	c.watchers = nil
	c.fetchers = nil
	c.mu.Unlock()
	err := withdrawn("connection closed")
	for _, k := range cons {
		k.finish(err)
	}
	for _, w := range watchers {
		w.stopWith(err)
	}
}

// Close releases the connection: every subscription and service it owns is
// withdrawn.
func (c *conn) Close() error {
	c.store.mu.Lock()
	delete(c.store.conns, c)
	c.mu.Lock()
	subs, svcs := c.subs, c.services
	c.subs, c.services = nil, nil
	c.closed = true
	c.mu.Unlock()
	c.store.dropSubsLocked(subs)
	for _, svc := range svcs {
		delete(c.store.services, svc.info.ID)
	}
	c.store.mu.Unlock()
	finishAll(nil, subs, svcs, withdrawn("connection closed"))
	c.markDead()
	return nil
}

// checkPublish runs the full gate for one concrete subject: liveness, subject
// grammar, permanent deny, grant, payload ceiling. Order matters only in that
// every refusal names the subject and the principal.
func (c *conn) checkPublish(kind bus.GrantKind, subject bus.Subject, data []byte) error {
	if err := c.alive(); err != nil {
		return err
	}
	if _, err := bus.ParseSubject(string(subject)); err != nil {
		return bus.Fault{Code: bus.FaultSchema, Op: string(subject), Message: err.Error(), Err: err}
	}
	if c.store.denySubject(subject) {
		return bus.Denied(string(subject), c.id.String(), "subject is permanently ineligible")
	}
	if !c.id.Grants.Can(kind, subject) {
		return bus.Denied(string(subject), c.id.String(), "no "+string(kind)+" grant covers this subject")
	}
	if max := c.store.cfg.caps.MaxPayload; max > 0 && len(data) > max {
		return bus.Fault{
			Code:    bus.FaultBudget,
			Op:      string(subject),
			Message: "principal " + c.id.String() + ": payload " + formatUint(uint64(len(data))) + " bytes exceeds MaxPayload " + formatUint(uint64(max)),
		}
	}
	return nil
}

// stamp strips anything the caller wrote into the bd-* namespace and writes
// the real identity. A sender that sets bd-caller to someone else is seen by
// the handler as itself.
func (c *conn) stamp(ctx context.Context, h bus.Headers) bus.Headers {
	return stampWith(ctx, c.id, h)
}

func stampWith(ctx context.Context, id bus.Identity, h bus.Headers) bus.Headers {
	out := bus.StripReserved(h)
	if out == nil {
		out = bus.Headers{}
	}
	out[bus.HeaderCaller] = id.PluginID
	out[bus.HeaderGeneration] = id.Generation
	if !id.Lease.IsZero() {
		out[bus.HeaderSubject] = id.Lease.Token()
	}
	return trace{}.Inject(ctx, out)
}

// Publish emits to a concrete subject. Delivery is asynchronous and bounded:
// the publisher never blocks on a slow handler.
func (c *conn) Publish(ctx context.Context, subject bus.Subject, data []byte, opts ...bus.PublishOpt) error {
	o := bus.ResolvePublish(opts)
	if err := c.checkPublish(bus.GrantPublish, subject, data); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return bus.Fault{Code: bus.FaultTimeout, Op: string(subject), Message: err.Error(), Err: err}
	}
	m := bus.NewMsg(subject, "", c.stamp(ctx, o.Headers), clone(data), nil)
	c.store.deliver(subject, m, nil)
	return nil
}

// Subscribe delivers every message matching pattern on the subscription's own
// goroutine, one at a time and in order.
func (c *conn) Subscribe(ctx context.Context, pattern bus.Pattern, h bus.Handler, opts ...bus.SubOpt) (bus.Subscription, error) {
	o := bus.ResolveSub(opts)
	return c.subscribe(ctx, pattern, h, o, nil, true)
}

// subscribe mounts a delivery queue. checkGrant is false only for a service
// endpoint, whose right to receive comes from GrantServe and has already been
// checked against the endpoint's concrete subject.
func (c *conn) subscribe(ctx context.Context, pattern bus.Pattern, h bus.Handler, o bus.SubOptions, onReply func(bus.Headers), checkGrant bool) (*subscription, error) {
	if err := c.alive(); err != nil {
		return nil, err
	}
	if h == nil {
		return nil, bus.Fault{Code: bus.FaultSchema, Op: string(pattern), Message: "principal " + c.id.String() + ": handler is nil"}
	}
	if _, err := bus.ParsePattern(string(pattern)); err != nil {
		return nil, bus.Fault{Code: bus.FaultSchema, Op: string(pattern), Message: err.Error(), Err: err}
	}
	if c.store.denyPattern(pattern) {
		return nil, bus.Denied(string(pattern), c.id.String(), "pattern reaches a permanently ineligible subject family")
	}
	if checkGrant && !c.id.Grants.CanPattern(bus.GrantSubscribe, pattern) {
		return nil, bus.Denied(string(pattern), c.id.String(), "no subscribe grant covers this pattern")
	}
	sub := newSubscription(ctx, c, pattern, h, o, onReply)
	c.store.mu.Lock()
	if c.store.closed || c.gen != c.store.gen {
		c.store.mu.Unlock()
		sub.finish(withdrawn("connection belongs to a previous generation"))
		return nil, withdrawn("connection belongs to a previous generation")
	}
	c.store.subs = append(c.store.subs, sub)
	c.store.mu.Unlock()
	c.mu.Lock()
	c.subs = append(c.subs, sub)
	c.mu.Unlock()
	return sub, nil
}

// Request sends and waits for one reply. With nothing listening it fails fast
// with FaultNoResponders rather than burning the timeout.
func (c *conn) Request(ctx context.Context, subject bus.Subject, data []byte, opts ...bus.ReqOpt) (*bus.Msg, error) {
	o := bus.ResolveReq(opts)
	if err := c.checkPublish(bus.GrantRequest, subject, data); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, bus.Fault{Code: bus.FaultTimeout, Op: string(subject), Message: err.Error(), Err: err}
	}

	c.mu.Lock()
	c.inboxN++
	n := c.inboxN
	c.mu.Unlock()
	reply := bus.Subject("_INBOX." + c.id.PluginID + "." + formatUint(c.gen) + "." + formatUint(n))

	replies := make(chan *bus.Msg, 1)
	c.store.mu.Lock()
	c.store.inboxes[reply] = replies
	c.store.mu.Unlock()
	defer func() {
		c.store.mu.Lock()
		delete(c.store.inboxes, reply)
		c.store.mu.Unlock()
	}()

	m := bus.NewMsg(subject, reply, c.stamp(ctx, o.Headers), clone(data), nil)
	if n := c.store.deliver(subject, m, &reply); n == 0 {
		return nil, bus.Fault{
			Code:    bus.FaultNoResponders,
			Op:      string(subject),
			Message: "principal " + c.id.String() + ": nothing is listening",
		}
	}

	timer := time.NewTimer(o.Timeout)
	defer timer.Stop()
	select {
	case r := <-replies:
		return r, nil
	case <-ctx.Done():
		return nil, bus.Fault{Code: bus.FaultTimeout, Op: string(subject), Message: "principal " + c.id.String() + ": " + ctx.Err().Error(), Err: ctx.Err()}
	case <-timer.C:
		return nil, bus.Fault{Code: bus.FaultTimeout, Op: string(subject), Message: "principal " + c.id.String() + ": no reply within " + o.Timeout.String()}
	}
}

// deliver fans a message out to every matching live subscription and returns
// how many received it. A queue group counts as one receiver.
func (s *Store) deliver(subject bus.Subject, m *bus.Msg, reply *bus.Subject) int {
	s.mu.Lock()
	targets := s.matchLocked(subject)
	s.mu.Unlock()
	for _, sub := range targets {
		sub.enqueue(copyFor(m, sub, reply))
	}
	return len(targets)
}

// copyFor gives each subscriber its own Msg, with a respond closure bound to
// the requester's inbox and to the responder's own identity.
func copyFor(m *bus.Msg, sub *subscription, reply *bus.Subject) *bus.Msg {
	var respond func(bus.Headers, []byte) error
	if reply != nil {
		to := *reply
		respond = func(h bus.Headers, data []byte) error {
			if sub.onReply != nil {
				sub.onReply(h)
			}
			return sub.conn.respondTo(to, h, data)
		}
	}
	return bus.NewMsg(m.Subject, m.Reply, m.Headers.Clone(), clone(m.Data), respond)
}

func (c *conn) respondTo(reply bus.Subject, h bus.Headers, data []byte) error {
	out := bus.StripReserved(h)
	if out == nil {
		out = bus.Headers{}
	}
	// The fault code is the one bd-* header a responder is allowed to set: it
	// is how RespondFault travels, and Services.Call turns it back into a
	// typed Fault.
	if code := h.Get(bus.HeaderFault); code != "" {
		out[bus.HeaderFault] = code
	}
	if corr := h.Get(bus.HeaderCorrelation); corr != "" {
		out[bus.HeaderCorrelation] = corr
	}
	out[bus.HeaderCaller] = c.id.PluginID
	out[bus.HeaderGeneration] = c.id.Generation
	if !c.id.Lease.IsZero() {
		out[bus.HeaderSubject] = c.id.Lease.Token()
	}

	c.store.mu.Lock()
	ch, ok := c.store.inboxes[reply]
	c.store.mu.Unlock()
	if !ok {
		return bus.Fault{Code: bus.FaultUnhandled, Op: string(reply), Message: "principal " + c.id.String() + ": requester is no longer waiting"}
	}
	msg := bus.NewMsg(reply, "", out, clone(data), nil)
	select {
	case ch <- msg:
		return nil
	default:
		return bus.Fault{Code: bus.FaultUnhandled, Op: string(reply), Message: "principal " + c.id.String() + ": request already answered"}
	}
}

// matchLocked selects the delivery set for a subject: every ungrouped
// subscription, plus exactly one member of each queue group, round-robin.
func (s *Store) matchLocked(subject bus.Subject) []*subscription {
	var out []*subscription
	groups := map[string][]*subscription{}
	var order []string
	live := s.subs[:0]
	for _, sub := range s.subs {
		if sub.done() {
			continue
		}
		live = append(live, sub)
		if !sub.pattern.Matches(subject) || sub.stopped() {
			continue
		}
		if sub.group == "" {
			out = append(out, sub)
			continue
		}
		if _, seen := groups[sub.group]; !seen {
			order = append(order, sub.group)
		}
		groups[sub.group] = append(groups[sub.group], sub)
	}
	for i := len(live); i < len(s.subs); i++ {
		s.subs[i] = nil
	}
	s.subs = live
	for _, g := range order {
		members := groups[g]
		i := s.rr[g] % uint64(len(members))
		s.rr[g]++
		out = append(out, members[i])
	}
	return out
}

func (s *Store) dropSubsLocked(subs []*subscription) {
	if len(subs) == 0 {
		return
	}
	drop := make(map[*subscription]struct{}, len(subs))
	for _, sub := range subs {
		drop[sub] = struct{}{}
	}
	kept := s.subs[:0]
	for _, sub := range s.subs {
		if _, ok := drop[sub]; ok {
			continue
		}
		kept = append(kept, sub)
	}
	for i := len(kept); i < len(s.subs); i++ {
		s.subs[i] = nil
	}
	s.subs = kept
}

func (s *Store) dropSub(sub *subscription) {
	s.mu.Lock()
	s.dropSubsLocked([]*subscription{sub})
	s.mu.Unlock()
	sub.conn.mu.Lock()
	kept := sub.conn.subs[:0]
	for _, x := range sub.conn.subs {
		if x != sub {
			kept = append(kept, x)
		}
	}
	sub.conn.subs = kept
	sub.conn.mu.Unlock()
}

func clone(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// unsupported is the refusal an accessor returns when its capability is off.
func unsupported(op, capability string) bus.Fault { return bus.Unsupported(op, capability) }
