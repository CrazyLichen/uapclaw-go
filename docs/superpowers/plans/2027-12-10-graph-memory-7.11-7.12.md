# GraphMemory + Graph Extraction (7.11 + 7.12) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 GraphMemory 知识图谱记忆系统（7.11）和 Graph Extraction 实体/关系抽取模块（7.12），使 Agent 具备实体间关系推理、属性聚合、去重合并能力。

**Architecture:** 7.11+7.12 合并实现。extraction 子包提供输出模型、提示词组装和 JSON 解析，graph_memory 子包提供主类管线。Schema 生成复用现有 StructSchemaExtractor + 运行时多语言 description 替换。LLM 结构化输出通过 InvokeParams 新增 ResponseFormat 字段 + 客户端按能力透传实现。

**Tech Stack:** Go 1.22+, foundation/llm (Model/Invoke), foundation/store/graph (BaseGraphStore/Entity/Relation/Episode), foundation/prompt (PromptTemplate), foundation/tool (StructSchemaExtractor), common/schema (Param/ToJSONSchemaMap)

---

## 文件结构

### 新增文件

```
internal/agentcore/memory/
├── config/
│   └── graph_config.go                         # 图记忆配置类型
├── graph/
│   ├── graph_memory/
│   │   ├── doc.go                              # 包文档
│   │   ├── base.go                             # GraphMemory 主类
│   │   ├── states.go                           # 状态结构 + persist + batch_embed
│   │   ├── parse_llm_response.go               # LLM 响应解析
│   │   ├── postprocess.go                      # 后处理
│   │   ├── utils.go                            # 辅助函数
│   │   ├── validate_input.go                   # 输入校验
│   │   ├── base_test.go                        # GraphMemory 主类测试
│   │   ├── states_test.go                      # 状态结构测试
│   │   ├── parse_llm_response_test.go          # LLM 响应解析测试
│   │   ├── postprocess_test.go                 # 后处理测试
│   │   ├── utils_test.go                       # 辅助函数测试
│   │   └── validate_input_test.go              # 输入校验测试
│   └── extraction/
│       ├── doc.go                              # 包文档
│       ├── base.go                             # 多语言替换 + ResponseFormat/ReadableSchema
│       ├── custom_types.go                     # JSONLike 类型
│       ├── entity_type_definition.go           # EntityDef/RelationDef
│       ├── extraction_models.go                # 输出模型 Go struct
│       ├── extraction_prompts.go               # 提示词组装函数
│       ├── parse_response.go                   # JSON 解析
│       ├── base_test.go                        # 多语言替换 + ResponseFormat 测试
│       ├── custom_types_test.go                # JSONLike 测试
│       ├── entity_type_definition_test.go      # EntityDef 测试
│       ├── extraction_models_test.go           # 输出模型 Schema 测试
│       ├── extraction_prompts_test.go          # 提示词组装测试
│       ├── parse_response_test.go              # JSON 解析测试
│       └── prompts/
│           ├── doc.go                          # 包文档
│           ├── manager.go                      # TemplateManager 单例
│           ├── pr_parser.go                    # .pr.md 解析器
│           ├── format_helpers.go               # 格式化辅助
│           ├── manager_test.go                 # TemplateManager 测试
│           ├── pr_parser_test.go               # 解析器测试
│           ├── format_helpers_test.go          # 格式化辅助测试
│           ├── cn/
│           │   ├── register.go                 # 中文注册
│           │   └── *.pr.md                     # 中文模板文件（从 Python 复制）
│           ├── en/
│           │   ├── register.go                 # 英文注册
│           │   └── *.pr.md                     # 英文模板文件（从 Python 复制）
│           └── entity_extraction/
│               ├── doc.go
│               └── format.go                   # cn/en 格式化辅助
```

### 修改文件

```
internal/agentcore/foundation/llm/model_clients/invoke_params.go   # 新增 ResponseFormat 字段 + WithResponseFormat
internal/agentcore/foundation/llm/model_clients/base_client.go     # BuildRequestParams 处理 ResponseFormat
internal/agentcore/foundation/llm/model.go                         # Model.Invoke 透传 ResponseFormat
internal/agentcore/memory/migration/migration_plan.go              # 无变更，仅确认不影响
```

---

## Task 1: 图记忆配置类型

**Files:**
- Create: `internal/agentcore/memory/config/graph_config.go`
- Test: `internal/agentcore/memory/config/graph_config_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/agentcore/memory/config/graph_config_test.go`:

```go
package config

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph/ranking"
)

func TestEpisodeType_String(t *testing.T) {
	if EpisodeTypeConversation.String() != "CONVERSATION" {
		t.Errorf("期望 CONVERSATION，实际 %s", EpisodeTypeConversation.String())
	}
	if EpisodeTypeDocument.String() != "DOCUMENT" {
		t.Errorf("期望 DOCUMENT，实际 %s", EpisodeTypeDocument.String())
	}
	if EpisodeTypeJSON.String() != "JSON" {
		t.Errorf("期望 JSON，实际 %s", EpisodeTypeJSON.String())
	}
}

func TestAddMemStrategy_默认值(t *testing.T) {
	s := DefaultAddMemStrategy
	if !s.ChineseEntity {
		t.Error("ChineseEntity 默认应为 true")
	}
	if s.ChineseEntityDedupe {
		t.Error("ChineseEntityDedupe 默认应为 false")
	}
	if !s.MergeEntities {
		t.Error("MergeEntities 默认应为 true")
	}
	if s.SummaryTarget != 250 {
		t.Errorf("SummaryTarget 默认应为 250，实际 %d", s.SummaryTarget)
	}
}

func TestSearchConfig_默认值(t *testing.T) {
	sc := DefaultSearchConfig
	if sc.BFSK != 3 {
		t.Errorf("BFSK 默认应为 3，实际 %d", sc.BFSK)
	}
	if sc.BFSDepth != 0 {
		t.Errorf("BFSDepth 默认应为 0，实际 %d", sc.BFSDepth)
	}
	if sc.Language != "en" {
		t.Errorf("Language 默认应为 en，实际 %s", sc.Language)
	}
}

func TestRetrievalStrategy_默认值(t *testing.T) {
	rs := DefaultRetrievalStrategy
	if rs.TopK != 3 {
		t.Errorf("TopK 默认应为 3，实际 %d", rs.TopK)
	}
	if rs.MinScore != 0.3 {
		t.Errorf("MinScore 默认应为 0.3，实际 %f", rs.MinScore)
	}
}

func TestEpisodeRetrievalStrategy_默认值(t *testing.T) {
	ers := DefaultEpisodeRetrievalStrategy
	if ers.ExcludeFutureResults {
		t.Error("ExcludeFutureResults 默认应为 true，实际 false") // 修正：应为 true
	}
	if !ers.ExcludeFutureResults {
		t.Error("ExcludeFutureResults 默认应为 true")
	}
	if ers.MinScore != 0.025 {
		t.Errorf("MinScore 默认应为 0.025，实际 %f", ers.MinScore)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/config/... -v -run "TestEpisodeType|TestAddMem|TestSearchConfig|TestRetrieval|TestEpisodeRetrieval" 2>&1 | head -20`
Expected: 编译失败，包不存在

- [ ] **Step 3: 实现 graph_config.go**

创建 `internal/agentcore/memory/config/graph_config.go`，对齐 Python `openjiuwen/core/memory/config/graph.py`。

包含：
- `EpisodeType` 枚举（CONVERSATION/DOCUMENT/JSON）
- `BaseStrategy` 结构体（TopK/MinScore/RankConfig）
- `RetrievalStrategy` 结构体（嵌入 BaseStrategy + SameKind）
- `EpisodeRetrievalStrategy` 结构体（嵌入 RetrievalStrategy + ExcludeFutureResults）
- `AddMemStrategy` 结构体（ChineseEntity/ChineseEntityDedupe/ChineseRelation/SkipUUIDDedupe/RecallEpisode/RecallEntity/RecallRelation/SummaryTarget/MergeEntities/MergeRelations/MergeFilter）
- `SearchConfig` 结构体（嵌入 BaseStrategy + BFSK/BFSDepth/FilterExpr/OutputFields/Rerank/Language）
- 各类型的默认值常量（`DefaultAddMemStrategy`、`DefaultSearchConfig` 等）
- 注意：RankConfig 字段类型引用 `graph.BaseRankConfig`、`graph.RRFRankConfig`、`graph.WeightedRankConfig`；FilterExpr 引用 `query.QueryExpr`

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/config/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/config/graph_config.go internal/agentcore/memory/config/graph_config_test.go
git commit -m "feat(memory): 添加图记忆配置类型 (7.11 前置)"
```

---

## Task 2: LLM InvokeParams 新增 ResponseFormat 字段

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/invoke_params.go`
- Modify: `internal/agentcore/foundation/llm/model_clients/base_client.go`
- Test: `internal/agentcore/foundation/llm/model_clients/invoke_params_test.go`

- [ ] **Step 1: 写失败测试**

在 `invoke_params_test.go` 末尾添加：

```go
func TestWithResponseFormat(t *testing.T) {
	schema := map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "test"}}
	p := NewInvokeParams(WithResponseFormat(schema))
	if p.ResponseFormat == nil {
		t.Error("ResponseFormat 不应为 nil")
	}
	if p.ResponseFormat["type"] != "json_schema" {
		t.Errorf("ResponseFormat.type 期望 json_schema，实际 %v", p.ResponseFormat["type"])
	}
}

func TestWithStreamResponseFormat(t *testing.T) {
	schema := map[string]any{"type": "json_schema"}
	p := NewStreamParams(WithStreamResponseFormat(schema))
	if p.ResponseFormat == nil {
		t.Error("ResponseFormat 不应为 nil")
	}
}

func TestBuildRequestParams_ResponseFormat(t *testing.T) {
	e := newTestBaseClientEmbed(t)
	messagesDict := []map[string]any{{"role": "user", "content": "hi"}}
	schema := map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "test", "schema": map[string]any{}}}
	params := NewInvokeParams(WithResponseFormat(schema))
	result, err := e.BuildRequestParams(context.Background(), messagesDict, params, false)
	if err != nil {
		t.Fatalf("BuildRequestParams 报错: %v", err)
	}
	rf, ok := result["response_format"]
	if !ok {
		t.Error("response_format 应出现在请求参数中")
	}
	rfMap, ok := rf.(map[string]any)
	if !ok || rfMap["type"] != "json_schema" {
		t.Errorf("response_format 内容不正确: %v", rf)
	}
}

func TestBuildRequestParams_ResponseFormat为空时不传入(t *testing.T) {
	e := newTestBaseClientEmbed(t)
	messagesDict := []map[string]any{{"role": "user", "content": "hi"}}
	params := NewInvokeParams()
	result, err := e.BuildRequestParams(context.Background(), messagesDict, params, false)
	if err != nil {
		t.Fatalf("BuildRequestParams 报错: %v", err)
	}
	if _, ok := result["response_format"]; ok {
		t.Error("ResponseFormat 为 nil 时不应传入请求参数")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/llm/model_clients/... -v -run "TestWithResponseFormat|TestBuildRequestParams_ResponseFormat" 2>&1 | tail -20`
Expected: 编译失败，字段/函数未定义

- [ ] **Step 3: 修改 InvokeParams 和 StreamParams**

在 `invoke_params.go` 中：
1. `InvokeParams` 结构体新增 `ResponseFormat map[string]any` 字段（在 `Extra` 之前）
2. `StreamParams` 结构体新增 `ResponseFormat map[string]any` 字段（在 `Extra` 之前）
3. 新增 `WithResponseFormat(schemaMap map[string]any) InvokeOption` 函数
4. 新增 `WithStreamResponseFormat(schemaMap map[string]any) StreamOption` 函数
5. `StreamParams.ToStreamParams()` 中补充 `ResponseFormat: p.ResponseFormat` 字段拷贝

- [ ] **Step 4: 修改 BuildRequestParams**

在 `base_client.go` 的 `BuildRequestParams` 方法中，在步骤 5（合并 params.Extra）之后，新增步骤处理 ResponseFormat：

```go
// 6. 处理 ResponseFormat（结构化输出）
if params.ResponseFormat != nil {
    reqParams["response_format"] = params.ResponseFormat
}
```

注意：将原有的步骤 6（日志记录）改为步骤 7，baseKeys 中增加 `"response_format": true`。

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/llm/model_clients/... -v -run "TestWithResponseFormat|TestBuildRequestParams_ResponseFormat"`
Expected: PASS

- [ ] **Step 6: 运行全量 LLM 测试确认无回归**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/llm/... -count=1`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/invoke_params.go internal/agentcore/foundation/llm/model_clients/base_client.go internal/agentcore/foundation/llm/model_clients/invoke_params_test.go
git commit -m "feat(llm): InvokeParams 新增 ResponseFormat 字段支持结构化输出 (7.11 前置)"
```

---

## Task 3: 复制提示词模板文件

**Files:**
- Create: `internal/agentcore/memory/graph/extraction/prompts/cn/*.pr.md` (12 个文件)
- Create: `internal/agentcore/memory/graph/extraction/prompts/en/*.pr.md` (12 个文件)

- [ ] **Step 1: 创建目录结构**

```bash
mkdir -p internal/agentcore/memory/graph/extraction/prompts/cn
mkdir -p internal/agentcore/memory/graph/extraction/prompts/en
mkdir -p internal/agentcore/memory/graph/extraction/prompts/entity_extraction
```

- [ ] **Step 2: 复制中文提示词**

```bash
cp /home/opensource/agent-core/openjiuwen/core/memory/graph/extraction/prompts/cn/*.pr.md internal/agentcore/memory/graph/extraction/prompts/cn/
```

- [ ] **Step 3: 复制英文提示词**

```bash
cp /home/opensource/agent-core/openjiuwen/core/memory/graph/extraction/prompts/en/*.pr.md internal/agentcore/memory/graph/extraction/prompts/en/
```

- [ ] **Step 4: 验证文件数量**

```bash
ls internal/agentcore/memory/graph/extraction/prompts/cn/*.pr.md | wc -l
ls internal/agentcore/memory/graph/extraction/prompts/en/*.pr.md | wc -l
```
Expected: 各 12 个文件

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/graph/extraction/prompts/cn/ internal/agentcore/memory/graph/extraction/prompts/en/
git commit -m "feat(memory): 复制 graph extraction 提示词模板 (7.12)"
```

---

## Task 4: .pr.md 解析器 + TemplateManager

**Files:**
- Create: `internal/agentcore/memory/graph/extraction/prompts/pr_parser.go`
- Create: `internal/agentcore/memory/graph/extraction/prompts/manager.go`
- Test: `internal/agentcore/memory/graph/extraction/prompts/pr_parser_test.go`
- Test: `internal/agentcore/memory/graph/extraction/prompts/manager_test.go`
- Create: `internal/agentcore/memory/graph/extraction/prompts/doc.go`

- [ ] **Step 1: 写 pr_parser 失败测试**

创建 `pr_parser_test.go`，测试解析 `.pr.md` 格式：

```go
package prompts

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

func TestParsePRContent_单角色(t *testing.T) {
	content := "`#system#`你是助手。"
	msgs := ParsePRContent(content)
	if len(msgs) != 1 {
		t.Fatalf("期望 1 条消息，实际 %d", len(msgs))
	}
	if msgs[0].Role() != "system" {
		t.Errorf("期望 role=system，实际 %s", msgs[0].Role())
	}
}

func TestParsePRContent_多角色(t *testing.T) {
	content := "`#system#`你是助手。`#user#`请提取实体。"
	msgs := ParsePRContent(content)
	if len(msgs) != 2 {
		t.Fatalf("期望 2 条消息，实际 %d", len(msgs))
	}
	if msgs[0].Role() != "system" {
		t.Errorf("消息[0] 期望 role=system，实际 %s", msgs[0].Role())
	}
	if msgs[1].Role() != "user" {
		t.Errorf("消息[1] 期望 role=user，实际 %s", msgs[1].Role())
	}
}

func TestParsePRContent_空内容(t *testing.T) {
	msgs := ParsePRContent("")
	if len(msgs) != 0 {
		t.Errorf("期望 0 条消息，实际 %d", len(msgs))
	}
}

func TestParsePRContent_未知角色跳过(t *testing.T) {
	content := "`#system#`你是助手。`#unknown#`无效内容`#user#`请提取。"
	msgs := ParsePRContent(content)
	if len(msgs) != 2 {
		t.Fatalf("期望 2 条消息（跳过 unknown），实际 %d", len(msgs))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/extraction/prompts/... -v -run TestParsePRContent`
Expected: 编译失败

- [ ] **Step 3: 实现 pr_parser.go**

创建 `pr_parser.go`，包含：
- `prPattern` 正则：`` `#(user|system|assistant|tool)#` ``
- `ParsePRContent(content string) []llmschema.BaseMessage` 函数
- 解析逻辑：用正则分割内容，交替匹配角色和内容，根据角色调用 `schema.NewSystemMessage`/`NewUserMessage`/`NewAssistantMessage`/`NewToolMessage`

- [ ] **Step 4: 运行 pr_parser 测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/extraction/prompts/... -v -run TestParsePRContent`
Expected: PASS

- [ ] **Step 5: 写 manager 失败测试**

创建 `manager_test.go`，测试 TemplateManager 加载模板：

```go
func TestTemplateManager_加载模板(t *testing.T) {
	mgr := GetTemplateManager()
	if mgr == nil {
		t.Fatal("TemplateManager 不应为 nil")
	}
	// 验证已注册的模板名
	if !mgr.Contains("entity_extraction_conversation_cn") {
		t.Error("应包含 entity_extraction_conversation_cn 模板")
	}
}

func TestTemplateManager_Get返回模板(t *testing.T) {
	mgr := GetTemplateManager()
	tmpl := mgr.Get("entity_extraction_conversation_cn")
	if tmpl == nil {
		t.Fatal("模板不应为 nil")
	}
	if tmpl.Name != "entity_extraction_conversation_cn" {
		t.Errorf("模板名期望 entity_extraction_conversation_cn，实际 %s", tmpl.Name)
	}
}

func TestTemplateManager_Get不存在的模板(t *testing.T) {
	mgr := GetTemplateManager()
	tmpl := mgr.Get("nonexistent_template")
	if tmpl != nil {
		t.Error("不存在的模板应返回 nil")
	}
}
```

- [ ] **Step 6: 实现 manager.go**

创建 `manager.go`，对齐 Python `ThreadSafePromptManager`：
- `TemplateManager` 结构体（`sync.RWMutex` + `templates map[string]*prompt.PromptTemplate`）
- `GetTemplateManager()` 单例函数（`sync.Once`）
- `Get(name string) *prompt.PromptTemplate`
- `Contains(name string) bool`
- 初始化逻辑：glob 扫描 `prompts/cn/*.pr.md` 和 `prompts/en/*.pr.md`，解析为 PromptTemplate 并缓存
- 注意：模板文件路径通过 `runtime.Caller` 获取当前文件目录的相对路径

- [ ] **Step 7: 运行 manager 测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/extraction/prompts/... -v -run TestTemplateManager`
Expected: PASS

- [ ] **Step 8: 创建 doc.go**

创建 `internal/agentcore/memory/graph/extraction/prompts/doc.go`

- [ ] **Step 9: 提交**

```bash
git add internal/agentcore/memory/graph/extraction/prompts/
git commit -m "feat(memory): 添加 .pr.md 解析器和 TemplateManager (7.12)"
```

---

## Task 5: 多语言注册 + 格式化辅助

**Files:**
- Create: `internal/agentcore/memory/graph/extraction/prompts/cn/register.go`
- Create: `internal/agentcore/memory/graph/extraction/prompts/en/register.go`
- Create: `internal/agentcore/memory/graph/extraction/prompts/format_helpers.go`
- Create: `internal/agentcore/memory/graph/extraction/prompts/entity_extraction/format.go`
- Create: `internal/agentcore/memory/graph/extraction/prompts/entity_extraction/doc.go`
- Test: `internal/agentcore/memory/graph/extraction/prompts/format_helpers_test.go`

- [ ] **Step 1: 写失败测试**

创建 `format_helpers_test.go`，测试格式化辅助函数：

```go
func TestFormatSourceDescription_有描述(t *testing.T) {
	result := FormatSourceDescription("测试数据源", "cn")
	if result == "" {
		t.Error("有描述时应返回非空字符串")
	}
}

func TestFormatSourceDescription_无描述(t *testing.T) {
	result := FormatSourceDescription("", "cn")
	if result != "" {
		t.Error("无描述时应返回空字符串")
	}
}

func TestFormatExistingEntities_基本(t *testing.T) {
	entities := []map[string]any{
		{"name": "张三", "content": "工程师"},
	}
	result := FormatExistingEntities(entities, 1, "cn")
	if result == "" {
		t.Error("应返回格式化后的实体字符串")
	}
}

func TestFormatExistingRelations_基本(t *testing.T) {
	relations := []map[string]any{
		{"content": "张三在华为工作"},
	}
	result := FormatExistingRelations(relations, 1, true)
	if result == "" {
		t.Error("应返回格式化后的关系字符串")
	}
}

func TestEnsureValidLanguage_有效(t *testing.T) {
	lang, err := EnsureValidLanguage("cn", 10)
	if err != nil {
		t.Errorf("cn 应为有效语言，报错: %v", err)
	}
	if lang != "cn" {
		t.Errorf("期望 cn，实际 %s", lang)
	}
}

func TestEnsureValidLanguage_无效(t *testing.T) {
	_, err := EnsureValidLanguage("xx", 10)
	if err == nil {
		t.Error("无效语言应返回错误")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 cn/register.go**

对齐 Python `entity_extraction/cn.py`，包含：
- `init()` 中注册 `MULTILINGUAL_DESCRIPTION["cn"]`（完整映射表，包含所有 `{{[xxx]}}` 占位符到中文描述的映射）
- 注册 `REGISTERED_LANGUAGE`、`SOURCE_DESCRIPTION`、`OUTPUT_FORMAT`、`DISPLAY_ENTITY`、`MARK_CURRENT_MSG`、`MARK_HISTORY_MSG`、`RELATION_FORMAT`、`NO_RELATION_GIVEN` 等全局 map

- [ ] **Step 4: 实现 en/register.go**

对齐 Python `entity_extraction/en.py`，同 cn 结构，但值为英文。

- [ ] **Step 5: 实现 format_helpers.go**

对齐 Python `entity_extraction/base.py`，包含：
- `FormatSchemaInfo` — 将 ReadableSchema 拼接到提示词末尾
- `FormatSourceDescription` — 格式化数据源描述
- `GetFormattingKwargs` — 组装提示词模板变量
- `FormatRelationDefinitions` — 格式化关系类型定义
- `FormatExistingRelations` — 格式化已有关系列表
- `FormatExistingEntities` — 格式化已有实体列表
- `EnsureValidLanguage` — 校验语言支持

- [ ] **Step 6: 实现 entity_extraction/format.go**

提供 `format_new_entities` 等对齐 Python `format_new_entities()` 的函数。

- [ ] **Step 7: 运行测试确认通过**

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/memory/graph/extraction/prompts/cn/register.go internal/agentcore/memory/graph/extraction/prompts/en/register.go internal/agentcore/memory/graph/extraction/prompts/format_helpers.go internal/agentcore/memory/graph/extraction/prompts/format_helpers_test.go internal/agentcore/memory/graph/extraction/prompts/entity_extraction/
git commit -m "feat(memory): 添加多语言注册和格式化辅助 (7.12)"
```

---

## Task 6: Extraction 输出模型 + Schema 生成

**Files:**
- Create: `internal/agentcore/memory/graph/extraction/custom_types.go`
- Create: `internal/agentcore/memory/graph/extraction/entity_type_definition.go`
- Create: `internal/agentcore/memory/graph/extraction/extraction_models.go`
- Create: `internal/agentcore/memory/graph/extraction/base.go`
- Create: `internal/agentcore/memory/graph/extraction/doc.go`
- Test: `internal/agentcore/memory/graph/extraction/extraction_models_test.go`
- Test: `internal/agentcore/memory/graph/extraction/base_test.go`

- [ ] **Step 1: 写失败测试**

创建 `extraction_models_test.go`，测试 Schema 生成：

```go
func TestEntityExtraction_Schema生成(t *testing.T) {
	params, err := tool.StructSchemaExtractor{}.Extract(reflect.TypeOf(EntityExtraction{}))
	if err != nil {
		t.Fatalf("Extract 报错: %v", err)
	}
	schemaMap := commonschema.ToJSONSchemaMap(params)
	if schemaMap["type"] != "object" {
		t.Errorf("顶层 type 应为 object，实际 %v", schemaMap["type"])
	}
	props, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties 应为 map")
	}
	if _, ok := props["extracted_entities"]; !ok {
		t.Error("应包含 extracted_entities 属性")
	}
}

func TestReplaceDescriptions_中文替换(t *testing.T) {
	params, _ := tool.StructSchemaExtractor{}.Extract(reflect.TypeOf(EntitySummary{}))
	// 替换前，description 应为占位符
	found := false
	for _, p := range params {
		if p.Name == "summary" && strings.Contains(p.Description, "{{[ent_summary]}}") {
			found = true
		}
	}
	if !found {
		t.Error("替换前应找到占位符 description")
	}
	// 执行替换
	cnMap := GetMultilingualDescription("cn")
	replaced := ReplaceDescriptions(params, cnMap)
	for _, p := range replaced {
		if p.Name == "summary" {
			if p.Description == "{{[ent_summary]}}" {
				t.Error("替换后 description 不应仍为占位符")
			}
		}
	}
}

func TestResponseFormat_包装(t *testing.T) {
	params, _ := tool.StructSchemaExtractor{}.Extract(reflect.TypeOf(EntityExtraction{}))
	cnMap := GetMultilingualDescription("cn")
	replaced := ReplaceDescriptions(params, cnMap)
	schemaMap := commonschema.ToJSONSchemaMap(replaced)
	rf := ResponseFormat("EntityExtraction", schemaMap)
	if rf["type"] != "json_schema" {
		t.Errorf("type 应为 json_schema，实际 %v", rf["type"])
	}
	js, ok := rf["json_schema"].(map[string]any)
	if !ok {
		t.Fatal("json_schema 应为 map")
	}
	if js["name"] != "EntityExtraction" {
		t.Errorf("name 应为 EntityExtraction，实际 %v", js["name"])
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 custom_types.go**

```go
// JSONLike JSON 类型的通用别名
type JSONLike = any
```

- [ ] **Step 4: 实现 entity_type_definition.go**

对齐 Python `entity_type_definition.py`，包含：
- `EntityDef` 结构体（Name/Description/Attributes）
- `RelationDef` 结构体（Name/Description/LHS/RHS）
- `HumanEntity` 和 `AIEntity` 预定义类型
- 全局 description map（`ENTITY_DEFINITION_DESCRIPTION` 等），由 cn/en register 填充

- [ ] **Step 5: 实现 extraction_models.go**

对齐 Python `extraction_models.py`，定义所有输出模型 Go struct：
- `EntityDeclaration` — 实体声明（Name/EntityTypeID）
- `Duplication` — 实体去重（Name/ID/DuplicateIDs）
- `Fact` — 事实关系（Name/Fact/ValidSince/ValidUntil/SourceID/TargetID）
- `PossibleTimezone` — 时区预测
- `EntityExtraction` — 实体抽取输出
- `EntitySummary` — 实体摘要输出
- `EntityDuplication` — 实体去重输出
- `RelationExtraction` — 关系抽取输出
- `RelevantFacts` — 关系过滤输出
- `TimezonePredictions` — 时区预测输出
- `MergeRelations` — 关系合并输出

每个 struct 的 json tag 和 jsonschema tag 对齐 Python 的 Field 定义。

- [ ] **Step 6: 实现 base.go**

对齐 Python `base.py`，包含：
- `MULTILINGUAL_DESCRIPTION` 全局注册表（`map[string]map[string]string`）
- `GetMultilingualDescription(lang string) map[string]string` 获取函数
- `ReplaceDescriptions(params []*schema.Param, langMap map[string]string) []*schema.Param` 递归替换函数
- `ResponseFormat(name string, schemaMap map[string]any) map[string]any` 包装函数
- `ReadableSchema(modelType reflect.Type, language string) (string, map[string]map[string]any)` 可读 Schema 生成函数

- [ ] **Step 7: 运行测试确认通过**

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/memory/graph/extraction/
git commit -m "feat(memory): 添加 extraction 输出模型和 Schema 生成 (7.12)"
```

---

## Task 7: JSON 解析器 + 提示词组装函数

**Files:**
- Create: `internal/agentcore/memory/graph/extraction/parse_response.go`
- Create: `internal/agentcore/memory/graph/extraction/extraction_prompts.go`
- Test: `internal/agentcore/memory/graph/extraction/parse_response_test.go`
- Test: `internal/agentcore/memory/graph/extraction/extraction_prompts_test.go`

- [ ] **Step 1: 写 parse_response 失败测试**

创建 `parse_response_test.go`，对齐 Python `parse_response.py` 的测试场景：

```go
func TestParseJSON_标准JSON(t *testing.T) {
	result := ParseJSON(`{"key": "value"}`, nil)
	if result == nil {
		t.Fatal("解析结果不应为 nil")
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("应为 map 类型")
	}
	if m["key"] != "value" {
		t.Errorf("key 期望 value，实际 %v", m["key"])
	}
}

func TestParseJSON_代码块中提取(t *testing.T) {
	input := "结果如下：\n```json\n{\"entities\": []}\n```"
	result := ParseJSON(input, nil)
	if result == nil {
		t.Fatal("应从代码块中提取 JSON")
	}
}

func TestParseJSON_截断修复(t *testing.T) {
	input := `{"relations": [{"fact": "a"}, {"fact": "b"},`
	result := ParseJSON(input, nil)
	if result == nil {
		t.Fatal("截断 JSON 应被修复")
	}
}

func TestParseJSON_空输入(t *testing.T) {
	result := ParseJSON("", nil)
	if result != nil {
		t.Error("空输入应返回 nil")
	}
}

func TestEnsureList_列表(t *testing.T) {
	result := EnsureList([]any{1, 2, 3})
	if len(result) != 3 {
		t.Errorf("期望 3 个元素，实际 %d", len(result))
	}
}

func TestEnsureList_单元素字典(t *testing.T) {
	result := EnsureList(map[string]any{"key": []any{1, 2}})
	arr, ok := result.([]any)
	if !ok {
		t.Fatal("应为 []any 类型")
	}
	if len(arr) != 2 {
		t.Errorf("期望 2 个元素，实际 %d", len(arr))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 parse_response.go**

对齐 Python `parse_response.py`，包含：
- `regexFindJSONStart` 和 `regexFindCodeBlock` 正则
- `ParseJSON(resp string, outputSchema map[string]any) JSONLike` — 从 LLM 响应中尝试多种方式提取 JSON
- `_rawDecodeJSON` — 容错解析（截断修复：`lastRelationIdx` 处理）
- `TryGetKey` — 模糊 key 匹配（使用 `difflib` 等价逻辑）
- `EnsureList(obj any) []any` — 确保返回列表

注意：Go 没有 Python 的 `json.JSONDecoder.raw_decode`，需要用 `encoding/json.Decoder` 的 `More()` + `Decode()` 实现等价容错逻辑。

- [ ] **Step 4: 运行 parse_response 测试确认通过**

- [ ] **Step 5: 写 extraction_prompts 测试**

创建 `extraction_prompts_test.go`，测试提示词组装函数的输出格式：

```go
func TestExtractEntityDeclaration_返回值(t *testing.T) {
	kwargs, tmpl, outputFmt := ExtractEntityDeclaration(
		EpisodeTypeConversation, "测试内容", "", nil, nil,
		WithExtractLanguage("cn"),
	)
	if tmpl == nil {
		t.Fatal("模板不应为 nil")
	}
	if kwargs == nil {
		t.Fatal("kwargs 不应为 nil")
	}
	if outputFmt == nil {
		t.Fatal("outputFmt 不应为 nil")
	}
	if _, ok := kwargs["entity_types"]; !ok {
		t.Error("kwargs 应包含 entity_types")
	}
	if outputFmt["type"] != "json_schema" {
		t.Errorf("outputFmt type 应为 json_schema，实际 %v", outputFmt["type"])
	}
}
```

- [ ] **Step 6: 实现 extraction_prompts.go**

对齐 Python `extraction_prompts.py`，包含所有提示词组装函数：
- `ExtractEntityDeclaration` — 实体声明抽取提示词
- `ExtractEntityAttributes` — 实体属性抽取提示词
- `ExtractRelationDeclaration` — 关系抽取提示词
- `ExtractTimezone` — 时区抽取提示词
- `MergeExistingEntities` — 实体合并提示词
- `FilterRelationsForMerge` — 关系过滤提示词
- `DedupeEntityList` — 实体去重提示词
- `DedupeRelationList` — 关系去重提示词
- `FormatNewEntities` — 格式化新实体列表

每个函数返回 `(kwargs map[string]string, tmpl *prompt.PromptTemplate, outputFmt map[string]any)`。

- [ ] **Step 7: 运行 extraction_prompts 测试确认通过**

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/memory/graph/extraction/parse_response.go internal/agentcore/memory/graph/extraction/extraction_prompts.go internal/agentcore/memory/graph/extraction/parse_response_test.go internal/agentcore/memory/graph/extraction/extraction_prompts_test.go
git commit -m "feat(memory): 添加 JSON 解析器和提示词组装函数 (7.12)"
```

---

## Task 8: GraphMemory 输入校验 + 辅助函数

**Files:**
- Create: `internal/agentcore/memory/graph/graph_memory/validate_input.go`
- Create: `internal/agentcore/memory/graph/graph_memory/utils.go`
- Create: `internal/agentcore/memory/graph/graph_memory/doc.go`
- Test: `internal/agentcore/memory/graph/graph_memory/validate_input_test.go`
- Test: `internal/agentcore/memory/graph/graph_memory/utils_test.go`

- [ ] **Step 1: 写 validate_input 失败测试**

创建 `validate_input_test.go`，对齐 Python `validate_input.py` 的边界条件：

```go
func TestValidateAddMemoryInput_有效输入(t *testing.T) {
	err := ValidateAddMemoryInput(32, EpisodeTypeConversation, "user123", nil)
	if err != nil {
		t.Errorf("有效输入不应报错: %v", err)
	}
}

func TestValidateAddMemoryInput_空UserID(t *testing.T) {
	err := ValidateAddMemoryInput(32, EpisodeTypeConversation, "", nil)
	if err == nil {
		t.Error("空 UserID 应报错")
	}
}

func TestValidateAddMemoryInput_超长UserID(t *testing.T) {
	err := ValidateAddMemoryInput(32, EpisodeTypeConversation, "123456789012345678901234567890123", nil)
	if err == nil {
		t.Error("超长 UserID 应报错")
	}
}

func TestValidateSearchInput_有效输入(t *testing.T) {
	userIDs, err := ValidateSearchInput("测试查询", "user123", []bool{true, true, true})
	if err != nil {
		t.Errorf("有效输入不应报错: %v", err)
	}
	if len(userIDs) != 1 || userIDs[0] != "user123" {
		t.Errorf("UserID 列表不正确: %v", userIDs)
	}
}

func TestValidateSearchInput_空查询(t *testing.T) {
	_, err := ValidateSearchInput("", "user123", []bool{true, true, true})
	if err == nil {
		t.Error("空查询应报错")
	}
}

func TestValidateSearchInput_UserID列表(t *testing.T) {
	userIDs, err := ValidateSearchInput("查询", []string{"u1", "u2"}, []bool{true, true, true})
	if err != nil {
		t.Errorf("列表形式 UserID 不应报错: %v", err)
	}
	if len(userIDs) != 2 {
		t.Errorf("期望 2 个 UserID，实际 %d", len(userIDs))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 validate_input.go**

对齐 Python `validate_input.py`：
- `ValidateAddMemoryInput(userIDMaxLength int, srcType EpisodeType, userID string, contentFmtKwargs map[string]string) error`
- `ValidateSearchInput(query string, userID any, settings []bool) ([]string, error)`

- [ ] **Step 4: 写 utils 测试**

创建 `utils_test.go`，测试 `Msg2Dict`、`UpdateEntity`、`AssembleInvokeParams`：

```go
func TestMsg2Dict_字符串消息(t *testing.T) {
	msgs := []llmschema.BaseMessage{llmschema.NewUserMessage("你好")}
	result, err := Msg2Dict(msgs, false)
	if err != nil {
		t.Fatalf("报错: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("期望 1 条，实际 %d", len(result))
	}
	if result[0]["role"] != "user" {
		t.Errorf("role 期望 user，实际 %v", result[0]["role"])
	}
}

func TestMsg2Dict_非消息类型报错(t *testing.T) {
	_, err := Msg2Dict([]int{1, 2}, false)
	if err == nil {
		t.Error("非消息类型应报错")
	}
}

func TestUpdateEntity_有效摘要(t *testing.T) {
	entity := graph.NewEntity()
	entity.Name = "张三"
	UpdateEntity(entity, `{"summary": "工程师", "attributes": {"部门": "研发"}}`, map[string]any{})
	if entity.Content != "工程师" {
		t.Errorf("Content 期望 工程师，实际 %s", entity.Content)
	}
	if entity.Attributes == nil || entity.Attributes["部门"] != "研发" {
		t.Errorf("Attributes 不正确: %v", entity.Attributes)
	}
}

func TestAssembleInvokeParams_基本(t *testing.T) {
	kwargs := map[string]any{"key1": "value1"}
	tmpl := prompt.NewPromptTemplate("test", "hello {{key1}}")
	params := AssembleInvokeParams(kwargs, tmpl, nil)
	if len(params) == 0 {
		t.Error("params 不应为空")
	}
}
```

- [ ] **Step 5: 实现 utils.go**

对齐 Python `utils.py`：
- `Msg2Dict(messages any, preserveMeta bool) ([]map[string]any, error)`
- `UpdateEntity(entity *graph.Entity, response string, extractionSchema map[string]any)`
- `AssembleInvokeParams(kwargs map[string]any, tmpl *prompt.PromptTemplate, outputModel map[string]any) map[string]any`

- [ ] **Step 6: 运行所有测试确认通过**

- [ ] **Step 7: 创建 doc.go**

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/memory/graph/graph_memory/
git commit -m "feat(memory): 添加 GraphMemory 输入校验和辅助函数 (7.11)"
```

---

## Task 9: GraphMemory 状态结构

**Files:**
- Create: `internal/agentcore/memory/graph/graph_memory/states.go`
- Test: `internal/agentcore/memory/graph/graph_memory/states_test.go`

- [ ] **Step 1: 写失败测试**

创建 `states_test.go`，测试状态结构行为：

```go
func TestLookupTables_GetEntity(t *testing.T) {
	lt := NewLookupTables()
	entity := &graph.Entity{}
	entity.UUID = "test-uuid"
	input := map[string]any{"uuid": "test-uuid", "name": "测试", "content": ""}
	got := lt.GetEntity(input)
	if got.UUID != "test-uuid" {
		t.Errorf("UUID 期望 test-uuid，实际 %s", got.UUID)
	}
	// 重复获取应返回同一对象
	got2 := lt.GetEntity(input)
	if got != got2 {
		t.Error("重复获取应返回同一对象")
	}
}

func TestGraphMemUpdate_合并(t *testing.T) {
	a := &GraphMemUpdate{}
	a.AddedEntity = []*graph.Entity{{}}
	a.RemovedEntity = map[string]struct{}{"old1": {}}

	b := &GraphMemUpdate{}
	b.AddedEntity = []*graph.Entity{{}}
	b.RemovedEntity = map[string]struct{}{"old2": {}}

	merged := a.Merge(b)
	if len(merged.AddedEntity) != 2 {
		t.Errorf("AddedEntity 合并后应 2 个，实际 %d", len(merged.AddedEntity))
	}
	if len(merged.RemovedEntity) != 2 {
		t.Errorf("RemovedEntity 合并后应 2 个，实际 %d", len(merged.RemovedEntity))
	}
}

func TestGraphMemState_ClearReferences(t *testing.T) {
	state := NewGraphMemState()
	state.Content = "test"
	state.LookupTable.Entities["a"] = graph.NewEntity()
	state.ClearReferences()
	if len(state.LookupTable.Entities) != 0 {
		t.Error("ClearReferences 应清空 LookupTable")
	}
}

func TestBatchEmbed_空输入(t *testing.T) {
	result, err := BatchEmbed(context.Background(), nil, nil, nil)
	if err != nil {
		t.Errorf("空输入不应报错: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("空输入应返回空切片，实际 %d", len(result))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 states.go**

对齐 Python `states.py`，包含：
- `LookupTables` 结构体 + `GetEntity`/`GetRelation`/`GetEpisode`/`Clear` 方法
- `EntityMerge` 结构体 + `Clear` 方法
- `GraphMemUpdate` 结构体 + `Merge` 方法（对齐 Python `__or__`）
- `GraphMemPrompting` 结构体 + `Clear` 方法
- `GraphMemState` 结构体（完整状态：Tasks/MergingTasks/PendingMerge/RelationDeferredUpdates/RelationFilterTasks/ToRemove/TmpBuffer/UpdatedEntitiesInCurrentEp/RetrievedEntities/RetrievedRelations/FaultyRelations/MergeInfos/MemUpdate/MemUpdateSkipEmbed/CurrentTimestamp/ReferenceTimestamp/LookupTable/Extras/Strategy/Prompting/EntityTypes/EpisodeType/Content/History）+ `ClearReferences` 方法
- `BatchEmbed` 函数
- `PersistToDB` 函数
- `ClassifyRelationsExtracted` 函数
- `BlockKeyboardInterrupt` 函数（Go 用 `os.Signal` 通道实现）

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/graph/graph_memory/states.go internal/agentcore/memory/graph/graph_memory/states_test.go
git commit -m "feat(memory): 添加 GraphMemory 状态结构 (7.11)"
```

---

## Task 10: LLM 响应解析

**Files:**
- Create: `internal/agentcore/memory/graph/graph_memory/parse_llm_response.go`
- Test: `internal/agentcore/memory/graph/graph_memory/parse_llm_response_test.go`

- [ ] **Step 1: 写失败测试**

创建 `parse_llm_response_test.go`，对齐 Python `parse_llm_response.py` 的关键测试场景：

```go
func TestParseISO_有效日期(t *testing.T) {
	ts, offset := ParseISO("2024-01-15T10:30:00Z")
	if ts <= 0 {
		t.Error("有效日期应返回正时间戳")
	}
	if offset != 0 {
		t.Errorf("Z 后缀偏移应为 0，实际 %d", offset)
	}
}

func TestParseISO_带时区(t *testing.T) {
	ts, offset := ParseISO("2024-01-15T10:30:00+08:00")
	if ts <= 0 {
		t.Error("有效日期应返回正时间戳")
	}
}

func TestParseISO_空输入(t *testing.T) {
	ts, offset := ParseISO("")
	if ts != -1 {
		t.Errorf("空输入应返回 -1，实际 %d", ts)
	}
}

func TestDict2Relation_有效输入(t *testing.T) {
	entities := []*graph.Entity{
		{Name: "张三"}, {Name: "华为"},
	}
	resp := map[string]any{
		"source_id": 1, "target_id": 2,
		"name": "工作于", "fact": "张三在华为工作",
	}
	rel := Dict2Relation(resp, entities)
	if rel == nil {
		t.Fatal("应返回 Relation")
	}
	if rel.Name != "工作于" {
		t.Errorf("Name 期望 工作于，实际 %s", rel.Name)
	}
}

func TestDict2Relation_无效ID(t *testing.T) {
	entities := []*graph.Entity{{}}
	resp := map[string]any{"source_id": 99, "target_id": 99}
	rel := Dict2Relation(resp, entities)
	if rel != nil {
		t.Error("无效 ID 应返回 nil")
	}
}

func TestParseAllRelations_基本(t *testing.T) {
	relations := []map[string]any{
		{"source_id": 1, "target_id": 2, "name": "关系1", "fact": "事实1"},
	}
	entityDecls := []EntityDeclaration{{Name: "A", EntityTypeID: 0}, {Name: "B", EntityTypeID: 0}}
	entityTypes := []EntityDef{{Name: "Entity"}}
	resultRels, resultEnts := ParseAllRelations(relations, entityDecls, entityTypes)
	if len(resultRels) != 1 {
		t.Errorf("期望 1 个关系，实际 %d", len(resultRels))
	}
	if len(resultEnts) != 2 {
		t.Errorf("期望 2 个实体，实际 %d", len(resultEnts))
	}
}

func TestResolveEntities_基本(t *testing.T) {
	candidates := []EntityDeclaration{{Name: "张三", EntityTypeID: 0}}
	existing := []*graph.Entity{graph.NewEntity()}
	existing[0].Name = "张三"
	duplication := []map[string]any{
		{"id": 1, "name": "张三", "duplicate_ids": []int{2}},
	}
	resolved, _, toRemove := ResolveEntities(candidates, existing, duplication, []EntityDef{{Name: "Entity"}})
	if len(resolved) != 1 {
		t.Errorf("期望 1 个 resolved entity，实际 %d", len(resolved))
	}
	if len(toRemove) > 0 {
		// 合并场景下可能有要删除的实体
		t.Logf("toRemove: %v", toRemove)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 parse_llm_response.go**

对齐 Python `parse_llm_response.py`，包含：
- `MATCH_ISO_DATETIME` 正则
- `ParseISO(timeStr string) (int64, int8)` — ISO 8601 日期解析
- `Dict2Relation(response map[string]any, entities []*graph.Entity) *graph.Relation`
- `ParseAllRelations(relations []map[string]any, entities []any, entityTypes []EntityDef) ([]*graph.Relation, []*graph.Entity)`
- `DeclareEntities(entities []any, entityTypes []EntityDef) []*graph.Entity`
- `ResolveEntities(candidates []EntityDeclaration, existing []*graph.Entity, duplication []map[string]any) ([]any, [][2]*graph.Entity, map[string]struct{})`
- `ParseRelationMerging(response map[string]any, relation *graph.Relation, existingRelations []map[string]any) map[string]struct{}`

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/graph/graph_memory/parse_llm_response.go internal/agentcore/memory/graph/graph_memory/parse_llm_response_test.go
git commit -m "feat(memory): 添加 LLM 响应解析 (7.11)"
```

---

## Task 11: 后处理函数

**Files:**
- Create: `internal/agentcore/memory/graph/graph_memory/postprocess.go`
- Test: `internal/agentcore/memory/graph/graph_memory/postprocess_test.go`

- [ ] **Step 1: 写失败测试**

创建 `postprocess_test.go`，对齐 Python `postprocess_graph_objects.py` 的关键逻辑：

```go
func TestCreateEpisode_基本(t *testing.T) {
	// 用 mock GraphStore 测试
	state := NewGraphMemState()
	state.CurrentTimestamp = 1700000000
	state.ReferenceTimestamp = -1
	state.EpisodeType = EpisodeTypeConversation
	ep, err := CreateEpisode(nil, "user1", "测试内容", state)
	if err != nil {
		// nil db 时可能报错，需要 mock
		t.Logf("CreateEpisode 报错（预期可能需要 mock）: %v", err)
	}
	if ep != nil && ep.UserID != "user1" {
		t.Errorf("UserID 期望 user1，实际 %s", ep.UserID)
	}
}

func TestValidateEntitiesEpisodes_同步连接信息(t *testing.T) {
	state := NewGraphMemState()
	entity := graph.NewEntity()
	entity.UUID = "ent-1"
	entity.Episodes = []string{"ep-1"}
	episode := graph.NewEpisode()
	episode.UUID = "ep-1"
	episode.Entities = []string{"ent-1"}
	state.LookupTable.Episodes["ep-1"] = episode
	entities := []*graph.Entity{entity}
	ValidateEntitiesEpisodes(entities, episode, state)
	// 验证双向连接同步
	found := false
	for _, e := range episode.Entities {
		if e == entity.UUID {
			found = true
		}
	}
	if !found {
		t.Error("Episode 应包含 Entity UUID")
	}
}

func TestProcessRelations_基本(t *testing.T) {
	state := NewGraphMemState()
	lhs := graph.NewEntity()
	lhs.UUID = "lhs-1"
	rhs := graph.NewEntity()
	rhs.UUID = "rhs-1"
	relation := graph.NewRelation()
	relation.LHS = lhs.UUID
	relation.RHS = rhs.UUID
	relations := []*graph.Relation{relation}
	// 需要验证 relation UUID 去重和关联更新
	// 此测试可能需要 mock GraphStore
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 postprocess.go**

对齐 Python `postprocess_graph_objects.py`，包含：
- `ValidateEntitiesEpisodes(entities []*graph.Entity, currentEpisode *graph.Episode, state *GraphMemState)`
- `CreateEpisode(database graph.BaseGraphStore, userID string, content string, state *GraphMemState) (*graph.Episode, error)`
- `ProcessRelations(database graph.BaseGraphStore, entities []*graph.Entity, relations []*graph.Relation, state *GraphMemState) error`
- `ProcessEntities(database graph.BaseGraphStore, entities []*graph.Entity, currentEpisode *graph.Episode, state *GraphMemState) error`
- `ParseRelationUUIDsToRemove(dedupeRelationTasks []DedupeRelationTask, state *GraphMemState) error`

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/graph/graph_memory/postprocess.go internal/agentcore/memory/graph/graph_memory/postprocess_test.go
git commit -m "feat(memory): 添加 GraphMemory 后处理函数 (7.11)"
```

---

## Task 12: GraphMemory 主类

**Files:**
- Create: `internal/agentcore/memory/graph/graph_memory/base.go`
- Test: `internal/agentcore/memory/graph/graph_memory/base_test.go`

- [ ] **Step 1: 写失败测试**

创建 `base_test.go`，测试 GraphMemory 构造和基本接口：

```go
func TestNewGraphMemory_构造(t *testing.T) {
	gm, err := NewGraphMemory(
		WithDBConfig(testGraphConfig),
		WithLanguage("cn"),
	)
	if err != nil {
		t.Fatalf("构造报错: %v", err)
	}
	if gm == nil {
		t.Fatal("GraphMemory 不应为 nil")
	}
	if gm.Language != "cn" {
		t.Errorf("Language 期望 cn，实际 %s", gm.Language)
	}
	if !gm.LLMStructuredOutput {
		t.Error("LLMStructuredOutput 默认应为 true")
	}
}

func TestNewGraphMemory_LLMStructuredOutputFalse(t *testing.T) {
	gm, _ := NewGraphMemory(
		WithDBConfig(testGraphConfig),
		WithLLMStructuredOutput(false),
	)
	if gm.LLMStructuredOutput {
		t.Error("应可设置为 false")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 base.go**

对齐 Python `GraphMemory` 类（`base.py` ~1139行），包含：

**结构体和构造函数：**
- `GraphMemory` 结构体（DBBackend/Config/LLMClient/LLMStructuredOutput/Reranker/DefaultExtractionStrategy/LLMExtraKwargs/Language/Debug/TokenRecord/UserLocks/Semaphore/ThreadLock/TimeTillNextGC）
- `NewGraphMemory(opts ...GraphMemoryOption)` 构造函数
- `GraphMemoryOption` 函数式选项

**AddMemory 管线：**
- `AddMemory(ctx, userID, content, opts ...AddMemoryOption) error` — 完整 16 步管线

**Search：**
- `Search(ctx, query, userID, searchEntity, searchRelation, searchEpisode bool, opts ...SearchOption) (*SearchResult, error)`

**内部方法：**
- `invokeLLM(kwargs map[string]any, tmpl *prompt.PromptTemplate, outputModel map[string]any, extra ...map[string]any) (*llmschema.AssistantMessage, error)`
- `prepareEpisodes(state *GraphMemState, content string, srcType EpisodeType, contentFmtKwargs map[string]string) error`
- `extractEntityDeclarations(state *GraphMemState) error`
- `fetchRelevantEntities(state *GraphMemState) error`
- `entityDedupe(state *GraphMemState) error`
- `entityEnrich(state *GraphMemState) error`
- `relationFilter(state *GraphMemState) error`
- `relationDedupe(state *GraphMemState) error`

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/graph/graph_memory/base.go internal/agentcore/memory/graph/graph_memory/base_test.go
git commit -m "feat(memory): 添加 GraphMemory 主类 (7.11)"
```

---

## Task 13: 运行全量测试 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 运行全量 memory 包测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/... -count=1
```
Expected: PASS

- [ ] **Step 2: 运行全量 LLM 包测试确认无回归**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/llm/... -count=1
```
Expected: PASS

- [ ] **Step 3: 运行覆盖率检查**

```bash
cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agentcore/memory/graph/... ./internal/agentcore/memory/config/...
```
Expected: 各包覆盖率 ≥ 85%

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md**

将 7.11 和 7.12 的状态从 ☐ 改为 ✅：

```
| 7.11 | ✅ | GraphMemory | 实体抽取，三元组存储 | `openjiuwen/core/memory/graph/graph_memory/` |
| 7.12 | ✅ | Graph Extraction | 图实体抽取 | `openjiuwen/core/memory/graph/extraction/` |
```

- [ ] **Step 5: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "feat(memory): 完成 7.11 GraphMemory + 7.12 Graph Extraction"
```
