package schema

import llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"

// ──────────────────────────── 结构体 ────────────────────────────

// AddAbilityResult 添加能力的返回结果。
//
// 对应 Python: AddAbilityResult
type AddAbilityResult struct {
	// Name 能力名称
	Name string
	// Added 是否成功添加
	Added bool
	// Reason 未添加的原因（如 "duplicate_tool"、"added_tool"）
	Reason string
}

// ExecuteResult 单个工具调用的执行结果。
type ExecuteResult struct {
	// Result 执行结果。
	//
	// 两种来源（对齐 Python 的 result tuple 第一个元素）：
	//   1. 拦截路径：_skip_tool=True 时，从 inputs.ToolResult 读取，
	//      值为 map[string]any{"error": msg}（Rail 拦截）或 string（RejectResult）
	//   2. 正常执行：tool.invoke() 的原始返回值，通常为 map[string]any；
	//      agent invoke 返回 map[string]any{"output": ..., "result_type": ...}
	//
	// 异常路径（不走 Result 字段，而是作为 error 返回）：
	//   - *ToolInterruptException
	//   - *workflow.WorkflowOutput (state=INPUT_REQUIRED)
	//   - *AbilityExecutionError
	Result any
	// ToolMsg 返回给 LLM 的 ToolMessage
	ToolMsg *llmschema.ToolMessage
}
