# LLM 客户端 C 组对齐实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落实 `docs/superpowers/specs/2026-11-06-llm-client-c-group-alignment-design.md` 的 7 批次（C-A~C-G），修复 C 组 11 个已核验缺陷并删除 6 类残留。

**Architecture:** 全部改动集中在 `internal/agentcore/foundation/llm`（schema / model_clients 两个子树）+ 3 个外围接线点（context_engine/usage、web 保存侧、intellirouter）。共享 Transport 池与 usage 收敛是两个结构性新文件，其余为逐函数对齐。

**Tech Stack:** Go 1.22 标准库（net/http / crypto/tls / encoding/json / unicode/utf8）；无新增第三方依赖。

**上游对照源：** `/home/opensource/agent-core/openjiuwen/core/foundation/llm/`（无 git 历史，按 HEAD 源码逐函数比对）。每个任务的「Python:」注释都要写清对应符号与行号。

**执行约定：**

- 每任务一个提交；提交前 `go build ./... && go vet ./internal/agentcore/foundation/llm/...`；
- 编译互斥（规则 5）：起编译前 `pgrep -f 'go (build|test)'`，有残留先 kill；
- 单进程串行：`go test ./internal/agentcore/foundation/llm/... -run <TestName> -v`（先红后绿，TDD）；
- 最终统一验收：全量 `-race` + 远端 CI 四 job（规则 6）。

---

## Task 1: C-B1 — `usage_extract.go` 四个抽取函数（TDD）

**Files:**
- Create: `internal/agentcore/foundation/llm/model_clients/usage_extract.go`
- Create: `internal/agentcore/foundation/llm/model_clients/usage_extract_test.go`

- [ ] **Step 1: 写失败测试（路径矩阵）**

```go
package model_clients

import "testing"

// 表格驱动：上游 _extract_cache_tokens 的 7 条嵌套路径 + 11 个平铺字段
func TestExtractCacheTokens_全路径矩阵(t *testing.T) {
	nested := []struct {
		name string
		path map[string]any
		want int
	}{
		{"prompt_tokens_details.cached_tokens", map[string]any{"prompt_tokens_details": map[string]any{"cached_tokens": 42}}, 42},
		{"promptTokensDetails.cachedTokens", map[string]any{"promptTokensDetails": map[string]any{"cachedTokens": 7}}, 7},
		{"input_tokens_details.cached_tokens", map[string]any{"input_tokens_details": map[string]any{"cached_tokens": 9}}, 9},
		{"input_token_details.cached_tokens", map[string]any{"input_token_details": map[string]any{"cached_tokens": 3}}, 3},
		{"inputTokensDetails.cachedTokens", map[string]any{"inputTokensDetails": map[string]any{"cachedTokens": 5}}, 5},
		{"inputTokenDetails.cachedTokens", map[string]any{"inputTokenDetails": map[string]any{"cachedTokens": 6}}, 6},
		{"usageMetadata.cachedContentTokenCount", map[string]any{"usageMetadata": map[string]any{"cachedContentTokenCount": 8}}, 8},
	}
	for _, tc := range nested {
		if got := ExtractCacheTokens(tc.path); got != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}

	flat := []struct {
		key  string
		want int
	}{
		{"prompt_cache_hit_tokens", 11}, {"cache_read_input_tokens", 12},
		{"cachedContentTokenCount", 13}, {"cached_content_token_count", 14},
		{"cache_tokens", 15}, {"cached_tokens", 16}, {"cache_hit_tokens", 17},
		{"cached_input_tokens", 18}, {"cache_read_tokens", 19},
		{"prompt_cache_tokens", 20}, {"prompt_cached_tokens", 21},
	}
	for _, tc := range flat {
		if got := ExtractCacheTokens(map[string]any{tc.key: tc.want}); got != tc.want {
			t.Errorf("flat %s: got %d want %d", tc.key, got, tc.want)
		}
	}

	// 非法值形态：bool/非数字/负数 → 0（对齐 _to_int）
	for _, v := range []any{true, "abc", -1.5, nil, []any{}} {
		if got := ExtractCacheTokens(map[string]any{"cache_tokens": v}); got != 0 {
			t.Errorf("invalid %v: got %d want 0", v, got)
		}
	}
	// 嵌套优先于平铺（先路径后字段）
	if got := ExtractCacheTokens(map[string]any{
		"prompt_tokens_details": map[string]any{"cached_tokens": 5}, "cache_tokens": 99,
	}); got != 5 {
		t.Errorf("nested precedence: got %d want 5", got)
	}
	// 全缺 → 0
	if got := ExtractCacheTokens(map[string]any{}); got != 0 {
		t.Errorf("empty: got %d want 0", got)
	}
}

// 上游 _cache_usage_metadata：read/miss/write 归一 + status/source/authoritative
func TestCacheUsageMetadata_归一(t *testing.T) {
	m := CacheUsageMetadata(map[string]any{
		"prompt_tokens_details": map[string]any{"cached_tokens": 30},
		"prompt_cache_miss_tokens": 10,
		"cache_creation_input_tokens": 4,
	})
	if m["cache_read_tokens"] != 30 || m["cache_miss_tokens"] != 10 || m["cache_write_tokens"] != 4 {
		t.Errorf("read/miss/write: %v", m)
	}
	if m["cache_status"] != "observed" || m["cache_source"] != "provider_usage" {
		t.Errorf("status/source: %v", m)
	}
	if m["cache_authoritative"] != true {
		t.Errorf("authoritative: %v", m)
	}

	// legacy：无 read 信号时回落 cache_tokens 且 authoritative=false
	legacy := CacheUsageMetadata(map[string]any{"cache_tokens": 30})
	if legacy["cache_read_tokens"] != 30 || legacy["cache_authoritative"] != false {
		t.Errorf("legacy: %v", legacy)
	}
	// 全缺 → 全 nil
	empty := CacheUsageMetadata(map[string]any{})
	if empty["cache_read_tokens"] != nil || empty["cache_status"] != nil {
		t.Errorf("empty: %v", empty)
	}
}

// 上游 _extract_cache_creation_tokens：3 嵌套 + 4 平铺
func TestExtractCacheCreationTokens_矩阵(t *testing.T) {
	if got := ExtractCacheCreationTokens(map[string]any{
		"input_tokens_details": map[string]any{"cache_creation_tokens": 12}}); got == nil || *got != 12 {
		t.Errorf("nested: %v", got)
	}
	for _, key := range []string{"cache_creation_input_tokens", "cache_write_input_tokens", "cache_creation_tokens", "cache_write_tokens"} {
		if got := ExtractCacheCreationTokens(map[string]any{key: 8}); got == nil || *got != 8 {
			t.Errorf("flat %s: %v", key, got)
		}
	}
	if got := ExtractCacheCreationTokens(map[string]any{}); got != nil {
		t.Errorf("absent: %v", got)
	}
}

// 上游 _extract_reasoning_tokens：completion_tokens_details.reasoning_tokens 等路径
func TestExtractReasoningTokens_矩阵(t *testing.T) {
	if got := ExtractReasoningTokens(map[string]any{
		"completion_tokens_details": map[string]any{"reasoning_tokens": 900}}); got != 900 {
		t.Errorf("nested: %d", got)
	}
	if got := ExtractReasoningTokens(map[string]any{"reasoning_tokens": 77}); got != 77 {
		t.Errorf("flat: %d", got)
	}
	if got := ExtractReasoningTokens(map[string]any{}); got != 0 {
		t.Errorf("empty: %d", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/llm/model_clients/ -run 'TestExtract|TestCacheUsage' -v`
Expected: FAIL（`undefined: ExtractCacheTokens` 等编译错误）

- [ ] **Step 3: 实现 usage_extract.go**

```go
package model_clients

import "strconv"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractCacheTokens 从 provider usage 元数据提取提示词缓存命中 token 数。
//
// 各 provider 字段形态不一致：只统计缓存命中/读 token；缓存写/创建与 miss 有意忽略。
//
// Python: base_model_client.py:86 _extract_cache_tokens（7 条嵌套路径 + 11 个平铺字段）
func ExtractCacheTokens(obj map[string]any) int {
	cacheTokenPaths := [][2]string{
		{"prompt_tokens_details", "cached_tokens"},
		{"promptTokensDetails", "cachedTokens"},
		{"input_tokens_details", "cached_tokens"},
		{"input_token_details", "cached_tokens"},
		{"inputTokensDetails", "cachedTokens"},
		{"inputTokenDetails", "cachedTokens"},
		{"usageMetadata", "cachedContentTokenCount"},
	}
	for _, path := range cacheTokenPaths {
		if nested, ok := obj[path[0]].(map[string]any); ok {
			if v := toIntNonNegative(nested[path[1]]); v > 0 {
				return v
			}
		}
	}
	cacheTokenFields := []string{
		"prompt_cache_hit_tokens", "cache_read_input_tokens", "cachedContentTokenCount",
		"cached_content_token_count", "cache_tokens", "cached_tokens", "cache_hit_tokens",
		"cached_input_tokens", "cache_read_tokens", "prompt_cache_tokens", "prompt_cached_tokens",
	}
	for _, field := range cacheTokenFields {
		if v := toIntNonNegative(obj[field]); v > 0 {
			return v
		}
	}
	return 0
}

// CacheUsageMetadata 归一缓存 read/miss/write 字段，保留零值语义。
//
// legacy cache_tokens 仅作兼容读取值，不具权威性——provider 对它表示命中还是写定义不一致。
//
// Python: base_model_client.py:152 _cache_usage_metadata
func CacheUsageMetadata(obj map[string]any) map[string]any {
	readTokens := firstInt(obj,
		[2]string{"prompt_tokens_details", "cached_tokens"},
		[2]string{"promptTokensDetails", "cachedTokens"},
		[2]string{"input_tokens_details", "cached_tokens"},
		[2]string{"input_token_details", "cached_tokens"},
		[2]string{"inputTokensDetails", "cachedTokens"},
		[2]string{"inputTokenDetails", "cachedTokens"},
		[2]string{"usageMetadata", "cachedContentTokenCount"},
	)
	legacyRead := firstInt(obj, [2]string{"cache_tokens"})
	legacy := readTokens == nil && legacyRead != nil
	if readTokens == nil {
		readTokens = legacyRead
	}
	missTokens := firstInt(obj,
		[2]string{"prompt_cache_miss_tokens"}, [2]string{"cache_miss_tokens"},
		[2]string{"cache_miss_input_tokens"}, [2]string{"uncached_tokens"},
	)
	writeTokens := firstInt(obj,
		[2]string{"cache_write_tokens"}, [2]string{"cache_creation_input_tokens"},
		[2]string{"cache_creation_tokens"},
	)
	hasCacheUsage := readTokens != nil || missTokens != nil || writeTokens != nil
	var status, source any
	if hasCacheUsage {
		status, source = "observed", "provider_usage"
	}
	var authoritative any
	if hasCacheUsage && !legacy {
		authoritative = true
	}
	return map[string]any{
		"cache_read_tokens":  readTokens,
		"cache_miss_tokens":  missTokens,
		"cache_write_tokens": writeTokens,
		"cache_status":       status,
		"cache_source":       source,
		// cache_tokens 保留为兼容读取值，但它表示命中还是写各 provider 不一致
		"cache_authoritative": authoritative,
	}
}

// ExtractCacheCreationTokens 仅在 provider 暴露缓存写 token 时提取。
//
// Python: base_model_client.py:253 _extract_cache_creation_tokens
func ExtractCacheCreationTokens(obj map[string]any) *int {
	paths := [][2]string{
		{"input_tokens_details", "cache_creation_tokens"},
		{"input_token_details", "cache_creation_tokens"},
		{"prompt_tokens_details", "cache_creation_tokens"},
	}
	fields := []string{
		"cache_creation_input_tokens", "cache_write_input_tokens",
		"cache_creation_tokens", "cache_write_tokens",
	}
	for _, path := range paths {
		if nested, ok := obj[path[0]].(map[string]any); ok {
			if v, ok := coerceInt(nested[path[1]]); ok {
				return &v
			}
		}
	}
	for _, field := range fields {
		if v, ok := coerceInt(obj[field]); ok {
			return &v
		}
	}
	return nil
}

// ExtractReasoningTokens 从 provider usage 元数据提取推理/思考 token 数。
//
// Python: base_model_client.py:293 _extract_reasoning_tokens
// （completion_tokens_details.reasoning_tokens 嵌套 + 顶层平铺字段）
func ExtractReasoningTokens(obj map[string]any) int {
	if nested, ok := obj["completion_tokens_details"].(map[string]any); ok {
		if v := toIntNonNegative(nested["reasoning_tokens"]); v > 0 {
			return v
		}
	}
	return toIntNonNegative(obj["reasoning_tokens"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toIntNonNegative 数值/数字字符串 → 非负 int；bool/非数字/负数 → 0。
//
// Python: _to_int（_extract_cache_tokens 内版本）
func toIntNonNegative(v any) int {
	n, ok := coerceInt(v)
	if !ok || n < 0 {
		return 0
	}
	return n
}

// coerceInt 数值形态转换：int 族/float64/数字字符串（含 +/- 前缀）→ int。
//
// Python: _cache_usage_metadata 内 _to_int 的可空版本（None/bool → 未命中）
func coerceInt(v any) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	case float32:
		return int(val), true
	case jsonNumber:
		if n, err := strconv.ParseInt(string(val), 10, 64); err == nil {
			return int(n), true
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
			return n, true
		}
	}
	return 0, false
}

// firstInt 按序取第一个命中的 int（嵌套 map 单键优先，平铺字段用 [1] 位）。
//
// Python: _cache_usage_metadata 内 _first
func firstInt(obj map[string]any, paths ...[2]string) *int {
	for _, path := range paths {
		var value any
		if path[0] == "" {
			value = obj[path[1]]
		} else if nested, ok := obj[path[0]].(map[string]any); ok {
			value = nested[path[1]]
		} else {
			value = obj[path[1]]
		}
		if v, ok := coerceInt(value); ok {
			return &v
		}
	}
	return nil
}
```

注意：`jsonNumber` 类型不存在的话，直接用 `string` 分支即可（`json.Unmarshal` 解到 `map[string]any` 时数字都是 `float64`），删除该 case 与 `strconv` 之外的多余导入。导入按最终保留分支调整：`strconv` + `strings`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/ -run 'TestExtract|TestCacheUsage' -v`
Expected: PASS 全绿

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/usage_extract.go internal/agentcore/foundation/llm/model_clients/usage_extract_test.go
git commit -m "feat(llm): usage 抽取四函数对齐上游（C-B1，C-04/C-06 前半）"
```

---

## Task 2: C-B2 — `BuildUsageMetadata` 统一入口 + `UsageMetadata` 补字段

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/usage_metadata.go`（补 8 字段）
- Modify: `internal/agentcore/foundation/llm/model_clients/usage_extract.go`（追加 BuildUsageMetadata）
- Modify: `internal/agentcore/foundation/llm/schema/usage_metadata_test.go`（新增序列化断言）

- [ ] **Step 1: 补 UsageMetadata 字段（写失败测试）**

在 `usage_metadata_test.go` 追加：

```go
// TestUsageMetadata_缓存与推理字段序列化 新增字段的 JSON 键名与 nil 语义（对齐 Python 字段名）
func TestUsageMetadata_缓存与推理字段序列化(t *testing.T) {
	meta := NewUsageMetadata()
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	for _, key := range []string{"cache_read_tokens", "cache_miss_tokens", "cache_write_tokens",
		"cache_creation_tokens", "reasoning_tokens", "cache_status", "cache_source", "cache_authoritative"} {
		if _, exists := m[key]; exists {
			t.Errorf("nil 指针字段 %s 不应序列化（omitempty）", key)
		}
	}

	read, miss, creation, reasoning := 30, 10, 4, 900
	meta.CacheReadTokens, meta.CacheMissTokens, meta.CacheCreationTokens = &read, &miss, &creation
	meta.ReasoningTokens = &reasoning
	meta.CacheStatus, meta.CacheSource = "observed", "provider_usage"
	data, _ = json.Marshal(meta)
	m = map[string]any{}
	_ = json.Unmarshal(data, &m)
	if m["cache_read_tokens"] != float64(30) || m["cache_miss_tokens"] != float64(10) ||
		m["cache_creation_tokens"] != float64(4) || m["reasoning_tokens"] != float64(900) {
		t.Errorf("数值字段: %v", m)
	}
	if m["cache_status"] != "observed" || m["cache_source"] != "provider_usage" {
		t.Errorf("字符串字段: %v", m)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/schema/ -run TestUsageMetadata_缓存 -v`
Expected: FAIL 编译错误（`meta.CacheReadTokens undefined`）

- [ ] **Step 3: 补字段并实现 BuildUsageMetadata**

`schema/usage_metadata.go` 在 `CacheTokens` 后追加（含中文注释，JSON 键名对齐 Python）：

```go
	// CacheReadTokens 缓存读 token 数（nil=provider 未上报）
	CacheReadTokens *int `json:"cache_read_tokens,omitempty"`
	// CacheMissTokens 缓存 miss token 数（nil=未上报）
	CacheMissTokens *int `json:"cache_miss_tokens,omitempty"`
	// CacheWriteTokens 缓存写 token 数（nil=未上报）
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
	// CacheCreationTokens 缓存创建（写）token 数，独立于 CacheWriteTokens 的 provider 形态（nil=未上报）
	CacheCreationTokens *int `json:"cache_creation_tokens,omitempty"`
	// ReasoningTokens 推理/思考 token 数（output_tokens 的子集，nil=未上报）
	ReasoningTokens *int `json:"reasoning_tokens,omitempty"`
	// CacheStatus 缓存用量观测状态（"observed"=已观测到缓存用量）
	CacheStatus string `json:"cache_status,omitempty"`
	// CacheSource 缓存用量来源（"provider_usage"）
	CacheSource string `json:"cache_source,omitempty"`
	// CacheAuthoritative 缓存读值是否权威（legacy cache_tokens 不具权威性）
	CacheAuthoritative bool `json:"cache_authoritative,omitempty"`
```

`usage_extract.go` 追加导出函数：

```go
// BuildUsageMetadata 从 provider 响应的 usage 对象构建完整 UsageMetadata。
//
// Python: openai_model_client.py:2817-2829 / 2940-2946（invoke 与 stream 两个组装点共用的字段集）
// 基础字段由调用方（各客户端）从强类型字段填入，本函数只负责抽齐 cache/reasoning 族。
func BuildUsageMetadata(usage map[string]any, modelName string) *llmschema.UsageMetadata {
	meta := llmschema.NewUsageMetadata()
	meta.ModelName = modelName
	meta.InputTokens = toIntNonNegative(usage["prompt_tokens"])
	meta.OutputTokens = toIntNonNegative(usage["completion_tokens"])
	meta.TotalTokens = toIntNonNegative(usage["total_tokens"])
	meta.CacheTokens = ExtractCacheTokens(usage)

	norm := CacheUsageMetadata(usage)
	if v, ok := norm["cache_read_tokens"].(*int); ok {
		meta.CacheReadTokens = v
	}
	if v, ok := norm["cache_miss_tokens"].(*int); ok {
		meta.CacheMissTokens = v
	}
	if v, ok := norm["cache_write_tokens"].(*int); ok {
		meta.CacheWriteTokens = v
	}
	if v, ok := norm["cache_status"].(string); ok {
		meta.CacheStatus = v
	}
	if v, ok := norm["cache_source"].(string); ok {
		meta.CacheSource = v
	}
	if v, ok := norm["cache_authoritative"].(bool); ok {
		meta.CacheAuthoritative = v
	}
	meta.CacheCreationTokens = ExtractCacheCreationTokens(usage)
	meta.ReasoningTokens = ptrOfInt(ExtractReasoningTokens(usage))
	return meta
}
```

注意：`BuildUsageMetadata` 里 `ReasoningTokens` 只有 >0 才取指针（对齐 Python「未暴露时不设」）——实现成 `if n := ExtractReasoningTokens(usage); n > 0 { meta.ReasoningTokens = &n }`，并删掉示例里的 `ptrOfInt`（不要引入未定义助手）。需要 `import llmschema "…/foundation/llm/schema"`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/foundation/llm/schema/ ./internal/agentcore/foundation/llm/model_clients/ -run 'TestUsageMetadata|TestExtract|TestCacheUsage' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/schema/usage_metadata.go internal/agentcore/foundation/llm/schema/usage_metadata_test.go internal/agentcore/foundation/llm/model_clients/usage_extract.go
git commit -m "feat(llm): UsageMetadata 补 8 字段与 BuildUsageMetadata 统一入口（C-B2）"
```

---

## Task 3: C-B3 — 4 个客户端 usage 构建点替换 + `session_aggregator` 切新字段

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/parse_response.go:159-194`（buildUsageMetadata/extractCostFromUsage 替换）
- Modify: `internal/agentcore/foundation/llm/model_clients/siliconflow/client.go:474-501`
- Modify: `internal/agentcore/foundation/llm/model_clients/inference_affinity/client.go:568-590`
- Modify: `internal/agentcore/context_engine/usage/session_aggregator.go:15-50`

- [ ] **Step 1: 写失败测试（usage 一致性）**

在 `model_clients/usage_extract_test.go` 追加（三个客户端同构契约）：

```go
// TestBuildUsageMetadata_同构契约 三客户端共用一个构建函数：同输入同输出
func TestBuildUsageMetadata_同构契约(t *testing.T) {
	usage := map[string]any{
		"prompt_tokens": 100, "completion_tokens": 200, "total_tokens": 300,
		"prompt_tokens_details":   map[string]any{"cached_tokens": 60},
		"completion_tokens_details": map[string]any{"reasoning_tokens": 150},
	}
	meta := BuildUsageMetadata(usage, "m1")
	if meta.InputTokens != 100 || meta.OutputTokens != 200 || meta.TotalTokens != 300 {
		t.Errorf("basic: %+v", meta)
	}
	if meta.CacheTokens != 60 || meta.CacheReadTokens == nil || *meta.CacheReadTokens != 60 {
		t.Errorf("cache: %+v", meta)
	}
	if meta.ReasoningTokens == nil || *meta.ReasoningTokens != 150 {
		t.Errorf("reasoning: %+v", meta)
	}
	if meta.CacheStatus != "observed" || !meta.CacheAuthoritative {
		t.Errorf("status: %+v", meta)
	}
}
```

在 `context_engine/usage` 包新建/追加测试：

```go
// TestRequestCacheFromUsage_新字段优先 CacheReadTokens 非空时直接采用，不再走旧 CacheTokens 算法
func TestRequestCacheFromUsage_新字段优先(t *testing.T) {
	read, miss := 60, 40
	meta := &schema.UsageMetadata{InputTokens: 100, CacheTokens: 60,
		CacheReadTokens: &read, CacheMissTokens: &miss, CacheStatus: "observed", CacheSource: "provider_usage"}
	out := RequestCacheFromUsage(meta)
	if out.CacheReadTokens == nil || *out.CacheReadTokens != 60 {
		t.Errorf("read: %+v", out)
	}
	if out.CacheMissTokens == nil || *out.CacheMissTokens != 40 {
		t.Errorf("miss: %+v", out)
	}
	if out.Status != "reported" || out.Source != "provider_usage" {
		t.Errorf("status: %+v", out)
	}
	// 旧字段回落：无新字段但 CacheTokens>0 时维持现有算法
	legacy := RequestCacheFromUsage(&schema.UsageMetadata{InputTokens: 100, CacheTokens: 50})
	if legacy.CacheReadTokens == nil || *legacy.CacheReadTokens != 50 || legacy.Status != "reported" {
		t.Errorf("legacy: %+v", legacy)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/context_engine/usage/ -run TestRequestCacheFromUsage_新字段优先 -v`
Expected: FAIL（`out.CacheMissTokens` 恒为算出来的值而非 40，且新字段未被读取）

- [ ] **Step 3: 替换三处构建点**

`openai/parse_response.go`：`buildUsageMetadata` 函数体改为调 `model_clients.BuildUsageMetadata(usageToMap(usage), modelName)` + 保留 cost 提取（`extractCostFromUsage` 保留，Python 同样在组装点单列 cost）；新增小助手 `usageToMap`（`json.Marshal` → `map[string]any`，仿照既有 `extractCostFromUsage` 的转换）。**删除** `meta.CacheTokens = usage.PromptTokensDetails.CachedTokens` 直读分支（已并入 ExtractCacheTokens）。siliconflow/inference_affinity 的 `buildSiliconFlowUsageMetadata`/`buildInferenceAffinityUsageMetadata` 同样改为委托 `model_clients.BuildUsageMetadata` + 各自 cost 逻辑，**删除原函数体**。

`session_aggregator.go` `RequestCacheFromUsage`：在 `meta.CacheTokens > 0` 分支前加：

```go
	if meta.CacheReadTokens != nil {
		read := *meta.CacheReadTokens
		out.CacheReadTokens = &read
		if meta.CacheMissTokens != nil {
			miss := *meta.CacheMissTokens
			out.CacheMissTokens = &miss
		} else if out.InputTokens != nil && *out.InputTokens > read {
			miss := *out.InputTokens - read
			out.CacheMissTokens = &miss
		}
		out.Status = "reported"
		if meta.CacheSource != "" {
			out.Source = meta.CacheSource
		}
		return out
	}
```

并更新函数头注释（删「C 组缺陷」TODO 口径，改为「新字段缺失时回落 CacheTokens 旧算法」）。

- [ ] **Step 4: 跑测试确认通过 + 全包回归**

Run: `go test ./internal/agentcore/foundation/llm/... ./internal/agentcore/context_engine/usage/ -v 2>&1 | tail -5`
Expected: PASS（注意 parse_response_test.go 既有用例若断言 CacheTokens 来自 PromptTokensDetails，值不变，应仍绿）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/openai/parse_response.go internal/agentcore/foundation/llm/model_clients/siliconflow/client.go internal/agentcore/foundation/llm/model_clients/inference_affinity/client.go internal/agentcore/context_engine/usage/session_aggregator.go
git commit -m "refactor(llm): 三客户端 usage 构建收敛 BuildUsageMetadata + aggregator 切新字段（C-B3）"
```

---

## Task 4: C-A1 — 共享 Transport 池（TDD）

**Files:**
- Create: `internal/agentcore/foundation/llm/model_clients/transport_pool.go`
- Create: `internal/agentcore/foundation/llm/model_clients/transport_pool_test.go`

- [ ] **Step 1: 写失败测试**

```go
package model_clients

import (
	"crypto/tls"
	"testing"
)

// TestGetSharedTransport_按SSL形态分桶 同 (verify_ssl, ssl_cert) 复用同一实例；不同形态分桶
func TestGetSharedTransport_按SSL形态分桶(t *testing.T) {
	t1 := GetSharedTransport(false, "")
	t2 := GetSharedTransport(false, "")
	if t1 != t2 {
		t.Error("同形态应复用同一 Transport 指针")
	}
	t3 := GetSharedTransport(true, "")
	if t3 == t2 {
		t.Error("verify_ssl 不同应分桶")
	}
	ResetSharedTransports()
	t4 := GetSharedTransport(false, "")
	if t4 == t2 {
		t.Error("Reset 后应重建")
	}
}

// TestGetSharedTransport_池参数 参数照抄上游 Limits
func TestGetSharedTransport_池参数(t *testing.T) {
	tr := GetSharedTransport(false, "")
	if tr.MaxIdleConns != 100 {
		t.Errorf("MaxIdleConns = %d want 100", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 20 {
		t.Errorf("MaxIdleConnsPerHost = %d want 20", tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout.Seconds() != 60 {
		t.Errorf("IdleConnTimeout = %v want 60s", tr.IdleConnTimeout)
	}
	if tr.ResponseHeaderTimeout.Seconds() != 360 {
		t.Errorf("ResponseHeaderTimeout = %v want 360s", tr.ResponseHeaderTimeout)
	}
	if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("verify_ssl=false 应设 InsecureSkipVerify")
	}
	strict := GetSharedTransport(true, "")
	if strict.TLSClientConfig == nil || strict.TLSClientConfig.InsecureSkipVerify {
		t.Error("verify_ssl=true 不应 InsecureSkipVerify")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/ -run TestGetSharedTransport -v`
Expected: FAIL（`undefined: GetSharedTransport`）

- [ ] **Step 3: 实现 transport_pool.go**

```go
package model_clients

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// 池参数对齐上游 httpx.Limits（openai_model_client.py:1460-1464）与 keepalive_expiry=60.0（L1457）
const (
	// sharedMaxIdleConns 对齐 max_connections=100
	sharedMaxIdleConns = 100
	// sharedMaxIdlePerHost 对齐 max_keepalive_connections=20
	sharedMaxIdlePerHost = 20
	// sharedIdleConnTimeout 对齐 keepalive_expiry=60.0；同时防复用服务端已关的 idle 连接
	sharedIdleConnTimeout = 60 * time.Second
	// sharedResponseHeaderTimeout 兜「连上但永不给响应头」；上游由 read timeout 覆盖（偏差 D-2）
	sharedResponseHeaderTimeout = 360 * time.Second
	// sharedDialTimeout 拨号上限
	sharedDialTimeout = 30 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	transportPool   = map[transportPoolKey]*http.Transport{}
	transportPoolMu sync.Mutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSharedTransport 获取按 (verify_ssl, ssl_cert) 分桶的共享 Transport。
//
// Go 的 Transport host 无关（URL/鉴权头 per-request），按安全形态分桶即可最大化复用；
// 上游按 connection_key（含 api_base）分桶——见设计文档偏差 D-1。
func GetSharedTransport(verifySSL bool, sslCert string) *http.Transport {
	key := transportPoolKey{verifySSL: verifySSL, sslCert: sslCert}
	transportPoolMu.Lock()
	defer transportPoolMu.Unlock()
	if tr, ok := transportPool[key]; ok {
		return tr
	}
	tr := &http.Transport{
		MaxIdleConns:          sharedMaxIdleConns,
		MaxIdleConnsPerHost:   sharedMaxIdlePerHost,
		IdleConnTimeout:       sharedIdleConnTimeout,
		ResponseHeaderTimeout: sharedResponseHeaderTimeout,
		DialContext: (&netDialer).DialContext,
		Proxy:                 http.ProxyFromEnvironment,
	}
	// SSL 配置
	if !verifySSL {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	} else if sslCert != "" {
		caCert, err := os.ReadFile(sslCert)
		if err != nil {
			// 证书读不出：返回带错误标记的独立 Transport 不合适——这里直接 panic 会伤生产；
			// 按既有 buildHTTPClient 语义，构建失败应由调用方报错。池层缓存 nil 不可行，
			// 故证书形态错误时退化为进程信任库并记录（调用方 Validate 已挡前置）。
			tr.TLSClientConfig = &tls.Config{}
		} else {
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				tr.TLSClientConfig = &tls.Config{}
			} else {
				tr.TLSClientConfig = &tls.Config{RootCAs: caCertPool}
			}
		}
	}
	transportPool[key] = tr
	return tr
}

// CloseIdleTransports 关闭全部池化 Transport 的 idle 连接。
//
// Python: openai_model_client.py:1500 aclose（teardown 专用，禁止热路径调用）
func CloseIdleTransports() {
	transportPoolMu.Lock()
	keys := make([]transportPoolKey, 0, len(transportPool))
	for k := range transportPool {
		keys = append(keys, k)
	}
	transportPoolMu.Unlock()
	for _, k := range keys {
		transportPoolMu.Lock()
		tr := transportPool[k]
		transportPoolMu.Unlock()
		if tr != nil {
			tr.CloseIdleConnections()
		}
	}
}

// ResetSharedTransports 清空池（测试用；生产 teardown 走 CloseIdleTransports）。
func ResetSharedTransports() {
	transportPoolMu.Lock()
	transportPool = map[transportPoolKey]*http.Transport{}
	transportPoolMu.Unlock()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// transportPoolKey Transport 池分桶键（安全形态）。
type transportPoolKey struct {
	verifySSL bool
	sslCert   string
}
```

补 `import "net"` 并声明 `var netDialer = net.Dialer{Timeout: sharedDialTimeout}`（放全局变量区）。`ResetSharedTransports` 仅供测试，生产代码 0 引用即可（测试文件在包内）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/ -run TestGetSharedTransport -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/transport_pool.go internal/agentcore/foundation/llm/model_clients/transport_pool_test.go
git commit -m "feat(llm): 共享 Transport 池（C-A1，C-14 前半）"
```

---

## Task 5: C-A2 — `BuildHTTPRequest` 超时语义重写（流式无总超时 + Invoke ctx deadline）

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/request_builder.go:56-164`
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/request_builder_test.go`
- Modify: `internal/agentcore/foundation/llm/model_clients/inference_affinity/client.go:716-760`（自有 buildHTTPClient 删除，改走池）

- [ ] **Step 1: 写失败测试**

在 `request_builder_test.go` 追加：

```go
// TestBuildHTTPRequest_流式无总超时 流式：Client.Timeout==0（读 body 由帧超时兜底）
func TestBuildHTTPRequest_流式无总超时(t *testing.T) {
	timeout := 1800.0
	streaming := true
	req, client, err := BuildHTTPRequest(context.Background(), "http://x", "k",
		map[string]any{"model": "m", "messages": []any{}, "stream": true},
		nil, &timeout, false, "", streaming)
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout != 0 {
		t.Errorf("流式 Client.Timeout = %v want 0", client.Timeout)
	}
	if client.Transport == nil {
		t.Error("应挂共享 Transport")
	}
	_ = req
}

// TestBuildHTTPRequest_Invoke走ctxDeadline 非流式：总超时通过 ctx 生效，Client.Timeout 恒 0
func TestBuildHTTPRequest_Invoke走ctxDeadline(t *testing.T) {
	timeout := 0.5
	req, client, err := BuildHTTPRequest(context.Background(), "http://x", "k",
		map[string]any{"model": "m", "messages": []any{}},
		nil, &timeout, false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout != 0 {
		t.Errorf("Invoke Client.Timeout = %v want 0（走 ctx）", client.Timeout)
	}
	dl, ok := req.Context().Deadline()
	if !ok || time.Until(dl) > time.Second {
		t.Errorf("ctx 应带 %vs deadline", timeout)
	}
}

// TestBuildHTTPRequest_默认超时360 未配置 timeout 时取默认 360（对齐 schema/config.py:145）
func TestBuildHTTPRequest_默认超时360(t *testing.T) {
	req, _, err := BuildHTTPRequest(context.Background(), "http://x", "k",
		map[string]any{"model": "m", "messages": []any{}}, nil, nil, false, "", false)
	if err != nil {
		t.Fatal(err)
	}
	dl, ok := req.Context().Deadline()
	if !ok || time.Until(dl) > 361*time.Second || time.Until(dl) < 358*time.Second {
		t.Errorf("默认 deadline 应约 360s，实际 %v", time.Until(dl))
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/openai/ -run 'TestBuildHTTPRequest_流式|TestBuildHTTPRequest_Invoke|TestBuildHTTPRequest_默认' -v`
Expected: FAIL（BuildHTTPRequest 无 streaming 参数、Client.Timeout 非 0）

- [ ] **Step 3: 重写 BuildHTTPRequest**

```go
// BuildHTTPRequest 构建发送给 OpenAI API 的 HTTP 请求。
//
// 超时语义（设计文档批次 C-A）：
//   - 非流式：ctx deadline（timeout 秒）；Client.Timeout 恒 0
//   - 流式：无总超时。等头由共享 Transport 的 ResponseHeaderTimeout 兜；
//     读 body 由 model.go 帧超时（首帧 300s/空闲 60s）+ cancelStream 兜
//   - timeout 为 nil 时取默认 360（对齐 Python schema/config.py:145 默认，
//     且满足 timeout >= stream_first_chunk_timeout=300 不变量）
func BuildHTTPRequest(
	ctx context.Context,
	apiBase, apiKey string,
	params map[string]any,
	headers map[string]string,
	timeout *float64,
	verifySSL bool,
	sslCert string,
	streaming bool,
) (*http.Request, *http.Client, error) {
	// 序列化请求体（编码失败/非法 UTF-8 → 181004，见 encodeJSONChecked）
	body, err := encodeJSONChecked(params)
	if err != nil {
		return nil, nil, err
	}

	// 构建 API URL（C-E1 归一）
	apiURL := NormalizeAPIBase(apiBase) + chatCompletionsPath

	// 非流式：把总超时挂到 ctx（共享 Transport 上不能挂 Client.Timeout）
	effectiveTimeout := defaultRequestTimeoutSeconds
	if timeout != nil && *timeout > 0 {
		effectiveTimeout = *timeout
	}
	if !streaming {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(effectiveTimeout*float64(time.Second)))
		_ = cancel // req 携带 ctx；cancel 由 http 客户端响应完成后的连接回收触发，无需调用方持有
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Transport: model_clients.GetSharedTransport(verifySSL, sslCert)}
	return req, client, nil
}
```

同文件：`defaultRequestTimeoutSeconds = 360.0` 常量；**删除** `buildHTTPClient` 函数整段（C-14 收敛）；`encodeJSON` 保留但 `encodeJSONChecked` 见 Task 7（本任务先实现为 `encodeJSON` 直调 + `utf8.Valid` 校验占位不需要——**注意**：为了让 Task 5 独立可编译，本步 `encodeJSONChecked` 暂时等于 `encodeJSON` 别名，Task 7 再替换实现；在函数上注明「编码分类在 C-E 批次落地」）。`http.NewRequestWithContext` 返回后立即 `_ = cancel` 的写法有 lint 风险（lostcancel）——改为 `defer` 不可行（req 返回后仍需 ctx 活着），正确做法：**不存储 cancel**，直接 `ctx, _ = context.WithTimeout(ctx, …)` 同行赋值（Go 允许 `_` 丢弃；staticcheck SA4006 不会告警此形态，若 lint 报 lostcancel 则改用 `defer cancel()` 前置说明——最终以本地 lint 实测为准，两种写法择一）。

同步改 4 个调用点加 `streaming` 实参：`openai/client.go:122`（invoke 传 false）、`:266`（stream 传 true）、`siliconflow/client.go:156`、`inference_affinity/client.go:179`、`intellirouter/client.go:264`。`inference_affinity/client.go:716-760` 的自有 buildHTTPClient 整段删除，release_kvcache 路径（`:342` 附近）也改用共享 Transport。

- [ ] **Step 4: 跑测试确认通过 + 修正默认值**

`schema/config.go:263` `Timeout: 60.0` → `Timeout: defaultClientTimeoutSeconds`（新常量 360.0，导出供测试引用）；`config_test.go:259-261` 的断言 `want 60.0` 改 `want 360.0`；`inference_affinity/client.go:741` 硬编码 60 删除（随 buildHTTPClient 一并消失）。`openai/client.go:64-67` 的 `finalTimeout <= 0 → 60` 日志兜底改 360。

Run: `go test ./internal/agentcore/foundation/llm/... -v 2>&1 | tail -8`
Expected: PASS（既有 request_builder_test 中断言 `client.Timeout == 60` 之类的用例按新语义改写为 ctx deadline 断言）

- [ ] **Step 5: 提交**

```bash
git add -A internal/agentcore/foundation/llm/
git commit -m "fix(llm): 流式无总超时+Invoke走ctx+共享Transport接线+默认360（C-A2，C-01/C-14）"
```

---

## Task 6: C-A3 — 流式超时集成用例（httptest 真流）

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/client_test.go`

- [ ] **Step 1: 写用例（长流不被切断 + 卡死流被帧超时切断）**

```go
// TestStream_持续输出超过总超时不被切断 C-01 核心回归：
// 1.2s 持续吐 SSE（配置 timeout=0.8s），流式无总超时 → 正常收完
func TestStream_持续输出超过总超时不被切断(t *testing.T) {
	var chunks int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for i := 0; i < 12; i++ {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
			flusher.Flush()
			time.Sleep(100 * time.Millisecond)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	cc := &llmschema.ModelClientConfig{ClientProvider: "OpenAI", APIKey: "k", APIBase: srv.URL, Timeout: 0.8, VerifySSL: false}
	c, err := NewOpenAIModelClient(llmschema.NewModelRequestConfig(), cc)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := c.Stream(context.Background(), model_clients.NewMessagesParam(llmschema.NewUserMessage("hi")))
	if err != nil {
		t.Fatal(err)
	}
	for chunk := range ch {
		if chunk.Content.Text() != "" {
			atomic.AddInt32(&chunks, 1)
		}
	}
	if atomic.LoadInt32(&chunks) != 12 {
		t.Errorf("应收满 12 块（1.2s > 0.8s 总超时），实际 %d", chunks)
	}
}
```

- [ ] **Step 2: 跑测试确认通过（验证 C-A 生效）**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/openai/ -run TestStream_持续输出 -v`
Expected: PASS（若 FAIL 说明总超时仍挂在 client 上，回查 Task 5）

注意：此用例走客户端直调（不经 model.go 帧超时），首帧等待 < 300s 自然通过；空闲 60s 语义在 `model_timeout_test.go` 已有覆盖，不重复。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/openai/client_test.go
git commit -m "test(llm): 持续输出超总超时不被切断的回归用例（C-A3）"
```

---

## Task 7: C-E1 — `NormalizeAPIBase` + 接线（含 dashscope 原生侧）

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/request_builder.go`（加 NormalizeAPIBase）
- Modify: `internal/agentcore/foundation/llm/model_clients/dashscope/api_client.go:30-53`
- Modify: `internal/agentcore/foundation/llm/model_clients/inference_affinity/client.go:342`
- Modify: `internal/swarm/gateway/channel_manager/web/app_web_handlers.go:1290-1293`

- [ ] **Step 1: 写失败测试（openai 包 + dashscope 包）**

```go
// openai/request_builder_test.go
// TestNormalizeAPIBase_矩阵 对齐 openai_model_client.py:307-321
func TestNormalizeAPIBase_矩阵(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://host/v1/chat/completions", "https://host/v1"},
		{"https://host/v1/chat/completions/", "https://host/v1"},
		{"https://host/v1/", "https://host/v1"},
		{"https://host/v1", "https://host/v1"},
		{"  https://host/v1  ", "https://host/v1"},   // strip 空白
		{"https://host/v1/chat/completions/chat/completions", "https://host/v1/chat/completions"}, // 只剥一层
		{"", ""},
	}
	for _, tc := range cases {
		if got := NormalizeAPIBase(tc.in); got != tc.want {
			t.Errorf("NormalizeAPIBase(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

// dashscope/api_client_test.go 追加
// TestResolveDashScopeBaseURL_完整URL 先归一再切分（偏差 D-4：超出上游透传语义）
func TestResolveDashScopeBaseURL_完整URL(t *testing.T) {
	if got := ResolveDashScopeBaseURL("https://proxy.example.com/v1/chat/completions"); got != "https://proxy.example.com/v1" {
		t.Errorf("got %q", got)
	}
	if got := ResolveDashScopeBaseURL("https://dashscope.aliyuncs.com/compatible-mode/v1"); got != "https://dashscope.aliyuncs.com" {
		t.Errorf("got %q", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/openai/ ./internal/agentcore/foundation/llm/model_clients/dashscope/ -run 'TestNormalizeAPIBase|TestResolveDashScopeBaseURL_完整' -v`
Expected: FAIL（undefined / 旧值）

- [ ] **Step 3: 实现**

```go
// NormalizeAPIBase 归一 api_base：strip 空白 → 去尾斜杠 → 剥掉已带的 /chat/completions。
//
// 不推断 /v1（上游同样不注入）。粘贴完整端点 URL 时避免拼出双段 404。
//
// Python: openai_model_client.py:307-321 _normalize_openai_base_url
func NormalizeAPIBase(apiBase string) string {
	base := strings.TrimSpace(apiBase)
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		base = strings.TrimSuffix(base, "/chat/completions")
		base = strings.TrimRight(base, "/")
	}
	return base
}
```

`ResolveDashScopeBaseURL` 改为入口先 `openai.NormalizeAPIBase(apiBase)` 再按 `compatibleModeMarker` 切分（保留现有切分逻辑与注释；头部补「完整 URL 先归一——超出上游透传，偏差 D-4」）。`inference_affinity/client.go:342` 与 `app_web_handlers.go:1291-1293` 的 `TrimSuffix/TrimRight` 两行替换为单次 `openai.NormalizeAPIBase` 调用（web 侧需 import openai 包，若引发循环依赖则改为在 web 侧留一行 `strings.TrimRight(openai.NormalizeAPIBase(apiBase), "/")`——实测后择一）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/... -run 'TestNormalizeAPIBase|TestResolveDashScope' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/openai/request_builder.go internal/agentcore/foundation/llm/model_clients/dashscope/api_client.go internal/agentcore/foundation/llm/model_clients/inference_affinity/client.go internal/swarm/gateway/channel_manager/web/app_web_handlers.go
git commit -m "fix(llm): NormalizeAPIBase 归一并四处接线（C-E1，C-08）"
```

---

## Task 8: C-E2 — 编码错误分类（181004 + utf8.Valid，A+ 严格版）

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/request_builder.go`（encodeJSONChecked 实现）
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/client.go:130-134/276-279`（放行判断）
- Modify: `internal/agentcore/foundation/llm/model_clients/siliconflow/client.go:158-161`（放行判断）

- [ ] **Step 1: 写失败测试**

```go
// TestBuildHTTPRequest_编码错误分类 NaN/不可序列化/非UTF-8 → 181004 + stage=request_encoding
func TestBuildHTTPRequest_编码错误分类(t *testing.T) {
	// NaN
	_, _, err := BuildHTTPRequest(context.Background(), "http://x", "k",
		map[string]any{"model": "m", "temperature": math.NaN(), "messages": []any{}},
		nil, nil, false, "", false)
	if !isRequestEncodingError(t, err) {
		t.Errorf("NaN: %v", err)
	}
	// 不可序列化类型（chan）
	_, _, err = BuildHTTPRequest(context.Background(), "http://x", "k",
		map[string]any{"model": "m", "bad": make(chan int), "messages": []any{}},
		nil, nil, false, "", false)
	if !isRequestEncodingError(t, err) {
		t.Errorf("chan: %v", err)
	}
	// 非 UTF-8（msg["content"] 含非法字节——通过 map 传 string 含 "\xff"）
	_, _, err = BuildHTTPRequest(context.Background(), "http://x", "k",
		map[string]any{"model": "m", "messages": []any{map[string]any{"role": "user", "content": "ab\xff\xfe"}}},
		nil, nil, false, "", false)
	if !isRequestEncodingError(t, err) {
		t.Errorf("非 UTF-8: %v", err)
	}
}

func isRequestEncodingError(t *testing.T, err error) bool {
	t.Helper()
	var base *exception.BaseError
	if !errors.As(err, &base) {
		return false
	}
	if base.Code() != 181004 { // 按实际 API 名调整（NewStatusCode 的取值访问器）
		return false
	}
	details, _ := base.Details().(map[string]any)
	return details["stage"] == "request_encoding"
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/openai/ -run TestBuildHTTPRequest_编码错误分类 -v`
Expected: FAIL（现在返回 `序列化请求参数失败` 普通错误）

- [ ] **Step 3: 实现**

```go
// encodeJSONChecked 序列化并校验 UTF-8，失败抛 181004 MODEL_INVOKE_PARAM_ERROR。
//
// Python: utils/request_encoding.py:27-35（SDK _build_request 边界）+ invoke/stream 的
// details=={"stage":"request_encoding"} 放行（openai_model_client.py:2011/2228）
func encodeJSONChecked(params map[string]any) ([]byte, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, exception.NewBaseError(exception.StatusModelInvokeParamError,
			exception.WithMsg(fmt.Sprintf("Cannot encode model request: %v", err)),
			exception.WithDetails(map[string]any{"stage": "request_encoding"}),
			exception.WithCause(err))
	}
	if !utf8.Valid(body) {
		return nil, exception.NewBaseError(exception.StatusModelInvokeParamError,
			exception.WithMsg("Cannot encode model request: invalid UTF-8 in request body"),
			exception.WithDetails(map[string]any{"stage": "request_encoding"}))
	}
	return body, nil
}
```

`client.go` Invoke/Stream 与 siliconflow 的 `if err != nil { return nil, c.WrapError(...) }` 前加放行：

```go
	if isRequestEncodingErr(err) {
		return nil, err // 编码参数错误原样上抛，不包 MODEL_CALL_FAILED（不参与重试）
	}
```

```go
// isRequestEncodingErr 判 181004 且 stage=request_encoding（对齐 Python isinstance+details 判断）。
func isRequestEncodingErr(err error) bool {
	var base *exception.BaseError
	if !errors.As(err, &base) {
		return false
	}
	details, _ := base.Details().(map[string]any)
	return details["stage"] == "request_encoding"
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/... -run 'TestBuildHTTPRequest_编码|TestBuildHTTPRequest_流式|TestBuildHTTPRequest_Invoke' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/openai/ internal/agentcore/foundation/llm/model_clients/siliconflow/
git commit -m "fix(llm): 请求编码错误分类 181004+utf8 严格校验（C-E2，C-10）"
```

---

## Task 9: C-C1 — reasoning 四级回退抽取 + 各客户端字段补齐

**Files:**
- Create: `internal/agentcore/foundation/llm/model_clients/reasoning_extract.go`
- Create: `internal/agentcore/foundation/llm/model_clients/reasoning_extract_test.go`
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/types.go`（ChunkDelta/ResponseMessage 补 Reasoning/ReasoningDetails）
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/parse_stream_chunk.go:68-72`、`parse_response.go:70-73`

- [ ] **Step 1: 写失败测试**

```go
// TestExtractReasoningContent_四级回退 对齐 openai_model_client.py:2751-2763
func TestExtractReasoningContent_四级回退(t *testing.T) {
	// 1) reasoning_details[0].text
	if got := ExtractReasoningContent(map[string]any{
		"reasoning_details": []any{map[string]any{"text": "think"}}}); got != "think" {
		t.Errorf("details: %q", got)
	}
	// 2) reasoning_content
	if got := ExtractReasoningContent(map[string]any{"reasoning_content": "a"}); got != "a" {
		t.Errorf("content: %q", got)
	}
	// 3) reasoning（OpenRouter）
	if got := ExtractReasoningContent(map[string]any{"reasoning": "b"}); got != "b" {
		t.Errorf("reasoning: %q", got)
	}
	// 4) reasoning_token_text
	if got := ExtractReasoningContent(map[string]any{"reasoning_token_text": "c"}); got != "c" {
		t.Errorf("token_text: %q", got)
	}
	// 优先级：details > reasoning_content
	if got := ExtractReasoningContent(map[string]any{
		"reasoning_details": []any{map[string]any{"text": "first"}}, "reasoning_content": "second"}); got != "first" {
		t.Errorf("priority: %q", got)
	}
	if got := ExtractReasoningContent(map[string]any{}); got != "" {
		t.Errorf("empty: %q", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/ -run TestExtractReasoningContent -v`
Expected: FAIL（undefined）

- [ ] **Step 3: 实现 + 接线**

```go
// ExtractReasoningContent 从消息或 delta 的 map 形态抽取推理内容（四级回退）。
//
// Python: openai_model_client.py:2751-2763 _extract_reasoning_content
// 顺序：reasoning_details[0].text → reasoning_content → reasoning（OpenRouter）→ reasoning_token_text
func ExtractReasoningContent(fields map[string]any) string {
	if details, ok := fields["reasoning_details"].([]any); ok && len(details) > 0 {
		if first, ok := details[0].(map[string]any); ok {
			if text, ok := first["text"].(string); ok && text != "" {
				return text
			}
		}
	}
	for _, attr := range []string{"reasoning_content", "reasoning", "reasoning_token_text"} {
		if v, ok := fields[attr].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
```

`types.go` `ChunkDelta`/`ResponseMessage` 追加（中文注释）：

```go
	// Reasoning OpenRouter 风格推理内容字段（reasoning_content 的别名形态）
	Reasoning *string `json:"reasoning,omitempty"`
	// ReasoningTokenText 部分网关的推理 token 文本字段
	ReasoningTokenText *string `json:"reasoning_token_text,omitempty"`
	// ReasoningDetails 结构化推理明细（Anthropic 风格 [{text:...}]）
	ReasoningDetails []map[string]any `json:"reasoning_details,omitempty"`
```

`parse_stream_chunk.go`/`parse_response.go` 的 `reasoningContent` 抽取改为先把 delta/message 转成 `map[string]any`（小助手 `reasoningFieldsOf`：`json.Marshal` 结构体 → unmap）再调 `model_clients.ExtractReasoningContent`；删除直读 `delta.ReasoningContent` 的单字段分支。

- [ ] **Step 4: 跑测试确认通过 + 既有回归**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/... -run 'TestExtractReasoning|TestParseStream|TestParseResponse' -v`
Expected: PASS（既有 ReasoningContent 用例的值不变）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/reasoning_extract.go internal/agentcore/foundation/llm/model_clients/reasoning_extract_test.go internal/agentcore/foundation/llm/model_clients/openai/types.go internal/agentcore/foundation/llm/model_clients/openai/parse_stream_chunk.go internal/agentcore/foundation/llm/model_clients/openai/parse_response.go
git commit -m "feat(llm): reasoning 四级回退抽取（C-C1，C-05 前半）"
```

---

## Task 10: C-C2 — DeepSeek 门卫改模型名 + 删 deepseek 包

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/base_client.go:316-400`（BuildRequestParams 补字段）
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/client.go:623-638`（init 注册 DeepSeek）
- Delete: `internal/agentcore/foundation/llm/model_clients/deepseek/`（client.go/doc.go/client_test.go）
- Modify: `internal/agentcore/foundation/llm/model_clients_register.go:17`（删空导入）
- Modify: `internal/agentcore/foundation/llm/model_clients/doc.go:62`（删 2.9 注册点行）

- [ ] **Step 1: 写失败测试**

```go
// base_client_test.go 追加
// TestBuildRequestParams_DeepSeek模型补reasoning 模型名含 deepseek 时 assistant 消息补空 reasoning_content
func TestBuildRequestParams_DeepSeek模型补reasoning(t *testing.T) {
	mc := llmschema.NewModelRequestConfig(llmschema.WithModelName("deepseek-chat"))
	e := &BaseClientEmbed{ModelConfig: mc, ClientConfig: &llmschema.ModelClientConfig{ClientProvider: "OpenAI"}, clientName: "t"}
	msgs := model_clients.NewMessagesParam(&llmschema.AssistantMessage{Content: llmschema.NewMessageContent("hi")})
	dict, err := e.ConvertMessagesToDict(msgs)
	if err != nil {
		t.Fatal(err)
	}
	params, err := e.BuildRequestParams(context.Background(), dict, &InvokeParams{Model: "deepseek-chat"}, false)
	if err != nil {
		t.Fatal(err)
	}
	reqMsgs := params["messages"].([]map[string]any)
	if _, ok := reqMsgs[0]["reasoning_content"]; !ok {
		t.Error("deepseek 模型的 assistant 消息应补 reasoning_content")
	}

	// 非 deepseek 模型不补
	params2, _ := e.BuildRequestParams(context.Background(), dict, &InvokeParams{Model: "qwen-max"}, false)
	reqMsgs2 := params2["messages"].([]map[string]any)
	if _, ok := reqMsgs2[0]["reasoning_content"]; ok {
		t.Error("非 deepseek 模型不应补 reasoning_content")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/ -run TestBuildRequestParams_DeepSeek -v`
Expected: FAIL（不补字段）

- [ ] **Step 3: 实现 + 删包**

`BuildRequestParams` 在消息 dict 就绪后（tools 转换之前）加：

```go
	// DeepSeek 兼容：模型名含 deepseek 时为 assistant 消息补 reasoning_content（缺省会 400）。
	// Python: endpoint_profiles.py:33-41 model_requires_reasoning_content + _deepseek_reasoning_content
	if modelRequiresReasoningContent(model) {
		for _, msg := range messagesDict {
			if role, _ := msg["role"].(string); role == "assistant" {
				if _, exists := msg["reasoning_content"]; !exists {
					msg["reasoning_content"] = ""
				}
			}
		}
	}
```

```go
// modelRequiresReasoningContent 判模型名是否含 deepseek（小写包含）。
//
// Python: utils/endpoint_profiles.py:33-34
func modelRequiresReasoningContent(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "deepseek")
}
```

`openai/client.go` init 段：`registry.Register("DeepSeek", "llm", openAIFactory)` 追加一行（deepseek 包删除后由 openai 工厂承接）。删除 `model_clients/deepseek/` 三文件与 `model_clients_register.go:17` 空导入；`model_clients/doc.go:62` 的 `2.9 DeepSeekModelClient` 行删除（规则 8：文件树条目同步）。**验收**：`grep -rn "model_clients/deepseek" internal/ cmd/ tests/` 0 命中；`grep -rn "WithInitTopP" internal/` 确认无 deepseek 特有残留。

- [ ] **Step 4: 跑测试确认通过 + 全量编译**

Run: `go build ./... && go test ./internal/agentcore/foundation/llm/... -v 2>&1 | tail -5`
Expected: PASS（deepseek/client_test.go 的用例随包删除消失；若有断言「DeepSeek 客户端类型」的用例在别处，改断言注册表工厂输出）

- [ ] **Step 5: 提交**

```bash
git add -A internal/agentcore/foundation/llm/
git commit -m "refactor(llm): DeepSeek 门卫改模型名并删除幽灵包（C-C2，C-05 后半）"
```

---

## Task 11: C-D1 — `AuthMode` 枚举 + 白名单矩阵 + 删 WithSkipValidate

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/config.go`（AuthMode/EndpointProfile 字段 + 白名单 + Validate 改两层）
- Modify: `internal/agentcore/foundation/llm/model_clients/base_client.go:100-214`（ValidateConfig 对齐 + 删 WithSkipValidate/skipValidate）
- Modify: `internal/agentcore/foundation/llm/model_clients/intellirouter/client.go:78-85`（删 WithSkipValidate 调用）
- Modify: `internal/agentcore/foundation/llm/model_clients/base_client_test.go:448-505`（删两个 SkipValidate 测试，改白名单用例）

- [ ] **Step 1: 写失败测试**

```go
// schema/config_test.go 追加
// TestValidate_白名单矩阵 对齐 config.py:234-256（7 provider 两白名单）
func TestValidate_白名单矩阵(t *testing.T) {
	// intelli_router：key/base 都不要求
	cfg, err := NewModelClientConfig("intelli_router", "", "", WithAuthMode(AuthModeNone))
	if err != nil {
		t.Errorf("intelli_router 无 key/base 应通过: %v", err)
	}
	// OpenAI + auth_mode=none：不要求 key，但 base 必填
	if _, err := NewModelClientConfig("OpenAI", "", "https://x", WithAuthMode(AuthModeNone)); err != nil {
		t.Errorf("auth_mode=none 不应要求 key: %v", err)
	}
	// OpenAI + api_key 模式：key 必填
	if _, err := NewModelClientConfig("OpenAI", "", "https://x"); err == nil {
		t.Error("OpenAI 默认模式缺 key 应报错")
	}
	// endpoint_profile 默认 base 回填（ollama）
	cfg, err = NewModelClientConfig("intelli_router", "", "", WithEndpointProfile("ollama"), WithAuthMode(AuthModeNone))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIBase != "http://localhost:11434/v1" {
		t.Errorf("ollama 默认 base: %q", cfg.APIBase)
	}
	// verify_ssl=true 无 ssl_cert 不再报错（schema 层只做形态）
	if _, err := NewModelClientConfig("OpenAI", "k", "https://x", WithVerifySSL(true)); err != nil {
		t.Errorf("verify_ssl=true 无证书不应报错: %v", err)
	}
}

// base_client_test.go 追加（替换原 SkipValidate 用例）
// TestValidateConfig_IntelliRouter白名单 provider 不在 base 白名单时跳过 api_base 校验（偏差 D-3）
func TestValidateConfig_IntelliRouter白名单(t *testing.T) {
	mc := llmschema.NewModelRequestConfig()
	cc := &llmschema.ModelClientConfig{ClientProvider: "intelli_router", AuthMode: llmschema.AuthModeNone, Timeout: 60}
	e := &BaseClientEmbed{ModelConfig: mc, ClientConfig: cc, clientName: "t"}
	if err := e.ValidateConfig(); err != nil {
		t.Errorf("intelli_router 空 key/base 应通过白名单跳过: %v", err)
	}
	// api_key 模式的白名单内 provider 仍要求 key
	cc2 := &llmschema.ModelClientConfig{ClientProvider: "OpenAI", APIBase: "https://x", Timeout: 60}
	e2 := &BaseClientEmbed{ModelConfig: mc, ClientConfig: cc2, clientName: "t"}
	if err := e2.ValidateConfig(); err == nil {
		t.Error("OpenAI 缺 key 应报错")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/schema/ ./internal/agentcore/foundation/llm/model_clients/ -run 'TestValidate_白名单|TestValidateConfig_IntelliRouter' -v`
Expected: FAIL（无 AuthMode 类型；现校验恒必填）

- [ ] **Step 3: 实现**

`schema/config.go`：

```go
// AuthMode LLM 鉴权模式枚举。
//
// Python: schema/config.py:40-44 LLMAuthMode（openai_account_oauth 分支暂不生效，
// openai_account_model_client.py 未迁移 ⤵️）
type AuthMode string

const (
	// AuthModeAPIKey Bearer API key（默认）
	AuthModeAPIKey AuthMode = "api_key"
	// AuthModeNone 无鉴权（自建网关）
	AuthModeNone AuthMode = "none"
	// AuthModeCustomHeaders 靠自定义请求头注入凭据
	AuthModeCustomHeaders AuthMode = "custom_headers"
	// AuthModeOpenAIAccountOAuth OpenAI 账号 OAuth（未迁移）
	AuthModeOpenAIAccountOAuth AuthMode = "openai_account_oauth"
)

// topLevelAPIKeyProviders 要求顶层 api_key 的 provider 白名单。
//
// Python: schema/config.py:111-126 _TOP_LEVEL_API_KEY_PROVIDERS
var topLevelAPIKeyProviders = map[string]struct{}{
	"OpenAI": {}, "OpenRouter": {}, "SiliconFlow": {}, "DashScope": {}, "DeepSeek": {},
	"Moonshot": {}, "MiniMax": {}, "ModelArts": {}, "VolcEngine": {}, "Qianfan": {},
	"Zhipu": {}, "MiMo": {}, "InferenceAffinity": {},
}

// topLevelAPIBaseProviders 要求顶层 api_base 的 provider 白名单（= key 白名单 ∪ OpenAIAccount）。
//
// Python: schema/config.py:127 _TOP_LEVEL_API_BASE_PROVIDERS
var topLevelAPIBaseProviders = map[string]struct{}{ /* 同上复制一份 + "OpenAIAccount": {} */ }

// defaultAPIBaseByEndpointProfile endpoint_profile 缺省 base 回填表。
//
// Python: schema/config.py:128-131 _DEFAULT_API_BASE_BY_ENDPOINT_PROFILE
var defaultAPIBaseByEndpointProfile = map[string]string{
	"ollama":   "http://localhost:11434/v1",
	"lmstudio": "http://localhost:1234/v1",
}
```

`ModelClientConfig` 补字段 `AuthMode AuthMode \`json:"auth_mode,omitempty"\``（默认零值须为 `api_key`——注意 Go string 零值是 `""`，因此 `Validate` 里 `if c.AuthMode == "" { c.AuthMode = AuthModeAPIKey }` 归一）与 `EndpointProfile string \`json:"endpoint_profile,omitempty"\``；新增 `WithAuthMode`/`WithEndpointProfile` 选项函数。`Validate()` 在 provider 归一后按白名单分支重写（key/base 各自按 auth_mode 与白名单判定 + endpoint_profile 回填）。

`base_client.go` `ValidateConfig`：删 `skipValidate` 分支与字段、`WithSkipValidate` 选项、`NewBaseClientEmbed` 的 opts 循环保留其余；新逻辑对齐 `base_model_client.py:428-445`（auth_mode != api_key → 不要求 key；api_base 在 provider ∈ base 白名单时必填，否则跳过——偏差 D-3）；**删除 `verify_ssl && ssl_cert == ""` 报错分支**。`intellirouter/client.go:81` 删 `model_clients.WithSkipValidate(),` 行（构造前的 deployment 校验已在，白名单后自然通过）。`base_client_test.go:448-505` 两个 SkipValidate 测试删除，由 Step 1 新用例替代。

- [ ] **Step 4: 跑测试确认通过 + 全量回归**

Run: `go build ./... && go test ./internal/agentcore/foundation/llm/... 2>&1 | tail -5`
Expected: PASS（注意 `TestValidateConfig_验证SSL无证书`（base_client_test.go:62）断言的是报错——删除该用例，语义已被「不再强制」取代）

- [ ] **Step 5: 提交**

```bash
git add -A internal/agentcore/foundation/llm/
git commit -m "feat(llm): AuthMode+白名单矩阵双层校验，删 WithSkipValidate 兼容壳（C-D1，C-07）"
```

---

## Task 12: C-D2 — 鉴权头按 auth_mode（省略 / EMPTY 占位）

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/request_builder.go:80-88`（Authorization 注入分支）
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/request_builder_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestBuildHTTPRequest_鉴权头按AuthMode none→省略；custom_headers+空key→省略；api_key+空→EMPTY
func TestBuildHTTPRequest_鉴权头按AuthMode(t *testing.T) {
	cases := []struct {
		authMode llmschema.AuthMode
		apiKey   string
		wantAuth string // "" 表示不设
	}{
		{llmschema.AuthModeNone, "", ""},
		{llmschema.AuthModeCustomHeaders, "", ""},
		{llmschema.AuthModeCustomHeaders, "k", "Bearer k"},
		{llmschema.AuthModeAPIKey, "k", "Bearer k"},
	}
	for _, tc := range cases {
		req, _, err := BuildHTTPRequestWithAuth(context.Background(), "http://x", tc.apiKey,
			map[string]any{"model": "m", "messages": []any{}}, nil, nil, false, "", false, tc.authMode)
		if err != nil {
			t.Fatal(err)
		}
		got := req.Header.Get("Authorization")
		if got != tc.wantAuth {
			t.Errorf("auth_mode=%v key=%q: Authorization=%q want %q", tc.authMode, tc.apiKey, got, tc.wantAuth)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/openai/ -run TestBuildHTTPRequest_鉴权头 -v`
Expected: FAIL（无 BuildHTTPRequestWithAuth；现在恒设 Bearer）

- [ ] **Step 3: 实现**

`BuildHTTPRequest` 增变体 `BuildHTTPRequestWithAuth(..., authMode llmschema.AuthMode)`（原函数签名保留为包装：默认按 `AuthModeAPIKey` 调变体，调用点零改动；新增参数仅 openai/siliconflow/affinity/intellirouter 四处后续按需传 config.AuthMode——本任务把四处调用改为传 `c.ClientConfig.AuthMode`）。Authorization 注入改：

```go
	// 鉴权头按 auth_mode：none / custom_headers(空 key) 省略；api_key 模式缺 key 用 EMPTY 占位。
	// Python: openai_model_client.py:298-305 _should_omit_authorization + :326-330 _resolved_api_key_for_config
	if !shouldOmitAuthorization(authMode, apiKey) {
		resolved := apiKey
		if strings.TrimSpace(resolved) == "" {
			resolved = "EMPTY"
		}
		req.Header.Set("Authorization", "Bearer "+resolved)
	}
```

```go
// shouldOmitAuthorization 判是否省略 Authorization 头。
//
// Python: openai_model_client.py:298-305 _should_omit_authorization
func shouldOmitAuthorization(authMode llmschema.AuthMode, apiKey string) bool {
	switch authMode {
	case llmschema.AuthModeNone, llenschema.AuthModeOpenAIAccountOAuth:
		return true
	case llmschema.AuthModeCustomHeaders:
		return strings.TrimSpace(apiKey) == ""
	default:
		return false
	}
}
```

（注意示例里的 `llenschema` 是笔误防御——写代码时统一 `llmschema`。）intellirouter/inference_affinity 请求头路径若自有 Authorization 设置，同样套 `shouldOmitAuthorization`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/... -run TestBuildHTTPRequest_鉴权头 -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A internal/agentcore/foundation/llm/model_clients/
git commit -m "feat(llm): 鉴权头按 auth_mode 省略/EMPTY 占位（C-D2，C-07 后半）"
```

---

## Task 13: C-D3 — `Temperature` 改 `*float64` + BuildRequestParams nil 不下发

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/config.go:58-59/373-377/414-424`
- Modify: `internal/agentcore/foundation/llm/model_clients/base_client.go:336-370`
- Modify: 调用点 `init_model.go:101`、`query_rewriter.go:170`、`im_outbound.go:419`、`im_inbound.go:824`、`wiki_tools.go:631-641`、`app_web_handlers.go:1316`、`agent_config.go` 等（编译器驱动全改）

- [ ] **Step 1: 写失败测试**

```go
// schema/config_test.go 追加
// TestNewModelRequestConfig_无默认 未配置时 Temperature/TopP 均为 nil（对齐 config.py:262-266）
func TestNewModelRequestConfig_无默认(t *testing.T) {
	cfg := NewModelRequestConfig()
	if cfg.Temperature != nil {
		t.Errorf("Temperature 默认应为 nil，实际 %v", *cfg.Temperature)
	}
	if cfg.TopP != nil {
		t.Errorf("TopP 默认应为 nil，实际 %v", *cfg.TopP)
	}
}

// base_client_test.go 追加
// TestBuildRequestParams_采样参数nil不下发 params/model_config 均未设 → 请求里无 temperature/top_p；显式 0 下发 0
func TestBuildRequestParams_采样参数nil不下发(t *testing.T) {
	e := &BaseClientEmbed{ModelConfig: llmschema.NewModelRequestConfig(), ClientConfig: &llmschema.ModelClientConfig{ClientProvider: "OpenAI", Timeout: 60}, clientName: "t"}
	dict := []map[string]any{{"role": "user", "content": "hi"}}
	params, err := e.BuildRequestParams(context.Background(), dict, &InvokeParams{Model: "m"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := params["temperature"]; exists {
		t.Error("未配置不应下发 temperature")
	}
	if _, exists := params["top_p"]; exists {
		t.Error("未配置不应下发 top_p")
	}

	// 显式 0 生效（此前被 !=0 判空吞掉）
	zero := 0.0
	e2 := &BaseClientEmbed{ModelConfig: llmschema.NewModelRequestConfig(llmschema.WithTemperaturePtr(&zero)), ClientConfig: &llmschema.ModelClientConfig{ClientProvider: "OpenAI", Timeout: 60}, clientName: "t"}
	params2, _ := e2.BuildRequestParams(context.Background(), dict, &InvokeParams{Model: "m"}, false)
	if v, _ := params2["temperature"].(float64); v != 0 {
		t.Errorf("显式 0 应下发 0，实际 %v", params2["temperature"])
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/... -run 'TestNewModelRequestConfig_无默认|TestBuildRequestParams_采样' -v`
Expected: FAIL（默认 0.95/0.1；编译错误 WithTemperaturePtr undefined）

- [ ] **Step 3: 实现 + 编译器驱动传播**

`schema/config.go`：`Temperature float64` → `Temperature *float64 \`json:"temperature,omitempty"\``；`WithTemperature(temp *float64)` 改指针签名，新增 `WithTemperatureValue(v float64)`（取地址便捷形，供既有值调用点机械替换）；`NewModelRequestConfig` 删两默认；`config.go:514` knownKeys 不变。`base_client.go:336-370` 改：

```go
	// 采样参数：params > model_config，nil 不下发（对齐 base_model_client.py:587-605）
	if params.Temperature == nil && e.ModelConfig != nil {
		if t := e.ModelConfig.Temperature; t != nil {
			reqParams["temperature"] = *t
		}
	}
	if params.TopP == nil && e.ModelConfig != nil && e.ModelConfig.TopP != nil {
		reqParams["top_p"] = *e.ModelConfig.TopP
	}
	// 方法参数覆盖 model_config
	if params.Temperature != nil {
		reqParams["temperature"] = *params.Temperature
	}
	if params.TopP != nil {
		reqParams["top_p"] = *params.TopP
	}
```

（max_tokens/stop 分支保持现状不动。）调用点替换策略：`WithTemperature(0.0)` → `WithTemperatureValue(0.0)`；`modelConfig.Temperature = 0`（app_web_handlers.go:1316）→ `modelConfig.Temperature = ptrOf(0.0)`（就地 `v := 0.0; modelConfig.Temperature = &v`）；`init_model.go` 的 `cfg.temperature` 字段改 `*float64`（`WithInitTemperature(t *float64)` 同步）；`pool.go:360` 的 `cfg.Temperature = f` 改 `cfg.Temperature = &f`。既有断言默认 0.95 的测试（`config_test.go` 等）按新语义改写：显式传 `WithTemperatureValue(0.95)` 断言，或删「默认」断言。**主链路不变**：`deep_adapter.go:2185` 已显式 0.95（改 `WithTemperatureValue(temperature)`），`default_models.go:189` 的 map 值不动（进 ModelRequestConfig 时走值拷贝）。

- [ ] **Step 4: 编译 + 全量测试**

Run: `go build ./... && go vet ./internal/... && go test ./internal/agentcore/... 2>&1 | grep -v "^ok" | head -20`
Expected: 编译过、无 FAIL 行（跨包测试若红：读断言语义按「nil 不下发」逐个修正，如 observability 的 `data.Temperature != nil` 已是指针语义应仍绿）

- [ ] **Step 5: 提交**

```bash
git add -A internal/
git commit -m "refactor(llm): Temperature 改指针，采样参数 nil 不下发（C-D3，C-09）"
```

---

## Task 14: C-F — finish_reason 非流式对齐

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/parse_response.go:122-126`
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/parse_response_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestParseResponse_FinishReason_对齐 provider 有值原样保留；空值按 tool_calls/stop 兜底
// Python: openai_model_client.py:2877-2879
func TestParseResponse_FinishReason_对齐(t *testing.T) {
	mkResp := func(finish string, withTools bool) *ChatCompletionResponse {
		msg := &ResponseMessage{Content: ptrOf("hi")}
		if withTools {
			msg.ToolCalls = []ResponseToolCall{{Type: "function", Function: ResponseFunction{Name: "f", Arguments: "{}"}}}
		}
		fr := finish
		return &ChatCompletionResponse{Choices: []ResponseChoice{{Message: msg, FinishReason: fr}}}
	}
	// stop + tools → 保留 stop（不改写）
	resp := ParseResponse(mkResp("stop", true), llmschema.NewModelRequestConfig(), nil)
	if resp.FinishReason != "stop" {
		t.Errorf("stop+tools 应原样保留 stop，实际 %q", resp.FinishReason)
	}
	// 空 + tools → tool_calls
	resp = ParseResponse(mkResp("", true), llmschema.NewModelRequestConfig(), nil)
	if resp.FinishReason != "tool_calls" {
		t.Errorf("空+tools 应兜底 tool_calls，实际 %q", resp.FinishReason)
	}
	// 空 + 无 tools → stop
	resp = ParseResponse(mkResp("", false), llmschema.NewModelRequestConfig(), nil)
	if resp.FinishReason != "stop" {
		t.Errorf("空无 tools 应兜底 stop，实际 %q", resp.FinishReason)
	}
}
```

（`ptrOf` 用既有测试助手；没有就 `s := "hi"; &s` 就地取址。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/openai/ -run TestParseResponse_FinishReason -v`
Expected: FAIL（stop+tools 被改写成 tool_calls）

- [ ] **Step 3: 实现**

```go
	// finish_reason 归一：provider 有值原样保留；空值按 tool_calls/stop 兜底。
	// Python: openai_model_client.py:2877-2879（注 2779-2786 的 docstring）
	finishReason := choice.FinishReason
	if finishReason == "" {
		if len(toolCalls) > 0 {
			finishReason = "tool_calls"
		} else {
			finishReason = "stop"
		}
	}
```

删除原 `if len(toolCalls) > 0 && finishReason == "stop" { finishReason = "tool_calls" }`。

- [ ] **Step 4: 跑测试确认通过 + 回归**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/openai/ -v 2>&1 | tail -5`
Expected: PASS（既有断言 `finish_reason == "tool_calls"`（stop+tools 场景）的用例按「保留 stop」语义修正）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/openai/
git commit -m "fix(llm): finish_reason 非流式双向对齐（C-F，C-03）"
```

---

## Task 15: C-G — router Close + 显式驱逐

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/intellirouter/router.go:537-568/933`
- Modify: `internal/agentcore/foundation/llm/model_clients/intellirouter/router_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestResetRouterCache_停旧健康检查 显式驱逐后旧 health checker goroutine 退出、条目删除
func TestResetRouterCache_停旧健康检查(t *testing.T) {
	routerCache = make(map[string]*ReliableRouter)
	cfg := &IntelliRouterClientConfig{Strategy: "round-robin", NumRetries: 1,
		EnableHealthCheck: true, HealthCheckInterval: 0.05}
	r := GetOrCreateRouter(cfg)
	if r == nil {
		t.Fatal("router nil")
	}
	key := makeRouterKey(cfg)
	if routerCache[key] == nil {
		t.Fatal("应已入缓存")
	}

	ResetRouterCache(key)

	if _, exists := routerCache[key]; exists {
		t.Error("驱逐后条目应删除")
	}
	// health checker goroutine 退出（轮询，最多 2s）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.healthChecker.mu.Lock()
		stopped := r.healthChecker.stopped
		r.healthChecker.mu.Unlock()
		if stopped {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("health checker 未在 2s 内停止")
}

// TestResetRouterCache_不存在键noOp
func TestResetRouterCache_不存在键noOP(t *testing.T) {
	routerCache = make(map[string]*ReliableRouter)
	ResetRouterCache("no-such-key") // 不应 panic
}
```

（`stopped` 字段若不存在于 HealthChecker，读其现有 Stop 机制的等价信号——实现时按实际结构调整断言，语义锚定「Stop 之后的 goroutine 退出」。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/intellirouter/ -run TestResetRouterCache -v`
Expected: FAIL（undefined: ResetRouterCache）

- [ ] **Step 3: 实现**

```go
// Close 关闭 router 持有的后台资源（health checker + idle 连接）。
//
// Python: intelli_router_model_client.py:691 await entry.router.close()
func (r *ReliableRouter) Close() {
	r.StopHealthChecker()
	if r.httpClient != nil {
		r.httpClient.CloseIdleConnections()
	}
}

// ResetRouterCache 显式驱逐并关闭缓存中的 router（配置换绑时调用）。
//
// 锁外 best-effort Close + 失败仅告警（对齐 L687-698）。loop 归属核对不迁移——
// Go 无事件循环，net 池进程级共享，该机制防护的失败模式不存在（设计文档偏差 D-5）。
//
// Python: intelli_router_model_client.py:675-700 _ensure_router_for_current_loop 的驱逐段
func ResetRouterCache(key string) {
	routerCacheLock.Lock()
	old, exists := routerCache[key]
	if exists {
		delete(routerCache, key)
	}
	routerCacheLock.Unlock()
	if !exists {
		return
	}
	old.Close()
}
```

`ReliableRouter` 增持 http client 引用字段（若请求路径直接用共享 Transport，则 `Close` 改调 `model_clients` 池的 `CloseIdleTransport(verifySSL, sslCert)` 单桶关闭——按 Task 4/5 接线后的实际结构择一实现，语义锚定「关掉该 router 可独占引用的连接」）。`GetOrCreateRouter` 不改（键变自然新建；显式换绑由调用方先 `ResetRouterCache`）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/intellirouter/ -v 2>&1 | tail -5`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/intellirouter/
git commit -m "feat(llm): router Close 与显式驱逐（C-G，C-13）"
```

---

## Task 16: 收尾 — defect-plan 回填 + IMPLEMENTATION_PLAN 登记 + 全量验收

**Files:**
- Modify: `docs/sync/defect-plan-2026-11-04.md`（C 组 15 行状态 + §0 摘要）
- Modify: `IMPLEMENTATION_PLAN.md`（登记 C-A~C-G）

- [ ] **Step 1: 回填 defect-plan C 组**

C-01~C-14 逐行：修复行改 `✅`（附提交号）；C-02/C-11/C-12 改 `➖`（附 Go 位置：`base_client.go:406-417` 阻塞 channel / `model_anomaly.go:860-1010` / `:552/584/313`）；C-06 并入 C-04 行标注；C-08 的「缺陷（Go 语义）」列文字修正为「不剥已带 /chat/completions 导致双段（非硬编码补 /v1）」；§0 摘要「本轮完成 C 组 11 行」。

- [ ] **Step 2: IMPLEMENTATION_PLAN 登记**

仿 B 组 24-A 先例新增「24-B LLM 客户端 C 组对齐（C-A~C-G）」小节：七个批次行（C-A1/A2/A3/B/C/D/E/F/G 按实际提交数），完成打 ✅；引用设计文档与计划路径。

- [ ] **Step 3: 全量本地验收**

```bash
go build ./... && go vet ./...
go test -race ./internal/agentcore/foundation/llm/... ./internal/agentcore/context_engine/... 2>&1 | tail -5
```

Expected: 全绿（race 用例含 Task 4 池并发、Task 6 长流）。

- [ ] **Step 4: 提交并推送，监控远端 CI（规则 6）**

```bash
git add docs/sync/defect-plan-2026-11-04.md IMPLEMENTATION_PLAN.md
git commit -m "docs: C 组缺陷处置回填与计划登记（C-A~C-G 完结）"
git push origin main
gh run watch $(gh run list --limit 1 --json databaseId -q '.[0].databaseId')
```

Expected: Build/Lint/Unit/Integration 四 job 全绿；任何红灯回修再推，直到全绿。

---

## Self-Review 记录

- **Spec 覆盖**：设计文档 §1→Task 4-6；§2→Task 1-3；§3→Task 9-10；§4→Task 11-13；§5→Task 7-8；§6→Task 14；§7→Task 15；§8 删除清单分布在 Task 5（buildHTTPClient×3）/8（encodeJSON 旧路径）/10（deepseek 包+doc.go）/11（WithSkipValidate）；§9 集成用例在 Task 6（SSE 真流）。无缺口。
- **占位符扫描**：Task 5 Step 3 的 `encodeJSONChecked` 有「本任务先别名、Task 7 替换」的显式两段安排（非 TBD）；Task 12 示例代码含一处明示的笔误防御标注。无未定义引用。
- **类型一致性**：`GetSharedTransport(verifySSL, sslCert)` 在 Task 4 定义、Task 5/15 使用一致；`BuildUsageMetadata(map[string]any, string)` Task 2 定义、Task 3 三客户端使用一致；`AuthMode` 枚举 Task 11 定义、Task 12 使用一致；`WithTemperatureValue`/`WithTemperaturePtr` Task 13 内自洽。
- **计数核对**：任务数 16；批次 C-A3+A2+A1+B3+C2+D3+E2+F+G = 设计 §9 的批次全覆盖。
