package plugin

import (
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
	"testing"
)

func TestGatewayFacadeCanBeRequestedButNeverImplemented(t *testing.T) {
	for _, verb := range []string{"request", "subscribe", "publish"} {
		t.Run(verb, func(t *testing.T) {
			m := Manifest{ID: "consumer"}
			switch verb {
			case "request":
				m.Permissions.Request = []bus.Pattern{"cmd.gateway.ai-decision.v1.start"}
			case "subscribe":
				m.Permissions.Subscribe = []bus.Pattern{"cmd.gateway.>"}
			case "publish":
				m.Permissions.Publish = []bus.Pattern{"cmd.gateway.*.>"}
			}
			var c collector
			validateSubjectPatterns(&c, m)
			if (len(c.list) > 0) != (verb != "request") {
				t.Fatalf("%s: %#v", verb, c)
			}
		})
	}
	var c collector
	validateSubjectPatterns(&c, Manifest{ID: "gateway"})
	if len(c.list) == 0 {
		t.Fatal("gateway owner accepted")
	}
}

func TestDecisionProvidersHaveHostOnlyIngress(t *testing.T) {
	for _, verb := range []string{"request", "publish", "subscribe"} {
		for _, pattern := range []bus.Pattern{"svc.*.ai.decision.>", "svc.jev.ai.decision.v1.start", "svc.>"} {
			m := Manifest{ID: "consumer"}
			switch verb {
			case "request":
				m.Permissions.Request = []bus.Pattern{pattern}
			case "publish":
				m.Permissions.Publish = []bus.Pattern{pattern}
			case "subscribe":
				m.Permissions.Subscribe = []bus.Pattern{pattern}
			}
			var c collector
			validateSubjectPatterns(&c, m)
			if len(c.list) == 0 {
				t.Fatalf("%s %s admitted", verb, pattern)
			}
		}
	}
	// Own provider endpoints need no declared subscription: own service namespace
	// is implicit. Do not reject the provider simply for implementing the point.
	var c collector
	validateSubjectPatterns(&c, Manifest{ID: "jev"})
	if len(c.list) != 0 {
		t.Fatalf("provider own namespace refused: %+v", c.list)
	}
}

func TestProjectProvidersHaveHostOnlyIngress(t *testing.T) {
	for _, point := range []Point{PointProjectTasks, PointProjectKnowledge} {
		if !IsKnownPoint(string(point)) {
			t.Fatalf("missing typed point %s", point)
		}
		for _, verb := range []string{"request", "publish", "subscribe"} {
			for _, pattern := range []bus.Pattern{bus.Pattern("svc.*." + string(point) + ".>"), bus.Pattern("svc.example." + string(point) + ".v1.write")} {
				m := Manifest{ID: "consumer"}
				switch verb {
				case "request":
					m.Permissions.Request = []bus.Pattern{pattern}
				case "publish":
					m.Permissions.Publish = []bus.Pattern{pattern}
				case "subscribe":
					m.Permissions.Subscribe = []bus.Pattern{pattern}
				}
				var c collector
				validateSubjectPatterns(&c, m)
				if len(c.list) == 0 {
					t.Fatalf("admitted peer %s %s", verb, pattern)
				}
			}
		}
		// Own service subscriptions are implicit and must not be repeated in
		// permissions. Merely implementing this point adds no peer authority.
		m := Manifest{ID: "example"}
		var c collector
		validateSubjectPatterns(&c, m)
		if len(c.list) != 0 {
			t.Fatalf("rejected own provider subscription: %+v", c.list)
		}
	}
	for _, command := range []bus.Pattern{"cmd.gateway.project-tasks.v1.>", "cmd.gateway.project-knowledge.v1.>"} {
		var c collector
		validateSubjectPatterns(&c, Manifest{ID: "container", Permissions: Permissions{Request: []bus.Pattern{command}}})
		if len(c.list) != 0 {
			t.Fatalf("rejected host facade %s: %+v", command, c.list)
		}
	}
}
