# 48小时代码逻辑审查报告

> 审查时间：2026-08-05
> 审查范围：48小时内提交（7ef6593 ~ e118f39）
> 涉及章节：7.2 通用记忆工具/编程记忆工具、7.5 frontmatter、10.3.2 UapClaw 门面、10.3.4-6 CodeAdapter 回填、9.65-1 循环依赖重构

---

## 一、严重问题

### S01：coding_memory_write_with_context 缺少 REDUNDANT (SKIP) 返回路径

**章节**：7.2 coding_memory_tool_ops

**问题描述**：Python 中 `_prepare_append_mode` 和创建模式下都通过 `_run_checker`（MemUpdateChecker）执行 LLM 冗余判断。当新记忆被判定为 REDUNDANT 时，直接返回 `WriteResult(mode=WriteMode.SKIP)`，**不执行写入**。Go 中 `runChecker` 始终返回 nil，导致**冗余记忆永远不会被跳过**，所有写入都会执行。

**Python 样例**（interface_code.py L396-408）：
```python
# 创建模式
if actions and not any(a.id == basename for a in actions):
    return WriteResult(
        success=True, path=resolved, mode=WriteMode.SKIP,
        note="Content is redundant with existing memories",
        type=fm.get("type")
    ).to_dict()
```

**Python 样例**（coding_memory_tool_ops.py L256-265）：
```python
# 追加模式 _prepare_append_mode
if actions and not any(a.id == basename for a in actions):
    return WriteResult(
        success=True, path=resolved, mode=WriteMode.SKIP,
        note="Content is redundant with existing memories",
        type=fm.get("type")
    ).to_dict()
```

**Go 问题**（coding_memory_tool_ops.go L197-199, L454-455）：
```go
// ⤵️ 回填: 7.8 MemUpdateChecker — LLM 冗余判断，当前跳过 SKIP 逻辑
// Python 中 runChecker 返回 actions 后，如果新记忆不在 actions 中（即 REDUNDANT），
// 直接返回 WriteResult(mode=SKIP)。当前缺少 LLM 判断，无法判断冗余，故不做 SKIP。
```

**修复方案**：
1. 7.8 实现 MemUpdateChecker 后，在 `CodingMemoryWriteWithContext` 创建模式中添加 REDUNDANT 判断：
```go
if actions := runChecker(...); len(actions) > 0 {
    found := false
    for _, a := range actions {
        if a.ID == basename { found = true; break }
    }
    if !found {
        return (&WriteResult{Success: true, Path: resolved, Mode: WriteModeSkip,
            Note: "Content is redundant with existing memories", Type: fm["type"]}).ToDict()
    }
}
```
2. 在 `prepareAppendMode` 中同样添加 REDUNDANT 判断，对齐 Python `_prepare_append_mode` L256-265

---

### S02：CodeAdapter CreateInstance 缺少 _get_current_agent_rails 热重载方法

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `JiuwenClawCodeAdapter` 覆盖了父类 `_get_current_agent_rails` 方法，将 `CodeAgentRail` 纳入热重载范围。Go 中 CodeAdapter 没有实现此方法，导致 config reload 时 `CodeAgentRail` 不会被正确重新初始化。

**Python 样例**（interface_code.py L836-848）：
```python
def _get_current_agent_rails(
    self, config: dict[str, Any], config_base: dict[str, Any] | None = None
) -> list[Any]:
    """扩展父类方法，将 CodeAgentRail 纳入热重载范围。"""
    rails_list = super()._get_current_agent_rails(config, config_base)
    if self._code_agent_rail is not None:
        rails_list.append(self._code_agent_rail)
    return rails_list
```

**Go 问题**：CodeAdapter 完全没有 `GetCurrentAgentRails` 方法，DeepAdapter 的 `ReloadAgentConfig` 调用时无法获取 CodeAgentRail。

**修复方案**：在 CodeAdapter 中添加 `GetCurrentAgentRails` 方法，委托 DeepAdapter 后追加 CodeAgentRail：
```go
func (c *CodeAdapter) GetCurrentAgentRails(config, configBase map[string]any) []sainterfaces.AgentRail {
    rails := c.deep.GetCurrentAgentRails(config, configBase)
    if c.codeAgentRail != nil {
        rails = append(rails, c.codeAgentRail)
    }
    return rails
}
```

---

### S03：CodeAdapter CreateInstance 缺少 coding_memory workspace set_directory

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `create_instance` 步骤 21.2 会调用 `self._instance.deep_config.workspace.set_directory(...)` 设置 coding_memory 目录结构（含 MEMORY.md 子节点）。Go 中标记为 ⤵️ 但这个步骤影响 coding_memory 工具能否正确找到文件路径。

**Python 样例**（interface_code.py L314-330）：
```python
_project_name = os.path.basename(self._project_dir) if self._project_dir else "default"
coding_memory_workspace_path = os.path.join("coding_memory", _project_name)
self._instance.deep_config.workspace.set_directory({
    "name": "coding_memory",
    "description": "Coding Agent 记忆模块",
    "path": coding_memory_workspace_path,
    "children": [
        {
            "name": "MEMORY.md",
            "description": "Coding 记忆索引",
            "path": "MEMORY.md",
            "children": [],
            "is_file": True,
            "default_content": "",
        },
    ],
})
```

**Go 问题**（code_adapter.go L313-315）：
```go
// 步骤 21.2: coding_memory workspace set_directory
// ⤵️ 10.6.3-10: 待 Workspace.set_directory 方法实现后回填
```

**修复方案**：在 Workspace 实现中添加 `SetDirectory` 方法，然后在 CodeAdapter.CreateInstance 步骤 21.2 调用：
```go
if c.deep.instance != nil {
    projectName := filepath.Base(c.deep.projectDir)
    if projectName == "." || projectName == "" {
        projectName = "default"
    }
    codingMemoryPath := filepath.Join("coding_memory", projectName)
    ws := c.deep.instance.GetWorkspace()
    if ws != nil {
        ws.SetDirectory(workspace.DirectoryConfig{
            Name:        "coding_memory",
            Description: "Coding Agent 记忆模块",
            Path:        codingMemoryPath,
            Children: []workspace.DirectoryConfig{{
                Name:           "MEMORY.md",
                Description:    "Coding 记忆索引",
                Path:           "MEMORY.md",
                IsFile:         true,
                DefaultContent: "",
            }},
        })
    }
}
```

---

### S04：CodeAdapter 缺少 configure_team_member_agent 方法

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `JiuwenClawCodeAdapter` 有完整的 `configure_team_member_agent` 方法，用于将 code 运行时配置应用到团队子代理。该方法设置 project_dir、coding_memory、rails、tools、subagents 等。Go 中完全没有此方法，Team 模式下子代理无法获得 code 配置。

**Python 样例**（interface_code.py L1074-1171）：
```python
def configure_team_member_agent(
    self,
    agent: Any,
    *,
    parent_agent: Any | None = None,
    skill_manager: Any | None = None,
    member_name: str | None = None,
    role: str | None = None,
    session_id: str | None = None,
    channel_id: str | None = None,
    project_dir: str | None = None,
    runtime_language: str | None = None,
    force_english_runtime_prompt: bool = True,
) -> None:
    """Apply the code runtime profile to a team member DeepAgent."""
    # ... 设置 project_dir, workspace_dir, model, tool_cards, rails, subagents, mcps
    _set_coding_memory_directory(agent, self._project_dir)
    setattr(agent, "_jiuwenswarm_adapter_mode", "code")
    setattr(agent, "_jiuwenswarm_code_project_dir", self._project_dir or self._workspace_dir)
```

**Go 问题**：CodeAdapter 无 `ConfigureTeamMemberAgent` 方法。

**修复方案**：在 CodeAdapter 中实现 `ConfigureTeamMemberAgent`，对齐 Python 的完整逻辑。此方法依赖 10.6.19-23 TeamManager 实现，但框架应先建立。

---

### S05：CodeAdapter 缺少 _update_rails_for_mode 方法

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `JiuwenClawCodeAdapter` 实现了 `_update_rails_for_mode`，在 code 模式下卸载 TaskPlanningRail/SkillEvolutionRail，保留 SubagentRail/ProjectMemoryRail/CodingMemoryRail。Go 中没有此方法，模式切换时 Rails 生命周期管理缺失。

**Python 样例**（interface_code.py L770-824）：
```python
async def _update_rails_for_mode(self, mode: str) -> None:
    """Code 模式下的 rail 生命周期管理."""
    # 卸载非 code 专属 rails
    rail_specs = (
        ("_task_planning_rail", "TaskPlanningRail"),
        ("_skill_evolution_rail", "SkillEvolutionRail"),
    )
    for attr, label in rail_specs:
        rail = getattr(self, attr, None)
        if rail is not None:
            await self._instance.unregister_rail(rail)
            setattr(self, attr, None)
    # code 模式保留 SubagentRail/ProjectMemoryRail/CodingMemoryRail
```

**Go 问题**：CodeAdapter 无 `UpdateRailsForMode` 方法。

**修复方案**：在 CodeAdapter 中实现 `UpdateRailsForMode`，对齐 Python 的 Rail 生命周期管理。

---

### S06：CodeAdapter buildCodeAgentRails 中 PermissionInterruptRail 缺少参数传递

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `PermissionInterruptRail` 通过 `build_permission_rail` 函数构建，传入 `config`, `llm`, `model_name` 参数。Go 中 `buildPermissionRail` 不接收任何参数，返回 nil，权限护栏完全缺失。

**Python 样例**（interface_code.py L363-373）：
```python
_RailBuildInfo(
    "_permission_rail",
    build_permission_rail,
    {
        "config": config_base,
        "llm": self._model,
        "model_name": config_base.get("models", {}).get(
            "default", {}
        ).get("model_client_config", {}).get("model_name", "gpt-4"),
    },
),
```

**Go 问题**（code_adapter.go L647-653）：
```go
func (c *CodeAdapter) buildPermissionRail(configBase map[string]any) sainterfaces.AgentRail {
    // ⤵️ 10.6.3-10: 实现 PermissionInterruptRail
    return nil
}
```

**修复方案**：实现 `buildPermissionRail`，传入 config、model、model_name 参数，对齐 Python 的 `build_permission_rail` 函数。

---

### S07：CodeAdapter 未重写 _build_configured_subagents — 缺少 explore_agent/plan_agent/code_agent 固定挂载

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `JiuwenClawCodeAdapter._build_configured_subagents()` 完全重写了父类方法，**固定挂载** explore_agent 和 plan_agent（始终启用），并按配置启用 code_agent 和 browser_agent。Go 中 CodeAdapter 直接调用 `c.deep.buildConfiguredSubagents()`，走的是 DeepAdapter 的通用实现，**完全没有 explore_agent/plan_agent/code_agent**。这是 Code 模式与 Deep 模式的核心差异点。

**Python 样例**（interface_code.py L677-729）：
```python
# ── 固定挂载：explore_agent（Code 模式核心子代理，始终启用）──
if not self._subagent_list_has_name(subagents, "explore_agent"):
    explore_spec = build_explore_agent_config(
        model=model, workspace=workspace, language=resolved_language, ...
    )
    explore_spec.factory_kwargs = {"auto_create_workspace": False}
    subagents.append(explore_spec)

# ── 固定挂载：plan_agent（Code 模式核心子代理，始终启用）──
if not self._subagent_list_has_name(subagents, "plan_agent"):
    plan_spec = build_plan_agent_config(
        model=model, workspace=workspace, language=resolved_language, ...
    )
    plan_spec.factory_kwargs = {"auto_create_workspace": False}
    subagents.append(plan_spec)

# code_agent subagent — 按配置启用
code_agent_cfg = subagents_cfg.get("code_agent")
if self._is_subagent_enabled(code_agent_cfg):
    code_agent_rails = None
    coding_memory_rail = self._coding_memory_rail
    if coding_memory_rail is not None:
        code_agent_rails = [SysOperationRail(), coding_memory_rail]
    code_spec = build_code_agent_config(model, workspace=workspace, ...)
    subagents.append(code_spec)
```

**Go 问题**（code_adapter.go L264）：
```go
subagentSpecs, _ := c.deep.buildConfiguredSubagents(c.deep.configCache, configBase)
```

**修复方案**：CodeAdapter 应实现自己的 `buildCodeConfiguredSubagents()` 方法：
```go
func (c *CodeAdapter) buildCodeConfiguredSubagents(config, configBase map[string]any) []harness.SubAgentConfig {
    var subagents []harness.SubAgentConfig
    // 1. 固定挂载 explore_agent
    exploreSpec := buildExploreAgentConfig(c.deep.model, c.deep.workspaceDir, resolvedLanguage, maxIter)
    subagents = append(subagents, exploreSpec)
    // 2. 固定挂载 plan_agent
    planSpec := buildPlanAgentConfig(c.deep.model, c.deep.workspaceDir, resolvedLanguage, maxIter)
    subagents = append(subagents, planSpec)
    // 3. 按配置启用 code_agent（复用 CodingMemoryRail）
    // 4. 按配置启用 browser_agent
    return subagents
}
```

---

### S08：CodeAdapter 未重写 _get_tool_cards — 不走 code 专有工具配置路径

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `JiuwenClawCodeAdapter._get_tool_cards()` 完全重写为 `self.build_code_tool_cards(agent_id)`，从 `config.yaml::modes.code.tools` 读取工具列表，支持 web_free_search/web_fetch_webpage/web_paid_search/user_todos/skill_toolkit/acp_chat 六种工具。Go 中 CodeAdapter 直接调用 `c.deep.getToolCards()`，走的是 DeepAdapter 的通用实现，**不走 code 专有的 `modes.code.tools` 配置路径**。

**Python 样例**（interface_code.py L931-965）：
```python
async def _get_tool_cards(self, agent_id: str) -> list[Any]:
    return self.build_code_tool_cards(agent_id)

def build_code_tool_cards(self, agent_id: str) -> list[Any]:
    config_base = get_config()
    mode_config = config_base.get("modes", {}).get("code", {})
    configured_tools = mode_config.get("tools") or []
    for tool_name in configured_tools:
        result = self._get_tool_build_func(tool_name, agent_id)
        ...
```

**Go 问题**（code_adapter.go L247）：
```go
toolCards := c.deep.getToolCards(agentCard.ID)
```

**修复方案**：CodeAdapter 应实现 `getCodeToolCards()` 方法，从 `configBase["modes"]["code"]["tools"]` 读取配置驱动的工具列表，对齐 Python 的 `_TOOL_BUILD_NAMES` 映射和 `build_code_tool_cards` 逻辑。

---

### S09：InitCodingMemoryManagerAsync 缺少 LLM 参数传递

**章节**：7.2 tools

**问题描述**：Python 的 `init_memory_manager_async`（coding_memory 版本）接受 `llm` 参数，并在创建 manager 后设置 `manager.llm = llm`。Go 版本没有 `llm` 参数，也没有设置 `llm` 字段。`memoryIndexManager` 结构体中有 `llm any` 字段（manager_impl.go:95），但 `InitCodingMemoryManagerAsync` 没有传入它。**即使 7.8 实现了 MemUpdateChecker，runChecker 也无法工作，因为 llm 永远为 nil**。

**Python 样例**（coding_memory_tools.py L20-53）：
```python
async def init_memory_manager_async(
    workspace: Workspace,
    agent_id: str = "default",
    embedding_config: Optional[EmbeddingConfig] = None,
    sys_operation: Optional["SysOperation"] = None,
    llm: Optional[Any] = None,       # ← 有 LLM 参数
) -> Optional[MemoryIndexManager]:
    manager = await MemoryIndexManager.get(params)
    if manager:
        if llm:
            manager.llm = llm          # ← 设置 LLM
```

**Go 问题**（tools.go:44-69）：
```go
func InitCodingMemoryManagerAsync(ctx context.Context, ws *workspace.Workspace, agentID string, embeddingConfig *embedding.EmbeddingConfig, sysOp sysop.SysOperation) (MemoryIndexManager, error) {
    // ... 没有 llm 参数
    mgr, err := GetMemoryIndexManager(params)
    // ... 没有 mgr.SetLLM() 调用
```

**修复方案**：在 `InitCodingMemoryManagerAsync` 中添加 `llm` 参数，并在创建 manager 后设置：
```go
func InitCodingMemoryManagerAsync(ctx context.Context, ws *workspace.Workspace, agentID string, embeddingConfig *embedding.EmbeddingConfig, sysOp sysop.SysOperation, llm any) (MemoryIndexManager, error) {
    mgr, err := GetMemoryIndexManager(params)
    if err != nil { return nil, err }
    if llm != nil { mgr.SetLLM(llm) }
    return mgr, nil
}
```
同时在 `MemoryIndexManager` 接口中添加 `SetLLM(llm any)` 方法。

---

## 二、一般问题

### G01：MemoryIndexManager._should_full_reindex 中 provider 比对使用 settings.Provider 而非 provider.ID

**章节**：7.2 manager

**问题描述**：Python 中 `_should_full_reindex` 比对 `meta.get("provider") != self.provider.id`，使用的是 provider 实例的 id 属性。Go 中比对 `meta["provider"] != m.settings.Provider`，使用的是配置中的 provider 字符串。两者可能不一致。

**Python 样例**（manager.py L600-601）：
```python
if meta.get("provider") != self.provider.id:
    return True
```

**Go 问题**（manager_impl.go L377-378）：
```go
if meta["provider"] != m.settings.Provider {
    return true
}
```

**修复方案**：将比对改为使用 `m.provider.ID()`（如果 provider 接口有此方法），或在 `initializeProvider` 中将 provider.id 保存到字段，用该字段比对。

---

### G02：MemoryIndexManager.search 缺少 _embed_query_with_timeout 超时保护

**章节**：7.2 manager

**问题描述**：Python 中搜索时调用 `_embed_query_with_timeout`，有 60 秒超时保护。Go 中 `Search` 直接调用 `m.provider.EmbedQuery(ctx, cleaned)`，没有独立的超时保护。

**Python 样例**（manager.py L1113-1126）：
```python
async def _embed_query_with_timeout(self, query: str) -> List[float]:
    timeout = 60.0
    return await asyncio.wait_for(
        self.provider.embed_query(query),
        timeout=timeout
    )
```

**Go 问题**（manager_impl.go L777）：
```go
queryVec, _ := m.provider.EmbedQuery(ctx, cleaned)
```

**修复方案**：添加带超时的 embed 调用：
```go
embedCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
defer cancel()
queryVec, _ := m.provider.EmbedQuery(embedCtx, cleaned)
```

---

### G03：MemoryIndexManager._index_file 中 sysOperation 为 nil 时 fallback 路径不一致

**章节**：7.2 manager

**问题描述**：Python 中 `_index_file` 如果 `sys_operation` 为 None，会记录错误日志 `logger.error("no available sys_operation when _index_file")`，但**不会 fallback 到 os.ReadFile**，导致 content 变量未定义。Go 中有 fallback 到 `os.ReadFile`，行为与 Python 不一致。

**Python 样例**（manager.py L715-720）：
```python
if self.sys_operation:
    read_result = await self.sys_operation.fs().read_file(entry["absPath"])
    content = read_result.data.content
else:
    logger.error("no available sys_operation when _index_file")
```

**Go 问题**（manager_impl.go L517-532）：
```go
if m.sysOperation != nil {
    // sys_operation 读取
} else {
    data, err := os.ReadFile(absPath)
    // fallback 到 os.ReadFile
}
```

**修复方案**：Go 的 fallback 行为更健壮，但与 Python 不一致。建议保留 Go 的 fallback 但添加 Warn 日志：
```go
} else {
    logger.Warn(logger.ComponentCommon).Msg("sys_operation 不可用，fallback 到 os.ReadFile")
    data, err := os.ReadFile(absPath)
```

---

### G04：CodeAdapter buildCodeAgentRails 中 ConfirmInterruptRail 缺少 tool_names 参数

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `ConfirmInterruptRail` 构建时传入 `tool_names=["switch_mode"]` 参数。Go 中 `buildConfirmInterruptRail` 不接收参数，即使实现也会缺少 tool_names 配置。

**Python 样例**（interface_code.py L378-382）：
```python
_RailBuildInfo(
    "_code_confirm_interrupt_rail",
    self._build_confirm_interrupt_rail,
    {"tool_names": ["switch_mode"]},
),
```

**Go 问题**（code_adapter.go L639-645）：
```go
func (c *CodeAdapter) buildConfirmInterruptRail() sainterfaces.AgentRail {
    // ⤵️ 10.6.3-10: 实现 ConfirmInterruptRail
    return nil
}
```

**修复方案**：实现时添加 `toolNames` 参数：
```go
func (c *CodeAdapter) buildConfirmInterruptRail() sainterfaces.AgentRail {
    return NewConfirmInterruptRail(WithToolNames("switch_mode"))
}
```

---

### G05：CodeAdapter 缺少 _TOOL_BUILD_NAMES 和 build_code_tool_cards 动态工具注册

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `JiuwenClawCodeAdapter` 有完整的 `_TOOL_BUILD_NAMES` 映射和 `build_code_tool_cards` 方法，从 config.yaml::modes.code.tools 读取动态工具列表并注册。Go 中 CodeAdapter 的 `getToolCards` 委托 DeepAdapter，没有 code 模式专有的工具注册。

**Python 样例**（interface_code.py L93-100, L931-965）：
```python
_TOOL_BUILD_NAMES: dict[str, str] = {
    "web_free_search": "_build_web_free_search_tool",
    "web_fetch_webpage": "_build_web_fetch_webpage_tool",
    "web_paid_search": "_build_paid_search_tool",
    "user_todos": "_build_user_todos_tool",
    "skill_toolkit": "_build_skill_toolkit",
    "acp_chat": "_build_acp_chat_tool",
}

async def _get_tool_cards(self, agent_id: str) -> list[Any]:
    return self.build_code_tool_cards(agent_id)
```

**Go 问题**：CodeAdapter 没有覆盖 `getToolCards`，直接使用 DeepAdapter 的通用实现。

**修复方案**：在 CodeAdapter 中添加 `codeToolBuildNames` 映射和 `buildCodeToolCards` 方法，对齐 Python 的动态工具注册。

---

### G06：CodeAdapter 缺少 _update_runtime_config 方法

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `JiuwenClawCodeAdapter` 覆盖了 `_update_runtime_config`，包含 ProjectMemoryRail 语言同步、trusted_dirs 注入、RuntimePromptRail 配置、user_todos 同步等。Go 中完全缺失此方法。

**Python 样例**（interface_code.py L852-911）：
```python
async def _update_runtime_config(self, runtime_config) -> None:
    self._seed_runtime_cwd(...)
    if self._runtime_prompt_rail:
        self._runtime_prompt_rail.set_language(resolved_language)
        self._runtime_prompt_rail.set_force_english(self._force_english_runtime_prompt)
        self._runtime_prompt_rail.set_channel(resolved_channel)
        ...
    if self._project_memory_rail is not None:
        self._project_memory_rail.set_language(resolved_language)
        self._project_memory_rail.set_additional_directories(runtime_config.trusted_dirs)
    await self._update_rails_for_mode(runtime_config.mode)
    ...
```

**Go 问题**：CodeAdapter 无 `UpdateRuntimeConfig` 方法。

**修复方案**：在 CodeAdapter 中实现 `UpdateRuntimeConfig`，对齐 Python 的完整运行时配置逻辑。

---

### G07：CodeAdapter 缺少 _resolve_output_language 方法

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `JiuwenClawCodeAdapter` 有 `_resolve_output_language` 方法，区分 prompt 语言（始终 en）和输出语言（用户偏好），用于 RuntimePromptRail 的语言段注入。Go 中没有此方法。

**Python 样例**（interface_code.py L204-217）：
```python
def _resolve_output_language(self) -> str:
    """Resolve user's preferred output language for runtime_state display."""
    config_base = get_config()
    raw = str(config_base.get("preferred_language", "zh")).strip().lower()
    if raw == "zh":
        raw = "cn"
    return resolve_language(raw)
```

**Go 问题**：CodeAdapter 无 `resolveOutputLanguage` 方法。

**修复方案**：在 CodeAdapter 中实现 `resolveOutputLanguage`，对齐 Python 的逻辑。

---

### G08：MemoryIndexManager provider_key 格式与 Python 不一致

**章节**：7.2 manager

**问题描述**：Python 中 `provider_key` 格式为 `{provider.id}:{provider.model}`，例如 `openai_compatible:text-embedding-v3`。Go 中格式为 `provider:{model}`，缺少了 provider.id 部分。这会影响 embedding 缓存的查询键——如果 provider 变化但 model 不变，Go 的缓存会错误命中。

**Python 样例**（manager.py L372）：
```python
self.provider_key = f"{self.provider.id}:{self.provider.model}"
```

**Go 问题**（manager_impl.go L287）：
```go
m.providerKey = fmt.Sprintf("provider:%s", m.settings.Model)
```

**修复方案**：对齐 Python 格式，使用 provider ID：
```go
m.providerKey = fmt.Sprintf("%s:%s", m.provider.ID(), m.settings.Model)
```

---

### G09：UapClaw.GetInstance() 始终返回 nil

**章节**：10.3.2 UapClaw

**问题描述**：Python 中 `JiuWenClaw.get_instance()` 返回 `self._adapter._instance`（DeepAgent 实例），用于外部获取 Agent 实例。Go 中 `GetInstance()` 始终返回 nil，没有实际实现。

**Python 样例**（interface.py）：
```python
def get_instance(self):
    return self._adapter._instance
```

**Go 问题**（uapclaw.go L714）：
```go
func (uc *UapClaw) GetInstance() *harness.DeepAgent { return nil }
```

**修复方案**：实现 GetInstance，从 adapter 获取底层实例：
```go
func (uc *UapClaw) GetInstance() *harness.DeepAgent {
    if da, ok := uc.adapter.(*adapter.DeepAdapter); ok {
        return da.GetInstance()
    }
    if ca, ok := uc.adapter.(*adapter.CodeAdapter); ok {
        return ca.GetInstance()
    }
    return nil
}
```

---

## 三、提示问题

### T01：frontmatter.go RebuildContentWithFrontmatter 中 map 遍历顺序不确定

**章节**：7.5 frontmatter

**问题描述**：Go 中 `RebuildContentWithFrontmatter` 使用 `for key, value := range fm` 遍历 map，Go 的 map 遍历顺序是随机的，导致 frontmatter 字段顺序不确定。Python 中 dict 在 3.7+ 是插入顺序保持的，字段顺序固定。

**Python 样例**（frontmatter.py L48-51）：
```python
fm_lines = ["---"]
for key, value in fm.items():
    fm_lines.append(f"{key}: {value}")
fm_lines.append("---")
```

**Go 问题**（frontmatter.go L76-78）：
```go
for key, value := range fm {
    fmLines = append(fmLines, key+": "+value)
}
```

**修复方案**：使用有序字段列表保证输出顺序：
```go
var fmOrder = []string{"name", "description", "type", "created_at", "updated_at"}
for _, key := range fmOrder {
    if value, ok := fm[key]; ok {
        fmLines = append(fmLines, key+": "+value)
    }
}
// 写入不在 fmOrder 中的剩余字段
for key, value := range fm {
    if !inSlice(fmOrder, key) {
        fmLines = append(fmLines, key+": "+value)
    }
}
```

---

### T02：coding_memory_tool_ops.go 中 searchSimilar 使用 context.Background() 而非传入的 ctx

**章节**：7.2 coding_memory_tool_ops

**问题描述**：Go 中 `searchSimilar` 和 `prepareAppendMode` 内部调用 `toolCtx.Manager.Search(context.Background(), ...)` 和 `sysOp.Fs().ReadFile(context.Background(), ...)` 使用了 `context.Background()` 而非传入的 `ctx`。Python 中使用 `await` 继承调用者的上下文。

**Go 问题**（coding_memory_tool_ops.go L393, L439）：
```go
results, err := toolCtx.Manager.Search(context.Background(), body, ...)
readResult, err := sysOp.Fs().ReadFile(context.Background(), fullPath)
```

**修复方案**：将 `ctx context.Context` 参数传入 `searchSimilar` 和 `prepareAppendMode`，使用传入的 ctx：
```go
func searchSimilar(ctx context.Context, toolCtx *CodingMemoryToolContext, ...) map[string]string {
    results, err := toolCtx.Manager.Search(ctx, body, ...)
```

---

### T03：snapshotMemoryFiles 使用 os.ReadDir 而非 sysOp.Fs().ListFiles

**章节**：7.2 coding_memory_tool_ops

**问题描述**：Python 中 `_snapshot_memory_files` 使用 `ctx.sys_operation.fs().list_files()` 读取目录，Go 中使用 `os.ReadDir`。在沙箱环境中，coding_memory 目录可能不在本地文件系统上，直接用 `os.ReadDir` 可能读不到文件。

**Python 样例**（coding_memory_tool_ops.py L283-308）：
```python
result = await ctx.sys_operation.fs().list_files(
    memory_dir, recursive=False
)
```

**Go 问题**（coding_memory_tool_ops.go L473-500）：
```go
func snapshotMemoryFiles(toolCtx *CodingMemoryToolContext, memoryDir string) []string {
    entries, err := os.ReadDir(memoryDir)
```

**修复方案**：优先使用 sysOp.Fs().ListFiles，fallback 到 os.ReadDir：
```go
if sysOp := toolCtx.SysOperation; sysOp != nil {
    result, err := sysOp.Fs().ListFiles(ctx, memoryDir, false)
    if err == nil && result != nil { /* 使用 result */ }
}
// fallback
entries, err := os.ReadDir(memoryDir)
```

---

### T04：UapClaw.ProcessMessageStream 中 Stream 模式下缺少 CANCEL 分支

**章节**：10.3.2 UapClaw

**问题描述**：Python 中 `process_message_stream` 有 CANCEL 分支处理，Go 中 `ProcessMessageStream` 没有处理 `ReqMethodChatCancel` 请求。

**Python 样例**（interface.py L909-918 附近）：
```python
# cancel 分支在流式入口也处理
if request.req_method == ReqMethod.CHAT_CANCEL:
    return self._process_interrupt(request)
```

**Go 问题**（uapclaw.go L274-518）：
```go
func (uc *UapClaw) ProcessMessageStream(...) {
    // 1. SkillDev 流式分支
    // 2. 确保 adapter
    // 3. 提取 sessionID
    // 没有 CANCEL 分支
}
```

**修复方案**：在 `ProcessMessageStream` 开头添加 CANCEL 分支：
```go
if request.ReqMethod == schema.ReqMethodChatCancel {
    // 返回中断响应 channel
    ch := make(chan *schema.AgentResponseChunk, 1)
    resp, err := uc.ProcessInterrupt(ctx, request)
    // 将 resp 转为 chunk 发送
    ...
    return ch, nil
}
```

---

### T05：CodeAdapter codeFixedRailNames 缺少 CodeAgentRail

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `_FIXED_RAIL_NAMES` 不包含 `CodeAgentRail`（因为它是最后一个固定 Rail，不在 Python 的 frozenset 中）。Go 的 `codeFixedRailNames` 包含了 `CodeAgentRail`，与 Python 不一致。

**Python 样例**（interface_code.py L167-175）：
```python
_FIXED_RAIL_NAMES = frozenset({
    "RuntimePromptRail", "ResponsePromptRail",
    "JiuClawStreamEventRail", "SecurityRail",
    "LspRail", "ProjectMemoryRail", "PermissionInterruptRail",
    "ContextProcessorRail",
    "SysOperationRail", "CodingMemoryRail",
    "AgentModeRail", "StructuredAskUserRail", "ConfirmInterruptRail",
    "FileSystemRail",  # 别名
})
# 注意：CodeAgentRail 不在 _FIXED_RAIL_NAMES 中
```

**Go 问题**（code_adapter.go L88）：
```go
"CodeAgentRail":           true,  // Python 中不在 _FIXED_RAIL_NAMES
```

**修复方案**：从 `codeFixedRailNames` 中移除 `CodeAgentRail`，对齐 Python。

---

### T06：CodeAdapter 缺少 merge_member_mcp_configs 方法

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 中 `JiuwenClawCodeAdapter` 有 `merge_member_mcp_configs` 方法，用于将 code 模式的 MCP 配置合并到团队子代理。Go 中没有此方法。

**Python 样例**（interface_code.py L1045-1072）：
```python
def merge_member_mcp_configs(self, agent: Any, config_base: dict[str, Any]) -> int:
    """Merge enabled code-mode MCP configs into a team member agent."""
```

**修复方案**：在 CodeAdapter 中实现 `MergeMemberMCPConfigs`，对齐 Python。

---

### T07：MemoryIndexManager.search 中 min_score 默认值与 Python 不一致

**章节**：7.2 manager

**问题描述**：Python 中 `search` 的默认 min_score 为 0.7，Go 中为 0.3。

**Python 样例**（manager.py L876-877）：
```python
min_score = opts.get("min_score") if opts and "min_score" in opts else \
    self.settings.query.get("min_score", 0.7)
```

**Go 问题**（manager_impl.go L735-738）：
```go
minScore := 0.3
if v, ok := opts["min_score"].(float64); ok {
    minScore = v
} else if v, ok := m.settings.Query["min_score"].(float64); ok {
    minScore = v
}
```

**修复方案**：将默认值改为 0.7，对齐 Python。

---

### T08：CodeAdapter buildCodeAgentRails 缺少 ContextAssembleRail

**章节**：10.3.4-6 CodeAdapter

**问题描述**：Python 的 `_RAIL_BUILD_NAMES` 中有 `ContextAssembleRail` 对应 `_build_context_assemble_rail`，但 Go 的 `codeRailBuildNames` 中没有此映射。动态 Rails 中如果配置了 `ContextAssembleRail` 将无法构建。

**Python 样例**（interface_code.py L84）：
```python
"ContextAssembleRail": "_build_context_assemble_rail",
```

**Go 问题**（code_adapter.go L93-106）：缺少 `ContextAssembleRail` 映射。

**修复方案**：在 `codeRailBuildNames` 中添加：
```go
"ContextAssembleRail": "buildContextAssembleRail",
```

---

## 四、⤵️ 待回填标记汇总

| 标记位置 | 内容 | 状态 | 优先级 |
|---------|------|------|--------|
| coding_memory_tool_ops.go L197-199 | 7.8 MemUpdateChecker LLM 冗余判断 | 未实现 | 高（S01） |
| code_adapter.go L311 | 步骤 21.1 _jiuwenswarm_adapter_mode = "code" | 未实现 | 中 |
| code_adapter.go L315 | 步骤 21.2 coding_memory workspace set_directory | 未实现 | 高（S03） |
| code_adapter.go L319 | 步骤 21.3 agent_history 写入路径修正 | 未实现 | 中 |
| code_adapter.go L331 | 步骤 24 load_user_rails() | 未实现 | 中 |
| code_adapter.go L454 | LspRail 尚未实现 | 未实现 | 中 |
| code_adapter.go L461 | ProjectMemoryRail 尚未实现 | 未实现 | 中 |
| code_adapter.go L468 | PermissionInterruptRail 尚未实现 | 未实现 | 高（S06） |
| code_adapter.go L482 | CodingMemoryRail 尚未实现 | 未实现 | 中 |
| code_adapter.go L495 | StructuredAskUserRail 尚未实现 | 未实现 | 中 |
| code_adapter.go L501 | ConfirmInterruptRail 尚未实现 | 未实现 | 中 |
| code_adapter.go L609 | WorktreeRail 尚未实现 | 未实现 | 中 |
| uapclaw.go L513 | Team 后续请求绕过 Session 队列 | 未实现 | 低 |
| uapclaw.go L773 | team_manager.pause_session_runtime | 未实现 | 低 |
| uapclaw.go L792 | team_manager.cancel_session_runtime | 未实现 | 低 |
| uapclaw.go L826 | cancelTeamWorkForSession 等待 TeamManager | 未实现 | 低 |

---

## 五、9.65-1 循环依赖重构确认

本次重构涉及以下改动，已确认无逻辑遗漏：

| 改动项 | 状态 | 备注 |
|--------|------|------|
| MessagerTransportConfig/TeamMemoryConfig/constants/i18n 搬入 schema 包 | ✅ | 打断 schema→messager/memory 循环链 |
| SessionState + context 函数搬入 schema 包 | ✅ | tools 包可调用 GetSessionID |
| Messager 接口改回 *schema.EventMessage | ✅ | 删除 SenderIDStamper |
| tools 包 publishTaskEvent/publishMessageEvent 改用 schema.TypedEvent | ✅ | 删除重复常量 |
| MessageID 改用 UUID v4 | ✅ | 对齐 Python uuid4() |
| 删除 sessionID 字段 | ✅ | 改用 schema.GetSessionID(ctx) |

---

## 六、问题统计

| 严重程度 | 数量 | 问题编号 |
|---------|------|---------|
| 严重 | 9 | S01-S09 |
| 一般 | 9 | G01-G09 |
| 提示 | 8 | T01-T08 |
| **合计** | **26** | |

---

## 七、修复优先级建议

### 第一优先级：立即修复（简单但影响大）

1. **S09（InitCodingMemoryManagerAsync 缺少 LLM 参数）**：即使 MemUpdateChecker 实现了也无法工作，是 S01 的前置阻塞项
2. **T07（min_score 默认值 0.3 vs 0.7）**：一行修改，影响搜索结果质量
3. **T01（map 遍历顺序不确定）**：简单修复，影响 frontmatter 输出一致性

### 第二优先级：7.8 实现后回填

4. **S01（SKIP 返回路径）**：依赖 7.8 MemUpdateChecker，但 S09 修复后才能完整工作
5. **S03（coding_memory workspace set_directory）**：依赖 Workspace.SetDirectory 实现

### 第三优先级：CodeAdapter 核心重写

6. **S07（explore_agent/plan_agent 子代理）**：Code 模式与 Deep 模式的核心差异，缺少会导致 Code 模式完全丧失搜索/规划能力
7. **S08（code 专有工具注册）**：Code 模式无法使用 web_search/skill_toolkit 等工具
8. **S05（_update_rails_for_mode）**：Code 模式 Rail 生命周期管理缺失
9. **S06（PermissionInterruptRail）**：权限护栏缺失，影响安全

### 第四优先级：Team 模式回填

10. **S04（configure_team_member_agent）**：Team 模式下子代理无法获得 code 配置
11. **S02（_get_current_agent_rails 热重载）**：config reload 时 CodeAgentRail 不被重新初始化
12. **G06（_update_runtime_config）**：运行时配置同步缺失

### 建议的修复顺序

```
S09 → T07 → T01 → S01 → S07 → S08 → S05 → S06 → S03 → S04 → S02 → G06
```

S07/S08 是 CodeAdapter 的核心功能缺失，**不修复则 Code 模式行为与 Deep 模式完全一致，失去 code 专有功能**。建议作为一个整体任务集中实现。
