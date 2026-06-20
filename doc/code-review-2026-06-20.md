# 代码审查报告 — 2026-06-20

> 审查范围：最近 48 小时提交涉及的功能模块
> 审查日期：2026-06-20
> 审查人：CodeBuddy Code Review Agent

---

## 一、审查范围

基于最近 48 小时的 Git 提交记录，审查覆盖以下功能模块：

| 章节 | 模块 | 关键提交 |
|------|------|---------|
| 5.9 | PersistenceCheckpointer | c7f3027 fix(checkpointer): 修复 review 严重/一般/提示问题 |
| 5.10 | StreamWriter (StreamMode/Schema/StreamQueue/StreamEmitter/StreamWriter/StreamWriterManager) | 94379d3 feat(session): 实现 StreamWriter |
| 5.11 | Session Tracer (Tracer/Span/Handler/Decorator/Workflow) | 63397de feat(session): 实现 5.11 Session Tracer |
| — | Session 集成层 (Agent/Node 回填) | 44241d2 fix(session): 对齐 Python 默认值行为 |
| — | Callback 框架扩展 (Off/OffAll) | efdc29b 包含 callback framework 变更 |

---

## 二、问题统计

| 严重程度 | 数量 | 说明 |
|---------|------|------|
| 🔴 严重 | 19 | 影响功能正确性或数据安全，需优先修复 |
| 🟡 一般 | 45 | 行为差异或功能缺失，建议修复 |
| 🔵 提示 | 27 | 代码质量/规范性/未来扩展，可延后处理 |
| **合计** | **91** | |

---

## 三、严重问题（🔴 共 19 个）

### S-01：StreamQueue 队列缓冲区大小不一致（Go 1024 vs Python 无限制）

- **模块**：5.10 StreamWriter
- **文件**：`stream/emitter.go:36`
- **问题**：Go 的 `NewStreamEmitter()` 创建队列时使用 `NewStreamQueue(1024)`，Python 的 `AsyncStreamQueue()` 默认 `maxsize=0`（无限制）。高吞吐场景下 Go 更容易触发 Send 超时重试。
- **Python 行为**：`asyncio.Queue(maxsize=0)` 永远不会因队列满而阻塞。
- **建议**：增大默认缓冲值或提供配置项，在注释中标注此差异。

### S-02：StreamOutput 超时不返回错误码，无法区分正常结束和超时

- **模块**：5.10 StreamWriter
- **文件**：`stream/manager.go:189-259`
- **问题**：Python 的 `stream_output()` 在首帧超时时抛 `STREAM_OUTPUT_FIRST_CHUNK_INTERVAL_TIMEOUT`，帧间超时抛 `STREAM_OUTPUT_CHUNK_INTERVAL_TIMEOUT`，两者使用不同错误码。Go 的 `StreamOutput()` 超时时只 debug 日志 + 静默退出 goroutine（关闭 out channel），不返回错误。Go 已定义了这两个错误码但从未使用。
- **建议**：在 `StreamOutput` goroutine 超时退出时，向 out channel 发送错误帧或通过 `chan error` 传播超时信息。

### S-03：StreamQueue Send 存在 TOCTOU 竞态窗口

- **模块**：5.10 StreamWriter
- **文件**：`stream/queue.go:67-119`
- **问题**：`Send()` 先 `q.closed.Load()` 检查关闭状态，然后 `q.ch <- data`。两者之间 `Close()` 可能被调用导致 `close(ch)`，向 closed channel 写入会 panic。当前使用 `defer recover()` 兜底，但不是最佳设计。
- **Python 行为**：Python asyncio 单线程协作式并发，不存在此竞态。
- **建议**：在注释中标注此竞态窗口和兜底策略，或使用 Mutex 保护原子性。

### S-04：StreamOutput goroutine 泄漏风险

- **模块**：5.10 StreamWriter
- **文件**：`stream/manager.go:197-256`
- **问题**：如果没人消费 out channel，goroutine 会在 `out <- data` 处永远阻塞，造成 goroutine 泄漏。Python 的 async generator 通过 `aclose()` 或 GC 自动清理，不存在此问题。
- **建议**：添加 `context.Context` 参数，goroutine 内在 select 中监听 `ctx.Done()`。

### S-05：Agent Handler FormatData 缺少深拷贝，存在数据竞争风险

- **模块**：5.11 Session Tracer
- **文件**：`tracer/handler.go:207-217, 400-413`
- **问题**：Python 的 `_send_data` 使用 `copy.deepcopy(span)` 确保写入流的数据是快照，后续对 span 的修改不会影响已写入数据。Go 的 `FormatData` 直接将 `*TraceAgentSpan` 指针放入 payload，后续对 span 的修改会反映到已写入流的对象上。
- **建议**：在 `FormatData` 或 `EmitStreamWriter` 中对 span 做深拷贝（可通过 `json.Marshal`/`json.Unmarshal` 实现）。

### S-06：Workflow Handler sendData 缺少深拷贝，引用字段存在数据竞争

- **模块**：5.11 Session Tracer
- **文件**：`tracer/handler.go:580-606, 659-748`
- **问题**：`buildWorkflowPayload` 手动构建 map 时，`span.Inputs`、`span.Outputs` 等字段是引用拷贝，不是深拷贝。Python 通过 `model_dump` + `model_validate` 重建 span 对象实现深拷贝。
- **建议**：与 S-05 一致，对 payload 做深拷贝。

### S-07：updateStartTraceData 的 JSON 序列化失败导致整个方法中断

- **模块**：5.11 Session Tracer
- **文件**：`tracer/handler.go:463-489`
- **问题**：`json.Marshal(instanceInfo)` 失败时，方法记录 Error 日志后直接 `return`，导致 `StartTime`、`InvokeType`、`Inputs`、`Name` 等核心字段全部不设置。Python 使用 `json.dumps(default=lambda ...)` 几乎不会失败，Go 的 `json.Marshal` 遇到不可序列化类型会直接失败。
- **Python 行为**：不可序列化对象被替换为 `<<no-serializable: ClassName>>` 字符串，继续处理。
- **建议**：`Marshal` 失败时 fallback 到直接使用 `instanceInfo` 作为 `metaData`，而非放弃所有字段设置。

### S-08：序列化格式差异（Python pickle vs Go JSON），跨语言不兼容

- **模块**：5.9 PersistenceCheckpointer
- **文件**：`checkpointer/serializer.go`, `checkpointer/inmemory.go:859,869,878`, `checkpointer/persistence.go:864,879,893`
- **问题**：Python 使用 `PickleSerializer`（`create_serializer("pickle")`），Go 使用 `JSONSerializer`。Python 的 `create_serializer("json")` 甚至会直接抛出 `ValueError("json is not yet supported")`。导致：(1) 持久化数据跨语言不兼容；(2) Go 的 JSON 无法序列化非基本类型。
- **建议**：文档标注序列化格式不兼容；确保 `GetState()` 返回的数据只包含 JSON 友好类型。

### S-09：WorkflowStorage.save() 依赖具体类型断言，非 WorkflowCommitState 时丢失 updates

- **模块**：5.9 PersistenceCheckpointer
- **文件**：`checkpointer/persistence.go:352-364`, `checkpointer/inmemory.go:769-780`
- **问题**：Go 使用 `if commitState, ok := session.State().(*state.WorkflowCommitState); ok` 类型断言，只有 `*WorkflowCommitState` 才保存 updates。Python 直接调用 `session.state().get_updates()` 不判断具体类型。如果状态实现了 `WorkflowState` 但不是 `*WorkflowCommitState`，updates 丢失。
- **Python 行为**：`updates = session.state().get_updates()` — 通过接口方法获取，不依赖具体类型。
- **建议**：在 `WorkflowState` 接口上添加 `GetUpdates()` 方法，替代具体类型断言。

### S-10：WorkflowStorage.recover() 对称问题，updates 恢复也依赖具体类型断言

- **模块**：5.9 PersistenceCheckpointer
- **文件**：`checkpointer/persistence.go:445-464`, `checkpointer/inmemory.go:812-826`
- **问题**：与 S-09 对称，recover 路径也使用 `*WorkflowCommitState` 类型断言，不匹配时 updates 不恢复。
- **建议**：同 S-09，通过接口方法解决。

### S-11：PreAgentExecute 设置交互输入方式与 Python 不一致

- **模块**：5.9 PersistenceCheckpointer
- **文件**：`checkpointer/persistence.go:542-550`, `checkpointer/inmemory.go:334`
- **问题**：InMemory 版 Go 使用 `st.Update()`（合并操作），Python InMemory 版使用 `st.set_state()`（整体替换），语义不同。Persistence 版 Go 使用 `Update` 对齐了 Python Persistence 版的 `update`。
- **Python 行为**：InMemory → `set_state`（替换），Persistence → `update`（合并）。
- **建议**：InMemory 版 Go 应使用 `SetState` 而非 `Update` 以匹配 Python InMemory 行为。

### S-12：GraphStore 完全未实现，导致多个关键路径数据残留

- **模块**：5.9 PersistenceCheckpointer
- **文件**：`checkpointer/persistence.go:96-97,721-723,770-771`, `checkpointer/inmemory.go:30-31,196-199,576-579`
- **问题**：Python 中 `GraphStore` 是完整实现，Go 中 `graphStore` 为 `nil`，所有调用位置是占位符。导致：(1) PreWorkflowExecute 强制删除跳过 graph state；(2) PostWorkflowExecute 正常完成跳过 graph state 删除；(3) 数据残留在 KV store 中。
- **建议**：8.7 回填时实现完整 GraphStore。

### S-13：Callback 框架 Off 方法只移除第一个匹配回调，Python 移除全部

- **模块**：Callback 框架
- **文件**：`callback/framework.go:125-140,172-187,215-230,268-283`
- **问题**：Go 的所有 Off 方法（OffLLM/OffTool/OffSession/OffCustom）找到第一个匹配后立即 return。Python 的 `unregister_sync` 使用列表推导式移除所有匹配回调。如果同一函数被注册多次，Go 只移除第一个。
- **Python 行为**：`self._callbacks[event] = [ci for ci in self._callbacks[event] if ci.callback != callback_to_remove]`
- **建议**：Off 方法应遍历所有回调，移除所有指针匹配的条目。

### S-14：Callback 框架 Trigger 过程中回调列表可能被并发修改

- **模块**：Callback 框架
- **文件**：`callback/framework.go:147-162,190-205,233-248,303-318`
- **问题**：Trigger 方法 `RLock` 后立即 `RUnlock`，然后在无锁状态下遍历回调。虽然当前 Off 的 `append` 会创建新 slice 不影响旧引用，但这种 TOCTOU 模式在维护中容易引入 bug，特别是递归/重入场景。
- **建议**：在 Trigger 遍历前做深拷贝（`copy` slice header + 底层数组），确保完全隔离。

### S-15：Callback 框架无法批量清除 LLM/Tool/Session 回调或重置框架状态

- **模块**：Callback 框架
- **文件**：`callback/framework.go`
- **问题**：Go 只为自定义事件提供 `OffAllCustom`，LLM/Tool/Session 域没有 `OffAllLLM/OffAllTool/OffAllSession`。Python 的 `unregister_event(event)` 对任何事件名都有效。默认注册的 `LoggingLLMCallback` 只能逐个按指针注销，非常不便。
- **建议**：添加 `OffAllLLM(event)`/`OffAllTool(event)`/`OffAllSession(event)` 方法或通用 `Reset()` 方法。

### S-16：Config 初始化丢失内置配置项

- **模块**：Session 集成层
- **文件**：`session/agent.go:95`
- **问题**：`NewSession` 中 `config := any(s.envs)` 直接将 `envs map` 作为 config，丢失了 Config 的默认初始化逻辑。Python 先创建 `Config()`（内部调用 `_load_envs_()` 加载内置配置项如超时、循环限制等），再 `config.set_envs(envs)`。
- **建议**：5.12 回填时先创建 `NewSessionConfig()`（内部执行等价 `_load_envs_` 逻辑），然后 `config.SetEnvs(envs)`。

### S-17：checkpointer 可能为 nil 导致 PreAgentExecute 被静默跳过

- **模块**：Session 集成层
- **文件**：`session/agent.go:352-387`
- **问题**：Go `PreRun` 中 `PreAgentExecute` 只在 checkpointer 非 nil 时执行。Python 始终通过工厂获取 checkpointer（不会为 nil）。如果 Go 的 `GetCheckpointer()` 返回 nil（全局工厂未初始化），检查点操作被静默跳过。
- **建议**：`PreRun` 中如果 checkpointer 为 nil，应记录 Error 日志或从工厂获取。

### S-18：NodeSessionFacade.Interact 缺少 trace_component_interactive_inputs 追踪

- **模块**：Session 集成层
- **文件**：`session/node.go:171-179`
- **问题**：Python 在获取到用户输入后调用 `TracerWorkflowUtils.trace_component_interactive_inputs(session, user_inputs)` 追踪交互输入。Go 版本只返回用户输入，没有追踪。
- **建议**：在 `NodeSessionFacade.Interact` 中添加 `TracerWorkflowUtils{}.TraceComponentInteractiveInputs` 调用。

### S-19：GetEnv/GetNodeConfig 空实现，阻塞节点级配置和环境变量读取

- **模块**：Session 集成层
- **文件**：`session/node.go:222-233`
- **问题**：`GetEnv` 和 `GetNodeConfig` 始终返回 nil，是空实现。Python 中通过 `config().get_env(key)` 和 `config().get_workflow_config(workflow_id).spec.comp_configs.get(node_id)` 获取。
- **建议**：5.12 回填时实现。

---

## 四、一般问题（🟡 共 45 个）

### StreamWriter (5.10)

| 编号 | 简述 | 文件 |
|------|------|------|
| G-01 | StreamMode 缺少 `desc`、`options` 属性 | `stream/base.go:42-52` |
| G-02 | CustomSchema 结构差异——Python 允许任意字段平铺，Go 使用嵌套 Data 字段 | `stream/base.go:32-38` |
| G-03 | Send 重试耗尽：Go 返回错误 vs Python 静默丢弃（Go 更正确） | `stream/queue.go:91-118` |
| G-04 | Receive 关闭后：Go 读残留数据 vs Python 直接抛异常（合理适配） | `stream/queue.go:121-156` |
| G-05 | Close 缺少 Python 中的超时排空和强制清空逻辑 | `stream/queue.go:158-184` |
| G-06 | StreamEmitter Close 缺少队列已关闭检查 | `stream/emitter.go:57-75` |
| G-07 | StreamWriter 缺少 Pydantic model_validate 等价校验（CustomSchema.Data 为 nil 时可能 panic） | `stream/writer.go:78-104` |
| G-08 | StreamOutput 缺少 Python 中的 need_close 参数 | `stream/manager.go:189-259` |
| G-09 | StreamOutput 缺少 Python 中的 UserConfig 敏感模式日志控制 | `stream/manager.go:243-254` |
| G-10 | Session 缺少 close_stream_on_post_run 参数 | `session/agent.go` |
| G-11 | StreamEmitter Emit 存在 TOCTOU 竞态（影响较小） | `stream/emitter.go:43-55` |
| G-12 | writeStream 中错误码与 Python 不完全对齐（缺少 model_validate 路径） | `stream/writer.go:82-104` |

### Session Tracer (5.11)

| 编号 | 简述 | 文件 |
|------|------|------|
| G-13 | Span.GetSpan 可简化——先查 map 再验 order | `tracer/span.go:127-136` |
| G-14 | Span.Update 使用反射设置字段，需确保 key 名与 Go 字段名一致 | `tracer/span.go:220-240` |
| G-15 | Workflow handler sendData 引用拷贝存在数据竞争（同 S-06） | `tracer/handler.go:580-606` |
| G-16 | OnCallStart 缺少 invoke_type 设置（Python 写的是 type 内置函数，属 bug） | `tracer/handler.go:220-236` |
| G-17 | metaData 的序列化/反序列化行为差异——Go Marshal 失败导致整个方法中断（同 S-07） | `tracer/handler.go:463-489` |
| G-18 | updateStartTraceData 中 instance_info 字段被忽略（Python 有但 Span 无此属性） | `tracer/handler.go:463-489` |
| G-19 | TraceComponentStreamInput/Output 缺少 Python 的 dict(chunk) 转换 | `tracer/workflow.go:98-110, 127-138` |
| G-20 | TraceComponentDone 缺少循环组件的 PopWorkflowSpan 逻辑（已标注 ⤵️ 8.20） | `tracer/workflow.go:159-174` |
| G-21 | getWorkflowMetadata 中 workflow_version/name 硬编码空（已标注 ⤵️ 5.12） | `tracer/workflow.go:222-229` |
| G-22 | TraceError 缺少 error==nil 校验 | `tracer/workflow.go:190-198` |
| G-23 | decorator 传入 inputs={"inputs": ...} 格式，Go 直接传 messages | `tracer/decorator.go:78-113` |
| G-24 | decorator 传入 outputs={"outputs": result} 格式，Go 直接传 result | `tracer/decorator.go:108-112` |
| G-25 | DecorateWorkflowWithTrace 使用硬编码 class_name/type，缺 metadata（已标注 ⤵️ 5.12） | `tracer/decorator.go:287-304` |
| G-26 | TracedModelClient.Stream 测试未验证 Final() 后 OnLLMEnd 事件 | `tracer/decorator_test.go:552-593` |
| G-27 | TracedWorkflow 无功能测试（占位） | `tracer/decorator_test.go` |
| G-28 | sendData 方法缺少独立测试 | `tracer/handler_test.go` |

### PersistenceCheckpointer (5.9)

| 编号 | 简述 | 文件 |
|------|------|------|
| G-29 | InMemory BaseSingleStateStorage 未采用 EntityHooks 注入模式 | `checkpointer/inmemory.go:44-51` |
| G-30 | PreAgentTeamExecute 设置交互输入方式（UpdateGlobal）需验证语义一致性 | `checkpointer/persistence.go:578-580` |
| G-31 | persistenceProvider.Create 默认 db_timeout=5，Python 默认 30 | `checkpointer/persistence.go:1066` |
| G-32 | basePersistenceStorage.Save 序列化失败静默返回 nil，InMemory 版返回错误（不一致） | `checkpointer/persistence.go:198-208, inmemory.go:646-648` |
| G-33 | InMemory 版 mainState 为 nil 时可能丢失 updates | `checkpointer/inmemory.go:746-748` |
| G-34 | PostWorkflowExecute 中清除工作流会话的错误被匿名函数吞掉 | `checkpointer/inmemory.go:240-248` |
| G-35 | InMemory WorkflowStorage.recover 类型断言 vs Python 直接访问 | `checkpointer/inmemory.go:806-808` |
| G-36 | basePersistenceStorage.deserializeState 缺少 base64 解码步骤 | `checkpointer/persistence.go:936-950` |
| G-37 | GraphStore 返回类型为 any，调用方需类型断言 | `checkpointer/persistence.go:853` |
| G-38 | 缺少 Release 指定 agentID 的测试覆盖 | `checkpointer/persistence_test.go`, `inmemory_test.go` |
| G-39 | 缺少 WorkflowStorage.Save 在 state=nil 但 updates≠nil 的测试 | `checkpointer/persistence_test.go` |
| G-40 | processInteractiveInputs 中 nil UserInputs 可能 panic | `checkpointer/inmemory.go:1027-1029` |
| G-41 | RestoreState 不返回 error，异常路径无 recover 保护 | `checkpointer/persistence.go` |

### Session 集成层

| 编号 | 简述 | 文件 |
|------|------|------|
| G-42 | config 字段类型为 any 占位，缺少完整方法集 | `session/agent.go:95` |
| G-43 | checkpointer 双重默认值获取 | `session/agent.go:99-101` |
| G-44 | PostRun 中 Commit 失败后设置 postRunDone=true，阻止重试 | `session/agent.go:406` |
| G-45 | CloseStream 错误被静默忽略 | `session/agent.go:400-401` |
| G-46 | Node Trace/TraceError 冗余 tracer nil 检查，返回 error 但始终为 nil | `session/node.go:137-162` |
| G-47 | Node TraceError 缺少 err==nil 参数校验 | `session/node.go:149-162` |
| G-48 | Interact 流式模式错误缺少错误码 | `session/node.go:171-179` |
| G-49 | CustomSchema 结构与 Python 不等价（嵌套 vs 平铺） | `session/agent.go:292-313` |
| G-50 | CreateWorkflowSession 的 state 包装链需确认与 Python 等价 | `session/agent.go:449-471` |
| G-51 | WriteStream writer==nil 时静默返回无日志 | `session/agent.go:266-286` |
| G-52 | UpdateState 错误只记录不传播 | `session/node.go:100-108` |
| G-53 | GetEnv/GetEnvs 临时实现（map 类型断言） | `session/agent.go:188-203` |
| G-54 | WorkflowSession.Close 空实现 | `internal/workflow_session.go:309-314` |

### Callback 框架

| 编号 | 简述 | 文件 |
|------|------|------|
| G-55 | Off 方法使用 fmt.Sprintf("%p") 比较函数指针不够可靠 | `callback/framework.go:135,182,226,278` |
| G-56 | 缺少 OffAllLLM/OffAllTool/OffAllSession 方法 | `callback/framework.go` |
| G-57 | 缺少 AgentEvents 事件定义 | `callback/events.go` |
| G-58 | OffAllCustom 与 OffCustom 并发安全性需注释说明 | `callback/framework.go:268-295` |
| G-59 | Trigger 缺少 transform 类型过滤（6.24 需注意） | `callback/framework.go` |
| G-60 | Trigger 缺少 panic recovery，回调 panic 会中断整个触发 | `callback/framework.go:147-162` |
| G-61 | Trigger 不检查 ctx.Done()，无法取消回调链 | `callback/framework.go` |
| G-62 | 缺少并发安全测试 | `callback/framework_test.go` |
| G-63 | 缺少回调 panic 时的行为测试 | `callback/framework_test.go` |
| G-64 | 缺少重复注册同一回调的测试 | `callback/framework_test.go` |
| G-65 | 默认 LoggingLLMCallback 干扰测试隔离性 | `callback/framework_test.go` |

---

## 五、提示问题（🔵 共 27 个）

| 编号 | 模块 | 简述 |
|------|------|------|
| T-01 | StreamWriter | Python StreamSchemas 联合类型 vs Go Schema 接口（功能等价） |
| T-02 | StreamWriter | 缺少 DEFAULT_RECEIVE_TIMEOUT 常量（Go 通过可变参数默认覆盖） |
| T-03 | StreamWriter | Python 泛型 StreamWriter vs Go 独立结构体（合理适配） |
| T-04 | StreamWriter | RemoveWriter 不返回被移除的 Writer |
| T-05 | StreamWriter | Emitter Emit 接受 Schema vs Any（设计合理） |
| T-06 | StreamWriter | close_stream 两阶段逻辑已对齐 Python |
| T-07 | StreamWriter | 缺少 create_manager 工厂方法（Go 用 NewXxx 即可） |
| T-08 | Tracer | Python trigger/sync_trigger vs Go 同步方法（无需 sync_trigger） |
| T-09 | Tracer | Python 统一 _handlers 字典 vs Go 独立 agentHandler/workflowHandlers（类型更安全） |
| T-10 | Tracer | SpanManager.refresh_span_record 接收单个 span vs Python Dict（功能等价） |
| T-11 | Tracer | fieldToSnakeCase 函数已定义但未被调用（死代码） |
| T-12 | Tracer | OnInvoke 中 elapsedTime 计算后未使用（Python 中也不生效） |
| T-13 | Tracer | 循环依赖处理（tracerSession/graphInterrupter/BaseWorkflowSession 接口注入） |
| T-14 | Tracer | OnInvoke 非异常路径缺少 update_data（与 Python 一致，无需修改） |
| T-15 | Tracer | OnCallDone 缺少 elapsedTime（Span 基类无此字段，Python setattr 也静默跳过） |
| T-16 | Tracer | AgentHandler FormatData 包含完整 span 对象而非 dict（json tag 保证序列化正确） |
| T-17 | Tracer | OnInvoke 的 exc 参数 default 分支缺少日志 |
| T-18 | Checkpointer | Python Storage.clear 签名差异（Go 统一两个参数，更合理） |
| T-19 | Checkpointer | Python _inner_clear_workflow_session 的 try/finally 语义（8.7 回填时注意） |
| T-20 | Checkpointer | CheckpointerFactory Python 装饰器注册 vs Go init 手动注册（等价） |
| T-21 | Checkpointer | Python 延迟导入 persistence 模块（Go 静态编译无需） |
| T-22 | Checkpointer | Python 不支持 shelve 后端（Go 合理取舍） |
| T-23 | Checkpointer | Release 支持多 agentID（Go 增强，合理） |
| T-24 | Checkpointer | 前缀匹配使用手动切片而非 strings.HasPrefix |
| T-25 | Callback | 缺少 unregister_namespace/unregister_by_tags（6.24 节） |
| T-26 | Callback | TriggerCustom 对 nil data 的处理与其他 Trigger 不一致 |
| T-27 | Callback | 缺少 once/priority 回调支持（6.24 节） |

---

## 六、优先修复建议

### 第一优先级（功能正确性）

1. **S-05/S-06**：Tracer 深拷贝缺失 — 写入流的数据可能被后续修改污染
2. **S-09/S-10**：WorkflowStorage 类型断言问题 — updates 可能丢失
3. **S-07**：updateStartTraceData JSON 失败导致核心字段不设置
4. **S-02**：StreamOutput 超时不传播错误 — 消费者无法区分正常结束和超时
5. **S-04**：StreamOutput goroutine 泄漏 — 缺少 context 取消机制

### 第二优先级（数据安全/一致性）

6. **S-13/S-14/S-15**：Callback 框架注销逻辑问题
7. **S-01/S-03**：StreamQueue 缓冲区大小和 TOCTOU 竞态
8. **S-11**：InMemory 版 PreAgentExecute 语义不一致
9. **S-16/S-17**：Session Config 初始化和 checkpointer nil 防护
10. **S-18**：Node Interact 缺少追踪

### 第三优先级（已标注回填）

11. **S-12**：GraphStore 未实现（8.7 回填）
12. **S-19**：GetEnv/GetNodeConfig 空实现（5.12 回填）
13. **S-08**：序列化格式不兼容（需文档标注）

---

## 七、与 Python 符合度总结

| 模块 | 符合度 | 核心差异 |
|------|--------|---------|
| 5.10 StreamWriter | 80% | 队列大小/超时错误传播/goroutine 泄漏/CustomSchema 结构 |
| 5.11 Session Tracer | 85% | 深拷贝缺失/JSON序列化失败中断/inputs/outputs格式/循环依赖接口注入 |
| 5.9 PersistenceCheckpointer | 75% | 序列化格式/GraphStore未实现/类型断言/InMemory架构差异 |
| Session 集成层 | 70% | Config占位/多处回填未完成/错误处理静默忽略 |
| Callback 框架 | 85% | Off只移除首个/缺少批量清除/缺少panic recovery |

**整体符合度：约 79%** — 核心逻辑路径与 Python 一致，但深拷贝、错误传播、类型断言等细节存在差异。大部分严重问题集中在并发安全（Go 特有的问题）和 Python→Go 语言适配时的语义丢失。
