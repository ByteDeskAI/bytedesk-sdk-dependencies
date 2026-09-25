package hostsettings

import "testing"

func TestRedactedSettingsContract(t *testing.T) {
	v := ValidateRequest{SectionID: "jev", ValuesJSON: `{"model":"version"}`, ConfiguredSecrets: []string{"api-key"}}
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	v.ValuesJSON = `["wrong"]`
	if v.Validate() == nil {
		t.Fatal("nonobject accepted")
	}
	for _, v := range []ValidateResult{{Valid: true, Errors: []FieldError{{Field: "model", Message: "invalid"}}}, {Valid: false}} {
		if v.Validate() == nil {
			t.Fatal("inconsistent validation accepted")
		}
	}
}
