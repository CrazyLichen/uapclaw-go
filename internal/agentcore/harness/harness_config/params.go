package harness_config

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CreateDeepAgentParams 创建 DeepAgent 的参数集。
//
// 对应 Python: create_deep_agent() 的全部关键字参数。
// 定义在 harness_config 包而非 harness 包，以避免 harness_config → harness 循环依赖。
// harness 包的 CreateDeepAgent() 接收此类型作为入参。
type CreateDeepAgentParams struct {
	// Model 预构造的 Model 实例
	Model *llm.Model
	// Card Agent 身份卡片，nil 时创建默认卡片
	Card *agentschema.AgentCard
	// SystemPrompt 内层 ReActAgent 的系统提示词
	SystemPrompt string
	// ToolInstances Tool 实例列表，从中提取 ToolCard + 注册到 resource_mgr
	ToolInstances []tool.Tool
	// Mcps MCP 服务器配置列表
	Mcps []*mcptypes.McpServerConfig
	// Subagents 子 Agent 配置列表
	Subagents []hschema.SubAgentConfig
	// Rails AgentRail 实例列表
	Rails []rail.AgentRail
	// EnableTaskLoop 启用外层任务循环
	EnableTaskLoop bool
	// EnableAsyncSubagent 启用异步子 Agent 模式
	EnableAsyncSubagent bool
	// AddGeneralPurposeAgent 添加通用目的子 Agent
	AddGeneralPurposeAgent bool
	// MaxIterations 每次 invoke 的最大 ReAct 迭代次数
	MaxIterations int
	// Workspace 工作空间，nil 时创建默认
	Workspace *workspace.Workspace
	// Skills 技能定义列表
	Skills []string
	// Backend 后端协议实例（any 占位，P2 预留，等 Backend 实现时回填）
	Backend any
	// SysOperation 系统操作，nil 时自动创建默认
	SysOperation sysop.SysOperation
	// Language 提示词语言
	Language string
	// PromptMode 提示词模式
	PromptMode hschema.PromptMode
	// VisionModelConfig 视觉模型配置
	VisionModelConfig *hschema.VisionModelConfig
	// AudioModelConfig 音频模型配置
	AudioModelConfig *hschema.AudioModelConfig
	// EnableReadImageMultimodal 启用图像多模态读取
	EnableReadImageMultimodal bool
	// EnableTaskPlanning 启用任务规划
	EnableTaskPlanning bool
	// RestrictToWorkDir 限制文件访问到工作空间目录
	RestrictToWorkDir bool
	// DefaultMode 初始 Agent 模式
	DefaultMode hschema.AgentMode
	// ModelSelection 模型选择配置
	ModelSelection []hschema.ModelSelectionEntry
	// EnableSkillDiscovery 启用技能发现
	EnableSkillDiscovery bool
}
