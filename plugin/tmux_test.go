package plugin

import (
	"reflect"
	"strings"
	"testing"
)

func TestTmuxContractIdentity(t *testing.T) {
	if TmuxService != "tmux" || TmuxContractRevision != 1 {
		t.Fatalf("service identity = %q revision %d", TmuxService, TmuxContractRevision)
	}
	want := map[string]string{
		"availability": "cmd.tmux.v1.availability",
		"sessions":     "cmd.tmux.v1.sessions",
		"windows":      "cmd.tmux.v1.windows",
		"panes":        "cmd.tmux.v1.panes",
	}
	got := map[string]string{
		"availability": TmuxAvailabilityCommand,
		"sessions":     TmuxSessionsCommand,
		"windows":      TmuxWindowsCommand,
		"panes":        TmuxPanesCommand,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
	formats := map[string]string{
		"sessions": "#{session_name}\t#{session_attached}\t#{session_created}\t#{session_activity}",
		"windows":  "#{session_name}\t#{window_index}\t#{window_name}\t#{window_active}\t#{window_layout}",
		"panes":    "#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_current_command}\t#{pane_current_path}\t#{pane_pid}\t#{pane_active}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_title}",
	}
	gotFormats := map[string]string{
		"sessions": TmuxSessionsFormat,
		"windows":  TmuxWindowsFormat,
		"panes":    TmuxPanesFormat,
	}
	if !reflect.DeepEqual(gotFormats, formats) {
		t.Fatalf("fixed formats = %#v, want %#v", gotFormats, formats)
	}
}

func TestTmuxAvailabilityValidation(t *testing.T) {
	valid := []TmuxAvailabilityResult{
		{Tmux: TmuxAvailability{State: TmuxStateAbsent, Message: "tmux is not installed"}},
		{Tmux: TmuxAvailability{State: TmuxStateNoServer, Version: "tmux 3.4", Message: "no tmux server is running"}},
		{Tmux: TmuxAvailability{State: TmuxStateOK, Version: "tmux 3.4", Message: "tmux is available"}},
	}
	for _, result := range valid {
		if err := result.Validate(); err != nil {
			t.Errorf("valid %s result: %v", result.Tmux.State, err)
		}
	}

	invalid := []TmuxAvailabilityResult{
		{},
		{Tmux: TmuxAvailability{State: "unknown", Message: "unknown"}},
		{Tmux: TmuxAvailability{State: TmuxStateAbsent, Version: "tmux 3.4", Message: "not installed"}},
		{Tmux: TmuxAvailability{State: TmuxStateOK}},
		{Tmux: TmuxAvailability{State: TmuxStateOK, Version: strings.Repeat("v", 257), Message: "available"}},
		{Tmux: TmuxAvailability{State: TmuxStateOK, Message: strings.Repeat("m", 513)}},
		{Tmux: TmuxAvailability{State: TmuxStateOK, Message: "bad\x00message"}},
	}
	for i, result := range invalid {
		if err := result.Validate(); err == nil {
			t.Errorf("invalid result %d accepted: %#v", i, result)
		}
	}
}

func TestTmuxReadOutputValidation(t *testing.T) {
	valid := "work\t1\t1700000000\t1700000001\n"
	for name, validate := range map[string]func() error{
		"sessions": func() error { return (TmuxSessionsResult{Output: valid}).Validate() },
		"windows":  func() error { return (TmuxWindowsResult{Output: valid}).Validate() },
		"panes":    func() error { return (TmuxPanesResult{Output: valid}).Validate() },
		"empty":    func() error { return (TmuxSessionsResult{}).Validate() },
	} {
		t.Run(name, func(t *testing.T) {
			if err := validate(); err != nil {
				t.Fatalf("valid fixed-format output: %v", err)
			}
		})
	}

	for name, validate := range map[string]func() error{
		"sessions NUL": func() error { return (TmuxSessionsResult{Output: "bad\x00row"}).Validate() },
		"windows NUL":  func() error { return (TmuxWindowsResult{Output: "bad\x00row"}).Validate() },
		"panes NUL":    func() error { return (TmuxPanesResult{Output: "bad\x00row"}).Validate() },
		"oversize":     func() error { return (TmuxPanesResult{Output: strings.Repeat("x", TmuxReadMaxBytes)}).Validate() },
		"escaped size": func() error { return (TmuxPanesResult{Output: strings.Repeat("\n", TmuxReadMaxBytes/2)}).Validate() },
	} {
		t.Run(name, func(t *testing.T) {
			if err := validate(); err == nil {
				t.Fatal("invalid output accepted")
			}
		})
	}
}

func TestTmuxPayloadClassification(t *testing.T) {
	availability := reflect.TypeOf(TmuxAvailability{})
	for _, name := range []string{"State", "Version", "Message"} {
		field, ok := availability.FieldByName(name)
		if !ok || field.Tag.Get("bd") != "public" {
			t.Fatalf("availability %s classification = %+v", name, field)
		}
	}
	field, ok := reflect.TypeOf(TmuxAvailabilityResult{}).FieldByName("Tmux")
	if !ok || field.Tag.Get("bd") != "public" {
		t.Fatalf("availability result classification = %+v", field)
	}
	for _, result := range []any{TmuxSessionsResult{}, TmuxWindowsResult{}, TmuxPanesResult{}} {
		field, ok := reflect.TypeOf(result).FieldByName("Output")
		if !ok || field.Tag.Get("bd") != "subject" {
			t.Fatalf("%T output classification = %+v", result, field)
		}
	}
}

func TestTmuxRequestsAreEmptyAndValid(t *testing.T) {
	for _, request := range []interface{ Validate() error }{
		TmuxAvailabilityRequest{}, TmuxSessionsRequest{}, TmuxWindowsRequest{}, TmuxPanesRequest{},
	} {
		if reflect.TypeOf(request).NumField() != 0 {
			t.Fatalf("%T accepts caller-controlled arguments", request)
		}
		if err := request.Validate(); err != nil {
			t.Fatalf("%T validation: %v", request, err)
		}
	}
}
