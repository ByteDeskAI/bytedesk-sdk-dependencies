package attest

import (
	"encoding/json"
	"io"
)

// decodeJSON refuses trailing content rather than ignoring it: a roots
// document with a second object appended is not a roots document, and reading
// only the first would be the quiet kind of wrong.
func decodeJSON(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return io.ErrUnexpectedEOF
	}
	return nil
}
