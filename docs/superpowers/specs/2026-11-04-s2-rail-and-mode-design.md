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

### 7.1 集成测试（验收主体）

目录：`tests/integration/swarm/server/adapter/attachment_mode/`（沿用 `testhelpers.NewAdapterTestEnv` + `mockllm`）。
每例均为**消息传递级模拟**：构造 `chat.send` 请求 → `DeepAdapter/CodeAdapter` → rails → MockLLM 记录**模型实际收到的窗口**。

| 编号 | 场景与模拟方式 | 跨子系统 | 关键断言 |
| --- | --- | --- | --- |
| IT-S2-1 | 新串正向：请求 `mode=agent.work.plan`，MockLLM 返回纯文本 | gateway 常量 + adapter + rails + context_engine | 历史出现 plan 附件（`mode=snapshot`）；模型窗口的 system 里**无** `MODE_INSTRUCTIONS` 节；`state.plan_mode.mode == "plan"` |
| IT-S2-2 | 旧串失效：请求 `mode=agent.plan` | 同上 | 归一到 normal（`state.plan_mode.mode == "normal"`），不产生 plan 附件，请求不报错 |
| IT-S2-3 | 显式进入：`mode=agent.work.plan` + `plan_entry_source=plan_toggle`，且预置该会话在 `exitedSessions` | runtime + adapter + session | 进入 plan；`exitedSessions` 被清；**发给模型的 query 以 `<system-reminder>` 开头**，原 query 存进 `PLAN_REMINDER_ORIGINAL_QUERY_KEY` |
| IT-S2-4 | 非显式 + 退出记忆：预置 `exitedSessions`，`mode=plan` 且无 `plan_entry_source` | runtime + adapter | 阻断：state 保持 normal；控制事件含 `PLAN_MODE_EXITED`；本轮模型窗口无 plan 附件 |
| IT-S2-5 | `plan_slug` 残留阻断：预置 `plan_slug` 且 state=normal，`mode=plan` 无 entry source | runtime + session + adapter | 阻断并**清空 slug**、强制 normal、发 `PLAN_MODE_EXITED` |
| IT-S2-6 | 退出后不被拉回：①显式进 plan ②`mode=agent.work.normal` 一轮 ③`mode=plan` 无 entry source | runtime + adapter + context_engine | ②发退出事件且出现"移除"delta；③仍为 normal |
| IT-S2-7 | 跳过矩阵：team 请求 / interrupt 续答 / 非 chat 方法各一轮 | runtime + adapter | 三者 state 均未被改写、无控制事件 |
| IT-S2-8 | rail 幂等：同一 agent 连续 3 次 plan 请求，再切 normal，再切 plan | adapter + rails | plan rail 实例数恒为 1；normal 期间 plan 专属 rail 不在集合里；二次切 plan 不重复挂 |
| IT-S2-9 | plan 指引投递：plan 模式一轮 + 退出后一轮 | rails(agent_mode) + context_engine | 进入轮：附件 `source=agent_core.agent_mode_rail`、system 无该节；退出轮：出现 delta 移除 `mode_instructions` |
| IT-S2-10 | todo 默认不注入：chat 一轮 | rails(task_planning) + context_engine | system prompt 无 todo 节；工具列表仍含 `todo_create/list/get/modify` |
| IT-S2-11 | code 提醒节奏：code 模式 12 轮纯文本对话（不调用 todo 工具），第 13 轮模拟一次 `todo_create` 调用 | CodeAdapter + rails + context_engine | 第 11 轮出现 `task_reminder` 附件（`kind=todo_reminder`）；第 12 轮不新增（内容未变）；调用工具后计数重置，之后需再满 10 轮才提醒 |
| IT-S2-12 | 其他消费方：分别跑 task_completion（goal 激活）/ progressive / skills / memory / context_assemble 各一轮 | 5 组 rails + context_engine | 各自附件出现且 `source`/`kind` 与表 §5.5 一致；system prompt 中对应节消失 |
| IT-S2-13 | 跨会话占位：plan 会话中调用 `SessionMessageRequiresQueue` | runtime | 返回 false（透传）并打 Warn；断言"未实现"不静默假装已排队 |

### 7.2 单元测试（分支级补充）

| 模块 | 用例 |
| --- | --- |
| `runtime` controller | 7 道守卫逐条（team/非 single/非 chat/interrupt/MayHold/IsExplicitEntry/plan_slug）、`ResetSession`、`ShouldSync` |
| `runtime/request` | `ResolveRequestRuntimeMode` 对三段/两段/裸串的归一 |
| `rails/task_planning` | `InjectPrompt=false` 不产生节；`true` 仍可注入（开关可逆） |
| `code/rails` | 提醒状态机：10/10 节奏、内容不变不重复、LRU 淘汰、`OnUserMessage` 恢复顺序 |
| 模式串 | slash 解析、`channel_state`、memory 映射、cron 默认值全部新串 |

## 8. 验收

1. `go build ./...`、受影响包 `-race` 单测全绿
2. **13 例集成测试**（IT-S2-1~13，消息传递级）全绿，`integration-audit -verify` 通过
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
