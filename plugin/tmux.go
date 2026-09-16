package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	TmuxService          = "tmux"
	TmuxContractRevision = 1

	TmuxAvailabilityCommand = "cmd.tmux.v1.availability"
	TmuxSessionsCommand     = "cmd.tmux.v1.sessions"
	TmuxWindowsCommand      = "cmd.tmux.v1.windows"
	TmuxPanesCommand        = "cmd.tmux.v1.panes"

	// TmuxReadMaxBytes leaves room for the command envelope within the host's
	// 64 KiB transport limit. Validation applies after JSON escaping.
	TmuxReadMaxBytes = 48 << 10

	TmuxStateAbsent   = "absent"
	TmuxStateNoServer = "no-server"
	TmuxStateOK       = "ok"

	// These formats are the complete read surface. Requests intentionally carry
	// no argv, target, format, or mutation fields.
	TmuxSessionsFormat = "#{session_name}\t#{session_attached}\t#{session_created}\t#{session_activity}"
	TmuxWindowsFormat  = "#{session_name}\t#{window_index}\t#{window_name}\t#{window_active}\t#{window_layout}"
	TmuxPanesFormat    = "#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_current_command}\t#{pane_current_path}\t#{pane_pid}\t#{pane_active}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_title}"
)

// TmuxAvailability is public host capability metadata. It contains no tmux
// inventory or operator data.
type TmuxAvailability struct {
	State   string `json:"state" bd:"public"`
	Version string `json:"version,omitempty" bd:"public"`
	Message string `json:"message" bd:"public"`
}

type TmuxAvailabilityRequest struct{}
type TmuxAvailabilityResult struct {
	Tmux TmuxAvailability `json:"tmux" bd:"public"`
}

// Inventory requests are deliberately empty. A host executes only the named,
// read-only operation with its fixed format and returns the resulting rows.
type TmuxSessionsRequest struct{}
type TmuxSessionsResult struct {
	Output string `json:"output" bd:"subject"`
}

type TmuxWindowsRequest struct{}
type TmuxWindowsResult struct {
	Output string `json:"output" bd:"subject"`
}

type TmuxPanesRequest struct{}
type TmuxPanesResult struct {
	Output string `json:"output" bd:"subject"`
}

func (TmuxAvailabilityRequest) Validate() error { return nil }
func (TmuxSessionsRequest) Validate() error     { return nil }
func (TmuxWindowsRequest) Validate() error      { return nil }
func (TmuxPanesRequest) Validate() error        { return nil }

func (v TmuxAvailability) Validate() error {
	switch v.State {
	case TmuxStateAbsent:
		if v.Version != "" {
			return fmt.Errorf("absent tmux cannot report a version")
		}
	case TmuxStateNoServer, TmuxStateOK:
	default:
		return fmt.Errorf("invalid tmux state %q", v.State)
	}
	if err := tmuxText("tmux.version", v.Version, 256, false); err != nil {
		return err
	}
	return tmuxText("tmux.message", v.Message, 512, true)
}

func (v TmuxAvailabilityResult) Validate() error { return v.Tmux.Validate() }
func (v TmuxSessionsResult) Validate() error     { return validateTmuxOutput(v) }
func (v TmuxWindowsResult) Validate() error      { return validateTmuxOutput(v) }
func (v TmuxPanesResult) Validate() error        { return validateTmuxOutput(v) }

func validateTmuxOutput(v any) error {
	var output string
	switch result := v.(type) {
	case TmuxSessionsResult:
		output = result.Output
	case TmuxWindowsResult:
		output = result.Output
	case TmuxPanesResult:
		output = result.Output
	default:
		return fmt.Errorf("unsupported tmux result %T", v)
	}
	if strings.ContainsRune(output, '\x00') {
		return fmt.Errorf("tmux output contains NUL")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode tmux result: %w", err)
	}
	if len(raw) > TmuxReadMaxBytes {
		return fmt.Errorf("tmux result exceeds %d bytes", TmuxReadMaxBytes)
	}
	return nil
}

func tmuxText(name, value string, limit int, required bool) error {
	if required && value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(value) > limit {
		return fmt.Errorf("%s exceeds %d bytes", name, limit)
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("%s contains a control character", name)
	}
	return nil
}
