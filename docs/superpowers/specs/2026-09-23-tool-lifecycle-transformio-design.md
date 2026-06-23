# Tool 层 LifecycleTool 包装 + TransformIO 设计

## 概述

补齐 Tool 层回调生命周期的两个核心缺口：
1. AbilityManager.executeTool 未用 LifecycleTool 包装，Tool 调用不触发任何回调事件
2. Tool 层缺少 TransformIO（CallbackFramework 只有 LLM/Agent 两层 TransformIO）

不对齐 ToolRail（依赖 6.4-6.10 里程碑，留后续回填）。

## 当前状态

### 三层回调对比

| 层级 | transform_io | emit_before | emit_after | 状态 |
|------|-------------|-------------|------------|------|
| LLM (model.go) | TransformLLMIO | TriggerLLM | TriggerLLM | 完整 |
| Agent (base.go) | TransformAgentIO | TriggerAgent | TriggerAgent | 完整 |
| Tool (lifecycle_tool.go) | 未实现 | TriggerTool | TriggerTool | 半完成 |
| Tool (ability_manager.go) | 未接入 | 未接入 | 未接入 | 缺失 |

### Python 对齐

Python `_ToolMeta.__call__`（`openjiuwen/core/foundation/tool/base.py`）分两步包装：

**第一步：`_lifecycle_invoke`** — 内层，负责 STARTED/FINISHED/ERROR：
```python
async def _lifecycle_invoke(*a, **kw):
    await _fw.trigger(ToolCallEvents.TOOL_CALL_STARTED, ...)
    try:
        result = await _original_invoke(*a, **kw)
        await _fw.trigger(ToolCallEvents.TOOL_CALL_FINISHED, ...)
        return result
    except Exception as e:
        await _fw.trigger(ToolCallEvents.TOOL_CALL_ERROR, ...)
        raise
```

**第二步：外层装饰链** — 在 `_lifecycle_invoke` 之上套：
```python
fn = instance.invoke  # 已是 _lifecycle_invoke
fn = _fw.emit_before(ToolCallEvents.TOOL_INVOKE_INPUT)(fn)
fn = _fw.transform_io(TOOL_INVOKE_INPUT, TOOL_INVOKE_OUTPUT)(fn)
fn = _fw.emit_after(ToolCallEvents.TOOL_INVOKE_OUTPUT)(fn)
```

**Python 完整执行顺序：**
```
emit_before(TOOL_INVOKE_INPUT)
  → transform_io input
    → TOOL_CALL_STARTED
    → _original_invoke
    → TOOL_CALL_FINISHED
  → transform_io output
→ emit_after(TOOL_INVOKE_OUTPUT)
```

## 设计方案

### 改动 1：CallbackFramework — 新增 Tool 层 TransformIO

**文件：`internal/agentcore/runner/callback/events.go`**

新增函数类型：
```go
// TransformToolIOInputFunc Tool 层输入变换函数
type TransformToolIOInputFunc func(ctx context.Context, event ToolCallEventType, input map[string]any) map[string]any

// TransformToolIOOutputFunc tool 层输出变换函数
type TransformToolIOOutputFunc func(ctx context.Context, event ToolCallEventType, output map[string]any) map[string]any
```

新增 entry 结构（非导出）：
```go
type toolTransformIOEntry struct {
    inputFn  TransformToolIOInputFunc
    outputFn TransformToolIOOutputFunc
}
```

**文件：`internal/agentcore/runner/callback/framework.go`**

CallbackFramework 结构体新增字段：
```go
toolTransformIO map[ToolCallEventType]*toolTransformIOEntry
```

NewCallbackFramework 中初始化：
```go
toolTransformIO: make(map[ToolCallEventType]*toolTransformIOEntry),
```

新增 3 个方法（与 RegisterLLMTransformIO / TransformLLMIOInput/Output 模式对称）：

```go
// RegisterToolTransformIO 注册 Tool 层 IO 变换回调。
// inputEvent 和 outputEvent 同时作为 key 存入同一 entry。
func (fw *CallbackFramework) RegisterToolTransformIO(
    inputEvent ToolCallEventType,
    outputEvent ToolCallEventType,
    inputFn TransformToolIOInputFunc,
    outputFn TransformToolIOOutputFunc,
)

// TransformToolIOInput 应用 Tool 层输入变换。未注册时透传原始输入。
func (fw *CallbackFramework) TransformToolIOInput(ctx context.Context, event ToolCallEventType, input map[string]any) map[string]any

// TransformToolIOOutput 应用 Tool 层输出变换。未注册时透传原始输出。
func (fw *CallbackFramework) TransformToolIOOutput(ctx context.Context, event ToolCallEventType, output map[string]any) map[string]any
```

### 改动 2：LifecycleTool — 简化构造 + 插入 TransformIO + 对齐 Python 顺序

**文件：`internal/agentcore/foundation/tool/lifecycle_tool.go`**

**2.1 NewLifecycleTool 签名变更**

```go
// NewLifecycleTool 创建带生命周期回调的工具包装器。
// fw 参数可选：不传或传 nil 时自动使用全局回调框架 GetCallbackFramework()。
func NewLifecycleTool(inner Tool, fw ...*runnnercallback.CallbackFramework) *LifecycleTool {
    var f *runnnercallback.CallbackFramework
    if len(fw) > 0 && fw[0] != nil {
        f = fw[0]
    } else {
        f = runnnercallback.GetCallbackFramework()
    }
    return &LifecycleTool{inner: inner, fw: f}
}
```

variadic 参数保持向后兼容：测试中可传显式 fw，AbilityManager 中直接 `NewLifecycleTool(t)`。

**2.2 Invoke 新顺序（对齐 Python）**

原顺序：
```
TOOL_CALL_STARTED → TOOL_INVOKE_INPUT(emit_before) → [执行] → TOOL_INVOKE_OUTPUT(emit_after) → TOOL_CALL_FINISHED
```

新顺序（对齐 Python 两步装饰链）：
```
1. emit_before：TriggerTool(TOOL_INVOKE_INPUT)
2. TransformToolIOInput
3. TriggerTool(TOOL_CALL_STARTED)
4. inner.Invoke(ctx, inputs)
5. [出错] TriggerTool(TOOL_CALL_ERROR) → return
6. TriggerTool(TOOL_CALL_FINISHED)
7. TransformToolIOOutput
8. emit_after：TriggerTool(TOOL_INVOKE_OUTPUT)
```

**2.3 Stream 新顺序（对齐 Python）**

```
1. emit_before：TriggerTool(TOOL_STREAM_INPUT)
2. TransformToolIOInput
3. TriggerTool(TOOL_CALL_STARTED)
4. inner.Stream(ctx, inputs)
5. per-chunk：
   - TransformToolIOOutput
   - TriggerTool(TOOL_RESULT_RECEIVED)
   - TriggerTool(TOOL_STREAM_OUTPUT)
6. [出错] TriggerTool(TOOL_CALL_ERROR) → return
7. Done：TriggerTool(TOOL_CALL_FINISHED)
8. emit_after：TriggerTool(TOOL_STREAM_OUTPUT)
```

**2.4 移除预留注释**

移除 `⤵️ 预留：transform_io 机制` 注释块。

### 改动 3：AbilityManager — 接入 LifecycleTool 包装

**文件：`internal/agentcore/single_agent/ability/ability_manager.go`**

executeTool 中变更：
```go
// 原：
result, err := t.Invoke(ctx, toolArgs)

// 新：
lt := NewLifecycleTool(t)
result, err := lt.Invoke(ctx, toolArgs)
```

移除 TODO 注释。

## 不改动的部分

- **ToolRail / railedExecuteSingleToolCall** — 保持 `⤵️ 预留`，等 6.4-6.10 里程碑
- **LLM/Agent 层 TransformIO** — 不涉及
- **Tool 接口签名** — 不变
- **CallbackFramework 其他域** — 不涉及

## 测试计划

### 新增测试

1. **CallbackFramework TransformToolIO**
   - `TestRegisterToolTransformIO_双键注册` — 注册后 inputEvent 和 outputEvent 都能查到同一 entry
   - `TestTransformToolIOInput_未注册时透传` — 无注册时返回原始 input
   - `TestTransformToolIOInput_已注册时变换` — 有注册时调用 inputFn 变换
   - `TestTransformToolIOOutput_未注册时透传` — 无注册时返回原始 output
   - `TestTransformToolIOOutput_已注册时变换` — 有注册时调用 outputFn 变换

2. **LifecycleTool TransformIO**
   - `TestLifecycleTool_Invoke_TransformIO` — 验证 Invoke 中 TransformIO Input/Output 被调用
   - `TestLifecycleTool_Invoke_TransformIO顺序` — 验证事件触发顺序对齐 Python
   - `TestLifecycleTool_Stream_TransformIO` — 验证 Stream 中 per-chunk TransformIO Output

3. **LifecycleTool 构造**
   - `TestNewLifecycleTool_自动获取全局fw` — 不传 fw 时使用 GetCallbackFramework()

### 修改测试

- `lifecycle_tool_test.go` 中 6 个现有调用 `NewLifecycleTool(inner, fw)` 无需修改（variadic 兼容）

### 影响范围

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `runner/callback/events.go` | 新增 | 2 个函数类型 + 1 个 entry 结构 |
| `runner/callback/framework.go` | 新增 | 1 个字段 + 初始化 + 3 个方法 |
| `runner/callback/framework_test.go` | 新增 | TransformToolIO 测试 |
| `foundation/tool/lifecycle_tool.go` | 修改 | 构造签名 + Invoke/Stream 插入 TransformIO + 移除预留注释 |
| `foundation/tool/lifecycle_tool_test.go` | 修改+新增 | 构造兼容 + TransformIO 测试 |
| `single_agent/ability/ability_manager.go` | 修改 | executeTool 中包装 LifecycleTool + 移除 TODO |
| `single_agent/ability/ability_manager_test.go` | 新增 | 验证 executeTool 走 LifecycleTool 路径 |
