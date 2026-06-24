package rail

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// AgentCallbackEvent Agent 生命周期回调事件类型。
//
// 定义 per-Agent 实例级的 10 个生命周期事件，
// 供 AgentCallbackManager 注册回调和 AgentRail 钩子使用。
// 与框架层 GlobalAgentEventType（全局观测事件）是不同层次：
//   - AgentCallbackEvent = 实例级 Rail 拦截/控制（重试/提前终止/steering）
//   - GlobalAgentEventType = 框架级全局观测（日志/监控/transform_io）
//
// 事件值即 Python AgentRail 对应方法名，无需额外 EVENT_METHOD_MAP 映射。
// AgentCallbackManager 注册时通过 agentID 前缀构造唯一事件名
// （如 "{agentID}_before_invoke"），与框架层事件互不冲突。
//
// 对应 Python: openjiuwen/core/single_agent/rail/base.py (AgentCallbackEvent)
type AgentCallbackEvent string

const (
	// CallbackBeforeInvoke invoke 开始前
	CallbackBeforeInvoke AgentCallbackEvent = "before_invoke"
	// CallbackAfterInvoke invoke 完成后
	CallbackAfterInvoke AgentCallbackEvent = "after_invoke"
	// CallbackBeforeTaskIteration 外层任务循环迭代开始前
	CallbackBeforeTaskIteration AgentCallbackEvent = "before_task_iteration"
	// CallbackAfterTaskIteration 外层任务循环迭代完成后
	CallbackAfterTaskIteration AgentCallbackEvent = "after_task_iteration"
	// CallbackBeforeModelCall LLM 调用前
	CallbackBeforeModelCall AgentCallbackEvent = "before_model_call"
	// CallbackAfterModelCall LLM 响应后
	CallbackAfterModelCall AgentCallbackEvent = "after_model_call"
	// CallbackOnModelException LLM 调用异常
	CallbackOnModelException AgentCallbackEvent = "on_model_exception"
	// CallbackBeforeToolCall 工具执行前
	CallbackBeforeToolCall AgentCallbackEvent = "before_tool_call"
	// CallbackAfterToolCall 工具执行后
	CallbackAfterToolCall AgentCallbackEvent = "after_tool_call"
	// CallbackOnToolException 工具执行异常
	CallbackOnToolException AgentCallbackEvent = "on_tool_exception"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// AllCallbackEvents 返回所有 AgentCallbackEvent 枚举值。
// 用于遍历清理等场景。
func AllCallbackEvents() []AgentCallbackEvent {
	return []AgentCallbackEvent{
		CallbackBeforeInvoke,
		CallbackAfterInvoke,
		CallbackBeforeTaskIteration,
		CallbackAfterTaskIteration,
		CallbackBeforeModelCall,
		CallbackAfterModelCall,
		CallbackOnModelException,
		CallbackBeforeToolCall,
		CallbackAfterToolCall,
		CallbackOnToolException,
	}
}

// String 实现 fmt.Stringer 接口。
func (e AgentCallbackEvent) String() string {
	return string(e)
}

// GoString 实现 fmt.GoStringer 接口，返回带类型名前缀的字符串表示。
func (e AgentCallbackEvent) GoString() string {
	return fmt.Sprintf("rail.AgentCallbackEvent(%q)", string(e))
}

// ──────────────────────────── 非导出函数 ────────────────────────────
