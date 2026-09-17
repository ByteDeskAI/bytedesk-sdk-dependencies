package memory

import (
	"context"
	"sort"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// scheduleEntry is one durable schedule. It carries a snapshot of the owning
// principal's identity, because a schedule outlives the connection that
// created it and its publishes still have to be attributed to — and gated
// on — the principal that asked for them.
type scheduleEntry struct {
	id      bus.Identity
	name    string
	subject bus.Subject
	data    []byte
	headers bus.Headers
	kind    string
	at      time.Time
	every   time.Duration
	cron    string
	expr    *cronExpr
	next    time.Time
}

func (e *scheduleEntry) schedule() bus.Schedule {
	return bus.Schedule{
		Name:    e.name,
		Subject: e.subject,
		Data:    clone(e.data),
		Headers: e.headers.Clone(),
		At:      e.at,
		Every:   e.every,
		Cron:    e.cron,
		Next:    e.next,
	}
}

// runSchedules is the store's one timer. It survives Restart — a durable
// schedule that stopped firing on restart would not be durable — and stops
// only with Close.
func (s *Store) runSchedules() {
	defer s.wg.Done()
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-tick.C:
			s.fireDue(nowUTC())
		}
	}
}

func (s *Store) fireDue(now time.Time) {
	type shot struct {
		id      bus.Identity
		subject bus.Subject
		data    []byte
		headers bus.Headers
	}
	var shots []shot
	s.mu.Lock()
	for pluginID, byName := range s.schedules {
		for name, e := range byName {
			if e.next.IsZero() || now.Before(e.next) {
				continue
			}
			shots = append(shots, shot{id: e.id, subject: e.subject, data: clone(e.data), headers: e.headers.Clone()})
			switch e.kind {
			case "at":
				delete(byName, name)
			case "every":
				e.next = e.next.Add(e.every)
				if !e.next.After(now) {
					e.next = now.Add(e.every)
				}
			case "cron":
				if next, ok := e.expr.next(now); ok {
					e.next = next
				} else {
					delete(byName, name)
				}
			}
		}
		if len(byName) == 0 {
			delete(s.schedules, pluginID)
		}
	}
	s.mu.Unlock()

	for _, sh := range shots {
		if s.denySubject(sh.subject) || !sh.id.Grants.Can(bus.GrantPublish, sh.subject) {
			// A schedule whose grant was withdrawn stops publishing. It is not
			// an error to report anywhere — there is no caller left — but it is
			// never silently allowed either.
			continue
		}
		h := stampWith(context.Background(), sh.id, sh.headers)
		s.deliver(sh.subject, bus.NewMsg(sh.subject, "", h, sh.data, nil), nil)
	}
}

// --------------------------------------------------------------- Scheduler

type scheduler struct{ c *conn }

func (c *conn) Schedule() bus.Scheduler { return scheduler{c: c} }

func (sc scheduler) off(op string) error {
	if !sc.c.store.cfg.caps.Schedule {
		return unsupported(op, "schedule")
	}
	return sc.c.alive()
}

func (sc scheduler) check(name string, subject bus.Subject, data []byte) error {
	if name == "" {
		return bus.Fault{Code: bus.FaultSchema, Op: string(subject), Message: "principal " + sc.c.id.String() + ": schedule name is empty"}
	}
	return sc.c.checkPublish(bus.GrantPublish, subject, data)
}

func (sc scheduler) register(e *scheduleEntry) error {
	st := sc.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		return withdrawn("substrate closed")
	}
	byName, ok := st.schedules[sc.c.id.PluginID]
	if !ok {
		byName = map[string]*scheduleEntry{}
		st.schedules[sc.c.id.PluginID] = byName
	}
	// Re-declaring a name replaces the schedule rather than adding a second.
	byName[e.name] = e
	return nil
}

func (sc scheduler) At(ctx context.Context, name string, t time.Time, subject bus.Subject, data []byte, opts ...bus.PublishOpt) error {
	if err := sc.off(string(subject)); err != nil {
		return err
	}
	if err := sc.check(name, subject, data); err != nil {
		return err
	}
	o := bus.ResolvePublish(opts)
	return sc.register(&scheduleEntry{
		id: sc.c.id.Clone(), name: name, subject: subject, data: clone(data),
		headers: bus.StripReserved(o.Headers), kind: "at", at: t.UTC(), next: t.UTC(),
	})
}

func (sc scheduler) Every(ctx context.Context, name string, d time.Duration, subject bus.Subject, data []byte, opts ...bus.PublishOpt) error {
	if err := sc.off(string(subject)); err != nil {
		return err
	}
	if err := sc.check(name, subject, data); err != nil {
		return err
	}
	if d <= 0 {
		return bus.Fault{Code: bus.FaultSchema, Op: string(subject), Message: "principal " + sc.c.id.String() + ": interval must be positive"}
	}
	o := bus.ResolvePublish(opts)
	return sc.register(&scheduleEntry{
		id: sc.c.id.Clone(), name: name, subject: subject, data: clone(data),
		headers: bus.StripReserved(o.Headers), kind: "every", every: d, next: nowUTC().Add(d),
	})
}

func (sc scheduler) Cron(ctx context.Context, name string, expr string, subject bus.Subject, data []byte, opts ...bus.PublishOpt) error {
	if err := sc.off(string(subject)); err != nil {
		return err
	}
	if err := sc.check(name, subject, data); err != nil {
		return err
	}
	parsed, err := parseCron(expr)
	if err != nil {
		return bus.Fault{Code: bus.FaultSchema, Op: string(subject), Message: "principal " + sc.c.id.String() + ": " + err.Error(), Err: err}
	}
	next, ok := parsed.next(nowUTC())
	if !ok {
		return bus.Fault{Code: bus.FaultSchema, Op: string(subject), Message: "principal " + sc.c.id.String() + ": cron expression " + expr + " never fires"}
	}
	o := bus.ResolvePublish(opts)
	return sc.register(&scheduleEntry{
		id: sc.c.id.Clone(), name: name, subject: subject, data: clone(data),
		headers: bus.StripReserved(o.Headers), kind: "cron", cron: expr, expr: parsed, next: next,
	})
}

// Cancel removes a schedule by name. Cancelling an unknown name is not an
// error: Stop calls it, and Stop must be safe to call twice.
func (sc scheduler) Cancel(ctx context.Context, name string) error {
	if err := sc.off("schedule:" + name); err != nil {
		return err
	}
	st := sc.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	if byName, ok := st.schedules[sc.c.id.PluginID]; ok {
		delete(byName, name)
		if len(byName) == 0 {
			delete(st.schedules, sc.c.id.PluginID)
		}
	}
	return nil
}

func (sc scheduler) List(ctx context.Context) ([]bus.Schedule, error) {
	if err := sc.off("schedule:list"); err != nil {
		return nil, err
	}
	st := sc.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []bus.Schedule
	for _, e := range st.schedules[sc.c.id.PluginID] {
		out = append(out, e.schedule())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
