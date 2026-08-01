package extensions

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryHookContext 记忆钩子上下文，对齐 Python MemoryHookContext dataclass
// before_chat 扩展写入 memory_blocks，宿主从本字段读取拼接结果
type MemoryHookContext struct {
	// SessionID 会话标识
	SessionID string `json:"session_id"`
	// RequestID 请求标识
	RequestID string `json:"request_id"`
	// ChannelID 渠道标识（可选）
	ChannelID *string `json:"channel_id,omitempty"`
	// AgentName Agent 名称
	AgentName string `json:"agent_name"`
	// WorkspaceDir 工作目录
	WorkspaceDir string `json:"workspace_dir"`
	// AssistantMessage 助手消息（输出字段，可选）
	AssistantMessage *string `json:"assistant_message,omitempty"`
	// Extra 输入扩展字段
	Extra map[string]any `json:"extra,omitempty"`
	// MemoryBlocks 记忆内容块（before_chat 扩展写入，宿主读取拼接结果）
	MemoryBlocks []string `json:"memory_blocks,omitempty"`
	// Metadata 输出扩展字段
	Metadata map[string]any `json:"metadata,omitempty"`
}

// GatewayChatHookContext Gateway 聊天钩子上下文，对齐 Python GatewayChatHookContext dataclass
// 扩展可直接原地修改 Params，Gateway 会将其传给 AgentRequest.params
type GatewayChatHookContext struct {
	// RequestID 请求标识
	RequestID string `json:"request_id"`
	// ChannelID 渠道标识
	ChannelID string `json:"channel_id"`
	// SessionID 会话标识（可选）
	SessionID *string `json:"session_id,omitempty"`
	// ReqMethod 请求方法（可选）
	ReqMethod *string `json:"req_method,omitempty"`
	// Params 扩展可直接原地修改，Gateway 会将其传给 AgentRequest.params
	Params map[string]any `json:"params,omitempty"`
}

// AgentServerChatHookContext AgentServer 聊天钩子上下文，对齐 Python AgentServerChatHookContext dataclass
// 扩展可直接原地修改 Params，AgentServer 后续逻辑继续使用 request.params
type AgentServerChatHookContext struct {
	// RequestID 请求标识
	RequestID string `json:"request_id"`
	// ChannelID 渠道标识
	ChannelID string `json:"channel_id"`
	// SessionID 会话标识（可选）
	SessionID *string `json:"session_id,omitempty"`
	// ReqMethod 请求方法（可选）
	ReqMethod *string `json:"req_method,omitempty"`
	// Params 扩展可直接原地修改，AgentServer 后续逻辑继续使用 request.params
	Params map[string]any `json:"params,omitempty"`
}

// SystemPromptHookContext 系统提示词钩子上下文，对齐 Python SystemPromptHookContext dataclass
type SystemPromptHookContext struct {
	// HomeDir 扩展可设置此目录覆盖默认 home_dir（可选）
	HomeDir *string `json:"home_dir,omitempty"`
	// SkillDir 扩展可设置此目录扩展默认 skill_dir（可选）
	SkillDir *string `json:"skill_dir,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToMap 将 MemoryHookContext 转为字典，对齐 Python MemoryHookContext.to_dict()
func (c *MemoryHookContext) ToMap() map[string]any {
	result := map[string]any{
		"session_id":        c.SessionID,
		"request_id":        c.RequestID,
		"channel_id":        c.ChannelID,
		"agent_name":        c.AgentName,
		"workspace_dir":     c.WorkspaceDir,
		"assistant_message": c.AssistantMessage,
		"extra":             c.Extra,
		"memory_blocks":     c.MemoryBlocks,
		"metadata":          c.Metadata,
	}
	return result
}

// ToMap 将 GatewayChatHookContext 转为字典，对齐 Python GatewayChatHookContext.to_dict()
func (c *GatewayChatHookContext) ToMap() map[string]any {
	result := map[string]any{
		"request_id": c.RequestID,
		"channel_id": c.ChannelID,
		"session_id": c.SessionID,
		"req_method": c.ReqMethod,
		"params":     c.Params,
	}
	return result
}

// ToMap 将 AgentServerChatHookContext 转为字典，对齐 Python AgentServerChatHookContext.to_dict()
func (c *AgentServerChatHookContext) ToMap() map[string]any {
	result := map[string]any{
		"request_id": c.RequestID,
		"channel_id": c.ChannelID,
		"session_id": c.SessionID,
		"req_method": c.ReqMethod,
		"params":     c.Params,
	}
	return result
}

// ToMap 将 SystemPromptHookContext 转为字典，对齐 Python SystemPromptHookContext.to_dict()
func (c *SystemPromptHookContext) ToMap() map[string]any {
	result := map[string]any{
		"home_dir":  c.HomeDir,
		"skill_dir": c.SkillDir,
	}
	return result
}
