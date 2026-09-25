// Package contractcheck contains validation shared by the public SDK contracts.
// It validates wire values only; authorization and resource ownership remain host checks.
package contractcheck

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxWireBytes = 64 << 10

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)

func ID(name, value string) error {
	if !idPattern.MatchString(value) {
		return fmt.Errorf("%s must be an opaque identifier", name)
	}
	return nil
}

func OptionalID(name, value string) error {
	if value == "" {
		return nil
	}
	return ID(name, value)
}

func Text(name, value string, max int, required bool) error {
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || len(value) > max || required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must be valid text of at most %d bytes", name, max)
	}
	return nil
}

func Enum(name, value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("unknown %s %q", name, value)
}

func Size(value any) error {
	return SizeLimit(value, MaxWireBytes)
}

func SizeLimit(value any, max int) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(b) > max {
		return fmt.Errorf("encoded payload exceeds %d bytes", max)
	}
	return nil
}

func Count(name string, size, min, max int) error {
	if size < min || size > max {
		return fmt.Errorf("%s must contain %d to %d items", name, min, max)
	}
	return nil
}

func Unique(name string, values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return fmt.Errorf("duplicate %s %q", name, value)
		}
		seen[value] = true
	}
	return nil
}

func Timestamp(name, value string) error {
	v, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || v.IsZero() {
		return fmt.Errorf("%s must be RFC3339", name)
	}
	return nil
}

func All(checks ...error) error {
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	return nil
}
