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
