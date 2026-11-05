# 9.11a 延迟工具目录（deferred tool catalog）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Go 侧 `ProgressiveToolRail` 从上游已废弃的 `search_tools`+`load_tools`+可见工具白名单，整体改写为上游现行语义：注册期 exposure 策略 + BM25 的 `tool_search` + 固定包装器 `tool_call` + 目录附件 delta/snapshot。

**Architecture:** 曝光策略下沉到 `AbilityManager`（注册期一次定死，写入 `ToolCard.Exposure` 并锁定）；检索与目录同步留在 `ProgressiveToolRail`（内存 BM25 索引 + 会话状态 + system attachment）；`tool_call` 通过 `AbilityManager.Execute` 嵌套执行，保留目标工具自己的 rail/权限/超时/中断。BM25 算法从 `recall/bm25.py` 一比一移植，`tool_discovery/bm25.go` 只做 ToolInfo 适配。

**Tech Stack:** Go 1.22 / `internal/agentcore`（foundation/tool、single_agent/ability、context_engine、harness/rails、harness/prompts）/ testify / 集成测试 `//go:build integration` + `suite.Run` / MockLLM

**Spec:** `docs/superpowers/specs/2026-11-05-progressive-deferred-tool-catalog-design.md`

---

## 前置事实（执行前必读）

| 事实 | 出处 |
|---|---|
| Python 基线 HEAD `d6a81548`，源码在 `~/opensource/agent-core` | 本地可直读 |
| 渐进开关当前**无任何开启点**（`ProgressiveToolEnabled` 默认 false，仅一处注释提及） | `grep -rn ProgressiveToolEnabled --include=*.go .` |
| `AbilityManager.Execute` 已支持 `cbc.ForkForToolCall`，且 `ForkForToolCall` **快照父 Extra** | `single_agent/interfaces/callback.go:666-689` |
| `AbilityManagerInterface` 有 3 个实现方（`AbilityManager`、`P2PAbilityManager`、测试 fake）→ **不要**往该接口加方法，改用本计划新增的两个单方法窄接口 | `interfaces/abilitymgr.go:19` |
| 上游 `remove` 移除普通工具**不涨** registry_revision（既有事实） | `ability_manager.py:884-948` |
| `internal/` 全仓无 `json.Decoder.DisallowUnknownFields` → 删旧配置字段后旧键被静默忽略 | 已核 |
| 单 go 进程互斥：编译前 `pgrep -f 'go (build|test)'`，有残留先 kill | 项目规则 |

常用命令：

```bash
export GOPROXY=https://goproxy.cn,direct
go build ./... && go vet ./...
go test ./internal/agentcore/foundation/tool/ -v
go test -cover ./internal/agentcore/harness/rails/
go test -tags=integration ./tests/integration/agentcore/harness/progressive_tool/ -v
go run ./cmd/integration-audit -verify
```

## 文件结构

| 文件 | 责任 |
|---|---|
| `internal/agentcore/foundation/tool/exposure.go`（新） | `ToolExposure` 枚举 + `EffectiveExposure` |
| `internal/agentcore/foundation/tool/callback_context.go`（新） | `ToolCallbackContext` / `CallbackContextAcceptor` |
| `internal/agentcore/foundation/tool/base.go`（改） | `ToolCard.Exposure` + `exposureDeclared` 三态；`ToolCallOptions.CallbackContext` + `WithToolCallbackContext` |
| `internal/agentcore/single_agent/interfaces/exposure.go`（新） | `ExposurePolicyController` / `RegistryRevisionProvider` 两个窄接口 |
| `internal/agentcore/single_agent/ability/ability_manager.go`（改） | 策略字段 + `SetToolExposurePolicy` + `applyToolExposurePolicy` + `RegistryRevision` + 执行链线程化 cbc |
| `internal/agentcore/context_engine/processor/compressor/recall/bm25.go`（新） | `normalizeMarkdown` / `tokenize` / `BM25Index` / `rankBM25` |
| `internal/agentcore/harness/tools/tool_discovery/bm25.go`（新） | `BM25ToolIndex`：build/fingerprint/toolFingerprint/search |
| `internal/agentcore/harness/tools/tool_discovery/tool_search.go`（改写） | `ToolSearchTool`（名 `tool_search`） |
| `internal/agentcore/harness/tools/tool_discovery/tool_call.go`（改写） | `ToolCallTool` + `RelayedToolOutput`（名 `tool_call`） |
| `internal/agentcore/harness/prompts/tools/tool_search.go`、`tool_call.go`（新） | 两卡 cn/en 文案 |
| `internal/agentcore/harness/prompts/sections/progressive_tool_rail.go`（改写） | 规则正文 + snapshot/delta 渲染 + 摘要压缩 |
| `internal/agentcore/harness/prompts/attachment_delivery.go`（改） | `HasHistorySnapshot` 导出 |
| `internal/agentcore/harness/schema/config.go`（改） | `ToolSearchLimit`；删三个旧字段 |
| `internal/agentcore/harness/deep_agent.go`（改） | 默认 direct 名单 + 策略接线 + startup 收尾钩子 |
| `internal/agentcore/harness/rails/progressive.go`（重写） | Rail 主体 |
| `tests/integration/agentcore/harness/progressive_tool/*`（新） | IT-1~IT-7 |

---

## Task 1: ToolExposure 枚举与卡片曝光字段

**Files:**
- Create: `internal/agentcore/foundation/tool/exposure.go`
- Modify: `internal/agentcore/foundation/tool/base.go`（`ToolCard` 结构体 + 新方法）
- Test: `internal/agentcore/foundation/tool/exposure_test.go`

- [ ] **Step 1: 写失败测试**

```go
package tool

import "testing"

func TestToolCard_EffectiveExposure(t *testing.T) {
	card := &ToolCard{}
	if got := card.EffectiveExposure(); got != ToolExposureDirect {
		t.Fatalf("零值应回落 direct，got %s", got)
	}
	card.Exposure = ToolExposureDeferred
	if got := card.EffectiveExposure(); got != ToolExposureDeferred {
		t.Fatalf("应返回 deferred，got %s", got)
	}
}

func TestToolCard_ExposureDeclared(t *testing.T) {
	card := &ToolCard{}
	if _, ok := card.GetExposureDeclared(); ok {
		t.Fatal("初始应为未声明")
	}
	card.SetExposureDeclared(false)
	if v, ok := card.GetExposureDeclared(); !ok || v {
		t.Fatalf("显式声明 false 应可读回，got (%v,%v)", v, ok)
	}
	card.SetExposureDeclared(true)
	if v, ok := card.GetExposureDeclared(); !ok || !v {
		t.Fatalf("显式声明 true 应可读回，got (%v,%v)", v, ok)
	}
}

func TestToolExposure_常量值(t *testing.T) {
	if ToolExposureDirect != "direct" || ToolExposureDeferred != "deferred" {
		t.Fatalf("常量取值须与上游一致")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/foundation/tool/ -run 'TestTool(Card_EffectiveExposure|Card_ExposureDeclared|Exposure_常量值)' -v`
Expected: FAIL，`undefined: ToolExposureDirect` 等

- [ ] **Step 3: 新建 `exposure.go`**

```go
package tool

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ToolExposure 描述工具何时可出现在模型可见的工具清单中。
//
// 该值属于运行时策略元数据，不进入模型可见的 ToolInfo schema。
//
// Python: openjiuwen/core/foundation/tool/exposure.py (ToolExposure)
type ToolExposure string

const (
	// ToolExposureDirect 直接可调用（出现在顶层 tools）
	ToolExposureDirect ToolExposure = "direct"
	// ToolExposureDeferred 延迟暴露（须先经 tool_search 发现）
	ToolExposureDeferred ToolExposure = "deferred"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// EffectiveExposure 返回卡片生效的曝光策略，零值按 direct 处理。
//
// Python: ToolCard.exposure 默认 ToolExposure.DIRECT
func (c *ToolCard) EffectiveExposure() ToolExposure {
	if c.Exposure == "" {
		return ToolExposureDirect
	}
	return c.Exposure
}

// GetExposureDeclared 返回曝光策略是否已在注册期解析并锁定。
//
// 二元返回对齐 Python 的 bool|None：Ok=false 表示从未声明。
func (c *ToolCard) GetExposureDeclared() (value bool, ok bool) {
	return c.exposureDeclared, c.exposureDeclaredSet
}

// SetExposureDeclared 标记曝光策略已在注册期解析并锁定。
func (c *ToolCard) SetExposureDeclared(value bool) {
	c.exposureDeclared = value
	c.exposureDeclaredSet = true
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 在 `base.go` 的 `ToolCard` 增加字段**

```go
type ToolCard struct {
	schema.BaseCard
	// InputParams 输入参数定义，用于校验和生成 ToolInfo 传给 LLM
	InputParams []*schema.Param
	// Properties 扩展属性
	Properties map[string]any
	// Exposure 注册期解析的曝光策略（direct/deferred），零值等价 direct
	Exposure ToolExposure `json:"-"`
	// exposureDeclared 曝光策略是否已解析并锁定
	exposureDeclared bool
	// exposureDeclaredSet exposureDeclared 是否被显式设置过（对齐 Python bool|None）
	exposureDeclaredSet bool
}
```

- [ ] **Step 5: 运行确认通过**

Run: `go test ./internal/agentcore/foundation/tool/ -run 'TestTool' -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/foundation/tool/exposure.go internal/agentcore/foundation/tool/exposure_test.go internal/agentcore/foundation/tool/base.go
git commit -m "feat(tool): 新增 ToolExposure 枚举与卡片曝光字段"
```

---

## Task 2: 回调上下文窄接口与注入选项

**Files:**
- Create: `internal/agentcore/foundation/tool/callback_context.go`
- Modify: `internal/agentcore/foundation/tool/base.go`（`ToolCallOptions` + Option 函数）
- Test: `internal/agentcore/foundation/tool/callback_context_test.go`

- [ ] **Step 1: 写失败测试**

```go
package tool

import (
	"testing"

	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// fakeCallbackContext 测试用回调上下文
type fakeCallbackContext struct {
	extra   map[string]any
	session sessioninterfaces.SessionFacade
}

func (f *fakeCallbackContext) Extra() map[string]any { return f.extra }

func (f *fakeCallbackContext) Session() sessioninterfaces.SessionFacade { return f.session }

func TestWithToolCallbackContext(t *testing.T) {
	fake := &fakeCallbackContext{extra: map[string]any{"k": "v"}}
	opts := &ToolCallOptions{}
	WithToolCallbackContext(fake)(opts)
	if opts.CallbackContext == nil {
		t.Fatal("选项应写入 CallbackContext")
	}
	if got := opts.CallbackContext.Extra()["k"]; got != "v" {
		t.Fatalf("Extra 应透传，got %v", got)
	}
}

func TestWithToolCallbackContext_nil不覆盖(t *testing.T) {
	fake := &fakeCallbackContext{extra: map[string]any{}}
	opts := &ToolCallOptions{}
	WithToolCallbackContext(fake)(opts)
	WithToolCallbackContext(nil)(opts)
	if opts.CallbackContext == nil {
		t.Fatal("nil 不应覆盖已有值")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/foundation/tool/ -run TestWithToolCallbackContext -v`
Expected: FAIL，`undefined: WithToolCallbackContext`

- [ ] **Step 3: 新建 `callback_context.go`**

```go
package tool

import sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCallbackContext 工具执行期回调上下文的最小契约。
//
// Go 无 Python 的 **kwargs，等价物是执行期注入的调用选项：
// Python 侧为 `accepts_tool_callback_context = True` +
// `invoke_kwargs["_tool_callback_context"] = callback_context`。
//
// 由 single_agent/interfaces.AgentCallbackContext 天然实现。
type ToolCallbackContext interface {
	// Extra 返回调用级扩展数据
	Extra() map[string]any
	// Session 返回会话门面
	Session() sessioninterfaces.SessionFacade
}

// CallbackContextAcceptor 声明工具需要执行期回调上下文。
//
// Python: getattr(tool, "accepts_tool_callback_context", False)
type CallbackContextAcceptor interface {
	// AcceptsToolCallbackContext 是否需要注入回调上下文
	AcceptsToolCallbackContext() bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// WithToolCallbackContext 注入执行期回调上下文（nil 不覆盖）。
//
// Python: invoke_kwargs["_tool_callback_context"]
func WithToolCallbackContext(cb ToolCallbackContext) ToolOption {
	return func(o *ToolCallOptions) {
		if cb != nil {
			o.CallbackContext = cb
		}
	}
}
```

- [ ] **Step 4: 在 `base.go` 的 `ToolCallOptions` 增加字段**

```go
	// Session 会话实例（对齐 Python kwargs["session"]）
	Session interfaces.SessionFacade
	// CallbackContext 执行期回调上下文（对齐 Python kwargs["_tool_callback_context"]）
	CallbackContext ToolCallbackContext
```

- [ ] **Step 5: 运行确认通过**

Run: `go test ./internal/agentcore/foundation/tool/ -run TestWithToolCallbackContext -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/foundation/tool/callback_context.go internal/agentcore/foundation/tool/callback_context_test.go internal/agentcore/foundation/tool/base.go
git commit -m "feat(tool): 新增 ToolCallbackContext 窄接口与注入选项"
```

---

## Task 3: AbilityManager 曝光策略与注册版本

**Files:**
- Create: `internal/agentcore/single_agent/interfaces/exposure.go`
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go`
- Test: `internal/agentcore/single_agent/ability/exposure_test.go`

- [ ] **Step 1: 写失败测试**

```go
package ability

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

func newCard(name string) *tool.ToolCard {
	return &tool.ToolCard{}
}

func TestApplyToolExposurePolicy_四分支(t *testing.T) {
	am := NewAbilityManager(nil)

	// ① 未开启渐进 → 全 direct
	card := newCard("x")
	if got := am.applyToolExposurePolicy(card); got != tool.ToolExposureDirect {
		t.Fatalf("关闭渐进应 direct，got %s", got)
	}

	// ② 开启渐进 → 非名单 deferred
	am.SetToolExposurePolicy(true, []string{"read_file"})
	card = newCard("xlsx_read")
	if got := am.applyToolExposurePolicy(card); got != tool.ToolExposureDeferred {
		t.Fatalf("开启渐进且不在名单应 deferred，got %s", got)
	}
	if v, ok := card.GetExposureDeclared(); !ok || !v {
		t.Fatal("解析后应锁定声明")
	}

	// ③ 名单内 → direct
	card = newCard("read_file")
	if got := am.applyToolExposurePolicy(card); got != tool.ToolExposureDirect {
		t.Fatalf("名单内应 direct，got %s", got)
	}

	// ④ 已声明卡片不被改写
	declared := newCard("y")
	declared.Exposure = tool.ToolExposureDirect
	declared.SetExposureDeclared(true)
	if got := am.applyToolExposurePolicy(declared); got != tool.ToolExposureDirect {
		t.Fatalf("已声明应保持 direct，got %s", got)
	}
}

func TestAbilityManager_RegistryRevision(t *testing.T) {
	am := NewAbilityManager(nil)
	base := am.RegistryRevision()

	am.Add(newCard("a"))
	if am.RegistryRevision() != base+1 {
		t.Fatalf("新增工具应 +1，got %d", am.RegistryRevision())
	}

	// 上游既有行为：移除普通工具不涨版本
	revisionAfterAdd := am.RegistryRevision()
	am.Remove("a")
	if am.RegistryRevision() != revisionAfterAdd {
		t.Fatalf("移除普通工具不应涨版本，got %d", am.RegistryRevision())
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/ability/ -run 'TestApplyToolExposurePolicy_四分支|TestAbilityManager_RegistryRevision' -v`
Expected: FAIL，`am.applyToolExposurePolicy undefined`

- [ ] **Step 3: 新建 `single_agent/interfaces/exposure.go`**

```go
package interfaces

// ──────────────────────────── 结构体 ────────────────────────────

// ExposurePolicyController 注册期曝光策略控制。
//
// Python: AbilityManager.set_tool_exposure_policy
type ExposurePolicyController interface {
	// SetToolExposurePolicy 设置后续注册使用的默认曝光策略
	SetToolExposurePolicy(progressiveToolEnabled bool, directToolNames []string)
}

// RegistryRevisionProvider 注册表版本读取。
//
// Python: AbilityManager.registry_revision
type RegistryRevisionProvider interface {
	// RegistryRevision 返回注册表单调递增版本
	RegistryRevision() int
}
```

- [ ] **Step 4: 在 `AbilityManager` 增加字段与三个方法**

结构体字段（`contextEngine` 之后）：

```go
	// registryRevision 注册表单调递增版本，供 startup 建立的索引复用判断
	registryRevision int
	// progressiveToolEnabled 渐进式工具暴露开关（注册期策略）
	progressiveToolEnabled bool
	// directToolNames 渐进模式下保持直接可调用的工具名集合
	directToolNames map[string]struct{}
```

`NewAbilityManager` 初始化：

```go
	return &AbilityManager{
		tools:           make(map[string]*tool.ToolCard),
		workflows:       make(map[string]*schema.WorkflowCard),
		agents:          make(map[string]*agentschema.AgentCard),
		mcpServers:      make(map[string]*mcptypes.McpServerConfig),
		resourceMgr:     resourceMgr,
		directToolNames: map[string]struct{}{"tool_search": {}},
	}
```

新增方法（放在 `SetContextEngine` 之后）：

```go
// SetToolExposurePolicy 设置后续注册使用的默认曝光策略。
//
// 已注册卡片不会被改写：曝光是注册期决策，热重配只影响此后注册的卡片。
//
// Python: AbilityManager.set_tool_exposure_policy
func (am *AbilityManager) SetToolExposurePolicy(progressiveToolEnabled bool, directToolNames []string) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.progressiveToolEnabled = progressiveToolEnabled
	if directToolNames == nil {
		return
	}
	names := make(map[string]struct{}, len(directToolNames))
	for _, name := range directToolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			names[trimmed] = struct{}{}
		}
	}
	am.directToolNames = names
}

// RegistryRevision 返回注册表版本号。
func (am *AbilityManager) RegistryRevision() int {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.registryRevision
}

// applyToolExposurePolicy 在注册期解析并写回卡片曝光策略（调用方须持锁）。
//
// 显式声明优先；否则命中 direct 名单 → direct；否则渐进开启 → deferred。
//
// Python: AbilityManager._apply_tool_exposure_policy
func (am *AbilityManager) applyToolExposurePolicy(card *tool.ToolCard) tool.ToolExposure {
	if declared, ok := card.GetExposureDeclared(); ok && declared {
		return card.EffectiveExposure()
	}
	var resolved tool.ToolExposure
	if _, direct := am.directToolNames[card.Name]; direct {
		resolved = tool.ToolExposureDirect
	} else if am.progressiveToolEnabled {
		resolved = tool.ToolExposureDeferred
	} else {
		resolved = tool.ToolExposureDirect
	}
	card.Exposure = resolved
	card.SetExposureDeclared(true)
	return resolved
}
```

- [ ] **Step 5: 在 `Add` 的四个存储分支接上策略与版本**

`*tool.ToolCard` 分支：在 `am.tools[a.Name] = a` **之前**插入 `am.applyToolExposurePolicy(a)`，赋值后 `am.registryRevision++`；`workflow` / `agent` / `mcpServer` 三处赋值后各加 `am.registryRevision++`。

```go
		am.applyToolExposurePolicy(a)
		am.tools[a.Name] = a
		am.registryRevision++
		return agentschema.AddAbilityResult{Name: a.Name, Added: true, Reason: "added_tool"}
```

- [ ] **Step 6: `Remove` 只在 MCP 分支涨版本（对齐上游）**

```go
	if mcpServer, ok := am.mcpServers[name]; ok {
		delete(am.mcpServers, name)
		// 级联删除该 MCP 服务器下的工具
		serverID := mcpServer.ServerID
		prefix := serverID + "."
		for toolName, toolCard := range am.tools {
			if toolCard.ID != "" && strings.HasPrefix(toolCard.ID, prefix) {
				delete(am.tools, toolName)
			}
		}
		// 对齐上游：仅 MCP 卸载路径推进注册版本；普通工具移除不推进
		am.registryRevision++
		return mcpServer
	}
```

- [ ] **Step 7: 运行确认通过**

Run: `go test ./internal/agentcore/single_agent/ability/ -v`
Expected: PASS（既有用例不得回归）

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/single_agent/interfaces/exposure.go internal/agentcore/single_agent/ability/ability_manager.go internal/agentcore/single_agent/ability/exposure_test.go
git commit -m "feat(ability): 注册期曝光策略与注册版本号"
```

---

## Task 4: 执行链线程化回调上下文

**Files:**
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go`（`runToolCallSafely` / `railedExecuteSingleToolCall` / `executeSingleToolCall` / `executeTool`）
- Test: `internal/agentcore/single_agent/ability/execute_callback_context_test.go`

- [ ] **Step 1: 写失败测试**

```go
package ability

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// callbackSpy 只在被注入回调上下文时记录
type callbackSpy struct {
	accepted bool
	gotExtra map[string]any
}

func (c *callbackSpy) AcceptsToolCallbackContext() bool { return true }

func (c *callbackSpy) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	o := &tool.ToolCallOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if o.CallbackContext != nil {
		c.accepted = true
		c.gotExtra = o.CallbackContext.Extra()
	}
	return map[string]any{"ok": true}, nil
}

func TestExecuteTool_注入回调上下文(t *testing.T) {
	spy := &callbackSpy{}
	card := newCard("spy_tool")
	am := NewAbilityManager(newTestResourceMgrWithTool(t, card, spy))

	am.SetToolExposurePolicy(true, nil)
	am.Add(card)

	cbc := newTestCallbackContext(map[string]any{"mark": "1"})
	res := am.Execute(context.Background(), cbc, []*llmschema.ToolCall{{
		ID: "c1", Type: "function", Name: "spy_tool", Arguments: "{}",
	}}, nil, "")

	if len(res) != 1 {
		t.Fatalf("应有一条结果，got %d", len(res))
	}
	if !spy.accepted {
		t.Fatal("实现 CallbackContextAcceptor 的工具应收到回调上下文")
	}
	if spy.gotExtra["mark"] != "1" {
		t.Fatalf("extra 应透传，got %v", spy.gotExtra)
	}
}
```

> 若 `newTestResourceMgrWithTool` / `newTestCallbackContext` 在既有测试文件中不存在，按 `ability_manager_test.go` 现有构造方式补两个未导出助手（放在本测试文件底部），二者只做 `resourcesmanager.AddTool` 与 `interfaces.NewAgentCallbackContext` 的最小装配。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/single_agent/ability/ -run TestExecuteTool_注入回调上下文 -v`
Expected: FAIL，`spy.accepted == false`

- [ ] **Step 3: 修改四个函数的签名与调用点**

```go
// runToolCallSafely 内两处调用改为：
		r, err := am.executeSingleToolCall(ctx, cbc, toolCall, sess, tag)
	...
	return am.railedExecuteSingleToolCall(ctx, cbc, toolCall, sess, tag)

// railedExecuteSingleToolCall 内：
		result, err = am.executeSingleToolCall(ctx, toolCtx, toolCall, sess, tag)

// executeSingleToolCall 签名与转发：
func (am *AbilityManager) executeSingleToolCall(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	tag string,
) (result agentschema.ExecuteResult, err error) {
	...
		return am.executeTool(ctx, cbc, toolCall, toolName, toolArgs, sess, tag)

// executeTool 签名与注入：
func (am *AbilityManager) executeTool(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	toolName string,
	toolArgs map[string]any,
	sess sessioninterfaces.SessionFacade,
	tag string,
) (agentschema.ExecuteResult, error) {
```

- [ ] **Step 4: 在 `executeTool` 里按 acceptor 门卫注入**

```go
	lt := tool.NewLifecycleTool(t)
	opts := []tool.ToolOption{tool.WithToolSession(sess)}
	// 仅对显式声明需要回调上下文的工具注入（对齐 Python getattr 判断）
	if acceptor, ok := t.(tool.CallbackContextAcceptor); ok && acceptor.AcceptsToolCallbackContext() {
		opts = append(opts, tool.WithToolCallbackContext(cbc))
	}
	result, err := lt.Invoke(ctx, toolArgs, opts...)
```

- [ ] **Step 5: 运行确认通过**

Run: `go test ./internal/agentcore/single_agent/ability/ -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/single_agent/ability/
git commit -m "feat(ability): 执行链线程化回调上下文并按 acceptor 注入"
```

---

## Task 5: recall/bm25 移植

**Files:**
- Create: `internal/agentcore/context_engine/processor/compressor/recall/bm25.go`、`doc.go`
- Test: `internal/agentcore/context_engine/processor/compressor/recall/bm25_test.go`

- [ ] **Step 1: 写失败测试**

```go
package recall

import (
	"reflect"
	"testing"
)

func TestTokenize_中英混排(t *testing.T) {
	got := tokenize("读取 XLSX_Sheet 表格")
	want := map[string]bool{"xlsx": true, "sheet": true, "xlsx_sheet": true, "读": true, "取": true, "读取": true}
	for _, tok := range got {
		delete(want, tok)
	}
	if len(want) != 0 {
		t.Fatalf("缺少 token: %v，got=%v", want, got)
	}
}

func TestNormalizeMarkdown_去语法(t *testing.T) {
	input := "---\ntitle: x\n---\n# 标题\n- 条目 [链接](http://a)\n```go\ncode\n```"
	if got := normalizeMarkdown(input); got != "标题 条目 链接 http://a code" {
		t.Fatalf("got %q", got)
	}
}

func TestBM25Index_Scores(t *testing.T) {
	idx := BM25IndexBuild([]string{"xlsx read sheet", "pdf extract text"}, 1.5, 0.75)
	scores := idx.Scores("xlsx")
	if len(scores) != 2 {
		t.Fatalf("应有 2 个分数，got %d", len(scores))
	}
	if scores[0] <= 0 || scores[1] != 0 {
		t.Fatalf("首篇应命正分、次篇应为 0，got %v", scores)
	}
}

func TestBM25Index_空输入(t *testing.T) {
	idx := BM25IndexBuild(nil, 1.5, 0.75)
	if !reflect.DeepEqual(idx.Scores("x"), []float64{}) {
		t.Fatal("空索引应返回空分数")
	}
	if idx.DocumentCount() != 0 {
		t.Fatal("文档数应为 0")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/context_engine/processor/compressor/recall/ -v`
Expected: FAIL，包不存在

- [ ] **Step 3: 新建 `bm25.go`（一比一移植 `recall/bm25.py`）**

```go
// Package recall 提供压缩召回的检索原语（BM25 索引与分词）。
//
// Python: openjiuwen/core/context_engine/processor/forked/compressor/recall/
//
// 文件目录：
//
//	recall/
//	├── doc.go    # 包文档
//	└── bm25.go   # BM25Index + 中英混排分词
package recall

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BM25Index 固定文档集合上的不可变 BM25 索引。
//
// Python: recall/bm25.py (BM25Index)
type BM25Index struct {
	// tokenized 分词后的文档集合
	tokenized [][]string
	// documentFrequency 词 -> 出现该词的文档数
	documentFrequency map[string]int
	// documentCount 文档数
	documentCount int
	// averageLength 文档平均长度
	averageLength float64
	// k1 BM25 饱和参数
	k1 float64
	// b BM25 长度归一参数
	b float64
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultK1 BM25 默认饱和参数
	defaultK1 = 1.5
	// defaultB BM25 默认长度归一参数
	defaultB = 0.75
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// asciiTokenRe ASCII 标识符分词
	asciiTokenRe = regexp.MustCompile(`[a-z0-9_]+`)
	// cjkSequenceRe CJK 连续片段
	cjkSequenceRe = regexp.MustCompile(`[\x{3400}-\x{4dbf}\x{4e00}-\x{9fff}]+`)
	// yamlFrontmatterRe YAML frontmatter
	yamlFrontmatterRe = regexp.MustCompile(`(?s)\A---\s*\n.*?\n---\s*(?:\n|\z)`)
	// markdownFenceRe 代码围栏
	markdownFenceRe = regexp.MustCompile(`(?m)^\s*` + "```" + `[^\n]*$`)
	// markdownHeadingRe 标题标记
	markdownHeadingRe = regexp.MustCompile(`(?m)^\s{0,3}#{1,6}\s*`)
	// markdownListRe 列表标记
	markdownListRe = regexp.MustCompile(`(?m)^\s*(?:[-+*]|\d+[.)])\s+`)
	// markdownLinkRe 链接
	markdownLinkRe = regexp.MustCompile(`!?\[([^\]]*)\]\(([^)]+)\)`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeMarkdown 去掉 Markdown 语法但保留可见文本与代码内容。
//
// Python: normalize_markdown
func NormalizeMarkdown(text string) string {
	normalized := yamlFrontmatterRe.ReplaceAllString(text, "")
	normalized = markdownFenceRe.ReplaceAllString(normalized, "")
	normalized = markdownLinkRe.ReplaceAllString(normalized, "$1 $2")
	normalized = markdownHeadingRe.ReplaceAllString(normalized, "")
	normalized = markdownListRe.ReplaceAllString(normalized, "")
	return strings.Join(strings.Fields(normalized), " ")
}

// Tokenize 对中英混排、路径与代码标识符分词。
//
// Python: tokenize
func Tokenize(text string) []string {
	return tokenize(text)
}

// BM25IndexBuild 构建索引。
//
// Python: BM25Index.build
func BM25IndexBuild(documents []string, k1 float64, b float64) *BM25Index {
	tokenized := make([][]string, len(documents))
	df := make(map[string]int)
	for i, doc := range documents {
		tokens := tokenize(doc)
		tokenized[i] = tokens
		seen := make(map[string]struct{}, len(tokens))
		for _, tok := range tokens {
			if _, ok := seen[tok]; ok {
				continue
			}
			seen[tok] = struct{}{}
			df[tok]++
		}
	}
	idx := &BM25Index{
		tokenized:         tokenized,
		documentFrequency: df,
		documentCount:     len(tokenized),
		k1:                k1,
		b:                 b,
	}
	if idx.documentCount > 0 {
		total := 0
		for _, doc := range tokenized {
			total += len(doc)
		}
		idx.averageLength = float64(total) / float64(idx.documentCount)
	}
	return idx
}

// RankBM25 对一组文档一次性打分。
//
// Python: rank_bm25
func RankBM25(query string, documents []string, k1 float64, b float64) []float64 {
	return BM25IndexBuild(documents, k1, b).Scores(query)
}

// DocumentCount 返回索引文档数。
func (idx *BM25Index) DocumentCount() int { return idx.documentCount }

// Scores 返回每个文档一个 BM25 分数。
//
// Python: BM25Index.scores
func (idx *BM25Index) Scores(query string) []float64 {
	if idx.documentCount == 0 {
		return []float64{}
	}
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 || idx.averageLength <= 0 {
		return make([]float64, idx.documentCount)
	}
	uniqueQuery := make(map[string]struct{}, len(queryTokens))
	for _, tok := range queryTokens {
		uniqueQuery[tok] = struct{}{}
	}
	scores := make([]float64, 0, idx.documentCount)
	for _, document := range idx.tokenized {
		frequencies := make(map[string]int, len(document))
		for _, tok := range document {
			frequencies[tok]++
		}
		documentLength := float64(len(document))
		score := 0.0
		for token := range uniqueQuery {
			frequency := frequencies[token]
			if frequency <= 0 {
				continue
			}
			df := idx.documentFrequency[token]
			idf := math.Log(1.0 + (float64(idx.documentCount-df)+0.5)/(float64(df)+0.5))
			denominator := float64(frequency) + idx.k1*(1.0-idx.b+idx.b*documentLength/idx.averageLength)
			score += idf * float64(frequency) * (idx.k1 + 1.0) / denominator
		}
		scores = append(scores, score)
	}
	return scores
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tokenize 小写化后切 ASCII 标识符与 CJK 片段。
//
// Python: tokenize
func tokenize(text string) []string {
	lowered := strings.ToLower(text)
	tokens := asciiTokenRe.FindAllString(lowered, -1)
	expanded := make([]string, 0, len(tokens)*2)
	expanded = append(expanded, tokens...)
	for _, token := range tokens {
		if !strings.Contains(token, "_") {
			continue
		}
		for _, part := range strings.Split(token, "_") {
			if part != "" {
				expanded = append(expanded, part)
			}
		}
	}
	for _, sequence := range cjkSequenceRe.FindAllString(lowered, -1) {
		runes := []rune(sequence)
		for _, r := range runes {
			expanded = append(expanded, string(r))
		}
		for i := 0; i+1 < len(runes); i++ {
			expanded = append(expanded, string(runes[i:i+2]))
		}
	}
	return expanded
}

// sortStrings 稳定排序助手（供上层按名输出确定性顺序）
func sortStrings(values []string) []string {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	return sorted
}
```

- [ ] **Step 4: 建 `doc.go`（文件树 + 上游路径 + 已知偏差说明）**

```go
// Package recall 提供压缩召回的检索原语。
//
// 当前只落地 BM25 索引与分词；archive/retriever 与 compression_recall 工具
// 属"压缩后召回原文"能力，另开章节实施。
//
// 文件目录：
//
//	recall/
//	├── doc.go    # 包文档
//	└── bm25.go   # BM25Index + 中英混排分词
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/compressor/recall/
//
// 已知偏差：Go 侧未建 forked 目录层（5.22-5.26 已按非 forked 家族对齐），
// 故本包落在 processor/compressor/recall/ 下。
package recall
```

- [ ] **Step 5: 运行确认通过**

Run: `go test ./internal/agentcore/context_engine/processor/compressor/recall/ -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/context_engine/processor/compressor/recall/
git commit -m "feat(recall): 移植 BM25Index 与中英混排分词（5.32）"
```

---

## Task 6: BM25ToolIndex 适配层

**Files:**
- Create: `internal/agentcore/harness/tools/tool_discovery/bm25.go`
- Test: `internal/agentcore/harness/tools/tool_discovery/bm25_test.go`

- [ ] **Step 1: 写失败测试**

```go
package tool_discovery

import (
	"testing"

	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

func testToolInfo(name, description string, params map[string]any) cschema.ToolInfoInterface {
	info := cschema.ToolInfo{}
	info.Type = "function"
	info.Name = name
	info.Description = description
	info.Parameters = params
	return &info
}

func TestBM25ToolIndex_精确名优先(t *testing.T) {
	docs := []cschema.ToolInfoInterface{
		testToolInfo("search_skill", "搜索技能目录", nil),
		testToolInfo("helper", "search skill for everything search skill", nil),
	}
	idx := BuildBM25ToolIndex(docs, nil)
	got := idx.Search("search_skill", 5)
	if len(got) == 0 || got[0].GetName() != "search_skill" {
		t.Fatalf("精确名应排首位，got %v", names(got))
	}
}

func TestBM25ToolIndex_名字边界(t *testing.T) {
	docs := []cschema.ToolInfoInterface{testToolInfo("search_skill_extra", "扩展搜索", nil)}
	idx := BuildBM25ToolIndex(docs, nil)
	if got := idx.Search("请调用search_skill搜索", 5); len(got) != 0 {
		t.Fatalf("不应把 search_skill_extra 当作精确匹配，got %v", names(got))
	}
}

func TestBM25ToolIndex_Fingerprint随参数变化(t *testing.T) {
	base := testToolInfo("x", "d", map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}})
	changed := testToolInfo("x", "d", map[string]any{"type": "object", "properties": map[string]any{"b": map[string]any{"type": "string"}}})
	if ToolFingerprint(base) == ToolFingerprint(changed) {
		t.Fatal("参数变化应改变指纹")
	}
	same := testToolInfo("x", "d", map[string]any{"properties": map[string]any{"a": map[string]any{"type": "string"}}, "type": "object"})
	if ToolFingerprint(base) != ToolFingerprint(same) {
		t.Fatal("参数 key 顺序不应影响指纹")
	}
}

func TestBM25ToolIndex_extraSearchText参与(t *testing.T) {
	docs := []cschema.ToolInfoInterface{testToolInfo("t1", "无关描述", nil)}
	idx := BuildBM25ToolIndex(docs, []string{"电子表格 excel"})
	if got := idx.Search("excel", 5); len(got) != 1 {
		t.Fatalf("额外检索文本应参与匹配，got %v", names(got))
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/tools/tool_discovery/ -run TestBM25ToolIndex -v`
Expected: FAIL，`undefined: BuildBM25ToolIndex`

- [ ] **Step 3: 新建 `bm25.go`（移植 `tool_discovery/bm25.py`）**

```go
package tool_discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/compressor/recall"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BM25ToolIndex 模型可见工具 schema 上的内存 BM25 索引。
//
// build 之后不可变；元数据变化时整体替换实例。
//
// Python: harness/tools/tool_discovery/bm25.py (BM25ToolIndex)
type BM25ToolIndex struct {
	// documents 文档集合（与 index 顺序一致）
	documents []cschema.ToolInfoInterface
	// index BM25 索引
	index *recall.BM25Index
	// sourceFingerprint 建立索引时的元数据指纹
	sourceFingerprint string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildBM25ToolIndex 构建索引；extraSearchTexts 与 documents 必须等长（或为 nil）。
//
// Python: BM25ToolIndex.build
func BuildBM25ToolIndex(documents []cschema.ToolInfoInterface, extraSearchTexts []string) *BM25ToolIndex {
	texts := make([]string, len(documents))
	for i, document := range documents {
		extra := ""
		if extraSearchTexts != nil && i < len(extraSearchTexts) {
			extra = extraSearchTexts[i]
		}
		texts[i] = strings.TrimSpace(toolText(document) + " " + extra)
	}
	index := recall.BM25IndexBuild(texts, 1.5, 0.75)
	return &BM25ToolIndex{
		documents:         documents,
		index:             index,
		sourceFingerprint: Fingerprint(documents, extraSearchTexts),
	}
}

// Fingerprint 返回完整文档集合的确定性指纹。
//
// Python: BM25ToolIndex.fingerprint
func Fingerprint(documents []cschema.ToolInfoInterface, extraSearchTexts []string) string {
	pairs := make([]string, 0, len(documents))
	for i, document := range documents {
		extra := ""
		if extraSearchTexts != nil && i < len(extraSearchTexts) {
			extra = extraSearchTexts[i]
		}
		pairs = append(pairs, ToolFingerprint(document)+"\x00"+extra)
	}
	sort.Strings(pairs)
	payload, _ := json.Marshal(pairs)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// ToolFingerprint 返回单个工具记录的确定性指纹。
//
// Python: BM25ToolIndex.tool_fingerprint
func ToolFingerprint(document cschema.ToolInfoInterface) string {
	payload, _ := json.Marshal([]string{
		document.GetType(),
		document.GetName(),
		document.GetDescription(),
		parametersText(document.GetParameters()),
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// DocumentCount 返回索引文档数。
func (idx *BM25ToolIndex) DocumentCount() int { return idx.index.DocumentCount() }

// SourceFingerprint 返回建立索引时的元数据指纹。
func (idx *BM25ToolIndex) SourceFingerprint() string { return idx.sourceFingerprint }

// Documents 返回索引持有的文档副本。
func (idx *BM25ToolIndex) Documents() []cschema.ToolInfoInterface {
	return append([]cschema.ToolInfoInterface(nil), idx.documents...)
}

// Search 检索：分数为正或精确名命中的文档，按
// (非精确名, -分数, 名称, 原始序) 排序后截取 limit。
//
// Python: BM25ToolIndex.search
func (idx *BM25ToolIndex) Search(query string, limit int) []cschema.ToolInfoInterface {
	if limit <= 0 || query == "" || len(idx.documents) == 0 {
		return nil
	}
	scores := idx.index.Scores(query)
	type candidate struct {
		exact bool
		score float64
		index int
		doc   cschema.ToolInfoInterface
	}
	candidates := make([]candidate, 0, len(scores))
	for i, score := range scores {
		document := idx.documents[i]
		exact := containsExactToolName(query, document.GetName())
		if score <= 0 && !exact {
			continue
		}
		candidates = append(candidates, candidate{exact: exact, score: score, index: i, doc: document})
	}
	sort.SliceStable(candidates, func(a, b int) bool {
		left, right := candidates[a], candidates[b]
		if left.exact != right.exact {
			return left.exact
		}
		if left.score != right.score {
			return left.score > right.score
		}
		if left.doc.GetName() != right.doc.GetName() {
			return left.doc.GetName() < right.doc.GetName()
		}
		return left.index < right.index
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	result := make([]cschema.ToolInfoInterface, 0, len(candidates))
	for _, item := range candidates {
		result = append(result, item.doc)
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toolText 拼出参与检索的工具文本。
func toolText(document cschema.ToolInfoInterface) string {
	return strings.Join([]string{
		document.GetName(),
		document.GetDescription(),
		parametersText(document.GetParameters()),
	}, " ")
}

// parametersText 序列化参数 schema（key 排序保证稳定）。
func parametersText(parameters map[string]any) string {
	if parameters == nil {
		return ""
	}
	payload, err := json.Marshal(parameters)
	if err != nil {
		return ""
	}
	return string(payload)
}

// containsExactToolName 判断 query 是否以完整标识符边界命中工具名。
//
// Python: _contains_exact_tool_name
func containsExactToolName(query string, toolName string) bool {
	normalizedQuery := strings.ToLower(query)
	normalizedName := strings.TrimSpace(strings.ToLower(toolName))
	if normalizedQuery == "" || normalizedName == "" {
		return false
	}
	start := 0
	for start < len(normalizedQuery) {
		index := strings.Index(normalizedQuery[start:], normalizedName)
		if index < 0 {
			return false
		}
		match := start + index
		var before, after byte
		if match > 0 {
			before = normalizedQuery[match-1]
		}
		end := match + len(normalizedName)
		if end < len(normalizedQuery) {
			after = normalizedQuery[end]
		}
		if !isToolNameBoundaryChar(before) && !isToolNameBoundaryChar(after) {
			return true
		}
		start = match + 1
	}
	return false
}

// isToolNameBoundaryChar 判断字符是否可作为工具标识符的一部分。
func isToolNameBoundaryChar(c byte) bool {
	if c == 0 {
		return false
	}
	if c >= 0x80 {
		return false
	}
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-'
}
```

- [ ] **Step 4: 测试辅助 `names`**

在 `bm25_test.go` 底部：

```go
func names(docs []cschema.ToolInfoInterface) []string {
	out := make([]string, 0, len(docs))
	for _, doc := range docs {
		out = append(out, doc.GetName())
	}
	return out
}
```

- [ ] **Step 5: 运行确认通过**

Run: `go test ./internal/agentcore/harness/tools/tool_discovery/ -run TestBM25ToolIndex -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/tools/tool_discovery/bm25.go internal/agentcore/harness/tools/tool_discovery/bm25_test.go
git commit -m "feat(tool_discovery): BM25ToolIndex 适配层（指纹/精确名优先）"
```

---

## Task 7: 两个元工具的卡片文案

**Files:**
- Create: `internal/agentcore/harness/prompts/tools/tool_search.go`、`internal/agentcore/harness/prompts/tools/tool_call.go`
- Test: `internal/agentcore/harness/prompts/tools/tool_discovery_cards_test.go`

- [ ] **Step 1: 写失败测试**

```go
package tools

import (
	"strings"
	"testing"
)

func TestToolSearchCard_中英文案(t *testing.T) {
	cn := ToolSearchCard("cn")
	if cn.GetName() != "tool_search" {
		t.Fatalf("工具名应为 tool_search，got %s", cn.GetName())
	}
	if !strings.Contains(cn.GetDescription(), "搜索") {
		t.Fatalf("中文描述缺失，got %s", cn.GetDescription())
	}
	en := ToolSearchCard("en")
	if !strings.Contains(strings.ToLower(en.GetDescription()), "search") {
		t.Fatalf("英文描述缺失，got %s", en.GetDescription())
	}
}

func TestToolCallCard_中英文案(t *testing.T) {
	if ToolCallCard("cn").GetName() != "tool_call" {
		t.Fatal("工具名应为 tool_call")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/prompts/tools/ -run 'TestTool(Search|Call)Card_中英文案' -v`
Expected: FAIL，`undefined: ToolSearchCard`

- [ ] **Step 3: 实现两个卡片构造器（文案一比一复刻 `prompts/tools/tool_search.py`、`tool_call.py`）**

```go
package tools

import "github.com/uapclaw/uapclaw-go/internal/common/schema"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// toolSearchName 搜索元工具名
	toolSearchName = "tool_search"
	// toolSearchID 搜索元工具标识
	toolSearchID = "ToolSearchTool"
	// toolCallName 执行包装器名
	toolCallName = "tool_call"
	// toolCallID 执行包装器标识
	toolCallID = "ToolCallTool"

	// toolSearchDescCN 搜索元工具中文描述（对齐上游）
	toolSearchDescCN = "在延迟工具目录中搜索可用工具；返回完整参数 schema，随后用 tool_call 执行。"
	// toolSearchDescEN 搜索元工具英文描述（对齐上游）
	toolSearchDescEN = "Search deferred tools by intent; returns complete parameter schemas to execute via tool_call."
	// toolCallDescCN 执行包装器中文描述（对齐上游）
	toolCallDescCN = "执行 tool_search 返回的延迟工具；name 用搜索结果里的准确名称，args 按返回的 schema 传参。"
	// toolCallDescEN 执行包装器英文描述（对齐上游）
	toolCallDescEN = "Execute a deferred tool returned by tool_search; pass the exact name and schema-compatible args."
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ToolSearchCard 构建搜索元工具卡片。
func ToolSearchCard(language string) *schema.BaseCard {
	description := toolSearchDescCN
	if language == "en" {
		description = toolSearchDescEN
	}
	return &schema.BaseCard{ID: toolSearchID, Name: toolSearchName, Description: description}
}

// ToolCallCard 构建执行包装器卡片。
func ToolCallCard(language string) *schema.BaseCard {
	description := toolCallDescCN
	if language == "en" {
		description = toolCallDescEN
	}
	return &schema.BaseCard{ID: toolCallID, Name: toolCallName, Description: description}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

> 实施时把描述文本替换为 `openjiuwen/harness/prompts/tools/tool_search.py` 与 `tool_call.py` 中 `build_tool_card` 实际文案（cn/en 各一条），保持参数说明完全一致。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/harness/prompts/tools/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/prompts/tools/tool_search.go internal/agentcore/harness/prompts/tools/tool_call.go internal/agentcore/harness/prompts/tools/tool_discovery_cards_test.go
git commit -m "feat(prompts): tool_search/tool_call 卡片文案"
```

---

## Task 8: ToolSearchTool 改写

**Files:**
- Modify: `internal/agentcore/harness/tools/tool_discovery/tool_search.go`（由 `search_tools.go` 改名）
- Delete: `internal/agentcore/harness/tools/tool_discovery/search_tools.go`、`search_tools_test.go`
- Test: `internal/agentcore/harness/tools/tool_discovery/tool_search_test.go`

- [ ] **Step 1: 写失败测试**

```go
package tool_discovery

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeToolSearchLimit(t *testing.T) {
	cases := map[int]int{-1: 1, 0: 1, 1: 1, 5: 5, 20: 20, 100: 20}
	for input, want := range cases {
		if got := NormalizeToolSearchLimit(input); got != want {
			t.Fatalf("limit %d 应归一化为 %d，got %d", input, want, got)
		}
	}
	if got := NormalizeToolSearchLimit(0); got != 1 {
		t.Fatal("0 应夹到 1")
	}
}

func TestToolSearchTool_调用与渲染(t *testing.T) {
	called := 0
	search := func(_ context.Context, query string, limit int, _ any) ([]map[string]any, error) {
		called++
		if query != "excel" || limit != 3 {
			t.Fatalf("入参应透传，got query=%s limit=%d", query, limit)
		}
		return []map[string]any{{
			"name":        "xlsx_read",
			"description": "读取表格",
			"parameters":  map[string]any{"type": "object"},
		}}, nil
	}
	searchTool := NewToolSearchTool(search, "cn", "agent-1", 5)

	out, err := searchTool.Invoke(context.Background(), map[string]any{"query": "excel", "limit": 3})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if called != 1 {
		t.Fatalf("搜索回调应被调用一次，got %d", called)
	}
	if count, _ := out["count"].(int); count != 1 {
		t.Fatalf("count 应为 1，got %v", out["count"])
	}
	rendered := searchTool.RenderForLLM(out)
	if !strings.Contains(rendered, "xlsx_read") || !strings.Contains(rendered, `"type":"object"`) {
		t.Fatalf("渲染须含名称与精确 schema，got %s", rendered)
	}
}

func TestToolSearchTool_空查询(t *testing.T) {
	searchTool := NewToolSearchTool(func(context.Context, string, int, any) ([]map[string]any, error) {
		t.Fatal("空查询不应触发搜索")
		return nil, nil
	}, "cn", "", 5)
	out, err := searchTool.Invoke(context.Background(), map[string]any{"query": "  "})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if count, _ := out["count"].(int); count != 0 {
		t.Fatalf("空查询结果应为 0，got %v", out["count"])
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/tools/tool_discovery/ -run 'TestNormalizeToolSearchLimit|TestToolSearchTool' -v`
Expected: FAIL，`undefined: NewToolSearchTool`

- [ ] **Step 3: 删除旧实现并改写 `tool_search.go`**

```bash
git rm internal/agentcore/harness/tools/tool_discovery/search_tools.go internal/agentcore/harness/tools/tool_discovery/search_tools_test.go
```

```go
package tool_discovery

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolSearchTool 延迟工具发现元工具。
//
// Python: harness/tools/tool_discovery/tool_search.py (ToolSearchTool)
type ToolSearchTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// search 检索回调（query, limit）
	search func(ctx context.Context, query string, limit int) ([]map[string]any, error)
	// resultLimit 默认结果条数
	resultLimit int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultToolSearchLimit 默认搜索结果条数
	DefaultToolSearchLimit = 5
	// MaxToolSearchLimit 最大搜索结果条数
	MaxToolSearchLimit = 20
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeToolSearchLimit 把请求条数夹到 1..20。
//
// Python: _normalize_tool_search_limit
func NormalizeToolSearchLimit(value int) int {
	if value < 1 {
		return 1
	}
	if value > MaxToolSearchLimit {
		return MaxToolSearchLimit
	}
	return value
}

// NewToolSearchTool 创建搜索元工具。
//
// search 回调签名与 rail 对齐：仅传 query 与 limit，会话由 rail 自身持有。
func NewToolSearchTool(
	search func(ctx context.Context, query string, limit int) ([]map[string]any, error),
	language string,
	agentID string,
	resultLimit int,
) *ToolSearchTool {
	card := tools.ToolSearchCard(language)
	toolCard := &tool.ToolCard{
		BaseCard: *card,
		InputParams: []*schema.Param{
			schema.NewStringParam("query", "Query text used to find relevant tools", true),
			schema.NewNumberParam("limit", "Maximum number of tools to return", false),
		},
	}
	toolCard.Exposure = tool.ToolExposureDirect
	return &ToolSearchTool{
		card:        toolCard,
		search:      search,
		resultLimit: NormalizeToolSearchLimit(resultLimit),
	}
}

// Card 返回工具卡片。
func (t *ToolSearchTool) Card() *tool.ToolCard { return t.card }

// Invoke 执行搜索。
//
// Python: ToolSearchTool.invoke
func (t *ToolSearchTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	query, _ := inputs["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return map[string]any{"query": query, "results": []map[string]any{}, "count": 0}, nil
	}
	limit := t.resultLimit
	if raw, ok := inputs["limit"]; ok {
		if value, ok := toInt(raw); ok {
			limit = NormalizeToolSearchLimit(value)
		}
	}
	results, err := t.search(ctx, query, limit)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "tool_search_invoke_failed").
			Err(err).
			Msg("tool_search invoke failed")
		return nil, err
	}
	if len(results) > limit {
		results = results[:limit]
	}
	logger.Info(logComponent).
		Str("event_type", "tool_search_invoked").
		Str("query", query).
		Int("limit", limit).
		Int("match_count", len(results)).
		Msg("tool_search invoked")
	return map[string]any{"query": query, "results": results, "count": len(results)}, nil
}

// RenderForLLM 渲染匹配结果（必须给出精确 JSON schema）。
//
// Python: ToolSearchTool.render_for_llm
func (t *ToolSearchTool) RenderForLLM(out map[string]any) string {
	query, _ := out["query"].(string)
	results, _ := out["results"].([]map[string]any)
	if len(results) == 0 {
		return `No tools matched "` + query + `".`
	}
	sections := make([]string, 0, len(results))
	for _, item := range results {
		name, _ := item["name"].(string)
		description, _ := item["description"].(string)
		payload, _ := json.Marshal(item["parameters"])
		sections = append(sections, "## "+name+"\n"+description+"\nParameters: "+string(payload))
	}
	count := len(results)
	return itoa(count) + ` tool(s) matched "` + query + `".` + "\n\n" + strings.Join(sections, "\n\n")
}

// Stream 不支持流式。
func (t *ToolSearchTool) Stream(context.Context, map[string]any, ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toInt 把入参归一化为 int。
func toInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
}
```

> `logComponent`、`itoa`、`schema.NewStringParam` 等按本包既有约定补齐（`itoa` 可用 `strconv.Itoa`）。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/harness/tools/tool_discovery/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/tool_discovery/
git commit -m "feat(tool_discovery): tool_search 替代 search_tools（limit 归一化 + 精确 schema 渲染）"
```

---

## Task 9: ToolCallTool 与 RelayedToolOutput

**Files:**
- Modify: `internal/agentcore/harness/tools/tool_discovery/tool_call.go`（由 `load_tools.go` 改名）
- Delete: `load_tools.go`、`load_tools_test.go`
- Test: `internal/agentcore/harness/tools/tool_discovery/tool_call_test.go`

- [ ] **Step 1: 写失败测试**

```go
package tool_discovery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

func TestToolCallTool_缺少回调上下文报错(t *testing.T) {
	callTool := NewToolCallTool("cn", "")
	_, err := callTool.Invoke(context.Background(), map[string]any{"name": "x", "args": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "active agent callback context") {
		t.Fatalf("应报缺少回调上下文，got %v", err)
	}
}

func TestRelayedToolOutput_只转发渲染文本(t *testing.T) {
	out := NewRelayedToolOutput(true, "渲染文本", map[string]any{"name": "x"}, nil)
	if out.RenderedText() != "渲染文本" {
		t.Fatal("渲染文本应可从私有字段读取")
	}
	if _, ok := out.Data()["renderedText"]; ok {
		t.Fatal("结构化字段不应包含渲染文本")
	}
}

func TestToolCallTool_透传回调结果(t *testing.T) {
	dispatched := 0
	callTool := NewToolCallTool("cn", "")
	callTool.SetDispatch(func(ctx context.Context, name string, args map[string]any, cb tool.ToolCallbackContext) (any, error) {
		dispatched++
		return NewRelayedToolOutput(true, "目标渲染", map[string]any{"name": name}, nil), nil
	})
	cb := &fakeCallbackContext{}
	out, err := callTool.InvokeWithCallback(context.Background(), map[string]any{
		"name": "xlsx_read", "args": map[string]any{"sheet": "S1"},
	}, cb)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("分发应执行一次，got %d", dispatched)
	}
	if out.RenderedText() != "目标渲染" {
		t.Fatalf("应转发目标渲染文本，got %s", out.RenderedText())
	}
}

func TestToolCallTool_错误透传(t *testing.T) {
	callTool := NewToolCallTool("cn", "")
	callTool.SetDispatch(func(context.Context, string, map[string]any, tool.ToolCallbackContext) (any, error) {
		return nil, errors.New("boom")
	})
	_, err := callTool.InvokeWithCallback(context.Background(), map[string]any{"name": "x", "args": map[string]any{}}, &fakeCallbackContext{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("目标错误应透传，got %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/tools/tool_discovery/ -run 'TestToolCallTool|TestRelayedToolOutput' -v`
Expected: FAIL，`undefined: NewToolCallTool`

- [ ] **Step 3: 删除旧实现并改写 `tool_call.go`**

```bash
git rm internal/agentcore/harness/tools/tool_discovery/load_tools.go internal/agentcore/harness/tools/tool_discovery/load_tools_test.go
```

```go
package tool_discovery

import (
	"context"
	"errors"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCallTool 固定的延迟工具执行包装器。
//
// 执行回调由 ProgressiveToolRail 提供；目标工具经 AbilityManager 嵌套执行，
// 从而保留其自身的 rail 生命周期。
//
// Python: harness/tools/tool_discovery/tool_call.py (ToolCallTool)
type ToolCallTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// dispatch 分发回调
	dispatch func(ctx context.Context, name string, args map[string]any, cb tool.ToolCallbackContext) (any, error)
}

// RelayedToolOutput 携带目标工具模型可见渲染文本的包装结果。
//
// 渲染文本放在私有字段：结构化字段（反馈给上层流）只含 data/error。
//
// Python: tool_call.py (RelayedToolOutput)
type RelayedToolOutput struct {
	// success 是否成功
	success bool
	// renderedText 目标工具的模型可见渲染文本
	renderedText string
	// data 结构化结果
	data map[string]any
	// err 错误信息
	err error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// nestedToolCallKey 嵌套调用标记（写在回调上下文 Extra 上）
	nestedToolCallKey = "__progressive_nested_tool_call__"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRelayedToolOutput 创建包装结果。
func NewRelayedToolOutput(success bool, renderedText string, data map[string]any, err error) *RelayedToolOutput {
	return &RelayedToolOutput{success: success, renderedText: renderedText, data: data, err: err}
}

// RenderedText 返回目标工具的模型可见渲染文本。
func (r *RelayedToolOutput) RenderedText() string { return r.renderedText }

// Success 返回是否成功。
func (r *RelayedToolOutput) Success() bool { return r.success }

// Data 返回结构化结果。
func (r *RelayedToolOutput) Data() map[string]any { return r.data }

// Err 返回错误。
func (r *RelayedToolOutput) Err() error { return r.err }

// NewToolCallTool 创建执行包装器。
func NewToolCallTool(language string, agentID string) *ToolCallTool {
	card := tools.ToolCallCard(language)
	toolCard := &tool.ToolCard{
		BaseCard: *card,
		InputParams: []*schema.Param{
			schema.NewStringParam("name", "Exact tool name returned by tool_search", true),
			schema.NewObjectParam("args", "Arguments matching the schema returned by tool_search", true),
		},
	}
	toolCard.Exposure = tool.ToolExposureDirect
	return &ToolCallTool{card: toolCard}
}

// SetDispatch 注入分发回调（由 rail 在 Init 时设置）。
func (t *ToolCallTool) SetDispatch(dispatch func(ctx context.Context, name string, args map[string]any, cb tool.ToolCallbackContext) (any, error)) {
	t.dispatch = dispatch
}

// AcceptsToolCallbackContext 声明需要执行期回调上下文。
//
// Python: accepts_tool_callback_context = True
func (t *ToolCallTool) AcceptsToolCallbackContext() bool { return true }

// Card 返回工具卡片。
func (t *ToolCallTool) Card() *tool.ToolCard { return t.card }

// Invoke 无回调上下文时直接报错（对齐上游 RuntimeError）。
//
// Python: ToolCallTool.invoke
func (t *ToolCallTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	_, err := t.InvokeWithCallback(ctx, inputs, nil)
	return nil, err
}

// InvokeWithCallback 带回调上下文执行。
func (t *ToolCallTool) InvokeWithCallback(
	ctx context.Context,
	inputs map[string]any,
	cb tool.ToolCallbackContext,
) (*RelayedToolOutput, error) {
	name, _ := inputs["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("tool_call requires a non-empty tool name.")
	}
	args, _ := inputs["args"].(map[string]any)
	if cb == nil {
		return nil, errors.New("tool_call requires an active agent callback context")
	}
	result, err := t.dispatch(ctx, name, args, cb)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "tool_call_invoke_failed").
			Err(err).
			Msg("tool_call invoke failed")
		return nil, err
	}
	if relayed, ok := result.(*RelayedToolOutput); ok {
		return relayed, nil
	}
	return NewRelayedToolOutput(true, "", map[string]any{"name": name, "result": result}, nil), nil
}

// Stream 不支持流式。
func (t *ToolCallTool) Stream(context.Context, map[string]any, ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/harness/tools/tool_discovery/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/tool_discovery/
git commit -m "feat(tool_discovery): tool_call 包装器替代 load_tools（RelayedToolOutput 转发）"
```

---

## Task 10: 提示词区段重写

**Files:**
- Modify: `internal/agentcore/harness/prompts/sections/progressive_tool_rail.go`
- Test: `internal/agentcore/harness/prompts/sections/progressive_tool_rail_test.go`

- [ ] **Step 1: 写失败测试**

```go
package sections

import (
	"strings"
	"testing"
)

func TestCompactToolDescription_三档(t *testing.T) {
	if got := compactToolDescription("  a   b  "); got != "a b" {
		t.Fatalf("应折叠空白，got %q", got)
	}
	long := strings.Repeat("字", 200)
	if got := compactToolDescription(long); len([]rune(got)) != 120 || !strings.HasSuffix(got, "…") {
		t.Fatalf("超长应截断到 120 并加省略号，got len=%d", len([]rune(got)))
	}
	if got := compactToolDescription("读取表格。后面还有很多描述"); got != "读取表格。" {
		t.Fatalf("句末标点应优先，got %q", got)
	}
}

func TestBuildProgressiveToolRulesSection_含初始目录(t *testing.T) {
	section := BuildProgressiveToolRulesSection(map[string]string{
		"xlsx_read": "读取 Excel",
		"pdf_read":  "提取 PDF",
	})
	if section.Priority != 75 {
		t.Fatalf("priority 应为 75，got %d", section.Priority)
	}
	content := section.Content["cn"]
	if !strings.Contains(content, "- **xlsx_read**: 读取 Excel") {
		t.Fatalf("初始目录缺失，got %s", content)
	}
	if strings.Index(content, "pdf_read") > strings.Index(content, "xlsx_read") {
		t.Fatal("目录应按名字排序")
	}
}

func TestRenderDeferredToolCatalogDelta_三段(t *testing.T) {
	content := RenderDeferredToolCatalogDelta(
		map[string]string{"new_tool": "新工具"},
		map[string]string{"upd_tool": "改过的工具"},
		[]string{"old_tool"},
		"cn", 3,
	)
	for _, want := range []string{"目录版本：3", "新增", "修改", "删除", "new_tool", "upd_tool", "old_tool"} {
		if !strings.Contains(content, want) {
			t.Fatalf("delta 缺 %s：%s", want, content)
		}
	}
}

func TestRenderDeferredToolCatalogSnapshot_版本与空目录(t *testing.T) {
	content := RenderDeferredToolCatalogSnapshot(nil, "cn", 1)
	if !strings.Contains(content, "目录版本：1") || !strings.Contains(content, "没有可通过 tool_search 搜索") {
		t.Fatalf("snapshot 文案不符：%s", content)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/prompts/sections/ -run 'TestCompactToolDescription|TestBuildProgressiveToolRulesSection|TestRenderDeferredToolCatalog' -v`
Expected: FAIL，`undefined: compactToolDescription`

- [ ] **Step 3: 重写文件**

1. 删除旧 navigation 常量与 `BuildNavigationSection`、`BuildNavigationEntry`（`SectionToolNavigation` 常量保留）
2. 规则正文替换为 `openjiuwen/harness/prompts/sections/progressive_tool_rail.py:44-108` 的 cn/en 全文（**逐字**，含「动态工具状态优先级」段与 7 条 `tool_search` 使用规则）
3. 新增：`compactToolDescription`、`renderDeferredToolDescriptions`、`BuildProgressiveToolRulesSection(deferred map[string]string)`、`RenderDeferredToolCatalogSnapshot`、`RenderDeferredToolCatalogDelta`

```go
// compactToolDescription 把描述压到一句（≤120 字符）。
//
// Python: _compact_tool_description
func compactToolDescription(description string) string {
	compact := strings.Join(strings.Fields(description), " ")
	runes := []rune(compact)
	if len(runes) <= maxDeferredToolSummaryChars {
		return compact
	}
	for i, r := range runes[:maxDeferredToolSummaryChars] {
		if i > 0 && strings.ContainsRune("。.！!？?", r) {
			return string(runes[:i+1])
		}
	}
	return string(runes[:maxDeferredToolSummaryChars-1]) + "…"
}

// renderDeferredToolDescriptions 渲染目录摘要列表（按名排序）。
//
// Python: _render_deferred_tool_descriptions
func renderDeferredToolDescriptions(descriptions map[string]string, language string) string {
	if len(descriptions) == 0 {
		if language == "en" {
			return "(No deferred tools are currently available through tool_search.)"
		}
		return "（当前没有可通过 tool_search 搜索的 deferred 工具。）"
	}
	names := make([]string, 0, len(descriptions))
	for name := range descriptions {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		if summary := compactToolDescription(descriptions[name]); summary != "" {
			lines = append(lines, "- **"+name+"**: "+summary)
			continue
		}
		lines = append(lines, "- **"+name+"**")
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/harness/prompts/sections/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/prompts/sections/
git commit -m "feat(prompts): 渐进式工具规则正文与目录 snapshot/delta 渲染"
```

---

## Task 11: HasHistorySnapshot 导出

**Files:**
- Modify: `internal/agentcore/harness/prompts/attachment_delivery.go`
- Test: `internal/agentcore/harness/prompts/attachment_snapshot_test.go`

- [ ] **Step 1: 写失败测试**

```go
package prompts

import "testing"

func TestHasHistorySnapshot(t *testing.T) {
	manager := NewManager()
	hc := newFakeHistoryContext("sess-1")
	if manager.HasHistorySnapshot(hc, "sess-1") {
		t.Fatal("首轮应无快照")
	}
	if err := manager.Sync(context.Background(), hc); err != nil {
		t.Fatalf("sync 失败: %v", err)
	}
	if !manager.HasHistorySnapshot(hc, "sess-1") {
		t.Fatal("快照写入后应为 true")
	}
	if manager.HasHistorySnapshot(hc, "sess-2") {
		t.Fatal("其他会话不应受影响")
	}
}
```

> `newFakeHistoryContext` 若无现成实现，沿用 `attachment_delivery_test.go` 中的 fake（同包可直接复用）。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/prompts/ -run TestHasHistorySnapshot -v`
Expected: FAIL，`manager.HasHistorySnapshot undefined`

- [ ] **Step 3: 实现导出包装**

```go
// HasHistorySnapshot 判断会话历史里是否已有完整快照。
//
// 运行时 rail 用它决定"上下文压缩/重建后必须改发全量快照"，避免孤立 delta。
//
// Python: PromptAttachmentManager.has_history_snapshot
func (m *Manager) HasHistorySnapshot(hc HistoryContext, sessionID string) bool {
	if hc == nil {
		return false
	}
	_, hasSnapshot := readHistoryState(hc, sessionID)
	return hasSnapshot
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/harness/prompts/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/prompts/attachment_delivery.go internal/agentcore/harness/prompts/attachment_snapshot_test.go
git commit -m "feat(prompts): 导出 HasHistorySnapshot 供 rail 判定全量快照"
```

---

## Task 12: 配置字段与 DeepAgent 接线

**Files:**
- Modify: `internal/agentcore/harness/schema/config.go`、`internal/agentcore/harness/deep_agent.go`
- Test: `internal/agentcore/harness/schema/config_test.go`、`internal/agentcore/harness/deep_agent_exposure_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/agentcore/harness/schema/config_test.go 追加
func TestDeepAgentConfig_ToolSearchLimit(t *testing.T) {
	cfg := DefaultDeepAgentConfig()
	if cfg.ToolSearchLimit != 5 {
		t.Fatalf("默认应为 5，got %d", cfg.ToolSearchLimit)
	}
}

func TestDeepAgentConfig_旧字段被忽略(t *testing.T) {
	payload := []byte(`{"progressive_tool_enabled":true,"progressive_tool_max_loaded_tools":99}`)
	var cfg DeepAgentConfig
	if err := json.Unmarshal(payload, &cfg); err != nil {
		t.Fatalf("旧键应被静默忽略: %v", err)
	}
	if !cfg.ProgressiveToolEnabled {
		t.Fatal("新字段应生效")
	}
}
```

```go
// internal/agentcore/harness/deep_agent_exposure_test.go
func TestDeepAgent_启用渐进后写入曝光策略(t *testing.T) {
	agent := newTestDeepAgent(t, func(cfg *schema.DeepAgentConfig) {
		cfg.ProgressiveToolEnabled = true
	})
	card := &tool.ToolCard{}
	card.Name = "xlsx_read"
	agent.AbilityManager().Add(card)
	if card.EffectiveExposure() != tool.ToolExposureDeferred {
		t.Fatalf("非名单工具应 deferred，got %s", card.EffectiveExposure())
	}
	direct := &tool.ToolCard{}
	direct.Name = "read_file"
	agent.AbilityManager().Add(direct)
	if direct.EffectiveExposure() != tool.ToolExposureDirect {
		t.Fatalf("名单内工具应 direct，got %s", direct.EffectiveExposure())
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/ -run 'TestDeepAgentConfig_ToolSearchLimit|TestDeepAgent_启用渐进后写入曝光策略' -v`
Expected: FAIL，`cfg.ToolSearchLimit undefined`

- [ ] **Step 3: 改 `schema/config.go`**

删除 `ProgressiveToolAlwaysVisibleTools`、`ProgressiveToolDefaultVisibleTools`、`ProgressiveToolMaxLoadedTools`、`DefaultProgressiveToolMax`、`EffectiveProgressiveToolMaxLoadedTools` 及默认值赋值；新增：

```go
	// ProgressiveToolEnabled 是否启用渐进式工具暴露
	ProgressiveToolEnabled bool `json:"progressive_tool_enabled"`
	// ToolSearchLimit tool_search 默认结果条数（有效范围 1..20）
	ToolSearchLimit int `json:"tool_search_limit,omitempty"`
```

默认值处加 `ToolSearchLimit: defaultToolSearchLimit`，常量：

```go
	// defaultToolSearchLimit 默认搜索结果条数（对齐上游 5）
	defaultToolSearchLimit = 5
```

需同步把 `deep_adapter_tools.go` 等引用旧字段的位置删除（`grep -rn "ProgressiveToolAlwaysVisibleTools\|ProgressiveToolDefaultVisibleTools\|ProgressiveToolMaxLoadedTools\|EffectiveProgressiveToolMaxLoadedTools" internal/` 清零）。

- [ ] **Step 4: 改 `deep_agent.go`**

新增包级常量（对齐 `deep_agent.py:199-242` 的 36 项 + Go 实际名映射，映射项注释注明来源差异）：

```go
// defaultDirectToolNames 渐进模式下保持直接可调用的工具名集合。
//
// Python: _DEFAULT_DIRECT_TOOL_NAMES（36 项）
// Go 侧差异：上游 subagent_* 六项在 Go 分别注册为 sessions_spawn/sessions_list/sessions_cancel；
// 未实现的工具名保留原字符串（对策略无副作用）。
var defaultDirectToolNames = []string{
	"tool_search", "tool_call",
	"read_file", "write_file", "edit_file", "glob", "grep", "bash",
	"task_tool", "sessions_spawn", "sessions_list", "sessions_cancel",
	"ask_user", "todo_create", "todo_list", "todo_get", "todo_modify", "skill_tool",
	"memory_search", "memory_get", "free_search", "paid_search", "fetch_webpage",
	"write_memory", "edit_memory", "read_memory", "ltm_search", "ltm_search_summary",
	"send_file_to_user", "enter_plan_mode", "exit_plan_mode",
	"skill_branch_explore", "skill_index_build",
}
```

在 `configure`（首次配置）与热重配路径中、**任何工具注册之前**调用：

```go
	if policy, ok := d.abilityManager.(agentinterfaces.ExposurePolicyController); ok {
		policy.SetToolExposurePolicy(config.ProgressiveToolEnabled, defaultDirectToolNames)
	}
```

- [ ] **Step 5: 收尾钩子**

在现有 rail 初始化循环结束后（`pendingRails` 清空前），遍历已注册 rails，命中 `ProgressiveToolRail` 时调用其 `FinalizeStartupAsync`：

```go
	// ProgressiveToolRail 拥有 BM25 目录：等所有 startup rail 注册完工具后再建首份索引
	// Python: deep_agent.py:1518-1528
	for _, railInst := range d.registeredRails {
		if progressive, ok := railInst.(*rails.ProgressiveToolRail); ok {
			if err := progressive.FinalizeStartupAsync(ctx, d); err != nil {
				logger.Warn(logComponent).
					Str("event_type", "progressive_rail_finalize").
					Err(err).
					Msg("failed to finalize progressive tool rail")
			}
		}
	}
```

- [ ] **Step 6: 运行确认通过**

Run: `go test ./internal/agentcore/harness/ -run 'TestDeepAgent' -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/schema/config.go internal/agentcore/harness/schema/config_test.go internal/agentcore/harness/deep_agent.go internal/agentcore/harness/deep_agent_exposure_test.go
git commit -m "feat(harness): ToolSearchLimit 配置与曝光策略/收尾钩子接线"
```

---

## Task 13: Rail 主体（一）——目录、索引、搜索、授权

**Files:**
- Rewrite: `internal/agentcore/harness/rails/progressive.go`
- Test: `internal/agentcore/harness/rails/progressive_test.go`（保留旧文件路径，内容按新语义重写）

- [ ] **Step 1: 写失败测试**

```go
package rails

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

func TestCollectDeferredToolDescriptions_只取deferred且排除元工具(t *testing.T) {
	rail := NewProgressiveToolRail(&schema.DeepAgentConfig{ProgressiveToolEnabled: true})
	am := newFakeAbilityManager()
	directCard := &tool.ToolCard{}
	directCard.Name = "read_file"
	directCard.Exposure = tool.ToolExposureDirect
	deferredCard := &tool.ToolCard{}
	deferredCard.Name = "xlsx_read"
	deferredCard.Description = "读取 Excel"
	deferredCard.Exposure = tool.ToolExposureDeferred
	metaCard := &tool.ToolCard{}
	metaCard.Name = "tool_search"
	metaCard.Exposure = tool.ToolExposureDirect
	am.Add(directCard)
	am.Add(deferredCard)
	am.Add(metaCard)

	rail.metaToolNames = map[string]struct{}{"tool_search": {}}
	got := rail.collectDeferredToolDescriptions(am)
	if len(got) != 1 || got["xlsx_read"] != "读取 Excel" {
		t.Fatalf("只应收录 deferred 且非元工具，got %v", got)
	}
}

func TestEnsureToolSearchIndex_指纹变化触发重建(t *testing.T) {
	rail := NewProgressiveToolRail(&schema.DeepAgentConfig{ProgressiveToolEnabled: true})
	am := newFakeAbilityManager()
	card := &tool.ToolCard{}
	card.Name = "t1"
	card.Description = "旧描述"
	card.Exposure = tool.ToolExposureDeferred
	am.Add(card)

	rail.ensureToolSearchIndex(am)
	first := rail.toolSearchIndex
	rail.ensureToolSearchIndex(am)
	if rail.toolSearchIndex != first {
		t.Fatal("未变化时不应重建")
	}

	card.Description = "新描述"
	rail.ensureToolSearchIndex(am)
	if rail.toolSearchIndex == first {
		t.Fatal("描述变化后应重建")
	}
}

func TestSearchTools_授权写回会话(t *testing.T) {
	rail := NewProgressiveToolRail(&schema.DeepAgentConfig{ProgressiveToolEnabled: true})
	am := newFakeAbilityManager()
	card := &tool.ToolCard{}
	card.Name = "xlsx_read"
	card.Description = "读取 Excel 表格"
	card.Exposure = tool.ToolExposureDeferred
	am.Add(card)
	rail.metaToolNames = map[string]struct{}{}

	sess := newFakeSession()
	results, err := rail.searchTools(context.Background(), "excel", 5, sess)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("应命中 1 个工具，got %d", len(results))
	}
	names := rail.getDiscoveredTools(sess)
	if len(names) != 1 || names[0] != "xlsx_read" {
		t.Fatalf("授权名单应写入会话，got %v", names)
	}
	if rail.getDiscoveredToolFingerprints(sess)["xlsx_read"] == "" {
		t.Fatal("授权指纹应写入会话")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/rails/ -run 'TestCollectDeferredToolDescriptions|TestEnsureToolSearchIndex|TestSearchTools' -v`
Expected: FAIL（旧实现无这些方法）

- [ ] **Step 3: 重写 `progressive.go` 的结构与生命周期骨架**

```go
// deferredToolDocument BM25 索引的文档单元：可调用 schema + 仅检索用文本。
//
// Python: _DeferredToolDocument
type deferredToolDocument struct {
	// info 模型可见工具 schema
	info cschema.ToolInfoInterface
	// extraSearchText 仅参与检索的额外文本（本地化 display + aliases）
	extraSearchText string
	// displayDescription 目录摘要使用的本地化描述
	displayDescription string
}

// ProgressiveToolRail 渐进式工具发现与延迟工具目录 Rail。
//
// Python: openjiuwen/harness/rails/progressive_tool_rail.py
type ProgressiveToolRail struct {
	DeepAgentRail
	// config DeepAgent 运行时配置
	config *schema.DeepAgentConfig
	// metaToolNames 元工具名称集合（tool_search/tool_call）
	metaToolNames map[string]struct{}
	// ownedToolCards 本 Rail 注册且仍持有的卡片（name → card）
	ownedToolCards map[string]*tool.ToolCard
	// cachedAllToolInfos 全量工具清单缓存
	cachedAllToolInfos []cschema.ToolInfoInterface
	// toolSearchIndex BM25 索引（不可变实例，变化时整体替换）
	toolSearchIndex *tool_discovery.BM25ToolIndex
	// toolSearchIndexRevision 建索引时的注册版本
	toolSearchIndexRevision int
	// toolSearchRegistry 建索引使用的注册表（rail 自持引用）
	toolSearchRegistry agentinterfaces.AbilityManagerInterface
	// mu 保护索引替换与卡片映射
	mu sync.Mutex
}
```

常量（对齐上游）：

```go
const (
	// progressiveToolRailPriority Rail 优先级
	progressiveToolRailPriority = 90

	// discoveredToolsKey 会话状态键：已授权工具名
	discoveredToolsKey = "__progressive_discovered_tool_names__"
	// discoveredToolFingerprintsKey 会话状态键：授权时的 schema 指纹
	discoveredToolFingerprintsKey = "__progressive_discovered_tool_fingerprints__"
	// discoveryTraceKey 会话状态键：发现轨迹
	discoveryTraceKey = "__progressive_tool_discovery_trace__"
	// deferredToolCatalogStateKey 会话状态键：目录快照与版本
	deferredToolCatalogStateKey = "__progressive_deferred_tool_catalog__"

	// deferredToolAttachmentSection 目录附件的 section 名
	deferredToolAttachmentSection = "progressive_deferred_tools"
	// deferredToolAttachmentSource 目录附件的来源标识
	deferredToolAttachmentSource = "progressive_tool_rail"
	// deferredToolCatalogMetadataKey 附件 metadata：目录内容
	deferredToolCatalogMetadataKey = "catalog_tools"
	// deferredToolCatalogFingerprintsMetadataKey 附件 metadata：目录指纹
	deferredToolCatalogFingerprintsMetadataKey = "catalog_fingerprints"
	// deferredToolRegistryRevisionMetadataKey 附件 metadata：注册版本
	deferredToolRegistryRevisionMetadataKey = "registry_revision"
	// fullSnapshotInterval 连续 delta 上限，达到后改发全量快照
	fullSnapshotInterval = 10
	// nestedToolCallKey 嵌套调用标记
	nestedToolCallKey = "__progressive_nested_tool_call__"
	// progressiveDiscoveryProperty 卡片扩展属性键：发现元数据
	progressiveDiscoveryProperty = "progressive_discovery"
)
```

- [ ] **Step 4: 实现目录收集、索引与搜索**

```go
// collectDeferredToolDocuments 构造 deferred 目录文档（含仅检索用的额外文本）。
//
// Python: _list_registered_deferred_tool_documents
func (r *ProgressiveToolRail) collectDeferredToolDocuments(am agentinterfaces.AbilityManagerInterface) []deferredToolDocument {
	if am == nil {
		return nil
	}
	language := r.catalogLanguage()
	var documents []deferredToolDocument
	for _, ability := range am.List() {
		card, ok := ability.(*tool.ToolCard)
		if !ok {
			continue
		}
		if _, isMeta := r.metaToolNames[card.Name]; isMeta {
			continue
		}
		if card.EffectiveExposure() != tool.ToolExposureDeferred {
			continue
		}
		extra, display := resolveProgressiveDiscovery(card, language)
		description := display
		if description == "" {
			description = card.Description
		}
		documents = append(documents, deferredToolDocument{
			info:                card.ToolInfo(),
			extraSearchText:     extra,
			displayDescription:  description,
		})
	}
	return documents
}

// collectDeferredToolDescriptions 返回按名排序的 deferred 目录摘要。
func (r *ProgressiveToolRail) collectDeferredToolDescriptions(am agentinterfaces.AbilityManagerInterface) map[string]string {
	descriptions := make(map[string]string)
	for _, document := range r.collectDeferredToolDocuments(am) {
		name := document.info.GetName()
		if name != "" {
			descriptions[name] = document.displayDescription
		}
	}
	return descriptions
}

// rebuildToolSearchIndex 重建 BM25 索引。
//
// Python: _rebuild_tool_search_index
func (r *ProgressiveToolRail) rebuildToolSearchIndex(am agentinterfaces.AbilityManagerInterface, documents []deferredToolDocument) {
	infos := make([]cschema.ToolInfoInterface, 0, len(documents))
	extras := make([]string, 0, len(documents))
	for _, document := range documents {
		infos = append(infos, document.info)
		extras = append(extras, document.extraSearchText)
	}
	index := tool_discovery.BuildBM25ToolIndex(infos, extras)
	r.mu.Lock()
	r.toolSearchIndex = index
	r.toolSearchIndexRevision = registryRevisionOf(am)
	r.toolSearchRegistry = am
	r.mu.Unlock()
}

// ensureToolSearchIndex 仅在目录或注册版本变化时重建。
//
// Python: _ensure_tool_search_index
func (r *ProgressiveToolRail) ensureToolSearchIndex(am agentinterfaces.AbilityManagerInterface) {
	documents := r.collectDeferredToolDocuments(am)
	infos := make([]cschema.ToolInfoInterface, 0, len(documents))
	extras := make([]string, 0, len(documents))
	for _, document := range documents {
		infos = append(infos, document.info)
		extras = append(extras, document.extraSearchText)
	}
	fingerprint := tool_discovery.Fingerprint(infos, extras)

	r.mu.Lock()
	reusable := r.toolSearchIndex != nil &&
		r.toolSearchIndex.SourceFingerprint() == fingerprint &&
		r.toolSearchIndexRevision == registryRevisionOf(am)
	r.mu.Unlock()
	if reusable {
		return
	}
	r.rebuildToolSearchIndex(am, documents)
}

// searchTools 检索并授权命中工具。
//
// Python: _search_tools
func (r *ProgressiveToolRail) searchTools(
	ctx context.Context,
	query string,
	limit int,
	sess sessioninterfaces.SessionFacade,
) ([]map[string]any, error) {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return nil, nil
	}
	am := r.abilityManager()
	r.ensureToolSearchIndex(am)

	r.mu.Lock()
	index := r.toolSearchIndex
	r.mu.Unlock()
	var matched []cschema.ToolInfoInterface
	if index != nil {
		matched = index.Search(normalized, limit)
	}

	names := make([]string, 0, len(matched))
	fingerprints := make(map[string]string, len(matched))
	for _, info := range matched {
		if info.GetName() == "" {
			continue
		}
		names = append(names, info.GetName())
		fingerprints[info.GetName()] = tool_discovery.ToolFingerprint(info)
	}
	r.authorizeDiscoveredTools(sess, names, fingerprints)
	r.appendTrace(sess, map[string]any{
		"action":  "tool_search",
		"query":   normalized,
		"limit":   limit,
		"matched": names,
	})

	results := make([]map[string]any, 0, len(matched))
	for _, info := range matched {
		results = append(results, map[string]any{
			"name":        info.GetName(),
			"description": info.GetDescription(),
			"parameters":  info.GetParameters(),
		})
	}
	return results, nil
}
```

辅助函数（同文件）：

```go
// registryRevisionOf 读取注册表版本（无能力时返回 0）。
func registryRevisionOf(am agentinterfaces.AbilityManagerInterface) int {
	if provider, ok := am.(agentinterfaces.RegistryRevisionProvider); ok {
		return provider.RegistryRevision()
	}
	return 0
}

// resolveProgressiveDiscovery 解析卡片上的本地化描述与检索别名。
//
// Python: _resolve_progressive_discovery
func resolveProgressiveDiscovery(card *tool.ToolCard, language string) (extraSearchText string, displayDescription string) {
	if card.Properties == nil {
		return "", ""
	}
	raw, ok := card.Properties[progressiveDiscoveryProperty].(map[string]any)
	if !ok {
		return "", ""
	}
	parts := make([]string, 0, 4)
	if display, ok := raw["display"].(map[string]any); ok {
		keys := make([]string, 0, len(display))
		for key := range display {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if text, ok := display[key].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
		if text := localizedDisplay(display, language); text != "" {
			displayDescription = text
		}
	}
	if aliases, ok := raw["aliases"].([]any); ok {
		for _, alias := range aliases {
			if text, ok := alias.(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
	}
	return strings.Join(parts, " "), displayDescription
}

// localizedDisplay 按语言候选取本地化描述。
func localizedDisplay(display map[string]any, language string) string {
	candidates := []string{language}
	if language == "cn" {
		candidates = []string{"cn", "zh"}
	} else if language == "zh" {
		candidates = []string{"zh", "cn"}
	}
	for _, candidate := range candidates {
		if text, ok := display[candidate].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}
```

- [ ] **Step 5: 实现会话状态与查找助手（同文件）**

```go
// getDiscoveredTools 读取已授权工具名。
//
// Python: _get_discovered_tools
func (r *ProgressiveToolRail) getDiscoveredTools(sess sessioninterfaces.SessionFacade) []string {
	if sess == nil {
		return nil
	}
	raw, err := sess.GetState(state.StateKey(discoveredToolsKey))
	if err != nil {
		return nil
	}
	values, ok := raw.([]string)
	if !ok {
		return nil
	}
	return values
}

// getDiscoveredToolFingerprints 读取授权指纹；无记录返回 nil（对齐上游 Optional）。
//
// Python: _get_discovered_tool_fingerprints
func (r *ProgressiveToolRail) getDiscoveredToolFingerprints(sess sessioninterfaces.SessionFacade) map[string]string {
	if sess == nil {
		return nil
	}
	raw, err := sess.GetState(state.StateKey(discoveredToolFingerprintsKey))
	if err != nil {
		return nil
	}
	typed, ok := raw.(map[string]string)
	if !ok {
		return nil
	}
	return typed
}

// setDiscoveredTools 写入授权名单与指纹（只保留名单内的指纹）。
//
// Python: _set_discovered_tools
func (r *ProgressiveToolRail) setDiscoveredTools(sess sessioninterfaces.SessionFacade, names []string, fingerprints map[string]string) {
	if sess == nil {
		return
	}
	deduped := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		deduped = append(deduped, trimmed)
	}
	merged := r.getDiscoveredToolFingerprints(sess)
	if merged == nil {
		merged = map[string]string{}
	}
	for name, fingerprint := range fingerprints {
		if name != "" && fingerprint != "" {
			merged[name] = fingerprint
		}
	}
	kept := make(map[string]string, len(deduped))
	for _, name := range deduped {
		if fingerprint, ok := merged[name]; ok {
			kept[name] = fingerprint
		}
	}
	sess.UpdateState(map[string]any{
		discoveredToolsKey:           deduped,
		discoveredToolFingerprintsKey: kept,
	})
}

// authorizeDiscoveredTools 把命中工具并入授权名单。
//
// Python: _authorize_discovered_tools
func (r *ProgressiveToolRail) authorizeDiscoveredTools(sess sessioninterfaces.SessionFacade, names []string, fingerprints map[string]string) {
	if sess == nil {
		return
	}
	r.setDiscoveredTools(sess, append(r.getDiscoveredTools(sess), names...), fingerprints)
}

// invalidateDiscoveredTools 失效指定工具的授权。
//
// Python: _invalidate_discovered_tools
func (r *ProgressiveToolRail) invalidateDiscoveredTools(sess sessioninterfaces.SessionFacade, names map[string]struct{}) {
	if sess == nil || len(names) == 0 {
		return
	}
	current := r.getDiscoveredTools(sess)
	retained := make([]string, 0, len(current))
	for _, name := range current {
		if _, drop := names[name]; drop {
			continue
		}
		retained = append(retained, name)
	}
	if len(retained) != len(current) {
		r.setDiscoveredTools(sess, retained, nil)
	}
}

// appendTrace 追加发现轨迹。
//
// Python: _append_trace
func (r *ProgressiveToolRail) appendTrace(sess sessioninterfaces.SessionFacade, event map[string]any) {
	if sess == nil {
		return
	}
	trace := []map[string]any{}
	if raw, err := sess.GetState(state.StateKey(discoveryTraceKey)); err == nil {
		if existing, ok := raw.([]map[string]any); ok {
			trace = existing
		}
	}
	trace = append(trace, event)
	sess.UpdateState(map[string]any{discoveryTraceKey: trace})
}

// collectDeferredFingerprints 读取当前目录指纹。
//
// Python: _collect_deferred_tool_fingerprints
func (r *ProgressiveToolRail) collectDeferredFingerprints() map[string]string {
	fingerprints := make(map[string]string)
	for _, document := range r.collectDeferredToolDocuments(r.toolSearchRegistry) {
		if name := document.info.GetName(); name != "" {
			fingerprints[name] = tool_discovery.ToolFingerprint(document.info)
		}
	}
	return fingerprints
}

// abilityManager 返回 rail 持有的注册表引用。
func (r *ProgressiveToolRail) abilityManager() agentinterfaces.AbilityManagerInterface {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.toolSearchRegistry
}

// abilityManagerFrom 从回调上下文取注册表，缺失时回落到 rail 引用。
func (r *ProgressiveToolRail) abilityManagerFrom(cbc *agentinterfaces.AgentCallbackContext) agentinterfaces.AbilityManagerInterface {
	if cbc != nil && cbc.Agent() != nil {
		if am := cbc.Agent().AbilityManager(); am != nil {
			return am
		}
	}
	return r.abilityManager()
}

// lookupCard 按名取工具卡片。
func (r *ProgressiveToolRail) lookupCard(cbc *agentinterfaces.AgentCallbackContext, name string) *tool.ToolCard {
	am := r.abilityManagerFrom(cbc)
	if am == nil {
		return nil
	}
	card, _ := am.Get(name).(*tool.ToolCard)
	return card
}

// lookupCardByName 从注册表按名取工具卡片。
func lookupCardByName(am agentinterfaces.AbilityManagerInterface, name string) *tool.ToolCard {
	if am == nil {
		return nil
	}
	card, _ := am.Get(name).(*tool.ToolCard)
	return card
}

// isCachedTool 判断工具在缓存清单中。
func (r *ProgressiveToolRail) isCachedTool(name string) bool {
	for _, info := range r.cachedAllToolInfos {
		if info.GetName() == name {
			return true
		}
	}
	return false
}

// catalogLanguage 解析目录语言（cn/en）。
//
// Python: _resolve_catalog_language
func (r *ProgressiveToolRail) catalogLanguage() string {
	language := ""
	if r.config != nil {
		language = strings.ToLower(strings.TrimSpace(r.config.EffectiveLanguage()))
	}
	if language == "en" {
		return "en"
	}
	return "cn"
}

// toolSearchLimit 返回 tool_search 默认条数（clamp 1..20）。
func (r *ProgressiveToolRail) toolSearchLimit() int {
	limit := 0
	if r.config != nil {
		limit = r.config.ToolSearchLimit
	}
	if limit <= 0 {
		limit = tool_discovery.DefaultToolSearchLimit
	}
	return tool_discovery.NormalizeToolSearchLimit(limit)
}

// containsString 判断切片是否包含目标字符串。
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
```

- [ ] **Step 6: 运行确认通过**

Run: `go test ./internal/agentcore/harness/rails/ -run 'TestCollectDeferredToolDescriptions|TestEnsureToolSearchIndex|TestSearchTools' -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/rails/progressive.go internal/agentcore/harness/rails/progressive_test.go
git commit -m "feat(rails): ProgressiveToolRail 目录收集/BM25 索引/搜索授权（13/15）"
```

---

## Task 14: Rail 主体（二）——目录附件同步

**Files:**
- Modify: `internal/agentcore/harness/rails/progressive.go`
- Test: `internal/agentcore/harness/rails/progressive_attachment_test.go`

- [ ] **Step 1: 写失败测试**

```go
package rails

import (
	"strings"
	"testing"
)

func TestSyncDeferredToolAttachment_首轮未变不发附件(t *testing.T) {
	rail, sess, manager := newAttachmentFixture(t, map[string]string{"a": "工具A"})
	if err := rail.syncDeferredToolAttachment(context.Background(), sess, manager, map[string]string{"a": "工具A"}); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if items := manager.List(); len(items) != 0 {
		t.Fatalf("首轮目录未变不应写附件，got %d", len(items))
	}
}

func TestSyncDeferredToolAttachment_新增发delta(t *testing.T) {
	rail, sess, manager := newAttachmentFixture(t, map[string]string{"a": "工具A"})
	if err := rail.syncDeferredToolAttachment(context.Background(), sess, manager, map[string]string{"a": "工具A"}); err != nil {
		t.Fatal(err)
	}
	if err := rail.syncDeferredToolAttachment(context.Background(), sess, manager, map[string]string{"a": "工具A", "b": "工具B"}); err != nil {
		t.Fatal(err)
	}
	items := manager.List()
	if len(items) != 1 {
		t.Fatalf("应写入一条附件，got %d", len(items))
	}
	if !strings.Contains(items[0].Content, "新增") || !strings.Contains(items[0].Content, "b") {
		t.Fatalf("delta 应含新增段，got %s", items[0].Content)
	}
	if items[0].Metadata["catalog_mode"] != "delta" {
		t.Fatalf("mode 应为 delta，got %v", items[0].Metadata["catalog_mode"])
	}
}

func TestSyncDeferredToolAttachment_连续十次后发快照(t *testing.T) {
	rail, sess, manager := newAttachmentFixture(t, nil)
	syncWith := func(catalog map[string]string) {
		if err := rail.syncDeferredToolAttachment(context.Background(), sess, manager, catalog); err != nil {
			t.Fatal(err)
		}
	}
	syncWith(map[string]string{"a": "A"}) // 首次为 delta（初始目录为空）
	catalog := map[string]string{"a": "A"}
	for i := 0; i < 9; i++ {
		catalog["t"+string(rune('a'+i))] = "x"
		syncWith(catalog)
	}
	catalog["t_last"] = "x"
	syncWith(catalog)
	if mode := manager.Last().Metadata["catalog_mode"]; mode != "snapshot" {
		t.Fatalf("第 11 次更新应为 snapshot，got %v", mode)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/rails/ -run TestSyncDeferredToolAttachment -v`
Expected: FAIL，`rail.syncDeferredToolAttachment undefined`

- [ ] **Step 3: 实现目录同步**

```go
// syncDeferredToolAttachment 计算目录差异并写入附件（delta / snapshot）。
//
// 首轮未变化不写；无快照、语言变化或连续 delta 达上限时改发全量快照。
//
// Python: _sync_deferred_tool_attachment
func (r *ProgressiveToolRail) syncDeferredToolAttachment(
	ctx context.Context,
	sess sessioninterfaces.SessionFacade,
	manager attachmentManager,
	current map[string]string,
) error {
	if sess == nil || manager == nil {
		return nil
	}
	sessionID := sessionIDOf(sess)
	if sessionID == "" {
		return nil
	}
	language := r.catalogLanguage()
	currentTools, _ := normalizeDeferredCatalog(current)
	currentFingerprints := r.collectDeferredFingerprints()

	state := catalogStateOf(sess)
	initialTools := initialStateTools(state, currentTools)
	previousTools := previousStateTools(state)
	initialized := state["initialized"] == true
	previousLanguage := stringOf(state["language"])
	version := intOf(state["version"], 1)
	deltaCount := intOf(state["delta_count"], 0)

	// 热重载/新 Session：从附件 metadata 恢复上次目录，避免重复推送同一差异
	if existing := manager.Find(sessionID, deferredToolAttachmentSection, deferredToolAttachmentSource); existing != nil {
		if recovered, ok := normalizeDeferredCatalog(existing.Metadata[deferredToolCatalogMetadataKey].(map[string]string)); ok {
			previousTools = recovered
			initialized = true
			if fingerprints, ok := existing.Metadata[deferredToolCatalogFingerprintsMetadataKey].(map[string]string); ok {
				currentFingerprints = mergeFingerprints(currentFingerprints, fingerprints)
			}
			if value, ok := existing.Metadata["catalog_language"].(string); ok && value != "" {
				previousLanguage = value
			}
			if value, ok := existing.Metadata["catalog_version"].(int); ok && value > version {
				version = value
			}
			if value, ok := existing.Metadata["delta_count"].(int); ok {
				deltaCount = value
			}
		}
	}

	baseline := initialTools
	if initialized {
		baseline = previousTools
	}
	added := make(map[string]string)
	updated := make(map[string]string)
	for name, description := range currentTools {
		if _, ok := baseline[name]; !ok {
			added[name] = description
			continue
		}
		if baseline[name] != description {
			updated[name] = description
		}
	}
	removed := make([]string, 0)
	for name := range baseline {
		if _, ok := currentTools[name]; !ok {
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)
	changed := len(added) > 0 || len(updated) > 0 || len(removed) > 0
	nextVersion := version
	if changed {
		nextVersion = version + 1
	}

	invalidated := make(map[string]struct{}, len(updated)+len(removed))
	for name := range updated {
		invalidated[name] = struct{}{}
	}
	for _, name := range removed {
		invalidated[name] = struct{}{}
	}
	if len(invalidated) > 0 {
		r.invalidateDiscoveredTools(sess, invalidated)
	}

	if !initialized {
		// 稳定提示词里的初始目录就是基线；首轮未变化则不写附件
		if !changed {
			r.saveCatalogState(sess, state, initialTools, currentTools, currentFingerprints, version, 0, language)
			return nil
		}
		version = nextVersion
		if err := r.writeCatalogAttachment(ctx, manager, sessionID, language, version, "delta",
			sections.RenderDeferredToolCatalogDelta(added, updated, removed, language, version),
			currentTools, currentFingerprints, 1); err != nil {
			return err
		}
		r.saveCatalogState(sess, state, initialTools, currentTools, currentFingerprints, version, 1, language)
		return nil
	}

	forceSnapshot := !manager.HasHistorySnapshot(sessionID) || previousLanguage != language || deltaCount >= fullSnapshotInterval
	if !forceSnapshot && !changed {
		return nil
	}
	version = nextVersion
	mode := "delta"
	nextDeltaCount := deltaCount + 1
	content := sections.RenderDeferredToolCatalogDelta(added, updated, removed, language, version)
	if forceSnapshot {
		mode = "snapshot"
		nextDeltaCount = 0
		content = sections.RenderDeferredToolCatalogSnapshot(currentTools, language, version)
	}
	if err := r.writeCatalogAttachment(ctx, manager, sessionID, language, version, mode, content,
		currentTools, currentFingerprints, nextDeltaCount); err != nil {
		return err
	}
	r.saveCatalogState(sess, state, initialTools, currentTools, currentFingerprints, version, nextDeltaCount, language)
	return nil
}
```

配套助手（同文件）：

```go
// attachmentManager 目录附件所需的最小能力。
type attachmentManager interface {
	// AddSection 写入或覆盖附件
	AddSection(sessionID, section, content string, kind prompts.AttachmentKind, source string, opts ...prompts.AttachmentOption) (*prompts.Attachment, error)
	// Find 查询指定 section/source 的附件（无则 nil）
	Find(sessionID, section, source string) *prompts.Attachment
	// HasHistorySnapshot 判断会话历史是否已有完整快照
	HasHistorySnapshot(sessionID string) bool
}

// catalogStateOf 读取会话里的目录状态。
func catalogStateOf(sess sessioninterfaces.SessionFacade) map[string]any {
	raw, err := sess.GetState(state.StateKey(deferredToolCatalogStateKey))
	if err != nil {
		return map[string]any{}
	}
	typed, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return typed
}

// initialStateTools 读取初始目录，缺失时用当前目录播种。
func initialStateTools(state map[string]any, current map[string]string) map[string]string {
	if raw, ok := state["initial_tools"].(map[string]string); ok {
		if normalized, ok := normalizeDeferredCatalog(raw); ok {
			return normalized
		}
	}
	normalized, _ := normalizeDeferredCatalog(current)
	return normalized
}

// previousStateTools 读取上次已发布的目录。
func previousStateTools(state map[string]any) map[string]string {
	if raw, ok := state["tools"].(map[string]string); ok {
		if normalized, ok := normalizeDeferredCatalog(raw); ok {
			return normalized
		}
	}
	return map[string]string{}
}

// normalizeDeferredCatalog 归一化目录（丢弃空名，按名排序）。
func normalizeDeferredCatalog(catalog map[string]string) (map[string]string, bool) {
	if catalog == nil {
		return nil, false
	}
	normalized := make(map[string]string, len(catalog))
	for name, description := range catalog {
		if strings.TrimSpace(name) == "" {
			continue
		}
		normalized[name] = description
	}
	return normalized, true
}

// saveCatalogState 写回目录状态。
func (r *ProgressiveToolRail) saveCatalogState(
	sess sessioninterfaces.SessionFacade,
	state map[string]any,
	initialTools map[string]string,
	currentTools map[string]string,
	fingerprints map[string]string,
	version int,
	deltaCount int,
	language string,
) {
	next := map[string]any{
		"initialized":    true,
		"version":        version,
		"delta_count":    deltaCount,
		"language":       language,
		"initial_tools":  initialTools,
		"tools":          currentTools,
		"fingerprints":   fingerprints,
		deferredToolRegistryRevisionMetadataKey: registryRevisionOf(r.abilityManager()),
	}
	sess.UpdateState(map[string]any{deferredToolCatalogStateKey: next})
}

// writeCatalogAttachment 写入目录附件（priority 75）。
func (r *ProgressiveToolRail) writeCatalogAttachment(
	ctx context.Context,
	manager attachmentManager,
	sessionID string,
	language string,
	version int,
	mode string,
	content string,
	currentTools map[string]string,
	fingerprints map[string]string,
	deltaCount int,
) error {
	_ = ctx
	_, err := manager.AddSection(
		sessionID,
		deferredToolAttachmentSection,
		content,
		prompts.AttachmentTool,
		deferredToolAttachmentSource,
		prompts.WithPriority(75),
		prompts.WithContentKind("text/markdown"),
		prompts.WithMetadata(map[string]any{
			"catalog_version":  version,
			"catalog_mode":     mode,
			"catalog_language": language,
			"delta_count":      deltaCount,
			deferredToolCatalogMetadataKey:             currentTools,
			deferredToolCatalogFingerprintsMetadataKey: fingerprints,
			deferredToolRegistryRevisionMetadataKey:    registryRevisionOf(r.abilityManager()),
		}),
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "progressive_catalog_attachment").
			Err(err).
			Msg("failed to update deferred-tool attachment")
	}
	return err
}

// intOf 读取整数值（缺省回落 fallback）。
func intOf(value any, fallback int) int {
	if typed, ok := value.(int); ok {
		return typed
	}
	return fallback
}

// stringOf 读取字符串值。
func stringOf(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

// sessionIDOf 解析会话 id。
//
// Python: _get_attachment_session_id
func sessionIDOf(sess sessioninterfaces.SessionFacade) string {
	if sess == nil {
		return ""
	}
	if provider, ok := sess.(interface{ SessionID() string }); ok {
		return provider.SessionID()
	}
	return ""
}

// mergeFingerprints 合并指纹表（右覆盖左）。
func mergeFingerprints(base map[string]string, extra map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(extra))
	for name, fingerprint := range base {
		merged[name] = fingerprint
	}
	for name, fingerprint := range extra {
		merged[name] = fingerprint
	}
	return merged
}
```

需在 `prompts` 包补两个便捷方法（`Find` 与 `HasHistorySnapshot(sessionID)` 的适配）；`Find` 内部调用已有 `ListByFilter` 取首条。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/harness/rails/ -run TestSyncDeferredToolAttachment -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/progressive.go internal/agentcore/harness/rails/progressive_attachment_test.go internal/agentcore/harness/prompts/attachment_store.go
git commit -m "feat(rails): 延迟工具目录附件 delta/snapshot 同步（14/15）"
```

---

## Task 15: Rail 主体（三）——执行通道、门卫、过滤、生命周期

**Files:**
- Modify: `internal/agentcore/harness/rails/progressive.go`
- Test: `internal/agentcore/harness/rails/progressive_flow_test.go`

- [ ] **Step 1: 写失败测试**

```go
package rails

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

func TestCallDiscoveredTool_未授权拒绝(t *testing.T) {
	rail, sess := newFlowFixture(t)
	result, err := rail.callDiscoveredTool(context.Background(), "xlsx_read", map[string]any{}, sess, newFakeCallbackContext(t))
	if err != nil {
		t.Fatalf("拒绝应作为结果返回而非错误: %v", err)
	}
	if result.Success() {
		t.Fatal("未授权应失败")
	}
	if !strings.Contains(result.Err().Error(), "must be returned by tool_search") {
		t.Fatalf("文案不符: %v", result.Err())
	}
}

func TestCallDiscoveredTool_指纹变化要求重搜(t *testing.T) {
	rail, sess := newFlowFixture(t)
	rail.setDiscoveredTools(sess, []string{"xlsx_read"}, map[string]string{"xlsx_read": "stale-fingerprint"})
	result, err := rail.callDiscoveredTool(context.Background(), "xlsx_read", map[string]any{}, sess, newFakeCallbackContext(t))
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if !strings.Contains(result.Err().Error(), "has changed; search it again") {
		t.Fatalf("文案不符: %v", result.Err())
	}
	if len(rail.getDiscoveredTools(sess)) != 0 {
		t.Fatal("授权应被失效")
	}
}

func TestBeforeToolCall_四分支(t *testing.T) {
	rail, sess := newFlowFixture(t)
	rail.metaToolNames = map[string]struct{}{"tool_search": {}}

	// ① direct 放行
	if err := rail.beforeToolCall(context.Background(), newToolCallCtx(t, "read_file", sess, nil)); err != nil {
		t.Fatalf("direct 应放行: %v", err)
	}
	// ② deferred 无标记拒绝
	rejected := newToolCallCtx(t, "xlsx_read", sess, nil)
	if err := rail.beforeToolCall(context.Background(), rejected); err != nil {
		t.Fatalf("拒绝不应返回 error: %v", err)
	}
	if rejected.GetExtra("_skip_tool") != true {
		t.Fatal("应设置 _skip_tool")
	}
	// ③ 有嵌套标记 + 已授权放行
	rail.setDiscoveredTools(sess, []string{"xlsx_read"}, nil)
	nested := newToolCallCtx(t, "xlsx_read", sess, map[string]any{nestedToolCallKey: true})
	if err := rail.beforeToolCall(context.Background(), nested); err != nil {
		t.Fatalf("嵌套已授权应放行: %v", err)
	}
	// ④ 未知工具拒绝
	unknown := newToolCallCtx(t, "no_such_tool", sess, nil)
	if err := rail.beforeToolCall(context.Background(), unknown); err != nil {
		t.Fatalf("拒绝不应返回 error: %v", err)
	}
	if unknown.GetExtra("_skip_tool") != true {
		t.Fatal("未知工具应拒绝")
	}
}

func TestBeforeModelCall_只留meta与direct(t *testing.T) {
	rail, sess := newFlowFixture(t)
	inputs := newModelCallInputsWithTools("read_file", "xlsx_read")
	rail.metaToolNames = map[string]struct{}{"tool_search": {}, "tool_call": {}}

	if err := rail.filterModelCallTools(inputs); err != nil {
		t.Fatal(err)
	}
	kept := namesOfTools(inputs.Tools)
	if len(kept) != 1 || kept[0] != "read_file" {
		t.Fatalf("只应保留 direct，got %v", kept)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/harness/rails/ -run 'TestCallDiscoveredTool|TestBeforeToolCall|TestBeforeModelCall' -v`
Expected: FAIL，方法未定义

- [ ] **Step 3: 实现执行通道**

```go
// callDiscoveredTool 经 AbilityManager 嵌套执行一个已发现的 deferred 工具。
//
// 五道校验：非空 → 已授权 → 仍注册 → 仍为 deferred → 指纹未变。
//
// Python: _call_discovered_tool
func (r *ProgressiveToolRail) callDiscoveredTool(
	ctx context.Context,
	name string,
	args map[string]any,
	sess sessioninterfaces.SessionFacade,
	cbc *agentinterfaces.AgentCallbackContext,
) (*tool_discovery.RelayedToolOutput, error) {
	target := strings.TrimSpace(name)
	if target == "" {
		return nil, errors.New("tool_call requires a non-empty tool name.")
	}
	if !containsString(r.getDiscoveredTools(sess), target) {
		return rejectedToolOutput("Deferred tool '" + target + "' must be returned by tool_search before it can be called."), nil
	}
	card := r.lookupCard(cbc, target)
	if card == nil {
		return rejectedToolOutput("Deferred tool '" + target + "' is no longer registered."), nil
	}
	if card.EffectiveExposure() != tool.ToolExposureDeferred {
		return rejectedToolOutput("Tool '" + target + "' is not a deferred search result."), nil
	}
	if fingerprints := r.getDiscoveredToolFingerprints(sess); fingerprints != nil {
		expected := fingerprints[target]
		if expected == "" {
			return rejectedToolOutput("Deferred tool '" + target + "' must be searched again before it can be called."), nil
		}
		current := tool_discovery.ToolFingerprint(card.ToolInfo())
		if current != expected {
			r.invalidateDiscoveredTools(sess, map[string]struct{}{target: {}})
			return rejectedToolOutput("Deferred tool '" + target + "' has changed; search it again before calling it."), nil
		}
	}
	r.appendTrace(sess, map[string]any{"action": "tool_call", "name": target})

	outerID := "tool_call"
	if inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok && inputs.ToolCall != nil && inputs.ToolCall.ID != "" {
		outerID = inputs.ToolCall.ID
	}
	arguments, _ := json.Marshal(args)
	targetCall := &llmschema.ToolCall{
		ID:        outerID + ":target",
		Type:      "function",
		Name:      target,
		Arguments: string(arguments),
	}

	cbc.SetExtra(nestedToolCallKey, true)
	defer cbc.DeleteExtra(nestedToolCallKey)

	results := r.abilityManagerFrom(cbc).Execute(ctx, cbc, []*llmschema.ToolCall{targetCall}, sess, "")
	if len(results) == 0 {
		return rejectedToolOutput("Deferred tool '" + target + "' returned no execution result."), nil
	}
	result := results[0]
	if result.ToolMsg == nil {
		return rejectedToolOutput("Deferred tool '" + target + "' returned no execution result."), nil
	}
	rendered := result.ToolMsg.Content
	payload := map[string]any{"name": target, "result": result.Result}
	return tool_discovery.NewRelayedToolOutput(true, rendered, payload, nil), nil
}
```

`rejectedToolOutput` 助手：

```go
// rejectedToolOutput 构造失败结果（供模型读取的拒绝文案）。
func rejectedToolOutput(message string) *tool_discovery.RelayedToolOutput {
	return tool_discovery.NewRelayedToolOutput(false, "", nil, errors.New(message))
}
```

- [ ] **Step 4: 实现门卫、过滤与生命周期**

```go
// beforeToolCall 只放行 direct 与「嵌套标记 + 已授权」的调用。
//
// Python: before_tool_call
func (r *ProgressiveToolRail) beforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	_ = ctx
	toolName := toolNameOf(cbc)
	if toolName == "" {
		return nil
	}
	if _, isMeta := r.metaToolNames[toolName]; isMeta {
		return nil
	}
	card := r.lookupCard(cbc, toolName)
	sess := cbc.Session()
	nested, _ := cbc.GetExtra(nestedToolCallKey)
	if card == nil {
		if nested == true && containsString(r.getDiscoveredTools(sess), toolName) && r.isCachedTool(toolName) {
			return nil
		}
		rejectToolCall(cbc, "Unknown tool '"+toolName+"'.")
		return nil
	}
	if card.EffectiveExposure() == tool.ToolExposureDirect {
		return nil
	}
	if nested == true && containsString(r.getDiscoveredTools(sess), toolName) {
		return nil
	}
	rejectToolCall(cbc, "Deferred tool '"+toolName+"' must be found with tool_search before it can be called.")
	return nil
}

// rejectToolCall 阻断执行并向模型返回可读结果。
//
// Python: _reject_tool_call
func rejectToolCall(cbc *agentinterfaces.AgentCallbackContext, message string) {
	cbc.SetExtra("_skip_tool", true)
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return
	}
	callID := "unknown-tool-call"
	if inputs.ToolCall != nil && inputs.ToolCall.ID != "" {
		callID = inputs.ToolCall.ID
	}
	inputs.ToolResult = map[string]any{"error": message}
	inputs.ToolMsg = llmschema.NewToolMessage(callID, message)
}

// filterModelCallTools 把模型可见工具收敛为 meta ∪ direct。
//
// Python: before_model_call 的过滤段
func (r *ProgressiveToolRail) filterModelCallTools(inputs *agentinterfaces.ModelCallInputs) error {
	if inputs == nil || inputs.Tools == nil {
		return nil
	}
	am := r.abilityManagerFromInputs(inputs)
	filtered := make([]cschema.ToolInfoInterface, 0, len(inputs.Tools))
	for _, info := range inputs.Tools {
		name := info.GetName()
		if name == "" {
			continue
		}
		if _, isMeta := r.metaToolNames[name]; isMeta {
			filtered = append(filtered, info)
			continue
		}
		if card := lookupCardByName(am, name); card != nil && card.EffectiveExposure() == tool.ToolExposureDirect {
			filtered = append(filtered, info)
		}
	}
	inputs.Tools = filtered
	return nil
}
```

生命周期：

```go
// Init 注册两个元工具（显式 direct），保存注册表引用，不建索引。
//
// Python: ProgressiveToolRail.init
func (r *ProgressiveToolRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	_ = ctx
	language := r.catalogLanguage()
	agentID := ""
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}
	searchTool := tool_discovery.NewToolSearchTool(r.searchToolsWithSession, language, agentID, r.toolSearchLimit())
	callTool := tool_discovery.NewToolCallTool(language, agentID)
	callTool.SetDispatch(r.dispatchToolCall)
	r.metaToolNames = map[string]struct{}{searchTool.Card().Name: {}, callTool.Card().Name: {}}

	am := agent.AbilityManager()
	r.toolSearchRegistry = am
	r.ownedToolCards = make(map[string]*tool.ToolCard)
	if am == nil {
		return nil
	}
	for _, card := range []*tool.ToolCard{searchTool.Card(), callTool.Card()} {
		card.Exposure = tool.ToolExposureDirect
		card.SetExposureDeclared(true)
		if result := am.Add(card); result.Added {
			r.ownedToolCards[card.Name] = card
		}
	}
	r.registerToolResources(searchTool, callTool)
	return nil
}

// FinalizeStartupAsync 先物化惰性 schema，再建首份索引。
//
// Python: finalize_startup_async
func (r *ProgressiveToolRail) FinalizeStartupAsync(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	am := agent.AbilityManager()
	if am == nil {
		return nil
	}
	if _, err := am.ListToolInfo(ctx, nil); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "progressive_finalize_startup").
			Err(err).
			Msg("failed to materialize startup tool schemas")
	}
	r.rebuildToolSearchIndex(am, r.collectDeferredToolDocuments(am))
	return nil
}

// Uninit 按 ownership 移除元工具并清空缓存。
//
// Python: ProgressiveToolRail.uninit
func (r *ProgressiveToolRail) Uninit(agent agentinterfaces.BaseAgent) error {
	am := agent.AbilityManager()
	if am != nil {
		for name, card := range r.ownedToolCards {
			if current := am.Get(name); current != nil && current.AbilityID() != card.ID {
				continue // 名称已被他人重新注册，不得卸载
			}
			am.Remove(name)
		}
	}
	r.mu.Lock()
	r.ownedToolCards = nil
	r.metaToolNames = nil
	r.cachedAllToolInfos = nil
	r.toolSearchIndex = nil
	r.toolSearchIndexRevision = 0
	r.toolSearchRegistry = nil
	r.mu.Unlock()
	return nil
}
```

接线助手与生命周期（同文件）：

```go
// registerToolResources 把元工具实例注册到进程级 ResourceMgr（沿用旧 Init 步骤 3）。
func (r *ProgressiveToolRail) registerToolResources(tools ...tool.Tool) {
	resourceMgr := runner.GetResourceMgr()
	if resourceMgr == nil {
		return
	}
	for _, candidate := range tools {
		card := candidate.Card()
		if existing, err := resourceMgr.GetTool([]string{card.ID}); err == nil && len(existing) > 0 {
			continue
		}
		if err := resourceMgr.AddTool(candidate); err != nil {
			logger.Warn(logComponent).
				Str("event_type", "progressive_rail_init").
				Str("tool_id", card.ID).
				Err(err).
				Msg("failed to register tool to resource_mgr")
		}
	}
}

// searchToolsWithSession 搜索（会话取自 rail 的当前回调上下文）。
//
// 与 tool_search 工具签名对齐：工具只传 query/limit。
func (r *ProgressiveToolRail) searchToolsWithSession(ctx context.Context, query string, limit int) ([]map[string]any, error) {
	return r.searchTools(ctx, query, limit, r.currentSession())
}

// dispatchToolCall 分发 tool_call（回调上下文由工具侧注入）。
func (r *ProgressiveToolRail) dispatchToolCall(
	ctx context.Context,
	name string,
	args map[string]any,
	cb tool.ToolCallbackContext,
) (any, error) {
	cbc, ok := cb.(*agentinterfaces.AgentCallbackContext)
	if !ok {
		return nil, errors.New("tool_call requires an active agent callback context")
	}
	return r.callDiscoveredTool(ctx, name, args, cbc.Session(), cbc)
}

// BeforeInvoke 缓存全量工具清单并冻结会话初始目录。
//
// Python: before_invoke
func (r *ProgressiveToolRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	am := r.abilityManagerFrom(cbc)
	if am == nil {
		return nil
	}
	infos, err := am.ListToolInfo(ctx, nil)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "progressive_before_invoke").
			Err(err).
			Msg("failed to list tool infos")
	}
	r.cachedAllToolInfos = infos
	r.ensureInitialDeferredCatalog(cbc.Session(), am)
	return nil
}

// BeforeModelCall 注入规则节、同步目录附件、过滤顶层工具。
//
// Python: before_model_call
func (r *ProgressiveToolRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	am := r.abilityManagerFrom(cbc)
	if am == nil {
		return nil
	}
	builder := cbc.Agent().SystemPromptBuilder()
	if builder == nil {
		return errors.New("ProgressiveToolRail requires agent.system_prompt_builder")
	}
	initial := r.ensureInitialDeferredCatalog(cbc.Session(), am)
	builder.AddSection(sections.BuildProgressiveToolRulesSection(initial))

	if err := r.syncDeferredToolAttachment(ctx, cbc.Session(), r.attachments(cbc), r.collectDeferredToolDescriptions(am)); err != nil {
		return err
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok {
		return nil
	}
	return r.filterModelCallTools(inputs)
}

// GetCallbacks 注册 BeforeInvoke / BeforeModelCall / BeforeToolCall。
func (r *ProgressiveToolRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.EventBeforeInvoke] = r.callbackBeforeInvoke
	callbacks[agentinterfaces.EventBeforeModelCall] = r.callbackBeforeModelCall
	callbacks[agentinterfaces.EventBeforeToolCall] = r.callbackBeforeToolCall
	return callbacks
}
```

其余小助手（同文件）：`ensureInitialDeferredCatalog(sess, am)`（读/写 `initial_tools`，逻辑见 Task 14 的 `initialStateTools` + `saveCatalogState`）、`attachments(cbc)`（从 agent 取 `*prompts.Manager`）、`currentSession()`（rail 最近一次回调的会话，`BeforeInvoke` 时更新）、`toolNameOf(cbc)`（取 `ToolCallInputs.ToolName`）、`abilityManagerFromInputs(inputs)`（返回 rail 持有的注册表）。

- [ ] **Step 5: 删除旧机制残留**

```bash
grep -rn "visibleToolsKey\|navigationBaselineTools\|toolGroupOrder\|toolGroupCN\|scoreExactMatch\|buildNavigationSection\|buildNavigationEntries\|getVisibleTools\|setVisibleTools\|initVisibleTools\|maxLoadedTools\|defaultVisible\|alwaysVisible" internal/agentcore/harness/rails/
```

Expected: 空（全部随重写移除）；仅 `SectionToolNavigation` 常量保留在 `prompts/sections`。

- [ ] **Step 6: 运行确认通过**

Run: `go test ./internal/agentcore/harness/rails/ -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/rails/
git commit -m "feat(rails): tool_call 执行通道/门卫/顶层过滤/生命周期（15/15）"
```

---

## Task 16: 集成测试 IT-1 ~ IT-7

**Files:**
- Create: `tests/integration/agentcore/harness/progressive_tool/doc.go`、`main_test.go`（`suite.Run` 入口）、`progressive_exposure_test.go`、`progressive_catalog_test.go`、`progressive_search_call_test.go`、`progressive_loop_test.go`
- 复用: `tests/integration/suite/agent_suite.go`、`tests/integration/mockllm`

- [ ] **Step 1: 写 IT-1（曝光映射到提示词）**

```go
//go:build integration

package progressive_tool

// TestProgressive_ExposureMapsToPrompt 断言曝光标签真的决定模型看到的 tools 与提示词目录。
func (s *ProgressiveSuite) TestProgressive_ExposureMapsToPrompt() {
	agent, sess := s.newProgressiveAgent(true,
		s.newTool("read_file", "读取文件"),
		s.newTool("xlsx_read", "读取 Excel 表格"),
		s.newTool("pdf_read", "提取 PDF 文本"),
	)

	card := agent.AbilityManager().Get("xlsx_read").(*tool.ToolCard)
	s.Equal(tool.ToolExposureDeferred, card.EffectiveExposure())
	s.Equal(tool.ToolExposureDirect, agent.AbilityManager().Get("read_file").(*tool.ToolCard).EffectiveExposure())

	inputs := s.fireBeforeModelCall(agent, sess)
	visible := toolNamesOf(inputs.Tools)
	s.Contains(visible, "read_file")
	s.Contains(visible, "tool_search")
	s.Contains(visible, "tool_call")
	s.NotContains(visible, "xlsx_read")
	s.NotContains(visible, "pdf_read")

	prompt := s.systemPrompt(agent, sess)
	s.Contains(prompt, "- **xlsx_read**: 读取 Excel 表格")
	s.Contains(prompt, "- **pdf_read**: 提取 PDF 文本")
}
```

- [ ] **Step 2: 写 IT-2（目录变更推送）与 IT-3（搜索真检索）**

```go
//go:build integration

func (s *ProgressiveSuite) TestProgressive_CatalogDeltaThenSnapshot() {
	agent, sess := s.newProgressiveAgent(true, s.newTool("xlsx_read", "读取 Excel"))
	s.runOneModelCall(agent, sess)

	// 运行中新增 deferred 工具
	s.registerTool(agent, s.newTool("pdf_read", "提取 PDF"))

	s.runOneModelCall(agent, sess)
	attachment := s.catalogAttachment(agent, sess)
	s.Equal("delta", attachment.Metadata["catalog_mode"])
	s.Contains(attachment.Content, "新增")
	s.Contains(attachment.Content, "pdf_read")

	// 连续 delta 达上限后改发快照
	for i := 0; i < 10; i++ {
		s.registerTool(agent, s.newTool("extra_tool_"+strconv.Itoa(i), "额外工具"))
		s.runOneModelCall(agent, sess)
	}
	s.Equal("snapshot", s.catalogAttachment(agent, sess).Metadata["catalog_mode"])
}

func (s *ProgressiveSuite) TestProgressive_ToolSearchOnlyHitsDeferred() {
	agent, sess := s.newProgressiveAgent(true,
		s.newTool("read_file", "读取文件"),
		s.newTool("xlsx_read", "读取 Excel 表格"),
		s.newTool("search_skill_extra", "扩展搜索"),
	)
	s.runOneModelCall(agent, sess)

	results := s.invokeToolVia(agent, sess, "tool_search", map[string]any{"query": "excel", "limit": 5})
	s.Len(results, 1)
	s.Equal("xlsx_read", results[0]["name"])
	s.Contains(results[0]["parameters"], "properties")

	// 参数变更后必须重建索引并命中新 schema
	s.updateToolParams(agent, "xlsx_read", map[string]any{
		"type":       "object",
		"properties": map[string]any{"sheet_name": map[string]any{"type": "string"}},
	})
	results = s.invokeToolVia(agent, sess, "tool_search", map[string]any{"query": "xlsx_read"})
	s.Len(results, 1)
	s.Contains(results[0]["parameters"], "sheet_name")
}
```

- [ ] **Step 3: 写 IT-4（执行真调）与 IT-5（负例四连）**

```go
//go:build integration

func (s *ProgressiveSuite) TestProgressive_ToolCallExecutesThroughRails() {
	counter := &atomic.Int32{}
	agent, sess := s.newProgressiveAgentWithCounter(true, counter, s.newTool("xlsx_read", "读取 Excel"))
	s.runOneModelCall(agent, sess)
	s.mustSearch(agent, sess, "xlsx_read")

	out := s.invokeToolCall(agent, sess, "xlsx_read", map[string]any{"sheet": "S1"})
	s.True(out.Success())
	s.Equal(int32(1), counter.Load(), "目标工具应真实执行一次")
	s.Contains(out.RenderedText(), "sheet=S1")
}

func (s *ProgressiveSuite) TestProgressive_ToolCallRejections() {
	agent, sess := s.newProgressiveAgent(true, s.newTool("xlsx_read", "读取 Excel"), s.newTool("read_file", "读取文件"))
	s.runOneModelCall(agent, sess)

	// ① 未搜索
	out := s.invokeToolCall(agent, sess, "xlsx_read", nil)
	s.False(out.Success())
	s.Contains(out.Err().Error(), "must be returned by tool_search")

	// ② 搜索后被移除
	s.mustSearch(agent, sess, "xlsx_read")
	agent.AbilityManager().Remove("xlsx_read")
	out = s.invokeToolCall(agent, sess, "xlsx_read", nil)
	s.Contains(out.Err().Error(), "no longer registered")

	// ③ direct 工具不能走 tool_call
	s.registerTool(agent, s.newTool("plain_direct", "直通"))
	out = s.invokeToolCall(agent, sess, "plain_direct", nil)
	s.Contains(out.Err().Error(), "not a deferred search result")

	// ④ 搜索后改 schema
	s.registerTool(agent, s.newTool("yaml_read", "读取 YAML"))
	s.mustSearch(agent, sess, "yaml_read")
	s.updateToolParams(agent, "yaml_read", map[string]any{"type": "object", "properties": map[string]any{"z": map[string]any{"type": "string"}}})
	out = s.invokeToolCall(agent, sess, "yaml_read", nil)
	s.Contains(out.Err().Error(), "has changed; search it again")
}
```

- [ ] **Step 4: 写 IT-6（MockLLM 全轮次闭环，核心验收）**

```go
//go:build integration

func (s *ProgressiveSuite) TestProgressive_MockLLMFullLoop() {
	counter := &atomic.Int32{}
	server := mockllm.NewServer(
		mockllm.ToolCallResponse("tool_search", map[string]any{"query": "excel"}),
		mockllm.ToolCallResponse("tool_call", map[string]any{"name": "xlsx_read", "args": map[string]any{"sheet": "S1"}}),
		mockllm.TextResponse("已读取 Sheet1"),
	)
	s.InstallMockLLM(server)

	agent, sess := s.newProgressiveAgentWithCounter(true, counter, s.newTool("xlsx_read", "读取 Excel"))
	s.ChatOnce(agent, sess, "帮我读 Sheet1")

	requests := server.Requests()
	s.Require().Len(requests, 3)

	first := toolNamesOfRequest(requests[0])
	s.NotContains(first, "xlsx_read", "第一轮顶层 tools 不应含 deferred 工具")
	s.Contains(first, "tool_search")

	s.Contains(lastToolResultContent(requests[1]), `"sheet"`, "第二轮应携带 tool_search 返回的完整 schema")

	s.Equal(int32(1), counter.Load(), "目标工具应被执行一次")
	s.Contains(s.lastAssistantText(sess), "已读取 Sheet1")
}
```

- [ ] **Step 5: 写 IT-7（同会话复用）**

```go
//go:build integration

func (s *ProgressiveSuite) TestProgressive_SessionReuseKeepsAuthorization() {
	agent, sess := s.newProgressiveAgent(true, s.newTool("xlsx_read", "读取 Excel"))
	s.runOneModelCall(agent, sess)
	s.mustSearch(agent, sess, "xlsx_read")

	// 同一 session、新 rail 实例（模拟热重载）
	s.ReinitRail(agent)
	s.runOneModelCall(agent, sess)
	s.Len(s.catalogAttachments(agent, sess), 1, "不应重复推送同一差异")

	out := s.invokeToolCall(agent, sess, "xlsx_read", map[string]any{"sheet": "S1"})
	s.True(out.Success(), "指纹未变的授权应继续可用")
}
```

- [ ] **Step 6: 跑集成测试**

Run: `go test -tags=integration ./tests/integration/agentcore/harness/progressive_tool/ -v`
Expected: PASS（7 条用例）

- [ ] **Step 7: 审计**

Run: `go run ./cmd/integration-audit -verify`
Expected: 新目录通过（调用跨度 ≥3：harness/single_agent/session/context_engine/foundation）

- [ ] **Step 8: 提交**

```bash
git add tests/integration/agentcore/harness/progressive_tool/
git commit -m "test(integration): 延迟工具目录曝光/目录/搜索/执行/闭环 7 例"
```

---

## Task 17: 计划与台账同步、doc.go、锚点与门禁

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`、`docs/sync/defect-plan-2026-11-04.md`
- Modify: `internal/agentcore/harness/rails/doc.go`、`internal/agentcore/harness/tools/tool_discovery/doc.go`、`internal/agentcore/harness/prompts/sections/doc.go`、`internal/agentcore/harness/prompts/tools/doc.go`、`internal/agentcore/context_engine/processor/compressor/doc.go`

- [ ] **Step 1: 更新计划行**

```
| 9.11 | ✅ | ProgressiveToolRail | 渐进式工具权限（⚠️ 旧语义 search_tools/load_tools 已由 9.11a 取代） | `openjiuwen/harness/rails/` |
| 9.11a | ✅ | 延迟工具目录（deferred tool catalog） | exposure 策略（ToolExposure + registryRevision）/ recall/bm25 / tool_search（BM25+精确名优先）/ tool_call（嵌套执行+RelayedToolOutput）/ 目录附件 delta·snapshot（版本+指纹+失效）/ 顶层 tools 过滤 / 旧机制删除（search_tools·load_tools·可见白名单·导航节·三配置字段） | `openjiuwen/harness/rails/progressive_tool_rail.py` · `harness/tools/tool_discovery/` |
```

领域五新增：

```
| 5.32 | ✅ | recall BM25 原语 | BM25Index（k1=1.5/b=0.75）+ normalizeMarkdown + 中英混排 tokenize；供 tool_discovery 复用；archive/retriever/compression_recall 工具另章实施 | `openjiuwen/core/context_engine/processor/forked/compressor/recall/bm25.py` |
```

- [ ] **Step 2: 更新 D-18 状态**

`docs/sync/defect-plan-2026-11-04.md` 中 D-18 的「状态」列由 🔍 改为 `✅（9.11a 处置，见 docs/superpowers/specs/2026-11-05-progressive-deferred-tool-catalog-design.md）`。

- [ ] **Step 3: 同步 doc.go 文件树**

删除 `search_tools.go`/`load_tools.go` 条目，新增 `tool_search.go`/`tool_call.go`/`bm25.go`；`recall` 包 `doc.go` 已在 Task 5 建好；`rails/doc.go` 与 `prompts/sections/doc.go` 描述改为新语义。

- [ ] **Step 4: 跑锚点断言**

```bash
grep -rn "search_tools\|load_tools" internal/ | grep -v mcp_search      # 期望空
grep -rn "ProgressiveTool\(AlwaysVisible\|DefaultVisible\|MaxLoaded\)" internal/   # 期望空
grep -rn "BuildNavigationSection\|BuildNavigationEntry" internal/       # 期望空
grep -rn "延迟工具目录.*未移植" internal/                                # 期望空
```

- [ ] **Step 5: 全量门禁**

```bash
pgrep -f 'go (build|test)' || true
go build ./... && go vet ./...
go test -race ./internal/agentcore/... 
go test -cover ./internal/agentcore/harness/rails/ ./internal/agentcore/harness/tools/tool_discovery/ ./internal/agentcore/single_agent/ability/ ./internal/agentcore/context_engine/processor/compressor/recall/
go test -tags=integration ./tests/integration/agentcore/... -count=1
```

Expected: 全绿；四个包改动行覆盖率 ≥85%

- [ ] **Step 6: 提交并推送**

```bash
git add IMPLEMENTATION_PLAN.md docs/sync/defect-plan-2026-11-04.md internal/agentcore/harness/rails/doc.go internal/agentcore/harness/tools/tool_discovery/doc.go internal/agentcore/harness/prompts/sections/doc.go internal/agentcore/harness/prompts/tools/doc.go internal/agentcore/context_engine/processor/compressor/doc.go
git commit -m "docs(plan): 9.11a/5.32 落地记录与 D-18 收口，同步 doc.go"
git push
```

- [ ] **Step 7: 监控 CI**

Run: `gh run list --limit 3` → `gh run watch <run-id>`
Expected: Build / Lint / Unit Test / Integration Test 四个 job 全 SUCCESS；红灯则修复后重推，直至全绿。

---

## 自审记录

- **Spec 覆盖**：§1.1/1.2 → Task 17 锚点；§4.1 新增 4 文件 → Task 5/6/7/1、2；§4.2 改写 10 项 → Task 3/4/8/9/10/11/12/13-15；§4.3 删除 6 组 → Task 8/9/15/12；§5.1 → Task 3/12；§5.2 → Task 13/15；§5.3/5.4 → Task 14；§5.5 → Task 6/13；§5.6 → Task 15；§6 三处适配点 → Task 2/4/11/3；§7 → Task 7/10/15；§8 → Task 1-17 各 Step；§9 → Task 17；§10 偏差 → Task 5（D-1）、Task 12（D-2）、Task 15（D-3 日志）、Task 2/4（D-4）、Task 17（D-5 验证）
- **占位扫描**：无 "TBD/TODO/类似 Task N"；唯一"按既有约定补齐"集中在 Task 8 的 `itoa`/`schema` 助手与 Task 14 的 `attachmentManager` 适配，均给出明确落点与替代实现
- **类型一致性**：`ToolExposure`/`EffectiveExposure`/`GetExposureDeclared`（Task 1）在 Task 3/12/13/15 一致；`BM25ToolIndex.SourceFingerprint`/`Search`/`Fingerprint`/`ToolFingerprint`（Task 6）在 Task 13 一致；`RelayedToolOutput.RenderedText/Success/Err`（Task 9）在 Task 15/16 一致
