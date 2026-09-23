# 7.11+7.12 实现审查修复设计

> 基于 7.11 (GraphMemory) + 7.12 (Graph Extraction) 实现后的审查，修复与 Python 对齐偏差、any 类型优化和功能缺失。

## 1. 修复清单总览

| 优先级 | # | 问题 | 修复方案 |
|--------|---|------|---------|
| 🔴 高 | 1 | GraphMemPrompting Schema 默认值为空 | `initState` 中调用 `ReadableSchema` + `StrictSchemaEnforce` 动态生成完整 Schema |
| 🔴 高 | 2 | FormatExistingRelations 缺少时间信息 | 补充 `includeTime` 分支，调用 `graph.LoadStoredTimeFromDB` |
| 🔴 高 | 3 | AddMemory 不支持消息列表输入 | `AddMemoryConfig` 新增 `Messages []schema.BaseMessage`（与 Content 二选一）+ 新增 `FormatListOfMessages` |
| 🟡 中 | 4 | ReadableSchema refDict 结构不一致 | `extractRefDict` 只保留 `properties` 子集 |
| 🟡 中 | 5 | format_new_entities 重复实现 | 删除 `entity_extraction/FormatNewEntities`（Python 中不存在） |
| 🟡 中 | 6 | Search 串行 vs Python 并发 | 用 `errgroup` 并发搜索 3 个集合 |
| 🟢 低 | 7 | userID 参数类型为 any | 改为 `[]string`，删除 `normalizeUserIDs` |

## 2. 高优先级修复

### 2.1 Schema 默认值动态生成

**问题**：`initState` 中 4 个 Schema 字段使用空 `map[string]any{}`，Python 通过 `EntitySummary.response_format(self.language)` 动态生成完整 JSON Schema。

**Python 行为**：
```python
state.prompting.schema_entity_extraction = EntitySummary.response_format(self.language)
state.prompting.schema_entity_dedupe = EntityDuplication.response_format(state.prompting.entity_dedupe_language)
state.prompting.schema_relation_merge = MergeRelations.response_format(self.language)
state.prompting.schema_relation_filter = RelevantFacts.response_format(self.language)
```

注意 4 个 Schema 的语言不完全一致：`schema_entity_dedupe` 用 `entity_dedupe_language`（可能因 strategy 的 chinese_entity_dedupe 标记覆盖为 "cn"），其他 3 个用 `self.language`。

**修复方案**：

1. `extraction/base.go` 新增 `StrictSchemaEnforce(schemaMap map[string]any)` 函数，BFS 遍历所有 `type=object` 节点设置 `additionalProperties=false` + `required=[all keys]`，对齐 Python `multilingual_model_json_schema(strict=True)`
2. 新增 `BuildResponseFormat(modelType reflect.Type, language string) map[string]any` 导出函数，封装 ReadableSchema → StrictSchemaEnforce → ResponseFormat 链路
3. `initState` 中 4 行空 Schema 替换为：
   ```go
   state.Prompting.SchemaEntityExtraction = extraction.BuildResponseFormat(reflect.TypeOf(extraction.EntitySummary{}), gm.Language)
   state.Prompting.SchemaEntityDedupe = extraction.BuildResponseFormat(reflect.TypeOf(extraction.EntityDuplication{}), state.Prompting.EntityDedupeLanguage)
   state.Prompting.SchemaRelationMerge = extraction.BuildResponseFormat(reflect.TypeOf(extraction.MergeRelations{}), gm.Language)
   state.Prompting.SchemaRelationFilter = extraction.BuildResponseFormat(reflect.TypeOf(extraction.RelevantFacts{}), gm.Language)
   ```
4. `newGraphMemPrompting()` 中的空 Schema 不修改（反正被 `initState` 覆盖）

### 2.2 FormatExistingRelations 时间格式化

**问题**：Go 版本完全省略了 `includeTime` 的时间格式化逻辑，导致 LLM 在关系去重时缺少时间上下文。

**Python 行为**：
```python
if include_time and valid_since != -1:
    offset_since = rel.get("offset_since", 0)
    valid_since = load_stored_time_from_db(valid_since, offset_since).isoformat(timespec="seconds")
    content += f"\n{valid_since=}"
if include_time and valid_until != -1:
    offset_until = rel.get("offset_until", 0)
    valid_until = load_stored_time_from_db(valid_until, offset_until).isoformat(timespec="seconds")
    content += f"\n{valid_until=}"
```

**已有依赖**：Go 中已有 `graph.LoadStoredTimeFromDB(timestamp int64, offset int8) (*time.Time, error)` 在 `foundation/store/graph/utils.go`。

**修复方案**：

在 `FormatExistingRelations` 中补充时间格式化分支：
- 从 relation dict 取 `valid_since`/`valid_until`（int64），`offset_since`/`offset_until`（int8）
- `timestamp == -1` 表示未知时间，跳过
- 调用 `graph.LoadStoredTimeFromDB(timestamp, offset)` 还原时间
- 用 `.Format("2006-01-02T15:04:05Z07:00")` 生成 ISO 字符串（对齐 Python `.isoformat(timespec="seconds")`）
- 追加 `valid_since=...` / `valid_until=...` 到 content 末尾

需新增 import：`"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"`

### 2.3 AddMemory 消息列表输入

**问题**：Python `add_memory` 支持 `content: list[BaseMessage | dict] | str`，Go 只接受 `string`。对话场景中消息列表是核心输入格式。

**Python 分支**：
1. `isinstance(content, str)` → 直接使用，`content_fmt_kwargs` 非空报错
2. content 是 `list` → 必须是 CONVERSATION → `msg2dict` → 校验 → `format_list_of_messages(role_replace=content_fmt_kwargs)`

**修复方案**：

1. `AddMemoryConfig` 新增 `Messages []schema.BaseMessage` 字段：
   ```go
   type AddMemoryConfig struct {
       SourceType        config.EpisodeType
       UserID            string
       Content           string                // 纯文本输入
       Messages          []schema.BaseMessage  // 消息列表输入（与 Content 二选一）
       ContentFmtKwargs  map[string]string
       ReferenceTime     *time.Time
   }
   ```

2. `prepareEpisodes` 增加二选一校验和消息列表分支：
   - 两者都空 → 报错
   - 两者都非空 → 报错
   - `Content` 非空 → 走现有 str 路径（`content_fmt_kwargs` 非空报错）
   - `Messages` 非空 → 必须是 CONVERSATION → `Msg2Dict` → `FormatListOfMessages` → 得到 string

3. 新增 `FormatListOfMessages` 到 `foundation/store/graph/utils.go`（对齐 Python 位置 `foundation/store/graph/utils.py`）：
   ```go
   func FormatListOfMessages(messages []map[string]any, roleReplace map[string]string, template string) string
   ```
   - 遍历 messages，每条取 `role` 和 `content`
   - `roleReplace` 映射角色名（如 `"user" → "张三（用户）"`）
   - `template` 默认 `"{role}: {content}\n"`

## 3. 中优先级修复

### 3.1 ReadableSchema refDict 结构

**问题**：Go `extractRefDict` 返回完整 def map，Python 只返回 `{key: val["properties"]}`。`FormatSchemaInfo` 做 JSON 序列化时多出字段干扰 LLM。

**修复**：`extractRefDictRecursive` 中只保留 `properties` 子集：
```go
// 修改前
refDict[defName] = defMap

// 修改后
if props, ok := defMap["properties"].(map[string]any); ok {
    refDict[defName] = props
}
```

### 3.2 删除多余 FormatNewEntities

**问题**：Go 中 `entity_extraction/FormatNewEntities` 接受 `[]map[string]any`，Python 的 `prompts/entity_extraction/base.py` 中**不存在此函数**。Python 中 `format_new_entities` 只在 `extraction_prompts.py` 中，Go 已有正确的 `extraction/formatNewEntities`。

**修复**：
1. 检查 `entity_extraction/FormatNewEntities` 的所有调用方
2. 无调用方 → 删除 `prompts/entity_extraction/format.go` 整个文件
3. 有调用方 → 改为使用 `extraction.formatNewEntities`，再删除
4. 更新 `prompts/entity_extraction/doc.go` 移除 format.go 条目

### 3.3 Search 并发化

**问题**：Go 串行搜索 3 个集合，Python 用 `asyncio.as_completed` 并发。

**修复**：用 `errgroup` 并发化：
```go
import "golang.org/x/sync/errgroup"

g, gctx := errgroup.WithContext(ctx)

if searchEntity {
    g.Go(func() error {
        objects, err := gm.performSearch(gctx, 0, userIDs, strategyName, query, queryEmbedding)
        if err != nil { return err }
        result.Entity = objects
        return nil
    })
}
if searchRelation {
    g.Go(func() error {
        objects, err := gm.performSearch(gctx, 1, userIDs, strategyName, query, queryEmbedding)
        if err != nil { return err }
        result.Relation = objects
        return nil
    })
}
if searchEpisode {
    g.Go(func() error {
        objects, err := gm.performSearch(gctx, 2, userIDs, strategyName, query, queryEmbedding)
        if err != nil { return err }
        result.Episode = objects
        return nil
    })
}

if err := g.Wait(); err != nil {
    return nil, err
}
```

## 4. any 类型优化

### 4.1 userID: `any` → `[]string`

**修复范围**：
- `base.go`：`Search` 签名 `userID any` → `userIDs []string`
- `validate_input.go`：`ValidateSearchInput` 签名 `userID any` → `userIDs []string`
- `validate_input.go`：删除 `normalizeUserIDs` 函数
- 调用方：传单个 userID 时写 `[]string{uid}`

### 4.2 不修改的 any 使用

| 位置 | 类型 | 保留原因 |
|------|------|---------|
| `TmpBuffer []any` | `[]any` | 对齐 Python `list`，存 Entity 和 string 两种类型 |
| `Extras map[string]any` | `map[string]any` | 与上下游联动，单独改会不一致 |
| `ScoredGraphObject.Object any` | `any` | 存 3 种图对象，改接口增加跨包依赖 |
| `LLMExtraKwargs map[string]any` | `map[string]any` | LLM 参数天然不固定 |
| `extraction_prompts.go` extras/tzInfo | `any` | 与上游 Extras 联动 |

## 5. 涉及文件清单

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `extraction/base.go` | 修改 | 新增 `StrictSchemaEnforce` + `BuildResponseFormat` |
| `extraction/base_test.go` | 修改 | 新增 StrictSchemaEnforce 测试 |
| `extraction/prompts/entity_extraction/base.go` | 修改 | FormatExistingRelations 补充时间格式化 |
| `extraction/prompts/entity_extraction/base_test.go` | 修改 | 新增时间格式化测试 |
| `extraction/prompts/entity_extraction/format.go` | **删除** | 删除 Python 中不存在的 FormatNewEntities |
| `extraction/prompts/entity_extraction/doc.go` | 修改 | 移除 format.go 条目 |
| `graph_memory/base.go` | 修改 | initState Schema 生成 + Search 并发化 + AddMemoryConfig 新增 Messages + userID 签名 |
| `graph_memory/base_test.go` | 修改 | 更新 Search/AddMemory 测试 |
| `graph_memory/validate_input.go` | 修改 | userID 签名 → []string，删除 normalizeUserIDs |
| `graph_memory/validate_input_test.go` | 修改 | 更新 userID 测试 |
| `foundation/store/graph/utils.go` | 修改 | 新增 FormatListOfMessages |
| `foundation/store/graph/utils_test.go` | 修改 | 新增 FormatListOfMessages 测试 |

## 6. 测试策略

- 每个 `StrictSchemaEnforce` 测试：验证 BFS 正确设置 additionalProperties=false + required
- `BuildResponseFormat` 测试：验证 4 个模型 struct 生成完整 Schema
- `FormatExistingRelations` 时间格式化测试：验证 valid_since/valid_until 的 ISO 输出
- `FormatListOfMessages` 测试：验证 role 替换和模板格式化
- `Search` 并发测试：验证 errgroup 并发结果与串行一致
- userID []string 测试：验证调用方传 `[]string{uid}` 正常工作
