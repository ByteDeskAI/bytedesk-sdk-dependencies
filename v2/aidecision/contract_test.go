package aidecision

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func textValue(s string) StructuredValue {
	return StructuredValue{Format: FormatText, Content: TextInput{Inline: s}}
}
func fixture() (BatchRequest, BatchResult) {
	zero := uint64(0)
	r := BatchRequest{ProviderID: "jev", Model: "jev-version", Purpose: PurposeEvaluation, State: textValue("task"), IdempotencyKey: "operation-1", Questions: []Question{
		{ID: "choose", Kind: KindChoice, Instructions: textValue("choose"), Choice: &ChoiceQuestion{Options: []Option{{ID: "a"}, {ID: "b"}}}},
		{ID: "score", Kind: KindScore, Instructions: textValue("score"), Score: &ScoreQuestion{Levels: []StructuredValue{textValue("low"), textValue("high")}}},
		{ID: "noul", Kind: KindNoul, Instructions: textValue("truth"), Noul: &NoulQuestion{}},
	}}
	v := BatchResult{ProviderID: "jev", RequestedModel: r.Model, Model: r.Model, Usage: Usage{InputTokens: &zero, OutputTokens: &zero}, Answers: []Answer{
		{ID: "choose", Kind: KindChoice, Choice: &ChoiceAnswer{Choice: "a", Confidence: "0.8", Probabilities: []Probability{{ID: "a", Value: "0.8"}, {ID: "b", Value: "0.2"}}}},
		{ID: "score", Kind: KindScore, Score: &ScoreAnswer{Score: "0.75", Confidence: "0.9", Legend: []LegendEntry{{ID: "0", Label: "low"}, {ID: "1", Label: "high"}}, Probabilities: []Probability{{ID: "0", Value: "0.25"}, {ID: "1", Value: "0.75"}}}},
		{ID: "noul", Kind: KindNoul, Noul: &NoulAnswer{Noul: "0.3333333333333333333333333333333333333333"}},
	}}
	return r, v
}
func TestMixedBatchCorrespondence(t *testing.T) {
	r, v := fixture()
	if err := v.ValidateFor(r); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*BatchResult){
		"missing usage":         func(v *BatchResult) { v.Usage.InputTokens = nil },
		"missing option":        func(v *BatchResult) { v.Answers[0].Choice.Probabilities = v.Answers[0].Choice.Probabilities[:1] },
		"wrong score range":     func(v *BatchResult) { v.Answers[1].Score.Score = "1.000001" },
		"wrong legend":          func(v *BatchResult) { v.Answers[1].Score.Legend[1].ID = "2" },
		"wrong answer id":       func(v *BatchResult) { v.Answers[0].ID = "invented" },
		"extra kind":            func(v *BatchResult) { v.Answers[2].Choice = &ChoiceAnswer{} },
		"wrong probability sum": func(v *BatchResult) { v.Answers[0].Choice.Probabilities[1].Value = "0.3" },
		"precise out of range":  func(v *BatchResult) { v.Answers[2].Noul.Noul = "1.00000000000000000000000000000000000001" },
	} {
		t.Run(name, func(t *testing.T) {
			r, v := fixture()
			mutate(&v)
			if v.ValidateFor(r) == nil {
				t.Fatal("accepted invalid result")
			}
		})
	}
}
func TestDecimalNormalizationPreservesPrecision(t *testing.T) {
	for in, want := range map[string]string{"1.000e-3": "0.001", "1e2": "100", "0.123456789012345678901234567890": "0.12345678901234567890123456789", "0e64": "0"} {
		got, err := DecimalFromJSONNumber(json.Number(in))
		if err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"NaN", "Inf", "-0.1", "01", "true", "null", "1e-65", strings.Repeat("9", 65)} {
		if _, err := DecimalFromJSONNumber(json.Number(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
	}
	for _, in := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1} {
		if _, err := DecimalFromFloat(in); err == nil {
			t.Fatal("accepted nonfinite/negative")
		}
	}
}
func TestStructuredValuesAndExplicitJobState(t *testing.T) {
	for _, raw := range []string{`{}`, `[]`, `{"nested":[1,true,null]}`} {
		if err := (StructuredValue{Format: FormatJSON, Content: TextInput{Inline: raw}}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`1`, `true`, `null`, `"text"`, `{"bad":`} {
		if (StructuredValue{Format: FormatJSON, Content: TextInput{Inline: raw}}).Validate() == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	_, result := fixture()
	j := Job{ID: "job", ProviderID: "jev", State: StateRunning, CreatedAt: "2026-09-25T00:00:00Z", DeadlineAt: "2026-09-25T00:00:30Z"}
	if err := j.Validate(); err != nil {
		t.Fatal(err)
	}
	j.Result = &result
	if j.Validate() == nil {
		t.Fatal("running job exposed result")
	}
	j.State = StateCompleted
	if err := j.Validate(); err != nil {
		t.Fatal(err)
	}
	j.ResultHandleID = "handle"
	if j.Validate() == nil {
		t.Fatal("two result sources")
	}
}
