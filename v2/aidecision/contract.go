// Package aidecision defines typed decision primitives, not text generation or
// coding-agent execution. The host resolves the selected ai.decision provider,
// authorizes the consumer, and binds every referenced payload before dispatch.
// Enabled declaring consumers may run without a human subject lease. Provider
// credentials remain host-owned and are never returned through this contract.
package aidecision

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	check "github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/internal/contractcheck"
	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/payloads"
)

const (
	ContractRevision         = 1
	CommandStart             = "cmd.gateway.ai-decision.v1.start"
	CommandRead              = "cmd.gateway.ai-decision.v1.read"
	CommandCancel            = "cmd.gateway.ai-decision.v1.cancel"
	CommandModels            = "cmd.gateway.ai-decision.v1.models"
	KindChoice               = "choice"
	KindScore                = "score"
	KindNoul                 = "noul"
	FormatText               = "text"
	FormatJSON               = "json"
	MaxQuestions             = 100
	MaxOptions               = 255
	MaxScoreLevels           = 10
	ProbabilityTolerance     = 0.000001
	StateQueued              = "queued"
	StateRunning             = "running"
	StateCompleted           = "completed"
	StateFailed              = "failed"
	StateCancelled           = "cancelled"
	StateExpired             = "expired"
	PurposeEvaluation        = "evaluation"
	PurposeRouting           = "routing"
	EvaluationTimeoutSeconds = 30
	RoutingTimeoutSeconds    = 10
)

type TextInput = payloads.TextInput

// StructuredValue preserves the decision provider's text/object/array input.
// JSON is serialized in a classified string so arbitrary application data does
// not widen the generated SDK type graph. The host validates assembled handle
// content using ValidateContent before sending it to the provider.
type StructuredValue struct {
	Format  string    `json:"format" bd:"public"`
	Content TextInput `json:"content" bd:"subject"`
}
type Option struct {
	ID string `json:"id" bd:"subject"`
	// Nil is the provider's null criterion; the option ID still names a choice.
	Description *StructuredValue `json:"description,omitempty" bd:"subject"`
}
type ChoiceQuestion struct {
	Options []Option `json:"options" bd:"subject"`
}
type ScoreQuestion struct {
	Levels []StructuredValue `json:"levels" bd:"subject"`
}
type NoulCriteria struct {
	True  StructuredValue `json:"true" bd:"subject"`
	False StructuredValue `json:"false" bd:"subject"`
}
type NoulQuestion struct {
	Criteria *NoulCriteria `json:"criteria,omitempty" bd:"subject"`
}
type Question struct {
	ID           string          `json:"id" bd:"subject"`
	Instructions StructuredValue `json:"instructions" bd:"subject"`
	Kind         string          `json:"kind" bd:"public"`
	Choice       *ChoiceQuestion `json:"choice,omitempty" bd:"subject"`
	Score        *ScoreQuestion  `json:"score,omitempty" bd:"subject"`
	Noul         *NoulQuestion   `json:"noul,omitempty" bd:"subject"`
}
type BatchRequest struct {
	Purpose        string          `json:"purpose" bd:"public"`
	ProviderID     string          `json:"providerId" bd:"subject"`
	Model          string          `json:"model" bd:"subject"`
	State          StructuredValue `json:"state" bd:"subject"`
	Questions      []Question      `json:"questions" bd:"subject"`
	IdempotencyKey string          `json:"idempotencyKey" bd:"subject"`
}

// Numeric decision values are decimal strings in the SDK wire contract. The
// provider adapter converts upstream JSON numbers; this preserves the SDK's
// explicit numeric wire discipline without rounding a score to an integer.
type Probability struct {
	ID    string `json:"id" bd:"subject"`
	Value string `json:"value" bd:"subject"`
}
type LegendEntry struct {
	ID    string `json:"id" bd:"subject"`
	Label string `json:"label" bd:"subject"`
}
type ChoiceAnswer struct {
	Choice        string        `json:"choice" bd:"subject"`
	Probabilities []Probability `json:"probabilities" bd:"subject"`
	Confidence    string        `json:"confidence" bd:"subject"`
}
type ScoreAnswer struct {
	Score         string        `json:"score" bd:"subject"`
	Legend        []LegendEntry `json:"legend" bd:"subject"`
	Probabilities []Probability `json:"probabilities" bd:"subject"`
	Confidence    string        `json:"confidence" bd:"subject"`
}
type NoulAnswer struct {
	Noul string `json:"noul" bd:"subject"`
}
type Answer struct {
	ID     string        `json:"id" bd:"subject"`
	Kind   string        `json:"kind" bd:"public"`
	Choice *ChoiceAnswer `json:"choice,omitempty" bd:"subject"`
	Score  *ScoreAnswer  `json:"score,omitempty" bd:"subject"`
	Noul   *NoulAnswer   `json:"noul,omitempty" bd:"subject"`
}
type Usage struct {
	// Pointers distinguish absent telemetry from an actual zero count.
	InputTokens  *uint64 `json:"inputTokens,string" bd:"subject"`
	OutputTokens *uint64 `json:"outputTokens,string" bd:"subject"`
}
type BatchResult struct {
	ProviderID     string   `json:"providerId" bd:"subject"`
	RequestedModel string   `json:"requestedModel" bd:"subject"`
	Model          string   `json:"model" bd:"subject"`
	Answers        []Answer `json:"answers" bd:"subject"`
	Usage          Usage    `json:"usage" bd:"subject"`
}

// ProviderStartRequest is sent only by the authenticated host to the selected
// provider. InvocationID is a host-minted authority bound to the original caller
// and provider generations, job and deadline. It must be forwarded on nested
// payload/egress calls. Public consumers never create or select this authority.
type ProviderStartRequest struct {
	InvocationID string       `json:"invocationId" bd:"subject"`
	Batch        BatchRequest `json:"batch" bd:"subject"`
}
type ProviderReadRequest struct {
	InvocationID string `json:"invocationId" bd:"subject"`
	JobID        string `json:"jobId" bd:"subject"`
}
type ProviderCancelRequest struct {
	InvocationID string `json:"invocationId" bd:"subject"`
	JobID        string `json:"jobId" bd:"subject"`
}
type ProviderModelsRequest struct {
	InvocationID string        `json:"invocationId" bd:"subject"`
	Request      ModelsRequest `json:"request" bd:"subject"`
}

func (v ProviderStartRequest) Validate() error {
	return check.All(check.ID("invocationId", v.InvocationID), v.Batch.Validate(), check.Size(v))
}
func (v ProviderReadRequest) Validate() error {
	return check.All(check.ID("invocationId", v.InvocationID), check.ID("jobId", v.JobID))
}
func (v ProviderCancelRequest) Validate() error {
	return check.All(check.ID("invocationId", v.InvocationID), check.ID("jobId", v.JobID))
}
func (v ProviderModelsRequest) Validate() error {
	return check.All(check.ID("invocationId", v.InvocationID), v.Request.Validate())
}

// Start returns promptly. Work is owned by the host job, not by the short bus
// request context. Read never long-polls; Cancel explicitly requests upstream
// cancellation. A disconnected client can resume polling the same owned job.
type StartResult struct {
	Job Job `json:"job" bd:"subject"`
}
type ReadRequest struct {
	JobID string `json:"jobId" bd:"subject"`
}
type ReadResult struct {
	Job Job `json:"job" bd:"subject"`
}
type CancelRequest struct {
	JobID string `json:"jobId" bd:"subject"`
}
type CancelResult struct {
	Job Job `json:"job" bd:"subject"`
}
type Failure struct {
	Code      string `json:"code" bd:"public"`
	Message   string `json:"message" bd:"subject"`
	Retryable bool   `json:"retryable" bd:"public"`
}
type Job struct {
	ID         string `json:"id" bd:"subject"`
	ProviderID string `json:"providerId" bd:"subject"`
	State      string `json:"state" bd:"public"`
	CreatedAt  string `json:"createdAt" bd:"subject"`
	DeadlineAt string `json:"deadlineAt" bd:"subject"`
	// Completed work has exactly one result source. Large normalized BatchResult
	// JSON uses a committed host payload handle bound to the original consumer.
	Result         *BatchResult `json:"result,omitempty" bd:"subject"`
	ResultHandleID string       `json:"resultHandleId,omitempty" bd:"subject"`
	Failure        *Failure     `json:"failure,omitempty" bd:"subject"`
}
type ModelsRequest struct {
	ProviderID string `json:"providerId" bd:"subject"`
}
type DecisionModel struct {
	ID    string   `json:"id" bd:"subject"`
	Label string   `json:"label" bd:"public"`
	Kinds []string `json:"kinds" bd:"public"`
}
type ModelsResult struct {
	ProviderID string          `json:"providerId" bd:"subject"`
	Models     []DecisionModel `json:"models" bd:"subject"`
	// Models is an immediate cache snapshot. A miss starts a bounded host-owned
	// refresh job; the caller polls Models again, never holds a bus request open.
	Refreshing bool     `json:"refreshing" bd:"public"`
	ObservedAt string   `json:"observedAt,omitempty" bd:"subject"`
	Failure    *Failure `json:"failure,omitempty" bd:"subject"`
}

func (v StructuredValue) Validate() error {
	if err := check.All(check.Enum("structured format", v.Format, FormatText, FormatJSON), v.Content.Validate()); err != nil {
		return err
	}
	if v.Content.Inline != "" {
		return v.ValidateContent([]byte(v.Content.Inline))
	}
	return nil
}
func (v StructuredValue) ValidateContent(content []byte) error {
	if len(content) == 0 || len(content) > payloads.MaxAssembledBytes {
		return fmt.Errorf("structured content exceeds payload bounds")
	}
	if err := check.Text("structured content", string(content), payloads.MaxAssembledBytes, true); err != nil {
		return err
	}
	if v.Format == FormatText {
		return nil
	}
	if v.Format != FormatJSON {
		return fmt.Errorf("unknown structured format")
	}
	trimmed := strings.TrimSpace(string(content))
	if !json.Valid(content) || len(trimmed) == 0 || trimmed[0] != '{' && trimmed[0] != '[' {
		return fmt.Errorf("structured JSON must be an object or array")
	}
	return nil
}
func (v Option) Validate() error {
	if err := check.Text("option.id", v.ID, 255, true); err != nil {
		return err
	}
	if v.Description != nil {
		return v.Description.Validate()
	}
	return nil
}
func (v ChoiceQuestion) Validate() error {
	if err := check.Count("choice options", len(v.Options), 1, MaxOptions); err != nil {
		return err
	}
	ids := make([]string, 0, len(v.Options))
	for _, option := range v.Options {
		if err := option.Validate(); err != nil {
			return err
		}
		ids = append(ids, option.ID)
	}
	return check.Unique("choice option", ids)
}
func (v ScoreQuestion) Validate() error {
	if err := check.Count("score levels", len(v.Levels), 2, MaxScoreLevels); err != nil {
		return err
	}
	for _, level := range v.Levels {
		if err := level.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v NoulCriteria) Validate() error { return check.All(v.True.Validate(), v.False.Validate()) }
func (v NoulQuestion) Validate() error {
	if v.Criteria != nil {
		return v.Criteria.Validate()
	}
	return nil
}
func one(kind string, choice, score, noul bool) error {
	if kind == KindChoice && choice && !score && !noul || kind == KindScore && score && !choice && !noul || kind == KindNoul && noul && !choice && !score {
		return nil
	}
	return fmt.Errorf("decision requires exactly its %q payload", kind)
}
func (v Question) Validate() error {
	if err := check.All(check.ID("question.id", v.ID), v.Instructions.Validate(), one(v.Kind, v.Choice != nil, v.Score != nil, v.Noul != nil)); err != nil {
		return err
	}
	if v.Choice != nil {
		return v.Choice.Validate()
	}
	if v.Score != nil {
		return v.Score.Validate()
	}
	return v.Noul.Validate()
}
func (v BatchRequest) Validate() error {
	if err := check.Enum("purpose", v.Purpose, PurposeEvaluation, PurposeRouting); err != nil {
		return err
	}
	if err := check.All(check.ID("providerId", v.ProviderID), check.Text("model", v.Model, 255, true), v.State.Validate(), check.ID("idempotencyKey", v.IdempotencyKey), check.Count("questions", len(v.Questions), 1, MaxQuestions)); err != nil {
		return err
	}
	ids := make([]string, 0, len(v.Questions))
	for _, q := range v.Questions {
		if err := q.Validate(); err != nil {
			return err
		}
		ids = append(ids, q.ID)
	}
	return check.All(check.Unique("question", ids), check.Size(v))
}

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// ParseDecimal returns the exact rational value; it never rounds through a
// float. JSON NaN/Inf, negative values and unbounded precision are refused.
func ParseDecimal(value string) (*big.Rat, error) {
	if len(value) > 64 || !decimalPattern.MatchString(value) {
		return nil, fmt.Errorf("value must be a nonnegative decimal string of at most 64 bytes")
	}
	n, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, fmt.Errorf("invalid decimal")
	}
	return n, nil
}

// DecimalFromJSONNumber normalizes an upstream JSON number without losing its
// precision. Exponent notation is expanded to the bounded plain decimal wire form.
func DecimalFromJSONNumber(value json.Number) (string, error) {
	raw := string(value)
	if len(raw) > 64 || raw == "" || raw[0] == '-' || !json.Valid([]byte(raw)) {
		return "", fmt.Errorf("invalid nonnegative JSON number")
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == 'e' || r == 'E' })
	exponent := 0
	if len(parts) == 2 {
		var err error
		exponent, err = strconv.Atoi(parts[1])
		if err != nil || exponent < -64 || exponent > 64 {
			return "", fmt.Errorf("decimal exponent exceeds wire precision")
		}
	}
	if len(parts) == 0 || len(parts) > 2 {
		return "", fmt.Errorf("invalid JSON number")
	}
	mantissa := parts[0]
	point := strings.IndexByte(mantissa, '.')
	if point < 0 {
		point = len(mantissa)
	}
	digits := strings.ReplaceAll(mantissa, ".", "")
	point += exponent
	if point <= 0 {
		digits = strings.Repeat("0", 1-point) + digits
		point = 1
	}
	if point >= len(digits) {
		digits += strings.Repeat("0", point-len(digits))
		point = len(digits)
	}
	whole := strings.TrimLeft(digits[:point], "0")
	if whole == "" {
		whole = "0"
	}
	fraction := strings.TrimRight(digits[point:], "0")
	out := whole
	if fraction != "" {
		out += "." + fraction
	}
	if _, err := ParseDecimal(out); err != nil {
		return "", err
	}
	return out, nil
}

// DecimalFromFloat is for callers that already hold an IEEE-754 value. Use
// DecimalFromJSONNumber when reading JSON to preserve precision before decoding.
func DecimalFromFloat(value float64) (string, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return "", fmt.Errorf("decimal must be finite and nonnegative")
	}
	return DecimalFromJSONNumber(json.Number(strconv.FormatFloat(value, 'g', -1, 64)))
}

func number(name, value string, max float64) (*big.Rat, error) {
	n, err := ParseDecimal(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if n.Cmp(new(big.Rat).SetFloat64(max)) > 0 {
		return nil, fmt.Errorf("%s must be between 0 and %g", name, max)
	}
	return n, nil
}
func (v Probability) Validate() error {
	_, err := number("probability", v.Value, 1)
	return check.All(check.Text("probability.id", v.ID, 255, true), err)
}
func probabilities(values []Probability) error {
	if err := check.Count("probabilities", len(values), 1, MaxOptions); err != nil {
		return err
	}
	sum := new(big.Rat)
	ids := make([]string, 0, len(values))
	for _, p := range values {
		if err := p.Validate(); err != nil {
			return err
		}
		n, _ := number("probability", p.Value, 1)
		sum.Add(sum, n)
		ids = append(ids, p.ID)
	}
	difference := new(big.Rat).Sub(sum, big.NewRat(1, 1))
	difference.Abs(difference)
	if difference.Cmp(big.NewRat(1, 1000000)) > 0 {
		return fmt.Errorf("probabilities must sum to one")
	}
	return check.Unique("probability", ids)
}
func (v LegendEntry) Validate() error {
	return check.All(check.Text("legend.id", v.ID, 255, true), check.Text("legend.label", v.Label, 4096, true))
}
func (v ChoiceAnswer) Validate() error {
	_, err := number("confidence", v.Confidence, 1)
	if err = check.All(err, probabilities(v.Probabilities)); err != nil {
		return err
	}
	for _, p := range v.Probabilities {
		if p.ID == v.Choice {
			return nil
		}
	}
	return fmt.Errorf("choice is absent from probabilities")
}
func (v ScoreAnswer) Validate() error {
	_, err := number("score", v.Score, MaxScoreLevels-1)
	_, confidenceErr := number("confidence", v.Confidence, 1)
	if err = check.All(err, confidenceErr, probabilities(v.Probabilities), check.Count("legend", len(v.Legend), 2, MaxScoreLevels)); err != nil {
		return err
	}
	ids := make([]string, 0, len(v.Legend))
	for _, entry := range v.Legend {
		if err := entry.Validate(); err != nil {
			return err
		}
		ids = append(ids, entry.ID)
	}
	return check.Unique("legend", ids)
}
func (v NoulAnswer) Validate() error { _, err := number("noul", v.Noul, 1); return err }
func (v Answer) Validate() error {
	if err := check.All(check.ID("answer.id", v.ID), one(v.Kind, v.Choice != nil, v.Score != nil, v.Noul != nil)); err != nil {
		return err
	}
	if v.Choice != nil {
		return v.Choice.Validate()
	}
	if v.Score != nil {
		return v.Score.Validate()
	}
	return v.Noul.Validate()
}
func (v Usage) Validate() error {
	if v.InputTokens == nil || v.OutputTokens == nil {
		return fmt.Errorf("usage requires explicit input and output counts")
	}
	return nil
}
func (v BatchResult) Validate() error {
	if err := check.Text("requestedModel", v.RequestedModel, 255, true); err != nil {
		return err
	}
	if err := check.All(check.ID("providerId", v.ProviderID), check.Text("model", v.Model, 255, true), v.Usage.Validate(), check.Count("answers", len(v.Answers), 1, MaxQuestions)); err != nil {
		return err
	}
	ids := make([]string, 0, len(v.Answers))
	for _, a := range v.Answers {
		if err := a.Validate(); err != nil {
			return err
		}
		ids = append(ids, a.ID)
	}
	return check.All(check.Unique("answer", ids), check.SizeLimit(v, payloads.MaxAssembledBytes))
}

// ValidateFor checks correspondence, not just shape: every question has exactly
// its answer, choice options are complete, and score levels use their zero-based IDs.
func (v BatchResult) ValidateFor(request BatchRequest) error {
	if err := check.All(request.Validate(), v.Validate()); err != nil {
		return err
	}
	if v.ProviderID != request.ProviderID || v.RequestedModel != request.Model || len(v.Answers) != len(request.Questions) {
		return fmt.Errorf("decision result does not match request")
	}
	for i, q := range request.Questions {
		a := v.Answers[i]
		if a.ID != q.ID || a.Kind != q.Kind {
			return fmt.Errorf("answer order, id or kind does not match question")
		}
		var expected []string
		var actual []Probability
		if q.Choice != nil {
			for _, option := range q.Choice.Options {
				expected = append(expected, option.ID)
			}
			actual = a.Choice.Probabilities
		}
		if q.Score != nil {
			if _, err := number("score", a.Score.Score, float64(len(q.Score.Levels)-1)); err != nil {
				return err
			}
			for n := range q.Score.Levels {
				expected = append(expected, strconv.Itoa(n))
			}
			actual = a.Score.Probabilities
			if len(a.Score.Legend) != len(expected) {
				return fmt.Errorf("score legend does not match levels")
			}
			for n, entry := range a.Score.Legend {
				if entry.ID != expected[n] {
					return fmt.Errorf("score legend keys must match zero-based levels")
				}
			}
		}
		if len(expected) != len(actual) {
			return fmt.Errorf("probabilities must cover all criteria")
		}
		for _, id := range expected {
			found := false
			for _, p := range actual {
				found = found || p.ID == id
			}
			if !found {
				return fmt.Errorf("probability missing criterion %q", id)
			}
		}
	}
	return nil
}
func (v ModelsRequest) Validate() error { return check.ID("providerId", v.ProviderID) }
func (v DecisionModel) Validate() error {
	if err := check.All(check.Text("model.id", v.ID, 255, true), check.Text("model.label", v.Label, 255, true), check.Count("model.kinds", len(v.Kinds), 1, 3), check.Unique("model kind", v.Kinds)); err != nil {
		return err
	}
	for _, kind := range v.Kinds {
		if err := check.Enum("model kind", kind, KindChoice, KindScore, KindNoul); err != nil {
			return err
		}
	}
	return nil
}
func (v ModelsResult) Validate() error {
	if v.ObservedAt != "" {
		if err := check.Timestamp("models.observedAt", v.ObservedAt); err != nil {
			return err
		}
	}
	if len(v.Models) > 0 && v.ObservedAt == "" {
		return fmt.Errorf("model catalog needs its observation time")
	}
	if v.Failure != nil {
		if v.Refreshing {
			return fmt.Errorf("refreshing catalog cannot have a terminal failure")
		}
		if err := v.Failure.Validate(); err != nil {
			return err
		}
	}
	if err := check.All(check.ID("providerId", v.ProviderID), check.Count("models", len(v.Models), 0, 100)); err != nil {
		return err
	}
	ids := make([]string, 0, len(v.Models))
	for _, m := range v.Models {
		if err := m.Validate(); err != nil {
			return err
		}
		ids = append(ids, m.ID)
	}
	return check.All(check.Unique("model", ids), check.Size(v))
}

func (v Failure) Validate() error {
	return check.All(check.ID("failure.code", v.Code), check.Text("failure.message", v.Message, 1024, true))
}
func (v Job) Validate() error {
	if err := check.All(check.ID("job.id", v.ID), check.ID("job.providerId", v.ProviderID), check.Enum("job state", v.State, StateQueued, StateRunning, StateCompleted, StateFailed, StateCancelled, StateExpired), check.Timestamp("job.createdAt", v.CreatedAt), check.Timestamp("job.deadlineAt", v.DeadlineAt), check.OptionalID("job.resultHandleId", v.ResultHandleID)); err != nil {
		return err
	}
	if v.State == StateCompleted {
		if (v.Result == nil) == (v.ResultHandleID == "") || v.Failure != nil {
			return fmt.Errorf("completed job requires exactly one result source")
		}
		if v.Result != nil {
			if v.Result.ProviderID != v.ProviderID {
				return fmt.Errorf("job provider mismatch")
			}
			if err := v.Result.Validate(); err != nil {
				return err
			}
		}
	} else if v.Result != nil || v.ResultHandleID != "" {
		return fmt.Errorf("unfinished job cannot expose a result")
	}
	if (v.State == StateFailed) != (v.Failure != nil) {
		return fmt.Errorf("failed job requires a failure, other states forbid it")
	}
	if v.Failure != nil {
		if err := v.Failure.Validate(); err != nil {
			return err
		}
	}
	return check.Size(v)
}
func (v StartResult) Validate() error   { return v.Job.Validate() }
func (v ReadRequest) Validate() error   { return check.ID("jobId", v.JobID) }
func (v ReadResult) Validate() error    { return v.Job.Validate() }
func (v CancelRequest) Validate() error { return check.ID("jobId", v.JobID) }
func (v CancelResult) Validate() error  { return v.Job.Validate() }
