# S2：Rail 生命周期与模式合并设计

> 上游基线：agent-core `5a35c39f` / jiuwenswarm `a2abceccf`
> 前置：S1 `docs/superpowers/specs/2026-11-04-prompt-attachment-foundation-design.md`（已完成）
> 后续：S3 `docs/superpowers/specs/2026-11-04-s3-contract-design.md`
> 关联缺陷：`docs/sync/defect-plan-2026-11-04.md` A 组（模式/rail 装配）、H-11、K-08

## 1. 背景与目标

S1 已把"动态提示词投递"的地基建好（manager + 窗口投影 + 两处 Sync），但**没有任何消费方**。同时我们与上游存在三类结构性差异：

1. rail 集合按 plan/fast 子模式分叉，上游已与子模式解耦
2. plan 状态每请求直写，没有退出记忆、计划文件阻断、结束事件
3. todo 体系仍是"常驻提示词"，上游已改为"工具描述 + 按需提醒"

目标：把 rail 生命周期、模式串、plan 状态机、todo 体系一次性对齐上游，并把 S1 的 attachment 通道接上真实消费方。

## 2. 范围

范围内：

- 模式串三段化（`agent.work.plan` / `agent.work.normal`）
- `PlanModeController`（含退出记忆、计划文件阻断、结束事件、激活提醒）
- rail 装配幂等化（`UpdateAgentRails`），删除 plan/fast 分叉
- todo：`InjectPrompt` 开关默认 false、`CodeTaskPlanningRail`、code 版工具文案
- attachment 消费方迁移（逐 rail 清单见 §5.5）

范围外：

- **跨会话消息排队**（Go 无 cross-session 消息链路，按 §5.2 注释保留步骤）
- 前端代码（用户后续自行迁移；本次不做任何兼容层）
- 轨迹/观测识别（S3）

## 3. 决策记录（本轮已拍板）

| # | 决策 |
| --- | --- |
| D1 | 模式串**全量切换**三段式，不保留旧枚举/别名/双写分支 |
| D2 | **不做前后端兼容层**，前端后续自行迁移；后端内部与出站统一新串 |
| D3 | `TaskPlanningRail.InjectPrompt` 是**开关，默认 false** |
| D4 | todo 工具名保持 `todo_create/list/get/modify`（与上游一致，不改名）；code 版只换 description 与 input schema |
| D5 | `plan_entry_source` **直接复刻 Python 解析**（`slash_command` / `plan_toggle`），不做后端推断 |
| D6 | 进入 plan 的激活提醒**复刻改 query**（原 query 存 `PLAN_REMINDER_ORIGINAL_QUERY_KEY`） |
| D7 | 附件契约常量**留在 `harness/prompts`**（与 Python 一致），不迁 schema |
| D8 | **函数名对齐 Python**（snake_case → CamelCase，语义不变），见 §6 |
| D9 | 因方案变更而不需要的旧代码**一律删除**，见 §5.6 |

## 4. 上游证据

| 主题 | 位置 |
| --- | --- |
| 模式合并 | `2826a9d23`、`ea105309`（plan/fast 合并，`_update_rails_for_mode` 统一） |
| plan 状态机 | `jiuwenswarm/runtime/plan.py:1-284`（`ensure_state` / `check_post_process_exit` / `reset_session` / 守卫） |
| 调用点 | `jiuwenswarm/runtime/service.py:1861`（执行前）、`:1911`（执行后）、`:3071`（删除会话） |
| rail 补挂 | `interface_deep.py:3290-3320`（`_update_agent_rails` 幂等补挂） |
| plan 指引投递 | `agent_mode_rail.py:406-420`（`AddFromPromptSection`，无 manager 时回落 section） |
| todo 关闭提示词 | `interface_deep.py:10756`（`TaskPlanningRail(inject_prompt=False)`） |
| code todo | `jiuwenswarm/agents/harness/code/rails/code_task_planning_rail.py`（跳过静态节 + 10/10 提醒） |
| code 工具文案 | `jiuwenswarm/agents/harness/code/tools/code_todo_tools.py:25-38`（`CODE_TODO_TOOL_PROMPTS`） |
| 其他 attachment 消费方 | `progressive_tool_rail.py`、`task_completion_rail.py`、`skills/skill_use_rail.py`、`context_engineer/context_assemble_rail.py`、`memory/{jiuwen,coding,external}_memory_rail.py` |

## 5. 设计

### 5.1 模式串三段化

`internal/swarm/schema/mode.go`：

```go
// ModeAgentWorkPlan agent-work 规划模式
ModeAgentWorkPlan Mode = "agent.work.plan"
// ModeAgentWorkNormal agent-work 常态模式
ModeAgentWorkNormal Mode = "agent.work.normal"
```

删除 `ModeAgentPlan`、`ModeAgentFast`。全量替换引用点：

| 文件 | 处理 |
| --- | --- |
| `swarm/gateway/message_handler/command_parser/slash_command.go` | `/plan`、`/fast` 解析结果改新串 |
| `swarm/gateway/message_handler/channel_state.go` | `ChannelMode*` 常量改新串 |
| `swarm/agents/harness/common/memory/config.go` | `modes.*` 映射键改新串（两段键同时删除） |
| `swarm/agents/harness/common/rails/runtime_prompt_rail.go` | 展示表改新串 |
| `swarm/agents/harness/common/tools/cron/cron_runtime.go` | 默认回落改 `agent.work.normal` |
| 单测 | 所有断言字面量同步改，**不保留旧值断言** |

### 5.2 `PlanModeController`

新增包 `internal/swarm/runtime/`（对齐上游路径）：

```
internal/swarm/runtime/
├── doc.go         # 包文档
├── plan.go        # PlanModeController
└── request.go     # ResolveRequestRuntimeMode / 常量
```

函数名与上游一一对应（见 §6）。核心流程：

```
EnsureState(request, mode, subMode, agent) (PlanStateResult, error)
  1. ShouldSync(request)?           否 → 直接返回
  2. LockFor(sessionID)             会话级串行
  3. OpenStateSession(agent, sid)   取或建 live 会话（首轮竞态处理）
  4. target = plan|normal；previous = state.plan_mode.mode
  5. previous != target 时：
       target=plan：
         a. IsExplicitEntry(request)          → 清退出记忆，放行
         b. sid ∈ exitedSessions              → 阻断
         c. state.plan_mode.plan_slug != ""   → 阻断（清 slug + 强制 normal + 发事件）
         放行后：switch_mode(plan) → 清 slug → 记入 active → InjectActivationReminder(request)
       target=normal：
         switch_mode(normal) → 移出 active → 发 PLAN_MODE_EXITED
       commit + 日志
CheckPostProcessExit(request, agent) ([]ControlEvent, error)
  工具已把状态切回 normal → 记入 exitedSessions + 移出 active + 发 PLAN_MODE_EXITED
ResetSession(sid)   删除会话时清两条记忆
```

守卫矩阵（逐条对齐上游）：

| 守卫 | 行为 |
| --- | --- |
| team 请求 / 非 `*-single` | 跳过 |
| `ShouldSync` 只认 chat 类 req_method | 跳过 |
| interrupt 续答 payload | 跳过 |
| `MayHoldState` 为假 | 不动 state |
| `IsExplicitEntry` | 允许进入并清退出记忆 |
| `exitedSessions` 命中 | 阻断 |
| `plan_slug` 非空 | 阻断并清 slug |

**跨会话消息排队（按用户要求以注释保留步骤，不实现）**：

```go
// SessionMessageRequiresQueue 判断会话是否要求跨会话消息排队。
//
// 上游 runtime/plan.py:252-261 的步骤（Go 侧尚无 cross-session 消息链路，暂不实现）：
//  1. 读取该会话 plan 状态：state.plan_mode.mode == "plan" → is_plan = true
//  2. 读取该会话 goal 状态：goal 活跃 → is_goal_active = true
//  3. is_plan || is_goal_active → 跨会话消息必须排队，不得打断当前计划
//  4. 否则直接透传（不需要排队）
// TODO(跨会话消息链路): runtime 的 session_message 通道落地后按上述 4 步复刻。
```

调用点替换：`deep_adapter_rails.go:980-988` 的 `updatePlanModeRails + switchModeForState` 两行，改为
`planController.EnsureState(...)`；执行结束后追加 `CheckPostProcessExit`；会话删除处接 `ResetSession`。

### 5.3 Rail 装配统一

```go
// UpdateAgentRails 幂等补挂 agent 模式 rail：目标集合算出来，缺的补、已有的不动。
// 对齐上游 interface_deep.py:3290-3320 `_update_agent_rails`。
func (d *DeepAdapter) UpdateAgentRails(ctx context.Context, sessionID, mode string)
```

- 删除 `updatePlanModeRails`（上游已无此函数）
- 删除 `switchModeForState`（状态写入归 controller）
- `updateRailsForMode` 只保留"装配 rail + 提示词模式"职责

`AgentModeRail` 对齐上游 `agent_mode_rail.py`：

- 不再 `RemoveSection(MODE_INSTRUCTIONS)`：plan 指引改 `AddFromPromptSection`（kind=runtime、source=`agent_core.agent_mode_rail`）
- 无 manager 时回落到 section（与上游一致）
- 退出 plan 时 `ClearSection(MODE_INSTRUCTIONS)`

### 5.4 todo 体系

`TaskPlanningRail`：

```go
// TaskPlanningRailOptions 装配选项
type TaskPlanningRailOptions struct {
    // InjectPrompt 是否注入常驻 todo 提示节（默认 false，对齐上游）
    InjectPrompt bool
}
```

- 默认 false：只注册工具、按任务切模型，不再往 system prompt 写 todo 节
- `BuildTodoSection` 及其单测**删除**（上游 code 模式也不用静态节）

`CodeTaskPlanningRail`（新包 `internal/swarm/agents/harness/code/rails/`）：

- 继承 `TaskPlanningRail`，强制 `InjectPrompt=false`
- `OnUserMessage`：先 `RestoreTaskReminderAttachment`（历史同步之前恢复状态）
- `BeforeModelCall`：连续 `taskReminderTurnsSinceManagement=10` 轮未管理 todo → 追加 `task_reminder` 附件（kind=todo_reminder），两次提醒间隔 ≥ `taskReminderTurnsBetweenReminders=10`；提醒内容不变时不产生 delta
- 状态：`map[sessionID]*reminderState` + LRU 上限 `maxTrackedTaskReminderSessions=1000`

code 工具文案：新增 `internal/swarm/agents/harness/code/tools/code_todo_tools.go`，`todo_create/list/get/modify` **同名**，description 与 input schema 取自上游 `CODE_TODO_TOOL_PROMPTS`。

### 5.5 动态提示词消费方迁移

| Rail（Go 路径） | 上游是否用 attachment | 本次动作 |
| --- | --- | --- |
| `rails/agent_mode.go` | 是 | 迁 `AddFromPromptSection`（kind=runtime） |
| `rails/task_completion.go` | 是 | 迁（kind=runtime，goal 协议节） |
| `rails/progressive.go` | 是 | 迁（kind=tool，工具披露规则） |
| `rails/skills/skill_use_rail.go` | 是 | 迁（kind=skill） |
| `rails/context_engineer/context_assemble_rail.go` | 是 | 迁（kind=runtime） |
| `rails/memory/{memory,coding_memory,external_memory}_rail.go` | 是 | 迁（kind=memory） |
| `rails/task_planning.go` | 否（只注册工具） | 保持 section 注入关闭，见 §5.4 |
| `rails/subagent/{subagent,verification,verification_contract}_rail.go` | 否（子代理一次性提示） | **保持 section**，不动 |
| `rails/heartbeat.go`、`rails/security/prompt_security_rail.go`、`rails/context_engineer/context_processor_rail.go` | 否 | 保持 section |
| `common/rails/{runtime_prompt,response_prompt}_rail.go`、`team/workspace_report_path_rail.go` | 否 | 保持 section |

每个迁移点统一：`source` 字符串与上游逐字一致（`agent_core.<模块名>`），无 manager 时回落 section 并打 Warn。

### 5.6 清理清单（随本 spec 删除）

| 删除项 | 原因 |
| --- | --- |
| `ModeAgentPlan`、`ModeAgentFast` 及所有两段串字面量 | 模式三段化 |
| `deep_adapter_rails.go`：`updatePlanModeRails`、`switchModeForState` | 归 `PlanModeController` / `UpdateAgentRails` |
| `AgentModeRail` 里 `RemoveSection(MODE_INSTRUCTIONS)` 与 plan/fast 分叉分支 | 上游已移除 |
| `TaskPlanningRail.BuildTodoSection` + 单测 | 上游不再注入静态节 |
| `runtime_prompt_rail` 旧展示键、`cron` 的 `agent.fast` 默认 | 模式串变更 |
| `prompts` 包中未被任何消费方使用的 `WithAttachmentContentPath`/`ContentPath` 字段 | S1 遗留，无调用方 |

## 6. 命名对齐规则（函数名对齐 Python）

| Python | Go | 说明 |
| --- | --- | --- |
| `PlanModeController.ensure_state` | `(*PlanModeController).EnsureState` | |
| `check_post_process_exit` | `CheckPostProcessExit` | |
| `reset_session` | `ResetSession` | |
| `should_sync` | `ShouldSync` | |
| `is_explicit_entry` | `IsExplicitEntry` | |
| `lock_for` | `LockFor` | |
| `may_hold_state` | `MayHoldState` | |
| `open_state_session` | `OpenStateSession` | |
| `inject_activation_reminder` | `InjectActivationReminder` | |
| `_exit_payload` | `exitPayload` | 非导出保持小写，语义一致 |
| `resolve_request_runtime_mode` | `ResolveRequestRuntimeMode` | |
| `_update_agent_rails` | `UpdateAgentRails` | 严格对齐（幂等语义在注释里说明） |
| `_is_prompt_attachment_user` | `IsPromptAttachmentUser` | 跨包使用，导出 |

## 7. 测试

单测（`internal/swarm/runtime/`）：

| 用例 | 断言 |
| --- | --- |
| 进入 plan（非显式） | state 变 plan、slug 清空、active 记录、query 被前置提醒且原 query 已存 |
| 退出 plan | state 变 normal、发 `PLAN_MODE_EXITED`、记入 exited |
| 退出后普通消息 | 不被拉回 plan、无事件 |
| 显式进入 | 清 exited 记忆、允许进入 |
| `plan_slug` 残留 | 阻断、清 slug、强制 normal、发事件 |
| team / 非 single / interrupt / 非 chat 方法 | 一律不写 state |
| `ResetSession` | 两条记忆清空 |

单测（rails）：todo 默认不注入节、code 提醒节奏 10/10、提醒内容不变不产生 delta、LRU 淘汰。
单测（模式串）：slash 解析、channel_state、memory 映射、cron 默认值全部用新串。

集成（`tests/integration/swarm/` 或 `agentcore/` 新目录，≥3 子系统）：

| 编号 | 用例 | 跨子系统 |
| --- | --- | --- |
| IT-S2-1 | 一轮进入 plan → 模型窗口含 plan 附件、系统提示无 MODE_INSTRUCTIONS 节 | adapter + rails + context_engine |
| IT-S2-2 | 退出 plan 后下一条普通消息不回 plan，历史里出现移除 delta | adapter + runtime + context_engine |
| IT-S2-3 | 残留计划文件阻断：不进入 plan、发事件、slug 被清 | runtime + session + adapter |
| IT-S2-4 | code 模式：卸载 TaskPlanningRail、挂 CodeTaskPlanningRail，10 轮后出现 task_reminder | rails + context_engine + session |
| IT-S2-5 | 模式串端到端：`agent.work.plan` 请求走 plan 分支，旧串不再被识别 | gateway + adapter + runtime |

## 8. 验收

1. `go build ./...`、受影响包 `-race` 单测全绿
2. 5 例集成测试全绿，`integration-audit -verify` 通过
3. 改动行覆盖率 ≥85%（CI 门禁）
4. CI 四 job 全绿
5. 全仓 `grep -r "agent.plan\|agent.fast"` 仅剩测试中"旧串不被识别"的负向断言

## 9. 风险

| 风险 | 缓解 |
| --- | --- |
| 关闭 todo 提示词后模型不使用 todo 工具 | 先核验工具描述文案与上游同源；IT-S2-4 断言工具仍可见、提醒按时到达 |
| `UpdateAgentRails` 幂等补挂改动影响其他模式 | 单测覆盖三种 mode 的目标集合；集成跑既有 team/code 用例回归 |
| 模式串全量替换漏改导致模式错乱 | 全仓 grep + 负向断言用例（旧串必须失效） |
| `PlanModeController` 会话锁与既有 session 锁嵌套 | `LockFor` 只包 state 读写，不跨 `switch_mode` 与文件 IO；单测用 -race |
