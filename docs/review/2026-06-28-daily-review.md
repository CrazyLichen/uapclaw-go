# 代码 Review 报告 — 2026-06-28

> 审查范围：最近 24 小时内提交的功能实现
> 涉及章节：6.20+6.21 事件驱动 Controller 底层组件、6.23 ResourceMgr 全局注册表、6.24 AsyncCallbackFramework
> 审查人：AI Review Expert
> 审查日期：2026-06-28

---

## 一、审查概览

| 章节 | 功能 | Go 路径 | Python 参考路径 | 功能符合度 |
|------|------|---------|----------------|-----------|
| 6.20+6.21 | 事件驱动 Controller 底层组件 | `internal/agentcore/controller/` | `openjiuwen/core/controller/` | **85%** |
| 6.23 | ResourceMgr 全局注册表 | `internal/agentcore/runner/resources_manager/` | `openjiuwen/core/runner/resources_manager/` | **82%** |
| 6.24 | AsyncCallbackFramework | `internal/agentcore/runner/callback/` | `openjiuwen/core/runner/callback/` | **75%** |

---

## 二、问题汇总

| 严重程度 | 6.20+6.21 | 6.23 | 6.24 | 合计 |
|---------|-----------|------|------|------|
| 🔴 严重 | 4 | 6 | 3 | **13** |
| 🟡 一般 | 10 | 6 | 5 | **21** |
| 🔵 提示 | 4 | 3 | 2 | **9** |
| **合计** | **18** | **15** | **10** | **43** |

---

## 三、严重问题（🔴 共 13 个）

### 3.1 事件驱动 Controller (6.20+6.21)

#### S-CTRL-01：IntentRecognizer.Recognize() 未实现，返回 nil, nil
- **文件**: `controller/modules/intent_recognizer.go:107-109`
- **描述**: Python `IntentRecognizer.recognize()` 是完整的 LLM 调用 + tool_calls 解析流程（约70行），Go 版本仅占位 `return nil, nil`
- **影响**: 意图识别功能完全不可用，EventHandlerWithIntentRecognition.HandleInput 会收到空 intents 列表
- **Python 差异**: Python 有完整的 context 获取/创建、输入校验、LLM invoke、tool_calls 循环解析逻辑

#### S-CTRL-02：TaskManager.notifyIfSubmitted 在锁内调用回调，可能死锁
- **文件**: `controller/modules/task_manager.go:769-783`
- **描述**: 回调 `tm.onTaskSubmitted()` 在 `tm.mu` 写锁内被调用
- **影响**: 如果回调中尝试获取 TaskManager 的锁（例如通过 TaskScheduler 间接调用），将死锁
- **Python 差异**: Python `add_task` 先 `async with self._lock` 添加任务，然后 `_notify_if_submitted(tasks)` 在锁外调用

#### S-CTRL-03：TaskScheduler.Sessions() 返回内部 map 引用，无并发保护
- **文件**: `controller/modules/task_scheduler.go:169-173`
- **描述**: `Sessions()` 返回内部 map 引用，锁释放后其他 goroutine 可并发读写
- **影响**: 并发 BindSession/UnbindSession/Stream 可能导致 map 并发读写 panic
- **Python 差异**: Python 中 asyncio 单线程 + GIL 保证安全

#### S-CTRL-04：Controller.Stream 中 delete sessions 无锁保护
- **文件**: `controller/controller.go:286`
- **描述**: 直接操作 TaskScheduler 内部的 sessions map，绕过了 TaskScheduler 的 mu 锁
- **影响**: 与 TaskScheduler 调度循环并发访问同一 map，可能 panic

### 3.2 ResourceMgr 全局注册表 (6.23)

#### S-RM-01：RemoveTag 未级联调用 ResourceRegistry.RemoveByID
- **文件**: `resources_manager/resource_manager.go:1022-1025`
- **描述**: Go 的 RemoveTag 仅清理标签索引，不触发子管理器的资源移除
- **影响**: 标签删了但资源仍在子管理器中，产生"孤儿资源"
- **Python 差异**: Python L1242-1271 在 `remove_tag` 中显式调用 `self._resource_registry.remove_by_id(resource_id)`

#### S-RM-02：AddSysOperation 缺少工具自动注册逻辑
- **文件**: `resources_manager/resource_manager.go:768-793`
- **描述**: Go 的 AddSysOperation 仅注册实例到 SysOperationMgr，没有工具自动注册
- **影响**: SysOperation 的操作方法无法作为 Tool 被调用
- **Python 差异**: Python L718-733 调用 `self._register_sys_operation_tools(single_card, instance, tag=tag)`

#### S-RM-03：RemoveSysOperation 缺少关联工具的级联移除
- **文件**: `resources_manager/resource_manager.go:799-821`
- **描述**: Go 的 RemoveSysOperation 没有级联移除关联工具的逻辑
- **影响**: 移除 SysOperation 后，关联的 Tool 仍然残留
- **Python 差异**: Python L754-773 显式处理关联工具移除

#### S-RM-04：SysOperationMgr 全部方法仅返回 "not implemented" 错误
- **文件**: `resources_manager/sys_operation_manager.go:44-62`
- **描述**: 所有方法返回 `fmt.Errorf("sys operation manager not implemented")`
- **影响**: SysOperation 功能完全不可用
- **Python 差异**: Python 有完整实现（L18-86），包括重复注册检测、sandbox key 冲突检测等

#### S-RM-05：AgentMgr.AddAgent 包装 provider 时传 nil card
- **文件**: `resources_manager/agent_manager.go:57-62`
- **描述**: `AddAgent` 将 `AgentProvider` 包装时，调用 `provider(ctx, nil)`，card 参数始终为 nil
- **影响**: 如果 provider 内部依赖 card 来构造 Agent 实例，将得到 nil card
- **Python 差异**: Python `AgentMgr.add_agent` 传入 card 参数

#### S-RM-06：RemoveAgent 还原 provider 时丢失原始 AgentProvider 签名
- **文件**: `resources_manager/agent_manager.go:105-107`
- **描述**: 还原后的 provider 忽略原始 card 参数，信息丢失
- **影响**: 还原后的 provider 无法再用原始 card 调用
- **Python 差异**: Python 的 `_unregister_resource_provider` 直接返回原始 provider 引用

### 3.3 AsyncCallbackFramework (6.24)

#### S-CB-01：TriggerParallel 并非真正并发执行
- **文件**: `callback/framework.go:1198-1207`
- **描述**: Go 的 TriggerParallel 直接调用 triggerCallbacks，这是顺序执行
- **影响**: 接口语义与 Python `trigger_parallel` 的 `asyncio.gather` 并发执行完全不同
- **Python 差异**: Python 使用 `asyncio.gather` 真正并发执行

#### S-CB-02：事件历史功能只写不读（死代码）
- **文件**: `callback/framework.go:97-101, 1277-1281`
- **描述**: 声明了 `enableEventHistory` 和 `eventHistory` 字段，但 triggerCallbacks 中没有写入历史记录的代码，也没有读取方法
- **影响**: 功能声明但未实现，属于死代码
- **Python 差异**: Python 有 `get_event_history()`/`replay_events()` 方法

#### S-CB-03：FilterActionModify 不生效
- **文件**: `callback/framework.go:1367-1369`
- **描述**: 过滤器返回 FilterActionModify 时，Go 代码没有修改 data 就继续执行回调
- **影响**: MODIFY 动作完全不生效
- **Python 差异**: Python 在 trigger() 中会更新 `final_args`/`final_kwargs`

---

## 四、一般问题（🟡 共 21 个）

### 4.1 事件驱动 Controller (6.20+6.21)

| 编号 | 问题 | 文件 | Python 差异 |
|------|------|------|------------|
| G-CTRL-01 | Controller._ensure_started() 缺少事件循环变更检测和组件重建逻辑 | `controller.go:457-469` | Python 检测 event loop 变更后 stop + 重建组件 |
| G-CTRL-02 | TaskFailedEvent 缺少自定义 JSON 序列化 | `event.go:72-82` | 其他事件类型都有 MarshalJSON |
| G-CTRL-03 | TaskExecutor 接口方法签名与 Python 有差异（增加了 error 返回） | `task_executor.go:20-32` | Python 无 error 返回 |
| G-CTRL-04 | TaskScheduler.contextEngine 和 card 使用 any 类型而非具体接口 | `task_scheduler.go:29,39` | Python 有具体类型 |
| G-CTRL-05 | EventHandlerWithIntentRecognition 的 errgroup 并发策略与 Python 的 asyncio.gather 语义不同 | `intent_recognizer.go:149-197` | Go fail-fast vs Python 宽松模式 |
| G-CTRL-06 | Controller.Stream 非 ControllerOutputChunk 类型 chunk 被跳过而非转发 | `controller.go:332-338` | Python 直接 yield 转发 |
| G-CTRL-07 | TaskManager.GetTask 的 UserID 过滤未实现 | `task_manager.go:788-817` | Python 有 user_id 过滤 |
| G-CTRL-08 | TaskFilter 缺少 validate_at_least_one_filter 校验 | `task_manager.go:17-34` | Python 有 model_validator |
| G-CTRL-09 | EventQueue.PublishEvent 中 WaitResponse 错误未区分 BaseError 和普通 error | `event_queue.go:208-220` | Python 区分 BaseError 透传 |
| G-CTRL-10 | TaskScheduler.waitAllTasksComplete 中 busy-wait 循环 | `task_scheduler.go:987-998` | Python 用 asyncio.gather 优雅等待 |

### 4.2 ResourceMgr 全局注册表 (6.23)

| 编号 | 问题 | 文件 | Python 差异 |
|------|------|------|------------|
| G-RM-01 | ThreadSafeDict.Delete 注释说"不存在时 panic"但实际不会 | `thread_safe_dict.go:65-72` | 注释与行为不一致 |
| G-RM-02 | ThreadSafeDict 使用 RWMutex 而非 RLock，不可重入 | `thread_safe_dict.go` | Python 用 RLock |
| G-RM-03 | ToolMgr 的锁粒度不统一（ThreadSafeDict vs 手动锁） | `tool_manager.go:49-62` | Python GIL 天然序列化 |
| G-RM-04 | innerFindResourceIDs 当 tag 为 TagGlobal 时不验证资源是否存在 | `resource_manager.go:1162-1188` | Python 的 _inner_get_resources 检查 has_resource |
| G-RM-05 | AddPrompts 静默跳过无效条目而不返回错误 | `prompt_manager.go:67-82` | Python 抛 ValueError |
| G-RM-06 | AddAgent 双重重复注册检查 | `resource_manager.go:1411-1417` | Python 也在两层检查 |

### 4.3 AsyncCallbackFramework (6.24)

| 编号 | 问题 | 文件 | Python 差异 |
|------|------|------|------------|
| G-CB-01 | updateMetrics 锁粒度不够直观 | `framework.go:1559-1568` | Python 单线程无此问题 |
| G-CB-02 | Off 方法只移除第一个匹配 | `framework.go:248-253` | Python 移除所有匹配项 |
| G-CB-03 | getCallbackNameFromAny 返回类型名而非函数名 | `framework.go:1576-1578` | Python 用 `callback.__name__` |
| G-CB-04 | AddCircuitBreaker 不同时添加到过滤器链 | `framework.go:1163-1168` | Python 统一走 Filter 链 |
| G-CB-05 | CallbackChain.rollback 持锁时间过长 | `chain.go:285-298` | Python 回滚不加锁 |

---

## 五、提示问题（🔵 共 9 个）

### 5.1 事件驱动 Controller (6.20+6.21)

| 编号 | 问题 | 文件 | 建议 |
|------|------|------|------|
| T-CTRL-01 | deepCopyTask 使用 JSON 序列化/反序列化，性能劣于 Python 的 model_copy | `task_manager.go:976-1000` | 考虑逐字段拷贝 |
| T-CTRL-02 | joinStrings 工具函数可用 strings.Join 替代 | `intent_recognizer.go:457-466` | 替换为 strings.Join |
| T-CTRL-03 | IntentToolkits Go 版修复了 Python 版的 choices 过滤 bug | `intent_toolkits.go:217-232` | 正向差异，无需修改 |
| T-CTRL-04 | ControllerOutputPayload.Type 为 string，Python 为 Literal 限定类型 | `controller_output.go:16` | 可考虑用常量枚举 |

### 5.2 ResourceMgr 全局注册表 (6.23)

| 编号 | 问题 | 文件 | 建议 |
|------|------|------|------|
| T-RM-01 | AddTool 不支持批量添加 | `resource_manager.go:458` | 考虑添加 AddTools 批量方法 |
| T-RM-02 | remove 方法不支持按 tag 批量移除 | `resource_manager.go:282-311` | 考虑添加批量移除方法 |
| T-RM-03 | innerValidateTag 实现简化，缺少多标签校验 | `resource_manager.go:1592-1599` | 对齐 Python 多标签校验 |

### 5.3 AsyncCallbackFramework (6.24)

| 编号 | 问题 | 文件 | 建议 |
|------|------|------|------|
| T-CB-01 | Off 方法删除 slice 元素时有潜在内存泄漏 | `framework.go:250` | 设 nil 断开引用 |
| T-CB-02 | ChainActionRetry 重试次数耗尽后结果放入 Results 可能不是预期行为 | `chain.go:231-236` | 评估是否应视为失败 |

---

## 六、缺失功能汇总

### 6.1 事件驱动 Controller (6.20+6.21)

| 功能 | Python 路径 | 说明 |
|------|------------|------|
| IntentRecognizer.Recognize() 完整实现 | `controller/intent_recognizer.py` | LLM 调用 + context + tool_calls 解析 |
| InputEvent.from_user_input 对 InteractiveInput 类型的支持 | `controller/schema/event.py` | Go 仅支持 3 种输入 |
| EventQueue.unsubscribe_all() | `controller/modules/event_queue.py` | 取消所有订阅 |
| EventHandler.wait_completion timeout 实现 | `controller/modules/event_handler.py` | Go 有参数但默认忽略 |
| TaskManager 批量 add/update/remove 支持 | `controller/modules/task_manager.py` | Go 仅支持单个操作 |
| ControllerOutput.input_event_id 在 Invoke 中设置 | `controller/schema/controller_output.py` | Go 有字段但未设置 |

### 6.2 ResourceMgr 全局注册表 (6.23)

| 功能 | Python 路径 | 说明 |
|------|------------|------|
| SysOperation 完整实现 | `resources_manager/sys_operation_manager.py` | 包括重复检测、sandbox key 管理 |
| SysOperation 工具自动注册 | `resources_manager/resource_manager.py` | `_register_sys_operation_tools` |
| remove_tag 级联移除资源 | `resources_manager/resource_manager.py` | 遍历 affected_resources 调用 remove_by_id |
| AgentTeamMgr 完整实现 | `resources_manager/agent_team_manager.py` | Go 全部返回 "not implemented" |
| AgentMgr 分布式支持 | `resources_manager/agent_manager.py` | RemoteAgent/AgentAdapter/distributed_mode |
| MCP get_mcp_tool 自动刷新 | `resources_manager/resource_manager.py` | 获取前调用 refresh_tool_server |
| get_tool_infos 按 tool_type 过滤 | `resources_manager/resource_manager.py` | Go 不支持 type 过滤 |

### 6.3 AsyncCallbackFramework (6.24)

| 功能 | Python 路径 | 说明 |
|------|------------|------|
| emit_before/emit_after/emit_around 装饰器 | `callback/decorator.py` | 核心用法，Go 需手动调用 Trigger |
| trigger_delayed | `callback/framework.py` | 延迟触发 |
| trigger_stream/trigger_generator | `callback/framework.py` | 流式/生成器模式触发 |
| 按 namespace/tags 批量注销 | `callback/framework.py` | Go 只能按指针逐个注销 |
| unregister_event | `callback/framework.py` | 一次性清除某事件的所有回调 |
| 事件历史回放 | `callback/framework.py` | get_event_history/replay_events |
| 状态持久化 | `callback/framework.py` | save_state |
| 查询接口 | `callback/framework.py` | list_events/list_callbacks |
| FilterResult 支持 modified_args/modified_kwargs | `callback/models.py` | Go 只有 ModifiedData any |

---

## 七、合理额外实现（Go 特有，非问题）

| 章节 | 额外实现 | 说明 |
|------|---------|------|
| 6.20+6.21 | Controller.Invoke 返回 errCh 双 channel | Go 惯例适配，替代 Python async generator |
| 6.20+6.21 | sendStreamError 区分 BaseError/非 BaseError | 对齐 Python except BaseError: raise 语义 |
| 6.20+6.21 | TaskExecutorRegistry 的 sync.RWMutex 并发保护 | Go 必要的并发安全 |
| 6.20+6.21 | executeTaskWrapper 的 recover() panic 捕获 | Go 特有的 panic 恢复机制 |
| 6.23 | ThreadSafeDict 丰富的集合操作方法 | 对齐 Python MutableMapping 接口 |
| 6.23 | Functional Options 模式替代 **kwargs | Go 惯例适配 |
| 6.24 | 多域拆分注册表（12 个独立 map） | Go 类型安全设计 |
| 6.24 | triggerCallbacks 泛型函数 | Go 统一各域触发逻辑 |
| 6.24 | CallbackInfo[F] 泛型包装 | Go 类型安全 |

---

## 八、优先修复建议

### P0 — 必须立即修复（影响功能正确性/可能 panic）

1. **S-CTRL-03 + S-CTRL-04**: TaskScheduler.Sessions() 返回内部 map 无并发保护 — 可能导致运行时 panic
2. **S-CTRL-02**: TaskManager.notifyIfSubmitted 在锁内调用回调 — 可能死锁
3. **S-CB-01**: TriggerParallel 并非真正并发执行 — 接口语义错误
4. **S-CB-03**: FilterActionModify 不生效 — 核心功能失效

### P1 — 尽快修复（功能缺失影响使用）

5. **S-CTRL-01**: IntentRecognizer.Recognize() 未实现 — 意图识别不可用
6. **S-RM-01**: RemoveTag 未级联移除资源 — 孤儿资源
7. **S-RM-02 + S-RM-03**: SysOperation 工具自动注册和级联移除 — SysOperation 功能不完整
8. **S-RM-04**: SysOperationMgr 全部返回 "not implemented" — SysOperation 完全不可用
9. **S-RM-05 + S-RM-06**: AgentMgr AddAgent/RemoveAgent 丢失 card 信息

### P2 — 计划修复（功能缺失但不阻塞核心路径）

10. **S-CB-02**: 事件历史功能死代码 — 移除或实现
11. **G-CTRL-07**: TaskManager.GetTask 的 UserID 过滤
12. **G-CTRL-09**: EventQueue.PublishEvent 错误类型区分
13. 各章节缺失功能（按项目优先级排序）

---

## 九、结论

本次审查覆盖了 24 小时内实现的 3 个核心模块（6.20+6.21、6.23、6.24），发现 **13 个严重问题、21 个一般问题、9 个提示问题**。

### 关键风险

1. **并发安全风险最高**：Controller 和 TaskScheduler 中多处 map 无锁并发访问，可能导致运行时 panic（S-CTRL-03、S-CTRL-04）
2. **功能完整性不足**：SysOperation 和 IntentRecognizer 两个核心功能未真正实现（S-CTRL-01、S-RM-04）
3. **级联操作缺失**：RemoveTag/RemoveSysOperation 缺少级联清理，可能产生孤儿资源（S-RM-01、S-RM-03）

### 建议优先级

- **立即**：修复并发安全问题（P0-1, P0-2），否则 Controller 在生产环境下大概率 panic
- **本周**：修复 TriggerParallel 语义错误和 FilterActionModify 不生效（P0-3, P0-4）
- **下周**：补全 IntentRecognizer 和 SysOperation 实现（P1）
- **后续迭代**：逐步补全缺失功能和一般问题
