package dataset

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewCase 测试 Case 构造函数
func TestNewCase(t *testing.T) {
	inputs := map[string]any{"query": "hello"}
	label := map[string]any{"answer": "world"}
	c := NewCase(inputs, label)
	if c.CaseID == "" {
		t.Error("期望 CaseID 自动生成，实际为空")
	}
	if len(c.Inputs) != 1 || c.Inputs["query"] != "hello" {
		t.Errorf("期望 Inputs 包含 query=hello, 实际=%v", c.Inputs)
	}
	if len(c.Label) != 1 || c.Label["answer"] != "world" {
		t.Errorf("期望 Label 包含 answer=world, 实际=%v", c.Label)
	}
	if len(c.Tools) != 0 {
		t.Errorf("期望 Tools 为空, 实际=%v", c.Tools)
	}
}

// TestNewCase_使用选项 测试 Case 构造函数带选项
func TestNewCase_使用选项(t *testing.T) {
	inputs := map[string]any{"q": "1"}
	label := map[string]any{"a": "2"}
	tools := []schema.ToolInfo{{Name: "tool1"}}
	c := NewCase(inputs, label, WithCaseTools(tools), WithCaseID("my-id"))
	if c.CaseID != "my-id" {
		t.Errorf("期望 CaseID=my-id, 实际=%s", c.CaseID)
	}
	if len(c.Tools) != 1 {
		t.Errorf("期望 Tools 长度 1, 实际=%d", len(c.Tools))
	}
}

// TestNewEvaluatedCase 测试 EvaluatedCase 构造
func TestNewEvaluatedCase(t *testing.T) {
	c := NewCase(map[string]any{"q": "1"}, map[string]any{"a": "2"})
	answer := map[string]any{"output": "result"}
	ec := NewEvaluatedCase(*c, answer)
	if ec.Score != 0.0 {
		t.Errorf("期望默认 Score=0.0, 实际=%f", ec.Score)
	}
	if ec.Reason != "" {
		t.Errorf("期望默认 Reason 为空, 实际=%s", ec.Reason)
	}
	if ec.PerMetric != nil {
		t.Errorf("期望默认 PerMetric 为 nil, 实际=%v", ec.PerMetric)
	}
	if len(ec.Answer) != 1 || ec.Answer["output"] != "result" {
		t.Errorf("期望 Answer 包含 output=result, 实际=%v", ec.Answer)
	}
}

// TestEvaluatedCase_SetScore_钳位 测试 Score 钳位到 [0, 1]
func TestEvaluatedCase_SetScore_钳位(t *testing.T) {
	c := NewCase(map[string]any{"q": "1"}, map[string]any{"a": "2"})
	ec := NewEvaluatedCase(*c, nil)

	ec.SetScore(1.5)
	if ec.Score != 1.0 {
		t.Errorf("期望 Score 钳位到 1.0, 实际=%f", ec.Score)
	}

	ec.SetScore(-0.5)
	if ec.Score != 0.0 {
		t.Errorf("期望 Score 钳位到 0.0, 实际=%f", ec.Score)
	}

	ec.SetScore(0.7)
	if ec.Score != 0.7 {
		t.Errorf("期望 Score=0.7, 实际=%f", ec.Score)
	}
}

// TestEvaluatedCase_便捷属性 测试 EvaluatedCase 代理属性
func TestEvaluatedCase_便捷属性(t *testing.T) {
	tools := []schema.ToolInfo{{Name: "tool1"}}
	c := NewCase(map[string]any{"q": "1"}, map[string]any{"a": "2"}, WithCaseTools(tools), WithCaseID("test-id"))
	ec := NewEvaluatedCase(*c, nil)

	if ec.GetInputs()["q"] != "1" {
		t.Errorf("期望 GetInputs 返回原始 inputs")
	}
	if ec.GetLabel()["a"] != "2" {
		t.Errorf("期望 GetLabel 返回原始 label")
	}
	if ec.GetCaseID() != "test-id" {
		t.Errorf("期望 GetCaseID=test-id, 实际=%s", ec.GetCaseID())
	}
	if len(ec.GetTools()) != 1 {
		t.Errorf("期望 GetTools 长度 1, 实际=%d", len(ec.GetTools()))
	}
}
