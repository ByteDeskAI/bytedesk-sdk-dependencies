package aidecision

import (
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
	"testing"
)

func TestOnlyExplicitDecisionDeclarationsQualify(t *testing.T) {
	for _, p := range []bus.Pattern{"cmd.>", "cmd.gateway.>", "cmd.gateway.*.v1.>", "svc.jev.>"} {
		if len(DeclaredCommands([]bus.Pattern{p})) != 0 {
			t.Fatalf("broad permission qualified %q", p)
		}
	}
	if len(DeclaredCommands([]bus.Pattern{"cmd.gateway.ai-decision.v1.>"})) != 4 {
		t.Fatal("versioned family not expanded")
	}
	if len(DeclaredCommands([]bus.Pattern{CommandStart, CommandStart})) != 1 {
		t.Fatal("duplicate or exact command handling")
	}
}
