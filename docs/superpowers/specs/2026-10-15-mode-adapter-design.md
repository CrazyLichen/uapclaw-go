# 10.3.4-6 模式适配器（DeepAdapter + CodeAdapter）设计文档

> 本文档描述 AgentAdapter 接口的三种模式适配器实现设计。
> 遵循 MVP 阶段约束：agentcore 未完成，适配器做骨架实现，真实逻辑用回填标记等待。

---

## 1. 决策记录

| 决策项 | 选择 | 理由 |
|--------|------|------|
| Agent 适配器策略 | 跟随 Python：不创建独立 AgentAdapter | Python 工厂中 mode≠code 均返回 DeepAdapter，agent/plan/fast/team 全由 DeepAdapter + mode 参数区分 |
| CodeAdapter 实现方式 | 内嵌 DeepAdapter 组合委托 | Go 无继承，用组合替代 Python 继承。仅覆盖 CreateInstance，其余 7 方法委托 |
| 待定字段类型 | `interface{}` 占位 + 回填注释 | 骨架阶段 agentcore 类型不可用，回填时替换为具体类型 |
| FakeAdapter | 不实现 | 用户确认，MVP 链路测试延后 |
| 辅助模块 | 回填注释标记 | CodeAgentRail/TeamHelpers/EvolutionHelpers 等用 `⤵️ 10.3.7-11` 标记 |

---

## 2. Python 模式区分机制

### 2.1 工厂路由

```python
# agent_adapters.py create_adapter()
if mode == "code":
    return JiuwenClawCodeAdapter()    # 编码模式
return JiuWenClawDeepAdapter()         # 其余所有模式
```

### 2.2 mode 子模式在 DeepAdapter 内部区分

| mode 值 | 工厂返回 | _update_rails_for_mode 行为 |
|---------|---------|---------------------------|
| `"agent.plan"` | DeepAdapter | 注册 TaskPlanningRail/SkillEvolutionRail/SubagentRail/ContextAssembleRail，按配置启用 MemoryRail |
| `"agent.fast"` | DeepAdapter | 卸载 TaskPlanningRail/SkillEvolutionRail/SkillCreateRail/SubagentRail，注册 multi-session 工具 |
| `"team"` / `"team.plan"` | DeepAdapter | process_message_stream_impl 内部分流到 team_helpers |
| `"code"` / `"code.normal"` / `"code.plan"` | CodeAdapter(继承DeepAdapter) | 覆盖 rails 构建（加 LspRail/CodeAgentRail/CodingMemoryRail/ProjectMemoryRail），卸载 TaskPlanningRail/SkillEvolutionRail |

### 2.3 CodeAdapter 覆盖的方法

Python 中 `JiuwenClawCodeAdapter` 继承 `JiuWenClawDeepAdapter`，**仅覆盖 5 个非接口方法**：
1. `create_instance` — 不传多模态/上下文引擎参数，使用 `build_code_system_prompt()`
2. `_build_agent_rails` — 加入编码专有 rails（LspRail/ProjectMemoryRail/CodingMemoryRail 等）
3. `_get_tool_cards` / `build_code_tool_cards` — 从 config.yaml 读取编码 tools
4. `_build_configured_subagents` — 固定 explore+plan 子代理
5. `_update_rails_for_mode` — code 模式保留 SubagentRail/ProjectMemoryRail/CodingMemoryRail

**所有 AgentAdapter 接口方法（process_message_impl / process_message_stream_impl / process_interrupt / handle_user_answer / handle_heartbeat / cleanup）全部继承 DeepAdapter，不覆盖。**

---

## 3. Go 实现架构

### 3.1 文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `adapter/deep_adapter.go` | 新建 | DeepAdapter 实现 |
| `adapter/deep_adapter_test.go` | 新建 | DeepAdapter 单元测试 |
| `adapter/code_adapter.go` | 新建 | CodeAdapter 实现（组合 DeepAdapter） |
| `adapter/code_adapter_test.go` | 新建 | CodeAdapter 单元测试 |
| `adapter/factory.go` | 修改 | 更新 CreateAdapter 路由 |
| `adapter/factory_test.go` | 修改 | 更新测试用例 |
| `adapter/doc.go` | 修改 | 更新文件目录 |

### 3.2 工厂路由更新

```go
func CreateAdapter(sdk string, mode string) (AgentAdapter, error) {
    sdkName := sdk
    if sdkName == "" {
        sdkName = ResolveSDKChoice()
    }
    switch sdkName {
    case "harness":
        if mode == "code" {
            return NewCodeAdapter(), nil
        }
        return NewDeepAdapter(), nil
    case "pi":
        return nil, fmt.Errorf("SDK %q 尚未实现，当前仅支持 harness", sdkName)
    default:
        return nil, fmt.Errorf("未知 SDK %q，支持: harness, pi (预留)", sdkName)
    }
}
```

---

## 4. DeepAdapter 详细设计

### 4.1 结构体

对应 Python: `jiuwenswarm/server/runtime/agent_adapter/interface_deep.py` `JiuWenClawDeepAdapter.__init__` (line 472-551)

```go
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
    // ⤵️ 10.3.7-11: agentcore 完成后替换为具体类型
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

    // ─── ⤵️ 10.3.7-11: 模型与配置 ───

    // model Model 实例
    // ⤵️ 10.3.7-11: openjiuwen.core.foundation.llm.Model
    model interface{}
    // modelClientConfig 模型客户端配置
    // ⤵️ 10.3.7-11: ModelClientConfig
    modelClientConfig interface{}
    // modelRequestConfig 模型请求配置
    // ⤵️ 10.3.7-11: ModelRequestConfig
    modelRequestConfig interface{}
    // instanceOverrides 实例覆盖配置
    // ⤵️ 10.3.7-11: create_instance 传入的 config 字典
    instanceOverrides map[string]any
    // modelCache 模型缓存（按模型名缓存已创建的 Model 实例）
    // ⤵️ 10.3.7-11: map[string]Model
    modelCache map[string]any
    // modelNameToKeys 模型名到 API key 列表的映射
    // ⤵️ 10.3.7-11: map[string][]string
    modelNameToKeys map[string]any
    // defaultModelName 默认模型名称
    // ⤵️ 10.3.7-11: 从配置解析
    defaultModelName string

    // ─── ⤵️ 10.3.7-11: Rails ───

    // filesystemRail 文件系统护栏
    // ⤵️ 10.3.7-11: SysOperationRail
    filesystemRail interface{}
    // skillRail 技能使用护栏
    // ⤵️ 10.3.7-11: SkillUseRail
    skillRail interface{}
    // streamEventRail 流事件护栏
    // ⤵️ 10.3.7-11: JiuClawStreamEventRail
    streamEventRail interface{}
    // taskPlanningRail 任务规划护栏
    // ⤵️ 10.3.7-11: TaskPlanningRail
    taskPlanningRail interface{}
    // contextAssembleRail 上下文组装护栏
    // ⤵️ 10.3.7-11: ContextAssembleRail
    contextAssembleRail interface{}
    // contextAssembleMode 当前上下文组装模式（"agent.plan" / "agent.fast"）
    // ⤵️ 10.3.7-11: 按模式切换
    contextAssembleMode string
    // contextProcessorRail 上下文处理护栏
    // ⤵️ 10.3.7-11: ContextProcessorRail
    contextProcessorRail interface{}
    // runtimePromptRail 运行时提示词护栏
    // ⤵️ 10.3.7-11: RuntimePromptRail
    runtimePromptRail interface{}
    // responsePromptRail 响应提示词护栏
    // ⤵️ 10.3.7-11: ResponsePromptRail
    responsePromptRail interface{}
    // securityRail 安全护栏
    // ⤵️ 10.3.7-11: SecurityRail
    securityRail interface{}
    // memoryRail 记忆护栏
    // ⤵️ 10.3.7-11: MemoryRail
    memoryRail interface{}
    // externalMemoryRail 外接记忆护栏
    // ⤵️ 10.3.7-11: 外部记忆 rail
    externalMemoryRail interface{}
    // externalMemoryRailRegistered 外接记忆护栏是否已注册
    // ⤵️ 10.3.7-11: 防止重复注册
    externalMemoryRailRegistered bool
    // heartbeatRail 心跳护栏
    // ⤵️ 10.3.7-11: HeartbeatRail
    heartbeatRail interface{}
    // skillEvolutionRail 技能演进护栏
    // ⤵️ 10.3.7-11: SkillEvolutionRail
    skillEvolutionRail interface{}
    // skillCreateRail 技能创建护栏
    // ⤵️ 10.3.7-11: SkillCreateRail
    skillCreateRail interface{}
    // subagentRail 子代理护栏
    // ⤵️ 10.3.7-11: SubagentRail
    subagentRail interface{}
    // permissionRail 权限护栏
    // ⤵️ 10.3.7-11: PermissionInterruptRail
    permissionRail interface{}
    // avatarRail 头像护栏
    // ⤵️ 10.3.7-11: AvatarRail
    avatarRail interface{}

    // ─── ⤵️ 10.3.7-11: 运行时 ───

    // toolCards 工具卡片列表
    // ⤵️ 10.3.7-11: []ToolCard
    toolCards interface{}
    // sysOperation 系统操作实例
    // ⤵️ 10.3.7-11: SysOperation
    sysOperation interface{}
    // sysOperationCard 系统操作卡片
    // ⤵️ 10.3.7-11: SysOperationCard
    sysOperationCard interface{}
    // visionModelConfig 视觉模型配置
    // ⤵️ 10.3.7-11: VisionModelConfig
    visionModelConfig interface{}
    // visionToolsRegistered 视觉工具是否已注册
    // ⤵️ 10.3.7-11: 标记视觉工具注册状态
    visionToolsRegistered bool
    // audioModelConfig 音频模型配置
    // ⤵️ 10.3.7-11: AudioModelConfig
    audioModelConfig interface{}
    // audioToolsRegistered 音频工具是否已注册
    // ⤵️ 10.3.7-11: 标记音频工具注册状态
    audioToolsRegistered bool
    // videoToolRegistered 视频工具是否已注册
    // ⤵️ 10.3.7-11: 标记视频工具注册状态
    videoToolRegistered bool
    // imageGenToolRegistered 图片生成工具是否已注册
    // ⤵️ 10.3.7-11: 标记图片生成工具注册状态
    imageGenToolRegistered bool
    // skillManager 技能管理器
    // ⤵️ 10.3.7-11: SkillManager
    skillManager interface{}
    // a2xClient A2X 客户端
    // ⤵️ 10.3.7-11: A2X 客户端实例
    a2xClient interface{}
    // a2xConfig A2X 配置
    // ⤵️ 10.3.7-11: dict
    a2xConfig map[string]any
    // a2xBlankServiceID A2X blank 服务 ID
    // ⤵️ 10.3.7-11: str
    a2xBlankServiceID string
    // a2xBlankDataset A2X blank 数据集
    // ⤵️ 10.3.7-11: str
    a2xBlankDataset string
    // cronRuntime Cron 运行时桥接
    // ⤵️ 10.3.7-11: CronRuntimeBridge
    cronRuntime interface{}
    // evolutionWatchers evolution 观察任务集合
    // ⤵️ 10.3.7-11: set of goroutine handles
    evolutionWatchers interface{}
    // dreamingMode dreaming 模式
    // ⤵️ 10.3.7-11: mode 决定 dreaming 行为
    dreamingMode string
    // dreamingStarted dreaming 是否已启动
    // ⤵️ 10.3.7-11: 防止重复启动
    dreamingStarted bool
    // registeredMCPServerIDs 已注册 MCP 服务 ID 集合
    // ⤵️ 10.3.7-11: set[string]
    registeredMCPServerIDs map[string]bool
    // registeredMCPServers 已注册 MCP 服务配置
    // ⤵️ 10.3.7-11: map[string]McpServerConfig
    registeredMCPServers map[string]any
    // autoHarnessService 自动 Harness 服务
    // ⤵️ 10.3.7-11: AutoHarnessService
    autoHarnessService interface{}
    // sendFileToolkit 发送文件工具包
    // ⤵️ 10.3.7-11: SendFileToolkit
    sendFileToolkit interface{}
    // isProactiveMemory 是否主动记忆
    // ⤵️ 10.3.7-11: bool | None → *bool
    isProactiveMemory *bool
    // paidSearchRegistered 付费搜索是否已注册
    // ⤵️ 10.3.7-11: 标记付费搜索注册状态
    paidSearchRegistered bool
    // paidSearchTool 付费搜索工具实例
    // ⤵️ 10.3.7-11: WebPaidSearchTool
    paidSearchTool interface{}
}
```

### 4.2 构造函数

对应 Python: `JiuWenClawDeepAdapter.__init__` (line 472-551)

```go
// NewDeepAdapter 创建 DeepAdapter 实例。
//
// 对应 Python: JiuWenClawDeepAdapter.__init__()
func NewDeepAdapter() *DeepAdapter {
    return &DeepAdapter{
        agentName:        "main_agent",
        isCodeAgent:      false,
        activeSessionIDs: make(map[string]int),
        // ⤵️ 10.3.7-11: 初始化 cronRuntime、registeredMCPServerIDs、registeredMCPServers 等
    }
}
```

### 4.3 接口方法实现

#### 4.3.1 CreateInstance

对应 Python: `JiuWenClawDeepAdapter.create_instance` (line 2527-2621)

```go
// CreateInstance 初始化底层 SDK Agent。
//
// 对应 Python: JiuWenClawDeepAdapter.create_instance() (line 2527-2621)
//
// Python 执行步骤：
//   1. await self.set_checkpoint()
//   2. self._dreaming_mode = mode if mode.startswith("agent") else "agent"
//   3. self._instance_overrides = dict(config or {})
//   4. load_dotenv(dotenv_path=get_env_file(), override=True)
//   5. config_base = get_config()
//   6. self._refresh_multimodal_configs(config_base)
//   7. config = config_base.get("react", {}).copy()
//   8. self._config_cache = config.copy()
//   9. self._agent_name = overrides.get("agent_name", config.get("agent_name", "main_agent"))
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
func (d *DeepAdapter) CreateInstance(ctx context.Context, config map[string]any, mode string, subMode string) error {
    // 步骤 2: dreaming_mode 设置
    if mode != "" && strings.HasPrefix(mode, "agent") {
        d.dreamingMode = mode
    } else {
        d.dreamingMode = "agent"
    }

    // 步骤 3: instanceOverrides
    if config != nil {
        d.instanceOverrides = make(map[string]any, len(config))
        for k, v := range config {
            d.instanceOverrides[k] = v
        }
    } else {
        d.instanceOverrides = make(map[string]any)
    }

    // ⤵️ 10.3.7-11: 步骤 1  set_checkpoint
    // ⤵️ 10.3.7-11: 步骤 4  load_dotenv
    // ⤵️ 10.3.7-11: 步骤 5  get_config → configBase
    // ⤵️ 10.3.7-11: 步骤 6  _refresh_multimodal_configs(configBase)
    // ⤵️ 10.3.7-11: 步骤 7-8 读取 react 配置段，缓存到 configCache

    // 步骤 9: agentName
    if v, ok := d.instanceOverrides["agent_name"]; ok {
        if s, ok := v.(string); ok {
            d.agentName = s
        }
    }
    // ⤵️ 10.3.7-11: 步骤 9 完整版需从 configCache 取默认值

    // 步骤 10: projectDir
    if v, ok := d.instanceOverrides["project_dir"]; ok {
        if s, ok := v.(string); ok {
            d.projectDir = s
        }
    }
    // ⤵️ 10.3.7-11: 步骤 10 完整版需从 configCache 取默认值

    // 步骤 11: workspaceDir
    // ⤵️ 10.3.7-11: 从 configCache 或 get_agent_workspace_dir() 获取

    // 存储 mode/subMode
    d.mode = mode
    d.subMode = subMode

    // ⤵️ 10.3.7-11: 步骤 12-25
    // 步骤 12: model = self._create_model(configBase)
    // 步骤 13: await self._try_init_a2x_client(configBase)
    // 步骤 14: agentCard = AgentCard{name: d.agentName, id: "jiuwenswarm"}
    // 步骤 15: toolCards = await self._get_tool_cards(agentCard.id)
    // 步骤 16: railsList = self._build_agent_rails(config, configBase, mode=mode)
    // 步骤 17: sysOperation = self._create_sys_operation()
    // 步骤 18: subagents = self._build_configured_subagents(model, config, configBase)
    // 步骤 19: d.instance = create_deep_agent(...)
    // 步骤 20: await d.instance.ensure_initialized()
    // 步骤 21: d._seed_runtime_cwd(d.projectDir or d.workspaceDir)
    // 步骤 22: d._sync_a2x_runtime_state()
    // 步骤 23: d.registeredMCPServerIDs = make(map[string]bool)
    // 步骤 24: await d._register_mcp_servers_from_config(configBase, tag)
    // 步骤 25: await d.load_user_rails()

    logger.Info(logComponent).
        Str("agent_name", d.agentName).
        Str("mode", mode).
        Str("sub_mode", subMode).
        Msg("DeepAdapter 初始化骨架完成，等待回填")
    return nil
}
```

#### 4.3.2 ReloadAgentConfig

对应 Python: `JiuWenClawDeepAdapter.reload_agent_config` (line 2646-2752)

```go
// ReloadAgentConfig 热重载配置，不重启进程。
//
// 对应 Python: JiuWenClawDeepAdapter.reload_agent_config() (line 2646-2752)
//
// Python 执行步骤：
//   1. config_base = configBase or get_config()
//   2. if envOverrides: apply env overrides
//   3. config = config_base.get("react", {}).copy()
//   4. self._config_cache = config.copy()
//   5. self._refresh_multimodal_configs(config_base)
//   6. model = self._create_model(config_base)
//   7. self._model = model
//   8. rails_list = self._get_current_agent_rails(config, config_base)
//   9. new_tool_cards = await self._get_tool_cards("jiuwenswarm")
//  10. self._update_permission_rail(config_base)
//  11. await self._instance.configure(
//        model=model, tools=new_tool_cards, rails=rails_list,
//        subagents=subagents, enable_task_loop=..., max_iterations=...)
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
```

#### 4.3.3 ProcessMessageImpl

对应 Python: `JiuWenClawDeepAdapter.process_message_impl` (line 4409-4512)

```go
// ProcessMessageImpl 执行非流式请求，返回完整响应。
//
// 对应 Python: JiuWenClawDeepAdapter.process_message_impl() (line 4409-4512)
//
// Python 执行步骤：
//   1. if self._instance is None: raise RuntimeError("未初始化")
//   2. _req_model = request.params.get("model_name", "")
//   3. if not self._has_valid_model_config(_req_model): return error response
//   4. session_id = request.session_id or "default"
//   5. query = request.params.get("query", "")
//   6. mode = request.params.get("mode", "agent.plan")
//   7. slash_result = await self._handle_slash_command(query, session_id, mode)
//   8. if slash_result: handle approval_chunks or content
//   9. cron_context_tokens = self._bind_runtime_cron_context(...)
//  10. token_cid = TOOL_PERMISSION_CHANNEL_ID.set(...)
//  11. token_perm = setup_permission_context(request)
//  12. resolved_model = self._resolve_model_for_request(request)
//  13. self._apply_model_to_react_agent(resolved_model)
//  14. self._mark_session_active(session_id)
//  15. if self._stream_event_rail: self._stream_event_rail.reset_abort(session_id)
//  16. try:
//  17.   await self._update_runtime_config(runtimeConfig)
//  18.   result = await Runner.run_agent(agent=self._instance, inputs=inputs)
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

    // ⤵️ 10.3.7-11: 步骤 2-3  模型配置校验
    // ⤵️ 10.3.7-11: 步骤 7-8  slash 命令处理
    // ⤵️ 10.3.7-11: 步骤 9    cron 上下文绑定
    // ⤵️ 10.3.7-11: 步骤 10-11 权限上下文设置
    // ⤵️ 10.3.7-11: 步骤 12-13 模型选择与应用
    // ⤵️ 10.3.7-11: 步骤 14    mark_session_active(sessionID)
    // ⤵️ 10.3.7-11: 步骤 15    streamEventRail.reset_abort(sessionID)
    // ⤵️ 10.3.7-11: 步骤 16-18 update_runtime_config + Runner.run_agent
    // ⤵️ 10.3.7-11: 步骤 19-20 异常处理 + 清理（unmark_session_active）
    // ⤵️ 10.3.7-11: 步骤 21    构造 AgentResponse

    logger.Info(logComponent).
        Str("session_id", sessionID).
        Str("mode", mode).
        Str("query", query).
        Msg("DeepAdapter ProcessMessageImpl 骨架，等待回填")

    return nil, fmt.Errorf("ProcessMessageImpl 骨架，等待 10.3.7-11 回填")
}
```

#### 4.3.4 ProcessMessageStreamImpl

对应 Python: `JiuWenClawDeepAdapter.process_message_stream_impl` (line 4514-4750)

```go
// ProcessMessageStreamImpl 执行流式请求，通过 channel 返回响应块。
//
// 对应 Python: JiuWenClawDeepAdapter.process_message_stream_impl() (line 4514-4750)
//
// Python 执行步骤：
//   1. if self._instance is None: raise RuntimeError("未初始化")
//   2. _req_model = request.params.get("model_name", "")
//   3. if not self._has_valid_model_config(_req_model): yield error chunk; return
//   4. session_id = request.session_id or "default"
//   5. query = request.params.get("query", "")
//   6. mode = request.params.get("mode", "agent.plan")
//   7. if mode in ("team", "team.plan", "code.team"): → team_helpers.process_team_message_stream
//   8. if mode == "auto_harness": → auto_harness 分流
//   9. slash_result = await self._handle_slash_command(query, session_id, mode)
//  10. cron_context_tokens = self._bind_runtime_cron_context(...)
//  11. token_cid = TOOL_PERMISSION_CHANNEL_ID.set(...)
//  12. token_perm = setup_permission_context(request)
//  13. resolved_model = self._resolve_model_for_request(request)
//  14. self._apply_model_to_react_agent(resolved_model)
//  15. self._mark_session_active(session_id)
//  16. try:
//  17.   await self._update_runtime_config(runtimeConfig)
//  18.   async for chunk in Runner.run_agent_streaming(agent=self._instance, inputs=inputs):
//  19.     yield chunk
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

    // ⤵️ 10.3.7-11: 步骤 2-3  模型配置校验
    // ⤵️ 10.3.7-11: 步骤 7    team 模式分流（"team"/"team.plan"/"code.team"）
    // ⤵️ 10.3.7-11: 步骤 8    auto_harness 分流
    // ⤵️ 10.3.7-11: 步骤 9    slash 命令处理
    // ⤵️ 10.3.7-11: 步骤 10-12 cron/权限上下文
    // ⤵️ 10.3.7-11: 步骤 13-14 模型选择与应用
    // ⤵️ 10.3.7-11: 步骤 15    mark_session_active(sessionID)
    // ⤵️ 10.3.7-11: 步骤 16-19 update_runtime_config + Runner.run_agent_streaming
    // ⤵️ 10.3.7-11: 步骤 20-21 异常处理 + 清理

    logger.Info(logComponent).
        Str("session_id", sessionID).
        Str("mode", mode).
        Msg("DeepAdapter ProcessMessageStreamImpl 骨架，等待回填")

    ch := make(chan *schema.AgentResponseChunk)
    close(ch)
    return ch, fmt.Errorf("ProcessMessageStreamImpl 骨架，等待 10.3.7-11 回填")
}
```

#### 4.3.5 ProcessInterrupt

对应 Python: `JiuWenClawDeepAdapter.process_interrupt` (line 3268-3578)

```go
// ProcessInterrupt 处理中断请求（pause/resume/cancel/supplement）。
//
// 对应 Python: JiuWenClawDeepAdapter.process_interrupt() (line 3268-3578)
//
// Python 执行步骤：
//   1. intent = request.params.get("intent", "cancel")
//   2. new_input = request.params.get("new_input")
//   3. _normalized_sid = request.session_id or "default"
//   4. _session_is_active = self._is_session_active(_normalized_sid)
//   5. if not _session_is_active: log & skip abort operations
//   6. if intent == "pause":
//        if session_active and streamEventRail: streamEventRail.pause(session_id)
//   7. elif intent == "resume":
//        if session_active and streamEventRail: streamEventRail.resume(session_id)
//   8. elif intent == "supplement":
//        if session_active:
//          - streamEventRail.abort(session_id)
//          - collect cancelled tool results
//          - if no other active sessions: instance.abort()
//        mark_session_active(new session) if new_input
//   9. elif intent == "cancel":
//        if session_active:
//          - streamEventRail.abort(session_id)
//          - collect cancelled tool results
//          - if no other active sessions: instance.abort()
//        unmark_session_active(session_id)
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
        // ⤵️ 10.3.7-11: streamEventRail.pause(sessionID)
        logger.Info(logComponent).Str("intent", "pause").Msg("中断: 已暂停执行")
    case "resume":
        // ⤵️ 10.3.7-11: streamEventRail.resume(sessionID)
        logger.Info(logComponent).Str("intent", "resume").Msg("中断: 已恢复执行")
    case "supplement":
        if sessionActive {
            // ⤵️ 10.3.7-11: streamEventRail.abort(sessionID)
            // ⤵️ 10.3.7-11: collect cancelled tool results
            // ⤵️ 10.3.7-11: if no other active sessions → instance.abort()
        }
        if newInput != nil {
            // ⤵️ 10.3.7-11: mark_session_active(new session)
        }
        logger.Info(logComponent).Str("intent", "supplement").Msg("中断: supplement 处理")
    case "cancel":
        if sessionActive {
            // ⤵️ 10.3.7-11: streamEventRail.abort(sessionID)
            // ⤵️ 10.3.7-11: collect cancelled tool results
            // ⤵️ 10.3.7-11: if no other active sessions → instance.abort()
        }
        d.unmarkSessionActive(normalizedSID)
        logger.Info(logComponent).Str("intent", "cancel").Msg("中断: cancel 处理")
    }

    // 步骤 10: 清理 evolution watchers
    // ⤵️ 10.3.7-11: cancel evolution watcher tasks

    // 步骤 11: 构造响应
    return schema.NewAgentResponse(req.RequestID, req.ChannelID), nil
}
```

#### 4.3.6 HandleUserAnswer

对应 Python: `JiuWenClawDeepAdapter.handle_user_answer` (line 3579-3605)

```go
// HandleUserAnswer 处理用户回答（evolution 审批或权限审批）。
//
// 对应 Python: JiuWenClawDeepAdapter.handle_user_answer() (line 3579-3605)
//
// Python 执行步骤：
//   1. request_id = request.params.get("request_id", "")
//   2. answers = request.params.get("answers", [])
//   3. session_id = request.session_id
//   4. resolved = False
//   5. if request_id.startswith("team_skill_evolve_"):
//        resolved = await self.handle_team_skill_evolve_approval(...)
//   6. elif request_id.startswith("evolve_simplify_"):
//        resolved = await self._handle_governance_approval(request_id, answers, "simplify")
//   7. elif request_id.startswith("skill_evolve_"):
//        resolved = await self._handle_evolution_approval(request_id, answers)
//   8. return AgentResponse(ok=True, payload={"accepted": True, "resolved": resolved})
func (d *DeepAdapter) HandleUserAnswer(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
    // 步骤 1-2: 解析 request_id 和 answers
    params := parseParams(req.Params)
    requestID := paramsString(params, "request_id", "")

    // 步骤 4: resolved 默认 false
    resolved := false

    // 步骤 5-7: 按 request_id 前缀分发
    if strings.HasPrefix(requestID, "team_skill_evolve_") {
        // ⤵️ 10.3.7-11: handle_team_skill_evolve_approval(requestID, answers, sessionID, channelID)
    } else if strings.HasPrefix(requestID, "evolve_simplify_") {
        // ⤵️ 10.3.7-11: _handle_governance_approval(requestID, answers, "simplify")
    } else if strings.HasPrefix(requestID, "skill_evolve_") {
        // ⤵️ 10.3.7-11: _handle_evolution_approval(requestID, answers)
    }

    // 步骤 8: 构造响应
    return schema.NewAgentResponse(req.RequestID, req.ChannelID,
        schema.WithPayload(map[string]any{"accepted": true, "resolved": resolved}),
    ), nil
}
```

#### 4.3.7 HandleHeartbeat

对应 Python: `JiuWenClawDeepAdapter.handle_heartbeat` (line 3607-3624)

```go
// HandleHeartbeat 处理心跳请求。
//
// 对应 Python: JiuWenClawDeepAdapter.handle_heartbeat() (line 3607-3624)
//
// Python 执行步骤：
//   1. sid = str(request.session_id or "")
//   2. if not sid.startswith("heartbeat"): return None
//   3. request.params["query"] = "这是一次心跳请求任务..."
//   4. log heartbeat query injected
//   5. return None（继续正常流程，query 已注入）
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
    // ⤵️ 10.3.7-11: 将 request.params["query"] 设为 "这是一次心跳请求任务，请根据</heartbeat_user_task>标签中的内容进行回复"
    // ⤵️ 10.3.7-11: 需要修改 req.Params（json.RawMessage → 重新编码）

    // 步骤 4: 日志
    logger.Info(logComponent).
        Str("request_id", req.RequestID).
        Str("session_id", sid).
        Msg("heartbeat query 已注入")

    // 步骤 5: 返回 nil，继续正常流程
    return nil, nil
}
```

#### 4.3.8 Cleanup

对应 Python: `JiuWenClawDeepAdapter.cleanup` (line 3245-3248)

```go
// Cleanup 清理适配器资源。
//
// 对应 Python: JiuWenClawDeepAdapter.cleanup() (line 3245-3248)
//
// Python 执行步骤：
//   1. await self._close_a2x_client()
func (d *DeepAdapter) Cleanup() error {
    // 步骤 1: 关闭 a2x 客户端
    // ⤵️ 10.3.7-11: _close_a2x_client()

    logger.Info(logComponent).Msg("DeepAdapter Cleanup 骨架，等待回填")
    return nil
}
```

### 4.4 非导出方法

#### 4.4.1 Session 活跃计数

对应 Python: `JiuWenClawDeepAdapter._mark_session_active / _unmark_session_active / _is_session_active / _other_active_sessions` (line 576-610)

```go
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
        // ⤵️ 10.3.7-11: if d.streamEventRail != nil { d.streamEventRail.cleanup_session(sessionID) }
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
```

### 4.5 Params 解析辅助

```go
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
```

---

## 5. CodeAdapter 详细设计

### 5.1 结构体

对应 Python: `jiuwenswarm/server/runtime/agent_adapter/interface_code.py` `JiuwenClawCodeAdapter` (line 154-191)

```go
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
```

### 5.2 构造函数

对应 Python: `JiuwenClawCodeAdapter.__init__` (line 177-192)

```go
// NewCodeAdapter 创建 CodeAdapter 实例。
//
// 对应 Python: JiuwenClawCodeAdapter.__init__() (line 177-192)
func NewCodeAdapter() *CodeAdapter {
    deep := NewDeepAdapter()
    deep.isCodeAgent = true // 单点 source-of-truth：code-agent → project_dir
    return &CodeAdapter{
        deep:                     deep,
        forceEnglishRuntimePrompt: true,
    }
}
```

### 5.3 接口方法实现

#### 5.3.1 CreateInstance（覆盖）

对应 Python: `JiuwenClawCodeAdapter.create_instance` (line 221-342)

```go
// CreateInstance 初始化底层 SDK Agent（code 模式）。
//
// 对应 Python: JiuwenClawCodeAdapter.create_instance() (line 221-342)
//
// 与 DeepAdapter.CreateInstance 的差异（对齐 Python）：
//   1. dreaming_mode = "code"（而非基于 mode 前缀判断）
//   2. _workspace_dir 优先使用 project_dir（LspTool sandbox 校验需要）
//   3. _agent_workspace_dir 始终指向系统 workspace（编码记忆、todo 等不应写入用户项目目录）
//   4. 不传 vision_model_config / audio_model_config / context_engine_config / completion_timeout
//   5. system_prompt 使用 build_code_system_prompt()（而非 build_agent_identity_prompt()）
//   6. rails 包含编码专有 rails（LspRail/ProjectMemoryRail/CodingMemoryRail/CodeAgentRail 等）
//   7. subagents 固定 explore_agent + plan_agent + 按配置启用 code_agent/browser_agent
func (c *CodeAdapter) CreateInstance(ctx context.Context, config map[string]any, mode string, subMode string) error {
    // 步骤 1: 设置 dreaming_mode = "code"
    c.deep.dreamingMode = "code"

    // 步骤 2: workspace 语义覆写
    // ⤵️ 10.3.7-11: workspaceDir 优先使用 projectDir
    // ⤵️ 10.3.7-11: agentWorkspaceDir 始终指向系统 workspace

    // 步骤 3: 调用 DeepAdapter 基础初始化（步骤 1-11 中通用的部分）
    // ⤵️ 10.3.7-11: 调用 deep 基础初始化逻辑
    // ⤵️ 10.3.7-11: 步骤 12  model = _create_model(configBase)  — 不传多模态配置
    // ⤵️ 10.3.7-11: 步骤 14  agentCard = AgentCard{name, id}
    // ⤵️ 10.3.7-11: 步骤 15  toolCards = _get_tool_cards("jiuwenswarm") — 编码 tools
    // ⤵️ 10.3.7-11: 步骤 16  railsList = _build_agent_rails(config, configBase, mode="code")
    //              编码专有 rails：LspRail, ProjectMemoryRail, CodingMemoryRail,
    //              CodeAgentRail, WorktreeRail, AgentModeRail, StructuredAskUserRail,
    //              ConfirmInterruptRail, FileSystemRail
    // ⤵️ 10.3.7-11: 步骤 17  sysOperation = _create_sys_operation()
    // ⤵️ 10.3.7-11: 步骤 18  subagents = _build_configured_subagents(model, config, configBase)
    //              固定: explore_agent + plan_agent
    //              按配置: code_agent + browser_agent
    // ⤵️ 10.3.7-11: 步骤 19  d.instance = create_deep_agent(
    //              model, card, system_prompt=build_code_system_prompt(),
    //              tools, subagents, rails,
    //              enable_task_loop, max_iterations, workspace, sys_operation, language,
    //              // 不传: vision_model_config, audio_model_config,
    //              //       context_engine_config, completion_timeout
    //            )
    // ⤵️ 10.3.7-11: 步骤 20  instance.ensure_initialized()
    // ⤵️ 10.3.7-11: 步骤 21-25 同 DeepAdapter

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
```

#### 5.3.2 委托方法

```go
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
```

---

## 6. 测试策略

### 6.1 DeepAdapter 测试

| 测试函数 | 覆盖内容 |
|---------|---------|
| `TestNewDeepAdapter` | 构造函数默认值：agentName="main_agent", isCodeAgent=false |
| `TestDeepAdapter_接口满足性` | 编译期检查 DeepAdapter 实现 AgentAdapter 接口 |
| `TestDeepAdapter_SessionActive` | mark/unmark/is/other 四个方法的完整语义 |
| `TestDeepAdapter_ProcessInterrupt_Intent分支` | pause/resume/cancel/supplement 四个 intent 分支 |
| `TestDeepAdapter_ProcessInterrupt_未初始化` | instance=nil 时的错误处理 |
| `TestDeepAdapter_HandleUserAnswer_前缀分发` | team_skill_evolve_ / evolve_simplify_ / skill_evolve_ / 未知前缀 |
| `TestDeepAdapter_HandleHeartbeat_前缀检查` | heartbeat 前缀 vs 非 heartbeat 前缀 |
| `TestDeepAdapter_ProcessMessageImpl_未初始化` | instance=nil 时返回错误 |
| `TestDeepAdapter_ProcessMessageStreamImpl_未初始化` | instance=nil 时返回错误 |
| `TestParseParams` | json.RawMessage → map[string]any，空/无效输入 |
| `TestParamsString` | 取值/默认值/类型不匹配 |

### 6.2 CodeAdapter 测试

| 测试函数 | 覆盖内容 |
|---------|---------|
| `TestNewCodeAdapter` | 构造函数：deep 非空、isCodeAgent=true、forceEnglishRuntimePrompt=true |
| `TestCodeAdapter_接口满足性` | 编译期检查 CodeAdapter 实现 AgentAdapter 接口 |
| `TestCodeAdapter_委托方法` | ReloadAgentConfig/ProcessMessageImpl 等委托调用验证 |

---

## 7. IMPLEMENTATION_PLAN 状态更新

| 步骤 | 当前状态 | 目标状态 |
|------|---------|---------|
| 10.3.4-6 | ☐ | ✅（骨架完成） |
| 10.3.3 | ✅ | ✅（不变，工厂路由更新属于 10.3.4-6 范围） |

---

## 8. 回填依赖图

```
10.3.4-6 (本步骤)
  ├── DeepAdapter 骨架 ✅
  ├── CodeAdapter 骨架 ✅
  └── 工厂路由更新 ✅

10.3.7-11 (适配器辅助，回填目标)
  ├── CodeAgentRail        → CodeAdapter.codeAgentRail
  ├── TeamHelpers          → DeepAdapter.ProcessMessageStreamImpl team 分流
  ├── EvolutionHelpers     → DeepAdapter.HandleUserAnswer evolution 审批
  ├── RecapPrompts         → DeepAdapter context 压缩
  └── SysOpBuilder         → DeepAdapter._create_sys_operation()

agentcore (后续回填目标)
  ├── DeepAgent            → DeepAdapter.instance
  ├── Runner               → ProcessMessageImpl/StreamImpl 中的 Runner.run_agent
  ├── Model                → DeepAdapter.model
  ├── Rails                → DeepAdapter 各种 rail 字段
  └── ToolCard             → DeepAdapter.toolCards
```
