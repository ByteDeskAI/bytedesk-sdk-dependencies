// Package memory is a complete in-memory implementation of bus.Bus.
//
// It is two things at once. It is the plugin author's test double: a whole
// plugin suite runs against it with no broker, no container and no network. It
// is also the substrate the conformance suite proves first, so it is written
// to be correct rather than approximate — grants are enforced and attributed,
// delivery is bounded and never silently dropped, cursors are signed, and
// durable state survives Restart exactly as a broker's would.
//
// Standard library only, by contract: nothing here may import a broker client,
// and an import-boundary test fails the build if anything does.
package memory

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// DefaultMaxPayload is the per-message ceiling a Store applies when
// WithMaxPayload is not given. It is the plugin ceiling from R5.
const DefaultMaxPayload = 64 * 1024

type config struct {
	maxPayload int
	caps       bus.Capabilities
	deny       []bus.Pattern
}

// Option configures a Store.
type Option func(*config)

// WithMaxPayload sets the per-message byte ceiling. A publish larger than n is
// refused with bus.FaultBudget; exactly n is allowed.
func WithMaxPayload(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxPayload = n
			c.caps.MaxPayload = n
		}
	}
}

// WithCapabilities replaces the substrate's reported capabilities, so a test
// can turn one off and check that the accessor refuses with
// bus.FaultUnsupported instead of half-working. A zero MaxPayload keeps the
// configured one.
func WithCapabilities(caps bus.Capabilities) Option {
	return func(c *config) {
		mp := caps.MaxPayload
		c.caps = caps
		if mp <= 0 {
			c.caps.MaxPayload = c.maxPayload
		}
	}
}

// WithDeny marks subject families permanently ineligible. Deny always wins: a
// subject a deny pattern reaches is refused even when a grant also covers it.
func WithDeny(p ...bus.Pattern) Option {
	return func(c *config) { c.deny = append(c.deny, p...) }
}

func defaultConfig() config {
	return config{
		maxPayload: DefaultMaxPayload,
		caps: bus.Capabilities{
			Durable:    true,
			KV:         true,
			Objects:    true,
			Services:   true,
			Schedule:   true,
			Counters:   true,
			Batch:      true,
			Trace:      false,
			MaxPayload: DefaultMaxPayload,
		},
	}
}

// Store is the substrate's state. Streams, KV buckets, object buckets and
// schedules live here and survive Restart; connections, core subscriptions,
// ephemeral consumers and mounted services do not.
type Store struct {
	cfg config
	// cursorKey signs replay cursors. It is created with the Store and kept
	// across Restart, so a cursor minted before a restart still verifies after
	// one — which is the whole point of handing a cursor to a browser.
	cursorKey []byte

	mu     sync.Mutex
	closed bool
	// gen rises on every Restart. A conn from an older generation is dead.
	gen    uint64
	nextID uint64

	conns map[*conn]struct{}
	subs  []*subscription
	rr    map[string]uint64

	inboxes map[bus.Subject]chan *bus.Msg

	streams   map[string]*stream
	buckets   map[string]*kvBucket
	objects   map[string]*objBucket
	schedules map[string]map[string]*scheduleEntry

	services map[string]*service

	stop chan struct{}
	wg   sync.WaitGroup
}

// NewStore builds an empty substrate.
func NewStore(opts ...Option) *Store {
	cfg := defaultConfig()
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// crypto/rand does not fail on any supported platform; a Store that
		// could not sign cursors would silently accept forged ones, so refuse
		// to exist instead.
		panic("memory: cannot seed cursor key: " + err.Error())
	}
	s := &Store{
		cfg:       cfg,
		cursorKey: key,
		conns:     map[*conn]struct{}{},
		rr:        map[string]uint64{},
		inboxes:   map[bus.Subject]chan *bus.Msg{},
		streams:   map[string]*stream{},
		buckets:   map[string]*kvBucket{},
		objects:   map[string]*objBucket{},
		schedules: map[string]map[string]*scheduleEntry{},
		services:  map[string]*service{},
		stop:      make(chan struct{}),
	}
	s.wg.Add(1)
	go s.runSchedules()
	return s
}

// Connect returns a Bus bound to id. Repeated calls for the same principal
// return independent connections, because a plugin and its own test harness
// are two principals' worth of state even when the identity matches.
func (s *Store) Connect(id bus.Identity) bus.Bus {
	c := &conn{store: s, id: id.Clone()}
	s.mu.Lock()
	c.gen = s.gen
	if !s.closed {
		s.conns[c] = struct{}{}
	} else {
		c.closed = true
	}
	s.mu.Unlock()
	return c
}

// Restart drops every connection and all ephemeral state, keeping durable
// state: streams and their messages, KV buckets and their history, object
// buckets, schedules, and the cursor-signing key. Buses returned before it are
// dead and every call on one returns a Fault.
func (s *Store) Restart() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.gen++
	conns, subs, svcs := s.takeEphemeralLocked()
	s.mu.Unlock()
	finishAll(conns, subs, svcs, withdrawn("substrate restarted"))
}

// Revoke ends every connection for pluginID. Their subscriptions' Err becomes
// a bus.Fault with Code bus.FaultWithdrawn and Done closes.
func (s *Store) Revoke(pluginID string) {
	s.mu.Lock()
	var conns []*conn
	for c := range s.conns {
		if c.id.PluginID == pluginID {
			conns = append(conns, c)
			delete(s.conns, c)
		}
	}
	var subs []*subscription
	kept := s.subs[:0]
	for _, sub := range s.subs {
		if sub.conn.id.PluginID == pluginID {
			subs = append(subs, sub)
			continue
		}
		kept = append(kept, sub)
	}
	s.subs = kept
	var svcs []*service
	for id, svc := range s.services {
		if svc.conn.id.PluginID == pluginID {
			svcs = append(svcs, svc)
			delete(s.services, id)
		}
	}
	s.mu.Unlock()
	finishAll(conns, subs, svcs, withdrawn("principal "+pluginID+" revoked"))
}

// Close shuts the store down. Every connection dies, the scheduler stops, and
// durable state is released.
func (s *Store) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	conns, subs, svcs := s.takeEphemeralLocked()
	close(s.stop)
	s.mu.Unlock()
	finishAll(conns, subs, svcs, withdrawn("substrate closed"))
	s.wg.Wait()
}

// takeEphemeralLocked removes every connection, subscription and service from
// the store and returns them for the caller to finish outside the lock.
func (s *Store) takeEphemeralLocked() ([]*conn, []*subscription, []*service) {
	conns := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.conns = map[*conn]struct{}{}
	subs := s.subs
	s.subs = nil
	svcs := make([]*service, 0, len(s.services))
	for _, svc := range s.services {
		svcs = append(svcs, svc)
	}
	s.services = map[string]*service{}
	s.inboxes = map[bus.Subject]chan *bus.Msg{}
	return conns, subs, svcs
}

func finishAll(conns []*conn, subs []*subscription, svcs []*service, err error) {
	for _, sub := range subs {
		sub.finish(err)
	}
	for _, svc := range svcs {
		svc.finish(err)
	}
	for _, c := range conns {
		c.markDead()
	}
}

func withdrawn(why string) bus.Fault {
	return bus.Fault{Code: bus.FaultWithdrawn, Op: "connection", Message: why}
}

func (s *Store) newID(prefix string) string {
	s.mu.Lock()
	s.nextID++
	n := s.nextID
	s.mu.Unlock()
	return prefix + "-" + formatUint(n)
}

func (s *Store) newIDLocked(prefix string) string {
	s.nextID++
	return prefix + "-" + formatUint(s.nextID)
}

// denySubject reports whether a permanent deny reaches this concrete subject.
func (s *Store) denySubject(sub bus.Subject) bool {
	return bus.MatchedByAny(s.cfg.deny, sub)
}

// denyPattern reports whether a permanent deny OVERLAPS this pattern. A
// subscription to ">" is refused by a deny of "$sys.>" even though ">" is not
// contained in it, because otherwise a wide subscription would be a way around
// the deny list.
func (s *Store) denyPattern(p bus.Pattern) bool {
	for _, d := range s.cfg.deny {
		if d.Covers(p) || p.Covers(d) {
			return true
		}
	}
	return false
}

func formatUint(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func nowUTC() time.Time { return time.Now().UTC() }
