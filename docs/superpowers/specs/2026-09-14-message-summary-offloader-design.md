# MessageSummaryOffloader (5.28) 设计文档

## 概述

MessageSummaryOffloader 是 MessageOffloader (5.27) 的增强版，解决同一问题——工具返回结果过大消耗过多 Token——但采用 **LLM 智能摘要** 替代简单裁剪。当新添加的消息超过大小阈值时，通过 LLM 生成任务感知的自适应压缩摘要，保留关键信息的同时大幅减少 Token 消耗。

### 在 Agent 会话中的流程位置

```
用户输入 → Session → ReAct 循环
                      │
                      ├─→ LLM 调用（模型推理）
                      │
                      ├─→ 工具执行 → 工具结果消息
                      │                    │
                      │                    ▼
                      │         ┌──────────────────────────────────┐
                      │         │  ContextEngine.on_add_messages() │ ← 5.28 在此介入
                      │         │  ┌─ TriggerAddMessages()         │    判断是否需要摘要卸载
                      │         │  ├─ Processor 链依次执行：        │
                      │         │  │  ├ DialogueCompressor         │
                      │         │  │  ├ FullCompactProcessor       │
                      │         │  │  ├ CurrentRoundCompressor     │
                      │         │  │  ├ RoundLevelCompressor       │
                      │         │  │  ├ MessageOffloader (5.27)    │ ← 简单裁剪卸载
                      │         │  │  └ MessageSummaryOffloader    │ ← 5.28: 智能摘要卸载
                      │         │  └─ 返回处理后消息               │
                      │         └──────────────────────────────────┘
                      │
                      └─→ 下轮推理（消息已被压缩/卸载，节省 Token）
```

### 与 MessageOffloader 的对比

| 对比 | MessageOffloader (5.27) | MessageSummaryOffloader (5.28) |
|------|------------------------|-------------------------------|
| 卸载策略 | 简单裁剪：保留前 N 个字符 + "..." | LLM 摘要：理解任务意图后生成压缩摘要 |
| 信息保留 | 丢失尾部所有信息 | 根据任务上下文保留关键信息 |
| 额外开销 | 无（纯字符串操作） | 需要 1-2 次 LLM 调用 |
| 触发方式 | 消息总数/Token 总量超阈值 | 新消息单条超过 largeMessageThreshold |
| 适用场景 | 信息价值均匀分布的大文本 | 需要保留关键数据的长工具结果 |

## 设计决策

### 1. 继承方式：独立实现

Python 中 `MessageSummaryOffloader` 继承自 `MessageOffloader`，Go 没有继承，采用**独立实现**方案：

- `MessageSummaryOffloader` 独立嵌入 `*processor.BaseProcessor`
- 各自实现 `shouldOffloadMessage`（逻辑有差异：token 计数 vs 字符计数）
- 各自实现 `isProtectedToolMessage`、`newOffloadHandleAndPath`（逻辑相同但独立）
- 不改动现有 `MessageOffloader` 代码

### 2. LLM 调用：构造时创建 Model

与现有 compressor（DialogueCompressor/CurrentRoundCompressor 等）对齐：

- 在 `NewMessageSummaryOffloader` 中根据 config 创建 `*llm.Model`
- 提供 `WithMessageSummaryModel(model *llm.Model)` 选项注入（测试用）
- 先应用选项，若 `model == nil` 再从 config 创建

### 3. 逻辑对齐：完全对齐 Python

`shouldOffloadMessage` 差异保留：
- MessageOffloader：字符长度 > largeMessageThreshold
- MessageSummaryOffloader：token 计数优先（TokenCounter），回退到 字符数//3

`trigger_add_messages` 差异保留：
- MessageOffloader：消息总数/Token 总量超阈值触发
- MessageSummaryOffloader：只看新添加的消息中是否有超过 largeMessageThreshold 的

## 文件组织

```
offloader/
├── doc.go                           # 包文档（需更新，添加 message_summary_offloader.go 条目）
├── message_offloader.go             # MessageOffloader + Config (5.27 已完成，不改动)
├── message_summary_offloader.go     # MessageSummaryOffloader + Config (5.28 新增)
└── message_summary_offloader_test.go # 测试 (5.28 新增)
```

## MessageSummaryOffloaderConfig

对齐 Python `MessageSummaryOffloaderConfig`：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| MessagesThreshold | *int | nil | 兼容字段；自适应压缩不用于触发 |
| TokensThreshold | int | 20000 | 兼容字段；自适应压缩用 per-message 检查 |
| LargeMessageThreshold | int | 1000 | 自适应压缩候选消息的最小大小 |
| OffloadMessageTypes | []string | ["tool"] | 可摘要卸载的角色 |
| ProtectedToolNames | []string | ["reload_original_context_messages"] | 受保护工具 |
| MessagesToKeep | *int | nil | 兼容字段 |
| KeepLastRound | bool | true | 兼容字段 |
| Model | *ModelRequestConfig | nil | 摘要模型请求配置 |
| ModelClient | *ModelClientConfig | nil | 摘要模型客户端配置 |
| SummaryMaxTokens | int | 900 | 摘要最大 token 数 |
| EnablePreciseStep | bool | false | 启用 LLM 精确任务提取 |
| StepSummaryMaxContextMessages | int | 8 | 任务提取上下文消息数 |
| ContentMaxCharsForCompression | int | 200000 | 压缩输入字符上限 |

Validate 规则：
- MessagesToKeep < MessagesThreshold（两者均非 nil 时）

## MessageSummaryOffloader 结构体

```go
type MessageSummaryOffloader struct {
    *processor.BaseProcessor
    config    *MessageSummaryOffloaderConfig
    model     *llm.Model
}
```

## 核心方法清单

### 接口实现（ContextProcessor）

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `NewMessageSummaryOffloader` | `__init__` | 构造 + 创建 Model + Validate |
| `WithMessageSummaryModel` | — | 选项注入 Model（测试用） |
| `ProcessorType` | `processor_type` | 返回 `"MessageSummaryOffloader"` |
| `TriggerAddMessages` | `trigger_add_messages` | 只检查新消息是否超过 largeMessageThreshold |
| `OnAddMessages` | `on_add_messages` | 遍历新消息，对符合条件的执行自适应摘要 |
| `SaveState` | `save_state` | 空操作 |
| `LoadState` | `load_state` | 空操作 |

### 自适应压缩核心方法

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `offloadMessageAdaptive` | `_offload_message_adaptive` | 核心方法：提取函数调用+任务+压缩+卸载 |
| `getFunctionCallFromChain` | `_get_function_call_from_chain` | 回溯查找 ToolCall |
| `getStepFromChainDefault` | `_get_step_from_chain_default` | 启发式：最后一条 UserMessage |
| `getStepFromChainPrecise` | `_get_step_from_chain_precise` | LLM 精确提取（enablePreciseStep=true） |
| `compressWithFallback` | `_compress_with_fallback` | 多次降级重试压缩 |
| `buildCompressionAttempts` | `_build_compression_attempts` | 构建 [全文, 截断, 半截断] 三级降级 |
| `smartTruncateContent` | `_smart_truncate_content` | 保留头/中/尾各 33% |
| `buildCompressionPrompt` | `_build_compression_prompt` | 构建自适应压缩提示词 |
| `parseCompressionResult` | `_parse_compression_result` | 解析 LLM JSON 响应 |
| `isContextOverflowError` | `_is_context_overflow_error` | 关键词检测上下文溢出错误 |

### 辅助方法

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `shouldOffloadMessage` | `_should_offload_message` | token 计数优先，回退到 字符数//3 |
| `messageSize` | `_message_size` | TokenCounter 优先，回退 字符//3 |
| `isProtectedToolMessage` | `_is_protected_tool_message` | 与 MessageOffloader 相同逻辑 |
| `newOffloadHandleAndPath` | `_new_offload_handle_and_path` | 与 MessageOffloader 相同逻辑 |
| `selectMessagesForStepSummary` | `_select_messages_for_step_summary` | 筛选有效消息用于任务提取 |
| `isValidForStepSummary` | `_is_valid_for_step_summary` | user 或 assistant(无 tool_calls) |

## 常量/提示词

完整对齐 Python 的常量和提示词模板：

| 常量 | 值 | 说明 |
|------|------|------|
| `contextOverflowKeywords` | ["context length", "token limit", "too long", "exceeds", "maximum context", "context window"] | 上下文溢出关键词 |
| `truncatedMarker` | `"...[TRUNCATED]..."` | 智能截断标记 |
| `adaptiveOffloadPromptTemplate` | 完整对齐 Python ADAPTIVE_OFFLOAD_PROMPT_TEMPLATE | 自适应压缩提示词 |
| `outputJSONSchema` | 完整对齐 Python OUTPUT_JSON_SCHEMA | 输出 JSON Schema |
| `stepSummaryPrompt` | 完整对齐 Python STEP_SUMMARY_PROMPT | 任务提取提示词 |
| `defaultOffloadSummaryPrompt` | 完整对齐 Python DEFAULT_OFFLOAD_SUMMARY_PROMPT | 旧版摘要提示词（兼容） |

## init 注册

```go
func init() {
    context_engine.RegisterProcessorFactory("MessageSummaryOffloader",
        func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
            cfg, ok := config.(*MessageSummaryOffloaderConfig)
            if !ok {
                return nil, fmt.Errorf("MessageSummaryOffloader: 配置类型不匹配")
            }
            return NewMessageSummaryOffloader(cfg)
        },
    )
}
```

## 回填项

- **⤵️ 5.31**：`mc.WorkspaceDir()` — `newOffloadHandleAndPath` 中使用空字符串（与 MessageOffloader 一致）
- **⤵️ 5.31**：`mc.OffloadMessages()` — in_memory 模式（通过 BaseProcessor.OffloadMessages 间接使用）
- **⤵️ 9.32**：`SysOperation` 异步写入（通过 BaseProcessor.OffloadMessages 间接使用）

## 测试策略

与现有 compressor 测试模式对齐：

1. **fakeBaseModelClient** mock LLM 响应，通过 `WithMessageSummaryModel` 注入
2. 覆盖场景：
   - 构造校验（Validate）
   - 触发判断（TriggerAddMessages：单条超阈值/不超/已卸载/受保护工具）
   - 自适应压缩流程（offloadMessageAdaptive：正常/无函数调用/精确步骤提取）
   - 降级重试（compressWithFallback：全文→截断→半截断→全部失败）
   - JSON 解析（parseCompressionResult：正常/Markdown 包裹/无 JSON/缺 summary）
   - 智能截断（smartTruncateContent：短内容/长内容各段比例）
   - 上下文溢出检测（isContextOverflowError：各关键词）
   - 任务提取（getStepFromChainDefault/Precise）
   - shouldOffloadMessage（token 计数 vs 字符计数回退）
3. 覆盖率目标：≥ 85%

## 对应 Python 代码

`openjiuwen/core/context_engine/processor/offloader/message_summary_offloader.py`
