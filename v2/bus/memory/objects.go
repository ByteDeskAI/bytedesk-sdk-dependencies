package memory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sort"
	"sync"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

type objBucket struct {
	spec     bus.BucketSpec
	objs     map[string]*object
	watchers []*objWatcher
}

type object struct {
	meta bus.ObjectMeta
	data []byte
}

type objectsAPI struct{ c *conn }

func (c *conn) Objects() bus.Objects { return objectsAPI{c: c} }

func (o objectsAPI) off(op string) error {
	if !o.c.store.cfg.caps.Objects {
		return unsupported(op, "objects")
	}
	return o.c.alive()
}

func (o objectsAPI) grant(name string) error {
	if !o.c.id.Grants.CanUse(bus.AssetObjects, name) {
		return bus.Denied("obj:"+name, o.c.id.String(), "object bucket is not provisioned for this principal")
	}
	return nil
}

func (o objectsAPI) lookupLocked(name string) (*objBucket, error) {
	if err := o.grant(name); err != nil {
		return nil, err
	}
	b, ok := o.c.store.objects[name]
	if !ok {
		return nil, bus.Fault{Code: bus.FaultNotFound, Op: "obj:" + name, Message: "principal " + o.c.id.String() + ": no such object bucket"}
	}
	return b, nil
}

func (o objectsAPI) Declare(ctx context.Context, spec bus.BucketSpec) error {
	if err := o.off("obj:" + spec.Name); err != nil {
		return err
	}
	if spec.Name == "" {
		return bus.Fault{Code: bus.FaultSchema, Op: "obj:", Message: "principal " + o.c.id.String() + ": bucket name is empty"}
	}
	if err := o.grant(spec.Name); err != nil {
		return err
	}
	st := o.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.objects[spec.Name]; ok {
		return nil
	}
	st.objects[spec.Name] = &objBucket{spec: spec, objs: map[string]*object{}}
	return nil
}

// Put streams r into the named object. The bus carries the name, not the
// bytes, so the payload ceiling does not apply here.
func (o objectsAPI) Put(ctx context.Context, meta bus.ObjectMeta, r io.Reader) (bus.ObjectMeta, error) {
	if err := o.off("obj:" + meta.Bucket + "/" + meta.Name); err != nil {
		return bus.ObjectMeta{}, err
	}
	if meta.Name == "" {
		return bus.ObjectMeta{}, bus.Fault{Code: bus.FaultSchema, Op: "obj:" + meta.Bucket, Message: "principal " + o.c.id.String() + ": object name is empty"}
	}
	if r == nil {
		r = bytes.NewReader(nil)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return bus.ObjectMeta{}, bus.Fault{Code: bus.FaultUnhandled, Op: "obj:" + meta.Bucket + "/" + meta.Name, Message: "principal " + o.c.id.String() + ": " + err.Error(), Err: err}
	}
	sum := sha256.Sum256(data)
	st := o.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	b, err := o.lookupLocked(meta.Bucket)
	if err != nil {
		return bus.ObjectMeta{}, err
	}
	if b.spec.MaxBytes > 0 && int64(len(data)) > b.spec.MaxBytes {
		return bus.ObjectMeta{}, bus.Fault{
			Code:    bus.FaultBudget,
			Op:      "obj:" + meta.Bucket + "/" + meta.Name,
			Message: "principal " + o.c.id.String() + ": object exceeds the bucket's MaxBytes",
		}
	}
	out := meta
	out.Size = int64(len(data))
	out.Digest = "sha-256=" + hex.EncodeToString(sum[:])
	out.Modified = nowUTC()
	out.Headers = meta.Headers.Clone()
	b.objs[meta.Name] = &object{meta: out, data: clone(data)}
	for _, w := range b.watchers {
		w.offer(out)
	}
	return out, nil
}

func (o objectsAPI) Get(ctx context.Context, bucketName, name string) (io.ReadCloser, bus.ObjectMeta, error) {
	if err := o.off("obj:" + bucketName + "/" + name); err != nil {
		return nil, bus.ObjectMeta{}, err
	}
	st := o.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	b, err := o.lookupLocked(bucketName)
	if err != nil {
		return nil, bus.ObjectMeta{}, err
	}
	obj, ok := b.objs[name]
	if !ok {
		return nil, bus.ObjectMeta{}, bus.Fault{Code: bus.FaultNotFound, Op: "obj:" + bucketName + "/" + name, Message: "principal " + o.c.id.String() + ": no such object"}
	}
	return io.NopCloser(bytes.NewReader(clone(obj.data))), obj.meta, nil
}

func (o objectsAPI) Info(ctx context.Context, bucketName, name string) (bus.ObjectMeta, error) {
	if err := o.off("obj:" + bucketName + "/" + name); err != nil {
		return bus.ObjectMeta{}, err
	}
	st := o.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	b, err := o.lookupLocked(bucketName)
	if err != nil {
		return bus.ObjectMeta{}, err
	}
	obj, ok := b.objs[name]
	if !ok {
		return bus.ObjectMeta{}, bus.Fault{Code: bus.FaultNotFound, Op: "obj:" + bucketName + "/" + name, Message: "principal " + o.c.id.String() + ": no such object"}
	}
	return obj.meta, nil
}

func (o objectsAPI) Delete(ctx context.Context, bucketName, name string) error {
	if err := o.off("obj:" + bucketName + "/" + name); err != nil {
		return err
	}
	st := o.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	b, err := o.lookupLocked(bucketName)
	if err != nil {
		return err
	}
	obj, ok := b.objs[name]
	if !ok {
		return bus.Fault{Code: bus.FaultNotFound, Op: "obj:" + bucketName + "/" + name, Message: "principal " + o.c.id.String() + ": no such object"}
	}
	delete(b.objs, name)
	gone := obj.meta
	gone.Size = 0
	gone.Digest = ""
	gone.Modified = nowUTC()
	for _, w := range b.watchers {
		w.offer(gone)
	}
	return nil
}

func (o objectsAPI) List(ctx context.Context, bucketName string) ([]bus.ObjectMeta, error) {
	if err := o.off("obj:" + bucketName); err != nil {
		return nil, err
	}
	st := o.c.store
	st.mu.Lock()
	defer st.mu.Unlock()
	b, err := o.lookupLocked(bucketName)
	if err != nil {
		return nil, err
	}
	out := make([]bus.ObjectMeta, 0, len(b.objs))
	for _, obj := range b.objs {
		out = append(out, obj.meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (o objectsAPI) Watch(ctx context.Context, bucketName string) (bus.ObjectWatcher, error) {
	if err := o.off("obj:" + bucketName); err != nil {
		return nil, err
	}
	w := &objWatcher{updates: make(chan bus.ObjectMeta), wake: make(chan struct{}, 1), doneCh: make(chan struct{})}
	st := o.c.store
	st.mu.Lock()
	b, err := o.lookupLocked(bucketName)
	if err != nil {
		st.mu.Unlock()
		return nil, err
	}
	names := make([]string, 0, len(b.objs))
	for name := range b.objs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w.queue = append(w.queue, b.objs[name].meta)
	}
	b.watchers = append(b.watchers, w)
	st.mu.Unlock()
	o.c.mu.Lock()
	o.c.watchers = append(o.c.watchers, w)
	o.c.mu.Unlock()
	go w.run()
	return w, nil
}

type objWatcher struct {
	updates chan bus.ObjectMeta

	mu      sync.Mutex
	queue   []bus.ObjectMeta
	err     error
	stopped bool

	wake   chan struct{}
	doneCh chan struct{}
	once   sync.Once
}

var _ bus.ObjectWatcher = (*objWatcher)(nil)

func (w *objWatcher) offer(m bus.ObjectMeta) {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.queue = append(w.queue, m)
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *objWatcher) run() {
	defer close(w.updates)
	for {
		w.mu.Lock()
		var m bus.ObjectMeta
		var have bool
		if len(w.queue) > 0 {
			m, have = w.queue[0], true
			w.queue = w.queue[1:]
		}
		w.mu.Unlock()
		if have {
			select {
			case w.updates <- m:
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

func (w *objWatcher) Updates() <-chan bus.ObjectMeta { return w.updates }

func (w *objWatcher) Stop() { w.stopWith(nil) }

func (w *objWatcher) stopWith(err error) {
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

func (w *objWatcher) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}
