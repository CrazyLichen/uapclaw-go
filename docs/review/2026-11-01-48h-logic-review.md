# 2026-11-01 48 小时逻辑审查

审查窗口：2026-10-30 04:00 ~ 2026-11-01 04:00（HEAD `4157122e`）
范围：219 个提交、316 个文件、+54347 / −4234 行。
其中与上一轮审查（`4dd0c210`，覆盖到 `7ce87f38` 2026-10-31 01:27）**不重叠的新增部分**为 122 个提交、254 个文件、+24134 / −3306 行，本文结论均落在这一区间；更早的提交只做回归抽查，不重复上一轮已登记的问题。

基线 CI：`41221389536`（HEAD）四 job 全绿。

审查依据：`IMPLEMENTATION_PLAN.md` 与 `docs/superpowers/specs|plans/` 下各节设计/计划，逐条对照 Python 参考实现
（`/home/opensource/agent-core` = openjiuwen；`/home/opensource/jiuwenswarm-develop` = jiuwenswarm）。

本轮覆盖的提交簇：

| 簇 | 章节 | 主要新代码 |
|----|------|-----------|
| A2A 协议通道 | 11.26 | `protocol/a2a/{a2a_channel,a2a_parts,a2a_executor,a2a_card}.go`(≈900)、`app_gateway.go` 装配 |
| Pregel 基座 | 8.3d/8.6/8.7 | `graph/pregel/{base,config,constants}.go`、`tracer/handler.go`、`session/interaction/*`、`stream_utils.go` |
| slash 命令对账 | 10.4.4a | `message_handler/{slash_cmd,dispatch,outbound}.go`、`server/handle_command.go`、`deep_adapter_slash.go` |
| ACP stdio 集成测试 | 10.4.6 | `utils/payload_bridge.go`、`tests/integration/swarm/acp/**`、`deep_agent.go`/`task_loop/handler.go` 错误帧 |
| 审查修复批次 | 09-28 22 项 + 10-31 34 项 | `foundation/store/kv/*`、`memory/common/distributed_lock.go`、`memory/external/viking_provider.go`、`agent_teams/*`、`observability/*`、`prompts/*` |
| 知识库/检索 | 13.27/13.28 | `knowledgebase/{base,simple_kb,graph_kb,multi_kb,kb_helpers}.go`、`utils/config_manager.go`、indexer `Close` |
| 网关复核/CLI | 11.25 | `route_config.go`、`acp_alias.go`、`gateway_server.go`、`cmd/uapclaw/acp_chat_cmd.go` |

统计：**严重 5 项 / 一般 17 项 / 提示 16 项**。

---

## 一、严重（5 项）

### S-01 rail 初始化失败后待注册队列被清空且不回填，重试静默变成「无 rail」

- Go：`internal/agentcore/harness/deep_agent.go:1898-1940`（提交 `35781684`）

```go
pendingToRegister := make([]agentinterfaces.AgentRail, len(d.pendingRails))
copy(pendingToRegister, d.pendingRails)
d.pendingRails = nil            // ① 先清空
d.railsMu.Unlock()
...
for _, r := range pendingToRegister {
    if err := r.Init(ctx, d); err != nil {
        return ctx, fmt.Errorf("rail %s 初始化失败: %w", ...)   // ② 失败项 + 余项一起消失
    }
    ...
}
d.initialized = true
```

- 现象：该提交的目的是「`ensureInitialized` 不再吞 rail 错误」（把 `Warn + continue` 改成向上返回 error），方向正确；但错误返回时 `d.initialized` 仍为 `false`，而 `d.pendingRails` 已被清空。调用方（`Invoke`/`Stream`，`:253`/`:315`）重试时会再次进入 `ensureInitialized`，此时循环源为空 → 直接 `d.initialized = true` 返回 nil。
- 影响：一次可恢复的初始化抖动（如 workspace 未就绪、plugin 握手超时）会把**整批 rail**（TaskPlanning / SkillEvolution / TeamPolicy / ProjectMemory / CodingMemory 等）永久丢失，且日志里只有第一次的 Error，第二次完全静默——这正是本次修复想消除的现象，只是从「单条 Warn」升级成了「整批静默」。另外，出错前已 `Init` 成功的 rail 也没有 `Uninit` 回滚，重试路径上它们既未注册也未释放。
- 建议：失败时把失败项与剩余项写回 `d.pendingRails`（`d.pendingRails = append(pendingToRegister[i:], d.pendingRails...)`），对已 `Init` 的 rail 逐个 `Uninit`；补一条「首个 rail Init 失败 → 重试 → 断言全部 rail 在册」的用例。

### S-02 slash 命令链路把 Python 的 `create_task` 改成同步 RPC，阻塞全局唯一 forwardLoop

- Go：`internal/swarm/gateway/message_handler/slash_cmd.go:320`（`/skills`）、`:398`（`/branch`）、`:442`、`:496`（`/rewind`）、`:514`；调用链 `forward_loop.go:94`（单 goroutine `for {}`）→ `message_handler.go:117`（`handleChannelControl` 同步）
- Python：`jiuwenswarm/gateway/message_handler/message_handler.py:646`、`:833`、`:854`、`:864`、`:873` —— 五个 notice 全部 `asyncio.create_task(...)` 后立即 `return True`

```go
resp, err := mh.agentClient.SendRequest(context.Background(), env)   // slash_cmd.go:320
```

- 影响：`app_gateway.go:134` 只创建**一个** `MessageHandler`，`forward_loop` 是单 goroutine 串行消费 `mh.userMessages`；`routing/agent_client.go:62` 的 unary 超时是 **600s**。因此 `/branch`（复制 history + checkpointer）、`/rewind`、`/skills list` 中任意一条在网络上卡住，**所有 IM 渠道、所有会话**的入站消息都会停摆最长 10 分钟。Python 侧 `create_task` 只延后该条命令的完成，不阻塞事件循环。
- 建议：三个 notice 改为 `go func(){ ... }()`（或按会话串行队列），并传 `forwardLoop` 的 ctx 而非 `context.Background()`；`/rewind` 与 `/branch` 的顺序要求（先取消再转发）在 goroutine 内部保持同步即可。

### S-03 `DbBasedKVStore.CompareAndDelete` 并非原子，分布式锁可能删掉他人持有的锁

- Go：`internal/agentcore/foundation/store/kv/db_based.go:146-172`（提交 `567ba7ef`）

```go
err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
    var row KVStoreRow
    err := tx.Where("key = ?", key).First(&row).Error   // ① 先读
    ...
    matched = true
    return tx.Where("key = ?", key).Delete(&KVStoreRow{}).Error   // ② 后删（无 value 条件）
})
```

- 契约：`base.go:29-36` 明确写「原子性地删除…避免 Get 与 Delete 之间因 TTL 过期或他人抢占造成的 TOCTOU」，提交信息也自称「避免 TOCTOU」。
- 现象：MySQL 默认 REPEATABLE READ 下两个并发事务都能读到同一快照值，`matched` 各自置真；第二条 `DELETE ... WHERE key = ?` 是加锁读，看到的是**最新已提交行**——若此时锁已被他人重新获取（key 存在但 value 不同），这条 DELETE 会把**别人的锁删掉**并返回 `RowsAffected=0`（不报错），`matched` 仍为 true。PostgreSQL READ COMMITTED 亦同理。
- 影响：分布式锁在竞争窗口内出现双持有者（`memory/common/distributed_lock.go` 的释放路径正是本方法），是内存索引/记忆写入类临界区的静默数据竞争。
- 建议：改为单条 `DELETE ... WHERE key = ? AND value = ?` 并校验 `RowsAffected > 0` 再置 `matched`；exclusive 信封（`{value,expiry}`）需在 SQL 层额外限定或先解包再比较。
- 附带：`base_test.go:157-175` 的契约测试只挂了 `in_memory` / `file` 两个 factory，`db_based_test.go`、`redis_test.go` 无任何 `CompareAndDelete` 用例。**最易写错的 SQL 后端零覆盖**，本缺陷因此漏网。建议把 sqlite 内存库与 miniredis 纳入同一契约表。

### S-04 A2A 通道 Stop 先 `Shutdown` 再投唤醒，有流式客户端时固定吃满 5s 并导致后续停机步骤拿到过期 ctx

- Go：`internal/swarm/gateway/channel_manager/protocol/a2a/a2a_channel.go:252-286`、`app_gateway.go:569`（`shutdownTimeout = 5s`）、`:593`（A2A 先停）、`:602`（WebChannel 后停）

```go
if srv != nil {
    shutdownErr = srv.Shutdown(ctx)      // 等活跃 handler 返回
}
ch.mu.Lock()
for id, pending := range ch.pending {    // 唤醒在这之后
```

- 现象：SSE 处理函数只有在收到唤醒消息后才返回（`a2a_executor.go:59-107` 的 `yield` 循环一直阻塞在 `<-pending.C()`）。`http.Server.Shutdown` 不会强断活跃连接，只等 handler 返回 → 两者互相等待，直到 5s ctx 超时。
- Python 参照：`a2a_connect.py:276-303` 顺序相同（先 await server task 再投唤醒），但 uvicorn 的 `should_exit` 分支会主动 `connection.shutdown()` 打断流式响应，因此不会自锁——**顺序照抄，语义不等价**。
- 影响：每次关机（含容器滚动、CI e2e 收尾）只要有 A2A 客户端连着，就固定阻塞满 5s；`Shutdown` 超时返回 `ctx.Err()` 后 `WebChannel.Stop(ctx)` 拿到的是已过期 ctx，优雅停机退化为强制关闭（`:602-606` 走 Error 日志）。剩余连接与 goroutine 也不会被 `Close()` 回收。
- 建议：先遍历 `pending` 投唤醒并清空，再 `Shutdown`；`Shutdown` 返回错时补 `srv.Close()`；并把 A2A 的 stop 挪到 `webChannel.Stop` 之后或各用独立 ctx。

### S-05 KB 组件指针的并发保护只加在基座方法上，子类仍在裸读字段

- Go：`internal/agentcore/retrieval/knowledgebase/simple_kb.go:303-317`、`:73-80`、`:262-282`；`graph_kb.go:114`、`:380-389`、`:414-418`；对照 `base.go:192-221`

```go
if s.vectorStore == nil { ... }                 // 无锁读
switch s.config.IndexType {
case common.IndexTypeVector:
    s.retriever = retriever.NewVectorRetriever(s.vectorStore, s.embedModel)   // 无锁写
```

- 提交 `534a3aa5`（M-08）自称「读写两侧统一走 RLock / Lock」：`SetVectorStore`/`SetIndexManager`/`VectorStore()`/`IndexManager()` 确实都加锁了，但**所有业务方法仍直接访问 `s.vectorStore` / `s.indexManager` / `s.retriever`**。
- 影响：① `-race` 下与 `Set*` 并发即为真实 data race（现有测试只有「懒创建并发」用例，没有「并发 Set* + Retrieve」，所以全绿不能作为证据）；② 在 `simple_kb.go:73` 的 nil 检查与 `:79` 的使用之间，若 `SetVectorStore(nil)` 插入，直接空指针 panic。
- 建议：业务方法开头一次性取 `s.VectorStore()` / `s.IndexManager()` 快照并在全方法内复用（不要在方法里多次读字段），补一条真并发 `Set*` + `Retrieve` 的 `-race` 用例。

---

## 二、一般（17 项）

### M-01 A2A 有界队列满时连终态消息一起丢，任务永久卡在 WORKING

`internal/swarm/gateway/channel_manager/protocol/a2a/a2a_channel.go:186-193`。偏差 D11 用 `select/default` 丢弃，未对 `is_complete` 终态豁免；executor 只有收到终态才 `terminalState` 收尾（`a2a_executor.go:104-107`）。Python `a2a_connect.py:305-309` 是 `await pending.queue.put(msg)`（阻塞投递，永不丢终态）。SSE 客户端一旦不读，64 槽填满后终态被丢 → 任务在 TaskStore 里永久 WORKING、连接常驻（还会反噬 S-04）。建议终态消息走阻塞投递（带 ctx 超时）或独立保底通道。

### M-02 A2A Cancel 不唤醒 Execute，连接与 goroutine 泄漏、内部会话继续空跑

`a2a_executor.go:117-124`：`Cancel` 只 `ClearPendingRequest`（从 map 删表，`a2a_channel.go:242-246`），原 `Execute` 仍阻塞在 `<-pending.C()`，且不通知内部会话停止。Python 的 canceler（`a2asrv/agentexec.go:414-436`）同样不取消原 ctx，但 Go 侧把「连接是否结束」完全押在队列上，后果更重。建议 Cancel 时向该 `requestID` 投一条中断消息（或关闭 done channel），并把取消意图透传内部会话。

### M-03 AgentCard 宣告 `protocolVersion=1.0.0`，而 a2a-go 服务端只接受严格相等的 `1.0`

`a2a_channel.go:117`（默认 `"1.0.0"`）→ `a2a_card.go:26`。SDK 侧 `a2a/core.go:33` 为 `Version = "1.0"`，`a2asrv/jsonrpc.go:54` 把 `callCtx.ProtocolVersion` 固定成 `a2a.Version`，`a2asrv/intercepted_handler.go:335-349` 是 `clientVersion != callCtx.ProtocolVersion` 严格比较。任何按卡片字段回填 `A2A-Version` 的第三方客户端都会被 `-32009` 拒；a2a-go 客户端因 factory 的 major 版本回退恰好可用，掩盖了问题。Python 的配置默认值同样是 `1.0.0`（`app_gateway.py:1083`），属继承来的自相矛盾，但 Go 侧服务端校验更硬。建议默认收敛到 `sdka2a.Version`（写卡时统一），或至少在设计文档记一条偏差与对外说明。

### M-04 `activate_confirm` 解析的键名与事件名双错，且同仓库存在三份复制（其中两份是对的）

`internal/swarm/server/utils/stream_utils.go:735`、`:750`（提交 `39f104e5`）：

```go
if m["__interaction__"] == "activate_confirm" {
    ...
    return map[string]any{"event_type": "interaction.activate_confirm", ...}
```

Python `jiuwenswarm/server/utils/stream_utils.py:356-372` 是 `payload.get("interaction_type") == "activate_confirm"` 且返回 `event_type = "harness.activate_interaction"`。同文件 `:471-497` 与 `deep_adapter_team.go:1764-1782` 两处实现是**正确**的。后果：真实信封（`interaction_type=activate_confirm`）永远不命中新分支 → 落到 converter 后因无 `questions` 静默丢失；即便命中，`interaction.activate_confirm` 全仓库无消费者。而该分支上方的注释（`:728-734`）直接写着 `// Python: interaction_type = interaction.get("__interaction__")`——把错误依据固化成了"对齐说明"，会误导后续审查不再复查（`39f104e5` 的同类问题还有 `:604-608` 为 `FindInteractionPayloads` 添加的「保留为移植后的对齐点，届时无需再改消费端」，但该函数在生产侧当前确无可达生产者）。另：三段近乎相同的解析逻辑应合并为一个（`kb_helpers.go` 式的下沉早有先例），否则下次修改仍会漏一处。

### M-05 A2A `DispatchA2ARequest` 忽略入站回调的返回值

`a2a_channel.go:235`：`cb(msg); return pending, nil`。`channel_manager.go:371-394` 明确 `return false` 表示「通道已注销 / 处理器为空」；丢弃后任务会一直挂到客户端断开。Python `a2a_connect.py:311-343` 同样丢返回值（parity），但 Go 已把布尔语义显式化，没有理由继续丢。建议 `if !cb(msg) { ClearPendingRequest(...); return nil, fmt.Errorf(...) }`，executor 已有转 FAILED 的分支。

### M-06 `/rewind` 用静默模式取消，用户看不到中断结果

`slash_cmd.go:496`：`CancelAgentWorkForSession(ctx, msg, targetSID, false)`。Python `message_handler.py:1076` 调用 `_cancel_agent_work_for_session(msg, target_sid)`，默认 `publish_interrupt_result=True`（`:386`）。同批次的 `/branch`（`:398` 附近）传的是 true，属自相矛盾。影响：rewind 前后的任务被取消但用户无任何提示，也看不到 `cancelled_tools`。建议改 true。

### M-07 `/branch` 把展示层默认标题写进请求参数，服务端标题派生规则失效

`slash_cmd.go:369-371`、`:392`：`if branchName == "" { branchName = "Branched conversation" }` 后放进 `params["title"]`。Python `message_handler.py:988` 传空串，只在 `:990` 展示时回退；服务端 `session_ops_service.py:101-108` / `session_ops.go:237-245` 的规则是「title 非空直接用，否则由源会话标题派生 `<标题> (Branch)`」。影响：无名分叉标题从 `"<源标题> (Branch)"` 变成 `"Branched conversation (Branch)"`，多个分叉互相重名。建议请求保持空串。

### M-08 `command.model` 参数 JSON 解析失败静默返回「查询成功」

`internal/swarm/server/handle_command.go:60-65`：`json.Unmarshal` 失败仅 Warn，返回零值 `modelCommandParams`，随后走 `default` 分支返回 `ok=true` + `current: "default-model"`。Python `agent_ws_server.py:2346` 的 `except` 返回 `ok=false, {"error": str(e)}`。影响：`env_updates` 传成字符串、`model` 传成数字等客户端错误被伪装成成功，前端拿到假状态。建议解析失败直接走 `modelErrorResponse`。

### M-09 GraphInterrupt 端到端只完成 tracer 一跳，生产侧无任何 recover 点

`internal/agentcore/session/tracer/handler.go:293-296` 已 `case *pregel.GraphInterrupt`，但 `PanicGraphInterrupt`（`pregel/base.go:92-103`）是 `panic()` 实现，`internal/agentcore/session/` 下生产代码 `grep recover(` 为 0，`stream_utils.go:307-310` 只消费 `__interaction__` 流。当前仅测试可达，不构成现行故障，但 8.7 计划表标 ✅ 容易被读成「中断链路已通」。建议把「8.3b 增加 `case *pregel.GraphInterrupt` 的 recover + panic→recover→tracer 端到端用例」写成 8.3b 的验收项，并在 8.7 spec 注明「本节点只交付 tracer 分支」。

### M-10 `Message.kind` 为非导出字段且无序列化，断点恢复后判别式丢失

`internal/agentcore/graph/pregel/base.go:19-23`：`kind MessageKind`（非导出，只能经 `NewTriggerMessage`/`NewBarrierMessage` 设置）。Python `core/graph/store/base.py:36` + `store/serde.py:36-43` 用 pickle，保留类型身份。`GraphState.pending_buffer` 一旦走 JSON/gob 持久化，恢复后 `Kind()==""`，8.4 两种通道都拒收 → 待恢复的超步被静默丢弃。建议 8.9 开工前定案：实现 `MarshalJSON/UnmarshalJSON`（含 kind 合法性校验），或提升为导出字段由通道校验，并在 spec 登记。

### M-11 `user_latest_input` 的流输出与中断值形状与 Python 不一致

`internal/agentcore/session/interaction/interaction.go:157-176` 两处都写 `payload := InteractionOutput{ID: nodeID, Value: value}`。Python `core/session/interaction/interaction.py:52-62`：流输出 payload 是元组 `(self._node_id, value)`，**中断值 payload 是 `(self._node_id, None)`**（不带真实 value）。按 Go 消费端 `ConvertInteractionsToAskUserQuestion` 的判定，`{id,value:"..."}` 会因 value 无 `questions` 命中「第二轮 permission_interrupt」，前端多弹一条本不该有的审批框；8.14 取 `Interrupts[0].Value` 时也会拿到真实 value 而 Python 为 None。要么复刻元组语义，要么在 8.7 spec 记为显式偏差并补齐消费端影响分析（当前只在代码注释里声明）。

### M-12 `ConfigManager.GetConfig` 在「键双轨」下返回不确定值，且配置解析静默分叉

`internal/agentcore/retrieval/utils/config_manager.go:177-187`：

```go
for _, cfg := range m.configs {          // Go map 迭代序随机
    if v, ok := cfg.(T); ok { return v, true }
}
```

Python `config_manager.py:89-95` 的 dict 是插入序，恒命中最先插入的 `"knowledge_base"`。`LoadFromFile` 后再 `UpdateConfig` 会让两个键存同类型值 → Go 时而返回加载值、时而返回更新值，Python 恒返回旧值。注释「同类型只会有一个键命中，行为等价」不成立。
同文件 `:252-260`、`:298-320` 的类型/数值解析也与 pydantic lax 语义分叉且完全静默：`chunk_size: 512.5` 被截断成 512（Python 报错）、`"1024"` / `"true"` 被判非法回落默认（Python 转 1024/True），且无任何日志；上界用 `>` 放行 `2^63`，`int()` 溢出成负数。建议固定优先 `knowledge_base` 键、区分「缺失/类型不符/越界」并 Warn、上界改 `>=`（或 `2^63-1`）。

### M-13 ACP 别名链的 ctx 透传只改了一半，上游仍恒为 `Background`

`message_handler/acp_alias.go:136` 已改为接收 ctx，但两个调用点仍传 `context.Background()`：`gateway_server.go:305`（`handleRawMessage`）、`acp_bridge.go:354`（`dispatchMessage`）。`b49ee41a` 声称「连接断即取消，避免已断仍阻塞在 RPC」，注释与实现不符，`session.create`（`acp_alias.go:91-111` 在全局写锁内做无超时 RPC）仍不受连接生命周期约束。建议 `HandleConnection` 派生每连接 ctx 并在断连时 cancel，一路透传到 `ensureAcpAgentSession`。

### M-14 `uapclaw acp-chat`：dotenv 缺失直接失败、退出码错档、SIGTERM 被当作中断

`cmd/uapclaw/acp_chat_cmd.go:106`（`signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)`）、`:319-322`（`if ctx.Err() != nil { return exitCodeInterrupted }`）、`:120` + `dotenv/early.go:82`（文件不存在返回 error）。
① Python `dotenv_early.py:90-98` 仅 warning 后继续，Go 直接终止冒烟；② 该 error 落到 `exitCodeFor` 得 1，与命令文档「2=配置错」矛盾；③ SIGTERM（容器/systemd 停止）也返回 130，脚本会误判为 Ctrl-C，且回复已正常输出后被 SIGTERM 命中会把 0 翻成 130（Python `acp/cli.py:69-72` 只把 KeyboardInterrupt 映成 130，SIGTERM 为 143）。建议 dotenv 侧降级为告警、区分信号类型、PreRunE 错误包成退出码 2。

### M-15 团队清理 30s 预算被 `FinalizeMember` 与 `PostRun` 共用

`internal/agent_teams/runtime/team_runner.go:752-756`（`teamCleanupContext` 返回单个 30s ctx），调用点 `:272-290` 把同一个 `cleanCtx` 先后传给 `FinalizeMember` 与 `PostRun`。慢存储下前一步吃满预算，后一步只剩残值 → checkpoint/终态落盘失败仅 Warn，会话终态可能丢。Python `team_runner.py:359-365` 的 finally 无超时、两步独立。建议两步各建 ctx。

### M-16 团队消息入队是 check-then-act，末轮结束后的输入无人消费

`internal/agent_teams/agent/team_agent.go:768-777`：`if a.streamController.HasInFlightRound()` 判断后释放锁，再 `QueuePendingInput(content)`；而 `runOneRound` 的 defer 只在轮内 drain。若入队落在 drain 之后、轮次退出之前，消息滞留到下一次 `StartRound`。Python 单线程无此窗口。建议把「判断 + 入队 + 必要时起轮」合并成一个持锁方法。

### M-17 `Close()` 规范化的收尾不完整

`Close() error` 统一（`c3f188de`）后：`base.go:330-351` 已改为聚合首错返回，但 `base_test.go:324` 仍叫 `TestClose_错误变体吞并` 且只 `b.Close()` 不断言返回值——「部分失败取首错」零覆盖；`indexer/chroma.go:190`、`indexer/milvus.go:325-334`、`vector_store/*.Close` 恒返回 nil，即 `Close() error` 在真实组件上没有错误产出者；13.28 spec §6.2 仍贴着 `maybeClose(obj any)` 旧代码与「Warn 吞」的描述。建议改名并断言首错，spec 同步。

---

## 三、提示（16 项）

### T-01 `FindOption.MaxDepth` 从未生效，且结构体信封双倍消耗深度、命中顺序不确定
`stream_utils.go:608-610`（`opts` 被丢弃，实现里硬编码 `depth > 8`）、`:663`（`PayloadToMap` 后再 `depth+1`，实际可达深度约为 Python 的一半）、`:715-717`（`for _, val := range v` 命中首个，多 interaction 时结果不确定；Python dict 保序）。建议落实 `MaxDepth`、结构体桥接不额外计深度、命中后按 index/request_id 排序。

### T-02 桥接泛化没有贯穿同一函数，`:281` 仍是裸类型断言
`stream_utils.go:281`：`if payloadMap, ok := output.Payload.(map[string]any); ok`——已桥接的 payload 就在上一行。`context.compression_state` 遇结构体 payload 只剩 `event_type`，与本批次「新增结构体无需改消费端」的目标相反。

### T-03 桥接失败静默降级成 `content:"map[]"` 伪帧；`ToMap()` 快路径键名规则不一致
`payload_bridge.go:56-72`（`json.Marshal/Unmarshal` 任一失败返回 nil）→ `stream_utils.go:82-84` 兜成空 map → default 分支输出 `chat.<type>` + `content = "map[]"`（Python 空 dict 走 `**payload` 分支，无 content）。另有 int→float64、`FileDataFrame.Bytes`→base64、`metadata,omitempty` 丢键与 `model_dump(mode="json")` 形状不同。`payload_bridge.go:40-42` 的 `ToMap()` 快路径产出小写键 `type/payload`，JSON 往返路径产出 `Type/Payload`，`stream_utils.go:682-687` 同时认两套正是补偿。建议失败返回哨兵让调用方跳过该 chunk，并删掉快路径统一为一条。

### T-04 `PayloadsToMaps` 无深度上限、`seen` 用 slice 首元素地址
`payload_bridge.go:80-98`、`stream_utils.go:668-677`：自引用 `[]any` 会无限递归；`rv.Pointer()` 对子切片返回同一底层数组地址，两个合法子切片会被误判重复而漏检。建议加深度参数、slice 用 `(ptr,len)` 作键。

### T-05 `DistributedLock.lockValue` 无锁读写，`Release` 吞错返回 nil
`memory/common/distributed_lock.go:83`/`:104-113`：`if l.lockValue != ""` 与赋值之间无同步，同实例并发调用在 `-race` 下必报；未 `Acquire` 直接 `Release` 会以 `[]byte("")` 参与比较。`Release` 在 store 报错时仅 Error 日志后 `return nil`（与 Python try/except 静默一致，可保留），但调用方无从得知锁泄漏——建议至少返回可区分的 sentinel 或加 metric。

### T-06 `Release` 使用调用方 ctx，请求被取消即锁残留至 TTL
`distributed_lock.go:108-126` 用 `ctx` 调 `CompareAndDelete`；请求 abort 后 Redis Eval 直接失败，锁残留一个 TTL，其他请求在 `Acquire` 自旋重试。建议 `context.WithoutCancel(ctx)` + 短超时（与 `teamCleanupContext` 同一模式）。

### T-07 A2A `Start`/`Stop` 无状态机互斥；路由路径非法会在后台 goroutine 里 panic
`a2a_channel.go:294-336`：`running` 要等 200ms 探活后才置真，`srv` 也是 Serve 前才落表。`app_gateway.go:377-384` 用 `go ch.Start(ctx)` 启动，若 Start 尚未被调度时 Stop 先到，Stop 见 `srv==nil` 直接返回而服务器随后照常监听，再也无法关闭；并发两次 Start 会双 Listen 覆盖 `srv`。`:342-365` 的 `mux.Handle(ch.cfg.RPCPath, ...)` 在 env 写成无前导 `/` 或两条路径相同时会 panic `invalid pattern`，而该 goroutine 无 recover。建议加 `state` 枚举 + mutex、newMux 校验返回 error。

### T-08 A2A 的 SSE 测试只比两个子串先后，未验证帧契约
`a2a_http_test.go:190-196` 用 `strings.Index` 比较顺序，`ReadAll` 要等连接关闭才返回，即使服务端不逐帧 flush 也能通过；spec §7 声称验证的「首帧 `append=false`、末帧 `lastChunk=true`、`Content-Type: text/event-stream`」均未断言。建议改 `bufio.Scanner` 逐帧读取。

### T-09 ACP 出站套件的构建失败被吞成 Skip，对端与实现同源
`tests/integration/swarm/acp/acp_chat_e2e_test.go:45-50`：`build.CombinedOutput()` 失败与「go 工具链不可用」同落到 `s.T().Skip`——fakeacp 编译错误会让 11 例全绿跳过而不报错。且 fakeacp 与本项目同仓库同作者，协议字段错位时两端一起错；建议构建失败一律 `t.Fatal`，并至少有一处按 ACP 规范原始报文（`sessionUpdate`/`content` 字面量）断言，而非用聚合结果反推。

### T-10 tracer 的中断用例没有断言 `span.Error == nil`
`session/tracer/handler_test.go:493-505` 只在 `wantCode > 0` 时检查 `span.Error`，`GraphInterrupt` 用例（`wantCode: 0`）因此完全没有断言——与 8.7 spec §9 的「仍断言 `span.Status == Interrupted` 且 `span.Error == nil`」不符，将来中断被误写进 `span.Error` 不会红。

### T-11 中断路径写流丢 ctx 并吞错
`session/interaction/base.go:152-169` → `stream/writer.go:96-99` 内部写死 `context.Background()`；`interaction.go:127`、`:159` 又以 `_ =` 丢弃错误。Python 是 `await output_writer.write(...)`，写失败会先于 `raise GraphInterrupt` 上抛。建议把 ctx 透传到 `WriteInteraction`，写失败至少 Error 日志。

### T-12 孤立注释与过时注释
`memory/external/viking_provider.go:765`（`truncStr` 已迁 `utils.TruncateRunes`，注释孤悬紧贴 `floatVal`）、`agent_teams/agent/team_agent.go:1758`（`truncateRunes` 函数与 `utf8` 导入均已删）、`knowledgebase/simple_kb.go:28`、`graph_kb.go:31` 仍写「值嵌入」（`534a3aa5` 已改指针嵌入）、`indexer/doc.go:4` 仍写「五类操作」（接口已含 `Close` 共 6 方法）、`base.go:51-52`「5 抽象方法未实现」过时。项目规则要求注释与实现同步，建议一并清理。

### T-13 `viking_search` 的 `top_k <= 0` 不再回退 `limit`
`viking_provider.go:465-471`：`else if` 结构使 `top_k` 存在但非正时既不写 `top_k` 也不试 `limit`；schema 注释说「默认 10」却不落 payload，服务端默认一旦不同即静默漂移。

### T-14 计划文档的 checkbox 与 `IMPLEMENTATION_PLAN.md` 不一致
`docs/superpowers/plans/2026-10-31-a2a-channel-11.26.md`（74 个 `- [ ]` 全未勾选）、`2026-10-31-slash-command-10.4.4a.md`（41 个全未勾选）均与 `IMPLEMENTATION_PLAN.md` 的 ✅ 矛盾。项目规则 2 要求逐步骤同步状态，建议批量回填或在 plan 抬头声明「以下 checklist 不维护，以 IMPLEMENTATION_PLAN 为准」。

### T-15 ACP CLI 短选项只补齐一半；数字 id 归一仍不完整
`cmd/uapclaw/cmd.go:444-445` 有 `StringP("gateway-url","g",...)` 却 `String("agent-server-url", ...)` 缺 `-u`（Python `acp_connect.py:1785-1796` 两个都有）。`internal/swarm/e2a/acp/protocol.go:91-109` 的 id 归一：`json.Number("777.0")` 与 `"777"` 不等价，`float64` 大数用 `'f'` 与 Python 指数形式不同，`int32/uint64` 直接变空串；`ScalarString` 是同实现别名，双名增加漂移面。建议收敛为单一函数并补 32 位/无符号整型。

### T-16 agentic 未知 kwargs 的报错文案缺 `.__init__` 且按键随机
`knowledgebase/kb_helpers.go:73-91` 遍历 `kbOpts.Extra`（map 序随机），多未知键时报哪个不确定；文案 `AgenticRetriever() got an unexpected keyword argument` 缺 Python 的 `.__init__`（`agentic_retriever.py:129`）。建议按 key 排序取首个并补齐文案。

---

## 四、已核验通过（抽样）

- **pregel 常量与配置**：11 颗常量与 `openjiuwen/core/graph/pregel/constants.py:6-19` 逐值一致；`CreateInnerConfig` 的 falsy→10000、负数保留、值拷贝不改入参等价；包内无共享状态/goroutine/全局变量，config 全值类型；`graphInterrupter`、`interaction.GraphInterrupt`、`InteractionOutputSchema` 已无残留引用；覆盖率 100%。
- **A2A 主体**：`ChannelTypeA2A` 枚举三处同步、parts 双向映射、executor 事件序列（artifact append/lastChunk → statusUpdate 终态）、三路由装配（rpc 仅 POST、`/.well-known/agent-card.json`、extended card）、`app_gateway` 启停接线均与 spec 一致；实测 `-race` + 93.0% 覆盖无告警。
- **ACP stdio 测试**：出站 11 例 / 入站 5 例为真子进程；`07b7521f` 修 `task_failed` 被丢弃、`1d54edef` 补错误帧的方向正确。
- **slash 命令文档对账**：`/evolve_rollback` 的五段校验（上一轮 S-04）已按 `interface_deep.py:4159-4242` 补齐，含「无 version 时列出归档清单」；`/skills list` 五种文案形态、`sendChannelNotice` 的 `params={}`、`handleChannelControl` 的提前返回顺序均逐字对齐。
- **审查修复批次**：file/in_memory/redis 三个后端的 `CompareAndDelete` 原子实现正确（仅 db_based 有问题）；观测 rail 优先级 10、回调降序与 Python 一致；redaction 改 rune 截断等价 `len()/[:N]`；code 模式提示词语言固定 `en` 与 `interface_code.py:200-202` 一致；`prompts` 调用点已无 `_ =`。
- **知识库 13.27/13.28 主体**：懒创建三分支只建一次、`mode` 推导、nil config 回落 TopK=5、多 KB 软失败（裸 gather + 逐槽位写）、首见序稳定排序、`limit` 真值链与负值切片、`WithRetrieveFilters` 死参数复刻、Close 顺序与重复关闭字面复刻、`doc.go` 目录已登记新文件；155509/155510/155304/155302 均有真实错误码断言；实测 knowledgebase 96.0%、utils 89.4%。
- **网关 11.25 复核**：路径匹配为精确 + 尾斜杠变体（`/acp_extra` 404、大小写敏感、无默认回退）与 Python `_resolve_route` 一致；`chat.final` 的 timer 取消条件、S-05 的一次性持锁快照、idle timer 槽比对均正确；acp-chat 全程仅回复写 stdout、`acp` 子命令先 `os.Stdout = os.Stderr`、退出路径均经 defer 关闭子进程。

## 五、结论

本轮 122 个提交的整体质量与既定口径吻合度高：pregel 基座是逐值对齐的干净移植，A2A 主路径、slash 文案对账、知识库懒创建/聚合都做了真断言测试，上一轮 34 项修复确实落地（不是"改注释"）。

需要优先处理的是 5 项严重：

1. **S-01** 是修复引入的新回归，且失败形态是"静默缺能力"，最难排查；
2. **S-02** 把 Python 的并发安全（`create_task`）换成了阻塞调用，影响面是全局消息派发；
3. **S-03** 让"原子删除"名不副实，且恰好缺了唯二难写的后端测试；
4. **S-04** 的顺序照抄了 Python，但 Go 的 `Shutdown` 语义不同，形成自锁；
5. **S-05** 是"锁加在门面上、业务穿墙"，`-race` 测试没覆盖到这条路径。

一般项中 M-04（activate_confirm 双错 + 三份复制）、M-09/M-10/M-11（8.7 中断链路的三个断点）建议在本节标 ✅ 前先补文档约束，避免下一节开工时把"已接通"当真。

---

## 六、处置决策（2026-11-02，逐项与用户讨论后定案）

**核验更正**（讨论阶段对本文的修正，实施以这些为准）：

- **S-03**：比较已前移到删除前，但 `DELETE` 仍只带 key、`matched` 不看 `RowsAffected` → 缺陷成立，描述已更新。
- **M-02**：`Cancel` 已不再 `ClearPendingRequest`（有回归测试）；真实缺口是「不唤醒 Execute」。
- **M-08**：解析失败静默成 `ok=true` 成立，但 `current` 实为 `getenvOr("MODEL_NAME","unknown")`。
- **M-09**：`session/` 下 `recover(` 计数为 1（`stream/queue.go` 与中断无关），非 0；结论不变。
- **M-10**：`GraphState` 尚未移植，判别式丢失是 8.9 的陷阱而非现行故障。
- **M-13**：锁内无超时 RPC 已修，只剩两个 `Background` 调用点。
- **M-17**：`base.go` 已聚合首错，剩测试未断言 + spec 过时。
- **T-01**：「结构体信封双倍消耗深度」不成立（与 Python 同构）；成立的是 `MaxDepth` 被丢弃 + 命中顺序随机。
- **T-07**：缺前导 `/` 在 Go 1.22+ 是静默 host pattern 而非 panic；panic 只在非法通配符/空串。

**决策表**：

| 项 | 结论 | 项 | 结论 |
|---|---|---|---|
| S-01 | 成功后摘除队列（对齐 Python clear 位置） | M-12 | 插入序（复用 `wk8/go-ordered-map`）+ 上界 `>=` + 拒绝非整浮点 + 分类日志 |
| S-02 | 各分支协程化 + 透传 forwardLoop ctx | M-13 | `InterceptorFunc` 加 ctx + 连接级 ctx |
| S-03 | 单条带 value 条件 DELETE + 校验 RowsAffected，契约表补 db/redis | M-14 | dotenv 降级 + 配置错落 2 + SIGINT/SIGTERM 区分 130/143 |
| S-04 | 先投唤醒/关 stopCh 再 Shutdown + 两通道独立 ctx | M-15 | 清理两步各建 ctx |
| S-05 | 定契约（Set* 仅构造期）+ 修注释 + 只读并发测试 | M-16 | 入队与轮次收尾各收进单次持锁 + `roundDone` 读写全部进锁 |
| M-01 | 终态走独立 1 容量槽 | M-17 | 测试改名+断言首错 + spec 同步 |
| M-02 | Cancel 投合成终态（复用终态槽） | T-01 | 保留 MaxDepth（默认 8）+ map 按 key 排序遍历 |
| M-03 | 默认取 `sdka2a.Version` | T-02 | 用已桥接 payload |
| M-04 | 修字面量 + 三份合并为 `ParseActivateConfirm` | T-03 | 桥接失败丢弃 chunk + Error 日志 |
| M-05 | 检查 `cb(msg)` 返回值 | T-04 | 深度统一走 MaxDepth + seen 键 `(ptr,len)` |
| M-06 | `/rewind` 改 `publishInterruptResult=true` | T-05 | mu 保护 + `ErrLockNotHeld` |
| M-07 | 请求传空串，展示层回退 | T-06 | `WithoutCancel` + 5s 超时 |
| M-08 | 解析返错 → `modelErrorResponse` | T-07 | 状态机 + 路径预校验 + recover |
| M-09 | spec 写清边界 + 8.3b 立验收项 | T-08 | 逐帧断言 SSE 契约 |
| M-10 | **跳过**，8.9 开工时定案 | T-09 | 构建失败硬失败 + 原始报文断言 |
| M-11 | `InteractionOutput.Kind` 判别式 + 中断值 nil | T-10 | 补 `span.Error == nil` 断言 |
| | | T-11 | `WriteInteraction` 加 ctx + 三处错误上抛 |
| | | T-12~T-16 | 全部修复（T-16 列全未知键，文案不加 `.__init__`） |
