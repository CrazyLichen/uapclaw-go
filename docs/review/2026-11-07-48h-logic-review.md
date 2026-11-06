# 2026-11-07 48 小时上库质量审查

> 审查范围：`a3a8a21e .. HEAD(0ac59fe9)`（2026-11-06 02:31 ~ 2026-11-07 03:09，115 个提交；上轮审查截止到 `a3a8a21e`，本轮的增量即这 115 个提交）
> 代码规模：非测试 Go 文件 +11,438 / −1,504，测试文件 +11,470 / −1,944（其余为 docs 计划与豁免登记）
> 审查对象：提交原因与实现正确性；对齐 Python（`jiuwenswarm` 0.2.5 产品层 / `openjiuwen` agent-core 库层）；桩与回填完整性；层级与步骤归属；稳定性、性能、可维护性、并发安全
> 审查方式：逐批读生产代码 + 对照 Python 同位置源码；关键结论均在本地读码复核，下文 `文件:行号` 以 HEAD `0ac59fe9` 为准
> **只读审查，未修改任何代码**

---

## 零、结论摘要

| 级别 | 数量 | 一句话 |
|---|---|---|
| 严重 | 3 | 崩溃重试没有「只重试一次」的护栏，确定性故障会无限循环；崩溃重试前没重置 coordinator，下一轮的续跑判断会读到永久 aborted 状态；TeamHarness 停机时不提交子会话，session 状态可能丢 |
| 一般 | 5 | 控制结果判定漏了 cooperative-stop 这一半（回填时必踩）；`detectTaskFailed` 里有一段永远走不到的死分支；上下文用量的 request_id 是纳秒时间戳伪造的、丢了请求关联语义；会话级 KV cache 聚合写成了无状态函数且没接进链路；`nativeSessionID` 只写不读 |
| 提示 | 3 | abort 的「有界等待 5 秒」给 Stop 留了并发窗口；crash 重试用 `active.Query` 而不是 `original_query`，字段语义被悄悄合并；若干「对齐但当前不可达」的分支没有登记进偏差表 |

**先说清楚这批里做得好的部分**（避免只看问题）：

1. **goal 域模型（9.88）是这轮对齐质量最高的部分**。`manager.go` 604 行 / Python 410 行，`schema.go`/`evaluation.go`/`store.go` 四个文件与上游四个 `.py` 一一对应（规则 9.1）。锁语义（`controlMu` 与监督器共用、带 `Locked` 后缀的方法约定调用方已持锁）、代际校验（`matchesActive`/`matchesInFlight` 对 goalID+revision 双重校验）、记账（`AccumulateUsage` 照抄上游「只 save 不 commit」、`BeginAttempt`/`ApplyAssessment` 才 commit）、评估三策略 + JSON 围栏兜底 + 硬上限，逐条对得上且注释都标了 Python 行号。`TaskView` 归属 manager.go 而非 schema.go 的循环依赖规避（spec D4）判断准确。
2. **ModelAnomalyDetectionRail（9.86）并发安全到位**。`mu sync.RWMutex` 保护两张会话表，注释明确「同一 rail 实例会被同一 channel 下多个会话共享」这个 Go 独有、Python 单线程没有的并发场景；`funcPtrEqual` 去重、异常链解析（最多 16 层防环）、`coerceStatusCode` 把 bool 排除、工具循环压缩的「背靠背相邻性 + 指纹排序」判定，都是实打实的防御，不是走过场。
3. **context 压缩比例化（137c7d4f）的删除很干净**。删 rail 层自研 `applyCompressionRatios`/`shrinkThreshold`、删 `ContextEngineConfig.compression_trigger_ratio/target_ratio`、删 `toRatio`，一次改动里把自研水位机制彻底移除（规则 8），而不是留兼容壳。`round_level_compressor.go` 五级降级链、工具边界保护、`hasCompressionBenefit`（防「压缩完还是满」的反复压缩）都齐。
4. **团队流程断链修复（af7e44ad）本身是对的**。kernel.Start 补 harness.Start + sc.Start、runRound 补写轮次结果、isStreaming 对齐 Python 硬编码 true，这三处确实是 25.1a 重构后断链的根因，修复方向正确，提交说明也把「为什么断」讲清楚了。

**这批最需要警惕的模式**：`settleRoundDone` 这一处——它声称「完整对齐上游决策优先级」，但实际上把 Python 崩溃重试里「reset coordinator + failure_retry 只重试一次」两个关键护栏都丢了，只抄了「重试」这个动作本身。这与上一轮 `e0ed3e69`「修 A 却删掉 A 需要的防护」是同一类问题：对齐时把「做事的步骤」抄过来了，把「让这件事不失控的约束」漏掉了。

---

## 一、严重（S）

### S-01 崩溃重试没有「只重试一次」护栏，确定性故障会无限循环

**这是哪条流程**：成员脑（NativeHarness）跑一轮的收尾决策——`runRound` 提交轮次后拿到结果/错误，回投 `cmdRoundFinished`，由 supervisor 的 `settleRoundDone` 决定「下一轮跑什么还是回到 IDLE」。

**缺陷本身**：Python 的 `_settle_round_done`（`native_harness.py:1175-1197`）在 `runtime_crashed` 分支靠 `active.failure_retry` 标志保证「崩溃只重试一次，第二次崩溃就放弃、大声失败」：

```python
if replayable and not active.failure_retry:
    nxt = self._start_round(active.original_query, _StartMode.FAILURE_RETRY)  # 重试
    ...
logger.warning("...giving up on its query and going idle")
await self._transition(HarnessState.IDLE)  # 第二次崩溃：放弃
```

而 `failure_retry` 是在 `_start_round` 里按 `mode is _StartMode.FAILURE_RETRY` 置位的（`native_harness.py:1337`）。

Go 侧 `internal/agent_teams/harness/state.go` 的 `ActiveRound` 结构**根本没有 `failure_retry` 字段**，`settleRoundDone`（`native_harness.go:626-634`）里 `runtimeCrashed` 时无条件：

```go
if runtimeCrashed {
    if replayable, ok := active.Query.(string); ok && replayable != "" {
        nxt := h.startRound(ctx, active.Query)  // 无条件重试
        ...
        return
    }
    h.transition(ctx, HarnessStateIdle)
    return
}
```

结果：只要 `SubmitRound` 持续返回同一个确定性错误（例如 scheduler 已 Stop、session 已解绑后仍被驱动），每轮 `runtimeCrashed` 都为真，`settleRoundDone` 就再 `startRound` 一次，**无限循环**，每圈还伴随 `coordinator.IncrementIteration()` 递增。Python 用「第一次重试、第二次放弃」这个护栏切断了这个环，Go 侧把它丢了。

**影响面**：确定性故障（不是偶发网络抖动）下，成员脑会空转——不产出、不报错、不回到 IDLE，团队流程卡死。这正是 failure_retry 要防的场景。

**建议**：给 `ActiveRound` 补 `failure_retry`（或等价的一次性标记），重试轮次带上该标记；第二次 crash 走「放弃、大声失败」分支。建议同步登记进 25.1a spec 的偏差表（当前偏差表里没有这条）。

### S-02 崩溃重试前没重置 coordinator，下一轮续跑判断读到永久 aborted 状态

**这是哪条流程**：同上，`settleRoundDone` 的崩溃重试路径。

**缺陷本身**：Python 在 crash 分支**先** `self._reset_coordinator()`（`native_harness.py:1176`），注释说得很清楚：`executor.cancel` 会经 `coordinator.request_abort()` 把 `is_aborted` 置真，不重置的话「下一轮的 continuation 检查会看到一个永久 aborted 的 coordinator」。`_reset_coordinator` 就是 `coordinator.reset()`（`native_harness.py:1647-1656`）。

Go 侧 `settleRoundDone`（`native_harness.go:625-634`）的 crash 分支**没有 `coordinator.Reset()` 这一步**。重试的新轮次跑完后，`settleRoundDone` 里 `if resultType == "interrupt" || h.coordinator.IsAborted()`（`native_harness.go:642`）这一判断——如果上一轮 cancel 的副作用把 `IsAborted()` 带真了，新轮次即使正常跑完也会被当成「用户中止」直接回 IDLE，续跑（follow-up / 剩余任务）全部被跳过。

**影响面**：crash 重试这条链路的后续轮次调度被静默破坏——重试成功了，但该续的 follow-up、该继续的任务计划都被误判为 aborted 而中断。

**建议**：crash 重试前补 `coordinator.Reset()`，与 Python 对齐。

### S-03 TeamHarness.Stop 不提交子会话，session 状态可能丢

**这是哪条流程**：团队成员的 run cycle 收尾——`TeamHarness.Stop` 停 native supervisor 后清理。

**缺陷本身**：Python `team_harness.py:159-181` 的 `stop()` 明确有一句：

```python
try:
    if self._active_agent_session is not None:
        await self._active_agent_session.commit()
```

提交子会话（把本轮写进 session 的 task plan / history / plan mode 等状态落盘）。Go 侧 `team_harness.go:493-502` 的 `Stop()` 只有：停 native → `nativeSessionID = ""` → `setActiveSession(nil)`，**没有 `child.Commit(ctx)`**。

`Session.Commit` 在 Go 侧是存在的（`internal/agentcore/session/agent.go:406`），不是「没这个能力」，是「没接」。

**影响面**：跨 run cycle 的状态（task plan、plan mode、history）依赖「子会话的持久化 id 共享」来恢复（Python `team_harness.py:143-144` 注释原话），不 commit 可能导致本轮状态丢在内存里，下一 cycle 或进程重启后恢复不到。具体影响取决于底层 session 的 commit 语义（有的 session 每次写即落盘、commit 是 no-op），需要确认 `AgentTeamSession`/`CreateAgentSession` 的 commit 是否必要。但就「对齐」而言，这一步是缺的。

**建议**：确认 `CreateAgentSession` 产出的子会话是否依赖显式 commit 持久化；若是，补 `commit`，并在异常时吞错（对齐 Python 的 try/except 语义）。

---

## 二、一般（M）

### M-01 控制结果判定漏了 cooperative-stop 这一半，回填时必踩

**这是哪条流程**：`runRound` 拿到结果后，判断这个结果是「真实答案」（要写流、发 chat.final）还是「控制信号」（不能写流）。

**缺陷本身**：Python `_is_control_result`（`native_harness.py:1484-1494`）是**两个条件**：

```python
return bool(result.get("error")) or result.get("result_type") == COOPERATIVE_STOP_TYPE
```

Go 侧 `isControlResult`（`native_harness.go:713-723`）只判断了 `error` 非空，**漏掉了 `result_type == COOPERATIVE_STOP_TYPE` 这一半**。注释里自己承认「25.1a 阶段无 pause/graceful-abort，cooperative-stop 分支不可达，仅判断 error 非空」。

问题在于：这是「当前不可达、回填时必改」的隐患。25.1c 回填 pause/graceful-abort 后，cooperative-stop 载荷会开始出现，如果那时忘了同步补 `isControlResult` 的第二个条件，cooperative-stop 载荷会被当成真实答案写进流、发 chat.final，前端把「协作暂停」渲染成「回答完成」。这种「依赖章节回填后某处旧判断忘了同步改」正是这次审核要盯的第 2 类问题。

**建议**：现在就把 `isControlResult` 的第二条件补上（哪怕当前恒 false，也要让判定结构和上游一致），或至少在 25.1c spec/计划里显式登记「回填时须补 `isControlResult` 的 cooperative-stop 分支」。

### M-02 `detectTaskFailed` 里有一段永远走不到的死分支

**这是哪条流程**：StreamController 转发输出时，识别 `task_failed` 错误块、驱动瞬时重试。

**缺陷本身**：`internal/agent_teams/agent/stream_controller.go:620-655` 的 `detectTaskFailed`，第 623-634 行已经 `payload.(*cschema.ControllerOutputPayload)` 断言并 `return` 了；紧接着第 638-652 行又写了一段 `if cop, ok := payload.(*cschema.ControllerOutputPayload); ok`——但走到这里时 `payload` 已经不可能是这个类型（上面分支已 return），这段是**永远不可达的死代码**。疑似 S-03 直读分支修复（补 `*ControllerOutputPayload` 断言）时，没删掉原来那段「结构体断言」的旧逻辑，两段并排残留。

**影响面**：不影响正确性（死代码），但制造「这里有两种处理」的假信号，后续维护容易改错分支。违反规则 8（删除残留）的精神。

**建议**：删掉第 638-652 这段不可达的 `*ControllerOutputPayload` 分支，只保留 map 分支。

### M-03 上下文用量的 request_id 是纳秒时间戳伪造的，丢了请求关联语义

**这是哪条流程**：`emitContextUsage` 发 `context.usage` 快照（前端用它渲染上下文占用指示器）。

**缺陷本身**：`stream_event_emit.go:249`：

```go
RequestID: session.GetSessionID() + "-" + strconv.FormatInt(time.Now().UnixNano(), 10),
```

注释说「前端只要求非空字符串；用会话 id + 纳秒保证同会话内可区分轮次」。但 Python 侧 `request_id` 来自 `work.request_id`（`deep_agent.py:3873` 等），是**真实的请求标识**，且在 `session_aggregator.py:59-61` 里被用于**幂等去重**（同一个 request_id 只累计一次）。

用「每次都在变的纳秒时间戳」伪造，意味着：① request_id 与真实模型请求失去关联，前端或下游若按 request_id 关联/去重会失效；② 即便只是展示，这个「每次都变」的值也没有任何语义锚点。

**影响面**：当前前端若只校验「非空」，功能不崩；但 request_id 的语义从「请求标识」退化成「随机串」，一旦前端或日志要做请求级关联（这是 request_id 存在的意义），就无从关联。这是「字段在、语义丢」的对齐偏差。

**建议**：如果 Go 侧 task_loop 的 work 有 request_id，透传真实的；如果没有（Go 侧可能没这个字段），至少登记为已知偏差，说明「Go 侧无 work.request_id，用时间戳占位，回填条件」。

### M-04 会话级 KV cache 聚合写成了无状态函数，且没接进链路

**这是哪条流程**：`context.usage` 快照里的 `session_cache` / `session_kv_cache_hit_rate` 字段（会话级 KV cache 命中率）。

**缺陷本身**：Python 的 `SessionKVCacheAggregator`（`session_aggregator.py:45`）是**有状态累加器**：按 `CacheAggregationKey`（session+provider+model+deployment…）分桶、用 `request_ids` set 幂等去重、随每次 `record()` 跨调用累积。Go 侧的 `AggregateSessionCache(requests []RequestKVCacheUsage)`（`session_aggregator.go:59`）是**无状态纯函数**，只对传入列表做一次加权平均。

更关键的是：`AggregateSessionCache` 在生产代码里**零调用**（只有测试引用）；`analyzer.go:131` 的 `SessionCache` 是从 `SnapshotInput.SessionCache` 直接透传，而 `emitContextUsage`（`stream_event_emit.go:246-257`）构造 SnapshotInput 时**根本没填 SessionCache**。所以实际发出的快照里 `session_cache` 恒为 nil、`session_kv_cache_hit_rate` 恒为 nil。

对照 Python：`SessionKVCacheAggregator` 类虽然也被导出，但我在上游生产代码里也没找到直接 `.record()` 调用（可能同样处于「已定义、未完整接线」状态）。所以这不算「上游有、Go 漏接」的明确回归，而是「两边都半接线，但 Go 这半语义已经悄悄简化」——request_id 去重、有状态累积这些语义一旦要接，现在这个纯函数是接不上的。

**影响面**：当前会话级命中率字段恒空，前端若依赖它做缓存命中展示会一直空；且后续要接线时，现成的 `AggregateSessionCache` 是个错误形态（无状态、无去重），需要重写。

**建议**：至少登记偏差，说明「会话级 KV cache 聚合暂未接线，`AggregateSessionCache` 是无状态简化、缺 request_id 去重，接线时需对齐 `SessionKVCacheAggregator` 的有状态语义」。

### M-05 `nativeSessionID` 只写不读（死字段）

**这是哪条流程**：TeamHarness 记录「当前 run cycle 的团队会话标识」。

**缺陷本身**：`team_harness.go:106` 的 `nativeSessionID` 字段，全仓只有赋值（`Start` 里 `= sessionIDOf(teamSession)`）、清空（`Stop` 里 `= ""`）和测试断言，**没有任何生产代码读取它**。Python 的 `_native_session_id` 在 `team_harness.py` 里是给 swarmflow 停止、session 生命周期 hook 判断用的，Go 侧这些消费方还没移植（`_stop_owned_swarmflow_runs`、`kv_cache_harness_session_lifecycle_hook` 在 Go 侧都是缺失/未接的）。

**影响面**：死字段本身无害，但它是「还有一批依赖 `_native_session_id` 的能力没移植」的信号。建议要么登记「该字段等 swarmflow/hook 移植后才有消费者」，要么标注回填点，避免后人以为它是无意义的残留而删掉。

---

## 三、提示（T）

### T-01 abort 的「有界等待 5 秒」给 Stop 留了并发窗口

`abortActiveRound`（`native_harness.go:700-704`）等 `active.Done` 用了 `time.After(5 * time.Second)` 有界等待，注释承认「runRound 的 RunOneRound 在 cancel 后应尽快返回」。但如果 cancel 没能在 5 秒内让 SubmitRound 返回（比如模型调用卡死、cancel 信号没传到位），5 秒后 `abortActiveRound` 返回，`Stop` 继续 `UnbindSession` → `RemoveSession`，而 runRound goroutine 还在 `SubmitRound` 里读写 session map，**并发读写 map 的 race 又回来了**——这正是 `abortActiveRound` 注释里自己说要「等 active.Done」防的那个问题。Python 是 `await` 无界等待。这是「用有界等待换防挂死，代价是重新打开 race 窗口」的权衡，建议至少把这个窗口登记清楚（当前是硬编码 5 秒魔法数 + 注释里一句「应尽快」）。

### T-02 crash 重试用 `active.Query` 而非 `original_query`，字段语义被悄悄合并

Python 的 `ActiveRound` 有 `original_query`（本轮原始查询，InteractiveInput 不可重放时会区分），crash 重试用的是 `active.original_query` 且判断 `not isinstance(active.original_query, InteractiveInput)`。Go 侧 `ActiveRound` 只有 `Query any`，crash 重试（`native_harness.go:627`）用 `active.Query.(string)` 且非空即重试。当前 25.1a 阶段 InteractiveInput 的 resume 语义还没接（25.1c），所以暂时等价；但「original_query 的 InteractiveInput 不可重放」这个语义一旦 25.1c 接入，`Query` 这个合并字段是表达不出来的。属于「字段语义被提前合并、后续要拆」的隐患，建议登记。

### T-03 若干「对齐但当前不可达」的分支没登记进偏差表

`isControlResult` 漏 cooperative-stop（M-01）、`onSend` 的 PAUSED 段、`settleRoundDone` 的「协作暂停」分支、`_emit_round_aborted` 缺（这条已在回填台账登记）——这些「25.1a 阶段不可达、25.1c 回填后生效」的分支，有些登记了（emitRoundAborted 在回填台账第 338 行），有些没登记（isControlResult 的 cooperative-stop、failure_retry、reset_coordinator 都没进偏差表）。偏差表（spec 第 315-329 行）是「须用户确认后实施」的唯一出口，漏登记的分支在 25.1c 回填时可能被当作「不需要回填」漏掉。建议把 S-01/S-02/M-01 涉及的三个点补进偏差表或回填台账。

---

## 附：本轮未深入但建议后续跟踪的点

- **9.89 Goal 接线**（887b79af / 7dc204b9）本轮只落了 spec + plan 文档，1034 行计划尚未实施，留待下一轮审核实施后的代码。
- **LLM 客户端 C 组对齐**（bc985b07 / 25f47b79）同样只落计划/设计文档，未实施。
- **`sessionKey` 兜底**（`model_anomaly.go:1111-1120`）无 session 时用 `session@%p` 指针兜底，对齐上游 `id(session)`；但指针地址在同一进程内会复用，不同会话若先后无 session_id 落到同一地址，计数会串。属于低概率、且上游同源的已知行为，仅记录。
