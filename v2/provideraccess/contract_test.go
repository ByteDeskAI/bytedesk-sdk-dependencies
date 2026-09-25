package provideraccess

import "testing"

func TestNamedEgressAndExplicitCancellationContract(t *testing.T) {
	v := EgressRequest{InvocationID: "invocation", CredentialID: "opaque", Operation: OperationEvaluate, PayloadID: "payload", IdempotencyKey: "key", TimeoutSeconds: 30}
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	v.Operation = "https://attacker.example"
	if v.Validate() == nil {
		t.Fatal("URL accepted")
	}
	v.Operation = OperationModels
	if v.Validate() == nil {
		t.Fatal("models body accepted")
	}
	v.PayloadID = ""
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	v.TimeoutSeconds = 31
	if v.Validate() == nil {
		t.Fatal("long deadline accepted")
	}
	j := EgressJob{ID: "job", State: "cancelled", DeadlineAt: "2026-09-25T00:00:30Z"}
	if err := j.Validate(); err != nil {
		t.Fatal(err)
	}
	j.Result = &EgressResult{StatusCode: 200, PayloadID: "result"}
	if j.Validate() == nil {
		t.Fatal("cancelled job exposed payload")
	}
	j.State = "completed"
	if err := j.Validate(); err != nil {
		t.Fatal(err)
	}
}
