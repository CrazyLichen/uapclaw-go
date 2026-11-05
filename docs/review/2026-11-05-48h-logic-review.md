# 2026-11-05 48 小时上库质量审查

> 审查范围：`7e613612 .. d4ee946f`（2026-11-03 04:02 ~ 2026-11-05 02:06，非合并提交 246 个）
> 代码规模：2447 文件 / +404,862 / −30,508；其中 Go 508 文件 / +36,588 / −4,170（生产代码 270 文件），其余为上游 Web 前端整体迁移（1891 文件）
> 审查对象：提交原因与实现正确性；对齐 Python 0.2.5（`jiuwenswarm`）/ agent-core 0.1.20（`openjiuwen`）；桩与回填完整性；层级/步骤归属；稳定性、性能、可维护、并发安全
> 审查方式：逐批读生产代码 + 对照 Python 同位置源码；关键结论均在本地读码复核（下文 `文件:行号` 为 HEAD `d4ee946f` 基线）
> **只读审查，未修改任何代码**
> 基线状态：`gh run list` 最新一次 CI 四绿（run 41886196115）
> 说明：审查期间远端历史被重排（原 `cbb9ba21`/`b0e80868` 已不存在，内容合入 `793f0cfa`），本文行号与哈希一律以 `d4ee946f` 为准

---

## 零、结论摘要

| 级别 | 数量 | 一句话 |
|---|---|---|
| 严重 | 6 | bash 工具把非零退出码当成执行失败；关服不取消 team 流任务（依赖早已实现却没接线）；同一会话能被两条流同时跑；流消费中断后泵协程永久卡住；计划状态同步失败被吞掉；共享提示词构建器被多会话并发写（Go 会直接崩进程） |
| 一般 | 15 | 续答判据用了不存在的键、取消链路缺编排、入站主流程缺 6 个上游步骤、只读 RPC 产生写副作用、计划状态每步新建会话对象、记忆分支收窄、时区硬编码、上下文占用口径与 Python 相反、覆盖率豁免把整段入口代码豁免掉等 |
| 提示 | 13 | 前端产物 30MB 入库但 CI 每次重建、心跳 Stop 在未 Start 时永久阻塞、自造步骤未登记偏差、构造参数顺序漂移、调试文件入库等 |

**这次的好消息（先说清楚，避免只看问题）**：
1. Windows/大输出丢字节这个老问题，本轮 `9f70b00a` 用「把 Stdout 交给 os/exec 托管」的方式真的修对了（我本地跑了新增的 10 轮 >64KB、200KB 单行、跨块 GBK 用例，全绿）。这是三轮里第一次改对方向。
2. `ddcaea4a` 的外层 50 轮硬上限、`7997cb8d` 的无进展停止守卫，默认值/文案与 agent-core 逐字一致（`enabled=True, max_consecutive_empty_answers=3, min_answer_chars=20`）。
3. `826ef334`/`df4c45fd` 的 mode 解析 `ResolveAgentRequestMode`、`36f85699` 的 `ModeFromRaw`/`CanonicalizeMode` 与上游 `resolve_agent_request_mode` 分支顺序逐条对得上，属于本批质量最高的部分。
4. 流任务「登记—释放」配对是对的（成功/失败/取消/panic 四路都只删自己的槽位），`eca8bdcd` 这条守恒成立。

**这批需要警惕的反复模式**：`36f85699` 这种「一次性把默认值、枚举、签名、白名单、会话分配全换掉」的超大提交，虽然说明了根因，但同一提交里混了 6 类互不相关的语义变更，且首跑 CI 红灯（run 41868043045），修复型提交（`566f6e94`）全靠人工复跑门禁脚本才转绿。这种提交一旦回滚或部分 cherry-pick，语义会散架。

---

## 一、严重（6 项）

### S-01 bash 工具把「命令返回非零」当成「工具执行失败」，与 Python 完全相反

- **归属流程**：系统操作 → shell 执行 → 结果构造（`execute_cmd`）
- **相关提交**：`9f70b00a`、`f1006578`（两次都改了同一个文件，都没碰这块）
- **证据**：

  `internal/agentcore/sys_operation/local/utils.go:260-262`（`Invoke`）把 `cmd.Wait()` 的所有错误塞进 `Exception`：

  ```go
  } else if waitErr != nil {
      data.Exception = waitErr
  }
  ```

  `internal/agentcore/sys_operation/local/shell_operation.go:226-237` 只要 `Exception != nil` 就返回错误结果：

  ```go
  if invokeData.Exception != nil {
      return createErrResult(fmt.Sprintf("execution error: %s", invokeData.Exception), ...)
  }
  ```

- **Python 对照**：`agent-core/openjiuwen/core/sys_operation/local/shell_operation.py:530-548` 只把 `asyncio.TimeoutError` 当错误，其余一律返回 `StatusCode.SUCCESS` + `exit_code`（非零也成功，退出码交给调用方判断）。`utils.py:115-205` 的 `InvokeData.exception` 也只在超时分支赋值。
- **影响与触发**：`grep` 没匹配、`test -f` 为假、`diff` 有差异、`npm run build` 报错——只要退出码非零，Go 返回 `code≠0` + `"execute_cmd: execution error: exit status 1"`。模型看到「工具失败」会误判命令写法有问题、反复重试或改用别的方案；而 Python 版模型看到 `exit_code=1` + 正常 stdout，能正确理解「命令成功了，只是条件不成立」。
- **为什么算严重**：这是 agent 与操作系统交互的主通道，语义整体走反，且任何依赖退出码的脚本化流程（CI 自检、构建）都会被误判。
- **建议**：`*exec.ExitError` 不算 `Exception`，只填 `ExitCode`；`Exception` 仅保留超时/启动失败/读错误。

### S-02 关服时「取消所有 team 流式任务」是空桩，而它依赖的函数早已实现

- **归属流程**：AgentServer 停机序列（`Stop` 第 4 步）
- **相关提交**：本轮 S3 重构（`793f0cfa`/`eca8bdcd`）重写入口时保留了这两个空桩
- **证据**：

  `internal/swarm/server/agent_server.go:207-208` 在停机序列里调用：

  ```go
  // 4. 取消所有 team 流式任务（对齐 Python agent_ws_server.py L737）
  s.cancelAllTeamStreamTasks()
  ```

  而 `agent_server.go:598-610` 是空的：

  ```go
  // TODO(⤵️ Team): 等 Team 流式任务管理实现后回填
  func (s *AgentServer) cancelAllTeamStreamTasks() {
      // 未实现：等 Team 功能实现后回填
  }
  ```

  **依赖早就完成了**：`internal/swarm/agents/harness/team/team_manager.go:246-259` 有完整实现：

  ```go
  func CancelAllTeamStreamTasksAcrossManagers(reason string) {
      teamManagersMu.RLock()
      ...
      mgr.CancelAllStreamTasks(reason)
  }
  ```

- **Python 对照**：`jiuwenswarm/agents/harness/team/team_manager.py:4191 cancel_all_team_stream_tasks_across_managers`，由 `agent_ws_server.py` 停机路径调用。
- **影响与触发**：`Ctrl+C`/进程收到退出信号/测试 `TearDown` 时，team 模式的流式任务不会被取消：子代理 goroutine 继续跑、checkpointer 可能被半写、会话被标记为 active 但进程已走。**这正是「依赖章节已完成、桩却没回填」的典型**。
- **建议**：一行接线即可（`team.CancelAllTeamStreamTasksAcrossManagers("server_shutdown")`）；顺带核对 `stopScheduler`（`agent_server.go:594`）是否同样已有可调用实现（当前全仓无 Scheduler 注册点，需确认是「未实现」还是「已实现未接线」）。

### S-03 同一会话允许两条流同时跑：单槽覆盖 + 缺「忙时段转 steer」保护

- **归属流程**：入站主流程 → 流任务登记 → 网关并发模型
- **相关提交**：`9f4024cd`（把静默覆盖改成打 warn）、`eca8bdcd`
- **证据**：

  1. 每条消息一个 goroutine，无按会话串行：`internal/swarm/server/agent_server.go:413` `go s.handleMessage(ctx, envelope)`。
  2. 流任务登记是「单槽覆盖」，被覆盖的旧流只是打了 warn 就失去取消能力：`agent_server.go:442-461`。
  3. Go 侧完全没有「会话忙」概念：`grep -rn "busy\|Busy" internal/swarm/runtime/ internal/swarm/server/handle_chat.go` 零命中。

- **Python 对照**：`jiuwenswarm/gateway/message_handler/message_handler.py:4969-4980`：

  ```python
  session_busy = self._session_has_streams_blocking_processing_false(msg.session_id)
  if session_busy and steer_busy_im_chat(msg):
      ...  # 转为 steer，并入正在跑的那一轮
  ```

  即上游在 IM 场景把「用户在生成中又发了一条」变成 steer，**不会**并发起第二轮。
- **影响与触发**：IM/Web 用户连续发两条消息（很常见的操作）→ 两个 `chat.send` 同时对同一 session 跑：上下文消息交错写入、checkpointer 双写、先注册那条流再也取消不掉、用户可能只看到第二个回答而第一个回答的副作用（工具已执行）已经发生。
- **建议**：入站按 session 串行（或至少移植「忙则转 steer」），并把单槽改成「按 session 的流集合」。

### S-04 流式消费中断后只 `break`，泵协程永久阻塞，会话一直「活跃」

- **归属流程**：`AgentRuntime.Stream` 消费循环 → adapter 流泵协程
- **相关提交**：`793f0cfa`（新 Stream 语义）、`f1006578`（同批修了 sys_operation 的同类问题，但这里没修）
- **证据**：

  `internal/swarm/runtime/service.go:180-184`：

  ```go
  for chunk := range ch {
      if onEvent != nil && !onEvent(FromAgentResponseChunk(chunk)) {
          break            // ← 只 break，不 cancel ctx
      }
  }
  ```

  `internal/swarm/server/adapter/deep_adapter.go:1246` 起的泵协程是 `for raw := range rawCh`，向 `outCh`（容量 64）裸写，全函数无 `ctx.Done()`/`select`：

  ```go
  go func() {
      defer close(outCh)
      defer d.unmarkSessionActive(sessionID)
      for raw := range rawCh { ... outCh <- ... }
  }()
  ```

- **Python 对照**：`jiuwenswarm/server/agent_ws_server.py:4753-4770` finally 里 `keepalive.signal_stop()` + `runtime_stream.aclose()`；`runtime/service.py:2196-2200` 用 `aclosing(...)`，生成器被关闭即触发上游清理。
- **影响与触发**：客户端断连、单帧发送失败（`handle_chat.go:152-159` 返回 false）→ `break` → 之后 `outCh` 写满 64 条即永久阻塞 → `defer unmarkSessionActive` 永不执行（该 session 永远 active，后续 busy/状态判断全错）、`close(outCh)` 不执行、`rawCh` 生产侧随之堵死。**每断一次连就漏一组协程**。
- **建议**：`break` 之后 `cancel()`（`beginStreamTask` 有现成 cancel），并把泵协程的写入改成 `select { case outCh <- x: case <-ctx.Done(): return }`。

### S-05 计划状态同步失败被吞掉，用户看到成功、实际计划约束没生效

- **归属流程**：`AgentRuntime` 每轮前置的 plan 状态同步与后置退出检测
- **相关提交**：`826ef334`、`eca8bdcd`
- **证据**：

  `internal/swarm/runtime/service.go:229-237`：

  ```go
  res, err := r.planController.EnsureState(...)
  if err != nil {
      logger.Warn(logComponent).Err(err).Msg("ensure plan state failed")
      return              // ← 继续跑本轮，不告诉任何人
  }
  ```

  `service.go:243-252`（`checkPlanExit`）同样是 `Error` 日志后 `return`，`Invoke` 仍返回成功。

- **Python 对照**：`jiuwenswarm/runtime/service.py:1859-1870`（`ensure_state` 在 try 内），1892-1901 捕获后追加 `RuntimeEvent.error(ok=False)`；1911-1919 收集 `plan_error`，1967 直接 `raise plan_error`，stream 路径同形（2744）。
- **影响与触发**：计划状态写盘失败（checkpointer 抖动、磁盘满、会话目录权限）时：前端仍显示「已进入计划模式」，agent 实际按 normal 跑，`ExitPlanMode` 被工具白名单允许——**计划模式的约束被静默绕过**，而这类约束恰是用来防止误改生产配置的。
- **附带**：`docs/superpowers/specs/2026-11-04-plan-runtime-layer-alignment-design.md` §9 表格写「ensure_state 仅记录 plan_result，继续」，与上游事实相反，会误导后续实现，建议一并改。

### S-06 多会话并发写同一个提示词构建器：可能 `concurrent map writes` 直接崩进程

- **归属流程**：请求期运行时提示词注入（rail 的 `BeforeModelCall`）
- **相关提交**：`9902d520`（给 rail 自己的字段加了锁，但没解决真正的共享点）
- **证据**：

  构建器是 agent 上的常驻字段（`internal/agentcore/harness/deep_agent.go:459-464` `SystemPromptBuilder()` 返回 `d.systemPromptBuilder`），而 map 写入没有锁（`internal/agentcore/single_agent/prompts/builder.go:122-125`）：

  ```go
  func (b *SystemPromptBuilder) AddSection(section PromptSection) *SystemPromptBuilder {
      b.sections[section.Name] = section
      return b
  }
  ```

  每个请求都在改它：`runtime_prompt_rail.go:241-256`（time/runtime/language/env/git_status/browser_policy/trusted_dirs 七节）、`response_prompt_rail.go`、`avatar_rail.go`、`code_task_planning_rail.go`、`stream_event_rail.go`、`structured_ask_user_rail.go` 都是「取 agent 的 builder → `AddSection`/`RemoveSection`」。

- **Python 对照**：`jiuwenswarm/agents/harness/common/rails/stream_event_rail.py` 结构一致，但 Python 单线程 asyncio 且回调体内无 await，天然原子。
- **影响与触发**：同一 agent 实例被多会话共享（S3 spec §13 偏差 6 明确写了 agent 缓存键是 channel+mode+subMode+projectDir），两个会话同时进入 `BeforeModelCall` → Go 运行时 `fatal error: concurrent map writes`（**不可 recover，进程直接死**）；即便侥幸不崩，A 会话也可能拿到 B 会话的 language/mode/cwd。
- **建议**：`sections` map 加锁（或每个请求用 `Clone()` 出的 builder），并把「按请求注入」改成「按请求快照」——这是本轮新增 rail 注入后必须补的地基。

---

## 二、一般（15 项）

### M-01 `UpdateAgentRails` 的幂等靠非原子 nil 判断，并发会重复注册 rail

- 归属：适配器 rail 装配（`e9d68e44` 合并后的新函数）
- 证据：`internal/swarm/server/adapter/deep_adapter_rails.go:1005-1019` 依次 `ensureXXX()`，每个 `ensureXXX` 都是「`d.xxxRail != nil` 就返回」；`deep_agent.go:515-518` 的 `registeredRails = append(...)` 无去重。
- 影响：两个会话同时 `updateRuntimeConfig` → 同一 rail 注册两次 → 钩子执行两遍（提醒注入两次、工具重复注册、演进触发两次）。
- 建议：adapter 级 rail 装配加互斥，或 `RegisterRail` 按 (类型, 名称) 去重。

### M-02 续答判据用了不存在的键 `resume`，真实续答轮不会跳过 plan 同步

- 归属：`planEnabled`（`793f0cfa`）
- 证据：`internal/swarm/runtime/service.go:221-224`：

  ```go
  if resume, ok := params["resume"].(bool); ok && resume {
      return false
  }
  ```

  全仓没有任何地方写 `params["resume"]`。
- Python 对照：`jiuwenswarm/agents/harness/common/rails/interrupt/interrupt_helpers.py:160-171` 的 `is_interrupt_resume_payload`（判 `answers`/`request_id`/`source`+`evolution_meta.approval_transport`）；Go 侧连这个助手函数都没有（三个名字 grep 全零）。
- 影响：审批/中断的续答轮（带 `answers`）不会跳过 plan 同步，一旦命中「进入 plan」分支，会把计划提醒前缀插进答案轮的 query 并改写 mode。

### M-03 `CancelRequest` 只等于上游模块级函数，缺 AgentRuntime 层编排

- 归属：取消链路（`7942`/`cbb9` 内容合入 `793f0cfa`）
- 证据：`internal/swarm/runtime/request.go:549-606` 只做了 `request.py:578-639` 的事。
- Python 对照：`jiuwenswarm/runtime/service.py:1299-1486` 另有 `normalize_request`、intent 解析、team pause/stop、`_clear_pending_interaction`、`session_coordinator.cancel_execution`、closed 守卫、web cron 的 `target_request_id/expected_execution_id` 护栏。
- 影响：`pause` 被当普通中断处理；待决交互不清理；定向取消退化成「取消本会话当前流」。

### M-04 会话元数据在 agent 准入之前就写盘，失败也留痕

- 归属：`prepare_chat_turn` / `SyncChatRequestMetadata`（`e4616e92` 补测、`793f0cfa` 迁移）
- 证据：`internal/swarm/runtime/request.go:420-432` 先 `SyncChatRequestMetadata(...)`，`:469` 才 `mgr.GetChatAgent(...)`。
- Python 对照：`jiuwenswarm/runtime/request.py:452-455` 的 metadata_sync 在 `admit_request` 内，539 才 `get_agent_for_request`——准入失败不落盘。
- 影响：agent 创建失败/被拒仍刷新 `last_message_at`，污染侧边栏排序和「用户最近活跃」信号。

### M-05 `session.plan_status` 是只读 RPC 却建会话、且只看一个 channel

- 归属：新增 RPC（`9442c656`）
- 证据：`internal/swarm/server/handle_session_plan.go:46-53`：

  ```go
  if agent := s.agentManager.GetAgentNoWait(request.ChannelID, "", "", ""); agent != nil {
      if snapshot, ok := agent.PlanModeSnapshot(ctx, sid); ok { ... }
  }
  ```

  `PlanModeSnapshot` → `LoadPlanState` → `session.CreateAgentSession + PreRun`（`deep_adapter_plan.go:29-41`），且不 `PostRun`。
- Python 对照：`jiuwenswarm/server/handlers/session.py:195-231` 的 `_try_read_live_plan_mode`：**遍历所有 channel 的所有 agent**，只要 `get_live_session_instance(sid)` + `resolve_live_agent_session` 非 nil 才读；docstring 明确「Does not start a session or build an adapter」。
- 影响：(a) 只读查询产生会话副作用（每查一次建一个 Session，不回收）；(b) 只按 `request.ChannelID` 取一个 agent，同 channel 多 mode/project 时会读到别的 agent 的快照，`in_plan` 可能错；(c) 没有 live session 时 Python 回落 metadata，Go 会信任持久化状态。
- **注**：返回形状 `{session_id, in_plan}` 与上游一致（`session.py:289-293`），是 spec §6.6 写错了（见提示 6）。

### M-06 计划状态每步新建一个 Session 对象，读路径永不收尾

- 归属：`PlanStateAccessor` 适配（`826ef334`）
- 证据：`internal/swarm/server/adapter/deep_adapter_plan.go:29-41`（`openPlanSession` 每次 `CreateAgentSession`+`PreRun`）、`:44-53`（`LoadPlanState` 读完直接返回，无 `PostRun`）、`:68-71`（`SwitchPlanMode` → `d.SwitchMode` 内部又建一个 session，`deep_adapter.go:1765-1791`）、`:74`（`CommitSession` 是空实现，注释以「已完成收尾」解释）。
- Python 对照：`jiuwenswarm/runtime/plan.py:100-144` 的 `open_state_session` 先 `get_live_session_instance` → `ensure_live_session_instance` → 才兜底 throwaway，注释明确警告 throwaway 会与首轮绑定竞争；`:210-247` 一次 open 后 load/switch/save/commit 复用同一 session。
- 影响：每轮 chat 至少 2~3 个 Session 对象 + 2 次 PreRun 读盘；`Load` 路径的对象永不 PostRun；状态写在「非运行中的会话对象」上，是否对当轮生效取决于 checkpointer 往返（**待确认**：若运行期状态缓存在 Session 对象上，可能出现「切了但本轮不生效」）。

### M-07 入站主流程仍缺 6 个上游步骤，且计划里没登记

- 归属：网关 `forwardLoop`（本轮多次改动的文件）
- 证据：Go 现有步骤为 `handleChannelControl → ApplyChannelState → EnsureInboundSession → IsUnknownTeamModeSend → hook → …`（`internal/swarm/gateway/message_handler/forward_loop.go:116-166`）。上游 0.2.5 `message_handler.py:4940-5010` 在这之间还有：

  | 上游步骤 | Go 现状 |
  |---|---|
  | `prepare_im_session_input(msg)`（IM 会话输入走公共投递） | 无（grep 零命中） |
  | `_resolve_external_channel_session(msg)`（在 `_apply_channel_state` **之前**） | 无 |
  | `_is_session_input_message` + `validate_session_input` + `input_mode`/`runtime_mode` 归一 | 无 |
  | `session_busy` + `steer_busy_im_chat`（见 S-03） | 无 |
  | `_is_unsupported_non_stream_team_send` 拒绝 | 无（只做了 unknown team 那条） |
  | `_maybe_register_godview` | 无 |

  且 `IMPLEMENTATION_PLAN.md` 中 `godview / session_input / 非流式 team` 均 0 命中——属**静默缺失**，不是已登记的延后项。
- 影响：会话级输入校验、非流式 team 拒绝、数字分身订阅全部缺失；缺的这几步恰好是「非法输入不该进 agent」和「team 该拒就拒」的门卫。
- 建议：要么补齐，要么在计划中显式登记 ⤵️ 并写明回收条件（规则 9.6）。

### M-08 手工记忆写入把上游的分支链收窄了（ACE section / ReMe when_to_use / Cognition 全缺）

- 归属：`TaskMemoryService.AddMemory`（`dc0c7089`、`e407921e`）
- 证据：`internal/agentcore/context_evolver/service/task_memory_service.go:378-382` 入口直接 `if req.Section == "" { req.Section = "general" }`；`:409-416` ReMe 强制要求 `content` 与 `when_to_use`；switch 无 `Cognition` 分支。
- Python 对照：`task_memory_service.py:1075-1082` 是 `section or attributes.domain or query or "general"`；`:1005-1014` 是 `when_to_use or query or description`；`:1027-1053` 有完整 Cognition 分支。
- 影响：调用方传 `section=""` 想走 `attributes.domain` → Go 落 general，记忆 ID 与 Python 不一致；ReMe 只传 query → Go 报错，Python 正常写；Cognition 算法无法表达（`AddMemoryRequest` 也没有 `Attributes` 字段）。

### M-09 关系时间写死 `+00:00`，非 UTC 时区标签错

- 归属：图记忆关系抽取提示词（`9fe7a2db`）
- 证据：`internal/agentcore/memory/graph/extraction/prompts/entity_extraction/base.go:121`、`:127`：

  ```go
  t.Format("2006-01-02T15:04:05+00:00")
  ```

  `Format` 里 `+00:00` 是字面量，不随时区变。
- Python 对照：`entity_extraction/base.py:102-107` 用 `.isoformat(timespec="seconds")`，UTC+8 输出 `+08:00`。
- 影响：任何带时区偏移的关系，提示词里时间标签恒为 `+00:00`，模型据此推断关系有效期会偏。修法：布局改 `-07:00`（UTC 输出 `+00:00`，与 Python 一致）。

### M-10 上下文占用分子用 `total_tokens`，与两版 Python 都相反，注释还写着「对齐」

- 归属：`context.usage` 展示 / ContextStats（`39eef1de`）
- 证据：`internal/swarm/agents/harness/common/rails/stream_event_emit.go:276-281` 直接返回 `usage.TotalTokens`；`:223-225` 计算占比无 clamp。另两处同口径：`context_engine/interface/types.go:438-449`、`context_engine/context/session_model_context.go:1072-1090`。
- Python 对照：agent-core `context/context.py:841-856` docstring 明写「total_tokens includes output tokens and must not be used as the context input size」；jiuwenswarm `stream_event_rail.py:1673-1681` 顺序取 `input_tokens → prompt_tokens → total_tokens`，并有单测断言 1200（非 2000）。
- 影响：长输出时占比被抬高，可 >100%（`total_tokens` 可能超窗口）；且 Go 的 `UsageMetadata`（`llm/schema/usage_metadata.go:10-31`）本就带 `InputTokens`，直接不用，等于放弃了上游「输入 token 才代表上下文占用」的语义。若这是 11-03 的有意产品偏差，注释里的「对齐 Python」必须改掉。

### M-11 抽取提示词的 `refDict` 与 Python 形状不同：数组丢 `items`、嵌套结构体记成 `object`、键序变字母序

- 归属：图记忆抽取提示词（`bc4eb10b` 反射生成）
- 证据：`internal/agentcore/memory/graph/extraction/base.go:290-330` 只写 `{"type": ...}`，`jsonSchemaType` 把 struct 映射成 `"object"`；`prompts/entity_extraction/base.go:34` 直接 `for k, v := range refDict` 渲染（map 序不确定）。
- Python 对照：`extraction/base.py:69` 取完整 `properties`（`duplicate_ids` 带 `"items":{"type":"integer"}`），按声明序渲染。
- 影响：`EntityDuplication.duplicate_ids` 在提示词里只剩 `"type":"array"`，模型对字段取值格式的约束变弱；键序不稳定会让提示词文本不可复现（缓存/对拍都受影响）。

### M-12 `Recover` 仍把读盘错误丢掉，损坏条目静默降级成「无状态」

- 归属：checkpointer 恢复（`702b9320`）
- 证据：`internal/agentcore/session/checkpointer/persistence.go:269-270`：

  ```go
  dumpTypeBytes, _ := pipelineGetResult(results, 0)
  blob, _ := pipelineGetResult(results, 1)
  ```

  随后 `dumpTypeBytes == nil` 直接返回 nil，只留一条 Debug「not found」。
- Python 对照：`persistence.py:229-239` 语义相近，但 `pipeline.get` 侧失败会直接抛。
- 影响：条目半写/损坏（崩溃中断）时会话上下文静默丢失，排查时只看到「not found」。本批「损坏条目回填 Err」的目标只完成一半。

### M-13 改动行覆盖率豁免把「整段入口代码」登记掉了（本轮累计 1382 行）

- 归属：CI 门禁配套（`566f6e94`、`e4616e92`、`531`/`532`/`b572`/`a74e` 等系列豁免提交）
- 证据：`git diff <base>..HEAD -- .coverage-exempt .integration-coverage-exempt` 新增行级豁免合计 1382 行，其中与「只豁免插入的注释行」不符的典型：

  | 文件 | 豁免区间 | 实际内容 |
  |---|---|---|
  | `internal/swarm/server/handle_message.go` | L96-L252 | 整个方法分发 switch（157 行，本轮重构的核心入口） |
  | `internal/agentcore/controller/modules/intent_toolkits.go` | L255-L439 | 整个 `initToolSchemaChoices` 函数体 |
  | `internal/evolving/optimizer/skill_call/team_optimizer.go` | L265-L468 | 整个 `GenerateUserPatch` 函数体 |

  `566f6e94` 提交信息写「只覆盖插入注释的那几行区间，不整文件豁免」，但上述区间等于「整函数豁免」——函数后续任何真实改动都不再有回归信号。
- 影响：门禁的初衷是「改动行必须被测到」，现在为「补注释」开的口子把入口和核心函数一起豁免了。这不是本轮独有，但本轮把它扩大了一倍。
- 建议：豁免按「注释行号列表」而非「区间」登记；或改成「纯注释 diff 才跳过」，而不是登记固定行段。

### M-14 集成测试目录里入库了调试文件，零断言

- 归属：提交纪律（`12319332` 附近）
- 证据：`tests/integration/swarm/server/adapter/attachment_mode/dbg2_test.go` 全文件只有 `s.T().Logf(...)`，无任何断言，名为 `Dbg2Suite/TestDbgRail`；文件里连 `firstN` 这种一行小工具都留着。
- 影响：集成审计要求「有效测试密度 ≥1（断言 ≥2）」是按目录聚合的，这种文件能混过去；同时它会真实跑一遍套件（拖慢 CI）却什么都不验证。文件保留在仓库里会让人以为这条链路有覆盖。

### M-15 输入侧「会话忙」与 steer 语义缺失导致网关无背压（与 S-03 同源，单列以便排期）

- 归属：网关 → runtime 的请求准入
- 证据：`internal/swarm/runtime/service.go` 全包无任何「同会话在跑」判断；`prepare_chat_turn`（`request.go:379-470`）也不校验。
- Python 对照：`runtime/service.py:1299-1486` 的 `is_session_running`（3085）+ gateway 侧 steer 转换，两处都有。
- 影响：并发请求不会排队也不会被拒，直接进 agent。配合 S-03 的单槽登记，等于「无背压 + 覆盖式取消」。

---

## 三、提示（13 项）

1. **前端 `dist` 30MB 入库，而 CI 每次重建并上传产物**：`internal/swarm/channels/web/frontend/dist` 30MB/128 文件在本批入库；`.github/workflows/ci.yml:42-66` 每次 `npm ci && npm run build` 并作为 artifact 下发给其他 job。仓库里那份只在无 CI 构建时生效，两边必然漂移；且每次前端迭代产生数万行 diff（本批 1891 文件）。建议移出版本库或发布时再生成。
2. **`StreamKeepalive.Stop()` 在未 `Start()` 时永久阻塞**：`keepalive.go:88-91` `<-k.done`，而 `done` 只在 `Start` 的 goroutine 里关闭。当前仅 `handle_chat.go` 使用（必先 Start），但这是可被后人踩到的死锁陷阱，建议加 `started` 标记。
3. **`plan.go` 的 `blockEntry` 是自造拆分且与上游分支不等价**：`internal/swarm/runtime/plan.go:363-380` 无条件清 `plan_slug` 并落盘，而 Python `plan.py:225-236` 只有「slug 非空」那条分支清，`_exited_sessions` 分支不清。另 `plan.go:196-201` 的 `if st.PlanSlug != ""` 是死代码（非空已在 179 行拦截并 return）。
4. **`SessionMessageRequiresQueue` 是死代码 + 测试把桩行为锁死**：`plan.go:291-306` 恒返回 false 并打 Warn，生产无调用方，`plan_test.go:258-262` 还断言 `require.False`。测试固化「未实现」会阻碍后续回填时改测试。
5. **一批自造步骤未写进 spec 偏差清单**：`planEnabled`、`ApplyPlanResult`、`ApplyCanonicalMode`、`blockEntry`、`CommitSession` 空实现，都是 Go 自造（Python 在 `ensure_state` 内联改写 params）。规则 9.6 要求登记偏差并写回收条件，目前只有代码注释。
6. **spec §6.6 把 `plan_status` 的返回契约写错了**：spec 写 `{session_id, plan_mode, mode}`，而代码与上游 `session.py:289-293` 都是 `{session_id, in_plan}`——**代码是对的，文档要改**。这类「文档与实现对不上」本轮还有 2 处（spec §9 plan 错误处理描述、spec §6.4 条件登记），建议统一以代码为准回改 spec。
7. **未登记 `NewUapClaw()` 旧名**：`internal/swarm/server/runtime/interface.go:111` 仍导出 `NewUapClaw()`（返回 `*UapClawSwarm`），规则 8「改名后旧符号 0 命中」未达成。
8. **`handle_chat.go:90-92` 丢弃 `Payload==nil` 的事件**：Python 会照发空 payload 事件，前端若依赖 event_type 心跳会少帧。
9. **ctx 被替换成 `context.Background()`**：`agent_server.go:122`、`handle_chat.go:225`（`SendPush`）——请求取消后推送还会继续尝试，且无法超时。
10. **`ContextEvolvingReActAgent` 构造函数参数顺序与 Python 不一致**：Go 是 `(…, inject, autoSummarize, autoSummarizeMattsMode, persistType, …)`，Python 是 `(…, inject, persist_type, persist_path, milvus_host, milvus_port, milvus_collection, auto_summarize, auto_summarize_matts_mode)`（`context_evolving_react_agent.go:93-107` vs `context_evolving_react_agent.py:47-60`）。相邻 bool/string 同型参数，调用点靠位置猜，极易错位。
11. **`matts_k <= 0` vs Python `is None`**：`trajectory_generator.go:220-225` 把「显式传 0」当成未配置（跑 3 次试验），Python 传 0 就是 0 次。
12. **`InvokeLLM` 在重试 sleep 期间释放信号量，注释说「对齐 Python」但事实相反**：`base.go:744-746` vs `graph_memory/base.py:504-517`（Python `async with self._semaphore` 包住整个重试循环）。429 时 Go 的在途请求更多。
13. **`dbg2_plan` 等测试辅助命名进入生产可执行路径**：`testhelpers.NewPlanEnv(s.T(), ..., "dbg2_plan")` 会创建真实会话目录，跑完留着（属测试卫生，非功能问题）。

---

## 四、桩与回填核对（用户关注点 2）

本轮扫描 `internal/` 下 114 处 `TODO(⤵️...)`/「未实现」/「等…后回填」标记，逐条核对「依赖章节是否已完成」：

| 桩 | 依赖现状 | 结论 |
|---|---|---|
| `AgentServer.cancelAllTeamStreamTasks`（`agent_server.go:598`） | `CancelAllTeamStreamTasksAcrossManagers` 已实现（`team_manager.go:246`），10.6.19 标 ✅ | **该回填未回填**（S-02） |
| `AgentServer.stopScheduler`（`agent_server.go:586`） | 全仓无 Scheduler 注册点（`TaskScheduler` 是 controller 模块，不是上游 `_scheduler`） | 依赖确实未建，保留合理，但应在计划中写明 |
| `AgentServer.resetHarnessPackagesState` / `bootstrapInternalJiuwenbox` / `startTeammateBootstrapDaemon` | AutoHarness 包管理、JiuwenBox、team bootstrap 守护均未迁移 | 保留合理 |
| `runtime.SessionMessageRequiresQueue`（`plan.go:291`） | 跨会话消息链路整体未迁移 | 保留合理，但当前是死代码（提示 4） |
| `browser_move` 15 处 `TODO(#9.38-49)` | Playwright MCP 未迁移 | 保留合理 |
| `handle_session_plan` 的 live 读取 | `get_agent_for_session_nowait` 缺失（S3 spec 偏差 8 已登记） | 已登记，属偏差 |

**另一个方向的缺口（不是桩，是静默少做）**：入站主流程 6 个上游步骤（M-07）、`TurnOutcomeTracker`/`_persist_repeated_session_question`（S3 spec 偏差 3 已写 ⤵️，但 `IMPLEMENTATION_PLAN.md` 无对应回填行）。建议把 spec 的 ⤵️ 逐条落到计划表，否则下次同步无法判断"是否已回填"。

---

## 五、对齐性复核（抽查通过项，非缺陷）

为避免只列问题，以下是我逐条对过、确认与 Python 一致的实现：

| 位置 | 对齐内容 |
|---|---|
| `runtime/request.go:163-234` | `resolve_agent_request_mode` 分支顺序、`plan/fast` 折叠、`team` subMode 置空、`work_mode` 重折算，逐行对应 |
| `runtime/plan.go:134-209` | `ensure_state` 主流程（team 跳过 → 单 agent 判定 → may_hold_state → should_sync → 锁 → load → 分支 → switch → 事件 → 激活提醒） |
| `runtime/service.go:210-226` | `planEnabled` 的 team/should_sync 判据（`resume` 键除外，见 M-02） |
| `harness/deep_agent.go` 外层 50 轮 | 常量 50、`>50` 判据、文案、「result_type=error + stop_reason=MaxOuterRounds」全部一致 |
| `harness/schema/config.go` 无进展守卫 | 默认 `enabled=true / 3 / 20`，字段名与上游一致 |
| `react_invoke.go:753-762` + `react_model_call.go:193-208` | 拒图降级（造提示消息 + 标志位 + 下一轮 continue）与 `react_agent.py:1850-1859/3160-3166` 一致 |
| `framework/llm/.../openai/client.go:356-360` | 流内 error 帧转 StreamError，避免被当空块丢弃 |
| `message_handler/channel_state.go:609-613` | `knownTeamModeInputs` 排序派生，与上游 `sorted(...)` 一致 |
| `cache` 相关：`llm_temperature` 通路 | 代码/config/schema 0 残留，旧实现删净（规则 8 达标） |
| `graph ReadableSchema` | 反射按声明序输出，类型名 (`list[EntityDeclaration]`/`list[int]`/裸 `dict`) 与 Python 一致，非结构体输入返回 `("", nil)` 不 panic |

---

## 六、建议处置顺序

1. **立刻**：S-01（退出码语义）、S-06（共享 builder 加锁）、S-04（break 后 cancel + 泵协程 select）——三条都是「一脚踩下去就出事」的类型，改动量都很小。
2. **本批内**：S-02（一行接线）、S-05（plan 失败上抛，至少回一帧 error）、S-03/M-15（入站按会话串行或移植 steer）。
3. **下批**：M-01、M-02、M-04、M-05、M-06、M-07（补齐或登记）、M-08、M-09、M-10（改注释或改回口径）。
4. **收尾**：M-13（豁免改成注释行列表）、M-14（删调试文件）、提示 1/3/4/5/6/7 的文档与死代码清理。

---

## 七、待核项（需要真实环境或上游确认）

1. `planStateStore.LoadPlanState` 用新建 Session 读状态，**对当轮是否真的生效**取决于运行期状态缓存落在 Session 对象还是 agent 全局（代码里两者都有迹象），需在真 LLM 集成下验证「进 plan 后第一轮工具就被锁」。
2. `handle_session_plan.go` 的 `GetAgentNoWait(channel, "", "", "")` 在同 channel 多 mode 场景下是否真的会取错实例（需要多 mode 并发用例）。
3. `context.usage` 用 `total_tokens` 是 11-03 的**有意产品裁决**（提交内有说明），需产品确认是否维持；若维持，注释与提交信息必须改，避免后续再把「对齐」当依据。
4. `MEMORY`/`db_max_concurrent` 覆盖下 `relationDedupe` 的实际并发峰值（当前上限 8，429 时是否仍打满）。
5. `cancelAllInflightWork` 传的是 `context.Background()`，停机超时行为与 Python 是否一致（Python 侧 `reason` 文案已对齐，超时策略未见）。
