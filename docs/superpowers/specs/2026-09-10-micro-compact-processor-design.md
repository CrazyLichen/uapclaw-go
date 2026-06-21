# MicroCompactProcessor 微压缩处理器设计

## 概述

MicroCompactProcessor（5.23）是上下文引擎处理器链中的轻量级压缩器，通过清除旧工具结果内容来减少 Token 消耗，不需要调用 LLM。它是 `session_memory` 模式下的第二道处理器（位于 ToolResultBudgetProcessor 之后、FullCompactProcessor 之前）。

对应 Python 代码：`openjiuwen/core/context_engine/processor/compressor/micro_compact_processor.py`

## 流程位置与作用

```
用户输入 → Session 收集消息 → ContextEngine.on_add_messages()
    → ToolResultBudgetProcessor → MicroCompactProcessor → FullCompactProcessor → LLM 推理
```

**核心作用**：监控指定工具（grep/glob/read_file/web_search/web_fetch）产生的 ToolMessage 数量，当某工具的旧结果超出阈值时，将超出保留窗口的 ToolMessage content 替换为标记文本，保留每个工具最近 N 条结果。

**与其他处理器的区别**：

| 处理器 | 策略 | 需要 LLM |
|--------|------|---------|
| MicroCompactProcessor | 清除旧 ToolMessage 内容（纯文本替换） | 否 |
| DialogueCompressor | LLM 生成对话摘要 | 是 |
| FullCompactProcessor | LLM 生成全量压缩摘要 | 是 |

## 文件规划

| 文件 | 内容 |
|------|------|
| `context_engine/context_utils/doc.go` | 子包文档 |
| `context_engine/context_utils/resolve.go` | 工具名解析函数 |
| `context_engine/context_utils/resolve_test.go` | 解析函数测试 |
| `context_engine/processor/micro_compact_processor.go` | MicroCompactProcessorConfig + MicroCompactProcessor |
| `context_engine/processor/micro_compact_processor_test.go` | 处理器测试 |

## 设计细节

### 1. context_utils 子包

包路径：`internal/agentcore/context_engine/context_utils/`

对应 Python `ContextUtils` 中的工具名解析相关方法。当前仅迁移 MicroCompactProcessor 直接依赖的函数，后续步骤（5.24-5.31）按需回填。

#### `ResolveToolCallFromMessage(message BaseMessage, contextMessages []BaseMessage) *ToolCall`

对应 Python `ContextUtils.resolve_tool_call_from_message()`。

逻辑：
1. 若 message 不是 `*ToolMessage` → 返回 nil
2. 取 `toolCallID`，若为空 → 返回 nil
3. 从后往前遍历 contextMessages，找到 `*AssistantMessage`
4. 遍历其 `ToolCalls`，匹配 `ToolCall.ID == toolCallID`
5. 匹配到 → 返回 `*ToolCall`
6. 未找到 → 返回 nil

#### `ResolveToolNameFromMessage(message BaseMessage, contextMessages []BaseMessage) string`

对应 Python `ContextUtils.resolve_tool_name_from_message()`。

内部调用 `ResolveToolCallFromMessage`，再调用 `ExtractToolName` 返回工具名字符串。未找到返回空字符串。

#### `ExtractToolName(toolCall *ToolCall) string`

对应 Python `ContextUtils.extract_tool_name()`。

从 ToolCall 提取工具名：优先 `Function.Name`，为空则尝试 `Name` 字段。

依赖：仅 `llm_schema` 包。

### 2. MicroCompactProcessorConfig

```go
type MicroCompactProcessorConfig struct {
    TriggerThreshold    int      // 触发阈值，超出保留尾部的数量（默认 5，必须 > 0）
    CompactableToolNames []string // 可压缩工具名（默认 ["grep","glob","read_file","web_search","web_fetch"]，不能为空）
    KeepRecentPerTool   int      // 每个工具保留最近 N 条（默认 15，必须 >= 0）
    ClearedMarker       string   // 替换文本（默认 "[Old tool result Content cleared]"，不能为空）
}
```

构造函数 `NewMicroCompactProcessorConfig()` 设置默认值。

`Validate()` 规则：
- `TriggerThreshold > 0`
- `KeepRecentPerTool >= 0`
- `ClearedMarker != ""`
- `CompactableToolNames` 不为空

实现 `iface.ProcessorConfig` 接口。

### 3. MicroCompactProcessor 结构体

```go
type MicroCompactProcessor struct {
    *BaseProcessor
    mcpConfig *MicroCompactProcessorConfig
}
```

构造函数 `NewMicroCompactProcessor(config *MicroCompactProcessorConfig) (*MicroCompactProcessor, error)`：
- 调用 `config.Validate()`
- 创建 `NewBaseProcessor(config)`
- 返回实例

#### 接口方法实现

| 方法 | 行为 |
|------|------|
| `ProcessorType() string` | 返回 `"MicroCompactProcessor"` |
| `TriggerAddMessages(ctx, mc, messagesToAdd, opts...)` | ① 合并 `mc.GetMessages(nil, true) + messagesToAdd`<br>② `IsAPIRound(allMessages)` 不满足 → false<br>③ `hasAnyToolExceedThreshold(allMessages)` 不满足 → false<br>④ 否则 true |
| `OnAddMessages(ctx, mc, messagesToAdd, opts...)` | ① 合并 allMessages<br>② `collectFlatIndicesForCompact(allMessages, force)`<br>③ 无索引 → return nil, messagesToAdd, nil<br>④ 遍历索引，对 `*ToolMessage` 且 content ≠ marker 调用 `msg.SetContent(NewTextContent(marker))`<br>⑤ 记录日志：清除了多少条<br>⑥ `mc.SetMessages(allMessages, true)`<br>⑦ return ContextEvent{EventType, modifiedIndices}, [] |
| `TriggerGetContextWindow(...)` | 继承 BaseProcessor 默认（不触发） |
| `OnGetContextWindow(...)` | 继承 BaseProcessor 默认（透传） |
| `SaveState()` | return `map[string]any{}` |
| `LoadState(_)` | 空操作 |

#### force 参数获取

从 opts 中提取 force 标志，通过 `iface.NewProcessorOption(opts...).Extra["force"]` 获取。

#### 非导出辅助方法

| 方法 | 对应 Python | 作用 |
|------|------------|------|
| `collectCompactableIndicesByTool(messages)` | `_collect_compactable_indices_by_tool` | 遍历消息，按工具名分组收集可压缩 ToolMessage 索引。使用 `context_utils.ResolveToolNameFromMessage()` 解析工具名 |
| `hasAnyToolExceedThreshold(messages)` | `_has_any_tool_exceed_threshold` | 判断任一工具的 ToolMessage 数量是否超过 TriggerThreshold + KeepRecentPerTool |
| `collectFlatIndicesForCompact(messages, force)` | `_collect_flat_indices_for_compact` | 收集需要清除的索引列表。force=true 时阈值降为 KeepRecentPerTool；超过阈值的工具，保留尾部 KeepRecentPerTool 条，其余加入清除列表 |

### 4. 自动注册

```go
func init() {
    context_engine.RegisterProcessorFactory("MicroCompactProcessor",
        func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
            cfg, ok := config.(*MicroCompactProcessorConfig)
            if !ok {
                return nil, fmt.Errorf("MicroCompactProcessor: 配置类型不匹配，期望 *MicroCompactProcessorConfig，实际 %T", config)
            }
            mcp, err := NewMicroCompactProcessor(cfg)
            if err != nil {
                return nil, err
            }
            return mcp, nil
        },
    )
}
```

### 5. doc.go 更新

- `context_engine/doc.go`：processor/ 子目录树中添加 `micro_compact_processor.go` 条目
- `processor/doc.go`：同步添加条目
- 新增 `context_engine/context_utils/doc.go`

### 6. 日志

Python 源码无 logger 调用。按项目规则 3.4 补充防御性日志：
- `OnAddMessages` 执行清除后：`logger.Info(ComponentAgentCore)` 记录清除了多少条消息、涉及哪些工具名

## 测试策略

### context_utils/resolve_test.go

| 测试函数 | 场景 |
|----------|------|
| `TestExtractToolName` | Function.Name 有值 → 返回；为空但有 Name → 返回 Name；都为空 → 空字符串 |
| `TestResolveToolCallFromMessage` | 非 ToolMessage → nil；无 ToolCallID → nil；匹配到 → ToolCall；多条 AssistantMessage 从后匹配；未匹配 → nil |
| `TestResolveToolNameFromMessage` | 组合测试：回溯找到工具名；未找到 → 空字符串 |

### processor/micro_compact_processor_test.go

| 测试函数 | 场景 |
|----------|------|
| `TestNewMicroCompactProcessorConfig` | 默认值；Validate 通过；各种非法参数 → 错误 |
| `TestMicroCompactProcessor_ProcessorType` | 返回 `"MicroCompactProcessor"` |
| `TestMicroCompactProcessor_TriggerAddMessages` | 不构成 API round → false；构成但无超限 → false；超限 → true |
| `TestMicroCompactProcessor_OnAddMessages` | 无需清除 → 透传；有需清除 → content 替换为 marker；已是 marker 不重复替换；非 ToolMessage 跳过 |
| `TestMicroCompactProcessor_SaveLoadState` | SaveState 空 map；LoadState 空操作 |
| `TestCollectCompactableIndicesByTool` | 按工具名分组；只收集可压缩工具名；已清除跳过 |
| `TestHasAnyToolExceedThreshold` | 未超限 → false；等于阈值 → false；超过 → true |
| `TestCollectFlatIndicesForCompact` | 超限保留尾部；force=true 阈值降低；KeepRecentPerTool=0 全部清除 |

Mock：通过 fake ModelContext 实现，不需要 mock LLM。
