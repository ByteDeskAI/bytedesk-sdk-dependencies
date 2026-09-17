package bus

import (
	"context"
	"io"
	"time"
)

// BucketSpec declares a KV bucket. The host provisions only buckets the
// manifest lists.
type BucketSpec struct {
	Name string
	// History is how many revisions of a key are kept. Zero means one.
	History int
	// TTL expires every key after this long. Zero means no expiry.
	TTL time.Duration
	// MaxBytes bounds the bucket.
	MaxBytes int64
	// Metadata carries bd-schema: one payload type per bucket, checked when
	// the bucket is opened rather than per message, because a KV value has no
	// headers of its own to carry a hash.
	Metadata map[string]string
}

// Entry is one KV value with its version.
type Entry struct {
	Key      string
	Value    []byte
	Revision Revision
	Created  time.Time
	// Deleted marks a tombstone: Watch and History report deletions, Get does
	// not return them.
	Deleted bool
}

// BucketStatus is a bucket's observed state.
type BucketStatus struct {
	Name    string
	Values  uint64
	Bytes   uint64
	History int
	TTL     time.Duration
}

// KV is the key/value store.
type KV interface {
	// Declare creates or confirms a bucket. Idempotent; refuses a bucket the
	// manifest does not list.
	Declare(ctx context.Context, spec BucketSpec) error
	// Open returns a handle to an existing bucket, checking bd-schema when the
	// spec that declared it carried one.
	Open(ctx context.Context, name string) (Bucket, error)
	// Delete removes a bucket and everything in it.
	Delete(ctx context.Context, name string) error
	// List names the buckets this principal may open.
	List(ctx context.Context) ([]string, error)
}

// Bucket is one key/value bucket.
type Bucket interface {
	// Get reads the current value. A missing key is FaultNotFound.
	Get(ctx context.Context, key string) (Entry, error)
	// Put writes unconditionally and returns the new revision.
	Put(ctx context.Context, key string, value []byte) (Revision, error)
	// Create writes only if the key does not exist; otherwise FaultConflict.
	Create(ctx context.Context, key string, value []byte) (Revision, error)
	// Update writes only if the current revision is expect; otherwise
	// FaultConflict. This is the compare-and-swap every reconciler needs.
	Update(ctx context.Context, key string, value []byte, expect Revision) (Revision, error)
	// Delete writes a tombstone, keeping history.
	Delete(ctx context.Context, key string) error
	// Purge removes a key and its history.
	Purge(ctx context.Context, key string) error
	// Watch delivers the current value of every key matching filter, then
	// every change. Filter "" or ">" watches the whole bucket.
	Watch(ctx context.Context, filter string) (Watcher, error)
	// History returns every retained revision of key, oldest first.
	History(ctx context.Context, key string) ([]Entry, error)
	// Keys lists the live keys.
	Keys(ctx context.Context) ([]string, error)
	// Status reports the bucket's state.
	Status(ctx context.Context) (BucketStatus, error)
}

// Watcher streams bucket changes.
type Watcher interface {
	// Updates yields each change. It closes when the watcher is stopped.
	Updates() <-chan Entry
	// Stop ends the watch.
	Stop()
	// Err reports why it ended.
	Err() error
}

// ObjectMeta describes a stored blob.
type ObjectMeta struct {
	Name        string
	Bucket      string
	Size        int64
	Digest      string
	Modified    time.Time
	Description string
	Headers     Headers
}

// Objects stores blobs by reference: the bus carries the name, not the bytes,
// so a 40 MB artifact does not have to fit in a message.
type Objects interface {
	// Declare creates or confirms an object bucket.
	Declare(ctx context.Context, spec BucketSpec) error
	// Put streams r into the named object and returns its metadata.
	Put(ctx context.Context, meta ObjectMeta, r io.Reader) (ObjectMeta, error)
	// Get opens the object for reading. The caller closes it.
	Get(ctx context.Context, bucket, name string) (io.ReadCloser, ObjectMeta, error)
	// Info reads metadata without the bytes.
	Info(ctx context.Context, bucket, name string) (ObjectMeta, error)
	// Delete removes an object.
	Delete(ctx context.Context, bucket, name string) error
	// List names the objects in a bucket.
	List(ctx context.Context, bucket string) ([]ObjectMeta, error)
	// Watch reports objects added, changed or removed.
	Watch(ctx context.Context, bucket string) (ObjectWatcher, error)
}

// ObjectWatcher streams object-bucket changes.
type ObjectWatcher interface {
	Updates() <-chan ObjectMeta
	Stop()
	Err() error
}
