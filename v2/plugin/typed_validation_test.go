package plugin

import (
	"context"
	"fmt"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
	"testing"
)

type checkedValue struct {
	Name string `json:"name"`
}

func (v checkedValue) Validate() error {
	if v.Name == "" {
		return fmt.Errorf("name required")
	}
	return nil
}
func TestValidatedWireBoundaryRefusesBeforeHandlerAndReply(t *testing.T) {
	cmd := NewValidatedCommand[checkedValue, checkedValue]("svc.demo.ai.decision.v1.start", 1, pingHash, "svc.demo.ai.decision.v1.start")
	svc := &captureServices{}
	b := &serviceBus{services: svc}
	calls := 0
	if _, err := ServeAtPoint(context.Background(), b, cmd, PointAIDecision, func(_ context.Context, req checkedValue, _ bus.Caller) (checkedValue, error) {
		calls++
		return checkedValue{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if svc.spec.Endpoints[0].Point != string(PointAIDecision) {
		t.Fatal("point not discoverable")
	}
	for _, raw := range []string{`{}`, `{"name":"a","name":"b"}`, `{"Name":"a"}`, `{"name":null}`, `{"name":"a","unknown":1}`, `null`} {
		var reply *bus.Msg
		svc.spec.Endpoints[0].Handler(context.Background(), bus.NewMsg(cmd.Descriptor().Subject(), "_INBOX.1", bus.Headers{bus.HeaderSchema: pingHash}, []byte(raw), func(h bus.Headers, data []byte) error { reply = &bus.Msg{Headers: h, Data: data}; return nil }))
		if calls != 0 || reply == nil || reply.Headers.Get(bus.HeaderFault) != bus.FaultSchema {
			t.Fatalf("invalid request reached handler: %s", raw)
		}
	}
	var reply *bus.Msg
	svc.spec.Endpoints[0].Handler(context.Background(), bus.NewMsg(cmd.Descriptor().Subject(), "_INBOX.1", bus.Headers{bus.HeaderSchema: pingHash}, []byte(`{"name":"valid"}`), func(h bus.Headers, data []byte) error { reply = &bus.Msg{Headers: h, Data: data}; return nil }))
	if calls != 1 || reply.Headers.Get(bus.HeaderFault) != bus.FaultSchema {
		t.Fatal("invalid response published")
	}
	client := newTestBus()
	client.reply = &bus.Msg{Headers: bus.Headers{bus.HeaderSchema: pingHash}, Data: []byte(`{}`)}
	if _, err := Call(context.Background(), client, cmd, checkedValue{Name: "valid"}); err == nil {
		t.Fatal("invalid response accepted")
	}
	client.reply = &bus.Msg{Headers: bus.Headers{bus.HeaderSchema: pingHash}, Data: []byte(`{"name":"valid"}`)}
	if _, err := Call(context.Background(), client, cmd, checkedValue{}); err == nil {
		t.Fatal("invalid request accepted")
	}
	if _, err := Call(context.Background(), client, cmd, checkedValue{Name: "valid"}); err != nil {
		t.Fatal(err)
	}
}
