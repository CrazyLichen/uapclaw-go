# 27.2 / 27.10 Goal 前端契约全量验收 设计

> 状态：2026-11-09 定稿。范围、分层、口径由用户逐节拍板（A 全量验收 → G4 加真 LLM 冒烟 → §2 以 Python 后端为准 → 整体确认落 spec）。
>
> 上游对齐目标：
> - `openjiuwen/harness/goal/{schema.py, manager.py, store.py, evaluation.py}`、`openjiuwen/harness/schema/interaction.py`、`openjiuwen/harness/rails/task_completion_rail.py`、`openjiuwen/harness/deep_agent.py`（L3731-4574）
> - `jiuwenswarm/server/runtime/agent_adapter/interface_deep.py`（L15402-20030）、`jiuwenswarm/server/agent_ws_server.py`（L3717）
> - 前端契约源：`internal/swarm/channels/web/frontend/src/{services/webClient.ts, hooks/useWebSocket.ts, types/goal.ts, stores/goalStore.ts}`

## 1. 概述

### 1.1 一句话

Goal 功能本体（9.87 交互监督器 / 9.88 域 / 9.89 接线 / 10.3.27 服务面）已全部落地且本地验证通过，但**后端出站契约与前端假定从未端到端对拍过**。本 spec 只在 `tests/integration/swarm/web/` 下补验收用例，把「Web GoalBar 可用」这最后一环验掉；不预设新生产功能，若用例暴露契约不符，按规则 13.2 先报缺口再由用户决策是否修。

### 1.2 在 agent 会话中的流程位置

```
Web GoalBar / InputArea / EditGoalModal
   │  requestGoalAction(get|pause|clear)          sendGoalStreamCommand(set|resume)
   │  webRequest('command.goal', {session_id,    webClient.sendFireAndForget(..., isStream:true)
   │              action, mode})                  │
   └──────────────────┬───────────────────────────┘
                      ▼  WS → WebChannel → Gateway(E2A)
              AgentServer.dispatch
                      ▼
              swarm/runtime  (goal 短路 / plan 互斥 / goal busy 拦截)
                      ▼
              swarm/server/adapter.DeepAdapter
                 ├ dispatchGoalControl → GoalManager.set/pause/resume/clear/get   ★ 域写者
                 ├ attach_output → DeepAgent.AttachOutput（补排 ACTIVE goal work）
                 └ 流式转发 → 出站帧序列
                      ▼
              agentcore/harness  监督器 → RunOneRound(run_kind=goal)
                 ├ TaskCompletionRail：goal_protocol 附件 / <goal_task> query 替换
                 ├ submit_goal_report → GoalReportSink
                 └ GoalEvaluator.assess → GoalManager.apply_assessment
                      ▼  InteractionEvent.goal_updated → ToOutputSchema → 输出流
              前端 applyGoalSnapshot(payload) → goalStore
```

### 1.3 作用（不做的后果）

| 机制 | 解决什么 | 现状 |
|---|---|---|
| G1 契约快照 | 后端 `GoalRecord` 字段/类型与前端 `types/goal.ts` 是否真的一致 | 静态看一致，零用例；`map[string]any` 的 int/float64/nil 漂移无法被 TS 编译期发现 |
| G2 事件顺序 | 前端 `pendingAction` 靠 `goal.snapshot`/`goal.updated`/`execution.error`/`runtime.accepted` 之一清空，否则按钮**无限期置灰** | Go 侧无一条顺序断言 |
| G3 断线补发 | 刷新/切会话后 GoalBar 自愈、1 分钟巡检兜底 | 前端有三级兜底逻辑，后端补排已实现（`deep_agent.go:1693-1698`），但无回归锁 |
| G4 全链 | 目标自动续跑到终态（COMPLETED/BLOCKED）+ 完成卡片幂等落盘 | 现有 10 例均为 MockLLM 单轮/控制面，无一条跑完整状态机 |

### 1.4 机器核对过的计数（落盘前逐条 shell 复核，非心算）

| 项 | 值 | 核对命令 |
|---|---|---|
| 前端 `command.goal` 字面量出现次数 | **16**（分布 9 文件；`useWebSocket.ts` 仅 2 处且均为注释） | `grep -ro "command\.goal" src --include=*.ts --include=*.tsx \| wc -l` |
| 前端**实际发起 RPC** 的函数 | **2**：`webClient.ts:892 requestGoalAction(get/pause/clear)`、`:916 sendGoalStreamCommand(set/resume)` | 逐行 Read |
| 其余 14 处 | 注释 / 类型 / 状态标记 / 文档串 | 逐行 Read |
| `GoalRecord.ToDict()` 顶层字段 | **15**（Go `schema.go:337-359`） | `sed -n '337,359p' \| grep -c '"[a-z_]*":'` → 15 |
| Python `GoalRecord.to_dict()` 顶层字段 | **15**（`schema.py:210-227`） | 同法 → 15，与 Go 逐字一致 |
| 现有 goal 集成用例 | **10 例**（dispatch 2 / control_plane 2 / interaction 2 / lifecycle 3 / abort_guard 1） | `grep -rh "^func (s \*.*) Test" ... \| wc -l` → 10 |
| goal 相关 Go 文件 | **21 个 / 3230 行** | `find ... -iname "*goal*" -name "*.go" \| wc -l` + `cat \| wc -l` |

> 设计讨论中曾口述「17 字段 / 9 例 / 16 处都在 useWebSocket.ts」，均为心算偏差，以本表机器核对值为准。

## 2. 现状盘点与已核实结论

### 2.1 Go 侧已交付（本 spec 不重写）

| 层 | 位置 | 状态 |
|---|---|---|
| 交互监督器 + 输出租约 + 双队列 | `harness/deep_agent.go`（Start/Stop/AttachOutput/DetachOutput/supervisorLoop/ExecuteRound）、`harness/schema/interaction.go`、`harness/task_loop/event_manager.go` | ✅ 9.87 |
| Goal 域（15 字段 record / 唯一写者 / 三策略评估 / Session state 存储） | `harness/goal/{schema,store,manager,evaluation}.go` | ✅ 9.88 |
| 提示词 + 两工具 + rail 生命周期 | `harness/prompts/sections/goal.go`、`prompts/tools/goal.go`、`tools/goal/goal.go`、`rails/task_completion.go` | ✅ 9.89 |
| `command.goal` 五 action / 三守卫 / goal busy / 历史落盘 / 中间 final 降级 | `swarm/runtime/service.go`、`server/runtime/interface.go`、`server/adapter/deep_adapter_goal*.go`、`deep_adapter_interaction.go` | ✅ 10.3.27 |
| 出站事件常量 | `server/adapter/goal_events.go`（`goal.updated` / `execution.error` / `goal.snapshot` / `goal.confirm_required` / `runtime.accepted`） | ✅ |
| 前端产物 | `frontend/src/components/GoalBar/`、`stores/goalStore.ts`、`features/goalMode/`、`types/goal.ts` | ✅ 已随前端迁移入库 |

### 2.2 设计阶段两处疑点，已核实**不是缺口**

1. **`created_at` / `updated_at` 是否该下发**：前端 `types/goal.ts:32` 注释称「后端已确认永久不含」，但 Python `schema.py:225-226` 明确下发，Go `ToDict()` 与 Python 逐字一致。**按用户裁决「对照 python 后端保持一致即可」→ 不改 Go，前端注释视为历史文档滞后**；用例只断言这两字段存在且为 ISO8601 字符串。
2. **G3 R1 的补排是否缺失**：Go `deep_agent.go:1693-1698` 在 `AttachOutput` 的 control lock 内调用 `EnsureActiveGoalWorkLocked` + `notifyWork`，与 `deep_agent.py:4065-4069` 等价；`OutputLeaseManager.Detach`（`schema/interaction.go:443-456`）token 不匹配直接返回 false，不摘新流。**R1/R3 的后端能力均已具备**，用例负责锁住它们。

### 2.3 契约源（前端实际读法）

- 一元响应：`webClient.ts:892-901` 读 `payload.goal ?? payload.record ?? null`；`clear` 直接返回 `null` 不读响应体。
- 流式：`webClient.ts:916-935` `sendFireAndForget(..., {isStream:true})`，**不等 res**；`set` 恒带 `overwrite_confirmed: true`（由 `EditGoalModal` 复用同一路径），因此正常路径不会触发 `goal.confirm_required`。
- 事件落地：`useWebSocket.ts:4673-4710` 五个 `webClient.on(...)` → `applyGoalSnapshot(payload)`（`:3143`）→ `payload.goal ?? null` + `session_id` 三级兜底（顶层 → `goal.session_id` → `activeSessionId`）。
- 顺序硬假设：`:4690-4693`「set/resume 均先于 `runtime.accepted` 下发 `goal.snapshot`，实测 16/16 次 resume 确认」；上游 `interface_deep.py:19887-19892` yield snapshot（`is_complete=false`）在前，`:19907/:19918/:19955` `_yield_runtime_accepted()` 在后。
- 乱序容忍：`:269-270`「`goal.updated` 与 `chat.delta`/`chat.final` 走两条独立通道，后端不保证到达顺序」→ **用例不得锁这两者的相对先后**。

## 3. 范围与分层

**不新增生产功能，不新建生产目录**。原 27.2 写过的 `internal/swarm/channels/web/goal/` 不建（2026-11-05 归属订正已说明后端逻辑全在 server 侧）。产出仅 `tests/integration/swarm/web/` 与 `IMPLEMENTATION_PLAN.md` 状态回填。

| 组 | 文件 | 用例 | 链路 |
|---|---|---|---|
| **G1** | `goal_rpc_contract_test.go` | 8 组 RPC/事件契约对拍 + 15 字段类型断言 | `ServerEnv.Send(ReqMethodCommandGoal)` → AgentServer(E2A) → runtime → adapter → GoalManager |
| **G2** | `goal_event_order_test.go` | O1-O5 顺序与形状 | 同上 + `AwaitStreamEnd` 全帧 |
| **G3** | `goal_reconnect_test.go` | R1-R4 断线/重连/切会话 | 两次 `AttachOutput` + `Send(get)` |
| **G4-Mock** | `goal_unattended_test.go` | 自动续跑到终态全链 | MockLLM 编排多轮确定性响应 |
| **G4-Real** | `goal_realllm_test.go` | 真模型接入后协议面冒烟 | `//go:build integration && llm` |
| — | `doc.go` | 包文档（`package web_test`，规则 1） | — |

**链路选择**：走 `testhelpers.ServerEnv`（`transport.NewChannelTransport` + 进程内 `AgentServer`），**不走** `testhelpers.CallWebRPC`。后者只覆盖 WebChannel 本地 handler，goal 属 `agent` 转发类 RPC，必须经完整 E2A 才碰得到 `runtime/interface.go` 的短路与 adapter 的流。跨 internal 二级子系统：`swarm/server` + `swarm/runtime` + `swarm/server/adapter` + `agentcore/harness` + `agentcore/session`（≥3，满足规则 7 第一层）。

## 4. G1 契约矩阵

| # | 前端 | 请求 | Go 出站帧 | 前端读的字段 | Python 依据 |
|---|---|---|---|---|---|
| 1 | `requestGoalAction('get')`、1 分钟巡检 | `{action:"get", mode}` | 单帧 `goal.snapshot` + `is_complete=true` | `payload.goal ?? payload.record` | `interface_deep.py:19350-19362`；`agent_ws_server.py:3717` |
| 2 | `sendGoalStreamCommand('set')` | `{action:"set", objective, mode, overwrite_confirmed:true, is_stream:true}` | 流式：先 `goal.snapshot`(非终结) → `runtime.accepted` → goal 轮输出 → `goal.updated` | `goal.snapshot` 清 pendingAction | `interface_deep.py:19880-19892`、`:16254` |
| 3 | `sendGoalStreamCommand('resume')` | `{action:"resume", mode, is_stream:true}` | 同 #2 | 同上 | `:16251-16257` |
| 4 | `requestGoalAction('pause')` | `{action:"pause", mode}` 一元 | `goal.snapshot`（`goal`/`cleared_goal`/`message`）+ `is_complete=true` | `payload.goal`；`time_used_seconds`/`active_started_at` 缺失时沿用旧值（`useWebSocket.ts:313`） | `:17929 goal_control` |
| 5 | `requestGoalAction('clear')` | `{action:"clear", mode}` 一元 | `goal.snapshot` 且 `goal:null` | `applyIncomingGoal` 的 `!goal` 分支 | `:17728` |
| 6 | `EditGoalModal` 覆盖 | `{action:"set", overwrite_confirmed:true}` | 正常 `goal.snapshot`；**未确认**路径 `goal.confirm_required` + `existing_goal`/`requested_objective` | `:4673` 起订阅 | `interface_deep.py:19365-19375`；`goal/manager.py:134-140` `already_exists` |
| 7 | `reportFailure` | 未知 action / 未 `start_interaction` | `chat.error` + `code`/`error`/`goal` | message 展示 | `goalErrInvalidAction` / `goalErrManagerNotStarted` |
| 8 | `execution.error` 订阅 | 轮次执行失败 | `execution.error` + `payload.goal` | `applyGoalSnapshot(payload)` | `interaction.py:167-177` |

**`GoalRecord` 15 字段类型断言**（防 Go `map[string]any` 数值/空值漂移）：

```
必填定长：goal_id(string) session_id(string) objective(string)
         status(string ∈ active|paused|completed|blocked)
         revision(number) attempt_count(number)
         token_usage(object: input_tokens/output_tokens/cached_input_tokens/total_tokens 均 number)
         time_used_seconds(number) created_at(string ISO8601) updated_at(string ISO8601)
可空：    max_attempts(number|null) token_budget(number|null)
         last_stop_reason(string|null) active_started_at(string|null)
         last_assessment(object|null)
last_assessment：status(string ∈ continue|complete|blocked) evidence(string)
                remaining_work/next_instruction(string|null)
```

**快照策略**：字段白名单 + 类型断言 + 枚举白名单，**不用 golden JSON 全等**（`goal_id` 为 12 位随机 hex、`created_at`/`updated_at` 为时间戳，全等必假红）。

## 5. G2 事件顺序不变量

| # | 不变量 | 断言 | Python 依据 |
|---|---|---|---|
| O1 | `set`/`resume` 流中 `goal.snapshot` **严格早于** `runtime.accepted` | `indexOf("goal.snapshot") < indexOf("runtime.accepted")`；两者只到其一时按「至少一种能清 pendingAction」验收 | `:19887-19892` 在前，`:19907/:19918/:19955` 在后；前端 `:4690` |
| O2 | 流式 `goal.snapshot` 为 `is_complete=false`；`get`/`pause`/`clear` 的为 `is_complete=true` | 逐帧 `IsComplete` | `:19892` false；Go `interface.go:1515` `WithChunkIsComplete(true)` |
| O3 | `goal.updated` 载荷恒 `{event_type, goal}`，`clear` 后 `goal:null`；对象时 15 字段合法 | 形状 + 类型 + 枚举 | `_interaction_goal_updated_payload` `:17725-17737`；Go `schema/interaction.go:292-296` |
| O4 | goal attempt 边界的 `chat.final` 降级为 `chat.delta + goal_intermediate=true`；终结帧唯一 | 全帧 `is_complete=true` 恰 1 个；`goal_intermediate` 出现次数 = 轮切换数 | `interface_deep.py:15619-15635`；Go `deep_adapter_interaction.go:511-535` |
| O5 | `execution.error` 带 `goal`；`runtime.accepted` 可兜底清 pendingAction | 构造失败路径断言帧含 `goal`；只发 accepted 的路径断言帧存在 | `interaction.py:167-177`；前端 `:4705`/`:4681` |

**明确不锁**：`goal.updated` 与 `chat.delta`/`chat.final` 的相对顺序（前端 `:269` 与上游均无保证），只断言两者最终都到达且形状合法。

## 6. G3 断线/重连不变量

| # | 场景 | 断言 | Python 依据 |
|---|---|---|---|
| R1 | 旧租约 detach → 新 `AttachOutput` | 新流拿到当前 ACTIVE goal 的快照/更新，非 nil | `deep_agent.py:4048-4070`；Go `deep_agent.go:1693-1698` |
| R2 | 刷新页面等价：`command.goal get` | 单帧 `goal.snapshot`、`goal` 反映持久 record、status 与之前一致；连发两次幂等无副作用 | `agent_ws_server.py:3717-3718` 只读不建 metadata |
| R3 | 旧 token 的 `Detach` 不摘新流 | 新租约 `HasConsumer()==true`、后续帧可达 | `interaction.py` `OutputLeaseManager.detach`；Go `schema/interaction.go:443-456` |
| R4 | 会话切换不串 goal | 会话 A 的 ACTIVE goal 不出现在会话 B 的 `get` | `deep_agent.py:3941-3947`；`GoalManager.get_store(session_id)` |

## 7. G4 无人值守全链

### 7.1 MockLLM 全链（默认门禁内）

编排多轮确定性响应（模型调 `submit_goal_report` → rail 消费 → assess → `apply_assessment` → `push_goal` → 下一轮）：

1. `attempt_count` 逐轮递增，终态为 `completed` 或 `blocked` 之一；
2. 每轮 user query 是重建的 `<goal_task>` XML（含 `objective`/`previous_assessment`/`current_instruction`/`budget_notice`），不复用上一轮；
3. 终态后不再 push 新 goal work（EventManager 无 pending goal work）；
4. `goal.updated(status=completed)` 到达，完成卡片历史落盘**幂等**（重复事件不重复落盘）；
5. `max_attempts=1` 硬刹车 → BLOCKED，`last_stop_reason` 合法。

### 7.2 真 LLM 冒烟（`integration && llm`，不进默认门禁）

- **双守卫**：文件级 `//go:build integration && llm` + 用例开头 `os.Getenv("LLM_API_KEY")=="" → t.Skip`；沿用 `RealLLMSuite` 环境变量约定与 `make test-llm`（`-tags "sqlite_fts5 test integration llm"`）。
- **ctx 处理**：继承 `RealLLMSuite` 的 `s.Ctx = context.Background()`（避免 `BaseIntegrationSuite` 5 分钟套件 deadline 封顶，CI 首跑 `40856190636` 实证），单用例另加 `context.WithTimeout(180s)` + `select` 有界等待。
- **硬断言（仅确定性契约）**：`set` 被受理且 `goal.snapshot` 帧形状合法；若 `runtime.accepted` 也到达则 O1 顺序成立；若出现 `goal.updated` 则 `goal` 为 15 字段合法结构且 `status` 属 4 值枚举；流最终关闭不 hang。
- **不硬断言**：模型是否调 `submit_goal_report`、是否跑到 COMPLETED、attempt 轮数、transcript assessor 是否被调用、完成卡片是否落盘（依据既有反馈：同一提示词两轮可完全不走团队流程，模型行为不可断言）。
- **软失败**：429 / 超时 / 未观察到终态 → 记日志 + `t.Skip`，不 FAIL，不占 CI 配额。

## 8. 测试策略与验收口径

**隔离**：每个 suite `SetupTest` 设 `UAPCLAW_DATA_DIR=t.TempDir()` + `pathutil.ResetCache()` + `session.ClearAllSessionMetadataCache()`，`TearDownTest` 刷盘还原（照 `tests/integration/swarm/server/goal_dispatch_test.go` 先例），防会话/历史/租约跨用例串味。

**规则 7 自检**：写完跑 `go run ./cmd/integration-audit -verify`；每文件 `//go:build integration`（G4-Real 为 `integration && llm`）、`suite.Run(`、目录 distinct internal import ≥2、有效测试方法 ≥1 且每条 ≥2 断言。

**契约不符的处理（规则 13.2）**：先报给用户，给出① Python 文件与行号、② 影响面（哪个前端分支会失效）、③ 是否属本批、④ 三档建议（本批一起修 / 挂 ⤵️ 到指定章节 / 不修并说明理由），**用户选完才动生产代码**；禁止默默改、禁止用 `// TODO` 代替决策、禁止写进 spec 就当已处理。

**已知偏差与处理**：
- `created_at`/`updated_at` 前端注释与后端实现不符 → 以 Python 为准，不改代码，仅断言（§2.2）。
- `goal.snapshot` 帧无顶层 `session_id` → 与 Python 一致，前端靠 `goal.session_id` 三级兜底，用例覆盖 `goal:null` 时走 activeSessionId 的路径。
- `goal.confirm_required` 在前端正常路径不可达（set 恒带 `overwrite_confirmed:true`），G1 #6 用**后端直接构造未确认请求**覆盖该契约，不依赖前端 UI。

**完成标准（规则 6 + 11，本地为准）**：
```
export GOPROXY=https://goproxy.cn,direct
go build ./...
go vet ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -race ./tests/integration/swarm/web/
go run ./cmd/integration-audit -verify
```
推送远端即交付；不主动追 CI 结果（用户 2026-11-06 明确要求）。汇报时给本地命令 + 结论，不给 CI 结论。

**计划回填**：27.2 / 27.10 由 🔄/☐ 改 ✅ 并写实测证据（用例数、契约矩阵 8 组、是否触发生产修复）；9-G「距离 Goal 完整使用的剩余工作」表第 1 项划掉、第 5 项标完成；若出现新缺口，按规则 13.2 在计划中新建 ⤵️ 行并注明回收条件。

## 9. 删除清单

无。本批不删除任何文件、符号、注释或测试桩；不动现有 10 例 goal 集成用例（它们是本批的对照组与基线）。

## 10. 已知偏差登记

| 编号 | Python 符号 / 位置 | Go 等价 | 偏差性质 | 理由 | 回收条件 |
|---|---|---|---|---|---|
| D-1 | `types/goal.ts:32`（前端文档注释，非 Python） | `goal/schema.go:356-357` | 前端文档滞后，后端刻意保留 | Python `schema.py:225-226` 明确下发 `created_at`/`updated_at`，Go 逐字对齐；用户裁决「对照 python 后端保持一致即可」 | 若上游 Python 移除这两字段，同步移除并回写前端注释 |
| D-2 | `interface_deep.py:19362` `goal_control` 单帧 | `runtime/interface.go:1511-1519` | Go 帧多带 `message` 字段 | 属超集，`requestGoalAction` 只读 `goal`/`record`，不破坏契约 | 无（超集无害） |
| D-3 | G4-Real 真 LLM 用例 | 无上游对应（Go 侧验收增强） | Go 侧新增，Python 无此测试 | 用户明确要求加冒烟；仅断言协议与状态机保证 | 无 |
