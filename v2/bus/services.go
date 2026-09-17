package bus

import (
	"context"
	"time"
)

// EndpointSpec is one operation of a service.
type EndpointSpec struct {
	Name string
	// Subject must sit under the service owner's own namespace
	// (svc.<id>. or cmd.<id>.); Registrar.Mount refuses anything else,
	// naming the subject.
	Subject Subject
	// QueueGroup defaults to the service's group when empty.
	QueueGroup string
	Handler    Handler
	// Point names the extension point this endpoint implements, from the
	// typed registry in plugin/points.go. Empty for an ordinary endpoint.
	Point string
}

// ServiceSpec declares a service: a named, versioned, discoverable group of
// endpoints. Extension points are services — a provider mounts
// svc.<provider>.<point>.v1.<op> and the host discovers it rather than the
// provider registering itself through a bespoke adapter.
type ServiceSpec struct {
	Name        string
	Version     string
	Description string
	QueueGroup  string
	Endpoints   []EndpointSpec
	// Metadata is advertised by Discover; the host reads priority from it.
	Metadata map[string]string
}

// ServiceInfo is one discovered service instance.
type ServiceInfo struct {
	ID        string
	Name      string
	Version   string
	Endpoints []EndpointInfo
	Metadata  map[string]string
}

// EndpointInfo is one discovered endpoint.
type EndpointInfo struct {
	Name       string
	Subject    Subject
	QueueGroup string
	Point      string
}

// ServiceStats is one service instance's counters.
type ServiceStats struct {
	ID         string
	Name       string
	Requests   uint64
	Errors     uint64
	Processing time.Duration
	Started    time.Time
}

// Service is a mounted service.
type Service interface {
	// Stop withdraws every endpoint.
	Stop(ctx context.Context) error
	// Info describes what is mounted.
	Info() ServiceInfo
	// Stats reports this instance's counters.
	Stats() ServiceStats
	// Done closes when the service has stopped.
	Done() <-chan struct{}
	// Err reports why it stopped.
	Err() error
}

// Services is request/reply with discovery, versioning and stats.
type Services interface {
	// Serve mounts a service. It refuses an endpoint outside the caller's own
	// namespace or outside the manifest's serves list, naming the subject.
	Serve(ctx context.Context, spec ServiceSpec) (Service, error)
	// Call invokes an endpoint. A service error reply (bd-fault) comes back
	// as a Fault, not as a payload the caller has to inspect.
	Call(ctx context.Context, subject Subject, data []byte, opts ...ReqOpt) (*Msg, error)
	// Discover finds live services by name, or every visible service when
	// name is empty.
	Discover(ctx context.Context, name string) ([]ServiceInfo, error)
	// Stats collects counters from live instances of a service.
	Stats(ctx context.Context, name string) ([]ServiceStats, error)
}
