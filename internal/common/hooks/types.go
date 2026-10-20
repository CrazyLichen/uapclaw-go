package hooks

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// HookType hook 类型枚举，对齐 Python HookType(COMMAND/PROMPT)
type HookType string

const (
	// HookTypeCommand command 类型 hook（子进程执行），对齐 Python HookType.COMMAND
	HookTypeCommand HookType = "command"
	// HookTypePrompt prompt 类型 hook（LLM 审核），对齐 Python HookType.PROMPT
	HookTypePrompt HookType = "prompt"
)

// ──────────────────────────── 常量 ────────────────────────────

// HookEvent hook 事件常量，对齐 Python HookEvent(17 个)
// 统一为字符串常量，格式与 Python 一致（如 "PreToolUse"）
const (
	// HookEventPreToolUse 工具调用前，对齐 Python PRE_TOOL_USE
	HookEventPreToolUse = "PreToolUse"
	// HookEventPostToolUse 工具调用后，对齐 Python POST_TOOL_USE
	HookEventPostToolUse = "PostToolUse"
	// HookEventPostToolUseFailure 工具调用异常后，对齐 Python POST_TOOL_USE_FAILURE
	HookEventPostToolUseFailure = "PostToolUseFailure"
	// HookEventStop Agent 停止，对齐 Python STOP
	HookEventStop = "Stop"
	// HookEventUserPromptSubmit 用户提交 prompt，对齐 Python USER_PROMPT_SUBMIT
	HookEventUserPromptSubmit = "UserPromptSubmit"
	// HookEventSessionStart 会话开始，对齐 Python SESSION_START
	HookEventSessionStart = "SessionStart"
	// HookEventSessionEnd 会话结束，对齐 Python SESSION_END
	HookEventSessionEnd = "SessionEnd"
	// HookEventNotification 通知，对齐 Python NOTIFICATION
	HookEventNotification = "Notification"
	// HookEventPermissionRequest 权限请求，对齐 Python PERMISSION_REQUEST
	HookEventPermissionRequest = "PermissionRequest"
	// HookEventPermissionDenied 权限拒绝，对齐 Python PERMISSION_DENIED
	HookEventPermissionDenied = "PermissionDenied"
	// HookEventSubagentStart 子 Agent 启动，对齐 Python SUBAGENT_START
	HookEventSubagentStart = "SubagentStart"
	// HookEventSubagentStop 子 Agent 停止，对齐 Python SUBAGENT_STOP
	HookEventSubagentStop = "SubagentStop"
	// HookEventConfigChange 配置变更，对齐 Python CONFIG_CHANGE
	HookEventConfigChange = "ConfigChange"
	// HookEventInstructionsLoaded 指令加载，对齐 Python INSTRUCTIONS_LOADED
	HookEventInstructionsLoaded = "InstructionsLoaded"
	// HookEventSetup 安装，对齐 Python SETUP
	HookEventSetup = "Setup"
	// HookEventBeforeModelCall 模型调用前，对齐 Python BEFORE_MODEL_CALL
	HookEventBeforeModelCall = "BeforeModelCall"
	// HookEventAfterModelCall 模型调用后，对齐 Python AFTER_MODEL_CALL
	HookEventAfterModelCall = "AfterModelCall"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// AgentRailEvents 需要在 AgentServer Rail 层执行的事件，对齐 Python _AGENT_RAIL_EVENTS
var AgentRailEvents = map[string]bool{
	HookEventPreToolUse:         true,
	HookEventPostToolUse:        true,
	HookEventPostToolUseFailure: true,
	HookEventStop:               true,
	HookEventPermissionRequest:  true,
	HookEventPermissionDenied:   true,
	HookEventSubagentStart:      true,
	HookEventSubagentStop:       true,
	HookEventBeforeModelCall:    true,
	HookEventAfterModelCall:     true,
}

// GatewayEvents 需要在 Gateway 层执行的事件，对齐 Python _GATEWAY_EVENTS
var GatewayEvents = map[string]bool{
	HookEventUserPromptSubmit:   true,
	HookEventSessionStart:       true,
	HookEventSessionEnd:         true,
	HookEventNotification:       true,
	HookEventConfigChange:       true,
	HookEventInstructionsLoaded: true,
	HookEventSetup:              true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsRailEvent 判断事件是否属于 AgentServer Rail 层，对齐 Python is_rail_event()
func IsRailEvent(event string) bool { return AgentRailEvents[event] }

// IsGatewayEvent 判断事件是否属于 Gateway 层，对齐 Python is_gateway_event()
func IsGatewayEvent(event string) bool { return GatewayEvents[event] }
