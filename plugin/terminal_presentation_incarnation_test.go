package plugin

import "testing"

func TestSameTerminalIncarnationComparesTmuxByValue(t *testing.T) {
	tmux := func(panePID string) *TmuxPresentationContext {
		return &TmuxPresentationContext{RepositoryKey: "0123456789abcdef", ServerKey: "/tmp/tmux-1000/default", ServerPID: "10", SessionID: "1", SessionCreated: "1700000000", PaneID: "2", PanePID: panePID}
	}
	bound := func(ctx *TmuxPresentationContext) PresentationTerminal {
		return PresentationTerminal{TerminalID: "t1", Context: TerminalBindingContext{Kind: TerminalBindingTmux, Tmux: ctx}}
	}
	if !sameTerminalIncarnation(bound(tmux("42")), bound(tmux("42"))) {
		t.Fatal("equal tmux bindings behind different pointers must be the same incarnation")
	}
	if sameTerminalIncarnation(bound(tmux("42")), bound(tmux("43"))) {
		t.Fatal("a changed pane pid is a different incarnation")
	}
	if sameTerminalIncarnation(bound(tmux("42")), bound(nil)) {
		t.Fatal("a tmux binding and a missing one are different incarnations")
	}
	none := PresentationTerminal{TerminalID: "t1", Context: TerminalBindingContext{Kind: TerminalBindingNone}}
	if !sameTerminalIncarnation(none, none) {
		t.Fatal("identical unbound terminals are the same incarnation")
	}
}
