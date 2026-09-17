package memory

import (
	"context"
	"sync"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// subscription is a bounded, never-silent delivery queue.
//
// Delivery is one message at a time, in order, on this subscription's own
// goroutine, so a handler that blocks delays only itself. The queue is capped
// at SubOptions.PendingLimit: on overflow the message is dropped, the drop is
// counted, Err becomes FaultSlowConsumer and Done closes. The publisher is
// never blocked by any of this.
type subscription struct {
	conn    *conn
	pattern bus.Pattern
	group   string
	limit   int
	h       bus.Handler
	ctx     context.Context
	onReply func(bus.Headers)

	mu       sync.Mutex
	queue    []*bus.Msg
	draining bool
	stop     bool
	err      error
	drops    uint64

	wake      chan struct{}
	doneCh    chan struct{}
	drained   chan struct{}
	finished  bool
	once      sync.Once
	drainOnce sync.Once
}

var _ bus.Subscription = (*subscription)(nil)

func newSubscription(ctx context.Context, c *conn, p bus.Pattern, h bus.Handler, o bus.SubOptions, onReply func(bus.Headers)) *subscription {
	if ctx == nil {
		ctx = context.Background()
	}
	limit := o.PendingLimit
	if limit <= 0 {
		limit = bus.DefaultPendingLimit
	}
	sub := &subscription{
		conn:    c,
		pattern: p,
		group:   o.QueueGroup,
		limit:   limit,
		h:       h,
		ctx:     ctx,
		onReply: onReply,
		wake:    make(chan struct{}, 1),
		doneCh:  make(chan struct{}),
		drained: make(chan struct{}),
	}
	go sub.run()
	return sub
}

// Dropped reports how many messages this subscription lost to overflow. It is
// the counter behind FaultSlowConsumer.
func (s *subscription) Dropped() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drops
}

func (s *subscription) enqueue(m *bus.Msg) {
	s.mu.Lock()
	if s.finished || s.stop || s.draining {
		s.mu.Unlock()
		return
	}
	if len(s.queue) >= s.limit {
		s.drops++
		n := s.drops
		s.mu.Unlock()
		s.finish(bus.Fault{
			Code: bus.FaultSlowConsumer,
			Op:   string(s.pattern),
			Message: "principal " + s.conn.id.String() + ": pending limit " +
				formatUint(uint64(s.limit)) + " exceeded, " + formatUint(n) + " message(s) dropped",
		})
		return
	}
	s.queue = append(s.queue, m)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *subscription) run() {
	for {
		s.mu.Lock()
		var m *bus.Msg
		if len(s.queue) > 0 && !s.stop {
			m = s.queue[0]
			s.queue[0] = nil
			s.queue = s.queue[1:]
		}
		empty := len(s.queue) == 0
		draining := s.draining
		s.mu.Unlock()

		if m != nil {
			s.call(m)
			continue
		}
		if empty && draining {
			s.drainOnce.Do(func() { close(s.drained) })
		}
		select {
		case <-s.wake:
		case <-s.doneCh:
			return
		case <-s.ctx.Done():
			s.finish(nil)
			return
		}
	}
}

func (s *subscription) call(m *bus.Msg) {
	defer func() {
		// A panicking handler is the plugin author's bug, not a reason for the
		// substrate to die. It ends this subscription, loudly.
		if r := recover(); r != nil {
			s.finish(bus.Fault{
				Code:    bus.FaultUnhandled,
				Op:      string(m.Subject),
				Message: "principal " + s.conn.id.String() + ": handler panicked",
			})
		}
	}()
	ctx := traceOf(s.conn).Extract(s.ctx, m.Headers)
	s.h(ctx, m)
}

// Cancel stops delivery immediately, dropping anything queued. A clean cancel
// leaves Err nil.
func (s *subscription) Cancel() { s.finish(nil) }

// Drain stops accepting new messages and returns once the queue is delivered
// or ctx expires.
func (s *subscription) Drain(ctx context.Context) error {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return nil
	}
	s.draining = true
	empty := len(s.queue) == 0
	s.mu.Unlock()
	if empty {
		s.drainOnce.Do(func() { close(s.drained) })
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-s.drained:
		s.finish(nil)
		return nil
	case <-s.doneCh:
		return s.Err()
	case <-ctx.Done():
		s.finish(nil)
		return bus.Fault{Code: bus.FaultTimeout, Op: string(s.pattern), Message: "principal " + s.conn.id.String() + ": drain did not finish: " + ctx.Err().Error(), Err: ctx.Err()}
	}
}

func (s *subscription) Done() <-chan struct{} { return s.doneCh }

func (s *subscription) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *subscription) done() bool {
	select {
	case <-s.doneCh:
		return true
	default:
		return false
	}
}

func (s *subscription) stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stop || s.draining
}

func (s *subscription) finish(err error) {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.finished = true
	s.stop = true
	if err != nil {
		s.err = err
	}
	s.queue = nil
	s.mu.Unlock()
	s.once.Do(func() {
		close(s.doneCh)
		s.drainOnce.Do(func() { close(s.drained) })
		go s.conn.store.dropSub(s)
	})
}
