package schema

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// buildRailsFromSpecs 逐条解析 rail 声明为实例列表（未知类型跳过，nil 过滤）。
// Python: deep_agent_spec.py:269-278 SubAgentSpec.build 的 rails 段 +
// extension_resolver.py:189-195 _as_built_list
func buildRailsFromSpecs(
	specs []RailSpec,
	language string,
	ws *workspace.Workspace,
	ctx BuildContextInterface,
) ([]sainterfaces.AgentRail, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	var rails []sainterfaces.AgentRail
	for i := range specs {
		built, err := specs[i].Build(language, ws, ctx)
		if err != nil {
			return nil, err
		}
		if rail, ok := built.(sainterfaces.AgentRail); ok {
			rails = append(rails, rail)
		}
	}
	return rails, nil
}

// ──────────────────────────── 结构体 ────────────────────────────

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

// Build 构建工作空间实例。
// Python: deep_agent_spec.py:193-196 WorkspaceSpec.build
//
// root_path 空时回退 "./"；stable_base 的锚定改写由调用方负责（上游同语义）。
func (s *WorkspaceSpec) Build() *workspace.Workspace {
	rootPath := s.RootPath
	if rootPath == "" {
		rootPath = "./"
	}
	return &workspace.Workspace{RootPath: rootPath, Language: s.Language}
}

// VisionModelSpec 视觉模型规格占位类型。
// Python: VisionModelSpec
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
// Python: AudioModelSpec
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
// Python: ProgressiveToolSpec
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
// Python: SysOperationSpec
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

// BuildCard 构建系统操作卡片。
// Python: deep_agent_spec.py:216-222 SysOperationSpec.build_card
func (s *SysOperationSpec) BuildCard() (*sysop.SysOperationCard, error) {
	var mode sysop.OperationMode
	switch s.Mode {
	case "local", "":
		mode = sysop.OperationModeLocal
	case "sandbox":
		mode = sysop.OperationModeSandbox
	default:
		return nil, fmt.Errorf("unknown sys_operation mode %q", s.Mode)
	}
	opts := []sysop.SysOperationCardOption{sysop.WithSysOpMode(mode)}
	if s.ID != "" {
		opts = append(opts, sysop.WithSysOpCustomID(s.ID))
	}
	// 运行配置透传：Local 用 WorkConfig，Sandbox 用 GatewayConfig
	switch mode {
	case sysop.OperationModeLocal:
		if cfg, ok := s.WorkConfig.(*sysop.LocalWorkConfig); ok {
			opts = append(opts, sysop.WithSysOpWorkConfig(cfg))
		}
	case sysop.OperationModeSandbox:
		if cfg, ok := s.GatewayConfig.(*sysop.SandboxGatewayConfig); ok {
			opts = append(opts, sysop.WithSysOpGatewayConfig(cfg))
		}
	}
	return sysop.NewSysOperationCard(opts...), nil
}

// Resolve 解析成活的 SysOperation 实例。
// Python: deep_agent_spec.py:224-249 SysOperationSpec.resolve
//
// ⚠️ 已知偏差：Python 此处经 Runner.resource_mgr 做 get-or-create（幂等，避免
// 会话恢复时重复注册），Go 侧既有实现（原 resolve_spec_sysop.go）直接构造新
// 实例、未接 resource_mgr——该偏差自 9.x 起即存在，本批保持同语义并登记，
// 待 resource_mgr 接线统一回填。
func (s *SysOperationSpec) Resolve() (sysop.SysOperation, error) {
	card, err := s.BuildCard()
	if err != nil {
		return nil, err
	}
	return sysop.NewSysOperation(card)
}

// SubAgentSpec 子代理规格占位类型。
// Python: SubAgentSpec
type SubAgentSpec struct {
	// AgentCard 代理身份卡片
	AgentCard *agentschema.AgentCard `json:"agent_card"`
	// SystemPrompt 系统提示词
	SystemPrompt string `json:"system_prompt"`
	// ToolCards 工具卡片列表
	ToolCards []*tool.ToolCard `json:"tool_cards,omitempty"`
	// BuiltinTools 内置工具声明列表
	BuiltinTools []BuiltinToolSpec `json:"builtin_tools,omitempty"`
	// Mcps MCP 服务器列表
	Mcps []*mcptypes.McpServerConfig `json:"mcps,omitempty"`
	// Model 模型配置
	Model *TeamModelConfig `json:"model,omitempty"`
	// Rails 约束规则列表
	Rails []RailSpec `json:"rails,omitempty"`
	// Skills 技能列表
	Skills []string `json:"skills,omitempty"`
	// Workspace 工作空间规格
	Workspace *WorkspaceSpec `json:"workspace,omitempty"`
	// SysOperation 系统操作规格
	SysOperation *SysOperationSpec `json:"sys_operation,omitempty"`
	// Language 语言偏好
	Language string `json:"language,omitempty"`
	// PromptMode 提示模式
	PromptMode string `json:"prompt_mode,omitempty"`
	// EnableTaskLoop 是否启用任务循环
	EnableTaskLoop bool `json:"enable_task_loop"`
	// MaxIterations 最大迭代次数
	MaxIterations *int `json:"max_iterations,omitempty"`
	// FactoryName 工厂名称
	FactoryName string `json:"factory_name,omitempty"`
	// FactoryKwargs 工厂参数
	FactoryKwargs map[string]any `json:"factory_kwargs"`
}

// Build 物化成运行时 SubAgentConfig。
// Python: deep_agent_spec.py:254-330 SubAgentSpec.build
//
// factory_name 命中注册表时直接交 provider 构造（:383-384）；未命中
// 落到正常路径（对齐 Python if 条件不成立的语义）。
func (s *SubAgentSpec) Build(
	parentModel *llm.Model,
	language string,
	ctx BuildContextInterface,
) (*SubAgentConfig, error) {
	// Python: if self.factory_name and self.factory_name in _SUBAGENT_PROVIDER_REGISTRY:
	//   return _SUBAGENT_PROVIDER_REGISTRY[self.factory_name](
	//       dict(self.factory_kwargs), context)（:383-384）
	if s.FactoryName != "" {
		if provider, ok := LookupSubagentProvider(s.FactoryName); ok {
			return provider(s.FactoryKwargs, ctx)
		}
		// 未命中 → 继续正常路径（对齐 Python if 条件不成立）
	}

	// Python: resolved_model = self.model.build() if self.model else None
	var resolvedModel *llm.Model
	if s.Model != nil {
		resolvedModel = s.Model.Build()
	}

	// Python: resolved_workspace = self.workspace.build() if self.workspace else None
	var resolvedWorkspace *workspace.Workspace
	if s.Workspace != nil {
		resolvedWorkspace = s.Workspace.Build()
	}

	// Python: resolved_rails = [rail_spec.build(...) for rail_spec in self.rails]（:270-278）
	resolvedRails, err := buildRailsFromSpecs(s.Rails, language, resolvedWorkspace, ctx)
	if err != nil {
		return nil, err
	}

	// Python: resolved_sys_op = self.sys_operation.resolve() if self.sys_operation else None
	var resolvedSysOp sysop.SysOperation
	if s.SysOperation != nil {
		resolvedSysOp, err = s.SysOperation.Resolve()
		if err != nil {
			return nil, err
		}
	}

	// Python: 工具解析——BuiltinToolSpec → Tool 实例，ToolCard 透传（:282-291）
	saPrefix := ""
	if s.AgentCard != nil {
		saPrefix = s.AgentCard.ID
		if saPrefix == "" {
			saPrefix = s.AgentCard.Name
		}
	}
	var toolCards []*tool.ToolCard
	var toolInstances []tool.Tool
	for i := range s.BuiltinTools {
		item := &s.BuiltinTools[i]
		tid := fmt.Sprintf("%s.%s", saPrefix, item.Type)
		built, err := item.Build(language, tid, ctx)
		if err != nil {
			return nil, err
		}
		switch typed := built.(type) {
		case nil:
		case tool.Tool:
			toolInstances = append(toolInstances, typed)
		case *tool.ToolCard:
			toolCards = append(toolCards, typed)
		default:
			return nil, fmt.Errorf("builtin tool spec %q built unexpected type %T", item.Type, built)
		}
	}
	toolCards = append(toolCards, s.ToolCards...)

	// Python: prompt_mode 透传
	promptMode := PromptModeFull
	if s.PromptMode != "" {
		parsed, parseErr := ParsePromptMode(s.PromptMode)
		if parseErr != nil {
			return nil, fmt.Errorf("subagent prompt_mode: %w", parseErr)
		}
		promptMode = parsed
	}

	maxIterations := 0
	if s.MaxIterations != nil {
		maxIterations = *s.MaxIterations
	}

	return &SubAgentConfig{
		AgentCard:      s.AgentCard,
		SystemPrompt:   s.SystemPrompt,
		Tools:          toolCards,
		ToolInstances:  toolInstances,
		Mcps:           s.Mcps,
		Model:          resolvedModel,
		Rails:          resolvedRails,
		Skills:         s.Skills,
		Workspace:      resolvedWorkspace,
		SysOperation:   resolvedSysOp,
		Language:       s.Language,
		PromptMode:     promptMode,
		EnableTaskLoop: s.EnableTaskLoop,
		MaxIterations:  maxIterations,
		FactoryName:    s.FactoryName,
		FactoryKwargs:  s.FactoryKwargs,
	}, nil
}

// DeepAgentSpec 单角色 DeepAgent 规格。
// Python: DeepAgentSpec
type DeepAgentSpec struct {
	// Model 模型配置
	Model *TeamModelConfig `json:"model,omitempty"`
	// Card 代理身份卡片
	Card *agentschema.AgentCard `json:"card,omitempty"`
	// SystemPrompt 系统提示词
	SystemPrompt string `json:"system_prompt,omitempty"`
	// ToolCards 工具卡片列表
	ToolCards []*tool.ToolCard `json:"tool_cards,omitempty"`
	// BuiltinTools 内置工具声明列表
	BuiltinTools []BuiltinToolSpec `json:"builtin_tools,omitempty"`
	// Mcps MCP 服务器列表
	Mcps []*mcptypes.McpServerConfig `json:"mcps,omitempty"`
	// Subagents 子代理列表
	Subagents []SubAgentSpec `json:"subagents,omitempty"`
	// Rails 约束规则列表
	Rails []RailSpec `json:"rails,omitempty"`
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
	// Cwd 成员工作目录。
	// Python: DeepAgentSpec.cwd (deep_agent_spec.py:456)
	Cwd string `json:"cwd,omitempty"`
	// ProjectRoot 项目身份锚点，缺省取 cwd。
	// Python: DeepAgentSpec.project_root (deep_agent_spec.py:458-459)
	ProjectRoot string `json:"project_root,omitempty"`
	// KvCacheAffinityConfig KV 缓存亲和策略（assembly 期由 config_specs 烘焙）。
	// Python: DeepAgentSpec.kv_cache_affinity_config (deep_agent_spec.py:494)
	KvCacheAffinityConfig *kv_cache.KVCacheAffinityConfig `json:"kv_cache_affinity_config,omitempty"`
	// ProgressiveTool 渐进式工具规格
	ProgressiveTool *ProgressiveToolSpec `json:"progressive_tool,omitempty"`
	// ApprovalRequiredTools 需审批的工具列表
	ApprovalRequiredTools []string `json:"approval_required_tools,omitempty"`
	// AgentTemplateSpec 能力快照：模板去掉 prompt_sections 后的整体形态，
	// 由 NativeHarness 在成员运行前重新校验并热挂载（persona 走成员 prompt lane，
	// 能力走本快照，二者不混）。字段可 JSON 序列化，使 TeamAgentSpec 跨成员
	// spawn 与恢复边界持久化。
	// 已随 15.14 从 map[string]any 占位收敛为类型（原 ⤵️ 15.7-15.9 哨兵消除）。
	// Python: DeepAgentSpec.agent_template_spec (deep_agent_spec.py:496)
	AgentTemplateSpec *AgentTemplateSpec `json:"agent_template_spec,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDeepAgentSpec 创建默认 DeepAgentSpec。
func NewDeepAgentSpec() DeepAgentSpec {
	return DeepAgentSpec{MaxIterations: 15, AutoCreateWorkspace: true, CompletionTimeout: 600.0}
}
