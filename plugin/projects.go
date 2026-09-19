package plugin

// DirectoryContextActionEligibilityCommand asks the contribution owner whether
// one action is available for one host-resolved directory. The owner must not
// reinterpret client-supplied paths: the host supplies the context after it has
// resolved the active project, checkout, worktree and directory.
const DirectoryContextActionEligibilityCommand = "projects.directory-context-action.eligibility.v1"

// ProjectViewContribution adds one owner-local panel to the Projects surface.
// ID is stable within the owning plugin. The host keys the mounted contribution
// by owner, generation and ID, and removes it when that generation is withdrawn.
type ProjectViewContribution struct {
	ID      string `json:"id" bd:"public"`
	Label   string `json:"label" bd:"public"`
	Icon    string `json:"icon" bd:"public"`
	Order   int    `json:"order,omitempty" bd:"public"`
	PanelID string `json:"panelId" bd:"public"`
}

// DirectoryContextActionContribution adds an action to the Projects file-tree
// directory menu, including the tree root/background menu. WizardPanelID must
// name a panel declared by the same manifest. Before displaying or invoking the
// action, the host asks its owner with DirectoryContextActionEligibilityCommand.
type DirectoryContextActionContribution struct {
	ID            string `json:"id" bd:"public"`
	Label         string `json:"label" bd:"public"`
	Icon          string `json:"icon,omitempty" bd:"public"`
	Order         int    `json:"order,omitempty" bd:"public"`
	WizardPanelID string `json:"wizardPanelId" bd:"public"`
}

// ProjectDirectoryContext is resolved by the host for the current principal.
// Paths are host paths and may be used as agent working context; their presence
// does not grant filesystem authority or weaken the host's existing checks.
type ProjectDirectoryContext struct {
	ProjectID     string `json:"projectId" bd:"subject"`
	CheckoutID    string `json:"checkoutId" bd:"subject"`
	WorktreeID    string `json:"worktreeId" bd:"subject"`
	ProjectRoot   string `json:"projectRoot" bd:"subject"`
	WorktreeRoot  string `json:"worktreeRoot" bd:"subject"`
	DirectoryPath string `json:"directoryPath" bd:"subject"`
}

// DirectoryContextActionEligibilityRequest identifies the owner-local action
// and the exact host-resolved target being considered.
type DirectoryContextActionEligibilityRequest struct {
	ActionID string                  `json:"actionId" bd:"public"`
	Context  ProjectDirectoryContext `json:"context" bd:"subject"`
}

// DirectoryContextActionEligibilityResult is authoritative only for the
// configuration revision and filesystem observation used by its owner. Hosts
// must ask again on wizard submission before performing the action.
type DirectoryContextActionEligibilityResult struct {
	Eligible bool   `json:"eligible" bd:"public"`
	Reason   string `json:"reason,omitempty" bd:"public"`
}

// DirectoryContextActionWizardContext is supplied to the owner-local wizard
// panel. The contribution identity prevents one plugin from opening another
// plugin's panel with forged context.
type DirectoryContextActionWizardContext struct {
	ActionID string                  `json:"actionId" bd:"public"`
	Context  ProjectDirectoryContext `json:"context" bd:"subject"`
}
