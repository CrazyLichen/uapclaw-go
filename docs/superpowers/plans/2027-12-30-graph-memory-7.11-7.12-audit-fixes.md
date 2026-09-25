# 7.11+7.12 实现审查修复计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 7.11+7.12 实现中与 Python 的 7 项偏差，包括 Schema 动态生成、时间格式化、消息列表输入、refDict 结构、多余函数删除、Search 并发化、userID 类型优化。

**Architecture:** 在 extraction/base.go 中新增 StrictSchemaEnforce + BuildResponseFormat；在 entity_extraction/base.go 中补充时间格式化；在 graph_memory/base.go 中修改 AddMemoryConfig/initState/Search；在 foundation/store/graph/utils.go 中新增 FormatListOfMessages。

**Tech Stack:** Go 1.23, golang.org/x/sync/errgroup, reflect, time

---

### Task 1: StrictSchemaEnforce + BuildResponseFormat

**Files:**
- Modify: `internal/agentcore/memory/graph/extraction/base.go`
- Create: `internal/agentcore/memory/graph/extraction/base_test.go`

- [x] **Step 1: 写 StrictSchemaEnforce 的测试**

创建 `internal/agentcore/memory/graph/extraction/base_test.go`：

```go
package extraction

import (
	"testing"
)

// TestStrictSchemaEnforce_设置additionalProperties 测试 BFS 设置 additionalProperties=false
func TestStrictSchemaEnforce_设置additionalProperties(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{"type": "string"},
			"attributes": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
				"properties": map[string]any{
					"key": map[string]any{"type": "string"},
				},
			},
		},
	}
	StrictSchemaEnforce(schema)

	// 顶层 object 应设置 additionalProperties=false
	if ap, ok := schema["additionalProperties"].(bool); !ok || ap {
		t.Errorf("顶层 additionalProperties 期望 false，实际 %v", schema["additionalProperties"])
	}
	// 顶层 required 应包含所有 key
	req, ok := schema["required"].([]string)
	if !ok || len(req) != 2 {
		t.Errorf("顶层 required 期望 [summary, attributes]，实际 %v", schema["required"])
	}
	// 嵌套 object 也应设置
	attrs := schema["properties"].(map[string]any)["attributes"].(map[string]any)
	if ap, ok := attrs["additionalProperties"].(bool); !ok || ap {
		t.Errorf("嵌套 additionalProperties 期望 false，实际 %v", attrs["additionalProperties"])
	}
}

// TestStrictSchemaEnforce_数组内嵌对象 测试 array items 中的 object 也被处理
func TestStrictSchemaEnforce_数组内嵌对象(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	StrictSchemaEnforce(schema)

	arrayItems := schema["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)
	if ap, ok := arrayItems["additionalProperties"].(bool); !ok || ap {
		t.Errorf("数组内对象 additionalProperties 期望 false，实际 %v", arrayItems["additionalProperties"])
	}
}

// TestBuildResponseFormat_EntitySummary 测试 BuildResponseFormat 生成完整 Schema
func TestBuildResponseFormat_EntitySummary(t *testing.T) {
	result := BuildResponseFormat(EntitySummary{}, "cn")

	rf, ok := result["json_schema"].(map[string]any)
	if !ok {
		t.Fatal("缺少 json_schema 字段")
	}
	if rf["name"] != "EntitySummary" {
		t.Errorf("name 期望 EntitySummary，实际 %v", rf["name"])
	}
	schema, ok := rf["schema"].(map[string]any)
	if !ok {
		t.Fatal("缺少 schema 字段")
	}
	if schema["type"] != "object" {
		t.Errorf("schema type 期望 object，实际 %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		t.Errorf("schema properties 不应为空")
	}
	if _, ok := props["summary"]; !ok {
		t.Error("缺少 summary 属性")
	}
	if _, ok := props["attributes"]; !ok {
		t.Error("缺少 attributes 属性")
	}
}

// TestBuildResponseFormat_EntityDuplication 测试 EntityDuplication
func TestBuildResponseFormat_EntityDuplication(t *testing.T) {
	result := BuildResponseFormat(EntityDuplication{}, "cn")
	rf := result["json_schema"].(map[string]any)
	if rf["name"] != "EntityDuplication" {
		t.Errorf("name 期望 EntityDuplication，实际 %v", rf["name"])
	}
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/extraction/... -run "TestStrictSchemaEnforce|TestBuildResponseFormat" -count=1 2>&1 | head -20`
Expected: 编译失败，未定义 StrictSchemaEnforce/BuildResponseFormat

- [x] **Step 3: 实现 StrictSchemaEnforce + BuildResponseFormat**

在 `internal/agentcore/memory/graph/extraction/base.go` 导出函数区块末尾（`ReadableSchema` 之后）添加：

```go
// StrictSchemaEnforce BFS 遍历 JSON Schema，对所有 type=object 节点设置 additionalProperties=false 和 required
//
// 对齐 Python MultilingualBaseModel.multilingual_model_json_schema(strict=True) 中的 BFS 逻辑
func StrictSchemaEnforce(schemaMap map[string]any) {
	toVisit := []map[string]any{schemaMap}
	for len(toVisit) > 0 {
		node := toVisit[0]
		toVisit = toVisit[1:]
		if node == nil {
			continue
		}
		if typeName, _ := node["type"].(string); typeName == "object" {
			if props, ok := node["properties"].(map[string]any); ok && len(props) > 0 {
				node["additionalProperties"] = false
				keys := make([]string, 0, len(props))
				for k := range props {
					keys = append(keys, k)
				}
				node["required"] = keys
			}
		}
		// BFS 继续遍历所有值
		for _, v := range node {
			switch child := v.(type) {
			case map[string]any:
				toVisit = append(toVisit, child)
			case []any:
				for _, item := range child {
					if m, ok := item.(map[string]any); ok {
						toVisit = append(toVisit, m)
					}
				}
			}
		}
	}
}

// BuildResponseFormat 从模型实例生成完整的 OpenAI response_format
//
// 封装 ReadableSchema → StrictSchemaEnforce → ResponseFormat 链路，
// 对齐 Python EntitySummary.response_format(language) 的完整调用链
func BuildResponseFormat(model any, language string) map[string]any {
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	_, schemaMap := ReadableSchema(modelType, language)
	if schemaMap == nil {
		schemaMap = map[string]any{}
	}
	StrictSchemaEnforce(schemaMap)

	// 从类型名提取 model name（去掉泛型后缀）
	name := modelType.Name()
	return ResponseFormat(name, schemaMap)
}
```

注意：需要在 base.go import 中确认已有 `"reflect"`，已有则不需添加。

- [x] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/extraction/... -run "TestStrictSchemaEnforce|TestBuildResponseFormat" -count=1`
Expected: PASS

- [x] **Step 5: 提交**

```bash
git add internal/agentcore/memory/graph/extraction/base.go internal/agentcore/memory/graph/extraction/base_test.go
git commit -m "feat(extraction): 新增 StrictSchemaEnforce + BuildResponseFormat

对齐 Python multilingual_model_json_schema(strict=True) 的 BFS 逻辑，
为 GraphMemPrompting Schema 动态生成提供完整链路"
```

---

### Task 2: initState Schema 动态生成

**Files:**
- Modify: `internal/agentcore/memory/graph/graph_memory/base.go`

- [x] **Step 1: 修改 initState 中 4 行空 Schema**

在 `internal/agentcore/memory/graph/graph_memory/base.go` 中，找到 `initState` 函数内的 4 行空 Schema：

```go
	state.Prompting.SchemaEntityExtraction = extraction.ResponseFormat("EntitySummary", map[string]any{})
	state.Prompting.SchemaEntityDedupe = extraction.ResponseFormat("EntityDuplication", map[string]any{})
	state.Prompting.SchemaRelationMerge = extraction.ResponseFormat("MergeRelations", map[string]any{})
	state.Prompting.SchemaRelationFilter = extraction.ResponseFormat("RelevantFacts", map[string]any{})
```

替换为：

```go
	state.Prompting.SchemaEntityExtraction = extraction.BuildResponseFormat(extraction.EntitySummary{}, gm.Language)
	state.Prompting.SchemaEntityDedupe = extraction.BuildResponseFormat(extraction.EntityDuplication{}, state.Prompting.EntityDedupeLanguage)
	state.Prompting.SchemaRelationMerge = extraction.BuildResponseFormat(extraction.MergeRelations{}, gm.Language)
	state.Prompting.SchemaRelationFilter = extraction.BuildResponseFormat(extraction.RelevantFacts{}, gm.Language)
```

注意：`EntityDedupe` 使用 `state.Prompting.EntityDedupeLanguage`（可能被 strategy 的 chinese_entity_dedupe 覆盖为 "cn"），其他 3 个使用 `gm.Language`，对齐 Python `_init_state`。

- [x] **Step 2: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/graph/graph_memory/...`
Expected: 编译成功

- [x] **Step 3: 运行 graph_memory 测试确认无回归**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/graph_memory/... -count=1`
Expected: PASS

- [x] **Step 4: 提交**

```bash
git add internal/agentcore/memory/graph/graph_memory/base.go
git commit -m "fix(graph_memory): initState 使用 BuildResponseFormat 动态生成完整 Schema

对齐 Python _init_state 中 EntitySummary.response_format(self.language)，
替代之前传入空 map[string]any{} 的错误实现"
```

---

### Task 3: FormatExistingRelations 时间格式化

**Files:**
- Modify: `internal/agentcore/memory/graph/extraction/prompts/entity_extraction/base.go`
- Modify: `internal/agentcore/memory/graph/extraction/prompts/entity_extraction/base_test.go`

- [x] **Step 1: 写时间格式化测试**

在 `internal/agentcore/memory/graph/extraction/prompts/entity_extraction/base_test.go` 中添加：

```go
// TestFormatExistingRelations_包含时间 测试 includeTime=true 时格式化 valid_since/valid_until
func TestFormatExistingRelations_包含时间(t *testing.T) {
	relations := []map[string]any{
		{
			"content":     "张三在北京工作",
			"valid_since": int64(1705276800), // 2024-01-15 00:00:00 UTC
			"offset_since": int8(32),          // 32*15min=480min=UTC+8
			"valid_until": int64(1735689600), // 2025-01-01 00:00:00 UTC
			"offset_until": int8(32),
		},
	}
	result := FormatExistingRelations(relations, 1, true)
	if !strings.Contains(result, "valid_since=") {
		t.Error("includeTime=true 时应包含 valid_since=")
	}
	if !strings.Contains(result, "valid_until=") {
		t.Error("includeTime=true 时应包含 valid_until=")
	}
	if !strings.Contains(result, "2024") {
		t.Errorf("valid_since 应包含年份 2024，实际: %s", result)
	}
}

// TestFormatExistingRelations_未知时间跳过 测试 timestamp=-1 时跳过格式化
func TestFormatExistingRelations_未知时间跳过(t *testing.T) {
	relations := []map[string]any{
		{
			"content":     "张三在北京工作",
			"valid_since": int64(-1),
			"valid_until": int64(-1),
		},
	}
	result := FormatExistingRelations(relations, 1, true)
	if strings.Contains(result, "valid_since=") {
		t.Error("timestamp=-1 时不应格式化 valid_since")
	}
	if strings.Contains(result, "valid_until=") {
		t.Error("timestamp=-1 时不应格式化 valid_until")
	}
}

// TestFormatExistingRelations_不包含时间 测试 includeTime=false
func TestFormatExistingRelations_不包含时间(t *testing.T) {
	relations := []map[string]any{
		{
			"content":     "张三在北京工作",
			"valid_since": int64(1705276800),
			"offset_since": int8(32),
		},
	}
	result := FormatExistingRelations(relations, 1, false)
	if strings.Contains(result, "valid_since=") {
		t.Error("includeTime=false 时不应包含时间信息")
	}
}
```

注意：测试文件需要空白导入 cn/en 包以触发 init()：
```go
import (
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/prompts/cn"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/prompts/en"
)
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/extraction/prompts/entity_extraction/... -run "TestFormatExistingRelations_包含时间" -count=1 2>&1 | tail -5`
Expected: PASS（当前函数不报错但输出不含时间信息，所以测试会 FAIL 在 Contains 检查）

- [x] **Step 3: 实现 FormatExistingRelations 时间格式化**

修改 `internal/agentcore/memory/graph/extraction/prompts/entity_extraction/base.go` 中的 `FormatExistingRelations` 函数，从：

```go
func FormatExistingRelations(relations []map[string]any, startIdx int, includeTime bool) string {
	if len(relations) == 0 {
		return ""
	}
	tmpl := "{i}. {content}"
	var lines []string
	for i, rel := range relations {
		content := fmt.Sprintf("%v", rel["content"])
		line := strings.ReplaceAll(tmpl, "{i}", fmt.Sprintf("%d", startIdx+i))
		line = strings.ReplaceAll(line, "{content}", content)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n\n")
}
```

改为：

```go
func FormatExistingRelations(relations []map[string]any, startIdx int, includeTime bool) string {
	if len(relations) == 0 {
		return ""
	}
	tmpl := "{i}. {content}"
	var lines []string
	for i, rel := range relations {
		content := fmt.Sprintf("%v", rel["content"])

		// 对齐 Python: include_time and valid_since != -1
		if includeTime {
			if validSince, _ := toInt64(rel["valid_since"]); validSince != -1 {
				offsetSince, _ := toInt8(rel["offset_since"])
				if t, err := graph.LoadStoredTimeFromDB(validSince, offsetSince); err == nil {
					content += fmt.Sprintf("\nvalid_since=%s", t.Format("2006-01-02T15:04:05Z07:00"))
				}
			}
			if validUntil, _ := toInt64(rel["valid_until"]); validUntil != -1 {
				offsetUntil, _ := toInt8(rel["offset_until"])
				if t, err := graph.LoadStoredTimeFromDB(validUntil, offsetUntil); err == nil {
					content += fmt.Sprintf("\nvalid_until=%s", t.Format("2006-01-02T15:04:05Z07:00"))
				}
			}
		}

		line := strings.ReplaceAll(tmpl, "{i}", fmt.Sprintf("%d", startIdx+i))
		line = strings.ReplaceAll(line, "{content}", content)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n\n")
}
```

同时在文件底部非导出函数区块添加两个辅助函数：

```go
// toInt64 将 any 转为 int64（对齐 Python rel.get("valid_since", 0)）
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}

// toInt8 将 any 转为 int8（对齐 Python rel.get("offset_since", 0)）
func toInt8(v any) (int8, bool) {
	switch n := v.(type) {
	case int8:
		return n, true
	case int:
		return int8(n), true
	case int64:
		return int8(n), true
	case float64:
		return int8(n), true
	default:
		return 0, false
	}
}
```

更新 import 添加：
```go
import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
)
```

- [x] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/extraction/prompts/entity_extraction/... -run "TestFormatExistingRelations" -count=1`
Expected: PASS

- [x] **Step 5: 提交**

```bash
git add internal/agentcore/memory/graph/extraction/prompts/entity_extraction/base.go internal/agentcore/memory/graph/extraction/prompts/entity_extraction/base_test.go
git commit -m "fix(entity_extraction): FormatExistingRelations 补充时间格式化

对齐 Python format_existing_relations 的 include_time 分支，
调用 graph.LoadStoredTimeFromDB 生成 ISO 时间字符串"
```

---

### Task 4: FormatListOfMessages + AddMemory 消息列表

**Files:**
- Modify: `internal/agentcore/foundation/store/graph/utils.go`
- Modify: `internal/agentcore/foundation/store/graph/utils_test.go`
- Modify: `internal/agentcore/memory/graph/graph_memory/base.go`

- [x] **Step 1: 写 FormatListOfMessages 测试**

在 `internal/agentcore/foundation/store/graph/utils_test.go` 中添加：

```go
// TestFormatListOfMessages_基本 测试基本消息格式化
func TestFormatListOfMessages_基本(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "你好"},
		{"role": "assistant", "content": "很高兴认识你"},
	}
	result := FormatListOfMessages(messages, nil, "")
	if !strings.Contains(result, "user: 你好") {
		t.Errorf("期望包含 'user: 你好'，实际: %s", result)
	}
	if !strings.Contains(result, "assistant: 很高兴认识你") {
		t.Errorf("期望包含 'assistant: 很高兴认识你'，实际: %s", result)
	}
}

// TestFormatListOfMessages_角色替换 测试 roleReplace 映射
func TestFormatListOfMessages_角色替换(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "你好"},
	}
	roleReplace := map[string]string{"user": "张三（用户）"}
	result := FormatListOfMessages(messages, roleReplace, "")
	if !strings.Contains(result, "张三（用户）: 你好") {
		t.Errorf("期望包含角色替换结果，实际: %s", result)
	}
}

// TestFormatListOfMessages_自定义模板 测试自定义 template
func TestFormatListOfMessages_自定义模板(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "你好"},
	}
	result := FormatListOfMessages(messages, nil, "[{role}] {content}\n")
	if !strings.Contains(result, "[user] 你好") {
		t.Errorf("期望包含自定义模板结果，实际: %s", result)
	}
}

// TestFormatListOfMessages_空列表 测试空消息列表
func TestFormatListOfMessages_空列表(t *testing.T) {
	result := FormatListOfMessages(nil, nil, "")
	if result != "" {
		t.Errorf("空列表应返回空字符串，实际: %s", result)
	}
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/store/graph/... -run "TestFormatListOfMessages" -count=1 2>&1 | head -5`
Expected: 编译失败，未定义 FormatListOfMessages

- [x] **Step 3: 实现 FormatListOfMessages**

在 `internal/agentcore/foundation/store/graph/utils.go` 导出函数区块末尾添加：

```go
// FormatListOfMessages 将消息列表格式化为字符串
//
// 对齐 Python foundation/store/graph/utils.py: format_list_of_messages
//
// messages 为包含 "role" 和 "content" 键的 dict 列表，
// roleReplace 用于将角色名映射为自定义名称（如 "user" → "张三（用户）"），
// template 为每条消息的格式化模板，默认 "{role}: {content}\n"。
func FormatListOfMessages(messages []map[string]any, roleReplace map[string]string, template string) string {
	if len(messages) == 0 {
		return ""
	}
	if template == "" {
		template = "{role}: {content}\n"
	}
	if roleReplace == nil {
		roleReplace = map[string]string{}
	}
	var result strings.Builder
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		if replaced, ok := roleReplace[role]; ok {
			role = replaced
		}
		content := fmt.Sprintf("%v", msg["content"])
		line := strings.ReplaceAll(template, "{role}", role)
		line = strings.ReplaceAll(line, "{content}", content)
		result.WriteString(line)
	}
	return result.String()
}
```

需要在 utils.go import 中确认有 `"fmt"` 和 `"strings"`，缺少则添加。

- [x] **Step 4: 运行 FormatListOfMessages 测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/store/graph/... -run "TestFormatListOfMessages" -count=1`
Expected: PASS

- [x] **Step 5: 修改 AddMemoryConfig 和 prepareEpisodes**

在 `internal/agentcore/memory/graph/graph_memory/base.go` 中：

1. `AddMemoryConfig` 结构体新增 `Messages` 字段：

```go
type AddMemoryConfig struct {
	// SourceType 片段类型
	SourceType config.EpisodeType
	// UserID 用户标识
	UserID string
	// Content 内容字符串（与 Messages 二选一）
	Content string
	// Messages 消息列表输入（与 Content 二选一，仅 CONVERSATION 类型）
	Messages []llmschema.BaseMessage
	// ContentFmtKwargs 内容格式化参数（对话场景的角色映射，仅 Messages 时有效）
	ContentFmtKwargs map[string]string
	// ReferenceTime 参考时间，nil 表示使用当前时间
	ReferenceTime *time.Time
}
```

2. 修改 `AddMemory` 方法中将 `cfg.Content` 传给 `prepareEpisodes` 的调用，改为同时传入 `cfg.Messages`：

找到 `content, err := gm.prepareEpisodes(ctx, state, cfg.Content, cfg.SourceType, cfg.UserID, cfg.ContentFmtKwargs)` 这行，将 `prepareEpisodes` 签名和调用改为：

```go
content, err := gm.prepareEpisodes(ctx, state, cfg.Content, cfg.Messages, cfg.SourceType, cfg.UserID, cfg.ContentFmtKwargs)
```

3. 修改 `prepareEpisodes` 函数签名，新增 `messages []llmschema.BaseMessage` 参数，并在函数体内添加消息列表分支：

将函数签名从：
```go
func (gm *GraphMemory) prepareEpisodes(ctx context.Context, state *GraphMemState, content string, srcType config.EpisodeType, userID string, contentFmtKwargs map[string]string) (string, error) {
```
改为：
```go
func (gm *GraphMemory) prepareEpisodes(ctx context.Context, state *GraphMemState, content string, messages []llmschema.BaseMessage, srcType config.EpisodeType, userID string, contentFmtKwargs map[string]string) (string, error) {
```

在 `prepareEpisodes` 函数体内，找到现有的 content 空值检查之前，添加二选一校验和消息列表处理：

```go
	// 二选一校验（对齐 Python: isinstance(content, str) vs list[BaseMessage | dict]）
	if content == "" && len(messages) == 0 {
		return "", exception.BuildError(
			exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", "content and messages cannot both be empty"),
		)
	}
	if content != "" && len(messages) > 0 {
		return "", exception.BuildError(
			exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", "content and messages are mutually exclusive, please provide only one"),
		)
	}

	// 消息列表路径（对齐 Python: content is list[BaseMessage | dict]）
	if len(messages) > 0 {
		if srcType != config.EpisodeTypeConversation {
			return "", exception.BuildError(
				exception.StatusMemoryStoreValidationInvalid,
				exception.WithParam("store_type", storeType),
				exception.WithParam("error_msg", "messages input requires src_type=CONVERSATION"),
			)
		}
		dicts, err := Msg2Dict(messages, false)
		if err != nil {
			return "", exception.BuildError(
				exception.StatusMemoryStoreValidationInvalid,
				exception.WithParam("store_type", storeType),
				exception.WithParam("error_msg", "The content must be str or list of messages in dict or BaseMessage standard"),
				exception.WithCause(err),
			)
		}
		// 校验每条消息有 role + content
		for _, msg := range dicts {
			if _, ok := msg["role"]; !ok {
				return "", exception.BuildError(
					exception.StatusMemoryStoreValidationInvalid,
					exception.WithParam("store_type", storeType),
					exception.WithParam("error_msg", `The content is not a list of dict with keys "role" and "content"`),
				)
			}
			if _, ok := msg["content"]; !ok {
				return "", exception.BuildError(
					exception.StatusMemoryStoreValidationInvalid,
					exception.WithParam("store_type", storeType),
					exception.WithParam("error_msg", `The content is not a list of dict with keys "role" and "content"`),
				)
			}
		}
		content = graph.FormatListOfMessages(dicts, contentFmtKwargs, "")
	}
```

注意：content 字符串路径中已有对 content_fmt_kwargs 的校验（非空报错），保留不变。

- [x] **Step 6: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/graph/graph_memory/...`
Expected: 编译成功

- [x] **Step 7: 运行 graph_memory 测试确认无回归**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/graph_memory/... -count=1`
Expected: PASS

- [x] **Step 8: 提交**

```bash
git add internal/agentcore/foundation/store/graph/utils.go internal/agentcore/foundation/store/graph/utils_test.go internal/agentcore/memory/graph/graph_memory/base.go
git commit -m "feat(graph_memory): AddMemory 支持消息列表输入 + FormatListOfMessages

对齐 Python add_memory(content: list[BaseMessage | dict] | str)，
AddMemoryConfig 新增 Messages 字段（与 Content 二选一），
新增 FormatListOfMessages 对齐 Python format_list_of_messages"
```

---

### Task 5: ReadableSchema refDict 结构修复

**Files:**
- Modify: `internal/agentcore/memory/graph/extraction/base.go`

- [x] **Step 1: 修改 extractRefDictRecursive 只保留 properties**

在 `internal/agentcore/memory/graph/extraction/base.go` 中，修改 `extractRefDictRecursive` 函数：

```go
// 修改前
func extractRefDictRecursive(schema map[string]any, refDict map[string]map[string]any) {
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		return
	}
	for _, propVal := range props {
		propMap, ok := propVal.(map[string]any)
		if !ok {
			continue
		}
		defs, _ := propMap["$defs"].(map[string]any)
		for defName, defVal := range defs {
			defMap, ok := defVal.(map[string]any)
			if ok {
				refDict[defName] = defMap
			}
		}
		extractRefDictRecursive(propMap, refDict)
	}
}

// 修改后
func extractRefDictRecursive(schema map[string]any, refDict map[string]map[string]any) {
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		return
	}
	for _, propVal := range props {
		propMap, ok := propVal.(map[string]any)
		if !ok {
			continue
		}
		defs, _ := propMap["$defs"].(map[string]any)
		for defName, defVal := range defs {
			defMap, ok := defVal.(map[string]any)
			if ok {
				// 对齐 Python: refDict = {key: val["properties"] for key, val in refs.items()}
				if propsOnly, ok := defMap["properties"].(map[string]any); ok {
					refDict[defName] = propsOnly
				}
			}
		}
		extractRefDictRecursive(propMap, refDict)
	}
}
```

- [x] **Step 2: 运行 extraction 测试确认无回归**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/extraction/... -count=1`
Expected: PASS

- [x] **Step 3: 提交**

```bash
git add internal/agentcore/memory/graph/extraction/base.go
git commit -m "fix(extraction): extractRefDict 只保留 properties 子集

对齐 Python readable_schema 返回 {key: val[\"properties\"]}，
避免 FormatSchemaInfo JSON 序列化时多出多余字段干扰 LLM"
```

---

### Task 6: 删除多余 FormatNewEntities

**Files:**
- Delete: `internal/agentcore/memory/graph/extraction/prompts/entity_extraction/format.go`
- Delete: `internal/agentcore/memory/graph/extraction/prompts/entity_extraction/format_test.go`
- Modify: `internal/agentcore/memory/graph/extraction/prompts/entity_extraction/doc.go`
- Modify: `internal/agentcore/memory/graph/extraction/prompts/doc.go`

- [x] **Step 1: 确认无调用方**

Run: `cd /home/opensource/uapclaw-gateway && grep -rn "FormatNewEntities" --include="*.go" | grep -v "_test.go" | grep -v format.go | grep -v doc.go | grep -v "format_new_entities"`
Expected: 仅在 extraction_prompts_test.go 的测试中引用 `formatNewEntities`（小写，是 extraction 包的函数），`entity_extraction.FormatNewEntities` 无外部调用方

- [x] **Step 2: 删除文件**

```bash
rm internal/agentcore/memory/graph/extraction/prompts/entity_extraction/format.go
rm internal/agentcore/memory/graph/extraction/prompts/entity_extraction/format_test.go
```

- [x] **Step 3: 更新 entity_extraction/doc.go**

从：
```go
//	entity_extraction/
//	├── doc.go       # 包文档
//	├── base.go      # 格式化辅助函数（FormatSchemaInfo/FormatSourceDescription/...）
//	└── format.go    # FormatNewEntities 格式化辅助
```

改为：
```go
//	entity_extraction/
//	├── doc.go       # 包文档
//	└── base.go      # 格式化辅助函数（FormatSchemaInfo/FormatSourceDescription/...）
```

- [x] **Step 4: 更新 prompts/doc.go**

从：
```go
//	    ├── doc.go            # 包文档
//	    ├── base.go           # 格式化辅助函数（FormatSchemaInfo/FormatSourceDescription/...）
//	    └── format.go         # FormatNewEntities 格式化辅助
```

改为：
```go
//	    ├── doc.go            # 包文档
//	    └── base.go           # 格式化辅助函数（FormatSchemaInfo/FormatSourceDescription/...）
```

- [x] **Step 5: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/graph/extraction/...`
Expected: 编译成功

- [x] **Step 6: 提交**

```bash
git add -A internal/agentcore/memory/graph/extraction/prompts/entity_extraction/
git add internal/agentcore/memory/graph/extraction/prompts/doc.go
git commit -m "refactor(entity_extraction): 删除 Python 中不存在的 FormatNewEntities

format.go + format_test.go 是误加的函数，Python prompts/entity_extraction/base.py
中无此函数。统一使用 extraction.formatNewEntities（对齐 Python extraction_prompts.py）"
```

---

### Task 7: Search 并发化

**Files:**
- Modify: `internal/agentcore/memory/graph/graph_memory/base.go`

- [x] **Step 1: 修改 Search 方法使用 errgroup**

在 `internal/agentcore/memory/graph/graph_memory/base.go` 中，找到 Search 方法的串行搜索部分：

```go
	// 实体搜索
	if searchEntity {
		objects, err := gm.performSearch(ctx, 0, userIDs, strategyName, query, queryEmbedding)
		if err != nil {
			return nil, err
		}
		result.Entity = objects
	}

	// 关系搜索
	if searchRelation {
		objects, err := gm.performSearch(ctx, 1, userIDs, strategyName, query, queryEmbedding)
		if err != nil {
			return nil, err
		}
		result.Relation = objects
	}

	// 片段搜索
	if searchEpisode {
		objects, err := gm.performSearch(ctx, 2, userIDs, strategyName, query, queryEmbedding)
		if err != nil {
			return nil, err
		}
		result.Episode = objects
	}

	return result, nil
```

替换为：

```go
	// 并发搜索三个集合（对齐 Python asyncio.as_completed）
	g, gctx := errgroup.WithContext(ctx)

	if searchEntity {
		g.Go(func() error {
			objects, err := gm.performSearch(gctx, 0, userIDs, strategyName, query, queryEmbedding)
			if err != nil {
				return err
			}
			result.Entity = objects
			return nil
		})
	}

	if searchRelation {
		g.Go(func() error {
			objects, err := gm.performSearch(gctx, 1, userIDs, strategyName, query, queryEmbedding)
			if err != nil {
				return err
			}
			result.Relation = objects
			return nil
		})
	}

	if searchEpisode {
		g.Go(func() error {
			objects, err := gm.performSearch(gctx, 2, userIDs, strategyName, query, queryEmbedding)
			if err != nil {
				return err
			}
			result.Episode = objects
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return result, nil
```

在文件 import 中添加：
```go
import "golang.org/x/sync/errgroup"
```

- [x] **Step 2: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/graph/graph_memory/...`
Expected: 编译成功

- [x] **Step 3: 运行测试确认无回归**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/graph_memory/... -count=1`
Expected: PASS

- [x] **Step 4: 提交**

```bash
git add internal/agentcore/memory/graph/graph_memory/base.go
git commit -m "perf(graph_memory): Search 使用 errgroup 并发搜索三个集合

对齐 Python asyncio.as_completed 并发搜索 entity/relation/episode，
三个搜索互相独立，errgroup 保证任一失败即返回错误"
```

---

### Task 8: userID any → []string

**Files:**
- Modify: `internal/agentcore/memory/graph/graph_memory/base.go`
- Modify: `internal/agentcore/memory/graph/graph_memory/validate_input.go`
- Modify: `internal/agentcore/memory/graph/graph_memory/validate_input_test.go`

- [x] **Step 1: 修改 Search 签名**

在 `internal/agentcore/memory/graph/graph_memory/base.go` 中，修改 `Search` 方法签名：

从：
```go
func (gm *GraphMemory) Search(ctx context.Context, query string, userID any, searchEntity, searchRelation, searchEpisode bool, opts ...SearchOption) (*SearchResult, error) {
```
改为：
```go
func (gm *GraphMemory) Search(ctx context.Context, query string, userIDs []string, searchEntity, searchRelation, searchEpisode bool, opts ...SearchOption) (*SearchResult, error) {
```

修改 `Search` 方法体内的 `ValidateSearchInput` 调用：

从：
```go
	userIDs, err := ValidateSearchInput(query, userID, []bool{searchEntity, searchRelation, searchEpisode})
```
改为：
```go
	userIDs, err := ValidateSearchInput(query, userIDs, []bool{searchEntity, searchRelation, searchEpisode})
```

注意：这里局部变量 `userIDs` 与参数 `userIDs` 同名，但 ValidateSearchInput 返回的 userIDs 是经过校验后的结果，可改为直接赋值给同一变量或用 `_` 忽略（如果校验逻辑已覆盖）。查看 ValidateSearchInput 当前实现——它返回 `([]string, error)`，其中校验每个 userID 长度 <= 32。保留调用即可：

```go
	validatedUserIDs, err := ValidateSearchInput(query, userIDs, []bool{searchEntity, searchRelation, searchEpisode})
	if err != nil {
		return nil, err
	}
```

然后将后续使用 `userIDs` 的地方改为 `validatedUserIDs`（3 个 performSearch 调用 + errgroup 中）。

- [x] **Step 2: 修改 ValidateSearchInput 签名**

在 `internal/agentcore/memory/graph/graph_memory/validate_input.go` 中：

从：
```go
func ValidateSearchInput(query string, userID any, settings []bool) ([]string, error) {
	// 校验 query
	...

	// 将 userID 规范化为 []string
	userIDs, err := normalizeUserIDs(userID)
	if err != nil {
		return nil, err
	}

	// 校验每个 userID 元素
	for _, uid := range userIDs {
		...
	}
	...
	return userIDs, nil
}
```
改为：
```go
func ValidateSearchInput(query string, userIDs []string, settings []bool) ([]string, error) {
	// 校验 query
	...

	// 校验每个 userID 元素
	for _, uid := range userIDs {
		trimmed := strings.TrimSpace(uid)
		if trimmed == "" || len(trimmed) > 32 {
			return nil, exception.BuildError(
				exception.StatusMemoryStoreValidationInvalid,
				exception.WithParam("store_type", storeType),
				exception.WithParam("error_msg", "user_id must be a non-empty string of length <= 32 or a list of such strings"),
			)
		}
	}
	...
	return userIDs, nil
}
```

删除 `normalizeUserIDs` 函数。

- [x] **Step 3: 更新 validate_input_test.go**

将所有 `ValidateSearchInput` 调用中的 `userID` 参数从 `any` 改为 `[]string`：
- `"user123"` → `[]string{"user123"}`
- `[]string{"user1", "user2"}` → 保持不变
- `123` (int) → 删除该无效类型测试（Go 类型系统已保证）
- `""` → `[]string{""}`

删除 `TestNormalizeUserIDs_*` 系列测试（函数已删除）。
删除 `TestValidateSearchInput_无效userID类型` 测试（Go 类型系统已保证不会传入非 string 类型）。

- [x] **Step 4: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/graph/graph_memory/...`
Expected: 编译成功

- [x] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/graph_memory/... -count=1`
Expected: PASS

- [x] **Step 6: 提交**

```bash
git add internal/agentcore/memory/graph/graph_memory/base.go internal/agentcore/memory/graph/graph_memory/validate_input.go internal/agentcore/memory/graph/graph_memory/validate_input_test.go
git commit -m "refactor(graph_memory): userID 参数类型从 any 改为 []string

对齐 Python user_id: str | list[str] 语义，调用方传 []string{uid} 兼容单个，
删除 normalizeUserIDs 运行时类型检查，Go 类型系统已保证"
```

---

### Task 9: 全量测试 + 最终提交

**Files:**
- 无新文件

- [x] **Step 1: 运行 extraction 全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/extraction/... -count=1`
Expected: PASS

- [x] **Step 2: 运行 graph_memory 全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/graph/graph_memory/... -count=1`
Expected: PASS

- [x] **Step 3: 运行 graph store 全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/store/graph/... -count=1`
Expected: PASS

- [x] **Step 4: 推送所有提交**

```bash
git push
```
