package memory

import (
	"context"
	"sort"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	mt "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/memory"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryRail 通用记忆护栏，集成记忆工具并注入记忆使用提示词。
//
// 功能:
//  1. 注册记忆相关工具到 agent 的 ability_manager
//  2. 注入记忆使用提示词到系统提示词
//  3. 初始化和管理记忆索引管理器
//
// 对齐 Python: MemoryRail (openjiuwen/harness/rails/memory/memory_rail.py)
type MemoryRail struct {
	rails.DeepAgentRail
	// embeddingConfig 嵌入模型配置
	embeddingConfig *embedding.EmbeddingConfig
	// isProactive 是否主动模式
	isProactive bool
	// initialized 是否已初始化
	initialized bool
	// managerInitialized 管理器是否已初始化
	managerInitialized bool
	// toolCtx 记忆工具上下文
	toolCtx *lite.MemoryToolContext
	// systemPromptBuilder 系统提示词构建器引用
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// isReadOnly 是否只读模式
	isReadOnly bool
	// ownedToolNames 本 Rail 注册到 ability_manager 的工具名称集合
	ownedToolNames map[string]struct{}
	// ownedToolIDs 本 Rail 注册到 resource_mgr 的工具 ID 集合
	ownedToolIDs map[string]struct{}
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// memoryRailPriority MemoryRail 优先级
	memoryRailPriority = 80
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 MemoryRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*MemoryRail)(nil)

// memoryLogComponent 日志组件标识
var memoryLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryRail 创建 MemoryRail 实例。
//
// 对齐 Python: MemoryRail.__init__(embedding_config, is_proactive)
func NewMemoryRail(embeddingConfig *embedding.EmbeddingConfig, isProactive bool) *MemoryRail {
	r := &MemoryRail{
		DeepAgentRail:   *rails.NewDeepAgentRail(),
		embeddingConfig: embeddingConfig,
		isProactive:     isProactive,
		ownedToolNames:  make(map[string]struct{}),
		ownedToolIDs:    make(map[string]struct{}),
	}
	r.WithPriority(memoryRailPriority)
	return r
}

// Init 注册记忆工具到 agent。
//
// 对齐 Python: MemoryRail.init(agent)
func (r *MemoryRail) Init(agent agentinterfaces.BaseAgent) error {
	// 获取 systemPromptBuilder
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	// 注册工具
	r.registerMemoryTools(agent)

	return nil
}

// Uninit 注销记忆工具。
//
// 对齐 Python: MemoryRail.uninit(agent)
func (r *MemoryRail) Uninit(agent agentinterfaces.BaseAgent) error {
	// 从 ability_manager 移除工具
	am := agent.AbilityManager()
	if am != nil {
		for name := range r.ownedToolNames {
			func(name string) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(memoryLogComponent).
							Str("event_type", "memory_rail_uninit").
							Str("tool_name", name).
							Msgf("从 ability_manager 移除工具失败: %v", rec)
					}
				}()
				am.Remove(name)
			}(name)
		}
	}

	// 从 resource_mgr 移除工具
	resourceMgr := runner.GetResourceMgr()
	if resourceMgr != nil {
		for toolID := range r.ownedToolIDs {
			func(toolID string) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(memoryLogComponent).
							Str("event_type", "memory_rail_uninit").
							Str("tool_id", toolID).
							Msgf("从 resource_mgr 移除工具失败: %v", rec)
					}
				}()
				_, _ = resourceMgr.RemoveTool([]string{toolID})
			}(toolID)
		}
	}

	// 清理状态
	r.ownedToolNames = make(map[string]struct{})
	r.ownedToolIDs = make(map[string]struct{})
	r.initialized = false
	r.managerInitialized = false
	r.toolCtx = nil

	// 从 systemPromptBuilder 移除 memory section
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionMemory)
		r.systemPromptBuilder = nil
	}

	logger.Info(memoryLogComponent).
		Str("event_type", "memory_rail_uninit").
		Msg("MemoryRail 注销完成")

	return nil
}

// BeforeInvoke 初始化记忆管理器并在首次 invoke 时注册工具。
//
// 对齐 Python: MemoryRail.before_invoke(ctx)
func (r *MemoryRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 首次初始化
	if !r.initialized {
		r.initMemoryManager(ctx)
		r.initialized = true
	}

	// 检查是否为只读模式
	r.isReadOnly = false
	inputs := cbc.Inputs()
	if invokeInputs, ok := inputs.(*agentinterfaces.InvokeInputs); ok {
		r.isReadOnly = invokeInputs.IsCron() || invokeInputs.IsHeartbeat()
	}

	logger.Info(memoryLogComponent).
		Str("event_type", "memory_rail_before_invoke").
		Bool("read_only", r.isReadOnly).
		Msg("MemoryRail BeforeInvoke 完成")

	return nil
}

// BeforeModelCall 更新系统提示词中的记忆节。
//
// 对齐 Python: MemoryRail.before_model_call(ctx)
func (r *MemoryRail) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}

	// 移除旧的 memory section
	r.systemPromptBuilder.RemoveSection(sections.SectionMemory)

	// 确定 mode
	mode := "inactive"
	if r.isReadOnly {
		mode = "read_only"
	} else if r.isProactive {
		mode = "proactive"
	}

	// 获取语言
	lang := r.systemPromptBuilder.Language()

	// 获取当前日期
	todayDate := time.Now().Format("2006-01-02")

	// 构建记忆节
	memorySection := sections.BuildMemorySection(mode, todayDate, lang)
	r.systemPromptBuilder.AddSection(memorySection)

	logger.Info(memoryLogComponent).
		Str("event_type", "memory_rail_before_model_call").
		Str("mode", mode).
		Msg("MemoryRail BeforeModelCall 完成")

	return nil
}

// GetCallbacks 覆盖基类回调映射，增加 BeforeInvoke/BeforeModelCall。
//
// 对齐 Python: MemoryRail 隐式覆盖 before_invoke/before_model_call
func (r *MemoryRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// registerMemoryTools 注册记忆工具到 agent。
//
// 对齐 Python: MemoryRail._register_memory_tools(agent)
func (r *MemoryRail) registerMemoryTools(agent agentinterfaces.BaseAgent) {
	am := agent.AbilityManager()
	if am == nil {
		logger.Warn(memoryLogComponent).
			Str("event_type", "memory_rail_register_tools").
			Msg("Agent 无 ability_manager，跳过工具注册")
		return
	}

	agentID := ""
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}
	if agentID == "" {
		agentID = "default"
	}

	// 获取 language
	language := "cn"
	if r.systemPromptBuilder != nil {
		lang := r.systemPromptBuilder.Language()
		if lang != "" {
			language = lang
		}
	}

	// 创建 MemoryToolContext
	memoryDir := ""
	if r.Workspace() != nil {
		if nodePath := r.Workspace().GetNodePath("memory"); nodePath != nil {
			memoryDir = *nodePath
		}
	}
	settings := lite.CreateMemorySettings(memoryDir, nil)

	r.toolCtx = lite.NewMemoryToolContext().
		WithWorkspace(r.Workspace()).
		WithSettings(settings).
		WithAgentID(agentID).
		WithEmbeddingConfig(r.embeddingConfig).
		WithSysOperation(r.SysOperation())

	// 创建工具
	memoryTools := mt.CreateMemoryTools(r.toolCtx, language, agentID)

	// 注册到 resource_mgr 和 ability_manager
	resourceMgr := runner.GetResourceMgr()
	for _, t := range memoryTools {
		toolCard := t.Card()
		if toolCard == nil {
			logger.Warn(memoryLogComponent).
				Str("event_type", "memory_rail_register_tools").
				Msg("工具无 card，跳过注册")
			continue
		}

		// 注册到 resource_mgr
		if resourceMgr != nil {
			func(t tool.Tool) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(memoryLogComponent).
							Str("event_type", "memory_rail_register_tools").
							Str("tool_id", t.Card().ID).
							Msgf("注册工具到 resource_mgr 失败: %v", rec)
					}
				}()
				existing, err := resourceMgr.GetTool([]string{t.Card().ID})
				if err != nil || len(existing) == 0 {
					if addErr := resourceMgr.AddTool(t); addErr != nil {
						logger.Warn(memoryLogComponent).
							Str("event_type", "memory_rail_register_tools").
							Str("tool_id", t.Card().ID).
							Err(addErr).
							Msg("注册工具到 resource_mgr 失败")
					} else {
						r.ownedToolIDs[t.Card().ID] = struct{}{}
					}
				}
			}(t)
		}

		// 注册到 ability_manager
		func(t tool.Tool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(memoryLogComponent).
						Str("event_type", "memory_rail_register_tools").
						Str("tool_name", t.Card().Name).
						Msgf("注册工具到 ability_manager 失败: %v", rec)
				}
			}()
			result := am.Add(t.Card())
			if result.Added {
				r.ownedToolNames[t.Card().Name] = struct{}{}
				logger.Info(memoryLogComponent).
					Str("event_type", "memory_rail_register_tools").
					Str("tool_name", t.Card().Name).
					Msg("注册工具成功")
			}
		}(t)
	}

	logger.Info(memoryLogComponent).
		Str("event_type", "memory_rail_register_tools").
		Strs("tool_names", memorySetToSortedSlice(r.ownedToolNames)).
		Msg("MemoryRail 工具注册完成")
}

// initMemoryManager 初始化记忆索引管理器。
//
// 对齐 Python: MemoryRail._init_memory_manager(ctx)
func (r *MemoryRail) initMemoryManager(ctx context.Context) {
	agentID := ""
	if r.toolCtx != nil {
		agentID = r.toolCtx.AgentID
	}
	if agentID == "" {
		agentID = "default"
	}

	// 无 workspace 时无法初始化
	if r.Workspace() == nil {
		logger.Warn(memoryLogComponent).
			Str("event_type", "memory_rail_init_manager").
			Msg("Workspace 为 nil，跳过管理器初始化")
		return
	}

	mgr, err := lite.InitMemoryManagerAsync(
		ctx,
		r.Workspace(),
		agentID,
		r.embeddingConfig,
		r.SysOperation(),
	)
	if err != nil {
		logger.Error(memoryLogComponent).
			Str("event_type", "memory_rail_init_manager").
			Str("agent_id", agentID).
			Err(err).
			Msg("初始化记忆管理器失败")
		return
	}

	if mgr != nil {
		r.managerInitialized = true
		if r.toolCtx != nil {
			r.toolCtx.Manager = mgr
		}
		logger.Info(memoryLogComponent).
			Str("event_type", "memory_rail_init_manager").
			Str("agent_id", agentID).
			Msg("记忆管理器初始化成功")
	} else {
		logger.Warn(memoryLogComponent).
			Str("event_type", "memory_rail_init_manager").
			Msg("记忆管理器初始化返回 nil")
	}
}

// memorySetToSortedSlice 将 set 转为排序后的切片（用于日志）
func memorySetToSortedSlice(s map[string]struct{}) []string {
	result := make([]string, 0, len(s))
	for k := range s {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}
