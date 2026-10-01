# 7.18 LongTermMemoryExtractor 实现设计

## 概述

实现 `LongTermMemoryExtractor`——通过 LLM 从对话中提取长期记忆碎片（用户画像、语义记忆、情景记忆）。严格对齐 Python `openjiuwen/core/memory/process/extract/` 目录结构，本次实现范围仅 7.18，同时附带实现必需的 `MemoryScopeConfig` 配置类型。

## 在 Agent 会话中的流程位置

```
用户消息 → Agent ReAct 循环 → 对话完成
                                    │
                                    ▼
              LongTermMemory.add_messages()    ← 7.27 (尚未实现)
                    │
                    ▼
              Generator.gen_all_memory()       ← 编排器（7.19+ 实现）
                    │
                    ├──→ MemoryAnalyzer.analyze()           ← 7.19
                    │    返回: {has_key_information, variables[], summary}
                    │
                    ├──→ LongTermMemoryExtractor.extract()  ← ★ 7.18 (本次)
                    │    返回: {has_explict_instruct, instruct_memories[],
                    │           user_profile[], semantic_memory[], episodic_memory[]}
                    │
                    ├──→ _handle_memory_with_instruct()     ← 7.19+
                    └──→ _get_fragment_memory_unit()        ← 7.19+
                              │
                              ▼
              WriteManager.add_memories()       ← 7.8 (已实现)
```

**作用**：LongTermMemoryExtractor 是"从对话中提取长期记忆碎片"的 LLM 驱动提取器。它拼接对话上下文、构建中文周范围时间上下文、调用 LLM（fragment_memory_prompt 模板）、解析 JSON 输出并重试。

## 文件组织方案

严格对齐 Python 目录结构（方案 A）：

```
memory/config/
  └── scope_config.go          # MemoryScopeConfig（本次附带实现）

memory/process/extract/
  ├── doc.go                   # 包文档
  ├── common.go                # ExtractMemoryParams + MemoryOperationParams
  └── extractor.go             # LongTermMemoryExtractor（包级函数）
```

## 详细设计

### 1. MemoryScopeConfig（config/scope_config.go）

```go
// MemoryScopeConfig 记忆作用域配置，定义各类型记忆的提取规则。
//
// Python: openjiuwen/core/memory/config/config.py (MemoryScopeConfig)
type MemoryScopeConfig struct {
    // UserProfileDefinition 用户画像提取规则定义
    UserProfileDefinition string
    // SemanticMemoryDefinition 语义记忆提取规则定义
    SemanticMemoryDefinition string
    // EpisodicMemoryDefinition 情景记忆提取规则定义
    EpisodicMemoryDefinition string
}
```

- **暂不包含** `model_cfg` / `model_client_cfg` / `embedding_cfg`——7.18 中未使用，后续 7.27 回填时补充
- 默认值通过 `DefaultMemoryScopeConfig()` 函数提供，对齐 Python 的 3 个中文默认字符串
- 放在 `memory/config/scope_config.go`，与已有的 `graph_config.go` 平级

### 2. ExtractMemoryParams + MemoryOperationParams（common.go）

```go
// ExtractMemoryParams 长期记忆提取参数。
//
// Python: openjiuwen/core/memory/process/extract/common.py (ExtractMemoryParams)
type ExtractMemoryParams struct {
    // UserID 用户标识
    UserID string
    // ScopeID 作用域标识
    ScopeID string
    // Messages 当前轮次消息列表
    Messages []schema.BaseMessage
    // HistoryMessages 历史消息列表
    HistoryMessages []schema.BaseMessage
    // BaseModel 基础聊天模型（具体类型 *llm.Model，非接口）
    BaseModel *llm.Model
}

// MemoryOperationParams 记忆操作参数（UPDATE/DELETE 语义验证时使用）。
//
// Python: openjiuwen/core/memory/process/extract/common.py (MemoryOperationParams)
type MemoryOperationParams struct {
    // UserID 用户标识
    UserID string
    // ScopeID 作用域标识
    ScopeID string
    // MessageMemID 关联消息 ID
    MessageMemID string
    // Timestamp 时间戳
    Timestamp string
    // BaseModel 基础聊天模型（具体类型 *llm.Model，非接口）
    BaseModel *llm.Model
    // SemanticStore 语义存储（用于搜索旧记忆做语义验证）
    SemanticStore any
}
```

- `base_chat_model` → `BaseModel *llm.Model`，Go 代码库中统一使用 `*llm.Model`（门面结构体），无 `model.Model` 接口
- `semantic_store: Any` → `SemanticStore any`，保持灵活性，后续回填时可收窄
- `MemoryOperationParams` 在 7.18 中不被直接使用，但属于 `common.py` 的一部分，按对齐原则一并定义

### 3. LongTermMemoryExtractor（extractor.go）

```go
// LongTermMemoryExtractor 长期记忆提取器，通过 LLM 从对话中提取用户画像、语义记忆和情景记忆。
//
// 无状态，所有方法为包级函数（对齐 Python @staticmethod）。
// Python: openjiuwen/core/memory/process/extract/long_term_memory_extractor.py

// ExtractLongTermMemory 从对话消息中提取长期记忆碎片。
//
// 返回 map[string]any，预期键：
//   - has_explict_instruct (bool): 是否有显式记忆指令
//   - instruct_memories ([]any): UPDATE/DELETE 操作列表
//   - user_profile ([]any): 用户画像碎片
//   - semantic_memory ([]any): 语义记忆碎片
//   - episodic_memory ([]any): 情景记忆碎片
//
// Python: LongTermMemoryExtractor.extract_long_term_memory
func ExtractLongTermMemory(
    ctx context.Context,
    params *ExtractMemoryParams,
    timestamp string,
    scopeConfig *MemoryScopeConfig,
    retries int,
) (map[string]any, error)

// buildTimeContext 将 ISO 时间戳转换为中文周范围字符串。
//
// 示例输出: "2026年1月5日(周一)～1月11日(周日)（即01.05～01.11）"
// Python: LongTermMemoryExtractor._build_time_context
func buildTimeContext(timestamp string) string
```

**关键设计决策**：

1. **无状态 → 包级函数**：Python `LongTermMemoryExtractor.__init__` 是空操作，两个方法都是 `@staticmethod`。Go 中直接用包级函数，避免无意义实例化
2. **返回值 `(map[string]any, error)`**：Python 返回 `Dict[str, Any]` 或空 dict。Go 用 `map[string]any` 对齐，增加 error 返回值符合 Go 惯例
3. **retries 参数**：Python 默认 `retries=3`，Go 中由调用方显式传入
4. **scopeConfig nil 处理**：Python 中 `if not scope_config: scope_config = MemoryScopeConfig()`。Go 中 `if scopeConfig == nil` 时使用 `DefaultMemoryScopeConfig()`
5. **日志同步**：Python 在最终重试失败时记录 `memory_logger.error("Long term memory extractor model output format error", event_type=LogEventType.MEMORY_PROCESS, exception=str(e))`。Go 等价对齐，使用 `logger.Error(logComponent).Str("event_type", "MEMORY_PROCESS").Str("exception", ...).Msg(...)`，其中 `exception` 字段对齐 Python 的 `exception=str(e)`，提供诊断上下文
6. **JsonOutputParser 重试模式**：对齐已有的 `update_checker.go` 先例——`NewJsonOutputParser()` + `for attempt` 循环

### 4. doc.go

```go
// Package extract 提供长期记忆提取功能，通过 LLM 从对话中提取用户画像、语义记忆和情景记忆碎片。
//
// 本包对齐 Python openjiuwen/core/memory/process/extract/，包含参数定义和核心提取器。
// 后续 7.19 MemoryAnalyzer 和 Generator 编排器将在此包中补充。
//
// 文件目录：
//
//	extract/
//	├── doc.go           # 包文档
//	├── common.go        # 提取参数结构体（ExtractMemoryParams / MemoryOperationParams）
//	└── extractor.go     # LongTermMemoryExtractor 长期记忆提取器
//
// 对应 Python 代码：openjiuwen/core/memory/process/extract/
package extract
```

## 测试策略

| 测试文件 | 覆盖内容 |
|---------|---------|
| `memory/config/scope_config_test.go` | `DefaultMemoryScopeConfig()` 默认值验证 |
| `memory/process/extract/common_test.go` | 结构体零值 / 赋值基本验证 |
| `memory/process/extract/extractor_test.go` | `ExtractLongTermMemory`：mock LLM 返回合法 JSON / 非法 JSON 重试 / 全部失败返回空 map；`buildTimeContext`：合法 ISO 时间 / 空串 / 非法格式 fallback |

mock LLM 方式：在测试文件中定义 `mockLLMClient` 结构体实现 `model_clients.BaseModelClient` 接口，通过 `llm.NewModel` + 合法 provider（`"openai"` + 虚假凭据 + `WithVerifySSL(false)`）+ `llm.WithClient(client)` 覆盖底层客户端。

> **偏差说明**：初始计划使用 `NewModelClientConfig("mock", ...)` 构造 mock 配置，但 `"mock"` 不是合法 provider 会被 `NewModelClientConfig` 校验拒绝。实际采用合法 provider + `WithClient` 覆盖模式，这是项目中 mock LLM 的标准做法。

> **偏差说明**：mock client 的 `Invoke` 方法中必须手动调用 `OutputParser.Parse()` 并设置 `resp.ParserContent`，以对齐真实 client 行为。初始计划未预见此步骤，但不模拟会导致 `ExtractLongTermMemory` 永远拿不到 `parsedResult`。

## 已有依赖（✅ = 已实现）

| 依赖组件 | 状态 | 位置 |
|---------|------|------|
| PromptApplier | ✅ | memory/prompts/ |
| fragment_memory_prompt.md | ✅ | memory/prompts/ |
| JsonOutputParser | ✅ | foundation/llm/output_parsers/ |
| *llm.Model 结构体 | ✅ | foundation/llm/ |
| BaseMessage 接口 | ✅ | foundation/llm/schema/ |
| MemoryScopeConfig | ❌ 本次实现 | memory/config/ |

## 回填标记

- 7.18 完成后，`IMPLEMENTATION_PLAN.md` 中 7.18 状态从 `☐` 更新为 `✅`
- `MemoryScopeConfig` 在 config 包的定义为 7.19 / 7.27 提供前置依赖
- `MemoryOperationParams` 为 7.19 Generator 的 `_handle_memory_with_instruct` 提供前置依赖
- 后续 7.27 回填时需补充 `model_cfg` / `model_client_cfg` / `embedding_cfg` 字段到 `MemoryScopeConfig`
