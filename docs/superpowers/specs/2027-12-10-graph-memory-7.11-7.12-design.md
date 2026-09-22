# GraphMemory + Graph Extraction 设计文档（7.11 + 7.12 合并实现）

> 对应 Python 源码：
> - `openjiuwen/core/memory/config/graph.py`
> - `openjiuwen/core/memory/graph/graph_memory/`
> - `openjiuwen/core/memory/graph/extraction/`

## 1. 背景与定位

### 1.1 在 Agent 会话中的位置

```
用户输入 → Gateway → AgentServer → AgentLoop (ReAct)
                                  ├─ 1. ContextEngine 组装上下文
                                  │   ├─ FragmentMemory（片段记忆）← 7.6 ✅
                                  │   ├─ SummaryMemory（摘要记忆）← 7.7 ✅
                                  │   ├─ VariableMemory（变量记忆）← 7.7 ✅
                                  │   └─ GraphMemory（图记忆）← 7.11 ☐
                                  ├─ 2. LLM 推理
                                  ├─ 3. Tool 执行
                                  └─ 4. 记忆写入
                                      └─ GraphMemory.AddMemory() ← 将对话/文档中的实体和关系写入知识图谱
```

### 1.2 核心价值

GraphMemory 是**知识图谱记忆**，与 Fragment/Summary/Variable（基于 KV + 向量搜索）互补：

| 记忆类型 | 存储模型 | 擅长 | 不擅长 |
|---------|---------|------|-------|
| Fragment（7.6） | 文本片段 + 向量 | 事实性片段检索 | 实体间关系推理 |
| Summary（7.7） | 摘要文本 + KV | 全局概览 | 细粒度实体属性 |
| Variable（7.7） | KV 对 | 用户偏好/配置 | 动态变化的实体网络 |
| **Graph（7.11）** | **实体 + 关系 + 片段 三元组** | **实体间关系推理、属性聚合、去重合并** | **轻量级简单键值存储** |

### 1.3 为什么 7.11 和 7.12 合并实现

7.11 GraphMemory 的每次 LLM 调用都依赖 7.12 Extraction 的模型和提示词，两者强耦合。分离实现会导致中间态不可用，合并实现可以端到端验证。

## 2. 包结构（对齐 Python 层级）

```
internal/agentcore/memory/
├── config/
│   └── graph_config.go           # AddMemStrategy/SearchConfig/EpisodeType/RetrievalStrategy
├── graph/
│   ├── graph_memory/             # 7.11 GraphMemory 主类
│   │   ├── doc.go
│   │   ├── base.go               # GraphMemory 主类（add_memory + search）
│   │   ├── states.go             # GraphMemState/GraphMemUpdate/LookupTables/EntityMerge + persist/batch_embed
│   │   ├── parse_llm_response.go # LLM 响应解析（parse_all_relations/resolve_entities/declare_entities）
│   │   ├── postprocess.go        # 后处理（validate_entities_episodes/create_episode/process_relations/entities）
│   │   ├── utils.go              # 辅助函数（msg2dict/update_entity/assembleInvokeParams）
│   │   └── validate_input.go     # 输入校验
│   └── extraction/               # 7.12 抽取模型 + 提示词
│       ├── doc.go
│       ├── base.go               # 多语言 description 替换机制 + ResponseFormat/ReadableSchema 生成
│       ├── custom_types.go       # JSONLike 类型别名
│       ├── entity_type_definition.go  # EntityDef/RelationDef/HumanEntity/AIEntity
│       ├── extraction_models.go  # 输出模型 Go struct（EntityExtraction/EntitySummary/...）+ jsonschema tag
│       ├── extraction_prompts.go # 提示词组装函数（extract_entity_declaration/...）
│       ├── parse_response.go     # JSON 解析（parse_json/ensure_list）
│       └── prompts/              # 提示词模板文件 + 管理器
│           ├── doc.go
│           ├── manager.go        # TemplateManager 单例（线程安全，glob 扫描 .pr.md 加载）
│           ├── pr_parser.go      # .pr.md 解析器（`#role#` → []BaseMessage）
│           ├── format_helpers.go # format_existing_entities/format_existing_relations/format_schema_info
│           ├── cn/               # 中文提示词（12 个 .pr.md）
│           │   ├── register.go   # 中文注册（MULTILINGUAL_DESCRIPTION/REGISTERED_LANGUAGE 等）
│           │   └── *.pr.md       # 提示词模板文件（从 Python 1:1 复制）
│           ├── en/               # 英文提示词（12 个 .pr.md）
│           │   ├── register.go
│           │   └── *.pr.md
│           └── entity_extraction/  # 格式化辅助
│               ├── doc.go
│               └── format.go
```

## 3. Schema 生成链路

### 3.1 整体流程

```
Go struct（extraction_models.go）+ jsonschema tag
  → StructSchemaExtractor.Extract() 生成 []*schema.Param
  → ReplaceDescriptions(params, langMap) 运行时替换多语言 description
  → ToJSONSchemaMap(params) 生成标准 JSON Schema map[string]any
  → ResponseFormat(name, schemaMap) 包装为 OpenAI response_format 格式
  → 填入 InvokeParams.ResponseFormat → client 按能力传入 API
```

### 3.2 输出模型定义（extraction_models.go）

每个 Python Pydantic 输出模型对应一个 Go struct，使用 `jsonschema` tag 标注：

```go
// EntityExtraction 实体声明抽取输出模型
//
// Python: EntityExtraction (MultilingualBaseModel)
type EntityExtraction struct {
    // ExtractedEntities 新提取的实体列表
    ExtractedEntities []EntityDeclaration `json:"extracted_entities" jsonschema:"description={{[ent_ext_list]}}"`
}

// EntitySummary 实体摘要与属性输出模型
//
// Python: EntitySummary (MultilingualBaseModel)
type EntitySummary struct {
    // Summary 实体相关的重要信息
    Summary string `json:"summary" jsonschema:"description={{[ent_summary]}}"`
    // Attributes 实体的属性值
    Attributes map[string]any `json:"attributes" jsonschema:"description={{[ent_attributes]}}"`
}

// EntityDuplication 实体去重输出模型
//
// Python: EntityDuplication (MultilingualBaseModel)
type EntityDuplication struct {
    // DuplicatedEntities 重复实体列表
    DuplicatedEntities []Duplication `json:"duplicated_entities" jsonschema:"description={{[ent_dupe_list]}}"`
}
```

### 3.3 多语言 description 替换

Python 用 `MULTILINGUAL_DESCRIPTION` 注册表在运行时替换 Schema 中的 `{{[xxx]}}` 占位符。

Go 方案：
- Go struct 的 `jsonschema` tag 写占位符（如 `description={{[ent_def_name]}}`）
- `StructSchemaExtractor.Extract()` 提取后，description 字段值为原始占位符
- 新增 `ReplaceDescriptions(params []*schema.Param, langMap map[string]string) []*schema.Param` 函数，遍历 params 递归替换 description 中的占位符
- langMap 从 `prompts/cn/register.go` 或 `prompts/en/register.go` 加载，对应 Python 的 `MULTILINGUAL_DESCRIPTION[language]`

### 3.4 ResponseFormat 包装

新增函数将标准 JSON Schema 包装为 OpenAI structured output 格式：

```go
// ResponseFormat 将 JSON Schema 包装为 OpenAI response_format 格式
//
// Python: MultilingualBaseModel.response_format()
func ResponseFormat(name string, schemaMap map[string]any) map[string]any {
    return map[string]any{
        "type": "json_schema",
        "json_schema": map[string]any{
            "schema": schemaMap,
            "name":   name,
            "strict": false,
        },
    }
}
```

### 3.5 ReadableSchema 生成

从 `[]*schema.Param` 生成提示词中嵌入的可读类型定义字符串（对齐 Python `readable_schema()`）：

```go
// ReadableSchema 从输出模型生成 LLM 可读的类型定义字符串
//
// Python: MultilingualBaseModel.readable_schema()
func ReadableSchema(modelType reflect.Type, language string) (string, map[string]map[string]any)
```

## 4. LLM 层改动

### 4.1 InvokeParams 新增 ResponseFormat 字段

```go
// InvokeParams 新增字段
type InvokeParams struct {
    // ... 现有字段 ...
    // ResponseFormat 结构化输出格式（OpenAI response_format），nil 表示不使用
    ResponseFormat map[string]any
}

// WithResponseFormat 设置结构化输出格式
func WithResponseFormat(schemaMap map[string]any) InvokeOption {
    return func(p *InvokeParams) { p.ResponseFormat = schemaMap }
}
```

### 4.2 StreamParams 同步新增

```go
type StreamParams struct {
    // ... 现有字段 ...
    // ResponseFormat 结构化输出格式（OpenAI response_format），nil 表示不使用
    ResponseFormat map[string]any
}

func WithStreamResponseFormat(schemaMap map[string]any) StreamOption {
    return func(p *StreamParams) { p.ResponseFormat = schemaMap }
}
```

### 4.3 Client 适配策略

对齐 Python 的 `**kwargs` 透传模式：

- 各 `model_client` 在 `BuildRequestParams` 中检查 `InvokeParams.ResponseFormat`
- 支持 structured output 的后端（openai/dashscope/deepseek/siliconflow/inference_affinity/intellirouter）：将 ResponseFormat 传入 API 请求
- 不支持的后端：静默忽略（与 Python 行为一致）
- Model 层的 `Invoke()` 方法需要透传 ResponseFormat 到 client

### 4.4 结构化输出双路径（对齐 Python llm_structured_output）

| `LLMStructuredOutput` | 传给 LLM 的参数 | 输出解析方式 |
|---|---|---|
| `true`（默认） | `ResponseFormat={"type":"json_schema","json_schema":{...}}` | LLM 被强制输出合法 JSON |
| `false` | 不传 `ResponseFormat` | 提示词要求 JSON + `parseJSON()` 容错解析 |

GraphMemory 构造时传 `LLMStructuredOutput bool`，`invokeLLM` 中根据开关决定是否填充 `ResponseFormat`。

## 5. 提示词模板管理

### 5.1 .pr.md 文件

从 Python 1:1 复制到 Go 项目的 `memory/graph/extraction/prompts/cn/` 和 `en/` 目录。

`.pr.md` 格式使用 `` `#role#` `` 分隔消息角色：

```markdown
`#system#`你是一个专门从对话消息中提取实体的可靠助手。
`#user#`# 上下文信息
{{source_description}}
...
```

### 5.2 .pr.md 解析器（pr_parser.go）

```
`#role#`content → []schema.BaseMessage
```

解析规则：
- `role` ∈ {system, user, assistant, tool}
- 每个 `` `#role#` `` 开始一个新的消息段落
- 解析结果为 `[]schema.BaseMessage`，用 `schema.NewSystemMessage(content)` 等构造

### 5.3 TemplateManager 单例

对齐 Python 的 `ThreadSafePromptManager`：

- 线程安全单例（`sync.Once` + `sync.RWMutex`）
- 启动时 glob 扫描 `prompts/**/*.pr.md`
- 解析为 `PromptTemplate`（Content 类型为 `[]schema.BaseMessage`）
- 缓存到 `map[string]*prompt.PromptTemplate`
- `Get(name string) *prompt.PromptTemplate` 按 name 获取（如 `entity_extraction_conversation_cn`）

### 5.4 格式化辅助（format_helpers.go）

对齐 Python `entity_extraction/base.py` 中的辅助函数：

| Go 函数 | Python 函数 | 说明 |
|---------|------------|------|
| `FormatSchemaInfo` | `format_schema_info` | 将 ReadableSchema 拼接到提示词末尾 |
| `FormatSourceDescription` | `format_source_description` | 格式化数据源描述 |
| `GetFormattingKwargs` | `get_formatting_kwargs` | 组装提示词模板变量 |
| `FormatRelationDefinitions` | `format_relation_definitions` | 格式化关系类型定义 |
| `FormatExistingRelations` | `format_existing_relations` | 格式化已有关系列表 |
| `FormatExistingEntities` | `format_existing_entities` | 格式化已有实体列表 |
| `EnsureValidLanguage` | `ensure_valid_language` | 校验语言支持 |

### 5.5 多语言注册（cn/register.go, en/register.go）

对齐 Python `entity_extraction/cn.py` 和 `en.py`：

- `MULTILINGUAL_DESCRIPTION map[string]map[string]string` — 全局注册表
- `REGISTERED_LANGUAGE map[string]struct{}` — 已注册语言
- `SOURCE_DESCRIPTION / OUTPUT_FORMAT / DISPLAY_ENTITY / ...` — 格式化模板字符串
- `init()` 时自动注册

## 6. GraphMemory 管线（对齐 Python）

### 6.1 AddMemory 管线

```
AddMemory(ctx, userID, content, opts...)
  → 1.  validateInput + ensureValidLanguage
  → 2.  初始化 GraphMemState（strategy/prompting/entityTypes/timestamps）
  → 3.  createEpisode(database, userID, content, state) — 创建 Episode 片段
  → 4.  并行启动 timezone LLM（_invokeLLM + extractTimezone 组装提示词）
  → 5.  extractEntityDeclarations → LLM → []EntityDeclaration
  → 6.  批量嵌入新实体名称（embedder.EmbedDocuments）
  → 7.  extractRelationDeclaration → LLM → []Relation（parseAllRelations 解析）
  → 8.  fetchRelevantEntities（search + query 已有实体/关系）
  → 9.  entityDedupe（LLM）+ resolve + merge
  → 10. relationFilter（LLM 过滤无关关系）
  → 11. relationDedupe（LLM 去重 + parseRelationMerging）
  → 12. entityEnrich（LLM summary + attributes → updateEntity）
  → 13. processRelations（更新关联实体 + ensureUniqueUUIDs）
  → 14. processEntities（完成合并 + 关联 Episode + ensureUniqueUUIDs）
  → 15. validateEntitiesEpisodes（同步 Entity-Episode 连接信息）
  → 16. persistToDB（嵌入向量 + 写入数据库 + blockKeyboardInterrupt 保护）
```

### 6.2 Search 方法

```go
// Search 搜索知识图谱
//
// Python: GraphMemory.search()
func (gm *GraphMemory) Search(ctx context.Context, query string, userID string, 
    searchEntity, searchRelation, searchEpisode bool, opts ...SearchOption) (*SearchResult, error)
```

支持：
- 混合搜索（dense + BM25 sparse）+ 可选 reranking
- BFS 图扩展（从搜索到的实体扩展关联实体/关系）
- 过滤表达式
- 输出字段选择

### 6.3 状态结构

对齐 Python `states.py`：

| Go 结构体 | Python 类 | 说明 |
|-----------|----------|------|
| `LookupTables` | `LookupTables` | UUID → Entity/Relation/Episode 的去重查找表 |
| `EntityMerge` | `EntityMerge` | 实体合并信息（target + sources + newRelations） |
| `GraphMemUpdate` | `GraphMemUpdate` | 增量变更累积器（added/updated/removed） |
| `GraphMemPrompting` | `GraphMemPrompting` | Schema 定义 + 语言设置 |
| `GraphMemState` | `GraphMemState` | 完整状态（tasks/缓冲区/lookup/strategy/prompting） |

### 6.4 LLM 响应解析

对齐 Python `parse_llm_response.py`：

| Go 函数 | Python 函数 | 说明 |
|---------|------------|------|
| `ParseISO` | `parse_iso` | ISO 8601 日期解析 |
| `Dict2Relation` | `dict2relation` | 将 LLM 输出 dict 转为 Relation 对象 |
| `ParseAllRelations` | `parse_all_relations` | 解析全部关系 + 去重 + 实体声明转换 |
| `DeclareEntities` | `declare_entities` | 将 EntityDeclaration 转为 Entity |
| `ResolveEntities` | `resolve_entities` | 实体去重/合并解析 |
| `ParseRelationMerging` | `parse_relation_merging` | 解析关系合并 LLM 响应 |

### 6.5 后处理

对齐 Python `postprocess_graph_objects.py`：

| Go 函数 | Python 函数 | 说明 |
|---------|------------|------|
| `ValidateEntitiesEpisodes` | `validate_entities_episodes` | 同步 Entity-Episode 连接信息 |
| `CreateEpisode` | `create_episode` | 创建 Episode 片段 |
| `ProcessRelations` | `process_relations` | 处理关系（删除废弃 + 更新关联 + UUID 去重） |
| `ProcessEntities` | `process_entities` | 处理实体（合并完成 + 关联 Episode + UUID 去重） |
| `ParseRelationUUIDsToRemove` | `parse_relation_uuids_to_remove` | 解析需删除的关系 UUID |

## 7. 配置类型（config/graph_config.go）

对齐 Python `memory/config/graph.py`：

| Go 类型 | Python 类 | 说明 |
|---------|----------|------|
| `EpisodeType` | `EpisodeType` | 片段类型枚举（CONVERSATION/DOCUMENT/JSON） |
| `BaseStrategy` | `BaseStrategy` | 基础检索策略（top_k/min_score/rank_config） |
| `RetrievalStrategy` | `RetrievalStrategy` | 检索策略（same_kind） |
| `EpisodeRetrievalStrategy` | `EpisodeRetrievalStrategy` | Episode 检索策略（exclude_future_results） |
| `AddMemStrategy` | `AddMemStrategy` | 添加记忆策略（语言/去重/检索/合并等） |
| `SearchConfig` | `SearchConfig` | 搜索配置（bfs_k/bfs_depth/filter_expr/rerank） |

## 8. 依赖关系

### 8.1 已有依赖（✅ 已实现）

| 依赖 | 包路径 |
|------|--------|
| BaseGraphStore / Entity / Relation / Episode | `agentcore/foundation/store/graph/` |
| Embedding | `agentcore/foundation/store/embedding/` |
| Reranker | `agentcore/foundation/store/reranker/` |
| LLM Model / Invoke / Stream | `agentcore/foundation/llm/` |
| PromptTemplate | `agentcore/foundation/prompt/` |
| StructSchemaExtractor | `agentcore/foundation/tool/` |
| Param / ToJSONSchemaMap | `common/schema/` |
| GraphConfig / GraphStoreFactory | `agentcore/foundation/store/graph/` |

### 8.2 需新增的代码

| 代码 | 包路径 | 说明 |
|------|--------|------|
| `InvokeParams.ResponseFormat` | `agentcore/foundation/llm/model_clients/` | LLM 层扩展 |
| `graph_config.go` | `agentcore/memory/config/` | 图记忆配置类型 |
| `extraction/*` | `agentcore/memory/graph/extraction/` | 7.12 抽取模型 + 提示词 |
| `graph_memory/*` | `agentcore/memory/graph/graph_memory/` | 7.11 GraphMemory 主类 |

### 8.3 不受影响的章节

| 章节 | 原因 |
|------|------|
| 7.24 Memory Codec | GraphMemory 用 GraphStore 而非 SQL，不依赖 codec |
| 7.25 Memory Common | GraphMemory 有自己的 utils.go，不依赖 memory/common |
| 7.26 Memory Prompts | GraphMemory 有独立的 extraction/prompts/ 目录 |
| 7.27 LongTermMemory | 上层编排，GraphMemory 是其子组件 |

## 9. 回填关系

- 7.11 + 7.12 完成后，IMPLEMENTATION_PLAN.md 中 7.11 和 7.12 的状态从 ☐ 改为 ✅
- 7.21 MigrationPlan 中的 ⤵️ 标记（MilvusVectorStore.UpdateSchema）不受 7.11 影响，保持不变
- 7.9 Memory DB Models 的 ⤴️ 回填来源（UserMemStore/SemanticStore/SupportMemoryType）已在此前完成，不受影响

## 10. 测试策略

### 10.1 单元测试（mock LLM + mock GraphStore）

- extraction_models: Schema 生成正确性（`ReplaceDescriptions` + `ToJSONSchemaMap` + `ResponseFormat`）
- parse_response: `ParseJSON` 各种容错场景
- parse_llm_response: `ParseAllRelations` / `ResolveEntities` / `DeclareEntities`
- validate_input: 输入校验边界条件
- states: `GraphMemUpdate` 的 `|` 合并操作、`LookupTables` 的去重行为
- graph_config: 配置默认值和校验

### 10.2 集成测试（build tag: integration）

需要真实 Milvus + LLM API：
- GraphMemory.AddMemory 端到端
- GraphMemory.Search 端到端

### 10.3 提示词模板测试

- .pr.md 解析器正确性
- TemplateManager 加载和缓存
- 模板 Format 变量替换完整性

## 11. 实现顺序

1. `memory/config/graph_config.go` — 配置类型
2. `foundation/llm InvokeParams.ResponseFormat` — LLM 层扩展
3. `memory/graph/extraction/` — 抽取模型 + 提示词（7.12）
   - prompts/cn/ + prompts/en/ 模板文件复制
   - prompts/manager.go + pr_parser.go
   - prompts/format_helpers.go
   - prompts/cn/register.go + prompts/en/register.go
   - base.go（多语言 description 替换 + ResponseFormat/ReadableSchema）
   - custom_types.go + entity_type_definition.go + extraction_models.go
   - extraction_prompts.go（提示词组装函数）
   - parse_response.go（JSON 解析）
4. `memory/graph/graph_memory/` — GraphMemory 主类（7.11）
   - validate_input.go
   - utils.go
   - states.go
   - parse_llm_response.go
   - postprocess.go
   - base.go（主类）
5. 单元测试 + doc.go 更新
6. IMPLEMENTATION_PLAN.md 状态更新
