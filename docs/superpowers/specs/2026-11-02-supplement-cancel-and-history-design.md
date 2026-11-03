# supplement 打断链路与 history 记录修复设计

## 1. 背景

界面使用中出现两类可复现问题：

| # | 现象 | 触发 |
|---|---|---|
| 1 | 模型输出中再发消息，新一轮跑了几轮后失败：`LLM stream 失败: … context canceled`，界面出现红色错误 | 前端 `InputArea` 在处理中发送 → `chat.interrupt{intent=supplement}` |
| 2 | 恢复历史会话只看到 user 消息与 `chat.final`；工具调用显示 `unknown` 且只剩一条，工具结果不显示 | `history.json` 记录缺字段 |

日志证据：

```
[INFO ] context messages cleared cleared_count=28   ← 清了一次
[INFO ] context messages cleared cleared_count=0    ← 又清一次（重复）
[ERROR] Agent invoke error error="迭代 6 …: LLM stream 失败: …: context canceled"
```

## 2. 缺陷清单

编号在测试用例中直接引用，用于回归追溯。

| 编号 | 缺陷 | 位置 | Python 参考 |
|---|---|---|---|
| D1 | 流式收尾把「清理登记」写成「再取消一次」，会杀掉窗口内新注册的下一轮 | `handle_envelope.go:378`、`agent_server.go:371-391` | `agent_ws_server.py:1259-1261`（仅 pop 自身，从不 cancel） |
| D2 | ctx 取消被当普通错误上报：写 `result_type=error` → `chat.error` 帧 + `chat.error` 落 history | `react_invoke.go:560-570`、`uapclaw.go:331-333` | `react_agent.py:1549-1554`（`except Exception` 抓不到 `CancelledError`） |
| D2b | 流式中途取消被吞成"正常结束"，半截回答被当作最终答复 | `openai/client.go:336-349` → `react_model_call.go:292-307` | Python 在 await 点抛 `CancelledError`，不产出 answer |
| D3a | `ClearContextMessages` 执行两次（28 → 0） | `react_invoke.go:63-68` 与 `:445-451` | `react_agent.py:1435-1441` 只清一次 |
| D3b | ~~`ClearMessages(withHistory=false)` 清空整个 buffer~~ **实施阶段更正：不成立**。`PopBack` 内部 `getBackInternal` 在 `withHistory=false` 时已把 size 夹到「本轮长度」，行为与 Python 等价；真实问题只是日志把 `totalSize` 当 `cleared_count`（历史 20 + 本轮 8 会打印 28），误导排查 | `session_model_context.go:250-282`（仅日志）、`message_buffer.go:311-352`（夹取） | `message_buffer.py:44-53`、`context.py:297-300` |
| D3c | 取消清理与新一轮 `AddMessages` 无同步（已知限制，本次不修） | 同上 + `engine.go:96-113` | Python 单事件循环天然有序 |
| D4 | 取消判定用 `==`/错误链，被 wrap 后失效 | `uapclaw.go:318`、`react_invoke.go:66/447` | — |
| D5 | history 仅对 `team.message` 构造 extra，工具/usage/file 字段全丢 | `uapclaw.go:368-383` | `interface.py:1063/1102` 除 `event_type`/`content` 外全收 |
| D6 | `extractChunkContent` 缺 `payload["error"]` 兜底 | `uapclaw.go:924-931` | `interface.py:1085` `content or error or ""` |
| D7 | 流式 user 记录 `channel_metadata` 传 nil | `uapclaw.go:284-286` | `interface.py:920` 传 `request.metadata` |

### 2.1 D1 的因果链（为什么"先 cancel 再发新消息"会失败）

```
T1 handleCancel: cancelStreamTask(sid) → cancelA() + delete      → 立即回 resp（不等 A 退出）
T2 新一轮 B 注册: map[sid] = cancelB                              ← resp 之后网关入队才发生
T3 旧轮 A 收尾: defer cancelStreamTask(sid) → 取到的已是 cancelB → B 被杀
```

改动前 `sessionStreamTasks` 是「sessionID → 裸 cancel 函数」的**单槽**，收尾动作对这个槽调用 `cancel()`，而不是"移除自己的登记"。因此 A 的收尾会命中窗口内注册的 B。

`cancelStreamTask` 只取消**这一轮流式请求的 `streamCtx`**（`context.WithCancel(ctx)` 的产物），影响面为：LLM HTTP 请求中止、`runKeepalive` 退出、尊重 ctx 的工具中止；不影响 session、history.json、传输通道。

## 3. 设计决策

| 编号 | 决策 | 被否方案与理由 |
|---|---|---|
| D1 | 单槽 + **entry 指针身份**：`register` 返回 `*streamTaskEntry`，`release` 仅在 `map[sid] == entry` 时 delete，**永不 cancel**；`cancelStreamTask` 仅由 `handleCancel` 调用 | ① 按 requestID 重建映射：会把"取消"语义改成"停该 session 全部流"，与 Python 有行为差异；② 加"停稳确认"（等旧流退出再回 resp）：能顺带消除 D3c，但每次打断多一次同步等待，本次不引入 |
| D2 | 以 `ctx.Err()` 为主判据静默取消；**只静默 `context.Canceled`**，`DeadlineExceeded` 仍上报 | 用 `errors.Is(err, …)` 单独判定：`openai.WrapError` 用 `%v` 断链，且 agentcore 有 64 处 `%v` 包装点，不可靠 |
| D2b | `callLLMStream` 循环结束后判 ctx，取消则返回 error | 改 `Stream` 签名加 error 通道：需改 4 个 client 实现与所有调用方，回归面过大 |
| D3a | 删除 `Invoke` 内的清理，只保留 `invokeImpl` 内的一次 | — |
| D3b | **不改语义**（原判断有误）：只把日志 `cleared_count` 改为实际弹出条数；另加 2 例行为锁定测试守住「只清本轮」 | 原计划改 `ClearMessages` 实现：实测 Go 的 `getBackInternal` 在 `withHistory=false` 时已夹取 size，改实现属于对不存在缺陷的改动，风险大于收益 |
| D4 | 三处判定收敛到 `utils.Canceled(ctx)` | — |
| D5 | 记录构造改为：全量 extra（除 `event_type`/`content`）+ `team.message` 叠加展平 + **保留嵌套 `event`** | ① 白名单只补工具字段：与 Python 结构性差异，新增事件要回补；② 改 `AppendHistoryRecord` 签名：会连锁影响 `swarmtools.SetSendFileHistoryFunc(session.AppendHistoryRecord)` 的函数值注入 |
| D6 | `content` 取值加 `error` 兜底 | — |
| D7 | 流式 user 记录传 `request.Metadata` | 不改：企业渠道流式会话永久缺 `channel_metadata`，且与非流式路径自相矛盾 |

## 4. 改动清单

| # | 文件 | 改动 |
|---|---|---|
| ① | `internal/swarm/server/agent_server.go` | 新增 `streamTaskEntry{requestID, cancel}`；`sessionStreamTasks map[string]*streamTaskEntry`；`registerStreamTask(sessionID, requestID, cancel) *streamTaskEntry`；`releaseStreamTask(sessionID, e)`（身份判定 + delete，不 cancel）；`cancelStreamTask` 保留取消语义并带 requestID 日志 |
| ② | `internal/swarm/server/handle_envelope.go` | `entry := s.registerStreamTask(...)`；`defer s.releaseStreamTask(sessionID, entry)` |
| ③ | `internal/common/utils/context.go`（新增） | `func Canceled(ctx context.Context) bool { return errors.Is(ctx.Err(), context.Canceled) }`；同步登记 `utils/doc.go` |
| ④ | `internal/agentcore/single_agent/agents/react_invoke.go` | 删除 `Invoke` 内清理（D3a）；`streamProcess` 错误分支先判 `utils.Canceled` 并直接返回（D2）；`:447` 判定换 helper（D4） |
| ⑤ | `internal/agentcore/single_agent/agents/react_model_call.go` | `callLLMStream` 循环后判 `utils.Canceled`，取消则 `return nil, ctx.Err()`（D2b） |
| ⑥ | `internal/agentcore/foundation/llm/model_clients/openai/client.go` | `WrapError` 增加 `exception.WithCause(err)`（文案不变） |
| ⑦ | `internal/agentcore/context_engine/context/session_model_context.go` | `ClearMessages` 行为不变；仅把日志 `cleared_count` 改为 `len(PopBack(...))` 的实际弹出条数（D3b 更正后） |
| ⑧ | `internal/swarm/server/runtime/uapclaw.go` | 全量 extra + team 叠加 + 保留嵌套 `event`（D5）；`content` 加 `error` 兜底（D6）；`:318` 换 helper（D4）；`:284-286` 传 `request.Metadata`（D7） |

声明顺序、中文注释、`doc.go` 目录条目按项目规范同步。

## 5. 改后的三条链路

### 5.1 打断（D1 后）

```
T1 handleCancel: cancelA + delete         → 回 resp（不等 A）
T2 B 注册: map[sid] = entryB
T3 A 收尾: release(sid, entryA)           → map[sid] != entryA → no-op
```

### 5.2 取消（D2 / D2b 后）

```
ctx 取消 → LLM 中断 → callLLMStream 判 ctx → 返回 error
→ reactLoop → invokeImpl → 清本轮（一次）→ Invoke 返回
→ streamProcess 判 ctx → 直接 return（不写帧、不落 history）
→ 网关侧 cancelled final 收尾气泡（前端已有逻辑）
```

### 5.3 history 记录（D5 后）

```
chat.tool_call     → {event_type, content:"", tool_call:{name, arguments, tool_call_id}}
chat.tool_result   → {event_type, content:"", result, tool_name, tool_call_id, success, raw_output}
chat.usage_summary → {event_type, content:"", usage:{...}}
team.message       → {event_type, content:"", event:{type, timestamp, ...}, message_id, member_name, ...}
chat.error         → content = payload["error"]
```

## 6. 边界与取舍

| 项 | 决定 | 说明 |
|---|---|---|
| 静默范围 | 仅 `context.Canceled` | `DeadlineExceeded` 仍上报（对齐 Python `TimeoutError` 属 Exception） |
| 真错误恰好发生在取消之后 | 以 ctx 为准，静默 | 窗口极窄；Python 的取消在 await 点优先 |
| 模型刚好生成完、取消最后到达 | 丢弃该回答 | 用户已按停止；用例中断言这一预期 |
| `release` 遇空槽 / 指针不匹配 | no-op | 不 cancel、不误删 |
| `cancelStreamTask` 的槽选择 | 仍按 session 取当前槽 | 已知取舍：并发两轮时只停最近注册的一轮 |
| `ClearMessages(false)` | 只清本轮 | 唯一生产调用方 `react_invoke.go:246`（全仓 grep 确认） |
| `history.json` 体积 | 会增大 | 与 Python 一致（`tool_result` 带 `result`/`raw_output`），先观察不截断 |

## 7. 测试设计

原则：**每个用例的注释标注它防的是哪个编号的回归**，避免后续被"顺手删掉"。

### 7.1 单元测试

| 用例 | 位置 | 断言（防回归点） |
|---|---|---|
| `TestRegisterStreamTask_ReturnEntry` | `internal/swarm/server/agent_server_stream_task_test.go`（新） | `register` 返回的 entry 与槽内指针相同（D1 前置） |
| `TestReleaseStreamTask_OnlyRemovesOwnEntry` | 同上 | A 注册 → `cancelStreamTask` → B 注册 → `release(A)`：槽内仍是 B 的 entry，且 **B 的 cancel 未被调用**（用带计数的 fake cancel 断言）。改动前此用例必红（D1 核心） |
| `TestReleaseStreamTask_EmptySlotNoop` | 同上 | 槽被 `handleCancel` 删除后 `release` 不 panic、不误删（D1） |
| `TestCancelStreamTask_CancelsCurrentOnly` | 同上 | `cancelStreamTask` 取消当前槽并删除；已删除时 no-op（D1） |
| `TestCanceled` | `internal/common/utils/context_test.go`（新） | `Canceled` / `DeadlineExceeded` / 无取消 三态判定（D4） |
| `TestCallLLMStream_CanceledMidStream` | `internal/agentcore/single_agent/agents/react_model_call_test.go` | 流通道在取消后关闭 → 返回 error，**不返回半截 assistant 消息**（D2b） |
| `TestCallLLMStream_CanceledAfterComplete` | 同上 | 生成完再取消 → 仍返回 error（记录第 6 节的取舍） |
| `TestStreamProcess_SuppressesErrorFrameOnCancel` | `internal/agentcore/single_agent/agents/react_agent_test.go` | ctx 取消时 session stream 无 `result_type=error` 帧（D2） |
| `TestStreamProcess_ReportsRealError` | 同上 | 非取消错误仍写错误帧（防"静默过度"） |
| `TestClearMessages_OnlyClearsCurrentRound` | `internal/agentcore/context_engine/context/session_model_context_test.go` | 历史前缀长度不变、本轮归零（D3b） |
| `TestClearMessages_HistorySizeConsistent` | 同上 | 清理后 `GetBack(0,false)` 为空、`GetBack(0,true)` 等于历史条数（D3b 脏计数） |
| `TestInvoke_ClearsContextMessagesOnce` | `react_agent_test.go` | 取消路径 `CONTEXT_CLEARED` 事件只触发一次（D3a） |
| `TestRecordPayloadExtras_ToolCall` | `internal/swarm/server/runtime/uapclaw_history_test.go`（新） | 记录顶层含 `tool_call` 嵌套对象（含 `tool_call_id`）（D5） |
| `TestRecordPayloadExtras_ToolResult` | 同上 | 顶层含 `result`/`tool_name`/`tool_call_id`/`success`（D5） |
| `TestRecordPayloadExtras_TeamMessageKeepsEvent` | 同上 | 同时存在嵌套 `event` 与展平字段，且展平不覆盖 `type`/`timestamp`/`content`（D5） |
| `TestExtractChunkContent_ErrorFallback` | 同上 | `content` 为空、`error` 非空时取 `error`（D6） |
| `TestAppendHistoryStream_UserChannelMetadata` | 同上 | 流式 user 记录的元数据含 `channel_metadata`（D7） |
| `TestWrapError_PreservesCause` | `internal/agentcore/foundation/llm/model_clients/openai/client_test.go` | `errors.Is(err, context.Canceled)` 为真，且 `Error()` 文案不变（⑥） |

### 7.2 集成测试

均带 `//go:build integration`、调用 `suite.Run(`，目录需通过 `cmd/integration-audit` 两层门禁（调用跨度 ≥3 个 internal 二级子系统）。

| 用例 | 位置 | 链路跨度 | 断言 |
|---|---|---|---|
| `TestSupplementInterrupt_NewTurnNotKilled` | `tests/integration/swarm/server/stream_interrupt_test.go`（新） | `internal/swarm/gateway`（MessageHandler 处理 supplement）+ `internal/swarm/server`（AgentServer 流任务表）+ `internal/swarm/transport`（真实 ChannelTransport 往返） | ① 旧轮 `stream task registered` → `handleCancel` → 新轮 `registered` 之后，旧轮收尾不得取消新轮 ctx（用可控假 agent 记录 `ctx.Err()`）；② 新轮能产出 chunk 直到自行结束；③ 帧序列中**无** `chat.error` |
| `TestSupplementInterrupt_NoErrorHistoryRecord` | 同上 | 追加 `internal/swarm/server/session`（history 写入） | 取消后 `history.json` 中**不存在** `chat.error` 记录，且 `chat.delta`/`chat.final` 等记录不受影响 |
| `TestHistoryRoundTrip_ToolCallsRestorable` | `tests/integration/swarm/server/history_roundtrip_test.go`（新） | `internal/swarm/server/runtime`（记录构造）+ `internal/swarm/server/session`（落盘）+ `internal/swarm/server`（`history.get` 分页过滤） | 带工具一轮后：`history.get` 返回的记录含 `chat.tool_call`（带 `tool_call.name/id`）与 `chat.tool_result`（带 `tool_call_id`），可被前端还原逻辑消费；`chat.error` 记录的 `content` 非空 |
| `TestHistoryRoundTrip_TeamEventShape` | 同上 | 同上 | `team.message` 记录同时含嵌套 `event` 与展平字段 |

补充要求：
- 集成用例的假 agent 必须能"卡住"流（持有通道，由测试放行），否则 D1 的交错无法稳定复现；
- 用例中显式断言"改动前应为红"的关键点（如 B 的 ctx 未被取消），确保不是空壳测试；
- 有效测试密度（断言 ≥2、方法体非空）满足审计第一层门禁。

### 7.3 覆盖率与门禁

- 单元测试：改动行覆盖率 ≥ 85%（CI 门禁）；
- 集成审计：`go run ./cmd/integration-audit -verify` 本地预检通过；
- CI 四 job（Build / Lint / Unit / Integration）全绿才算完成。

## 8. 提交切分

| 提交 | 内容 | 说明 |
|---|---|---|
| 1 | `fix(swarm/server): 流式任务收尾只释放自身，不再取消下一轮` | ① ② + D1 单测（含"改动前必红"用例） |
| 2 | `fix(agentcore): 取消不再上报为错误，清理只清本轮` | ③ ④ ⑤ ⑥ ⑦ + D2/D2b/D3/D4 单测 |
| 3 | `fix(swarm/server): history 记录保留 payload 字段与渠道元数据` | ⑧ + D5/D6/D7 单测 + 两条集成用例 |

每个提交独立跑 CI，避免多缺陷混在一个红灯里难以定位。

## 9. 风险与观测点

| 风险 | 缓解 |
|---|---|
| D2 后前端只靠网关的 cancelled final 收尾，若前端对"空 content 的 `chat.final`"处理有遗漏，气泡可能停在处理中 | 集成用例断言帧序列；Python 同款行为已有前端逻辑 |
| D3 改变 `ClearMessages` 语义，可能有测试/扩展依赖"全清" | 全仓 grep 确认仅一个生产调用方；单测覆盖两种 `withHistory` |
| `history.json` 体积上涨 | 与 Python 一致；上线后观察单会话文件大小，必要时另议截断策略 |
| 并发两轮同 session 时 `cancelStreamTask` 只停最近注册者（已知限制） | 记录在案；若要根治需引入 D1 的"停稳确认"，本次不做 |

## 10. 验收标准

1. D1–D7 全部落地，单测/集成分区通过；
2. 集成用例在改动前可稳定复现失败（至少 D1、D5 各一条）；
3. `go test -race ./...`、`go vet`、`staticcheck`、`gofmt -l` 无新增问题；
4. `cmd/integration-audit -verify` 通过；
5. CI 四个 job 全绿。
