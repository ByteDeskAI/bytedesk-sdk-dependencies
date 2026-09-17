package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// kvBucket is one durable key/value bucket. Revisions are bucket-scoped and
// monotonic, so a compare-and-swap is a plain revision equality check.
type kvBucket struct {
	spec     bus.BucketSpec
	rev      bus.Revision
	keys     map[string][]bus.Entry
	watchers []*kvWatcher
}

func (b *kvBucket) history() int {
	if b.spec.History <= 0 {
		return 1
	}
	return b.spec.History
}

func (b *kvBucket) current(key string) (bus.Entry, bool) {
	h := b.keys[key]
	if len(h) == 0 {
		return bus.Entry{}, false
	}
	return h[len(h)-1], true
}

// expired reports whether the bucket TTL has retired this entry.
func (b *kvBucket) expired(e bus.Entry, now time.Time) bool {
	return b.spec.TTL > 0 && now.Sub(e.Created) > b.spec.TTL
}

func (b *kvBucket) append(key string, e bus.Entry) {
	h := append(b.keys[key], e)
	if max := b.history(); len(h) > max {
		h = h[len(h)-max:]
	}
	b.keys[key] = h
	for _, w := range b.watchers {
		w.offer(e)
	}
}

func (b *kvBucket) bytes() uint64 {
	var n uint64
	for _, h := range b.keys {
		for _, e := range h {
			n += uint64(len(e.Value))
		}
	}
	return n
}

// -------------------------------------------------------------------- KV

type kvAPI struct{ c *conn }

func (c *conn) KV() bus.KV { return kvAPI{c: c} }

func (k kvAPI) off(op string) error {
	if !k.c.store.cfg.caps.KV {
		return unsupported(op, "kv")
	}
	return k.c.alive()
}

func (k kvAPI) grant(name string) error {
	if !k.c.id.Grants.CanUse(bus.AssetKV, name) {
		return bus.Denied("kv:"+name, k.c.id.String(), "bucket is not provisioned for this principal")
	}
	return nil
}

func (k kvAPI) Declare(ctx context.Context, spec bus.BucketSpec) error {
	if err := k.off("kv:" + spec.Name); err != nil {
		return err
	}
	if spec.Name == "" {
		return bus.Fault{Code: bus.FaultSchema, Op: "kv:", Message: "principal " + k.c.id.String() + ": bucket name is empty"}
	}
	if err := k.grant(spec.Name); err != nil {
		return err
	}
	st := k.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.buckets[spec.Name]; ok {
		return nil
	}
	st.buckets[spec.Name] = &kvBucket{spec: spec, keys: map[string][]bus.Entry{}}
	return nil
}

func (k kvAPI) Open(ctx context.Context, name string) (bus.Bucket, error) {
	if err := k.off("kv:" + name); err != nil {
		return nil, err
	}
	if err := k.grant(name); err != nil {
		return nil, err
	}
	st := k.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	b, ok := st.buckets[name]
	if !ok {
		return nil, bus.Fault{Code: bus.FaultNotFound, Op: "kv:" + name, Message: "principal " + k.c.id.String() + ": no such bucket"}
	}
	return &bucket{c: k.c, name: name, b: b}, nil
}

func (k kvAPI) Delete(ctx context.Context, name string) error {
	if err := k.off("kv:" + name); err != nil {
		return err
	}
	if err := k.grant(name); err != nil {
		return err
	}
	st := k.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	b, ok := st.buckets[name]
	if !ok {
		return bus.Fault{Code: bus.FaultNotFound, Op: "kv:" + name, Message: "principal " + k.c.id.String() + ": no such bucket"}
	}
	for _, w := range b.watchers {
		w.stopWith(bus.Fault{Code: bus.FaultWithdrawn, Op: "kv:" + name, Message: "bucket deleted"})
	}
	delete(st.buckets, name)
	return nil
}

func (k kvAPI) List(ctx context.Context) ([]string, error) {
	if err := k.off("kv:list"); err != nil {
		return nil, err
	}
	st := k.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []string
	for name := range st.buckets {
		if k.c.id.Grants.CanUse(bus.AssetKV, name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ---------------------------------------------------------------- Bucket

type bucket struct {
	c    *conn
	name string
	b    *kvBucket
}

var _ bus.Bucket = (*bucket)(nil)

func (bk *bucket) notFound(key string) error {
	return bus.Fault{Code: bus.FaultNotFound, Op: "kv:" + bk.name + "/" + key, Message: "principal " + bk.c.id.String() + ": no such key"}
}

func (bk *bucket) conflict(key, why string) error {
	return bus.Fault{Code: bus.FaultConflict, Op: "kv:" + bk.name + "/" + key, Message: "principal " + bk.c.id.String() + ": " + why}
}

func (bk *bucket) Get(ctx context.Context, key string) (bus.Entry, error) {
	if err := bk.c.alive(); err != nil {
		return bus.Entry{}, err
	}
	st := bk.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := bk.b.current(key)
	if !ok || e.Deleted || bk.b.expired(e, nowUTC()) {
		return bus.Entry{}, bk.notFound(key)
	}
	return cloneEntry(e), nil
}

func (bk *bucket) put(key string, value []byte, deleted bool) bus.Revision {
	bk.b.rev++
	e := bus.Entry{Key: key, Value: clone(value), Revision: bk.b.rev, Created: nowUTC(), Deleted: deleted}
	bk.b.append(key, e)
	return e.Revision
}

func (bk *bucket) Put(ctx context.Context, key string, value []byte) (bus.Revision, error) {
	if err := bk.c.alive(); err != nil {
		return 0, err
	}
	st := bk.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	return bk.put(key, value, false), nil
}

// Create writes only when the key does not exist. A race between two
// reconcilers resolves as one winner and one FaultConflict, never two writes.
func (bk *bucket) Create(ctx context.Context, key string, value []byte) (bus.Revision, error) {
	if err := bk.c.alive(); err != nil {
		return 0, err
	}
	st := bk.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	if e, ok := bk.b.current(key); ok && !e.Deleted && !bk.b.expired(e, nowUTC()) {
		return 0, bk.conflict(key, "key already exists at revision "+e.Revision.String())
	}
	return bk.put(key, value, false), nil
}

// Update is the compare-and-swap: it writes only when the current revision is
// expect.
func (bk *bucket) Update(ctx context.Context, key string, value []byte, expect bus.Revision) (bus.Revision, error) {
	if err := bk.c.alive(); err != nil {
		return 0, err
	}
	st := bk.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := bk.b.current(key)
	if !ok || e.Deleted || bk.b.expired(e, nowUTC()) {
		return 0, bk.notFound(key)
	}
	if e.Revision != expect {
		return 0, bk.conflict(key, "expected revision "+expect.String()+" but key is at "+e.Revision.String())
	}
	return bk.put(key, value, false), nil
}

func (bk *bucket) Delete(ctx context.Context, key string) error {
	if err := bk.c.alive(); err != nil {
		return err
	}
	st := bk.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	if e, ok := bk.b.current(key); !ok || e.Deleted {
		return bk.notFound(key)
	}
	bk.put(key, nil, true)
	return nil
}

func (bk *bucket) Purge(ctx context.Context, key string) error {
	if err := bk.c.alive(); err != nil {
		return err
	}
	st := bk.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := bk.b.keys[key]; !ok {
		return bk.notFound(key)
	}
	delete(bk.b.keys, key)
	bk.b.rev++
	e := bus.Entry{Key: key, Revision: bk.b.rev, Created: nowUTC(), Deleted: true}
	for _, w := range bk.b.watchers {
		w.offer(e)
	}
	return nil
}

func (bk *bucket) History(ctx context.Context, key string) ([]bus.Entry, error) {
	if err := bk.c.alive(); err != nil {
		return nil, err
	}
	st := bk.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	h, ok := bk.b.keys[key]
	if !ok {
		return nil, bk.notFound(key)
	}
	out := make([]bus.Entry, 0, len(h))
	for _, e := range h {
		out = append(out, cloneEntry(e))
	}
	return out, nil
}

func (bk *bucket) Keys(ctx context.Context) ([]string, error) {
	if err := bk.c.alive(); err != nil {
		return nil, err
	}
	st := bk.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	now := nowUTC()
	var out []string
	for key := range bk.b.keys {
		if e, ok := bk.b.current(key); ok && !e.Deleted && !bk.b.expired(e, now) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (bk *bucket) Status(ctx context.Context) (bus.BucketStatus, error) {
	if err := bk.c.alive(); err != nil {
		return bus.BucketStatus{}, err
	}
	st := bk.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	now := nowUTC()
	var values uint64
	for key := range bk.b.keys {
		if e, ok := bk.b.current(key); ok && !e.Deleted && !bk.b.expired(e, now) {
			values++
		}
	}
	return bus.BucketStatus{
		Name:    bk.name,
		Values:  values,
		Bytes:   bk.b.bytes(),
		History: bk.b.history(),
		TTL:     bk.b.spec.TTL,
	}, nil
}

// Watch delivers the current value of every matching key first, then every
// change. Filter "" and ">" both watch the whole bucket.
func (bk *bucket) Watch(ctx context.Context, filter string) (bus.Watcher, error) {
	if err := bk.c.alive(); err != nil {
		return nil, err
	}
	if filter == "" {
		filter = ">"
	}
	p, err := bus.ParsePattern(filter)
	if err != nil {
		return nil, bus.Fault{Code: bus.FaultSchema, Op: "kv:" + bk.name, Message: err.Error(), Err: err}
	}
	w := &kvWatcher{filter: p, updates: make(chan bus.Entry), wake: make(chan struct{}, 1), doneCh: make(chan struct{})}
	st := bk.c.store
	st.mu.Lock()
	now := nowUTC()
	keys := make([]string, 0, len(bk.b.keys))
	for key := range bk.b.keys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		e, ok := bk.b.current(key)
		if !ok || e.Deleted || bk.b.expired(e, now) {
			continue
		}
		if p.Matches(bus.Subject(key)) {
			w.queue = append(w.queue, cloneEntry(e))
		}
	}
	bk.b.watchers = append(bk.b.watchers, w)
	st.mu.Unlock()
	bk.c.mu.Lock()
	bk.c.watchers = append(bk.c.watchers, w)
	bk.c.mu.Unlock()
	go w.run()
	return w, nil
}

// ---------------------------------------------------------------- Watcher

type kvWatcher struct {
	filter  bus.Pattern
	updates chan bus.Entry

	mu      sync.Mutex
	queue   []bus.Entry
	err     error
	stopped bool

	wake   chan struct{}
	doneCh chan struct{}
	once   sync.Once
}

var _ bus.Watcher = (*kvWatcher)(nil)

// offer is called with the store lock held, so it must never block: a slow
// watcher queues, it does not stall the writer.
func (w *kvWatcher) offer(e bus.Entry) {
	if !w.filter.Matches(bus.Subject(e.Key)) {
		return
	}
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.queue = append(w.queue, cloneEntry(e))
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *kvWatcher) run() {
	defer close(w.updates)
	for {
		w.mu.Lock()
		var e bus.Entry
		var have bool
		if len(w.queue) > 0 {
			e, have = w.queue[0], true
			w.queue = w.queue[1:]
		}
		w.mu.Unlock()
		if have {
			select {
			case w.updates <- e:
			case <-w.doneCh:
				return
			}
			continue
		}
		select {
		case <-w.wake:
		case <-w.doneCh:
			return
		}
	}
}

func (w *kvWatcher) Updates() <-chan bus.Entry { return w.updates }

func (w *kvWatcher) Stop() { w.stopWith(nil) }

func (w *kvWatcher) stopWith(err error) {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	if err != nil {
		w.err = err
	}
	w.mu.Unlock()
	w.once.Do(func() { close(w.doneCh) })
}

func (w *kvWatcher) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func cloneEntry(e bus.Entry) bus.Entry {
	e.Value = clone(e.Value)
	return e
}
