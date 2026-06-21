# 5.27 MessageOffloader 实现设计

## 概述

MessageOffloader 是上下文引擎处理器链中的"安全阀"：当对话消息数/Token 数超过阈值时，将大消息的内容裁剪并卸载到文件系统或内存，生成轻量占位符。原始内容可通过 `reloader_tool` 按 `offload_handle` 取回。

## 流程位置

```
Agent 会话流程:
  用户输入 → Session 接收消息 → Context Engine 处理器链执行
    → [5.22-5.26 RoundLevelCompressor 等压缩器] ← 先尝试压缩
    → [5.27 MessageOffloader]                     ← 压缩不够时，卸载大消息
    → [5.28 MessageSummaryOffloader]               ← LLM 摘要式卸载（继承自 5.27）
    → [5.29 ToolResultBudgetProcessor]             ← 工具结果预算控制（继承自 5.27）
    → 消息进入 LLM 调用
```

与 5.28/5.29 的继承关系：

```
BaseProcessor (processor/base.go)
  └── MessageOffloader (5.27) — 基础裁剪卸载
        ├── MessageSummaryOffloader (5.28) — 用 LLM 生成摘要替代裁剪
        └── ToolResultBudgetProcessor (5.29) — 按轮次控制工具结果 Token 预算
```

## 架构变更

### 1. 将 `compressor/util.go` 迁移到 `processor/util.go`

**原因**：offloader 需要的工具方法（`ResolveToolCallFromMessage`、`ExtractToolName`、`FindLastFinalAssistantIdx` 等）目前在 `compressor` 子包中，offloader 作为同级子包无法访问。这些方法本质上是 `ContextUtils` 的 Go 等价物，属于 `processor` 层级更合理（Python 中它们在 `ContextUtils` 中，不属于任何子包）。

**迁移内容**：

| 方法 | 说明 |
|------|------|
| `ExtractToolName` | 从 ToolCall 提取工具名称 |
| `ResolveToolCallFromMessage` | 从 ToolMessage 回溯查找 ToolCall |
| `ResolveToolNameFromMessage` | 从 ToolMessage 回溯查找工具名称 |
| `MessageToText` | 提取消息纯文本 |
| `GroupCompletedAPIRoundsMessages` | 按已完成 API 轮次分组返回消息子列表 |
| `MessageSignature` | 生成消息签名（用于去重） |
| `RoundSignature` | 生成轮次签名 |
| `FlattenGroups` | 展平消息分组 |
| `IsSkillFilePath` | 判断是否为 skill 文件路径 |
| `ExtractArgumentValue` | 从 JSON 参数提取指定 key 的值 |
| `RoundContainsSkillRead` | 检查轮次是否包含 skill 文件读取 |
| `EstimateContentTokens` | 估算内容 Token 数 |
| `IsSummaryMessage` | 判断是否为摘要消息 |
| `CollectSummaryIndices` | 收集摘要消息索引 |
| `CountMessagesTokens` | 计算 Token 数 |
| `FindLastCompletedAPIRoundEndIdx` | 找到范围内最后一个完整 API 轮次结束索引 |
| `IterSummaryMergeRanges` | 返回连续摘要消息范围 |
| `ParseToolArguments` | 解析工具调用 JSON 参数 |
| `DescribeToolCall` | 生成工具调用可读描述 |
| `FindToolResultText` | 根据 toolCallID 查找工具结果文本 |
| `ExtractToolResultHint` | 提取工具结果简要提示 |
| `ExtractSkillNameFromPath` | 从路径提取 skill 名称 |
| `ExtractSkillFileContent` | 提取 skill 文件内容 |
| `FindLastFinalAssistantIdx` | 从 dialogue_compressor.go 提取到此 |

同时迁移 `util_test.go` 到 `processor/util_test.go`，更新 compressor 中的调用为 `processor.XXX()`。

### 2. 新增 `processor/offloader/` 子包

```
processor/offloader/
├── doc.go                    # 包文档
├── message_offloader.go      # MessageOffloader + Config
└── message_offloader_test.go # 单元测试
```

## MessageOffloaderConfig

```go
type MessageOffloaderConfig struct {
    MessagesThreshold     *int     // 消息数触发阈值，nil 表示不启用
    TokensThreshold       int      // Token 数触发阈值（默认 20000）
    LargeMessageThreshold int      // 大消息判定阈值（默认 1000）
    OffloadMessageTypes   []string // 可卸载的消息角色（默认 ["tool"]）
    ProtectedToolNames    []string // 受保护的工具名称（默认 ["reload_original_context_messages"]）
    TrimSize              int      // 裁剪保留 token 数（默认 100）
    MessagesToKeep        *int     // 保留最近 N 条，nil 表示不保留
    KeepLastRound         bool     // 保留最后一轮（默认 true）
}
```

交叉校验（Validate 方法）：
- `TrimSize < LargeMessageThreshold`
- `MessagesToKeep < MessagesThreshold`（两者均非 nil 时）

可选字段用 `*int` 指针表示，nil 表示未设置。这与 Python 的 `int | None = None` 语义对齐。

## MessageOffloader 核心方法

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `ProcessorType()` | `processor_type()` | 返回 `"MessageOffloader"` |
| `TriggerAddMessages()` | `trigger_add_messages()` | 双阈值触发 + 候选检查 |
| `OnAddMessages()` | `on_add_messages()` | 执行卸载，替换大消息 |
| `SaveState()` / `LoadState()` | `save_state()` / `load_state()` | 空操作 |
| `offloadLargeMessages()` | `_offload_large_messages()` | 遍历卸载范围，逐条卸载 |
| `offloadMessage()` | `_offload_message()` | 裁剪内容 + 调用 `BaseProcessor.OffloadMessages` |
| `newOffloadHandleAndPath()` | `_new_offload_handle_and_path()` | 生成 UUID + 文件路径 |
| `getOffloadRange()` | `_get_offload_range()` | 计算卸载范围 |
| `shouldOffloadMessage()` | `_should_offload_message()` | 5 条规则判定 |
| `isProtectedToolMessage()` | `_is_protected_tool_message()` | 受保护工具检查 |
| `hasOffloadCandidate()` | `_has_offload_candidate()` | 候选消息检查 |
| `extractToolArgs()` | `_extract_tool_args()` | 从 ToolCall 提取参数 dict |
| `matchPattern()` | `_match_pattern()` | `filepath.Match` 通配符匹配 |

### TriggerAddMessages 逻辑

1. 若 `MessagesToKeep != nil && 总消息数 <= MessagesToKeep` → `false`
2. 若 `MessagesThreshold != nil && 总消息数 > MessagesThreshold` → 检查候选 → `true/false`
3. 计算 Token 数 → 若 `tokens > TokensThreshold` → 检查候选 → `true/false`
4. 默认 `false`

### shouldOffloadMessage 5 条规则

1. 消息角色不在 `OffloadMessageTypes` 中 → `false`
2. 消息 content 不是 string → `false`
3. 消息 content 长度 <= `LargeMessageThreshold` → `false`
4. 消息已是 OffloadMixin（`schema.IsOffloaded`）→ `false`
5. 消息是受保护工具的结果（`isProtectedToolMessage`）→ `false`

通过所有检查 → `true`

### isProtectedToolMessage 逻辑

1. 消息不是 `*ToolMessage` → `false`
2. 回溯查找 `ToolCall`（`processor.ResolveToolCallFromMessage`）
3. 提取工具名（`processor.ExtractToolName`）和参数（`extractToolArgs`）
4. 遍历 `ProtectedToolNames`：
   - `"tool_name:pattern"` 格式 → 匹配工具名 + `filepath.Match` 匹配参数值
   - `"tool_name"` 格式 → 仅匹配工具名

### 注册机制

```go
func init() {
    context_engine.RegisterProcessorFactory("MessageOffloader",
        func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
            cfg, ok := config.(*MessageOffloaderConfig)
            if !ok {
                return nil, fmt.Errorf(...)
            }
            return NewMessageOffloader(cfg)
        },
    )
}
```

## 依赖关系与回填点

### 已可用的依赖（迁移 util.go 后）

- `processor.ResolveToolCallFromMessage` ✓
- `processor.ExtractToolName` ✓
- `processor.FindLastFinalAssistantIdx` ✓
- `processor.ReplaceMessages` ✓
- `BaseProcessor.OffloadMessages` ✓
- `schema.NewOffloadMessage` / `schema.IsOffloaded` ✓
- `processor.CountMessagesTokens` ✓

### ⤵️ 回填点（占位对齐策略）

| 回填点 | 目标步骤 | 当前处理 |
|--------|---------|---------|
| `mc.OffloadMessages(handle, messages)` | 5.31 | 注释占位，待 ModelContext 补充方法后回填 |
| `mc.WorkspaceDir()` | 5.31 | `newOffloadHandleAndPath` 中留 TODO，当前使用空字符串 |
| `SysOperation` 类型 | 9.32 | 沿用 `any` 类型，待 9.32 替换为接口类型 |

## 测试策略

### 单元测试覆盖

1. **MessageOffloaderConfig.Validate**：
   - trim_size >= large_message_threshold 报错
   - messages_to_keep >= messages_threshold 报错
   - 正常配置通过

2. **TriggerAddMessages**：
   - 消息数未达阈值 → false
   - 消息数超阈值但无候选 → false
   - 消息数超阈值有候选 → true
   - Token 数超阈值有候选 → true
   - MessagesToKeep 保护 → false

3. **OnAddMessages**：
   - 无需卸载 → 透传
   - 有大消息需卸载 → 替换为 OffloadMessage
   - 已卸载消息不重复卸载
   - 受保护工具消息不卸载

4. **shouldOffloadMessage**：
   - 5 条规则各自独立测试

5. **isProtectedToolMessage**：
   - 非 ToolMessage → false
   - 工具名精确匹配 → true
   - "tool_name:pattern" 格式 + filepath.Match → true
   - 不匹配 → false

6. **getOffloadRange**：
   - keepLastRound=true → 受最后 AI 消息位置约束
   - keepLastRound=false → 仅受 MessagesToKeep 约束

7. **matchPattern**：
   - 精确匹配、通配符匹配、不匹配

8. **extractToolArgs**：
   - dict 格式、string JSON 格式、属性访问格式

### Mock 策略

- `ModelContext` 接口用 fake 实现
- `TokenCounter` 用 fake 返回固定值
- 不依赖真实文件系统，offload 写入用 `t.TempDir()`
