package memory

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// service is one mounted instance: a set of queue subscriptions plus the
// counters Stats reports.
type service struct {
	conn *conn
	info bus.ServiceInfo
	subs []*subscription

	requests   atomic.Uint64
	errors     atomic.Uint64
	processing atomic.Int64
	started    time.Time

	mu       sync.Mutex
	err      error
	finished bool
	doneCh   chan struct{}
	once     sync.Once
}

var _ bus.Service = (*service)(nil)

func (s *service) Info() bus.ServiceInfo { return s.info }

func (s *service) Stats() bus.ServiceStats {
	return bus.ServiceStats{
		ID:         s.info.ID,
		Name:       s.info.Name,
		Requests:   s.requests.Load(),
		Errors:     s.errors.Load(),
		Processing: time.Duration(s.processing.Load()),
		Started:    s.started,
	}
}

func (s *service) Done() <-chan struct{} { return s.doneCh }

func (s *service) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Stop withdraws every endpoint.
func (s *service) Stop(ctx context.Context) error {
	st := s.conn.store
	st.mu.Lock()
	delete(st.services, s.info.ID)
	st.dropSubsLocked(s.subs)
	st.mu.Unlock()
	s.conn.mu.Lock()
	kept := s.conn.services[:0]
	for _, x := range s.conn.services {
		if x != s {
			kept = append(kept, x)
		}
	}
	s.conn.services = kept
	s.conn.mu.Unlock()
	s.finish(nil)
	return nil
}

func (s *service) finish(err error) {
	for _, sub := range s.subs {
		sub.finish(err)
	}
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.finished = true
	if err != nil {
		s.err = err
	}
	s.mu.Unlock()
	s.once.Do(func() { close(s.doneCh) })
}

// ---------------------------------------------------------------- Services

type servicesAPI struct{ c *conn }

func (c *conn) Services() bus.Services { return servicesAPI{c: c} }

func (a servicesAPI) off(op string) error {
	if !a.c.store.cfg.caps.Services {
		return unsupported(op, "services")
	}
	return a.c.alive()
}

// Serve mounts every endpoint as a queue subscription. The group is the
// endpoint's, else the service's, else the service name — so two instances of
// the same service are competing consumers by default, which is what makes a
// second instance a replica rather than a duplicate.
func (a servicesAPI) Serve(ctx context.Context, spec bus.ServiceSpec) (bus.Service, error) {
	if err := a.off("svc:" + spec.Name); err != nil {
		return nil, err
	}
	if spec.Name == "" {
		return nil, bus.Fault{Code: bus.FaultSchema, Op: "svc:", Message: "principal " + a.c.id.String() + ": service name is empty"}
	}
	if len(spec.Endpoints) == 0 {
		return nil, bus.Fault{Code: bus.FaultSchema, Op: "svc:" + spec.Name, Message: "principal " + a.c.id.String() + ": service declares no endpoints"}
	}
	for _, ep := range spec.Endpoints {
		if _, err := bus.ParseSubject(string(ep.Subject)); err != nil {
			return nil, bus.Fault{Code: bus.FaultSchema, Op: string(ep.Subject), Message: err.Error(), Err: err}
		}
		if ep.Handler == nil {
			return nil, bus.Fault{Code: bus.FaultSchema, Op: string(ep.Subject), Message: "principal " + a.c.id.String() + ": endpoint " + ep.Name + " has no handler"}
		}
		if a.c.store.denySubject(ep.Subject) {
			return nil, bus.Denied(string(ep.Subject), a.c.id.String(), "subject is permanently ineligible")
		}
		if !a.c.id.Grants.Can(bus.GrantServe, ep.Subject) {
			return nil, bus.Denied(string(ep.Subject), a.c.id.String(), "no serve grant covers this subject")
		}
	}

	svc := &service{
		conn:    a.c,
		started: nowUTC(),
		doneCh:  make(chan struct{}),
	}
	svc.info = bus.ServiceInfo{
		ID:       a.c.store.newID("svc"),
		Name:     spec.Name,
		Version:  spec.Version,
		Metadata: copyMeta(spec.Metadata),
	}

	for _, ep := range spec.Endpoints {
		group := ep.QueueGroup
		if group == "" {
			group = spec.QueueGroup
		}
		if group == "" {
			group = spec.Name
		}
		handler := ep.Handler
		wrapped := func(ctx context.Context, m *bus.Msg) {
			svc.requests.Add(1)
			start := time.Now()
			defer func() { svc.processing.Add(int64(time.Since(start))) }()
			handler(ctx, m)
		}
		onReply := func(h bus.Headers) {
			if h.Get(bus.HeaderFault) != "" {
				svc.errors.Add(1)
			}
		}
		sub, err := a.c.subscribe(ctx, bus.Pattern(ep.Subject), wrapped,
			bus.SubOptions{QueueGroup: group, PendingLimit: bus.DefaultPendingLimit}, onReply, false)
		if err != nil {
			svc.finish(nil)
			a.c.store.mu.Lock()
			a.c.store.dropSubsLocked(svc.subs)
			a.c.store.mu.Unlock()
			return nil, err
		}
		svc.subs = append(svc.subs, sub)
		svc.info.Endpoints = append(svc.info.Endpoints, bus.EndpointInfo{
			Name:       ep.Name,
			Subject:    ep.Subject,
			QueueGroup: group,
			Point:      ep.Point,
		})
	}

	a.c.store.mu.Lock()
	a.c.store.services[svc.info.ID] = svc
	a.c.store.mu.Unlock()
	a.c.mu.Lock()
	a.c.services = append(a.c.services, svc)
	a.c.mu.Unlock()
	return svc, nil
}

// Call invokes an endpoint. A reply carrying bd-fault comes back as a typed
// Fault, not as a payload the caller has to inspect.
func (a servicesAPI) Call(ctx context.Context, subject bus.Subject, data []byte, opts ...bus.ReqOpt) (*bus.Msg, error) {
	if err := a.off(string(subject)); err != nil {
		return nil, err
	}
	m, err := a.c.Request(ctx, subject, data, opts...)
	if err != nil {
		return nil, err
	}
	if code := m.Headers.Get(bus.HeaderFault); code != "" {
		return nil, bus.Fault{Code: code, Op: string(subject), Message: string(m.Data)}
	}
	return m, nil
}

// Discover finds live services by name; an empty name lists every visible one.
func (a servicesAPI) Discover(ctx context.Context, name string) ([]bus.ServiceInfo, error) {
	if err := a.off("svc:discover"); err != nil {
		return nil, err
	}
	st := a.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []bus.ServiceInfo
	for _, svc := range st.services {
		if name != "" && svc.info.Name != name {
			continue
		}
		out = append(out, svc.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Stats collects counters from every live instance of a service.
func (a servicesAPI) Stats(ctx context.Context, name string) ([]bus.ServiceStats, error) {
	if err := a.off("svc:stats"); err != nil {
		return nil, err
	}
	st := a.c.store
	st.mu.Lock()
	var live []*service
	for _, svc := range st.services {
		if name != "" && svc.info.Name != name {
			continue
		}
		live = append(live, svc)
	}
	st.mu.Unlock()
	out := make([]bus.ServiceStats, 0, len(live))
	for _, svc := range live {
		out = append(out, svc.Stats())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func copyMeta(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
