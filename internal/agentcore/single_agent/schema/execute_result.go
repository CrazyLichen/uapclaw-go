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
	// 正常返回: map[string]any 或具体类型
	// 工具中断: *ToolInterruptException
	// 工作流中断: *workflow.WorkflowOutput (state=INPUT_REQUIRED)
	// 执行错误: *AbilityExecutionError
	Result any
	// ToolMsg 返回给 LLM 的 ToolMessage
	ToolMsg *llmschema.ToolMessage
}
