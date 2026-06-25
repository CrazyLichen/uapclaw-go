package rail

import (
	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EventInputs 回调事件输入接口。
//
// 各事件类型对应不同的 Inputs 结构体，均实现此接口。
// 调用方通过 type switch 获取具体类型。
//
// 对应 Python: EventInputs = Union[InvokeInputs, ModelCallInputs, ToolCallInputs, TaskIterationInputs, Dict]
type EventInputs interface {
	// EventKind 返回事件输入的种类标识
	EventKind() string
}

// InvokeQuery Invoke 阶段的查询输入接口。
//
// 对齐 Python: InvokeInputs.query: Optional[str, InteractiveInput]
// InvokeQueryString（普通字符串）和 *InteractiveInput（中断恢复）均实现此接口。
//
// 调用方通过 type switch 获取具体类型：
//
//	switch q := inputs.Query.(type) {
//	case InvokeQueryString:
//	    // 普通字符串查询
//	case *interaction.InteractiveInput:
//	    // 中断恢复输入
//	}
type InvokeQuery interface {
	// IsInteractiveInput 检查是否为交互式输入（中断恢复）
	IsInteractiveInput() bool
	// PlainText 提取纯文本表示。
	// 对齐 Python: _extract_plain_text(user_input)
	// - InvokeQueryString → 直接返回字符串
	// - *InteractiveInput → 从 RawInputs 提取 string，无法提取则返回空串
	PlainText() string
}

// RunContext 结构化运行时上下文（心跳等场景）。
//
// 对应 Python: RunContext (openjiuwen/core/single_agent/rail/base.py L56-62)
type RunContext struct {
	// Reason 心跳触发原因
	Reason HeartbeatReason
	// SessionID 会话标识
	SessionID string
	// ContextMode 上下文模式（如 "lightweight"）
	ContextMode string
	// Extra 额外上下文信息
	Extra map[string]any
}

// InvokeInputs BEFORE/AFTER_INVOKE 事件输入。
//
// Before 阶段填充 query + conversationID；
// After 阶段额外填充 result。
//
// 对应 Python: InvokeInputs (openjiuwen/core/single_agent/rail/base.py L68-96)
type InvokeInputs struct {
	// Query 用户查询输入（普通字符串或交互式输入）
	// 对齐 Python: query: Optional[str, InteractiveInput]
	Query InvokeQuery
	// ConversationID 会话/对话标识（零值空串表示未设置）
	ConversationID string
	// Result Agent invoke 结果（invoke 完成后填充）
	Result map[string]any
	// RunKind 运行模式（normal/heartbeat/cron，零值空串表示未设置）
	RunKind RunKind
	// RunContext 结构化运行时上下文
	RunContext *RunContext
}

// ModelCallInputs BEFORE/AFTER_MODEL_CALL 事件输入。
//
// 对应 Python: ModelCallInputs (openjiuwen/core/single_agent/rail/base.py L103-116)
type ModelCallInputs struct {
	// Messages 发送给 LLM 的消息列表（context window 构建后填充）
	Messages []schema.BaseMessage
	// Tools 工具定义列表（对齐 Python: tools: Optional[List[ToolInfo]]）
	Tools []*cschema.ToolInfo
	// ModelContext 当前 ModelContext（构建 context window 使用）
	ModelContext ceinterface.ModelContext
	// Response LLM 响应（调用完成后填充，对齐 Python: response）
	Response *schema.AssistantMessage
}

// ToolCallInputs BEFORE/AFTER_TOOL_CALL 事件输入。
//
// before_tool_call 钩子可改写 ToolName/ToolArgs；
// after_tool_call 钩子可改写 ToolResult/ToolMsg。
//
// 对应 Python: ToolCallInputs (openjiuwen/core/single_agent/rail/base.py L119-134)
type ToolCallInputs struct {
	// ToolCall 原始工具调用对象
	ToolCall *schema.ToolCall
	// ToolName 工具名称（before 钩子可改写）
	ToolName string
	// ToolArgs 工具参数 JSON 字符串（before 钩子可改写）
	// 对齐 Python: tool_args: Any（实际为 str，即 ToolCall.arguments）
	ToolArgs string
	// ToolResult 工具执行结果（调用完成后填充）
	ToolResult any
	// ToolMsg 工具返回消息（调用完成后填充）
	ToolMsg *schema.ToolMessage
}

// TaskIterationInputs BEFORE/AFTER_TASK_ITERATION 事件输入。
//
// 用于支持外层任务循环的 Agent（如 DeepAgent 扩展）。
// before_task_iteration 钩子可修改 Query 字段来改写内层 Agent 的查询。
//
// 对应 Python: TaskIterationInputs (openjiuwen/core/single_agent/rail/base.py L137-162)
type TaskIterationInputs struct {
	// Iteration 1-based 外层循环迭代索引
	Iteration int
	// LoopEvent 触发本次迭代的事件对象
	LoopEvent any
	// ConversationID 会话/对话标识（零值空串表示未设置）
	ConversationID string
	// Result 迭代结果（迭代完成后填充）
	Result map[string]any
	// Query 本次迭代的有效查询（before_task_iteration 钩子可修改，零值空串表示未设置）
	Query string
	// IsFollowUp 是否由 controller follow-up 触发（而非原始用户查询）
	IsFollowUp bool
}

// MapInputs 任意字典事件输入，作为 EventInputs 的兜底类型。
//
// 对齐 Python: EventInputs = Union[..., Dict[str, Any]]
// 当 inputs 不属于四种 typed struct 时使用。
type MapInputs struct {
	// Data 任意事件输入数据
	Data map[string]any
}

// RetryRequest 重试指令，由 on_exception 钩子产生。
//
// 对应 Python: RetryRequest (openjiuwen/core/single_agent/rail/base.py L165-169)
type RetryRequest struct {
	// DelaySeconds 重试前等待秒数
	DelaySeconds float64
}

// ForceFinishRequest 提前终止信号，使 Agent 循环立即返回结果。
//
// 对应 Python: ForceFinishRequest (openjiuwen/core/single_agent/rail/base.py L172-176)
type ForceFinishRequest struct {
	// Result 提前终止时返回的结果
	Result map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// InvokeQueryString 普通字符串查询，实现 InvokeQuery 接口。
//
// 对齐 Python: InvokeInputs.query 为 str 类型时的分支
type InvokeQueryString string

// RunKind 运行模式枚举。
//
// 对应 Python: RunKind (openjiuwen/core/single_agent/rail/base.py L43-47)
type RunKind string

// HeartbeatReason 心跳触发原因枚举。
//
// 对应 Python: HeartbeatReason (openjiuwen/core/single_agent/rail/base.py L50-53)
type HeartbeatReason string

const (
	// RunKindNormal 正常运行
	RunKindNormal RunKind = "normal"
	// RunKindHeartbeat 心跳运行
	RunKindHeartbeat RunKind = "heartbeat"
	// RunKindCron 定时任务运行
	RunKindCron RunKind = "cron"
)

const (
	// HeartbeatReasonInterval 定时心跳
	HeartbeatReasonInterval HeartbeatReason = "interval"
	// HeartbeatReasonManual 手动触发心跳
	HeartbeatReasonManual HeartbeatReason = "manual"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRunContext 创建 RunContext 实例。
func NewRunContext() *RunContext {
	return &RunContext{Extra: make(map[string]any)}
}

// NewInvokeQueryString 从字符串构造 InvokeQuery。
//
// 对齐 Python: InvokeInputs(query="hello") 中 query 为 str 的分支
func NewInvokeQueryString(s string) InvokeQuery {
	return InvokeQueryString(s)
}

// NewMapInputs 创建 MapInputs 实例。
func NewMapInputs() *MapInputs {
	return &MapInputs{Data: make(map[string]any)}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsInteractiveInput 实现 InvokeQuery 接口，普通字符串查询始终返回 false。
func (q InvokeQueryString) IsInteractiveInput() bool { return false }

// PlainText 实现 InvokeQuery 接口，返回字符串本身。
func (q InvokeQueryString) PlainText() string { return string(q) }

// IsHeartbeat 检查是否为心跳运行。
//
// 对应 Python: InvokeInputs.is_heartbeat()
func (i *InvokeInputs) IsHeartbeat() bool {
	return i.RunKind == RunKindHeartbeat
}

// IsLightweightContext 检查是否启用轻量上下文模式。
//
// 对应 Python: InvokeInputs.is_lightweight_context()
func (i *InvokeInputs) IsLightweightContext() bool {
	if i.RunContext != nil && i.RunContext.ContextMode != "" {
		return i.RunContext.ContextMode == "lightweight"
	}
	return false
}

// IsCron 检查是否为定时任务运行。
//
// 对应 Python: InvokeInputs.is_cron()
func (i *InvokeInputs) IsCron() bool {
	return i.RunKind == RunKindCron
}

// EventKind 实现 EventInputs 接口
func (i *InvokeInputs) EventKind() string { return "invoke" }

// EventKind 实现 EventInputs 接口
func (i *ModelCallInputs) EventKind() string { return "model_call" }

// EventKind 实现 EventInputs 接口
func (i *ToolCallInputs) EventKind() string { return "tool_call" }

// EventKind 实现 EventInputs 接口
func (i *TaskIterationInputs) EventKind() string { return "task_iteration" }

// EventKind 实现 EventInputs 接口
func (m *MapInputs) EventKind() string { return "map" }

// ──────────────────────────── 非导出函数 ────────────────────────────
