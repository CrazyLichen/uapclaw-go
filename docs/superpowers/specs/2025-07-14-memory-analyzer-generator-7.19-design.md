# 7.19 MemoryAnalyzer / Generator 编排器实现设计

## 概述

实现记忆精炼流水线的核心编排层——`MemoryAnalyzer`（对话分析器）和 `Generator`（编排器），串联 7.18（LongTermMemoryExtractor）和 7.8（WriteManager），完成从对话中提取、分析、精炼记忆的完整流程。同时实现前置依赖 `Param` 类型、`MemoryEngineConfig` 和 `AgentMemoryConfig`，以及补全 `MemoryScopeConfig` 缺失字段。

严格对齐 Python `openjiuwen/core/memory/process/extract/` 和 `openjiuwen/core/memory/config/` 目录结构。

> **注意**：实现计划中 7.19 的 Python 参考路径 `openjiuwen/core/memory/process/refine/` 是过时的——Python 的 `refine/` 目录只有空的 `__init__.py`，实际代码全部在 `extract/` 目录（`memory_analyzer.py` + `generation.py`）。本次实现放在 `process/extract/` 包，与 Python 实际代码对齐。

## 在 Agent 会话中的流程位置

```
用户消息 → Agent ReAct 循环 → 对话完成
                                    │
                                    ▼
              LongTermMemory.add_messages()     ← 7.27（尚未实现）
                    │
                    ├── 获取用户级分布式锁
                    ├── 存储 messages 到 MessageManager
                    ├── 获取 scope 对应的 LLM + scope_config + embedding
                    │
                    ▼
              Generator.gen_all_memory()        ← ★ 7.19 编排器（本次实现）
                    │
                    ├── Step 1: MemoryAnalyzer.analyze()     ← 7.19 LLM 调用 #1
                    │   → {has_key_information, variables[], summary}
                    │
                    ├── Step 2: processExtractedData()       ← 7.19
                    │   → VariableUnit 列表
                    │
                    ├── Step 3: if enable_summary_memory
                    │   → processSummaryData()               ← 7.19
                    │   → SummaryUnit
                    │
                    ├── Step 4: if has_key_information
                    │   ├── LongTermMemoryExtractor.extract()   ← 7.18 ✅ LLM 调用 #2
                    │   │
                    │   ├── handleMemoryWithInstruct()          ← 7.19 LLM 调用 #3（逐条串行）
                    │   │   ├── SearchManager.search(旧记忆)    ← DB 查询
                    │   │   └── semanticValidation()            ← LLM 语义校验
                    │   │
                    │   └── getFragmentMemoryUnit()             ← 7.19
                    │       → FragmentMemoryUnit 列表（ADD 类型）
                    │
                    ▼
              WriteManager.add_memories()       ← 7.8 ✅（已实现）
```

**作用**：Generator 是记忆处理流水线的"大脑"——它先调 MemoryAnalyzer 判断对话是否含关键信息并提取变量/摘要，再调 LongTermMemoryExtractor 提取碎片记忆，最后处理 UPDATE/DELETE 指令（含语义校验），输出可直接写入 WriteManager 的记忆单元列表。

## 文件组织方案

```
agentcore/schema/                          ← 新建包
  ├── doc.go                               # 包文档
  ├── param.go                             # ParamType 枚举 + Param 结构体
  └── param_test.go                        # Param 校验 + 工厂方法测试

agentcore/memory/config/                   ← 已有包，新增文件
  ├── doc.go                               # 更新文件目录
  ├── scope_config.go                      # 修改：补全 ModelCfg/ModelClientCfg/EmbeddingCfg
  ├── scope_config_test.go                 # 修改：补充新字段测试
  ├── engine_config.go                     # 新增：MemoryEngineConfig
  ├── engine_config_test.go                # 新增：MemoryEngineConfig 测试
  ├── agent_config.go                      # 新增：AgentMemoryConfig
  └── agent_config_test.go                 # 新增：AgentMemoryConfig 测试

agentcore/memory/process/extract/          ← 已有包，新增文件
  ├── doc.go                               # 更新文件目录
  ├── common.go                            # 保留不变
  ├── extractor.go                         # 保留不变
  ├── extractor_test.go                    # 保留不变
  ├── analyzer.go                          # 新增：VariableResult + MemoryAnalyzerResult + MemoryAnalyzer
  ├── analyzer_test.go                     # 新增：MemoryAnalyzer 测试
  ├── generator.go                         # 新增：Generator 结构体 + 全部方法
  └── generator_test.go                    # 新增：Generator 测试
```

## 详细设计

### 1. Param 类型（agentcore/schema/param.go）

对齐 Python `openjiuwen/core/common/schema/param.py`。

```go
// ParamType 参数类型枚举
type ParamType int

const (
    ParamTypeString  ParamType = iota  // string
    ParamTypeBoolean                    // boolean
    ParamTypeInteger                    // integer
    ParamTypeNumber                     // number
    ParamTypeArray                      // array
    ParamTypeObject                     // object
)

// Param 参数定义结构体
type Param struct {
    // Name 参数名称（必填）
    Name string
    // Description 参数描述（必填）
    Description string
    // Type 参数类型（必填）
    Type ParamType
    // Required 是否必填（必填）
    Required bool
    // Default 默认值（可选，nil 表示未设置）
    Default any
    // Items 数组元素类型定义（仅 Array 类型使用）
    Items *Param
    // Properties 对象属性列表（仅 Object 类型使用）
    Properties []Param
}
```

**校验规则（Validate 方法）**：

| Type | Items | Properties | 约束 |
|------|-------|------------|------|
| Array | 必须 != nil | 必须为空 | 否则返回错误 |
| Object | 必须为空 | 必须 len > 0 | 否则返回错误 |
| 其他 | 必须为空 | 必须为空 | 否则返回错误 |

**工厂方法**：`NewStringParam`、`NewBooleanParam`、`NewIntegerParam`、`NewNumberParam`、`NewArrayParam`、`NewObjectParam`——对齐 Python 的 `Param.string()`、`Param.boolean()` 等类方法。

### 2. MemoryEngineConfig（memory/config/engine_config.go）

对齐 Python `openjiuwen/core/memory/config/config.py` MemoryEngineConfig。

```go
// MemoryEngineConfig 记忆引擎配置
type MemoryEngineConfig struct {
    // DefaultModelCfg 默认模型请求配置
    DefaultModelCfg *llmschema.ModelRequestConfig
    // DefaultModelClientCfg 默认模型客户端配置
    DefaultModelClientCfg *llmschema.ModelClientConfig
    // ForbiddenVariables 禁用变量名列表（逗号分隔）
    ForbiddenVariables string
    // InputMsgMaxLen 输入消息最大长度
    InputMsgMaxLen int
    // CryptoKey AES 加密密钥（空=不加密，非空则长度必须 == 32）
    CryptoKey []byte
    // SingleTurnHistorySummaryMaxToken 单轮历史摘要最大 token 数
    SingleTurnHistorySummaryMaxToken int
}
```

**校验规则**：
- `CryptoKey`：空 or 长度 == AES_KEY_LENGTH(32)，否则返回错误
- `SingleTurnHistorySummaryMaxToken`：必须 > 0
- 默认值：`ForbiddenVariables=""`, `InputMsgMaxLen=8192`, `CryptoKey=[]byte{}`, `SingleTurnHistorySummaryMaxToken=128`
- `DefaultModelCfg` / `DefaultModelClientCfg` 默认 nil（对齐 Python `default=None`）

**引用**：`llmschema` = `internal/agentcore/foundation/llm/schema`

### 3. AgentMemoryConfig（memory/config/agent_config.go）

对齐 Python `openjiuwen/core/memory/config/config.py` AgentMemoryConfig。

```go
// AgentMemoryConfig Agent 记忆配置
type AgentMemoryConfig struct {
    // MemVariables 记忆变量配置列表
    MemVariables []schema.Param
    // EnableLongTermMem 是否启用长期记忆
    EnableLongTermMem bool
    // EnableUserProfile 是否启用用户画像记忆
    EnableUserProfile bool
    // EnableSemanticMemory 是否启用语义记忆
    EnableSemanticMemory bool
    // EnableEpisodicMemory 是否启用情景记忆
    EnableEpisodicMemory bool
    // EnableSummaryMemory 是否启用摘要记忆
    EnableSummaryMemory bool
}
```

- 默认值：全部 `true`，`MemVariables` 为空切片
- **引用**：`schema` = `internal/agentcore/schema`（Param 类型）

### 4. MemoryScopeConfig 补全（memory/config/scope_config.go）

在现有 3 个 definition 字段基础上补全 3 个配置字段：

```go
type MemoryScopeConfig struct {
    // --- 现有字段（保留不变）---
    UserProfileDefinition    string
    SemanticMemoryDefinition string
    EpisodicMemoryDefinition string

    // --- 新增字段 ---
    // ModelCfg 模型请求配置（7.27 回填时使用）
    ModelCfg *llmschema.ModelRequestConfig
    // ModelClientCfg 模型客户端配置（7.27 回填时使用）
    ModelClientCfg *llmschema.ModelClientConfig
    // EmbeddingCfg 嵌入模型配置（7.27 回填时使用）
    EmbeddingCfg *embedding.EmbeddingConfig
}
```

- `DefaultMemoryScopeConfig()` 更新：3 个新字段默认为 nil（对齐 Python `default=None`），3 个 definition 字符串保留原默认值

### 5. MemoryAnalyzer（memory/process/extract/analyzer.go）

对齐 Python `openjiuwen/core/memory/process/extract/memory_analyzer.py`。

#### 数据模型

```go
// VariableResult 变量提取结果
type VariableResult struct {
    // VariableKey 变量键
    VariableKey string
    // VariableValue 变量值
    VariableValue string
}

// MemoryAnalyzerResult 记忆分析结果
type MemoryAnalyzerResult struct {
    // HasKeyInformation 是否包含关键信息
    HasKeyInformation bool
    // Variables 提取的变量列表
    Variables []VariableResult
    // Summary 对话摘要
    Summary string
}
```

#### MemoryAnalyzer.Analyze 方法

```go
// MemoryAnalyzer 记忆分析器
type MemoryAnalyzer struct{}

// Analyze 分析对话内容，提取变量和摘要。
//
// Python: MemoryAnalyzer.analyze (static method)
func (MemoryAnalyzer) Analyze(
    ctx context.Context,
    params ExtractMemoryParams,           // 复用 7.18 已有结构体
    memoryConfig AgentMemoryConfig,        // 7.19b 新增
    summaryMaxToken int,                   // 对齐 Python summary_max_token
    scopeConfig *MemoryScopeConfig,        // 可选，nil 时使用默认值
    forbiddenVariables string,             // 对齐 Python forbidden_variables
    retries int,                           // LLM 重试次数，默认 3
) (*MemoryAnalyzerResult, error)
```

**执行流程**：

1. `len(params.Messages) == 0` → 返回 `(nil, nil)` + warn 日志（对齐 Python: return None with warning）
2. 格式化 `history` 和 `conversation` 字符串（`"{role}: {content}\n"` 格式，对齐 Python）
3. 从 `memoryConfig.MemVariables` 构建：
   - `variablesDescriptionJSON`：`[{variable_key: name, variable_value: description}, ...]`
   - `variablesOutputFormatJSON`：`[{variable_key: name, variable_value: ""}, ...]`（空值模板）
4. `scopeConfig` 为 nil 时 fallback 到 `DefaultMemoryScopeConfig()`
5. 加载 `memory_analysis_prompt` 模板，填充变量：`history`、`conversation`、`has_variable`、`variables_define_template`、`variables_output_template`、`forbidden_variables`、`max_message_token`、`user_profile_definition`、`semantic_memory_definition`、`episodic_memory_definition`
6. 循环 `retries` 次：
   - `params.BaseModel.Invoke(ctx, modelInput)`
   - `JsonOutputParser` 解析响应
   - 验证结果为 `MemoryAnalyzerResult`
   - 如果 `!memoryConfig.EnableLongTermMem || !memoryConfig.EnableSummaryMemory` → 清空 `Summary` 为 `""`
   - 成功返回 `*MemoryAnalyzerResult`
7. 全部重试失败 → 返回空 `MemoryAnalyzerResult`（对齐 Python: `return MemoryAnalyzerResult()`）

### 6. Generator 编排器（memory/process/extract/generator.go）

对齐 Python `openjiuwen/core/memory/process/extract/generation.py`。

#### 结构体

```go
// Generator 记忆生成编排器，串联 MemoryAnalyzer → LongTermMemoryExtractor → 语义校验 → 记忆单元输出
type Generator struct {
    // dataIdGenerator 记忆 ID 生成器
    dataIdGenerator *mem_model.DataIdManager
    // searchManager 搜索管理器（可选，处理 UPDATE/DELETE 指令时使用）
    searchManager *search.Manager
}
```

#### 映射常量

```go
// categoryToClass 记忆类别到 MemoryType 的映射
var categoryToClass = map[string]mem_model.MemoryType{
    "user_profile":    mem_model.MemoryTypeUserProfile,
    "semantic_memory": mem_model.MemoryTypeSemanticMemory,
    "episodic_memory": mem_model.MemoryTypeEpisodicMemory,
}

// operationStrToEnum 操作字符串到 OperationType 的映射
var operationStrToEnum = map[string]mem_model.OperationType{
    "ADD":    mem_model.OperationTypeAdd,
    "UPDATE": mem_model.OperationTypeUpdate,
    "DELETE": mem_model.OperationTypeDelete,
}
```

#### 方法列表

| Go 方法 | Python 方法 | 接收者 | 说明 |
|---------|------------|--------|------|
| `NewGenerator` | `Generator.__init__` | — | 构造函数 |
| `GenAllMemory` | `gen_all_memory` | `*Generator` | 主入口，编排全流程 |
| `processExtractedData` | `_process_extracted_data` | `*Generator` | VariableResult→VariableUnit 列表 |
| `processSummaryData` | `_process_summary_data` | `*Generator` | 生成 SummaryUnit |
| `categoriesToMemoryUnit` | `_categories_to_memory_unit` | `*Generator` | 调 Extractor + 处理 instruct |
| `handleMemoryWithInstruct` | `_handle_memory_with_instruct` | `*Generator` | 处理 UPDATE/DELETE 指令 |
| `processMemoryOperations` | `_process_memory_operations` | `*Generator` | 搜索旧记忆+语义校验 |
| `semanticValidation` | `_semantic_validation` | `*Generator` | LLM 语义一致性校验（逐条串行） |
| `getFragmentMemoryUnit` | `_get_fragment_memory_unit` | `*Generator` | 提取结果→FragmentMemoryUnit 列表 |
| `processProactiveMemoryData` | `_process_proactive_memory_data` | `*Generator` | 处理主动记忆数据（ADD-only） |

#### GenAllMemory 流程（核心编排）

```go
// GenAllMemory 编排全部记忆生成流程。
//
// 从 engineConfig 中提取 forbiddenVariables 和 summaryMaxToken 传递给 MemoryAnalyzer。
// Python: Generator.gen_all_memory
func (g *Generator) GenAllMemory(
    ctx context.Context,
    params ExtractMemoryParams,
    memoryConfig AgentMemoryConfig,
    engineConfig *MemoryEngineConfig,
    scopeConfig *MemoryScopeConfig,
) ([]*mem_model.BaseMemoryUnit, error)
```

```
GenAllMemory(ctx, params, memoryConfig, engineConfig, scopeConfig)
  │
  ├── 1. MemoryAnalyzer.Analyze(ctx, params, memoryConfig,
  │       engineConfig.SingleTurnHistorySummaryMaxToken, scopeConfig,
  │       engineConfig.ForbiddenVariables, retries=3)
  │   → analyzerResult
  │
  ├── 2. processExtractedData(analyzerResult.Variables)
  │   → 遍历 variables，构建 VariableUnit 列表 → append results
  │
  ├── 3. if !memoryConfig.EnableLongTermMem → return results
  │
  ├── 4. if memoryConfig.EnableSummaryMemory
  │   → processSummaryData(ctx, analyzerResult.Summary, params.Timestamp)
  │   → 构建 SummaryUnit → append results
  │
  ├── 5. if !analyzerResult.HasKeyInformation → return results
  │
  ├── 6. categoriesToMemoryUnit(ctx, params, memoryConfig, scopeConfig)
  │   │
  │   ├── 6a. ExtractLongTermMemory(ctx, params, scopeConfig) → extractionResult
  │   │
  │   ├── 6b. if extractionResult["has_explict_instruct"] == true
  │   │   → handleMemoryWithInstruct(ctx, instructMemories, params)
  │   │     ├── 分离 UPDATE vs DELETE 指令
  │   │     ├── processMemoryOperations(ctx, updateItems, "UPDATE", params)
  │   │     │   ├── searchManager.Search(旧记忆关键词)  ← DB 查询
  │   │     │   └── semanticValidation(ctx, newMem, oldMem, model)  ← LLM 调用（逐条串行）
  │   │     └── processMemoryOperations(ctx, deleteItems, "DELETE", params)
  │   │       → 同上流程
  │   │
  │   └── 6c. getFragmentMemoryUnit(extractionResult)
  │       → 按 category 映射构建 FragmentMemoryUnit 列表 → append results
  │
  └── 7. 按 enable 标志过滤 → return results
```

#### semanticValidation 流程

```
semanticValidation(ctx, obtainedMem, oldMem, model)
  │
  ├── 加载 semantic_validation 提示词模板
  ├── 填充 obtained_mem + old_mem 变量
  ├── model.Invoke(ctx, messages)
  ├── 解析响应 → "CORRECT" / "WRONG"
  └── "CORRECT" → true（语义一致）, "WRONG" → false
```

**逐条串行调用**：对齐 Python 行为，每条 UPDATE/DELETE 指令单独调一次 LLM，不做并发优化。

## 回填项

| 文件 | 修改内容 |
|------|---------|
| `extract/doc.go` | 更新文件目录，新增 `analyzer.go` + `generator.go` 条目 |
| `config/doc.go` | 更新文件目录，新增 `engine_config.go` + `agent_config.go` 条目 |
| `config/scope_config.go` | 新增 `ModelCfg`、`ModelClientCfg`、`EmbeddingCfg` 3 个字段；更新 `DefaultMemoryScopeConfig()` |
| `config/scope_config_test.go` | 补充新字段测试 |
| `IMPLEMENTATION_PLAN.md` | 7.19 Python 参考路径修正为 `openjiuwen/core/memory/process/extract/` |

**已有内容不丢失**：scope_config.go 的原有 3 个 definition 字段和 `DefaultMemoryScopeConfig()` 函数保留不变，仅追加字段。

## 依赖关系

```
agentcore/schema/param.go
    ↓
memory/config/agent_config.go ──→ schema.Param
memory/config/engine_config.go ──→ llm/schema.ModelRequestConfig + ModelClientConfig
                                ──→ retrieval/embedding.EmbeddingConfig
memory/config/scope_config.go ──→ 同上 3 个类型（补全）
    ↓
memory/process/extract/analyzer.go ──→ config.AgentMemoryConfig + config.MemoryScopeConfig
                                    ──→ prompts.PromptApplier (memory_analysis_prompt)
                                    ──→ llm.Model + JsonOutputParser
memory/process/extract/generator.go ──→ extract.MemoryAnalyzer + ExtractLongTermMemory
                                    ──→ mem_model.* (DataIdManager, FragmentMemoryUnit, etc.)
                                    ──→ search.Manager
                                    ──→ prompts.PromptApplier (semantic_validation)
```

## 测试策略

### Param 测试（param_test.go）

- 校验规则：Array 无 Items 报错、Object 无 Properties 报错、其他类型有 Items/Properties 报错
- 工厂方法：每个工厂方法创建正确类型的 Param
- 序列化/反序列化：JSON 往返

### Config 测试

- MemoryEngineConfig：CryptoKey 校验（空/32字节/非32字节）、SummaryMaxToken 校验
- AgentMemoryConfig：默认值验证
- MemoryScopeConfig：补全字段默认 nil、原有字段默认值不变

### MemoryAnalyzer 测试（analyzer_test.go）

- 复用 extractor_test.go 的 `mockLLMClient`
- 正常分析返回、messages 为空返回 nil、LLM 解析失败返回空结果
- EnableLongTermMem/EnableSummaryMemory 关闭时 Summary 被清空
- scopeConfig 为 nil 时使用默认值

### Generator 测试（generator_test.go）

- processExtractedData：VariableResult→VariableUnit 转换
- processSummaryData：生成 SummaryUnit + ID
- getFragmentMemoryUnit：category 映射 + FragmentMemoryUnit 构建
- handleMemoryWithInstruct：UPDATE/DELETE 分离 + 语义校验
- semanticValidation：CORRECT/WRONG 解析
- GenAllMemory 端到端：mock LLM + mock SearchManager

## 实现顺序

1. `schema/param.go` + `schema/doc.go` + `schema/param_test.go`
2. `config/engine_config.go` + `config/engine_config_test.go`
3. `config/agent_config.go` + `config/agent_config_test.go`
4. `config/scope_config.go` 修改 + `config/scope_config_test.go` 修改 + `config/doc.go` 更新
5. `extract/analyzer.go` + `extract/analyzer_test.go`
6. `extract/generator.go` + `extract/generator_test.go`
7. `extract/doc.go` 更新
8. `IMPLEMENTATION_PLAN.md` 修正 7.19 路径
