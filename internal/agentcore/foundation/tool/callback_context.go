package tool

import sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCallbackContext 工具执行期回调上下文的最小契约。
// Go 无 Python 的 **kwargs，等价物是执行期注入的调用选项：
// Python 侧为 `accepts_tool_callback_context = True` +
// `invoke_kwargs["_tool_callback_context"] = callback_context`。
// 由 single_agent/interfaces.AgentCallbackContext 天然实现。
type ToolCallbackContext interface {
	// Extra 返回调用级扩展数据
	Extra() map[string]any
	// Session 返回会话门面
	Session() sessioninterfaces.SessionFacade
}

// CallbackContextAcceptor 声明工具需要执行期回调上下文。
// Python: getattr(tool, "accepts_tool_callback_context", False)
type CallbackContextAcceptor interface {
	// AcceptsToolCallbackContext 是否需要注入回调上下文
	AcceptsToolCallbackContext() bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithToolCallbackContext 注入执行期回调上下文（nil 不覆盖）。
// Python: invoke_kwargs["_tool_callback_context"]
func WithToolCallbackContext(cb ToolCallbackContext) ToolOption {
	return func(o *ToolCallOptions) {
		if cb != nil {
			o.CallbackContext = cb
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
