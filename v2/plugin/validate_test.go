package plugin

import (
	"strings"
	"testing"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// TestValidate is the contract table. Each case asserts the SUBSTRING that
// carries the rule — the offending pattern, the suggestion, the vocabulary —
// rather than the whole message, so rewording an error does not fail the suite
// but dropping the evidence from it does.
func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Manifest)
		wantErr string // "" means the manifest must validate
	}{
		{"the reference manifest", func(*Manifest) {}, ""},

		// Grammar. The parser is the one the substrate matches with, so a
		// pattern that validates here means the same thing at delivery time.
		{"token outside the grammar", func(m *Manifest) {
			m.Permissions.Publish = []bus.Pattern{"event.Files.>"}
		}, `event.Files.>`},
		{"more than sixteen tokens", func(m *Manifest) {
			m.Permissions.Publish = []bus.Pattern{bus.Pattern(strings.Repeat("a.", 16) + "a")}
		}, "more than 16 tokens"},
		{"longer than 255 bytes", func(m *Manifest) {
			m.Permissions.Subscribe = []bus.Pattern{bus.Pattern("event." + strings.Repeat("a", 260))}
		}, "longer than 255 bytes"},
		{"> is not the last token", func(m *Manifest) {
			m.Permissions.Subscribe = []bus.Pattern{"event.>.opened"}
		}, `">" is only allowed as the last token`},
		{"* in a concrete subject", func(m *Manifest) {
			m.Serves[0].Endpoints[0].Subject = "svc.tmux-manager.*"
		}, `"*" is not allowed in a concrete subject`},
		{"duplicate permission entry", func(m *Manifest) {
			m.Permissions.Request = []bus.Pattern{"cmd.files.v1.list", "cmd.files.v1.list"}
		}, "duplicate"},

		// The plugin's own namespace is implicit and is never written down.
		{"own event family listed", func(m *Manifest) {
			m.Permissions.Publish = []bus.Pattern{"event.tmux-manager.>"}
		}, "implicit"},
		{"one subject of the own namespace listed", func(m *Manifest) {
			m.Permissions.Publish = []bus.Pattern{"event.tmux-manager.opened"}
		}, `"event.tmux-manager.opened"`},
		{"own command tree listed", func(m *Manifest) {
			m.Permissions.Subscribe = []bus.Pattern{"cmd.tmux-manager.v1.attach"}
		}, "implicit"},

		// Reserved tokens parse on purpose; the manifest is where they stop.
		{"substrate namespace", func(m *Manifest) {
			m.Permissions.Subscribe = []bus.Pattern{"$SYS.>"}
		}, "reserved token"},
		{"another principal's inbox", func(m *Manifest) {
			m.Permissions.Subscribe = []bus.Pattern{"_INBOX.x.>"}
		}, "reserved token"},
		{"the mesh prefix", func(m *Manifest) {
			m.Permissions.Publish = []bus.Pattern{"bd.mesh.>"}
		}, "mesh prefix"},

		// Permanently ineligible families. Refused whole, never narrowed.
		{"an ineligible subject", func(m *Manifest) {
			m.Permissions.Request = []bus.Pattern{"cmd.auth.v1.mint"}
		}, "permanently ineligible"},
		{"a pattern that merely reaches one", func(m *Manifest) {
			m.Permissions.Request = []bus.Pattern{"cmd.>"}
		}, "refused rather than narrowed"},
		{"an ineligible exact operation", func(m *Manifest) {
			m.Permissions.Request = []bus.Pattern{"cmd.terminal.v1.write"}
		}, "cmd.terminal.v1.write"},
		{"the operator's disable verb", func(m *Manifest) {
			m.Permissions.Request = []bus.Pattern{"cmd.plugin.v1.disable"}
		}, "permanently ineligible"},
		{"the whole lifecycle family", func(m *Manifest) {
			m.Permissions.Request = []bus.Pattern{"cmd.plugin.>"}
		}, "refused rather than narrowed"},
		{"the spawned handshake is requestable", func(m *Manifest) {
			m.Permissions.Request = []bus.Pattern{"cmd.plugin.v1.negotiate"}
		}, ""},

		// Serves: the export surface.
		{"endpoint outside the own namespace", func(m *Manifest) {
			m.Serves[0].Endpoints[0].Subject = "svc.files.v1.list"
		}, `"svc.files.v1.list"`},
		{"endpoint under another plugin's command tree", func(m *Manifest) {
			m.Serves[0].Endpoints[0].Subject = "cmd.identity.v1.enroll"
		}, "outside this plugin's own namespace"},
		{"duplicate service name", func(m *Manifest) {
			m.Serves = append(m.Serves, ServiceDecl{Name: "tmux"})
		}, `duplicate service name "tmux"`},
		{"duplicate endpoint name within a service", func(m *Manifest) {
			m.Serves[0].Endpoints = append(m.Serves[0].Endpoints,
				EndpointDecl{Name: "list", Subject: "svc.tmux-manager.list2"})
		}, `duplicate endpoint name "list"`},

		// Extension points, with the suggestion an author actually needs.
		{"the settings point without its namespace", func(m *Manifest) {
			m.Implements = []Provider{{Point: "settings.section", ID: "tmux-manager"}}
		}, `did you mean "host.settings.section"?`},
		{"a serves endpoint naming a host command", func(m *Manifest) {
			m.Serves[0].Endpoints[0].Point = "host.auth.method"
		}, "is not a host extension point"},
		{"a serves endpoint naming a host action", func(m *Manifest) {
			m.Serves[0].Endpoints[0].Point = "host.system.action"
		}, "is not a host extension point"},

		// Assets. An unbounded stream is a disk that fills without a symptom.
		{"a stream without a byte ceiling", func(m *Manifest) {
			m.Streams[0].MaxBytes = 0
		}, "maxBytes is required"},
		{"a stream without an age ceiling", func(m *Manifest) {
			m.Streams[0].MaxAgeSeconds = 0
		}, "maxAgeSeconds is required"},
		{"duplicate stream name", func(m *Manifest) {
			m.Streams = append(m.Streams, StreamDecl{Name: "tmux-events", MaxBytes: 1, MaxAgeSeconds: 1})
		}, `duplicate stream name "tmux-events"`},
		{"duplicate kv bucket name", func(m *Manifest) {
			m.KV = append(m.KV, KVDecl{Name: "tmux-prefs"})
		}, `duplicate bucket name "tmux-prefs"`},
		{"duplicate object bucket name", func(m *Manifest) {
			m.Objects = append(m.Objects, ObjectDecl{Name: "tmux-captures"})
		}, `duplicate bucket name "tmux-captures"`},

		// Needs: a closed vocabulary, because an ignored typo fails at the
		// first call instead of at authoring time.
		{"an unknown capability", func(m *Manifest) {
			m.Needs = []string{"postgres"}
		}, `unknown capability "postgres"`},
		{"an unknown capability lists the vocabulary", func(m *Manifest) {
			m.Needs = []string{"postgres"}
		}, "durable kv objects services schedule counters batch trace"},
		{"a duplicate capability", func(m *Manifest) {
			m.Needs = []string{"kv", "kv"}
		}, "duplicate capability"},

		// Carried-over v1 rules that still hold.
		{"no version", func(m *Manifest) { m.Version = "" }, "plugin version required"},
		{"requires self", func(m *Manifest) {
			m.Requires = []Requirement{{ID: "tmux-manager"}}
		}, "cannot be self"},
		{"a spawn binary with a path", func(m *Manifest) {
			m.Spawn, m.Binary = true, "../evil"
		}, "relative basename"},
		{"a public route this manifest does not own", func(m *Manifest) {
			m.PublicRoutes = []string{"/p/other/login"}
		}, "is not one of this manifest's routes"},
		{"an unknown ui slot", func(m *Manifest) {
			m.UI[0].Slot = "sidebar"
		}, `unknown ui slot "sidebar"`},
		{"a binding event that is not an exact name", func(m *Manifest) {
			m.UI[0].Bindings[0].Event = "event.tmux-manager.*"
		}, "ui.bindings.event"},
		{"a protocol major from another schema", func(m *Manifest) {
			m.Protocol.Major = 1
		}, "is not this manifest schema"},
		{"an unknown lifecycle hook", func(m *Manifest) {
			m.Protocol.Hooks = []string{"stop"}
		}, `unknown lifecycle hook "stop"`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := fullManifest()
			c.mutate(&m)
			err := Validate(m)
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("want valid, got %v", err)
			case c.wantErr == "":
			case err == nil:
				t.Fatalf("want an error containing %q, got none", c.wantErr)
			case !strings.Contains(err.Error(), c.wantErr):
				t.Fatalf("error %q does not carry %q", err, c.wantErr)
			}
		})
	}
}

// TestPermanentlyIneligibleIsARealFence is the check this task exists to make
// unskippable: a deny list that silently became empty, or that carries an entry
// the matcher cannot parse, is a fence with no wire in it — and every test
// asking "is this refused?" still passes, because nothing was granted either.
func TestPermanentlyIneligibleIsARealFence(t *testing.T) {
	set := PermanentlyIneligible()
	if len(set) == 0 {
		t.Fatal("PermanentlyIneligible is empty: deny-wins denies nothing")
	}
	seen := map[bus.Pattern]bool{}
	for _, p := range set {
		if _, err := bus.ParsePattern(string(p)); err != nil {
			t.Errorf("%q does not parse, so it can never match: %v", string(p), err)
		}
		if seen[p] {
			t.Errorf("%q appears twice", string(p))
		}
		seen[p] = true
	}
	// The families the gateway's hostFixedObjectFamilies table fixed
	// (src/host_read_rules.go:72) must all still be covered, or this list
	// dropped a rule while looking healthy.
	for _, name := range []string{
		"$SYS.account.info",
		"cmd.auth.v1.mint", "cmd.session.v1.sign", "cmd.identity.v1.enroll",
		"cmd.secrets.v1.read", "cmd.cutover.v1.run",
		"cmd.plugin.v1.disable", "cmd.plugin.v1.announce",
		"cmd.statedir.v1.read", "cmd.terminal.v1.write",
	} {
		if !bus.MatchedByAny(set, bus.Subject(name)) {
			t.Errorf("%q is no longer permanently ineligible", name)
		}
	}
	// A plugin's own inbox and a plain command must NOT be caught, or the fence
	// is denying the traffic it exists to protect.
	for _, name := range []string{
		"_INBOX.tmux-manager.7", "cmd.files.v1.list", "event.session.opened",
		"cmd.plugin.v1.negotiate",
	} {
		if bus.MatchedByAny(set, bus.Subject(name)) {
			t.Errorf("%q is denied by the ineligible set; the fence is too wide", name)
		}
	}
}

// TestSpawnedHandshakeIsNotDenied is the one assertion that catches widening
// this list back to the family "cmd.plugin.>".
//
// PermanentlyIneligible is compiled into every credential as deny, and deny
// wins over every allow. cmd.plugin.v1.negotiate is the FIRST thing a spawned
// plugin does after dialling, before it is bound, so a deny covering it refuses
// every spawned plugin at startup. Nothing else would notice: the memory
// substrate has no handshake, so the conformance suite and the validator table
// both stay green while the real wire contract is unusable.
func TestSpawnedHandshakeIsNotDenied(t *testing.T) {
	const handshake bus.Subject = "cmd.plugin.v1.negotiate"
	for _, d := range PermanentlyIneligible() {
		if d.Matches(handshake) {
			t.Fatalf("%q denies %q. That is the spawned handshake, requested after Dial "+
				"and before Bind; denying it refuses every spawned plugin. If cmd.plugin. "+
				"must be closed as a family, the handshake has to move out of it first.",
				string(d), string(handshake))
		}
	}
	// And a manifest must be able to ASK for it, or the grant compiler has
	// nothing to put in the allow set.
	m := fullManifest()
	m.Permissions.Request = append(m.Permissions.Request, bus.Pattern(handshake))
	if err := Validate(m); err != nil {
		t.Fatalf("a manifest requesting the handshake was refused: %v", err)
	}
}

// TestPatternsOverlapRefusesRatherThanIntersects pins the helper that makes
// "no narrowing" structural. Overlap is a yes/no question a refusal can be
// written against; an intersection would be a smaller grant nobody declared.
func TestPatternsOverlapRefusesRatherThanIntersects(t *testing.T) {
	cases := []struct {
		a, b bus.Pattern
		want bool
	}{
		{"cmd.auth.>", "cmd.>", true},
		{"cmd.auth.>", "cmd.auth.v1.mint", true},
		{"cmd.auth.>", "cmd.files.>", false},
		{"cmd.auth.>", "cmd.*.v1.mint", true},
		{"cmd.terminal.v1.write", "cmd.terminal.v1.read", false},
		{"cmd.terminal.v1.write", "cmd.terminal.*.write", true},
		{"cmd.terminal.v1.write", "cmd.terminal.v1", false},
		{"event.a.b", "event.a.b", true},
		{"event.a", "event.a.b", false},
	}
	for _, c := range cases {
		if got := patternsOverlap(c.a, c.b); got != c.want {
			t.Errorf("patternsOverlap(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
		if got := patternsOverlap(c.b, c.a); got != c.want {
			t.Errorf("patternsOverlap(%q, %q) = %v, want %v (not symmetric)", c.b, c.a, got, c.want)
		}
	}
}

// TestValidateExtendsNames is v1's author-time half, carried over: a point a
// plugin OWNS is well formed and namespaced. Whether a host. name is allowed at
// all is the host's decision at admission, because a manifest cannot prove
// where it runs.
func TestValidateExtendsNames(t *testing.T) {
	acme := &Publisher{ID: "acme", Name: "Acme"}
	cases := []struct {
		name      string
		publisher *Publisher
		point     string
		ok        bool
	}{
		{"publisher namespace", acme, "acme.widgets.panel", true},
		{"host namespace is well formed", nil, "host.widgets.panel", true},
		{"mixed-case publisher id", &Publisher{ID: "Acme"}, "acme.widgets.panel", true},
		{"hyphens and digits", acme, "acme.s3-v2.bucket", true},
		{"bare legacy name", acme, "files.s3", false},
		{"two-part publisher name", acme, "acme.widgets", true},
		{"host name needs an area and a name", nil, "host.files", false},
		{"single segment", acme, "acme", false},
		{"uppercase", acme, "acme.Widgets.panel", false},
		{"empty segment", acme, "acme..panel", false},
		{"another publisher's namespace", acme, "bytedesk.widgets.panel", false},
		{"publisher id is only a prefix of the segment", acme, "acmecorp.widgets.panel", false},
		{"no publisher", nil, "acme.widgets.panel", false},
		{"empty publisher id", &Publisher{}, "acme.widgets.panel", false},
	}
	for _, c := range cases {
		m := Manifest{ID: "example", Version: "1", Publisher: c.publisher,
			Extends: []ExtensionPoint{{Name: c.point, Interface: "x.v1"}}}
		if err := Validate(m); (err == nil) != c.ok {
			t.Errorf("%s: %q ok=%v err=%v", c.name, c.point, c.ok, err)
		}
	}
}

// TestValidateDiscoverLiftsOnlyTheVersionRequirement. Discovery reads a
// plugin.json off disk before anything has assigned it a version, so the strict
// gate cannot be the loading gate. What must NOT come off with it is any
// authority rule: the manifest discovery admits is the one that gets enabled,
// so a lenient discover path would be a way to smuggle a grant past the
// validator by omitting a version.
func TestValidateDiscoverLiftsOnlyTheVersionRequirement(t *testing.T) {
	unversioned := fullManifest()
	unversioned.Version = ""
	if err := ValidateDiscover(unversioned); err != nil {
		t.Fatalf("ValidateDiscover refused an unversioned manifest: %v", err)
	}
	if err := Validate(unversioned); err == nil {
		t.Fatal("Validate accepted a manifest with no version")
	}

	// Every authority rule still applies without a version.
	for _, c := range []struct {
		name    string
		mutate  func(*Manifest)
		wantErr string
	}{
		{"substrate namespace", func(m *Manifest) {
			m.Permissions.Subscribe = []bus.Pattern{"$SYS.>"}
		}, "reserved token"},
		{"own namespace listed", func(m *Manifest) {
			m.Permissions.Publish = []bus.Pattern{"event.tmux-manager.>"}
		}, "implicit"},
		{"an ineligible family", func(m *Manifest) {
			m.Permissions.Request = []bus.Pattern{"cmd.auth.v1.mint"}
		}, "permanently ineligible"},
		{"an unbounded stream", func(m *Manifest) { m.Streams[0].MaxBytes = 0 }, "maxBytes is required"},
		{"an unknown capability", func(m *Manifest) { m.Needs = []string{"postgres"} }, "unknown capability"},
		{"a plugin with no id", func(m *Manifest) { m.ID = "" }, "plugin id required"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := fullManifest()
			m.Version = ""
			c.mutate(&m)
			err := ValidateDiscover(m)
			if err == nil {
				t.Fatalf("ValidateDiscover accepted %s; the version is the only rule it lifts", c.name)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error %q does not carry %q", err, c.wantErr)
			}
		})
	}
}
