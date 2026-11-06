# 上下文引擎 B 组缺陷对齐实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `docs/sync/defect-plan-2026-11-04.md` §3.B 中 8 组确认为缺陷的项（B-02/B-03/B-04/B-05/B-06/B-07/B-09/B-10/B-12/B-13/B-14/B-16）按上游 agent-core `5a35c39f` 语义在 Go 侧落地，并删除 Go 自创的窗口与水位比例机制。

**Architecture:** 分 6 个可独立提交、独立 CI 绿的批次（C1~C6，对应 `IMPLEMENTATION_PLAN.md` 24-A）：先修取消态落盘（数据丢失），再统一「模型窗口来源 + 压缩阈值比例化 + 预算 guard」这一条主链（最大改动面），随后是计数口径与图像计费、溢出恢复链、零散状态字段，最后整体迁移 `context_engine/usage/` 子系统并把 `context.usage` 事件切到 `context-usage.v1` 快照。

**Tech Stack:** Go 1.26（`context.WithoutCancel`、`net/http`、`httptest`）、标准库 `image`、tiktoken-go、zerolog（`internal/common/logger`）、SQLite KV（checkpointer）。

**上游参考（本地已更新至基线）：** `~/agent-core/openjiuwen/core/context_engine/**`（HEAD = `5a35c39f`）、`~/jiuwenswarm-develop/jiuwenswarm/resources/config.yaml`（HEAD = `a2abceccf`）。

**设计文档：** `docs/superpowers/specs/2026-11-06-context-engine-b-group-alignment-design.md`

---

## 批次总览

| 批次 | 覆盖缺陷 | 任务 | 前置 | 提交信息前缀 |
|------|---------|------|------|-------------|
| C1 | B-03、B-04 | Task 1 | — | `fix(context):` |
| C2 | B-02、B-14 | Task 2~14 | — | `refactor(context):` / `fix(context):` |
| C3 | B-12、B-05 | Task 15~19 | C2（TokenCounter 接口稳定） | `fix(context):` |
| C4 | B-07、B-09 | Task 20~23 | C2（`CompressContext` 语义稳定） | `fix(context):` |
| C5 | B-13、B-16 | Task 24~25 | — | `fix(context):` |
| C6 | B-06、B-10 | Task 26~32 | C2（窗口来源）、C5（状态字段） | `feat(context):` |

**通用约定**

- 每个 task 完成即提交；提交前跑 `go build ./... && go vet ./...`。
- 只改本 task 列出的文件；发现必须额外改动时先在本计划追加说明。
- Go 注释一律中文；新增文件必须同步同包 `doc.go` 的文件树（项目规则 1）。
- 删除字段/函数时，同一提交内清掉全部引用（项目规则 8）：`rg -n "<符号>" internal/ resources/` 必须 0 命中（测试与设计文档除外）。
- `gofmt` 若不在 PATH，用 `$(go env GOROOT)/bin/gofmt -l <改动文件>`。

---

## C1：取消态落盘（B-03、B-04）

### Task 1: 取消后清洗结果必须落盘

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_helpers.go:152-160`（`saveContexts`）
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go:380-395`（invoke 的 finally）、`:600-620`（streamImpl 的 finally）、`:1144-1180`（`handleContextAbort`）
- Test: `internal/agentcore/single_agent/agents/react_abort_test.go`（新建）

**背景**：`handleContextAbort` 与 finally 都用调用方 ctx；取消时 `SaveContexts → checkpointer.Save(ctx) → kvStore.Pipeline(ctx).Execute(ctx)` 立即失败，`saveContexts` 只 warn 吞掉 → 清洗后的上下文没落盘。上游用 `asyncio.shield` 规避（`react_agent.py:1906-1930`）。

- [ ] **Step 1: 写失败用例**

新建 `internal/agentcore/single_agent/agents/react_abort_test.go`：

```go
package agents

import (
	"context"
	"errors"
	"testing"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

type detachedSaveContext struct {
	ceiface.ModelContext
	msgs       []llmschema.BaseMessage
	saveCalled bool
}

func (d *detachedSaveContext) GetMessages(_ int, _ bool) ([]llmschema.BaseMessage, error) {
	return d.msgs, nil
}

func (d *detachedSaveContext) SetMessages(msgs []llmschema.BaseMessage, _ bool) {
	d.msgs = msgs
}

func (d *detachedSaveContext) SaveState() map[string]any {
	d.saveCalled = true
	return map[string]any{"saved": true}
}

// TestSaveContextsDetached_取消态仍落盘 断言取消 ctx 下仍能把清洗结果写回 session。
func TestSaveContextsDetached_取消态仍落盘(t *testing.T) {
	engine := newFakeContextEngine(t)   // 同包已有测试助手，见 react_agent_test.go 的 fake 定义；若无则用 ceiface 桩实现
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立刻取消

	agent := &ReActAgent{contextEngine: engine}
	sess := newFakeSession(t) // 同包测试助手，见 react_invoke_test.go

	agent.saveContextsDetached(ctx, sess)

	state, _ := sess.GetState("context")
	if state == nil {
		t.Fatalf("取消态下上下文未落盘：state 为空")
	}
}
```

> 若同包没有 `newFakeContextEngine` / `newFakeSession`，在本文件内按 `ceiface.ContextEngine` 与 `sessioninterfaces.SessionFacade` 接口实现最小桩即可（参考 `react_agent_test.go` 现有桩的写法；桩只需实现 `SaveContexts`、`GetSessionID`、`UpdateState`、`GetState`）。

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/agentcore/single_agent/agents/ -run TestSaveContextsDetached_取消态仍落盘 -v
```

Expected: `undefined: saveContextsDetached`（编译失败即“红灯”）。

- [ ] **Step 3: 实现 detached 助手并替换三处调用**

`react_helpers.go` 在 `saveContexts` 之后新增：

```go
// saveContextsDetached 以脱离取消信号的 ctx 持久化上下文。
//
// 取消/异常路径必须写完：调用方 ctx 已取消时，KV/SQL 写入会因
// ExecContext(canceled) 立刻失败，导致本轮清洗结果丢失，下一轮从旧
// checkpoint 重建（等价 Python 的 asyncio.shield(self._save_contexts_on_cancel(session))）。
func (a *ReActAgent) saveContextsDetached(ctx context.Context, sess sessioninterfaces.SessionFacade) {
	if a.contextEngine == nil || sess == nil {
		return
	}
	detached := context.WithoutCancel(ctx)
	if _, err := a.contextEngine.SaveContexts(detached, sess, nil); err != nil {
		logger.Warn(logComponent).Str("event_type", "save_contexts_error").Err(err).Msg("savecontext failed")
	}
}
```

`react_invoke.go` 三处替换（`handleContextAbort` 内 `a.saveContexts(ctx, sess)`、invoke finally、streamImpl finally）：

```go
a.saveContextsDetached(ctx, sess)
```

同时把 finally 里的 `as.Commit(ctx)` / `agentSess.Commit(ctx)` 换成 detached：

```go
_ = as.Commit(context.WithoutCancel(ctx))
```

`handleContextAbort` 内 `as.Commit(ctx)` 同样改 `context.WithoutCancel(ctx)`，并在函数注释补一句“落盘用脱离取消的 ctx，对齐 Python asyncio.shield”。

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/agentcore/single_agent/agents/ -run 'TestSaveContextsDetached|TestReActAgent_' -v
```

Expected: PASS（含既有 `TestReActAgent_saveContexts_*`）。

- [ ] **Step 5: 提交**

```bash
go build ./... && go vet ./internal/agentcore/single_agent/...
git add internal/agentcore/single_agent/agents/react_helpers.go internal/agentcore/single_agent/agents/react_invoke.go internal/agentcore/single_agent/agents/react_abort_test.go
git commit -m "fix(context): 取消后以脱离取消的 ctx 持久化清洗结果（B-03/B-04）"
```

---

## C2：窗口来源与压缩预算（B-02、B-14）

### Task 2: `ModelRequestConfig` 增加 `context_window` 且不外发

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/config.go:55-68`
- Test: `internal/agentcore/foundation/llm/schema/config_window_test.go`（新建）

- [ ] **Step 1: 写失败用例**

```go
func TestModelRequestConfig_ContextWindow不外发(t *testing.T) {
	cfg := NewModelRequestConfig(WithModelName("m"), WithContextWindow(262144))
	if cfg.ContextWindow == nil || *cfg.ContextWindow != 262144 {
		t.Fatalf("ContextWindow = %v, want 262144", cfg.ContextWindow)
	}
	if _, ok := cfg.Extra["context_window"]; ok {
		t.Fatalf("context_window 不得进入 Extra（会被当厂商参数下发）")
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./internal/agentcore/foundation/llm/schema/ -run TestModelRequestConfig_ContextWindow不外发 -v
```

Expected: `undefined: WithContextWindow`。

- [ ] **Step 3: 实现字段与选项**

`config.go` 的 `ModelRequestConfig` 增：

```go
	// ContextWindow 模型支持的上下文窗口总长（token），内部元数据，不参与厂商请求
	ContextWindow *int `json:"context_window,omitempty"`
```

并新增选项函数（与既有 `WithMaxTokens` 同风格）：

```go
// WithContextWindow 设置模型上下文窗口（内部元数据，不随请求下发）。
//
// 对齐上游 ModelRequestConfig.context_window：base_model_client 组装请求参数时
// 显式 exclude，本仓请求参数由 base_client.go 以 map 拼装且只合并 Extra，
// 因此该字段天然不外发（由 config_window_test.go 固化）。
func WithContextWindow(n int) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) {
		if n > 0 {
			c.ContextWindow = &n
		}
	}
}
```

- [ ] **Step 4: 运行确认通过**

```bash
go test ./internal/agentcore/foundation/llm/schema/ -run TestModelRequestConfig_ContextWindow不外发 -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/schema/config.go internal/agentcore/foundation/llm/schema/config_window_test.go
git commit -m "feat(llm): ModelRequestConfig 增加 context_window 内部元数据（B-14）"
```

### Task 3: `ContextEngineConfig` 增 override/OpenRouter，删两个水位比例字段

**Files:**
- Modify: `internal/agentcore/context_engine/schema/config.go:20-75`
- Test: `internal/agentcore/context_engine/schema/config_test.go`

- [ ] **Step 1: 写失败用例**

```go
func TestContextEngineConfig_窗口override与比例字段移除(t *testing.T) {
	cfg := NewContextEngineConfig()
	cfg.ModelContextWindowTokensOverride = 131072
	cfg.EnableOpenRouterModelContextWindowTokens = true
	cfg.OpenRouterRequestTimeout = 3
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	cfg.ModelContextWindowTokensOverride = -1
	if err := cfg.Validate(); err == nil {
		t.Fatalf("负 override 应校验失败")
	}
}
```

- [ ] **Step 2: 运行确认失败**：`go test ./internal/agentcore/context_engine/schema/ -run TestContextEngineConfig_窗口override与比例字段移除 -v` → `unknown field ModelContextWindowTokensOverride`。

- [ ] **Step 3: 改结构体**

```go
	// ModelContextWindowTokensOverride 当前选中模型配置声明的上下文窗口（低优先于 ContextWindowTokens）
	ModelContextWindowTokensOverride int `json:"model_context_window_tokens_override"`
	// EnableOpenRouterModelContextWindowTokens 是否从 OpenRouter 拉取模型窗口表
	EnableOpenRouterModelContextWindowTokens bool `json:"enable_openrouter_model_context_window_tokens"`
	// OpenRouterRequestTimeout OpenRouter 拉取超时（秒），<=0 用默认 3
	OpenRouterRequestTimeout float64 `json:"openrouter_request_timeout"`
```

删除 `CompressionTriggerRatio` / `CompressionTargetRatio` 两个字段（规则 8）。`Validate()` 增：

```go
	if c.ModelContextWindowTokensOverride < 0 {
		return fmt.Errorf("model_context_window_tokens_override 不能为负数，当前值: %d", c.ModelContextWindowTokensOverride)
	}
```

- [ ] **Step 4: 运行通过** + `go build ./...`（此时 `deep_adapter.go` 与 rail 会因引用已删字段而编译失败，属预期，Task 7/9 修；本 task 先 `go build ./internal/agentcore/context_engine/...` 通过）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/schema/config.go internal/agentcore/context_engine/schema/config_test.go
git commit -m "refactor(context): 引擎配置增窗口 override，删水位比例字段（B-02/B-14）"
```

### Task 4: 内置窗口表重写 + OpenRouter 拉取

**Files:**
- Modify: `internal/agentcore/context_engine/context/context_utils.go:1-135`
- Test: `internal/agentcore/context_engine/context/context_utils_window_test.go`（新建）

- [ ] **Step 1: 写失败用例**

```go
func TestBuildModelContextWindowTokens_用户值覆盖拉取值(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/qwen3-max","context_length":262144},{"id":"gpt-x/vendor/qwen3-max","context_length":1}]}`))
	}))
	defer srv.Close()
	resetOpenRouterCache()
	overrideOpenRouterURL(srv.URL)

	got := BuildModelContextWindowTokens(map[string]int{"qwen3-max": 1000}, true, 3)
	if got["qwen3-max"] != 1000 {
		t.Fatalf("用户显式值应覆盖拉取值，got %d", got["qwen3-max"])
	}
	if got["vendor/qwen3-max"] != 262144 {
		t.Fatalf("带前缀 id 应保留，got %v", got)
	}
}

func TestResolveContextMax_override优先于映射与内置表(t *testing.T) {
	if got := ResolveContextMax("qwen-max", 131072, map[string]int{"qwen-max": 32000}); got != 131072 {
		t.Fatalf("fallback 应最高优先，got %d", got)
	}
}
```

- [ ] **Step 2: 运行确认失败** → `undefined: BuildModelContextWindowTokens`。

- [ ] **Step 3: 实现**

1. `ModelDefaultContextWindowTokens` 按上游 `MODEL_DEFAULT_CONTEXT_WINDOW_TOKENS` 整体替换（数值见设计文档 §2.3）。
2. 新增（`context_utils.go`）：

```go
var (
	openRouterModelsURL     = "https://openrouter.ai/api/v1/models"
	openRouterCacheTTL      = time.Hour
	openRouterTokensMu      sync.Mutex
	openRouterTokens        map[string]int
	openRouterTokensFetched time.Time
)

// BuildModelContextWindowTokens 合并 OpenRouter 拉取表与用户显式表（用户值优先）。
//
// Python: ContextUtils.build_model_context_window_tokens()
func BuildModelContextWindowTokens(user map[string]int, enable bool, timeoutSeconds float64) map[string]int {
	resolved := make(map[string]int)
	if enable {
		for k, v := range fetchOpenRouterContextWindows(timeoutSeconds) {
			resolved[k] = v
		}
	}
	for k, v := range user {
		if v > 0 {
			resolved[k] = v
		}
	}
	return resolved
}

// fetchOpenRouterContextWindows 拉取 OpenRouter 模型窗口表，进程级 TTL 缓存，失败沿用旧值。
//
// 解析 {"data":[{"id","context_length"}]}；带 "/" 的 id 额外产出无前缀别名，
// 同名别名冲突时丢弃该别名（避免把两家厂商的同名模型混为一谈）。
func fetchOpenRouterContextWindows(timeoutSeconds float64) map[string]int {
	openRouterTokensMu.Lock()
	if !openRouterTokensFetched.IsZero() && time.Since(openRouterTokensFetched) < openRouterCacheTTL {
		cached := copyIntMap(openRouterTokens)
		openRouterTokensMu.Unlock()
		return cached
	}
	openRouterTokensFetched = time.Now()
	openRouterTokensMu.Unlock()

	timeout := timeoutSeconds
	if timeout <= 0 {
		timeout = 3
	}
	client := &http.Client{Timeout: time.Duration(timeout * float64(time.Second))}
	resp, err := client.Get(openRouterModelsURL)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("failed to fetch openrouter model context windows, use cached or built-in values")
		return copyIntMap(openRouterTokens)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		logger.Warn(logComponent).Int("status_code", resp.StatusCode).
			Msg("failed to fetch openrouter model context windows, use cached or built-in values")
		return copyIntMap(openRouterTokens)
	}

	var payload struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		logger.Warn(logComponent).Err(err).Msg("failed to parse openrouter models payload, use cached or built-in values")
		return copyIntMap(openRouterTokens)
	}

	parsed := make(map[string]int, len(payload.Data))
	aliases := make(map[string]int, len(payload.Data))
	ambiguous := make(map[string]struct{})
	for _, m := range payload.Data {
		if m.ID == "" || m.ContextLength <= 0 {
			continue
		}
		parsed[m.ID] = m.ContextLength
		idx := strings.Index(m.ID, "/")
		if idx < 0 {
			continue
		}
		alias := m.ID[idx+1:]
		if _, exists := aliases[alias]; exists {
			ambiguous[alias] = struct{}{}
			continue
		}
		aliases[alias] = m.ContextLength
	}
	for alias, tokens := range aliases {
		if _, dup := ambiguous[alias]; dup {
			continue
		}
		parsed[alias] = tokens
	}

	openRouterTokensMu.Lock()
	openRouterTokens = parsed
	openRouterTokensMu.Unlock()
	return copyIntMap(parsed)
}
```

`fetchOpenRouterContextWindows` 要求：TTL 内直接返回缓存副本；超时默认 3s；解析 `{"data":[{"id","context_length"}]}`，带 `/` 的 id 同时产出**无前缀别名且别名唯一**的键；任何错误只 `logger.Warn` 并返回缓存。同时导出两个测试钩子（同包私有即可）：

```go
func resetOpenRouterCache()            { /* 清空 map 与时间戳 */ }
func overrideOpenRouterURL(u string)   { openRouterModelsURL = u }
```

- [ ] **Step 4: 运行通过**

```bash
go test ./internal/agentcore/context_engine/context/ -run 'TestBuildModelContextWindowTokens|TestResolveContextMax' -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/context/context_utils.go internal/agentcore/context_engine/context/context_utils_window_test.go
git commit -m "feat(context): 内置窗口表对齐上游并支持 OpenRouter 拉取（B-14）"
```

### Task 5: 引擎与上下文的 `UpdateModelContext`

**Files:**
- Modify: `internal/agentcore/context_engine/engine.go`（新增方法，放在 `SaveContexts` 之后）
- Modify: `internal/agentcore/context_engine/context/session_model_context.go`（新增方法 + 构造期窗口优先级）
- Modify: `internal/agentcore/context_engine/interface/types.go:100-120`（接口声明）
- Test: `internal/agentcore/context_engine/context/session_model_context_window_test.go`（新建）

- [ ] **Step 1: 写失败用例**

```go
func TestUpdateModelContext_覆盖全局优先级(t *testing.T) {
	mc := newTestSessionModelContext() // 同包已有助手（session_model_context_test.go:118）
	mc.UpdateModelContext("qwen3-max", 262144)
	if got := mc.ContextMaxTokens(); got != 262144 {
		t.Fatalf("override 生效后窗口 = %d, want 262144", got)
	}
	mc.UpdateModelContext("qwen3-max", 0) // <=0 清除 override，回落到模型名解析
	if got := mc.ContextMaxTokens(); got == 262144 {
		t.Fatalf("清除 override 后不应沿用旧值")
	}
}

func TestUpdateModelContext_全局窗口优先(t *testing.T) {
	mc := newTestSessionModelContext(func(o *testContextOpts) { o.contextWindowTokens = 131072 })
	mc.UpdateModelContext("qwen3-max", 262144)
	if got := mc.ContextMaxTokens(); got != 131072 {
		t.Fatalf("全局 context_window_tokens 应最高优先，got %d", got)
	}
}
```

- [ ] **Step 2: 运行确认失败** → `mc.UpdateModelContext undefined`。

- [ ] **Step 3: 实现**

`SessionModelContext` 新增字段 `globalContextWindowTokens int`（构造时保存）与 `modelContextWindowTokensOverride int`；新增：

```go
// UpdateModelContext 刷新选中模型的上下文元数据（全局窗口仍最高优先）。
//
// Python: SessionModelContext.update_model_context()
func (mc *SessionModelContext) UpdateModelContext(modelName string, contextWindowTokens int) {
	mc.modelName = modelName
	mc.modelContextWindowTokensOverride = 0
	if contextWindowTokens > 0 {
		mc.modelContextWindowTokensOverride = contextWindowTokens
	}
	if mc.globalContextWindowTokens > 0 {
		mc.contextWindowTokens = mc.globalContextWindowTokens
	} else {
		mc.contextWindowTokens = mc.modelContextWindowTokensOverride
	}
}

// contextWindowFallback 返回 resolve_context_max 的 fallback 参数（global 优先，其次 override）。
func (mc *SessionModelContext) contextWindowFallback() int {
	if mc.globalContextWindowTokens > 0 {
		return mc.globalContextWindowTokens
	}
	return mc.modelContextWindowTokensOverride
}
```

所有 `ResolveContextMax(mc.modelName, mc.contextWindowTokens, ...)` 调用点的 fallback 改为 `mc.contextWindowFallback()`。

`engine.go` 新增（接口同步声明）：

```go
// UpdateModelContext 把选中模型元数据推给引擎配置与池内全部上下文（不重建上下文）。
//
// Python: ContextEngine.update_model_context()
func (ce *contextEngine) UpdateModelContext(modelName string, contextWindowTokens int) {
	ce.mu.Lock()
	ce.config.ModelName = modelName
	ce.config.ModelContextWindowTokensOverride = 0
	if contextWindowTokens > 0 {
		ce.config.ModelContextWindowTokensOverride = contextWindowTokens
	}
	targets := make([]iface.ModelContext, 0, len(ce.contextPool))
	for _, mc := range ce.contextPool {
		targets = append(targets, mc)
	}
	ce.mu.Unlock()

	for _, mc := range targets {
		if updater, ok := mc.(interface {
			UpdateModelContext(string, int)
		}); ok {
			updater.UpdateModelContext(modelName, contextWindowTokens)
		}
	}
}
```

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/context_engine/... -run TestUpdateModelContext -v`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/engine.go internal/agentcore/context_engine/interface/types.go internal/agentcore/context_engine/context/session_model_context.go internal/agentcore/context_engine/context/session_model_context_window_test.go
git commit -m "feat(context): 新增 UpdateModelContext 热切换窗口元数据（B-14）"
```

### Task 6: `ReActAgent` 装配模型名与窗口、暴露热切换入口

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_prompt.go:54-68`（`Configure`）
- Modify: `internal/agentcore/single_agent/agents/react_agent.go`（新增 `UpdateModelContext`，放 `SetContextEngine` 之后）
- Test: `internal/agentcore/single_agent/agents/react_window_test.go`（新建）

- [ ] **Step 1: 写失败用例**

```go
func TestConfigure_从模型配置写入窗口override(t *testing.T) {
	window := 262144
	cfg := newReActConfigWithModel(t, "qwen3-max", &window) // 同包助手：构造 ReActAgentConfig + ModelRequestConfig
	agent := NewReActAgent(...)                             // 按现有构造器调用
	if err := agent.Configure(context.Background(), cfg); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if got := cfg.ContextEngineConfig.ModelContextWindowTokensOverride; got != 262144 {
		t.Fatalf("override = %d, want 262144", got)
	}
	if cfg.ContextEngineConfig.ModelName != "qwen3-max" {
		t.Fatalf("model_name 应从模型配置补齐，got %q", cfg.ContextEngineConfig.ModelName)
	}
}
```

- [ ] **Step 2: 运行确认失败**（override 仍为 0）。

- [ ] **Step 3: 实现**

`Configure` 里在 `a.config = cfg` 之前插入两步等价逻辑：

```go
	cfg = withContextEngineModelName(cfg)
	cfg = withContextEngineModelWindow(cfg)
```

同文件新增：

```go
// withContextEngineModelName 在未显式配置时用当前模型名补齐引擎 model_name。
//
// Python: ReActAgent._with_context_engine_model_name()
func withContextEngineModelName(cfg *saconfig.ReActAgentConfig) *saconfig.ReActAgentConfig {
	if strings.TrimSpace(cfg.ContextEngineConfig.ModelName) != "" {
		return cfg
	}
	name := ""
	if cfg.ModelConfig != nil {
		name = cfg.ModelConfig.ModelName
	}
	if name == "" {
		return cfg
	}
	cfg.ContextEngineConfig.ModelName = name
	return cfg
}

// withContextEngineModelWindow 把模型条目声明的 context_window 写入引擎 override（不清空已有显式值）。
//
// Python: ReActAgent._with_context_engine_model_window()
func withContextEngineModelWindow(cfg *saconfig.ReActAgentConfig) *saconfig.ReActAgentConfig {
	if cfg.ModelConfig == nil || cfg.ModelConfig.ContextWindow == nil {
		return cfg
	}
	window := *cfg.ModelConfig.ContextWindow
	if window <= 0 || cfg.ContextEngineConfig.ModelContextWindowTokensOverride == window {
		return cfg
	}
	cfg.ContextEngineConfig.ModelContextWindowTokensOverride = window
	return cfg
}
```

`ReActAgent` 新增：

```go
// UpdateModelContext 热切换当前模型与其上下文窗口（不重建上下文）。
//
// Python: ReActAgent.update_model_context()
func (a *ReActAgent) UpdateModelContext(modelName string, contextWindowTokens int) {
	if a.config != nil {
		if strings.TrimSpace(modelName) != "" {
			a.config.ContextEngineConfig.ModelName = modelName
		}
		if contextWindowTokens > 0 {
			a.config.ContextEngineConfig.ModelContextWindowTokensOverride = contextWindowTokens
		} else {
			a.config.ContextEngineConfig.ModelContextWindowTokensOverride = 0
		}
	}
	if a.contextEngine != nil {
		a.contextEngine.UpdateModelContext(modelName, contextWindowTokens)
	}
}
```

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/single_agent/agents/ -run TestConfigure_从模型配置写入窗口override -v`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_prompt.go internal/agentcore/single_agent/agents/react_agent.go internal/agentcore/single_agent/agents/react_window_test.go
git commit -m "feat(agent): Configure 装配模型窗口并支持热切换（B-14）"
```

### Task 7: 模型条目键迁移（adapter 侧）

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go:2019-2110`（`buildContextEngineConfig`）、`:2194-2200`（`buildModelFromEntry`）、`:2250-2262`（knownKeys）
- Test: `internal/swarm/server/adapter/deep_adapter_ceconfig_test.go`

- [ ] **Step 1: 写失败用例**（在已有 ceconfig 测试文件追加）

```go
func TestBuildContextEngineConfig_从model_config_obj取窗口(t *testing.T) {
	got := buildContextEngineConfigForTest(t, map[string]any{
		"models": map[string]any{"defaults": []any{map[string]any{
			"is_default":          true,
			"model_client_config": map[string]any{"model_name": "qwen3-max"},
			"model_config_obj":    map[string]any{"context_window": "256k"},
		}}},
	})
	if got.ModelContextWindowTokensOverride != 262144 {
		t.Fatalf("override = %d, want 262144", got.ModelContextWindowTokensOverride)
	}
	if len(got.ModelContextWindowTokens) != 0 {
		t.Fatalf("不再从模型条目填 name→tokens 映射，got %v", got.ModelContextWindowTokens)
	}
}
```

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**

1. 删除 `buildContextEngineConfig` 中收集 `mcc["max_input_tokens"]` 的整段（`defaultModelName` 只保留模型名解析用途），改为解析 `model_config_obj.context_window`（用 `config.Int`/`config.Float` 兼容 `"256k"` 形态——在 `internal/common/config` 内新增 `ParsePositiveTokens(any) (int, bool)`，支持纯数字、`256k`、`1m`，与上游 `common/context_window.py:parse_positive_int` 同口径，并配单测 `internal/common/config/context_window_test.go`）。
2. `buildModelFromEntry`：`mcc["max_output_tokens"]` → `mco["max_tokens"]`；新增 `WithContextWindow(...)`（读 `mco["context_window"]`，同样走 `ParsePositiveTokens`）。
3. knownKeys 排除表 `"max_input_tokens": {}, "max_output_tokens": {}` 改为 `"max_tokens": {}, "context_window": {}`。

- [ ] **Step 4: 运行通过**

```bash
go test ./internal/common/config/ -run TestParsePositiveTokens -v
go test ./internal/swarm/server/adapter/ -run 'TestBuildContextEngineConfig|TestBuildModel' -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/common/config/context_window.go internal/common/config/context_window_test.go internal/swarm/server/adapter/deep_adapter.go internal/swarm/server/adapter/deep_adapter_ceconfig_test.go
git commit -m "refactor(adapter): 模型条目改 context_window/max_tokens，删 max_input_tokens（B-14）"
```

### Task 8: Web 模型设置面板字段改名

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/web/app_web_handlers.go:960-975`（读取扁平化）、`:2925-2975`（写入校验）
- Test: `internal/swarm/gateway/channel_manager/web/app_web_handlers_models_test.go`（若不存在则新建）

- [ ] **Step 1: 写失败用例**：请求体带 `context_window: "256k"`、`max_tokens: 4096`，断言写回 `model_config_obj` 且 `model_client_config` 不再出现 `max_input_tokens`/`max_output_tokens`。

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**：两处键名与校验错误文案同步改；`context_window` 校验用 `config.ParsePositiveTokens`（允许字符串形态），`max_tokens` 用非负整数。

- [ ] **Step 4: 运行通过** → `go test ./internal/swarm/gateway/channel_manager/web/ -run 'Model' -v`

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/gateway/channel_manager/web/app_web_handlers.go internal/swarm/gateway/channel_manager/web/app_web_handlers_models_test.go
git commit -m "refactor(web): 模型设置面板改用 context_window/max_tokens（B-14）"
```

### Task 9: 删除 `applyCompressionRatios` / `shrinkThreshold`

**Files:**
- Modify: `internal/agentcore/harness/rails/context_engineer/context_processor_rail.go:145-170`（调用点）、`:625-670`（两个函数整体删除）
- Test: `internal/agentcore/harness/rails/context_engineer/context_processor_rail_preset_test.go`

- [ ] **Step 1: 改测试为失败用例**：删除/改写断言“阈值被收缩”的既有用例，新增

```go
func TestRail_不再做阈值收缩(t *testing.T) {
	r := newRailForTest(t, map[string]any{"compression_trigger_ratio": 0.5})
	cfg := processorConfigOf[*compressor.RoundLevelCompressorConfig](t, r) // 取 preset 中 round-level 配置
	if cfg.TriggerContextRatio != 0.8 {
		t.Fatalf("round-level 比例应来自 preset，got %v", cfg.TriggerContextRatio)
	}
}
```

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**：删除调用点与两个函数（含 `math` 若变为未使用则一并删 import）；`Init` 注释中“阈值收口 / spec §6.1”段落同步删除。

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/harness/rails/context_engineer/ -v`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/context_engineer/context_processor_rail.go internal/agentcore/harness/rails/context_engineer/context_processor_rail_preset_test.go
git commit -m "refactor(context): 删除 rail 层自研阈值收缩（B-02）"
```

### Task 10: 三个压缩器阈值比例化

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/dialogue_compressor.go:27-30,193,225-227,262-290`
- Modify: `internal/agentcore/context_engine/processor/compressor/current_round_compressor.go:27-30,482-505,560,599-620`
- Modify: `internal/agentcore/context_engine/processor/compressor/round_level_compressor.go:31-33,96-97,184-215,278,316-410`
- Test: 三个既有 `*_test.go` + 新增 `internal/agentcore/context_engine/processor/compressor/trigger_ratio_test.go`

- [ ] **Step 1: 写失败用例**

```go
func TestDialogueCompressor_按比例触发(t *testing.T) {
	cfg := NewDialogueCompressorConfig()
	if cfg.TriggerContextRatio != 0.8 || cfg.MinTargetContextRatio != 0.1 {
		t.Fatalf("默认比例应为 0.8/0.1，got %v/%v", cfg.TriggerContextRatio, cfg.MinTargetContextRatio)
	}
	mc := newWindowModelContext(t, 100000, 85000) // 见下方说明：本包内新增最小桩
	c := mustNewDialogueCompressor(t, cfg)
	triggered, err := c.TriggerAddMessages(context.Background(), mc, nil)
	if err != nil || !triggered {
		t.Fatalf("85%% 占用应触发 0.8 比例阈值，triggered=%v err=%v", triggered, err)
	}
}
```

桩说明（本文件内实现，勿引入生产依赖）：`newWindowModelContext` 返回一个实现 `ceiface.ModelContext` 的结构体，`TokenCounter()` 返回固定计数桩（对任意消息列表返回「已用 token」）、`GetMessages(0, true)` 返回 `nil`，并实现 `GetContextWindow` 之外的最小方法集（未用到的接口方法直接 `panic("unused")`，`-race` 下不影响）。`mustNewDialogueCompressor` 用 `NewDialogueCompressor(cfg, WithModelClient(...))` 之外的既有构造器签名（参考 `dialogue_compressor_test.go` 的既有构造方式）。

同型用例覆盖 `CurrentRoundCompressor`、`RoundLevelCompressor`。

- [ ] **Step 2: 运行确认失败**（字段不存在）。

- [ ] **Step 3: 实现**

1. 配置结构体：`TokensThreshold int` → `TriggerContextRatio float64` + `MinTargetContextRatio float64`；`TriggerTotalTokens int` → `TriggerContextRatio float64`（round-level）+ `MinTargetContextRatio float64`。
2. 默认值：dialogue/current_round = 0.8/0.1；round-level = 0.9/0.1（`New*Config` 与 Validate 的默认填充同步）。
3. `Validate()`：比例须 `0 < ratio <= 1`，`minTarget` 须 `0 <= minTarget < ratio`。
4. 触发判定统一改为：

```go
func triggerThresholdByRatio(mc iface.ModelContext, ratio float64, modelConfig *llm_schema.ModelRequestConfig) int {
	budget := processor.EffectiveContextBudget(mc, modelConfig, 0)
	threshold := int(float64(budget) * ratio)
	if threshold < 1 {
		threshold = 1
	}
	return threshold
}
```

并改比较为 `>=`（round-level 两处、dialogue/current_round 各一处）。`TargetTotalTokens`（round-level）保留绝对值，但下限由 `MinTargetContextRatio × budget` 兜底（`target = max(target, int(budget*minTarget))`）。

- [ ] **Step 4: 运行通过**

```bash
go test ./internal/agentcore/context_engine/processor/compressor/ -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/processor/compressor/
git commit -m "refactor(context): 压缩器阈值改按上下文比例触发（B-02）"
```

> 注：`processor.EffectiveContextBudget` 在 Task 11 落地；本 task 与 Task 11 同批提交亦可（先写 Task 11 的 guard 文件再回填触发）。

### Task 11: `processor/budget_guard.go`

**Files:**
- Create: `internal/agentcore/context_engine/processor/budget_guard.go`
- Create: `internal/agentcore/context_engine/processor/budget_guard_test.go`
- Modify: `internal/agentcore/context_engine/processor/doc.go`（文件树补两行）

- [ ] **Step 1: 写失败用例**（覆盖：候选取最小正值、全无效回退解析值、tokenizer 异常降级 len//3、截断保留后缀、trigger<=0 直接给标记）

```go
func TestEffectiveContextBudget_取最小正值(t *testing.T) {
	mc := newBudgetTestContext(t, 200000) // context_window_tokens=200000
	got := EffectiveContextBudget(mc, nil, 50000)
	if got != 50000 {
		t.Fatalf("min(窗口, 调用预算) = %d, want 50000", got)
	}
}

func TestTruncateMessageContent_保留后缀(t *testing.T) {
	long := strings.Repeat("a", 20000) + "\n[[OFFLOAD:abc]]"
	got := TruncateMessageContentToFixedHeadTailIfOverTokenLimit(long, 100, func(s string) int { return len(s) / 3 }, "\n[[OFFLOAD:abc]]")
	if !strings.HasSuffix(got, "\n[[OFFLOAD:abc]]") {
		t.Fatalf("后缀丢失: %q", got[max(0, len(got)-40):])
	}
	if !strings.Contains(got, TruncatedByBudgetMarker) {
		t.Fatalf("缺少截断标记")
	}
}
```

桩说明：`newBudgetTestContext(t, window int)` 为本文件内最小桩——嵌入 `iface.ModelContext` 接口值（未覆写的方法保持 nil panic，测试只走以下两个方法），覆写 `TokenCounter() token.TokenCounter`（返回 nil，触发 `len//3` 降级）与窗口读取方法（与 `EffectiveContextBudget` 内部取窗口的方法同名，实现时二者必须同源，避免测试桩绕过真实取值口径）。

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**（对齐 `processor/budget_guard.py`）

```go
// TruncatedByBudgetMarker 预算截断标记
const TruncatedByBudgetMarker = "... placeholder truncated; original content is preserved in offload storage ..."
// TruncatedSideMaxChars 截断时首尾各自保留的最大字符数
const TruncatedSideMaxChars = 2000

// EffectiveContextBudget 返回最严格的可用上下文预算（窗口 / 调用预算 / 模型解析值取最小正值）。
//
// Python: budget_guard.effective_context_budget()
func EffectiveContextBudget(mc iface.ModelContext, modelConfig *llm_schema.ModelRequestConfig, callBudget int) int
// CountMessagesTokens 统计消息 token（tokenizer 优先，失败降级 字符数//3）。
//
// Python: budget_guard.count_messages_tokens()
func CountMessagesTokens(mc iface.ModelContext, messages []llm_schema.BaseMessage) int
// TruncateMessageContentToFixedHeadTailIfOverTokenLimit 超限时截成固定首尾预览并保留后缀。
//
// Python: budget_guard.truncate_message_content_to_fixed_head_tail_if_over_token_limit()
func TruncateMessageContentToFixedHeadTailIfOverTokenLimit(content string, triggerTokenLimit int, countContentTokens func(string) int, preservedSuffix string) string
```

实现要点：`triggerTokenLimit <= 0` 返回 `preservedSuffix` 或标记；未超限原样返回；超限时先剥离 `preservedSuffix`，再取 `head = min(2000, len/2)`、`tail = min(2000, len-head)`，用 `\n<标记>\n` 连接后拼回后缀；空体场景仅返回标记+后缀。

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/context_engine/processor/ -run 'TestEffectiveContextBudget|TestCountMessagesTokens|TestTruncateMessageContent' -v`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/processor/budget_guard.go internal/agentcore/context_engine/processor/budget_guard_test.go internal/agentcore/context_engine/processor/doc.go
git commit -m "feat(context): 新增压缩预算 guard（B-02）"
```

### Task 12: RoundLevelCompressor 接入预算 guard

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/round_level_compressor.go`（压缩调用预算、替换消息预算、提示词预算字段）
- Test: `internal/agentcore/context_engine/processor/compressor/round_level_compressor_test.go`

- [ ] **Step 1: 写失败用例**

```go
func TestRoundLevelCompressor_替换消息超预算则跳过(t *testing.T) {
	// 预算 1000，替换消息 5000 token → 不应写回
	...
	if replacementsApplied {
		t.Fatalf("超预算的替换必须跳过（对齐 _is_replacement_under_budget）")
	}
}

func TestRoundLevelCompressor_压缩调用预算受窗口收口(t *testing.T) {
	// 窗口 8000、CompressionCallMaxTokens 250000 → 预算应为 8000
	if got := c.compressionCallBudget(mc); got != 8000 {
		t.Fatalf("预算 = %d, want 8000", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**

1. 新增 `compressionCallBudget(mc) int` 助手（`processor.EffectiveContextBudget(mc, rlc.roundConfig.Model, rlc.compressionCallMaxTokens)`），`isUnderCompressionCallBudget` 改为 `total <= rlc.compressionCallBudget(mc)`。
2. 新增 `isReplacementUnderBudget(mc, replacementMessages) bool`，在普通替换与合并替换两处各插一次：

```go
		if !rlc.isReplacementUnderBudget(mc, []llm_schema.BaseMessage{replacementMessage}) {
			continue // 超预算替换跳过，避免"压缩完仍超限"的反复压缩
		}
```

3. 提示词中的 `compression_call_budget_limit` 用同一 effective budget。
4. 保留 `roundConfig *RoundLevelCompressorConfig` 引用（对齐上游 `_round_config`），`getModel` 用它取 model/modelClient。

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/context_engine/processor/compressor/ -run TestRoundLevelCompressor -v`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/processor/compressor/round_level_compressor.go internal/agentcore/context_engine/processor/compressor/round_level_compressor_test.go
git commit -m "fix(context): round-level 压缩接入预算 guard（B-02）"
```

### Task 13: 卸载占位预览超预算截断

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offloader/message_summary_offloader.go:557-635`（`offloadMessageAdaptive` 收尾处）
- Test: `internal/agentcore/context_engine/processor/offloader/message_summary_offloader_test.go`

- [ ] **Step 1: 写失败用例**：构造占位预览 20000 字符 + 后缀 `[[OFFLOAD:xx]]`，断言卸载后消息长度 < 5000 且后缀保留、含 `TruncatedByBudgetMarker`。

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**

```go
// truncateOffloadPlaceholderPreviewIfLarge 占位预览自身超预算时截断（保留 [[OFFLOAD:...]] 句柄）。
//
// 否则"卸载完成仍超限"会让压缩器反复触发（对齐上游
// _truncate_offload_placeholder_preview_if_large）。
func (mso *MessageSummaryOffloader) truncateOffloadPlaceholderPreviewIfLarge(
	message llm_schema.BaseMessage, mc iface.ModelContext,
) {
	content := message.GetContent().Text()
	if content == "" {
		return
	}
	trigger := mso.config.LargeMessageThreshold
	if budget := processor.EffectiveContextBudget(mc, mso.config.Model, 0); budget > 0 && budget < trigger {
		trigger = budget
	}
	if processor.CountMessagesTokens(mc, []llm_schema.BaseMessage{message}) <= trigger {
		return
	}
	suffix := offloadMarkerSuffix(content)
	// 只按内容文本估算候选长度（避免 clone 消息；与整条消息计数的差异仅在几十 token 量级，
	// 不影响"是否截断"的判断）。SetContent 无返回值（用法见 prompt/assembler.go:343）。
	count := func(candidate string) int { return processor.EstimateContentTokens(candidate) }
	message.SetContent(llm_schema.NewTextContent(
		processor.TruncateMessageContentToFixedHeadTailIfOverTokenLimit(content, trigger, count, suffix)))
}

// offloadMarkerSuffix 取内容中最后一个 [[OFFLOAD: 起的后缀。
func offloadMarkerSuffix(content string) string {
	idx := strings.LastIndex(content, "[[OFFLOAD:")
	if idx < 0 {
		return ""
	}
	return content[idx:]
}
```

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/context_engine/processor/offloader/ -v`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/processor/offloader/
git commit -m "fix(context): 卸载占位预览超预算时截断（B-02）"
```

### Task 14: 配置模板与 parity 测试同步

**Files:**
- Modify: `resources/config.yaml:225-285`
- Modify: `internal/common/config/config_yaml_parity_test.go`、`config_template_token_test.go`
- Test: 上述两文件

- [ ] **Step 1: 改断言（先红）**：把 `react.context_engine_config.compression_trigger_ratio|compression_target_ratio`、`*_compressor_config.tokens_threshold|trigger_total_tokens` 从白名单移除，改为 `dialogue_compressor_config.trigger_context_ratio`、`current_round_compressor_config.trigger_context_ratio`、`round_level_compressor_config.trigger_context_ratio|min_target_context_ratio`、`round_level_compressor_config.target_total_tokens`、`models.defaults[].model_config_obj.context_window|max_tokens`。

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 改 yaml**：删除 `compression_trigger_ratio` / `compression_target_ratio` 两行与注释；`message_summary_offloader_config` 下删除 `messages_threshold`/`tokens_threshold`/`messages_to_keep`/`keep_last_round`（B-13 同批）；`dialogue_compressor_config`/`current_round_compressor_config` 增 `trigger_context_ratio: 0.8`/`min_target_context_ratio: 0.1` 并删 `tokens_threshold`；`round_level_compressor_config` 增 `trigger_context_ratio: 0.8`/`min_target_context_ratio: 0.1` 并删 `tokens_threshold`/`trigger_total_tokens`；模型条目把 `max_input_tokens`/`max_output_tokens` 改为 `model_config_obj.context_window: 0`/`max_tokens: 0`。

- [ ] **Step 4: 运行通过**

```bash
go test ./internal/common/config/ -v
go test ./internal/agentcore/harness/rails/context_engineer/ -v
```

- [ ] **Step 5: 提交**

```bash
git add resources/config.yaml internal/common/config/
git commit -m "chore(config): 配置模板与 parity 断言同步比例阈值（B-02/B-13/B-14）"
```

---

## C3：计数口径与图像计费（B-12、B-05）

### Task 15: 六处 `/4` → `/3`

**Files:**
- Modify: `internal/agentcore/context_engine/token/tiktoken_counter.go:226`
- Modify: `internal/agentcore/context_engine/context/session_model_context.go:1216,1241`
- Modify: `internal/agentcore/context_engine/context/processor_state_recorder.go:296`
- Modify: `internal/agentcore/context_engine/interface/types.go:462,485`
- Test: 上述各包既有测试 + 断言降级值

- [ ] **Step 1: 写失败用例**：`newTestTiktokenCounterWithoutEncoder().fallbackCount(strings.Repeat("a", 30))` 断言 10（原实现返回 7）。

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 改 6 处**（`/ 4` → `/ 3`，`math.Ceil(float64(totalChars)/4)` → `/3`），并把注释「对齐 Python len//4」改为「对齐 Python len//3」。

- [ ] **Step 4: 运行通过**

```bash
go test ./internal/agentcore/context_engine/token/ ./internal/agentcore/context_engine/context/ ./internal/agentcore/context_engine/interface/ -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/
git commit -m "fix(context): token 降级口径统一为 3 字符/token（B-12）"
```

### Task 16: `content_sanitize.go`

**Files:**
- Create: `internal/agentcore/context_engine/content_sanitize.go`
- Create: `internal/agentcore/context_engine/content_sanitize_test.go`
- Modify: `internal/agentcore/context_engine/doc.go`

- [ ] **Step 1: 写失败用例**（`data:` URL、`data_url`/`url` 键、`source{type:base64}`、`image_url.url`、普通文本不受影响、非 image 的 `data:application/pdf` 不误标）

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**（对齐 `content_sanitize.py`）

```go
// ImageDataURLPlaceholder 图片 data URL 占位符
const ImageDataURLPlaceholder = "[image:data-url-omitted]"
// ImageBase64Placeholder 图片 base64 载荷占位符
const ImageBase64Placeholder = "[image:base64-omitted]"

var dataURLPattern = regexp.MustCompile(`(?i)data:image/[^;]+;base64,[A-Za-z0-9+/=]+`)

// SanitizeValueForText 递归替换值中的图片/base64 载荷（不改变其它结构）。
//
// Python: content_sanitize.sanitize_value_for_text()
func SanitizeValueForText(value any) any
// SanitizeContentForText 把消息内容序列化为文本，且不内嵌图片原始字节。
//
// Python: content_sanitize.sanitize_content_for_text()
func SanitizeContentForText(content any) string
```

注意：payload 字符类**不含** `\s`（上游注释：data URL 无空白，允许会吞掉后续散文）；仅当值以 `data:image/` 开头时才整体替换，避免把 `data:application/pdf` 误标成图片。

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/context_engine/ -run TestSanitize -v`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/content_sanitize.go internal/agentcore/context_engine/content_sanitize_test.go internal/agentcore/context_engine/doc.go
git commit -m "feat(context): 新增图片/base64 文本净化（B-05）"
```

### Task 17: `ContentPart` 尺寸字段 + 图像分块计费

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/message.go:54-66`
- Create: `internal/agentcore/context_engine/token/image_tokens.go`
- Create: `internal/agentcore/context_engine/token/image_tokens_test.go`
- Modify: `internal/agentcore/context_engine/token/tiktoken_counter.go:170-200`（`CountMessages`）
- Test: `internal/agentcore/context_engine/token/tiktoken_counter_test.go`

- [ ] **Step 1: 写失败用例**

```go
func TestCountMessages_图像按尺寸计费(t *testing.T) {
	c := mustTiktokenCounter(t, "gpt-4o")
	w, h := 1024, 768
	msg := llmschema.NewUserMessage("", llmschema.WithMultiModalContent(llmschema.ContentPart{
		Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "data:image/png;base64," + strings.Repeat("A", 400000)},
		Width: &w, Height: &h,
	}))
	got, err := c.CountMessages([]llmschema.BaseMessage{msg}, "")
	if err != nil { t.Fatal(err) }
	if got > 5000 {
		t.Fatalf("图像应按尺寸计费（约 1.5k），got %d —— base64 被当文本了", got)
	}
}

func TestImageTokens_读不到尺寸用1445(t *testing.T) {
	if got := imageBlockTokens(llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://x/y.png"}}); got != 1445 {
		t.Fatalf("got %d, want 1445", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**

1. `ContentPart` 增 `Width *int`、`Height *int`、`Dimensions string`（`json:"width,omitempty"` 等）。
2. `image_tokens.go`：常量 `defaultImagePlaceholderTokens=1445`、`imageBaseTokens=85`、`imageTileTokens=170`、`claudePatchSize=28`、`claudeMaxImageTokens=1568`、`lowDetailTokens=85`；`imageBlockTokens(part) int`（env → low → 尺寸 → 兜底）；`imageDimensions(part)`（显式字段 → PNG/GIF/JPEG/WebP 头）；`estimateImageTokens(w,h)` 取两公式较大者。
3. `CountMessages`：content 为多模态时按块累加（文本块走 tokenizer、图像块走 `imageBlockTokens`、未知块走 sanitize 后 compact JSON）；纯字符串 content 先 `SanitizeContentForText`。

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/context_engine/token/ -v`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/schema/message.go internal/agentcore/context_engine/token/
git commit -m "feat(context): 图像按尺寸计费并支持显式尺寸字段（B-05）"
```

### Task 18: 估算函数走 sanitize

**Files:**
- Modify: `internal/agentcore/context_engine/processor/util.go:236-290`
- Test: `internal/agentcore/context_engine/processor/util_test.go`

- [ ] **Step 1: 写失败用例**：含 400KB base64 的多模态 content 估算值 < 5000（原实现 > 100000）。

- [ ] **Step 2: 运行确认失败** → **Step 3: 实现**：`EstimateContentTokens` 用 `context_engine.SanitizeContentForText(content)` 后再算长度；`EstimateMessageTokens` 多模态分支同样先 sanitize。

- [ ] **Step 4: 运行通过** → **Step 5: 提交**

```bash
git commit -m "fix(context): 内容估算先净化图片载荷（B-05）"
```

### Task 19: `read_file` 图片预算统一 + 标注缩放后尺寸

**Files:**
- Modify: `internal/agentcore/harness/tools/filesystem/read_file.go:495-610,799-806`
- Test: `internal/agentcore/harness/tools/filesystem/read_file_image_test.go`（新建，若无）

- [ ] **Step 1: 写失败用例**：给 3000×2000 PNG，断言 (a) 返回块里 `Width/Height` 等于**缩放后**尺寸；(b) `estimateImageTokens` 使用尺寸公式（同一图 3000×2000 的估算 > 1536×1024 的估算）。

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**：把 `estimateImageTokens([]byte)` 改为 `estimateImageTokensFromDimensions(w, h int) int`（复用 `token.ImageTokens` 的公式，通过导出的 `token.EstimateImageTokens(w,h)`），在三级缩放的每级之后用 `img.Bounds()` 重新取宽高；写入 multimodal 块的 `width/height`。

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/harness/tools/filesystem/ -run 'ReadFile.*Image|Image' -v`

- [ ] **Step 5: 提交**

```bash
git commit -m "fix(tools): read_file 图像预算与上下文计费统一并标注真实尺寸（B-05）"
```

---

## C4：溢出识别与恢复（B-07、B-09）

### Task 20: 引擎层 `IsContextOverflowError`

**Files:**
- Modify: `internal/agentcore/context_engine/engine.go`（新增导出函数，放文件末尾“导出函数”区）
- Modify: `internal/agentcore/context_engine/interface/types.go`（接口声明）
- Create: `internal/agentcore/context_engine/overflow_error_test.go`

- [ ] **Step 1: 写失败用例**（9 短语逐一 + `errors.Join` 因果链 + 包装错误 + 非溢出负例）

```go
func TestIsContextOverflowError(t *testing.T) {
	phrases := []string{
		"context length", "context window", "maximum context", "context limit",
		"prompt is too long", "prompt too long", "input is too long",
		"input too long", "range of input length should be [1, 1000000]",
	}
	for _, p := range phrases {
		if !IsContextOverflowError(fmt.Errorf("400: %s exceeded", p)) {
			t.Fatalf("%q 应识别为上下文溢出", p)
		}
	}
	wrapped := fmt.Errorf("llm call failed: %w", fmt.Errorf("bad request: context limit"))
	if !IsContextOverflowError(wrapped) {
		t.Fatalf("因果链应被扫描")
	}
	if IsContextOverflowError(errors.New("invalid api key")) {
		t.Fatalf("非溢出错误不应误判")
	}
}
```

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**：常量切片 `contextOverflowPhrases`（9 条，含注释注明百炼原文）、`contextOverflowFields`（16 个字段名，Go 侧等价为：沿 `errors.Unwrap` 链 + `exception.BaseError` 的 message/cause 字段 + `fmt.Sprintf("%+v")` 兜底），`IsContextOverflowError(err error) bool` 小写匹配。

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/context_engine/ -run TestIsContextOverflowError -v`

- [ ] **Step 5: 提交**

```bash
git commit -m "feat(context): 引擎层上下文溢出识别（含百炼错误，B-09）"
```

### Task 21: `RecoverFromModelContextOverflow`

**Files:**
- Modify: `internal/agentcore/context_engine/engine.go`
- Modify: `internal/agentcore/context_engine/interface/types.go`
- Test: `internal/agentcore/context_engine/engine_overflow_recover_test.go`（新建）

- [ ] **Step 1: 写失败用例**（compressed → true；noop → false；`streaming && emitted>0` → false 且不调用压缩）

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**

```go
// RecoverFromModelContextOverflow 模型报上下文溢出后压缩一次，返回是否值得重试。
//
// 流式场景只在"尚未吐出任何 chunk"时允许恢复，否则重试会重发已发送前缀。
// Python: ContextEngine.recover_from_model_context_overflow()
func (ce *contextEngine) RecoverFromModelContextOverflow(
	ctx context.Context, sess sessioninterfaces.SessionFacade, contextID string,
	streaming bool, streamChunksEmitted int,
) bool {
	if streaming && streamChunksEmitted > 0 {
		logger.Warn(logComponent).
			Str("event_type", "context_recovery_skipped").
			Int("stream_chunks_emitted", streamChunksEmitted).
			Msg("skip model context recovery after streaming output was emitted")
		return false
	}
	res, err := ce.CompressContext(ctx, contextID, sess)
	if err != nil || res == nil {
		return false
	}
	return res.Result == "compressed"
}
```

- [ ] **Step 4: 运行通过** → **Step 5: 提交** `git commit -m "feat(context): 溢出后压缩一次的自救入口（B-07）"`

### Task 22: ReAct 与 DeepAgent 接入

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_model_call.go`（模型调用错误出口）
- Modify: `internal/agentcore/harness/deep_agent.go`（模型调用错误出口）
- Test: `internal/agentcore/single_agent/agents/react_model_call_test.go`、`internal/agentcore/harness/deep_agent_test.go`

- [ ] **Step 1: 写失败用例**：模型返回 "context limit exceeded" → agent 调 `RecoverFromModelContextOverflow` 一次并重试模型调用；第二次仍失败 → 返回错误（不无限重试）。

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**：在重试耗尽分支（`react_model_call.go` 中判定 `visible output 未产生` 的位置）插入：

```go
		if ce.IsContextOverflowError(err) && !overflowRecovered {
			if ceEngine.RecoverFromModelContextOverflow(ctx, sess, contextID, streaming, emittedChunks) {
				overflowRecovered = true
				continue // 只重试一次
			}
		}
```

`emittedChunks` 取 `cbc.ModelVisibleOutputEmitted()` 对应的 chunk 计数（无计数则按 0/1 二值：`emitted ? 1 : 0`）。

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/single_agent/agents/ ./internal/agentcore/harness/ -run 'Overflow' -v`

- [ ] **Step 5: 提交** `git commit -m "fix(agent): 上下文溢出压缩一次后重试（B-07）"`

### Task 23: 卸载器复用引擎判定

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offloader/message_summary_offloader.go:210-220,964-1000`
- Test: `internal/agentcore/context_engine/processor/offloader/message_summary_offloader_test.go`

- [ ] **Step 1: 改测试**：把私有判定用例改为经 `isContextOverflowError` 走引擎层（关键词 `context limit`、`range of input length` 应命中）。

- [ ] **Step 2: 运行确认失败**（旧表不含这两个词）。

- [ ] **Step 3: 实现**：删除 `contextOverflowKeywords` 私有表，`isContextOverflowError` 改为 `return context_engine.IsContextOverflowError(err)`（注意包名冲突：offloader 已 import `context_engine` 作为 `iface` 之外的根包需起别名 `ce_root "github.com/.../context_engine"`；若形成循环依赖则把判定留在 `processor` 包并以 `processor.IsContextOverflowError` 暴露，由引擎调用 processor 版本）。

- [ ] **Step 4: 运行通过** + `go build ./...`（确认无循环依赖）

- [ ] **Step 5: 提交** `git commit -m "refactor(context): 卸载器复用引擎层溢出判定（B-09）"`

---

## C5：摘要卸载与压缩状态（B-13、B-16）

### Task 24: 摘要卸载删冗余字段 + assistant/tool_calls 守卫

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offloader/message_summary_offloader.go:29-70,86-90,262-271,400-445`
- Test: `internal/agentcore/context_engine/processor/offloader/message_summary_offloader_test.go`

- [ ] **Step 1: 写失败用例**：`offload_message_type=["tool","assistant"]` 时，带 `tool_calls` 的 assistant 消息不被卸载；`Validate()` 对 `messages_to_keep >= messages_threshold` 不再报错（字段已删，用编译期断言代替）。

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**：删 4 个字段与 `msoDefaultTokensThreshold`；`Validate()` 仅 `c.applyMSODefaults(); return nil`；`shouldOffloadMessage` 规则 2 之后插入：

```go
	// 规则 2.5：带 tool_calls 的 assistant 消息不得卸载——替换后 tool 结果失去配对，
	// 后续请求会被上游判 400（对齐上游 _should_offload_message 的同名守卫）。
	if am, ok := message.(*llm_schema.AssistantMessage); ok && len(am.ToolCalls) > 0 {
		return false
	}
```

- [ ] **Step 4: 运行通过** → **Step 5: 提交** `git commit -m "fix(context): 摘要卸载删冗余参数并禁止卸载 tool_calls 消息（B-13）"`

### Task 25: 压缩状态 `modified_messages`

**Files:**
- Modify: `internal/agentcore/context_engine/schema/context_state.go`（新增结构 + 状态字段）
- Modify: `internal/agentcore/context_engine/context/processor_state_recorder.go`（`BuildState` + `buildModifiedMessages` + `stateToMap`）
- Test: `internal/agentcore/context_engine/context/processor_state_recorder_test.go`

- [ ] **Step 1: 写失败用例**（内容改写 / 新卸载 / 删除 / 新增 / 无 id 五类）

```go
func TestBuildModifiedMessages_五种情况(t *testing.T) {
	before := []llmschema.BaseMessage{
		msgWithID(t, "a", "原文"),
		msgWithID(t, "b", "工具结果"),
		msgWithID(t, "c", "将被删除"),
		msgNoID(t, "无 id 不参与"),
	}
	after := []llmschema.BaseMessage{
		msgWithID(t, "a", "改写后"),
		offloadedMsgWithID(t, "b", "占位", "handle-1", "filesystem"),
		msgWithID(t, "d", "新增"),
		msgNoID(t, "无 id 不参与"),
	}
	got := buildModifiedMessagesForTest(before, after)
	if len(got) != 2 {
		t.Fatalf("应只记改写与新卸载两条，got %d", len(got))
	}
	if got[0].MessageID != "a" || got[0].OffloadHandle != "" {
		t.Fatalf("改写项不应带句柄: %+v", got[0])
	}
	if got[1].MessageID != "b" || got[1].OffloadHandle != "handle-1" || got[1].OffloadType != "filesystem" {
		t.Fatalf("新卸载项应带句柄与类型: %+v", got[1])
	}
}
```

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**

`context_state.go`：

```go
// ContextCompressionModifiedMessage 压缩过程中被就地改写的消息条目。
//
// Python: ContextCompressionModifiedMessage
type ContextCompressionModifiedMessage struct {
	MessageID     string `json:"message_id"`
	Role          string `json:"role,omitempty"`
	ToolCallID    string `json:"tool_call_id,omitempty"`
	OffloadHandle string `json:"offload_handle,omitempty"`
	OffloadType   string `json:"offload_type,omitempty"`
}
```

`ContextCompressionState` 增 `ModifiedMessages []ContextCompressionModifiedMessage `json:"modified_messages"``（放在 `Saved` 之前，与上游字段顺序一致）。

`processor_state_recorder.go`：

```go
// buildModifiedMessages 找出处理器就地改写的消息（id 相同但内容变化或新变成卸载占位）。
//
// Python: ContextProcessorStateRecorder._build_modified_messages()
func (r *ProcessorStateRecorder) buildModifiedMessages(before, after []llm_schema.BaseMessage) []schema.ContextCompressionModifiedMessage {
	index := make(map[string]llm_schema.BaseMessage, len(before))
	for _, msg := range before {
		if id := GetContextMessageID(msg); id != "" {
			if _, exists := index[id]; !exists {
				index[id] = msg
			}
		}
	}
	modified := make([]schema.ContextCompressionModifiedMessage, 0)
	for _, msg := range after {
		id := GetContextMessageID(msg)
		if id == "" {
			continue
		}
		original, ok := index[id]
		if !ok || original == msg {
			continue
		}
		newOffload, handle, offloadType := false, "", ""
		if offline, ok := msg.(schema.Offloadable); ok {
			info := offline.GetOffloadInfo()
			if prev, had := original.(schema.Offloadable); !had || prev.GetOffloadInfo().OffloadHandle != info.OffloadHandle {
				newOffload, handle, offloadType = true, info.OffloadHandle, info.OffloadType
			}
		}
		if !newOffload && original.GetContent().Text() == msg.GetContent().Text() {
			continue
		}
		entry := schema.ContextCompressionModifiedMessage{
			MessageID: id, Role: msg.GetRole().String(), OffloadHandle: handle, OffloadType: offloadType,
		}
		if tm, ok := msg.(*llm_schema.ToolMessage); ok {
			entry.ToolCallID = tm.ToolCallID
		}
		modified = append(modified, entry)
	}
	return modified
}
```

`BuildState` 里 `AfterMessages != nil` 时填 `ModifiedMessages: r.buildModifiedMessages(input.BeforeMessages, input.AfterMessages)`；`stateToMap` 增 `m["modified_messages"] = <逐条 map>`（键名 snake_case，与前端 `compaction.ts` 一致）。

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/context_engine/context/ -run 'ModifiedMessages|BuildState' -v`

- [ ] **Step 5: 提交** `git commit -m "feat(context): 压缩状态上报 modified_messages 与 offload 句柄（B-16）"`

---

## C6：上下文用量子系统（B-06、B-10）

### Task 26: `usage/models.go`

**Files:**
- Create: `internal/agentcore/context_engine/usage/models.go`、`usage/doc.go`
- Test: `internal/agentcore/context_engine/usage/models_test.go`

- [ ] **Step 1: 写失败用例**

```go
// TestContextUsageSnapshot_序列化键名 快照 JSON 键名必须与前端 v1 校验一致。
func TestContextUsageSnapshot_序列化键名(t *testing.T) {
	limit, input, rate := 200000, 12345, 0.0617
	snap := ContextUsageSnapshot{
		Phase: PhasePostCall, RequestID: "req-1", SessionID: "s1", ProductSessionID: "s1",
		ContextWindow: ContextWindowUsage{LimitTokens: &limit, InputTokens: &input, OccupancyRate: &rate},
		Parts: map[string]ContextPartUsage{
			"messages": {Category: CategoryMessages, Tokens: 12345},
		},
	}
	buf, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{
		`"event_type":"context.usage"`, `"schema_version":"context-usage.v1"`, `"phase":"post_call"`,
		`"request_id":"req-1"`, `"product_session_id":"s1"`, `"context_window":{`,
		`"limit_tokens":200000`, `"input_tokens":12345`, `"occupancy_rate":0.0617`, `"parts":{`,
	} {
		if !strings.Contains(string(buf), key) {
			t.Fatalf("缺少键 %s，实际 %s", key, buf)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败** → `undefined: ContextUsageSnapshot`。

- [ ] **Step 3: 实现**（字段与上游 `usage/models.py` 一一对应）

```go
// ContextCategory 上下文用量分类
type ContextCategory string

const (
	// CategorySystemPrompt 系统提示词
	CategorySystemPrompt ContextCategory = "system_prompt"
	// CategoryTools 工具定义
	CategoryTools ContextCategory = "tools"
	// CategorySkills 技能
	CategorySkills ContextCategory = "skills"
	// CategoryMessages 消息
	CategoryMessages ContextCategory = "messages"
	// CategoryMemory 记忆（兼容输入，序列化前归一化为 system_prompt）
	CategoryMemory ContextCategory = "memory"
)

// ContextPartUsage 单个分类的用量
type ContextPartUsage struct {
	Category              ContextCategory `json:"category"`
	Tokens                int             `json:"tokens"`
	PercentageOfWindow    *float64        `json:"percentage_of_window"`
	PercentageOfInput     *float64        `json:"percentage_of_input"`
	Estimated             bool            `json:"estimated"`
	Tokenizer             string          `json:"tokenizer,omitempty"`
	Carrier               string          `json:"carrier,omitempty"`
	Source                string          `json:"source"`
	MeasurementSource     string          `json:"measurement_source"`
	FallbackReason        string          `json:"fallback_reason,omitempty"`
	FallbackTokenizerName string          `json:"fallback_tokenizer_model,omitempty"`
	Order                 *int            `json:"order,omitempty"`
	StableID              string          `json:"stable_id,omitempty"`
}

// ContextWindowUsage 上下文窗口用量
type ContextWindowUsage struct {
	LimitTokens              *int     `json:"limit_tokens"`
	InputTokens              *int     `json:"input_tokens"`
	OccupancyRate            *float64 `json:"occupancy_rate"`
	TokensSource             string   `json:"tokens_source"`
	LocalEstimatedInputToken int      `json:"local_estimated_input_tokens"`
	ReconciliationDeltaToken *int     `json:"reconciliation_delta_tokens"`
	ContextSnapshotID        string   `json:"context_snapshot_id,omitempty"`
}

// RequestKVCacheUsage 单次请求的 KV cache 用量
type RequestKVCacheUsage struct {
	InputTokens      *int     `json:"input_tokens"`
	CacheReadTokens  *int     `json:"cache_read_tokens"`
	CacheMissTokens  *int     `json:"cache_miss_tokens"`
	CacheWriteTokens *int     `json:"cache_write_tokens"`
	HitRate          *float64 `json:"hit_rate"`
	Status           string   `json:"status"`
	Source           string   `json:"source"`
	Authoritative    bool     `json:"authoritative"`
	MissTokensDerived bool    `json:"miss_tokens_derived"`
}

// SessionKVCacheUsage 会话级 KV cache 聚合
type SessionKVCacheUsage struct {
	Scope                   string   `json:"scope"`
	AggregationMethod       string   `json:"aggregation_method"`
	CallsTotal              int      `json:"calls_total"`
	CallsObserved           int      `json:"calls_observed"`
	CallsPartial            int      `json:"calls_partial"`
	CallsNotReported        int      `json:"calls_not_reported"`
	InputTokensTotal        int      `json:"input_tokens_total"`
	CacheReadTokensTotal    int      `json:"cache_read_tokens_total"`
	CacheMissTokensTotal    int      `json:"cache_miss_tokens_total"`
	CacheWriteTokensTotal   *int     `json:"cache_write_tokens_total"`
	CacheWriteCoverage      string   `json:"cache_write_coverage"`
	AverageInputPerCall     *float64 `json:"average_input_per_call"`
	WeightedHitRate         *float64 `json:"weighted_hit_rate"`
	AverageCacheReadPerCall *float64 `json:"average_cache_read_per_call"`
	AverageCacheMissPerCall *float64 `json:"average_cache_miss_per_call"`
	Status                  string   `json:"status"`
	DataQuality             string   `json:"data_quality"`
}

// ContextUsageMeasurement 测量元信息
type ContextUsageMeasurement struct {
	CategorySource    string `json:"category_source"`
	TotalSource       string `json:"total_source"`
	Tokenizer         string `json:"tokenizer,omitempty"`
	Estimated         bool   `json:"estimated"`
	AuthoritativeTotal bool  `json:"authoritative_total"`
	MeasurementVersion string `json:"measurement_version"`
	ContextSnapshotID  string `json:"context_snapshot_id,omitempty"`
}

// ContextUsageSnapshot 上下文用量快照（context-usage.v1）
type ContextUsageSnapshot struct {
	EventType             string                       `json:"event_type"`
	SchemaVersion         string                       `json:"schema_version"`
	Phase                 string                       `json:"phase"`
	RequestID             string                       `json:"request_id"`
	SessionID             string                       `json:"session_id"`
	ProductSessionID      string                       `json:"product_session_id,omitempty"`
	ExecutionSessionID    string                       `json:"execution_session_id,omitempty"`
	ContextOwnerID        string                       `json:"context_owner_id,omitempty"`
	AgentID               string                       `json:"agent_id,omitempty"`
	TeamID                string                       `json:"team_id"`
	MemberName            string                       `json:"member_name"`
	Role                  string                       `json:"role,omitempty"`
	InvocationID          string                       `json:"invocation_id,omitempty"`
	ParentInvocationID    string                       `json:"parent_invocation_id,omitempty"`
	DelegationID          string                       `json:"delegation_id,omitempty"`
	AgentPath             []string                     `json:"agent_path"`
	Depth                 int                          `json:"depth"`
	ContextWindow         ContextWindowUsage           `json:"context_window"`
	Parts                 map[string]ContextPartUsage  `json:"parts"`
	SessionKVCacheHitRate *float64                     `json:"session_kv_cache_hit_rate"`
	Measurement           ContextUsageMeasurement      `json:"measurement"`
	RequestCache          RequestKVCacheUsage          `json:"request_kv_cache,omitempty"`
	SessionCache          *SessionKVCacheUsage         `json:"session_kv_cache,omitempty"`
}

const (
	// PhasePreCall 模型调用前
	PhasePreCall = "pre_call"
	// PhasePostCall 模型调用后
	PhasePostCall = "post_call"
	// PhasePostCompact 手动/主动压缩后
	PhasePostCompact = "post_compact"
	// SchemaVersion 快照协议版本
	SchemaVersion = "context-usage.v1"
	// EventTypeContextUsage 事件类型
	EventTypeContextUsage = "context.usage"
)
```

`TeamID`/`MemberName` 用不带 `omitempty` 的 `string`（前端要求键存在且为 string|null，Go 侧空串在 JSON 为 `""`，前端 `isNullableString` 对 `""` 通过），`session_kv_cache_hit_rate` 用 `*float64` 以输出 `null`。

- [ ] **Step 4: 运行通过** → `go test ./internal/agentcore/context_engine/usage/ -run TestContextUsageSnapshot_序列化键名 -v`

- [ ] **Step 5: 提交** `git commit -m "feat(context): usage 快照模型（context-usage.v1）"`

### Task 27: `usage/provider_usage.go`

**Files:** Create `usage/provider_usage.go` + 测试。

- [ ] **Step 1: 写失败用例**：全字段 / 仅 input / 无 usage / cache_read > input 四例。

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**

```go
// RequestCacheFromUsage 把 provider 的 usage_metadata 转成请求级 KV cache 用量。
//
// Go 的 UsageMetadata 目前只有 CacheTokens（= 缓存读 token，对齐上游 cache_tokens）；
// 缓存写字段缺失属于 C 组缺陷（C-04），此处按 not_reported 处理，不臆造数据。
// miss = input - read（读回超过输入时钳制为 0 并标 derived）。
//
// Python: provider_usage.request_kv_cache_usage()
func RequestCacheFromUsage(usage *llm_schema.UsageMetadata) RequestKVCacheUsage {
	const notReported = "not_reported"
	out := RequestKVCacheUsage{Status: notReported, Source: notReported}
	if usage == nil {
		return out
	}
	if usage.InputTokens > 0 {
		input := usage.InputTokens
		out.InputTokens = &input
		out.Source = "provider_usage"
		out.Status = "reported"
	}
	if usage.CacheTokens > 0 {
		read := usage.CacheTokens
		out.CacheReadTokens = &read
		if out.InputTokens != nil && *out.InputTokens > 0 {
			hit := float64(read) / float64(*out.InputTokens)
			if hit > 1 {
				hit = 1
			}
			out.HitRate = &hit
		}
		if out.InputTokens != nil {
			miss := *out.InputTokens - read
			if miss < 0 {
				miss = 0
				out.MissTokensDerived = true
			}
			out.CacheMissTokens = &miss
		}
	}
	out.Authoritative = out.InputTokens != nil
	return out
}
```

- [ ] **Step 4: 运行通过** → **Step 5: 提交** `git commit -m "feat(context): provider usage→KV cache 用量转换"`

### Task 28: `usage/analyzer.go`

**Files:** Create `usage/analyzer.go` + 测试。

- [ ] **Step 1: 写失败用例**：无 provider 用量（`tokens_source=local_estimate`）；有 provider 用量（差额归因到 messages）；fallback 源按比例分摊；`limit=0` 时 `occupancy_rate=nil`。

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**

```go
// Analyzer 上下文用量分析器
type Analyzer struct {
	// LimitTokens 上下文窗口上限，0 表示未知
	LimitTokens int
	// TokenizerName 测量所用 tokenizer 名称
	TokenizerName string
}

// Report 测量结果（供 pre_call 与 post_call 复用同一份快照输入）
type Report struct {
	Parts       map[string]ContextPartUsage
	Window      ContextWindowUsage
	Measurement ContextUsageMeasurement
}

// Measure 统计当前窗口各类别占用（不改动上下文）。
//
// Python: ContextUsageAnalyzer.measure()
func (a *Analyzer) Measure(
	mc iface.ModelContext, systemMessages []llm_schema.BaseMessage,
	contextMessages []llm_schema.BaseMessage, tools []schema.ToolInfoInterface,
) Report {
	parts := map[string]ContextPartUsage{}
	parts[string(CategorySystemPrompt)] = ContextPartUsage{
		Category: CategorySystemPrompt, Tokens: a.countMessages(mc, systemMessages),
		Source: "tokenizer", MeasurementSource: a.TokenizerName, Estimated: true,
	}
	parts[string(CategoryTools)] = ContextPartUsage{
		Category: CategoryTools, Tokens: a.countTools(mc, tools),
		Source: "tokenizer", MeasurementSource: a.TokenizerName, Estimated: true,
	}
	parts[string(CategoryMessages)] = ContextPartUsage{
		Category: CategoryMessages, Tokens: a.countMessages(mc, contextMessages),
		Source: "tokenizer", MeasurementSource: a.TokenizerName, Estimated: true,
	}
	local := 0
	for _, p := range parts {
		local += p.Tokens
	}
	window := ContextWindowUsage{LocalEstimatedInputToken: local, TokensSource: "local_estimate"}
	if a.LimitTokens > 0 {
		limit := a.LimitTokens
		window.LimitTokens = &limit
	}
	return Report{Parts: parts, Window: window, Measurement: ContextUsageMeasurement{
		CategorySource: a.TokenizerName, TotalSource: "local_estimate",
		Tokenizer: a.TokenizerName, Estimated: true, MeasurementVersion: "1",
	}}
}

// Snapshot 由 Report 生成快照，provider 用量非空时以其为权威总量并归因差额。
//
// Python: ContextUsageAnalyzer.snapshot_from_report()
func (a *Analyzer) Snapshot(
	report Report, req RequestKVCacheUsage, meta SnapshotMeta,
) ContextUsageSnapshot { /* 见下方归因规则 */ }
```

`Snapshot` 归因规则（对齐上游 `_attribute_provider_remainder_to_messages`）：`providerInput == nil` → 直接用 `local`；否则 `delta = providerInput - local`，全部加到 `messages` 类（fallback 源时按各类别占比分摊）；`occupancy_rate = providerInput/limit`（`limit<=0` 时 `nil`）；`measurement.total_source = "provider_usage"`、`authoritative_total = true`。

- [ ] **Step 4: 运行通过** → **Step 5: 提交** `git commit -m "feat(context): 上下文用量分析器（parts/归因/三时点）"`

### Task 29: `usage/session_aggregator.go`

**Files:** Create `usage/session_aggregator.go` + 测试。

- [ ] **Step 1: 写失败用例**：空集（`status=not_reported`、`calls_total=0`）、全部 not_reported、部分缺失（`calls_partial>0`、`data_quality=partial`）。

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**

```go
// AggregateSessionCache 汇总一次会话内的请求级 KV cache 用量。
//
// 聚合方式为 token 加权：命中率 = Σread / Σinput（仅统计已上报输入量的调用）。
//
// Python: session_aggregator.aggregate()
func AggregateSessionCache(requests []RequestKVCacheUsage) SessionKVCacheUsage {
	out := SessionKVCacheUsage{
		Scope: "session", AggregationMethod: "token_weighted",
		CallsTotal: len(requests), Status: "not_reported", DataQuality: "not_reported",
		CacheWriteCoverage: "none",
	}
	var inputSum, readSum, writeSum int
	writeObserved, observed := 0, 0
	for _, r := range requests {
		switch {
		case r.InputTokens == nil:
			out.CallsNotReported++
		case r.CacheReadTokens == nil:
			out.CallsPartial++
			inputSum += *r.InputTokens
			observed++
		default:
			out.CallsObserved++
			inputSum += *r.InputTokens
			readSum += *r.CacheReadTokens
			observed++
		}
		if r.CacheWriteTokens != nil {
			writeSum += *r.CacheWriteTokens
			writeObserved++
		}
	}
	out.InputTokensTotal, out.CacheReadTokensTotal = inputSum, readSum
	out.CacheMissTokensTotal = inputSum - readSum
	if writeObserved > 0 {
		w := writeSum
		out.CacheWriteTokensTotal = &w
		out.CacheWriteCoverage = "partial"
		if writeObserved == observed {
			out.CacheWriteCoverage = "full"
		}
	}
	if observed > 0 {
		avgInput := float64(inputSum) / float64(observed)
		out.AverageInputPerCall = &avgInput
		out.Status = "reported"
		out.DataQuality = "full"
		if out.CallsPartial > 0 {
			out.DataQuality = "partial"
		}
		if inputSum > 0 {
			hit := float64(readSum) / float64(inputSum)
			out.WeightedHitRate = &hit
		}
	}
	return out
}
```

- [ ] **Step 4: 运行通过** → **Step 5: 提交** `git commit -m "feat(context): 会话级 KV cache 聚合"`

### Task 30: 消息级 usage 基线 + 五处失效

**Files:**
- Modify: `internal/agentcore/context_engine/context/context_utils.go`（`InvalidateUsageMetadata`/`HasValidUsageMetadata`）
- Modify: `internal/agentcore/context_engine/context/session_model_context.go`（`ClearMessages`/`AddMessages` 两条快路径/`SetMessages`/重载）
- Test: `internal/agentcore/context_engine/context/session_model_context_usage_test.go`

- [ ] **Step 1: 写失败用例**：`AddMessages` 纯 append 且保留前缀未变 → 基线保留；处理器改写前缀 → 基线清空；`SetMessages`/`ClearMessages` → 清空。

- [ ] **Step 2: 运行确认失败**。

- [ ] **Step 3: 实现**：`InvalidateUsageMetadata(msgs)` 把带 `UsageMetadata` 的消息置空；`AddMessages` 入队前记录 `retained := mc.messageBuffer.GetBack()`，入队后若 `retainedMessagePrefixPreserved(retained, mc.messageBuffer.GetBack())` 为假则 `InvalidateUsageMetadata(current)`；统计口径（`StatMessages`）优先取最后一条 `HasValidUsageMetadata` 消息的 `InputTokens`，否则 tokenizer/估算。

- [ ] **Step 4: 运行通过** → **Step 5: 提交** `git commit -m "feat(context): 消息级 usage 基线与失效规则（B-06）"`

### Task 31: 快照接入与 `context.usage` 载荷切换

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_model_call.go`（pre/post call）、`react_invoke.go`（手动压缩后 post_compact）
- Modify: `internal/swarm/agents/harness/common/rails/stream_event_emit.go:190-245`
- Test: `internal/swarm/agents/harness/common/rails/stream_event_emit_test.go`

- [ ] **Step 1: 写失败用例**：断言发出的 `context.usage` 载荷含 `event_type=context.usage`、`schema_version=context-usage.v1`、`phase ∈ {post_call,post_compact}`、`context_window.limit_tokens`、`parts` 非空、`product_session_id` 非空（即前端 `parseContextUsageSnapshot` 能通过）。

- [ ] **Step 2: 运行确认失败**（现载荷只有 3 个键）。

- [ ] **Step 3: 实现**：删除自拼 `{rate, context_max, tokens_used}`；由 analyzer 产快照后 `session.WriteStream(ctx, &stream.OutputSchema{Type: "context.usage", Payload: snapshotMap})`；`react_invoke.go` 的手动 `CompressContext` 完成分支追加 `post_compact` 快照。

- [ ] **Step 4: 运行通过** + 手工核对前端解析器（`contextUsageModel.ts:36-80` 的必填项全部满足）

- [ ] **Step 5: 提交** `git commit -m "feat(context): context.usage 事件切换 context-usage.v1 快照（B-10）"`

### Task 32: 集成用例（跨子系统）

**Files:** `tests/integration/agentcore/context_usage_test.go`（新建，`//go:build integration`）

- [ ] **Step 1: 写用例**：`ReActAgent`（含 ContextProcessorRail）+ `ContextEngine` + `Session`/stream 三系统联动 —— 模型调用后发出 post_call 快照；手动压缩后发 post_compact 快照；窗口来自 `context_engine_config` override。
- [ ] **Step 2: 跑集成**：`go test -tags=integration ./tests/integration/agentcore/ -run TestContextUsage -v`
- [ ] **Step 3: 审计**：`go run ./cmd/integration-audit -verify`（跨 ≥3 个 internal 二级子系统）
- [ ] **Step 4: 提交** `git commit -m "test(integration): 上下文用量快照链路覆盖（B-06/B-10）"`

---

## 收尾校验（全部批次完成后）

- [ ] `go build ./... && go vet ./...`
- [ ] `go test ./internal/agentcore/context_engine/... ./internal/agentcore/single_agent/... ./internal/agentcore/harness/... ./internal/swarm/server/adapter/... -race`
- [ ] `go run ./cmd/integration-audit -verify`
- [ ] 残留扫描：`rg -n "compression_trigger_ratio|max_input_tokens|applyCompressionRatios|shrinkThreshold|TriggerTotalTokens|msoDefaultTokensThreshold" internal/ resources/` → 仅允许 `full_compact_processor.go` 的 `TriggerTotalTokens`（上游保留）
- [ ] 更新 `IMPLEMENTATION_PLAN.md`：24-A 表格 C1~C6 状态改 ✅，24.14 视完成情况改 ✅
- [ ] 更新 `docs/sync/defect-plan-2026-11-04.md`：B 表已修项状态备注「已修（提交 hash）」
- [ ] 推送并盯 CI：Build / Lint / Unit / Integration 四 job 全绿（项目规则 6）
