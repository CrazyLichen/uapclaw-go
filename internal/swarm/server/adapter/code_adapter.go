package adapter

import (
	"context"
	"fmt"

	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	cfgPkg "github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
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
	// ⤵️ 10.6.3-10: CodingMemoryRail
	codingMemoryRail sainterfaces.AgentRail
	// worktreeRail 工作树护栏
	// ⤵️ 10.6.3-10: WorktreeRail
	worktreeRail sainterfaces.AgentRail
	// codeAgentRail 编码 Agent 护栏（管理 /agents 创建的自定义 agent）
	// ⤵️ 10.3.7-11 CodeAgentRail
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
//  1. 设置检查点
//  2. 合并实例覆盖配置
//  3. 获取基础配置
//  4. 刷新多模态配置
//  5. config = config_base.get("react", {}).copy()
//  6. self._config_cache = config.copy()
//  7. self._agent_name = overrides.get("agent_name", config.get("agent_name", "main_agent"))
//  8. self._project_dir = overrides.get("project_dir", config.get("project_dir"))
//  9. self._workspace_dir = self._project_dir or config.get("workspace_dir") or get_agent_workspace_dir()
//  10. self._agent_workspace_dir = str(get_agent_workspace_dir())
//  11. self._dreaming_mode = "code"
//  12. model = self._create_model(config_base)
//  13. agent_card = AgentCard(name=self._agent_name, id='jiuwenswarm')
//  14. tool_cards = await self._get_tool_cards(agent_card.id)
//  15. rails_list = self._build_agent_rails(config, config_base, mode="code")
//  16. sys_operation = self._create_sys_operation()
//  17. configured_subagents = self._build_configured_subagents(model, config, config_base)
//  18. self._instance = create_deep_agent(model, card, system_prompt=build_code_system_prompt(), ...)
//  19. await self._instance.ensure_initialized()
//  20. self._seed_runtime_cwd(self._project_dir or self._workspace_dir)
//  21. coding_memory workspace 设置
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

	// 步骤 3: get_config → configBase（委托 DeepAdapter 步骤 5）
	cfg, err := cfgPkg.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}
	configBase, err := cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 步骤 4: ⤵️ 10.6.24 多模态工具: _refresh_multimodal_configs(configBase)

	// 步骤 5-6: 读取 react 配置段，缓存到 configCache（委托 DeepAdapter 步骤 7-8）
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

	// ⤵️ A2X / 11.10: 步骤 13 _try_init_a2x_client(configBase)
	// 步骤 14: ⤵️ agentcore.DeepAgent: agentCard = AgentCard{name, id}
	// 步骤 15: ⤵️ agentcore.DeepAgent: _get_tool_cards("jiuwenswarm") — 编码 tools
	// 步骤 16: ⤵️ 10.6.3-10: _build_agent_rails(config, configBase, mode="code")
	//              编码专有护栏：LspRail, ProjectMemoryRail, CodingMemoryRail,
	//              CodeAgentRail, WorktreeRail, AgentModeRail, StructuredAskUserRail,
	//              ConfirmInterruptRail, FileSystemRail
	// ⤴️ 10.3.7-11 CodeAgentRail: Code 模式专有护栏，管理 /agents 创建的自定义 Agent
	codeAgentRail := c.buildCodeAgentRail()
	if codeAgentRail != nil {
		c.codeAgentRail = codeAgentRail
		// rail 将在 CreateDeepAgent 时通过 params.Rails 传入
		logger.Info(logComponent).Msg("CodeAgentRail 已创建")
	}

	// 步骤 17: _create_sys_operation() — 调用 DeepAdapter.createSysOperation
	// 对齐 Python: sys_operation = self._create_sys_operation()
	sysOp, _ := c.deep.createSysOperation(configBase)
	if sysOp != nil {
		c.deep.sysOperation = sysOp
	}
	// 步骤 18: ⤵️ agentcore.DeepAgent: _build_configured_subagents(model, config, configBase)
	//              固定: explore_agent + plan_agent
	//              按配置: code_agent + browser_agent
	// 步骤 19: ⤵️ agentcore.DeepAgent: c.deep.instance = create_deep_agent(...)
	// 步骤 20: ⤵️ agentcore.DeepAgent: instance.ensure_initialized()
	// 步骤 20.5: _seed_runtime_cwd(c.projectDir or c.workspaceDir)
	// 对齐 Python: self._seed_runtime_cwd(self._project_dir or self._workspace_dir) (interface_code.py:1131)
	initCwd := c.deep.projectDir
	if initCwd == "" {
		initCwd = c.deep.workspaceDir
	}
	c.deep.seedRuntimeCwd(ctx, initCwd)
	// 步骤 21: ⤵️ agentcore.DeepAgent: coding_memory workspace

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
		Msg("CodeAdapter 初始化骨架完成，等待回填")
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
