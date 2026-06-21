# 代码审查报告 2026-06-20

> 审查范围：最近 24 小时内提交的代码，涵盖领域五（会话系统）5.12-5.18 章节
> 审查方法：对比 Python 参考项目 `openjiuwen`，检查功能符合度和实现缺陷
> 审查日期：2026-06-20

---

## 审查范围

24 小时内共 28 个提交，涉及以下功能章节：

| 章节 | 功能 | Python 参考 | 状态 |
|------|------|------------|------|
| 5.12 | Session Config | `openjiuwen/core/session/config/` | ✅ |
| 5.13 | Session Constants | `openjiuwen/core/session/constants.py` | ✅ |
| 5.14 | Session Utils | `openjiuwen/core/session/utils.py` | ✅ |
| 5.15 | ModelContext 接口 | `openjiuwen/core/context_engine/base.py` | ✅ |
| 5.16 | ContextWindow / ContextStats | `openjiuwen/core/context_engine/base.py` | ✅ |
| 5.17 | ContextEngineConfig | `openjiuwen/core/context_engine/schema/config.py` | ✅ |
| 5.18 | BaseMessage 接口化 + Offload 消息模型 | `openjiuwen/core/context_engine/schema/messages.py` | ✅ |

此外还涉及 6.19 code review 修复、tracer↔single_agent 循环依赖打破、SessionConfig 移到 config 包消除循环依赖、BaseMessage 接口化全项目适配等重构工作。

---

## 问题汇总

| 级别 | 数量 |
|------|------|
| 🔴 严重 | 7 |
| 🟡 一般 | 22 |
| 🔵 提示 | 15 |

---

## 🔴 严重问题（7 个）

### S1. 环境变量优先级与 Python 相反 — Go 中 os.Getenv 优先于 context.Value，Python 中相反

- **章节**: 5.12 SessionConfig
- **文件**: `internal/agentcore/session/config/env_loader.go:134-154`
- **详细说明**: Python 的 `_load_env_configs` 对每个 `(env_key, config_key)` 对**先**调用 `_try_set_env(os.environ.get(env_key))`，**再**调用 `_try_set_env(workflow_session_vars.get().get(env_key))`。Python 的 `_try_set_env` 逻辑是"值非 None 就覆盖"，所以**后者覆盖前者**，即 `workflow_session_vars`（对应 Go 的 `context.Value`）的优先级**高于** `os.environ`。而 Go 的 `loadEnvConfigs` 在第 139-145 行，先读 `os.Getenv`，如果已经设置了就 `continue` 跳过 `context.Value`，这意味着 `os.Getenv` 优先级**高于** `context.Value`。

  Python 优先级: `workflow_session_vars > os.environ > 内置默认值`
  Go 优先级: `os.Getenv > context.Value > 内置默认值`

  **影响**: 当同时设置了环境变量和 context 注入时，Python 使用 context 注入值，Go 使用 os.Getenv 值，同一配置在两个语言中取值不同。

### S2. ParseListIndexes 对 `['key']` 引号路径未剥除引号

- **章节**: 5.14 Session Utils
- **文件**: `internal/agentcore/session/utils/string.go:73-104`
- **详细说明**: Python `split_nested_path` 使用 `re.findall(r"\[(-?\d+)\]|\[\'([^\']+)\'\]", param)` 正则，会**剥除引号**，将 `['key']` 解析为字符串 `"key"`（不含引号）。而 Go 的 `ParseListIndexes` 对于非整数索引内容直接保留 `indexStr`，即 `"'key'"`（含引号）。

  ```python
  # Python
  split_nested_path("a['key']")  # → ["a", "key"]
  ```

  ```go
  // Go
  SplitNestedPath("a['key']")  // → []any{"a", "'key'"}  ← 包含引号
  ```

  **影响**: 后续 `GetValueByNestedPath("a['key']", source)` 会以 `"'key'"` 为 key 查找 map，无法匹配原始 key `"key"`，导致查询失败返回 nil。

### S3. deepCopyMessages 反序列化为 DefaultMessage 丢失具体类型信息

- **章节**: 5.18 BaseMessage 接口化
- **文件**: `internal/agentcore/foundation/prompt/assembler.go:420`
- **详细说明**: `deepCopyMessages` 函数将所有 BaseMessage 统一反序列化为 `schema.DefaultMessage`，丢失了原始的具体类型信息。一个 `*AssistantMessage`（含 ToolCalls、FinishReason 等特有字段）经过深拷贝后变成了 `*DefaultMessage`，所有 AssistantMessage 特有字段全部丢失。同样，ToolMessage 的 `ToolCallID` 也会丢失。

  **应使用 `UnmarshalMessage` 工厂函数替代直接反序列化为 DefaultMessage。**

### S4. convertOneMessage 无法正确处理 Offload 消息类型

- **章节**: 5.18 BaseMessage 接口化
- **文件**: `internal/agentcore/foundation/llm/model_clients/base_client.go:501`
- **详细说明**: `convertOneMessage` 通过 `toBaseMessage(msg)` 提取 BaseMessage 接口后，再用类型断言 `msg.(*llmschema.AssistantMessage)` 提取特有字段。然而，当传入 Offload 消息时（如 `*OffloadAssistantMessage`），它嵌入了 `AssistantMessage` 但本身不是 `*AssistantMessage` 类型，类型断言会失败，导致 Offload 消息的 `tool_calls` 和 `reasoning_content` 不会被序列化到 OpenAI dict 中。

  **影响**: 如果使用了 Offload 机制，调用 LLM 时 Offload 消息的工具调用和推理内容会丢失。

### S5. UnmarshalMessage 无法识别 Offload 消息

- **章节**: 5.18 BaseMessage 接口化
- **文件**: `internal/agentcore/foundation/llm/schema/message.go:319`
- **详细说明**: `UnmarshalMessage` 只根据 `role` 字段分派到 4 种基本消息类型，无法识别 Offload 消息。如果 JSON 数据包含 `offload_type` 和 `offload_handle` 字段，`UnmarshalMessage` 会将其反序列化为基本的 AssistantMessage/UserMessage 等，丢失 OffloadInfo 信息。

  当前项目中存在两个独立的反序列化工厂（`UnmarshalMessage` 和 `UnmarshalOffloadMessage`），调用方需要预先知道消息是否为 Offload 类型才能选择正确的工厂，这在实际使用中不可行（如从数据库/检查点读取消息时无法预知类型）。

  **建议**: 在 `UnmarshalMessage` 中增加对 `offload_type` 字段的检测，如果存在则委托给 `UnmarshalOffloadMessage`，实现统一的反序列化入口。

### S6. StreamEmitter.Emit 与 Close 之间存在竞态窗口

- **章节**: 6.19 review 修复
- **文件**: `internal/agentcore/session/stream/emitter.go:43-55`
- **详细说明**: `Emit` 方法先 `RLock` 读取 `closed`，然后释放锁再调用 `queue.Send`。在 `RLock` 释放到 `Send` 调用之间，`Close` 可能已经调用 `queue.Close()`，导致 `Send` 向已关闭的 channel 写入而 panic。虽然 `Queue.Send` 有 `recover` 兜底返回 `ErrQueueClosed`，但 `Emit` 将 `ErrQueueClosed` 包装为 `StatusStreamWriterWriteStreamError` 返回给调用方，这与 Python 行为不一致。

  **影响**: 并发场景下流写入可能产生误导性错误。

### S7. InMemoryCheckpointer.innerClearWorkflowSession 存在数据竞争

- **章节**: 6.19 review 修复
- **文件**: `internal/agentcore/session/checkpointer/inmemory.go:928-931`
- **详细说明**: `innerClearWorkflowSession` 先获取 `RLock` 读取 `workflowStore` 和 `workflowIDs`，然后释放 `RLock`，再在 `Lock` 下修改 `workflowIDs`。在 `RLock` 释放到 `Lock` 获取之间，其他 goroutine 可能通过 `innerSaveWorkflowCheckpoint` 修改了同一个 `workflowIDs` map，造成数据竞争。

  **建议**: 将 `innerClearWorkflowSession` 改为全程使用 `Lock` 而非 `RLock+Lock`。

---

## 🟡 一般问题（22 个）

### G1. defaultSessionConfig 不是线程安全 — SetEnvs/GetEnv/GetEnvs 无并发保护

- **章节**: 5.12 SessionConfig
- **文件**: `internal/agentcore/session/config/config.go:107-136`
- **详细说明**: 如果多个 goroutine 同时调用 `SetEnvs` 和 `GetEnv`，会触发 `fatal error: concurrent map read and map write`。建议添加 `sync.RWMutex` 保护 `env` 字段。

### G2. GetEnvs 不是真正的深拷贝 — 注释与实现不符

- **章节**: 5.12 SessionConfig
- **文件**: `internal/agentcore/session/config/config.go:119-125`
- **详细说明**: Python 的 `get_envs()` 使用 `deepcopy(self._env)`，Go 的 `GetEnvs` 只做了 map 的浅拷贝。注释写的是"深拷贝"但实际是浅拷贝。当前内置配置值都是基础类型，暂无影响，但 `SetEnvs` 传入嵌套结构时有隐患。建议修改注释为"浅拷贝"或实现真正的深拷贝。

### G3. GetWorkflowConfig/AddWorkflowConfig 对空字符串静默返回而非报错

- **章节**: 5.12 SessionConfig
- **文件**: `internal/agentcore/session/config/config.go:140-145, 161-169`
- **详细说明**: Python 在 `workflow_id is None` 时 `raise ValueError`，Go 对空字符串静默返回 nil。如果调用者传入空字符串，Go 会静默忽略，可能导致配置丢失后难以排查。建议至少在 `AddWorkflowConfig` 中记录警告日志。

### G4. NewSession 中创建 SessionConfig 使用 context.Background()

- **章节**: 5.12 SessionConfig
- **文件**: `internal/agentcore/session/agent.go:93`
- **详细说明**: `cfg := config.NewSessionConfig(context.Background())`，无法从上层 context 传播请求级环境变量和超时控制。建议 `NewSession` 接受 `ctx context.Context` 参数。

### G5. callbackMetadata 字段初始化但无任何访问方法 — 死代码

- **章节**: 5.12 SessionConfig
- **文件**: `internal/agentcore/session/config/config.go:62`
- **详细说明**: `defaultSessionConfig` 有 `callbackMetadata map[string]MetadataLike` 字段，但 `SessionConfig` 接口没有暴露任何访问方法。建议添加接口方法或移除该字段。

### G6. EndCompTemplateBranchRenderTimeoutKey 常量名与 Python BATCH_READER 不一致

- **章节**: 5.13 Session Constants
- **文件**: `internal/agentcore/session/constants/constants.go:44`
- **详细说明**: Python 中常量名为 `END_COMP_TEMPLATE_BATCH_READER_TIMEOUT_KEY`，Go 中命名为 `EndCompTemplateBranchRenderTimeoutKey`，"BatchReader" vs "BranchRender" 语义不同，会让对照代码的开发者难以找到对应关系。

### G7. UpdateByKey 的 int key 分支缺少递归合并逻辑

- **章节**: 5.14 Session Utils
- **文件**: `internal/agentcore/session/utils/dict.go:48-56`
- **详细说明**: Python 的 `update_by_key` 对 int key 也做了递归合并处理，Go 版本只做简单覆盖。但由于 Go 中 int key 对应 list 索引，实际影响有限。

### G8. DeleteByKey 的 int key 行为与 Python 不一致

- **章节**: 5.14 Session Utils
- **文件**: `internal/agentcore/session/utils/dict.go:67-70`
- **详细说明**: Python `delete_by_key` 对 int key 直接 return（不做操作），Go 的 `DeleteByKey` 对 int key 执行 `list[k] = nil`（将元素设为 nil）。语义差异：Python 不删除列表元素，Go 用 nil 占位。

### G9. RootToPath 的 createIfAbsent 时中间值为原始类型时错误覆盖

- **章节**: 5.14 Session Utils
- **文件**: `internal/agentcore/session/utils/path.go:136-151`
- **详细说明**: Python 在中间节点为非 dict/list 非 None 时，即使 `create_if_absent=True` 也返回 `(None, None)`，不会替换原始值。Go 版本在 `create=true` 时强制替换为新的空 map，可能意外丢失数据。

  例如：`source = {"a": "string_value"}`，调用 `RootToPath("a.b", source, true)`
  - Python：返回 `(None, None)`，source 不变
  - Go：将 `source["a"]` 替换为 `map[string]any{}`，原始值丢失

### G10. PopMessages 注释"从尾部弹出"与 Python 语义不一致

- **章节**: 5.15 ModelContext 接口
- **文件**: `internal/agentcore/context_engine/base.go:34`
- **详细说明**: Python `pop_messages` 明确说明"Remove and return the **oldest** size messages"，即从头部（最旧端）移除。Go 注释写"从尾部弹出"，语义相反。建议改为"移除最旧的 size 条消息"。

### G11. AddMessages 参数使用 any 类型，缺乏类型安全

- **章节**: 5.15 ModelContext 接口
- **文件**: `internal/agentcore/context_engine/base.go:41`
- **详细说明**: Go 接口 `AddMessages(ctx context.Context, message any)` 用 `any` 接受单条或列表消息，过于宽泛。Python 使用 `BaseMessage | List[BaseMessage]` 联合类型。建议使用 `[]BaseMessage` 切片参数，单条消息包装为切片。

### G12. 3 个 Offload 非 Assistant 子类型缺少自定义 MarshalJSON/UnmarshalJSON

- **章节**: 5.18 Offload 消息模型
- **文件**: `internal/agentcore/context_engine/schema/offload.go:42-72`
- **详细说明**: 只有 `OffloadAssistantMessage` 实现了自定义序列化。`OffloadUserMessage`、`OffloadSystemMessage`、`OffloadToolMessage` 没有实现。当 `DefaultMessage.Metadata` 和 `OffloadInfo.Metadata` 同时有值时，JSON 序列化时后者会覆盖前者，反序列化时只有一个字段能被填充。

### G13. OffloadInfo.Metadata 与 DefaultMessage.Metadata 的 JSON tag 冲突

- **章节**: 5.18 Offload 消息模型
- **文件**: `internal/agentcore/context_engine/schema/offload.go:33-40`
- **详细说明**: `OffloadInfo.Metadata` 的 JSON tag 为 `"metadata,omitempty"`，与 `DefaultMessage.Metadata` 的 JSON tag 冲突。Python 中多继承会让同名字段合并，Go 中两个字段是独立的存储空间。如果同时设置了消息元数据和卸载元数据，其中一个会丢失。

### G14. NewOffloadMessage 工厂函数对 Assistant 分支丢失 opts 选项

- **章节**: 5.18 Offload 消息模型
- **文件**: `internal/agentcore/context_engine/schema/offload.go:132-144`
- **详细说明**: `NewOffloadMessage` 的 `opts` 参数类型是 `MessageOption`，但 `NewOffloadAssistantMessage` 接受的是 `AssistantMessageOption`，类型不兼容，导致工厂函数创建的 OffloadAssistantMessage 无法携带 ToolCalls。

### G15. SqlMessageStore 反序列化丢失具体消息类型

- **章节**: 5.18 BaseMessage 接口化
- **文件**: `internal/agentcore/memory/manage/model/sql_message_store.go:432-434`
- **详细说明**: `rowToMessageAndMeta` 方法从数据库读取消息后统一构造为 `schema.NewDefaultMessage(role, "")`，丢失了 ToolCallID、ToolCalls、FinishReason 等特有字段。

### G16. ContextEngineConfig.Validate 使用值接收器

- **章节**: 5.17 ContextEngineConfig
- **文件**: `internal/agentcore/context_engine/schema/config.go:55`
- **详细说明**: `Validate()` 使用值接收器，无法在校验后修改字段（如做字段规范化）。当前仅校验不改值，但如需扩展建议改为指针接收器。

### G17. AbilityManager.Execute 中读锁在 goroutine 启动前释放

- **章节**: 5.15 回填
- **文件**: `internal/agentcore/single_agent/ability_manager.go:372-385`
- **详细说明**: goroutine 启动后立即释放读锁，但 goroutine 内部会读取 `am.tools`、`am.workflows` 等字段，如果并发有 Add/Remove 操作，可能读到不一致状态。

### G18. StreamQueue.Close 中 closed.Load 与 closed.Store 存在非原子的检查-设置

- **章节**: 6.19 review 修复
- **文件**: `internal/agentcore/session/stream/queue.go:173-177`
- **详细说明**: `Close` 先 `closed.Load()` 检查，再 `closed.Store(true)`，两次原子操作之间不是原子的。建议使用 `CompareAndSwap` 替代。

### G19. OnInvoke 中 exc!=nil 和 exc==nil 分支有大量重复代码

- **章节**: 6.19 review 修复
- **文件**: `internal/agentcore/session/tracer/handler.go:289-355`
- **详细说明**: `innerError` 提取和 `OnInvokeData` 追加逻辑完全相同，仅 `EndTime`/`Error`/`elapsed_time` 设置有差异。违反 DRY 原则。

### G20. sendData 中硬编码 context.Background()

- **章节**: 6.19 review 修复
- **文件**: `internal/agentcore/session/tracer/handler.go:642`
- **详细说明**: `sendData` 使用 `context.Background()` 而非传入的 context，无法支持超时和取消。对比 `EmitStreamWriter` 方法接收 `ctx` 参数并传递给 `streamWriter.Write`。

### G21. processInteractiveInputs 中 nodeState.Update 返回错误被忽略

- **章节**: 6.19 review 修复
- **文件**: `internal/agentcore/session/checkpointer/base.go:148-150`
- **详细说明**: `_ = nodeState.Update(map[string]any{constants.InteractiveInputKey: ...})`，Update 返回的 error 被忽略。如果 `Update` 实现可能返回错误（如只读状态），应至少记录日志。

### G22. TokenCounter 方法签名缺少 model 参数的默认值语义说明

- **章节**: 5.15 TokenCounter
- **文件**: `internal/agentcore/context_engine/token/base.go:17-23`
- **详细说明**: Python 的 `model` 参数有默认值 `""`，是 keyword-only 参数。Go 中 `model` 是位置参数，接口注释未说明 `model=""` 表示使用默认模型。

---

## 🔵 提示问题（15 个）

### T1. WithEnvs 的 key 应使用环境变量键名而非配置键名

- **章节**: 5.12 SessionConfig
- **文件**: `internal/agentcore/session/config/context.go:24`
- **详细说明**: `WithEnvs` 注入的 map 应使用环境变量键名（如 `"WORKFLOW_EXECUTE_TIMEOUT"`），而非配置键名（如 `"_execute_timeout"`）。当前测试使用正确，但文档注释未明确说明此约定。

### T2. loadEnvConfigs 中 os.Getenv 返回空字符串时无法区分"未设置"和"空字符串值"

- **章节**: 5.12 SessionConfig
- **文件**: `internal/agentcore/session/config/env_loader.go:139`
- **详细说明**: 可使用 `os.LookupEnv` 区分。当前所有配置键类型都是 float/int/bool，空字符串在类型转换时被拒绝，暂无实际影响。

### T3. loadEnvConfigs 优先级验证缺少关键测试

- **章节**: 5.12 SessionConfig
- **文件**: `internal/agentcore/session/config/env_loader_test.go`
- **详细说明**: 缺少 `os.Getenv` 和 `context.Value` 同时设置时的优先级测试。

### T4. Go 默认值使用 float64 而 Python 使用 int

- **章节**: 5.13 Session Constants
- **文件**: `internal/agentcore/session/constants/constants.go:70-89`
- **详细说明**: Python 中超时默认值使用整数，Go 中定义为 float64。由于类型映射表已声明这些键为 float 类型，Go 的做法实际上更一致。这是合理的改进。

### T5. IsRefPath 额外添加了长度上限检查（Python 无此限制）

- **章节**: 5.14 Session Utils
- **文件**: `internal/agentcore/session/utils/ref.go:6-9`
- **详细说明**: Go 版本添加了 `len(path) <= RegexMaxLength` 的上限检查，这是 Python 中没有的额外限制。实际影响极小。

### T6. getBySchema 参数签名使用 ...any 不够清晰

- **章节**: 5.14 Session Utils
- **文件**: `internal/agentcore/session/state/utils.go:12-23`
- **详细说明**: 可变参数 `isRootOrNestedPath ...any` 通过类型断言区分参数，无法在编译时检查类型，且无法同时传入 nestedPath 和 isRoot。

### T7. ContainsChar 函数命名与行为不匹配

- **章节**: 5.14 Session Utils
- **文件**: `internal/agentcore/session/utils/string.go:6-8`
- **详细说明**: `ContainsChar` 实际支持多字符子串搜索，不仅仅是"字符"。

### T8. SetMessages 缺少 error 返回值

- **章节**: 5.15 ModelContext 接口
- **文件**: `internal/agentcore/context_engine/base.go:32`
- **详细说明**: Python 的 `set_messages` 通过 `validate_messages` 校验失败时抛异常，Go 当前签名无法返回 error。建议预留 `error` 返回值。

### T9. ContextEngineConfig Validate 校验逻辑与 Python gt=0 约束的映射可更明确

- **章节**: 5.17 ContextEngineConfig
- **文件**: `internal/agentcore/context_engine/schema/config.go:55-74`
- **详细说明**: 建议在注释中明确说明 Go 的 0 与 Python None 的映射关系。

### T10. EnableTiktokenCounter 字段注释过于简略

- **章节**: 5.17 ContextEngineConfig
- **文件**: `internal/agentcore/context_engine/schema/config.go:26`
- **详细说明**: 缺少 Python docstring 中的详细使用场景和默认值说明。

### T11. TokenCounter 缺少 Python `**kwargs` 的扩展点

- **章节**: 5.15 TokenCounter
- **文件**: `internal/agentcore/context_engine/token/base.go:17-23`
- **详细说明**: 未来添加参数需修改接口签名。建议预留 `...Option` 参数。

### T12. ContextEngine 的 CreateContext 直接使用 *session.Session 而非接口

- **章节**: 5.15 回填
- **文件**: `internal/agentcore/context_engine/base.go:65`
- **详细说明**: 所有实现者必须依赖具体的 `session.Session` 类型，耦合度较高。

### T13. normalizeOutputStream 中 index 类型断言为 int，可能与 JSON 反序列化冲突

- **章节**: 6.19 review 修复
- **文件**: `internal/agentcore/session/agent.go:545`
- **详细说明**: JSON 反序列化 `map[string]any` 时 `index` 是 `float64` 而非 `int`，类型断言会失败。

### T14. EmitStreamWriter 中类型断言可能 panic

- **章节**: 6.19 review 修复
- **文件**: `internal/agentcore/session/tracer/handler.go:432, 448`
- **详细说明**: `data["type"].(string)` 硬类型断言，如果 `type` 键不存在会 panic。建议使用安全类型断言。

### T15. OffloadType 缺少枚举约束

- **章节**: 5.18 Offload 消息模型
- **文件**: `internal/agentcore/context_engine/schema/offload.go:35`
- **详细说明**: `OffloadType` 是 string 类型，没有枚举约束。建议定义枚举类型，避免拼写错误。

---

## 各章节审查总结

### 5.12 SessionConfig

功能覆盖度良好。核心接口、默认实现、BuiltinConfigLoader 钩子、WithEnvs context 注入、env_loader 类型转换均已实现。**最严重问题是环境变量优先级与 Python 相反**，需要明确决策：如果 Go 项目有意改变优先级，应在注释中标注；如果要对齐 Python 行为，需修改 `loadEnvConfigs` 逻辑让 `context.Value` 后覆盖 `os.Getenv`。

### 5.13 Session Constants

所有 Python 中定义的配置键名常量（9个）、环境变量键名常量（7个）、默认值（9个）、映射表（7条）和类型映射（7条）在 Go 中均有完整覆盖，无遗漏。唯一问题是 `EndCompTemplateBranchRenderTimeoutKey` 命名与 Python 的 `BATCH_READER` 不一致。

### 5.14 Session Utils

20 个导出函数完整覆盖了 Python 中需要移植的功能。**两个严重问题**：`ParseListIndexes` 未剥除引号会导致带引号的字典键路径查询失败；`RootToPath` 在 createIfAbsent 时中间值为原始类型会错误覆盖，可能导致数据丢失。

### 5.15-5.16 ModelContext + ContextWindow/ContextStats

核心接口方法完整覆盖，ContextStats 和 ContextWindow 字段与 Python 一一对应。主要风险点是 PopMessages 注释语义错误可能导致实现者误解。

### 5.17 ContextEngineConfig

字段和 Validate 逻辑与 Python 对齐。0 表示不限（对应 Python None）的映射合理。整体实现质量好，仅有提示级别的文档改进建议。

### 5.18 BaseMessage 接口化 + Offload 消息模型

BaseMessage 接口化的适配工作**不完整**。3 个严重问题集中在：反序列化时统一退化为 DefaultMessage 丢失类型信息、Offload 消息与现有处理管道（convertOneMessage、UnmarshalMessage）之间存在适配断裂。建议优先修复这 3 个严重问题，确保 Offload 消息在整个处理管道中正确流转。

### 6.19 Review 修复 + 重构

stream 包双层锁设计基本正确，Emit 的竞态窗口虽有 recover 兜底但错误语义不对齐。tracer 包逻辑正确性良好。checkpointer 包 `innerClearWorkflowSession` 的锁升级模式存在数据竞争风险。循环依赖打破方案完整正确。

---

## 建议修复优先级

1. **P0 — 立即修复**：S1（优先级相反）、S2（引号未剥除）、S3/S4/S5（Offload 消息类型丢失链路）
2. **P1 — 本迭代修复**：S6（竞态窗口）、S7（数据竞争）、G9（RootToPath 数据丢失）、G12/G13（metadata 冲突）
3. **P2 — 下迭代修复**：其余一般和提示级别问题
