package codingsessions

import (
	"fmt"
	"math/big"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/aidecision"
	check "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/internal/contractcheck"
)

const (
	CommandPreview       = "cmd.gateway.coding-sessions.v1.preview"
	CommandPreviewRead   = "cmd.gateway.coding-sessions.v1.preview-read"
	CommandPreviewCancel = "cmd.gateway.coding-sessions.v1.preview-cancel"
)

// Preview uses the host's real routing/eligibility path and ten-second deadline.
// It MUST NOT create a coding session, native process or worktree, consume next
// task overrides, or edit the project. It may incur decision-provider usage.
type PreviewRequest struct {
	ProjectID      string      `json:"projectId" bd:"subject"`
	CheckoutRef    string      `json:"checkoutRef" bd:"subject"`
	Preferences    Preferences `json:"preferences" bd:"subject"`
	Content        TextInput   `json:"content" bd:"subject"`
	IdempotencyKey string      `json:"idempotencyKey" bd:"subject"`
}
type PreviewResult struct {
	Status       string   `json:"status" bd:"public"`
	Route        *Route   `json:"route,omitempty" bd:"subject"`
	Reason       string   `json:"reason" bd:"subject"`
	Confidence   string   `json:"confidence,omitempty" bd:"subject"`
	CostKnown    bool     `json:"costKnown" bd:"public"`
	LatencyKnown bool     `json:"latencyKnown" bd:"public"`
	Warnings     []string `json:"warnings" bd:"subject"`
}
type PreviewJob struct {
	ID         string         `json:"id" bd:"subject"`
	State      string         `json:"state" bd:"public"`
	DeadlineAt string         `json:"deadlineAt" bd:"subject"`
	Result     *PreviewResult `json:"result,omitempty" bd:"subject"`
	Failure    *Failure       `json:"failure,omitempty" bd:"subject"`
}
type PreviewJobResult struct {
	Job PreviewJob `json:"job" bd:"subject"`
}
type PreviewJobRequest struct {
	JobID string `json:"jobId" bd:"subject"`
}

func (v PreviewRequest) Validate() error {
	return check.All(check.ID("projectId", v.ProjectID), check.Text("checkoutRef", v.CheckoutRef, 512, true), v.Preferences.Validate(), v.Content.Validate(), check.ID("idempotencyKey", v.IdempotencyKey), check.Size(v))
}
func (v PreviewResult) Validate() error {
	if err := check.All(check.Enum("preview status", v.Status, "selected", "pending"), check.Text("preview reason", v.Reason, 4096, true), check.Count("preview warnings", len(v.Warnings), 0, 64)); err != nil {
		return err
	}
	if (v.Status == "selected") != (v.Route != nil) {
		return fmt.Errorf("selected preview requires route; pending forbids route")
	}
	if v.Route != nil {
		if err := v.Route.Validate(); err != nil {
			return err
		}
	}
	if v.Confidence != "" {
		n, err := aidecision.ParseDecimal(v.Confidence)
		if err != nil || n.Cmp(big.NewRat(1, 1)) > 0 {
			return fmt.Errorf("preview confidence must be between zero and one")
		}
	}
	for _, w := range v.Warnings {
		if err := check.Text("preview warning", w, 1024, true); err != nil {
			return err
		}
	}
	return check.Size(v)
}
func (v PreviewJob) Validate() error {
	if err := check.All(check.ID("job.id", v.ID), check.Enum("preview job state", v.State, "queued", "running", "completed", "failed", "cancelled", "expired"), check.Timestamp("job.deadlineAt", v.DeadlineAt)); err != nil {
		return err
	}
	if (v.State == "completed") != (v.Result != nil) || (v.State == "failed") != (v.Failure != nil) {
		return fmt.Errorf("preview job state/result mismatch")
	}
	if v.Result != nil {
		if err := v.Result.Validate(); err != nil {
			return err
		}
	}
	if v.Failure != nil {
		if err := v.Failure.Validate(); err != nil {
			return err
		}
	}
	return check.Size(v)
}
func (v PreviewJobResult) Validate() error  { return v.Job.Validate() }
func (v PreviewJobRequest) Validate() error { return check.ID("jobId", v.JobID) }
