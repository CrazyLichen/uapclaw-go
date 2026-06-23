# StreamResult 移除与 Stream 回调架构对齐 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 移除 StreamResult，修复 channel 竞争问题，对齐 Python 的 Stream 回调架构（per_item LLMStreamOutput + transform_io 装饰器链 + Tracer 非阻塞）

**Architecture:** OpenAI 客户端 goroutine 内部累积 final_message 并通过 tracer_record_data 传出；Model 层对齐 Python 装饰器链（transform_io input → emit_before → 调用 → transform_io output → emit_after per_item）；TracedModelClient 逐 chunk 透传而非 Final() 阻塞；CallbackFramework 新增 TransformLLMIO/TransformAgentIO 接口默认透传

**Tech Stack:** Go 1.23+, channel-based streaming, CallbackFramework

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 删除 | `model_clients/stream_result.go` | StreamResult 类型（含 channel 竞争的 Merge goroutine） |
| 删除 | `model_clients/stream_result_test.go` | StreamResult 测试 |
| 修改 | `model_clients/base_client.go` | BaseModelClient.Stream() 返回类型改为纯 channel |
| 修改 | `model_clients/doc.go` | 移除 stream_result.go 条目 |
| 修改 | `model_clients/openai/client.go` | Stream 返回纯 channel，goroutine 内加 final_message 累积 + tracer(llm_response) |
| 修改 | `model_clients/deepseek/client.go` | Stream 返回类型跟着改 |
| 修改 | `model_clients/siliconflow/client.go` | Stream 返回纯 channel，加 final_message + tracer(llm_response) + LLM_RESPONSE_RECEIVED |
| 修改 | `model_clients/inference_affinity/client.go` | 同 SiliconFlow |
| 修改 | `model_clients/intellirouter/client.go` | Stream 返回类型跟着改，不补齐回调 |
| 修改 | `llm/model.go` | Stream per_item + transform_io；Invoke 加 transform_io；调整 emit 顺序 |
| 修改 | `runner/callback/framework.go` | 新增 TransformLLMIO*/TransformAgentIO* 接口 + 透传实现 |
| 修改 | `runner/callback/events.go` | 新增 TransformIO 回调函数类型 |
| 修改 | `single_agent/base.go` | Invoke/Stream 加入 transform_io 占位，调整 emit 顺序 |
| 修改 | `session/tracer/decorator.go` | Stream 改为逐 chunk 透传 + 异步 TraceLLMEnd |
| 修改 | 所有受影响的 `*_test.go` | 适配新返回类型和新行为 |
| 修改 | 若干 `doc.go` | 更新文件目录和说明 |

---

### Task 1: 删除 StreamResult

**Files:**
- 删除: `internal/agentcore/foundation/llm/model_clients/stream_result.go`
- 删除: `internal/agentcore/foundation/llm/model_clients/stream_result_test.go`
- 修改: `internal/agentcore/foundation/llm/model_clients/doc.go`

- [ ] **Step 1: 删除 stream_result.go 和 stream_result_test.go**

```bash
rm internal/agentcore/foundation/llm/model_clients/stream_result.go
rm internal/agentcore/foundation/llm/model_clients/stream_result_test.go
```

- [ ] **Step 2: 更新 model_clients/doc.go，移除 stream_result.go 条目**

从文件目录树中删除 `stream_result.go` 和 `stream_result_test.go` 的条目，更新包功能概述中 StreamResult 相关描述。

- [ ] **Step 3: 编译确认**

此时编译会失败（多处引用 StreamResult），预期行为。后续 Task 逐步修复。

---

### Task 2: BaseModelClient 接口签名变更

**Files:**
- 修改: `internal/agentcore/foundation/llm/model_clients/base_client.go:49-54`

- [ ] **Step 1: 修改 BaseModelClient.Stream() 返回类型**

将：
```go
Stream(ctx context.Context, messages MessagesParam, opts ...StreamOption) (*StreamResult, error)
```
改为：
```go
Stream(ctx context.Context, messages MessagesParam, opts ...StreamOption) (<-chan *llmschema.AssistantMessageChunk, error)
```

同时更新 Stream 方法的注释，说明返回的是纯 chunk channel，调用方通过 `range chunkChan` 消费。

---

### Task 3: CallbackFramework 新增 TransformIO 接口

**Files:**
- 修改: `internal/agentcore/runner/callback/events.go`
- 修改: `internal/agentcore/runner/callback/framework.go`
- 修改: `internal/agentcore/runner/callback/framework_test.go`

- [ ] **Step 1: 在 events.go 中新增 TransformIO 回调函数类型**

在事件类型和回调函数类型区块中新增：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// （无新增结构体，TransformIO 使用现有 LLMCallEventType / AgentCallEventType）

// ──────────────────────────── 枚举 ────────────────────────────

// （无新增枚举，复用现有 LLMCallEventType / AgentCallEventType）

// ──────────────────────────── 常量 ────────────────────────────

// （无新增常量）

// ──────────────────────────── 全局变量 ────────────────────────────

// （无新增全局变量）

// ──────────────────────────── 导出函数 ────────────────────────────

// （无新增导出函数）

// ──────────────────────────── 非导出函数 ────────────────────────────
```

在回调函数类型区块（`LLMCallbackFunc` 等附近）新增：

```go
// TransformLLMIOInputFunc LLM 层输入变换回调函数类型。
// 接收事件名和原始输入，返回变换后的输入。
// 对齐 Python: transform_io 的 input_fn（LLM_STREAM_INPUT / LLM_INVOKE_INPUT）
type TransformLLMIOInputFunc func(ctx context.Context, event LLMCallEventType, input any) any

// TransformLLMIOOutputFunc LLM 层输出变换回调函数类型。
// 接收事件名和原始输出，返回变换后的输出。
// 对齐 Python: transform_io 的 output_fn（LLM_STREAM_OUTPUT / LLM_INVOKE_OUTPUT）
type TransformLLMIOOutputFunc func(ctx context.Context, event LLMCallEventType, output any) any

// TransformAgentIOInputFunc Agent 层输入变换回调函数类型。
// 对齐 Python: transform_io 的 input_fn（AGENT_STREAM_INPUT / AGENT_INVOKE_INPUT）
type TransformAgentIOInputFunc func(ctx context.Context, event AgentCallEventType, input any) any

// TransformAgentIOOutputFunc Agent 层输出变换回调函数类型。
// 对齐 Python: transform_io 的 output_fn（AGENT_STREAM_OUTPUT / AGENT_INVOKE_OUTPUT）
type TransformAgentIOOutputFunc func(ctx context.Context, event AgentCallEventType, output any) any
```

- [ ] **Step 2: 在 CallbackFramework 结构体中新增 TransformIO 注册表字段**

```go
// llmTransformIO LLM 层 IO 变换回调注册表，键为 inputEvent
llmTransformIO map[LLMCallEventType]*llmTransformIOEntry
// agentTransformIO Agent 层 IO 变换回调注册表，键为 inputEvent
agentTransformIO map[AgentCallEventType]*agentTransformIOEntry
```

其中 entry 类型为非导出：

```go
// llmTransformIOEntry LLM 层 TransformIO 注册条目
type llmTransformIOEntry struct {
	inputFn  TransformLLMIOInputFunc
	outputFn TransformLLMIOOutputFunc
}

// agentTransformIOEntry Agent 层 TransformIO 注册条目
type agentTransformIOEntry struct {
	inputFn  TransformAgentIOInputFunc
	outputFn TransformAgentIOOutputFunc
}
```

- [ ] **Step 3: 在 NewCallbackFramework 中初始化 TransformIO 注册表**

```go
llmTransformIO:   make(map[LLMCallEventType]*llmTransformIOEntry),
agentTransformIO: make(map[AgentCallEventType]*agentTransformIOEntry),
```

- [ ] **Step 4: 实现 Register / Transform 方法**

```go
// RegisterLLMTransformIO 注册 LLM 层 IO 变换回调。
//
// 对齐 Python: CallbackFramework.transform_io 注册机制。
// inputFn 在 emit_before 前对输入做变换，outputFn 在 emit_after 前对输出做变换。
// 具体变换逻辑在 6.24 节实现，当前注册后默认透传。
func (fw *CallbackFramework) RegisterLLMTransformIO(
	inputEvent LLMCallEventType,
	outputEvent LLMCallEventType,
	inputFn TransformLLMIOInputFunc,
	outputFn TransformLLMIOOutputFunc,
) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.llmTransformIO[inputEvent] = &llmTransformIOEntry{
		inputFn:  inputFn,
		outputFn: outputFn,
	}
}

// TransformLLMIOInput 应用 LLM 层输入变换。
//
// 如果没有注册变换回调，返回原始输入（透传）。
// 对齐 Python: transform_io 的 input_fn 在 emit_before 前执行。
func (fw *CallbackFramework) TransformLLMIOInput(ctx context.Context, event LLMCallEventType, input any) any {
	fw.mu.RLock()
	entry, ok := fw.llmTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.inputFn == nil {
		return input
	}
	return entry.inputFn(ctx, event, input)
}

// TransformLLMIOOutput 应用 LLM 层输出变换。
//
// 如果没有注册变换回调，返回原始输出（透传）。
// 对齐 Python: transform_io 的 output_fn 在 emit_after 前执行。
func (fw *CallbackFramework) TransformLLMIOOutput(ctx context.Context, event LLMCallEventType, output any) any {
	fw.mu.RLock()
	entry, ok := fw.llmTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.outputFn == nil {
		return output
	}
	return entry.outputFn(ctx, event, output)
}

// RegisterAgentTransformIO 注册 Agent 层 IO 变换回调。
//
// 对齐 Python: CallbackFramework.transform_io 注册机制。
func (fw *CallbackFramework) RegisterAgentTransformIO(
	inputEvent AgentCallEventType,
	outputEvent AgentCallEventType,
	inputFn TransformAgentIOInputFunc,
	outputFn TransformAgentIOOutputFunc,
) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.agentTransformIO[inputEvent] = &agentTransformIOEntry{
		inputFn:  inputFn,
		outputFn: outputFn,
	}
}

// TransformAgentIOInput 应用 Agent 层输入变换。
//
// 如果没有注册变换回调，返回原始输入（透传）。
func (fw *CallbackFramework) TransformAgentIOInput(ctx context.Context, event AgentCallEventType, input any) any {
	fw.mu.RLock()
	entry, ok := fw.agentTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.inputFn == nil {
		return input
	}
	return entry.inputFn(ctx, event, input)
}

// TransformAgentIOOutput 应用 Agent 层输出变换。
//
// 如果没有注册变换回调，返回原始输出（透传）。
func (fw *CallbackFramework) TransformAgentIOOutput(ctx context.Context, event AgentCallEventType, output any) any {
	fw.mu.RLock()
	entry, ok := fw.agentTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.outputFn == nil {
		return output
	}
	return entry.outputFn(ctx, event, output)
}
```

- [ ] **Step 5: 写 TransformIO 透传测试**

在 `framework_test.go` 中新增：

```go
func TestCallbackFramework_TransformLLMIO_透传(t *testing.T) {
	fw := callback.NewCallbackFramework()
	ctx := context.Background()

	// 未注册时透传
	input := map[string]any{"key": "value"}
	result := fw.TransformLLMIOInput(ctx, callback.LLMStreamInput, input)
	assert.Equal(t, input, result)

	output := &llmschema.AssistantMessageChunk{}
	result = fw.TransformLLMIOOutput(ctx, callback.LLMStreamOutput, output)
	assert.Equal(t, output, result)
}

func TestCallbackFramework_TransformAgentIO_透传(t *testing.T) {
	fw := callback.NewCallbackFramework()
	ctx := context.Background()

	input := map[string]any{"key": "value"}
	result := fw.TransformAgentIOInput(ctx, callback.AgentStreamInput, input)
	assert.Equal(t, input, result)

	result = fw.TransformAgentIOOutput(ctx, callback.AgentStreamOutput, input)
	assert.Equal(t, input, result)
}

func TestCallbackFramework_TransformLLMIO_注册后变换(t *testing.T) {
	fw := callback.NewCallbackFramework()
	ctx := context.Background()

	// 注册变换：输入加前缀，输出加倍
	fw.RegisterLLMTransformIO(
		callback.LLMStreamInput, callback.LLMStreamOutput,
		func(ctx context.Context, event callback.LLMCallEventType, input any) any {
			return "transformed_" + input.(string)
		},
		func(ctx context.Context, event callback.LLMCallEventType, output any) any {
			return output.(string) + output.(string)
		},
	)

	result := fw.TransformLLMIOInput(ctx, callback.LLMStreamInput, "hello")
	assert.Equal(t, "transformed_hello", result)

	result = fw.TransformLLMIOOutput(ctx, callback.LLMStreamOutput, "ab")
	assert.Equal(t, "abab", result)
}
```

- [ ] **Step 6: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/... -v -run TestCallbackFramework_Transform
```

- [ ] **Step 7: 提交**

```
feat(callback): 新增 TransformLLMIO/TransformAgentIO 回调接口和透传实现
```

---

### Task 4: OpenAI 客户端 Stream 改造

**Files:**
- 修改: `internal/agentcore/foundation/llm/model_clients/openai/client.go:214-403`

- [ ] **Step 1: 修改 Stream 方法签名**

将返回类型从 `(*model_clients.StreamResult, error)` 改为 `(<-chan *llmschema.AssistantMessageChunk, error)`。

- [ ] **Step 2: 在 SSE 消费 goroutine 内部新增 final_message 累积逻辑**

在 goroutine 内 `accumulatedContent` 变量旁新增：

```go
// 对齐 Python: final_message 累积（在 generator 内部通过 __add__ 合并）
var finalMessage *llmschema.AssistantMessageChunk
```

在每个 chunk 发送到 channel 前（`LLMResponseReceived` 回调触发之后、`chunkChan <- chunk` 之前），新增：

```go
// 对齐 Python: final_message = final_message + parsed_chunk
if finalMessage == nil {
    finalMessage = chunk
} else {
    finalMessage = finalMessage.Merge(chunk)
}
```

- [ ] **Step 3: 在流结束时新增 tracer_record_data(llm_response=finalMessage) 回调**

在 `io.EOF` 分支中，`LLMOutput` 回调触发之前，新增：

```go
// 对齐 Python: if tracer_record_data: await tracer_record_data(llm_response=final_message)
if params.TracerRecordData != nil {
    params.TracerRecordData(map[string]any{"llm_response": finalMessage})
}
```

- [ ] **Step 4: 修改返回语句**

将 `return model_clients.NewStreamResult(chunkChan), nil` 改为 `return chunkChan, nil`。

- [ ] **Step 5: 更新方法注释**

更新 Stream 方法的文档注释，说明：
- 返回纯 chunk channel，调用方通过 `range chunkChan` 消费
- goroutine 内部累积 final_message，流结束时通过 tracer_record_data(llm_response=finalMessage) 传出
- 不再创建 StreamResult

---

### Task 5: DeepSeek 客户端 Stream 返回类型适配

**Files:**
- 修改: `internal/agentcore/foundation/llm/model_clients/deepseek/client.go:99-112`

- [ ] **Step 1: 修改 Stream 方法签名**

将返回类型从 `(*model_clients.StreamResult, error)` 改为 `(<-chan *llmschema.AssistantMessageChunk, error)`。

DeepSeek 委托给 OpenAI，OpenAI 的 Stream 已经在 Task 4 中改好，所以只需改签名即可，方法体不需要改动（`return c.OpenAIModelClient.Stream(...)` 自然返回新类型）。

---

### Task 6: SiliconFlow 客户端 Stream 改造

**Files:**
- 修改: `internal/agentcore/foundation/llm/model_clients/siliconflow/client.go`

- [ ] **Step 1: 修改 Stream 方法签名**

将返回类型从 `(*model_clients.StreamResult, error)` 改为 `(<-chan *llmschema.AssistantMessageChunk, error)`。

- [ ] **Step 2: 在 SSE 消费 goroutine 内部新增 final_message 累积逻辑**

同 Task 4 Step 2：在 goroutine 内新增 `var finalMessage *llmschema.AssistantMessageChunk`，每个 chunk 发送到 channel 前做 Merge 累积。

- [ ] **Step 3: 新增 LLM_RESPONSE_RECEIVED 回调**

在每个 chunk 发送到 channel 前（final_message 累积之前），新增：

```go
// 对齐 OpenAI: 逐 chunk 触发 LLMResponseReceived 回调
_ = callback.GetCallbackFramework().TriggerLLM(ctx, &callback.LLMCallEventData{
    Event:         callback.LLMResponseReceived,
    ModelName:     modelName,
    ModelProvider: c.ClientConfig.ClientProvider,
    IsStream:      true,
})
```

- [ ] **Step 4: 在流结束时新增 tracer_record_data(llm_response=finalMessage) 回调**

同 Task 4 Step 3：在 `io.EOF` 分支中 `LLMOutput` 回调触发前，新增 `tracer_record_data(llm_response=finalMessage)` 调用。

- [ ] **Step 5: 修改返回语句**

将 `return model_clients.NewStreamResult(chunkChan), nil` 改为 `return chunkChan, nil`。

---

### Task 7: InferenceAffinity 客户端 Stream 改造

**Files:**
- 修改: `internal/agentcore/foundation/llm/model_clients/inference_affinity/client.go`

- [ ] **Step 1-5: 同 Task 6**

与 SiliconFlow 完全相同的改造步骤：
1. 修改 Stream 方法签名
2. 新增 final_message 累积逻辑
3. 新增 LLM_RESPONSE_RECEIVED 回调
4. 新增 tracer_record_data(llm_response=finalMessage) 回调
5. 修改返回语句

---

### Task 8: IntelliRouter 客户端 Stream 返回类型适配

**Files:**
- 修改: `internal/agentcore/foundation/llm/model_clients/intellirouter/client.go`

- [ ] **Step 1: 修改 Stream 方法签名**

将返回类型从 `(*model_clients.StreamResult, error)` 改为 `(<-chan *llmschema.AssistantMessageChunk, error)`。

- [ ] **Step 2: 修改返回语句**

将 `return model_clients.NewStreamResult(chunkChan), nil` 改为 `return chunkChan, nil`。

注意：IntelliRouter 不补齐回调/final_message/tracer_record_data（对齐 Python IntelliRouter 本身也缺失）。

---

### Task 9: Model 层 Stream/Invoke 改造

**Files:**
- 修改: `internal/agentcore/foundation/llm/model.go`

- [ ] **Step 1: 修改 Model.Stream() 方法签名**

将返回类型从 `(*model_clients.StreamResult, error)` 改为 `(<-chan *llmschema.AssistantMessageChunk, error)`。

- [ ] **Step 2: 重写 Model.Stream() 方法体**

对齐 Python 装饰器链执行顺序：`transform_io input → emit_before → 调用 → transform_io output(per item) → emit_after(per item)`

```go
func (m *Model) Stream(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	params := model_clients.NewStreamParams(opts...)
	modelName := m.resolveStreamModelName(params.Model)
	fw := m.callbackFramework

	// ① transform_io 输入变换（对齐 Python transform_io 的 input_fn）
	_ = fw.TransformLLMIOInput(ctx, callback.LLMStreamInput, messages)

	// ② emit_before: 触发 LLMStreamInput 事件（流开始前）
	_ = fw.TriggerLLM(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMStreamInput,
		ModelName:     modelName,
		ModelProvider: m.ClientConfig.ClientProvider,
		IsStream:      true,
		Extra: map[string]any{
			"model_config":        m.ModelConfig,
			"model_client_config": m.ClientConfig,
		},
	})

	// 调用底层客户端
	chunkChan, err := m.client.Stream(ctx, messages, opts...)
	if err != nil {
		_ = fw.TriggerLLM(ctx, &callback.LLMCallEventData{
			Event:         callback.LLMCallError,
			ModelName:     modelName,
			ModelProvider: m.ClientConfig.ClientProvider,
			IsStream:      true,
			Error:         err,
			Extra: map[string]any{
				"model_config":        m.ModelConfig,
				"model_client_config": m.ClientConfig,
			},
		})
		return nil, err
	}

	// 包装 channel：per-item { ③ transform_io 输出变换 → ④ emit_after }
	out := make(chan *llmschema.AssistantMessageChunk)
	go func() {
		defer close(out)
		for chunk := range chunkChan {
			// ③ transform_io 输出变换（对齐 Python transform_io 的 output_fn，per item）
			chunk = fw.TransformLLMIOOutput(ctx, callback.LLMStreamOutput, chunk).(*llmschema.AssistantMessageChunk)

			// ④ emit_after (per_item): 每 chunk 触发 LLMStreamOutput
			var usage *llmschema.UsageMetadata
			if chunk != nil && chunk.UsageMetadata != nil {
				usage = chunk.UsageMetadata
			}
			_ = fw.TriggerLLM(ctx, &callback.LLMCallEventData{
				Event:         callback.LLMStreamOutput,
				ModelName:     modelName,
				ModelProvider: m.ClientConfig.ClientProvider,
				IsStream:      true,
				Response:      chunk,
				Usage:         usage,
				Extra: map[string]any{
					"model_config":        m.ModelConfig,
					"model_client_config": m.ClientConfig,
				},
			})

			out <- chunk
		}
	}()

	return out, nil
}
```

关键变化：
- 删除原来的 `Final()` 后台 goroutine 和一次性 `LLMStreamOutput` 触发
- 新增 per_item 包装：每个 chunk 触发 `LLMStreamOutput`
- 新增 `TransformLLMIOInput` / `TransformLLMIOOutput` 调用
- 执行顺序：transform_io input → emit_before → 调用 → per-item(transform_io output → emit_after)

- [ ] **Step 3: 重写 Model.Invoke() 方法体**

对齐 Python 装饰器链执行顺序：`transform_io input → emit_before → 调用 → transform_io output → emit_after`

在当前 `Invoke` 方法中：
- 在 `LLMInvokeInput` 触发前新增 `TransformLLMIOInput` 调用
- 在 `client.Invoke()` 调用后、`LLMInvokeOutput` 触发前新增 `TransformLLMIOOutput` 调用
- 调整顺序为：transform_io input → emit_before(INVOKE_INPUT) → 调用 → transform_io output → emit_after(INVOKE_OUTPUT)

---

### Task 10: Agent 层 WarpBaseAgent Invoke/Stream 改造

**Files:**
- 修改: `internal/agentcore/single_agent/base.go`

- [ ] **Step 1: 重写 WarpBaseAgent.Invoke() 方法体**

将当前顺序 `emit_before → transform_io占位(输入) → invokeImpl → transform_io占位(输出) → emit_after` 改为：

`transform_io input → emit_before → invokeImpl → transform_io output → emit_after`

```go
func (w *WarpBaseAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...AgentOption) (any, error) {
	// ... invoker nil 检查 ...
	fw := callback.GetCallbackFramework()

	// ① transform_io 输入变换
	_ = fw.TransformAgentIOInput(ctx, callback.AgentInvokeInput, inputs)

	// ② emit_before: 触发 AgentInvokeInput
	fw.TriggerAgent(ctx, &callback.AgentCallEventData{Event: AgentInvokeInput, AgentID: w.card.ID, Inputs: inputs})

	// 执行子类真实逻辑
	result, err := w.invoker.invokeImpl(ctx, inputs, opts...)
	// ... 错误处理 ...

	// ③ transform_io 输出变换
	result = fw.TransformAgentIOOutput(ctx, callback.AgentInvokeOutput, result)

	// ④ emit_after: 触发 AgentInvokeOutput
	fw.TriggerAgent(ctx, &callback.AgentCallEventData{Event: AgentInvokeOutput, AgentID: w.card.ID, Result: result})

	return result, nil
}
```

- [ ] **Step 2: 重写 WarpBaseAgent.Stream() 方法体**

将当前顺序 `emit_before → streamImpl → per-item(transform_io占位 → emit_after)` 改为：

`transform_io input → emit_before → streamImpl → per-item(transform_io output → emit_after)`

```go
func (w *WarpBaseAgent) Stream(ctx context.Context, inputs map[string]any, opts ...AgentOption) (<-chan stream.Schema, error) {
	// ... invoker nil 检查 ...
	fw := callback.GetCallbackFramework()

	// ① transform_io 输入变换
	_ = fw.TransformAgentIOInput(ctx, callback.AgentStreamInput, inputs)

	// ② emit_before
	fw.TriggerAgent(ctx, &callback.AgentCallEventData{Event: AgentStreamInput, AgentID: w.card.ID, Inputs: inputs})

	// 调用子类真实 stream
	ch, err := w.invoker.streamImpl(ctx, inputs, opts...)
	// ... 错误处理 ...

	// 包装 channel：per-item { ③ transform_io 输出变换 → ④ emit_after }
	out := make(chan stream.Schema)
	go func() {
		defer close(out)
		for item := range ch {
			// ③ transform_io 输出变换（per item）
			item = fw.TransformAgentIOOutput(ctx, callback.AgentStreamOutput, item).(stream.Schema)
			// ④ emit_after (per_item)
			fw.TriggerAgent(ctx, &callback.AgentCallEventData{Event: AgentStreamOutput, AgentID: w.card.ID, Result: item})
			out <- item
		}
	}()

	return out, nil
}
```

---

### Task 11: TracedModelClient.Stream() 改造

**Files:**
- 修改: `internal/agentcore/session/tracer/decorator.go:130-172`

- [ ] **Step 1: 修改 TracedModelClient.Stream() 方法签名**

将返回类型从 `(*model_clients.StreamResult, error)` 改为 `(<-chan *llmschema.AssistantMessageChunk, error)`。

- [ ] **Step 2: 重写方法体——逐 chunk 透传 + 异步 TraceLLMEnd**

```go
func (c *TracedModelClient) Stream(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	span := c.tracer.AgentSpanManager.CreateAgentSpan(c.agentSpan)
	c.tracer.TriggerAgent(ctx, TraceLLMStart, &TriggerParams{
		Span:         &span.Span,
		Inputs:       messages,
		InstanceInfo: c.instanceInfo,
	})

	// 注入 tracer_record_data 回调（对齐 Python: call_kwargs["tracer_record_data"] = tracer_record_data）
	spanPtr := &span.Span
	tracerRecordData := func(data map[string]any) {
		c.tracer.TriggerAgent(ctx, TraceLLMRequest, &TriggerParams{
			Span:         spanPtr,
			OnInvokeData: data,
		})
	}
	opts = append(opts, model_clients.WithStreamTracerRecordData(tracerRecordData))

	chunkChan, err := c.inner.Stream(ctx, messages, opts...)
	if err != nil {
		c.tracer.TriggerAgent(ctx, TraceLLMError, &TriggerParams{
			Span:  &span.Span,
			Error: err,
		})
		return nil, err
	}

	// 逐 chunk 透传，流结束后触发 TraceLLMEnd（对齐 Python _make_trace_stream_wrap_handler）
	out := make(chan *llmschema.AssistantMessageChunk)
	go func() {
		defer close(out)
		for chunk := range chunkChan {
			out <- chunk
		}
		// 流结束，触发 TraceLLMEnd
		c.tracer.TriggerAgent(ctx, TraceLLMEnd, &TriggerParams{
			Span: &span.Span,
		})
	}()

	return out, nil
}
```

关键变化：
- 不再调 `Final()` 阻塞
- final_message 通过 tracer_record_data(llm_response=finalMessage) 从 OpenAI 客户端 goroutine 内部传出，在 TraceLLMRequest 中记录
- 逐 chunk 透传给调用方

---

### Task 12: 所有受影响测试文件适配

**Files:**
- 修改: `model_clients/openai/client_test.go`
- 修改: `model_clients/deepseek/client_test.go`
- 修改: `model_clients/siliconflow/client_test.go`
- 修改: `model_clients/inference_affinity/client_test.go`
- 修改: `model_clients/intellirouter/client_test.go`
- 修改: `model_clients/registry_test.go`
- 修改: `llm/model_test.go`
- 修改: `llm/output_parsers/json_output_parser_test.go`
- 修改: `session/tracer/decorator_test.go`
- 修改: `context_engine/processor/compressor/current_round_compressor_test.go`
- 修改: `context_engine/processor/compressor/round_level_compressor_test.go`
- 修改: `context_engine/processor/compressor/full_compact_processor_test.go`
- 修改: `context_engine/processor/offloader/message_summary_offloader_test.go`
- 修改: `single_agent/base_test.go`

- [ ] **Step 1: 全局替换 StreamResult 引用模式**

在每个测试文件中，将以下模式替换：

**旧模式（StreamResult）：**
```go
result, err := client.Stream(ctx, messages, opts...)
// 消费 chunk
for chunk := range result.Chunks { ... }
// 或等 final
final := result.Final()
```

**新模式（纯 channel）：**
```go
chunkChan, err := client.Stream(ctx, messages, opts...)
// 消费 chunk
for chunk := range chunkChan { ... }
// final_message 需要自行累积（如果测试需要）
var final *llmschema.AssistantMessageChunk
for chunk := range chunkChan {
    if final == nil { final = chunk } else { final = final.Merge(chunk) }
}
```

- [ ] **Step 2: 更新 registry_test.go 中的 mock 客户端**

mock 客户端的 Stream 方法返回类型从 `(*StreamResult, error)` 改为 `(<-chan *llmschema.AssistantMessageChunk, error)`。

- [ ] **Step 3: 更新 model_test.go**

Model.Stream() 返回纯 channel 后，测试中：
- `result, err := model.Stream(...)` → `chunkChan, err := model.Stream(...)`
- 删除 `result.Final()` 调用
- 改用 `range chunkChan` 消费
- 测试 per_item 回调：验证每个 chunk 触发一次 LLMStreamOutput

- [ ] **Step 4: 更新 tracer/decorator_test.go**

TracedModelClient.Stream() 不再返回 StreamResult：
- 删除 `streamResult.Final()` 相关测试
- 验证逐 chunk 透传行为
- 验证流结束后触发 TraceLLMEnd

---

### Task 13: doc.go 更新

**Files:**
- 修改: `model_clients/doc.go`
- 修改: `llm/output_parsers/doc.go`（如有 StreamResult 引用的示例代码）

- [ ] **Step 1: 更新 model_clients/doc.go**

- 移除 `stream_result.go` 条目
- 更新 `base_client.go` 的说明：Stream 返回纯 chunk channel
- 更新包功能概述中 StreamResult 相关描述

- [ ] **Step 2: 更新 output_parsers/doc.go**

如果文档示例中有 `result.Chunks` 或 `result.Final()` 的使用方式，更新为 `range chunkChan` 模式。

---

### Task 14: 编译验证与全量测试

- [ ] **Step 1: 编译检查**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

修复所有编译错误。

- [ ] **Step 2: 运行全量测试**

```bash
go test ./internal/agentcore/... -count=1
```

- [ ] **Step 3: 检查覆盖率**

```bash
go test -cover ./internal/agentcore/foundation/llm/... ./internal/agentcore/runner/callback/... ./internal/agentcore/single_agent/... ./internal/agentcore/session/tracer/...
```

确保覆盖率 ≥ 85%。

- [ ] **Step 4: 最终提交**

```
feat(llm): 移除 StreamResult，Stream 回调架构对齐 Python

- 删除 StreamResult 类型，BaseModelClient.Stream() 返回纯 chunk channel
- OpenAI/SiliconFlow/InferenceAffinity goroutine 内部累积 final_message + tracer_record_data(llm_response)
- Model.Stream() 对齐 Python 装饰器链：transform_io input → emit_before → 调用 → per-item(transform_io output → emit_after)
- Model.Invoke() 加入 transform_io 占位，调整 emit 顺序
- WarpBaseAgent Invoke/Stream 同步对齐装饰器链顺序
- TracedModelClient.Stream() 逐 chunk 透传 + 异步 TraceLLMEnd
- CallbackFramework 新增 TransformLLMIO/TransformAgentIO 接口（默认透传，6.24 回填）
- SiliconFlow/InferenceAffinity 补齐 LLM_RESPONSE_RECEIVED 回调
```
