package schema

import (
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamModelConfig 可序列化的团队模型配置。
// 对齐 Python: TeamModelConfig (openjiuwen/agent_teams/schema/deep_agent_spec.py)
type TeamModelConfig struct {
	// ModelClientConfig 模型客户端配置
	ModelClientConfig llmschema.ModelClientConfig `json:"model_client_config"`
	// ModelRequestConfig 模型请求配置（可选）
	ModelRequestConfig *llmschema.ModelRequestConfig `json:"model_request_config,omitempty"`
}

// WorkspaceSpec 工作空间规格占位类型。
// ⤵️ 回填: 9.57
type WorkspaceSpec struct {
	// RootPath 工作空间根路径
	RootPath string `json:"root_path"`
	// Language 工作空间语言
	Language string `json:"language"`
	// StableBase 是否使用稳定基路径
	StableBase bool `json:"stable_base"`
}

// VisionModelSpec 视觉模型规格占位类型。
// 对齐 Python: VisionModelSpec
type VisionModelSpec struct {
	// APIKey API 密钥
	APIKey string `json:"api_key"`
	// BaseURL API 基础 URL
	BaseURL string `json:"base_url"`
	// Model 模型名称
	Model string `json:"model"`
	// MaxRetries 最大重试次数
	MaxRetries int `json:"max_retries"`
}

// AudioModelSpec 音频模型规格占位类型。
// 对齐 Python: AudioModelSpec
type AudioModelSpec struct {
	// APIKey API 密钥
	APIKey string `json:"api_key"`
	// BaseURL API 基础 URL
	BaseURL string `json:"base_url"`
	// TranscriptionModel 转录模型名称
	TranscriptionModel string `json:"transcription_model"`
	// QAModel 问答模型名称
	QAModel string `json:"qa_model"`
	// MaxRetries 最大重试次数
	MaxRetries int `json:"max_retries"`
	// HTTPTimeout HTTP 超时时间（秒）
	HTTPTimeout int `json:"http_timeout"`
	// MaxAudioBytes 最大音频字节数
	MaxAudioBytes int `json:"max_audio_bytes"`
	// ACRAccessKey ACR 访问密钥
	ACRAccessKey string `json:"acr_access_key"`
	// ACRAccessSecret ACR 访问密钥秘密
	ACRAccessSecret string `json:"acr_access_secret"`
	// ACRBaseURL ACR 基础 URL
	ACRBaseURL string `json:"acr_base_url"`
}

// ProgressiveToolSpec 渐进式工具规格占位类型。
// 对齐 Python: ProgressiveToolSpec
type ProgressiveToolSpec struct {
	// Enabled 是否启用渐进式工具
	Enabled bool `json:"enabled"`
	// AlwaysVisibleTools 始终可见的工具列表
	AlwaysVisibleTools []string `json:"always_visible_tools,omitempty"`
	// DefaultVisibleTools 默认可见的工具列表
	DefaultVisibleTools []string `json:"default_visible_tools,omitempty"`
	// MaxLoadedTools 最大加载工具数
	MaxLoadedTools int `json:"max_loaded_tools"`
}

// SysOperationSpec 系统操作规格占位类型。
// 对齐 Python: SysOperationSpec
type SysOperationSpec struct {
	// ID 操作标识
	ID string `json:"id"`
	// Mode 操作模式
	Mode string `json:"mode"`
	// WorkConfig 工作配置
	// ⤵️ 回填: LocalWorkConfig 类型就绪后替换 any
	WorkConfig any `json:"work_config,omitempty"`
	// GatewayConfig 网关配置
	// ⤵️ 回填: SandboxGatewayConfig 类型就绪后替换 any
	GatewayConfig any `json:"gateway_config,omitempty"`
}

// RailSpec 约束规则规格占位类型。
// 对齐 Python: RailSpec
type RailSpec struct {
	// Type 规则类型
	Type string `json:"type"`
	// Params 规则参数
	Params map[string]any `json:"params,omitempty"`
}

// BuiltinToolSpec 内置工具规格占位类型。
// 对齐 Python: BuiltinToolSpec
type BuiltinToolSpec struct {
	// Type 工具类型
	Type string `json:"type"`
	// Params 工具参数
	Params map[string]any `json:"params,omitempty"`
}

// SubAgentSpec 子代理规格占位类型。
// 对齐 Python: SubAgentSpec
type SubAgentSpec struct {
	// AgentCard 代理身份卡片
	AgentCard any `json:"agent_card"`
	// SystemPrompt 系统提示词
	SystemPrompt string `json:"system_prompt"`
	// Tools 工具列表
	// 对齐 Python: tools: list[ToolCard | BuiltinToolSpec] = []
	Tools any `json:"tools"`
	// Mcps MCP 服务器列表
	// 对齐 Python: mcps: list[McpServerConfig] = []
	Mcps any `json:"mcps"`
	// Model 模型配置
	// 对齐 Python: model: Optional[TeamModelConfig] = None
	Model *TeamModelConfig `json:"model,omitempty"`
	// Rails 约束规则列表
	// 对齐 Python: rails: Optional[list[RailSpec]] = None
	// ⤵️ 回填: RailSpec 类型就绪后替换 any
	Rails any `json:"rails,omitempty"`
	// Skills 技能列表
	// 对齐 Python: skills: Optional[list[str]] = None
	Skills []string `json:"skills,omitempty"`
	// Workspace 工作空间规格
	// 对齐 Python: workspace: Optional[WorkspaceSpec] = None
	Workspace *WorkspaceSpec `json:"workspace,omitempty"`
	// SysOperation 系统操作规格
	// 对齐 Python: sys_operation: Optional[SysOperationSpec] = None
	SysOperation *SysOperationSpec `json:"sys_operation,omitempty"`
	// Language 语言偏好
	// 对齐 Python: language: Optional[str] = None
	Language string `json:"language,omitempty"`
	// PromptMode 提示模式
	// 对齐 Python: prompt_mode: Optional[str] = None
	PromptMode string `json:"prompt_mode,omitempty"`
	// EnableTaskLoop 是否启用任务循环
	// 对齐 Python: enable_task_loop: bool = False
	EnableTaskLoop bool `json:"enable_task_loop"`
	// MaxIterations 最大迭代次数
	// 对齐 Python: max_iterations: Optional[int] = None
	MaxIterations *int `json:"max_iterations,omitempty"`
	// FactoryName 工厂名称
	// 对齐 Python: factory_name: Optional[str] = None
	FactoryName string `json:"factory_name,omitempty"`
	// FactoryKwargs 工厂参数
	// 对齐 Python: factory_kwargs: dict[str, Any] = {}
	FactoryKwargs map[string]any `json:"factory_kwargs"`
}

// DeepAgentSpec 单角色 DeepAgent 规格。
// 对齐 Python: DeepAgentSpec
type DeepAgentSpec struct {
	// Model 模型配置
	Model *TeamModelConfig `json:"model,omitempty"`
	// Card 代理身份卡片
	Card *agentschema.AgentCard `json:"card,omitempty"`
	// SystemPrompt 系统提示词
	SystemPrompt string `json:"system_prompt,omitempty"`
	// Tools 工具列表
	Tools []any `json:"tools,omitempty"`
	// Mcps MCP 服务器列表
	Mcps []any `json:"mcps,omitempty"`
	// Subagents 子代理列表
	Subagents []any `json:"subagents,omitempty"`
	// Rails 约束规则列表
	Rails []any `json:"rails,omitempty"`
	// EnableTaskLoop 是否启用任务循环
	EnableTaskLoop bool `json:"enable_task_loop"`
	// EnableAsyncSubagent 是否启用异步子代理
	EnableAsyncSubagent bool `json:"enable_async_subagent"`
	// AddGeneralPurposeAgent 是否添加通用代理
	AddGeneralPurposeAgent bool `json:"add_general_purpose_agent"`
	// MaxIterations 最大迭代次数
	MaxIterations int `json:"max_iterations"`
	// Workspace 工作空间规格
	Workspace *WorkspaceSpec `json:"workspace,omitempty"`
	// Skills 技能列表
	Skills []string `json:"skills,omitempty"`
	// EnableSkillDiscovery 是否启用技能发现
	EnableSkillDiscovery bool `json:"enable_skill_discovery"`
	// SysOperation 系统操作规格
	SysOperation *SysOperationSpec `json:"sys_operation,omitempty"`
	// Language 语言偏好
	Language string `json:"language,omitempty"`
	// PromptMode 提示模式
	PromptMode string `json:"prompt_mode,omitempty"`
	// VisionModel 视觉模型规格
	VisionModel *VisionModelSpec `json:"vision_model,omitempty"`
	// AudioModel 音频模型规格
	AudioModel *AudioModelSpec `json:"audio_model,omitempty"`
	// EnableTaskPlanning 是否启用任务规划
	EnableTaskPlanning bool `json:"enable_task_planning"`
	// RestrictToSandbox 是否限制在沙箱内
	RestrictToSandbox bool `json:"restrict_to_sandbox"`
	// AutoCreateWorkspace 是否自动创建工作空间
	AutoCreateWorkspace bool `json:"auto_create_workspace"`
	// CompletionTimeout 完成超时时间（秒）
	CompletionTimeout float64 `json:"completion_timeout"`
	// ProgressiveTool 渐进式工具规格
	ProgressiveTool *ProgressiveToolSpec `json:"progressive_tool,omitempty"`
	// ApprovalRequiredTools 需审批的工具列表
	ApprovalRequiredTools []string `json:"approval_required_tools,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamModelConfig 创建默认 TeamModelConfig。
func NewTeamModelConfig() TeamModelConfig {
	return TeamModelConfig{
		ModelClientConfig:  *llmschema.NewModelClientConfig("", "", ""),
		ModelRequestConfig: llmschema.NewModelRequestConfig(),
	}
}

// Build 构建团队模型配置。⤵️ 回填: 9.57
func (c TeamModelConfig) Build() (any, error) { return nil, nil }

// NewDeepAgentSpec 创建默认 DeepAgentSpec。
func NewDeepAgentSpec() DeepAgentSpec {
	return DeepAgentSpec{MaxIterations: 15, AutoCreateWorkspace: true, CompletionTimeout: 600.0}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
