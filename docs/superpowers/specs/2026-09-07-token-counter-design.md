# 5.20 TokenCounter (Tiktoken Go 实现) 设计

## 概述

实现 `TiktokenCounter`，为上下文引擎提供精确的 Token 计数能力。这是 5.21-5.31 所有后续步骤的前置依赖——没有它，整个上下文压缩/卸载链无法判断何时该压缩、压缩多少。

### 在 Agent 会话流程中的位置

```
Agent.run()
  ↓
Session 创建 (5.2-5.5)
  ↓
ContextEngine.create_context()  ←── 5.30 门面
  ↓
SessionModelContext 初始化 (5.31)
  ├── token_counter = NewTiktokenCounter("gpt-4")  ←── ★ 5.20
  ├── ProcessorStateRecorder(token_counter)
  ↓
ReAct 循环
  ├── GetContextWindow() → StatMessages/StatTools（依赖 tokenCounter）
  ├── Processor 链 (5.21-5.29) ←── 全部依赖 tokenCounter
```

### 回填依赖链

```
5.20 TokenCounter 实现
  ↓
5.31 Context 实现 ← 回填 StatMessages/StatTools/StatContextWindow
  ↓
5.16 ContextStats ← 统计计算逻辑真正可用
  ↓
5.21-5.29 Processor 链 ← 所有压缩/卸载处理器依赖 token 计数
  ↓
5.30 ContextEngine 门面 ← 组装一切
```

## 技术选型

| 决策 | 选择 | 理由 |
|------|------|------|
| tiktoken 库 | `tiktoken-go/tokenizer` | 纯 Go 实现，BPE 编译期嵌入，无需运行时下载 |
| 文件组织 | 单文件 `tiktoken_counter.go` | 与 Python 一一对应，逻辑紧凑 |
| ToolCalls 处理 | 对齐 Python：AssistantMessage 额外序列化 ToolCalls | 精度最高，完全对齐 Python |
| 降级策略 | 两级降级（对齐 Python） | 防御性设计，未来换库也安全 |
| 构造函数 | 构造时选定 encoding | 对齐 Python，简单、线程安全 |

## 结构体设计

```go
// TiktokenCounter 基于 tiktoken-go/tokenizer 的 Token 计数器。
//
// 对应 Python: openjiuwen/core/context_engine/token/tiktoken_counter.py (TiktokenCounter)
type TiktokenCounter struct {
    // enc tiktoken 编码器实例，初始化失败时为 nil
    enc *tokenizer.Tokenizer
    // model 构造时指定的模型名称
    model string
    // fallbackWarned 是否已输出降级警告（只警告一次）
    fallbackWarned bool
    // mu 保护 fallbackWarned 的互斥锁
    mu sync.Mutex
}
```

### 构造函数

```go
// NewTiktokenCounter 创建 TiktokenCounter 实例。
// model 为模型名称，用于选择对应的 encoding；空字符串默认使用 "gpt-4"。
// 初始化失败时 enc 为 nil，后续计数降级为 len(text)//4。
func NewTiktokenCounter(model string) *TiktokenCounter
```

- `model` 为空时默认 `"gpt-4"`
- 通过 `modelToEncoding(model)` 查映射表获取 encoding name
- 用 `tokenizer.New(encodingName)` 创建编码器，失败则 `enc = nil` + 打印警告日志

## 核心方法

### Count(text, model)

1. `enc != nil` → `enc.Encode(text, nil, nil)` 返回 token 数
2. 编码失败 → 打印警告日志 + `len(text)//4`
3. `enc == nil` → `fallbackCount(text)`（只警告一次）

### CountMessages(messages, model)

1. 空列表 → 返回 0
2. 遍历消息，格式化为 `<|start|>{role}\n{content}<|end|>` 后调用 `Count`
3. AssistantMessage 特殊处理：额外 `json.Marshal(asst.ToolCalls)` 计入 token
4. 多模态内容通过 `contentToString()` 转换为字符串
5. 末尾 +3 tokens（对齐 OpenAI 惯例）

### CountTools(tools, model)

1. 空列表 → 返回 0
2. 遍历工具，构造 `{name, description, parameters}` JSON 对象
3. 格式化为 `<|start|>functions.{name}:{idx}\n{json}<|end|>` 后调用 `Count`
4. 末尾 +3 tokens

## 辅助函数

### model2enc 映射表

对齐 Python `TiktokenCounter._MODEL2ENC` 的 7 条映射，未知模型默认 `"cl100k_base"`：

| 模型 | 编码 |
|------|------|
| gpt-3.5-turbo | cl100k_base |
| gpt-4 | cl100k_base |
| gpt-4-turbo | cl100k_base |
| gpt-4o | o200k_base |
| gpt-4o-mini | o200k_base |
| text-embedding-ada-002 | cl100k_base |
| text-embedding-3-small | cl100k_base |
| text-embedding-3-large | cl100k_base |

### fallbackCount

`len(text)//4` 降级计算，通过 `fallbackWarned` + `sync.Mutex` 保证只警告一次。

### contentToString

`MessageContent` 是结构体（非 `any`），已有 `IsText()`/`Text()`/`Parts()`/`String()` 方法：

- `IsText() == true` → 直接使用 `content.Text()`
- `IsText() == false` → 遍历 `content.Parts()`，提取 `Type == "text"` 的分片文本拼接（忽略 image_url 等非文本分片的 token，因为它们在 LLM 端有独立的 token 计算规则）

注：不直接使用 `content.String()`，因为多模态模式下它会 `json.Marshal(parts)`，会将 image_url 等 JSON 结构也计入 token，与 Python 的行为不一致（Python 对 list content 只提取 text 部分）。

## 日志

```go
const logComponent = logger.ComponentAgentCore
```

日志点对齐 Python：
- 初始化失败 → `logger.Warn`（一次性）
- 运行时编码失败 → `logger.Warn`（每次）
- 日志字段包含 `model`、`err` 等上下文

## 回填边界

5.20 **只回填** `token/doc.go`（添加 `tiktoken_counter.go` 条目，更新包功能概述）。

**不触碰**以下已有代码中的 `⤵️` 标记：
- `context_engine/base.go` 的 `StatMessages/StatTools/StatContextWindow` → 待 5.31 回填
- `schema/config.go` 的 `EnableTiktokenCounter` → 待 5.30/5.31 消费

## go.mod 变更

- 添加 `github.com/tiktoken-go/tokenizer` 依赖
- 移除 go.mod 注释中的 `pkoukk/tiktoken-go` 占位

## 测试策略

测试文件：`token/tiktoken_counter_test.go`

| 测试用例 | 说明 |
|---------|------|
| `TestNewTiktokenCounter_默认模型` | 验证 model="" 时使用 "gpt-4" → "cl100k_base" |
| `TestNewTiktokenCounter_GPT4o` | 验证 "gpt-4o" → "o200k_base" |
| `TestNewTiktokenCounter_未知模型` | 验证未知模型降级到 "cl100k_base" |
| `TestCount_纯文本` | 验证英文/中文/混合文本的 token 计数 |
| `TestCount_空字符串` | 验证返回 0 |
| `TestCountMessages_多角色` | 验证 system/user/assistant/tool 消息格式化后计数 |
| `TestCountMessages_AssistantToolCalls` | 验证 AssistantMessage 带 ToolCalls 时额外计数 |
| `TestCountMessages_空列表` | 验证返回 0 |
| `TestCountTools_多个工具` | 验证 tools 按 functions.{name}:{idx} 格式计数 |
| `TestCountTools_空列表` | 验证返回 0 |
| `TestCountTools_Parameters为空` | 验证 parameters 为 nil 时的处理 |
| `TestModelToEncoding_所有映射` | 遍历 model2enc 映射表验证每个映射正确 |
| `TestContentToString_字符串` | 验证 string 类型直接返回 |
| `TestContentToString_ContentPart` | 验证 []ContentPart 提取 text 拼接 |

覆盖率目标：≥ 85%。`tiktoken-go/tokenizer` 是纯 Go 编译期嵌入，无需 build tag 隔离。

## 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `token/tiktoken_counter.go` | 新增 | TiktokenCounter 结构体 + 所有方法 + 辅助函数 |
| `token/tiktoken_counter_test.go` | 新增 | 单元测试 |
| `token/doc.go` | 更新 | 添加 tiktoken_counter.go 条目，更新包概述 |
| `go.mod` | 更新 | 添加 tiktoken-go/tokenizer 依赖 |
| `go.sum` | 自动更新 | go mod tidy |
