package codingsessions

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The portable browser contract represents an optional workUnit by omission,
// not explicit null. Restrict only this new field; other optional fields retain
// their existing behavior. Alias decoding avoids recursive UnmarshalJSON calls.
// DecodeValidated still checks exact keys and duplicates against the original
// DTO before invoking these methods. DisallowUnknownFields remains effective
// for callers using a regular JSON decoder as well.
func decodeWorkUnitJSON(data []byte, into any) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if raw, present := fields["workUnit"]; present && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("workUnit must be omitted or an object, not null")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(into)
}

func (v *CreateRequest) UnmarshalJSON(data []byte) error {
	type wire CreateRequest
	var next wire
	if err := decodeWorkUnitJSON(data, &next); err != nil {
		return err
	}
	*v = CreateRequest(next)
	return nil
}
func (v *NewTaskRequest) UnmarshalJSON(data []byte) error {
	type wire NewTaskRequest
	var next wire
	if err := decodeWorkUnitJSON(data, &next); err != nil {
		return err
	}
	*v = NewTaskRequest(next)
	return nil
}
func (v *Session) UnmarshalJSON(data []byte) error {
	type wire Session
	var next wire
	if err := decodeWorkUnitJSON(data, &next); err != nil {
		return err
	}
	*v = Session(next)
	return nil
}
