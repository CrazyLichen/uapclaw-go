# 11.10 Cron 调度服务 — 全量对齐设计（方案 B：委托降级单调度器）

> 状态：待评审
> 日期：2026-10-24
> 范围：IMPLEMENTATION_PLAN.md 11.10（Cron 调度服务）全量 + 用户决策带出的 Agent 侧桥接
> （Python `agents/harness/common/tools/cron/` 归属 10.6.24"Swarm 内置工具集"，用户拍板并入 11.10 一次性实现，
> 回填 team_manager_spec.go 的 ⤵️(10.6.24/11.10) 标记）。
> Python 参考：jiuwenswarm/gateway/cron/（6 文件 2035 行）+ jiuwenswarm/agents/harness/common/tools/cron/（3 文件 865 行）

## 一、已锁决策（四项）

| # | 决策点 | 选择 | 说明 |
|---|--------|------|------|
| 1 | cron 表达式引擎 | **B：go-quartz v0.15.2** | MIT 协议已进 go.mod。Quartz dow 约定 1=SUN..7=SAT，需从 croniter 0=SUN 变换（`v→v%7+1`、区间 `1-5→2-6`、`0→7或1`、名称不变） |
| 2 | `_push_to_targets` 渠道元数据 | **A：全量对齐** | 9 渠道（web/tui/feishu/feishu_enterprise/wecom/dingtalk/wechat/whatsapp/xiaoyi）逐字段对齐，约 150 行 |
| 3 | 实施范围 | **A：全量实现含 Agent 侧桥接** | gateway/cron + agents 桥接 + 全部 ⤵️ 回填 + team_manager 接线点，LLM 工具链完整可用 |
| 4 | 内嵌调度器 | **B：委托降级单调度器** | 进程级注册表：Gateway 调度器在跑则 CronTools 委托 reload 它，否则启动内嵌 fallback。修正 Python 单进程双跑缺陷，偏差集中 ensureScheduler ~30 行 |

## 二、Python 事实基线（全部已核查，勿重查）

1. **Python 双调度器是双进程设计**：Gateway 进程与 AgentServer 进程各自一个 CronSchedulerService 读写同一 `~/.jiuwenswarm/agent/home/cron_jobs.json`（`get_cron_jobs_path()`）。防双跑只有 per-instance `_running`/`_scheduler_started` 标志，无全局互斥——**单进程下写一次 = 双执行双推送（Python 现存缺陷）**。`reload()`（scheduler.py:157-170）重建整个事件队列（仅保留飞行中 push_update），两个调度循环各自排 wake/push 事件。
2. **内嵌调度器真实启动路径**：`CronRuntimeBridge.ensure_scheduler_started()` 全仓零外部调用方（死代码入口）；实际由 `CronTools._reload_scheduler()`（cron_tools.py L271/298/310/322，create/update/delete/toggle 写后）触发。`CronTools._local_store` 与 Gateway store 同路径同文件。
3. **`_cancel_scheduler_running_tasks`（interface_deep.py:3478-3506）与 cron 调度器无关**：取消的是 agentcore TaskScheduler 的 `_running_tasks`（in-flight LLM HTTP 调用，`instance._loop_controller._task_scheduler`）。只是与 cron 同期合入才标 11.10。Go 对应 `TaskScheduler` 的 `entry.cancel()`（task_scheduler.go:375-440 已确认可访问）。
4. **`_run_agent` 完成通知**（scheduler.py:368-443）：finally 块里调度即时 `push_update` 事件（非回调）+ push_at 已过期的迟到补发；`asyncio.create_task(_run_agent(), name=f"cron-run-{job.id}")`，run_tasks map 跟踪。Go 用 goroutine + run-state map + mutex 等价。
5. **wake 后端**（cron_runtime.py:139-168）：构造 Message（`id=cron-wake-{ms}`、params.query/content/mode 默认 `agent.fast`）→ `message_handler.publish_user_messages(msg)` → `{"queued": True}`。Go 对应 `PublishUserMessagesNowait`（message_handler.go:208-224 已确认签名）。
6. **`_handle_cron_push_payload`**（message_handler.py:1677-1737）：action（list/get/create/update/delete/toggle/preview/run_now）路由到 CronController，结果以 `chat.tool_result` event_type 经 publish_robot_messages 回发；create 时从 `_stream_modes` 取 mode 覆盖 LLM 工具默认值。
7. **store.create_job 无唯一性校验**（store.py:52-94）：job_id 已存在直接覆盖（upsert 语义）——Agent 双写 + Gateway 转发双写天然幂等。
8. **`_send_split`**（cron_tools.py:145-163）：`{request_id, channel_id, session_id, response_kind: "cron", body: {action, status: "ok", data, message}}` 经 `gateway_push.send_push` 转发——工具结果异步回显，本地写即时生效。
9. **team_manager.py:492-515**：`register_member_runtime_tools` 为每个成员 agent 走 CronRuntimeBridge 注册 cron 工具（`Runner.resource_mgr.add_tool` + `agent.ability_manager.add`）。
10. **cron_json_convert.py（264 行）不移植**：CLI 平铺↔嵌套转换工具，非运行时代码。
11. **A2X 的 5 处标记（deep_adapter.go:206-216/479/582/702-703）不属于 11.10**，本次不动。
12. **`_bind_runtime_cron_context`**（interface_deep.py:2720-2747）：4 个 ContextVars（channel_id 归一化默认 web/session_id/metadata 注入 request_id/mode），Go 用 ctx values 等价（不可变值模式，defer 清理只需 unmark）。
13. **`_is_valid_target`/`normalize_target_channel_id`**（models.py）：feishu_enterprise 需带 `:<app_id>` 后缀校验。
14. **E2A 侧已完成**：`gateway_normalize.go:640-700` 已实现 E2AResponseKindCron → cron.response 转换，无需改动。

## 三、agent 会话流程位置（11.10 在链路中的坐标）

```
【主交互链路】（现有）
用户消息 → InboundGatewayServer → MessageHandler.forwardLoop
  → AgentClient.SendRequest(Stream) → ChannelTransport → AgentServer.handleEnvelope
  → DeepAdapter.processMessage(Stream)
      步骤 9: ⤵️ cron 路由上下文绑定（4 ContextVars → ctx values）    ← 回填点 #2/#3
      步骤 18: Runner.RunAgent → LLM 调 cron 工具
          → agentcore cron dispatch → CronToolBackend（11 方法接口已就绪）
          → cronToolsBackend（本设计新建，实现该接口）
              读: 本地 CronJobStore（与 Gateway 同文件）
              写: 本地写 + _send E2A 转发（gateway_push → handleCronPushPayload → CronController）
              wake: → PublishUserMessagesNowait（重新注入用户消息队列）
      finally: cron ctx reset                                        ← 回填点 #3b

【独立调度循环】（本设计核心新建）
CronSchedulerService（单实例，方案B注册表保证）
  主循环: 事件堆（wake/push/push_update）+ reload 通知 + 5s mtime 轮询
  wake   (push_at - wake_offset, 默认提前300s) → runAgent goroutine
          → AgentClient.SendRequest(envelope: query=job.description, session=cron_{ts}_{job_id})
  push   (push_at): agent 未完成 → 占位符 "[cron] {name} 正在执行中…"
  push_update   : agent 完成（finally 即时排）或迟到 → pushToTargets
          → MessagePublisher.PublishRobotMessages（接口注入破环）
          → 9 渠道元数据路由（feishu_enterprise 解析 routing_sid 三段、
             feishu/xiaoyi/whatsapp/dingtalk 取 config.yaml 最近回发身份）

【中断链路】（回填点 #4/#5）
Interrupt(cancel) / AbortOnGatewayDisconnect → 取消 TaskScheduler 运行任务
```

## 四、交付物清单

### 新建包 `internal/swarm/gateway/cron/`（对应 Python gateway/cron/，5 文件+doc）

#### 1. `models.go`（对齐 models.py，250 行）

- `CronJob` 结构体 15 字段：id/name/enabled/cron_expr/timezone/wake_offset_seconds(默认300)/description/targets/session_id/chat_type/mode(默认agent)/delete_after_run/created_at/updated_at/expired
- `CronTarget`、`CronTargetChannel`（web/tui/feishu/feishu_enterprise/wecom/dingtalk/wechat/whatsapp/xiaoyi + feishu_enterprise 带可选 app_id 后缀）
- 导出：`IsValidTargetChannelID`、`NormalizeTargetChannelID`（feishu_enterprise:<app_id> 校验与归一化）
- `ToDict`/`FromDict` map 转换（E2A payload 兼容，与 Python to_dict 字段名一致）

#### 2. `cron_expr.go`（对齐 cron_expr.py，75 行）

- `NormalizeCronExpr(expr) (string, error)`：
  - 5 域 → 7 域（补 `0 ` 秒前缀 + ` *` 年后缀）
  - ISO 8601 一次性时间 → 7 域表达式（秒 分 时 日 月 周 年）
  - **dow 变换**（croniter 0=SUN → Quartz 1=SUN..7=SAT）：单值 `v→v%7+1`（0→7）；区间 `a-b→(a%7+1)-(b%7+1)`；步长/名称不变
  - go-quartz `quartz.NewCronTrigger(expr)` 校验（提前暴露非法表达式）
- `NextFireTime(expr string, from time.Time, tz *time.Location) (time.Time, error)`：包装 go-quartz，`prev.UnixNano()` 精度陷阱已验证；`ErrTriggerExpired` → one-shot 过期语义

#### 3. `store.go`（对齐 store.py，238 行）

- `CronJobStore`：路径注入（默认 `pathutil.CronJobsPath()`）；`sync.Mutex`；JSON 持久化
- 方法：ListJobs/GetJob/CreateJob(upsert)/UpdateJob/DeleteJob/ToggleJob
- 原子写：写 tmp 文件 → `os.Rename`；损坏 JSON 兜底（重建空 store，对齐 Python try/except 重建）
- 构造注入路径便于测试（`t.TempDir`）

#### 4. `scheduler.go`（对齐 scheduler.py，713 行）

- `CronSchedulerService` 结构体：store、agentClient(接口)、messagePublisher(接口)、nowFn（**可注入假时钟**）、事件堆（`container/heap`，(at_ts, seq, kind, job_id, run_id) 五元组，seq 保证同刻稳定序）、runs map（run-state）、runTasks map、running 标志、reload 通知 chan、5s mtime 轮询
- `AgentClient` 依赖定义为**小接口**（SendRequest 一方法）：`routing.AgentClient` 隐式满足
- `MessagePublisher` 接口（**破环关键**）：`PublishRobotMessages(msg) error`——message_handler 隐式实现，gateway/cron 不 import message_handler
- 事件三分支（对齐 Python `_loop`）：
  - `wake`：`runAgent` goroutine（session 命名 `cron_{ts}_{job_id}`、query=description、mode 传递、run_id={job_id}:{push_ts}）；finally：即时排 push_update + 迟到补发判定
  - `push`：result_text 空 → 占位符；非空 → 直接推终稿
  - `push_update`：pushed_final 已置则跳过
- `runAgent` 状态机：succeeded/failed(含 cancelled)/finished_at/result_text（失败也产 result_text 供推送：`[cron] 任务执行失败: {error}`）
- `pushToTargets`（~150 行，全量对齐）：feishu_enterprise routing_sid `::` 三段解析（chat_part/open_id）；feishu/xiaoyi/whatsapp/dingtalk 从 config 取 last_* 平台身份写 metadata；web/tui 不绑 session_id（避免前端过滤）；targets 空 return
- `Reload()` 加 `sync.Mutex`（**并发补齐**：Python asyncio 单线程天然安全；Go 委托路径与 controller 路径并发触发需显式互斥）
- 进程级注册表（**方案 B 核心**）：`SetProcessScheduler`/`GetProcessScheduler`（RWMutex 保护），采用 `SetDefaultSendPushFunc` 同款全局注入先例
- 过期标记：computeNextRun 报 ErrTriggerExpired → `{enabled: false, expired: true}` 持久化
- delete_after_run：runAgent 结束后删 job
- 5s mtime 轮询：外部进程改文件感知（方案 B 单调度器下仍保留——双部署形态时另一进程写文件仍需感知）

#### 5. `controller.go`（对齐 controller.py，474 行）

- `CronController` 门面：ListJobs/GetJob/CreateJob/UpdateJob/DeleteJob/ToggleJob/PreviewJob/RunNow/Status/GetRuns
- targets 归一化、schedule 校验（表达式+时区）、description 兜底、routing session_id 推导
- 写操作后 `scheduler.Reload()`
- **不移植 cron_json_convert.py**（CLI 工具，doc.go 注明）

#### 6. `doc.go`：包文档 + 文件目录 + Python 对应路径 + 不移植说明

### 新建包 `internal/swarm/agents/harness/common/tools/cron/`（对应 Python agents/harness/common/tools/cron/，3 文件+doc）

#### 7. `cron_tools.go`（对齐 cron_tools.py，~430 行）

- `CronTools` 结构体：gatewayPush(接口)、localStore、scheduler/started 标志、agentClient/messageHandler(接口)注入
- 路由上下文：ctx values（4 key：channelID/sessionID/metadata/requestID+mode，对应 Python 4 ContextVars；`pushCronRoute`/`resetCronRoute` 配对）
- `_send`/`_sendSplit`：E2A cron 转发（response_kind=cron，body{action,status:ok,data,message}）
- 本地四写操作：create/update/delete/toggle（对齐 Python 双实例同文件语义——store upsert 幂等）
- `ensureScheduler`（**方案 B 唯一结构性偏差，~30 行**）：
  ```
  if 内嵌已启动且运行中 → 返回内嵌
  if 已尝试启动过 → 返回
  svc := cron.GetProcessScheduler()
  if svc != nil && svc.IsRunning() → svc.Reload()（委托）→ 标记已尝试 → 返回 nil
  启动内嵌调度器（standalone fallback，对齐 Python 81-114 行）
  ```
- 默认 target 推导：`_default_target_from_channel`（feishu_enterprise:/feishu/wecom/xiaoyi/whatsapp/wechat/dingtalk/tui 前缀匹配，兜底 web）
- targets 归一化：显式值校验 → 路由 channel 推导 → 隐式 web 映射

#### 8. `cron_runtime.go`（对齐 cron_runtime.py，~400 行）

- `CronRuntimeBridge`：backend 覆盖/已解析两级；`getBackend` 默认构造 `cronToolsBackend`
- `cronToolsBackend`（**实现 agentcore `CronToolBackend` 11 方法接口**，backend.go:14-59 已就绪）：
  - ListJobs/GetJob/CreateJob/UpdateJob/DeleteJob/ToggleJob/PreviewJob/RunNow/Status/GetRuns → 包装 CronTools 对应操作 + `_to_backend_job` 字段映射
  - `Wake`：text/channel 校验 → messagePublisher 为空报错（对齐 "unavailable before message handler startup"）→ 构造 Message → `PublishUserMessagesNowait` → `{"queued": true}`
- `_extractLegacyParams`（~110 行）：delivery.channel/data.targets/context.channel_id 三级 target 解析 + session_id 补齐
- `ensureSchedulerStarted`（对齐死代码入口，保留接口面但不接线——**Python 一致性决策：死代码保留**）

#### 9. `doc.go`：包文档

### 修改文件（9 个）

| # | 文件 | 内容 | Python 对应 |
|---|------|------|------------|
| 1 | `common/utils/path/paths.go` | 补 `CronJobsPath()` = WorkspaceDir()/agent/home/cron_jobs.json | `get_cron_jobs_path()` |
| 2 | `server/adapter/deep_adapter.go:217` | `cronRuntime any` → `*cronbridge.CronRuntimeBridge`（import agents/.../tools/cron，已有先例） | `self._cron_runtime = CronRuntimeBridge()` |
| 3 | `deep_adapter.go:852+1052` | 步骤 9 两处：绑定 cron 路由上下文（ctx values）+ defer reset | `_bind_runtime_cron_context`（4459/4662 两处） |
| 4 | `deep_adapter.go:1402` | interrupt(cancel)：`cancelSchedulerRunningTasks()` 实现（TaskScheduler runningTasks 遍历 entry.cancel） | `_cancel_scheduler_running_tasks`（interrupt 3393） |
| 5 | `deep_adapter.go:1643` | AbortOnGatewayDisconnect：同上 | 同上（3538） |
| 6 | `gateway/message_handler/outbound.go:110` | `handleCronPushPayload` 真实现：action 路由 CronController + `chat.tool_result` 回发 + create 时 streamModes mode 覆盖 | `_handle_cron_push_payload`（1677-1737） |
| 7 | `gateway/channel_manager/web/web_handlers.go:328-337` | 8 个 cron.job.* 桩 → 真实 handler 接 CronController | web 通道 cron 端点 |
| 8 | `agents/harness/team/team_manager_spec.go:269` | RegisterMemberRuntimeTools 真注册（bridge.BuildTools → ResourceMgr.AddTool + abilityManager.Add） | `team_manager.py:492-515` |
| 9 | `gateway/app_gateway.go:158` | 装配：store/scheduler/controller 构造 + `cron.SetProcessScheduler` + `MessageHandler.SetCronController` + `scheduler.Start()`；停机 `Stop()`（含 runTasks 清理）；bridge wake 推送函数全局注入 | `app_gateway.py:770-1070` 装配段 |

### 接口注入破环（依赖方向，无环验证过）

```
gateway/cron ──定义 MessagePublisher 接口──→ message_handler 隐式实现（不反向 import）✅
message_handler ──import gateway/cron（SetCronController *CronController）──→ 单向 ✅
gateway/cron ──import gateway/routing（AgentClient 满足小接口）──→ 单向 ✅
agents/.../tools/cron ──import gateway/cron + agentcore cron 接口──→ 镜像 Python 方向 ✅
server/adapter ──import agents/.../tools/cron──→ 已有先例（code_adapter.go:35-37）✅
```

**Go 无 MessageHandler 单例**：Python 用 `MessageHandler.get_instance()`，Go 侧 CronTools 的 wake 依赖经 `SetDefaultSendPushFunc` 同款全局注入先例由 app_gateway 装配时设置。

## 五、⤵️ 回填点最终处置表（10 处全覆盖）

| # | 位置 | 处置 |
|---|------|------|
| 1 | deep_adapter.go:206-216（A2X 4 字段）+ 479/582/702-703 | **保留**——A2X 功能不属于 11.10（注：物理驻 a2x 文件的 `bindRuntimeCronContext`/`resetRuntimeCronContext` 两桩是 10.3.6 误挂的 cron 桩，已随 11.10 清理删除，见 §十 #5；A2X 4 字段本身保留不动） |
| 2 | deep_adapter.go:217-219（cronRuntime any） | **回填**——升级具体类型 |
| 3 | deep_adapter.go:852（非流式步骤 9 绑定） | **回填** |
| 4 | deep_adapter.go:909（reset） | **回填**（ctx values 不可变，仅 unmark；reset 语义并入 defer） |
| 5 | deep_adapter.go:1052（流式步骤 10 绑定） | **回填** |
| 6 | deep_adapter.go:1402（interrupt cancel） | **回填**（取消 TaskScheduler 任务） |
| 7 | deep_adapter.go:1643（AbortOnGatewayDisconnect） | **回填**（同上） |
| 8 | outbound.go:110（handleCronPushPayload） | **回填** |
| 9 | web_handlers.go:328-337（8 桩） | **回填** |
| 10 | team_manager_spec.go:269（RegisterMemberRuntimeTools） | **回填**（cron 部分；SendFileToolkit 属 10.6.24 保留桩） |

**IMPLEMENTATION_PLAN.md 状态标记策略**：11.10 行 ✅ + 备注四决策与方案 B；10.6.24 行备注 "cron 工具集已随 11.10 实现回填（team_manager 注册点已接）"，其余工具（浏览器/MCP/搜索/视频/发文件/TODO/小艺电话）保留 ⤵️。

## 六、关键设计决策汇总（含理由）

| 决策 | 理由 |
|------|------|
| 方案 B 单调度器 | Python per-instance 标志在单进程无法防双跑（缺陷）；Go 修正为进程级判定。fallback 逻辑保留（注册表空 = standalone 形态，对齐 Python 双进程设计意图）。偏差集中 ensureScheduler ~30 行，审查可解释 |
| MessagePublisher 接口破环 | gateway/cron 直接持 message_handler 引用会循环依赖（message_handler → gateway/cron 已定单向）；接口注入是既有模式（PublishEventFunc 等先例） |
| AgentClient 小接口 | scheduler 只需 SendRequest；routing.AgentClient 隐式满足；测试 fake 零成本 |
| nowFn 注入 | Python 原生 `now_fn` 参数，零偏差对齐；确定性测试 wake→push→push_update 时序 |
| 保留 5s mtime 轮询 | 方案 B 单调度器下，双部署形态（两进程各一调度器）仍需感知对方写文件；轮询成本可忽略 |
| cron ctx 用 ctx values | Python 4 ContextVars 在 Go goroutine 模型下用 ctx values 最贴近（不可变传递）；defer 只需 unmark（reset 语义天然满足） |
| ToolScopeOverride 显式覆盖字段 | Python team 场景用 `SimpleNamespace(tool_scope=f"team_member_{agent_id}")` **动态属性遮蔽** frozen dataclass 的 `tool_scope` property（`{channel}:{session}` 计算）——Python 特有动态语义；Go 无属性遮蔽，用 `CronToolContext.ToolScopeOverride` 显式字段对齐（空串/空白不覆盖，回退默认计算）。ToolScope() 优先返回非空白 override |
| cronbridge 三依赖全局注入 | Python `ensure_scheduler` 内嵌 fallback 依赖经两个单例解析（`AgentServerClient.get_instance()` + `MessageHandler.get_instance()`，均 try/except 容错）；Go 无单例，用 `SetGlobalUserPublisher`/`SetGlobalAgentClient`/`SetGlobalConfigLoader` 三全局注入（app_gateway 装配时设置，`SetDefaultSendPushFunc` 同款先例）；E2A push 通道由 agent_server 侧注入（`SetGlobalGatewayPushFunc`），与 Gateway 侧三注入职责分离 |
| 死代码入口保留 | `ensure_scheduler_started` Python 全仓零调用方，保留接口面与 Python 一致（零调用死代码保留原则，对齐既有决策先例） |
| store 双实例保留 | 对齐 Python 结构（Agent 本地 store + Gateway store 同文件）；upsert 幂等使双写无害；删双实例需 CronTools 感知 controller，违反"工具层不感知 controller"的 Python 结构原则 |

## 七、测试策略（≥85% 覆盖率）

| 模块 | 测试要点 |
|------|---------|
| `cron_expr.go` | **黄金用例矩阵**：croniter（已 pip install）生成"表达式×时区×基准时刻→下次触发"期望表；dow 变换全覆盖（数字/区间/步长/0/7/名称）；5域/7域/ISO 归一化；过期报错 |
| `store.go` | t.TempDir；原子写中断恢复（残留 tmp）；并发读写；损坏 JSON 兜底；upsert 语义 |
| `scheduler.go` | **nowFn 假时钟**确定性测试：wake→push→push_update 事件顺序；占位符分支；agent 完成即时 push_update；迟到补发；delete_after_run；过期标记；5s mtime 轮询；Reload 并发安全（-race）；注册表 get/set；cancelScheduler 场景 |
| `controller.go` | targets 归一化矩阵；schedule 校验错误；preview 计数；routing session_id 推导 |
| `cron_tools.go` | 路由上下文绑定/复位；_send E2A payload 结构断言；默认 target 推导前缀矩阵；ensureScheduler 三分支（内嵌运行中/委托 Gateway/standalone fallback） |
| `cron_runtime.go` | cronToolsBackend 11 方法 fake 包装测试；_extractLegacyParams 三级 target 解析；wake 校验与错误路径 |
| deep_adapter 回填点 | 既有 interrupt/Abort 测试文件扩展：cancel 路径调用验证 |
| outbound/web_handlers | fake CronController 注入；action 路由断言；chat.tool_result payload 断言 |

## 八、风险与缓解

| 风险 | 缓解 |
|------|------|
| go-quartz NextFireTime 时区/dow 语义差异 | 黄金用例矩阵（croniter 生成期望值）先行锁定；探针脚本已验证 1=SUN 约定与 NANOSECONDS 精度 |
| 单进程双写竞态（Agent 本地写 vs Gateway controller 写） | store upsert 幂等 + tmp+rename 原子写 + per-instance mutex；方案 B 注册表保证调度循环唯一 |
| pushToTargets 9 渠道元数据遗漏 | 全量对齐已锁；逐渠道字段级断言测试 |
| deep_adapter 步骤 9 插入位置偏差 | 既有步骤编号注释（步骤 9: cron 上下文绑定）已就位，对照 Python 4459/4662 两处 |
| 团队成员工具注册与主链路重复注册 | 对齐 Python：`if !Runner.resource_mgr.get_tool(id)` 守卫（team_manager.py:512 先例） |
| IMPLEMENTATION_PLAN.md 状态漂移 | 本设计第五节处置表 + 规范 2 同步更新 |

## 九、实施顺序（供 writing-plans 参考）

1. `paths.go` CronJobsPath + `gateway/cron/models.go`（零依赖基础）
2. `cron_expr.go`（黄金用例矩阵先行）
3. `store.go`
4. `scheduler.go`（含注册表 + MessagePublisher 接口）+ 测试（假时钟）
5. `controller.go` + `outbound.go` 回填 + `web_handlers.go` 回填
6. `agents/.../tools/cron` 桥接包（cron_tools + cron_runtime）
7. deep_adapter 5 处回填（类型升级/绑定/cancel×2）
8. team_manager 注册点 + app_gateway 装配 + bridge 三依赖全局注入（UserPublisher/AgentClient/ConfigLoader）+ agent_server E2A push 注入
9. DeepAdapter `updateSessionTools` 回填（Deep 模式会话级 cron 工具注册，实施后审查补）
10. 全量回归（CGO_ENABLED=1 + `-tags "sqlite_fts5 test"`）+ 覆盖率 ≥85% + IMPLEMENTATION_PLAN.md 状态更新

## 十、实施偏差记录（实施时回填）

| # | 偏差 | 原因 | 处理 |
|---|------|------|------|
| 1 | team_manager 的 `Mode: "team"` + metadata 直传（计划稿写 `Mode: ""` 且 metadata 注入 request_id） | **计划代码块起草失误**：Python team_manager.py L498 原文 `mode="team"`、`metadata=request_metadata` 直传；request_id 注入是 deep_adapter 步骤 9 绑定的行为，team 场景无此注入 | plan Task 12 Step 1 已就地修正；实现正确（20ce3133） |
| 2 | cancel 调用 3 处（计划列 2 处：1402+1643） | Python `_cancel_scheduler_running_tasks` 实际 3 处调用（L3347 supplement / L3393 cancel / L3538 disconnect）；计划漏列 supplement 分支。**教训：调用点计数以 grep Python 源码为准，不能只信 plan 枚举** | plan Task 11 Step 3 已修正为 3 处；实现正确（ef159cab：1397/1413/1657） |
| 3 | `CronToolContext` 新增 `ToolScopeOverride` 字段（计划无此字段，tool_scope 当纯计算属性） | 计划未识别 Python 双路径：默认 property 计算 + team 场景 SimpleNamespace 动态遮蔽；Go 需显式覆盖字段表达（见 §六 决策行） | 翻译决策归档 §六；实现见 2b685360 |
| 4 | `FromRuntimeCronContext` 导出读取器（计划只规划写侧 Bind） | 测试可观测性：deep_adapter 跨包测试需读绑定值断言归一化语义；降级非导出不可行（测试必须驻 adapter 包驱动真实 Bind 调用）；生产路径零消费 | 注释已注明用途；实现见 ef159cab |
| 5 | 删除 deep_adapter_a2x.go 的 `bindRuntimeCronContext`/`resetRuntimeCronContext` 两 any 桩（处置表 #1 说"保留"） | 两桩物理驻 a2x 文件但语义是 cron（10.3.6 误挂，桩注释 ⤵️ 11.10）；11.10 公开函数落地后成误导性死代码（桩签名 `(requestID, sessionID) any` 与真实 5 参实现失配）；顺带清理 any 债务 | 处置表 #1 已行内补注；删除见 892873c6 |
| 6 | app_gateway 装配补 `SetGlobalAgentClient(agentClient)`（计划 Task 12 Step 2 装配块无此行） | Python `ensure_scheduler` 内嵌 fallback 依赖 `AgentServerClient.get_instance()` 单例解析，Go 对应全局注入；缺此行则内嵌调度器 fallback 静默失效 | plan Task 12 Step 2 已补行；§六 L174 描述已补齐三依赖；实现见 e68c3a69 |
| 7 | GetTool 查重用单查重 `len(existing) == 0`（计划稿为 `gerr != nil \|\| !toolAlreadyRegistered` 双重查重，且 `toolAlreadyRegistered`/`GetCronRuntimeBridge` 两函数 Go 侧不存在） | 计划稿过度设计：Python 无双重查重（`if not get_tool(id)` 单守卫）；bridge 为局部构造对齐 Python 每次 `CronRuntimeBridge()` 新建 | plan Task 12 Step 1 已就地修正（删不存在的辅助函数引用）；实现正确（20ce3133） |
| 8 | cron sessionID 时间戳用十进制（Python `format(ms, "x")` 十六进制） | 实施时格式直译遗漏；功能等价（均唯一）但与 Python 生成格式不一致，按 sessionID 解析时间戳的工具会不兼容 | 本轮归档暂不改码（无现存解析方；若 11.11 心跳或后续要加 sessionID 时间戳解析，一并统一为十六进制） |
| 9 | DeepAdapter `_update_session_tools` 未回填（Deep 模式会话级 cron 工具注册缺失） | 11.10 Task 11 只覆盖步骤 9 绑定（ContextVar 写侧），未覆盖步骤 16-17 后的注册消费侧——绑定后无人消费，**Deep 模式普通会话 agent 无 cron 工具可用**；Python L2966-3010（cron 注册分支 + send_file 分支） | 本轮代码回填补齐（cron 分支落地，send_file 分支留 ⤵️ 10.6.24）；plan 自检清单补核对项 |
