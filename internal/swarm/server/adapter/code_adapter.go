package adapter

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	memoryrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cfgPkg "github.com/uapclaw/uapclaw-go/internal/common/config"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	serverhooks "github.com/uapclaw/uapclaw-go/internal/swarm/server/hooks"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAdapter Code 模式适配器，组合委托 DeepAdapter。
//
// 继承 UapClawDeepAdapter 的全部接口方法，仅覆盖 CreateInstance。
// Go 中通过内嵌 *DeepAdapter 实现组合委托。
//
// Code 模式差异点（对齐 Python JiuwenClawCodeAdapter）：
//   - create_instance: 不传多模态/上下文引擎参数，使用 code system prompt
//   - rails: 加入 LspRail/CodeAgentRail/CodingMemoryRail/ProjectMemoryRail
//   - subagents: 固定 explore+plan 子代理
//   - _update_rails_for_mode: 保留 SubagentRail/ProjectMemoryRail/CodingMemoryRail
//   - 语言: 强制英文系统提示词
//
// 对应 Python: jiuwenswarm/server/runtime/agent_adapter/interface_code.py (JiuwenClawCodeAdapter)
type CodeAdapter struct {
	// deep 内嵌 DeepAdapter，组合委托全部接口方法
	deep *DeepAdapter

	// ─── Code 模式专有 Rails ───

	// lspRail LSP 护栏
	// ⤵️ 10.6.3-10: LspRail
	lspRail sainterfaces.AgentRail
	// projectMemoryRail 项目记忆护栏
	// ⤵️ 10.6.3-10: ProjectMemoryRail
	projectMemoryRail sainterfaces.AgentRail
	// codingMemoryRail 编码记忆护栏
	codingMemoryRail sainterfaces.AgentRail
	// worktreeRail 工作树护栏
	// ⤵️ 10.6.3-10: WorktreeRail
	worktreeRail sainterfaces.AgentRail
	// codeAgentRail Code 模式专有护栏
	codeAgentRail sainterfaces.AgentRail

	// ─── Code 模式配置 ───

	// runtimeLanguageOverride 运行时语言覆盖
	runtimeLanguageOverride string
	// forceEnglishRuntimePrompt 强制英文运行时提示词
	forceEnglishRuntimePrompt bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// codeFixedRailNames Code 模式固定 Rails 名字集合，用于动态 Rails 去重。
// 对齐 Python: JiuwenClawCodeAdapter._FIXED_RAIL_NAMES
var codeFixedRailNames = map[string]bool{
	"RuntimePromptRail":       true,
	"ResponsePromptRail":      true,
	"JiuClawStreamEventRail":  true,
	"SecurityRail":            true,
	"LspRail":                 true,
	"ProjectMemoryRail":       true,
	"PermissionInterruptRail": true,
	"ContextProcessorRail":    true,
	"SysOperationRail":        true,
	"CodingMemoryRail":        true,
	"AgentModeRail":           true,
	"StructuredAskUserRail":   true,
	"ConfirmInterruptRail":    true,
	"FileSystemRail":          true, // 别名
	"CodeAgentRail":           true,
}

// codeRailBuildNames 动态 Rail 名字 → builder 方法名映射。
// 对齐 Python: _RAIL_BUILD_NAMES
var codeRailBuildNames = map[string]string{
	"SysOperationRail":     "buildFilesystemRail",
	"FileSystemRail":       "buildFilesystemRail",
	"SkillUseRail":         "buildSkillRail",
	"HeartbeatRail":        "buildHeartbeatRail",
	"AvatarPromptRail":     "buildAvatarRail",
	"TaskPlanningRail":     "buildTaskPlanningRail",
	"SubagentRail":         "buildSubagentRail",
	"ContextAssembleRail":  "buildContextAssembleRail",
	"ContextProcessorRail": "buildContextProcessorRail",
	"SkillEvolutionRail":   "buildSkillEvolutionRail",
	"WorktreeRail":         "buildWorktreeRail",
	"CodeAgentRail":        "buildCodeAgentRail",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCodeAdapter 创建 CodeAdapter 实例。
//
// 对应 Python: JiuwenClawCodeAdapter.__init__() (line 177-192)
func NewCodeAdapter() *CodeAdapter {
	deep := NewDeepAdapter()
	deep.isCodeAgent = true // 单点 source-of-truth：code-agent → project_dir
	return &CodeAdapter{
		deep:                      deep,
		forceEnglishRuntimePrompt: true,
	}
}

// CreateInstance 初始化底层 SDK Agent（code 模式）。
//
// 对应 Python: JiuwenClawCodeAdapter.create_instance() (line 221-342)
//
// Python 执行步骤：
//  1. set_checkpoint
//  2. instance_overrides = dict(config or {})
//  3. config_base = get_config()
//  4. _refresh_multimodal_configs(config_base)
//  5. config = config_base.get("react", {}).copy()
//  6. self._config_cache = config.copy()
//  7. self._agent_name = overrides.get("agent_name", config.get("agent_name", "main_agent"))
//  8. self._project_dir = overrides.get("project_dir", config.get("project_dir"))
//  9. self._workspace_dir = self._project_dir or config.get("workspace_dir") or get_agent_workspace_dir()
//  10. self._agent_workspace_dir = str(get_agent_workspace_dir())
//  11. self._dreaming_mode = "code"
//  12. model = self._create_model(config_base)
//  13. ⤵️ A2X / 11.10: _try_init_a2x_client — code 模式不初始化
//  14. agent_card = AgentCard(name=self._agent_name, id='jiuwenswarm')
//  15. tool_cards = await self._get_tool_cards(agent_card.id)
//  16. rails_list = self._build_agent_rails(config, config_base, mode="code")
//  17. sys_operation = self._create_sys_operation()
//  18. configured_subagents = self._build_configured_subagents(model, config, config_base)
//  19. self._instance = create_deep_agent(model, card, system_prompt=build_code_system_prompt(), ...)
//  20. await self._instance.ensure_initialized()
//  21. self._seed_runtime_cwd(self._project_dir or self._workspace_dir)
//     21.1 setattr(self._instance, "_jiuwenswarm_adapter_mode", "code")
//     21.2 coding_memory workspace set_directory
//     21.3 agent_history 写入路径修正
//  22. self._registered_mcp_server_ids.clear()
//  23. await self._register_mcp_servers_from_config(config_base, tag="code")
//  24. await self.load_user_rails()
func (c *CodeAdapter) CreateInstance(ctx context.Context, config map[string]any, mode string, subMode string) error {
	// 步骤 11: dreaming_mode = "code"（Python 中在步骤 11，此处提前设置，其余步骤仍按 Python 编号）
	c.deep.dreamingMode = "code"

	// 步骤 1: set_checkpoint
	if err := c.deep.setCheckpoint(); err != nil {
		return fmt.Errorf("set_checkpoint 失败: %w", err)
	}
	// 步骤 2: instanceOverrides 初始化
	if config != nil {
		c.deep.instanceOverrides = make(map[string]any, len(config))
		for k, v := range config {
			c.deep.instanceOverrides[k] = v
		}
	} else {
		c.deep.instanceOverrides = make(map[string]any)
	}

	// 步骤 3: get_config → configBase
	cfg, err := cfgPkg.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}
	configBase, err := cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 步骤 4: ⤵️ 10.6.24 多模态工具: _refresh_multimodal_configs(configBase)

	// 步骤 5-6: 读取 react 配置段，缓存到 configCache
	if reactRaw, ok := configBase["react"]; ok {
		if reactMap, ok := reactRaw.(map[string]any); ok {
			c.deep.configCache = make(map[string]any, len(reactMap))
			for k, v := range reactMap {
				c.deep.configCache[k] = v
			}
		}
	} else {
		c.deep.configCache = make(map[string]any)
	}

	// 步骤 7: agentName（完整版：优先 overrides，其次 configCache）
	if v, ok := c.deep.instanceOverrides["agent_name"]; ok {
		if s, ok := v.(string); ok {
			c.deep.agentName = s
		}
	} else if v, ok := c.deep.configCache["agent_name"]; ok {
		if s, ok := v.(string); ok {
			c.deep.agentName = s
		}
	}

	// 步骤 8: projectDir（完整版：优先 overrides，其次 configCache）
	if v, ok := c.deep.instanceOverrides["project_dir"]; ok {
		if s, ok := v.(string); ok {
			c.deep.projectDir = s
		}
	} else if v, ok := c.deep.configCache["project_dir"]; ok {
		if s, ok := v.(string); ok {
			c.deep.projectDir = s
		}
	}

	// 步骤 9: workspaceDir 优先使用 projectDir
	if c.deep.projectDir != "" {
		c.deep.workspaceDir = c.deep.projectDir
	} else if v, ok := c.deep.configCache["workspace_dir"]; ok {
		if s, ok := v.(string); ok && s != "" {
			c.deep.workspaceDir = s
		}
	}
	if c.deep.workspaceDir == "" {
		c.deep.workspaceDir = workspace.AgentRootDir()
	}

	// 步骤 10: agentWorkspaceDir 始终指向系统 workspace
	// （Go 中 workspace.AgentRootDir() 即为系统 workspace）

	// 步骤 12: model = d.createModel(configBase) — 不传多模态配置
	c.deep.model = c.deep.createModel(configBase)

	// 步骤 13: ⤵️ A2X / 11.10: code 模式不初始化 A2X 客户端

	// 步骤 14: agentCard = AgentCard(name=agent_name, id='jiuwenswarm')
	// 对齐 Python: agent_card = AgentCard(name=self._agent_name, id='jiuwenswarm')
	agentCard := agentschema.NewAgentCard(
		agentschema.WithAgentName(c.deep.agentName),
		agentschema.WithAgentID("uapclaw"),
	)

	// 步骤 15: tool_cards = d.deep.getToolCards(agent_card.id)
	// 对齐 Python: tool_cards = await self._get_tool_cards(agent_card.id)
	toolCards := c.deep.getToolCards(agentCard.ID)
	c.deep.toolCards = toolCards

	// 步骤 16: rails_list = _build_agent_rails(config, configBase, mode="code")
	// CodeAdapter 完全重写 buildAgentRails，对齐 Python 固定 rails + 动态 rails
	railsList := c.buildCodeAgentRails(c.deep.configCache, configBase)

	// 步骤 17: sys_operation = _create_sys_operation()
	// 对齐 Python: sys_operation = self._create_sys_operation()
	sysOpInstance, _ := c.deep.createSysOperation(configBase)
	if sysOpInstance == nil {
		return fmt.Errorf("sys_operation 不可用，可能任务未在运行")
	}
	c.deep.sysOperation = sysOpInstance

	// 步骤 18: configured_subagents = _build_configured_subagents(model, config, config_base)
	// 对齐 Python: configured_subagents, _should_add_general = self._build_configured_subagents(model, config, config_base)
	subagentSpecs, _ := c.deep.buildConfiguredSubagents(c.deep.configCache, configBase)

	// 步骤 19: create_deep_agent(...)
	// 对齐 Python: self._instance = create_deep_agent(model, card, system_prompt=build_code_system_prompt(), ...)
	// code 模式不传: vision_model_config, audio_model_config, context_engine_config, completion_timeout
	// ⤵️ 10.6.1-2: system_prompt 等待 build_code_system_prompt 实现，当前先用 buildAgentIdentityPrompt("en")
	systemPrompt := c.deep.buildAgentIdentityPrompt("en")
	resolvedLanguage := c.deep.resolveRuntimeLanguage()

	params := harness_config.CreateDeepAgentParams{
		Model:               c.deep.model,
		Card:                agentCard,
		SystemPrompt:        systemPrompt,
		ToolCards:           toolCards,
		Subagents:           subagentSpecs,
		Rails:               railsList,
		EnableTaskLoop:      c.deep.resolveEnableTaskLoop(c.deep.configCache, configBase),
		MaxIterations:       paramsInt(c.deep.configCache, "max_iterations", 15),
		Workspace:           hworkspace.NewWorkspace(c.deep.workspaceDir, resolvedLanguage),
		Language:            resolvedLanguage,
		EnableTaskPlanning:  true, // 对齐 Python: 硬编码 true
		AutoCreateWorkspace: false,
		SysOperation:        sysOpInstance,
	}

	agent, createErr := harness.CreateDeepAgent(ctx, params)
	if createErr != nil {
		return fmt.Errorf("CreateDeepAgent 失败: %w", createErr)
	}
	c.deep.instance = agent

	// 步骤 20: d.instance.EnsureInitialized(ctx)
	// 对齐 Python: await self._instance.ensure_initialized()
	if _, initErr := c.deep.instance.EnsureInitialized(ctx); initErr != nil {
		return fmt.Errorf("DeepAgent EnsureInitialized 失败: %w", initErr)
	}

	// 步骤 21: _seed_runtime_cwd(c.projectDir or c.workspaceDir)
	// 对齐 Python: self._seed_runtime_cwd(self._project_dir or self._workspace_dir) (interface_code.py:300)
	initCwd := c.deep.projectDir
	if initCwd == "" {
		initCwd = c.deep.workspaceDir
	}
	c.deep.seedRuntimeCwd(ctx, initCwd)

	// 步骤 21.1: _jiuwenswarm_adapter_mode = "code"
	// 对齐 Python: setattr(self._instance, "_jiuwenswarm_adapter_mode", "code")
	// ⤵️ 10.6.3-10: 待 DeepAgent 实例属性扩展后回填（Python 猴子补丁，Go 需等 TeamManager 设计）

	// 步骤 21.2: coding_memory workspace set_directory
	// ✅ 已回填：Workspace.SetDirectory（对齐 Python: self._instance.deep_config.workspace.set_directory(...)）
	projectName := "default"
	if c.deep.projectDir != "" {
		projectName = filepath.Base(c.deep.projectDir)
	}
	codingMemoryPath := filepath.Join("coding_memory", projectName)
	if ws := c.deep.instance.DeepConfig().Workspace; ws != nil {
		_ = ws.SetDirectory(map[string]any{
			"name":        "coding_memory",
			"description": "Coding Agent 记忆模块",
			"path":        codingMemoryPath,
			"children": []any{
				map[string]any{
					"name":            "MEMORY.md",
					"description":     "Coding 记忆索引",
					"path":            "MEMORY.md",
					"children":        []any{},
					"is_file":         true,
					"default_content": "",
				},
			},
		})
	}

	// 步骤 21.3: agent_history 写入路径修正
	// 对齐 Python: 修正 .agent_history 写入路径到 agent 系统 workspace
	// ⤵️ 10.6.3-10: 待 DeepAgent 实例属性扩展后回填

	// 步骤 22: c.deep.registeredMCPServerIDs = make(map[string]bool)
	c.deep.registeredMCPServerIDs = make(map[string]bool)
	c.deep.registeredMCPServers = make(map[string]any)

	// 步骤 23: _register_mcp_servers_from_config(configBase, tag="code")
	// 对齐 Python: await self._register_mcp_servers_from_config(config_base, tag="code")
	if regErr := c.deep.registerMcpServersFromConfig(ctx, configBase, "code"); regErr != nil {
		logger.Warn(logComponent).Err(regErr).Msg("MCP 服务注册(code 模式)失败，继续执行")
	}

	// 步骤 24: ⤵️ 10.6.3-10: load_user_rails()

	// 存储 mode/subMode
	c.deep.mode = mode
	c.deep.subMode = subMode

	logger.Info(logComponent).
		Str("agent_name", c.deep.agentName).
		Str("mode", mode).
		Str("sub_mode", subMode).
		Bool("is_code_agent", c.deep.isCodeAgent).
		Msg("CodeAdapter CreateInstance 完成")
	return nil
}

// ReloadAgentConfig 委托 DeepAdapter。
func (c *CodeAdapter) ReloadAgentConfig(ctx context.Context, configBase map[string]any, envOverrides map[string]any) error {
	return c.deep.ReloadAgentConfig(ctx, configBase, envOverrides)
}

// ProcessMessageImpl 委托 DeepAdapter。
func (c *CodeAdapter) ProcessMessageImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (*schema.AgentResponse, error) {
	return c.deep.ProcessMessageImpl(ctx, req, inputs)
}

// ProcessMessageStreamImpl 委托 DeepAdapter。
func (c *CodeAdapter) ProcessMessageStreamImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (<-chan *schema.AgentResponseChunk, error) {
	return c.deep.ProcessMessageStreamImpl(ctx, req, inputs)
}

// ProcessInterrupt 委托 DeepAdapter。
func (c *CodeAdapter) ProcessInterrupt(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	return c.deep.ProcessInterrupt(ctx, req)
}

// HandleUserAnswer 委托 DeepAdapter。
func (c *CodeAdapter) HandleUserAnswer(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	return c.deep.HandleUserAnswer(ctx, req)
}

// HandleHeartbeat 委托 DeepAdapter。
func (c *CodeAdapter) HandleHeartbeat(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	return c.deep.HandleHeartbeat(ctx, req)
}

// Cleanup 委托 DeepAdapter 清理适配器资源。
func (c *CodeAdapter) Cleanup() error {
	return c.deep.Cleanup()
}

// CompressContext 委托 DeepAdapter 的 ContextCompressor 接口。
func (c *CodeAdapter) CompressContext(ctx context.Context, sessionID string, session sessioninterfaces.SessionFacade, returnState bool) (map[string]any, error) {
	return c.deep.CompressContext(ctx, sessionID, session, returnState)
}

// GetContextUsage 委托 DeepAdapter 的 ContextCompressor 接口。
func (c *CodeAdapter) GetContextUsage(ctx context.Context, sessionID string) (map[string]any, error) {
	return c.deep.GetContextUsage(ctx, sessionID)
}

// GenerateRecap 委托 DeepAdapter 的 ContextCompressor 接口。
func (c *CodeAdapter) GenerateRecap(ctx context.Context, sessionID string) (map[string]any, error) {
	return c.deep.GenerateRecap(ctx, sessionID)
}

// TryStartDreaming 委托 DeepAdapter 的 DreamingController 接口。
func (c *CodeAdapter) TryStartDreaming(ctx context.Context, busyChecker func() bool) error {
	return c.deep.TryStartDreaming(ctx, busyChecker)
}

// TryStopDreaming 委托 DeepAdapter 的 DreamingController 接口。
func (c *CodeAdapter) TryStopDreaming(ctx context.Context) error {
	return c.deep.TryStopDreaming(ctx)
}

// SwitchMode 委托 DeepAdapter 的模式切换（含 session 生命周期）。
func (c *CodeAdapter) SwitchMode(ctx context.Context, sessionID, subMode string) error {
	return c.deep.SwitchMode(ctx, sessionID, subMode)
}

// AbortOnGatewayDisconnect 委托 DeepAdapter 的 GatewayDisconnectHandler 接口。
func (c *CodeAdapter) AbortOnGatewayDisconnect(ctx context.Context) {
	c.deep.AbortOnGatewayDisconnect(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveEmbeddingConfig 解析嵌入配置。
// 从 configCache 中获取 embedding 配置，暂不可用则返回 nil。
// TODO: 补充从 config 解析 EmbeddingConfig 的逻辑
func (c *CodeAdapter) resolveEmbeddingConfig() *embedding.EmbeddingConfig {
	// 暂时返回 nil，等 embedding 配置解析链路完善后补充
	return nil
}

// buildCodeAgentRails 构建 Code 模式 Agent Rails 列表。
// 对齐 Python: JiuwenClawCodeAdapter._build_agent_rails(config, config_base, mode="code")
//
// Code 模式固定 Rails 列表（对齐 Python _RailBuildInfo）+ 动态 Rails from config。
// 未实现的 Rail builder 返回 nil，通过 nil 检查自动跳过。
func (c *CodeAdapter) buildCodeAgentRails(config map[string]any, configBase map[string]any) []sainterfaces.AgentRail {
	var railsList []sainterfaces.AgentRail

	// ─── 固定 Rails — code 模式特有 ───
	// 对齐 Python: rail_infos = [...] 中的固定列表

	// 1: RuntimePromptRail
	if rp := c.deep.buildRuntimePromptRail(); rp != nil {
		c.deep.runtimePromptRail = rp
		railsList = append(railsList, rp)
	}

	// 2: ResponsePromptRail
	if resp := c.deep.buildResponsePromptRail(); resp != nil {
		c.deep.responsePromptRail = resp
		railsList = append(railsList, resp)
	}

	// 3: StreamEventRail
	if se := c.deep.buildStreamEventRail(); se != nil {
		c.deep.streamEventRail = se
		railsList = append(railsList, se)
	}

	// 4: SecurityRail
	if sec := c.deep.buildSecurityRail(configBase); sec != nil {
		c.deep.securityRail = sec
		railsList = append(railsList, sec)
	}

	// 5: LspRail
	// ⤵️ 10.6.3-10: LspRail 尚未实现
	if lsp := c.buildLspRail(); lsp != nil {
		c.lspRail = lsp
		railsList = append(railsList, lsp)
	}

	// 6: ProjectMemoryRail
	// ⤵️ 10.6.3-10: ProjectMemoryRail 尚未实现
	if pm := c.buildProjectMemoryRail(); pm != nil {
		c.projectMemoryRail = pm
		railsList = append(railsList, pm)
	}

	// 7: PermissionInterruptRail
	// ⤵️ 10.6.3-10: PermissionInterruptRail 尚未实现
	if perm := c.buildPermissionRail(configBase); perm != nil {
		c.deep.permissionRail = perm
		railsList = append(railsList, perm)
	}

	// 8: FileSystemRail (SysOperationRail)
	fs := c.deep.buildFilesystemRail(false)
	if fs != nil {
		c.deep.filesystemRail = fs
		railsList = append(railsList, fs)
	}

	// 9: CodingMemoryRail
	if cm := c.buildCodingMemoryRail(); cm != nil {
		c.codingMemoryRail = cm
		railsList = append(railsList, cm)
	}

	// 10: AgentModeRail
	am := c.deep.buildAgentModeRail(nil)
	if am != nil {
		railsList = append(railsList, am)
	}

	// 11: StructuredAskUserRail
	// ⤵️ 10.6.3-10: StructuredAskUserRail 尚未实现
	if sau := c.buildStructuredAskUserRail(); sau != nil {
		railsList = append(railsList, sau)
	}

	// 12: ConfirmInterruptRail
	// ⤵️ 10.6.3-10: ConfirmInterruptRail 尚未实现
	if ci := c.buildConfirmInterruptRail(); ci != nil {
		railsList = append(railsList, ci)
	}

	// 13: ContextProcessorRail
	cp := c.deep.buildContextProcessorRail()
	c.deep.contextProcessorRail = cp
	railsList = append(railsList, cp)

	// 14: CodeAgentRail
	if car := c.buildCodeAgentRail(); car != nil {
		c.codeAgentRail = car
		railsList = append(railsList, car)
	}

	// ─── 动态 Rails — 从 config.yaml::modes.code.rails 读取 ───
	// 对齐 Python: for rail_name in configured_rails
	c.appendDynamicRails(configBase, &railsList)

	// ─── UserHookRail — 用户配置的 hooks ───
	// 对齐 Python: try/except 包裹注册流程，失败时 warning 并继续
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("panic", r).
					Msg("UserHookRail 加载失败，跳过")
			}
		}()
		hooksCfg := hookscfg.LoadHooksConfig(configBase)
		if len(hooksCfg.Events) > 0 {
			llmCfg := extractLLMConfig(configBase)
			hookExec := serverhooks.NewHookExecutor(llmCfg)
			userHookRail := serverhooks.NewUserHookRail(*hooksCfg, hookExec)
			railsList = append(railsList, userHookRail)
			logger.Info(logComponent).Int("event_types", len(hooksCfg.Events)).Msg("UserHookRail 加载完成")
		}
	}()

	logger.Info(logComponent).
		Str("mode", "code").
		Int("rails_count", len(railsList)).
		Msg("buildCodeAgentRails 完成")

	return railsList
}

// appendDynamicRails 从 config.yaml::modes.code.rails 读取动态 Rails 并追加。
// 对齐 Python: for rail_name in configured_rails
func (c *CodeAdapter) appendDynamicRails(configBase map[string]any, railsList *[]sainterfaces.AgentRail) {
	modesCfg, _ := configBase["modes"].(map[string]any)
	codeCfg, _ := modesCfg["code"].(map[string]any)
	configuredRails, _ := codeCfg["rails"].([]any)

	for _, railNameRaw := range configuredRails {
		railName, ok := railNameRaw.(string)
		if !ok {
			continue
		}

		// 跳过已在固定列表中的 rail
		if codeFixedRailNames[railName] {
			logger.Info(logComponent).Str("rail_name", railName).
				Msg("动态 Rail 已在固定集合中，跳过")
			continue
		}

		// MemoryRail 不支持 code 模式
		if railName == "MemoryRail" {
			logger.Warn(logComponent).Str("rail_name", railName).
				Msg("MemoryRail 不支持 code 模式，请使用 CodingMemoryRail，跳过")
			continue
		}

		// 查找 builder 方法名
		methodName, ok := codeRailBuildNames[railName]
		if !ok {
			logger.Warn(logComponent).Str("rail_name", railName).
				Msg("未知的动态 Rail 名称，跳过")
			continue
		}

		// 调用 DeepAdapter 的 builder 方法
		rail := c.deep.buildDynamicRail(methodName, configBase)
		if rail != nil {
			*railsList = append(*railsList, rail)
			logger.Info(logComponent).Str("rail_name", railName).
				Msg("动态 Rail 从配置加载成功")
		} else {
			logger.Warn(logComponent).Str("rail_name", railName).Str("method", methodName).
				Msg("动态 Rail 构建返回 nil")
		}
	}
}

// buildCodeAgentRail 构建 CodeAgentRail。
// 对齐 Python: JiuwenClawCodeAdapter._build_code_agent_rail() (interface_code.py L826-834)
//
// 仅当 configLister 可用时创建 CodeAgentRail，否则返回 nil。
func (c *CodeAdapter) buildCodeAgentRail() *CodeAgentRail {
	if c.deep.configLister == nil {
		return nil
	}
	return NewCodeAgentRail(c.deep.workspaceDir, c.deep.configLister)
}

// buildLspRail 构建 LSP 护栏。
// 对齐 Python: JiuwenClawCodeAdapter._build_lsp_rail_via_config() (interface_code.py)
// ⤵️ 10.6.3-10: LspRail 尚未实现
func (c *CodeAdapter) buildLspRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 LspRail
	return nil
}

// buildProjectMemoryRail 构建项目记忆护栏。
// 对齐 Python: JiuwenClawCodeAdapter._build_project_memory_rail() (interface_code.py)
// ⤵️ 10.6.3-10: ProjectMemoryRail 尚未实现
func (c *CodeAdapter) buildProjectMemoryRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 ProjectMemoryRail
	return nil
}

// buildCodingMemoryRail 构建编码记忆护栏。
// 对齐 Python: JiuwenClawCodeAdapter._build_coding_memory_rail() (interface_code.py)
func (c *CodeAdapter) buildCodingMemoryRail() sainterfaces.AgentRail {
	// 获取 embeddingConfig（从 config 中获取，暂不可用则返回 nil）
	embCfg := c.resolveEmbeddingConfig()
	if embCfg == nil {
		return nil
	}

	// 获取 codingMemoryDir
	codingMemoryDir := ""
	if c.deep.instance != nil && c.deep.instance.Config().Workspace != nil {
		if nodePath := c.deep.instance.Config().Workspace.GetNodePath("coding_memory"); nodePath != nil {
			codingMemoryDir = *nodePath
		}
	}

	// 获取语言
	language := "en"
	if c.deep.instance != nil && c.deep.instance.Config().Language != "" {
		language = c.deep.instance.Config().Language
	}

	return memoryrail.NewCodingMemoryRail(codingMemoryDir, embCfg, language)
}

// buildStructuredAskUserRail 构建结构化询问护栏。
// 对齐 Python: JiuwenClawCodeAdapter._build_structured_ask_user_rail() (interface_code.py)
// ⤵️ 10.6.3-10: StructuredAskUserRail 尚未实现
func (c *CodeAdapter) buildStructuredAskUserRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 StructuredAskUserRail
	return nil
}

// buildConfirmInterruptRail 构建确认中断护栏。
// ✅ 已回填：ConfirmInterruptRail（对齐 Python: _build_confirm_interrupt_rail() — ConfirmInterruptRail(tool_names=["switch_mode"])）
func (c *CodeAdapter) buildConfirmInterruptRail() sainterfaces.AgentRail {
	rail := interrupt.NewConfirmInterruptRail("switch_mode")
	logger.Info(logComponent).Msg("ConfirmInterruptRail 创建成功")
	return rail
}

// buildPermissionRail 构建权限护栏。
// 对齐 Python: build_permission_rail() (interface_code.py)
// ⤵️ 10.6.3-10: PermissionInterruptRail 尚未实现
func (c *CodeAdapter) buildPermissionRail(configBase map[string]any) sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 PermissionInterruptRail
	return nil
}

// buildWorktreeRail 构建工作树护栏。
// 对齐 Python: JiuwenClawCodeAdapter._build_worktree_rail_via_config() (interface_code.py)
// ⤵️ 10.6.3-10: WorktreeRail 尚未实现
func (c *CodeAdapter) buildWorktreeRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 WorktreeRail
	return nil
}
