# 2026-10-02 审查 S 级修复设计

> 审查文档：docs/review/2026-10-02-48h-logic-review.md
> 验证日期：2026-10-02
> 修复范围：38 个 S 级问题中 37 个确认修复，1 个跳过（S-16 误判），1 个已不存在（S-39）

## 修复决策汇总

### 模块一：TeamAgent 核心流程（S-01~S-15）

| 编号 | 问题 | 修复方案 |
|------|------|----------|
| S-01 | Invoke/Stream 核心流程未实现 | 实现完整流程：Start → EnqueueUserInput → EnqueueMailboxAfterFirstIteration → streamQueue 循环读取 → FinalizeRound |
| S-02 | 缺少 EnqueueMailboxAfterFirstIteration | 在 CoordinationKernel 添加方法，对齐 Python kernel.py:391-401 |
| S-03 | 缺少 FinalizeRound | 在 CoordinationKernel 添加方法，对齐 Python kernel.py:421-434 |
| S-04 | 缺少 TeamCompletion callback 注册 | Configure 末尾查找 TeamSkillRail 并注册 completion callback |
| S-05 | Setup 传 nil blueprint/infra | 改为 a.coordination.Setup(role, a.Blueprint(), a.Infra()) |
| S-06 | Pause 缺少 Leader 步骤 | 添加 CancelRecoveryTasks/ShutdownAllHandles/persistLifecycleState/publishTeamStandby |
| S-07 | Stop 缺少 Leader 步骤 | 添加 CancelRecoveryTasks/ShutdownAllHandles |
| S-08 | MarkLiveTeammates 过滤逻辑不对齐 | 增加 spawned_handles 检查、跳过 leader/UNSTARTED |
| S-09 | RecoverForExistingSession 缺少 StopCoordination | 先调用 StopCoordination 再委托 sessionManager |
| S-10 | SubscribeTransport 严重简化 | 完整对齐：TeamTopic 遍历、self-filter、direct_message_handler 注册、event_listeners 通知 |
| S-11 | UnsubscribeTransport 简化+ctx.Background | 完整对齐：取消 direct_message_handler、遍历 subscribedTopics 取消、清空列表、传播 ctx |
| S-12 | ShutdownSelf 缺少 CloseStream | 末尾添加 CloseStream() 调用 |
| S-13 | LookupHumanAgentRuntime 缺少 IsHumanAgent | 先检查 backend.IsHumanAgent(memberName) |
| S-14 | Broadcast 缺少 UserInbox 对齐 | 改用 UserInbox.Broadcast() |
| S-15 | HumanAgentSay 缺少 HumanAgentInbox | 改用 HumanAgentInbox.Send() |

### 模块二：适配器/DeepAdapter/CodeAdapter（S-16~S-23）

| 编号 | 问题 | 修复方案 |
|------|------|----------|
| S-16 | CodeAdapter.CreateInstance 缺 updateRuntimeConfig | **跳过**（审查误判：Python CreateInstance 也不调用 _update_runtime_config，该方法仅在 process_message 时调用） |
| S-17 | CodeAdapter.updateRuntimeConfig 不被委托调用 | DeepAdapter 新增 runtimeConfigUpdater func 字段，CodeAdapter 构造时覆写；3 处 d.updateRuntimeConfig 改为 d.runtimeConfigUpdater |
| S-18 | ProcessMessageStreamImpl 缺 ResetAbort | markSessionActive 后添加 d.streamEventRail.ResetAbort(sessionID) |
| S-19 | isOutcomeEvent 类型断言错误 | payload["_evolution_meta"].(map[string]string) → .(map[string]any) |
| S-20 | CodeAdapter.CreateInstance 缺 instance 属性设置 | 步骤 21.1 增加 c.deep.instance.SetUapswarmAdapterMode("code") 等调用 |
| S-21 | handleEvolutionApproval ctx.Background | 增加 ctx context.Context 参数，从 HandleUserAnswer 传入 |
| S-22 | pushEventToFrontend ctx.Background | 增加 ctx context.Context 参数，从 watchEvolutionAndPush 传入 |
| S-23 | processTeamMessageStream req 类型为 any | req 类型改为 *schema.AgentRequest，从中解析 sessionID/channelID/requestID，inputs 仅取 query 等业务参数 |

### 模块三：Team Tools（S-24~S-31）

| 编号 | 问题 | 修复方案 |
|------|------|----------|
| S-24 | SpawnMemberTool 漏检 prompt | human_agent 路径同时校验 model_name 和 prompt |
| S-25 | inputs["prompt"] 裸断言 panic | 改为 prompt, _ := inputs["prompt"].(string) |
| S-26 | ViewTaskTool claimable 未传 PENDING | ListTasksWithDeps 增加 status 参数，claimable 传 "pending" |
| S-27 | ViewTaskTool list 未传 inputs["status"] | list 路径传入 inputs["status"] |
| S-28 | ListMembersTool 返回字段不完整 | 完全对齐 Python model_dump()，返回 TeamMember 所有字段 |
| S-29 | SubmitPlanTool 缺少 message 字段 | 增加 "message": "Member plan submitted. Wait for leader approval before execution." |
| S-30 | multicast 缺全员覆盖拒绝 | 检查 deduped 是否覆盖 roster 全员，是则拒绝引导用 to='*' |
| S-31 | CancelMember 返回值被忽略 | 3 处检查返回值，失败记录 Warn 日志 |

### 模块四：其他（S-32~S-38）

| 编号 | 问题 | 修复方案 |
|------|------|----------|
| S-32 | AfterInvoke goroutine ctx 取消 | 使用 context.WithoutCancel(ctx) |
| S-33 | handleInteractiveInput ctx.Background | 从 Interact 传入 ctx |
| S-34 | resolveRecipients ctx.Background | 增加 ctx context.Context 参数 |
| S-35 | PoolEntry 接口方法不完整 | 补充 HasActive/ListTeamNames/TeamsForSession 等方法 |
| S-36 | Viking Initialize 健康检查返回 error | 静默降级：warning + client=nil + initialized=true + return nil |
| S-37 | autoRecall 截断 rune/字节不匹配 | 字节数对比 len(truncated) > remaining + rune 安全截断 |
| S-38 | 多模态消息 Text() 返回空串 | IsText() 为 false 时回退到 String() 或提取 parts 文本 |

## 关键设计决策

1. **S-17 函数指针方案**：DeepAdapter 新增 `runtimeConfigUpdater func(ctx context.Context, cfg *runtimeConfig)` 字段，默认指向 `d.updateRuntimeConfig`，CodeAdapter 构造时覆写。3 处调用点（CreateInstance:556、ProcessMessageImpl:875、ProcessMessageStreamImpl:1069）改为 `d.runtimeConfigUpdater(ctx, cfg)`。

2. **S-37 修 bug 而非对齐 Python**：Python 的 `body[:remaining]` 存在同样的 rune/字节单位 bug，Go 版本选择修复 bug（字节数对比 + rune 安全截断），比 Python 实现更正确。

3. **S-23 对齐 Python 的 req/inputs 分工**：Python 从 `request` 对象获取元信息（session_id/request_id/channel_id），从 `inputs` 获取业务参数（query）。Go 对齐此区分。

4. **S-16 跳过原因**：验证发现 Python 的 CodeAdapter.create_instance() 和 DeepAdapter.create_instance() 都不调用 _update_runtime_config。该方法仅在 process_message_impl/process_message_stream_impl 中调用。审查报告将 CreateInstance 中缺失调用误判为问题。
