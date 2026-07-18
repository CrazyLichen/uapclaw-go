package signal

import (
	"testing"
)

func TestEvolutionSignal_字段赋值(t *testing.T) {
	skill := "my_skill"
	sig := EvolutionSignal{
		SignalType: "low_score",
		Section:    "Troubleshooting",
		Excerpt:    "score=0.00",
		SkillName:  &skill,
		Context: map[string]any{
			"score":  0.0,
			"source": "offline_evaluation",
			"reason": "wrong answer",
		},
	}

	if sig.SignalType != "low_score" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "low_score")
	}
	if sig.Section != "Troubleshooting" {
		t.Errorf("Section = %q, want %q", sig.Section, "Troubleshooting")
	}
	if sig.Excerpt != "score=0.00" {
		t.Errorf("Excerpt = %q, want %q", sig.Excerpt, "score=0.00")
	}
	if sig.SkillName == nil || *sig.SkillName != "my_skill" {
		t.Errorf("SkillName = %v, want %q", sig.SkillName, "my_skill")
	}
	if sig.Context["score"] != 0.0 {
		t.Errorf("Context[score] = %v, want 0.0", sig.Context["score"])
	}
}

func TestEvolutionSignal_可选字段为零值(t *testing.T) {
	sig := EvolutionSignal{
		SignalType: "evaluated",
		Section:    "Examples",
		Excerpt:    "score=0.50",
	}

	if sig.SkillName != nil {
		t.Errorf("SkillName = %v, want nil", sig.SkillName)
	}
	if sig.Context != nil {
		t.Errorf("Context = %v, want nil", sig.Context)
	}
}
