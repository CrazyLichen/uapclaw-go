# Tool 层 LifecycleTool 包装 + TransformIO 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补齐 Tool 层回调生命周期：AbilityManager 接入 LifecycleTool 包装 + CallbackFramework 新增 Tool 层 TransformIO

**Architecture:** 对齐 Python `_ToolMeta.__call__` 两步装饰链模式。Invoke/Stream 的新执行顺序为：emit_before → TransformIO(input) → STARTED → [执行] → FINISHED → TransformIO(output) → emit_after。CallbackFramework 新增 `toolTransformIO` 注册表，与 LLM/Agent 两层 TransformIO 模式对称。

**Tech Stack:** Go 1.22+, sync.RWMutex, variadic 参数模式

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/agentcore/runner/callback/events.go:300-316` | 新增 Tool TransformIO 函数类型 |
| 修改 | `internal/agentcore/runner/callback/framework.go:28-51,108-130,564-565` | 新增 toolTransformIO 字段/初始化/3个方法 |
| 修改 | `internal/agentcore/foundation/tool/lifecycle_tool.go:11-36,46-120` | 构造签名变 variadic + Invoke/Stream 插入 TransformIO + 对齐 Python 顺序 |
| 修改 | `internal/agentcore/single_agent/ability/ability_manager.go:498-502` | 用 LifecycleTool 包装替代直接 Invoke |
| 修改 | `internal/agentcore/runner/callback/framework_test.go` | 新增 Tool TransformIO 测试 |
| 修改 | `internal/agentcore/foundation/tool/lifecycle_tool_test.go` | 新增 TransformIO + 顺序 + 构造测试 |
| 修改 | `internal/agentcore/foundation/tool/doc.go:55-61` | 更新回调生命周期描述 |

---

### Task 1: CallbackFramework events.go — 新增 Tool TransformIO 函数类型

**Files:**
- Modify: `internal/agentcore/runner/callback/events.go:300-316`

- [ ] **Step 1: 在 events.go 的 TransformAgentIOOutputFunc 之后添加 Tool 层函数类型**

在 `events.go` 第 316 行（`TransformAgentIOOutputFunc` 定义）之后添加：

```go
// TransformToolIOInputFunc Tool 层输入变换回调函数类型。
// 接收事件名和原始输入，返回变换后的输入。
// 对齐 Python: transform_io 的 input_fn（TOOL_STREAM_INPUT / TOOL_INVOKE_INPUT）
type TransformToolIOInputFunc func(ctx context.Context, event ToolCallEventType, input map[string]any) map[string]any

// TransformToolIOOutputFunc Tool 层输出变换回调函数类型。
// 接收事件名和原始输出，返回变换后的输出。
// 对齐 Python: transform_io 的 output_fn（TOOL_STREAM_OUTPUT / TOOL_INVOKE_OUTPUT）
type TransformToolIOOutputFunc func(ctx context.Context, event ToolCallEventType, output map[string]any) map[string]any
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/runner/callback/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/runner/callback/events.go
git commit -m "feat(callback): 新增 Tool 层 TransformIO 函数类型"
```

---

### Task 2: CallbackFramework framework.go — 新增 toolTransformIO 字段和方法

**Files:**
- Modify: `internal/agentcore/runner/callback/framework.go:28-51` (结构体)
- Modify: `internal/agentcore/runner/callback/framework.go:108-130` (NewCallbackFramework)
- Modify: `internal/agentcore/runner/callback/framework.go:564-565` (末尾新增方法)

- [ ] **Step 1: 在 CallbackFramework 结构体中新增 toolTransformIO 字段**

在 `framework.go` 第 50 行（`agentTransformIO` 字段）之后添加：

```go
	// toolTransformIO Tool 层 IO 变换回调注册表，键为 inputEvent 或 outputEvent
	toolTransformIO map[ToolCallEventType]*toolTransformIOEntry
```

在 `agentTransformIOEntry` 结构体之后（第 67 行后）添加：

```go
// toolTransformIOEntry Tool 层 TransformIO 注册条目
type toolTransformIOEntry struct {
	// inputFn 输入变换函数
	inputFn TransformToolIOInputFunc
	// outputFn 输出变换函数
	outputFn TransformToolIOOutputFunc
}
```

- [ ] **Step 2: 在 NewCallbackFramework 中初始化 toolTransformIO**

在第 117 行（`agentTransformIO: make(...)` 之后）添加：

```go
		toolTransformIO:    make(map[ToolCallEventType]*toolTransformIOEntry),
```

- [ ] **Step 3: 在 framework.go 末尾（非导出函数区块前）添加 3 个方法**

在 `TransformAgentIOOutput` 方法之后（第 563 行后），非导出函数区块之前添加：

```go
// RegisterToolTransformIO 注册 Tool 层 IO 变换回调。
//
// 对齐 Python: CallbackFramework.transform_io 注册机制。
// inputFn 在 emit_before 前对输入做变换，outputFn 在 emit_after 前对输出做变换。
// 同时用 inputEvent 和 outputEvent 作为 key 注册，确保通过任一事件都能查到 entry。
func (fw *CallbackFramework) RegisterToolTransformIO(
	inputEvent ToolCallEventType,
	outputEvent ToolCallEventType,
	inputFn TransformToolIOInputFunc,
	outputFn TransformToolIOOutputFunc,
) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	entry := &toolTransformIOEntry{
		inputFn:  inputFn,
		outputFn: outputFn,
	}
	fw.toolTransformIO[inputEvent] = entry
	fw.toolTransformIO[outputEvent] = entry
}

// TransformToolIOInput 应用 Tool 层输入变换。
//
// 如果没有注册变换回调，返回原始输入（透传）。
// 对齐 Python: transform_io 的 input_fn 在 emit_before 前执行。
func (fw *CallbackFramework) TransformToolIOInput(ctx context.Context, event ToolCallEventType, input map[string]any) map[string]any {
	fw.mu.RLock()
	entry, ok := fw.toolTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.inputFn == nil {
		return input
	}
	return entry.inputFn(ctx, event, input)
}

// TransformToolIOOutput 应用 Tool 层输出变换。
//
// 如果没有注册变换回调，返回原始输出（透传）。
// 对齐 Python: transform_io 的 output_fn 在 emit_after 前执行。
func (fw *CallbackFramework) TransformToolIOOutput(ctx context.Context, event ToolCallEventType, output map[string]any) map[string]any {
	fw.mu.RLock()
	entry, ok := fw.toolTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.outputFn == nil {
		return output
	}
	return entry.outputFn(ctx, event, output)
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/runner/callback/...`
Expected: 编译成功

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/runner/callback/framework.go
git commit -m "feat(callback): 新增 Tool 层 TransformIO 注册表和方法"
```

---

### Task 3: CallbackFramework TransformToolIO 测试

**Files:**
- Modify: `internal/agentcore/runner/callback/framework_test.go`

- [ ] **Step 1: 编写 TransformToolIO 测试**

在 `framework_test.go` 末尾添加以下测试函数：

```go
func TestRegisterToolTransformIO_双键注册(t *testing.T) {
	fw := NewCallbackFramework()
	var inputCalled, outputCalled bool

	fw.RegisterToolTransformIO(
		ToolInvokeInput, ToolInvokeOutput,
		func(_ context.Context, event ToolCallEventType, input map[string]any) map[string]any {
			inputCalled = true
			input["transformed"] = true
			return input
		},
		func(_ context.Context, event ToolCallEventType, output map[string]any) map[string]any {
			outputCalled = true
			output["transformed"] = true
			return output
		},
	)

	// 通过 inputEvent 查找
	result := fw.TransformToolIOInput(context.Background(), ToolInvokeInput, map[string]any{"key": "val"})
	if !inputCalled {
		t.Error("inputFn 未被调用")
	}
	if result["transformed"] != true {
		t.Error("input 变换未生效")
	}

	// 通过 outputEvent 查找
	outResult := fw.TransformToolIOOutput(context.Background(), ToolInvokeOutput, map[string]any{"key": "val"})
	if !outputCalled {
		t.Error("outputFn 未被调用")
	}
	if outResult["transformed"] != true {
		t.Error("output 变换未生效")
	}
}

func TestTransformToolIOInput_未注册时透传(t *testing.T) {
	fw := NewCallbackFramework()
	input := map[string]any{"key": "val"}
	result := fw.TransformToolIOInput(context.Background(), ToolInvokeInput, input)
	if result["key"] != "val" {
		t.Errorf("未注册时应该透传，got %v", result)
	}
}

func TestTransformToolIOOutput_未注册时透传(t *testing.T) {
	fw := NewCallbackFramework()
	output := map[string]any{"key": "val"}
	result := fw.TransformToolIOOutput(context.Background(), ToolInvokeOutput, output)
	if result["key"] != "val" {
		t.Errorf("未注册时应该透传，got %v", result)
	}
}

func TestTransformToolIOInput_已注册时变换(t *testing.T) {
	fw := NewCallbackFramework()
	fw.RegisterToolTransformIO(
		ToolInvokeInput, ToolInvokeOutput,
		func(_ context.Context, _ ToolCallEventType, input map[string]any) map[string]any {
			input["added"] = "by_transform"
			return input
		},
		nil,
	)
	result := fw.TransformToolIOInput(context.Background(), ToolInvokeInput, map[string]any{"key": "val"})
	if result["added"] != "by_transform" {
		t.Errorf("变换未生效，got %v", result)
	}
}

func TestTransformToolIOOutput_已注册时变换(t *testing.T) {
	fw := NewCallbackFramework()
	fw.RegisterToolTransformIO(
		ToolInvokeInput, ToolInvokeOutput,
		nil,
		func(_ context.Context, _ ToolCallEventType, output map[string]any) map[string]any {
			output["added"] = "by_transform"
			return output
		},
	)
	result := fw.TransformToolIOOutput(context.Background(), ToolInvokeOutput, map[string]any{"key": "val"})
	if result["added"] != "by_transform" {
		t.Errorf("变换未生效，got %v", result)
	}
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v -run 'TestRegisterToolTransformIO|TestTransformToolIO' ./internal/agentcore/runner/callback/...`
Expected: 全部 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/runner/callback/framework_test.go
git commit -m "test(callback): 新增 Tool 层 TransformIO 测试"
```

---

### Task 4: LifecycleTool — 构造签名变 variadic

**Files:**
- Modify: `internal/agentcore/foundation/tool/lifecycle_tool.go:11-36`

- [ ] **Step 1: 修改 NewLifecycleTool 签名为 variadic**

将 `lifecycle_tool.go` 第 18-22 行的预留注释块替换为空（移除），将第 30-36 行替换为：

```go
// NewLifecycleTool 创建带生命周期回调的工具包装器。
//
// fw 参数可选：不传或传 nil 时自动使用全局回调框架 GetCallbackFramework()，
// 对齐 Python: _ToolMeta.__call__ 中通过 Runner.callback_framework 获取。
func NewLifecycleTool(inner Tool, fw ...*runnnercallback.CallbackFramework) *LifecycleTool {
	var f *runnnercallback.CallbackFramework
	if len(fw) > 0 && fw[0] != nil {
		f = fw[0]
	} else {
		f = runnnercallback.GetCallbackFramework()
	}
	return &LifecycleTool{
		inner: inner,
		fw:    f,
	}
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/foundation/tool/...`
Expected: 编译成功（现有测试 `NewLifecycleTool(inner, fw)` 因 variadic 兼容无需修改）

- [ ] **Step 3: 运行现有测试确认不破坏**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v ./internal/agentcore/foundation/tool/...`
Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/foundation/tool/lifecycle_tool.go
git commit -m "refactor(tool): NewLifecycleTool 签名变 variadic，自动获取全局回调框架"
```

---

### Task 5: LifecycleTool — Invoke 插入 TransformIO + 对齐 Python 顺序

**Files:**
- Modify: `internal/agentcore/foundation/tool/lifecycle_tool.go:43-71`

- [ ] **Step 1: 重写 Invoke 方法**

将 `lifecycle_tool.go` 第 43-71 行的 Invoke 方法替换为：

```go
// Invoke 包装生命周期（对齐 Python _ToolMeta 两步装饰链顺序）：
//
//	emit_before(INVOKE_INPUT) → TransformIO(input) → STARTED → [执行] → FINISHED → TransformIO(output) → emit_after(INVOKE_OUTPUT)
//
// 异常时：emit_before(INVOKE_INPUT) → TransformIO(input) → STARTED → [执行] → ERROR
//
// 对应 Python: _lifecycle_invoke（内层 STARTED/FINISHED）+ 外层 emit_before/transform_io/emit_after
func (t *LifecycleTool) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	card := t.inner.Card()

	// 1. emit_before：触发 TOOL_INVOKE_INPUT
	_ = t.fw.TriggerTool(ctx, newInvokeInputData(card, inputs))

	// 2. TransformToolIOInput — 输入变换
	inputs = t.fw.TransformToolIOInput(ctx, ToolInvokeInput, inputs)

	// 3. 触发 TOOL_CALL_STARTED
	_ = t.fw.TriggerTool(ctx, newStartedData(card, inputs))

	// 4. 执行内部 Tool
	result, err := t.inner.Invoke(ctx, inputs, opts...)

	if err != nil {
		// 5. 触发 TOOL_CALL_ERROR
		_ = t.fw.TriggerTool(ctx, newErrorData(card, inputs, err))
		return nil, err
	}

	// 6. 触发 TOOL_CALL_FINISHED
	_ = t.fw.TriggerTool(ctx, newFinishedData(card, inputs, result))

	// 7. TransformToolIOOutput — 输出变换
	result = t.fw.TransformToolIOOutput(ctx, ToolInvokeOutput, result)

	// 8. emit_after：触发 TOOL_INVOKE_OUTPUT
	_ = t.fw.TriggerTool(ctx, newInvokeOutputData(card, result))

	return result, nil
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/foundation/tool/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/lifecycle_tool.go
git commit -m "feat(tool): LifecycleTool.Invoke 插入 TransformIO + 对齐 Python 事件顺序"
```

---

### Task 6: LifecycleTool — Stream 插入 TransformIO + 对齐 Python 顺序

**Files:**
- Modify: `internal/agentcore/foundation/tool/lifecycle_tool.go:73-120`

- [ ] **Step 1: 重写 Stream 方法**

将 Stream 方法替换为：

```go
// Stream 包装生命周期（对齐 Python _ToolMeta 两步装饰链顺序）：
//
//	emit_before(STREAM_INPUT) → TransformIO(input) → STARTED → [执行]
//	  → per-chunk: TransformIO(output) → RESULT_RECEIVED → STREAM_OUTPUT
//	  → Done: FINISHED → emit_after(STREAM_OUTPUT)
//
// 异常时：触发 TOOL_CALL_ERROR
//
// 对应 Python: _lifecycle_stream（内层 STARTED/FINISHED）+ 外层 emit_before/transform_io/emit_after
func (t *LifecycleTool) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	card := t.inner.Card()

	// 1. emit_before：触发 TOOL_STREAM_INPUT
	_ = t.fw.TriggerTool(ctx, newStreamInputData(card, inputs))

	// 2. TransformToolIOInput — 输入变换
	inputs = t.fw.TransformToolIOInput(ctx, ToolStreamInput, inputs)

	// 3. 触发 TOOL_CALL_STARTED
	_ = t.fw.TriggerTool(ctx, newStartedData(card, inputs))

	// 4. 执行内部 Tool
	innerCh, err := t.inner.Stream(ctx, inputs, opts...)
	if err != nil {
		// 出错时触发 TOOL_CALL_ERROR
		_ = t.fw.TriggerTool(ctx, newErrorData(card, inputs, err))
		return nil, err
	}

	// 5. 包装输出 channel，逐 chunk 触发生命周期事件
	outCh := make(chan StreamChunk, 1)
	go func() {
		defer close(outCh)
		for chunk := range innerCh {
			if chunk.Error != nil {
				// 流出错
				_ = t.fw.TriggerTool(ctx, newErrorData(card, inputs, chunk.Error))
				outCh <- chunk
				return
			}
			if chunk.Done {
				// 流正常结束
				// 6. 触发 TOOL_CALL_FINISHED
				_ = t.fw.TriggerTool(ctx, newFinishedData(card, inputs, nil))
				// 7. emit_after：触发 TOOL_STREAM_OUTPUT
				_ = t.fw.TriggerTool(ctx, newStreamOutputData(card, nil))
				outCh <- chunk
				return
			}
			// TransformToolIOOutput — per-chunk 输出变换
			transformedData := t.fw.TransformToolIOOutput(ctx, ToolStreamOutput, chunk.Data)
			// 触发 TOOL_RESULT_RECEIVED
			_ = t.fw.TriggerTool(ctx, newResultReceivedData(card, transformedData))
			// 触发 TOOL_STREAM_OUTPUT
			_ = t.fw.TriggerTool(ctx, newStreamOutputData(card, transformedData))
			// 用变换后的数据构造新 chunk 发给下游
			outCh <- StreamChunk{Data: transformedData}
		}
	}()

	return outCh, nil
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/foundation/tool/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/lifecycle_tool.go
git commit -m "feat(tool): LifecycleTool.Stream 插入 TransformIO + 对齐 Python 事件顺序"
```

---

### Task 7: LifecycleTool — 新增 TransformIO 和顺序测试

**Files:**
- Modify: `internal/agentcore/foundation/tool/lifecycle_tool_test.go`

- [ ] **Step 1: 新增 Invoke TransformIO + 顺序测试**

在 `lifecycle_tool_test.go` 末尾添加：

```go
func TestLifecycleTool_Invoke_TransformIO(t *testing.T) {
	card := NewToolCard("transform_tool", "变换工具", nil, nil)
	inner := &mockTool{
		card: card,
		invokeFn: func(_ context.Context, inputs map[string]any, _ ...ToolOption) (map[string]any, error) {
			return map[string]any{"echo": inputs["msg"]}, nil
		},
	}

	fw := runnnercallback.NewCallbackFramework()
	// 注册 TransformIO：输入加前缀，输出加后缀
	fw.RegisterToolTransformIO(
		runnnercallback.ToolInvokeInput, runnnercallback.ToolInvokeOutput,
		func(_ context.Context, _ runnnercallback.ToolCallEventType, input map[string]any) map[string]any {
			input["msg"] = "prefix_" + input["msg"].(string)
			return input
		},
		func(_ context.Context, _ runnnercallback.ToolCallEventType, output map[string]any) map[string]any {
			output["echo"] = output["echo"].(string) + "_suffix"
			return output
		},
	)

	lt := NewLifecycleTool(inner, fw)
	result, err := lt.Invoke(context.Background(), map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	// 输入变换：msg = "prefix_hello"，inner 返回 echo = "prefix_hello"
	// 输出变换：echo = "prefix_hello_suffix"
	if result["echo"] != "prefix_hello_suffix" {
		t.Errorf("result[echo] = %v, want prefix_hello_suffix", result["echo"])
	}
}

func TestLifecycleTool_Invoke_事件顺序对齐Python(t *testing.T) {
	card := NewToolCard("order_tool", "顺序工具", nil, nil)
	inner := &mockTool{
		card: card,
		invokeFn: func(_ context.Context, _ map[string]any, _ ...ToolOption) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		},
	}

	fw := runnnercallback.NewCallbackFramework()
	var order []string
	fw.OnTool(runnnercallback.ToolInvokeInput, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		order = append(order, "INVOKE_INPUT")
		return nil
	})
	fw.OnTool(runnnercallback.ToolCallStarted, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		order = append(order, "STARTED")
		return nil
	})
	fw.OnTool(runnnercallback.ToolCallFinished, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		order = append(order, "FINISHED")
		return nil
	})
	fw.OnTool(runnnercallback.ToolInvokeOutput, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		order = append(order, "INVOKE_OUTPUT")
		return nil
	})

	lt := NewLifecycleTool(inner, fw)
	_, err := lt.Invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}

	expected := []string{"INVOKE_INPUT", "STARTED", "FINISHED", "INVOKE_OUTPUT"}
	if len(order) != len(expected) {
		t.Fatalf("事件数 = %d, want %d; order = %v", len(order), len(expected), order)
	}
	for i, e := range expected {
		if order[i] != e {
			t.Errorf("事件[%d] = %s, want %s", i, order[i], e)
		}
	}
}

func TestNewLifecycleTool_自动获取全局fw(t *testing.T) {
	card := NewToolCard("auto_fw", "自动fw", nil, nil)
	inner := &mockTool{
		card: card,
		invokeFn: func(_ context.Context, inputs map[string]any, _ ...ToolOption) (map[string]any, error) {
			return inputs, nil
		},
	}

	// 不传 fw，应自动使用全局回调框架
	lt := NewLifecycleTool(inner)
	if lt.fw == nil {
		t.Error("fw 不应为 nil")
	}
	if lt.fw != runnnercallback.GetCallbackFramework() {
		t.Error("fw 应为全局回调框架")
	}
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v -run 'TestLifecycleTool_Invoke_TransformIO|TestLifecycleTool_Invoke_事件顺序|TestNewLifecycleTool_自动获取' ./internal/agentcore/foundation/tool/...`
Expected: 全部 PASS

- [ ] **Step 3: 运行全部已有 LifecycleTool 测试确认不破坏**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v ./internal/agentcore/foundation/tool/...`
Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/foundation/tool/lifecycle_tool_test.go
git commit -m "test(tool): 新增 LifecycleTool TransformIO + 事件顺序测试"
```

---

### Task 8: AbilityManager — 接入 LifecycleTool 包装

**Files:**
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go:498-502`

- [ ] **Step 1: 替换 executeTool 中的直接 Invoke 调用**

将 `ability_manager.go` 第 498-502 行：

```go
	// TODO: 后续需用 NewLifecycleTool(t, fw) 包装，使所有 Tool 调用自动走
	// LifecycleTool 的回调链（TOOL_CALL_STARTED / TOOL_CALL_FINISHED 等），
	// 与 Python 的 LifecycleTool 机制对齐。当前直接调用 t.Invoke() 缺失生命周期回调。

	result, err := t.Invoke(ctx, toolArgs)
```

替换为：

```go
	// 用 LifecycleTool 包装，使 Tool 调用走完整回调链
	// （emit_before → TransformIO → STARTED → [执行] → FINISHED → TransformIO → emit_after）
	// 对齐 Python: _ToolMeta.__call__ 中的自动生命周期注入
	lt := tool.NewLifecycleTool(t)
	result, err := lt.Invoke(ctx, toolArgs)
```

注意：需要在文件头部 import 中确认已有 `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"` 的导入（AbilityManager 已使用 `tool.ToolCard` 等，应已存在）。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/ability/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/single_agent/ability/ability_manager.go
git commit -m "feat(ability): AbilityManager.executeTool 接入 LifecycleTool 包装"
```

---

### Task 9: 更新 tool/doc.go 回调生命周期描述

**Files:**
- Modify: `internal/agentcore/foundation/tool/doc.go:55-61`

- [ ] **Step 1: 更新回调生命周期描述**

将 `doc.go` 第 55-61 行：

```go
// 回调生命周期：
//
//	LifecycleTool 包装器在 Invoke/Stream 调用前后自动触发以下事件
//	（事件定义在 agentcore/runner/callback/ 包中）：
//	  TOOL_CALL_STARTED → TOOL_INVOKE_INPUT → [执行] → TOOL_INVOKE_OUTPUT → TOOL_CALL_FINISHED
//	  异常时触发 TOOL_CALL_ERROR
//	  Stream 模式额外触发 TOOL_RESULT_RECEIVED（逐 chunk）
```

替换为：

```go
// 回调生命周期（对齐 Python _ToolMeta 两步装饰链顺序）：
//
//	LifecycleTool 包装器在 Invoke/Stream 调用前后自动触发以下事件
//	（事件定义在 agentcore/runner/callback/ 包中）：
//	  Invoke: emit_before(INVOKE_INPUT) → TransformIO(input) → STARTED → [执行] → FINISHED → TransformIO(output) → emit_after(INVOKE_OUTPUT)
//	  Stream: emit_before(STREAM_INPUT) → TransformIO(input) → STARTED → [执行] → per-chunk{TransformIO(output) → RESULT_RECEIVED → STREAM_OUTPUT} → FINISHED → emit_after(STREAM_OUTPUT)
//	  异常时触发 TOOL_CALL_ERROR
```

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/foundation/tool/doc.go
git commit -m "docs(tool): 更新回调生命周期描述，对齐 Python 事件顺序"
```

---

### Task 10: 全量编译和测试

**Files:**
- 无新文件

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 运行受影响的包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v ./internal/agentcore/runner/callback/... ./internal/agentcore/foundation/tool/... ./internal/agentcore/single_agent/ability/...`
Expected: 全部 PASS

- [ ] **Step 3: 提交（如有任何遗漏修复）**

如有修复，在此提交。无修复则跳过。
