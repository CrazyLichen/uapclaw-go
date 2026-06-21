# CurrentRoundCompressor (5.25) 设计文档

> 当轮增量压缩器 — 将较早的对话片段压缩为结构化记忆块，支持两阶段压缩

## 1. 概述

CurrentRoundCompressor 是上下文引擎处理器链中的第三个处理器（位于 MicroCompactProcessor 和 DialogueCompressor 之后），负责当轮增量压缩。当累计 Token 超过阈值时，将较早的对话片段压缩为协议化的 `[CURRENT_ROUND_MEMORY_BLOCK]` 记忆块，并在历史记忆块累积过多时触发二次合并。

### 在处理器链中的位置

```
消息输入 → [MicroCompactProcessor] → [DialogueCompressor] → [CurrentRoundCompressor] → [RoundLevelCompressor]
                 ↓ 轻量                    ↓ 中等                    ↓ 重量               ↓ 最重
           清除旧工具结果             对话轮次级压缩           当轮增量压缩            轮级递归压缩
           (不调用LLM)              (调用LLM)              (调用LLM,两阶段)        (调用LLM)
```

### 与 DialogueCompressor 的核心差异

| 维度 | DialogueCompressor | CurrentRoundCompressor |
|------|---|---|
| 压缩粒度 | 对话轮次 | 当前轮次内增量片段 |
| 输出标记 | `[DIALOGUE_MEMORY_BLOCK]` | `[CURRENT_ROUND_MEMORY_BLOCK]` |
| 二次合并 | 无 | 有（MergeSummaryBlocks） |
| LLM 输出格式 | JSON（blocks 数组） | 纯文本摘要 |
| 上下文参考 | 仅目标消息 | 前序摘要 + 近期上下文 + 用户意图窗口 |
| 触发条件 | 消息数或 Token 数 | 仅 Token 数 |
| 降级路径 | JSON 解析 → Fallback 整段 | 无 JSON 解析，直接用文本 |

### 对应 Python 代码

- 主体：`openjiuwen/core/context_engine/processor/compressor/current_round_compressor.py`
- 工具函数：`openjiuwen/core/context_engine/processor/compressor/util.py`
- 测试：`tests/unit_tests/core/context_engine/test_current_round_compressor.py`

## 2. util.go 统一提取

与 Python util.py 保持一致，将所有共享函数统一迁入 `util.go`。同时将 `dialogue_compressor.go` 和 `full_compact_processor.go` 中已有的重复函数提取到 util.go，原有文件改为调用导出版本。

### 2.1 核心压缩工具函数（5.25 直接依赖）

| Go 函数 | Python 对应 | 来源 |
|---|---|---|
| `MessageToText(msg BaseMessage) string` | `message_to_text` | 从 full_compact 的 `_messageToText` 提取 |
| `IsSummaryMessage(msg BaseMessage, marker string) bool` | `is_summary_message` | **新增** |
| `CollectSummaryIndices(messages []BaseMessage, marker string) []int` | `collect_summary_indices` | **新增** |
| `CountMessagesTokens(tokenCounter TokenCounter, messages []BaseMessage, modelName string, processorType string) int` | `count_messages_tokens` | 从 dialogue_compressor 的 `countMessagesTokens` 提取 |
| `FindLastCompletedAPIRoundEndIdx(messages []BaseMessage, startIdx int, endIdx int) int` | `find_last_completed_api_round_end_idx` | **新增** |
| `IterSummaryMergeRanges(messages []BaseMessage, marker string, minBlocks int) [][2]int` | `iter_summary_merge_ranges` | **新增** |

### 2.2 API 轮次工具函数

| Go 函数 | Python 对应 | 来源 |
|---|---|---|
| `GroupCompletedAPIRoundsMessages(messages []BaseMessage) [][]BaseMessage` | `group_completed_api_rounds` | 从 full_compact 的 `groupCompletedAPIRounds` 提取 |
| `MessageSignature(msg BaseMessage) string` | `message_signature` | 从 full_compact 的 `_messageSignature` 提取 |
| `RoundSignature(messages []BaseMessage) string` | N/A (Go 特有) | 从 full_compact 的 `_roundSignature` 提取 |
| `FlattenGroups(groups [][]BaseMessage) []BaseMessage` | N/A (Go 特有) | 从 full_compact 的 `flattenGroups` 提取 |

### 2.3 Skill 相关工具函数

| Go 函数 | Python 对应 | 来源 |
|---|---|---|
| `IsSkillFilePath(filePath string) bool` | `is_skill_file_path` | 从 full_compact 的 `isSkillFilePath` 提取 |
| `ExtractSkillNameFromPath(filePath string) string` | `extract_skill_name_from_path` | **新增** |
| `RoundContainsSkillRead(messages []BaseMessage) bool` | `round_contains_skill_read` | 从 full_compact 的 `roundContainsSkillRead` 提取 |
| `ExtractSkillFileContent(truncateFn func(string) string, resultText string) string` | `extract_skill_file_content` | **新增** |

### 2.4 Tool Call 工具函数

| Go 函数 | Python 对应 | 来源 |
|---|---|---|
| `DescribeToolCall(toolName string, argumentsText string) string` | `describe_tool_call` | **新增** |
| `ParseToolArguments(argumentsText string) map[string]any` | `parse_tool_arguments` | **新增** |
| `ExtractArgumentValue(parsedArgs map[string]any, argumentsText string, keys ...string) string` | `extract_argument_value` | 从 full_compact 的 `extractArgumentValue` 提取（签名改为与 Python 对齐） |
| `FindToolResultText(messages []BaseMessage, toolCallID string) string` | `find_tool_result_text` | **新增** |
| `ExtractToolResultHint(toolName string, resultText string, allowedToolNames []string) string` | `extract_tool_result_hint` | **新增** |

### 2.5 EstimateContentTokens 迁移

`EstimateContentTokens` 从 `dialogue_compressor.go` 迁移到 `util.go`（Python `estimate_content_tokens` 在 util.py 中）。

### 2.6 不迁移的函数

以下函数保留在原文件：

| 函数 | 原位置 | 原因 |
|---|---|---|
| `ReinjectedStateBuilderSpec` / `FullCompactStateReinjector` | full_compact_processor.go | 与 FullCompactProcessor 紧密耦合 |
| `build*ReinjectedContent` 系列 | full_compact_processor.go | 与 FullCompactProcessor 紧密耦合 |
| `SerializeMessage` | dialogue_compressor.go | Go 特有序列化格式，Python 无对应 |
| `WrapMemoryBlock` | dialogue_compressor.go | DialogueCompressor 专用格式 |
| `FindLastFinalAssistantIdx` | dialogue_compressor.go | DialogueCompressor 专用逻辑 |
| `GetCompressPairs` | dialogue_compressor.go | DialogueCompressor 专用逻辑 |
| `IsValidBlocksPayload` | dialogue_compressor.go | DialogueCompressor JSON 解析专用 |
| context_utils 子包函数 | context_utils/ | MicroCompactProcessor 专用 |

### 2.7 对原有文件的改动

**dialogue_compressor.go**：
- `EstimateContentTokens` → 迁移到 util.go，原位改为调用 `EstimateContentTokens`
- `countMessagesTokens` 方法 → 改为调用 `CountMessagesTokens` 函数

**full_compact_processor.go**：
- `_messageToText` → 改为调用 `MessageToText`
- `groupCompletedAPIRounds` → 改为调用 `GroupCompletedAPIRoundsMessages`
- `isSkillFilePath` → 改为调用 `IsSkillFilePath`
- `extractArgumentValue` → 改为调用 `ExtractArgumentValue`（签名微调：增加 parsedArgs 参数）
- `_messageSignature` → 改为调用 `MessageSignature`
- `_roundSignature` → 改为调用 `RoundSignature`
- `flattenGroups` → 改为调用 `FlattenGroups`
- `roundContainsSkillRead` → 改为调用 `RoundContainsSkillRead`

## 3. CurrentRoundCompressor 主体设计

### 3.1 配置结构体

```go
type CurrentRoundCompressorConfig struct {
    TokensThreshold                int    // 触发压缩的 Token 阈值（默认 100000）
    MessagesToKeep                 int    // 保留最近 N 条消息不压缩（默认 3）
    Model                          *llm_schema.ModelRequestConfig
    ModelClient                    *llm_schema.ModelClientConfig
    MinSelectedTokensForCompression int   // 最小可压缩 Token 数（默认 20000）
    CompressionTargetTokens        int    // 第一阶段压缩目标 Token 数（默认 4000）
    SummaryMergeTargetTokens       int    // 第二阶段合并目标 Token 数（默认 4000）
    AccumulatedSummaryTokenLimit   int    // 累积摘要触发合并的 Token 阈值（默认 20000）
    SummaryMergeMinBlocks          int    // 触发合并的最少连续摘要块数（默认 3）
    PriorContextWindowSize         int    // 前序上下文窗口大小（默认 10）
    CustomCompressionPrompt        string // 自定义压缩提示词
}
```

含 `Validate() error` 方法 + `NewCurrentRoundCompressorConfig() *CurrentRoundCompressorConfig` 默认构造函数。

### 3.2 处理器结构体

```go
type CurrentRoundCompressor struct {
    *processor.BaseProcessor
    model                        *llm.Model
    tokenThreshold               int
    messagesToKeep               int
    minSelectedTokens             int
    compressionTargetTokens      int
    summaryMergeTargetTokens     int
    accumulatedSummaryTokenLimit int
    summaryMergeMinBlocks        int
    priorContextWindowSize       int
    compressedPrompt             string
    cleanPrompt                  string
}
```

含 `CurrentRoundCompressorOption func(*CurrentRoundCompressor)` 和 `WithCurrentRoundModel(model *llm.Model)` 注入选项。

### 3.3 常量

```go
const (
    currentRoundMemoryBlockMarker = "[CURRENT_ROUND_MEMORY_BLOCK]"
    defaultCurrentRoundCompressionPrompt = `...` // 与 Python DEFAULT_COMPRESSION_PROMPT 完全对齐
    defaultCleanPrompt = `...`                   // 与 Python CLEAN_PROMPT 完全对齐
)
```

### 3.4 核心方法链

```
TriggerAddMessages → 判断总 Token 是否超过阈值
       ↓ true
OnAddMessages → 入口方法
       ↓
GetCompressIdx → 找到最新可压缩的 UserMessage 边界
       ↓
MultiCompress → 两阶段压缩协调
  ├── 第一阶段：Compress → 增量压缩选中片段 → UserMessage(CURRENT_ROUND_MEMORY_BLOCK)
  └── 第二阶段：MergeSummaryBlocks → 合并旧记忆块 → UserMessage(CURRENT_ROUND_MEMORY_BLOCK)
       ↓
ReplaceMessages → 替换原始消息（从后往前避免索引偏移）
       ↓
返回 ContextEvent
```

### 3.5 各方法详细说明

**`TriggerAddMessages`**：总 Token 数（context + messagesToAdd）超过 `TokensThreshold` 时返回 true。使用 `CountMessagesTokens` 计算。

**`OnAddMessages`**：
1. 合并 context + messagesToAdd
2. 调用 `GetCompressIdx` 找压缩边界
3. 若无边界，透传
4. 计算 `keepStartIdx`
5. 调用 `MultiCompress` 压缩选中范围
6. 成功则 `ReplaceMessages` + 返回 `ContextEvent`
7. MODEL_CALL_FAILED 时降级跳过，其他异常 raise CONTEXT_EXECUTION_ERROR

**`GetCompressIdx`**：从后往前找最后一个 UserMessage，且不在保留尾部（`messagesToKeep`）内。如果最后一条就是 UserMessage，返回 -1（不压缩）。如果 UserMessage 在保留区域内，也返回 -1。

**`MultiCompress`**：
1. 第一阶段：调用 `Compress` 压缩 `[lastUserIdx+1, endIdx)` 的消息
2. 用 `FindLastCompletedAPIRoundEndIdx` 调整 endIdx 到完整 API 轮次边界
3. 第二阶段：`IterSummaryMergeRanges` 找连续摘要范围，`MergeSummaryBlocks` 合并
4. 每次只合并一个范围（break after first success）

**`Compress`**（第一阶段）：
1. 如果选中 Token < `MinSelectedTokensForCompression`，跳过
2. 收集前序摘要（`CollectSummaryIndices` + `MessageToText`）
3. 收集近期上下文（`FormatRecentContext`）
4. 收集用户意图窗口（`FormatPriorContextAndQuery`）
5. 填充提示词模板（`BuildPrompt`）
6. 调用 `model.Invoke()`
7. 判断压缩收益（`CountMessagesTokens` 比较）
8. 包装为 `WrapCurrentRoundMemoryBlock`

**`MergeSummaryBlocks`**（第二阶段）：
1. 如果总 Token ≤ `AccumulatedSummaryTokenLimit`，跳过
2. 格式化旧块为 `[MEMORY_BLOCK_N]` 分段
3. 填充 `CLEAN_PROMPT` 模板
4. 调用 LLM 合并
5. 包装为 `WrapCurrentRoundMemoryBlock`
6. 日志记录成功/失败

### 3.6 辅助方法

| 方法 | 用途 |
|---|---|
| `WrapCurrentRoundMemoryBlock(summary string) string` | 包装摘要为协议化记忆块（含 8 个元数据头） |
| `UnwrapMemoryBlockSummary(summary string) string` | 剥离已有记忆块包装（二次压缩前使用） |
| `BuildPrompt(targetTokens int, priorSummaries, recentContext, priorContextAndQuery string) string` | 填充第一阶段提示词模板 |
| `FormatRecentContext(allMessages []BaseMessage, endIdx int) string` | 序列化压缩范围后的尾部上下文（排除已有记忆块） |
| `FormatPriorContextAndQuery(allMessages []BaseMessage, currentQueryIdx int) string` | 格式化用户意图窗口 + 当前查询（仅含纯 UserMessage 和无 tool_calls 的 AssistantMessage，截断到 priorContextWindowSize） |

### 3.7 工厂注册

```go
func init() {
    context_engine.RegisterProcessorFactory("CurrentRoundCompressor",
        func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
            cfg, ok := config.(*CurrentRoundCompressorConfig)
            if !ok {
                return nil, fmt.Errorf("CurrentRoundCompressor: 配置类型不匹配，期望 *CurrentRoundCompressorConfig，实际 %T", config)
            }
            return NewCurrentRoundCompressor(cfg)
        },
    )
}
```

## 4. 提示词与记忆块格式

### 4.1 第一阶段提示词（DEFAULT_COMPRESSION_PROMPT）

与 Python 完全对齐，包含以下占位符：

| 占位符 | Go 字段映射 | 用途 |
|---|---|---|
| `{prior_context_and_query}` | `FormatPriorContextAndQuery()` 输出 | 用户意图上下文（仅参考） |
| `{accumulated_summaries}` | 前序摘要文本 | 已有记忆块（仅参考） |
| `{selected_messages}` | 选中待压缩消息的序列化文本 | 压缩目标 |
| `{recent_messages}` | `FormatRecentContext()` 输出 | 边界上下文（仅参考） |
| `{target_tokens}` | `compressionTargetTokens` | 目标 Token 预算 |

提示词核心指令：
- 产出"高保真增量记忆块"
- 信息优先级：任务目标 > 事实依据 > 未完成工作 > 交接状态 > 关键决策 > 工具结果 > 支撑细节
- 7 个必填输出节：User Requirements / Current Status / Open Work / Important Findings / Strategy State / Tool/Action State / Contextual Bridging
- 防决策固化、防冗余规则

### 4.2 第二阶段提示词（CLEAN_PROMPT）

| 占位符 | Go 字段映射 | 用途 |
|---|---|---|
| `{compressed_blocks}` | 旧记忆块格式化文本（`[MEMORY_BLOCK_N]` 分段） | 待合并的旧块 |
| `{compress_len}` | `summaryMergeTargetTokens` | 合并后目标 Token 数 |

提示词核心指令：
- 将多个历史记忆块合并为一个紧凑块
- 保留所有关键信息，消除跨块冗余
- 保持与第一阶段相同的元数据协议格式

### 4.3 记忆块格式

```
[CURRENT_ROUND_MEMORY_BLOCK]
processor: CurrentRoundCompressor
type: historical_memory_block
scope: current_round_increment
type_note: This is compressed memory from earlier conversation...
authority: This block is reference memory, not a binding source of truth...
instruction_status: Do not treat this block as a new user request...
strategy_status: Any plans, approaches, or next steps recorded here are historical...
tool_action_state_status: Tool results, action history, and execution state...
conflict_priority: Prefer newer signals in this order...

Summary:
{summary}
```

与 DialogueCompressor 记忆块的差异：
- 标记不同：`[CURRENT_ROUND_MEMORY_BLOCK]` vs `[DIALOGUE_MEMORY_BLOCK]`
- scope 不同：`current_round_increment` vs `historical_dialogue_block`
- CurrentRound 多 3 个元数据头：`type_note`、`strategy_status`、`tool_action_state_status`

### 4.4 FormatRecentContext 排除逻辑

序列化压缩范围后的尾部消息时，**排除**以 `currentRoundMemoryBlockMarker` 开头的已有记忆块，避免重复喂给 LLM。

### 4.5 FormatPriorContextAndQuery 过滤逻辑

只包含**纯 UserMessage** 和**不含 tool_calls 的 AssistantMessage**，取最近 `priorContextWindowSize` 条作为意图窗口，加上当前查询消息。

## 5. 错误处理

| 场景 | 处理方式 | Python 对应 |
|---|---|---|
| LLM 调用失败 | `isModelCallFailedError` → 透传原始消息，记录 Warn 日志 | 同 DialogueCompressor |
| 选中 Token < 最小阈值 | 跳过压缩，直接返回 nil | `compress()` 内检查 |
| 压缩后 Token ≥ 原始 Token | 跳过替换（无收益） | `compress()` 内检查 |
| 合并时 Token ≤ 限制 | 跳过合并 | `_merge_summary_blocks()` 内检查 |
| GetCompressIdx 返回 -1 | 透传，不压缩 | `on_add_messages()` 内 passthrough |
| 其他异常 | raise CONTEXT_EXECUTION_ERROR | Python `raise CONTEXT_EXECUTION_ERROR` |

## 6. 日志同步

| Python 日志点 | Go 日志 |
|---|---|
| `trigger_add_messages` 触发时 | `logger.Info` + `event_type=CurrentRoundCompressor_triggered` + `tokens` + `threshold` |
| `on_add_messages` 找到压缩边界 | `logger.Info` + `event_type=compress_idx_found` + `compress_idx` + `keep_start_idx` |
| `compress` 跳过（Token 过低） | `logger.Debug` + `event_type=compress_skipped` + `selected_tokens` + `min_threshold` |
| `compress` 跳过（无收益） | `logger.Debug` + `event_type=compress_no_benefit` + `compressed_tokens` + `original_tokens` |
| `compress` 成功 | `logger.Info` + `event_type=compress_success` + `original_tokens` + `compressed_tokens` + `saved_tokens` |
| `_merge_summary_blocks` 跳过 | `logger.Debug` + `event_type=merge_skipped` + `total_tokens` + `limit` |
| `_merge_summary_blocks` 成功 | `logger.Info` + `event_type=merge_success` + `block_count` + `original_tokens` + `merged_tokens` |
| `_merge_summary_blocks` 失败 | `logger.Warn` + `event_type=merge_failed` + `Err(err)` |
| LLM 调用失败降级 | `logger.Warn` + `processor_type` + `Err(err)` |

## 7. 测试策略

### 7.1 配置校验测试

- `TestNewCurrentRoundCompressorConfig_默认值` — 验证所有默认值与 Python 对齐
- `TestCurrentRoundCompressorConfig_Validate_正常` — 合法配置
- `TestCurrentRoundCompressorConfig_Validate_TokensThreshold零` — TokensThreshold ≤ 0 报错
- `TestCurrentRoundCompressorConfig_Validate_MessagesToKeep负数` — MessagesToKeep < 0 报错

### 7.2 核心方法测试

- `TestCurrentRoundCompressor_ProcessorType` — 返回 `"CurrentRoundCompressor"`
- `TestCurrentRoundCompressor_TriggerAddMessages_超过阈值` — 返回 true
- `TestCurrentRoundCompressor_TriggerAddMessages_低于阈值` — 返回 false
- `TestCurrentRoundCompressor_OnAddMessages_触发压缩` — mock LLM，验证输出含 `[CURRENT_ROUND_MEMORY_BLOCK]`
- `TestCurrentRoundCompressor_OnAddMessages_不压缩` — token 低于阈值，透传
- `TestCurrentRoundCompressor_OnAddMessages_最后一条是UserMessage` — GetCompressIdx 返回 -1，不压缩
- `TestCurrentRoundCompressor_GetCompressIdx_找到边界` — 正常找到 UserMessage
- `TestCurrentRoundCompressor_GetCompressIdx_最后UserMessage` — 返回 -1
- `TestCurrentRoundCompressor_GetCompressIdx_在保留区域内` — 返回 -1
- `TestCurrentRoundCompressor_Compress_Token过低跳过` — 选中 token < min
- `TestCurrentRoundCompressor_Compress_无收益跳过` — compressed >= original
- `TestCurrentRoundCompressor_Compress_成功` — mock LLM 返回摘要
- `TestCurrentRoundCompressor_MultiCompress_第一阶段成功` — mock Compress
- `TestCurrentRoundCompressor_MultiCompress_含二次合并` — mock 合并逻辑
- `TestCurrentRoundCompressor_MergeSummaryBlocks_跳过` — token 未超限
- `TestCurrentRoundCompressor_MergeSummaryBlocks_成功` — mock LLM 合并
- `TestCurrentRoundCompressor_MergeSummaryBlocks_失败` — LLM 报错

### 7.3 辅助方法测试

- `TestWrapCurrentRoundMemoryBlock` — 验证完整元数据头
- `TestUnwrapMemoryBlockSummary` — 剥离包装
- `TestFormatRecentContext_排除记忆块` — 跳过 SUMMARY_MARKER 消息
- `TestFormatPriorContextAndQuery_过滤工具消息` — 只含纯 User + 无 tool_calls Assistant
- `TestFormatPriorContextAndQuery_窗口截断` — 超过 windowSize 时截断

### 7.4 工厂注册测试

- `TestCurrentRoundCompressor_工厂注册` — `GetProcessorFactory("CurrentRoundCompressor")` 存在
- `TestCurrentRoundCompressor_工厂配置类型不匹配` — 错误配置类型报错

### 7.5 util.go 新增函数测试

- `TestIsSummaryMessage_是摘要` / `_不是摘要`
- `TestCollectSummaryIndices_多个摘要` / `_无摘要`
- `TestCountMessagesTokens_有TokenCounter` / `_无TokenCounter` / `_空消息`
- `TestFindLastCompletedAPIRoundEndIdx_有完成轮次` / `_无完成轮次` / `_空范围`
- `TestIterSummaryMergeRanges_足够连续块` / `_不足连续块` / `_分散块`
- `TestMessageToText_字符串内容` / `_非字符串内容`
- `TestDescribeToolCall_各工具类型`（read_file / write_file / grep / 未知工具）
- `TestParseToolArguments_正常JSON` / `_空字符串` / `_非法JSON`
- `TestFindToolResultText_找到` / `_未找到` / `_空ToolCallID`
- `TestExtractToolResultHint_各工具类型`（read_file / glob / grep / edit_file / write_file）
- `TestIsSkillFilePath_匹配` / `_不匹配`
- `TestExtractSkillNameFromPath_正常` / `_非skill路径` / `_空路径`
- `TestExtractSkillFileContent_正常内容` / `_空内容`

### 7.6 重构验证

- `dialogue_compressor_test.go` — 全部通过
- `full_compact_processor_test.go` — 全部通过

## 8. 文件组织

### 新增文件

```
compressor/
├── current_round_compressor.go       # CurrentRoundCompressor 主体
├── current_round_compressor_test.go  # 测试
```

### 修改文件

```
compressor/
├── util.go                           # 大幅扩充（新增约 20 个函数）
├── util_test.go                      # 大幅扩充（新增约 25 个测试函数）
├── dialogue_compressor.go            # 小改（countMessagesTokens → CountMessagesTokens，EstimateContentTokens 迁移）
├── dialogue_compressor_test.go       # 微调
├── full_compact_processor.go         # 中改（8 个函数改为调用 util.go 导出版本）
├── full_compact_processor_test.go    # 微调
├── doc.go                            # 更新文件目录
```

### doc.go 更新后文件目录

```
compressor/
├── doc.go                          # 包文档
├── util.go                         # 包级共享函数
├── dialogue_compressor.go          # DialogueCompressor 对话压缩器
├── micro_compact_processor.go      # MicroCompactProcessor 微压缩处理器
├── full_compact_processor.go       # FullCompactProcessor 全量压缩处理器
├── current_round_compressor.go     # CurrentRoundCompressor 当轮增量压缩器
```

## 9. 实现顺序

1. **重构 util.go**：迁移 + 新增所有函数，补充测试，确保 util_test 通过
2. **重构 dialogue_compressor.go**：改为调用 util.go 导出函数，确认原有测试通过
3. **重构 full_compact_processor.go**：改为调用 util.go 导出函数，确认原有测试通过
4. **实现 current_round_compressor.go**：主体逻辑 + 提示词 + 辅助方法
5. **补充 current_round_compressor_test.go**：覆盖所有核心场景
6. **更新 doc.go**：文件目录
7. **全量测试**：`go test ./internal/agentcore/context_engine/...`
