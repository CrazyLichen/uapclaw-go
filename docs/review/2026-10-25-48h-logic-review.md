# 48h 逻辑审查 — 2026-10-25

> 审查范围：48 小时内提交（10-23 04:11 ~ 10-25 04:11）涉及的实现章节
> - **11.10 Cron 调度服务**（16 提交：scheduler/store/models/controller/cronbridge/cron_tools/装配链）
> - **13.12 PDF Parser / 13.13 Word+Excel Parser / 13.14 HTML/Web/WeChat Parser / 13.15 Image Parser+Captioner**
> - **14.1 devtools utils/constants**（5 提交）
> - **11.9 GatewayServer 多路由 WS 服务器本体**（d9c863ca 系列，随 cron 链装配复查）
> - 顺带复核：11.7 SessionMap / 11.13 GatewayHook（10-23 已由 92acbaa0 审查修复，本轮仅确认无回归）
>
> 审查方法：对照 Python 参考项目（`/home/opensource/jiuwenswarm-develop/jiuwenswarm` + `/home/opensource/agent-core/openjiuwen`），逐函数对比方法签名、步骤完整性、⤵️/⤴️占位代码实际状态

---

## 汇总

| 级别 | 数量 |
|------|------|
| 严重 (S) | 2 |
| 一般 (M) | 2 |
| 提示 (T) | 2 |
| **合计** | **6** |

---

## 严重问题 (S-01 ~ S-02)

### S-01 CronSchedulerService.onWake 缺少 in-flight 去重——Reload 重建事件堆后同一 runID 双跑 agent

**Python 样例**（`jiuwenswarm/gateway/cron/scheduler.py` L341-366）：

```python
async def _on_wake(self, job: CronJob, run_id: str) -> None:
    state = self._runs.get(run_id)
    if state is None:
        ...  # 构造 state
    # ⚠️ 关键去重：同一 runID 的 agent task 尚在飞行中则直接返回
    if run_id in self._run_tasks and not self._run_tasks[run_id].done():
        return

    async def _run_agent() -> None:
        ...
```

Python 维护 `_run_tasks: dict[str, asyncio.Task]`，`_on_wake` 在启动新 task 前检查同 runID 的 task 是否仍在运行（L365-366）。这条防御是必要的：`reload()`（L157-198）会**清空事件堆并按当前时刻重算**所有 enabled job 的 wake/push 事件——正在执行中的 run 的 wake 事件可能被重新入堆，到期后再次触发 `_on_wake`。

**Go 问题**（`internal/swarm/gateway/cron/scheduler.go` L460-474）：

```go
// onWake 唤醒事件：启动 runAgent goroutine（对齐 Python _on_wake L341-444）。
// Python 在 _on_wake 中先建 state、再检查 _run_tasks 去重；
// Go 用 mu 互斥保证同一 runID 的 goroutine 只启动一次（去重语义由 getOrCreateRun 的
// 存在性 + runAgent 串行写状态共同保证，wake 事件本身不会重复入堆）。
func (s *CronSchedulerService) onWake(ctx context.Context, job *CronJob, runID string) {
	state := s.getOrCreateRun(job, runID)
	if state == nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runAgent(ctx, job, state)
	}()
}
```

注释中"**wake 事件本身不会重复入堆**"的论断不成立：Go `Reload()`（L247-302）与 Python 一样清空 `s.events` 后按 `computeNextRun(now)` 重算入堆。`getOrCreateRun` 的存在性只复用 state 指针，**不能阻止第二个 goroutine 启动**——第二个 `runAgent` 会再次 `SendRequest` 调用 agent。

**具体流程示例**（`0 * * * *` 每小时任务，`wake_offset_seconds=300`）：

1. 10:55:00 wake 事件到期，`onWake` 启动 goroutine 调 agent（长任务，预计 10:55~11:05 运行中）
2. 10:57:00 用户调用 `cron_update_job` 修改另一个 job B → `CronTools.UpdateJob` → `reloadScheduler` → `svc.Reload()`
3. Reload 清空事件堆，按 now=10:57 重算 job A：`computeNextRun` 返回 push_dt=11:00、**wake_dt=10:55（已过去，无 now 守卫）**，两事件重新入堆
4. 10:55 < now → wake 事件立即到期 → `onWake` 再次执行 → `getOrCreateRun` 返回已有 state → **又启动一个 goroutine 对同一 runID 调 agent**
5. 后果：同一轮定时任务 agent 被调用两次（双倍 LLM 消耗）；两次写 `state.Status/ResultText` 互相覆盖；占位符/补发推送逻辑在不一致状态上运行

触发面不止 update：所有写操作（create/update/delete/toggle）后的 `reloadScheduler`、`checkStoreChanged` 的 5 秒 mtime 轮询（双部署形态另一进程写文件）、controller 路径 8 个端点的 `scheduler.Reload()`——只要 agent 执行期间发生任一 reload，且重算出的 wake_dt 落在过去，即双跑。`wake_offset_seconds` 默认 300，意味着**每次整点前 5 分钟内的任何 reload 都必然命中**。

**修复方案**：对齐 Python 维护活跃 run 集合：

```go
// CronSchedulerService 增加字段
activeRuns map[string]struct{}  // runID → runAgent goroutine 飞行中

// onWake 增加 in-flight 去重（对齐 Python _run_tasks 检查）
func (s *CronSchedulerService) onWake(ctx context.Context, job *CronJob, runID string) {
	state := s.getOrCreateRun(job, runID)
	if state == nil {
		return
	}
	s.mu.Lock()
	if _, running := s.activeRuns[runID]; running {
		s.mu.Unlock()
		return // in-flight 去重（对齐 Python L365-366）
	}
	s.activeRuns[runID] = struct{}{}
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.activeRuns, runID)
			s.mu.Unlock()
		}()
		s.runAgent(ctx, job, state)
	}()
}
```

同步补一条回归测试：fake agentClient 阻塞 200ms 的慢调用 + 执行中途 Reload + 断言 SendRequest 仅 1 次。

---

### S-02 cronbridge 内嵌调度器 publisher 注入 nil——standalone 兜底路径到期推送直接 panic

**Python 样例**（`agents/harness/common/tools/cron/cron_tools.py` L106-110）：

```python
self._scheduler = CronSchedulerService(
    store=self._local_store,
    agent_client=agent_client,
    message_handler=message_handler,  # ⚠️ 从 MessageHandler.get_instance() 解析后传入
)
```

Python `ensure_scheduler` 构造内嵌调度器时把 `message_handler`（L94-99 从单例解析，可能为 None）传入 `_push_to_targets` 的消费端。即便为 None，Python L713 `await self._message_handler.publish_robot_messages(msg)` 抛 `AttributeError` 后被 `_loop` 的 `except Exception`（L278-279）捕获——**该轮消息丢弃，调度器存活**。

**Go 问题**（`internal/swarm/agents/harness/common/tools/cron/cron_runtime.go` L144-150）：

```go
if b.toolsOverride != nil {
	b.toolsOverride.SetSchedulerDeps(agentClient, nil, configLoader)  // ⚠️ publisher 硬编码 nil
} else {
	tools := NewCronTools(pushFn)
	tools.SetSchedulerDeps(agentClient, nil, configLoader)           // ⚠️ 同样 nil
	b.toolsOverride = tools
}
```

`GetBackend` 惰性解析时调用 `SetSchedulerDeps(agentClient, nil, configLoader)`，第二参数（调度器 push 消息发布器）硬编码 `nil`——而同函数上方刚从全局注册表取出 `userPublisher`（`app_gateway.go` 装配注入的 `msgHandler`，L137-142），明明可用却没传。

`EnsureScheduler` 步骤 4（standalone 兜底，cron_tools.go L182）构造 `NewCronSchedulerService(t.localStore, t.agentClient, t.publisher=nil, nil)`。到期推送时：

```go
// scheduler.go L759 pushToTargets 尾部
s.publisher.PublishRobotMessages(msg)  // ⚠️ nil 接口方法调用 → panic
```

`onPush`/`onPushUpdate` 在 `loop` goroutine 中直接调用 `pushToTargets`，**无 recover** → panic 炸掉整个进程（比 Python 的"消息丢弃+调度器存活"严重一级）。

**具体触发窗口**（`globalAgentClient` 非 nil 且 `GetProcessScheduler()` 无运行中条目）：

1. Gateway 装配（`NewAppGatewayServer`）注入三个全局依赖：`SetGlobalAgentClient(agentClient)` 等（e68c3a69）
2. Gateway 的调度器**尚未 Start**（进程启动早期 / 已 Stop）→ `processScheduler.IsRunning()` 为 false
3. 此时 agent 侧任意 cron 工具首次调用 → `CronRuntimeBridge.GetBackend` 惰性构造 → `EnsureScheduler` 步骤 3 委托失败（未运行）→ 步骤 4 启动内嵌调度器（`agentClient` 非 nil 通过检查）
4. 内嵌调度器到期 → `pushToTargets` → `s.publisher.PublishRobotMessages` → **nil 接口 panic → 进程崩溃**

**修复方案**（双管齐下）：

```go
// ① cron_runtime.go GetBackend：传递 userPublisher（MessageHandler 同时实现
//    PublishRobotMessages 与 PublishUserMessagesNowait，隐式满足 gwcron.MessagePublisher）
if b.toolsOverride != nil {
	b.toolsOverride.SetSchedulerDeps(agentClient, userPublisher, configLoader)
} else {
	tools := NewCronTools(pushFn)
	tools.SetSchedulerDeps(agentClient, userPublisher, configLoader)
	b.toolsOverride = tools
}

// ② scheduler.go pushToTargets：nil 守卫兜底（对齐 Python message_handler=None 时
//    loop 捕获异常的净效果——消息丢弃+Error 日志+调度器存活）
if s.publisher == nil {
	logger.Error(logComponent).
		Str("job_id", job.ID).Str("run_id", state.RunID).
		Msg("[Cron] push skipped: message publisher is nil")
	return
}
s.publisher.PublishRobotMessages(msg)
```

①是根因修复（正常部署路径 publisher 恒可用），②是防御对齐（Python 同样存在 None 炸点但被 loop 捕获；Go panic 无法被无 recover 的 loop goroutine 容忍）。

---

## 一般问题 (M-01 ~ M-02)

### M-01 Agent 侧 CronTools.CreateJob 传递 mode（"agent.fast"），Python 显式丢弃（store 兜底 "agent"）

**Python 样例**（`agents/harness/common/tools/cron/cron_tools.py` L253-264）：

```python
job = await self._local_store.create_job(
    job_id=str(normalized.get("id") or "").strip() or None,
    name=str(normalized.get("name") or "").strip(),
    cron_expr=str(normalized.get("cron_expr") or "").strip(),
    timezone=str(normalized.get("timezone") or "Asia/Shanghai").strip() or "Asia/Shanghai",
    description=str(normalized.get("description") or ""),
    targets=targets_str,
    enabled=bool(normalized.get("enabled", True)),
    wake_offset_seconds=normalized.get("wake_offset_seconds"),
    delete_after_run=normalized.get("delete_after_run"),
    **session_kw,   # 仅 session_id / chat_type——⚠️ 无 mode
)
```

`_extract_legacy_params` 计算的 `out["mode"]`（默认 "agent.fast"，cron_runtime.py L317-327）在 `CronTools.create_job` 的显式参数列表中被丢弃——store 层（store.py L65/L71）`mode=None` 兜底 `"agent"`。只有 gateway controller 路径（controller.py L135/L155）传 mode。

**Go 问题**（`internal/swarm/agents/harness/common/tools/cron/cron_tools.go` L278-291）：

```go
job, err := t.localStore.CreateJob(gwcron.CreateJobParams{
	...
	Mode:              strings.TrimSpace(strVal(normalized["mode"])),  // ⚠️ "agent.fast" 被写入
	...
})
```

Go 把 `extractLegacyParams` 输出的 mode（默认 **"agent.fast"**）写入了 store。分歧传导链：

- scheduler 唤醒时 envelope `params["mode"]`：Python `"agent"` vs Go `"agent.fast"`（scheduler.py L381 / scheduler.go L515-517）
- deep_adapter `_update_runtime_config(mode)` 两侧行为差异：
  - `updateRailsForMode`：两侧都落入 agent-mode 分支（非 "agent.plan"）——rails 等价
  - `runtime_state.yaml` 写入的 mode 值不同（`"agent"` vs `"agent.fast"`）
  - `RuntimePromptRail.SetMode` 值不同（影响以 mode 为变量的提示词模板）
  - Python `_update_tools_for_mode`（L2939 `if mode != "agent.fast": return`）：Python "agent" **不注册** MultiSessionToolkit；Go "agent.fast" 在该函数（若后续实现）语义下会注册

需要说明：Python `models.py` L110-111 注释称 mode "创建时从对话上下文继承"——`_extract_legacy_params` 算出 mode 却被 `CronTools.create_job` 丢弃更像 Python 侧的实现疏漏，Go 行为反而贴合注释意图。但按一比一复刻原则，行为对齐优先于注释意图，提请裁决。

**修复方案**（二选一，需用户决策）：

- 方案 A（严格对齐 Python）：`cron_tools.go` CreateJob 的 `CreateJobParams` 不传 `Mode`（空串），让 store 层 `modeAny("")` 兜底 `"agent"`：
  ```go
  // 对齐 Python CronTools.create_job 显式参数列表不含 mode——store 兜底 "agent"
  Mode: "",  // 或直接删除该字段赋值
  ```
- 方案 B（保留 Go 行为，归档有意偏差）：在 spec §8 增补偏差条目"agent 侧创建任务保留 mode 继承（Python 丢弃系上游疏漏）"，并在 `cron_tools.go` CreateJob 注释中引用该条目

### M-02 GatewayServer.Send 定向发送前不检查连接存活性——Python 的 `ws.closed` 广播 fallback 分支缺失

**Python 样例**（`gateway/app_gateway.py` L428-431）：

```python
if ws is None or bool(getattr(ws, "closed", False)):
    if ws is None:
        channel_id = getattr(msg, "channel_id", None)
        if channel_id and channel_id != "acp" and msg.type != "res":
            clients = self._find_channel_clients(channel_id)
            ...
```

Python 检查 `ws.closed`：定向目标已断开时**视为未命中**，落入广播 fallback——同 channel 的其他在线客户端（如多开 web 页面）仍能收到消息。

**Go 问题**（`internal/swarm/gateway/gateway_server.go` L278-293）：

```go
func (s *GatewayServer) Send(ctx context.Context, msg *schema.Message) error {
	var ws *websocket.Conn
	requestKey, ok := clientRouteKey(msg.ChannelID, msg.ID)
	if ok { ... }
	if ws == nil {
		sessionKey, ok := clientRouteKey(msg.ChannelID, msg.SessionID)
		if ok { ... }
	}
	// ⚠️ 只判 ws == nil；ws 非 nil 但已断开 → 直接进入定向 WriteMessage →
	// 写失败仅 Debug 日志 → 消息丢弃（不广播）
```

Go 只查 `ws == nil`。客户端断开后、`HandleConnection` finally 清理 session 键之前（TCP 半关闭、ReadMessage 尚未感知的窗口），定向发送命中 stale 连接 → `WriteMessage` 失败 → Debug 日志 → **消息丢弃**。Python 同场景会广播 fallback 送达其他客户端。

**修复方案**：利用现有 `probeWritable`（L583-587，local handler 已用于同一目的）在定向发送前探测：

```go
// 定向发送前探测连接存活性（对齐 Python ws.closed 检查）
if ws != nil && !s.probeWritable(ws) {
	ws = nil // 视为未命中 → 落入广播 fallback（对齐 Python L428-439）
}
```

注意 `probeWritable` 会 SetWriteDeadline + Ping，50ms 开销可接受（仅 stale 场景多一次探测失败）。

---

## 提示问题 (T-01 ~ T-02)

### T-01 cronbridge create_job 入口日志缺 raw_keys 字段

**Python 样例**（`agents/harness/common/tools/cron/cron_runtime.py` L67-76）：

```python
logger.info(
    "[CronRuntimeBridge] create_job in: context.channel_id=%s "
    "context.session_id=%s metadata.request_id=%s raw_keys=%s",
    ..., sorted(list((params or {}).keys())),  # ⚠️ raw_keys
)
```

**Go 问题**（`internal/swarm/agents/harness/common/tools/cron/cron_runtime.go` L242-246）：日志只有 channel_id/session_id/request_id 三字段，缺 `raw_keys`（入参 key 排序列表）——排查 legacy 参数映射问题时该字段是关键线索。

**修复方案**：

```go
keys := make([]string, 0, len(params))
for k := range params {
	keys = append(keys, k)
}
sort.Strings(keys)
logger.Info(logComponent).
	Str("channel_id", contextChannelID(cronCtx)).
	Str("session_id", contextSessionID(cronCtx)).
	Str("request_id", requestID).
	Strs("raw_keys", keys).
	Msg("[CronRuntimeBridge] create_job in")
```

### T-02 cronbridge BuildTools 缺尾部 "Built N tools" 汇总日志

**Python 样例**（`agents/harness/common/tools/cron/cron_runtime.py` L396-398）：

```python
logger.info("[CronRuntimeBridge] Built %d cron tools: %s",
            len(tools), [tool.card.name if hasattr(tool, 'card') else str(tool) for tool in tools])
```

**Go 问题**（`internal/swarm/agents/harness/common/tools/cron/cron_runtime.go` L173-194）：`BuildTools` 有入口日志（`Building cron tools for context`）无尾部汇总（构建数量与工具名清单）。

**修复方案**：`BuildTools` 返回前追加：

```go
names := make([]string, 0, len(tools))
for _, tl := range tools {
	names = append(names, tl.Card().Name)
}
logger.Info(logComponent).
	Int("count", len(tools)).
	Strs("tools", names).
	Msg("[CronRuntimeBridge] Built cron tools")
```

---

## 已验证对齐清单（正面确认）

以下项经逐行对照确认与 Python 语义一致，**无需修改**，列出以缩小后续审查关注面：

| 模块 | 验证结论 |
|------|---------|
| scheduler Reload/loop/handleEvent | 只跳过 `enabled=False`（与 Python 一致）；notifyReload 非阻塞无死锁；expired 经 UpdateJob 落盘后下次 reload 自然跳过 |
| scheduler rescheduleNext/push_update 补发 | delete_after_run / 迟到补发 / 占位符三链路全对齐 |
| pushToTargets 9 渠道元数据 | feishu/xiaoyi/whatsapp/wecom/wechat/dingtalk + feishu_enterprise 前缀 + group_digital_avatar 路由逐字段对齐 |
| handleCronPushPayload | 8 action 路由 + chat.tool_result 回发对齐（outbound.go） |
| deep_adapter cron 绑定 + 任务取消 | BindRuntimeCronContext 4 值语义 + CancelRunningTasks ×2 对齐 |
| team_manager cron 注册 | CronRuntimeBridge.BuildTools 双注册（ResourceMgr + abilityMgr）对齐 |
| list_jobs include_disabled | dispatch 默认 false（读输入参数），legacy wrapper true——两路径均对齐 openjiuwen cron.py |
| cronbridge extractLegacyParams/toBackendJob | 嵌套参数映射、per-request routing（隐式 web → 上下文频道）、默认字段注入逐条对齐 |
| 11.7 SessionMap / 11.13 GatewayHook | 10-23 已审（92acbaa0），本轮确认无回归 |
| 13.12 PDFParser | 双通道 + 页关联 caption + 软失败对齐（6 项偏差已归档 spec §8） |
| 13.13 WordParser/ExcelParser | markdown 转换/表格展开/rowsToDocuments 黄金基准/CSV-TSV 分支对齐 |
| 13.14 HTML/Web/WeChat | 选择器列表字面一致、阈值 100/200 一致、effectiveID 三级回落一致 |
| 13.15 ImageParser+Captioner | CpImage 四分支/CaptionImages 串行/空 caption 单 Document 对齐 |
| 14.1 devtools utils/constants | SafeJSONLoads 三层语义/LoadJSONFile 四分支/20 常量对齐 |
| cron_json_convert.py | CLI 工具，Go doc.go 已文档化有意不移植——非缺陷 |
| Python L417-418 裸键回退 | Go spec §7-2 已归档有意偏差（dict 对 tuple 的意外行为，永不命中的死代码） |

## ⤵️/⤴️ 占位符状态

48h 窗口内完成章节（11.10/13.12-13.15/14.1）无遗留占位符。全仓扫描到的 ⤵️ 标记均属 6.x/9.x 等计划内未实施章节（`harness_config/builder.go` 9.38、`deep_agent.go` 9.3 等），状态真实——非"标 ✅ 但实为桩"的虚标。

---

## 结论

- **S-01/S-02 均集中在 11.10 cron 链**：前者是并发语义分歧（Reload 重建事件堆 → 双跑 agent，注释论断错误），后者是装配遗漏（nil publisher → 进程 panic）。两者都有明确的高频触发窗口，建议优先修复。
- **M-01 需要用户裁决**：严格对齐 Python（丢 mode）还是保留 Go 行为（贴合 models.py 注释意图）并归档偏差。
- 13.x parser 与 14.1 devtools 移植质量高，无发现。
