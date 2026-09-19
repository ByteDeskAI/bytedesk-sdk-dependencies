package plugin

// DirectoryContextActionEligibilityCommand asks the contribution owner whether
// one action is available for one host-resolved directory.
const DirectoryContextActionEligibilityCommand = "projects.directory-context-action.eligibility.v1"

// ProjectViewContribution adds one owner-local panel to the Projects surface.
// The host scopes ID to the owner generation and removes it on withdrawal.
type ProjectViewContribution struct {
	ID      string `json:"id" bd:"public"`
	Label   string `json:"label" bd:"public"`
	Icon    string `json:"icon" bd:"public"`
	Order   int    `json:"order,omitempty" bd:"public"`
	PanelID string `json:"panelId" bd:"public"`
}

// DirectoryContextActionContribution adds an action to a Projects file-tree
// directory menu. WizardPanelID names a panel in the same manifest.
type DirectoryContextActionContribution struct {
	ID            string `json:"id" bd:"public"`
	Label         string `json:"label" bd:"public"`
	Icon          string `json:"icon,omitempty" bd:"public"`
	Order         int    `json:"order,omitempty" bd:"public"`
	WizardPanelID string `json:"wizardPanelId" bd:"public"`
}

// ProjectDirectoryContext is resolved by the host for the current principal.
// These paths are working context and do not grant filesystem authority.
type ProjectDirectoryContext struct {
	ProjectID     string `json:"projectId" bd:"subject"`
	CheckoutID    string `json:"checkoutId" bd:"subject"`
	WorktreeID    string `json:"worktreeId" bd:"subject"`
	ProjectRoot   string `json:"projectRoot" bd:"subject"`
	WorktreeRoot  string `json:"worktreeRoot" bd:"subject"`
	DirectoryPath string `json:"directoryPath" bd:"subject"`
}

type DirectoryContextActionEligibilityRequest struct {
	ActionID string                  `json:"actionId" bd:"public"`
	Context  ProjectDirectoryContext `json:"context" bd:"subject"`
}

type DirectoryContextActionEligibilityResult struct {
	Eligible bool   `json:"eligible" bd:"public"`
	Reason   string `json:"reason,omitempty" bd:"public"`
}

// DirectoryContextActionWizardContext is the immutable target supplied to the
// owner-local wizard. Hosts recheck eligibility when the wizard is submitted.
type DirectoryContextActionWizardContext struct {
	ActionID string                  `json:"actionId" bd:"public"`
	Context  ProjectDirectoryContext `json:"context" bd:"subject"`
}
