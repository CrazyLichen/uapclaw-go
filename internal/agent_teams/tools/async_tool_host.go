package tools

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// AsyncToolLaunchOptions 异步工具启动参数。
// Python: AsyncToolRuntime.launch(...) 的 tool_name/description/format_* 关键字参数
type AsyncToolLaunchOptions struct {
	// ToolName 启动工具名（完成文案用）
	ToolName string
	// Description 人类可读任务描述（注册表用）
	Description string
	// FormatCompleted 自定义完成文案；nil 用 async_tool.completed
	FormatCompleted func(any) string
	// FormatFailed 自定义失败文案；nil 用 async_tool.failed
	FormatFailed func(error) string
}

// AsyncToolHost 两阶段异步工具的宿主能力（由 NativeHarness 实现）。
// Python: AsyncTool 持有的 parent_agent（launch_async_tool + session_id）
type AsyncToolHost interface {
	// LaunchAsyncTool 启动后台任务
	LaunchAsyncTool(taskID string, factory func(context.Context) (any, error), opts AsyncToolLaunchOptions) error
	// SessionID 返回宿主会话标识（spill 目录用）
	SessionID() string
	// SetAsyncToolOutputDirResolver 注入溢出目录解析器（rail init 时设置）
	SetAsyncToolOutputDirResolver(fn func() string)
}

// ──────────────────────────── 常量 ────────────────────────────

// TeamHandleAsyncToolHost BuildContext extras 中的异步工具宿主句柄键。
const TeamHandleAsyncToolHost = "team.async_tool_host"
