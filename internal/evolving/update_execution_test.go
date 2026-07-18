package evolving

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeOperator 用于测试的模拟 Operator
type fakeOperator struct {
	operatorID string
}

func (f *fakeOperator) OperatorID() string                           { return f.operatorID }
func (f *fakeOperator) GetTunables() map[string]operator.TunableSpec { return nil }
func (f *fakeOperator) GetState() map[string]any                     { return nil }
func (f *fakeOperator) SetParameter(target string, value any)        {}
func (f *fakeOperator) LoadState(state map[string]any)               {}
func (f *fakeOperator) ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	return schema.ApplyResult{
		OperatorID: f.operatorID,
		Target:     target,
		Applied:    true,
		Mode:       update.Mode,
		Effect:     update.Effect,
		Value:      update.Payload,
		ChangeType: update.ChangeType,
		Records:    []any{},
		Errors:     []string{},
		Metadata:   schema.MetadataClone(update.Metadata),
	}
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestExecuteUpdates_正常应用(t *testing.T) {
	operators := map[string]operator.Operator{
		"llm_call": &fakeOperator{operatorID: "llm_call"},
	}
	updates := map[schema.UpdateKey]any{
		schema.UpdateKey{"llm_call", "system_prompt"}: "new prompt",
	}
	results := ExecuteUpdates(operators, updates)
	if len(results) != 1 {
		t.Fatalf("results count = %d, expected 1", len(results))
	}
	if !results[0].Applied {
		t.Error("should be applied")
	}
}

func TestExecuteUpdates_operator不存在(t *testing.T) {
	operators := map[string]operator.Operator{}
	updates := map[schema.UpdateKey]any{
		schema.UpdateKey{"missing_op", "target"}: "value",
	}
	results := ExecuteUpdates(operators, updates)
	if len(results) != 1 {
		t.Fatalf("results count = %d, expected 1", len(results))
	}
	if results[0].Applied {
		t.Error("should not be applied")
	}
	if len(results[0].Errors) == 0 {
		t.Error("should have errors")
	}
	if results[0].Errors[0] != "operator not found: missing_op" {
		t.Errorf("error = %q, expected %q", results[0].Errors[0], "operator not found: missing_op")
	}
}

func TestExecuteUpdates_nil值过滤(t *testing.T) {
	operators := map[string]operator.Operator{
		"llm_call": &fakeOperator{operatorID: "llm_call"},
	}
	updates := map[schema.UpdateKey]any{
		schema.UpdateKey{"llm_call", "target1"}: "value",
		schema.UpdateKey{"llm_call", "target2"}: nil,
	}
	results := ExecuteUpdates(operators, updates)
	if len(results) != 2 {
		t.Fatalf("results count = %d, expected 2", len(results))
	}
	// nil 值应有错误结果
	var nilResult *schema.ApplyResult
	for i := range results {
		if results[i].Target == "target2" {
			nilResult = &results[i]
		}
	}
	if nilResult == nil {
		t.Fatal("should have result for nil value")
	}
	if nilResult.Applied {
		t.Error("nil value result should not be applied")
	}
	if len(nilResult.Errors) == 0 || nilResult.Errors[0] != "update value is nil" {
		t.Errorf("nil error = %v, expected 'update value is nil'", nilResult.Errors)
	}
}

func TestApplyUpdates_兼容别名(t *testing.T) {
	operators := map[string]operator.Operator{
		"llm_call": &fakeOperator{operatorID: "llm_call"},
	}
	updates := map[schema.UpdateKey]any{
		schema.UpdateKey{"llm_call", "system_prompt"}: "new prompt",
	}
	results := ApplyUpdates(operators, updates)
	if len(results) != 1 {
		t.Fatalf("results count = %d, expected 1", len(results))
	}
	if !results[0].Applied {
		t.Error("should be applied")
	}
}

func TestSummarizeApplyResults(t *testing.T) {
	t.Run("混合结果", func(t *testing.T) {
		results := []schema.ApplyResult{
			{Applied: true},
			{Applied: false},
			{Applied: true},
		}
		summary := SummarizeApplyResults(results)
		if summary["total"] != 3 {
			t.Errorf("total = %d, expected 3", summary["total"])
		}
		if summary["applied"] != 2 {
			t.Errorf("applied = %d, expected 2", summary["applied"])
		}
		if summary["failed"] != 1 {
			t.Errorf("failed = %d, expected 1", summary["failed"])
		}
	})

	t.Run("空结果", func(t *testing.T) {
		summary := SummarizeApplyResults(nil)
		if summary["total"] != 0 {
			t.Errorf("total = %d, expected 0", summary["total"])
		}
	})
}
