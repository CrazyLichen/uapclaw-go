# 代码审查报告 — 2026-06-29

> 审查范围：最近 24 小时内 26 个提交
> 审查领域：6.25 Runner 单例 / 6.26 RunnerConfig / 6.27 LocalMessageQueue / 6.28 Spawn 子进程 / 6.29 Agent Prompts / 编码规范修复 / build/lint 修复
> 审查人：Code Review Agent

---

## 提交清单

| 提交 | 说明 |
|------|------|
| 987235c | fix(build): 修复 Windows 交叉编译 — syscall.Kill/O_NOFOLLOW 平台分离 |
| d5c13ef | fix(lint): 修复 golangci-lint 问题 |
| a9d1cdc | style: 修复 gofmt 格式问题 — 15个文件格式化对齐 |
| 92cc61b | style: 修正编码规范问题 — 英文注释改中文、声明顺序调整、doc.go修正 |
| 6c5309e | feat(6.29): 实现 warnMissingSkillReadFileTool |
| 8e9b48b | docs(plan): 6.29 Agent Prompts 标记为已完成 |
| 1a911e2 | test(6.29): 删除迁移的 prompts 测试，适配新签名 |
| 80bbf50 | refactor(6.29): 迁移 PromptSection/SystemPromptBuilder 至 prompts 包 |
| 3213110 | feat(6.29): 创建 prompts 包 |
| 20e84fb | docs(plan): 添加 6.29 Agent Prompts 实现计划 |
| 6497b5e | docs(specs): 添加 6.29 Agent Prompts 设计文档 |
| 8c1ab63 | fix(runner): 对齐 Python 6.25/6.26 实现差异修复 |
| 18f7e14 | fix[spawn]: 消除 handle.go 重复常量 |
| 98d7d22 | feat[spawn]: 实现 6.28 Spawn 子进程 |
| 795735f | refactor(single_agent): 删除 reexports.go |
| 99ee248 | refactor(session): 重命名 WithWorkflowSessionSessionID |
| 485214d | test(runner): 补充测试覆盖率至 ≥ 85% |
| a6aff44 | docs: 更新 runner/doc.go，更新 IMPLEMENTATION_PLAN 6.25 |
| 0aeb155 | test(runner): 适配新签名，新增测试 |
| d0f9af4 | feat(runner): 重写 Runner 结构体 + 全局实例 + 全部包级函数 |
| 1f2a761 | feat(runner): 新增 AgentRef/WorkflowRef 类型 |
| 1d29e35 | refactor(single_agent): ReActAgent 删除 base 字段 |
| 220b3ad | refactor(single_agent): 删除 BaseAgent 结构体 |
| 3ba8f5f | docs: 添加 6.25 Runner 单例实现计划 |
| e68493a | docs: 添加 6.25 Runner 单例设计文档 |
| fdcfd62 | feat(runner): 实现 6.26 RunnerConfig + 6.27 LocalMessageQueue |

---

## 一、严重问题（9 个）

### S1. [Spawn] stdout 并发读取竞争 — 多 goroutine 竞争同一个 io.Reader

**文件**: `internal/agentcore/runner/spawn/handle.go`

**描述**: `performHealthCheck` 和 `waitForShutdownAck` 都会调用 `ReadMessage(h.stdout)` 读取 stdout，但父进程可能同时在 `ReceiveMessage()` 中读取同一个 `h.stdout`。多个 goroutine 同时从同一个 `io.Reader` 读取会导致数据窜乱或 panic。

**Python 参考**: Python 使用 asyncio 单线程模型，不存在并发读同一个 stream 的问题。

**影响**: 生产环境中可能导致协议消息解析失败、健康检查误判、关闭流程异常。

**建议**: 对 `h.stdout` 的读取加互斥锁保护，或引入消息分发机制（一个 goroutine 专职读取，通过 channel 分发）。

---

### S2. [Spawn] stdin 并发写入竞争 — 多 goroutine 竞争同一个 io.Writer

**文件**: `internal/agentcore/runner/spawn/handle.go`

**描述**: `SendMessage` 对 `h.stdin` 的写入没有锁保护。多个 goroutine 可能并发调用 `SendMessage`（健康检查 + 正常通信 + 关闭），NDJSON 消息可能交错写入，导致子进程解析失败。

**Python 参考**: Python 的 `send_message` 在 asyncio 单线程中不存在并发写问题。

**建议**: 在 `SendMessage` 中对 `h.stdin` 加写锁保护。

---

### S3. [Spawn] 健康检查响应不跳过非目标消息

**文件**: `internal/agentcore/runner/spawn/handle.go:411-432`

**描述**: `waitForHealthCheckResponse` 读取下一条消息后直接返回，未校验是否为 `HEALTH_CHECK_RESPONSE` 类型。如果子进程先发回 OUTPUT/DONE 等消息，会被误当作健康检查响应。

**Python 参考**: Python 的 `_wait_for_health_check_response` 用 while 循环跳过非健康检查响应的消息。

**建议**: 循环读取直到收到 `HEALTH_CHECK_RESPONSE` 类型消息，跳过中间的其他消息。

---

### S4. [Spawn] waitForShutdownAck 不跳过非 SHUTDOWN_ACK 消息

**文件**: `internal/agentcore/runner/spawn/handle.go:434-464`

**描述**: 读取一条消息后直接判断，如果子进程先发回 DONE/OUTPUT/STREAM_CHUNK，会被误判为"未收到 ACK"。

**Python 参考**: Python 的 `_wait_for_shutdown_ack` 循环读取，跳过非 SHUTDOWN_ACK 消息，且 DONE 消息也视为 ACK。

**建议**: 循环读取直到收到 SHUTDOWN_ACK 或 DONE，跳过中间消息。

---

### S5. [Spawn] ParseSpawnAgentConfig 对 class_agent 类型丢失子类字段

**文件**: `internal/agentcore/runner/spawn/config.go:93-113`

**描述**: 对 `class_agent` 先解析为 `ClassAgentSpawnConfig`，但返回的是 `cfg.SpawnAgentConfig`（嵌入的基类），丢弃了 `AgentName`/`InitKwargs` 字段。调用方无法获取 class_agent 的特有字段，子进程执行 Agent 时无法实例化正确的 Agent。

**Python 参考**: `parse_spawn_agent_config` 对 `CLASS_AGENT` 返回 `ClassAgentSpawnConfig` 实例，保留全部字段。

**建议**: 返回值类型改为返回接口或具体类型（如 `ClassAgentSpawnConfig`），或者在 `SpawnAgentConfig` 中嵌入 `agent_name`/`init_kwargs` 以不丢失数据。

---

### S6. [Spawn] ForceKill 不停止健康检查

**文件**: `internal/agentcore/runner/spawn/handle.go:287-315`

**描述**: `ForceKill()` 不停止健康检查、不设置 `shutdownRequested`。ForceKill 后健康检查仍在运行，会向已死进程发消息导致错误日志刷屏。

**Python 参考**: `force_kill()` 先设 `_shutdown_requested=True`，再调 `stop_health_check()`。

**建议**: `ForceKill` 中先停止健康检查，设置 `shutdownRequested=true`。

---

### S7. [Runner] getRunner() 存在竞态条件 + SetGlobalRunner 与 runnerOnce 不协同

**文件**: `internal/agentcore/runner/runner.go:583-596, 82-86`

**描述**:
1. 当 `globalRunner` 不为 nil 时，读锁释放后、返回前，另一个 goroutine 调用 `SetGlobalRunner(nil)` 置空，`r` 仍指向旧实例但 `globalRunner` 已被替换。
2. 更严重的是，`initRunner()` 内部用 `runnerOnce.Do()` 但 `runnerOnce` 不会随 `SetGlobalRunner` 重置——如果先 `SetGlobalRunner(nil)` 再 `getRunner()`，`runnerOnce` 已经消费过，`initRunner()` 不会再执行，`globalRunner` 保持 nil，导致 NPE。

**Python 参考**: Python 无此问题因为 GIL + 模块加载时即初始化 `GLOBAL_RUNNER`。

**建议**: 统一为 `sync.Once` + 不可变全局实例模式，或让 `SetGlobalRunner` 同时重置 `runnerOnce`。

---

### S8. [Prompts] Build() 空内容过滤逻辑不一致

**文件**: `internal/agentcore/single_agent/prompts/builder.go:152-154`

**描述**: Python 用 `part.strip()` 过滤纯空白内容（空字符串、空格、换行等），Go 用 `content != ""` 仅过滤空字符串。Python 中 `"  \n  "` 这样的纯空白节会被跳过，Go 中不会。

**建议**: 将 Go 过滤条件改为 `strings.TrimSpace(content) != ""` 以对齐 Python 的 `part.strip()` 语义。

---

### S9. [Build] uapclaw 编译产物被提交到仓库

**提交**: 92cc61b

**描述**: `uapclaw` 二进制文件（11MB ELF 可执行文件）被提交到仓库。`.gitignore` 中未忽略此文件名。这会导致仓库体积膨胀、每次构建后 git status 出现变更噪音、二进制 diff 无法 review。

**建议**: 从 git 中移除（`git rm uapclaw`），在 `.gitignore` 中添加 `uapclaw`。

---

## 二、一般问题（15 个）

### M1. [Spawn] 缺少 stdout 重定向防污染

**文件**: `internal/agentcore/runner/spawn/child.go:25-26`

**描述**: 子进程内直接使用 `os.Stdin/os.Stdout`，没有 stdout 重定向保护。如果 Agent 或第三方库向 `os.Stdout` 直接写入（如 `fmt.Println`），会污染 NDJSON 协议流。

**Python 参考**: `child_process.py:347-351` 子进程内将 `sys.stdout` 重定向到 `sys.stderr`。

**建议**: 在子进程入口处将 `os.Stdout` 重定向到 `os.Stderr`，协议通信使用管道 fd。

---

### M2. [Spawn] 缺少 Runner.start()/Runner.stop() 生命周期管理

**文件**: `internal/agentcore/runner/spawn/child.go:16-42`

**描述**: `RunSpawnedProcess` 直接运行消息循环，缺少 Runner 生命周期管理。Runner（线程池/连接池等资源）未初始化就执行 Agent。

**Python 参考**: `child_process.py:456-468` 在消息循环前调 `Runner.start()`，在 finally 中调 `Runner.stop()`。

**建议**: 后续集成时需补充 Runner 生命周期管理。

---

### M3. [Spawn] 缺少 Runner.set_config() 配置注入

**描述**: `RunSpawnedProcess` 未处理 `runner_config`，反序列化逻辑存在但未被调用。

**Python 参考**: `child_process.py:456-457` 在子进程入口将 `runner_config` 反序列化后设置到 Runner。

---

### M4. [Spawn] SHUTDOWN 消息的 payload 与 Python 不一致

**文件**: `internal/agentcore/runner/spawn/handle.go:233`

**描述**: Go 中 SHUTDOWN 消息 payload 为 `nil`，Python 中为 `{"reason": "parent_initiated"}`。

**建议**: 保持一致，payload 设为 `map[string]any{"reason": "parent_initiated"}`。

---

### M5. [Spawn] 缺少日志配置应用

**描述**: `prepareSpawnAgentConfig` 仅解析配置，未应用日志配置。

**Python 参考**: `_prepare_spawn_agent_config` 解析配置后会调用 `configure_log_config`。

---

### M6. [Runner] RunAgent/RunAgentStreaming 未传递 modelCtx 和 envs 参数

**文件**: `internal/agentcore/runner/runner.go:226-334`

**描述**: 接收了 `modelCtx any` 和 `envs map[string]any` 但完全未使用。Python 中 `envs` 传递给 `agent.invoke(inputs, agent_session, context=context)`，context 信息丢失。

---

### M7. [Runner] SpawnAgent 缺少 session_id 解析和 logging_config 回退逻辑

**文件**: `internal/agentcore/runner/runner.go:422-449`

**描述**: Python `spawn_agent` 有输入规范化、session_id 提取、logging_config 回退逻辑，Go 版本仅简单注入 envs。

---

### M8. [Runner] SpawnAgentStreaming 返回类型与 Python 不一致

**文件**: `internal/agentcore/runner/runner.go:453-503`

**描述**: Python 返回 `AsyncIterator[tuple[SpawnedProcessHandle, Any]]`（yield handle+message 元组），Go 返回 `<-chan stream.Schema`（仅 stream chunk），丢失了 SpawnedProcessHandle。

---

### M9. [Runner] MessageQueue 默认参数与 Python 不一致

**文件**: `internal/agentcore/runner/queue.go`, `runner.go`

**描述**: Python 默认 `queue_max_size=10000, timeout=120000.0`；Go 默认 `defaultMQMaxSize = 1000, defaultMQTimeout = 30 * time.Second`。超时差异巨大：Python 120000 秒 vs Go 30 秒。

**建议**: 对齐 Python 的默认值，或评估 Go 场景下是否需要调整。

---

### M10. [Runner] SubscriptionInMemory.deactivate() 不重建 Queue

**描述**: Python 的 `deactivate` 在停用后会重建 `asyncio.Queue`，Go 版本不重建 channel，deactivate 后 channel 仍保留旧数据。

---

### M11. [Prompts] PromptSection.Priority 默认值不一致

**描述**: Python 默认 `priority: int = 100`，Go 零值为 0。通过结构体字面量构造若漏写 Priority，Go 中 priority=0（排最前面），Python 中 priority=100（排后面），排序行为截然不同。

**建议**: 在 `NewPromptSection` 中对 `priority == 0` 设置默认值 100。

---

### M12. [Prompts] Render() 回退到 map 首个值的顺序不确定

**描述**: Python `next(iter(...))` 确定有序（Python 3.7+ dict 保序），Go `range` 随机化。当 Content 中既没有精确匹配语言也没有 "cn"（DefaultLanguage）时，Go 的回退值不确定。

**建议**: 改为按字母序取首个 key 的值，确保确定性。

---

### M13. [Runner] initRunner() 中重复 SetRunnerConfig 无意义

**文件**: `internal/agentcore/runner/runner.go:571`

**描述**: `config.SetRunnerConfig(config.GetRunnerConfig())` 先 Get 再 Set 同一个值，毫无效果。

---

### M14. [Runner] OffAllCustom/OffAllPerAgent 共享 filters/chains/hooks 清理可能相互影响

**文件**: `internal/agentcore/runner/callback/framework.go`

**描述**: 两个方法都删除 `fw.filters[event]`、`fw.chains[event]`、`fw.hooks[event]`。如果同一 event 同时注册了 Custom 和 PerAgent 回调，调用 `OffAllCustom` 会清除 PerAgent 回调的资源。

---

### M15. [Lint] ST1005 错误消息首字母小写修复不完整

**描述**: 仅修复了 "Agent不存在" → "agent不存在" 和 "Workflow不存在" → "workflow不存在"，同文件仍有 "获取Agent失败"、"PreRun失败" 等大写开头的错误消息未修复。

---

## 三、提示级问题（12 个）

### T1. [Spawn] on_unhealthy 回调缺少异常保护

**描述**: Go 中 `recordHealthFailure` 直接调用回调，无 `defer/recover` 保护。Python 中用 `try/except` 包裹。

---

### T2. [Spawn] waitForHealthCheckResponse 未使用传入的 messageID 参数

**描述**: 方法签名接受 `messageID string`，但方法体内从未使用。Python 同样未使用。

---

### T3. [Spawn] ProcessMessageLoop 中 stream_modes 类型断言可能失败

**文件**: `internal/agentcore/runner/spawn/child.go:118-121`

**描述**: `sm.([]string)` 断言，但 JSON 反序列化到 `any` 后数组类型为 `[]any` 而非 `[]string`，类型断言会失败。

**建议**: 改为递归类型转换 `[]any` → `[]string`。

---

### T4. [Spawn] IsAlive() 不加锁保护

**描述**: 读取 `h.cmd.ProcessState` 和 `h.cmd.Process` 未加锁，与 Shutdown 等操作并发时可能读到不一致状态。

---

### T5. [Spawn] WaitForCompletion 不停止健康检查

**Python 参考**: `wait_for_completion` 先调 `stop_health_check()`。

---

### T6. [Spawn] 日志同步遗漏

**描述**: Python 中部分 logger 调用在 Go 中缺失：
- `child_process.py:58` `logger.debug(f"Received message from stdin: {message.type}")`
- `child_process.py:75` `logger.debug(f"Sent message to stdout: {message.type}")`
- `child_process.py:224` `logger.error(f"Error executing agent: {e}", exc_info=True)`
- `process_manager.py:357-363` 健康检查触发回调的 warn 日志

---

### T7. [Runner] 日志组件使用 ComponentCommon 应改为 ComponentAgentCore

**描述**: runner 子包属于 agentcore，应使用 `ComponentAgentCore` 而非 `ComponentCommon`。

---

### T8. [Runner] AgentRef.IsByID() 和 IsByInstance() 可以同时为 true

**描述**: Python 中 `agent: str | BaseAgent | LegacyBaseAgent` 是互斥的 Union type，Go 版本允许两者共存。

---

### T9. [Runner] GetRunnerConfig 返回指针而非副本

**描述**: 调用方拿到 `*RunnerConfig` 后可直接修改内部字段，影响全局状态。Python 也返回引用，行为对齐，但 Go 中更容易意外修改。

---

### T10. [Prompts] Go 日志中 existing_tools 与 Msg() 格式化重复

**描述**: `existing_tools` 字段与 `errMsg` 中 `%v` 格式化都包含工具列表，信息重复。

---

### T11. [DeepCopy] mohae/deepcopy 库不拷贝未导出字段 — 依赖脆弱

**描述**: 当前 `RunnerConfig` 全部使用导出字段，不触发问题。但如果后续添加未导出字段，`cloneDefaultConfig()` 会静默丢失数据。该库自 2017 年后无更新。

---

### T12. [Lint] 分隔注释 `// ---` 格式与编码规范不一致

**描述**: 部分文件使用 `// ---` 格式而非规范中的 `// ────────────────────────────` 格式。属于历史遗留。

---

## 四、问题汇总

| 级别 | 数量 | 关键问题 |
|------|------|---------|
| **严重** | 9 | S1-stdout 竞争, S2-stdin 竞争, S3-健康检查不跳过, S4-shutdownACK 不跳过, S5-配置丢失, S6-ForceKill 缺停止, S7-getRunner 竞态, S8-Build 过滤不一致, S9-二进制提交 |
| **一般** | 15 | M1-stdout 重定向, M2-Runner 生命周期, M3-配置注入, M4-payload 不一致, M5-日志配置, M6-参数未传递, M7-spawn 逻辑缺失, M8-返回类型不一致, M9-默认参数不一致, M10-队列不重建, M11-priority 默认值, M12-回退顺序不确定, M13-无效 SetRunnerConfig, M14-OffAll 清理, M15-ST1005 不完整 |
| **提示** | 12 | T1-回调异常保护, T2-未用 messageID, T3-类型断言, T4-IsAlive 锁, T5-WaitForCompletion, T6-日志遗漏, T7-日志组件, T8-AgentRef 互斥, T9-返回指针, T10-日志重复, T11-deepcopy 脆弱, T12-分隔注释格式 |

---

## 五、修复优先级建议

### P0 — 必须立即修复（数据正确性 / 安全性）

1. **S1 + S2**: Spawn 包 stdin/stdout 并发读写加互斥锁
2. **S5**: ParseSpawnAgentConfig 返回完整类型
3. **S7**: getRunner() + SetGlobalRunner 竞态修复
4. **S9**: 移除二进制文件 + 更新 .gitignore

### P1 — 尽快修复（功能正确性）

5. **S3 + S4**: 健康检查/shutdown 循环读取跳过非目标消息
6. **S6**: ForceKill 停止健康检查
7. **S8**: Build() 使用 TrimSpace 过滤空白
8. **M1**: 子进程 stdout 重定向防污染
9. **M4**: SHUTDOWN payload 对齐 Python

### P2 — 计划修复（功能完整性）

10. **M2 + M3**: Runner 生命周期管理 + 配置注入
11. **M6**: RunAgent 传递 modelCtx/envs
12. **M7 + M8**: SpawnAgent 逻辑补全
13. **M9**: MessageQueue 默认参数对齐
14. **M11 + M12**: PromptSection 默认值和回退顺序

---

## 六、并发安全专项审查

Spawn 包从 Python asyncio 单线程模型转为 Go 多 goroutine 模型后，**stdin/stdout 的并发读写是最大的并发安全隐患**：

| 组件 | Python (asyncio) | Go (goroutine) | 问题 |
|------|-----------------|----------------|------|
| stdout 读取 | 天然串行 | **多 goroutine 竞争** | S1 |
| stdin 写入 | 天然串行 | **多 goroutine 竞争** | S2 |
| isHealthy/shutdownRequested | 单线程安全 | ✅ 已用 mu | — |
| onUnhealthy 回调 | 单线程安全 | 需 defer/recover | T1 |

---

*报告生成时间: 2026-06-29*
