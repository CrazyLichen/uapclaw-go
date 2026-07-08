package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/dotenv"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAdapter Deep SDK 适配器，实现 AgentAdapter 接口。
//
// 封装所有 Deep SDK 专属逻辑：
//   - DeepAgent 实例生命周期管理
//   - Deep runtime tools 注册
//   - Deep stream event 解析
//   - Deep evolution 绑定
//   - Deep interrupt / user_answer 处理
//
// 对应 Python: jiuwenswarm/server/runtime/agent_adapter/interface_deep.py (JiuWenClawDeepAdapter)
type DeepAdapter struct {
	// ─── 当前可用字段 ───

	// instance DeepAgent 实例
	// ⤵️ agentcore.DeepAgent
	instance interface{}
	// agentName Agent 名称，默认 "main_agent"
	agentName string
	// projectDir 项目目录
	projectDir string
	// workspaceDir 工作区目录
	workspaceDir string
	// isCodeAgent 是否编码 Agent 形态（Deep=false, Code=true）
	// 单点 source-of-truth，决定沙箱"主写入根"：
	//   - code-agent → project_dir
	//   - deep-agent → workspace_dir
	isCodeAgent bool
	// mode 当前运行模式（agent.plan / agent.fast / code 等）
	mode string
	// subMode 子模式
	subMode string
	// configCache 配置缓存（react 配置段）
	configCache map[string]any
	// activeSessionIDs 会话活跃计数（Counter 语义，允许并发同 session）
	activeSessionIDs map[string]int

	// ─── 模型与配置 ───

	// model Model 实例
	model *llm.Model
	// modelClientConfig 模型客户端配置
	modelClientConfig *llmschema.ModelClientConfig
	// modelRequestConfig 模型请求配置
	modelRequestConfig *llmschema.ModelRequestConfig
	// instanceOverrides 实例覆盖配置
	// ⤵️ 10.3.7-11: create_instance 传入的 config 字典
	instanceOverrides map[string]any
	// modelCache 模型缓存（按模型名缓存已创建的 Model 实例）
	modelCache map[string]*llm.Model
	// modelNameToKeys 模型名到 API key 列表的映射
	modelNameToKeys map[string][]string
	// defaultModelName 默认模型名称
	defaultModelName string

	// ─── ⤵️ 10.6.3-10: Rails ───

	// filesystemRail 文件系统护栏
	// ⤵️ 10.6.3-10: SysOperationRail
	filesystemRail interface{}
	// skillRail 技能使用护栏
	// ⤵️ 10.6.3-10: SkillUseRail
	skillRail interface{}
	// streamEventRail 流事件护栏
	// ⤵️ 10.6.3-10: JiuClawStreamEventRail
	streamEventRail interface{}
	// taskPlanningRail 任务规划护栏
	// ⤵️ 10.6.3-10: TaskPlanningRail
	taskPlanningRail interface{}
	// contextAssembleRail 上下文组装护栏
	// ⤵️ 10.6.3-10: ContextAssembleRail
	contextAssembleRail interface{}
	// contextAssembleMode 当前上下文组装模式（"agent.plan" / "agent.fast"）
	// ⤵️ 10.6.3-10: 按模式切换
	contextAssembleMode string
	// contextProcessorRail 上下文处理护栏
	// ⤵️ 10.6.3-10: ContextProcessorRail
	contextProcessorRail interface{}
	// runtimePromptRail 运行时提示词护栏
	// ⤵️ 10.6.3-10: RuntimePromptRail
	runtimePromptRail interface{}
	// responsePromptRail 响应提示词护栏
	// ⤵️ 10.6.3-10: ResponsePromptRail
	responsePromptRail interface{}
	// securityRail 安全护栏
	// ⤵️ 10.6.3-10: SecurityRail
	securityRail interface{}
	// memoryRail 记忆护栏
	// ⤵️ 10.6.3-10: MemoryRail
	memoryRail interface{}
	// externalMemoryRail 外接记忆护栏
	// ⤵️ 10.6.3-10: 外部记忆 rail
	externalMemoryRail interface{}
	// externalMemoryRailRegistered 外接记忆护栏是否已注册
	// ⤵️ 10.6.3-10: 防止重复注册
	externalMemoryRailRegistered bool
	// heartbeatRail 心跳护栏
	// ⤴️ 9.15 回填：HeartbeatRail
	heartbeatRail *rails.HeartbeatRail
	// skillEvolutionRail 技能演进护栏
	// ⤵️ 10.6.3-10: SkillEvolutionRail
	skillEvolutionRail interface{}
	// skillCreateRail 技能创建护栏
	// ⤵️ 10.6.3-10: SkillCreateRail
	skillCreateRail interface{}
	// subagentRail 子代理护栏
	// ⤵️ 10.6.3-10: SubagentRail
	subagentRail interface{}
	// permissionRail 权限护栏
	// ⤵️ 10.6.3-10: PermissionInterruptRail
	permissionRail interface{}
	// avatarRail 头像护栏
	// ⤵️ 10.6.3-10: AvatarRail
	avatarRail interface{}

	// ─── 运行时 ───

	// toolCards 工具卡片列表
	// ⤵️ agentcore.DeepAgent（工具卡片依赖 agent 实例）
	toolCards interface{}
	// sysOperation 系统操作实例
	// ⤵️ 10.3.7-11 sysop_builder
	sysOperation interface{}
	// sysOperationCard 系统操作卡片
	// ⤵️ 10.3.7-11 sysop_builder
	sysOperationCard interface{}
	// visionModelConfig 视觉模型配置
	// ⤵️ 10.3.7-11 多模态配置
	visionModelConfig interface{}
	// visionToolsRegistered 视觉工具是否已注册
	// ⤵️ 10.3.7-11 多模态工具
	visionToolsRegistered bool
	// audioModelConfig 音频模型配置
	// ⤵️ 10.3.7-11 多模态配置
	audioModelConfig interface{}
	// audioToolsRegistered 音频工具是否已注册
	// ⤵️ 10.3.7-11 多模态工具
	audioToolsRegistered bool
	// videoToolRegistered 视频工具是否已注册
	// ⤵️ 10.3.7-11 多模态工具
	videoToolRegistered bool
	// imageGenToolRegistered 图片生成工具是否已注册
	// ⤵️ 10.3.7-11 多模态工具
	imageGenToolRegistered bool
	// skillManager 技能管理器
	// ⤵️ 10.3.19-20
	skillManager interface{}
	// a2xClient A2X 客户端
	// ⤵️ A2X
	a2xClient interface{}
	// a2xConfig A2X 配置
	// ⤵️ A2X
	a2xConfig map[string]any
	// a2xBlankServiceID A2X blank 服务 ID
	// ⤵️ A2X
	a2xBlankServiceID string
	// a2xBlankDataset A2X blank 数据集
	// ⤵️ A2X
	a2xBlankDataset string
	// cronRuntime Cron 运行时桥接
	// ⤵️ 11.10
	cronRuntime interface{}
	// evolutionWatchers evolution 观察任务集合
	// ⤵️ 10.3.7-11
	evolutionWatchers interface{}
	// dreamingMode dreaming 模式
	dreamingMode string
	// dreamingStarted dreaming 是否已启动
	dreamingStarted bool
	// registeredMCPServerIDs 已注册 MCP 服务 ID 集合
	registeredMCPServerIDs map[string]bool
	// registeredMCPServers 已注册 MCP 服务配置
	// ⤵️ agentcore MCP（McpServerConfig 已定义，适配器层管理未实现）
	registeredMCPServers map[string]any
	// autoHarnessService 自动 Harness 服务
	// ⤵️ 10.6.11-12
	autoHarnessService interface{}
	// sendFileToolkit 发送文件工具包
	// ⤵️ agentcore.DeepAgent
	sendFileToolkit interface{}
	// isProactiveMemory 是否主动记忆
	isProactiveMemory *bool
	// paidSearchRegistered 付费搜索是否已注册
	// ⤵️ 10.3.7-11
	paidSearchRegistered bool
	// paidSearchTool 付费搜索工具实例
	// ⤵️ 10.3.7-11
	paidSearchTool interface{}
}

// ──────────────────────────── 全局变量 ────────────────────────────

// persistentCheckpointerReady 持久化检查点器是否已就绪。
// 对应 Python: interface_deep.py (_PERSISTENT_CHECKPOINTER_READY)
var persistentCheckpointerReady bool

// persistentCheckpointerLock 持久化检查点器初始化锁（double-check locking）。
// 对应 Python: interface_deep.py (_PERSISTENT_CHECKPOINTER_LOCK)
var persistentCheckpointerLock sync.Mutex

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDeepAdapter 创建 DeepAdapter 实例。
//
// 对应 Python: JiuWenClawDeepAdapter.__init__()
func NewDeepAdapter() *DeepAdapter {
	return &DeepAdapter{
		agentName:              "main_agent",
		isCodeAgent:            false,
		activeSessionIDs:       make(map[string]int),
		modelCache:             make(map[string]*llm.Model),
		modelNameToKeys:        make(map[string][]string),
		registeredMCPServerIDs: make(map[string]bool),
		registeredMCPServers:   make(map[string]any),
	}
}

// CreateInstance 初始化底层 SDK Agent。
//
// 对应 Python: JiuWenClawDeepAdapter.create_instance() (line 2527-2621)
//
// Python 执行步骤：
//  1. await self.set_checkpoint()
//  2. self._dreaming_mode = mode if mode.startswith("agent") else "agent"
//  3. self._instance_overrides = dict(config or {})
//  4. load_dotenv(dotenv_path=get_env_file(), override=True)
//  5. config_base = get_config()
//  6. self._refresh_multimodal_configs(config_base)
//  7. config = config_base.get("react", {}).copy()
//  8. self._config_cache = config.copy()
//  9. self._agent_name = overrides.get("agent_name", config.get("agent_name", "main_agent"))
//  10. self._project_dir = overrides.get("project_dir", config.get("project_dir"))
//  11. self._workspace_dir = config.get("workspace_dir", str(get_agent_workspace_dir()))
//  12. model = self._create_model(config_base)
//  13. await self._try_init_a2x_client(config_base)
//  14. agent_card = AgentCard(name=self._agent_name, id='jiuwenswarm')
//  15. tool_cards = await self._get_tool_cards(agent_card.id)
//  16. rails_list = self._build_agent_rails(config, config_base, mode=mode)
//  17. sys_operation = self._create_sys_operation()
//  18. configured_subagents, should_add_general_agent = self._build_configured_subagents(...)
//  19. self._instance = create_deep_agent(**common_kwargs)
//  20. await self._instance.ensure_initialized()
//  21. self._seed_runtime_cwd(self._project_dir or self._workspace_dir)
//  22. self._sync_a2x_runtime_state()
//  23. self._registered_mcp_server_ids.clear()
//  24. await self._register_mcp_servers_from_config(config_base, tag=f"agent.{mode}")
//  25. await self.load_user_rails()
func (d *DeepAdapter) CreateInstance(ctx context.Context, configMap map[string]any, mode string, subMode string) error {
	// 步骤 2: dreaming_mode 设置
	if mode != "" && strings.HasPrefix(mode, "agent") {
		d.dreamingMode = mode
	} else {
		d.dreamingMode = "agent"
	}

	// 步骤 3: instanceOverrides
	if configMap != nil {
		d.instanceOverrides = make(map[string]any, len(configMap))
		for k, v := range configMap {
			d.instanceOverrides[k] = v
		}
	} else {
		d.instanceOverrides = make(map[string]any)
	}

	// 步骤 1: set_checkpoint
	if err := d.setCheckpoint(); err != nil {
		return fmt.Errorf("set_checkpoint 失败: %w", err)
	}

	// 步骤 4: load_dotenv
	if err := dotenv.Load(workspace.EnvFile()); err != nil {
		logger.Warn(logComponent).Err(err).Msg("load_dotenv 失败，继续使用当前环境变量")
	}

	// 步骤 5: get_config → configBase
	cfg, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}
	configBase, err := cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 步骤 6: _refresh_multimodal_configs(configBase)
	// ⤵️ 10.3.7-11: 多模态配置刷新（vision/audio/video/image_gen）

	// 步骤 7-8: 读取 react 配置段，缓存到 configCache
	if reactRaw, ok := configBase["react"]; ok {
		if reactMap, ok := reactRaw.(map[string]any); ok {
			d.configCache = make(map[string]any, len(reactMap))
			for k, v := range reactMap {
				d.configCache[k] = v
			}
		}
	} else {
		d.configCache = make(map[string]any)
	}

	// 步骤 9: agentName（完整版：优先 overrides，其次 configCache）
	if v, ok := d.instanceOverrides["agent_name"]; ok {
		if s, ok := v.(string); ok {
			d.agentName = s
		}
	} else if v, ok := d.configCache["agent_name"]; ok {
		if s, ok := v.(string); ok {
			d.agentName = s
		}
	}

	// 步骤 10: projectDir（完整版：优先 overrides，其次 configCache）
	if v, ok := d.instanceOverrides["project_dir"]; ok {
		if s, ok := v.(string); ok {
			d.projectDir = s
		}
	} else if v, ok := d.configCache["project_dir"]; ok {
		if s, ok := v.(string); ok {
			d.projectDir = s
		}
	}

	// 步骤 11: workspaceDir（从 configCache 或 get_agent_workspace_dir() 获取）
	if v, ok := d.configCache["workspace_dir"]; ok {
		if s, ok := v.(string); ok && s != "" {
			d.workspaceDir = s
		}
	}
	if d.workspaceDir == "" {
		d.workspaceDir = workspace.AgentRootDir()
	}

	// 存储 mode/subMode
	d.mode = mode
	d.subMode = subMode

	// 步骤 12: model = d.createModel(configBase)
	d.model = d.createModel(configBase)

	// 步骤 13: ⤵️ A2X: _try_init_a2x_client(configBase)

	// 步骤 14: agentCard = schema.AgentCard{Name: d.agentName, ID: "jiuwenswarm"}
	// agentCard := &schema.AgentCard{Name: d.agentName, ID: "jiuwenswarm"}
	// ⤵️ agentcore.DeepAgent: agentCard 用于步骤 15-19

	// 步骤 15: ⤵️ agentcore.DeepAgent: _get_tool_cards(agentCard.ID)
	// 步骤 16: ⤵️ 10.6.3-10: _build_agent_rails(config, configBase, mode=mode)
	// 步骤 17: ⤵️ 10.3.7-11: _create_sys_operation()
	// 步骤 18: ⤵️ agentcore.DeepAgent: _build_configured_subagents(model, config, configBase)
	// 步骤 19: ⤵️ agentcore.DeepAgent: d.instance = create_deep_agent(...)
	// 步骤 20: ⤵️ agentcore.DeepAgent: d.instance.ensure_initialized()
	// 步骤 21: ⤵️ agentcore.DeepAgent: _seed_runtime_cwd(d.projectDir or d.workspaceDir)
	// 步骤 22: ⤵️ A2X: _sync_a2x_runtime_state()

	// 步骤 23: d.registeredMCPServerIDs = make(map[string]bool)（可回填）
	d.registeredMCPServerIDs = make(map[string]bool)
	d.registeredMCPServers = make(map[string]any)

	// 步骤 24: ⤵️ agentcore MCP: _register_mcp_servers_from_config(configBase, tag)
	// 步骤 25: ⤵️ 10.6.3-10: load_user_rails()

	logger.Info(logComponent).
		Str("agent_name", d.agentName).
		Str("mode", mode).
		Str("sub_mode", subMode).
		Msg("DeepAdapter 初始化骨架完成，等待回填")
	return nil
}

// ReloadAgentConfig 热重载配置，不重启进程。
//
// 对应 Python: JiuWenClawDeepAdapter.reload_agent_config() (line 2646-2752)
//
// Python 执行步骤：
//  1. config_base = configBase or get_config()
//  2. if envOverrides: apply env overrides
//  3. config = config_base.get("react", {}).copy()
//  4. self._config_cache = config.copy()
//  5. self._refresh_multimodal_configs(config_base)
//  6. model = self._create_model(config_base)
//  7. self._model = model
//  8. rails_list = self._get_current_agent_rails(config, config_base)
//  9. new_tool_cards = await self._get_tool_cards("jiuwenswarm")
//  10. self._update_permission_rail(config_base)
//  11. await self._instance.configure(
//     model=model, tools=new_tool_cards, rails=rails_list,
//     subagents=subagents, enable_task_loop=..., max_iterations=...)
//  12. self._registered_mcp_server_ids.clear()
//  13. await self._register_mcp_servers_from_config(config_base, tag)
func (d *DeepAdapter) ReloadAgentConfig(ctx context.Context, configBase map[string]any, envOverrides map[string]any) error {
	// ⤵️ 10.3.7-11: 步骤 1-13 完整实现
	// 步骤 1: configBase 或 get_config()
	// 步骤 2: 应用环境变量覆盖
	// 步骤 3-4: 读取 react 配置段，更新 configCache
	// 步骤 5: _refresh_multimodal_configs(configBase)
	// 步骤 6-7: 重建模型
	// 步骤 8: _get_current_agent_rails(config, configBase)
	// 步骤 9: _get_tool_cards("jiuwenswarm")
	// 步骤 10: _update_permission_rail(configBase)
	// 步骤 11: instance.configure(model, tools, rails, subagents, ...)
	// 步骤 12-13: 重新注册 MCP

	logger.Info(logComponent).Msg("DeepAdapter ReloadAgentConfig 骨架，等待回填")
	return nil
}

// ProcessMessageImpl 执行非流式请求，返回完整响应。
//
// 对应 Python: JiuWenClawDeepAdapter.process_message_impl() (line 4409-4512)
//
// Python 执行步骤：
//  1. if self._instance is None: raise RuntimeError("未初始化")
//  2. _req_model = request.params.get("model_name", "")
//  3. if not self._has_valid_model_config(_req_model): return error response
//  4. session_id = request.session_id or "default"
//  5. query = request.params.get("query", "")
//  6. mode = request.params.get("mode", "agent.plan")
//  7. slash_result = await self._handle_slash_command(query, session_id, mode)
//  8. if slash_result: handle approval_chunks or content
//  9. cron_context_tokens = self._bind_runtime_cron_context(...)
//  10. token_cid = TOOL_PERMISSION_CHANNEL_ID.set(...)
//  11. token_perm = setup_permission_context(request)
//  12. resolved_model = self._resolve_model_for_request(request)
//  13. self._apply_model_to_react_agent(resolved_model)
//  14. self._mark_session_active(session_id)
//  15. if self._stream_event_rail: self._stream_event_rail.reset_abort(session_id)
//  16. try:
//  17. await self._update_runtime_config(runtimeConfig)
//  18. result = await Runner.run_agent(agent=self._instance, inputs=inputs)
//  19. except asyncio.CancelledError: ...
//  20. finally: cleanup (unmark_session_active, reset context vars)
//  21. return AgentResponse from result
func (d *DeepAdapter) ProcessMessageImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (*schema.AgentResponse, error) {
	// 步骤 1: 实例 nil 检查
	if d.instance == nil {
		return nil, fmt.Errorf("DeepAdapter 未初始化，请先调用 CreateInstance()")
	}

	// 步骤 4: session_id 规范化
	params := parseParams(req.Params)
	sessionID := "default"
	if req.SessionID != nil && *req.SessionID != "" {
		sessionID = *req.SessionID
	}

	// 步骤 5-6: 提取 query/mode
	query := paramsString(params, "query", "")
	mode := paramsString(params, "mode", "agent.plan")

	// 步骤 2-3: 模型配置校验
	reqModelName := paramsString(params, "model_name", "")
	if !d.hasValidModelConfig(reqModelName) {
		return schema.NewAgentResponse(req.RequestID, req.ChannelID,
			schema.WithPayload(map[string]any{
				"ok":    false,
				"error": fmt.Sprintf("请求的模型 %q 未配置", reqModelName),
			}),
		), nil
	}

	// ⤵️ 10.3.7-11: 步骤 7-8  slash 命令处理
	// ⤵️ 11.10: 步骤 9    cron 上下文绑定
	// ⤵️ 10.1.8: 步骤 10-11 权限上下文设置

	// 步骤 12-13: 模型选择
	resolvedModel := d.resolveModelForRequest(req)
	// ⤵️ agentcore.DeepAgent: _apply_model_to_react_agent(resolvedModel)

	// 步骤 14: mark_session_active
	d.markSessionActive(sessionID)

	// ⤵️ 10.6.3-10: 步骤 15 streamEventRail.reset_abort(sessionID)
	// ⤵️ agentcore.DeepAgent: 步骤 16-17 update_runtime_config
	// ⤵️ agentcore.Runner: 步骤 18 Runner.run_agent
	// ⤵️ 10.6.3-10: 步骤 19-20 异常处理 + 清理（unmark_session_active）

	_ = query
	_ = mode
	_ = resolvedModel

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("mode", mode).
		Str("query", query).
		Msg("DeepAdapter ProcessMessageImpl 骨架，等待回填")

	return nil, fmt.Errorf("ProcessMessageImpl 骨架，等待 agentcore.Runner 回填")
}

// ProcessMessageStreamImpl 执行流式请求，通过 channel 返回响应块。
//
// 对应 Python: JiuWenClawDeepAdapter.process_message_stream_impl() (line 4514-4750)
//
// Python 执行步骤：
//  1. if self._instance is None: raise RuntimeError("未初始化")
//  2. _req_model = request.params.get("model_name", "")
//  3. if not self._has_valid_model_config(_req_model): yield error chunk; return
//  4. session_id = request.session_id or "default"
//  5. query = request.params.get("query", "")
//  6. mode = request.params.get("mode", "agent.plan")
//  7. if mode in ("team", "team.plan", "code.team"): → team_helpers.process_team_message_stream
//  8. if mode == "auto_harness": → auto_harness 分流
//  9. slash_result = await self._handle_slash_command(query, session_id, mode)
//  10. cron_context_tokens = self._bind_runtime_cron_context(...)
//  11. token_cid = TOOL_PERMISSION_CHANNEL_ID.set(...)
//  12. token_perm = setup_permission_context(request)
//  13. resolved_model = self._resolve_model_for_request(request)
//  14. self._apply_model_to_react_agent(resolved_model)
//  15. self._mark_session_active(session_id)
//  16. try:
//  17. await self._update_runtime_config(runtimeConfig)
//  18. async for chunk in Runner.run_agent_streaming(agent=self._instance, inputs=inputs):
//  19. yield chunk
//  20. except asyncio.CancelledError: ...
//  21. finally: cleanup
func (d *DeepAdapter) ProcessMessageStreamImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (<-chan *schema.AgentResponseChunk, error) {
	// 步骤 1: 实例 nil 检查
	if d.instance == nil {
		ch := make(chan *schema.AgentResponseChunk)
		close(ch)
		return ch, fmt.Errorf("DeepAdapter 未初始化，请先调用 CreateInstance()")
	}

	// 步骤 4: session_id 规范化
	params := parseParams(req.Params)
	sessionID := "default"
	if req.SessionID != nil && *req.SessionID != "" {
		sessionID = *req.SessionID
	}

	// 步骤 5-6: 提取 query/mode
	mode := paramsString(params, "mode", "agent.plan")

	// 步骤 2-3: 模型配置校验
	reqModelName := paramsString(params, "model_name", "")
	if !d.hasValidModelConfig(reqModelName) {
		ch := make(chan *schema.AgentResponseChunk)
		close(ch)
		// ⤵️ agentcore.Runner: 需要构造错误 chunk 发送到 channel
		return ch, nil
	}

	// ⤵️ 10.3.7-11: 步骤 7    team 模式分流（"team"/"team.plan"/"code.team"）
	// ⤵️ 10.6.11-12: 步骤 8  auto_harness 分流
	// ⤵️ 10.3.7-11: 步骤 9    slash 命令处理
	// ⤵️ 11.10: 步骤 10   cron 上下文绑定
	// ⤵️ 10.1.8: 步骤 11-12 权限上下文设置

	// 步骤 13-14: 模型选择
	resolvedModel := d.resolveModelForRequest(req)
	// ⤵️ agentcore.DeepAgent: _apply_model_to_react_agent(resolvedModel)

	// 步骤 15: mark_session_active
	d.markSessionActive(sessionID)

	// ⤵️ agentcore.DeepAgent: 步骤 16-17 update_runtime_config
	// ⤵️ agentcore.Runner: 步骤 18-19 Runner.run_agent_streaming
	// ⤵️ 10.6.3-10: 步骤 20-21 异常处理 + 清理

	_ = mode
	_ = resolvedModel

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("mode", mode).
		Msg("DeepAdapter ProcessMessageStreamImpl 骨架，等待回填")

	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, fmt.Errorf("ProcessMessageStreamImpl 骨架，等待 agentcore.Runner 回填")
}

// ProcessInterrupt 处理中断请求（pause/resume/cancel/supplement）。
//
// 对应 Python: JiuWenClawDeepAdapter.process_interrupt() (line 3268-3578)
//
// Python 执行步骤：
//  1. intent = request.params.get("intent", "cancel")
//  2. new_input = request.params.get("new_input")
//  3. _normalized_sid = request.session_id or "default"
//  4. _session_is_active = self._is_session_active(_normalized_sid)
//  5. if not _session_is_active: log & skip abort operations
//  6. if intent == "pause": streamEventRail.pause(session_id)
//  7. elif intent == "resume": streamEventRail.resume(session_id)
//  8. elif intent == "supplement": abort + optional instance.abort()
//  9. elif intent == "cancel": abort + unmark_session_active
//  10. cleanup evolution watchers
//  11. return AgentResponse with interrupt_result
func (d *DeepAdapter) ProcessInterrupt(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 步骤 1-2: 解析 intent 和 new_input
	params := parseParams(req.Params)
	intent := paramsString(params, "intent", "cancel")
	newInput := params["new_input"] // 可能为 nil

	// 步骤 3-4: session 规范化 + 活跃检查
	normalizedSID := "default"
	if req.SessionID != nil && *req.SessionID != "" {
		normalizedSID = *req.SessionID
	}
	sessionActive := d.isSessionActive(normalizedSID)

	if !sessionActive {
		logger.Info(logComponent).
			Str("intent", intent).
			Str("session_id", normalizedSID).
			Msg("interrupt: session 不活跃，跳过 abort 操作")
	}

	// 步骤 6-9: 按 intent 分支
	switch intent {
	case "pause":
		// ⤵️ 10.6.3-10: if sessionActive && d.streamEventRail != nil { d.streamEventRail.pause(normalizedSID) }
		logger.Info(logComponent).Str("intent", "pause").Msg("中断: 已暂停执行")
	case "resume":
		// ⤵️ 10.6.3-10: if sessionActive && d.streamEventRail != nil { d.streamEventRail.resume(normalizedSID) }
		logger.Info(logComponent).Str("intent", "resume").Msg("中断: 已恢复执行")
	case "supplement":
		if newInput != nil {
			d.markSessionActive(normalizedSID)
		}
		logger.Info(logComponent).Str("intent", "supplement").Msg("中断: supplement 处理")
	case "cancel":
		d.unmarkSessionActive(normalizedSID)
		logger.Info(logComponent).Str("intent", "cancel").Msg("中断: cancel 处理")
	}

	// 步骤 10: 清理 evolution watchers
	// ⤵️ 10.3.7-11: cancel evolution watcher tasks

	// 步骤 11: 构造响应
	return schema.NewAgentResponse(req.RequestID, req.ChannelID), nil
}

// HandleUserAnswer 处理用户回答（evolution 审批或权限审批）。
//
// 对应 Python: JiuWenClawDeepAdapter.handle_user_answer() (line 3579-3605)
//
// Python 执行步骤：
//  1. request_id = request.params.get("request_id", "")
//  2. answers = request.params.get("answers", [])
//  3. session_id = request.session_id
//  4. resolved = False
//  5. if request_id.startswith("team_skill_evolve_"): resolved = handle_team_skill_evolve_approval(...)
//  6. elif request_id.startswith("evolve_simplify_"): resolved = _handle_governance_approval(...)
//  7. elif request_id.startswith("skill_evolve_"): resolved = _handle_evolution_approval(...)
//  8. return AgentResponse(ok=True, payload={"accepted": True, "resolved": resolved})
func (d *DeepAdapter) HandleUserAnswer(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 步骤 1-2: 解析 request_id 和 answers
	params := parseParams(req.Params)
	requestID := paramsString(params, "request_id", "")

	// 步骤 4: resolved 默认 false
	resolved := false

	// 步骤 5-7: 按 request_id 前缀分发
	switch {
	case strings.HasPrefix(requestID, "team_skill_evolve_"):
		// ⤵️ 10.3.7-11: handle_team_skill_evolve_approval(requestID, answers, sessionID, channelID)
	case strings.HasPrefix(requestID, "evolve_simplify_"):
		// ⤵️ 10.3.7-11: _handle_governance_approval(requestID, answers, "simplify")
	case strings.HasPrefix(requestID, "skill_evolve_"):
		// ⤵️ 10.3.7-11: _handle_evolution_approval(requestID, answers)
	}

	// 步骤 8: 构造响应
	return schema.NewAgentResponse(req.RequestID, req.ChannelID,
		schema.WithPayload(map[string]any{"accepted": true, "resolved": resolved}),
	), nil
}

// HandleHeartbeat 处理心跳请求。
//
// 对应 Python: JiuWenClawDeepAdapter.handle_heartbeat() (line 3607-3624)
//
// Python 执行步骤：
//  1. sid = str(request.session_id or "")
//  2. if not sid.startswith("heartbeat"): return None
//  3. request.params["query"] = "这是一次心跳请求任务..."
//  4. log heartbeat query injected
//  5. return None（继续正常流程，query 已注入）
//
// 返回 nil 表示非心跳请求或心跳已处理（query 已注入），上层应继续正常流程。
func (d *DeepAdapter) HandleHeartbeat(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 步骤 1: session_id 前缀检查
	sid := ""
	if req.SessionID != nil {
		sid = *req.SessionID
	}

	// 步骤 2: 非 heartbeat → 返回 nil
	if !strings.HasPrefix(sid, "heartbeat") {
		return nil, nil
	}

	// 步骤 3: 注入心跳 prompt
	heartbeatQuery := "这是一次心跳请求任务，请根据</heartbeat_user_task>标签中的内容进行回复"
	if len(req.Params) > 0 {
		var params map[string]any
		if err := json.Unmarshal(req.Params, &params); err != nil {
			params = make(map[string]any)
		}
		params["query"] = heartbeatQuery
		if updated, err := json.Marshal(params); err == nil {
			req.Params = updated
		}
	} else {
		req.Params = json.RawMessage(`{"query":"` + heartbeatQuery + `"}`)
	}

	// 步骤 4: 日志
	logger.Info(logComponent).
		Str("request_id", req.RequestID).
		Str("session_id", sid).
		Msg("heartbeat query 已注入")

	// 步骤 5: 返回 nil，继续正常流程
	return nil, nil
}

// Cleanup 清理适配器资源。
//
// 对应 Python: JiuWenClawDeepAdapter.cleanup() (line 3245-3248)
//
// Python 执行步骤：
//  1. await self._close_a2x_client()
func (d *DeepAdapter) Cleanup() error {
	// 步骤 1: 关闭 a2x 客户端
	// ⤵️ A2X: _close_a2x_client()

	logger.Info(logComponent).Msg("DeepAdapter Cleanup 骨架，等待回填")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensurePersistentCheckpointer 确保进程级默认检查点器使用 SQLite 持久化。
//
// 对应 Python: interface_deep.py ensure_persistent_checkpointer() (line 393-424)
//
// 使用 double-check locking 模式保证只初始化一次。
// 初始化步骤：
//  1. 若已就绪则直接返回
//  2. 加锁后再次检查（double-check）
//  3. 调用 PersistenceCheckpointerProvider() 触发注册
//  4. 获取 checkpoint_dir，构造 SQLite 路径
//  5. 通过 CheckpointerFactory.Create 创建 persistence 实例
//  6. SetDefaultCheckpointer 设为全局默认
func ensurePersistentCheckpointer() error {
	if persistentCheckpointerReady {
		return nil
	}

	persistentCheckpointerLock.Lock()
	defer persistentCheckpointerLock.Unlock()

	if persistentCheckpointerReady {
		return nil
	}

	// 步骤 3: PersistenceCheckpointerProvider() — Go 中 persistence provider 已在 init() 时注册
	// 无需显式调用，等价于 Python 的 PersistenceCheckpointerProvider()

	// 步骤 4: 获取 checkpoint 目录
	checkpointDir := workspace.CheckpointDir()
	dbPath := filepath.Join(checkpointDir, "checkpoint")

	// 步骤 5-6: 创建 persistence checkpointer 并设为全局默认
	cp, err := checkpointer.CreateCheckpointer(
		context.Background(),
		checkpointer.CheckpointerFactoryConfig{
			Type: "persistence",
			Conf: map[string]any{
				"db_type": "sqlite",
				"db_path": dbPath,
			},
		},
	)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("db_path", dbPath).
			Msg("持久化检查点器初始化失败")
		return fmt.Errorf("持久化检查点器初始化失败: %w", err)
	}

	checkpointer.SetDefaultCheckpointer(cp)
	persistentCheckpointerReady = true

	logger.Info(logComponent).
		Str("db_path", dbPath+".db").
		Msg("持久化检查点器已就绪")

	return nil
}

// setCheckpoint 设置持久化检查点。
//
// 对应 Python: JiuWenClawDeepAdapter.set_checkpoint() (line 1479-1480)
//
// Python 实现：
//
//	async def set_checkpoint() -> None:
//	    await ensure_persistent_checkpointer()
//
// Go 中 ensurePersistentCheckpointer 已同步化（SQLite 打开无需异步），
// 后续将检查点器绑定到 DeepAgent 实例的步骤需等 DeepAgent 实现后回填。
func (d *DeepAdapter) setCheckpoint() error {
	return ensurePersistentCheckpointer()
	// ⤵️ agentcore.DeepAgent: 将 checkpointer 绑定到 d.instance（DeepAgent）的 session 上
}

// buildModelFromEntry 根据单个模型条目的 model_client_config / model_config_obj 构建 Model 实例。
//
// 对应 Python: JiuWenClawDeepAdapter._build_model_from_entry() (line 1483-1493)
func (d *DeepAdapter) buildModelFromEntry(mcc map[string]any, mco map[string]any) (*llm.Model, error) {
	name, _ := mcc["model_name"].(string)

	// 构建 ModelRequestConfig
	temperature := 0.95
	if v, ok := mco["temperature"]; ok {
		if f, ok := v.(float64); ok {
			temperature = f
		}
	}
	topP := 0.1
	if v, ok := mco["top_p"]; ok {
		if f, ok := v.(float64); ok {
			topP = f
		}
	}
	mConfig := llmschema.NewModelRequestConfig(
		llmschema.WithModelName(name),
		llmschema.WithTemperature(temperature),
		llmschema.WithTopP(topP),
	)

	// 构建 ModelClientConfig：从 mcc 提取字段
	provider, _ := mcc["client_provider"].(string)
	if provider == "" {
		provider = "OpenAI"
	}
	apiKey, _ := mcc["api_key"].(string)
	apiBase, _ := mcc["api_base"].(string)

	clientConfigOpts := make([]llmschema.ModelClientConfigOption, 0)
	if v, ok := mcc["timeout"]; ok {
		if f, ok := v.(float64); ok {
			clientConfigOpts = append(clientConfigOpts, llmschema.WithTimeout(f))
		}
	}
	if v, ok := mcc["max_retries"]; ok {
		switch n := v.(type) {
		case float64:
			clientConfigOpts = append(clientConfigOpts, llmschema.WithMaxRetries(int(n)))
		case int:
			clientConfigOpts = append(clientConfigOpts, llmschema.WithMaxRetries(n))
		}
	}
	if v, ok := mcc["verify_ssl"]; ok {
		if b, ok := v.(bool); ok {
			clientConfigOpts = append(clientConfigOpts, llmschema.WithVerifySSL(b))
		}
	}
	if v, ok := mcc["ssl_cert"]; ok {
		if s, ok := v.(string); ok {
			clientConfigOpts = append(clientConfigOpts, llmschema.WithSSLCert(s))
		}
	}

	// 提取 extra 字段（排除已知字段）
	extra := make(map[string]any)
	knownKeys := map[string]struct{}{
		"model_name": {}, "client_provider": {}, "api_key": {}, "api_base": {},
		"timeout": {}, "max_retries": {}, "verify_ssl": {}, "ssl_cert": {},
		"custom_headers": {}, "client_id": {},
	}
	for k, v := range mcc {
		if _, ok := knownKeys[k]; !ok {
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithConfigExtra(extra))
	}

	clientConfig := llmschema.NewModelClientConfig(provider, apiKey, apiBase, clientConfigOpts...)

	return llm.NewModel(clientConfig, mConfig)
}

// buildModelCacheFromDefaults 从 models.defaults 列表构建模型缓存。
//
// 对应 Python: JiuWenClawDeepAdapter._build_model_cache_from_defaults() (line 1495-1533)
//
// key 使用 {model_name}#{index} 格式以支持同名模型共存。
// 同时记录 modelNameToKeys 映射以便按 model_name 查找。
func (d *DeepAdapter) buildModelCacheFromDefaults(configBase map[string]any) {
	d.modelNameToKeys = make(map[string][]string)
	nameCounter := make(map[string]int)

	defaults := getDefaultModels(configBase)
	for _, entry := range defaults {
		mcc, _ := entry["model_client_config"].(map[string]any)
		if mcc == nil {
			continue
		}
		modelName, _ := mcc["model_name"].(string)
		if modelName == "" {
			continue
		}

		idx := nameCounter[modelName]
		nameCounter[modelName] = idx + 1
		cacheKey := fmt.Sprintf("%s#%d", modelName, idx)

		mco, _ := entry["model_config_obj"].(map[string]any)
		if mco == nil {
			mco = make(map[string]any)
		}
		m, err := d.buildModelFromEntry(mcc, mco)
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("model_name", modelName).
				Msg("跳过无效模型条目")
			continue
		}
		d.modelCache[cacheKey] = m
		d.modelNameToKeys[modelName] = append(d.modelNameToKeys[modelName], cacheKey)

		// 同时用纯 model_name 作为 key 指向 is_default=true 的条目
		if isDefault, _ := entry["is_default"].(bool); isDefault {
			d.modelCache[modelName] = m
		}

		alias, _ := entry["alias"].(string)
		if alias != "" && alias != modelName {
			if _, exists := d.modelCache[alias]; !exists {
				d.modelCache[alias] = m
			}
		}
	}
}

// buildModelCacheLegacy 回退到旧格式（models.default / react 段）构建单条目缓存。
//
// 对应 Python: JiuWenClawDeepAdapter._build_model_cache_legacy() (line 1535-1560)
func (d *DeepAdapter) buildModelCacheLegacy(configBase map[string]any) {
	modelsSection, _ := configBase["models"].(map[string]any)
	reactSection, _ := configBase["react"].(map[string]any)

	var defaultModelConfig map[string]any
	if modelsSection != nil {
		defaultModelConfig, _ = modelsSection["default"].(map[string]any)
	}

	var mcc map[string]any
	if defaultModelConfig != nil {
		if v, ok := defaultModelConfig["model_client_config"].(map[string]any); ok {
			mcc = v
		}
	}
	if mcc == nil && reactSection != nil {
		if v, ok := reactSection["model_client_config"].(map[string]any); ok {
			mcc = v
		}
	}
	if mcc == nil {
		mcc = make(map[string]any)
	}

	modelName, _ := mcc["model_name"].(string)
	if modelName == "" && reactSection != nil {
		if v, ok := reactSection["model_name"].(string); ok {
			modelName = v
		}
	}
	if modelName == "" {
		modelName = "gpt-4"
	}
	if _, ok := mcc["model_name"]; !ok {
		mcc["model_name"] = modelName
	}

	var mco map[string]any
	if defaultModelConfig != nil {
		if v, ok := defaultModelConfig["model_config_obj"].(map[string]any); ok {
			mco = v
		}
	}
	if mco == nil && reactSection != nil {
		if v, ok := reactSection["model_config_obj"].(map[string]any); ok {
			mco = v
		}
	}
	if mco == nil {
		mco = make(map[string]any)
	}

	m, err := d.buildModelFromEntry(mcc, mco)
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Str("model_name", modelName).
			Msg("跳过无效模型条目(legacy)")
		return
	}
	d.modelCache[modelName] = m
}

// createModel 从配置创建 Model 实例并构建模型缓存。
//
// 对应 Python: JiuWenClawDeepAdapter._create_model() (line 1562-1593)
func (d *DeepAdapter) createModel(configBase map[string]any) *llm.Model {
	d.modelCache = make(map[string]*llm.Model)
	d.buildModelCacheFromDefaults(configBase)
	if len(d.modelCache) == 0 {
		d.buildModelCacheLegacy(configBase)
	}

	if len(d.modelCache) == 0 {
		logger.Error(logComponent).Msg("配置中未找到有效的模型条目")
		return nil
	}

	// 优先取 is_default=true 的条目（纯 model_name key），否则取第一个
	defaultName := ""
	for name := range d.modelNameToKeys {
		if _, ok := d.modelCache[name]; ok {
			defaultName = name
			break
		}
	}
	if defaultName == "" {
		// 回退：取第一个 #index key
		for key := range d.modelCache {
			if strings.Contains(key, "#") {
				defaultName = key
				break
			}
		}
	}
	if defaultName == "" {
		// 最后回退：取第一个 key
		for key := range d.modelCache {
			defaultName = key
			break
		}
	}

	d.defaultModelName = defaultName
	d.model = d.modelCache[defaultName]
	if d.model != nil {
		d.modelClientConfig = d.model.ClientConfig
		d.modelRequestConfig = d.model.ModelConfig
	}

	logger.Info(logComponent).
		Str("default_model_name", d.defaultModelName).
		Int("model_cache_size", len(d.modelCache)).
		Msg("模型缓存构建完成")

	return d.model
}

// resolveModelForRequest 根据请求中的 model_name 参数查找对应模型。
//
// 对应 Python: JiuWenClawDeepAdapter._resolve_model_for_request() (line 1595-1612)
//
// 支持两种格式：
//   - 纯 model_name：查找 is_default=true 的条目
//   - {model_name}#{index}：查找指定索引的条目
func (d *DeepAdapter) resolveModelForRequest(req *schema.AgentRequest) *llm.Model {
	params := parseParams(req.Params)
	requested := ""
	if v, ok := params["model_name"]; ok {
		if s, ok := v.(string); ok {
			requested = strings.TrimSpace(s)
		}
	}
	if requested == "" {
		return d.model
	}

	// 精确匹配（#index 格式或纯 model_name key）
	if m, ok := d.modelCache[requested]; ok {
		return m
	}

	// 回退：按纯 model_name 查找 is_default=true 的条目
	if _, ok := d.modelNameToKeys[requested]; ok {
		if m, ok := d.modelCache[requested]; ok {
			return m
		}
	}

	return d.model
}

// hasValidModelConfig 检查请求的模型名是否有有效配置。
//
// 对应 Python: JiuWenClawDeepAdapter._has_valid_model_config() (line ~4389)
func (d *DeepAdapter) hasValidModelConfig(requestedModelName string) bool {
	if requestedModelName == "" {
		return true // 空字符串表示使用默认模型
	}
	_, ok := d.modelCache[requestedModelName]
	return ok
}

// markSessionActive 递增 session 活跃任务计数。
//
// 对应 Python: _mark_session_active() (line 576-578)
// Counter 语义：允许并发同 session（如 supplement 同时旧任务还在），
// 避免第一个任务结束时驱逐第二个。
func (d *DeepAdapter) markSessionActive(sessionID string) {
	d.activeSessionIDs[sessionID]++
}

// unmarkSessionActive 递减 session 活跃任务计数，归零时移除。
//
// 对应 Python: _unmark_session_active() (line 580-599)
// 归零时清理 StreamEventRail 的 per-session 状态，防止长期运行内存泄漏。
func (d *DeepAdapter) unmarkSessionActive(sessionID string) {
	count := d.activeSessionIDs[sessionID]
	if count <= 1 {
		delete(d.activeSessionIDs, sessionID)
		// ⤵️ 10.6.3-10: if d.streamEventRail != nil { d.streamEventRail.cleanup_session(sessionID) }
	} else {
		d.activeSessionIDs[sessionID] = count - 1
	}
}

// isSessionActive 检查 session 是否有活跃任务。
//
// 对应 Python: _is_session_active() (line 601-603)
func (d *DeepAdapter) isSessionActive(sessionID string) bool {
	return d.activeSessionIDs[sessionID] > 0
}

// otherActiveSessions 返回除指定 session 外的活跃任务总数。
//
// 对应 Python: _other_active_sessions() (line 605-610)
func (d *DeepAdapter) otherActiveSessions(sessionID string) int {
	total := 0
	for sid, count := range d.activeSessionIDs {
		if sid != sessionID {
			total += count
		}
	}
	return total
}

// parseParams 将 json.RawMessage 解析为 map[string]any。
// 对应 Python 中 request.params 直接作为 dict 使用。
func parseParams(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return make(map[string]any)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return make(map[string]any)
	}
	return m
}

// paramsString 从 params 中取字符串值，支持默认值。
// 对应 Python: request.params.get(key, default)
func paramsString(params map[string]any, key string, defaultVal string) string {
	v, ok := params[key]
	if !ok {
		return defaultVal
	}
	s, ok := v.(string)
	if !ok {
		return defaultVal
	}
	return s
}

// getDefaultModels 从配置获取默认模型列表。
//
// 对应 Python: common/config.py get_default_models() (line 697+)
//
// 优先级：models.defaults（列表） > models.default（单对象）
func getDefaultModels(configBase map[string]any) []map[string]any {
	modelsSection, _ := configBase["models"].(map[string]any)
	if modelsSection == nil {
		return nil
	}

	// 新格式：defaults 列表
	if defaults, ok := modelsSection["defaults"].([]any); ok && len(defaults) > 0 {
		result := make([]map[string]any, 0, len(defaults))
		for _, entry := range defaults {
			if m, ok := entry.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	}

	// 旧格式：单个 default 对象 → 包装为列表
	if defaultEntry, ok := modelsSection["default"].(map[string]any); ok {
		return []map[string]any{defaultEntry}
	}

	return nil
}
