# StreamResult 移除与 Stream 回调架构对齐设计

## 问题背景

Go 当前 LLM 层的 `StreamResult` 存在两个核心问题：

1. **channel 竞争**：`NewStreamResult` 内部 goroutine 消费 `Chunks` channel 做 Merge 合并，调用方如果也 `range result.Chunks` 读取，两者竞争同一个 channel，每个 chunk 只被一方读到
2. **回调层次错误**：chunk 合并和 `LLMStreamOutput` 回调放错了层次。Python 中 `LLMStreamOutput` 是 Model 层 per_item 触发（每个 chunk 一次），Go 中仅流结束后触发一次；Python 的 final_message 累积在 OpenAI 客户端 generator 内部，Go 放在了独立的 StreamResult 结构体中

## 设计决策

| 决策项 | 结论 |
|--------|------|
| 接口变更范围 | 一步到位全部改 |
| chunk 合并位置 | OpenAI 客户端 goroutine 内部（对齐 Python） |
| LLMStreamOutput 语义 | per_item 触发（对齐 Python emit_after 装饰器） |
| transform_io | 一起实现回调接口，具体实现 6.24 回填 |
| TracedModelClient.Stream() | 逐 chunk 透传，流结束后异步触发 TraceLLMEnd |
| StreamResult | 删除 |
| final_message 对外 | 只通过 tracer_record_data 传出（对齐 Python） |
| IntelliRouter | 暂不修改 |
| SiliconFlow/InferenceAffinity | 对齐 OpenAI 完整回调（加 LLM_RESPONSE_RECEIVED + final_message + tracer(llm_response)） |

## 第一节：接口变更

### 1.1 BaseModelClient.Stream() 返回类型变更

**Before:**
```go
Stream(ctx context.Context, messages MessagesParam, opts ...StreamOption) (*StreamResult, error)
```

**After:**
```go
Stream(ctx context.Context, messages MessagesParam, opts ...StreamOption) (<-chan *llmschema.AssistantMessageChunk, error)
```

### 1.2 删除 StreamResult

- 删除 `stream_result.go` 和 `stream_result_test.go`
- 从 `doc.go` 中移除 `stream_result.go` 条目

### 1.3 各客户端改动

| 客户端 | 改动方式 |
|--------|---------|
| **OpenAI** | Stream 返回 `(<-chan, error)`，goroutine 内新增 final_message 累积 + 流结束时 `tracer_record_data(llm_response=final_message)` |
| **DeepSeek** | 委托 OpenAI，返回类型跟着改 |
| **DashScope** | 嵌入 OpenAI 复用，无额外改动 |
| **SiliconFlow** | Stream 返回 `(<-chan, error)`，对齐 OpenAI：加 final_message 累积 + tracer(llm_response) + LLM_RESPONSE_RECEIVED |
| **InferenceAffinity** | 同 SiliconFlow |
| **IntelliRouter** | Stream 返回类型跟着改（否则编译不过），但不补齐回调/final_message |

## 第二节：OpenAI 客户端 goroutine 内部改造

### 2.1 Stream 返回类型改为纯 chunk channel

```go
func (c *OpenAIModelClient) Stream(
    ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error)
```

### 2.2 SSE 消费 goroutine 内部新增 final_message 累积

```
sseReader.ReadEvent()
  → ParseStreamChunk()
  → OutputParser
  → LLMResponseReceived 回调
  → final_message = final_message.Merge(chunk)   ← 新增：每个 chunk 累积
  → chunkChan <- chunk

流结束（io.EOF）时:
  → tracer_record_data(llm_response=final_message)  ← 新增：传出合并结果
  → LLMOutput 回调（已有）
  → close(chunkChan)
```

关键点：
- `final_message` 累积在 goroutine 内部，不干扰 chunkChan 的外部消费
- `tracer_record_data(llm_response=final_message)` 在流结束时调用，和 Python 对齐
- `chunkChan` 仍然逐个发送原始 chunk，调用方可以 `range chunkChan` 自由消费
- 不再创建 `NewStreamResult(chunkChan)`

## 第三节：Model 层改造——对齐 Python 装饰器链

### 3.1 装饰器链执行顺序

Python 装饰器从内到外包装，从外到内执行。完整执行顺序：

**输入侧：** ① transform_io input → ② emit_before(INPUT)
**调用原始函数**
**输出侧（per item）：** ③ transform_io output → ④ emit_after(OUTPUT)

两侧对称：先变换再通知。

### 3.2 Model.Stream() 改造

```go
func (m *Model) Stream(...) (<-chan *llmschema.AssistantMessageChunk, error) {
    // ① transform_io 输入变换
    // messages = fw.TransformLLMIOInput(ctx, LLMStreamInput, messages)

    // ② emit_before: 触发 LLMStreamInput
    fw.TriggerLLM(ctx, &LLMCallEventData{Event: LLMStreamInput, ...})

    // 调用底层客户端
    chunkChan, err := m.client.Stream(ctx, messages, opts...)

    // 包装 channel：per-item { ③ transform_io 输出变换 → ④ emit_after }
    out := make(chan *llmschema.AssistantMessageChunk)
    go func() {
        defer close(out)
        for chunk := range chunkChan {
            // ③ transform_io 输出变换（per item）
            chunk = fw.TransformLLMIOOutput(ctx, LLMStreamOutput, chunk)
            // ④ emit_after (per_item): 每 chunk 触发 LLMStreamOutput
            fw.TriggerLLM(ctx, &LLMCallEventData{
                Event: LLMStreamOutput, Response: chunk, ...
            })
            out <- chunk
        }
    }()
    return out, nil
}
```

### 3.3 Model.Invoke() 改造

```go
func (m *Model) Invoke(...) (*llmschema.AssistantMessage, error) {
    // ① transform_io 输入变换
    // messages = fw.TransformLLMIOInput(ctx, LLMInvokeInput, messages)

    // ② emit_before: 触发 LLMInvokeInput
    fw.TriggerLLM(ctx, &LLMCallEventData{Event: LLMInvokeInput, ...})

    // 调用底层客户端
    result, err := m.client.Invoke(ctx, messages, opts...)

    // ③ transform_io 输出变换
    result = fw.TransformLLMIOOutput(ctx, LLMInvokeOutput, result)

    // ④ emit_after: 触发 LLMInvokeOutput
    fw.TriggerLLM(ctx, &LLMCallEventData{Event: LLMInvokeOutput, Response: result, ...})
    return result, nil
}
```

## 第四节：CallbackFramework — TransformIO 回调接口

### 4.1 变换回调函数类型

```go
// TransformLLMIOInputFunc LLM 层输入变换回调函数类型。
// 对齐 Python: transform_io 的 input_fn（LLM_STREAM_INPUT / LLM_INVOKE_INPUT）
type TransformLLMIOInputFunc func(ctx context.Context, event LLMCallEvent, input any) any

// TransformLLMIOOutputFunc LLM 层输出变换回调函数类型。
// 对齐 Python: transform_io 的 output_fn（LLM_STREAM_OUTPUT / LLM_INVOKE_OUTPUT）
type TransformLLMIOOutputFunc func(ctx context.Context, event LLMCallEvent, output any) any

// TransformAgentIOInputFunc Agent 层输入变换回调函数类型。
// 对齐 Python: transform_io 的 input_fn（AGENT_STREAM_INPUT / AGENT_INVOKE_INPUT）
type TransformAgentIOInputFunc func(ctx context.Context, event AgentCallEvent, input any) any

// TransformAgentIOOutputFunc Agent 层输出变换回调函数类型。
// 对齐 Python: transform_io 的 output_fn（AGENT_STREAM_OUTPUT / AGENT_INVOKE_OUTPUT）
type TransformAgentIOOutputFunc func(ctx context.Context, event AgentCallEvent, output any) any
```

### 4.2 CallbackFramework 新增方法

```go
// RegisterLLMTransformIO 注册 LLM 层 IO 变换回调。
func (fw *CallbackFramework) RegisterLLMTransformIO(
    inputEvent LLMCallEvent, outputEvent LLMCallEvent,
    inputFn TransformLLMIOInputFunc, outputFn TransformLLMIOOutputFunc,
)

// TransformLLMIOInput 应用 LLM 层输入变换。
func (fw *CallbackFramework) TransformLLMIOInput(ctx context.Context, event LLMCallEvent, input any) any

// TransformLLMIOOutput 应用 LLM 层输出变换。
func (fw *CallbackFramework) TransformLLMIOOutput(ctx context.Context, event LLMCallEvent, output any) any

// RegisterAgentTransformIO 注册 Agent 层 IO 变换回调。
func (fw *CallbackFramework) RegisterAgentTransformIO(
    inputEvent AgentCallEvent, outputEvent AgentCallEvent,
    inputFn TransformAgentIOInputFunc, outputFn TransformAgentIOOutputFunc,
)

// TransformAgentIOInput 应用 Agent 层输入变换。
func (fw *CallbackFramework) TransformAgentIOInput(ctx context.Context, event AgentCallEvent, input any) any

// TransformAgentIOOutput 应用 Agent 层输出变换。
func (fw *CallbackFramework) TransformAgentIOOutput(ctx context.Context, event AgentCallEvent, output any) any
```

### 4.3 默认行为：透传

当前没有实际 transform_io 实现需求，所有方法默认透传（identity function）。具体实现在 6.24 回填。

## 第五节：TracedModelClient.Stream() 改造

**Before：** 注入 tracer_record_data → `inner.Stream()` → `streamResult.Final()` 阻塞 → 触发 `TraceLLMEnd`

**After：** 对齐 Python——注入 tracer_record_data → `inner.Stream()` → 逐 chunk 透传 → 流结束后异步触发 `TraceLLMEnd`

```go
func (c *TracedModelClient) Stream(
    ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
    span := c.tracer.AgentSpanManager.CreateAgentSpan(c.agentSpan)
    c.tracer.TriggerAgent(ctx, TraceLLMStart, &TriggerParams{
        Span: &span.Span, Inputs: messages, InstanceInfo: c.instanceInfo,
    })

    // 注入 tracer_record_data 回调
    spanPtr := &span.Span
    tracerRecordData := func(data map[string]any) {
        c.tracer.TriggerAgent(ctx, TraceLLMRequest, &TriggerParams{
            Span: spanPtr, OnInvokeData: data,
        })
    }
    opts = append(opts, model_clients.WithStreamTracerRecordData(tracerRecordData))

    chunkChan, err := c.inner.Stream(ctx, messages, opts...)
    if err != nil {
        c.tracer.TriggerAgent(ctx, TraceLLMError, &TriggerParams{Span: &span.Span, Error: err})
        return nil, err
    }

    // 逐 chunk 透传，流结束后触发 TraceLLMEnd
    out := make(chan *llmschema.AssistantMessageChunk)
    go func() {
        defer close(out)
        for chunk := range chunkChan {
            out <- chunk
        }
        // 流结束，触发 TraceLLMEnd
        c.tracer.TriggerAgent(ctx, TraceLLMEnd, &TriggerParams{Span: &span.Span})
    }()

    return out, nil
}
```

关键变化：
- 不再调 `Final()` 阻塞
- `final_message` 通过 `tracer_record_data(llm_response=final_message)` 从 OpenAI 客户端 goroutine 内部传出
- 逐 chunk 透传给调用方，流结束后异步触发 `TraceLLMEnd`

## 第六节：Agent 层 WarpBaseAgent 改造

### 6.1 WarpBaseAgent.Stream() 改造

```go
func (w *WarpBaseAgent) Stream(...) (<-chan stream.Schema, error) {
    // ① transform_io 输入变换
    // inputs = fw.TransformAgentIOInput(ctx, AgentStreamInput, inputs)

    // ② emit_before: 触发 AgentStreamInput
    fw.TriggerAgent(ctx, &AgentCallEventData{Event: AgentStreamInput, ...})

    // 调用子类真实 stream
    ch, err := w.invoker.streamImpl(ctx, inputs, opts...)

    // 包装 channel：per-item { ③ transform_io 输出变换 → ④ emit_after }
    out := make(chan stream.Schema)
    go func() {
        defer close(out)
        for item := range ch {
            // ③ transform_io 输出变换（per item）
            item = fw.TransformAgentIOOutput(ctx, AgentStreamOutput, item)
            // ④ emit_after (per_item)
            fw.TriggerAgent(ctx, &AgentCallEventData{Event: AgentStreamOutput, Result: item})
            out <- item
        }
    }()
    return out, nil
}
```

### 6.2 WarpBaseAgent.Invoke() 改造

```go
func (w *WarpBaseAgent) Invoke(...) (any, error) {
    // ① transform_io 输入变换
    // inputs = fw.TransformAgentIOInput(ctx, AgentInvokeInput, inputs)

    // ② emit_before: 触发 AgentInvokeInput
    fw.TriggerAgent(ctx, &AgentCallEventData{Event: AgentInvokeInput, ...})

    // 调用子类真实逻辑
    result, err := w.invoker.invokeImpl(ctx, inputs, opts...)

    // ③ transform_io 输出变换
    result = fw.TransformAgentIOOutput(ctx, AgentInvokeOutput, result)

    // ④ emit_after: 触发 AgentInvokeOutput
    fw.TriggerAgent(ctx, &AgentCallEventData{Event: AgentInvokeOutput, Result: result})
    return result, nil
}
```

## 第七节：受影响文件清单与测试策略

### 7.1 受影响文件清单

| 文件 | 改动类型 |
|------|---------|
| `model_clients/stream_result.go` | **删除** |
| `model_clients/stream_result_test.go` | **删除** |
| `model_clients/base_client.go` | 改 `BaseModelClient.Stream()` 返回类型 |
| `model_clients/doc.go` | 移除 `stream_result.go` 条目，更新说明 |
| `model_clients/openai/client.go` | Stream 返回纯 channel，goroutine 内加 final_message 累积 + tracer(llm_response) |
| `model_clients/openai/client_test.go` | 适配新返回类型 |
| `model_clients/deepseek/client.go` | Stream 返回类型跟着改 |
| `model_clients/deepseek/client_test.go` | 适配新返回类型 |
| `model_clients/siliconflow/client.go` | Stream 返回纯 channel，加 final_message + tracer(llm_response) + LLM_RESPONSE_RECEIVED |
| `model_clients/siliconflow/client_test.go` | 适配新返回类型 |
| `model_clients/inference_affinity/client.go` | 同 SiliconFlow |
| `model_clients/inference_affinity/client_test.go` | 适配新返回类型 |
| `model_clients/intellirouter/client.go` | Stream 返回类型跟着改，不补齐回调 |
| `model_clients/intellirouter/client_test.go` | 适配新返回类型 |
| `model_clients/registry_test.go` | mock 客户端适配新接口 |
| `llm/model.go` | Stream per_item 包装 + transform_io；Invoke 加 transform_io |
| `llm/model_test.go` | 适配新返回类型 |
| `llm/output_parsers/json_output_parser_test.go` | 适配 |
| `llm/output_parsers/doc.go` | 更新示例代码 |
| `runner/callback/framework.go` | 新增 TransformLLMIO* / TransformAgentIO* 接口 + 透传实现 |
| `runner/callback/framework_test.go` | 测试新增接口的透传行为 |
| `single_agent/base.go` | Invoke/Stream 加入 transform_io 占位，调整 emit_before/after 顺序 |
| `single_agent/base_test.go` | 适配 |
| `session/tracer/decorator.go` | Stream 改为逐 chunk 透传 + 异步 TraceLLMEnd |
| `session/tracer/decorator_test.go` | 适配新返回类型 |
| `context_engine/processor/compressor/*_test.go` | 适配 |
| `context_engine/processor/offloader/*_test.go` | 适配 |

### 7.2 IntelliRouter

返回类型必须跟着改（否则编译不过），但不补齐回调/final_message（对齐 Python IntelliRouter 本身也缺失）。

### 7.3 测试策略

1. **单元测试**：所有受影响的 `*_test.go` 适配新返回类型，确保 `go test ./...` 通过
2. **TransformIO 透传测试**：验证 `TransformLLMIOInput/Output` 和 `TransformAgentIOInput/Output` 默认透传
3. **per_item 回调测试**：验证 Model.Stream() 每个 chunk 触发一次 `LLMStreamOutput`
4. **Tracer 非阻塞测试**：验证 TracedModelClient.Stream() 不阻塞，逐 chunk 透传
5. **final_message 累积测试**：验证 OpenAI/SiliconFlow/InferenceAffinity goroutine 内部 final_message 正确累积
6. **覆盖率**：改动后整体覆盖率仍 ≥ 85%
