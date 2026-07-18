package context_engineer

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextAssembleRail 上下文组装护栏，注入工作空间目录结构和上下文文件到系统提示词。
//
// 在 Init 中捕获 system_prompt_builder 和 ability_manager 引用。
// 在 BeforeModelCall 中构建并注入 workspace/context/tools 节到系统提示词构建器。
//
// 对齐 Python: ContextAssembleRail (openjiuwen/harness/rails/context_engineer/context_assemble_rail.py)
type ContextAssembleRail struct {
	rails.DeepAgentRail
	// systemPromptBuilder 系统提示词构建器引用
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// abilityManager 能力管理器引用
	abilityManager sainterfaces.AbilityManagerInterface
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// contextAssembleRailPriority ContextAssembleRail 优先级
	// 对齐 Python: ContextAssembleRail.priority = 85
	contextAssembleRailPriority = 85
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextAssembleRail 创建 ContextAssembleRail 实例。
//
// 对齐 Python: ContextAssembleRail()
func NewContextAssembleRail() *ContextAssembleRail {
	r := &ContextAssembleRail{}
	r.WithPriority(contextAssembleRailPriority)
	return r
}

// Init Rail 初始化钩子：捕获 system_prompt_builder 和 ability_manager 引用。
//
// 对齐 Python: ContextAssembleRail.init(agent)
func (r *ContextAssembleRail) Init(agent sainterfaces.BaseAgent) error {
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	r.abilityManager = agent.AbilityManager()

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "context_assemble_rail_init").
		Msg("ContextAssembleRail 初始化完成")

	return nil
}

// Uninit Rail 注销钩子：移除 workspace、context 节。
//
// 对齐 Python: ContextAssembleRail.uninit(agent)
func (r *ContextAssembleRail) Uninit(_ sainterfaces.BaseAgent) error {
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionWorkspace)
		r.systemPromptBuilder.RemoveSection(sections.SectionContext)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "context_assemble_rail_uninit").
		Msg("ContextAssembleRail 注销完成")

	return nil
}

// BeforeModelCall LLM 调用前：注入工作空间目录结构和上下文文件到系统提示词。
//
// 对齐 Python: ContextAssembleRail.before_model_call(ctx)
func (r *ContextAssembleRail) BeforeModelCall(_ context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}

	ws := r.Workspace()
	if ws == nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionWorkspace)
		r.systemPromptBuilder.RemoveSection(sections.SectionContext)
		return nil
	}

	lang := r.systemPromptBuilder.Language()

	// 构建工作空间节
	// 对齐 Python: workspace_section = await _build_workspace(self.sys_operation, workspace, lang)
	// Go 的 BuildWorkspaceSection 是同步的，需要先获取目录树
	rootPath := ws.RootPath
	dirTree := ""
	if rootPath != "" {
		nodes := sections.ScanDirectoryStructure(rootPath, 0, 2, lang)
		dirTree = sections.FormatTree(nodes, lang)
	}
	workspaceSection := sections.BuildWorkspaceSection(rootPath, dirTree, lang)

	// 构建工具节
	// 对齐 Python: tools_section = build_tools_section(self._ability_manager, lang)
	// Python 遍历 ability_manager.list() 获取 ToolCard 的 name/description
	var toolsSection *saprompt.PromptSection
	if r.abilityManager != nil {
		toolDescriptions := make(map[string]string)
		for _, ability := range r.abilityManager.List() {
			name := ability.AbilityName()
			// 获取描述：尝试通过 CardInterface 获取
			type cardInterface interface {
				GetDescription() string
			}
			if ci, ok := ability.(cardInterface); ok && name != "" {
				desc := ci.GetDescription()
				if desc != "" {
					toolDescriptions[name] = desc
				}
			}
		}
		if len(toolDescriptions) > 0 {
			toolsSection = sections.BuildToolsSection(toolDescriptions, lang)
		}
	}

	// 构建上下文节
	// 对齐 Python: context_section = await _build_context(self.sys_operation, workspace, lang, include_daily_memory=not is_heartbeat)
	// Go 中需要通过 SysOperation 的 Fs() 读取上下文文件
	// TODO: 后续实现 ReadContextFiles 或由 DeepAdapter 在 Init 时提供回调
	isHeartbeat := false
	if cbc != nil && cbc.Extra() != nil {
		if runKind, ok := cbc.Extra()["run_kind"]; ok {
			isHeartbeat = runKind == sainterfaces.RunKindHeartbeat
		}
	}

	// 暂不注入上下文节（需要 ReadContextFiles 支持）
	// 后续回填时通过 FsOperation 读取 workspace 中的上下文文件
	_ = isHeartbeat

	// 注入节到系统提示词构建器
	r.systemPromptBuilder.AddSection(workspaceSection)

	if toolsSection != nil {
		r.systemPromptBuilder.AddSection(*toolsSection)
	} else {
		r.systemPromptBuilder.RemoveSection(sections.SectionTools)
	}

	// TODO: 后续回填 context section 注入

	return nil
}

// GetCallbacks 返回已覆盖的钩子映射。
func (r *ContextAssembleRail) GetCallbacks() map[sainterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[sainterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*sainterfaces.AgentCallbackContext))
	}

	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────
