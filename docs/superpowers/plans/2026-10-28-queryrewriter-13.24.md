# 13.24 QueryRewriter 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 13.24 QueryRewriter——检索前查询改写器（指代消解/补全 + 历史压缩降级），对齐 Python `openjiuwen/core/retrieval/query_rewriter/`。

**Architecture:** 新包 `internal/agentcore/retrieval/query_rewriter/`（与 Python 目录同构）：4 个 md 提示词 go:embed 嵌入 + repair.go 模块级函数（fillTemplate/extractJSON/parseLLMJSON/force 三兄弟/schemaRepair）+ query_rewriter.go 主体（NewQueryRewriter 构造从 ModelConfig 建 Model、LoadTemplate 缓存、Msg2Text 格式化、Compress/Rewrite 双 LLM 链 + 压缩失败降级）。设计 spec：`docs/superpowers/specs/2026-10-28-queryrewriter-13.24-design.md`（7 项拍板决策 + 8 条偏差归档，实施中遇到分歧以 spec 为准）。

**Tech Stack:** Go 1.x + go:embed + testify/assert + RealAlexandreAI/json-repair v0.0.18（已有依赖）+ 项目自有 llm.Model/ceinterface.ModelContext/output_parsers.JsonOutputParser/exception 错误码 155603-155606（已注册零消费）。

**关键约定（全任务适用）：**
- 注释全部中文；源码声明按 结构体→枚举→常量→全局变量→导出函数→非导出函数 排列（go-code-conventions.md）
- 日志 Msg 英文（`logger.Debug/Info/Warn(logComponent)` 风格，logComponent = logger.ComponentAgentCore）
- 测试命令统一 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/query_rewriter/... -cover`（记忆规则：CI 同款构建标签）
- git 提交显式 `git add <文件>`，禁止 `-am`（工作区有他任务改动）
- 编译前 `pgrep -f 'go (build|test)'` 检查残留进程；GOPROXY 已配置无需重设
- Python 源码路径：`/home/opensource/agent-core/openjiuwen/core/retrieval/query_rewriter/`；测试：`/home/opensource/agent-core/tests/unit_tests/core/retrieval/query_rewriter/test_query_rewriter.py`

---

### Task 1: 提示词文件 + embed.go + doc.go

**Files:**
- Create: `internal/agentcore/retrieval/query_rewriter/prompts/compression_zh.md`
- Create: `internal/agentcore/retrieval/query_rewriter/prompts/compression_en.md`
- Create: `internal/agentcore/retrieval/query_rewriter/prompts/intention_completion_zh.md`
- Create: `internal/agentcore/retrieval/query_rewriter/prompts/intention_completion_en.md`
- Create: `internal/agentcore/retrieval/query_rewriter/embed.go`
- Create: `internal/agentcore/retrieval/query_rewriter/doc.go`

- [ ] **Step 1: 逐字复制 4 个提示词文件**

从 Python 源目录复制（**禁止任何改写/翻译**，记忆规则 feedback_prompt_verbatim_copy）：

```bash
mkdir -p internal/agentcore/retrieval/query_rewriter/prompts
cp /home/opensource/agent-core/openjiuwen/core/retrieval/query_rewriter/prompts/compression_zh.md internal/agentcore/retrieval/query_rewriter/prompts/
cp /home/opensource/agent-core/openjiuwen/core/retrieval/query_rewriter/prompts/compression_en.md internal/agentcore/retrieval/query_rewriter/prompts/
cp /home/opensource/agent-core/openjiuwen/core/retrieval/query_rewriter/prompts/intention_completion_zh.md internal/agentcore/retrieval/query_rewriter/prompts/
cp /home/opensource/agent-core/openjiuwen/core/retrieval/query_rewriter/prompts/intention_completion_en.md internal/agentcore/retrieval/query_rewriter/prompts/
```

- [ ] **Step 2: 逐字节校验（md5 对比）**

```bash
for f in compression_zh compression_en intention_completion_zh intention_completion_en; do
  a=$(md5sum /home/opensource/agent-core/openjiuwen/core/retrieval/query_rewriter/prompts/$f.md | cut -d' ' -f1)
  b=$(md5sum internal/agentcore/retrieval/query_rewriter/prompts/$f.md | cut -d' ' -f1)
  [ "$a" = "$b" ] && echo "$f OK" || echo "$f MISMATCH"
done
```

Expected: 4 行全 OK

- [ ] **Step 3: 写 embed.go**

```go
// Package query_rewriter 提供检索前查询改写（go:embed 嵌入模板文件）。
package query_rewriter

import "embed"

// ──────────────────────────── 全局变量 ────────────────────────────

// promptsFS 编译时嵌入的提示词模板文件系统。
//
// Python: Path(__file__).resolve().parent / "prompts"（源码部署），
// Go 使用 go:embed 确保二进制自带模板（memory/prompts 先例）。
//
//go:embed prompts/*.md
var promptsFS embed.FS
```

- [ ] **Step 4: 写 doc.go**

```go
// Package query_rewriter 提供检索前查询改写器（Query Rewriter）。
//
// 把多轮对话中的省略/指代 query 改写成独立可检索的 standalone_query；
// 历史条数达阈值时用 LLM 压缩历史（失败降级为原文写回），控制提示词
// token 膨胀。只消费 ModelContext 的 GetMessages/SetMessages 两方法，
// 当前轮消息的追加由调用方负责（rewrite → 检索 → add_messages 顺序）。
//
// 文件目录：
//
//	query_rewriter/
//	├── doc.go                 # 包文档
//	├── embed.go               # go:embed 嵌入提示词模板
//	├── query_rewriter.go      # QueryRewriter 主体（构造/LoadTemplate/Msg2Text/Compress/Rewrite）
//	├── repair.go              # 模块级函数（fillTemplate/extractJSON/parseLLMJSON/force 三兄弟/schemaRepair）
//	└── prompts/               # 4 个提示词模板（zh/en × compression/intention_completion）
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/query_rewriter/
package query_rewriter
```

注意：go:embed 与 `package` 声明同文件时 embed.go 需带包注释或 doc.go 承担包注释——上面 embed.go 的首行注释是包注释，doc.go 也是包注释会冲突。**修正：embed.go 首行改为普通注释 `// 嵌入提示词模板文件。`（无 Package 前缀），包注释只保留在 doc.go。**

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/retrieval/query_rewriter/
```

Expected: 无输出（成功）

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/retrieval/query_rewriter/
git commit -m "feat(13.24): 提示词 4 文件逐字嵌入 + embed.go + doc.go"
```

---

### Task 2: repair.go 模块级函数（fillTemplate/extractJSON/parseLLMJSON）+ 测试

**Files:**
- Create: `internal/agentcore/retrieval/query_rewriter/repair.go`
- Create: `internal/agentcore/retrieval/query_rewriter/repair_test.go`

- [ ] **Step 1: 写失败测试（fillTemplate/extractJSON/parseLLMJSON 部分）**

repair_test.go（本任务先写前三个函数的用例，Task 3 补 force/schemaRepair 用例）：

```go
package query_rewriter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFillTemplate 占位符替换（Python TestFillTemplate 2 例）
func TestFillTemplate(t *testing.T) {
	// 占位符替换
	assert.Equal(t, "a=1 b=2", fillTemplate("a={a} b={b}", map[string]string{"a": "1", "b": "2"}))
	// JSON 示例花括号不误伤
	assert.Equal(t, `output: hi, example: {"x":1}`,
		fillTemplate(`output: {history}, example: {"x":1}`, map[string]string{"history": "hi"}))
}

// TestExtractJSON 首尾花括号提取（Python TestExtractJson 4 例）
func TestExtractJSON(t *testing.T) {
	assert.Equal(t, `{"a":1}`, extractJSON(`prefix {"a":1} suffix`)) // 单对象
	assert.Equal(t, "", extractJSON("no json here"))                 // 无括号
	assert.Equal(t, "", extractJSON("{"))                            // 只有开括号
	assert.Equal(t, `{"outer":{"inner":1}}`, extractJSON(` {"outer":{"inner":1}} `)) // 首{到尾}
}

// TestParseLLMJSON JSON 解析与修复（Python TestParseLlmJson 5 例）
func TestParseLLMJSON(t *testing.T) {
	// 合法 JSON 返回 dict
	m, ok := parseLLMJSON(`{"a":1}`)
	assert.True(t, ok)
	assert.Equal(t, float64(1), m["a"])

	// 空串/空白 → 不 ok
	_, ok = parseLLMJSON("")
	assert.False(t, ok)
	_, ok = parseLLMJSON("   ")
	assert.False(t, ok)

	// 纯文本（无括号）→ 不 ok
	_, ok = parseLLMJSON("not json")
	assert.False(t, ok)

	// 尾逗号由 json-repair 修复
	m, ok = parseLLMJSON(`{"theme":["a"], "summary":"b",}`)
	assert.True(t, ok)
	assert.Equal(t, []any{"a"}, m["theme"])
	assert.Equal(t, "b", m["summary"])

	// 非字典根（数组/null）→ 不 ok
	_, ok = parseLLMJSON("[1,2,3]")
	assert.False(t, ok)
	_, ok = parseLLMJSON("null")
	assert.False(t, ok)
}
```

- [ ] **Step 2: 运行确认编译失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/query_rewriter/... 
```

Expected: FAIL（fillTemplate 等未定义，编译错误）

- [ ] **Step 3: 写 repair.go 前半部分**

```go
package query_rewriter

import (
	"encoding/json"
	"strings"

	jsonrepair "github.com/RealAlexandreAI/json-repair"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// fillTemplate 只替换模板中显式 {key} 占位符，其他花括号不动
// （JSON 示例不触发 str.format KeyError，Python _fill_template）。
func fillTemplate(template string, kv map[string]string) string {
	out := template
	for key, value := range kv {
		out = strings.ReplaceAll(out, "{"+key+"}", value)
	}
	return out
}

// extractJSON 提取 LLM 输出中首个 { 到最后 } 之间的 JSON 文本
// （Python _extract_json）。
func extractJSON(modelOutput string) string {
	start := strings.IndexByte(modelOutput, '{')
	end := strings.LastIndexByte(modelOutput, '}')
	if start == -1 || end == -1 || end <= start {
		logger.Debug(logComponent).Msg("No JSON object found in model output")
		return ""
	}
	logger.Debug(logComponent).Msg("JSON object from output extracted")
	return modelOutput[start : end+1]
}

// parseLLMJSON 解析 LLM 输出 JSON 字符串：先 json.Unmarshal，
// 失败再经 json-repair 修复重试；返回 dict 与是否成功，不返回 error
// （Python _parse_llm_json 的 None 语义）。
func parseLLMJSON(jsonStr string) (map[string]any, bool) {
	if strings.TrimSpace(jsonStr) == "" {
		return nil, false
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &out); err == nil {
		return out, true
	}
	result, err := jsonrepair.RepairWithOptions(jsonStr, jsonrepair.RepairOptions{})
	if err != nil {
		return nil, false
	}
	if err := json.Unmarshal([]byte(result.Value.(string)), &out); err != nil {
		return nil, false
	}
	logger.Debug(logComponent).Msg("JSON parsed after repair_json")
	return out, true
}
```

注意：`jsonrepair.RepairWithOptions` 返回 `result.Value` 为 `any`（13.17 先例是取 `result.Value.(string)` 后再 Unmarshal——若类型断言失败需防御）。实施时以 13.17 parseTriples 的真实调用形态为准：`result.Value.(string)` 断言失败时返回 false。

- [ ] **Step 4: 运行测试通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run "TestFillTemplate|TestExtractJSON|TestParseLLMJSON" ./internal/agentcore/retrieval/query_rewriter/... -v
```

Expected: 3 个测试全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/query_rewriter/repair.go internal/agentcore/retrieval/query_rewriter/repair_test.go
git commit -m "feat(13.24): fillTemplate/extractJSON/parseLLMJSON 模块级函数 + 测试"
```

---

### Task 3: repair.go 后半（force 三兄弟 + schemaRepair + schemaFieldType）+ 测试

**Files:**
- Modify: `internal/agentcore/retrieval/query_rewriter/repair.go`
- Modify: `internal/agentcore/retrieval/query_rewriter/repair_test.go`

- [ ] **Step 1: 追加失败测试**

repair_test.go 追加：

```go
// TestForceString 值转字符串（Python TestForceHelpers.string）
func TestForceString(t *testing.T) {
	assert.Equal(t, "x", forceString("x"))
	// dict → JSON 序列化（空格差异容忍：Python '{"a": 1}'，Go '{"a":1}'）
	assert.Contains(t, forceString(map[string]any{"a": 1}), `"a":1`)
}

// TestForceList 值转列表（Python TestForceHelpers.list）
func TestForceList(t *testing.T) {
	assert.Equal(t, []any{1, 2}, forceList([]any{1, 2}))
	assert.Equal(t, []any{"x"}, forceList("x"))
}

// TestForceJSON 值转 dict（Python TestForceHelpers.json）
func TestForceJSON(t *testing.T) {
	// dict 直返
	assert.Equal(t, map[string]any{"a": 1}, forceJSON("k", map[string]any{"a": 1}))
	// JSON 字符串解析成 dict
	assert.Equal(t, map[string]any{"a": float64(1)}, forceJSON("k", `{"a":1}`))
	// 普通字符串包裹
	assert.Equal(t, map[string]any{"k": "plain"}, forceJSON("k", "plain"))
	// Go 增强：合法 JSON 但非对象根 → {key: parsed}
	assert.Equal(t, map[string]any{"k": []any{1}}, forceJSON("k", `[1]`))
	// Go 增强：list → {key: raw}
	assert.Equal(t, map[string]any{"k": []any{1, 2}}, forceJSON("k", []any{1, 2}))
}

// TestSchemaRepair schema 校验修复（Python TestSchemaRepair 5 例）
func TestSchemaRepair(t *testing.T) {
	compressSchema := map[string]schemaFieldType{"theme": schemaFieldList, "summary": schemaFieldString}

	// compress schema 原样通过
	out, err := schemaRepair(map[string]any{"theme": []any{"a"}, "summary": "b"}, compressSchema)
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{"theme": []any{"a"}, "summary": "b"}, out)

	// None 字段填默认
	out, err = schemaRepair(map[string]any{"theme": nil, "summary": nil}, compressSchema)
	assert.NoError(t, err)
	assert.Equal(t, []any{}, out["theme"])
	assert.Equal(t, "", out["summary"])

	// rewrite schema 全字段
	rewriteSchema := map[string]schemaFieldType{
		"before": schemaFieldString, "intention": schemaFieldString,
		"standalone_query": schemaFieldString, "references": schemaFieldDict,
		"missing": schemaFieldList, "typo": schemaFieldList,
		"gibberish": schemaFieldList, "from_history": schemaFieldString,
	}
	raw := map[string]any{
		"before": "那运费呢？", "intention": "咨询运费",
		"standalone_query": "淘美乐退货的运费谁出？",
		"references": map[string]any{"那": "退货运费"},
		"missing": []any{}, "typo": []any{}, "gibberish": []any{},
		"from_history": "history",
	}
	out, err = schemaRepair(raw, rewriteSchema)
	assert.NoError(t, err)
	assert.Equal(t, "淘美乐退货的运费谁出？", out["standalone_query"])
	assert.Equal(t, []any{}, out["typo"])

	// typo 子结构修复
	out, err = schemaRepair(
		map[string]any{"typo": []any{map[string]any{"original": "teh", "corrected": "the", "reason": "typo"}}},
		map[string]schemaFieldType{"typo": schemaFieldList},
	)
	assert.NoError(t, err)
	typoList := out["typo"].([]any)
	assert.Equal(t, "teh", typoList[0].(map[string]any)["original"])
	assert.Equal(t, "the", typoList[0].(map[string]any)["corrected"])

	// Go 增强：typo 非 dict 元素 forceJSON 包裹
	out, err = schemaRepair(
		map[string]any{"typo": []any{"teh"}},
		map[string]schemaFieldType{"typo": schemaFieldList},
	)
	assert.NoError(t, err)
	typoList = out["typo"].([]any)
	assert.Equal(t, "teh", typoList[0].(map[string]any)["typo"])

	// 非 dict → 155604
	_, err = schemaRepair(map[string]any{}, compressSchema) // 正常输入不报错
	assert.NoError(t, err)
}
```

注意最后一例：Python `_schema_repair("not a dict", ...)` 测非 dict 输入抛 155604，但 Go 签名入参已是 `map[string]any`（编译期类型保证），该用例归档不可达（spec §8-3 消解的一部分）——Go 侧改为验证正常输入不报错 + 不可修复类型走 error 路径：

```go
	// Go 增强：不可修复类型（三类型外）→ 155604。构造：string 字段给 list 且 force 失败不可能，
	// 故真实触发路径是 schemaFieldType 超集校验——包内私有类型无此风险，仅验证 error 分支不可达。
```

实际不可达则删除该子用例，在测试文件头部注释记录归档原因。

- [ ] **Step 2: 运行确认编译失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run "TestForce|TestSchemaRepair" ./internal/agentcore/retrieval/query_rewriter/...
```

Expected: FAIL（forceString 等未定义）

- [ ] **Step 3: repair.go 追加实现**

在 Task 2 的 repair.go 追加（枚举区 + 非导出函数区）：

```go
// ──────────────────────────── 枚举 ────────────────────────────

// schemaFieldType schema 字段期望类型（Python str/list/dict 类型对象）。
type schemaFieldType int

const (
	schemaFieldString schemaFieldType = iota
	schemaFieldList
	schemaFieldDict
)

// forceString 值转字符串：dict 走 JSON 序列化（非 ASCII 不转义），
// 失败 Debug 日志后回落 fmt.Sprint；其他类型 fmt.Sprint
// （Python _force_string）。
func forceString(raw any) string {
	if m, ok := raw.(map[string]any); ok {
		data, err := json.Marshal(m)
		if err == nil {
			return string(data)
		}
		logger.Debug(logComponent).Msg("json.dumps failed when forcing value to string, using str(raw)")
	}
	return fmt.Sprint(raw)
}

// forceList 值转列表：list 直返，其他包单元素（Python _force_list）。
func forceList(raw any) []any {
	if l, ok := raw.([]any); ok {
		return l
	}
	return []any{raw}
}

// forceJSON 值转 dict：dict 直返；str 尝试 JSON 解析（成 dict 返回 /
// 成功非 dict 包 {key: parsed} / 失败包 {key: raw}）；list/其他包 {key: raw}
// （Python _force_json）。
func forceJSON(key string, raw any) map[string]any {
	switch v := raw.(type) {
	case map[string]any:
		return v
	case string:
		var parsed any
		if err := json.Unmarshal([]byte(v), &parsed); err == nil {
			if m, ok := parsed.(map[string]any); ok {
				return m
			}
			return map[string]any{key: parsed}
		}
		return map[string]any{key: raw}
	default:
		return map[string]any{key: raw}
	}
}

// schemaRepair 按 schema 校验修复 LLM 输出：类型不匹配时强制转换，
// 不可修复返回 155604（Python _schema_repair）。
//
// Go 入参已是 map[string]any（Python 非 dict 的 155604 分支编译期消解）。
func schemaRepair(output map[string]any, outputSchema map[string]schemaFieldType) (map[string]any, error) {
	repaired := map[string]any{}

	for field, expectedType := range outputSchema {
		value, exists := output[field]
		if !exists || value == nil {
			switch expectedType {
			case schemaFieldString:
				value = ""
			case schemaFieldList:
				value = []any{}
			case schemaFieldDict:
				value = map[string]any{}
			}
		}

		if field == "typo" {
			if _, isList := value.([]any); !isList {
				value = forceList(value)
			}
			repairedTypo := []any{}
			for _, item := range value.([]any) {
				m, isMap := item.(map[string]any)
				if !isMap {
					m = forceJSON(field, item)
				}
				repairedItem := map[string]any{}
				for _, key := range []string{"original", "corrected", "reason"} {
					v, ok := m[key]
					if !ok || v == nil {
						v = ""
					} else if _, isStr := v.(string); !isStr {
						v = forceString(v)
					}
					repairedItem[key] = v
				}
				repairedTypo = append(repairedTypo, repairedItem)
			}
			value = repairedTypo
		}

		switch expectedType {
		case schemaFieldString:
			if _, isStr := value.(string); !isStr {
				value = forceString(value)
			}
		case schemaFieldList:
			if _, isList := value.([]any); !isList {
				value = forceList(value)
			}
		case schemaFieldDict:
			if _, isMap := value.(map[string]any); !isMap {
				value = forceJSON(field, value)
			}
		}

		repaired[field] = value
	}
	logger.Debug(logComponent).Msg("Schema check and repair done")
	return repaired, nil
}
```

注意：Go 入参类型已保证 dict，且 schemaFieldType 只有三种，Python "其他类型 155604" 两分支编译期消解——schemaRepair 的 error 返回值恒为 nil 但保留签名（与 spec §8-3 消解一致，后续消费方语义对齐）。若实施时认为永不返回 error 的签名更干净，去掉 error 返回值并同步改 spec 偏差表（新增 §8-9）。

- [ ] **Step 4: 运行测试通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run "TestForce|TestSchemaRepair" ./internal/agentcore/retrieval/query_rewriter/... -v
```

Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/query_rewriter/repair.go internal/agentcore/retrieval/query_rewriter/repair_test.go
git commit -m "feat(13.24): force 三兄弟 + schemaRepair + schemaFieldType + 测试"
```

---

### Task 4: query_rewriter.go 结构体 + 构造 + LoadTemplate/Msg2Text + 测试

**Files:**
- Create: `internal/agentcore/retrieval/query_rewriter/query_rewriter.go`
- Create: `internal/agentcore/retrieval/query_rewriter/query_rewriter_test.go`

- [ ] **Step 1: 写测试基建（stubQRClient + fakeModelContext）**

query_rewriter_test.go：

```go
package query_rewriter

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// stubQRClient 惰性 mock 客户端（13.17 stubTripleClient 先例）。
// 按 messages 首条 SystemMessage 内容分派：rewrite 提示词（含
// "当前用户输入"）返回 rewriteResp，否则返回 compressResp；
// invokeErr 非空时返回错误。所有字段并发安全（mu 保护）。
type stubQRClient struct {
	mu           sync.Mutex
	rewriteResp  string
	compressResp string
	invokeErr    error
	compressCnt  int
	rewriteCnt   int
}

func (s *stubQRClient) Invoke(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content := ""
	if msgs := messages.Messages(); len(msgs) > 0 {
		content = msgs[0].GetContent().Text()
	}
	if s.invokeErr != nil {
		return nil, s.invokeErr
	}
	if strings.Contains(content, "当前用户输入") || strings.Contains(content, "Current user input") {
		s.rewriteCnt++
		return llmschema.NewAssistantMessage(s.rewriteResp), nil
	}
	s.compressCnt++
	return llmschema.NewAssistantMessage(s.compressResp), nil
}
func (s *stubQRClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}
func (s *stubQRClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubQRClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubQRClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubQRClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubQRClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (s *stubQRClient) SupportsKVCacheRelease() bool { return false }

// fakeModelContext 测试用 ModelContext（GetMessages/SetMessages/Len/AddMessages 真逻辑）。
type fakeModelContext struct {
	messages []llmschema.BaseMessage
}

func (f *fakeModelContext) Len() int { return len(f.messages) }
func (f *fakeModelContext) GetMessages(size int, _ bool) ([]llmschema.BaseMessage, error) {
	if size <= 0 {
		return append([]llmschema.BaseMessage{}, f.messages...), nil
	}
	if size > len(f.messages) {
		return append([]llmschema.BaseMessage{}, f.messages...), nil
	}
	return append([]llmschema.BaseMessage{}, f.messages[len(f.messages)-size:]...), nil
}
func (f *fakeModelContext) SetMessages(msgs []llmschema.BaseMessage, _ bool) {
	f.messages = append([]llmschema.BaseMessage{}, msgs...)
}
func (f *fakeModelContext) PopMessages(size int, _ bool) []llmschema.BaseMessage {
	if size <= 0 || size > len(f.messages) {
		return nil
	}
	popped := append([]llmschema.BaseMessage{}, f.messages[len(f.messages)-size:]...)
	f.messages = f.messages[:len(f.messages)-size]
	return popped
}
func (f *fakeModelContext) ClearMessages(_ context.Context, _ bool, _ ...ceinterface.Option) error {
	f.messages = nil
	return nil
}
func (f *fakeModelContext) AddMessages(_ context.Context, msgs []llmschema.BaseMessage, _ ...ceinterface.Option) ([]llmschema.BaseMessage, error) {
	f.messages = append(f.messages, msgs...)
	return msgs, nil
}
func (f *fakeModelContext) GetContextWindow(_ context.Context, sys []llmschema.BaseMessage, _ []cschema.ToolInfoInterface, _, _ int, _ ...ceinterface.Option) (*ceinterface.ContextWindow, error) {
	return &ceinterface.ContextWindow{SystemMessages: sys, ContextMessages: append([]llmschema.BaseMessage{}, f.messages...)}, nil
}
func (f *fakeModelContext) Statistic() *ceinterface.ContextStats { return &ceinterface.ContextStats{} }
func (f *fakeModelContext) SessionID() string                    { return "qr_test_session" }
func (f *fakeModelContext) ContextID() string                    { return "qr_test_context" }
func (f *fakeModelContext) TokenCounter() token.TokenCounter     { return nil }
func (f *fakeModelContext) ReloaderTool() tool.Tool              { return nil }
func (f *fakeModelContext) WorkspaceDir() string                 { return "" }
func (f *fakeModelContext) SetSessionRef(_ sessioninterfaces.SessionFacade) {}
func (f *fakeModelContext) GetSessionRef() sessioninterfaces.SessionFacade   { return nil }
func (f *fakeModelContext) OffloadMessages(_ string, _ []llmschema.BaseMessage) {}
func (f *fakeModelContext) SaveState() map[string]any                 { return nil }
func (f *fakeModelContext) LoadState(_ map[string]any)                {}
func (f *fakeModelContext) CompressContext(_ context.Context, _ ...ceinterface.CompressContextOption) (*ceinterface.CompressContextResult, error) {
	return nil, errors.New("not implemented")
}

// newQRTestRewriter 构造带 stub 的 QueryRewriter（WithModelClientOption 注入）。
func newQRTestRewriter(t *testing.T, stub *stubQRClient, fmc *fakeModelContext, opts ...QueryRewriterOption) *QueryRewriter {
	t.Helper()
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	assert.NoError(t, err)
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(stub))
	assert.NoError(t, err)
	opts = append([]QueryRewriterOption{WithModelClientOption(model)}, opts...)
	qr, err := NewQueryRewriter(testModelConfig(), fmc, opts...)
	assert.NoError(t, err)
	return qr
}

// testModelConfig 构造测试 ModelConfig（verify_ssl 走 Extra 同生产路径）。
func testModelConfig() *llmschema.ModelConfig {
	mi := llmschema.NewBaseModelInfo("https://mock.test",
		llmschema.WithAPIKey("test-key"),
		llmschema.WithModelNameForInfo("gpt-4"),
		llmschema.WithTemperatureForInfo(0.0),
		llmschema.WithTopPForInfo(0.1),
		llmschema.WithTimeoutForInfo(60),
		llmschema.WithInfoExtra(map[string]any{"verify_ssl": false}),
	)
	return llmschema.NewModelConfig("OpenAI", *mi)
}
```

注意（已核实 2026-10-28）：`MessagesParam.Messages()` 公开访问器存在（messages_param.go L102，返回 `[]llmschema.BaseMessage`）；`WithMessageName`/`GetName` 存在（message.go）；`GetContent()` 返回值 `MessageContent` 有 `IsText()/Text()/Parts()` 三态方法；`resp.Content.Text()` 经 AssistantMessage 嵌入的 DefaultMessage 可达。import 别名按 ceinterface/types.go 实际导入抄写（`token`、`tool`、`sessioninterfaces` 等——实施时以编译器提示为准补齐）。

- [ ] **Step 2: 写 LoadTemplate/Msg2Text 失败测试**

query_rewriter_test.go 追加：

```go
// TestLoadTemplate_加载现有模板（Python test_load_existing_template）
func TestLoadTemplate_加载现有模板(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{})
	content, err := qr.LoadTemplate("intention_completion")
	assert.NoError(t, err)
	assert.True(t, strings.Contains(content, "standalone_query") || strings.Contains(content, "角色"))
}

// TestLoadTemplate_二次调用走缓存（Python test_load_template_cached_on_second_call）
func TestLoadTemplate_二次调用走缓存(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{})
	first, err := qr.LoadTemplate("intention_completion")
	assert.NoError(t, err)
	second, err := qr.LoadTemplate("intention_completion")
	assert.NoError(t, err)
	assert.Equal(t, first, second)
}

// TestLoadTemplate_语言不存在 → 155603（Python test_prompt_not_found_raises）
func TestLoadTemplate_语言不存在(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{}, WithPromptLang("nonexistent_lang"))
	_, err := qr.LoadTemplate("intention_completion")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155603")
}

// TestMsg2Text_显式消息（Python test_msg_2_text_with_messages）
func TestMsg2Text_显式消息(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{})
	messages := []llmschema.BaseMessage{
		llmschema.NewUserMessage("今天天气如何？"),
		llmschema.NewAssistantMessage("晴天。"),
	}
	text, err := qr.Msg2Text(messages)
	assert.NoError(t, err)
	assert.Contains(t, text, "user: 今天天气如何？")
	assert.Contains(t, text, "assistant: 晴天。")
}

// TestMsg2Text_空消息读上下文（Python test_msg_2_text_from_context_when_none）
func TestMsg2Text_空消息读上下文(t *testing.T) {
	fmc := &fakeModelContext{}
	_, err := fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"), llmschema.NewAssistantMessage("你好！"),
	})
	assert.NoError(t, err)
	qr := newQRTestRewriter(t, &stubQRClient{}, fmc)
	text, err := qr.Msg2Text(nil)
	assert.NoError(t, err)
	assert.Contains(t, text, "user: 你好")
	assert.Contains(t, text, "assistant: 你好！")
}
```

- [ ] **Step 3: 运行确认编译失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run "TestLoadTemplate|TestMsg2Text" ./internal/agentcore/retrieval/query_rewriter/...
```

Expected: FAIL（QueryRewriter/NewQueryRewriter 未定义）

- [ ] **Step 4: 写 query_rewriter.go 主体（构造 + LoadTemplate + Msg2Text）**

```go
package query_rewriter

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// QueryRewriter 查询重写器：把多轮对话中的省略/指代 query 改写成
// 独立可检索的 standalone_query；历史达阈值时用 LLM 压缩历史。
//
// 只消费 sessionCtx 的 GetMessages/SetMessages；当前轮消息的追加
// 由调用方负责（rewrite → 检索 → add_messages 顺序）。
//
// Python: query_rewriter.py QueryRewriter
type QueryRewriter struct {
	// modelConfig LLM 配置（model_provider + model_info）
	modelConfig *llmschema.ModelConfig
	// compressRange 历史条数达此值触发一次压缩；亦为 rewrite 提示词
	// 携带的历史消息上限（防无限膨胀）
	compressRange int
	// sessionCtx 当前会话上下文（Python self.context，字段名避让 ctx 参数）
	sessionCtx ceinterface.ModelContext
	// promptLang 提示词语言后缀（zh/en）
	promptLang string
	// llm 内部 LLM 客户端
	llm *llm.Model
	// jsonOutputParser 解析兜底器
	jsonOutputParser *output_parsers.JsonOutputParser
	// compressOutputSchema 压缩输出 schema（theme: list / summary: string）
	compressOutputSchema map[string]schemaFieldType
	// rewriteOutputSchema 改写输出 schema（8 字段）
	rewriteOutputSchema map[string]schemaFieldType
	// templateCache 模板缓存：(base, lang) → 内容
	templateCache map[promptCacheKey]string
	// cacheMu 模板缓存并发保护（Python asyncio 单线程隐式安全的 Go 显式化）
	cacheMu sync.Mutex
}

// promptCacheKey 模板缓存键。
type promptCacheKey struct {
	base string
	lang string
}

// QueryRewriterOption QueryRewriter 构造选项函数。
type QueryRewriterOption func(*QueryRewriter)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultCompressRange 默认压缩阈值（Python 默认参数 20）
	defaultCompressRange = 20
	// defaultPromptLang 默认提示词语言
	defaultPromptLang = "zh"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithCompressRange 设置压缩阈值（构造内 max(n,1) 钳制）。
func WithCompressRange(n int) QueryRewriterOption {
	return func(q *QueryRewriter) { q.compressRange = n }
}

// WithPromptLang 设置提示词语言后缀。
func WithPromptLang(lang string) QueryRewriterOption {
	return func(q *QueryRewriter) { q.promptLang = lang }
}

// WithModelClientOption 注入外部 Model（测试通道；传入则跳过内部构造）。
func WithModelClientOption(m *llm.Model) QueryRewriterOption {
	return func(q *QueryRewriter) { q.llm = m }
}

// NewQueryRewriter 构造查询重写器：绑定 LLM 配置与会话上下文。
//
// 从 cfg.ModelInfo 读取连接信息建 Model：ssl_cert/verify_ssl/custom_headers
// 走 Extra 防御读取（Python getattr + pydantic extra="allow" 同构，
// spec §4.2）；verify_ssl=true 且无证书时强制 false（Python 注释规则）。
//
// Python: query_rewriter.py QueryRewriter.__init__
func NewQueryRewriter(cfg *llmschema.ModelConfig, ctx ceinterface.ModelContext, opts ...QueryRewriterOption) (*QueryRewriter, error) {
	q := &QueryRewriter{
		modelConfig:   cfg,
		compressRange: defaultCompressRange,
		sessionCtx:    ctx,
		promptLang:    defaultPromptLang,
		templateCache: map[promptCacheKey]string{},
		compressOutputSchema: map[string]schemaFieldType{
			"theme":   schemaFieldList,
			"summary": schemaFieldString,
		},
		rewriteOutputSchema: map[string]schemaFieldType{
			"before":           schemaFieldString,
			"intention":        schemaFieldString,
			"standalone_query": schemaFieldString,
			"references":       schemaFieldDict,
			"missing":          schemaFieldList,
			"typo":             schemaFieldList,
			"gibberish":        schemaFieldList,
			"from_history":     schemaFieldString,
		},
	}
	for _, opt := range opts {
		opt(q)
	}
	if q.compressRange < 1 {
		q.compressRange = 1
	}
	if q.promptLang == "" {
		q.promptLang = defaultPromptLang
	}
	q.jsonOutputParser = output_parsers.NewJsonOutputParser()

	if q.llm != nil {
		return q, nil
	}

	mi := cfg.ModelInfo
	sslCert := ""
	if v, ok := mi.Extra["ssl_cert"].(string); ok {
		sslCert = v
	}
	verifySSL := true
	if v, ok := mi.Extra["verify_ssl"].(bool); ok {
		verifySSL = v
	}
	// BaseModelClient 要求 verify_ssl=true 时必须配证书；无证书强制关闭
	if verifySSL && sslCert == "" {
		verifySSL = false
	}
	var customHeaders map[string]string
	if v, ok := mi.Extra["custom_headers"].(map[string]string); ok {
		customHeaders = v
	}

	clientOpts := []llmschema.ModelClientConfigOption{
		llmschema.WithTimeout(float64(mi.Timeout)),
		llmschema.WithVerifySSL(verifySSL),
		llmschema.WithCustomHeaders(customHeaders),
	}
	if sslCert != "" {
		clientOpts = append(clientOpts, llmschema.WithSSLCert(sslCert))
	}
	clientCfg, err := llmschema.NewModelClientConfig(cfg.ModelProvider, mi.APIKey, mi.APIBase, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("创建 ModelClientConfig 失败: %w", err)
	}
	requestCfg := llmschema.NewModelRequestConfig(
		llmschema.WithModelName(mi.ModelName),
		llmschema.WithTemperature(0.0),
		llmschema.WithTopP(mi.TopP),
	)
	model, err := llm.NewModel(clientCfg, requestCfg)
	if err != nil {
		return nil, fmt.Errorf("创建 Model 失败: %w", err)
	}
	q.llm = model
	return q, nil
}

// LoadTemplate 加载提示词模板（语言后缀路由 + 缓存）。
//
// Python: query_rewriter.py QueryRewriter.load_template
func (q *QueryRewriter) LoadTemplate(promptBase string) (string, error) {
	key := promptCacheKey{base: promptBase, lang: q.promptLang}
	q.cacheMu.Lock()
	defer q.cacheMu.Unlock()
	if cached, ok := q.templateCache[key]; ok {
		return cached, nil
	}
	name := "prompts/" + promptBase + "_" + q.promptLang + ".md"
	content, err := fs.ReadFile(promptsFS, name)
	if err != nil {
		return "", exception.BuildError(
			exception.StatusRetrievalQueryRewriterPromptNotFound,
			exception.WithParam("error_msg", "prompt file not found: "+name),
		)
	}
	q.templateCache[key] = string(content)
	return string(content), nil
}

// Msg2Text 格式化消息列表为 "role: content" 纯文本。
//
// messages 为 nil 时读全量上下文历史；多模态 content 走 JSON 序列化
// （Python json.dumps ensure_ascii=False）。Python _extract_json。
func (q *QueryRewriter) Msg2Text(messages []llmschema.BaseMessage) (string, error) {
	if messages == nil {
		var err error
		messages, err = q.sessionCtx.GetMessages(0, true)
		if err != nil {
			return "", err
		}
	}
	lines := make([]string, 0, len(messages))
	for _, m := range messages {
		content := m.GetContent()
		var text string
		if content.IsText() {
			text = content.Text()
		} else {
			data, err := json.Marshal(content.Parts())
			if err != nil {
				return "", fmt.Errorf("序列化多模态 content 失败: %w", err)
			}
			text = string(data)
		}
		lines = append(lines, m.GetRole().String()+": "+text)
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}
```

（`model_clients` import 在本 Task 暂未用到可先省略，Task 5 补 Compress 时再加。）

- [ ] **Step 5: 运行测试通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run "TestLoadTemplate|TestMsg2Text" ./internal/agentcore/retrieval/query_rewriter/... -v
```

Expected: 5 个测试全 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/retrieval/query_rewriter/query_rewriter.go internal/agentcore/retrieval/query_rewriter/query_rewriter_test.go
git commit -m "feat(13.24): QueryRewriter 结构体 + 构造(Extra 防御读取) + LoadTemplate/Msg2Text + 测试基建"
```

---

### Task 5: Compress + 测试

**Files:**
- Modify: `internal/agentcore/retrieval/query_rewriter/query_rewriter.go`
- Modify: `internal/agentcore/retrieval/query_rewriter/query_rewriter_test.go`

- [ ] **Step 1: 写失败测试**

query_rewriter_test.go 追加：

```go
// makeCompressResponse 构造合法压缩响应（Python _make_compress_response）。
func makeCompressResponse() string {
	return `{"theme": ["主题"], "summary": "摘要内容"}`
}

// makeFullRewriteResponse 构造合法改写响应（Python _make_full_rewrite_response）。
func makeFullRewriteResponse(standaloneQuery string) string {
	return fmt.Sprintf(`{
		"before": %q, "intention": "用户咨询", "standalone_query": %q,
		"references": {}, "missing": [], "typo": [], "gibberish": [],
		"from_history": ""}`, standaloneQuery, standaloneQuery)
}

// TestCompress_合法响应（Python test_compress_valid_mock）
func TestCompress_合法响应(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("用户问"), llmschema.NewAssistantMessage("助手答"),
	})
	qr := newQRTestRewriter(t, &stubQRClient{compressResp: makeCompressResponse()}, fmc, WithCompressRange(5))
	result, err := qr.Compress(context.Background(), nil) // nil → 读上下文（Python get_messages）
	assert.NoError(t, err)
	assert.Contains(t, result, "theme")
	assert.Contains(t, result, "summary")
}

// TestCompress_非法JSON → 155604（Python test_compress_invalid_json_raises）
func TestCompress_非法JSON(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("用户问"), llmschema.NewAssistantMessage("助手答"),
	})
	qr := newQRTestRewriter(t, &stubQRClient{compressResp: "not valid json at all"}, fmc, WithCompressRange(5))
	_, err := qr.Compress(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155604")
}

// TestCompress_invoke失败 → 155605（Python test_compress_llm_invoke_failure_raises）
func TestCompress_invoke失败(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("用户问"), llmschema.NewAssistantMessage("助手答"),
	})
	qr := newQRTestRewriter(t, &stubQRClient{invokeErr: errors.New("network error")}, fmc, WithCompressRange(5))
	_, err := qr.Compress(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155605")
}
```

注意：`Compress(ctx, nil)` 传 nil 时 Python compress 里 `msg_2_text(raw)` 收到的是调用方传入的 messages（Rewrite 内部传 historyFull），直接调 Compress 传 None 也走 context 读取（Python msg_2_text(None) 分支）——Go 保持 `raw == nil → Msg2Text(nil)` 同链路。

- [ ] **Step 2: 运行确认编译失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run "TestCompress" ./internal/agentcore/retrieval/query_rewriter/...
```

Expected: FAIL（Compress 未定义）

- [ ] **Step 3: 实现 Compress**

query_rewriter.go 追加（导出函数区）：

```go
// Compress 用 LLM 把一段历史消息压缩成结构化摘要（theme + summary）。
//
// 解析链：extractJSON → parseLLMJSON → 兜底 jsonOutputParser →
// schemaRepair；失败统一 155604，invoke 失败 155605。
//
// Python: query_rewriter.py QueryRewriter.compress
func (q *QueryRewriter) Compress(ctx context.Context, raw []llmschema.BaseMessage) (map[string]any, error) {
	rawText, err := q.Msg2Text(raw)
	if err != nil {
		return nil, err
	}
	template, err := q.LoadTemplate("compression")
	if err != nil {
		return nil, err
	}
	compressionPrompt := fillTemplate(template, map[string]string{"history": rawText})

	resp, err := q.llm.Invoke(ctx,
		model_clients.NewMessagesParam(llmschema.NewSystemMessage(compressionPrompt)),
		model_clients.WithInvokeTemperature(0.0))
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalQueryRewriterLlmInvokeFailed,
			exception.WithParam("error_msg", err.Error()),
			exception.WithCause(err),
		)
	}
	var contentStr string
	if resp != nil {
		contentStr = strings.TrimSpace(resp.Content.Text())
	}
	contentJSON := extractJSON(contentStr)

	parsed, ok := parseLLMJSON(contentJSON)
	if !ok {
		// 兜底：JsonOutputParser（markdown 代码块提取 + 直接解析）
		fallback, perr := q.jsonOutputParser.Parse(contentJSON)
		if perr == nil {
			if m, isMap := fallback.(map[string]any); isMap {
				parsed, ok = m, true
			}
		}
	}
	if !ok {
		return nil, exception.BuildError(
			exception.StatusRetrievalQueryRewriterOutputInvalid,
			exception.WithParam("error_msg", "LLM compress output is not valid JSON (parse returned non-dict); content: "+truncateForMsg(contentJSON, 500)),
		)
	}
	repaired, err := schemaRepair(parsed, q.compressOutputSchema)
	if err != nil {
		return nil, err
	}
	logger.Info(logComponent).
		Str("theme", fmt.Sprint(repaired["theme"])).
		Str("summary", fmt.Sprint(repaired["summary"])).
		Msg("Compress completed")
	return repaired, nil
}

// truncateForMsg 截取前 n 字节用于错误消息（Python content[:500]）。
func truncateForMsg(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
```

- [ ] **Step 4: 运行测试通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run "TestCompress" ./internal/agentcore/retrieval/query_rewriter/... -v
```

Expected: 3 个测试全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/query_rewriter/query_rewriter.go internal/agentcore/retrieval/query_rewriter/query_rewriter_test.go
git commit -m "feat(13.24): Compress 压缩链（extractJSON→parseLLMJSON→parser 兜底→schemaRepair）+ 测试"
```

---

### Task 6: Rewrite（含压缩降级链）+ 测试

**Files:**
- Modify: `internal/agentcore/retrieval/query_rewriter/query_rewriter.go`
- Modify: `internal/agentcore/retrieval/query_rewriter/query_rewriter_test.go`

- [ ] **Step 1: 写失败测试**

query_rewriter_test.go 追加：

```go
// TestRewrite_合法响应（Python test_rewrite_valid_mock）
func TestRewrite_合法响应(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"), llmschema.NewAssistantMessage("你好！"),
	})
	currentQuery := "那运费呢？"
	qr := newQRTestRewriter(t, &stubQRClient{rewriteResp: makeFullRewriteResponse(currentQuery)}, fmc, WithCompressRange(5))
	result, err := qr.Rewrite(context.Background(), currentQuery)
	assert.NoError(t, err)
	assert.Equal(t, currentQuery, result["standalone_query"])
	assert.Contains(t, result, "before")
	assert.Contains(t, result, "intention")
}

// TestRewrite_前后缀包裹JSON（Python test_rewrite_with_json_prefix_suffix）
func TestRewrite_前后缀包裹JSON(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"), llmschema.NewAssistantMessage("你好！"),
	})
	qr := newQRTestRewriter(t, &stubQRClient{rewriteResp: "这是回答：\n" + makeFullRewriteResponse("测试") + "\n以上是结果。"}, fmc, WithCompressRange(5))
	result, err := qr.Rewrite(context.Background(), "测试")
	assert.NoError(t, err)
	assert.Equal(t, "测试", result["standalone_query"])
}

// TestRewrite_非法输出 → 155604（Python test_rewrite_invalid_output_raises）
func TestRewrite_非法输出(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"), llmschema.NewAssistantMessage("你好！"),
	})
	qr := newQRTestRewriter(t, &stubQRClient{rewriteResp: "not json"}, fmc, WithCompressRange(5))
	_, err := qr.Rewrite(context.Background(), "问题")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155604")
}

// TestRewrite_空查询 → 155606（Python test_rewrite_invalid_input_empty_raises）
func TestRewrite_空查询(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{}, WithCompressRange(5))
	_, err := qr.Rewrite(context.Background(), "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155606")
}

// TestRewrite_纯空白查询 → 155606（Python test_rewrite_invalid_input_whitespace_raises）
func TestRewrite_纯空白查询(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{}, WithCompressRange(5))
	_, err := qr.Rewrite(context.Background(), "   ")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155606")
}

// TestRewrite_压缩失败降级原文（Python test_rewrite_compress_failure_fallback）
func TestRewrite_压缩失败降级原文(t *testing.T) {
	fmc := &fakeModelContext{}
	for i := 0; i < 3; i++ {
		_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
			llmschema.NewUserMessage(fmt.Sprintf("用户问%d", i)),
			llmschema.NewAssistantMessage(fmt.Sprintf("助手答%d", i)),
		})
	}
	// compress 响应是坏 JSON（触发 Compress 内部 155604 → Rewrite 降级）
	stub := &stubQRClient{compressResp: "broken", rewriteResp: makeFullRewriteResponse("总结一下")}
	qr := newQRTestRewriter(t, stub, fmc, WithCompressRange(5))
	result, err := qr.Rewrite(context.Background(), "总结一下")
	assert.NoError(t, err)
	assert.Equal(t, "总结一下", result["standalone_query"])
	// 降级后历史被替换为一条 system 消息（name=original_history）
	msgs, _ := fmc.GetMessages(0, true)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "system", msgs[0].GetRole().String())
	assert.Equal(t, "original_history", msgs[0].GetName())
	assert.Contains(t, msgs[0].GetContent().Text(), "user: 用户问0")
	// rewrite 只被调用一次
	assert.Equal(t, 1, stub.rewriteCnt)
}

// TestRewrite_尾逗号JSON修复（Python test_rewrite_with_trailing_comma_mock）
func TestRewrite_尾逗号JSON修复(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"), llmschema.NewAssistantMessage("你好！"),
	})
	broken := `{"before":"x","intention":"y","standalone_query":"x","references":{},"missing":[],"typo":[],"gibberish":[],"from_history":"",}`
	qr := newQRTestRewriter(t, &stubQRClient{rewriteResp: broken}, fmc, WithCompressRange(5))
	result, err := qr.Rewrite(context.Background(), "x")
	assert.NoError(t, err)
	assert.Equal(t, "x", result["standalone_query"])
}
```

- [ ] **Step 2: 运行确认编译失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run "TestRewrite" ./internal/agentcore/retrieval/query_rewriter/...
```

Expected: FAIL（Rewrite 未定义）

- [ ] **Step 3: 实现 Rewrite**

query_rewriter.go 追加（导出函数区，Compress 之后）：

```go
// Rewrite 在当前会话上下文里把用户 query 改写为独立可检索的
// standalone_query；历史达 compressRange 时先触发一次压缩（失败降级
// 为原文写回），rewrite 提示词只携带最近 compressRange 条历史。
//
// Python: query_rewriter.py QueryRewriter.rewrite
func (q *QueryRewriter) Rewrite(ctx context.Context, query string) (map[string]any, error) {
	if strings.TrimSpace(query) == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalQueryRewriterInputInvalid,
			exception.WithParam("error_msg", "query must be a non-empty string"),
		)
	}
	promptTemplate, err := q.LoadTemplate("intention_completion")
	if err != nil {
		return nil, err
	}

	// 全量历史判断是否触发压缩
	historyFull, err := q.sessionCtx.GetMessages(0, true)
	if err != nil {
		return nil, err
	}
	if len(historyFull) >= q.compressRange {
		compressed, cerr := q.Compress(ctx, historyFull)
		if cerr == nil {
			historyText, merr := json.Marshal(compressed)
			if merr == nil {
				q.sessionCtx.SetMessages([]llmschema.BaseMessage{
					llmschema.NewSystemMessage(string(historyText), llmschema.WithMessageName("compressed_history")),
				}, true)
			}
		} else {
			// 压缩失败降级：原文写回（Python except BaseError 分支）
			logger.Warn(logComponent).
				Err(cerr).
				Msg("Query rewriter compress failed, falling back to original history")
			historyText, _ := q.Msg2Text(historyFull)
			q.sessionCtx.SetMessages([]llmschema.BaseMessage{
				llmschema.NewSystemMessage(historyText, llmschema.WithMessageName("original_history")),
			}, true)
		}
	}

	// 只取最近 compressRange 条防提示词无限膨胀
	historyForRewrite, err := q.sessionCtx.GetMessages(q.compressRange, true)
	if err != nil {
		return nil, err
	}
	historyText, err := q.Msg2Text(historyForRewrite)
	if err != nil {
		return nil, err
	}
	completionPrompt := fillTemplate(promptTemplate, map[string]string{
		"history": historyText,
		"query":   query,
	})

	resp, err := q.llm.Invoke(ctx,
		model_clients.NewMessagesParam(llmschema.NewSystemMessage(completionPrompt)),
		model_clients.WithInvokeTemperature(0.0))
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalQueryRewriterLlmInvokeFailed,
			exception.WithParam("error_msg", err.Error()),
			exception.WithCause(err),
		)
	}
	var contentStr string
	if resp != nil {
		contentStr = strings.TrimSpace(resp.Content.Text())
	}
	contentJSON := extractJSON(contentStr)

	parsed, ok := parseLLMJSON(contentJSON)
	if !ok {
		fallback, perr := q.jsonOutputParser.Parse(contentJSON)
		if perr == nil {
			if m, isMap := fallback.(map[string]any); isMap {
				parsed, ok = m, true
			}
		}
	}
	if !ok {
		return nil, exception.BuildError(
			exception.StatusRetrievalQueryRewriterOutputInvalid,
			exception.WithParam("error_msg", "LLM rewrite output is not valid JSON (parse returned non-dict); content: "+truncateForMsg(contentJSON, 500)),
		)
	}
	return schemaRepair(parsed, q.rewriteOutputSchema)
}
```

- [ ] **Step 4: 运行测试通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -run "TestRewrite" ./internal/agentcore/retrieval/query_rewriter/... -v
```

Expected: 8 个测试全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/query_rewriter/query_rewriter.go internal/agentcore/retrieval/query_rewriter/query_rewriter_test.go
git commit -m "feat(13.24): Rewrite 主链 + 压缩降级（compressed_history/original_history 双通道）+ 测试"
```

---

### Task 7: 完整对话流 + Go 增强 + 构造路径测试 + 覆盖率核验

**Files:**
- Modify: `internal/agentcore/retrieval/query_rewriter/query_rewriter_test.go`

- [ ] **Step 1: 写完整对话流测试（Python TestFullConversationWithCompressAndRewrite）**

```go
// longConversation Python LONG_CONVERSATION 六轮语料。
var longConversation = [][2]string{
	{"你们这个淘美乐 App 是干什么的？", "淘美乐是一款综合购物与生活服务的 App。"},
	{"怎么注册和登录？", "使用手机号验证码或第三方账号登录。"},
	{"我想买点日用品，从哪里进？", "首页有「日百」等入口。"},
	{"搜索出来的结果太多，怎么筛选？", "搜索结果页有「筛选」按钮。"},
	{"下单后多久能送到？", "一般 1～3 天送达。"},
	{"可以修改订单吗？", "待发货状态下可以修改或取消。"},
}

// TestRewrite_完整对话流 6 轮 × compressRange=5（Python test_full_conversation_with_compress_and_rewrite）
func TestRewrite_完整对话流(t *testing.T) {
	fmc := &fakeModelContext{}
	stub := &stubQRClient{
		compressResp: makeCompressResponse(),
		rewriteResp:  makeFullRewriteResponse("占位"),
	}
	qr := newQRTestRewriter(t, stub, fmc, WithCompressRange(5))

	rewriteAfterTurns := map[int]string{2: "那运费呢？", 4: "会员怎么升级？", 6: "生鲜能退吗？"}
	for turnIdx, turn := range longConversation {
		_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
			llmschema.NewUserMessage(turn[0]), llmschema.NewAssistantMessage(turn[1]),
		})
		if q, ok := rewriteAfterTurns[turnIdx+1]; ok {
			stub.mu.Lock()
			stub.rewriteResp = makeFullRewriteResponse(q)
			stub.mu.Unlock()
			result, err := qr.Rewrite(context.Background(), q)
			assert.NoError(t, err)
			assert.Contains(t, result, "standalone_query")
			assert.Equal(t, q, result["standalone_query"])
		}
	}
	assert.Equal(t, 3, stub.rewriteCnt)
	assert.GreaterOrEqual(t, stub.compressCnt, 0) // compress 调用次数 ≥ 0（Python 同款宽松断言）
}
```

- [ ] **Step 2: 写 Go 增强测试**

```go
// TestLoadTemplate_并发竞态 Go 增强：并发加载同模板不炸不脏。
func TestLoadTemplate_并发竞态(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			content, err := qr.LoadTemplate("compression")
			assert.NoError(t, err)
			assert.True(t, len(content) > 0)
		}()
	}
	wg.Wait()
}

// TestNewQueryRewriter_配置透传 custom_headers 经 Extra（Python test_init_passes_only_custom_headers）
func TestNewQueryRewriter_配置透传(t *testing.T) {
	mi := llmschema.NewBaseModelInfo("https://mock.test",
		llmschema.WithAPIKey("test-key"),
		llmschema.WithModelNameForInfo("gpt-4"),
		llmschema.WithTimeoutForInfo(60),
		llmschema.WithInfoExtra(map[string]any{
			"custom_headers": map[string]string{"X-Tenant": "tenant-a"},
			"verify_ssl":     false,
		}),
	)
	qr, err := NewQueryRewriter(llmschema.NewModelConfig("OpenAI", *mi), &fakeModelContext{}, WithCompressRange(5))
	assert.NoError(t, err)
	assert.Equal(t, map[string]string{"X-Tenant": "tenant-a"}, qr.llm.ClientConfig.CustomHeaders)
}

// TestNewQueryRewriter_verifySSL无证书强制关闭 Go 增强：Extra 边界。
func TestNewQueryRewriter_verifySSL无证书强制关闭(t *testing.T) {
	mi := llmschema.NewBaseModelInfo("https://mock.test",
		llmschema.WithAPIKey("test-key"),
		llmschema.WithModelNameForInfo("gpt-4"),
		llmschema.WithInfoExtra(map[string]any{"verify_ssl": true}), // 无 ssl_cert
	)
	qr, err := NewQueryRewriter(llmschema.NewModelConfig("OpenAI", *mi), &fakeModelContext{})
	assert.NoError(t, err)
	assert.False(t, qr.llm.ClientConfig.VerifySSL)
}

// TestNewQueryRewriter_verifySSL关闭有证书透传 Go 增强：Extra 边界（spec §6.3）。
func TestNewQueryRewriter_verifySSL关闭有证书透传(t *testing.T) {
	mi := llmschema.NewBaseModelInfo("https://mock.test",
		llmschema.WithAPIKey("test-key"),
		llmschema.WithModelNameForInfo("gpt-4"),
		llmschema.WithInfoExtra(map[string]any{"verify_ssl": false, "ssl_cert": "/tmp/ca.pem"}),
	)
	qr, err := NewQueryRewriter(llmschema.NewModelConfig("OpenAI", *mi), &fakeModelContext{})
	assert.NoError(t, err)
	assert.False(t, qr.llm.ClientConfig.VerifySSL)
	assert.Equal(t, "/tmp/ca.pem", qr.llm.ClientConfig.SSLCert)
}

// TestNewQueryRewriter_非法provider Go 增强：错误透传。
func TestNewQueryRewriter_非法provider(t *testing.T) {
	mi := llmschema.NewBaseModelInfo("https://mock.test",
		llmschema.WithAPIKey("test-key"),
		llmschema.WithModelNameForInfo("gpt-4"),
	)
	_, err := NewQueryRewriter(llmschema.NewModelConfig("NotAProvider", *mi), &fakeModelContext{})
	assert.Error(t, err)
}

// TestNewQueryRewriter_注入通道 Go 增强：WithModelClientOption 跳过内部构造。
func TestNewQueryRewriter_注入通道(t *testing.T) {
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	assert.NoError(t, err)
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(&stubQRClient{}))
	assert.NoError(t, err)
	// 非法 cfg（provider 错误）本应报错，但注入通道跳过构造 → 不报错
	mi := llmschema.NewBaseModelInfo("https://mock.test", llmschema.WithAPIKey("k"), llmschema.WithModelNameForInfo("m"))
	qr, err := NewQueryRewriter(llmschema.NewModelConfig("NotAProvider", *mi), &fakeModelContext{}, WithModelClientOption(model))
	assert.NoError(t, err)
	assert.Equal(t, model, qr.llm)
}

// TestRewrite_英文模板 Go 增强：prompt_lang=en 路由。
func TestRewrite_英文模板(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("hello"), llmschema.NewAssistantMessage("hi!"),
	})
	qr := newQRTestRewriter(t, &stubQRClient{rewriteResp: makeFullRewriteResponse("test")}, fmc,
		WithPromptLang("en"), WithCompressRange(5))
	result, err := qr.Rewrite(context.Background(), "test")
	assert.NoError(t, err)
	assert.Equal(t, "test", result["standalone_query"])
}

// TestCompress_触发压缩写回上下文 Go 增强：compressed_history 消息形态。
func TestCompress_触发压缩写回上下文(t *testing.T) {
	fmc := &fakeModelContext{}
	for i := 0; i < 3; i++ {
		_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
			llmschema.NewUserMessage(fmt.Sprintf("q%d", i)), llmschema.NewAssistantMessage(fmt.Sprintf("a%d", i)),
		})
	}
	stub := &stubQRClient{compressResp: makeCompressResponse(), rewriteResp: makeFullRewriteResponse("x")}
	qr := newQRTestRewriter(t, stub, fmc, WithCompressRange(5))
	_, err := qr.Rewrite(context.Background(), "x")
	assert.NoError(t, err)
	msgs, _ := fmc.GetMessages(0, true)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "system", msgs[0].GetRole().String())
	assert.Equal(t, "compressed_history", msgs[0].GetName())
	assert.Contains(t, msgs[0].GetContent().Text(), `"theme"`)
}
```

- [ ] **Step 3: 全量运行 + 覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/query_rewriter/... -cover -v 2>&1 | tail -20
```

Expected: 全 PASS，覆盖率 ≥ 90%（spec §6.4）。若不足，按 `go test -coverprofile` 输出补分支用例（Msg2Text 多模态 content、GetMessages err 路径等）。

- [ ] **Step 4: -race 竞态检查**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/agentcore/retrieval/query_rewriter/... 
```

Expected: 全 PASS 无 DATA RACE

- [ ] **Step 5: 全仓回归（防外溢）**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/... ./internal/agentcore/context_engine/... 2>&1 | tail -8
```

Expected: 全 PASS（新包是叶子，不应影响任何既有包）

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/retrieval/query_rewriter/query_rewriter_test.go
git commit -m "test(13.24): 完整对话流 + Go 增强（并发/Extra 边界/注入通道/英文模板）+ 覆盖率达标"
```

---

### Task 8: 实施后审查 + IMPLEMENTATION_PLAN.md 回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（13.24 行状态 + 领域13 头部记录）
- Create: 审查结论（对话内输出，偏差入 spec §8 或维持归档）

- [ ] **Step 1: 对齐审查（逐项核 Python 源码）**

对照 `/home/opensource/agent-core/openjiuwen/core/retrieval/query_rewriter/query_rewriter.py` 逐行核：

1. `__init__`：compress_range 钳制 / prompt_lang 回落 / ssl 三字段 Extra 读取 / verify_ssl 无证书强制 false / temperature=0.0 + top_p 透传
2. `load_template`：缓存键 (base, lang) / 文件名拼接 / 155603
3. `msg_2_text`：nil 读上下文 / role: content 格式 / 多模态 JSON / join+strip
4. `compress`：模板填充 / Invoke temperature / 155605 / 解析链三层 / schemaRepair / Info 日志
5. `rewrite`：155606 输入校验 / 压缩触发条件 >= / 成功写 compressed_history / 失败降级 original_history / GetMessages(size) 截断 / 解析链 / 返回 8 字段
6. 模块级 7 函数逐个核（含 typo 特判三键收敛）
7. 日志 7 条：位置/级别/Msg/字段
8. spec §8 偏差表 8 条逐条核对实现是否守约

- [ ] **Step 2: 发现偏差时的处置**

- 实现与 Python 行为不一致且 spec 未归档 → 修复代码（优先）或 spec 偏差表补条目（需说明理由）
- 记忆规则：审查文档全修复才闭环

- [ ] **Step 3: 回填 IMPLEMENTATION_PLAN.md**

13.24 行状态 ☐ → ✅，产出列写入实现概要（含偏差要点），头部追加一行日期记录（**保留既有 2026-10-27/2026-10-28 各行不删**）：

```
> 2026-10-28：13.24 QueryRewriter 完成（……概要……）。设计：docs/superpowers/specs/2026-10-28-queryrewriter-13.24-design.md
```

领域13 验证点行第二项保持 ☐（13.25-13.28 未完成）。

- [ ] **Step 4: 提交回填**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(13.24): 计划状态 ✅ 回填（覆盖率 xx%）"
```

- [ ] **Step 5: 推送**

```bash
git push
```
