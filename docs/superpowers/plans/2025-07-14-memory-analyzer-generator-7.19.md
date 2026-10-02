# 7.19 MemoryAnalyzer / Generator 编排器 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现记忆精炼流水线的核心编排层——MemoryAnalyzer（对话分析器）+ Generator（编排器），以及前置依赖 MemoryEngineConfig、AgentMemoryConfig 和 MemoryScopeConfig 补全。

**Architecture:** MemoryAnalyzer 是无状态分析器，通过 LLM 调用从对话中提取变量和摘要；Generator 是有状态编排器，持有 DataIdManager + SearchManager，串联 Analyzer→Extractor→语义校验，输出记忆单元列表。Param/ParamType 引用已有的 `common/schema` 包。

**Tech Stack:** Go 1.22+, 项目内部 llm/search/mem_model/prompts 包

**Spec:** `docs/superpowers/specs/2025-07-14-memory-analyzer-generator-7.19-design.md`

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `internal/agentcore/memory/config/engine_config.go` | MemoryEngineConfig 结构体 + Validate + 默认值 |
| Create | `internal/agentcore/memory/config/engine_config_test.go` | MemoryEngineConfig 测试 |
| Create | `internal/agentcore/memory/config/agent_config.go` | AgentMemoryConfig 结构体 + 默认值 |
| Create | `internal/agentcore/memory/config/agent_config_test.go` | AgentMemoryConfig 测试 |
| Modify | `internal/agentcore/memory/config/scope_config.go` | 补全 ModelCfg/ModelClientCfg/EmbeddingCfg |
| Modify | `internal/agentcore/memory/config/scope_config_test.go` | 补充新字段测试 |
| Modify | `internal/agentcore/memory/config/doc.go` | 更新文件目录 |
| Create | `internal/agentcore/memory/process/extract/analyzer.go` | VariableResult + MemoryAnalyzerResult + MemoryAnalyzer.Analyze |
| Create | `internal/agentcore/memory/process/extract/analyzer_test.go` | MemoryAnalyzer 测试 |
| Create | `internal/agentcore/memory/process/extract/generator.go` | Generator 结构体 + 全部方法 |
| Create | `internal/agentcore/memory/process/extract/generator_test.go` | Generator 测试 |
| Modify | `internal/agentcore/memory/process/extract/doc.go` | 更新文件目录 |
| Modify | `IMPLEMENTATION_PLAN.md` | 7.19 路径修正 + 状态更新 |

---

### Task 1: MemoryEngineConfig 结构体

**Files:**
- Create: `internal/agentcore/memory/config/engine_config.go`
- Create: `internal/agentcore/memory/config/engine_config_test.go`

- [ ] **Step 1: 写 engine_config.go 结构体定义**

```go
package config

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryEngineConfig 记忆引擎配置。
//
// 包含默认模型配置、禁用变量、加密密钥等引擎级参数。
//
// Python: openjiuwen/core/memory/config/config.py (MemoryEngineConfig)
type MemoryEngineConfig struct {
	// DefaultModelCfg 默认模型请求配置
	DefaultModelCfg *schema.ModelRequestConfig
	// DefaultModelClientCfg 默认模型客户端配置
	DefaultModelClientCfg *schema.ModelClientConfig
	// ForbiddenVariables 禁用变量名列表（逗号分隔）
	ForbiddenVariables string
	// InputMsgMaxLen 输入消息最大长度
	InputMsgMaxLen int
	// CryptoKey AES 加密密钥（空=不加密，非空则长度必须 == AESKeyLength）
	CryptoKey []byte
	// SingleTurnHistorySummaryMaxToken 单轮历史摘要最大 token 数
	SingleTurnHistorySummaryMaxToken int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// AESKeyLength AES 密钥长度（32 字节 = AES-256）
const AESKeyLength = 32

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultMemoryEngineConfig 返回默认记忆引擎配置。
//
// 对齐 Python MemoryEngineConfig 的字段默认值。
func DefaultMemoryEngineConfig() *MemoryEngineConfig {
	return &MemoryEngineConfig{
		ForbiddenVariables:               "",
		InputMsgMaxLen:                   8192,
		CryptoKey:                        []byte{},
		SingleTurnHistorySummaryMaxToken: 128,
	}
}

// Validate 校验记忆引擎配置。
//
// 规则：
//   - CryptoKey：空 or 长度 == AESKeyLength(32)，否则返回错误
//   - SingleTurnHistorySummaryMaxToken：必须 > 0
func (c *MemoryEngineConfig) Validate() error {
	if len(c.CryptoKey) > 0 && len(c.CryptoKey) != AESKeyLength {
		return fmt.Errorf("crypto_key 长度必须为 %d 或为空，当前长度: %d", AESKeyLength, len(c.CryptoKey))
	}
	if c.SingleTurnHistorySummaryMaxToken <= 0 {
		return fmt.Errorf("single_turn_history_summary_max_token 必须 > 0，当前值: %d", c.SingleTurnHistorySummaryMaxToken)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 写 engine_config_test.go 测试**

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultMemoryEngineConfig(t *testing.T) {
	cfg := DefaultMemoryEngineConfig()
	assert.Equal(t, "", cfg.ForbiddenVariables)
	assert.Equal(t, 8192, cfg.InputMsgMaxLen)
	assert.Empty(t, cfg.CryptoKey)
	assert.Equal(t, 128, cfg.SingleTurnHistorySummaryMaxToken)
	assert.Nil(t, cfg.DefaultModelCfg)
	assert.Nil(t, cfg.DefaultModelClientCfg)
}

func TestMemoryEngineConfig_Validate_正常(t *testing.T) {
	cfg := DefaultMemoryEngineConfig()
	assert.NoError(t, cfg.Validate())
}

func TestMemoryEngineConfig_Validate_CryptoKey32字节(t *testing.T) {
	cfg := DefaultMemoryEngineConfig()
	cfg.CryptoKey = make([]byte, 32)
	assert.NoError(t, cfg.Validate())
}

func TestMemoryEngineConfig_Validate_CryptoKey非32字节(t *testing.T) {
	cfg := DefaultMemoryEngineConfig()
	cfg.CryptoKey = make([]byte, 16)
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "crypto_key")
}

func TestMemoryEngineConfig_Validate_SummaryMaxToken为零(t *testing.T) {
	cfg := DefaultMemoryEngineConfig()
	cfg.SingleTurnHistorySummaryMaxToken = 0
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "single_turn_history_summary_max_token")
}

func TestMemoryEngineConfig_Validate_SummaryMaxToken为负(t *testing.T) {
	cfg := DefaultMemoryEngineConfig()
	cfg.SingleTurnHistorySummaryMaxToken = -1
	err := cfg.Validate()
	assert.Error(t, err)
}
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/config/... -run TestMemoryEngineConfig -v -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/config/engine_config.go internal/agentcore/memory/config/engine_config_test.go
git commit -m "feat(7.19a): 实现 MemoryEngineConfig 结构体 + Validate + 默认值"
```

---

### Task 2: AgentMemoryConfig 结构体

**Files:**
- Create: `internal/agentcore/memory/config/agent_config.go`
- Create: `internal/agentcore/memory/config/agent_config_test.go`

- [ ] **Step 1: 写 agent_config.go 结构体定义**

```go
package config

import (
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentMemoryConfig Agent 记忆配置。
//
// 定义 Agent 级别的记忆功能开关和变量配置。
//
// Python: openjiuwen/core/memory/config/config.py (AgentMemoryConfig)
type AgentMemoryConfig struct {
	// MemVariables 记忆变量配置列表
	MemVariables []commonschema.Param
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

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultAgentMemoryConfig 返回默认 Agent 记忆配置。
//
// 所有 enable 标志默认为 true，MemVariables 为空。
// 对齐 Python AgentMemoryConfig 的字段默认值。
func DefaultAgentMemoryConfig() *AgentMemoryConfig {
	return &AgentMemoryConfig{
		MemVariables:        []commonschema.Param{},
		EnableLongTermMem:   true,
		EnableUserProfile:   true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory: true,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 写 agent_config_test.go 测试**

```go
package config

import (
	"testing"

	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/stretchr/testify/assert"
)

func TestDefaultAgentMemoryConfig(t *testing.T) {
	cfg := DefaultAgentMemoryConfig()
	assert.Empty(t, cfg.MemVariables)
	assert.True(t, cfg.EnableLongTermMem)
	assert.True(t, cfg.EnableUserProfile)
	assert.True(t, cfg.EnableSemanticMemory)
	assert.True(t, cfg.EnableEpisodicMemory)
	assert.True(t, cfg.EnableSummaryMemory)
}

func TestAgentMemoryConfig_自定义变量(t *testing.T) {
	cfg := &AgentMemoryConfig{
		MemVariables: []commonschema.Param{
			*commonschema.NewStringParam("name", "用户姓名", true),
		},
		EnableLongTermMem:   true,
		EnableUserProfile:   true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory: true,
	}
	assert.Len(t, cfg.MemVariables, 1)
	assert.Equal(t, "name", cfg.MemVariables[0].Name)
}

func TestAgentMemoryConfig_部分关闭(t *testing.T) {
	cfg := &AgentMemoryConfig{
		EnableLongTermMem:    true,
		EnableUserProfile:    true,
		EnableSemanticMemory: false,
		EnableEpisodicMemory: false,
		EnableSummaryMemory:  false,
	}
	assert.True(t, cfg.EnableLongTermMem)
	assert.False(t, cfg.EnableSemanticMemory)
	assert.False(t, cfg.EnableEpisodicMemory)
	assert.False(t, cfg.EnableSummaryMemory)
}
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/config/... -run TestAgentMemoryConfig -v -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/config/agent_config.go internal/agentcore/memory/config/agent_config_test.go
git commit -m "feat(7.19a): 实现 AgentMemoryConfig 结构体 + 默认值"
```

---

### Task 3: MemoryScopeConfig 补全 + config/doc.go 更新

**Files:**
- Modify: `internal/agentcore/memory/config/scope_config.go`
- Modify: `internal/agentcore/memory/config/scope_config_test.go`
- Modify: `internal/agentcore/memory/config/doc.go`

- [ ] **Step 1: 在 scope_config.go 中补全 3 个字段**

在 `EpisodicMemoryDefinition string` 字段后追加：

```go
	// ModelCfg 模型请求配置（7.27 回填时使用）
	// Python: model_cfg: ModelRequestConfig = None
	ModelCfg *llmschema.ModelRequestConfig
	// ModelClientCfg 模型客户端配置（7.27 回填时使用）
	// Python: model_client_cfg: ModelClientConfig = None
	ModelClientCfg *llmschema.ModelClientConfig
	// EmbeddingCfg 嵌入模型配置（7.27 回填时使用）
	// Python: embedding_cfg: EmbeddingConfig = None
	EmbeddingCfg *embedding.EmbeddingConfig
```

需要新增 import：
```go
import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)
```

`DefaultMemoryScopeConfig()` 无需修改（新字段默认 nil 即对齐 Python `default=None`）。

- [ ] **Step 2: 在 scope_config_test.go 中补充新字段测试**

追加测试：

```go
func TestDefaultMemoryScopeConfig_新字段为nil(t *testing.T) {
	cfg := DefaultMemoryScopeConfig()
	assert.Nil(t, cfg.ModelCfg)
	assert.Nil(t, cfg.ModelClientCfg)
	assert.Nil(t, cfg.EmbeddingCfg)
}

func TestMemoryScopeConfig_设置模型配置(t *testing.T) {
	cfg := DefaultMemoryScopeConfig()
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	cfg.ModelCfg = modelCfg
	assert.Equal(t, "test-model", cfg.ModelCfg.ModelName)
}
```

- [ ] **Step 3: 更新 config/doc.go 文件目录**

更新文件目录树，新增 `engine_config.go` 和 `agent_config.go`：

```
//	config/
//	├── doc.go              # 包文档
//	├── graph_config.go     # 图记忆配置类型
//	├── scope_config.go     # 记忆作用域配置（MemoryScopeConfig + 补全模型/嵌入配置）
//	├── engine_config.go    # 记忆引擎配置（MemoryEngineConfig）
//	└── agent_config.go     # Agent 记忆配置（AgentMemoryConfig）
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/config/... -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/config/
git commit -m "feat(7.19a): 补全 MemoryScopeConfig 模型/嵌入字段 + 更新 doc.go"
```

---

### Task 4: MemoryAnalyzer 实现

**Files:**
- Create: `internal/agentcore/memory/process/extract/analyzer.go`
- Create: `internal/agentcore/memory/process/extract/analyzer_test.go`

- [ ] **Step 1: 写 analyzer.go**

对齐 Python `memory_analyzer.py`，包含：
- `VariableResult` 结构体
- `MemoryAnalyzerResult` 结构体
- `MemoryAnalyzer` 结构体（空结构体，对齐 Python `__init__: pass`）
- `Analyze` 方法：完整流程（messages 空检查→格式化→构建变量 JSON→加载模板→LLM 调用+重试→解析→enable 检查→返回）

关键细节对齐 Python：
- `scope_config` 字段使用 `scopeConfig.UserProfileDefinition` 前先检查 `scopeConfig != nil`，否则用空字符串
- `forbidden_variables` 空字符串映射为 `"None"`
- `has_variable` = `len(memoryConfig.MemVariables) > 0`
- 重试循环捕获 JSON 解析错误，最后返回空 `MemoryAnalyzerResult`
- `!memoryConfig.EnableLongTermMem || !memoryConfig.EnableSummaryMemory` 时清空 `Summary`
- 日志使用 `logger.ComponentAgentCore` + `LogEventType` 常量

- [ ] **Step 2: 写 analyzer_test.go**

测试用例（复用 extractor_test.go 的 mockLLMClient 模式）：
- `TestAnalyze_消息为空返回nil`：messages=[] → 返回 nil
- `TestAnalyze_正常分析返回结果`：mock LLM 返回有效 JSON → 验证 has_key_information/variables/summary
- `TestAnalyze_LLM解析失败返回空结果`：mock LLM 返回非法 JSON → 返回空 MemoryAnalyzerResult
- `TestAnalyze_关闭长期记忆时清空摘要`：EnableLongTermMem=false → Summary==""
- `TestAnalyze_关闭摘要记忆时清空摘要`：EnableSummaryMemory=false → Summary==""
- `TestAnalyze_scopeConfig为nil时使用默认值`：scopeConfig=nil → 不 panic

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/process/extract/... -run TestAnalyze -v -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/process/extract/analyzer.go internal/agentcore/memory/process/extract/analyzer_test.go
git commit -m "feat(7.19c): 实现 MemoryAnalyzer + VariableResult + MemoryAnalyzerResult"
```

---

### Task 5: Generator 结构体 + 映射常量 + processExtractedData + processSummaryData

**Files:**
- Create: `internal/agentcore/memory/process/extract/generator.go`
- Create: `internal/agentcore/memory/process/extract/generator_test.go`（逐步添加）

这是最大的 Task，拆为多个 Step。

- [ ] **Step 1: 写 generator.go 骨架 + 映射常量 + processExtractedData + processSummaryData**

```go
package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/search"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Generator 记忆生成编排器，串联 MemoryAnalyzer → LongTermMemoryExtractor → 语义校验 → 记忆单元输出。
//
// Python: openjiuwen/core/memory/process/extract/generation.py (Generator)
type Generator struct {
	// dataIdGenerator 记忆 ID 生成器
	dataIdGenerator *mem_model.DataIdManager
	// searchManager 搜索管理器（可选，处理 UPDATE/DELETE 指令时使用）
	searchManager *search.SearchManager
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// categoryToClass 记忆类别到 MemoryType 的映射。
// Python: category_to_class
var categoryToClass = map[string]mem_model.MemoryType{
	"user_profile":    mem_model.MemoryTypeUserProfile,
	"semantic_memory": mem_model.MemoryTypeSemanticMemory,
	"episodic_memory": mem_model.MemoryTypeEpisodicMemory,
}

// operationStrToEnum 操作字符串到 OperationType 的映射。
// Python: operation_str_to_enum = {op.value: op for op in OperationType}
var operationStrToEnum = map[string]mem_model.OperationType{
	mem_model.OperationTypeAdd.String():    mem_model.OperationTypeAdd,
	mem_model.OperationTypeUpdate.String(): mem_model.OperationTypeUpdate,
	mem_model.OperationTypeDelete.String(): mem_model.OperationTypeDelete,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGenerator 创建记忆生成编排器。
//
// Python: Generator.__init__(data_id_generator, search_manager)
func NewGenerator(dataIdGenerator *mem_model.DataIdManager, searchManager *search.SearchManager) *Generator {
	return &Generator{
		dataIdGenerator: dataIdGenerator,
		searchManager:   searchManager,
	}
}

// GenAllMemory 编排全部记忆生成流程。
// ...（完整实现在后续 step 中补全）

// ──────────────────────────── 非导出函数 ────────────────────────────

// processExtractedData 将 VariableResult 列表转换为 VariableUnit 列表。
//
// 对齐 Python: _process_extracted_data（@staticmethod）
func (g *Generator) processExtractedData(variableResults []VariableResult) []*mem_model.VariableUnit {
	variableUnits := make([]*mem_model.VariableUnit, 0, len(variableResults))
	for _, tmpData := range variableResults {
		if tmpData.VariableValue == "" {
			continue
		}
		variableUnits = append(variableUnits, &mem_model.VariableUnit{
			VariableName: tmpData.VariableKey,
			VariableMem:  tmpData.VariableValue,
		})
	}
	return variableUnits
}

// processSummaryData 生成摘要记忆单元。
//
// 对齐 Python: _process_summary_data
func (g *Generator) processSummaryData(userID, messageMemID, summary, timestamp string) *mem_model.SummaryUnit {
	memID := g.dataIdGenerator.GenerateNextID(userID)
	return &mem_model.SummaryUnit{
		BaseMemoryUnit: mem_model.BaseMemoryUnit{
			MemType: mem_model.MemoryTypeSummary,
			MemID:   memID,
		},
		Summary:       summary,
		MessageMemID:  messageMemID,
		Timestamp:     timestamp,
	}
}
```

注意：Go 版 `processSummaryData` 和 `processExtractedData` 不需要 async（Go 无需 await），`DataIdManager.GenerateNextID` 在 Go 中是同步方法。

- [ ] **Step 2: 写 generator_test.go 的初始测试**

```go
func TestProcessExtractedData_正常转换(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	variables := []VariableResult{
		{VariableKey: "name", VariableValue: "张三"},
		{VariableKey: "age", VariableValue: "25"},
	}
	result := g.processExtractedData(variables)
	assert.Len(t, result, 2)
	assert.Equal(t, "name", result[0].VariableName)
	assert.Equal(t, "张三", result[0].VariableMem)
}

func TestProcessExtractedData_空值跳过(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	variables := []VariableResult{
		{VariableKey: "name", VariableValue: "张三"},
		{VariableKey: "empty", VariableValue: ""},
	}
	result := g.processExtractedData(variables)
	assert.Len(t, result, 1)
}

func TestProcessSummaryData(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	unit := g.processSummaryData("user1", "msg1", "这是摘要", "2025-01-01T00:00:00Z")
	assert.Equal(t, "这是摘要", unit.Summary)
	assert.Equal(t, "msg1", unit.MessageMemID)
	assert.NotEmpty(t, unit.MemID)
}
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/process/extract/... -run "TestProcessExtractedData|TestProcessSummaryData" -v -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/process/extract/generator.go internal/agentcore/memory/process/extract/generator_test.go
git commit -m "feat(7.19c): 实现 Generator 骨架 + 映射常量 + processExtractedData + processSummaryData"
```

---

### Task 6: Generator.getFragmentMemoryUnit + processProactiveMemoryData

**Files:**
- Modify: `internal/agentcore/memory/process/extract/generator.go`
- Modify: `internal/agentcore/memory/process/extract/generator_test.go`

- [ ] **Step 1: 在 generator.go 中追加 getFragmentMemoryUnit 和 processProactiveMemoryData**

对齐 Python `_get_fragment_memory_unit`：
- 遍历 `memoryDict` 的每个 key
- 通过 `categoryToClass` 查找 `MemoryType`，找不到则跳过
- `memContent` 非 string 时尝试 `.content` 字段，再 fallback 到 `fmt.Sprint()`
- 所有单元的 `OperationType` 为 `ADD`

对齐 Python `_process_proactive_memory_data`：
- 仅处理 `ADD` 操作
- `memContent` 非 string 时尝试 `.mem_content` 字段，再 fallback

- [ ] **Step 2: 追加测试**

```go
func TestGetFragmentMemoryUnit_正常(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryDict := map[string]any{
		"user_profile":    []any{"用户喜欢Python"},
		"semantic_memory": []any{"项目使用Go语言"},
		"unknown_type":    []any{"忽略此项"},
	}
	result, err := g.getFragmentMemoryUnit("user1", "msg1", memoryDict, "2025-01-01T00:00:00Z")
	assert.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, mem_model.MemoryTypeUserProfile, result[0].MemType)
	assert.Equal(t, mem_model.OperationTypeAdd, result[0].OperationType)
}

func TestGetFragmentMemoryUnit_非string内容(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryDict := map[string]any{
		"user_profile": []any{map[string]any{"content": "嵌套内容"}},
	}
	result, err := g.getFragmentMemoryUnit("user1", "msg1", memoryDict, "2025-01-01T00:00:00Z")
	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "嵌套内容", result[0].Content)
}

func TestProcessProactiveMemoryData_仅ADD(t *testing.T) {
	g := NewGenerator(mem_model.NewDataIdManager(), nil)
	memoryList := []any{
		map[string]any{"mem_instruct": "add", "mem_type": "user_profile", "mem_content": "新增内容"},
		map[string]any{"mem_instruct": "delete", "mem_type": "user_profile", "mem_content": "忽略"},
	}
	result, err := g.processProactiveMemoryData("user1", "msg1", memoryList, "2025-01-01T00:00:00Z")
	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "新增内容", result[0].Content)
}
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/process/extract/... -run "TestGetFragmentMemoryUnit|TestProcessProactiveMemoryData" -v -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/process/extract/generator.go internal/agentcore/memory/process/extract/generator_test.go
git commit -m "feat(7.19c): 实现 getFragmentMemoryUnit + processProactiveMemoryData"
```

---

### Task 7: Generator.semanticValidation + handleMemoryWithInstruct + processMemoryOperations

**Files:**
- Modify: `internal/agentcore/memory/process/extract/generator.go`
- Modify: `internal/agentcore/memory/process/extract/generator_test.go`

- [ ] **Step 1: 在 generator.go 中追加 semanticValidation**

对齐 Python `_semantic_validation`：
- 逐条串行 LLM 调用（对齐 Python 行为）
- 加载 `semantic_validation` 模板，填充 `obtained_mem` + `old_mem`
- 调 `baseChatModel.Invoke`，解析响应
- 响应包含 "CORRECT"（且不含 "WRONG"）→ 返回 `(id, mem)` 元组
- Go 返回类型：`[]semanticValidationMatch`（自定义小结构体 `{ID, Mem string}`）

- [ ] **Step 2: 在 generator.go 中追加 handleMemoryWithInstruct**

对齐 Python `_handle_memory_with_instruct`：
- 遍历 memory_list，按 `mem_instruct` 分离 UPDATE 和 DELETE
- 先处理 UPDATE，再处理 DELETE

- [ ] **Step 3: 在 generator.go 中追加 processMemoryOperations**

对齐 Python `_process_memory_operations`：
- 对每个 mem_dict：无 `old_mem` 则跳过
- 构建 SearchParams（top_k=1，search_type 包含 3 种 fragment 类型）
- 调 `searchManager.Search`
- 按分数降序排序，取第一条
- 调 `semanticValidation`
- 构建 FragmentMemoryUnit（mem_id 用语义校验返回的 ID）

**注意 Go/Python 差异**：
- Python SearchManager.search 接收 `semantic_store` 参数，Go 版 SearchManager.Search 不需要（已内置在 SearchManager 内部）
- Python search_data 是 `[]dict`，Go 返回 `[]*storeindex.MemorySearchResult`，需要适配：将 MemorySearchResult 的 Doc.Content 和 Doc.ID 转换为 semanticValidation 需要的格式

- [ ] **Step 4: 追加测试**

```go
func TestSemanticValidation_CORRECT(t *testing.T) {
	// mock LLM 返回包含 "CORRECT" 的响应
	// 验证返回匹配列表
}

func TestSemanticValidation_WRONG(t *testing.T) {
	// mock LLM 返回包含 "WRONG" 的响应
	// 验证返回空列表
}

func TestHandleMemoryWithInstruct_分离UPDATE和DELETE(t *testing.T) {
	// 构造混合 instruct 列表
	// mock searchManager + LLM
	// 验证结果正确
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/process/extract/... -run "TestSemanticValidation|TestHandleMemoryWithInstruct" -v -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/process/extract/generator.go internal/agentcore/memory/process/extract/generator_test.go
git commit -m "feat(7.19c): 实现 semanticValidation + handleMemoryWithInstruct + processMemoryOperations"
```

---

### Task 8: Generator.categoriesToMemoryUnit + GenAllMemory（核心编排）

**Files:**
- Modify: `internal/agentcore/memory/process/extract/generator.go`
- Modify: `internal/agentcore/memory/process/extract/generator_test.go`

- [ ] **Step 1: 在 generator.go 中追加 categoriesToMemoryUnit**

对齐 Python `_categories_to_memory_unit`：
- 调 `ExtractLongTermMemory`（7.18 已实现）
- 如果 `has_explict_instruct`，构建 MemoryOperationParams 并调 `handleMemoryWithInstruct`
- 调 `getFragmentMemoryUnit`
- 返回合并结果

- [ ] **Step 2: 在 generator.go 中实现 GenAllMemory（主入口）**

对齐 Python `gen_all_memory`：
- 方法签名：`func (g *Generator) GenAllMemory(ctx context.Context, params ExtractMemoryParams, memoryConfig config.AgentMemoryConfig, engineConfig *config.MemoryEngineConfig, scopeConfig *config.MemoryScopeConfig, messageMemID, timestamp string) (map[string][]mem_model.MemoryUnit, error)`
- 验证必填参数
- 调 `MemoryAnalyzer{}.Analyze`
- 调 `processExtractedData`
- 按 enable 标志判断是否继续
- 调 `processSummaryData`
- 按 `has_key_information` 判断是否继续
- 调 `categoriesToMemoryUnit`（含 triple except 对齐 Python 的 AttributeError/ValueError/BaseException → Go 中用 recover 或 error 检查）
- 按 fragment_enable 过滤
- 返回 `map[string][]mem_model.MemoryUnit`（key 为 MemoryType.String()）

- [ ] **Step 3: 追加 GenAllMemory 端到端测试**

```go
func TestGenAllMemory_完整流程(t *testing.T) {
	// mock LLM 返回分析结果 + 提取结果
	// mock SearchManager
	// 验证最终输出包含 variables + summary + fragments
}

func TestGenAllMemory_关闭长期记忆(t *testing.T) {
	// EnableLongTermMem=false
	// 验证只返回 variables
}

func TestGenAllMemory_无关键信息(t *testing.T) {
	// has_key_information=false
	// 验证只返回 variables + summary
}
```

- [ ] **Step 4: 运行全部测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/process/extract/... -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/process/extract/generator.go internal/agentcore/memory/process/extract/generator_test.go
git commit -m "feat(7.19c): 实现 GenAllMemory 核心编排 + categoriesToMemoryUnit"
```

---

### Task 9: doc.go 更新 + IMPLEMENTATION_PLAN.md 修正

**Files:**
- Modify: `internal/agentcore/memory/process/extract/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 extract/doc.go 文件目录**

追加 `analyzer.go` 和 `generator.go`：

```
//	extract/
//	├── doc.go           # 包文档
//	├── common.go        # ExtractMemoryParams + MemoryOperationParams
//	├── extractor.go     # ExtractLongTermMemory 提取函数
//	├── analyzer.go      # MemoryAnalyzer + VariableResult + MemoryAnalyzerResult
//	└── generator.go     # Generator 记忆生成编排器
```

- [ ] **Step 2: 修正 IMPLEMENTATION_PLAN.md 7.19 路径**

将 7.19 行的 Python 参考路径从 `openjiuwen/core/memory/process/refine/` 修正为 `openjiuwen/core/memory/process/extract/`，并将状态改为 ✅。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/memory/process/extract/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 extract/doc.go + 修正 7.19 路径"
```

---

### Task 10: 全量编译 + 测试验证

**Files:** 无新文件

- [ ] **Step 1: 全量编译确认无错误**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 成功

- [ ] **Step 2: 运行涉及包的测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/config/... ./internal/agentcore/memory/process/extract/... -v -count=1 -cover`
Expected: 全部 PASS，覆盖率 ≥ 85%

- [ ] **Step 3: 最终提交**

```bash
git add -A
git commit -m "feat(7.19): 完成 MemoryAnalyzer/Generator 编排器 + MemoryEngineConfig + AgentMemoryConfig + ScopeConfig 补全"
```
