# 6.4 AgentCallbackEvent 枚举 + AgentCallEventType 重命名设计

## 概述

实现 Agent 生命周期回调事件枚举（`AgentCallbackEvent`），定义 per-Agent 实例级的 10 个事件类型，
供后续 `AgentCallbackContext`（6.5）、`AgentCallbackManager`（6.6）、`AgentRail`（6.7）使用。
同时将已有的框架层 `AgentCallEventType` 重命名为 `AgentCallGlobalEventType`，明确两层事件体系的区分。

## 两层事件体系

| 层次 | 类型 | 事件数 | 用途 | 触发方式 |
|------|------|--------|------|----------|
| 框架层（全局观测） | `AgentCallGlobalEventType` | 5 | 日志/监控/transform_io | `CallbackFramework.OnAgent/TriggerAgent` |
| Rail 层（实例控制） | `AgentCallbackEvent` | 10 | Rail 钩子拦截/重试/提前终止/steering | `AgentCallbackManager.execute` |

两层事件**不桥接**，各自独立运行，与 Python 保持一致。

Python 中 `AgentCallbackManager.register_callback()` 通过 `Runner.callback_framework.register(agent_event, callback)` 走自定义事件名路径（`"{agent_id}_{event}"`），与框架层 `AgentEvents` 是不同注册表条目。

## 决策记录

### 1. 包位置：`single_agent/rail/`

对齐 Python `openjiuwen/core/single_agent/rail/base.py`。
后续 6.5（AgentCallbackContext）、6.7（AgentRail）等也在同一包或子包。

### 2. 枚举实现：`type AgentCallbackEvent string` + 显式字符串值

与已有 `AgentCallGlobalEventType` 风格一致，值与 Python `str, Enum` 完全对应（如 `"before_invoke"`）。
常量名用 `Callback` 前缀（`CallbackBeforeInvoke`）而非 `Agent` 前缀，
避免与框架层常量（`AgentInvokeInput`）混淆，同时在 `rail` 包内避免与 `AgentRail` 类型名冲突。

### 3. 不需要 EVENT_METHOD_MAP

事件值本身就是方法名（`"before_invoke"` = Python `AgentRail.before_invoke` 方法名），
6.7 AgentRail 实现时直接用 `string(event)` 定位方法，无需额外映射表。

### 4. 两层事件不桥接

与 Python 一致，各自独立触发。

### 5. 重命名范围

| 改什么 | 旧名 | 新名 |
|--------|------|------|
| 类型名 | `AgentCallEventType` | `AgentCallGlobalEventType` |
| 常量名 | `AgentStarted` / `AgentInvokeInput` / ... | **不变** |

涉及文件：`callback/events.go`、`callback/framework.go`、`callback/doc.go`、`single_agent/base.go`、`single_agent/base_test.go`

## 枚举定义

```go
// single_agent/rail/event.go

// AgentCallbackEvent Agent 生命周期回调事件类型。
//
// 定义 per-Agent 实例级的 10 个生命周期事件，
// 供 AgentCallbackManager 注册回调和 AgentRail 钩子使用。
// 与框架层 AgentCallGlobalEventType（全局观测事件）是不同层次：
//   - AgentCallbackEvent = 实例级 Rail 拦截/控制（重试/提前终止/steering）
//   - AgentCallGlobalEventType = 框架级全局观测（日志/监控/transform_io）
//
// 事件值即 Python AgentRail 对应方法名，无需额外 EVENT_METHOD_MAP 映射。
//
// 对应 Python: openjiuwen/core/single_agent/rail/base.py (AgentCallbackEvent)
type AgentCallbackEvent string

const (
    // CallbackBeforeInvoke invoke 开始前
    CallbackBeforeInvoke AgentCallbackEvent = "before_invoke"
    // CallbackAfterInvoke invoke 完成后
    CallbackAfterInvoke AgentCallbackEvent = "after_invoke"
    // CallbackBeforeTaskIteration 外层任务循环迭代开始前
    CallbackBeforeTaskIteration AgentCallbackEvent = "before_task_iteration"
    // CallbackAfterTaskIteration 外层任务循环迭代完成后
    CallbackAfterTaskIteration AgentCallbackEvent = "after_task_iteration"
    // CallbackBeforeModelCall LLM 调用前
    CallbackBeforeModelCall AgentCallbackEvent = "before_model_call"
    // CallbackAfterModelCall LLM 响应后
    CallbackAfterModelCall AgentCallbackEvent = "after_model_call"
    // CallbackOnModelException LLM 调用异常
    CallbackOnModelException AgentCallbackEvent = "on_model_exception"
    // CallbackBeforeToolCall 工具执行前
    CallbackBeforeToolCall AgentCallbackEvent = "before_tool_call"
    // CallbackAfterToolCall 工具执行后
    CallbackAfterToolCall AgentCallbackEvent = "after_tool_call"
    // CallbackOnToolException 工具执行异常
    CallbackOnToolException AgentCallbackEvent = "on_tool_exception"
)
```

## 事件在 Agent 会话流程中的位置

```
用户提问
  │
  ▼
Agent.invoke()
  │
  ├─ 【CallbackBeforeInvoke】           ← invoke 开始前
  │
  ├─ Task Iteration Loop（外层任务循环）
  │   │
  │   ├─ 【CallbackBeforeTaskIteration】 ← 迭代开始前
  │   │
  │   ├─ ReAct Loop（Think→Act→Observe）
  │   │   │
  │   │   ├─ 【CallbackBeforeModelCall】  ← LLM 调用前
  │   │   ├─ LLM 调用
  │   │   ├─ 【CallbackAfterModelCall】   ← LLM 响应后
  │   │   │   或 【CallbackOnModelException】 ← LLM 异常
  │   │   │
  │   │   ├─ 【CallbackBeforeToolCall】   ← 工具执行前
  │   │   ├─ 工具执行
  │   │   ├─ 【CallbackAfterToolCall】    ← 工具执行后
  │   │   │   或 【CallbackOnToolException】  ← 工具异常
  │   │   └─ ...
  │   │
  │   └─ 【CallbackAfterTaskIteration】  ← 迭代完成后
  │
  └─ 【CallbackAfterInvoke】            ← invoke 完成后
```

## 重命名 AgentCallEventType → AgentCallGlobalEventType

### 改动清单

| 文件 | 改动内容 |
|------|----------|
| `callback/events.go` | 类型定义 `AgentCallEventType` → `AgentCallGlobalEventType`；`String()` 方法接收者改为 `AgentCallGlobalEventType` |
| `callback/framework.go` | `agentCallbacks` map 类型、`agentTransformIO` map 类型、`OnAgent/OffAgent/RegisterAgentTransformIO/TransformAgentIOInput/TransformAgentIOOutput` 函数签名中的 `AgentCallEventType` → `AgentCallGlobalEventType` |
| `callback/doc.go` | 文档中 `AgentCallEventType` → `AgentCallGlobalEventType` |
| `single_agent/base.go` | `callback.AgentInvokeInput` 等常量引用不变（常量名不改），但类型上下文中涉及 `AgentCallEventType` 的注释更新 |
| `single_agent/base_test.go` | `callback.AgentCallEventType` → `callback.AgentCallGlobalEventType`；常量引用不变 |

常量名（`AgentStarted`/`AgentInvokeInput`/`AgentInvokeOutput`/`AgentStreamInput`/`AgentStreamOutput`）**不变**。

## 新增文件

```
single_agent/rail/
├── doc.go           # 包文档
└── event.go         # AgentCallbackEvent 枚举定义 + String() + AllCallbackEvents()
```

## 回填影响

### 6.4 本步无直接回填

`AgentCallbackEvent` 仅定义枚举，不改变任何已有代码的行为。
5.30 计划中的 `⤵️ 6.4-6.10` 是整体标注，6.4 仅完成最基础的类型定义。

### 后续回填路径

| 步骤 | 回填内容 | 位置 |
|------|----------|------|
| 6.5 | `AgentCallbackContext` 中引用 `AgentCallbackEvent`；`SessionMemoryManager.MaybeScheduleUpdate` 参数可能改为接收 `AgentCallbackContext` | `context_engine/context/session_memory_manager.go` |
| 6.6 | `WarpBaseAgent.callbackManager` 类型从 `any` 改为 `*AgentCallbackManager`；`RegisterCallback/RegisterRail` 委托真实逻辑 | `single_agent/base.go` |
| 6.7-6.10 | 创建 `ContextProcessorRail`（AgentRail 实现），在 `AfterModelCall` 等钩子中调用 ContextEngine 方法 | 新文件 `harness/rails/` 或类似位置 |

## 测试

- `event_test.go`：验证 10 个事件值与 Python `AgentCallbackEvent` 完全对齐
- `event_test.go`：验证 `String()` 方法返回值
- `event_test.go`：验证 `AllCallbackEvents()` 返回全部 10 个事件
- 已有 `base_test.go` 中 `TestAgentCallEventType_事件名对齐Python` 更新为 `TestAgentCallGlobalEventType_事件名对齐Python`
