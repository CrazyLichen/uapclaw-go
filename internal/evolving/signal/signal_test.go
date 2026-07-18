package signal

import (
	"strings"
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

func TestEvolutionCategory_值对齐(t *testing.T) {
	if EvolutionCategorySkillExperience != "skill_experience" {
		t.Errorf("EvolutionCategorySkillExperience = %q, want %q", EvolutionCategorySkillExperience, "skill_experience")
	}
	if EvolutionCategoryNewSkill != "new_skill" {
		t.Errorf("EvolutionCategoryNewSkill = %q, want %q", EvolutionCategoryNewSkill, "new_skill")
	}
}

func TestEvolutionTarget_值对齐(t *testing.T) {
	if EvolutionTargetDescription != "description" {
		t.Errorf("EvolutionTargetDescription = %q, want %q", EvolutionTargetDescription, "description")
	}
	if EvolutionTargetBody != "body" {
		t.Errorf("EvolutionTargetBody = %q, want %q", EvolutionTargetBody, "body")
	}
	if EvolutionTargetScript != "script" {
		t.Errorf("EvolutionTargetScript = %q, want %q", EvolutionTargetScript, "script")
	}
}

func TestEvolutionSignal_ToDict_含Context(t *testing.T) {
	skill := "my_skill"
	sig := EvolutionSignal{
		SignalType: "execution_failure",
		Section:    "Troubleshooting",
		Excerpt:    "error occurred",
		SkillName:  &skill,
		Context:    map[string]any{"source": "passive_conversation"},
	}
	d := sig.ToDict()
	if d["type"] != "execution_failure" {
		t.Errorf("ToDict type = %v, want %q", d["type"], "execution_failure")
	}
	if d["section"] != "Troubleshooting" {
		t.Errorf("ToDict section = %v, want %q", d["section"], "Troubleshooting")
	}
	if d["excerpt"] != "error occurred" {
		t.Errorf("ToDict excerpt = %v, want %q", d["excerpt"], "error occurred")
	}
	if _, ok := d["context"]; !ok {
		t.Error("ToDict missing context key")
	}
}

func TestEvolutionSignal_ToDict_无Context(t *testing.T) {
	sig := EvolutionSignal{
		SignalType: "low_score",
		Section:    "Troubleshooting",
		Excerpt:    "score=0.00",
	}
	d := sig.ToDict()
	if _, ok := d["context"]; ok {
		t.Error("ToDict should not contain context when nil")
	}
}

func TestMakeEvolutionSignal_基本(t *testing.T) {
	sig := MakeEvolutionSignal("execution_failure", "Troubleshooting", "error",
		WithSource("passive_conversation"),
		WithToolName("bash"),
		WithSkillName("my_skill"),
	)
	if sig.SignalType != "execution_failure" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "execution_failure")
	}
	if sig.SkillName == nil || *sig.SkillName != "my_skill" {
		t.Errorf("SkillName = %v, want %q", sig.SkillName, "my_skill")
	}
	if sig.Context["source"] != "passive_conversation" {
		t.Errorf("Context[source] = %v, want %q", sig.Context["source"], "passive_conversation")
	}
	if sig.Context["tool_name"] != "bash" {
		t.Errorf("Context[tool_name] = %v, want %q", sig.Context["tool_name"], "bash")
	}
}

func TestMakeEvolutionSignal_不覆盖已有Context(t *testing.T) {
	sig := MakeEvolutionSignal("test", "section", "excerpt",
		WithContext(map[string]any{"source": "existing"}),
		WithSource("new_source"),
	)
	if sig.Context["source"] != "existing" {
		t.Errorf("Context[source] = %v, want %q (setdefault should not overwrite)", sig.Context["source"], "existing")
	}
}

func TestMakeEvolutionSignal_无选项时Context为nil(t *testing.T) {
	sig := MakeEvolutionSignal("test", "section", "excerpt")
	if sig.Context != nil {
		t.Errorf("Context = %v, want nil when no options", sig.Context)
	}
}

func TestGetSignalSource_有source(t *testing.T) {
	sig := MakeEvolutionSignal("test", "section", "excerpt", WithSource("passive_conversation"))
	src := GetSignalSource(sig)
	if src == nil || *src != "passive_conversation" {
		t.Errorf("GetSignalSource = %v, want %q", src, "passive_conversation")
	}
}

func TestGetSignalSource_无source(t *testing.T) {
	sig := &EvolutionSignal{SignalType: "test", Section: "s", Excerpt: "e"}
	src := GetSignalSource(sig)
	if src != nil {
		t.Errorf("GetSignalSource = %v, want nil", src)
	}
}

func TestMakeSignalFingerprint_基本(t *testing.T) {
	sig := MakeEvolutionSignal("execution_failure", "Troubleshooting", "error in bash command",
		WithToolName("bash"),
		WithSkillName("my_skill"),
	)
	fp := MakeSignalFingerprint(sig)
	if fp[0] != "execution_failure" {
		t.Errorf("fingerprint[0] = %q, want %q", fp[0], "execution_failure")
	}
	if fp[1] != "bash" {
		t.Errorf("fingerprint[1] = %q, want %q", fp[1], "bash")
	}
	if fp[2] != "my_skill" {
		t.Errorf("fingerprint[2] = %q, want %q", fp[2], "my_skill")
	}
	if fp[3] != "error in bash command" {
		t.Errorf("fingerprint[3] = %q, want %q", fp[3], "error in bash command")
	}
}

func TestMakeSignalFingerprint_截断长excerpt(t *testing.T) {
	longExcerpt := strings.Repeat("a", 300)
	sig := &EvolutionSignal{SignalType: "test", Section: "s", Excerpt: longExcerpt}
	fp := MakeSignalFingerprint(sig)
	if len(fp[3]) != 200 {
		t.Errorf("fingerprint[3] length = %d, want 200", len(fp[3]))
	}
}
