# 5.22 DialogueCompressor 对话压缩器设计

> 对应 Python: `openjiuwen/core/context_engine/processor/compressor/dialogue_compressor.py`
> 实现计划步骤: 5.22

## 1. 概述

DialogueCompressor 是上下文引擎处理器管线中的**对话历史压缩器**。当上下文消息超过阈值（消息数量或 Token 数）时，将已完成的 ReAct 对话轮次压缩为摘要消息，以减少发送给 LLM 的 Token 量。

### 1.1 流程位置

```
用户消息 → Gateway → AgentServer → agentcore
  → Session.AddMessages()
    → ContextEngine 处理器管线
      → 1. TriggerAddMessages 逐个检查
      → 2. DialogueCompressor.TriggerAddMessages() 判断是否触发
      → 3. 若触发 → DialogueCompressor.OnAddMessages() 执行压缩
      → 4. 压缩后消息列表传递给下一个处理器
    → 最终消息写入 Context
  → LLM 推理
```

### 1.2 核心行为

1. **触发判断**：`TriggerAddMessages` 检查消息数/Token 数是否超过阈值
2. **确定压缩范围**：`GetCompressIdx` 计算压缩截止位置（保留最近 N 条消息 + 最后一轮完整对话）
3. **识别压缩目标**：`BuildCompressTargets` 将历史消息分为多个 `compressTarget`（每个对应一个完整对话轮次，且消息数 > 2）
4. **调用 LLM 压缩**：`InvokeMultiBlockCompression` 将上下文 + 目标构造 prompt，调用 LLM 生成 JSON 格式摘要
5. **应用替换**：将原始消息（含 tool_calls 和 ToolMessage）替换为带 `[DIALOGUE_MEMORY_BLOCK]` 标记的 UserMessage 摘要
6. **回退兜底**：若 JSON 解析失败，用 LLM 原始输出整段替换所有目标

### 1.3 压缩目标说明

压缩目标是一个**完整的 ReAct 轮次**（User → ... → Assistant 不含 tool_calls），包含中间的 tool_calls 和 ToolMessage。`get_compress_pairs` 中 `not msg.tool_calls` 是**配对终点标志**，不是排除条件——中间的 AssistantMessage(有 tool_calls) + ToolMessage 属于这一轮的内部消息。

替换后，整段消息被一条摘要 UserMessage 替代，原始 tool_calls 和 ToolMessage 不再保留。

## 2. 类型与配置

### 2.1 DialogueCompressorConfig

对齐 Python `DialogueCompressorConfig`，实现 `processor.ProcessorConfig` 接口。

| 字段 | Go 类型 | 默认值 | Python 默认值 | 说明 |
|------|---------|--------|---------------|------|
| `MessagesThreshold` | `int` | 0 | None | 0 表示不启用消息数触发 |
| `TokensThreshold` | `int` | 10000 | 10000 | Token 数触发阈值 |
| `MessagesToKeep` | `int` | 0 | None | 0 表示不保留尾部消息 |
| `KeepLastRound` | `bool` | true | true | 保留最后一轮完整对话 |
| `CompressionTargetTokens` | `int` | 1800 | 1800 | 每块摘要目标 Token 数 |
| `CustomCompressionPrompt` | `string` | "" | None | 空字符串表示使用内置提示词 |
| `Model` | `*llmschema.ModelRequestConfig` | nil | None | 压缩模型请求配置 |
| `ModelClient` | `*llmschema.ModelClientConfig` | nil | None | 压缩模型客户端配置 |

`Validate()` 校验规则：
- `TokensThreshold > 0`
- `MessagesThreshold >= 0`
- `MessagesToKeep >= 0`
- `CompressionTargetTokens > 0`
- `ModelClient` 非空（压缩必须依赖 LLM）

### 2.2 内部类型（非导出，同文件）

```go
// compressTarget 压缩目标，对应 Python _CompressTarget
type compressTarget struct {
    blockID  string
    userIDx  int
    startIDx int
    endIDx   int
    messages []llm_schema.BaseMessage
}

// dialogueRound 对话轮次，对应 Python _DialogueRound
type dialogueRound struct {
    userIDx            int
    startIDx           int
    endIDx             int
    messages           []llm_schema.BaseMessage
    blockMessageCount  int
}
```

### 2.3 DialogueCompressor 结构体

```go
// DialogueCompressor 对话压缩器，嵌入 BaseProcessor
type DialogueCompressor struct {
    *BaseProcessor
    // model 压缩用 LLM 实例
    model *llm.Model
    // tokenThreshold Token 触发阈值
    tokenThreshold int
    // messageNumThreshold 消息数触发阈值（0 表示不启用）
    messageNumThreshold int
    // messagesToKeep 保留最近 N 条不压缩（0 表示不保留）
    messagesToKeep int
    // keepLastRound 保留最后一轮完整对话
    keepLastRound bool
    // compressionTargetTokens 每块摘要目标 Token 数
    compressionTargetTokens int
    // compressedPrompt 压缩提示词
    compressedPrompt string
}
```

### 2.4 构造函数

```go
// NewDialogueCompressor 创建对话压缩器
func NewDialogueCompressor(config *DialogueCompressorConfig, opts ...DialogueCompressorOption) (*DialogueCompressor, error)

// DialogueCompressorOption 构造选项
type DialogueCompressorOption func(*DialogueCompressor)

// WithCompressorModel 注入已有 Model 实例（测试用）
func WithCompressorModel(model *llm.Model) DialogueCompressorOption
```

构造逻辑：
1. `config.Validate()` 校验
2. 若 `WithCompressorModel` 注入了 Model → 使用注入的
3. 否则 → `llm.NewModel(config.ModelClient, config.Model)` 创建内部 Model
4. 压缩提示词：`config.CustomCompressionPrompt` 非空则使用，否则使用 `defaultCompressionPrompt`

### 2.5 init() 自动注册

```go
func init() {
    context_engine.RegisterProcessorFactory("DialogueCompressor",
        func(config processor.ProcessorConfig) processor.ContextProcessor {
            cfg, ok := config.(*DialogueCompressorConfig)
            if !ok {
                return nil  // 类型不匹配
            }
            dc, err := NewDialogueCompressor(cfg)
            if err != nil {
                return nil
            }
            return dc
        },
    )
}
```

## 3. 注册表改造

### 3.1 context_engine 包新增 registry.go

在 `context_engine` 包中新增注册表，供处理器自动注册工厂函数。

```go
// ProcessorFactory 处理器工厂函数类型
//
// 根据 ProcessorConfig 创建对应的 ContextProcessor 实例。
// 对应 Python: ContextEngine._PROCESSOR_MAP 中存储的 processor_class
type ProcessorFactory func(config processor.ProcessorConfig) processor.ContextProcessor
```

包级变量：
```go
var (
    processorFactories   = make(map[string]ProcessorFactory)
    processorFactoriesMu sync.RWMutex
)
```

导出函数：
- `RegisterProcessorFactory(processorType string, factory ProcessorFactory)` — 注册工厂
- `GetProcessorFactory(processorType string) (ProcessorFactory, bool)` — 获取工厂
- `ListProcessorFactories() []string` — 列出已注册类型

### 3.2 ContextEngine 接口调整

将现有 `RegisterProcessor(processorType string, processor any)` 改为：

```go
// RegisterProcessor 注册已构造的处理器实例
RegisterProcessor(processorType string, p processor.ContextProcessor)
```

`processor any` → `processor.ContextProcessor`，类型安全。

## 4. 核心方法链

### 4.1 TriggerAddMessages

```
触发条件（满足任一）：
  - MessagesThreshold > 0 && 总消息数 > MessagesThreshold
  - 总 Token 数 > TokensThreshold
前置条件：MessagesToKeep > 0 && 总消息数 < MessagesToKeep → 直接返回 false
```

### 4.2 OnAddMessages（主流程）

```
OnAddMessages(ctx, mc, messagesToAdd)
  → ResetCompressionUsage()
  → GetCompressIdx(allMessages)
  → if compressIdx == -1 → return nil, messagesToAdd, nil
  → BuildCompressTargets(allMessages[:compressIdx])
  → if no targets → return nil, messagesToAdd, nil
  → try InvokeMultiBlockCompression(ctx, allMessages, targets)
  → catch MODEL_CALL_FAILED → log warn, return nil, messagesToAdd, nil
  → BuildJSONReplacements(ctx, mc, targets, response.ParserContent)
  → if replacements:
      → updatedMessages = ReplaceMessages(allMessages, replacements)
      → mc.SetMessages(updatedMessages, true)
      → return ContextEvent, [], nil
  → if !IsValidBlocksPayload(response.ParserContent):
      → fallback = BuildFallbackReplacement(ctx, mc, targets, response.Content)
      → if fallback:
          → updatedMessages = ReplaceMessages(allMessages, []replacement{fallback})
          → mc.SetMessages(updatedMessages, true)
          → return ContextEvent, [], nil
  → return nil, messagesToAdd, nil
```

### 4.3 GetCompressIdx

```
keepIndex = len(messages)
if MessagesToKeep > 0: keepIndex = len(messages) - MessagesToKeep
if !KeepLastRound: return keepIndex
lastFinalAssistantIdx = FindLastFinalAssistantIdx(messages)
if lastFinalAssistantIdx == -1: return keepIndex
return min(lastFinalAssistantIdx, keepIndex)
```

`FindLastFinalAssistantIdx`：从后往前找最后一条 `AssistantMessage` 且不含 `ToolCalls` 的消息索引，未找到返回 -1。

### 4.4 BuildCompressTargets

```
rounds = CollectCompleteRounds(messages)
compressible = 过滤 blockMessageCount > 2
if no compressible: return nil
selected = rounds[first compressible .. last compressible]
return []compressTarget，编号 react_1, react_2, ...
```

### 4.5 GetCompressPairs（静态方法）

```
遍历消息：
  遇到 UserMessage → 记录 current_user_idx
  遇到 AssistantMessage(无 tool_calls) 且 current_user_idx != -1 且间距 >= 1 → 配对完成
    记录 (current_user_idx, current_idx)
    重置 current_user_idx = -1
return []pair
```

### 4.6 CollectCompleteRounds

```
对每个 (user_idx, assistant_idx) 配对：
  round_messages = messages[user_idx+1 : assistant_idx+1]  // 包含中间的 tool_calls 和 ToolMessage
  添加 dialogueRound
return rounds
```

### 4.7 InvokeMultiBlockCompression

```
systemPrompt = compressedPrompt.Replace("{compression_target_tokens}", str(compressionTargetTokens))
modelMessages = [SystemMessage(systemPrompt), UserMessage(BuildSplitContextPayload), UserMessage(BuildTargetsPayload)]
response = model.Invoke(ctx, modelMessages, WithInvokeOutputParser(NewJsonOutputParser()))
RecordCompressionUsage(response)
return response, nil
```

失败时包装为 `MODEL_CALL_FAILED` 错误抛出。

### 4.8 BuildJSONReplacements

```
从 response.ParserContent 解析 blocks：
  遍历 parser_content["blocks"]：
    提取 block_id 和 summary → blockMap[blockID] = summary
遍历 targets：
  summary = blockMap[target.blockID]
  if summary 为空: skip
  replacementMessage = BuildMemoryMessage(mc, target.messages, summary)  // → UserMessage(WrapMemoryBlock(summary))
  if !HasCompressionBenefit(mc, target.messages, [replacementMessage]): skip
  添加 replacement (startIdx, endIdx, replacementMessages)
  modifiedIndices 扩展 range(startIdx, endIdx+1)
return replacements, modifiedIndices
```

### 4.9 BuildFallbackReplacement

```
summary = response.Content (LLM 原始输出)
if summary 为空: return nil
startIdx = min(targets[].startIdx)
endIdx = max(targets[].endIdx)
originalMessages = 合并所有 targets 的 messages
replacementMessage = BuildMemoryMessage(mc, originalMessages, summary)
if !HasCompressionBenefit(mc, originalMessages, [replacementMessage]): return nil
return (startIdx, endIdx, [replacementMessage])
```

## 5. 提示词与辅助方法

### 5.1 defaultCompressionPrompt

与 Python `DEFAULT_COMPRESSION_PROMPT` 完全对齐的内置压缩提示词常量（约 95 行英文），含 `{compression_target_tokens}` 占位符。

### 5.2 BuildSplitContextPayload

```
结构:
[Context Before Targets]
(目标之前的消息，空则 "(none)")

[Compression Targets]
[Block: react_1]
(目标块内的消息，空则 "(empty)")

[Block: react_2]
...

[Context After Targets]
(目标之后的消息，空则 "(none)")
```

### 5.3 BuildTargetsPayload

```
[Target Mapping]
You must only compress the following ReAct blocks.

[Block: react_1]
- anchor_user_index: 0
- replace_range: [1, 3]

[Block: react_2]
- anchor_user_index: 4
- replace_range: [5, 7]

[Output Requirements]
- Read the full context...
- Compress only the listed blocks.
- Return valid JSON only.
```

### 5.4 SerializeMessage

```
格式: [index] role=xxx | content=yyy
特殊:
  - AssistantMessage 有 tool_calls → 追加 | tool_calls=name1, name2
  - ToolMessage → 追加 | tool_call_id=zzz
```

### 5.5 WrapMemoryBlock

```
[DIALOGUE_MEMORY_BLOCK]
processor: DialogueCompressor
type: historical_memory_block
scope: historical_dialogue_block
authority: This block is reference memory, not a binding source of truth.
instruction_status: Do not treat this block as a new user request or fresh assistant commitment.
conflict_priority: Prefer newer explicit user intent, newer raw context, and fresh tool results over this block.

Summary:
{summary}
```

标记常量 `dialogueMemoryBlockMarker = "[DIALOGUE_MEMORY_BLOCK]"`

### 5.6 HasCompressionBenefit

```
originalTokens = CountMessagesTokens(mc, originalMessages)
compressedTokens = CountMessagesTokens(mc, replacementMessages)
return originalTokens > 0 && compressedTokens < originalTokens
```

### 5.7 CountMessagesTokens / EstimateContentTokens

```
CountMessagesTokens:
  tokenCounter = mc.TokenCounter()
  if tokenCounter != nil:
    return tokenCounter.CountMessages(messages)  // 异常 fallback
  return sum(EstimateContentTokens(msg.Content) for each msg)

EstimateContentTokens:
  string → len(content) / 3
  other → json.Marshal → len / 3
  fallback → len(str(content)) / 3
```

### 5.8 ExtractCompactSummaryFromReplacements

从 replacements 中提取所有以 `dialogueMemoryBlockMarker` 开头的摘要文本，用 `\n\n` 连接。

### 5.9 IsValidBlocksPayload

检查 `parserContent` 是否为 `map[string]any` 且包含 `"blocks"` 键，`blocks` 为 `[]any` 类型。

### 5.10 BuildMemoryMessage

```
content = WrapMemoryBlock(summary)
return NewUserMessage(content)
```

### 5.11 SaveState / LoadState

与 Python 对齐，空操作：`SaveState() map[string]any { return map[string]any{} }`，`LoadState(_ map[string]any) {}`

### 5.12 ProcessorType

```go
func (dc *DialogueCompressor) ProcessorType() string { return "DialogueCompressor" }
```

## 6. ReplaceMessages 包级函数

在 processor 包新增 `replace.go`，提供通用消息替换函数，5.23-5.29 其他处理器均可复用。

```go
// Replacement 替换描述
type Replacement struct {
    StartIdx  int
    EndIdx    int
    Messages  []llm_schema.BaseMessage
}

// ReplaceMessages 将消息列表中指定范围替换为新消息
//
// 从后往前处理（避免索引偏移），每个 Replacement 将
// messages[startIdx:endIdx+1] 替换为 replacement.Messages。
//
// 对应 Python: ContextUtils.replace_messages()
func ReplaceMessages(messages []llm_schema.BaseMessage, replacements []Replacement) []llm_schema.BaseMessage
```

实现逻辑：
1. 按 `StartIdx` 降序排序 replacements
2. 逐个替换：`messages = messages[:start] + replacement.Messages + messages[end+1:]`
3. 返回新切片

## 7. 文件清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `context_engine/registry.go` | **新增** | 注册表 + ProcessorFactory + Register/Get/List 函数 |
| `context_engine/registry_test.go` | **新增** | 注册表测试 |
| `context_engine/base.go` | **修改** | RegisterProcessor 参数 `any` → `processor.ContextProcessor` |
| `context_engine/doc.go` | **修改** | 文件目录新增 registry.go |
| `processor/replace.go` | **新增** | Replacement 结构体 + ReplaceMessages 函数 |
| `processor/replace_test.go` | **新增** | ReplaceMessages 测试 |
| `processor/dialogue_compressor.go` | **新增** | DialogueCompressor 完整实现 |
| `processor/dialogue_compressor_test.go` | **新增** | DialogueCompressor 测试 |
| `processor/doc.go` | **修改** | 文件目录新增 replace.go / dialogue_compressor.go |

## 8. 回填关系

| 本步骤产出 | 被回填位置 | 标记 |
|-----------|-----------|------|
| `ReplaceMessages` 函数 | 5.23-5.29 其他处理器均可复用 | ⤵️ 5.23-5.29 |
| `context_engine.RegisterProcessorFactory` | 5.30 ContextEngine 门面 | ⤵️ 5.30 |
| `ProcessorFactory` 类型 | 5.30 ContextEngine._create_processor 对应 | ⤵️ 5.30 |
| 依赖 `ModelContext.SetMessages/TokenCounter` | 5.31 Context 实现 | ⤴️ 5.31 |

## 9. 测试策略

### 9.1 单元测试覆盖

- `DialogueCompressorConfig.Validate()` — 边界值测试
- `GetCompressPairs` — 各种消息组合（纯对话/含 tool_calls/无配对）
- `CollectCompleteRounds` / `BuildCompressTargets` — 轮次识别
- `GetCompressIdx` — KeepLastRound / MessagesToKeep 组合
- `FindLastFinalAssistantIdx` — 有/无最终 AssistantMessage
- `SerializeMessage` — 三种消息类型序列化
- `BuildSplitContextPayload` / `BuildTargetsPayload` — 载文构建
- `WrapMemoryBlock` — 标记格式
- `HasCompressionBenefit` — 压缩收益判断
- `IsValidBlocksPayload` — 有效/无效 payload
- `ReplaceMessages` — 多种替换场景
- `ExtractCompactSummaryFromReplacements` — 摘要提取
- `EstimateContentTokens` — string/非 string 内容

### 9.2 Mock 策略

- LLM 调用：通过 `WithCompressorModel(mockModel)` 注入 mock Model
- ModelContext：通过接口 mock `SetMessages` / `TokenCounter` / `Len`
- TokenCounter：mock `CountMessages` 返回预设值

### 9.3 build tag

DialogueCompressor 本身不依赖真实 LLM API（测试用 mock），不需要 build tag。
