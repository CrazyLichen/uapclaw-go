package evolving

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ExecuteUpdates 归一化并执行一批更新，不包含持久化或审批。
//
// 1. 过滤 nil 值更新，为 nil 值生成错误结果
// 2. 归一化后逐一应用到对应 operator
// 3. operator 不存在时生成完整 ApplyResult（含 ChangeType 和 Metadata）
//
// 对应 Python: openjiuwen/agent_evolving/update_execution.py execute_updates
func ExecuteUpdates(
	operators map[string]operator.Operator,
	updates map[schema.UpdateKey]any,
) []schema.ApplyResult {
	var results []schema.ApplyResult

	// 1. 过滤 nil 值更新
	nonNilUpdates := make(map[schema.UpdateKey]any)
	for key, value := range updates {
		if value != nil {
			nonNilUpdates[key] = value
		}
	}

	// 2. 归一化后逐一应用
	normalized := schema.NormalizeUpdates(nonNilUpdates)
	for key, update := range normalized {
		op, ok := operators[key.OperatorID()]
		if !ok {
			results = append(results, schema.ApplyResult{
				OperatorID: key.OperatorID(),
				Target:     key.Target(),
				Applied:    false,
				Mode:       update.Mode,
				Effect:     update.Effect,
				Value:      update.Payload,
				ChangeType: update.ChangeType,
				Records:    []any{},
				Errors:     []string{fmt.Sprintf("operator not found: %s", key.OperatorID())},
				Metadata:   schema.MetadataClone(update.Metadata),
			})
			continue
		}
		results = append(results, op.ApplyUpdate(key.Target(), update))
	}

	// 3. 为 nil 值更新生成错误结果（对齐 Python ApplyResult 默认值）
	for key, value := range updates {
		if value == nil {
			results = append(results, schema.ApplyResult{
				OperatorID: key.OperatorID(),
				Target:     key.Target(),
				Applied:    false,
				Mode:       schema.UpdateModeReplace,
				Effect:     schema.UpdateEffectState,
				Records:    []any{},
				Errors:     []string{"update value is nil"},
			})
		}
	}

	return results
}

// ApplyUpdates ExecuteUpdates 的兼容别名。
//
// 对应 Python: openjiuwen/agent_evolving/update_execution.py apply_updates
func ApplyUpdates(
	operators map[string]operator.Operator,
	updates map[schema.UpdateKey]any,
) []schema.ApplyResult {
	return ExecuteUpdates(operators, updates)
}

// SummarizeApplyResults 返回更新执行的聚合统计。
//
// 对应 Python: openjiuwen/agent_evolving/update_execution.py summarize_apply_results
func SummarizeApplyResults(results []schema.ApplyResult) map[string]int {
	applied := 0
	for _, r := range results {
		if r.Applied {
			applied++
		}
	}
	return map[string]int{
		"total":   len(results),
		"applied": applied,
		"failed":  len(results) - applied,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
