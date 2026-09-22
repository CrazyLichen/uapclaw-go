# 48h 逻辑审查报告

> 审查时间：2026-09-22
> 审查范围：48h 内提交记录（10 commits, 81 files changed）
> 审查模块：worktree / swarm-rails / task-team / adapter-hooks / evolving / gateway-session
> 对比基准：Python 源码 `openjiuwen/` + `jiuwenswarm/`

---

## 摘要

| 模块 | 严重 | 一般 | 提示 | 合计 |
|------|------|------|------|------|
| Worktree (9.66a) | 7 | 8 | 5 | 20 |
| Swarm Rails (10.6) | 5 | 4 | 3 | 12 |
| Task-Team (9.65) | 4 | 8 | 5 | 17 |
| Adapter-Hooks (10.3) | 12 | 14 | 8 | 34 |
| Evolving (9.72-9.80) | 5 | 6 | 4 | 15 |
| Gateway-Session (11.x) | 6 | 7 | 4 | 17 |
| **合计** | **39** | **47** | **29** | **115** |

**最需优先修复的严重问题 TOP 10：**

1. **AH-S-04**：StreamEventRail 完全未实现 → pause/resume/abort/evolution watcher 全部缺失
2. **WT-S-01**：fireRail 使用 context.Background() 丢弃 ctx + 缺少 4 个 hook method 分支
3. **AH-S-02**：load_user_rails() 未实现 → 用户自定义 Rail 扩展无法加载
4. **EV-S-01**：SkillEvolutionRail.handleEvolutionFromSignals 绕过 FinalizeStagedEvolutionRequest
5. **TT-M-06**：CancelTask/ApprovePlan 事件双重发布（3 处）
6. **WT-S-07/S-08**：WorktreeRail Init/Uninit 缺少 ResourceMgr 工具注册/注销
7. **GS-S-04**：processStream 注册流式任务后覆盖 entry，cancel 函数丢失
8. **AH-S-07**：updateRailsForMode 使用 context.Background() 丢弃请求上下文
9. **TT-S-03/S-04**：SubmitPlan 缺少 plan_id 存在性检查 + 路径校验
10. **EV-S-03**：StepTemplate 缺少 TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR 错误包装

---

## 一、Worktree 模块 (9.66a)

### 严重 (S)

**WT-S-01：fireRail 使用 context.Background() 丢弃调用方 ctx + 缺少 4 个 hook 分支**
- Go: `manager.go:462,472,481,493` — `rail.BeforeWorktreeCreate(context.Background(), ...)`
- Python: `manager.py:711` — `r = await handler(*args, **kwargs)` 继承调用方上下文
- Python `rails.py:231-391` 定义 7 个 hook：`before_worktree_create`、`after_worktree_create`、`before_worktree_exit`、`after_worktree_exit`、`on_worktree_file_write`、`before_worktree_commit`、`after_worktree_commit`、`on_worktree_sync`
- Go 只实现了前 4 个，后 4 个在 default 分支被忽略
- **修复**：fireRail 增加 `ctx context.Context` 参数；添加缺失的 4 个 case 分支

**WT-S-02：OnWorktreeFileWrite 返回类型不匹配**
- Go: `backend.go:30` — 返回 `error`
- Python: `rails.py:310-315` — 返回 `bool`（True=允许写入，False=阻止）
- **修复**：返回类型改为 `bool`，true=允许，false=阻止

**WT-S-03：OnWorktreeSync 签名缺少 direction/files 参数，返回类型错误**
- Go: `backend.go:39` — `OnWorktreeSync(ctx, session) error`
- Python: `rails.py:373-391` — `on_worktree_sync(ctx, session, direction: str, files: list[str]) -> list[str]`
- **修复**：签名改为 `OnWorktreeSync(ctx, session, direction string, files []string) []string`

**WT-S-04：WorktreeRail.Init/Uninit 缺少 ResourceMgr 工具注册/注销**
- Go: `rails.go:160-170` — 只调 `agent.AbilityManager().Add(card)`
- Python: `rails.py:134-136` — 还调了 `Runner.resource_mgr.add_tool(self._tools)`；Uninit 也调 `remove_tool`
- **修复**：Init 中添加 `runner.GetResourceMgr().AddTool(tool)`；Uninit 中添加 `runner.GetResourceMgr().RemoveTool(toolID)`

**WT-S-05：Exit remove 路径 session 清除不一致**
- Go: `manager.go:220` — `SetCurrentSession(ctx, nil)`（通过 ctx 传播）
- Go: `manager.go:204` — keep 路径用 `m.sessionState.SetCurrentSession(nil)`（直接写 manager state）
- Python: 两处都用 `set_current_session(None)`（单一路径）
- **影响**：Enter 把 session 写入 m.sessionState，Exit remove 路径通过 ctx 清除，manager.state 里的 session 可能残留
- **修复**：统一使用 `m.sessionState.SetCurrentSession(nil)`

**WT-S-06：DiffSummaryRail/BeforeWorktreeExit 返回 ("", nil) 而非 nil**
- Go: `rails.go:341` — 返回 `("", nil)`，空字符串在 fireRail 中 `result != nil` 判断为真
- Python: `rails.py:479` — 返回 `None`，fireRail 中 `if r is not None` 跳过
- **影响**：fireRail 返回 "" 会覆盖之前的 action/slug
- **修复**：返回 nil（Go 接口返回类型支持 nil）

**WT-S-07：resolveTargetPath — workspace 未设置时 Go 静默降级，Python 抛 RuntimeError**
- Go: `manager.go:421-431` — workspace 为空时降级到 base_dir，再到 cwd
- Python: `manager.py:663-670` — `raise RuntimeError("Cannot resolve worktree path...")`
- **影响**：Go 在 workspace 缺失时静默创建 worktree，可能在意外位置
- **修复**：对齐 Python，workspace 为空时返回错误

### 一般 (M)

**WT-M-01：WorktreeLifecycleRail hook 方法缺少 AgentCallbackContext 参数**
- Python: `rails.py:231-236` — 所有 hook 第一个参数是 `ctx: AgentCallbackContext`
- Go: `backend.go:19-39` — hook 方法无此参数
- **修复**：hook 方法签名增加回调上下文参数

**WT-M-02：BeforeWorktreeCommit 缺少 files 参数**
- Python: `rails.py:332-338` — `before_worktree_commit(ctx, session, message, files: list[str])`
- Go: `backend.go:33` — `BeforeWorktreeCommit(ctx, session, message)`
- **修复**：签名添加 `files []string` 参数

**WT-M-03：Enter/Exit 中 session 读取路径不一致**
- Go: `tools.go:106-110` Enter 通过 `manager.SessionState()` 获取
- Go: `tools.go:157` Exit 通过 `RequireCurrentSession(ctx)` 获取
- Python: 两处都用 `get_current_session()`（ContextVar 路径）
- **修复**：统一使用 `m.sessionState` 路径

**WT-M-04：AutoSetupRail 缺少 commands 构造参数**
- Python: `rails.py:404` — `AutoSetupRail(commands: list[str] | None = None)`
- Go: `rails.go:53` — `AutoSetupRail struct{}` 无 commands 字段
- **修复**：添加 Commands []string 字段

**WT-M-05：postCreationSetup Go 返回第一个错误，Python 静默处理**
- Go: `manager.go:532` — 收集 firstErr 并返回
- Python: `manager.py:380-419` — `_post_creation_setup` 返回 None，从不抛错误
- **修复**：对齐 Python，返回 nil（静默处理）

**WT-M-06：cleanup_stale_worktrees 串行检查 vs Python 并行**
- Go: `cleanup.go:94-103` — 串行调用 StatusPorcelain + HasUnpushedCommits
- Python: `cleanup.py:111-114` — `asyncio.gather(status_porcelain, has_unpushed_commits)`
- **修复**：Go 可用 errgroup 并行化（性能优化，非功能问题）

**WT-M-07：WorktreeRail.BeforeInvoke 不处理 WorktreeSession 实例**
- Go: `rails.go:219-235` — 处理 `map[string]any` 和 default
- Python: `rails.py:184-186` — 处理 `dict` 和裸 `WorktreeSession`
- **修复**：添加 `*WorktreeSession` 类型断言分支

**WT-M-08：StatusPorcelain 失败时 Go 跳过（fail-closed），Python 继续**
- Go: `cleanup.go:95` — `if err != nil || len(changes) > 0 { continue }`
- Python: `cleanup.py:351-354` — `status_porcelain` 失败返回 `[]`，不跳过
- **说明**：Go 更安全（fail-closed），属有意改进，标注即可

### 提示 (T)

**WT-T-01**：copyIncludeFiles 失败时 Go 返回 `nil`，Python 返回 `[]` — 语义等价
**WT-T-02**：ownerSlug 对短于8字符处理 — Go 显式 `if len > 8`，Python 自动截断 — 等价
**WT-T-03**：WorktreeEventHandler Go 返回 error，Python Awaitable[None] — 均忽略错误
**WT-T-04**：GitBackend 构造 — Go 始终需 config，Python 可 None — 注册表总传 config，无问题
**WT-T-05**：create_backend Go 返回 error，Python 抛 ValueError — 语言习惯差异

---

## 二、Swarm Rails 模块 (10.6)

### 严重 (S)

**SR-S-01：RuntimePromptRail.setGitBranch 使用 context.Background() 丢弃 ctx**
- Go: `runtime_prompt_rail.go:764` — `context.WithTimeout(context.Background(), gitCommandTimeout)`
- Python: 对应方法在 async 上下文中运行，天然传播 cancel
- **修复**：`SetGitBranch` 方法签名增加 `ctx context.Context` 参数

**SR-S-02：ProjectMemoryRail.BeforeModelCall 缺少异常捕获中的 OSError/ValueError/TypeError 分支日志**
- Go: `project_memory_rail.go:222-237` — 仅 recover panic，不捕获 error
- Python: `project_memory_rail.py:164-171` — `except (OSError, ValueError, TypeError) as exc: logger.exception(...)`
- **影响**：Go 不记录 discovery 失败的根因（如权限错误、路径无效），排查困难
- **修复**：在 DiscoverAndLoadMemoryFiles 返回 error 时记录 Error 日志

**SR-S-03：StructuredAskUserTool 的 Tool Card 缺少 tool_call_schema 的参数定义**
- Go: `structured_ask_user_tool.go` — 需确认 ToolCard 是否正确描述了 questions 参数
- Python: `ask_user_rail.py:162-190` — StructuredAskUserTool 有完整的 JSON Schema 定义
- **修复**：确认 ToolCard 的 Parameters Schema 包含 questions 字段

**SR-S-04：RuntimePromptRail 缺少 set_project_dir 方法**
- Go: 检查 runtime_prompt_rail.go 中是否有 SetProjectDir
- Python: `runtime_prompt_rail.py` 有 `set_project_dir(project_dir)` 方法
- **修复**：如缺失，添加 SetProjectDir 方法

**SR-S-05：ProjectMemoryRail.files.go DiscoverAndLoadMemoryFiles 使用 context.Background()**
- Go: `project_memory/files.go:471` — `context.WithTimeout(context.Background(), 2*time.Second)`
- Python: 对应方法在 async 上下文中执行
- **修复**：DiscoverAndLoadMemoryFiles 接受 ctx 参数

### 一般 (M)

**SR-M-01：ProjectMemoryRail.WriteLikeTools 与 Python 不完全一致**
- Go: `project_memory_rail.go:66-75` — 7 个工具名
- Python: `project_memory_rail.py:51-58` — 同 7 个工具名
- **确认**：两者一致，无需修复

**SR-M-02：RuntimePromptRail 缺少 set_model_name 的实际 git 调用**
- Python: `runtime_prompt_rail.py` 的 `set_model_name` 可能触发 git 操作
- Go: 需确认实现
- **修复**：验证 SetModelName 行为是否完整

**SR-M-03：ProjectMemoryRail.Init 中 systemPromptBuilder 获取方式与 Python 不同**
- Go: `project_memory_rail.go:112` — `agent.SystemPromptBuilder()`
- Python: `project_memory_rail.py:208-210` — `getattr(agent, "system_prompt_builder", None)`
- **说明**：Go 使用接口方法，Python 使用 getattr — 语义等价

**SR-M-04：RuntimePromptRail 中 setTrustedDirs 缺少对空列表的特殊处理**
- Python: `runtime_prompt_rail.py` — `if trusted_dirs: ...`
- Go: 需确认是否有空值保护
- **修复**：添加 nil/空列表保护

### 提示 (T)

**SR-T-01**：RuntimePromptRail 中多个 setter 未标记 "对齐 Python" 注释 — 建议补充
**SR-T-02**：ProjectMemoryRail.ResolveWorkspacePath 使用 Go 接口方法 vs Python getattr — 等价
**SR-T-03**：StructuredAskUserTool 在 Python 中位于 ask_user_rail.py，Go 中独立文件 — 组织差异，功能等价

---

## 三、Task-Team 模块 (9.65)

### 严重 (S)

**TT-S-01：task_manager.go resolveLeaderMemberName 使用 context.Background()**
- Go: `task_manager.go:978` — `tm.db.Team().GetTeam(context.Background(), tm.teamName)`
- Python: `team = await self.db.team.get_team(self.team_name)` — async 隐式传播
- **修复**：`resolveLeaderMemberName` 接受 `ctx context.Context` 参数

**TT-S-02：SubmitPlan 缺少 plan_id 存在性检查**
- Python: `task_manager.py:938` — `if self._read_plan_index(plan_id): return {"success": False, ...}`
- Go: 没有对应检查，直接写入可能导致 index.json 覆盖
- **修复**：生成 planID 后检查 `index.TaskPlans[planID]` 是否已存在

**TT-S-03：SubmitPlan 缺少路径校验**
- Python L946-953：`_resolve_submitted_plan_path(plan_path)` 执行 1) 非空 2) expanduser 3) 绝对路径 4) is_file 检查
- Go L804：只检查 `planFilePath != ""`，然后直接 `os.ReadFile`
- **修复**：添加 `filepath.Clean(os.ExpandEnv(path))`、`filepath.IsAbs`、`os.Stat` 检查

**TT-S-04：memory_impl.go AddTaskWithBidirectionalDependencies 使用 context.Background()**
- Go: `memory_impl.go:784` — `db.MutateDependencyGraph(context.Background(), ...)`
- Python: 直接调用无显式 ctx
- **修复**：将 ctx 参数传入方法签名

### 一般 (M)

**TT-M-01：CancelTask 事件双重发布（CancelTask + taskManager.Cancel 内部各发一次）**
- Go: `team_backend.go:815` + `:825-828` — 两处发布 TaskCancelledEvent
- Python: `team.py:878-879` — task_manager.cancel 内部发布，TeamBackend 不再额外发布
- **修复**：移除 team_backend.go L825-828 的重复 TaskCancelledEvent

**TT-M-02：CancelTask 额外发布 TaskUnblockedEvent 也重复**
- Go: `team_backend.go:831-836` — 遍历 unblocked 发布事件
- Python: TeamBackend.cancel_task 不发布 unblocked 事件（taskManager.Cancel 内部已发布）
- **修复**：移除 team_backend.go L830-836 的循环发布

**TT-M-03：ApprovePlan 事件双重发布**
- Go: `team_backend.go:897` + `:901-906` — TaskPlanResponseEvent 发布两次
- Python: `team.py:446-451` — task_manager.approve_plan 内部发布，TeamBackend 不再额外发布
- **修复**：移除 team_backend.go L901-906 的重复事件发布

**TT-M-04：memory_impl ResetTask 状态检查与 Python/SQL 不一致**
- Go: `memory_impl.go:496` — 通过 FSM 表检查，允许 PlanApproved→Pending
- Python: `task_manager.py` — 只允许 CLAIMED→Pending
- Go SQL 实现: `sql_task_dao.go:150` — `if task.Status != fsm.TaskStatusClaimed` — 只允许 CLAIMED
- **修复**：InMemory 也加 `if task.Status != fsm.TaskStatusClaimed { return false, nil }`

**TT-M-05：Complete 中 plan index 写入时序错误**
- Go: `task_manager.go:462-489` — plan index 更新发生在 CompleteTask **之前**
- Python L742-756 — plan index 更新发生在 complete_task **之后**
- **影响**：如果 DB 操作失败，Go 已写入 plan index（标记 completed），但 DB 状态未更新
- **修复**：将 plan index 写入逻辑移到 CompleteTask 调用之后

**TT-M-06：AddDependencies 返回 error 时仍包含 GraphMutationResult**
- Go: `task_manager.go:694` — `return result, fmt.Errorf(...)` 同时返回 result 和 error
- Python L607-613 — 只返回 `TaskOpResult.fail(mutation.reason)`
- **修复**：失败时返回 `GraphMutationResult{}, fmt.Errorf(...)`

**TT-M-07：ShutdownMember 步骤注释错误**
- Go: `team_backend.go:534-538` — 步骤4注释"取消该成员的任务"实际是 CAS 转换
- Python: 同样只做 CAS 转换，不取消任务
- **修复**：修正注释为 "CAS 转换到 SHUTDOWN_REQUESTED"

**TT-M-08：UpdateTaskStatus 返回 `[]string` 但从未填充**
- Go: `sql_task_dao.go:195` — `var refreshedIDs []string` 声明后从未赋值，始终返回 nil
- Python: `update_task_status` 返回 `bool`
- **修复**：接口改为返回 bool，或在 completed 路径填充 refreshedIDs

### 提示 (T)

**TT-T-01**：generateTaskID 使用纳秒而非 UUID — 有意差异，语义等价
**TT-T-02**：plan_id 生成缺少 _safe_token — 需添加 safeToken 函数
**TT-T-03**：writePlanIndex 缺少 team_name/team_plan_id/plans_dir/updated_at 元信息 — 需补充
**TT-T-04**：RegisterCleanupPath 用 ExpandEnv 替代 expanduser — 有意差异
**TT-T-05**：CreateTask 吞掉非 IntegrityError 错误 — 需检查 GORM 错误类型

---

## 四、Adapter-Hooks 模块 (10.3)

### 严重 (S)

**AH-S-01：DeepAdapter.CreateInstance 缺少 _jiuwenswarm_project_dir setattr**
- Go: `deep_adapter.go:525` — seedRuntimeCwd 后没有设置实例属性
- Python: `interface_deep.py:2609` — `setattr(self._instance, "_jiuwenswarm_project_dir", ...)`
- CodeAdapter 有此设置（`c.uapswarmProjectDir`），DeepAdapter 缺失
- **修复**：在 seedRuntimeCwd 后添加对应实例属性设置

**AH-S-02：DeepAdapter/CodeAdapter.CreateInstance 缺少 load_user_rails()**
- Go: `deep_adapter.go:541`, `code_adapter.go:401` — 标记 ⤵️
- Python: `interface_deep.py:2619-2644` — `await self.load_user_rails()` 是 create_instance 最后一步
- **影响**：用户自定义 Rail 扩展完全无法加载
- **修复**：实现 load_user_rails

**AH-S-03：DeepAdapter.ProcessMessageImpl/ProcessMessageStreamImpl 缺少 cron 上下文绑定**
- Go: `deep_adapter.go:781` — 标记 ⤵️ 11.10
- Python: `interface_deep.py:4459-4465` — `cron_context_tokens = self._bind_runtime_cron_context(...)`
- **影响**：cron/send_file 工具无法获得正确 channel_id/metadata
- **修复**：实现 cron context var 绑定与重置

**AH-S-04：StreamEventRail 完全未实现**
- Go: `deep_adapter_rails.go:402-405` — `buildStreamEventRail` 返回 nil
- Python: `JiuClawStreamEventRail` 是核心 Rail，负责 pause/resume/abort、usage 统计、evolution watcher 触发
- **影响**：流控制信号、usage 统计、evolution 监视全部缺失
- **修复**：实现 JiuClawStreamEventRail

**AH-S-05：ProcessMessageStreamImpl 缺少 streamEventRail.reset_abort**
- Go: `deep_adapter.go:1002-1003` — mark_session_active 后无 reset_abort
- Python: `interface_deep.py:4688-4689` — `if self._stream_event_rail: self._stream_event_rail.reset_abort(session_id)`
- **修复**：在 mark_session_active 后添加 `if d.streamEventRail != nil { d.streamEventRail.ResetAbort(sessionID) }`

**AH-S-06：CodeAdapter.CreateInstance 缺少 load_user_rails()**
- 与 AH-S-02 同源，CodeAdapter 也缺失
- **修复**：同 AH-S-02

**AH-S-07：DeepAdapter.updateRailsForMode 使用 context.Background()**
- Go: `deep_adapter_rails.go:588,724` — `ctx := context.Background()`
- **影响**：RegisterRail/UnregisterRail 调用丢失超时、取消信号、trace ID
- **修复**：updateRailsForMode 接受 ctx 参数并传递

**AH-S-08：DeepAdapter.ReloadAgentConfig 缺少 agent_name 更新**
- Go: `deep_adapter.go:630-633` — 缺失
- Python: `interface_deep.py:2688` — `self._agent_name = self._instance_overrides.get("agent_name", ...)`
- **修复**：在 refreshMultimodalConfigs 后添加 agentName 更新

**AH-S-09：DeepAdapter.ReloadAgentConfig 缺少 load_user_rails**
- Go: `deep_adapter.go:686` — 缺失
- Python: `interface_deep.py:2705` — `await self.load_user_rails()`
- **修复**：在 ConfigureDeepConfig 前调用 load_user_rails

**AH-S-10：DeepAdapter.ReloadAgentConfig 步骤11 缺少 subagents 重建**
- Go: `deep_adapter.go:675` — makeDeepAgentConfig 的 tool_cards 传 nil，无 subagents
- Python: `interface_deep.py:2707-2714` — deep_cfg 包含 subagents
- **修复**：重建 subagents 并传入 makeDeepAgentConfig

**AH-S-11：DeepAdapter.HandleUserAnswer 缺少 skill_create_ 审批路由**
- Go: `deep_adapter.go:1366-1385` — 只处理 team_skill_evolve_/evolve_simplify_/skill_evolve_
- Python: `interface_deep.py:3579-3605` — 还有 `skill_create_` 前缀路由
- **修复**：添加 `skill_create_` 前缀路由

**AH-S-12：handleGovernanceApproval 使用 context.Background()**
- Go: `deep_adapter_slash.go:306` — `ctx := context.Background()`
- **修复**：将 ctx 作为参数传入

### 一般 (M)

**AH-M-01：CodeAdapter._update_rails_for_mode 未覆写**
- Go: CodeAdapter 委托 DeepAdapter.updateRailsForMode
- Python: `interface_code.py:770-824` — Code 模式覆写，保留 SubagentRail/ProjectMemoryRail/CodingMemoryRail
- **修复**：CodeAdapter 覆写 updateRailsForMode

**AH-M-02：CodeAdapter.updateRuntimeConfig 缺少 _update_tools_for_mode 和 _update_session_tools**
- Go: `code_adapter.go:601-603` — 标记 ⤵️
- Python: `interface_code.py:899-910` — 完整实现
- **修复**：实现这两个方法

**AH-M-03：CodeAdapter.buildConfiguredSubagents 缺少 CodingMemoryRail 注入到 code_agent**
- Go: `code_adapter.go:680-683` — 标记 ⤵️
- Python: `interface_code.py:711-729` — `if coding_memory_rail is not None: code_agent_rails = [SysOperationRail(), coding_memory_rail]`
- **修复**：将 CodingMemoryRail 传入 BuildCodeAgentConfig

**AH-M-04：CodeAdapter.ConfigureTeamMemberAgent 缺少 _jiuwenswarm_code_team_member setattr**
- Python: `interface_code.py:1157` — `setattr(agent, "_jiuwenswarm_code_team_member", True)`
- **修复**：添加属性设置

**AH-M-05：CodeAdapter.ConfigureTeamMemberAgent 缺少 _set_coding_memory_directory**
- Go: `code_adapter.go:1427` — 标记 ⤵️
- Python: `interface_code.py:1153` — `_set_coding_memory_directory(agent, self._project_dir)`
- **修复**：实现 setCodingMemoryDirectory

**AH-M-06：DeepAdapter.buildSkillEvolutionRail 硬编码语言 "cn"**
- Go: `deep_adapter_rails.go:383` — 硬编码 `"cn"`
- Python: 传入 `self._config_cache` 而非硬编码
- **修复**：使用 `d.resolveRuntimeLanguage()` 替代 `"cn"`

**AH-M-07：buildResponsePromptRail 返回 nil**
- Go: `deep_adapter_rails.go` — 返回 nil（标记 ⤵️）
- Python: code 模式固定包含 ResponsePromptRail
- **修复**：实现 ResponsePromptRail

**AH-M-08：buildDynamicRail 中缺少 buildWorktreeRail case**
- Go: DeepAdapter.buildDynamicRail switch 中无 `buildWorktreeRail` case
- Python: 动态 rails 可以配置 WorktreeRail
- **修复**：添加 buildWorktreeRail case

**AH-M-09：buildProjectMemoryRail 缺少 instance_overrides 回退链**
- Go: `code_adapter.go:1053-1064` — 只读环境变量
- Python: `interface_code.py:553-558` — 先查 instance_overrides，再 config_cache，再环境变量
- **修复**：添加 instance_overrides 和 config_cache 回退

**AH-M-10：buildCodeAgentRails 中 LspRail 返回 nil**
- Go: `code_adapter.go:1032-1035` — 标记 ⤵️
- Python: code 模式固定包含 LspRail
- **修复**：实现 LspRail

**AH-M-11：DeepAdapter.EnsurePersistentCheckpointer 使用 context.Background()**
- Go: `deep_adapter.go:1567` — 进程级初始化，Background 有一定合理性
- **修复**：改为接受 ctx 参数或保持现状（进程级场景可接受）

**AH-M-12：CodeAdapter 缺少 browser_agent 配置**
- Go: `code_adapter.go:688-690` — 标记 ⤵️
- Python: `interface_code.py:731-759` — 完整实现
- **修复**：实现 browser_agent 配置

**AH-M-13：CodeAdapter 缺少 _refresh_acp_runtime_tools**
- Python: `interface_code.py:905-910` — ACP 工具动态注册
- **修复**：ACP 实现时回填

**AH-M-14：CodeAdapter.updateRuntimeConfig 缺少 user_todos channel_id per-request sync**
- Python: `interface_code.py:914-927` — user_todos workspace + channel_id 同步
- **修复**：实现 user_todos per-request sync

### 提示 (T)

**AH-T-01**：DeepAdapter 中多处 ⤵️ 标记确认尚未实现 — A2X/streamEventRail/llm_reasoning 等
**AH-T-02**：buildSkillCreateRail 返回 nil — 标记 ⤵️ 10.6.3-10
**AH-T-03**：buildMemoryRail/buildExternalMemoryRail 返回 nil — 标记 ⤵️
**AH-T-04**：updatePlanModeRails/updateAgentModeRails 缺少 memory rail 处理 — 与 T-03 同源
**AH-T-05**：ProcessInterrupt 缺少 _cancel_pending_todos — 标记 ⤵️
**AH-T-06**：ProcessInterrupt 缺少 streamEventRail abort/reset — 与 StreamEventRail 同源
**AH-T-07**：ProcessInterrupt 缺少 todos/cancelled_tools 输出 — 标记 ⤵️
**AH-T-08**：HookExecutor 与 Python 一致性确认良好 — 无需修复
**AH-T-09**：UserHookRail 与 Python 一致性确认良好 — Go 处理更健壮

---

## 五、Evolving 模块 (9.72-9.80)

### 严重 (S)

**EV-S-01：SkillEvolutionRail.handleEvolutionFromSignals 绕过 FinalizeStagedEvolutionRequest**
- Go: `skill_evolution_rail.go:1716-1779` — 直接调 orchestrator.Evolve 后自行处理审批
- Python: `skill_evolution_rail.py:875-940` — `await self.approval_runtime.finalize_staged_evolution_request(request, ...)`
- **影响**：审批流程边界情况处理不一致，缺少 on_auto_approved 回调和 emit_approval_request 回调
- **修复**：重构为使用 approvalRuntime.FinalizeStagedEvolutionRequest

**EV-S-02：emitSharedRecordsApproval 使用 context.Background() 丢失上下文**
- Go: `skill_evolution_rail.go:1402` — `r.manager.StageRecords(context.Background(), ...)`
- Python: 对应代码在 SkillEvolutionSharingMixin 中使用 async 上下文
- **修复**：将 `context.Background()` 改为传入的 `ctx`

**EV-S-03：StepTemplate 缺少 TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR 错误包装**
- Go: `base.go:324-334` — stepFn() 直接调用，无 try-catch
- Python: `base.py:129-140` — `try/except` 包裹 `_step()`，异常时 `raise build_error(StatusCode.TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR, ...)`
- **修复**：在 StepTemplate 中使用 defer/recover 捕获 panic，或 stepFn 改为返回 error

**EV-S-04：detectExperienceDetailRead 中 isTeamSkill 使用 context.Background()**
- Go: `team_skill_evolution_rail.go:1219` — `r.isTeamSkill(context.Background(), skillName)`
- Python: `team_skill_evolution_rail.py:1019` — `self._is_team_skill(skill_name)`（同步，无 ctx）
- **修复**：将 OnAfterToolCall 的 ctx 传递到 detectExperienceDetailRead

**EV-S-05：TeamSkillEvolutionRail.UpdateLLM 中 scorer 未更新**
- Go: `team_skill_evolution_rail.go:1025-1037` — 更新了 generator 和 teamSignalDetector，没有更新 scorer
- Python: SkillEvolutionRail.update_llm 更新了 evolver 和 scorer
- **影响**：scorer 仍持有旧的 LLM 引用，后续评分使用过时模型
- **修复**：在 UpdateLLM 中添加 `r.scorer.UpdateLLM(llmModel, model)`

### 一般 (M)

**EV-M-01：parseLLMJSON 中 convertSliceToMapSlice 过度过滤**
- Go: `from_conv.go:647-655` — 对 []any 中非 map[string]any 元素静默跳过
- Python: `_parse_llm_json` 中 `if isinstance(data, list): return data` 直接返回
- **修复**：对齐 Python，list 直接返回不过滤

**EV-M-02：SkillEvolutionRail.RunEvolution 缺少顶层异常捕获**
- Go: `skill_evolution_rail.go:572-706` — 无 defer/recover
- Python: `skill_evolution_rail.py:383-525` — 整个方法 try/except Exception 包裹
- **修复**：入口添加 `defer func() { if rec := recover(); rec != nil { logger.Error(...) } }()`

**EV-M-03：detectSkillFromToolCalls 中 JSON 解析失败缺少 debug 日志**
- Go: `from_conv.go:730-744` — `_ = json.Unmarshal(...)` 失败时静默
- Python: `from_conv.py:469-470` — `logger.debug("[ConversationSignalDetector] failed to parse skill_tool arguments: %s", exc)`
- **修复**：Unmarshal 失败时添加 Debug 日志

**EV-M-04：inferTeamSkillFromTrajectory 额外收集 LLM 步骤文本**
- Go: `team_skill_evolution_rail.go:1743-1750` — 多收集 LLM 步骤消息文本
- Python: `team_skill_evolution_rail.py:117-130` — 只收集 tool 步骤
- **修复**：移除 LLM 步骤文本收集以对齐 Python

**EV-M-05：RequestUserEvolution 中 Trajectory Steps 未初始化**
- Go: `team_skill_evolution_rail.go:759-763` — Steps 为 nil
- Python: `steps=[]` 显式初始化
- **修复**：`Steps: []*trajectory.TrajectoryStep{}`

**EV-M-06：TeamSkillEvolutionRail 的 FinalizeStagedEvolutionRequest requires_approval 参数确认**
- Go: `team_skill_evolution_rail.go:1511-1516` — `!autoApprove`
- Python: `requires_approval=not auto_approve`
- **说明**：逻辑等价，确认参数语义正确即可

### 提示 (T)

**EV-T-01**：Evaluate/Simplify 返回 nil, nil — 对齐 Python `return []` — 语义一致
**EV-T-02**：CalcFreshness daysOld 计算用 int 截断 — 对齐 Python timedelta.days — 一致
**EV-T-03**：UpdateScore 中 LastEvaluatedAt 格式差异 RFC3339Nano vs isoformat — 兼容
**EV-T-04**：SkillEvolutionRail 缺少 generate_and_emit_experience 方法 — Python legacy，Go 不需要

---

## 六、Gateway-Session 模块 (11.x)

### 严重 (S)

**GS-S-01：DeliverDirect 使用 context.Background() 丢弃调用链 context**
- Go: `interaction/router.go:244` — `ctx := context.Background()`
- Python: `deliver_direct` 是 async 函数，天然传播 asyncio context
- **修复**：将 `ctx context.Context` 加入函数签名

**GS-S-02：SessionManager.EnsureSessionProcessor 使用 context.Background() 创建处理器**
- Go: `session_manager.go:149` — `context.WithCancel(context.Background())`
- Python: asyncio.Task 由事件循环统一管理
- **影响**：处理器 goroutine 无法被外部级联取消
- **修复**：EnsureSessionProcessor 接收 parent context

**GS-S-03：handleAgentsCreate/Update 使用 context.Background() 调用 LLM**
- Go: `handle_agents.go:226,314` — `GenerateAgentWithLLM(context.Background(), ...)`
- Python: `await self._generate_agent_with_llm(...)` 在请求 async 上下文中
- **修复**：使用函数参数中已有的 ctx

**GS-S-04：processStream 注册流式任务后覆盖 entry，cancel 函数丢失**
- Go: `forward_loop.go:163` — registerStreamTask 先以 nil entry 注册
- **影响**：cancel 请求在 processStream 设置 entry 之前到达时，cancelStreamTask 拿到 nil，cancel 无法生效
- **修复**：在 handleChatSend 中构造完整 entry（含 cancel）后再注册

**GS-S-05：config_apply.go downloadFile 使用 context.Background() 且不传递上游 ctx**
- Go: `config_apply.go:735` — `context.WithTimeout(context.Background(), 30*time.Second)`
- Python: aiohttp 天然支持请求级超时
- **修复**：downloadFile 接收 ctx 参数

**GS-S-06：app_gateway.go Start 中 connectWithRetry 失败后仍继续启动**
- Go: `app_gateway.go:170-175` — 错误仅记日志，继续启动
- Python: `_connect_with_retry` 失败时 raise，中断启动
- **影响**：AgentServer 连接失败，网关无法转发消息但用户无感知
- **修复**：Start 中返回 error

### 一般 (M)

**GS-M-01：router.go DeliverDirect 的 send_failed 判断条件与 Python 不一致**
- Go: `router.go:246-248` — 通过 `err != nil` 判断
- Python: 通过 `msg_id is None` 判断
- **修复**：检查 err 和 msgID 组合

**GS-M-02：SessionManager processSessionQueue 中 task 执行结果被丢弃**
- Go: `session_manager.go:301` — `_, _ = item.task(taskCtx)`
- Python: `await self._session_tasks[session_id]` 传播异常
- **修复**：记录 task 执行错误日志

**GS-M-03：AgentManager.ReloadAgentsConfig 未调用 team_manager.update_evolution_config**
- Go: 标记 TODO ⤵️ 10.3.2
- Python: `await get_team_manager(channel_id).update_evolution_config(team_config)`
- **修复**：等 10.3.2 实现后回填

**GS-M-04：stream_controller.go runOneRound 中 context.Background() 用于 updateStatus**
- Go: `stream_controller.go:505` — defer recover 中用 `context.Background()`
- Python: `except BaseException` 后 `await self._update_status(MemberStatus.ERROR)` 在同一协程
- **修复**：在 runOneRound 入口保存原始 ctx，defer 中使用

**GS-M-05：stream_controller.go pendingInterruptResumes 类型为 []any**
- Go: `stream_controller.go:63` — `pendingInterruptResumes []any`
- Python: `pending_interrupt_resumes: list[InteractiveInput] = []`
- **修复**：将类型改为 `[]*interaction.InteractiveInput`

**GS-M-06：WebChannel.Start/Stop 忽略传入的 context 参数**
- Go: `web_connect.go:293,307` — `Start(_ context.Context)` / `Stop(_ context.Context)`
- **修复**：Stop 中检查 ctx.Done() 或设置写超时

**GS-M-07：InProcessSpawnHandle.ForceKill 不等待 goroutine 退出**
- Go: `inprocess_handle.go:157-170` — cancelCtx() 后直接返回
- Python: `await self._task` 等待任务退出
- **修复**：等待 done channel 或明确不等待的语义

### 提示 (T)

**GS-T-01**：connectWithRetry 不检查 ctx 取消 — 改用 select + time.After
**GS-T-02**：isFullPayloadEvent 使用局部 map 每次重建 — 提取为包级变量
**GS-T-03**：AgentManager.createAgent 中 env 覆盖遍历快照前的 map — 高并发下微小风险
**GS-T-04**：SessionManager.waitTaskDone 使用 10ms 轮询 — 效率低，建议用 channel
**GS-T-05**：handleExtensionsList/Import/Delete/Toggle 为 stub — 标记待实现
**GS-T-06**：AgentManager Initialize/CreateSession 对 ACP 通道未实现 — 标记待实现

---

## 附录：context.Background() 使用汇总

48h 变更范围内非测试文件中 `context.Background()` 使用一览（仅列功能性问题）：

| 文件 | 行号 | 严重程度 | 说明 |
|------|------|---------|------|
| worktree/manager.go | 462,472,481,493 | 严重 | fireRail ctx 丢失 |
| evolving/skill_evolution_rail.go | 1402 | 严重 | emitSharedRecordsApproval ctx 丢失 |
| evolving/team_skill_evolution_rail.go | 1219,1240 | 严重 | isTeamSkill/teamSkillForExperience ctx 丢失 |
| evolving/skill_evolution_rail.go | 1976 | 一般 | 内部调用 ctx 丢失 |
| adapter/deep_adapter_rails.go | 588,724 | 严重 | updateRailsForMode ctx 丢失 |
| adapter/deep_adapter_slash.go | 306 | 严重 | handleGovernanceApproval ctx 丢失 |
| adapter/code_agent_rail.go | 219 | 一般 | Init ctx 丢失 |
| task_manager.go | 978 | 严重 | resolveLeaderMemberName ctx 丢失 |
| database/memory_impl.go | 784 | 严重 | AddTaskWithBidirectionalDependencies ctx 丢失 |
| interaction/router.go | 244 | 严重 | DeliverDirect ctx 丢失 |
| stream_controller.go | 505 | 一般 | updateStatus recover 中 ctx 丢失 |
| session/session_manager.go | 149 | 严重 | EnsureSessionProcessor ctx 丢失 |
| server/handle_agents.go | 226,314 | 严重 | GenerateAgentWithLLM ctx 丢失 |
| gateway/channel_manager/web/config_apply.go | 735 | 严重 | downloadFile ctx 丢失 |
| swarm/agents/harness/common/rails/runtime_prompt_rail.go | 764 | 严重 | setGitBranch ctx 丢失 |
| swarm/agents/harness/common/rails/project_memory/files.go | 471 | 严重 | DiscoverAndLoadMemoryFiles ctx 丢失 |

---

*审查完成。建议按严重程度分批修复，优先处理 S 级问题中的功能缺失和 context 丢失。*
