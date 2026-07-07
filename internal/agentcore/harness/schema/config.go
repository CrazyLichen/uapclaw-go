package schema

import (
	"os"
	"strconv"
	"strings"

	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	security "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	workspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	schema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SubagentSpec 子 Agent 规格接口。
// 允许 *SubAgentConfig 和 *DeepAgent 以统一类型返回。
// 对齐 Python: _find_subagent_spec 返回 Optional[SubAgentConfig | DeepAgent]。
// 放在 schema 包以避免 schema↔interfaces 循环依赖。
type SubagentSpec interface {
	// SpecName 返回规格名称，用于匹配 subagent_type。
	SpecName() string
}

// VisionModelConfig 视觉模型运行时配置
type VisionModelConfig struct {
	// APIKey API 密钥
	APIKey string `json:"api_key"`
	// BaseURL API 基础地址
	BaseURL string `json:"base_url"`
	// Model 模型名称
	Model string `json:"model"`
	// MaxRetries 最大重试次数
	MaxRetries int `json:"max_retries"`
}

// AudioModelConfig 音频模型运行时配置
type AudioModelConfig struct {
	// APIKey API 密钥
	APIKey string `json:"api_key"`
	// BaseURL API 基础地址
	BaseURL string `json:"base_url"`
	// TranscriptionModel 语音转录模型名称
	TranscriptionModel string `json:"transcription_model"`
	// QAModel 语音问答模型名称
	QAModel string `json:"question_answering_model"`
	// MaxRetries 最大重试次数
	MaxRetries int `json:"max_retries"`
	// HTTPTimeout HTTP 请求超时秒数
	HTTPTimeout int `json:"http_timeout"`
	// MaxAudioBytes 最大音频字节数
	MaxAudioBytes int `json:"max_audio_bytes"`
	// ACRAccessKey ACR Cloud 访问密钥
	ACRAccessKey string `json:"acr_access_key"`
	// ACRAccessSecret ACR Cloud 访问密钥
	ACRAccessSecret string `json:"acr_access_secret"`
	// ACRBaseURL ACR Cloud 基础地址
	ACRBaseURL string `json:"acr_base_url"`
}

// ModelSelectionEntry 模型选择条目，替代 Python Dict[Model, str]
type ModelSelectionEntry struct {
	// Model 模型实例
	Model *llm.Model `json:"model,omitempty"`
	// ModeName 模式名称
	ModeName string `json:"mode_name"`
}

// SubAgentConfig 子 Agent 配置
type SubAgentConfig struct {
	// AgentCard Agent 身份卡片
	AgentCard *schema.AgentCard `json:"agent_card,omitempty"`
	// SystemPrompt 系统提示词
	SystemPrompt string `json:"system_prompt"`
	// Tools 工具卡片列表
	Tools []*tool.ToolCard `json:"tools,omitempty"`
	// Mcps MCP 服务器配置列表
	Mcps []*mcptypes.McpServerConfig `json:"mcps,omitempty"`
	// Model 模型实例
	Model *llm.Model `json:"model,omitempty"`
	// Rails Rail 列表
	Rails []sainterfaces.AgentRail `json:"-"`
	// Skills 技能名称列表
	Skills []string `json:"skills,omitempty"`
	// Backend 后端协议实例（any 占位，P2 预留，等 Backend 实现时回填）
	Backend any `json:"-"`
	// Workspace 工作空间
	Workspace *workspace.Workspace `json:"workspace,omitempty"`
	// SysOperation 系统操作实例
	SysOperation sysop.SysOperation `json:"-"`
	// Language 语言
	Language string `json:"language,omitempty"`
	// PromptMode 提示词模式
	PromptMode PromptMode `json:"prompt_mode,omitempty"`
	// EnableTaskLoop 是否启用任务循环
	EnableTaskLoop bool `json:"enable_task_loop"`
	// MaxIterations 最大迭代次数
	MaxIterations int `json:"max_iterations,omitempty"`
	// FactoryName 工厂名称
	FactoryName string `json:"factory_name,omitempty"`
	// FactoryKwargs 工厂参数（any 占位，Python 原型为 Dict[str, Any]）
	FactoryKwargs map[string]any `json:"factory_kwargs,omitempty"`
	// EnablePlanMode 是否启用规划模式
	EnablePlanMode bool `json:"enable_plan_mode"`
	// RestrictToWorkDir 是否限制在工作目录
	RestrictToWorkDir bool `json:"restrict_to_work_dir"`
}

// SubagentCreateParams 子 Agent 创建参数。
// 对齐 Python: DeepAgent.create_subagent 中 create_kwargs 字典。
// 替代 map[string]any，提供类型安全的参数传递。
type SubagentCreateParams struct {
	// Model 模型实例
	Model *llm.Model
	// Card Agent 身份卡片
	Card *schema.AgentCard
	// SystemPrompt 系统提示词
	SystemPrompt string
	// Tools 工具卡片列表
	Tools []*tool.ToolCard
	// Mcps MCP 服务器配置列表
	Mcps []*mcptypes.McpServerConfig
	// Rails Rail 列表
	Rails []sainterfaces.AgentRail
	// EnableTaskLoop 是否启用任务循环
	EnableTaskLoop bool
	// MaxIterations 最大迭代次数
	MaxIterations int
	// Workspace 工作空间
	Workspace *workspace.Workspace
	// Skills 技能名称列表
	Skills []string
	// Backend 后端协议实例（any 占位，P2 预留，等 Backend 实现时回填）
	Backend any
	// SysOperation 系统操作实例
	SysOperation sysop.SysOperation
	// Language 语言
	Language string
	// PromptMode 提示词模式
	PromptMode PromptMode
	// Subagents 子 Agent 列表（创建时为 nil）
	Subagents []SubAgentConfig
	// EnableAsyncSubagent 是否启用异步子 Agent
	EnableAsyncSubagent bool
	// AddGeneralPurposeAgent 是否添加通用 Agent
	AddGeneralPurposeAgent bool
	// EnablePlanMode 是否启用规划模式
	EnablePlanMode bool
	// RestrictToWorkDir 是否限制在工作目录
	RestrictToWorkDir bool
}

// DeepAgentConfig DeepAgent 运行时配置中枢
type DeepAgentConfig struct {
	// Model 预构建的 LLM 模型实例
	Model *llm.Model `json:"model,omitempty"`
	// Card Agent 身份卡片
	Card *schema.AgentCard `json:"card,omitempty"`
	// SystemPrompt 注入到 ReAct Agent 的系统提示词
	SystemPrompt string `json:"system_prompt,omitempty"`
	// ContextEngineConfig 上下文引擎配置
	ContextEngineConfig *ceschema.ContextEngineConfig `json:"context_engine_config,omitempty"`
	// EnableTaskLoop 是否启用外层任务循环
	EnableTaskLoop bool `json:"enable_task_loop"`
	// EnableAsyncSubagent 是否启用异步子 Agent 模式
	EnableAsyncSubagent bool `json:"enable_async_subagent"`
	// AddGeneralPurposeAgent 是否添加通用目的 Agent 作为子 Agent
	AddGeneralPurposeAgent bool `json:"add_general_purpose_agent"`
	// MaxIterations 单次调用最大 ReAct 迭代次数，0 表示使用默认值 15
	MaxIterations int `json:"max_iterations,omitempty"`
	// Subagents 子 Agent 规格列表，支持 *SubAgentConfig 和 *DeepAgent
	// 对齐 Python: subagents: Optional[List[SubAgentConfig | DeepAgent]] = None
	Subagents []SubagentSpec `json:"-"`
	// Tools 挂载到 Agent 的工具卡片
	Tools []*tool.ToolCard `json:"tools,omitempty"`
	// Mcps 挂载到 Agent 的 MCP 服务器配置
	Mcps []*mcptypes.McpServerConfig `json:"mcps,omitempty"`
	// Workspace 工作空间
	Workspace *workspace.Workspace `json:"workspace,omitempty"`
	// Skills 技能定义列表
	Skills []string `json:"skills,omitempty"`
	// EnableSkillDiscovery 是否启用技能发现
	EnableSkillDiscovery bool `json:"enable_skill_discovery"`
	// Backend 后端协议实例（any 占位，P2 预留，等 Backend 实现时回填）
	Backend any `json:"-"`
	// SysOperation 系统操作实例
	SysOperation sysop.SysOperation `json:"-"`
	// AutoCreateWorkspace 是否自动创建工作空间
	AutoCreateWorkspace bool `json:"auto_create_workspace"`
	// CompletionTimeout 单次任务循环迭代完成超时秒数，0 表示使用默认值 600.0
	CompletionTimeout float64 `json:"completion_timeout,omitempty"`
	// Language 语言，空表示使用默认值 "cn"
	Language string `json:"language,omitempty"`
	// PromptMode 提示词注入模式
	PromptMode PromptMode `json:"prompt_mode,omitempty"`
	// VisionModelConfig 视觉模型配置
	VisionModelConfig *VisionModelConfig `json:"vision_model_config,omitempty"`
	// AudioModelConfig 音频模型配置
	AudioModelConfig *AudioModelConfig `json:"audio_model_config,omitempty"`
	// EnableReadImageMultimodal 是否启用图片多模态读取
	EnableReadImageMultimodal bool `json:"enable_read_image_multimodal"`
	// Rails Rail 列表
	Rails []sainterfaces.AgentRail `json:"-"`
	// EnablePlanMode 是否启用规划模式
	EnablePlanMode bool `json:"enable_plan_mode"`
	// ModelSelection 模型选择列表
	ModelSelection []ModelSelectionEntry `json:"model_selection,omitempty"`
	// ProgressiveToolEnabled 是否启用渐进式工具暴露
	ProgressiveToolEnabled bool `json:"progressive_tool_enabled"`
	// ProgressiveToolAlwaysVisibleTools 渐进式工具中始终可见的工具列表
	ProgressiveToolAlwaysVisibleTools []string `json:"progressive_tool_always_visible_tools,omitempty"`
	// ProgressiveToolDefaultVisibleTools 渐进式工具中默认可见的工具列表
	ProgressiveToolDefaultVisibleTools []string `json:"progressive_tool_default_visible_tools,omitempty"`
	// ProgressiveToolMaxLoadedTools 渐进式工具最大加载工具数，0 表示使用默认值 12
	ProgressiveToolMaxLoadedTools int `json:"progressive_tool_max_loaded_tools,omitempty"`
	// DefaultMode 默认 Agent 执行模式
	DefaultMode AgentMode `json:"default_mode,omitempty"`
	// Permissions 权限策略配置
	Permissions *security.PermissionsSection `json:"permissions,omitempty"`
	// PermissionHost 权限宿主回调（any 占位，⤵️ 9.1 回填为 PermissionHostCallback 接口）
	PermissionHost any `json:"-"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultMaxIterations 默认最大迭代次数
	DefaultMaxIterations = 15
	// DefaultCompletionTimeout 默认完成超时秒数
	DefaultCompletionTimeout = 600.0
	// DefaultProgressiveToolMax 默认渐进式工具最大加载数
	DefaultProgressiveToolMax = 12
	// DefaultLanguage 默认语言
	DefaultLanguage = "cn"
	// DefaultOpenAIBaseURL 默认 OpenAI 基础地址
	DefaultOpenAIBaseURL = "https://api.openai.com/v1"
	// DefaultOpenAIVisionModel 默认 OpenAI 视觉模型
	DefaultOpenAIVisionModel = "gpt-4.1-mini"
	// DefaultOpenRouterVisionModel 默认 OpenRouter 视觉模型
	DefaultOpenRouterVisionModel = "google/gemini-2.5-pro"
	// DefaultOpenAIAudioTranscriptionModel 默认 OpenAI 语音转录模型
	DefaultOpenAIAudioTranscriptionModel = "gpt-4o-transcribe"
	// DefaultOpenAIAudioQAModel 默认 OpenAI 语音问答模型
	DefaultOpenAIAudioQAModel = "gpt-4o-audio-preview"
	// DefaultACRBaseURL 默认 ACR Cloud 基础地址
	DefaultACRBaseURL = "https://identify-ap-southeast-1.acrcloud.com/v1/identify"
	// DefaultAudioHTTPTimeout 默认音频 HTTP 超时秒数
	DefaultAudioHTTPTimeout = 20
	// DefaultMaxAudioBytes 默认最大音频字节数
	DefaultMaxAudioBytes = 25 * 1024 * 1024
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SpecName 返回规格名称，用于子 Agent 匹配。
// 实现 SubagentSpec 接口。
// 对齐 Python: isinstance(spec, SubAgentConfig) 时通过 spec.agent_card.name 匹配。
func (c *SubAgentConfig) SpecName() string {
	if c.AgentCard == nil {
		return ""
	}
	return c.AgentCard.Name
}

// NewVisionModelConfig 创建带默认值的视觉模型配置
func NewVisionModelConfig() *VisionModelConfig {
	return &VisionModelConfig{
		BaseURL:    DefaultOpenAIBaseURL,
		Model:      DefaultOpenAIVisionModel,
		MaxRetries: 3,
	}
}

// FromEnv 从环境变量构建视觉模型配置
func (VisionModelConfig) FromEnv() VisionModelConfig {
	apiKey := envOr(
		"VISION_API_KEY",
		"OPENROUTER_API_KEY",
		"OPENAI_API_KEY",
	)

	baseURL := envOr(
		"VISION_BASE_URL",
		"VISION_API_BASE",
		"OPENROUTER_BASE_URL",
		"OPENAI_BASE_URL",
	)
	if baseURL == "" {
		baseURL = DefaultOpenAIBaseURL
	}

	model := os.Getenv("VISION_MODEL")
	if model == "" {
		model = os.Getenv("VISION_MODEL_NAME")
	}
	if model == "" {
		if containsOpenRouter(baseURL) {
			model = DefaultOpenRouterVisionModel
		} else {
			model = DefaultOpenAIVisionModel
		}
	}

	return VisionModelConfig{
		APIKey:     apiKey,
		BaseURL:    baseURL,
		Model:      model,
		MaxRetries: parseIntEnv("VISION_MAX_RETRIES", 3),
	}
}

// NewAudioModelConfig 创建带默认值的音频模型配置
func NewAudioModelConfig() *AudioModelConfig {
	return &AudioModelConfig{
		BaseURL:            DefaultOpenAIBaseURL,
		TranscriptionModel: DefaultOpenAIAudioTranscriptionModel,
		QAModel:            DefaultOpenAIAudioQAModel,
		MaxRetries:         3,
		HTTPTimeout:        DefaultAudioHTTPTimeout,
		MaxAudioBytes:      DefaultMaxAudioBytes,
		ACRBaseURL:         DefaultACRBaseURL,
	}
}

// FromEnv 从环境变量构建音频模型配置
func (AudioModelConfig) FromEnv() AudioModelConfig {
	baseURL := envOr(
		"AUDIO_BASE_URL",
		"AUDIO_API_BASE",
		"OPENAI_BASE_URL",
	)
	if baseURL == "" {
		baseURL = DefaultOpenAIBaseURL
	}

	return AudioModelConfig{
		APIKey: envOr(
			"AUDIO_API_KEY",
			"OPENAI_API_KEY",
		),
		BaseURL: baseURL,
		TranscriptionModel: envOrWithDefault(
			DefaultOpenAIAudioTranscriptionModel,
			"AUDIO_TRANSCRIPTION_MODEL",
			"AUDIO_MODEL_NAME",
		),
		QAModel: envOrWithDefault(
			DefaultOpenAIAudioQAModel,
			"AUDIO_QUESTION_ANSWERING_MODEL",
		),
		MaxRetries:      parseIntEnv("AUDIO_MAX_RETRIES", 3),
		HTTPTimeout:     parseIntEnv("AUDIO_HTTP_TIMEOUT", DefaultAudioHTTPTimeout),
		MaxAudioBytes:   parseIntEnv("AUDIO_MAX_AUDIO_BYTES", DefaultMaxAudioBytes),
		ACRAccessKey:    os.Getenv("ACR_ACCESS_KEY"),
		ACRAccessSecret: os.Getenv("ACR_ACCESS_SECRET"),
		ACRBaseURL:      envOrDefault("ACR_BASE_URL", DefaultACRBaseURL),
	}
}

// NewDeepAgentConfig 创建带默认值的 DeepAgent 配置
// 对齐 Python: DeepAgentConfig 字段默认值（max_iterations=15, completion_timeout=600.0 等）
func NewDeepAgentConfig() *DeepAgentConfig {
	return &DeepAgentConfig{
		AutoCreateWorkspace:       true,
		EnableReadImageMultimodal: true,
		MaxIterations:             DefaultMaxIterations,
		CompletionTimeout:         DefaultCompletionTimeout,
		ProgressiveToolMaxLoadedTools: DefaultProgressiveToolMax,
		Language:                  DefaultLanguage,
	}
}

// EffectiveMaxIterations 返回有效的最大迭代次数，0 取默认值
func (c *DeepAgentConfig) EffectiveMaxIterations() int {
	if c.MaxIterations == 0 {
		return DefaultMaxIterations
	}
	return c.MaxIterations
}

// EffectiveCompletionTimeout 返回有效的完成超时秒数，0 取默认值
func (c *DeepAgentConfig) EffectiveCompletionTimeout() float64 {
	if c.CompletionTimeout == 0 {
		return DefaultCompletionTimeout
	}
	return c.CompletionTimeout
}

// EffectiveLanguage 返回有效的语言，空取默认值
func (c *DeepAgentConfig) EffectiveLanguage() string {
	if c.Language == "" {
		return DefaultLanguage
	}
	return c.Language
}

// EffectiveProgressiveToolMaxLoadedTools 返回有效的渐进式工具最大加载数，0 取默认值
func (c *DeepAgentConfig) EffectiveProgressiveToolMaxLoadedTools() int {
	if c.ProgressiveToolMaxLoadedTools == 0 {
		return DefaultProgressiveToolMax
	}
	return c.ProgressiveToolMaxLoadedTools
}

// EffectiveRestrictToWorkDir 返回有效的 RestrictToWorkDir 值
// Python 默认值为 True，Go 通过 NewSubAgentConfig 构造函数设置默认值
func (c *SubAgentConfig) EffectiveRestrictToWorkDir() bool {
	return c.RestrictToWorkDir
}

// NewSubAgentConfig 创建子 Agent 配置
// Python 中 restrict_to_work_dir 默认为 True，Go 零值为 false，需显式设置
func NewSubAgentConfig() *SubAgentConfig {
	return &SubAgentConfig{
		RestrictToWorkDir: true,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// envOr 依次检查多个环境变量，返回第一个非空值，均空则返回空字符串
func envOr(names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}

// envOrDefault 读取环境变量，空则返回默认值
func envOrDefault(name string, defaultVal string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return defaultVal
}

// envOrWithDefault 依次检查多个环境变量，均空则返回默认值
func envOrWithDefault(defaultVal string, names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return defaultVal
}

// parseIntEnv 从环境变量解析整数，失败或空则返回默认值
func parseIntEnv(name string, defaultVal int) int {
	v := os.Getenv(name)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
}

// containsOpenRouter 判断 URL 是否包含 openrouter.ai
func containsOpenRouter(url string) bool {
	return strings.Contains(url, "openrouter.ai")
}
