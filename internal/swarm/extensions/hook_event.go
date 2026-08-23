package extensions

import "github.com/uapclaw/uapclaw-go/internal/swarm/schema"

// ──────────────────────────── 结构体 ────────────────────────────

// GatewayHookEvents Gateway 侧钩子事件，对齐 Python GatewayHookEvents
// scope = "gateway"，继承 HookEventBase
type GatewayHookEvents struct {
	*schema.HookEventBase
}

// AgentServerHookEvents AgentServer 侧钩子事件，对齐 Python AgentServerHookEvents
// scope = "agent_server"，继承 HookEventBase
type AgentServerHookEvents struct {
	*schema.HookEventBase
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

// Gateway 事件常量，对齐 Python GatewayHookEvents 类属性
// 格式: scope:event_name
const (
	// GatewayStarted Gateway 启动事件，对齐 Python GATEWAY_STARTED
	GatewayStarted = "gateway:gateway_started"
	// GatewayStopped Gateway 停止事件，对齐 Python GATEWAY_STOPPED
	GatewayStopped = "gateway:gateway_stopped"
	// GatewayBeforeChatRequest Gateway 聊天请求前事件，对齐 Python BEFORE_CHAT_REQUEST
	GatewayBeforeChatRequest = "gateway:before_chat_request"
)

// AgentServer 事件常量，对齐 Python AgentServerHookEvents 类属性
// 格式: scope:event_name
const (
	// AgentServerStarted AgentServer 启动事件，对齐 Python AGENT_SERVER_STARTED
	AgentServerStarted = "agent_server:agent_server_started"
	// AgentServerStopped AgentServer 停止事件，对齐 Python AGENT_SERVER_STOPPED
	AgentServerStopped = "agent_server:agent_server_stopped"
	// AgentServerBeforeChatRequest AgentServer 聊天请求前事件，对齐 Python BEFORE_CHAT_REQUEST
	AgentServerBeforeChatRequest = "agent_server:before_chat_request"
	// AgentServerMemoryBeforeChat 记忆对话前事件，对齐 Python MEMORY_BEFORE_CHAT
	AgentServerMemoryBeforeChat = "agent_server:memory_before_chat"
	// AgentServerMemoryAfterChat 记忆对话后事件，对齐 Python MEMORY_AFTER_CHAT
	AgentServerMemoryAfterChat = "agent_server:memory_after_chat"
	// AgentServerBeforeSystemPromptBuild 系统提示词构建前事件，对齐 Python BEFORE_SYSTEM_PROMPT_BUILD
	AgentServerBeforeSystemPromptBuild = "agent_server:before_system_prompt_build"
)

// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewGatewayHookEvents 创建 Gateway 钩子事件实例，对齐 Python GatewayHookEvents(scope="gateway")
func NewGatewayHookEvents() *GatewayHookEvents {
	return &GatewayHookEvents{
		HookEventBase: &schema.HookEventBase{Scope: "gateway"},
	}
}

// NewAgentServerHookEvents 创建 AgentServer 钩子事件实例，对齐 Python AgentServerHookEvents(scope="agent_server")
func NewAgentServerHookEvents() *AgentServerHookEvents {
	return &AgentServerHookEvents{
		HookEventBase: &schema.HookEventBase{Scope: "agent_server"},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
