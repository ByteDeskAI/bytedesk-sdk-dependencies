package projectmanagement

import (
	"fmt"
	"reflect"
	"strings"
)

// ActionFields rejects ambiguous action unions. Names are JSON field names;
// action, scope, mutation, and resource are the common envelope. Providers must
// additionally validate current field schemas and actor authorization.
func ActionFields(value any, fields string) error {
	allowed := map[string]bool{"scope": true, "mutation": true, "action": true, "resource": true}
	for _, name := range strings.Fields(fields) {
		allowed[name] = true
	}
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("action must be a struct")
	}
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		name := strings.Split(v.Type().Field(i).Tag.Get("json"), ",")[0]
		if allowed[name] || f.IsZero() || (f.Kind() == reflect.Slice && f.Len() == 0) {
			continue
		}
		return fmt.Errorf("field %s is not valid for this action", name)
	}
	return nil
}
