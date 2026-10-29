# 2026-10-29 48h 逻辑审查报告

> 审查窗口：2026-10-27 12:00 ~ 2026-10-29 01:22（HEAD = `31c56d79`，共 58 个非 merge 提交）
> 审查方法：逐提交对照 Python 参考项目（`agent-core/openjiuwen` + `jiuwenswarm-develop/jiuwenswarm`）核对方法签名、步骤序列、并发语义；`git log -S` 溯源问题引入提交。
> 本轮重点：窗口内**未经前轮审查**的新章节（13.24 QueryRewriter、11.15 CLI 路由全链路）全量审查；对已有前轮审查闭环的章节（13.21-13.23/10.6.24a-c）做修复落地复核抽查。
> 审查产出：**4 严重 / 4 一般 / 9 提示**（S-01~S-04 / M-01~M-04 / T-01~T-09）。

---

## 一、审查范围确认（步骤 1）

### 1.1 窗口内完成的实现计划章节

| 章节 | 状态 | 完成时间 | 提交链 | 前轮审查 |
|------|------|----------|--------|----------|
| 13.21 HybridRetriever | ✅ | 10-27 深夜（覆盖率 88.0%→96.7%） | `957a7fc1..7302d60e` | 无专项（本轮主线程复核） |
| 13.22 读取侧数据模型 | ✅ | 10-27 23:06 ~ 10-28 06:46 | `719273cc..725a1ece` | 已闭环（`725a1ece` M8 修复） |
| 13.23 GraphRetriever | ✅ | 10-28 12:38 ~ 15:27 | `fbc7e03e..b9805983` | 已闭环（`b9805983` 两项修复） |
| 13.24 QueryRewriter | ✅ | 10-28（8 提交链） | `0ea180e8..04089114` | **无**（本轮全量审查） |
| 10.6.24a 轻量工具+装配 | ✅ | 10-27 18:32 ~ 10-28 01:39 | `b6eb76c9..19c1aa68` | 已闭环（`e620425c`） |
| 10.6.24b send_file 全链 | ✅ | 10-28 早间 | `96bda791..d0b1c3a0` | 已闭环（`df6596af`/`f75c952e`，文档已删） |
| 10.6.24c 会话级工具 | ✅ | 10-28 | `bd9a514c..273bb16a` | 已闭环（`d84dfaf5` cancel_session 修复） |
| 11.15 CLI 路由全链路 | 🔄 | 10-28 晚间（36 步全勾选，`fcbc11d9` CI 全绿） | `e30c6798..13b2f409` | **无**（本轮全量审查） |

### 1.2 待回填核验（步骤 3.2：标记待回填的代码是否真的未实现）

**核验结论：全部属实，与计划标记一致，无谎报。**

- `/tui` 路由（`tui_route.go` L215-221）`LocalHandlers` 仅注册 5 个方法：`chat.send`/`chat.resume`/`chat.interrupt`/`chat.user_answer`/`history.get`——与设计文档附录 A 机器核对的差集完全一致（前向 80 / 无本地 75 / 已注册本地 5）。
- 27 个纯本地方法（`command.model` 1 个、`config.*` 3 个、`cron.job.*` 8 个、`history.list_turns`、`hooks.list`、`memory.*` 5 个、`models.list`、`session.*` 7 个；合计 27，与设计文档附录 A 机器核对清单逐项一致）**在 Go 侧确实未注册**，调用时落入 `gateway_server.go` L609 的 `unknown method: <method>` 分支——与 Python 侧行为一致的方向（Python 侧这些方法有完整本地实现），属计划内 ⤵️ 11.15 回填项，`IMPLEMENTATION_PLAN.md` 11.15 状态 🔄 与之自洽。
- Python 参照（`tui_connect.py` L1395-1444）的 5 个本地 handler 与 Go 的 5 个 ack handler 逐一对齐（含 `chat.interrupt` 的 intent 可选透传、`chat.user_answer` 的 request_id 可选透传）。

---

## 二、问题总览（步骤 4）

| 编号 | 级别 | 章节 | 位置 | 一句话描述 |
|------|------|------|------|-----------|
| S-01 | **严重** | 11.15 | `message_handler/forward_loop.go` L246-247 | stream 两个 map 在锁外裸写，与后台清理 delete 并发触发 Go runtime fatal |
| S-02 | **严重** | 11.15 | `message_handler/cancel.go` L145-165 | cancelStreamTask 先 delete 再 Wait，与 streamFinalCleanup 的守卫式 Done 互斥死锁 |
| S-03 | **严重** | 11.15 | `gateway/gateway_server.go` L281-287 | 定向发送命中 stale 连接时 Go 广播给同渠道其他客户端，Python 静默丢弃 |
| S-04 | **严重** | 13.24 | `retrieval/query_rewriter/query_rewriter.go` L152-155 | custom_headers 只读 Extra 漏声明字段，生产配置路径自定义请求头静默丢失 |
| M-01 | 一般 | 11.15 | `gateway/gateway_server.go` L470/494/500/516/609 | 内联错误帧恒带 `"payload": {}`，Python 对应帧无 payload 字段 |
| M-02 | 一般 | 11.15 | `gateway/gateway_server.go` L569-577 | on_message 回调异常 Go 捕获后继续，Python 异常传播导致连接失败 |
| M-03 | 一般 | 13.24 | `retrieval/query_rewriter/query_rewriter.go` L392-407/L414-420 | truncateForMsg 字节截断——CJK 边界产出无效 UTF-8 且 500 字节 ≠ Python 500 字符（项目第 7 次系统性风险）；另错误消息三分派收敛、Python JSONDecodeError 路径不截断 |
| M-04 | 一般 | 10.6.24c | `agents/harness/common/tools/user_todo_tool.go` L217/221/393/397/439 | status/priority 非法非空值不校验：Python Enum 构造抛 ValueError 拒绝，Go 裸 string 转换静默接受并可持久化坏值（create/update/fromDict 三处往返） |
| T-01 | 提示 | 11.15 | `message_handler/disconnect.go` L39-46 | 断连取消 stub 缺 `intent`/`mode` params 注入与 `ws_drop_` 前缀 ID |
| T-02 | 提示 | 13.23 | `retriever/graph_beam_search.go` L290-291 | 空 beam 的 RuntimeError 防御缺失（不可达死代码防御） |
| T-03 | 提示 | 13.24 | `retrieval/query_rewriter/repair.go` L41-47 | fillTemplate map 迭代序不确定 vs Python kwargs 有序（history→query），历史含字面 `{query}` 时 50% 概率不替换 |
| T-04 | 提示 | 13.24 | `query_rewriter.go` L228/L338 + `repair.go` L94 | 三处 json.Marshal 默认 HTML 转义（`<`→`\u003c`）vs Python `ensure_ascii=False` 不转义 |
| T-05 | 提示 | 13.24 | `query_rewriter.go` L335-353 | Rewrite 降级分支 catch-all vs Python `except BaseError`（行为等价但设计文档 §4.6 写 errors.As 与代码有出入）；Marshal 失败静默跳过写回（Python 不可达分支） |
| T-06 | 提示 | 10.6.24a | swarmtools 各工具 clampInt | clampInt 显式 0=默认值 vs Python 严格 clamp（max/min 边界行为差异，已归档项） |
| T-07 | 提示 | 10.6.24c | user_todo_tool cancel 文案 | cancel_session 已完成任务文案差异（Go 更合理但未归档进 spec 偏差表） |
| T-08 | 提示 | 10.6.24a | wiki_ingest | wiki_ingest JSON 键序差异 |
| T-09 | 提示 | 10.6.24a | web_fetch | web_fetch U+FFFD 严格解码近似（Python errors="strict" vs Go 容错解码） |

**对齐确认（无问题）部分**：80/75 方法集合逐项比对零差异；5 本地 handler + `build_cli_route_binding` 装配对齐；`RouteConfig` 字段语义对齐；`InboundGatewayServer` 对齐（满队列丢弃为已归档有意偏差）；forward_loop 主体步骤序列对齐；Python cancel 的 interrupt-then-cancel 顺序对齐；`NormalizeGatewayMessage` 对齐；web 渠道 9 个 schedule 转发方法对齐；13.21 HybridRetriever 三模式路由逐段对齐；13.22 三个数据模型（TripleBeam/TripleMemory/TripleFact/MultiKBRetrievalResult）对齐（含 Python 原有的去重键小写/展示保原样不对称的忠实复刻）；13.23 GraphRetriever 五层降级链/RRF 融合顺序 `[newChunks, chunks]`/mode 校验/graph_hops 默认值/Milvus `chunk_id IN [...]` 与 Chroma `{"$in": [...]}` 查询构造全部对齐；13.24 四个提示词文件逐字节一致、`GetBack(0, true)` 与 Python `get_messages(with_history=True)` 语义等价、schemaRepair 去 error 重构（`85b9cfcc`）正确、`_streaming` 透传修复（`13b2f409`）链路（deep_agent→controller→executor→react）全程贯通。

---

## 三、严重问题详述（步骤 5：Python 样例 / Go 问题 / 修复方案 / 流程示例）

### S-01【严重】streamEmitsProcessingStatus / streamModes 锁外裸写 → Go runtime fatal（concurrent map writes and map deletion）

**引入提交**：`da20b1a1`（2026-07-12，message_handler 完整对齐 Python _forward_loop 时引入），在 11.15 CLI 路由全链路集成测试（`13b2f409`）中首次被流量覆盖放大。

**Python 样例**（`jiuwenswarm/gateway/message_handler/message_handler.py` L2513-2518）：

```python
# 单线程 asyncio：事件循环内串行赋值，天然无并发问题
self._stream_tasks[stream_rid] = task
self._stream_sessions[stream_rid] = msg.session_id
self._stream_metadata[stream_rid] = msg.metadata
self._stream_emits_processing_status[stream_rid] = emit_processing_status
self._stream_modes[stream_rid] = (
    msg.params.get("mode", "plan") if isinstance(msg.params, dict) else "plan"
)
```

**Go 问题**（`internal/swarm/gateway/message_handler/forward_loop.go` L245-247）：

```go
mh.registerStreamTask(streamRid, msg.SessionID, msg.Metadata, entry)  // ← 这个持锁
mh.streamEmitsProcessingStatus[streamRid] = emitProcessingStatus      // ← 这两行不持锁！
mh.streamModes[streamRid] = mh.extractModeFromParams(msg)             // ←
```

`registerStreamTask`（L863-871）持 `streamMu` 写 `streamTasks`/`streamSessions`/`streamMetadata`，但紧接着的两行对 `streamEmitsProcessingStatus`/`streamModes` **裸写**。与此同时：

- `streamFinalCleanup`（L796-808）在 stream goroutine 退出时持锁 **delete 同两个 map**；
- `cancelStreamTask`（cancel.go L145-154）在取消时持锁 **delete 同两个 map**。

Go 的 map 并发"一写一删"是 runtime 级 fatal（`concurrent map writes and map deletion`），**不可 recover、直接崩进程**。

**并发流程示例**：

```
时刻 T0：用户 A 经 /tui 发起 chat.send（is_stream=true）
  forwardLoop goroutine：
    T1: registerStreamTask(rid)         [持锁 ✓]
    T2: streamEmitsProcessingStatus[rid] = true   [裸写 ✗]  ←── 与 T3 并发！
        streamModes[rid] = "code.normal"          [裸写 ✗]

时刻 T0'：同一 rid 的上一个请求恰在收尾（或用户 B 的取消请求到达）
  stream goroutine（defer streamFinalCleanup）：
    T3: streamMu.Lock(); delete(streamEmitsProcessingStatus, rid);
        delete(streamModes, rid); Unlock()  [持锁 delete]

T2 与 T3 竞争 → runtime fatal: concurrent map writes and map deletion
→ 整个 gateway 进程崩溃，所有渠道连接全断
```

**触发条件现实性**：CLI 快速连发消息（第二次 send 时第一次流恰在收尾）、或断连触发 `cancelStreamTask` 与新注册竞态——11.15 集成测试 `TestCLI路由全链路_*` 的多请求场景即在该窗口内，竞态概率随会话密度上升。

**修复方案**（最小改动，两 map 写点收编进锁内）：

```go
// 方案 A（推荐）：扩展 registerStreamTask 一次持锁写全部五个 map
func (mh *MessageHandler) registerStreamTask(requestID, sessionID string,
    metadata map[string]any, entry *streamTaskEntry,
    emitsProcessingStatus bool, mode string) {
    mh.streamMu.Lock()
    defer mh.streamMu.Unlock()
    mh.streamTasks[requestID] = entry
    mh.streamSessions[requestID] = sessionID
    if metadata != nil {
        mh.streamMetadata[requestID] = metadata
    }
    mh.streamEmitsProcessingStatus[requestID] = emitsProcessingStatus
    mh.streamModes[requestID] = mode
}

// forward_loop.go 调用点改为：
mh.registerStreamTask(streamRid, msg.SessionID, msg.Metadata, entry,
    emitProcessingStatus, mh.extractModeFromParams(msg))
```

注意 `extractModeFromParams` 读 `msg.Params`（消息本地数据，无共享），先在锁外求值再传参即可。同步删除 L262-266 处已加锁的并发计数读取上方不再需要的裸写。

**验证建议**：`go test -race -count=30 ./internal/swarm/gateway/message_handler/ -run TestStreamTask`（对齐项目既有 race 复现法），并在集成测试中加"流式进行中立即二次发送同 rid"场景。

---

### S-02【严重】cancelStreamTask delete-then-Wait × streamFinalCleanup 守卫式 Done → WaitGroup 死锁

**引入提交**：`d4146eb4`（2026-07-14，G15/G16/G17 修复时引入 wg.Wait 等待语义），`13b2f409` 集成测试中以测试侧 workaround 绕过而非修复。

**Python 样例**（`message_handler.py` `_cancel_agent_work_for_session`，gather 语义）：

```python
# Python: cancel 只 cancel task，task 退出时 finally 块统一清理 dict
for task in tasks:
    task.cancel()
if tasks:
    await asyncio.gather(*tasks, return_exceptions=True)  # 等 goroutine 退出
# process_stream 的 finally（L2618-2648）负责 pop 四个 dict——
# cancel 与 finally 串行于同一事件循环，不存在"先删导致 Done 丢失"
```

**Go 问题**（`cancel.go` L145-165 与 `forward_loop.go` L796-808 组合）：

```go
// cancel.go：先 delete 再 Wait
func (mh *MessageHandler) cancelStreamTask(requestID string) {
    mh.streamMu.Lock()
    entry, exists := mh.streamTasks[requestID]
    if exists {
        delete(mh.streamTasks, requestID)      // ← 先删除 entry
        delete(mh.streamSessions, requestID)
        ...
    }
    mh.streamMu.Unlock()
    if exists && entry != nil && entry.cancel != nil {
        entry.cancel()
        entry.wg.Wait()                        // ← 再等待
    }
}

// forward.go streamFinalCleanup：守卫式 Done——entry 已被删则跳过
mh.streamMu.Lock()
if entry, ok := mh.streamTasks[requestID]; ok {  // ← cancel 已 delete → ok=false
    entry.wg.Done()                               // ← Done 被跳过！
}
```

**死锁流程示例**：

```
时刻 T1：用户经 /tui 发 chat.interrupt（或 WebSocket 断连触发 disconnect.go 路径）
  cancel goroutine：
    T2: streamMu.Lock → 取 entry、delete map → Unlock
    T3: entry.cancel()                # 发出取消信号
    T4: entry.wg.Wait()               # 阻塞等待 Done ——①

时刻 T2'：stream goroutine 收到 ctx 取消
  T5: processStreamCore 返回 → defer streamFinalCleanup
    T6: streamMu.Lock → 查 streamTasks[rid] → 已被 T2 删除 → 不执行 Done
    T7: Unlock，streamFinalCleanup 返回，goroutine 退出

结果：T4 的 Wait 永远等不到 Done → cancel goroutine 永久挂死
  → chat.interrupt 无响应 / 断连清理 goroutine 泄漏
```

**测试侧的自供状态**（`cancel_test.go` L99-100、`message_handler_test.go` L89）：

```go
entry.wg.Done() // 模拟 goroutine 退出，避免 cancelStreamTask 中 Wait 死锁
mh.cancelStreamTask("req-1")
```

测试注释直白承认死锁存在并用"预 Done"绕过——生产路径无此保护。

**修复方案**（推荐 A：Done 与 map 状态解耦，"注册即负责退出"）：

```go
// 方案 A：streamFinalCleanup 增参 entry *streamTaskEntry，Done 无条件执行
// forward_loop.go L796-808 改为：
func (mh *MessageHandler) streamFinalCleanup(requestID, channelID, sessionID string,
    emitProcessingStatus bool, state *streamFinalState, entry *streamTaskEntry) {
    mh.streamMu.Lock()
    if _, stillRegistered := mh.streamTasks[requestID]; stillRegistered {
        delete(mh.streamTasks, requestID)      // 正常退出路径：自己清
        delete(mh.streamSessions, requestID)
        delete(mh.streamMetadata, requestID)
        delete(mh.streamEmitsProcessingStatus, requestID)
        delete(mh.streamModes, requestID)
    }
    // 已被 cancelStreamTask 删除时此处全部跳过——不影响下面的 Done
    mh.streamMu.Unlock()
    entry.wg.Done()   // 无条件：本 goroutine 注册时 Add(1)，退出必 Done(1)
    ...
}
// processStreamCore 的 defer 改传 entry（闭包本就持有）
// cancelStreamTask 保持 delete → cancel → Wait 不变（删除后 Wait 能等到无条件 Done）
```

守卫式 Done 的根因错误：把"是否 Done"耦合到"map 里是否还有我"——而 map 的删除权有两条路径（正常退出/取消），取消方先删就饿死了退出方的 Done。解法即所有权分离：**map 状态谁删都行，Done 只由注册-退出的 goroutine 恰好执行一次**（Add(1) 的镜像）。cancelStreamTask 从不 Done、只 Wait，与 Python `asyncio.gather` 语义对齐（等待方不替任务完成）。

**验证建议**：新增单测——注册真实 stream goroutine（内部 `select { case <-ctx.Done(): return }`），调用 `cancelStreamTask`，用 `done := make(chan struct{})` + `go func(){ cancelStreamTask(rid); close(done) }()` 断言 2 秒内返回。

---

### S-03【严重】定向发送命中 stale 连接：Go 广播给同渠道其他客户端，Python 静默丢弃

**引入提交**：`a07f3f68`（2026-10-25 审查修复中引入 probeWritable nil 重置），注释声称对齐 Python L428 实为误读。

**Python 样例**（`gateway/app_gateway.py` L412-451，`send` 方法）：

```python
if ws is None or bool(getattr(ws, "closed", False)):   # L428：命中但已关
    if ws is None:                                      # L429：仅"从未命中"才广播
        channel_id = ...
        if channel_id and channel_id != "acp" and msg.type != "res":
            clients = self._find_channel_clients(channel_id)
            if clients:
                ...  # 广播 fallback
                return
        logger.warning("message dropped: ...")
    return                                             # L451：ws 命中但 closed → 静默丢弃，绝不广播
```

Python 的语义区分两种"拿不到 ws"：
1. **从未命中**（`ws is None`）→ 广播 fallback（找渠道内其他客户端）；
2. **命中但连接已死**（`ws.closed`）→ **直接 return 丢弃**——因为路由键明确指向这个死连接代表的会话，消息不应被同渠道无关客户端收到。

**Go 问题**（`gateway_server.go` L281-287）：

```go
// 定向发送前探测连接存活性（对齐 Python L428 ws.closed 检查）：
// ...定向发送命中 stale 连接时 Python 视为未命中落入广播 fallback——
// 同 channel 其他在线客户端仍能收到消息       ← 注释错误转述 Python 行为
if ws != nil && !s.probeWritable(ws) {
    ws = nil                                            // ← 与未命中混同
}

if ws == nil {
    // 广播 fallback（对齐 L423-439）
    if msg.ChannelID != "" && ... {
        clients := s.findChannelClients(msg.ChannelID)
        ...  // 广播给同渠道全部客户端
```

probeWritable 失败（Ping 50ms 超时/写失败）后将 `ws` 置 nil，与"从未命中"走同一条广播路径。

**错误行为流程示例（跨客户端消息泄漏场景）**：

```
前提：渠道 tui 上有两个客户端连接
  C1：会话 S-A（requestToClient["tui:S-A-req"] → C1）
  C2：会话 S-B（sessionToClient["tui:S-B"] → C2）

T1：C1 的 TCP 半关闭（拔网线/杀进程），HandleConnection 尚未清理路由键
T2：S-A 的流式响应 chunk 到达 gateway 出站
T3：Python 语义：命中 C1 键但 C1 closed → 丢弃（S-A 会话死亡，chunk 随之沉没）
    Go 现状：  命中 C1 键但 probeWritable 失败 → ws=nil → 广播！
T4：C2 收到 S-A 会话的响应 chunk —— 无关客户端收到他人会话内容
```

影响面：所有经 `GatewayServer.Send` 出站的消息（机器人消息、processing_status、event 帧）在该窗口内都可能错投。对于 TUI 单用户多会话场景表现为"串会话输出"；对多客户端渠道则是消息泄漏。

**修复方案**（区分"未命中"与"命中已死"）：

```go
// gateway_server.go Send（L264 起）改造：
hitStale := false
if ws != nil && !s.probeWritable(ws) {
    hitStale = true   // 命中路由键但连接已死：对齐 Python L428-451 静默丢弃
}
if ws == nil && !hitStale {
    // 仅"从未命中"走广播 fallback（对齐 L423-439）
    ...
}
if hitStale {
    logger.Warn(logComponent).
        Str("channel_id", msg.ChannelID).
        Str("session_id", msg.SessionID).
        Str("id", msg.ID).
        Msg("message dropped: routed WebSocket client closed")
    return
}
```

同步修正 L281-284 的错误注释（Python 并无"stale 视为未命中广播"语义）。

**验证建议**：集成测试——同渠道双客户端（A/B），关闭 A 的底层 TCP（不触发 WS 关闭帧），向 A 的会话发消息，断言 B **未**收到且日志出现 dropped 记录；对照组：清空 A 的路由键后重发，断言 B 收到（真未命中仍广播）。

---

### S-04【严重】13.24 custom_headers 只读 Extra 漏声明字段——生产配置路径自定义请求头静默丢失

**发现方式**：13.24 专项审查（Q-01），主线程三处证据独立复核属实。

**Python 样例**：

```python
# mode_info.py L19：custom_headers 是 BaseModelInfo 的声明字段
custom_headers: Optional[dict[str, Any]] = Field(default=None)

# query_rewriter.py L263：getattr 读的就是声明字段
custom_headers=getattr(mi, "custom_headers", None),
```

**Go 问题**（`query_rewriter.go` L152-155）：

```go
var customHeaders map[string]string
if v, ok := mi.Extra["custom_headers"].(map[string]string); ok {
    customHeaders = v
}
```

Go 端 `BaseModelInfo` 同样有声明字段（`model_info.go` L37 `CustomHeaders map[string]string`），且 `custom_headers` 在 `baseModelInfoKnownKeys`（L228）中——JSON 反序列化把 custom_headers 路由进**声明字段**，`mi.Extra["custom_headers"]` 恒为空。断裂链：

```
生产路径（断）：
配置文件 custom_headers → json.Unmarshal → known key 拦截 → mi.CustomHeaders
→ QueryRewriter 只读 mi.Extra["custom_headers"] → nil → WithCustomHeaders(nil)
→ LLM 客户端请求头丢失（租户头/鉴权头等）→ API 侧鉴权失败或降级

测试路径（通，掩盖 bug）：
WithInfoExtra(map{"custom_headers": ...}) → Extra 命中 → 测试绿
（Python 测试 L109-128 走的是 BaseModelInfo(custom_headers=...) 声明字段——
 两边测试都绿但覆盖的是不同路径，Go 生产路径零覆盖且是坏的）
```

另注意：即便手工塞进 Extra，经 JSON 反序列化出的类型是 `map[string]any` 而非 `map[string]string`，类型断言同样失败——Extra 读取 essentially 是"测试专用通道"。根因是设计文档 §4.2 把 custom_headers 与 ssl_cert/verify_ssl 打包进 Extra 读取（后两者 Python 确实未声明、走 extra="allow"，处理正确），custom_headers 是声明字段被张冠李戴。

**修复方案**（对齐 `buildModelFromEntry` 的 custom_headers 处理先例，deep_adapter.go L2078-2089）：

```go
// 声明字段优先（Python getattr(mi, "custom_headers", None) 读的就是声明字段）
customHeaders := mi.CustomHeaders
if customHeaders == nil {
    // Extra 兜底（仅测试注入路径可达；JSON 路径下该键被 knownKeys 拦截进声明字段）
    if v, ok := mi.Extra["custom_headers"].(map[string]any); ok && len(v) > 0 {
        headers := make(map[string]string, len(v))
        for k, hv := range v {
            if s, ok := hv.(string); ok {
                headers[k] = s
            }
        }
        customHeaders = headers
    }
}
```

注意：当前 L153 的 `.(map[string]string)` 断言即使键存在也恒失败——`json.Unmarshal` 反序列化出的嵌套 map 恒为 `map[string]any`（"Extra 读取 essentially 测试专用通道"的第二重原因）。Extra 兜底须用 `map[string]any` 逐项转换（deep_adapter.go 先例）。

同时把 `TestNewQueryRewriter_配置透传` 的构造改为 `WithCustomHeadersForInfo(...)` 走声明字段（对齐 Python 测试路径），可保留 Extra 路径用例作为 Go 增强。

**验证建议**：新增用例——`json.Unmarshal` 一份含 custom_headers 的完整 model 配置 JSON → 构造 QueryRewriter → 断言客户端配置中请求头非空（锁死生产路径）。

---

## 四、一般问题详述

### M-01【一般】内联错误帧恒带 `"payload": {}`，Python 对应帧无 payload 字段

**Python 样例**（`app_gateway.py` 四处内联错误帧，L569/L601/L613/L632/L717）：

```python
await ws.send(json.dumps(
    {"type": "res", "id": "", "ok": False, "error": "invalid json"},  # 无 payload 键
    ensure_ascii=False,
))
# invalid request / unknown method: {method} 同样只有 type/id/ok/error 四键
```

注意 Python 的 `send_response`（L332-356，本地 handler 错误路径）**有** `payload or {}`——Python 自身就是两套帧形状：内联帧无 payload、handler 错误帧有 payload。

**Go 问题**（`gateway_server.go`）：`invalid json`（L470）、`invalid request`（L494、L500）、`unknown method`（L516、L609）全部经 `sendResponse` → `writeResponseFrame`（L635-643），后者统一 `if payload == nil { payload = map[string]any{} }` 后放入帧——内联帧也带上了 `"payload": {}`。

**影响**：协议形状偏离。严格按 Python 帧键集解析的客户端（如逐键 diff 的兼容性测试）会看到多余字段；宽松客户端读取 `error` 字段不受影响。不破坏功能流，定级一般。

**修复方案**：`writeResponseFrame` 增加 `includePayload bool` 参数（或拆出 `writeInlineErrorFrame`），内联四处传 false、`sendResponse`（handler 错误路径）保持 true：

```go
func writeResponseFrame(ws *websocket.Conn, reqID string, ok bool,
    payload map[string]any, errMsg, code string, includePayload bool) {
    frame := map[string]any{"type": "res", "id": reqID, "ok": ok}
    if includePayload {
        if payload == nil { payload = map[string]any{} }
        frame["payload"] = payload
    }
    ...
}
// 内联：s.sendResponseInline(ws, "", false, "invalid json") → 无 payload
// handler 错误：sendResponse(...) → 有 payload（对齐 Python send_response）
```

### M-02【一般】on_message 回调异常语义：Go 捕获继续，Python 传播致连接失败

**Python 样例**（`app_gateway.py` L674-677）：

```python
if self._on_message_cb is not None:
    result = self._on_message_cb(msg)
    if asyncio.iscoroutine(result):
        await result        # ← 无 try/except：异常向上传播
# 传播路径：_handle_raw_message → _connection_handler 的 async for →
# 非 ConnectionClosedError → 逃出 handler → websockets 框架关闭该连接
```

**Go 问题**（`gateway_server.go` L569-577）：

```go
if s.onMessageCb != nil {
    if err := s.onMessageCb(ctx, msg); err != nil {
        // 对齐 Python await 无 except——回调异常仅记录不中断   ← 注释与 Python 相反
        logger.Warn(logComponent).Err(err)...
    }
}
```

注释称"对齐 Python await 无 except"但实际行为相反：Python 无 except 意味着异常**炸掉连接**（消息处理链路故障时快速失败），Go 则 Warn 后继续走 no-local-handler return，连接保持。

**影响**：入站管线（`InboundGatewayServer.HandleMessage`，即 normalize+forward 链）持续报错时，Python 客户端会感知连接断开并重连/报错，Go 客户端表现为"消息静默黑洞"。语义分歧不改变正常流程，定级一般。

**修复方案**：二选一并归档决策——
- A（严格对齐）：onMessageCb 出错时关闭该 ws（对齐 Python 连接失败语义）；
- B（有意偏差归档）：保持 Go 容错但在注释中如实记录"有意偏离 Python 传播语义，理由：Go 侧连接归 gateway 生命周期管理，单消息回调失败不应断链"，并补 `error` 级别日志（当前 Warn 弱于 Python 的隐式 error）。

---

### M-03【一般】13.24 truncateForMsg 字节截断（CJK 风险第 7 次）+ 错误消息三分派收敛

**Python 样例**（`query_rewriter.py` L500-525 rewrite 尾部，compress L385-410 同构）：

```python
if not isinstance(rewrote_json, dict):
    raise build_error(
        ...,  # 分支 1："parse returned non-dict; content: {content_json[:500]!r}" —— 按字符截断 500
    )
except json.JSONDecodeError as e:
    raise build_error(
        ...,  # 分支 2："is not valid JSON: {e}; content: {content_json}" —— 完整不截断！
    )
except Exception as e:
    raise build_error(
        ...,  # 分支 3："parsing failed: {e}; content: {content_json[:500]!r}" —— 字符截断 500
    )
```

**Go 问题一（字节截断）**（`query_rewriter.go` L414-420）：

```go
func truncateForMsg(s string, n int) string {
    if len(s) <= n { return s }
    return s[:n]   // ← 字节切片：中文内容只截 ~166 字符；
                   //   第 500 字节落在 CJK 3 字节中间时产出无效 UTF-8 尾巴（乱码）
}
```

Python `[:500]` 是 code point 语义。这是本项目 CJK 截断系统性风险的**第 7 次出现**——task_loop handler（旧 `s[:500]` 已修，handler_test.go:1169 注释明确记录"中文 3 字节边界会截出半个字符"）、matts.go、Viking truncStr 等前 6 处均已修复为 rune 语义，本处为漏网。

**Go 问题二（三分派收敛）**（L392-407，Compress 链同构）：`parseLLMJSON` 失败 → `jsonOutputParser.Parse` 兜底失败后统一落"non-dict"文案——丢失 Python 分支 2 的"具体解码错误 + 不截断完整内容"信息。错误码一致（155604）、控制流一致，仅 error_msg 形状偏离。

**修复方案**：

```go
// 问题一：rune 截断（对齐 Python str 切片语义，task_loop 先例）
func truncateForMsg(s string, n int) string {
    r := []rune(s)
    if len(r) <= n { return s }
    return string(r[:n])
}

// 问题二：parser 返回错误时区分文案（若严格对齐）：
//   解码错误 → "is not valid JSON: %v; content: %s"（不截断）
//   泛型错误 → "parsing failed: %v; content: %s"（rune 截断 500）
// 或在 spec §8 偏差归档"三分派收敛为单一 non-dict 文案"
```

错误路径专用、不影响主流程，故定级一般（问题一）+ 提示（问题二，随本条合并记录）。

### M-04【一般】10.6.24c user_todos status/priority 非法值不校验——Python Enum 构造即拒绝 vs Go 裸转换静默接受

**Python 样例**（`jiuwenswarm/agents/harness/common/tools/user_todo_tool.py`）：

```python
class TodoStatus(str, Enum):
    PENDING = "pending"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    CANCELLED = "cancelled"

# L283（create）：非法值 → ValueError 直接抛出，工具报错
status=TodoStatus(params.status) if params.status else TodoStatus.PENDING,
# L315-317（update）：同样构造即校验
todo.status = TodoStatus(params.status)
# L82-83（fromDict 持久化往返）：坏值文件加载也会抛
status=TodoStatus(data.get("status", "pending")),
```

Python `Enum(x)` 构造对非成员值抛 `ValueError`——非法 status/priority 在**入口即被拒绝**。

**Go 问题**（`user_todo_tool.go` L217/221（create）/L393/397（update）/L439（往返））：

```go
status = TodoStatus(s)      // 裸 string 转换：任意字符串静默通过
priority = TodoPriority(p)
```

`type TodoStatus string` 类型别名使 `TodoStatus(" bananas ")` 合法——非法值进入 TodoItem 后可**持久化到 markdown 文件**，下次往返仍不报错，坏值扩散；Python 侧同文件会在 fromDict 抛错暴露问题。

**系统性模式（建议纳入后续审查清单）**：Python `Enum(x)` 构造即校验 vs Go `MyType(x)` 裸转换不校验，是跨移植的**系统性盲区**（与既有记忆"Supports 有 HTTP 门卫、parse 无门卫"同源）。凡涉及 Python Enum → Go string 枚举移植，必查非法值路径（构造/解析/往返三处）。

**修复方案**：为 TodoStatus/TodoPriority 增加 `Valid() bool` 或 `ParseTodoStatus(s) (TodoStatus, error)` 门卫，三处转换点替换：

```go
func parseTodoStatus(s string) (TodoStatus, error) {
    switch TodoStatus(s) {
    case TodoStatusPending, TodoStatusInProgress, TodoStatusCompleted, TodoStatusCancelled:
        return TodoStatus(s), nil
    }
    return "", fmt.Errorf("invalid todo status: %q", s)  // 对齐 Python ValueError 语义
}
```

注意 L434 注释已归档"空串跳过"（偏差 #8，对齐 Python `TodoStatus("")` 抛错的绕开）——空串路径保持现状，**非空非法值**是本条要补的门卫。修复后补测试：非法 status 调用 create/update 报错、坏值文件往返报错。

---

## 五、提示问题详述

### T-01【提示】断连取消 stub 缺 `intent`/`mode` params 注入与 `ws_drop_` 前缀 ID

**Python 样例**（`message_handler.py` L544-562）：
```python
disconnect_params = {"intent": "cancel", "session_id": sid}
disconnect_state = self._channel_states.get(...)      # 注入 mode
if disconnect_state is not None:
    disconnect_params["mode"] = disconnect_state.mode.value
stub = Message(
    id=f"ws_drop_{int(time.time() * 1000):x}_{secrets.token_hex(4)}",  # 可识别前缀
    ...
    params=disconnect_params, ...
)
```

**Go 现状**（`disconnect.go` L39-48）：cancelMsg 无 Params（`intent`/`mode` 均未注入）、ID 直接用 sid。

**影响评估**：`CancelAgentWorkForSession` → `buildCancelMessage`（cancel.go L171-194）会从 channelStates 补 mode，`deep_adapter.go` L1354-1372 的 intent 默认即 "cancel"——功能等效，仅丢失 `ws_drop_` 可观测性前缀（排查断连取消日志时无法区分来源）。定级提示。

**修复方案**：构造 stub 时注入 `Params: {"intent": "cancel", "session_id": sid}`，ID 改为 `fmt.Sprintf("ws_drop_%x_%s", time.Now().UnixMilli(), randomHex4)`。

### T-02【提示】13.23 searchCandidates 缺空 beam RuntimeError 防御（不可达）

**Python 样例**（`graph_retriever.py` L172-174）：

```python
async def _search_candidates(self, beam: TripleBeam) -> List[RetrievalResult]:
    if len(beam) < 1:
        raise RuntimeError("unexpected empty beam")   # 会被 graph_expansion 的 except 降级捕获
```

**Go 现状**（`graph_beam_search.go` L290-291）：直接 `tail := beam.Get(-1)`，空 beam 时 `idx += len` 后 `b.beam[-1]` panic 且无 recover → 进程崩溃（Go panic 不等价 Python 异常传播，graph_expansion 的 Warn 降级拦不住）。

**影响评估**：beams 恒非空（初始 topk 来自非空 triples；扩展轮 texts 空 → 原样返回非空 beams），属死代码防御缺失。定级提示。

**修复方案**：`searchCandidates` 开头加 `if beam.Len() < 1 { return nil, fmt.Errorf("unexpected empty beam") }`（错误冒泡至 BeamSearch → GraphExpansion 的 Warn 降级，对齐 Python RuntimeError 被 except 捕获降级的最终效果）。

### T-03【提示】13.24 fillTemplate map 迭代序不确定 vs Python kwargs 有序

**Python 样例**（`query_rewriter.py` L32-41）：

```python
def _fill_template(template: str, **kwargs) -> str:
    out = template
    for key, value in kwargs.items():   # kwargs 按声明序：history → query
        out = out.replace("{" + key + "}", value)
    return out
```

调用处恒为 `_fill_template(prompt_template, history=history_text, query=query)`——history 先替换（L476-480）；Compress 侧单 key（L367 `history=raw_text`）。

**Go 问题**（`repair.go` L41-47）：`for key, value := range kv`（map）迭代序随机——50% 概率 query 先替换，此时 history 值中带入的字面 `{query}` 占位符**不会被二次替换**（Python 语义下 history 先替换，其值中的 `{query}` 会被 query 值替换掉）。触发条件：用户对话历史含字面 `{query}` 字符串（罕见但对话内容不可控，例如用户在讨论模板语法本身）。Compress 单 key 无此问题。

**修复方案**：改为有序替换（固定先 history 后 query，或切片二元组），一比一对齐 Python kwargs 序。

### T-04【提示】13.24 三处 json.Marshal 默认 HTML 转义 vs Python ensure_ascii=False

**Python 样例**：`json.dumps(..., ensure_ascii=False)`（query_rewriter.py L343/L83/L452）不转义 `<` `>` `&`，分隔符 `", "`/`": "`。

**Go 问题**：`json.Marshal`（query_rewriter.go L228 多模态 Parts / L338 compressed_history 写回 / repair.go L94 forceString）默认把 `<`→`\u003c` 等转义、分隔符 `,`/`:`。多模态 content 或压缩 summary 含 HTML 字符时，进入 LLM 提示词/历史写回的字面内容与 Python 不同（LLM 会看到 `\u003c` 字面序列）。触发路径低频（多模态历史/非 str 字段值），定级提示。

**修复方案**：封装 pyJSONMarshal 助手（`json.Encoder` + `SetEscapeHTML(false)`）统一三处；分隔符差异可一并处理或归档。

### T-05【提示】13.24 Rewrite 降级 catch-all + Marshal 失败静默跳过写回（文档-代码出入）

**Python 样例**（query_rewriter.py L450-467）：`except BaseError` 降级，其他异常向上传播。

**Go 现状**（query_rewriter.go L335-353）：`cerr != nil` 捕获一切降级。Compress 错误路径（155603/155604/155605）全为 BaseError，无现实非 BaseError 路径，**行为等价**；但设计文档 §4.6 写的是 `errors.As BaseError`，代码是裸 catch-all——文档与代码有出入。附带：Go `if merr == nil` 才写 compressed_history，Marshal 失败时既不写压缩也不走降级（历史保持全量）；Python dumps 对 dict 不会失败，该分支不可达。

**修复方案**：无需改代码（或按设计文档补 errors.As 严格化）；将两点补进设计文档 §8 偏差表保持文档-代码一致。

### T-06 ~ T-09【提示】10.6.24a/c 审查四项提示级差异

以下四项来自 10.6.24a/b/c 三章专项审查（12+11 项既有偏差全部核实成立之外的新发现），均为不影响主流程的边界差异，合并记录：

| 编号 | 位置 | 差异 | 处置建议 |
|------|------|------|----------|
| T-06 | swarmtools clampInt | 显式 0=默认值 vs Python 严格 clamp（max/min 边界行为差异） | 归档进对应 spec 偏差表 |
| T-07 | user_todo_tool cancel_session | 已完成任务的取消文案差异（Go 措辞更合理） | 归档进 spec §7（24c 偏差表已有 12 项，补第 13 项） |
| T-08 | wiki_ingest | JSON 键序差异 | 归档（字节级一致性范围外） |
| T-09 | web_fetch | U+FFFD 严格解码近似：Python `errors="strict"` vs Go 容错解码 | 归档；若修复需评估 io.UTF8Error 传播链 |

---

## 六、复核确认清单（无问题项）

| 复核项 | 结论 | 证据 |
|--------|------|------|
| 80 前向 / 75 无本地方法集合 | 零差异 | `tui_route.go` 两 map vs Python `CLI_FORWARD_*` 逐项 diff，空对称差 |
| 5 本地 handler ack 形状 | 对齐 | `tui_route.go` L215-279 vs `tui_connect.py` L1395-1444 |
| `/tui` 装配 + 出站注册 + on_message 全局回调 | 对齐 | `app_gateway.go` L415-439（`ff24b7ba`）vs `app_gateway.py` L1045-1069 |
| RouteConfig 字段语义 | 对齐 | `route_config.go` vs Python dataclass |
| 13.21 HybridRetriever 三模式路由/rsr/Close | 对齐 | 前轮已审 + 本轮主线程抽查 |
| 13.22 TripleBeam 判重集合/负索引/浅拷贝密封 | 对齐 | `triple_beam.go` vs `triple_beam.py`（本轮全读） |
| 13.22 TripleMemory lower 去重键/TriplesStr 保序 | 对齐（含 Python 原有小写不对称的忠实复刻） | `triple_memory.go` vs `triple_memory.py`（本轮全读） |
| 13.22 MultiKBRetrievalResult 字段形状 | 对齐 | `result.go` L44-58 vs `retrieval_result.py` L33-41 |
| 13.23 五层降级链（空 chunks sparse 回落→fetchTriples 吞错→空 triples 原样→beam 失败降级→RRF） | 对齐 | `graph_retriever_impl.go` L250-379 vs `graph_retriever.py` L435-526 |
| 13.23 RRF 融合列表顺序 `[newChunks, chunks]` | 对齐 | impl L374 vs py L522 |
| 13.23 mode 校验/threshold 仅 vector/graph_hops 默认 2 | 对齐 | impl L203-221 vs py L380-433 |
| 13.23 Milvus `chunk_id IN [...]` / Chroma `$in` 查询构造 | 对齐 | impl L561-581 vs py L620-813 |
| 13.24 提示词 4 文件 | 逐字节一致 | md5/diff 双验证（10942247…/a54a9ff1…/59abacba…/228b6c5b…） |
| 13.24 GetBack(0, true) 全量历史语义 | 等价 Python `get_messages(with_history=True)`；GetBack(N)=尾部 N 条 = Python `[-N:]` | `message_buffer.go` L55-114 |
| 13.24 schemaRepair 去 error 重构（`85b9cfcc`） | 正确——Python 双 raise 路径（非 dict 入参/未知 expected_type）在 Go 均编译期消解（调用方双重 dict 保证 + 未导出三值枚举全覆盖） | `85b9cfcc` diff 审查 + 专项验证 |
| 13.24 状态码 4 个 / 构造函数（compressRange 钳制、verify_ssl 无证书强制 false、temperature=0.0）/ LoadTemplate 缓存 / Msg2Text role 格式 / Compress 主链 | 逐项对齐 | 13.24 专项审查（Q-01 之外全部一致） |
| 13.24 compressed_history/original_history 双通道降级 + 尾部截断取历史 + 8 字段 schemaRepair | 对齐 | query_rewriter.go L333-409 vs py L447-508 |
| 11.15 `_streaming` 透传修复 | 链路贯通 | `13b2f409`：deep_agent→controller→executor→react 逐层核对，对齐 Python L202 硬编码 True |
| 10.6.24c cancel_session per-task done channel 修复（`d84dfaf5`） | 落地正确（tasks map 存 handle{cancel,done}，CancelSession 只等单个任务，对齐 Python gather 单任务语义）+ 并发测试毫秒级返回 | `d84dfaf5` diff 审查 |
| 10.6.24a/b/c 三章既有偏差归档（spec §7/§8 共 12+11 项） | 全部核实成立 | 三章专项审查 |
| 10.6.24b send_file 令牌载荷/HMAC/quoteAll | 字节级对齐（前轮闭环 `df6596af`/`f75c952e`） | 前轮审查记录 |
| 27 个待回填方法 | 属实未实现，与计划标记自洽 | `tui_route.go` L215-221 + `gateway_server.go` L609 unknown method 分支 |

---

## 七、处置建议优先序

1. **S-01 / S-02 同修**（同文件族、同测试场景验证）：forward_loop.go + cancel.go + 两处测试 workaround 删除，`-race -count=30` 回归；
2. **S-03**（gateway_server.go 出站路由分支 + 注释修正 + 双客户端集成测试）；
3. **S-04**（query_rewriter custom_headers 声明字段优先双读 + 测试路径矫正为 WithCustomHeadersForInfo + 新增 JSON 反序列化生产路径用例）；
4. **M-03 问题一**（truncateForMsg 改 rune 截断，与 task_loop 先例一致，CJK 风险第 7 次收口）；
5. **M-04**（user_todo_tool 加 parseTodoStatus/parseTodoPriority 门卫，三处转换点替换 + 非法值测试）；
6. M-01 / M-02（帧形状与回调语义，建议与 S-03 同提交链处理 gateway 侧）；
7. T-01 / T-02 / T-03 / T-04（随手修复）；T-05 ~ T-09 归档进对应 spec 偏差表。

---

## 八、系统性模式备忘（供后续审查沿用）

1. **CJK 截断**（第 7 次）：`s[:n]` 字节切片 vs Python `[:n]` 字符切片——凡新移植的错误消息截断，一律 rune 语义；
2. **Enum 裸转换**：Python `Enum(x)` 构造即校验 vs Go `MyType(x)` 静默通过——凡 Python Enum → Go string 枚举，必查构造/解析/往返三处门卫（与"Supports 有 HTTP 门卫、parse 无门卫"既有记忆同源）；
3. **JSON HTML 转义**：Go `json.Marshal` 默认转义 `<>&` vs Python `ensure_ascii=False`——需字面一致处用 `json.Encoder` + `SetEscapeHTML(false)`（web_file_download.go:217 已有 marshalCompactNoEscape 先例可复用）；
4. **声明字段 vs Extra 双读**：Python `getattr` 读声明字段、pydantic extra 只收未声明键——Go 已知字段被塞进 Extra 读取即断生产路径（本报告 S-04 与 10.6.28 审查 custom_headers 顺手修同源）。

> 修复后按项目规则 6 提交远端并确认 CI 四 job 全绿（Build/Lint/UnitTest/IntegrationTest），Integration Test job 需含 `13b2f409` 引入的 CLI 路由全链路套件。
