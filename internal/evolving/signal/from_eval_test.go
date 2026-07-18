package signal

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
)

func TestFromEvaluatedCase_低分产生信号(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "what is Go?"},
		map[string]any{"answer": "a programming language"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "a car"})
	ec.SetScore(0.0)

	sig := FromEvaluatedCase(ec, "", nil)

	if sig == nil {
		t.Fatal("FromEvaluatedCase returned nil, want non-nil signal")
	}
	if sig.SignalType != "low_score" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "low_score")
	}
	if sig.Section != "Troubleshooting" {
		t.Errorf("Section = %q, want %q", sig.Section, "Troubleshooting")
	}
	if sig.Context == nil {
		t.Fatal("Context is nil")
	}
	if sig.Context["score"] != 0.0 {
		t.Errorf("Context[score] = %v, want 0.0", sig.Context["score"])
	}
	if sig.Context["source"] != "offline_evaluation" {
		t.Errorf("Context[source] = %v, want %q", sig.Context["source"], "offline_evaluation")
	}
}

func TestFromEvaluatedCase_非零分产生Evaluated信号(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "what is Go?"},
		map[string]any{"answer": "a programming language"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "a language"})
	ec.SetScore(0.5)

	sig := FromEvaluatedCase(ec, "", nil)

	if sig == nil {
		t.Fatal("FromEvaluatedCase returned nil, want non-nil signal")
	}
	if sig.SignalType != "evaluated" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "evaluated")
	}
}

func TestFromEvaluatedCase_达到阈值返回nil(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "good"})
	ec.SetScore(1.0)

	threshold := 1.0
	sig := FromEvaluatedCase(ec, "", &threshold)

	if sig != nil {
		t.Errorf("FromEvaluatedCase = %v, want nil when score >= threshold", sig)
	}
}

func TestFromEvaluatedCase_无阈值不过滤(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "good"})
	ec.SetScore(1.0)

	sig := FromEvaluatedCase(ec, "", nil)

	if sig == nil {
		t.Fatal("FromEvaluatedCase returned nil with no threshold, want non-nil")
	}
	if sig.SignalType != "evaluated" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "evaluated")
	}
}

func TestFromEvaluatedCase_operatorID传入SkillName(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "bad"})
	ec.SetScore(0.0)

	sig := FromEvaluatedCase(ec, "my_operator", nil)

	if sig.SkillName == nil || *sig.SkillName != "my_operator" {
		t.Errorf("SkillName = %v, want %q", sig.SkillName, "my_operator")
	}
}

func TestFromEvaluatedCase_空operatorID无SkillName(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "bad"})
	ec.SetScore(0.0)

	sig := FromEvaluatedCase(ec, "", nil)

	if sig.SkillName != nil {
		t.Errorf("SkillName = %v, want nil when operatorID is empty", sig.SkillName)
	}
}

func TestFromEvaluatedCase_Context包含完整诊断信息(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "what is Go?"},
		map[string]any{"answer": "a programming language"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "a car"})
	ec.SetScore(0.0)
	ec.Reason = "wrong answer"

	sig := FromEvaluatedCase(ec, "op1", nil)

	if sig.Context == nil {
		t.Fatal("Context is nil")
	}
	// 验证 Python 对齐的字段
	expectedKeys := []string{"question", "label", "answer", "reason", "score", "source"}
	for _, key := range expectedKeys {
		if _, ok := sig.Context[key]; !ok {
			t.Errorf("Context missing key %q", key)
		}
	}
	if sig.Context["source"] != "offline_evaluation" {
		t.Errorf("Context[source] = %v, want %q", sig.Context["source"], "offline_evaluation")
	}
}

func TestFromEvaluatedCase_Excerpt格式(t *testing.T) {
	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "bad"})
	ec.SetScore(0.5)

	sig := FromEvaluatedCase(ec, "", nil)

	expected := "score=0.50"
	if sig.Excerpt != expected {
		t.Errorf("Excerpt = %q, want %q", sig.Excerpt, expected)
	}
}

func TestFromEvaluatedCases_批量转换(t *testing.T) {
	case1 := dataset.NewCase(
		map[string]any{"query": "q1"},
		map[string]any{"answer": "a1"},
	)
	case2 := dataset.NewCase(
		map[string]any{"query": "q2"},
		map[string]any{"answer": "a2"},
	)
	ec1 := dataset.NewEvaluatedCase(*case1, map[string]any{"output": "bad"})
	ec1.SetScore(0.0)
	ec2 := dataset.NewEvaluatedCase(*case2, map[string]any{"output": "good"})
	ec2.SetScore(1.0)

	threshold := 1.0
	signals := FromEvaluatedCases([]*dataset.EvaluatedCase{ec1, ec2}, "", &threshold)

	if len(signals) != 1 {
		t.Fatalf("FromEvaluatedCases returned %d signals, want 1", len(signals))
	}
	if signals[0].SignalType != "low_score" {
		t.Errorf("SignalType = %q, want %q", signals[0].SignalType, "low_score")
	}
}

func TestFromEvaluatedCases_无阈值全部保留(t *testing.T) {
	case1 := dataset.NewCase(
		map[string]any{"query": "q1"},
		map[string]any{"answer": "a1"},
	)
	case2 := dataset.NewCase(
		map[string]any{"query": "q2"},
		map[string]any{"answer": "a2"},
	)
	ec1 := dataset.NewEvaluatedCase(*case1, map[string]any{"output": "bad"})
	ec1.SetScore(0.0)
	ec2 := dataset.NewEvaluatedCase(*case2, map[string]any{"output": "good"})
	ec2.SetScore(1.0)

	signals := FromEvaluatedCases([]*dataset.EvaluatedCase{ec1, ec2}, "", nil)

	if len(signals) != 2 {
		t.Fatalf("FromEvaluatedCases returned %d signals, want 2", len(signals))
	}
}

func TestFromEvaluatedCases_空列表(t *testing.T) {
	signals := FromEvaluatedCases(nil, "", nil)

	if len(signals) != 0 {
		t.Errorf("FromEvaluatedCases returned %d signals, want 0", len(signals))
	}
}
