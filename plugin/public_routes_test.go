package plugin

import "testing"

func publicRouteManifest(routes, public []string) Manifest {
	return Manifest{ID: "sample", Version: "1.0.0", Routes: routes, PublicRoutes: public}
}

// TestPublicRoutesMustBeOwned is the rule that makes the field safe to trust
// (gateway TM-325). A plugin may waive authentication only on a route it
// already declared; otherwise a manifest could name another plugin's route, or
// a host route, and open a hole in a surface it does not own.
func TestPublicRoutesMustBeOwned(t *testing.T) {
	if err := publicRouteManifest([]string{"/login", "/logout"}, []string{"/login"}).Validate(); err != nil {
		t.Fatalf("a declared route was refused as public: %v", err)
	}
	// Declaring none is the normal case and must stay valid: every manifest
	// that exists today declares no public routes.
	if err := publicRouteManifest([]string{"/login"}, nil).Validate(); err != nil {
		t.Fatalf("a manifest with no public routes was refused: %v", err)
	}

	for name, m := range map[string]Manifest{
		"not owned":   publicRouteManifest([]string{"/login"}, []string{"/admin"}),
		"owns none":   publicRouteManifest(nil, []string{"/login"}),
		"host route":  publicRouteManifest([]string{"/login"}, []string{"/healthz"}),
		"empty entry": publicRouteManifest([]string{"/login"}, []string{""}),
		"duplicate":   publicRouteManifest([]string{"/login"}, []string{"/login", "/login"}),
	} {
		if err := m.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// TestPublicRouteMatchesPrefixesAndFailsClosed pins how the host will ask the
// question. The default answer is false, because the consequence of being wrong
// in that direction is a login prompt, and in the other direction it is an
// anonymous caller reaching an authenticated surface.
func TestPublicRouteMatchesPrefixesAndFailsClosed(t *testing.T) {
	m := publicRouteManifest(
		[]string{"/login", "/icons/", "/api/session"},
		[]string{"/login", "/icons/"},
	)

	for _, path := range []string{"/login", "/icons/", "/icons/app.png", "/icons/deep/nested.svg"} {
		if !m.PublicRoute(path) {
			t.Errorf("%s should be public", path)
		}
	}
	for _, path := range []string{
		"/api/session", // declared, but not declared public
		"/logins",      // an exact route must not match by prefix
		"/icons",       // the prefix form requires the trailing slash
		"/admin",       // never declared at all
		"",             // nothing is public
	} {
		if m.PublicRoute(path) {
			t.Errorf("%s must not be public", path)
		}
	}

	// A manifest that declares nothing public makes everything authenticated,
	// which is what keeps this additive for every plugin shipping today.
	none := publicRouteManifest([]string{"/login"}, nil)
	if none.PublicRoute("/login") {
		t.Error("a manifest with no public routes reported one")
	}
}
