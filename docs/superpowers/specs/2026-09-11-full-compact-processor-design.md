# FullCompactProcessor 设计文档

## 概述

FullCompactProcessor 是上下文引擎处理器链中的"最后防线"——当其他更轻量的处理器都无法将上下文降到安全阈值时，FullCompact 启动全量压缩：将历史对话整体替换为 LLM 生成的摘要，仅保留最近 N 条消息原文。

**实现计划编号**：5.24
**对应 Python 代码**：`openjiuwen/core/context_engine/processor/compressor/full_compact_processor.py` + `util.py`

## 流程位置与作用

在 Context Engine 处理器链中，各处理器按触发阈值从低到高渐进式介入：

```
MicroCompactProcessor    (5.23 ✅)  — 单工具结果清理
    ↓
DialogueCompressor       (5.22 ✅)  — 对话轮次级压缩
    ↓
CurrentRoundCompressor   (5.25 ☐)  — 当轮增量压缩
    ↓
FullCompactProcessor     (5.24 ☐)  — ★ 全量压缩，最后防线 ★
    ↓
RoundLevelCompressor     (5.26 ☐)  — 轮级递归压缩（最激进）
```

核心作用：
- **全量摘要**：将大量历史消息压缩为一条摘要 SystemMessage，释放大量 token 空间
- **Session Memory 优先路径**：若 session memory 可用，优先使用内存文件而非 LLM 调用，节省成本
- **状态重注入**：压缩后通过 FullCompactStateReinjector 重注入关键状态（skills/task_status/plan_mode），防止丢失上下文
- **边界标记**：用 `[FULL_COMPACT_BOUNDARY]` 标记压缩边界，后续压缩可增量操作

## 文件结构与迁移

### 新建目录结构

```
processor/
├── doc.go                          # 更新文件目录
├── base.go                         # BaseProcessor（不动）
├── hooks.go                        # 钩子默认实现（不动）
├── state.go                        # SaveState/LoadState（不动）
├── offload.go                      # OffloadMessages（不动）
├── usage.go                        # CompressionUsage（不动）
├── round.go                        # GroupCompletedAPIRounds（不动）
├── replace.go                      # Replacement + ReplaceMessages（不动）
├── compressor/
│   ├── doc.go                      # 子包文档
│   ├── util.go                     # 包级共享函数（从 context_utils 迁移 + 新增）
│   ├── dialogue_compressor.go      # 从 processor/ 迁移（含自己的提示词）
│   ├── micro_compact_processor.go  # 从 processor/ 迁移
│   └── full_compact_processor.go   # 新增：FullCompactProcessor 全部代码（含 Config/处理器/内部方法/专用提示词常量/Reinjector+4个Builder）
```

### 迁移动作

1. `dialogue_compressor.go` 从 `processor/` → `compressor/`
2. `micro_compact_processor.go` 从 `processor/` → `compressor/`
3. `context_utils/` 整个目录删除，3 个函数移入 `compressor/util.go`
4. 所有 import 路径更新
5. init() 注册入口仍在各处理器文件中，通过 import 触发

### 删除

- `context_utils/` 目录（resolve.go + doc.go + resolve_test.go）

### 不动

- `processor/` 下的基类基础设施（base/hooks/state/offload/usage/round/replace），offloader 子包（5.27-5.29）也要用

## 核心结构体

### FullCompactProcessorConfig

```go
type FullCompactProcessorConfig struct {
    TriggerTotalTokens          int      // 触发阈值，默认 180000
    CompressionCallMaxTokens    int      // 压缩调用最大 token 预算，默认 200000
    MessagesToKeep              int      // 保留最近 N 条消息原文，默认 10
    SessionMemoryEnabled        bool     // 优先使用 session memory，默认 true
    Model                       string   // LLM 模型名
    ModelClient                 string   // LLM 客户端标识
    KeepToolMessagePairs        bool     // 保留 tool result 时也保留匹配的 assistant tool-call，默认 true
    StateSnapshotMaxChars       int      // 每个状态重注入快照最大字符数，默认 4000
    ReinjectRecentSkills        int      // 重注入最近 N 个 skill 读取轮次，默认 3
    ReinjectFileToolNames       []string // 文件相关状态重注入工具名
    ReinjectToolResultHintNames []string // 工具结果提示工具名
    Marker                      string   // 默认 "[FULL_COMPACT_BOUNDARY]"
    StateMarker                 string   // 默认 "[FULL_COMPACT_STATE]"
    SyntheticUserMarker         string   // 默认 "[earlier conversation truncated for compaction retry]"
    SummaryIntro                string   // 摘要前缀
    SessionMemoryMarker         string   // 默认 "[SESSION_MEMORY_BOUNDARY]"
    SessionMemoryIntro          string   // session memory 前缀
}
```

### FullCompactProcessor

```go
type FullCompactProcessor struct {
    *processor.BaseProcessor
    model *llm.Model
}
```

## 双路径流程

### 触发条件

```
TriggerAddMessages(ctx, mc, messages)
    ├─ 条件1: IsAPIRound(messages) == false → return false
    └─ 条件2: CountContextWindowTokens > TriggerTotalTokens → return true
```

### 主入口

```
OnAddMessages(ctx, mc, messages)
    └─ _buildReplacementMessages(ctx, mc, allMessages)
         │
         ├─ 1. 查找上次压缩边界 marker，分割为 prefix + activeMessages
         │
         ├─ 2. Session Memory 路径（优先）
         │   └─ _buildSessionMemoryMessages(ctx, mc, prefix, activeMessages, hasBoundary)
         │       ├─ _loadSessionMemoryRuntime(ctx, mc)          // ⤵️ 5.31 回填
         │       ├─ _loadSessionMemoryText(ctx, mc, runtime)    // ⤵️ 5.31 回填
         │       ├─ _selectMessagesAfterSessionMemory(...)       // ⤵️ 5.31 回填
         │       └─ 构建: prefix + sessionMemoryBoundary + sessionMemoryMsg + kept + reinjectedState
         │
         └─ 3. LLM 全量压缩路径（回退）
             └─ _buildFullCompactMessages(ctx, mc, prefix, activeMessages)
                 ├─ 准备消息（去除 boundary/state/sessionMemory 消息）
                 ├─ _truncateForPromptBudget(messages, ctx)
                 ├─ _generateSummary(messages, ctx)
                 ├─ _selectMessagesToKeep(messages)
                 ├─ buildReinjectedStateMessages(...)
                 └─ 构建: prefix + boundary + summary + kept + reinjectedState
```

## FullCompactStateReinjector 与 4 个 Builder

### 注册表设计

```go
type ReinjectedStateBuilderSpec struct {
    Name    string
    Label   string
    Builder func(ctx context.Context, mc iface.ModelContext,
                messages []llm_schema.BaseMessage,
                messagesToKeep []llm_schema.BaseMessage) any
}

type FullCompactStateReinjector struct {
    builders []ReinjectedStateBuilderSpec
}

func (r *FullCompactStateReinjector) RegisterBuilder(name, label string, builder BuilderFunc)
func (r *FullCompactStateReinjector) IterBuilders() []ReinjectedStateBuilderSpec
```

### 全局单例 + 4 个 Builder

```go
var defaultReinjector = &FullCompactStateReinjector{}

func init() {
    defaultReinjector.RegisterBuilder("skills", "Skills Context", buildSkillReinjectedContent)
    defaultReinjector.RegisterBuilder("task_status", "Task Status", buildTaskStatusReinjectedContent)
    defaultReinjector.RegisterBuilder("plan_mode", "Plan Mode", buildPlanModeReinjectedContent)
    defaultReinjector.RegisterBuilder("plan", "Plan", buildPlanReinjectedContent)
}
```

### 4 个 Builder 详情

| Builder | 输入 | 输出 | 依赖 | ⤵️ 回填 |
|---------|------|------|------|---------|
| skills | messages, messagesToKeep | `[]BaseMessage` | 遍历已完成 API round，找含 skill 文件读取的轮次，提取 skill 内容 | 无 |
| task_status | context session 状态 | `string` | `context.GetSessionRef().DumpState()` → 读取 task_state | ⤵️ 5.31 |
| plan_mode | context session 状态 | `string` | `context.GetSessionRef().DumpState()` → 读取 plan_mode | ⤵️ 5.31 |
| plan | — | `""` | 空实现 | 无 |

### skills builder 核心逻辑

1. GroupCompletedAPIRounds(sourceMessages) → 按 API round 分组
2. 从后向前遍历，找含 skill 文件读取的轮次（读 read_file/glob 等工具调用 skill.md 文件）
3. 最多取 ReinjectRecentSkills 个轮次
4. 对每个轮次：提取 skill 名称 + skill 文件内容 + 工具调用描述
5. 包装为 UserMessage 列表返回

## 消息识别、截断与辅助方法

### 消息识别方法族

| 方法 | 判断逻辑 |
|------|---------|
| `_isBoundaryMessage` | SystemMessage 且 Content 以 Marker 开头 |
| `_isStateMessage` | UserMessage 且 Content 以 StateMarker 开头 |
| `_isSessionMemoryBoundaryMessage` | SystemMessage 且 Content 以 SessionMemoryMarker 开头 |
| `_isSessionMemorySummaryMessage` | UserMessage 且 Content 以 SessionMemoryIntro 开头 |
| `_isSyntheticMarkerMessage` | UserMessage 且 Content == SyntheticUserMarker |

### 消息截断与选择

| 方法 | 说明 |
|------|------|
| `_selectMessagesToKeep` | 保留最近 MessagesToKeep 条，含 keepToolMessagePairs 调整 |
| `_adjustStartIndexForToolPairs` | 向前扩展保留范围，确保不在 AssistantMessage(tool_calls) 和对应 ToolMessage 之间切割 |
| `_truncateForPromptBudget` | 按 API round 分组，从前往后丢弃直到满足 CompressionCallMaxTokens 预算 |

### 序列化

FullCompactProcessor 各自实现序列化（不复用 DialogueCompressor 的简化版），因为压缩提示词需要 LLM 看到完整的工具调用细节（id/arguments/type 等）。

| 方法 | 说明 |
|------|------|
| `_serializeMessage` | 完整序列化：role + tool_calls JSON（含 id/name/arguments/type）+ tool_call_id + content |
| `_serializeMessages` | 逐条调用 `_serializeMessage`，换行连接 |
| `_formatSummary` | 从 LLM 输出中提取 `<summary>` 标签内容，无标签则返回原文 |
| `_buildFallbackSummary` | 降级摘要：最近 20 条消息序列化 |
| `_makeStateMessage` | 构建状态重注入 UserMessage |
| `truncateStateText` | 截断超长状态文本到 StateSnapshotMaxChars |

### 摘要生成

| 方法 | 说明 |
|------|------|
| `_generateSummary` | 调用 LLM（BASE_COMPACT_PROMPT）→ 提取 `<summary>` → 记录 CompressionUsage → 失败回退 `_buildFallbackSummary` |
| `_buildFullCompactMessages` | LLM 全量压缩路径完整流程 |

### 提示词

BASE_COMPACT_PROMPT 完整翻译自 Python 版，包含：
- NO_TOOLS_PREAMBLE 前缀（禁止调用工具）
- `<analysis>` → `<summary>` 结构要求
- 9 个固定章节：Primary Request / Key Technical Concepts / Files and Code Sections / Errors and Fixes / Problem Solving / All User Messages / Pending Tasks / Current Work / Optional Next Step

## ⤵️ 回填标记完整清单

| 位置 | 回填目标 | 内容 |
|------|---------|------|
| `_loadSessionMemoryRuntime` | 5.31 | ModelContext 需暴露 session 引用，读取 `__session_memory__` 状态 |
| `_loadSessionMemoryText` | 5.31 | 读取 session memory 文件内容 |
| `_resolveSessionMemoryPath` | 5.31 | 解析 session memory 文件路径 |
| `_selectMessagesAfterSessionMemory` | 5.31 | 根据 `notes_upto_message_id` 锚点筛选消息 |
| `_invalidateSessionMemoryAnchor` | 5.31 | 更新 session memory 运行时状态 |
| `buildTaskStatusReinjectedContent` | 5.31 | 需要 `context.GetSessionRef().DumpState()` 读取 task_state |
| `buildPlanModeReinjectedContent` | 5.31 | 需要 `context.GetSessionRef().DumpState()` 读取 plan_mode |

回填函数内部用注释标注：`// ⤵️ 5.31 回填：待 ModelContext 暴露 GetSessionRef() 后实现`，函数体返回零值/空值，确保编译通过且不影响 LLM 路径的正常运行。

## 测试策略

| 测试类别 | 覆盖范围 | mock 方式 |
|---------|---------|----------|
| Config Validate | 字段校验 | 无需 mock |
| TriggerAddMessages | 触发条件判断 | mock ModelContext.TokenCounter() |
| LLM 路径 - 正常压缩 | `_generateSummary` / `_buildFullCompactMessages` | fake LLM Model 返回含 `<summary>` 的响应 |
| LLM 路径 - fallback | 无 model 配置 / LLM 调用失败 | fake Model 返回 error |
| Session Memory 路径 | `_buildSessionMemoryMessages` | mock ModelContext，⤵️ 标注待 5.31 回填后补充集成测试 |
| 消息识别 | `_isBoundaryMessage` 等 5 个方法 | 直接构造消息测试 |
| 消息选择与截断 | `_selectMessagesToKeep` / `_truncateForPromptBudget` / `_adjustStartIndexForToolPairs` | 直接构造消息列表测试 |
| 状态重注入 | `buildReinjectedStateMessages` + 4 个 builder | skills builder 用真实消息测试；task_status/plan_mode mock session 状态 |
| 序列化 | `_serializeMessage` / `_serializeMessages` / `_formatSummary` | 直接测试 |
| 边界标记 | boundary/state/synthetic 消息的生成与识别 | 直接测试 |

## 迁移影响范围

| 动作 | 影响 |
|------|------|
| `dialogue_compressor.go` 移入 `compressor/` | import 路径变更，包名变更 |
| `micro_compact_processor.go` 移入 `compressor/` | import 路径变更，包名变更 |
| `context_utils/` 删除 | import 路径变更，函数移入 `compressor/util.go` |
| `processor/doc.go` | 更新文件目录 |
| `processor/base.go` 等 | 不动，但 `compressor/` 需 import `processor` 包 |
