package adapter

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAdapter Code 模式适配器，组合委托 DeepAdapter。
//
// 继承 JiuWenClawDeepAdapter 的全部接口方法，仅覆盖 CreateInstance。
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
	// ⤵️ 10.3.7-11: LspRail
	lspRail interface{}
	// projectMemoryRail 项目记忆护栏
	// ⤵️ 10.3.7-11: ProjectMemoryRail
	projectMemoryRail interface{}
	// codingMemoryRail 编码记忆护栏
	// ⤵️ 10.3.7-11: CodingMemoryRail
	codingMemoryRail interface{}
	// worktreeRail 工作树护栏
	// ⤵️ 10.3.7-11: WorktreeRail
	worktreeRail interface{}
	// codeAgentRail 编码 Agent 护栏（管理 /agents 创建的自定义 agent）
	// ⤵️ 10.3.7-11: CodeAgentRail
	codeAgentRail interface{}

	// ─── Code 模式配置 ───

	// runtimeLanguageOverride 运行时语言覆盖
	runtimeLanguageOverride string
	// forceEnglishRuntimePrompt 强制英文运行时提示词
	forceEnglishRuntimePrompt bool
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
//   1. await self.set_checkpoint()
//   2. self._instance_overrides = dict(config or {})
//   3. config_base = get_config()
//   4. self._refresh_multimodal_configs(config_base)
//   5. config = config_base.get("react", {}).copy()
//   6. self._config_cache = config.copy()
//   7. self._agent_name = overrides.get("agent_name", config.get("agent_name", "main_agent"))
//   8. self._project_dir = overrides.get("project_dir", config.get("project_dir"))
//   9. self._workspace_dir = self._project_dir or config.get("workspace_dir") or get_agent_workspace_dir()
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

	// ⤵️ 10.3.7-11: 步骤 1  set_checkpoint
	// 步骤 2: instanceOverrides 初始化
	if config != nil {
		c.deep.instanceOverrides = make(map[string]any, len(config))
		for k, v := range config {
			c.deep.instanceOverrides[k] = v
		}
	} else {
		c.deep.instanceOverrides = make(map[string]any)
	}
	// ⤵️ 10.3.7-11: 步骤 3  get_config → configBase
	// ⤵️ 10.3.7-11: 步骤 4  _refresh_multimodal_configs(configBase)
	// ⤵️ 10.3.7-11: 步骤 5-6 读取 react 配置段，缓存到 configCache

	// 步骤 7: agentName
	if v, ok := c.deep.instanceOverrides["agent_name"]; ok {
		if s, ok := v.(string); ok {
			c.deep.agentName = s
		}
	}
	// ⤵️ 10.3.7-11: 步骤 7 完整版需从 configCache 取默认值

	// 步骤 8: projectDir
	if v, ok := c.deep.instanceOverrides["project_dir"]; ok {
		if s, ok := v.(string); ok {
			c.deep.projectDir = s
		}
	}
	// ⤵️ 10.3.7-11: 步骤 8 完整版需从 configCache 取默认值

	// ⤵️ 10.3.7-11: 步骤 9  workspaceDir 优先使用 projectDir
	// ⤵️ 10.3.7-11: 步骤 10 agentWorkspaceDir 始终指向系统 workspace
	// ⤵️ 10.3.7-11: 步骤 12 model = _create_model(configBase)  — 不传多模态配置
	// ⤵️ 10.3.7-11: 步骤 13 agentCard = AgentCard{name, id}
	// ⤵️ 10.3.7-11: 步骤 14 toolCards = _get_tool_cards("jiuwenswarm") — 编码 tools
	// ⤵️ 10.3.7-11: 步骤 15 railsList = _build_agent_rails(config, configBase, mode="code")
	//              编码专有 rails：LspRail, ProjectMemoryRail, CodingMemoryRail,
	//              CodeAgentRail, WorktreeRail, AgentModeRail, StructuredAskUserRail,
	//              ConfirmInterruptRail, FileSystemRail
	// ⤵️ 10.3.7-11: 步骤 16 sysOperation = _create_sys_operation()
	// ⤵️ 10.3.7-11: 步骤 17 subagents = _build_configured_subagents(model, config, configBase)
	//              固定: explore_agent + plan_agent
	//              按配置: code_agent + browser_agent
	// ⤵️ 10.3.7-11: 步骤 18 c.deep.instance = create_deep_agent(
	//              model, card, system_prompt=build_code_system_prompt(),
	//              tools, subagents, rails,
	//              enable_task_loop, max_iterations, workspace, sys_operation, language,
	//              // 不传: vision_model_config, audio_model_config,
	//              //       context_engine_config, completion_timeout
	//            )
	// ⤵️ 10.3.7-11: 步骤 19 instance.ensure_initialized()
	// ⤵️ 10.3.7-11: 步骤 20-21 同 DeepAdapter (seed_cwd, coding_memory workspace, setattr)
	// ⤵️ 10.3.7-11: 步骤 22-24 MCP 重新注册 + load_user_rails

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

// Cleanup 委托 DeepAdapter。
func (c *CodeAdapter) Cleanup() error {
	return c.deep.Cleanup()
}
