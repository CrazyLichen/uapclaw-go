# 5.29 ToolResultBudgetProcessor 设计文档

## 目标

实现 ToolResultBudgetProcessor（工具结果预算处理器），按对话轮次控制工具结果的 Token 预算。每轮内所有 ToolMessage 的 Token 总数超过阈值时，从最大的工具结果开始逐个卸载，直到该轮预算内。

对应 Python: `openjiuwen/core/context_engine/processor/offloader/tool_result_budget_processor.py`

## 架构

与 MessageOffloader / MessageSummaryOffloader 平级实现，嵌入 `*processor.BaseProcessor`，不继承其他 offloader。

```
processor/offloader/
├── doc.go                              # 包文档（更新）
├── message_offloader.go                # MessageOffloader
├── message_summary_offloader.go        # MessageSummaryOffloader
├── tool_result_budget_processor.go     # ToolResultBudgetProcessor ← 新增
└── tool_result_budget_processor_test.go # 测试 ← 新增
```

同时补充 `processor/round.go` 中的 `FindAllDialogueRound` 和 `processor/util.go` 中的 `EstimateMessageTokens`。

## 一、ToolResultBudgetProcessorConfig

```go
type ToolResultBudgetProcessorConfig struct {
    // ── 实际使用字段 ──

    // TokensThreshold 每轮工具结果 Token 预算（默认 50000）
    TokensThreshold int
    // LargeMessageThreshold 单条工具消息最小卸载大小（默认 10000）
    LargeMessageThreshold int
    // TrimSize 卸载后占位符保留的前 N 个字符（默认 3000）
    TrimSize int
    // ToolNameAllowlist 白名单工具名称列表（在名单内的工具结果永不卸载，默认 nil）
    ToolNameAllowlist []string
    // OffloadFilePrefix 卸载文件名前缀（默认 "ToolResultBudgetProcessor"）
    OffloadFilePrefix string

    // ── 兼容字段（不使用，保留用于序列化兼容）──

    // OffloadMessageTypes 兼容字段；仅支持 ["tool"]
    OffloadMessageTypes []string
    // MessagesThreshold 兼容字段；此处理器不使用消息数触发（0=未设置）
    MessagesThreshold int
    // MessagesToKeep 兼容字段；此处理器不按计数保留尾部（0=未设置）
    MessagesToKeep int
}
```

### 默认值（applyDefaults）

| 字段 | 默认值 |
|------|--------|
| TokensThreshold | 50000 |
| LargeMessageThreshold | 10000 |
| TrimSize | 3000 |
| ToolNameAllowlist | nil |
| OffloadFilePrefix | "ToolResultBudgetProcessor" |
| OffloadMessageTypes | ["tool"] |
| MessagesThreshold | 0（不启用） |
| MessagesToKeep | 0（不启用） |

### Validate

- TokensThreshold > 0
- LargeMessageThreshold > 0
- TrimSize > 0

## 二、ToolResultBudgetProcessor 结构体

```go
type ToolResultBudgetProcessor struct {
    *processor.BaseProcessor
    // config 具体配置
    config *ToolResultBudgetProcessorConfig
    // sysOperation 系统操作接口，通过 WithSysOperation 选项注入
    // ⤵️ 9.32 回填：将 any 替换为 SysOperation 接口类型
    sysOperation any
}

type ToolResultBudgetProcessorOption func(*ToolResultBudgetProcessor)

// WithSysOption 注入系统操作接口
// ⤵️ 9.32 回填：参数类型从 any 改为 SysOperation
func WithSysOption(op any) ToolResultBudgetProcessorOption
```

## 三、核心流程

### TriggerAddMessages

```
合并 contextMessages + messagesToAdd
→ FindAllDialogueRound(allMessages) 获取所有轮次
→ 遍历每个轮次：
    计算该轮内所有 ToolMessage 的 Token 总和（messageSize）
    → 总和 > TokensThreshold 且有候选消息 → return true
→ 所有轮次都不超预算 → return false
```

### OnAddMessages

```
合并 contextMessages + messagesToAdd，记录 contextSize
→ 遍历每个轮次：
    shrinkRoundToBudget(messages, roundRange, mc):
      while 轮内工具结果总Token > TokensThreshold:
        收集候选（ToolMessage + 非已卸载 + content.IsText() + 非allowlist + > LargeMessageThreshold）
        按大小降序排列 → 卸载最大的 → 替换原消息
→ 收集所有修改索引
→ mc.SetMessages(messages[:contextSize])
→ 返回 ContextEvent + messages[contextSize:]
```

## 四、shouldOffloadMessage 规则（5 条）

| # | 规则 | Python | Go |
|---|------|--------|-----|
| 1 | 必须是 ToolMessage | `isinstance(msg, ToolMessage)` | `msg.GetRole() == RoleTypeTool` |
| 2 | 不能是已卸载消息 | `isinstance(msg, OffloadToolMessage)` | `schema.IsOffloaded(msg)` |
| 3 | content 必须是纯文本 | `isinstance(content, str)` | `msg.GetContent().IsText()` |
| 4 | 不能是白名单工具 | `_is_allowlisted_tool_message()` | `isAllowlistedToolMessage()` |
| 5 | 消息大小 > LargeMessageThreshold | `_message_size() > threshold` | `messageSize() > threshold` |

**关于 content 判断**：Go 中 `GetContent()` 返回 `MessageContent` 结构体，有两种模式：
- 纯文本：`IsText()` = true，`Text()` 返回字符串
- 多模态：`IsText()` = false，`Parts()` 返回分片列表

Python 用 `isinstance(content, str)` 检查，Go 对应 `msg.GetContent().IsText()` 检查。只有纯文本的 ToolMessage 才能卸载，多模态内容不支持卸载。

## 五、卸载后占位符格式

### 常量

```go
const (
    PersistedOutputTag        = "<persisted-output>"
    PersistedOutputClosingTag = "</persisted-output>"
)
```

### 两阶段构建

**阶段 1 — 初始构建**（调用 OffloadMessages 前，handle 传 "pending"）：

```
<persisted-output>
Output too large (50000 bytes).
pending
Preview (first 3000 chars):
{前TrimSize字符的预览}
...
</persisted-output>
```

**阶段 2 — 最终替换**（OffloadMessages 返回后，用实际 handle 替换）：

```
<persisted-output>
Output too large (50000 bytes).
[[OFFLOAD: handle=abc123, type=filesystem, path=/workspace/context/.../ToolResultBudgetProcessor_abc123.json]]
Preview (first 3000 chars):
{前TrimSize字符的预览}
...
</persisted-output>
```

### buildPersistedOutputMessage

```go
func buildPersistedOutputMessage(originalSize int, offloadHandle string, preview string, hasMore bool) string
```

参数：
- `originalSize`：原始内容长度（字节数）
- `offloadHandle`：阶段 1 传 "pending"，阶段 2 传实际的 `[[OFFLOAD: handle=..., type=..., path=...]]`
- `preview`：前 TrimSize 字符的预览
- `hasMore`：原始内容是否超过 TrimSize（控制尾部是 `\n...\n` 还是 `\n`）

## 六、isAllowlistedToolMessage

```go
func (p *ToolResultBudgetProcessor) isAllowlistedToolMessage(
    message llm_schema.BaseMessage,
    contextMessages []llm_schema.BaseMessage,
) bool
```

- 通过 `processor.ResolveToolNameFromMessage(message, contextMessages)` 回溯工具名
- 检查工具名是否在 `config.ToolNameAllowlist` 集合中
- allowlist 为 nil 时返回 false（无保护）

与 MessageOffloader 的 `isProtectedToolMessage` 语义相同（在名单内不卸载），但命名和默认值不同。

## 七、offloadToolMessage

```go
func (p *ToolResultBudgetProcessor) offloadToolMessage(
    ctx context.Context,
    message llm_schema.BaseMessage,
    mc iface.ModelContext,
) (llm_schema.BaseMessage, error)
```

流程：
1. 获取 content 文本（`msg.GetContent().Text()`）
2. 生成 offloadHandle + offloadPath（`newOffloadHandleAndPath`）
3. 构建 preview（`content[:config.TrimSize]`）
4. 构建 persistedContent（`buildPersistedOutputMessage`，handle 传 "pending"）
5. 调用 `p.OffloadMessages(ctx, mc, "tool", persistedContent, ...)` 卸载
6. 从返回的 offloadMsg 提取实际 `offload_handle` 和 `offload_type`
7. 用实际 handle 重建 persistedContent（`buildPersistedOutputMessage`，handle 传实际的 `[[OFFLOAD: ...]]`）
8. 设置 offloadMsg 的 content 并返回

## 八、newOffloadHandleAndPath

```go
// ⤵️ 5.31 回填：mc.WorkspaceDir() 方法
func (p *ToolResultBudgetProcessor) newOffloadHandleAndPath(mc iface.ModelContext) (string, string)
```

- `offloadHandle`：`uuid.New().String()`
- `offloadPath`：`{workspaceDir}/context/{sessionID}_context/offload/{OffloadFilePrefix}_{handle}.json`
- `workspaceDir` 当前为空字符串，待 5.31 回填 `mc.WorkspaceDir()`

文件名使用 `OffloadFilePrefix`（默认 "ToolResultBudgetProcessor"），与 Python 的 `offload_file_prefix` 对齐。

## 九、messageSize

```go
func (p *ToolResultBudgetProcessor) messageSize(message llm_schema.BaseMessage, mc iface.ModelContext) int
```

1. 优先使用 `mc.TokenCounter().CountMessages([msg])`
2. 降级使用 `len(content) / 3`（与 MessageSummaryOffloader 一致）

## 十、processor 包新增函数

### round.go 新增

```go
// DialogueRound 对话轮次，[0]=userIdx, [1]=assistantIdx（nil 表示不完整轮次）
type DialogueRound [2]*int

// FindAllDialogueRound 查找所有对话轮次边界。
//
// 从后往前扫描消息列表，识别 user → assistant(无 tool_calls) 的轮次。
// 返回从新到旧排列的轮次列表。
// 连续的 user 消息被视为同组的起始。
// 不完整轮次（有 user 无 assistant）的 assistantIdx 为 nil。
//
// 对应 Python: ContextUtils.find_all_dialogue_round()
func FindAllDialogueRound(messages []llm_schema.BaseMessage) []DialogueRound
```

### util.go 新增

```go
// EstimateMessageTokens 估算单条消息的 Token 数。
//
// 优先使用 content 文本估算，为空时尝试 JSON 序列化后估算。
//
// 对应 Python: ContextUtils.estimate_message_tokens()
func EstimateMessageTokens(msg llm_schema.BaseMessage) int
```

## 十一、init() 自动注册

```go
func init() {
    context_engine.RegisterProcessorFactory("ToolResultBudgetProcessor",
        func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
            cfg, ok := config.(*ToolResultBudgetProcessorConfig)
            if !ok {
                return nil, fmt.Errorf("ToolResultBudgetProcessor: 配置类型不匹配")
            }
            return NewToolResultBudgetProcessor(cfg)
        },
    )
}
```

## 十二、回填标记保留

| 标记 | 位置 | 说明 |
|------|------|------|
| `⤵️ 5.31 回填` | `newOffloadHandleAndPath` | workspaceDir 当前为空字符串，待 mc.WorkspaceDir() |
| `⤵️ 5.31 回填` | BaseProcessor.offloadMessagesToMemory | mc.OffloadMessages() 调用待回填 |
| `⤵️ 9.32 回填` | sysOperation 字段 | any → SysOperation 接口类型 |
| `⤵️ 9.32 回填` | BaseProcessor.writeOffloadToFile | sysOperation 参数当前被忽略 |

## 十三、doc.go 更新

offloader/doc.go 文件目录新增：
```
//	├── tool_result_budget_processor.go     # ToolResultBudgetProcessor + Config
//	└── tool_result_budget_processor_test.go # ToolResultBudgetProcessor 单元测试
```

删除 "后续实现（5.29）" 段落，将 ToolResultBudgetProcessor 移入 "当前实现" 列表。

## 十四、测试策略

### 测试文件

`processor/offloader/tool_result_budget_processor_test.go`

### 测试用例

| 类别 | 用例 | 覆盖 |
|------|------|------|
| Config 验证 | TestToolResultBudgetProcessorConfig | 默认值、自定义值、Validate 边界 |
| Trigger | TestTriggerAddMessages_低于阈值不触发 | 所有轮次都不超预算 |
| Trigger | TestTriggerAddMessages_超过阈值触发 | 存在轮次超预算且有候选 |
| Trigger | TestTriggerAddMessages_超预算但无候选不触发 | 超预算但无 ToolMessage |
| OnAdd | TestOnAddMessages_单轮超预算卸载 | 单轮卸载最大的工具结果 |
| OnAdd | TestOnAddMessages_多轮独立处理 | 多轮各自独立计算预算 |
| OnAdd | TestOnAddMessages_无修改返回nil | 未触发时返回 nil |
| shouldOffload | TestShouldOffloadMessage_五条规则 | 逐条验证 5 条规则 |
| allowlist | TestIsAllowlistedToolMessage | 白名单内不卸载、非白名单可卸载 |
| alreadyOffloaded | TestIsAlreadyOffloaded | OffloadToolMessage 不重复卸载 |
| buildPersisted | TestBuildPersistedOutputMessage | 标签格式、字节数、preview、has_more |
| offload | TestOffloadToolMessage | 完整卸载流程、handle 替换 |
| FindAllDialogueRound | TestFindAllDialogueRound | 单轮、多轮、不完整轮次、连续 user 消息组 |
| SaveState/LoadState | TestSaveState_TestLoadState | 空操作验证 |
| ProcessorType | TestProcessorType | 返回 "ToolResultBudgetProcessor" |

### 覆盖率目标

≥ 85%

## 十五、不包含在本次实现中的内容

- `FindLastNDialogueRound`（5.31 Context 门面需要时再实现）
- WorkspaceDir / OffloadMessages 实际调用（5.31 回填）
- SysOperation 实际调用（9.32 回填）
- Rail 集成（6.x 领域六实现后）
