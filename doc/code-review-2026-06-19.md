# 代码审查报告 — 2026-06-19

> 审查范围：最近 24 小时提交记录涉及的领域五（会话系统）5.8/5.10/5.11 章节
> 审查方式：Go 实现与 Python 参考代码逐一对比

---

## 一、审查概要

| 模块 | 章节对应 | 严重 | 一般 | 提示 | 合计 |
|------|---------|------|------|------|------|
| Stream (5.10) | `session/stream/` | 15 | 16 | 9 | 40 |
| Tracer (5.11) | `session/tracer/` | 8 | 18 | 4 | 30 |
| Checkpointer (5.8) | `session/checkpointer/` | 3 | 12 | 7 | 22 |
| **合计** | | **26** | **46** | **20** | **92** |

---

## 二、严重问题（26 个）

### 2.1 Stream 模块（15 个严重）

#### SW-01 — StreamMode 缺少 mode/desc/options 字段
**问题**：Python `StreamMode` 是富枚举，每个成员携带 `mode`/`desc`/`options` 三个字段。Go 的 `StreamMode` 只是简单 `iota` 整数枚举，`String()` 只返回 mode 字符串，缺少 `Desc` 和 `Options` 字段。
**Python 参考**：`base.py:24-27` — `BaseStreamMode(StreamMode)` 携带 `mode`/`desc`/`options`
**Go 位置**：`stream/base.go:43-52`
**修复方向**：将 StreamMode 改为带字段的结构体枚举，或补充 `StreamModeDesc`/`StreamModeOptions` 映射常量

---

#### SW-03 — CustomSchema 与 Python 语义不一致
**问题**：Python `CustomSchema` 配置 `extra="allow"`，可接受任意顶层字段（如 `CustomSchema(type="event", key1="val1")`）。Go 的 `CustomSchema` 只有 `Type` 和 `Data map[string]any` 两个字段，将自定义数据收敛到 `Data` 字典内。序列化结构不兼容：Python `{"type":"event","key1":"val1"}` vs Go `{"type":"custom","data":{"key1":"val1"}}`。
**Python 参考**：`base.py:37-42` — `model_config = ConfigDict(extra="allow")`
**Go 位置**：`stream/base.go:33-38`
**修复方向**：去掉 `Data` 字段，改用内嵌 `map[string]any` 或自定义 `MarshalJSON`/`UnmarshalJSON` 实现 `extra="allow"` 语义

---

#### SW-06 — StreamQueue.Close() drain goroutine 与正常消费者竞争
**问题**：`Close()` 先发送 endFrame，然后启动 goroutine 排空 channel（drain loop）。drain goroutine 与 `StreamOutput()` 的 Receive 循环竞争消费 channel 数据，导致 endFrame 可能被 drain 消费，`StreamOutput()` 永远收不到结束信号。Python 的 `close()` 只调用 `queue.join()` 等待所有 item 被消费完，不主动消费数据。
**Python 参考**：`emitter.py:142-147` — `await asyncio.wait_for(self._stream_queue.join(), timeout)`
**Go 位置**：`stream/queue.go:147-201`
**修复方向**：移除 drain goroutine，改为等待 channel 自然排空或只关闭标记

---

#### SW-07 — StreamQueue.Close() channel 可能被 close 两次导致 panic
**问题**：`Close()` 的超时分支调用 `forceClear()` 关闭 channel，但 `forceClear()` 在其他场景被调用时 channel 可能已关闭。`StreamEmitter.Close()` 也会向已关闭 channel 发送 endFrame 导致 panic。
**Go 位置**：`stream/queue.go:189-199`, `queue.go:218-233`
**修复方向**：用 `sync.Once` 保证 channel 只 close 一次，`StreamEmitter.Close()` 发送前检查队列是否已关闭

---

#### SW-13 — END_FRAME 类型不一致
**问题**：Python `END_FRAME` 是字符串常量 `"all streaming outputs finish"`，可序列化为 JSON。Go 用 `endFrame{}` 空结构体，无法序列化。如果两端需要通过网络交互流数据，END_FRAME 将无法互通。
**Python 参考**：`emitter.py:122` — `END_FRAME = "all streaming outputs finish"`
**Go 位置**：`stream/queue.go:15-16`, `stream/emitter.go:69`
**修复方向**：如果仅进程内使用可保留；若需跨进程传输应改为字符串常量

---

#### SW-17 — Writer 缺少 Schema 字段校验（Python 用 Pydantic model_validate）
**问题**：Python `StreamWriter.write()` 先用 `self._schema_type.model_validate(stream_data)` 校验，校验失败抛 `STREAM_WRITER_WRITE_STREAM_VALIDATION_ERROR`。Go 的 `writeStream()` 只做了 nil 校验，没有对 Schema 字段进行任何校验（如 OutputSchema.Type 不能为空、Index >= 0 等）。
**Python 参考**：`writer.py:31-36` — `validated_data = self._schema_type.model_validate(stream_data)`
**Go 位置**：`stream/writer.go:82-104`
**修复方向**：添加 `Validate() error` 方法到 Schema 接口，或在 `writeStream` 中增加字段校验逻辑

---

#### SW-21 — StreamOutput 缺少 first_frame_timeout/timeout 参数
**问题**：Python `stream_output()` 有 `first_frame_timeout` 和 `timeout` 参数，首帧超时用 `STREAM_OUTPUT_FIRST_CHUNK_INTERVAL_TIMEOUT` 错误码。Go 的 `StreamOutput()` 无超时控制，使用 `context.Background()` 无限等待，可能导致消费者永远阻塞。
**Python 参考**：`manager.py:40-56` — first_frame_timeout/timeout 参数
**Go 位置**：`stream/manager.go:148-190`
**修复方向**：添加 `firstFrameTimeout` 和 `timeout` 参数，首帧和后续帧分别设超时 context

---

#### SW-22 — StreamOutput 收到 END_FRAME 后未调用 queue.close()
**问题**：Python `stream_output()` 在收到 END_FRAME 时调用 `await self._stream_emitter.stream_queue.close(timeout=timeout)` 关闭队列。Go 的 `StreamOutput()` 收到 endFrame 后直接 return，不关闭 queue，可能导致资源泄漏。
**Python 参考**：`manager.py:60-62` — `await self._stream_emitter.stream_queue.close(timeout=timeout)`
**Go 位置**：`stream/manager.go:167-172`
**修复方向**：收到 endFrame 后应调用 `queue.Close(ctx)` 或至少确保 channel 被关闭

---

#### SW-31 — WriteStream 缺少 trigger 回调
**问题**：Python `write_stream()` 在写入前调用 `await trigger(self._session_id + "write_stream", data=stream_data)`，触发回调事件。Go `WriteStream()` 没有对应回调触发。依赖回调的监控/日志/转发功能失效。
**Python 参考**：`agent.py:82-83` — `await trigger(self._session_id + "write_stream", ...)`
**Go 位置**：`session/agent.go:265-278`
**修复方向**：回填 callback_framework 支持后添加 trigger 调用（已标注 ⤵️ R6）

---

#### SW-32 — WriteCustomStream 缺少 trigger 回调
**问题**：与 SW-31 同类，Python `write_custom_stream()` 也调用了 `trigger`。
**Python 参考**：`agent.py:89-90`
**Go 位置**：`session/agent.go:283-304`
**修复方向**：同 SW-31

---

#### SW-33 — CloseStream 缺少 callback_framework.unregister_event
**问题**：Python `close_stream()` 在关闭 emitter 后调用 `await Runner.callback_framework.unregister_event(event=self._session_id + "write_stream")` 注销回调事件。Go `CloseStream()` 缺少此注销逻辑。
**Python 参考**：`agent.py:123` — `await Runner.callback_framework.unregister_event(...)`
**Go 位置**：`session/agent.go:323-332`
**修复方向**：回填 callback_framework 支持后添加 unregister_event 调用（已标注 ⤵️ R6）

---

#### SW-35 — NodeSessionFacade.WriteStream/WriteCustomStream 是空实现
**问题**：`NodeSessionFacade.WriteStream()` 和 `WriteCustomStream()` 目前只返回 nil，没有任何实际逻辑。注释标注 `⤵️ 5.10 回填`，但尚未回填。stream 包已就绪但未接入。
**Python 参考**：`node.py:75-83` — 通过 `_stream_writer()`/`_custom_writer()` 获取 writer 并写入
**Go 位置**：`session/node.go:185-196`
**修复方向**：实现 WriteStream/WriteCustomStream，从 inner.StreamWriterManager() 获取 writer 写入

---

#### SW-37 — StreamWriterManager.writers map 无并发保护
**问题**：`StreamWriterManager.writers` 是 `map[StreamMode]StreamWriter`，`AddWriter`/`RemoveWriter`/`GetWriter` 均无锁保护。如果 `StreamOutput()` 的 goroutine 与 `AddWriter`/`RemoveWriter` 并发执行，存在 data race。
**Go 位置**：`stream/manager.go:39`
**修复方向**：添加 `sync.RWMutex` 保护 writers map，或在文档中明确写入器只允许初始化时配置

---

#### SW-38 — StreamQueue.Close() drain goroutine 与 StreamOutput 消费者竞争
**问题**：与 SW-06 同源问题，drain goroutine 与 `StreamOutput()` 的 Receive 循环竞争消费 channel 数据，导致数据可能被 drain goroutine 误消费，而 `StreamOutput()` 漏收。
**Go 位置**：`stream/queue.go:173-187`, `stream/manager.go:154-156`
**修复方向**：移除 drain goroutine，channel 中的数据应由正常消费者（StreamOutput）消费

---

### 2.2 Tracer 模块（8 个严重）

#### TR-02 — updateStartTraceData 遗漏 instance_info 字段
**问题**：Go 将 `instanceInfo` 整体作为 MetaData，但 Python 的 `update_data` 还包含 `"instance_info": instance_info`（原始对象），这会被 `span.update(update_data)` 设置到 span 上（Pydantic model 允许额外字段）。Go 遗漏了此字段。
**Python 参考**：`handler.py:111-118` — `update_data` 包含 `"instance_info": instance_info`
**Go 位置**：`tracer/span.go:470-480`
**修复方向**：确认是否需要保留 instance_info 字段，如 Python 端实际依赖则需同步

---

#### TR-07 — GetNodeStatus 未检查 InnerError 字段
**问题**：Python `GetNodeStatus` 还检查 `inner_error` 字段：`if inner_error: return NodeStatus.ERROR.value`。Go 未检查 `InnerError`，导致有 inner_error 但无 error 的 span 返回 FINISH 而 Python 返回 ERROR。
**Python 参考**：`handler.py:68-69` — `inner_error = getattr(span, "inner_error", None)`
**Go 位置**：`tracer/handler.go:436-450`
**修复方向**：让 `GetNodeStatus` 能接收 `InnerError` 参数，或在 `TraceWorkflowHandler.FormatData` 中单独检查

---

#### TR-11 — TracedModelClient 缺少 tracer_record_data 回调机制
**问题**：Python 对 LLM 调用传递了 `tracer_record_data` 回调参数，允许 LLM 客户端在调用过程中间记录 `on_llm_request` 事件。Go 的 `TracedModelClient` 不支持此回调，`on_llm_request` 事件永远不会从 decorator 触发。
**Python 参考**：`decorator.py:48-51` — `call_kwargs["tracer_record_data"] = tracer_record_data`
**Go 位置**：`tracer/decorator.go:77-99`
**修复方向**：在 `TracedModelClient.Invoke` 中支持 tracer_record_data 机制，或通过 InvokeOption 扩展传递回调

---

#### TR-12 — Decorator instanceInfo 硬编码
**问题**：Python `decorate_model_with_trace` 从 `model.config.model_config.model_name` 获取模型名，Go 硬编码为 `"BaseModelClient"`。`decorate_tool_with_trace` 在 Python 中使用 `tool.card.name`，Go 硬编码为 `"Tool"`。`decorate_workflow_with_trace` 在 Python 中使用 `workflow.card.name`，Go 硬编码为 `"Workflow"`。
**Python 参考**：`decorator.py:120-168`
**Go 位置**：`tracer/decorator.go:210-213, 233-235, 257-258`
**修复方向**：从 model/tool/workflow 的配置中获取实际名称和类型信息

---

#### TR-16 — getWorkflowMetadata 中 workflow_version/workflow_name 硬编码为空
**问题**：Python 从 `session.config().get_workflow_config(executable_id).card` 提取 `version` 和 `name`，Go 中硬编码为空字符串，导致前端展示工作流名称和版本缺失。
**Python 参考**：`workflow_tracer.py:15-23`
**Go 位置**：`tracer/workflow.go:204-213`
**修复方向**：从 `session.Config()` 解析 workflow_config，提取 card 中的 version 和 name

---

#### TR-17 — getComponentMetadata 缺少 loop_node_id/loop_index 逻辑
**问题**：Python 从 `state.get_global(LOOP_ID)` 获取循环信息，Go 用 TODO 标注但未实现。循环组件的追踪信息会缺失。
**Python 参考**：`workflow_tracer.py:35-43`
**Go 位置**：`tracer/workflow.go:218-231`
**修复方向**：从 `session.State().GetGlobal(LOOP_ID)` 获取 loop_id，然后获取 loop_index

---

#### TR-18 — TraceComponentDone 的 PopWorkflowSpan 条件与 Python 不一致
**问题**：Python 中 `trace_component_done` 先触发 `on_call_done`，然后检查 `loop_id`：如果 `loop_id is None` 则直接 return（不 pop），如果 `loop_id` 非空则 pop。Go 中无条件执行 `PopWorkflowSpan`，非循环组件也会 pop span。
**Python 参考**：`workflow_tracer.py:140-153` — `loop_id = state.get_global(LOOP_ID); if loop_id is None: return`
**Go 位置**：`tracer/workflow.go:146-160`
**修复方向**：修正逻辑：只有循环组件才执行 PopWorkflowSpan，非循环组件不 pop

---

#### TR-25 — PopWorkflowSpan 导致内存泄漏
**问题**：`PopWorkflowSpan` 只从 `SpanManager` 中移除 span，不从 `handler.workflowSpans` 缓存中删除对应条目，导致内存泄漏。Python 无此问题因为 `_session_spans` 存储具体类型。
**Python 参考**：`tracer.py:34-38`
**Go 位置**：`tracer/tracer.go:157-172`
**修复方向**：`PopWorkflowSpan` 应同时从 `handler.workflowSpans` 中删除对应条目

---

#### TR-27 — SpanManager 无并发保护
**问题**：`SpanManager` 的 `order` 切片和 `sessionSpans` map 在并发读写时会 panic（Go 的 map 不是并发安全的）。Python 中 asyncio 是单线程协作式并发不需要锁。
**Go 位置**：`tracer/span.go:94-99`
**修复方向**：为 `SpanManager` 添加 `sync.RWMutex`，保护 `order` 和 `sessionSpans` 的读写

---

#### TR-28 — Tracer 的 workflow 相关 map 无并发保护
**问题**：`Tracer` 的 `WorkflowSpanManagerDict`、`workflowHandlers`、`workflowDispatch` 等 map 字段无并发保护。`RegisterWorkflowSpanManager` 会写入这些 map，如果与 `TriggerWorkflow` 并发执行会 panic。
**Go 位置**：`tracer/tracer.go:43-60`
**修复方向**：为 `Tracer` 添加 `sync.RWMutex`，保护所有共享 map 的读写

---

### 2.3 Checkpointer 模块（3 个严重）

#### CP-01 — InMemoryCheckpointer.Release agentID 路径未加锁
**问题**：`Release` 在 `len(agentID) > 0` 分支下直接读取 `cp.agentStores[sessionID]` 并调用 `agentStore.Clear()`，但整个分支没有任何锁保护。而全量释放路径（else）正确获取了 `cp.mu.Lock()`。并发调用可能导致数据竞争。
**Python 参考**：`inmemory.py:311-328` — Python 单线程异步无需锁
**Go 位置**：`checkpointer/inmemory.go:545-558`
**修复方向**：在 agentID 分支也加读锁 `cp.mu.RLock()` / `cp.mu.RUnlock()`

---

#### CP-02 — Persistence WorkflowStorage.Exists 查询 key 数与 Python 不一致
**问题**：Python `WorkflowStorage.exists()` 查询了 4 个 key（state_dump_type + state_blob + update_dump_type + update_blob），Go 只查询了 2 个 key（state_dump_type + state_blob）。虽然最终判断逻辑相同（只检查 state key 存在即可），但查询范围不一致，可能在某些存储后端上有行为差异。
**Python 参考**：`persistence.py:520-551` — 查询 4 个 key，`_KEY_NUMS = 4`
**Go 位置**：`checkpointer/persistence.go:491-511`
**修复方向**：补充查询 update 相关的 2 个 key，与 Python 对齐

---

#### CP-20 — isNewWorkflowStore TOCTOU 问题
**问题**：`isNewWorkflowStore` 在锁内判断但解锁后才使用。虽然当前函数内没有其他 goroutine 能修改 `cp.workflowStores`，但 `isNewWorkflowStore` 的赋值与使用跨越了锁区间，未来代码变更可能引入 TOCTOU。
**Go 位置**：`checkpointer/inmemory.go:127-131`
**修复方向**：将 `isNewWorkflowStore` 的使用移到锁释放前，或将日志记录改到锁内

---

## 三、一般问题（46 个）

### 3.1 Stream 模块（16 个一般）

| 编号 | 问题描述 | Python 参考 | Go 位置 |
|------|---------|-------------|---------|
| SW-02 | 未区分 StreamMode 和 BaseStreamMode | `base.py:24-27` | `stream/base.go:43-52` |
| SW-04 | Schema 接口缺乏类型约束，缺少 StreamSchemas Union | `base.py:50` | `stream/base.go:6-9` |
| SW-08 | Send 重试耗尽返回错误，Python 静默丢弃 | `emitter.py:62-66` | `stream/queue.go:104-109` |
| SW-09 | Receive closed 后不读 channel 中剩余数据 | `emitter.py:69-70` | `stream/queue.go:114-120` |
| SW-10 | Close 发送 endFrame 非阻塞，endFrame 可能丢失 | `emitter.py:146-147` | `stream/queue.go:162-167` |
| SW-14 | Emit 只接受 Schema，Python 接受 Any | `emitter.py:132` | `stream/emitter.go:43` |
| SW-15 | Close 后未关闭内部 Queue，lifecycle 不明确 | — | `stream/emitter.go:59-75` |
| SW-19 | WriteInteraction 使用 context.Background() | — | `stream/writer.go:70-74` |
| SW-23 | 缺少 UserConfig.is_sensitive() 日志分支 | `manager.py:64-75` | `stream/manager.go:174-179` |
| SW-24 | 缺少 create_manager 工厂方法 | `manager.py:32-35` | `stream/manager.go:55-72` |
| SW-25 | add_writer nil 校验差异（Go 有 Python 无） | — | `stream/manager.go:82-91` |
| SW-26 | remove_writer 返回值不一致（Python 返回 writer） | `manager.py:99-104` | `stream/manager.go:132-143` |
| SW-28 | normalizeOutputStream index 字段类型处理不完整（JSON float64 vs int） | `agent.py:168` | `session/agent.go:533` |
| SW-29 | WriteCustomStream 非 map 时包裹为 {"value": data} | `agent.py:87-91` | `session/agent.go:296-301` |
| SW-34 | CloseStreamOnPostRun 完整性待确认 | — | `session/agent.go:166` |
| SW-36 | NodeSessionFacade 缺少 streamWriter/customWriter 辅助方法 | `node.py:88-98` | `session/node.go:185-196` |

### 3.2 Tracer 模块（18 个一般）

| 编号 | 问题描述 | Python 参考 | Go 位置 |
|------|---------|-------------|---------|
| TR-01 | updateStartTraceData 序列化失败后继续 EmitStreamWriter，发送不完整数据 | `handler.py:98-109` | `tracer/span.go:456-463` |
| TR-04 | OnInvoke 中 elapsed 计算后赋值给 _ 丢弃，代码意图不明确 | `handler.py:329-330` | `tracer/handler.go:306-309` |
| TR-05 | Span.Outputs 类型定义不一致（Python Union vs Go any） | `span.py:15` | `tracer/span.go:28-29` |
| TR-08 | _send_data 直接传递 span 引用（Python deepcopy） | `handler.py:48-53` | `tracer/handler.go:395-408` |
| TR-13 | Stream outputs 格式不一致（Python {"outputs": results} vs Go AssistantMessage） | `decorator.py:93-98` | `tracer/decorator.go:121-134` |
| TR-14 | TracedWorkflow 是空壳占位，缺少 Invoke/Stream 实现 | `decorator.py:166-167` | `tracer/decorator.go:53-65` |
| TR-19 | TraceComponentStreamInput/Output 中 chunk 类型处理差异 | `workflow_tracer.py:98` | `tracer/workflow.go:92-99` |
| TR-21 | 缺少 TracerHandlerName 枚举定义，字符串硬编码 | `handler.py:23-28` | `tracer/handler.go:208-217` |
| TR-23 | tracerSession/graphInterrupter 接口定义合理（记录） | — | `tracer/decorator.go:15-20` |
| TR-24 | BaseWorkflowSession.Config() 返回 any 导致无法提取 workflow card | — | `tracer/workflow.go:29` |
| TR-26 | NodeSessionFacade 的 state.UpdateTrace 未在 tracer 包实现 | — | `tracer/decorator.go:15-20` |
| TR-29 | TraceAgentHandler/TraceWorkflowHandler 的 spans 缓存无并发保护 | — | `tracer/handler.go:27-38` |
| TR-31 | 日志字段缺少 metadata 前缀 | `handler.py:104-108` | `tracer/handler.go:456-463` |
| TR-32 | EmitStreamWriter 写入失败有 Error 日志（Python 无），合理增强 | `handler.py:43-46` | `tracer/handler.go:404,420` |
| TR-34 | Span.Update 设置字段失败日志级别过低（Debug → Warn） | `span.py:26-30` | `tracer/span.go:211-215` |
| TR-35 | fieldToSnakeCase 函数已定义但未使用（dead code） | — | `tracer/span.go:359-368` |
| TR-36 | buildWorkflowPayload 排除空字符串但 Python exclude_none 只排除 None | `handler.py:258-263` | `tracer/handler.go:643-723` |
| TR-38 | OnCallDone 空更新操作效率低下 | `handler.py:368-369` | `tracer/handler.go:361-363` |

### 3.3 Checkpointer 模块（12 个一般）

| 编号 | 问题描述 | Python 参考 | Go 位置 |
|------|---------|-------------|---------|
| CP-03 | pre_agent_execute 设置交互输入 InMemory 版 set_state vs Update | `inmemory.py:187-188` | `checkpointer/inmemory.go:332-340` |
| CP-04 | InMemory AgentStorage.Save 跳过 nil 状态 vs Python 总是保存 | `inmemory.py:410-415` | `checkpointer/inmemory.go:633-648` |
| CP-05 | force_del 分支缺少 try/finally 语义对齐 | `inmemory.py:64-73` | `checkpointer/inmemory.go:186-206` |
| CP-06 | innerClearWorkflowSession isSucceed 逻辑与 Python 不一致 | `inmemory.py:133-163` | `checkpointer/inmemory.go:927-981` |
| CP-07 | WorkflowStorage.Save nil 状态处理与 AgentStorage 不一致 | — | `checkpointer/inmemory.go:739-780` |
| CP-08 | Persistence pre_agent_execute 用 Update，与 Python InMemory 的 set_state 不一致 | `persistence.py:757` | `checkpointer/persistence.go:538` |
| CP-09 | basePersistenceStorage.Recover 忽略 RestoreState 异常 | `persistence.py:241-256` | `checkpointer/persistence.go:273` |
| CP-10 | Persistence Release agentID 分支缺日志 | `persistence.py:931-939` | `checkpointer/persistence.go:805-813` |
| CP-11 | InMemory Release agentID 分支缺日志 | `inmemory.py:312-328` | `checkpointer/inmemory.go:546-558` |
| CP-12 | InMemory PostWorkflowExecute 吞掉 Clear 错误 | `persistence.py:876-885` | `checkpointer/inmemory.go:240-248` |
| CP-18 | WorkflowStorage.Recover 未检查 session.State() nil | — | `checkpointer/persistence.go:389-466` |
| CP-19 | PersistenceWorkflowStorage.Save 未检查 session.State() nil | — | `checkpointer/persistence.go:325-385` |
| CP-21 | Python InMemory/Persistence pre_agent_execute 本身不一致 | `inmemory.py:188` vs `persistence.py:757` | — |
| CP-25 | processInteractiveInputs 代码重复 | — | `checkpointer/inmemory.go:1015-1049`, `persistence.go:973-1006` |

---

## 四、提示问题（20 个）

### 4.1 Stream 模块（9 个提示）

| 编号 | 问题描述 | Go 位置 |
|------|---------|---------|
| SW-05 | WriteCustomStream 硬编码 Type="custom"，Python 由传入数据决定 | `session/agent.go:302` |
| SW-11 | 缺少 maxSize 参数校验（负数会 panic） | `stream/queue.go:57-61` |
| SW-12 | 缺少 DEFAULT_RECEIVE_TIMEOUT 常量 | `stream/queue.go:33-42` |
| SW-16 | 未暴露 END_FRAME 常量/判断函数 | `stream/queue.go:15-16` |
| SW-18 | StreamWriter 无泛型约束（Python Generic[T,S]） | `stream/writer.go:13-17` |
| SW-20 | 三个 Writer 的 Write 实现相同，无 Schema 类型匹配校验 | `stream/writer.go:55-67` |
| SW-27 | writers map 无并发保护（与 SW-37 同源） | `stream/manager.go:39` |
| SW-30 | tagStreamPayload 未处理 CustomSchema 类型 | `session/agent.go:482-519` |
| SW-39 | StreamEmitter closed TOCTOU 竞态 | `stream/emitter.go:43-55` |

### 4.2 Tracer 模块（4 个提示）

| 编号 | 问题描述 | Go 位置 |
|------|---------|---------|
| TR-03 | updateEndTraceData 赋值方式更直接清晰（记录） | `tracer/span.go:484-492` |
| TR-15 | _should_decorate 命名对齐正确（记录） | `tracer/decorator.go:195-198` |
| TR-20 | TraceError 未检查 error nil（防御性编程） | `tracer/workflow.go:176-184` |
| TR-22 | TraceEvent 枚举数量验证正确（记录） | `tracer/data.go:44-110` |
| TR-30 | Span/TraceAgentSpan/TraceWorkflowSpan 本身不是并发安全的 | `tracer/span.go:18-41` |
| TR-33 | 日志组件使用正确（记录） | `tracer/handler.go:13` |

### 4.3 Checkpointer 模块（7 个提示）

| 编号 | 问题描述 | Go 位置 |
|------|---------|---------|
| CP-13 | PostWorkflowExecute 清理与删除不在同一锁区间 | `checkpointer/inmemory.go:240-269` |
| CP-14 | GetCheckpointer 空字符串处理差异 | `checkpointer/factory.go:136-157` |
| CP-15 | Persistence PreWorkflowExecute 日志消息有误导 | `checkpointer/persistence.go:664-727` |
| CP-16 | String() 格式与 Python 不同 | `checkpointer/factory.go:76-79` |
| CP-17 | pickle vs json 序列化能力差异（已知） | `checkpointer/inmemory.go:858` |
| CP-22 | LoadsTyped 未处理空字节数组 | `checkpointer/serializer.go:52-58` |
| CP-23 | graphStore 为 any 缺类型安全（已标注 ⤵️ 8.7 回填） | `checkpointer/inmemory.go:31` |
| CP-24 | Recover updates 后是否需要 commit 取决于 SetUpdates 实现 | `checkpointer/inmemory.go:810-825` |

---

## 五、高优先级修复建议

按影响范围和风险等级，建议按以下顺序修复：

### 第一优先级：并发安全（可能导致 panic/数据竞争）

| 编号 | 模块 | 问题 |
|------|------|------|
| TR-27 | Tracer | SpanManager 无并发保护 |
| TR-28 | Tracer | Tracer 的 workflow map 无并发保护 |
| CP-01 | Checkpointer | InMemory Release agentID 路径未加锁 |
| SW-37 | Stream | StreamWriterManager.writers map 无并发保护 |
| SW-07 | Stream | StreamQueue channel 可能被 close 两次 |

### 第二优先级：逻辑正确性（行为与 Python 不一致）

| 编号 | 模块 | 问题 |
|------|------|------|
| TR-18 | Tracer | TraceComponentDone PopWorkflowSpan 条件错误 |
| TR-07 | Tracer | GetNodeStatus 未检查 InnerError |
| SW-06 | Stream | Close() drain 与消费者竞争 |
| TR-25 | Tracer | PopWorkflowSpan 内存泄漏 |
| CP-02 | Checkpointer | Exists 查询 key 数与 Python 不一致 |

### 第三优先级：功能缺失

| 编号 | 模块 | 问题 |
|------|------|------|
| SW-35 | Stream | NodeSessionFacade.WriteStream/WriteCustomStream 空实现 |
| SW-21 | Stream | StreamOutput 缺少超时控制 |
| TR-11 | Tracer | 缺少 tracer_record_data 回调 |
| TR-12 | Tracer | instanceInfo 硬编码 |
| TR-16/17 | Tracer | workflow metadata / loop 信息缺失 |
| SW-31/32/33 | Stream | 回调 trigger/unregister 缺失 |

---

## 六、审查方法说明

1. 通过 `git log --since="24 hours ago"` 获取最近 10 次提交
2. 从 `IMPLEMENTATION_PLAN.md` 确认涉及领域五会话系统的 5.8/5.10/5.11 章节
3. 逐一阅读 Go 实现代码和 Python 参考代码，对比功能映射和逻辑差异
4. 重点关注：功能完整度、Python-Go 行为一致性、并发安全、边界条件、日志对照
5. 问题按严重/一般/提示三级分类
