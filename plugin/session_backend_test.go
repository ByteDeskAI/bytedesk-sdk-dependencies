package plugin

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

type stubSessionBackend struct{}

func (stubSessionBackend) ID() string { return SessionBackendLocal }
func (stubSessionBackend) Create(context.Context, SessionSpec) (SessionRef, error) {
	return SessionRef{}, nil
}
func (stubSessionBackend) Close(context.Context, SessionRef) error                  { return nil }
func (stubSessionBackend) List(context.Context) ([]SessionStatus, error)            { return nil, nil }
func (stubSessionBackend) SendText(context.Context, SessionRef, string, bool) error { return nil }
func (stubSessionBackend) Capture(context.Context, SessionRef, int) (string, error) { return "", nil }
func (stubSessionBackend) Attach(context.Context, SessionRef, int, int) (Stream, error) {
	return nil, nil
}

type stubStream struct{}

func (stubStream) Read([]byte) (int, error)  { return 0, nil }
func (stubStream) Write([]byte) (int, error) { return 0, nil }
func (stubStream) Close() error              { return nil }
func (stubStream) Resize(int, int) error     { return nil }

var (
	_ SessionBackend = stubSessionBackend{}
	_ Stream         = stubStream{}
)

// TestSessionBackendMethodSetIsPinned: the host routes every terminal operation
// through this interface, and each backend is a separate implementation. Adding
// or removing a method breaks all of them at once, so it needs a version bump
// and a deliberate edit here.
func TestSessionBackendMethodSetIsPinned(t *testing.T) {
	want := []string{"Attach", "Capture", "Close", "Create", "ID", "List", "SendText"}
	typ := reflect.TypeOf((*SessionBackend)(nil)).Elem()
	got := make([]string, 0, typ.NumMethod())
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SessionBackend method set changed.\n got: %v\nwant: %v\n\n"+
			"Every session backend implements all of it. Bump the SDK version and "+
			"update this list deliberately.", got, want)
	}
}

// TestStreamMethodSetIsPinned: same reasoning for the attach stream. It is
// io.ReadWriteCloser plus Resize and nothing else, because the host pumps it
// byte for byte between the session and the browser.
func TestStreamMethodSetIsPinned(t *testing.T) {
	want := []string{"Close", "Read", "Resize", "Write"}
	typ := reflect.TypeOf((*Stream)(nil)).Elem()
	got := make([]string, 0, typ.NumMethod())
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Stream method set changed.\n got: %v\nwant: %v", got, want)
	}
}

// TestSessionBackendPointIsNamespaced keeps the point name one a manifest can
// actually declare. A bare "session.backend" passes review and then fails
// admission, because Extends names must be namespaced.
func TestSessionBackendPointIsNamespaced(t *testing.T) {
	if SessionBackendPoint != "host.session.backend" {
		t.Errorf("SessionBackendPoint = %q, want host.session.backend", SessionBackendPoint)
	}
	if err := ValidateExtendsName(nil, SessionBackendPoint); err != nil {
		t.Errorf("ValidateExtendsName(%q) = %v, want nil", SessionBackendPoint, err)
	}
}
