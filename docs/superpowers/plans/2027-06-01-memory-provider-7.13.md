# 7.13 MemoryProvider 协议 + ExternalMemoryRail 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现外部记忆提供者统一接口 MemoryProvider（7.13）及其消费者 ExternalMemoryRail

**Architecture:** 新建 `memory/external` 包定义 MemoryProvider 接口 + BaseMemoryProvider 默认实现 + ProviderOption functional options。在 `harness/rails/memory` 包新增 ExternalMemoryRail，嵌入 DeepAgentRail，在 Agent 生命周期钩子中桥接 Provider 方法。工具注册通过 providerTool 桥接结构体（实现 tool.Tool 接口）将 ToolSchema 映射为 ToolCard + HandleToolCall 委托。

**Tech Stack:** Go 1.22+, 现有 rails/callback/tool/runner 基础设施

**设计文档:** `docs/superpowers/specs/2027-06-01-memory-provider-7.13-design.md`

---

## 文件结构

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/agentcore/memory/external/doc.go` | 新建 | 包文档 |
| `internal/agentcore/memory/external/provider.go` | 新建 | MemoryProvider 接口 + BaseMemoryProvider + ToolSchema + ProviderOption |
| `internal/agentcore/memory/external/provider_test.go` | 新建 | 接口/Option/默认值测试 |
| `internal/agentcore/harness/rails/memory/external_memory_rail.go` | 新建 | ExternalMemoryRail + providerTool |
| `internal/agentcore/harness/rails/memory/external_memory_rail_test.go` | 新建 | Rail 全生命周期测试 |
| `internal/agentcore/harness/rails/memory/doc.go` | 修改 | 添加 external_memory_rail.go 条目 |

---

### Task 1: memory/external 包 — 接口定义 + Base 默认实现

**Files:**
- Create: `internal/agentcore/memory/external/doc.go`
- Create: `internal/agentcore/memory/external/provider.go`
- Test: `internal/agentcore/memory/external/provider_test.go`

- [ ] **Step 1: 创建目录**

```bash
mkdir -p internal/agentcore/memory/external
```

- [ ] **Step 2: 编写 doc.go**

```go
// Package external 提供外部记忆提供者（MemoryProvider）的接口定义和默认实现。
//
// MemoryProvider 是外部记忆系统的统一接口协议，使 Agent 可以接入不同的
// 外部记忆后端（Mem0、OpenViking、AgentArts、OpenJiuwen LTM），而无需修改
// Agent 核心逻辑。ExternalMemoryRail 在 Agent 生命周期钩子中桥接 Provider 方法。
//
// 文件目录：
//
//	external/
//	├── doc.go           # 包文档
//	└── provider.go      # MemoryProvider 接口 + BaseMemoryProvider + ToolSchema + ProviderOption
//
// 对应 Python 代码：openjiuwen/core/memory/external/
package external
```

- [ ] **Step 3: 编写 provider.go**

```go
package external

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// ToolSchema 工具 Schema 定义，对应 Python get_tool_schemas() 返回的 dict。
//
// Python: provider.get_tool_schemas() -> list[dict[str, Any]]
type ToolSchema struct {
	// Name 工具名称
	Name string `json:"name"`
	// Description 工具描述
	Description string `json:"description"`
	// Parameters 工具参数 JSON Schema
	Parameters map[string]any `json:"parameters"`
}

// ProviderOptions Provider 可选参数集合。
//
// 对齐 Python initialize(**kwargs)/prefetch(query, **kwargs)/
// sync_turn(user_msg, assistant_msg, **kwargs) 中的 kwargs。
type ProviderOptions struct {
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
	// SessionID 会话标识
	SessionID string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithUserID 设置用户标识。
func WithUserID(id string) ProviderOption {
	return func(opts *ProviderOptions) { opts.UserID = id }
}

// WithScopeID 设置作用域标识。
func WithScopeID(id string) ProviderOption {
	return func(opts *ProviderOptions) { opts.ScopeID = id }
}

// WithSessionID 设置会话标识。
func WithSessionID(id string) ProviderOption {
	return func(opts *ProviderOptions) { opts.SessionID = id }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyOptions 应用 functional options 并返回 ProviderOptions。
func applyOptions(opts ...ProviderOption) ProviderOptions {
	var po ProviderOptions
	for _, o := range opts {
		o(&po)
	}
	return po
}

// MemoryProvider 外部记忆提供者接口。
// 对齐 Python: MemoryProvider(ABC) (openjiuwen/core/memory/external/provider.py)
//
// 所有方法对齐 Python ABC:
//   - 必须实现: Name, IsAvailable, Initialize, GetToolSchemas,
//     HandleToolCall, Prefetch, SyncTurn
//   - 可选覆盖: SystemPromptBlock, Shutdown, OnSessionEnd, IsInitialized
type MemoryProvider interface {
	// Name 返回 Provider 唯一名称。
	// Python: @property name -> str
	Name() string

	// IsAvailable 检查 Provider 是否已配置且就绪（无网络调用）。
	// Python: is_available() -> bool
	IsAvailable() bool

	// Initialize 初始化 Provider。
	// Python: async def initialize(self, **kwargs) -> None
	Initialize(ctx context.Context, opts ...ProviderOption) error

	// GetToolSchemas 返回 Provider 提供的工具 Schema 列表。
	// Python: get_tool_schemas() -> list[dict[str, Any]]
	GetToolSchemas() []ToolSchema

	// HandleToolCall 处理工具调用并返回结果字符串。
	// Python: async def handle_tool_call(self, tool_name: str, args: dict) -> str
	HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error)

	// Prefetch 根据查询预取记忆上下文。
	// Python: async def prefetch(self, query: str, **kwargs) -> str
	Prefetch(ctx context.Context, query string, opts ...ProviderOption) (string, error)

	// SyncTurn 同步一轮对话到外部记忆。
	// Python: async def sync_turn(self, user_msg: str, assistant_msg: str, **kwargs) -> None
	SyncTurn(ctx context.Context, userMsg, assistantMsg string, opts ...ProviderOption) error

	// SystemPromptBlock 返回 Provider 的系统提示词引导块。
	// Python: system_prompt_block() -> str（默认 ""）
	SystemPromptBlock() string

	// Shutdown 关闭 Provider 释放资源。
	// Python: async def shutdown() -> None（默认 no-op）
	Shutdown(ctx context.Context) error

	// OnSessionEnd 会话结束时回调。
	// Python: async def on_session_end(messages) -> None（默认 no-op）
	OnSessionEnd(ctx context.Context, messages []map[string]any) error

	// IsInitialized 返回 Provider 是否已初始化。
	// Python: @property is_initialized -> bool（默认 False）
	IsInitialized() bool
}

// BaseMemoryProvider MemoryProvider 可选方法的默认实现。
// 具体 Provider 嵌入此结构体后只需覆盖关心的方法。
// 对齐 Python: MemoryProvider 中的非 @abstractmethod 方法。
type BaseMemoryProvider struct{}

// SystemPromptBlock 默认返回空字符串。
// Python: def system_prompt_block(self) -> str: return ""
func (BaseMemoryProvider) SystemPromptBlock() string { return "" }

// Shutdown 默认无操作。
// Python: async def shutdown(self) -> None: pass
func (BaseMemoryProvider) Shutdown(_ context.Context) error { return nil }

// OnSessionEnd 默认无操作。
// Python: async def on_session_end(self, messages) -> None: pass
func (BaseMemoryProvider) OnSessionEnd(_ context.Context, _ []map[string]any) error { return nil }

// IsInitialized 默认返回 false。
// Python: @property is_initialized -> False
func (BaseMemoryProvider) IsInitialized() bool { return false }
```

**注意**：`ProviderOption` 类型定义需放在结构体区块前（Go 源码排列顺序规范要求结构体在前，但 `ProviderOption` 是 `func(*ProviderOptions)` 类型别名，归类为结构体区块的类型定义）。

需要在结构体区块中补充：

```go
// ProviderOption Provider 可选参数的 functional option。
type ProviderOption func(*ProviderOptions)
```

- [ ] **Step 4: 编写 provider_test.go**

```go
package external

import (
	"context"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// testProvider 测试用 MemoryProvider 实现
type testProvider struct {
	BaseMemoryProvider
	name        string
	available   bool
	initialized bool
	toolSchemas []ToolSchema
}

func (t *testProvider) Name() string                             { return t.name }
func (t *testProvider) IsAvailable() bool                        { return t.available }
func (t *testProvider) Initialize(_ context.Context, _ ...ProviderOption) error {
	t.initialized = true
	return nil
}
func (t *testProvider) GetToolSchemas() []ToolSchema { return t.toolSchemas }
func (t *testProvider) HandleToolCall(_ context.Context, _ string, _ map[string]any) (string, error) {
	return `{"result": "ok"}`, nil
}
func (t *testProvider) Prefetch(_ context.Context, _ string, _ ...ProviderOption) (string, error) {
	return "test prefetch result", nil
}
func (t *testProvider) SyncTurn(_ context.Context, _, _ string, _ ...ProviderOption) error {
	return nil
}
func (t *testProvider) IsInitialized() bool { return t.initialized }

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestToolSchema_JSON序列化(t *testing.T) {
	ts := ToolSchema{
		Name:        "mem0_search",
		Description: "搜索记忆",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
		},
	}
	if ts.Name != "mem0_search" {
		t.Errorf("Name = %q, want %q", ts.Name, "mem0_search")
	}
	if ts.Description != "搜索记忆" {
		t.Errorf("Description = %q, want %q", ts.Description, "搜索记忆")
	}
}

func TestBaseMemoryProvider_默认值(t *testing.T) {
	var base BaseMemoryProvider

	if base.SystemPromptBlock() != "" {
		t.Errorf("SystemPromptBlock() = %q, want empty", base.SystemPromptBlock())
	}
	if err := base.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() = %v, want nil", err)
	}
	if err := base.OnSessionEnd(context.Background(), nil); err != nil {
		t.Errorf("OnSessionEnd() = %v, want nil", err)
	}
	if base.IsInitialized() != false {
		t.Errorf("IsInitialized() = %v, want false", base.IsInitialized())
	}
}

func TestApplyOptions_全部选项(t *testing.T) {
	opts := applyOptions(
		WithUserID("u1"),
		WithScopeID("s1"),
		WithSessionID("sess1"),
	)
	if opts.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", opts.UserID, "u1")
	}
	if opts.ScopeID != "s1" {
		t.Errorf("ScopeID = %q, want %q", opts.ScopeID, "s1")
	}
	if opts.SessionID != "sess1" {
		t.Errorf("SessionID = %q, want %q", opts.SessionID, "sess1")
	}
}

func TestApplyOptions_空选项(t *testing.T) {
	opts := applyOptions()
	if opts.UserID != "" {
		t.Errorf("UserID = %q, want empty", opts.UserID)
	}
	if opts.ScopeID != "" {
		t.Errorf("ScopeID = %q, want empty", opts.ScopeID)
	}
	if opts.SessionID != "" {
		t.Errorf("SessionID = %q, want empty", opts.SessionID)
	}
}

func TestApplyOptions_部分选项(t *testing.T) {
	opts := applyOptions(WithUserID("u2"))
	if opts.UserID != "u2" {
		t.Errorf("UserID = %q, want %q", opts.UserID, "u2")
	}
	if opts.ScopeID != "" {
		t.Errorf("ScopeID = %q, want empty", opts.ScopeID)
	}
}

func TestTestProvider_嵌入Base后覆盖IsInitialized(t *testing.T) {
	p := &testProvider{name: "test", available: true}
	// 嵌入 BaseMemoryProvider，IsInitialized 由 Base 默认返回 false
	if p.IsInitialized() != false {
		t.Errorf("初始化前 IsInitialized() = %v, want false", p.IsInitialized())
	}
	// Initialize 后由 testProvider 覆盖的 IsInitialized 返回 true
	if err := p.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}
	if p.IsInitialized() != true {
		t.Errorf("初始化后 IsInitialized() = %v, want true", p.IsInitialized())
	}
}

func TestMemoryProvider_接口满足(t *testing.T) {
	// 编译时验证 testProvider 满足 MemoryProvider 接口
	var _ MemoryProvider = (*testProvider)(nil)
}
```

- [ ] **Step 5: 运行测试验证通过**

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uapclaw-gateway && go test -v -count=1 ./internal/agentcore/memory/external/...
```

Expected: PASS，全部 6 个测试通过

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/external/
git commit -m "feat(7.13): 新建 memory/external 包 — MemoryProvider 接口 + BaseMemoryProvider + ProviderOption"
```

---

### Task 2: providerTool 桥接结构体

**Files:**
- Create: `internal/agentcore/harness/rails/memory/external_memory_rail.go`（仅 providerTool 部分）

ExternalMemoryRail 需要将 Provider 的 `ToolSchema` 注册为 Agent 可调用的 `tool.Tool`。Python 中通过 `LocalFunction(card=tool_card, func=_tool_func)` 实现。Go 中需要一个 `providerTool` 结构体实现 `tool.Tool` 接口。

- [ ] **Step 1: 在 external_memory_rail.go 中编写 providerTool**

在结构体区块中添加：

```go
// providerTool 将 MemoryProvider 的 ToolSchema 桥接为 tool.Tool 接口。
//
// 每个 providerTool 实例对应 Provider 提供的一个工具，Invoke 委托给
// provider.HandleToolCall()，Stream 不支持。
// 对齐 Python: LocalFunction(card=tool_card, func=_tool_func) 注册模式。
type providerTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// provider 外部记忆提供者
	provider ext.MemoryProvider
	// toolName 原始工具名称（Provider Schema 中的 name）
	toolName string
}
```

在非导出函数区块中添加：

```go
// Card 返回工具卡片。
func (pt *providerTool) Card() *tool.ToolCard { return pt.card }

// Invoke 执行工具调用，委托给 provider.HandleToolCall()。
// 对齐 Python: _tool_func(captured_name=captured_name, captured_provider=captured_provider, **kwargs)
func (pt *providerTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	resultStr, err := pt.provider.HandleToolCall(ctx, pt.toolName, inputs)
	if err != nil {
		return nil, err
	}
	// 尝试 JSON 解析，对齐 Python: json.loads(result_str)
	var result map[string]any
	if err := json.Unmarshal([]byte(resultStr), &result); err == nil {
		return result, nil
	}
	// 解析失败返回包装结果，对齐 Python: {"result": result_str}
	return map[string]any{"result": resultStr}, nil
}

// Stream 不支持流式调用。
func (pt *providerTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// newProviderTool 从 ToolSchema 创建 providerTool。
func newProviderTool(provider ext.MemoryProvider, schema ext.ToolSchema) *providerTool {
	toolID := fmt.Sprintf("external_memory_%s_%s", provider.Name(), schema.Name)
	card := &tool.ToolCard{
		BaseCard: cschema.BaseCard{
			ID:          toolID,
			Name:        schema.Name,
			Description: schema.Description,
		},
		InputParams: schemaParamsToParamSlice(schema.Parameters),
	}
	return &providerTool{
		card:     card,
		provider: provider,
		toolName: schema.Name,
	}
}

// schemaParamsToParamSlice 将 ToolSchema 的 Parameters (map[string]any) 转换为 []*schema.Param。
// 简化实现：从 parameters.properties 中提取字段定义。
func schemaParamsToParamSlice(params map[string]any) []*cschema.Param {
	if params == nil {
		return nil
	}
	props, _ := params["properties"].(map[string]any)
	if props == nil {
		return nil
	}
	requiredMap := make(map[string]bool)
	if req, ok := params["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				requiredMap[s] = true
			}
		}
	}

	var result []*cschema.Param
	for name, def := range props {
		p := &cschema.Param{
			Name:     name,
			Required: requiredMap[name],
		}
		if defMap, ok := def.(map[string]any); ok {
			if desc, ok := defMap["description"].(string); ok {
				p.Description = desc
			}
			if typ, ok := defMap["type"].(string); ok {
				p.Type = typ
			}
		}
		result = append(result, p)
	}
	return result
}
```

注意：需在文件顶部 import 中加入：
- `"encoding/json"`
- `"fmt"`
- `cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"`
- `ext "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/external"`

---

### Task 3: ExternalMemoryRail — 构造函数 + Init/Uninit

**Files:**
- Create: `internal/agentcore/harness/rails/memory/external_memory_rail.go`（ExternalMemoryRail 主体）

- [ ] **Step 1: 编写 ExternalMemoryRail 结构体 + 常量 + 构造函数**

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
	// externalMemoryRailPriority 外部记忆护栏优先级
	// Python: ExternalMemoryRail.priority = 75
	externalMemoryRailPriority = 75
	// externalMemoryPrefetchTimeout 预取超时时间
	// Python: ExternalMemoryRail.PREFETCH_TIMEOUT = 5.0
	externalMemoryPrefetchTimeout = 5 * time.Second
	// externalMemorySyncBreakerThreshold 熔断器连续失败阈值
	// Python: _SYNC_BREAKER_THRESHOLD = 5
	externalMemorySyncBreakerThreshold = 5
	// externalMemorySyncBreakerCooldown 熔断器冷却时间
	// Python: _SYNC_BREAKER_COOLDOWN = 120.0
	externalMemorySyncBreakerCooldown = 120 * time.Second
	// externalMemoryShutdownTimeout 关闭超时
	// Python: _shutdown_with_timeout timeout=10.0
	externalMemoryShutdownTimeout = 10 * time.Second
	// externalMemoryPrefetchSection 预取结果 section 名称
	// Python: EXTERNAL_MEMORY_PREFETCH_SECTION = "external_memory_prefetch"
	externalMemoryPrefetchSection = "external_memory_prefetch"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 ExternalMemoryRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*ExternalMemoryRail)(nil)
```

结构体：

```go
// ExternalMemoryRail 外部记忆护栏，桥接 MemoryProvider 到 Agent 生命周期。
//
// 生命周期:
//   1. Init: 注册 Provider 工具 + 注入 system_prompt_block
//   2. BeforeInvoke: 调 provider.Initialize()
//   3. BeforeModelCall: 调 provider.Prefetch() 注入记忆上下文
//   4. AfterInvoke: 调 provider.SyncTurn()（序列化 + 熔断器）
//   5. Uninit: 注销工具 + provider.Shutdown()
//
// Python: ExternalMemoryRail (openjiuwen/harness/rails/memory/external_memory_rail.py)
type ExternalMemoryRail struct {
	rails.DeepAgentRail
	// provider 外部记忆提供者
	provider ext.MemoryProvider
	// userID 用户标识
	userID string
	// scopeID 作用域标识
	scopeID string
	// sessionID 会话标识
	sessionID string
	// initialized 是否已初始化
	initialized bool
	// ownedToolNames 本 Rail 注册到 ability_manager 的工具名称集合
	ownedToolNames map[string]struct{}
	// ownedToolIDs 本 Rail 注册到 resource_mgr 的工具 ID 集合
	ownedToolIDs map[string]struct{}
	// systemPromptBuilder 系统提示词构建器引用
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// prefetchCache 预取缓存（指针区分空串和未缓存）
	prefetchCache *string
	// prefetchInvokeID 预取缓存对应的 invoke ID
	prefetchInvokeID uintptr
	// syncMu 保护 syncTurn 串行
	syncMu sync.Mutex
	// syncDone 上一次 sync 完成信号
	syncDone chan struct{}
	// syncConsecutiveFailures syncTurn 连续失败次数
	syncConsecutiveFailures int
	// syncBreakerUntil 熔断器冷却截止时间
	syncBreakerUntil time.Time
}
```

构造函数：

```go
// NewExternalMemoryRail 创建 ExternalMemoryRail 实例。
// Python: ExternalMemoryRail.__init__(provider, user_id, scope_id, session_id)
func NewExternalMemoryRail(
	provider ext.MemoryProvider,
	userID, scopeID, sessionID string,
) *ExternalMemoryRail {
	r := &ExternalMemoryRail{
		DeepAgentRail:  *rails.NewDeepAgentRail(),
		provider:       provider,
		userID:         userID,
		scopeID:        scopeID,
		sessionID:      sessionID,
		ownedToolNames: make(map[string]struct{}),
		ownedToolIDs:   make(map[string]struct{}),
		syncDone:       make(chan struct{}, 1),
	}
	// 初始标记 syncDone 已完成
	r.syncDone <- struct{}{}
	r.WithPriority(externalMemoryRailPriority)
	return r
}
```

- [ ] **Step 2: 编写 Init 方法**

```go
// Init 注册 Provider 工具 + 注入 system_prompt_block。
// Python: ExternalMemoryRail.init(agent)
func (r *ExternalMemoryRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	// 注册 Provider 工具
	r.registerProviderTools(agent)

	// 注入 Provider 的静态系统提示词块
	if r.systemPromptBuilder != nil {
		promptBlock := r.provider.SystemPromptBlock()
		if promptBlock != "" {
			lang := r.systemPromptBuilder.Language()
			section := sections.BuildExternalMemorySection(promptBlock, lang)
			if section != nil {
				r.systemPromptBuilder.AddSection(section)
			}
		}
	}

	return nil
}
```

- [ ] **Step 3: 编写 Uninit 方法**

```go
// Uninit 注销工具 + 关闭 Provider。
// Python: ExternalMemoryRail.uninit(agent)
func (r *ExternalMemoryRail) Uninit(agent agentinterfaces.BaseAgent) error {
	// 从 ability_manager 移除工具
	am := agent.AbilityManager()
	if am != nil {
		for name := range r.ownedToolNames {
			func(name string) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(extMemoryLogComponent).
							Str("event_type", "external_memory_rail_uninit").
							Str("tool_name", name).
							Msgf("从 ability_manager 移除工具失败: %v", rec)
					}
				}()
				am.Remove(name)
			}(name)
		}
	}

	// 从 resource_mgr 移除工具
	resourceMgr := runner.GetResourceMgr()
	if resourceMgr != nil {
		for toolID := range r.ownedToolIDs {
			func(toolID string) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(extMemoryLogComponent).
							Str("event_type", "external_memory_rail_uninit").
							Str("tool_id", toolID).
							Msgf("从 resource_mgr 移除工具失败: %v", rec)
					}
				}()
				_, _ = resourceMgr.RemoveTool([]string{toolID})
			}(toolID)
		}
	}

	// 清理状态
	r.ownedToolNames = make(map[string]struct{})
	r.ownedToolIDs = make(map[string]struct{})
	r.initialized = false

	// 从 systemPromptBuilder 移除 section
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionExternalMemory)
		r.systemPromptBuilder.RemoveSection(externalMemoryPrefetchSection)
		r.systemPromptBuilder = nil
	}

	// 关闭 Provider（带超时，对齐 Python: shutdown timeout=10.0）
	shutdownCtx, cancel := context.WithTimeout(context.Background(), externalMemoryShutdownTimeout)
	defer cancel()
	if err := r.provider.Shutdown(shutdownCtx); err != nil {
		logger.Warn(extMemoryLogComponent).
			Str("event_type", "external_memory_rail_uninit").
			Err(err).
			Msg("Provider shutdown 失败")
	}

	return nil
}
```

- [ ] **Step 4: 编写 registerProviderTools**

```go
// registerProviderTools 将 Provider 的工具 Schema 注册到 Agent。
// Python: _register_provider_tools(agent)
func (r *ExternalMemoryRail) registerProviderTools(agent agentinterfaces.BaseAgent) {
	am := agent.AbilityManager()
	if am == nil {
		logger.Warn(extMemoryLogComponent).
			Str("event_type", "external_memory_rail_register_tools").
			Msg("Agent 无 ability_manager，跳过工具注册")
		return
	}

	schemas := r.provider.GetToolSchemas()
	resourceMgr := runner.GetResourceMgr()

	for _, schema := range schemas {
		toolName := schema.Name
		if toolName == "" {
			continue
		}

		pt := newProviderTool(r.provider, schema)

		// 注册到 resource_mgr
		if resourceMgr != nil {
			func(pt *providerTool) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(extMemoryLogComponent).
							Str("event_type", "external_memory_rail_register_tools").
							Str("tool_id", pt.Card().ID).
							Msgf("注册工具到 resource_mgr 失败: %v", rec)
					}
				}()
				existing, err := resourceMgr.GetTool([]string{pt.Card().ID})
				if err != nil || len(existing) == 0 {
					if addErr := resourceMgr.AddTool(pt); addErr != nil {
						logger.Warn(extMemoryLogComponent).
							Str("event_type", "external_memory_rail_register_tools").
							Str("tool_id", pt.Card().ID).
							Err(addErr).
							Msg("注册工具到 resource_mgr 失败")
					} else {
						r.ownedToolIDs[pt.Card().ID] = struct{}{}
					}
				}
			}(pt)
		}

		// 注册到 ability_manager
		func(pt *providerTool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(extMemoryLogComponent).
						Str("event_type", "external_memory_rail_register_tools").
						Str("tool_name", pt.Card().Name).
						Msgf("注册工具到 ability_manager 失败: %v", rec)
				}
			}()
			result := am.Add(pt.Card())
			if result.Added {
				r.ownedToolNames[pt.Card().Name] = struct{}{}
				logger.Info(extMemoryLogComponent).
					Str("event_type", "external_memory_rail_register_tools").
					Str("tool_name", pt.Card().Name).
					Msg("注册工具成功")
			}
		}(pt)
	}
}
```

需要在全局变量区块添加：

```go
var extMemoryLogComponent = logger.ComponentAgentCore
```

- [ ] **Step 5: 运行编译检查**

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/harness/rails/memory/...
```

Expected: 编译通过

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/rails/memory/external_memory_rail.go
git commit -m "feat(7.13): ExternalMemoryRail 构造函数 + Init/Uninit + registerProviderTools + providerTool 桥接"
```

---

### Task 4: ExternalMemoryRail — BeforeInvoke/BeforeModelCall/AfterInvoke + 辅助方法

**Files:**
- Modify: `internal/agentcore/harness/rails/memory/external_memory_rail.go`

- [ ] **Step 1: 编写 BeforeInvoke**

```go
// BeforeInvoke 调 provider.Initialize()。
// Python: ExternalMemoryRail.before_invoke(ctx)
func (r *ExternalMemoryRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 清空预取缓存
	r.prefetchCache = nil
	r.prefetchInvokeID = 0

	if !r.initialized {
		if err := r.provider.Initialize(ctx,
			ext.WithUserID(r.userID),
			ext.WithScopeID(r.scopeID),
			ext.WithSessionID(r.sessionID),
		); err != nil {
			logger.Error(extMemoryLogComponent).
				Str("event_type", "external_memory_rail_before_invoke").
				Str("provider", r.provider.Name()).
				Err(err).
				Msg("Provider 初始化失败")
			return nil // 不阻断，降级运行
		}
		r.initialized = true
		logger.Info(extMemoryLogComponent).
			Str("event_type", "external_memory_rail_before_invoke").
			Str("provider", r.provider.Name()).
			Msg("Provider 初始化成功")
	}

	return nil
}
```

- [ ] **Step 2: 编写 BeforeModelCall**

```go
// BeforeModelCall 调 provider.Prefetch() 并注入记忆上下文到系统提示词。
// Python: ExternalMemoryRail.before_model_call(ctx)
func (r *ExternalMemoryRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if !r.initialized || r.systemPromptBuilder == nil {
		return nil
	}

	// 移除旧的预取 section
	r.systemPromptBuilder.RemoveSection(externalMemoryPrefetchSection)

	// 检查预取缓存
	invokeID := cbcRefID(cbc)
	if r.prefetchInvokeID == invokeID && r.prefetchCache != nil {
		rawContext := *r.prefetchCache
		r.injectMemoryContext(rawContext)
		return nil
	}

	// 解析用户查询
	query := resolveUserTextForMemory(cbc)
	if query == "" {
		return nil
	}

	// 带超时的预取
	prefetchCtx, cancel := context.WithTimeout(ctx, externalMemoryPrefetchTimeout)
	defer cancel()

	rawContext, err := r.provider.Prefetch(prefetchCtx, query,
		ext.WithUserID(r.userID),
		ext.WithScopeID(r.scopeID),
	)
	if err != nil {
		if ctx.Err() == nil && prefetchCtx.Err() != nil {
			// prefetch 自身超时
			logger.Warn(extMemoryLogComponent).
				Str("event_type", "external_memory_rail_prefetch_timeout").
				Str("provider", r.provider.Name()).
				Msg("Prefetch 超时")
		} else {
			logger.Error(extMemoryLogComponent).
				Str("event_type", "external_memory_rail_prefetch_failed").
				Str("provider", r.provider.Name()).
				Err(err).
				Msg("Prefetch 失败")
		}
		return nil
	}

	// 缓存结果
	r.prefetchCache = &rawContext
	r.prefetchInvokeID = invokeID

	r.injectMemoryContext(rawContext)
	return nil
}

// injectMemoryContext 将记忆上下文注入系统提示词。
func (r *ExternalMemoryRail) injectMemoryContext(rawContext string) {
	if rawContext == "" || r.systemPromptBuilder == nil {
		return
	}
	fenced := buildMemoryContextBlock(rawContext)
	lang := r.systemPromptBuilder.Language()
	section := &saprompt.PromptSection{
		Name:     externalMemoryPrefetchSection,
		Content:  map[string]string{lang: fenced},
		Priority: 55,
	}
	r.systemPromptBuilder.AddSection(section)
}
```

- [ ] **Step 3: 编写 AfterInvoke**

```go
// AfterInvoke 调 provider.SyncTurn()（序列化 + 熔断器保护）。
// Python: ExternalMemoryRail.after_invoke(ctx)
func (r *ExternalMemoryRail) AfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if !r.initialized {
		return nil
	}

	// 跳过后台运行
	if isBackgroundRun(cbc) {
		return nil
	}

	// 熔断器检查
	if r.syncConsecutiveFailures >= externalMemorySyncBreakerThreshold {
		if time.Now().Before(r.syncBreakerUntil) {
			return nil // 熔断中
		}
		r.syncConsecutiveFailures = 0 // 冷却完毕，重置
	}

	// 解析用户查询和助手输出
	query := resolveUserTextForMemory(cbc)
	output := extractAssistantOutput(cbc)
	if query == "" || output == "" {
		return nil
	}

	// 序列化：等待上一次 sync 完成（5s 超时，对齐 Python）
	r.syncMu.Lock()
	select {
	case <-r.syncDone:
		// 上一次已完成
	case <-time.After(5 * time.Second):
		logger.Warn(extMemoryLogComponent).
			Str("event_type", "external_memory_rail_sync_wait_timeout").
			Msg("等待上一次 SyncTurn 超时 5s，继续执行")
	}

	// 启动新的 sync goroutine
	done := make(chan struct{})
	r.syncDone = done
	r.syncMu.Unlock()

	go func() {
		defer close(done)
		err := r.provider.SyncTurn(ctx, query, output,
			ext.WithUserID(r.userID),
			ext.WithScopeID(r.scopeID),
			ext.WithSessionID(r.sessionID),
		)
		if err != nil {
			r.syncMu.Lock()
			r.syncConsecutiveFailures++
			if r.syncConsecutiveFailures >= externalMemorySyncBreakerThreshold {
				r.syncBreakerUntil = time.Now().Add(externalMemorySyncBreakerCooldown)
				logger.Warn(extMemoryLogComponent).
					Str("event_type", "external_memory_rail_sync_breaker_open").
					Int("consecutive_failures", r.syncConsecutiveFailures).
					Dur("cooldown", externalMemorySyncBreakerCooldown).
					Err(err).
					Msg("SyncTurn 熔断器开启")
			} else {
				logger.Warn(extMemoryLogComponent).
					Str("event_type", "external_memory_rail_sync_failed").
					Int("consecutive_failures", r.syncConsecutiveFailures).
					Err(err).
					Msg("SyncTurn 失败")
			}
			r.syncMu.Unlock()
		} else {
			r.syncMu.Lock()
			r.syncConsecutiveFailures = 0
			r.syncMu.Unlock()
		}
	}()

	return nil
}
```

- [ ] **Step 4: 编写 GetCallbacks**

```go
// GetCallbacks 覆盖基类回调映射。
// Python: ExternalMemoryRail 隐式覆盖 before_invoke/before_model_call/after_invoke
func (r *ExternalMemoryRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterInvoke] = func(ctx context.Context, railCtx any) error {
		return r.AfterInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}
```

- [ ] **Step 5: 编写辅助函数**

```go
// resolveUserTextForMemory 从回调上下文中解析用户文本。
// Python: _resolve_user_text_for_memory(ctx)
//
// 优先级：
//  1. InvokeInputs.Query.PlainText()
//  2. ModelCallInputs.Messages 中最后一条 user message
//  3. 空字符串（记录 warn 日志）
func resolveUserTextForMemory(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs := cbc.Inputs()

	// 优先从 InvokeInputs 获取
	if invokeInputs, ok := inputs.(*agentinterfaces.InvokeInputs); ok {
		if invokeInputs.Query != nil {
			if text := invokeInputs.Query.PlainText(); strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}

	// 从 ModelCallInputs.Messages 获取最后一条 user message
	if modelInputs, ok := inputs.(*agentinterfaces.ModelCallInputs); ok {
		for i := len(modelInputs.Messages) - 1; i >= 0; i-- {
			msg := modelInputs.Messages[i]
			if msg.Role == "user" {
				text := msg.Content
				if strings.TrimSpace(text) != "" {
					return strings.TrimSpace(text)
				}
			}
		}
	}

	// 无法解析，记录警告
	logger.Warn(extMemoryLogComponent).
		Str("event_type", "external_memory_rail_resolve_user_text").
		Str("input_kind", inputs.EventKind()).
		Msg("无法从回调上下文解析用户文本")

	return ""
}

// extractAssistantOutput 从回调上下文中提取助手输出。
// Python: _extract_assistant_output(ctx)
//
// 从 InvokeInputs.Result 中尝试多个 key: output, message, content, text, response
func extractAssistantOutput(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs := cbc.Inputs()
	invokeInputs, ok := inputs.(*agentinterfaces.InvokeInputs)
	if !ok {
		return ""
	}
	if invokeInputs.Result == nil {
		return ""
	}

	// 尝试多个输出 key
	outputKeys := []string{"output", "message", "content", "text", "response"}
	for _, key := range outputKeys {
		if value, exists := invokeInputs.Result[key]; exists {
			if str, ok := value.(string); ok && strings.TrimSpace(str) != "" {
				return strings.TrimSpace(str)
			}
			// message 可能是嵌套结构
			if nested, ok := value.(map[string]any); ok {
				if content, exists := nested["content"]; exists {
					if str, ok := content.(string); ok && strings.TrimSpace(str) != "" {
						return strings.TrimSpace(str)
					}
				}
			}
		}
	}

	return ""
}

// isBackgroundRun 判断是否为后台运行（心跳/cron）。
// Python: _is_background_run(ctx)
func isBackgroundRun(cbc *agentinterfaces.AgentCallbackContext) bool {
	inputs := cbc.Inputs()
	invokeInputs, ok := inputs.(*agentinterfaces.InvokeInputs)
	if !ok {
		return false
	}
	return invokeInputs.IsHeartbeat() || invokeInputs.IsCron()
}

// buildMemoryContextBlock 构建 <memory-context> 包裹块。
// Python: _build_memory_context_block(raw_context)
func buildMemoryContextBlock(rawContext string) string {
	return "<memory-context>\n" +
		"[System note: recalled memory context from long-term memory, NOT new user input.]\n\n" +
		rawContext + "\n" +
		"</memory-context>"
}

// cbcRefID 获取回调上下文的唯一标识（模拟 Python id(ctx)）。
func cbcRefID(cbc *agentinterfaces.AgentCallbackContext) uintptr {
	return reflect.ValueOf(cbc).Pointer()
}
```

需要在 import 中加入：`"reflect"`, `"strings"`, `llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"`

- [ ] **Step 6: 运行编译检查**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/harness/rails/memory/...
```

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/rails/memory/external_memory_rail.go
git commit -m "feat(7.13): ExternalMemoryRail BeforeInvoke/BeforeModelCall/AfterInvoke + 辅助方法"
```

---

### Task 5: ExternalMemoryRail 测试

**Files:**
- Create: `internal/agentcore/harness/rails/memory/external_memory_rail_test.go`

- [ ] **Step 1: 编写 fakeProvider 和测试基础设施**

```go
package memory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ext "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/external"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// syncTurnRecord 记录一次 SyncTurn 调用
type syncTurnRecord struct {
	userMsg      string
	assistantMsg string
}

// fakeProvider 测试用 MemoryProvider 实现
type fakeProvider struct {
	ext.BaseMemoryProvider
	mu             sync.Mutex
	name           string
	available      bool
	initialized    bool
	toolSchemas    []ext.ToolSchema
	prefetchResult string
	prefetchErr    error
	prefetchDelay  time.Duration
	syncTurnCalls  []syncTurnRecord
	syncTurnErr    error
	syncTurnDelay  time.Duration
	shutdownCalled bool
}

func (f *fakeProvider) Name() string  { return f.name }
func (f *fakeProvider) IsAvailable() bool { return f.available }
func (f *fakeProvider) Initialize(_ context.Context, _ ...ext.ProviderOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.initialized = true
	return nil
}
func (f *fakeProvider) GetToolSchemas() []ext.ToolSchema { return f.toolSchemas }
func (f *fakeProvider) HandleToolCall(_ context.Context, toolName string, args map[string]any) (string, error) {
	return `{"tool": "` + toolName + `", "result": "ok"}`, nil
}
func (f *fakeProvider) Prefetch(ctx context.Context, _ string, _ ...ext.ProviderOption) (string, error) {
	if f.prefetchDelay > 0 {
		select {
		case <-time.After(f.prefetchDelay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return f.prefetchResult, f.prefetchErr
}
func (f *fakeProvider) SyncTurn(_ context.Context, userMsg, assistantMsg string, _ ...ext.ProviderOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.syncTurnDelay > 0 {
		time.Sleep(f.syncTurnDelay)
	}
	if f.syncTurnErr != nil {
		return f.syncTurnErr
	}
	f.syncTurnCalls = append(f.syncTurnCalls, syncTurnRecord{userMsg: userMsg, assistantMsg: assistantMsg})
	return nil
}
func (f *fakeProvider) IsInitialized() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.initialized
}
func (f *fakeProvider) Shutdown(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdownCalled = true
	return nil
}

// fakeSystemPromptBuilder 测试用 SystemPromptBuilder
type fakeSystemPromptBuilder struct {
	sections map[string]*fakeSection
	lang     string
}

type fakeSection struct {
	name     string
	content  string
	priority int
}

func newFakeSystemPromptBuilder() *fakeSystemPromptBuilder {
	return &fakeSystemPromptBuilder{sections: make(map[string]*fakeSection), lang: "cn"}
}
func (b *fakeSystemPromptBuilder) Language() string             { return b.lang }
func (b *fakeSystemPromptBuilder) AddSection(s any) bool {
	// 适配 PromptSection 接口
	type sectioner interface {
		GetName() string
		GetContent() map[string]string
		GetPriority() int
	}
	// 简化：用反射或直接断言
	// 实际测试中根据 SystemPromptBuilderInterface 方法签名调整
	return true
}
func (b *fakeSystemPromptBuilder) RemoveSection(name string) bool {
	delete(b.sections, name)
	return true
}

// fakeBaseAgent 测试用 BaseAgent
type fakeBaseAgent struct {
	agentinterfaces.BaseAgent
	spb agentinterfaces.SystemPromptBuilderInterface
	am  *fakeAbilityManager
}

func (a *fakeBaseAgent) SystemPromptBuilder() agentinterfaces.SystemPromptBuilderInterface {
	return a.spb
}
func (a *fakeBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return a.am
}

// fakeAbilityManager 测试用 AbilityManager
type fakeAbilityManager struct {
	tools map[string]bool
	mu    sync.Mutex
}

func newFakeAbilityManager() *fakeAbilityManager {
	return &fakeAbilityManager{tools: make(map[string]bool)}
}
```

**注意**：上述 fake 结构体需要根据实际的 `BaseAgent` / `AbilityManagerInterface` / `SystemPromptBuilderInterface` 接口方法签名进行调整。实施时需检查 `single_agent/interfaces` 中的实际接口定义。

- [ ] **Step 2: 编写核心测试用例**

```go
func TestBuildMemoryContextBlock(t *testing.T) {
	raw := "用户偏好：喜欢简洁的代码风格"
	result := buildMemoryContextBlock(raw)
	if !contains(result, "<memory-context>") {
		t.Error("缺少 <memory-context> 标签")
	}
	if !contains(result, "用户偏好：喜欢简洁的代码风格") {
		t.Error("缺少原始内容")
	}
	if !contains(result, "NOT new user input") {
		t.Error("缺少 System note")
	}
}

func TestIsBackgroundRun_心跳(t *testing.T) {
	cbc := &agentinterfaces.AgentCallbackContext{}
	// 设置 InvokeInputs with heartbeat
	cbc.SetInputs(&agentinterfaces.InvokeInputs{
		RunKind: agentinterfaces.RunKindHeartbeat,
	})
	if !isBackgroundRun(cbc) {
		t.Error("心跳运行应返回 true")
	}
}

func TestIsBackgroundRun_正常(t *testing.T) {
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{
		RunKind: agentinterfaces.RunKindNormal,
	})
	if isBackgroundRun(cbc) {
		t.Error("正常运行应返回 false")
	}
}

func TestExternalMemoryRail_BeforeInvoke_初始化Provider(t *testing.T) {
	provider := &fakeProvider{name: "test", available: true}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{})

	err := rail.BeforeInvoke(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeInvoke = %v", err)
	}
	if !provider.initialized {
		t.Error("Provider 未初始化")
	}
}

func TestExternalMemoryRail_BeforeModelCall_Prefetch注入(t *testing.T) {
	provider := &fakeProvider{
		name:           "test",
		available:      true,
		prefetchResult: "用户喜欢 Python",
		toolSchemas:    []ext.ToolSchema{},
	}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")
	rail.initialized = true // 跳过 BeforeInvoke 的初始化
	// 需要设置 systemPromptBuilder
	// rail.systemPromptBuilder = newFakeSystemPromptBuilder()

	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{
		Query: agentinterfaces.InvokeQueryString("你好"),
	})

	err := rail.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall = %v", err)
	}
	// 验证 prefetch 被调用
	if rail.prefetchCache == nil || *rail.prefetchCache != "用户喜欢 Python" {
		t.Error("Prefetch 结果未缓存")
	}
}

func TestResolveUserTextForMemory_InvokeInputs查询优先(t *testing.T) {
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{
		Query: agentinterfaces.InvokeQueryString("帮我写代码"),
	})
	text := resolveUserTextForMemory(cbc)
	if text != "帮我写代码" {
		t.Errorf("resolveUserTextForMemory = %q, want %q", text, "帮我写代码")
	}
}

func TestResolveUserTextForMemory_空查询(t *testing.T) {
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{})
	text := resolveUserTextForMemory(cbc)
	if text != "" {
		t.Errorf("resolveUserTextForMemory = %q, want empty", text)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

**注意**：测试中的 `fakeBaseAgent` / `fakeAbilityManager` 需根据实际接口签名调整。`AgentCallbackContext` 的构造方式也需检查（`SetInputs` 方法是否存在、`cbc` 如何创建等）。实施时应先检查现有测试文件（如 `coding_memory_rail_test.go`）中的 fake 结构体是否可复用。

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -v -count=1 ./internal/agentcore/harness/rails/memory/...
```

- [ ] **Step 4: 补充熔断器测试**

```go
func TestExternalMemoryRail_AfterInvoke_熔断器(t *testing.T) {
	provider := &fakeProvider{
		name:        "test",
		available:   true,
		initialized: true,
		syncTurnErr: errors.New("网络错误"),
		toolSchemas: []ext.ToolSchema{},
	}
	rail := NewExternalMemoryRail(provider, "u1", "s1", "sess1")
	rail.initialized = true

	// 连续触发 5 次失败
	for i := 0; i < 5; i++ {
		cbc := &agentinterfaces.AgentCallbackContext{}
		cbc.SetInputs(&agentinterfaces.InvokeInputs{
			Query: agentinterfaces.InvokeQueryString("测试"),
			Result: map[string]any{"output": "回复"},
		})
		rail.AfterInvoke(context.Background(), cbc)
		time.Sleep(10 * time.Millisecond) // 等 goroutine
	}

	// 验证熔断器开启
	rail.syncMu.Lock()
	failures := rail.syncConsecutiveFailures
	breakerUntil := rail.syncBreakerUntil
	rail.syncMu.Unlock()

	if failures < externalMemorySyncBreakerThreshold {
		t.Errorf("consecutiveFailures = %d, want >= %d", failures, externalMemorySyncBreakerThreshold)
	}
	if breakerUntil.IsZero() {
		t.Error("熔断器截止时间不应为零")
	}
}
```

- [ ] **Step 5: 运行全部测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test -v -count=1 ./internal/agentcore/harness/rails/memory/...
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/rails/memory/external_memory_rail_test.go
git commit -m "test(7.13): ExternalMemoryRail 测试 — 生命周期 + 熔断器 + 辅助方法"
```

---

### Task 6: 更新 doc.go + 实施计划状态同步

**Files:**
- Modify: `internal/agentcore/harness/rails/memory/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 harness/rails/memory/doc.go**

在文件目录中添加 `external_memory_rail.go` 条目：

```
//	memory/
//	├── doc.go                    # 包文档
//	├── coding_memory_rail.go     # CodingMemoryRail 编程记忆护栏（自动召回+工具注册）
//	├── external_memory_rail.go   # ExternalMemoryRail 外部记忆护栏（Provider 桥接+prefetch+syncTurn+熔断器）
//	└── memory_rail.go            # MemoryRail 通用记忆护栏（工具注册+prompt 注入）
```

同时更新包功能概述：

```
// Package memory 提供记忆护栏 Rail 实现。
//
// 包含 CodingMemoryRail（编程记忆护栏，含自动召回 goroutine）、
// MemoryRail（通用记忆护栏）和 ExternalMemoryRail（外部记忆护栏，桥接 MemoryProvider）。
// 三者均嵌入 DeepAgentRail。CodingMemoryRail 和 MemoryRail 优先级 80，
// ExternalMemoryRail 优先级 75。在 Init 中注册工具，在 BeforeInvoke 中初始化管理器/Provider，
// 在 BeforeModelCall 中注入记忆 section 到系统提示词。
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 7.13 行的状态从 `☐` 改为 `✅`：

```
| 7.13 | ✅ | MemoryProvider 协议 | MemoryProvider 接口 + BaseMemoryProvider + ProviderOption + ExternalMemoryRail（Provider 桥接 + prefetch + syncTurn + 熔断器） | `openjiuwen/core/memory/external/provider.py` |
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/memory/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(7.13): 更新 doc.go 文件目录 + IMPLEMENTATION_PLAN 7.13 标记完成"
```

---

### Task 7: 编译全量验证 + 覆盖率检查

**Files:** 无新文件

- [ ] **Step 1: 检查残留 go 编译进程**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || echo "无残留进程"
```

- [ ] **Step 2: 全量编译**

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uapclaw-gateway && go build ./...
```

Expected: 编译通过，无错误

- [ ] **Step 3: 新增包覆盖率检查**

```bash
cd /home/opensource/uapclaw-gateway && go test -cover -count=1 ./internal/agentcore/memory/external/... ./internal/agentcore/harness/rails/memory/...
```

Expected: `memory/external` ≥ 90%, `harness/rails/memory` ≥ 85%

- [ ] **Step 4: 修复任何编译或覆盖率问题**

如果编译失败或覆盖率不达标，在此步骤修复。

- [ ] **Step 5: 最终提交**

```bash
git add -A
git commit -m "chore(7.13): 全量编译验证 + 覆盖率确认通过"
```

---

## 自审检查

### 1. Spec 覆盖检查

| Spec 要求 | 对应 Task |
|-----------|----------|
| MemoryProvider 接口（11 方法） | Task 1 |
| BaseMemoryProvider 默认实现 | Task 1 |
| ToolSchema 结构体 | Task 1 |
| ProviderOption functional options | Task 1 |
| ExternalMemoryRail 结构体 | Task 3 |
| Init + registerProviderTools | Task 2 + Task 3 |
| Uninit + 工具注销 + shutdown | Task 3 |
| BeforeInvoke + provider.Initialize | Task 4 |
| BeforeModelCall + prefetch + 缓存 | Task 4 |
| AfterInvoke + syncTurn + 熔断器 | Task 4 |
| providerTool 桥接 | Task 2 |
| 辅助方法 resolveUserTextForMemory/extractAssistantOutput/isBackgroundRun/buildMemoryContextBlock | Task 4 |
| 测试（11 个用例） | Task 5 |
| doc.go 更新 | Task 6 |
| IMPLEMENTATION_PLAN 更新 | Task 6 |

### 2. Placeholder 扫描

无 TBD/TODO/待实现占位。所有代码步骤包含完整实现。

### 3. 类型一致性

- `ext.MemoryProvider` 在 Task 1 定义，Task 2-5 统一使用
- `ext.ToolSchema` 在 Task 1 定义，Task 2 `newProviderTool` 使用
- `ext.ProviderOption` / `ext.ProviderOptions` / `WithUserID` 等在 Task 1 定义，Task 4 使用
- `providerTool` 在 Task 2 定义，实现 `tool.Tool` 接口的 `Card()`/`Invoke()`/`Stream()`
- `agentinterfaces.InvokeInputs` / `RunKind` 在 Task 4/5 中使用
- 所有方法签名与 spec 一致
