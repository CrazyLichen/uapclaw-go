# 9.11 ProgressiveToolRail 渐进式工具权限 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 实现 ProgressiveToolRail 渐进式工具权限，解决工具数量过多时 LLM 上下文爆炸问题，同时重构 BaseRail.GetCallbacks 分层、RailAgent 接口扩充、SystemPromptBuilder 统一接口。

**Architecture:** 分5个层次实施——(1)core层GetCallbacks分层 (2)SystemPromptBuilder统一接口+RailAgent扩充 (3)DeepAgentRail基类 (4)元工具SearchToolsTool/LoadToolsTool (5)ProgressiveToolRail核心+回填。每层完成后可独立编译测试。

**Tech Stack:** Go 1.21+, 项目内已有框架（AgentRail/BaseRail/AbilityManager/ResourceMgr/SessionFacade/SystemPromptBuilder）

**设计文档:** `docs/superpowers/specs/2026-07-06-progressive-tool-rail-9.11-design.md`

**Python 对照代码:**
- `openjiuwen/harness/rails/base.py` (DeepAgentRail, 108行)
- `openjiuwen/harness/rails/progressive_tool_rail.py` (635行)
- `openjiuwen/harness/tools/tool_discovery/search_tools.py` (107行)
- `openjiuwen/harness/tools/tool_discovery/load_tools.py` (77行)

---

## Task 1: SystemPromptBuilder Language 字段改造为 getter 方法

**Files:**
- Modify: `internal/agentcore/single_agent/prompts/builder.go`
- Modify: `internal/agentcore/single_agent/prompts/builder_test.go`
- Modify: `internal/agentcore/harness/prompts/builder.go`
- Modify: 所有引用 `.Language` 的文件（需 grep 确认）

**背景:** 当前 `SystemPromptBuilder.Language` 是公开字段，无法满足接口约束。需改为私有字段 `language` + getter 方法 `Language() string`。

- [x] **Step 1: 搜索所有引用 `.Language` 的位置**

Run: `cd /home/opensource/uap-claw-go && grep -rn '\.Language' internal/agentcore/single_agent/prompts/ internal/agentcore/harness/prompts/ --include='*.go' | grep -v '_test.go' | grep -v 'doc.go'`

记录所有需要从 `.Language` 改为 `.Language()` 的位置。

- [x] **Step 2: 修改 `single_agent/prompts/builder.go`**

将 `Language string` 公开字段改为 `language string` 私有字段，新增 `Language() string` getter 方法，新增 `SetLanguage(lang string)` setter（构造函数中设置）。所有内部引用 `.Language` 改为 `.language` 或 `.Language()`。保持 `NewSystemPromptBuilder` / `NewSystemPromptBuilderWithFilter` / `NewSystemPromptBuilderWithPromptMode` 构造函数参数名不变。

- [x] **Step 3: 修改 `single_agent/prompts/builder_test.go`**

将测试中 `.Language = ` 赋值改为使用构造函数或 `SetLanguage()`，将 `.Language` 读取改为 `.Language()`。

- [x] **Step 4: 修改 `harness/prompts/builder.go`**

`harness/prompts/builder.go` 中 `ResolveLanguage` 等函数引用了 `b.Language`，改为 `b.Language()` 调用。`buildWithFilter` 中的 `s.Render(b.Language)` 改为 `s.Render(b.Language())`。

- [x] **Step 5: 修改 `harness/prompts/builder_test.go`**

适配 Language 改造。

- [x] **Step 6: Grep 全量搜索并修复其余 `.Language` 引用**

Run: `cd /home/opensource/uap-claw-go && grep -rn '\.Language\b' internal/agentcore/ --include='*.go' | grep -v '_test.go' | grep -v 'doc.go' | grep -v 'Language()' | grep -v 'SetLanguage' | grep -v 'language'`

修复所有从公开字段访问改为方法调用的位置。

- [x] **Step 7: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/...`

Expected: 编译通过

- [x] **Step 8: 运行相关测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/prompts/... ./internal/agentcore/harness/prompts/... -count=1`

Expected: PASS

- [x] **Step 9: 提交**

```
refactor: SystemPromptBuilder.Language 从公开字段改为 getter 方法
```

---

## Task 2: 定义 SystemPromptBuilderInterface 最小接口

**Files:**
- Modify: `internal/agentcore/single_agent/prompts/builder.go`
- Modify: `internal/agentcore/single_agent/prompts/builder_test.go`

- [x] **Step 1: 在 `builder.go` 中定义 `SystemPromptBuilderInterface`**

在 `single_agent/prompts/builder.go` 的结构体区块新增：

```go
// SystemPromptBuilderInterface 系统提示词构建器最小接口。
//
// 供 Rail 等消费者通过 RailAgent 接口访问 SystemPromptBuilder，
// 避免依赖具体类型。saprompt.SystemPromptBuilder 和
// hprompts.SystemPromptBuilder（嵌入 base）均隐式满足此接口。
//
// 对齐 Python: agent.system_prompt_builder 属性的类型约束
type SystemPromptBuilderInterface interface {
	// AddSection 添加或替换节
	AddSection(section PromptSection) *SystemPromptBuilder
	// RemoveSection 移除指定名称的节
	RemoveSection(name string) *SystemPromptBuilder
	// Language 返回当前语言
	Language() string
	// GetSection 按名称获取单个节
	GetSection(name string) *PromptSection
	// HasSection 检查节是否存在
	HasSection(name string) bool
}
```

注意：`AddSection` / `RemoveSection` 返回 `*SystemPromptBuilder`（链式调用），接口方法签名与现有方法一致，`*SystemPromptBuilder` 隐式满足此接口。

- [x] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/single_agent/prompts/...`

- [x] **Step 3: 添加接口满足编译时断言**

在 `builder.go` 全局变量区块新增：`var _ SystemPromptBuilderInterface = (*SystemPromptBuilder)(nil)`

- [x] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/prompts/... -count=1`

- [x] **Step 5: 提交**

```
feat(prompts): 定义 SystemPromptBuilderInterface 最小接口
```

---

## Task 3: RailAgent 接口扩充 AbilityManager + SystemPromptBuilder

**Files:**
- Modify: `internal/agentcore/single_agent/rail/context.go`
- Modify: `internal/agentcore/single_agent/rail/context_test.go`
- Modify: 所有实现 RailAgent 接口的 fake/stub 类型

- [x] **Step 1: 扩充 RailAgent 接口**

在 `rail/context.go` 中 `RailAgent` 接口新增两个方法：

```go
type RailAgent interface {
	// CallbackManager 返回 PerAgent 回调管理器
	CallbackManager() *AgentCallbackManager
	// AgentID 返回 Agent 唯一标识
	AgentID() string
	// AbilityManager 返回能力管理器
	AbilityManager() agentinterfaces.AbilityManagerInterface
	// SystemPromptBuilder 返回系统提示词构建器
	SystemPromptBuilder() saprompt.SystemPromptBuilderInterface
}
```

需新增 import：
- `agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"`
- `saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"`

- [x] **Step 2: 适配 context_test.go 中的 fakeRailAgent**

在 `fakeRailAgent` 结构体新增 `abilityMgr` 和 `promptBuilder` 字段，实现两个新方法。

- [x] **Step 3: Grep 搜索并修复所有实现 RailAgent 的 fake/stub**

Run: `cd /home/opensource/uap-claw-go && grep -rn 'RailAgent' internal/agentcore/ --include='*_test.go' | grep 'type.*struct'`

对每个实现 RailAgent 的测试 fake/stub 补充 `AbilityManager()` 和 `SystemPromptBuilder()` 方法（返回 nil 即可）。

- [x] **Step 4: 确认 DeepAgent 和 ReActAgent 已满足新接口**

DeepAgent 已有 `AbilityManager()` 方法（L370），需新增 `SystemPromptBuilder()` 导出方法（当前是私有字段 `systemPromptBuilder`）。

ReActAgent 已有 `AbilityManager()`（react_prompt.go:96）和 `PromptBuilder()`（react_prompt.go:153），需确认 `PromptBuilder()` 返回类型是否可满足 `SystemPromptBuilderInterface`。

- [x] **Step 5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/...`

如有编译错误，逐个修复 fake/stub 的缺失方法。

- [x] **Step 6: 运行 rail 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/rail/... -count=1`

- [x] **Step 7: 提交**

```
feat(rail): RailAgent 接口扩充 AbilityManager + SystemPromptBuilder
```

---

## Task 4: DeepAgent 暴露 SystemPromptBuilder() + 删除 syncBuilderToActiveRails 类型断言注入

**Files:**
- Modify: `internal/agentcore/harness/deep_agent.go`
- Modify: `internal/agentcore/harness/deep_agent_test.go`

- [x] **Step 1: DeepAgent 新增 SystemPromptBuilder() 导出方法**

在 `deep_agent.go` 导出函数区块新增：

```go
// SystemPromptBuilder 返回系统提示词构建器。
//
// 对齐 Python: DeepAgent.system_prompt_builder 属性
func (d *DeepAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	if d.systemPromptBuilder != nil {
		return d.systemPromptBuilder.SystemPromptBuilder
	}
	return nil
}
```

- [x] **Step 2: 删除 syncBuilderToActiveRails 中 SetSystemPromptBuilder 类型断言注入**

将 `syncBuilderToActiveRails` 函数中 for 循环内的 `SetSystemPromptBuilder` 类型断言代码删除。保留 ReActAgent 的 `SetPromptBuilder` 同步逻辑。

修改前：
```go
for _, r := range allRails {
    if setter, ok := r.(interface {
        SetSystemPromptBuilder(*saprompts.SystemPromptBuilder)
    }); ok {
        if d.systemPromptBuilder != nil {
            setter.SetSystemPromptBuilder(d.systemPromptBuilder.SystemPromptBuilder)
        }
    }
}
```

修改后：删除整个 for 循环（此函数可能变空，考虑删除整个函数或仅保留 ReActAgent 同步部分）。

- [x] **Step 3: 删除 `_sync_builder_to_active_rails` 调用点**

搜索 `syncBuilderToActiveRails` 的所有调用点，删除或简化。

- [x] **Step 4: 适配 deep_agent_test.go**

确保测试通过。如有 fake Agent 需补充 `SystemPromptBuilder()` 方法。

- [x] **Step 5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/...`

- [x] **Step 6: 运行 harness 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -count=1 -timeout 120s`

- [x] **Step 7: 提交**

```
refactor(deep_agent): 暴露 SystemPromptBuilder() 导出方法，删除 syncBuilderToActiveRails 类型断言注入
```

---

## Task 5: BaseRail.GetCallbacks 分层 + 事件枚举分组

**Files:**
- Modify: `internal/agentcore/single_agent/rail/event.go`
- Modify: `internal/agentcore/single_agent/rail/event_test.go`
- Modify: `internal/agentcore/single_agent/rail/rail.go`
- Modify: `internal/agentcore/single_agent/rail/rail_test.go`

**Python 对照:** `openjiuwen/core/single_agent/rail/base.py` L434-443 (EVENT_METHOD_MAP 8个), `openjiuwen/harness/rails/base.py` L22-25 (DEEP_EVENT_METHOD_MAP 2个)

- [x] **Step 1: event.go 新增事件分组函数和映射**

在 event.go 常量区块后新增：

```go
// BaseEventMethodMap 基础事件→方法名映射（8个，不含 task-iteration）。
//
// 对齐 Python: EVENT_METHOD_MAP (openjiuwen/core/single_agent/rail/base.py L434-442)
var BaseEventMethodMap = map[AgentCallbackEvent]string{
	CallbackBeforeInvoke:      "BeforeInvoke",
	CallbackAfterInvoke:       "AfterInvoke",
	CallbackBeforeModelCall:   "BeforeModelCall",
	CallbackAfterModelCall:    "AfterModelCall",
	CallbackOnModelException:  "OnModelException",
	CallbackBeforeToolCall:    "BeforeToolCall",
	CallbackAfterToolCall:     "AfterToolCall",
	CallbackOnToolException:   "OnToolException",
}

// DeepEventMethodMap DeepAgent 扩展事件→方法名映射（2个 task-iteration hooks）。
//
// 对齐 Python: DEEP_EVENT_METHOD_MAP (openjiuwen/harness/rails/base.py L22-25)
var DeepEventMethodMap = map[AgentCallbackEvent]string{
	CallbackBeforeTaskIteration: "BeforeTaskIteration",
	CallbackAfterTaskIteration:  "AfterTaskIteration",
}
```

在导出函数区块新增：

```go
// AllBaseCallbackEvents 返回 8 个基础回调事件（不含 task-iteration）。
func AllBaseCallbackEvents() []AgentCallbackEvent {
	return []AgentCallbackEvent{
		CallbackBeforeInvoke,
		CallbackAfterInvoke,
		CallbackBeforeModelCall,
		CallbackAfterModelCall,
		CallbackOnModelException,
		CallbackBeforeToolCall,
		CallbackAfterToolCall,
		CallbackOnToolException,
	}
}

// AllDeepCallbackEvents 返回 2 个 DeepAgent 扩展回调事件（task-iteration）。
func AllDeepCallbackEvents() []AgentCallbackEvent {
	return []AgentCallbackEvent{
		CallbackBeforeTaskIteration,
		CallbackAfterTaskIteration,
	}
}
```

- [x] **Step 2: 修改 BaseRail.GetCallbacks() 只提取 8 个基础事件**

在 `rail.go` 中重写 `BaseRail.GetCallbacks()`：

```go
// GetCallbacks 返回基础事件映射（8个，不含 task-iteration）。
//
// DeepAgentRail 子类通过 GetCallbacks 合并 DeepEventMethodMap。
// 对齐 Python: AgentRail.get_callbacks() 只提取 EVENT_METHOD_MAP 中的 8 个基础 hooks。
func (r *BaseRail) GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := make(map[AgentCallbackEvent]cb.PerAgentCallbackFunc)
	for event, methodName := range BaseEventMethodMap {
		method := r.getMethodByName(methodName)
		if method != nil && !r.isBaseMethod(methodName) {
			callbacks[event] = method
		}
	}
	return callbacks
}
```

新增辅助方法 `getMethodByName` 和 `isBaseMethod`（对齐 Python `_is_base_method`）。

- [x] **Step 3: 适配 event_test.go**

新增 `AllBaseCallbackEvents` / `AllDeepCallbackEvents` 测试，验证 `BaseEventMethodMap` / `DeepEventMethodMap` 长度和内容。

- [x] **Step 4: 适配 rail_test.go**

修改 `TestBaseRail_GetCallbacks` 验证只返回基础事件（不含 BeforeTaskIteration/AfterTaskIteration）。

- [x] **Step 5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/single_agent/rail/...`

- [x] **Step 6: 运行 rail 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/rail/... -count=1`

- [x] **Step 7: 提交**

```
refactor(rail): BaseRail.GetCallbacks 分层，只提取8个基础事件
```

---

## Task 6: 新建 harness/rails/ 包 + DeepAgentRail 基类

**Files:**
- Create: `internal/agentcore/harness/rails/doc.go`
- Create: `internal/agentcore/harness/rails/base.go`
- Create: `internal/agentcore/harness/rails/base_test.go`

**Python 对照:** `openjiuwen/harness/rails/base.py` (108行)

- [x] **Step 1: 创建 rails/doc.go**

```go
// Package rails 提供 DeepAgent 扩展 Rail 实现。
//
// 在 single_agent/rail 基础上增加：
//   - DeepAgentRail 基类：扩展 AgentRail，增加 workspace/sys_operation 和 task-iteration hooks
//   - ProgressiveToolRail：渐进式工具权限 Rail
//
// 文件目录：
//
//	rails/
//	├── doc.go           # 包文档
//	├── base.go          # DeepAgentRail 基类
//	└── progressive.go   # ProgressiveToolRail
//
// 对应 Python 代码：openjiuwen/harness/rails/
package rails
```

- [x] **Step 2: 创建 rails/base.go**

实现 `DeepAgentRail` 结构体，嵌入 `rail.BaseRail`，增加 `workspace` / `sysOperation` 字段。

关键方法：
- `NewDeepAgentRail()` 构造函数，priority=50
- `SetWorkspace(w)` / `Workspace()`
- `SetSysOperation(op)` / `SysOperation()`
- `BeforeTaskIteration()` / `AfterTaskIteration()` no-op
- `GetCallbacks()` 合并 BaseRail.GetCallbacks() + DeepEventMethodMap 中被覆盖的 task-iteration hooks
- `isDeepBase(methodName)` 检测子类是否覆盖了 DeepAgentRail 的 no-op

需 import：
- `"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"`
- `"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"`
- sysop 包路径

- [x] **Step 3: 创建 rails/base_test.go**

测试用例：
- `TestNewDeepAgentRail` — 验证默认优先级和零值字段
- `TestDeepAgentRail_SetWorkspace` — 验证 Set/Get
- `TestDeepAgentRail_SetSysOperation` — 验证 Set/Get
- `TestDeepAgentRail_GetCallbacks_基础hooks` — 验证只返回8个基础事件（未覆盖 task-iteration 时）
- `TestDeepAgentRail_GetCallbacks_合并taskIteration` — 子类覆盖 BeforeTaskIteration 后验证返回9个事件

- [x] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/...`

- [x] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/... -count=1`

- [x] **Step 6: 提交**

```
feat(harness/rails): 新建 rails 包，实现 DeepAgentRail 基类
```

---

## Task 7: 元工具 SearchToolsTool

**Files:**
- Create: `internal/agentcore/harness/tools/tool_discovery/doc.go`
- Create: `internal/agentcore/harness/tools/tool_discovery/search_tools.go`
- Create: `internal/agentcore/harness/tools/tool_discovery/search_tools_test.go`

**Python 对照:** `openjiuwen/harness/tools/tool_discovery/search_tools.py` (107行)

- [x] **Step 1: 创建 tool_discovery/doc.go**

包文档，说明本包提供渐进式工具发现元工具（SearchToolsTool / LoadToolsTool）。

- [x] **Step 2: 创建 search_tools.go**

实现 `SearchToolsTool`，持有 `card *tool.ToolCard`、`searchToolsFn` 回调、`appendTraceFn` 回调。

```go
// SearchToolsInput search_tools 工具输入参数
type SearchToolsInput struct {
    Query       string
    Limit       int
    DetailLevel int
}

// SearchToolsTool 搜索候选工具元工具
type SearchToolsTool struct {
    card          *tool.ToolCard
    searchToolsFn func(ctx context.Context, query string, limit int, detailLevel int) ([]map[string]any, error)
    appendTraceFn func(session any, event map[string]any)
}
```

方法：
- `NewSearchToolsTool(searchFn, traceFn, language, agentID)` — 用 `tool.NewToolCard` + `GetSearchToolsMetadataProviderInputParams` 构建 card
- `Card()` / `Invoke()` / `Stream()` — 实现 `tool.Tool` 接口
- `Invoke` 中解析输入、调用 searchFn、调用 appendTraceFn、返回 matches + callability_note + next_step_hint
- `Stream` 返回 `ErrStreamNotSupported`

- [x] **Step 3: 创建 search_tools_test.go**

测试用例：
- `TestSearchToolsTool_Card` — 验证 card name/description
- `TestSearchToolsTool_Invoke_正常` — mock searchFn 返回结果，验证输出格式
- `TestSearchToolsTool_Invoke_限幅` — 验证 limit 上限为 20
- `TestSearchToolsTool_Invoke_错误` — searchFn 返回 error，验证 success=false
- `TestSearchToolsTool_Stream` — 验证返回 ErrStreamNotSupported

- [x] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/tools/tool_discovery/...`

- [x] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/tool_discovery/... -count=1`

- [x] **Step 6: 提交**

```
feat(tool_discovery): 实现 SearchToolsTool 元工具
```

---

## Task 8: 元工具 LoadToolsTool

**Files:**
- Create: `internal/agentcore/harness/tools/tool_discovery/load_tools.go`
- Create: `internal/agentcore/harness/tools/tool_discovery/load_tools_test.go`

**Python 对照:** `openjiuwen/harness/tools/tool_discovery/load_tools.py` (77行)

- [x] **Step 1: 创建 load_tools.go**

实现 `LoadToolsTool`，持有 `card *tool.ToolCard`、`loadToolsFn` 回调。

```go
// LoadToolsInput load_tools 工具输入参数
type LoadToolsInput struct {
    ToolNames []string
    Replace   bool
}

// LoadToolsTool 加载工具到 session 可见集合元工具
type LoadToolsTool struct {
    card         *tool.ToolCard
    loadToolsFn  func(ctx context.Context, session any, toolNames []string, replace bool) (map[string]any, error)
}
```

方法：
- `NewLoadToolsTool(loadFn, language, agentID)`
- `Card()` / `Invoke()` / `Stream()`
- `Invoke` 中解析输入、调用 loadFn、返回 loaded_tools + visible_tools + skipped_tools + message

- [x] **Step 2: 创建 load_tools_test.go**

测试用例：
- `TestLoadToolsTool_Card` — 验证 card name/description
- `TestLoadToolsTool_Invoke_正常` — mock loadFn 返回结果，验证输出格式
- `TestLoadToolsTool_Invoke_错误` — loadFn 返回 error，验证 success=false
- `TestLoadToolsTool_Stream` — 验证返回 ErrStreamNotSupported

- [x] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/tools/tool_discovery/...`

- [x] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/tool_discovery/... -count=1`

- [x] **Step 5: 提交**

```
feat(tool_discovery): 实现 LoadToolsTool 元工具
```

---

## Task 9: ProgressiveToolRail 核心实现

**Files:**
- Create: `internal/agentcore/harness/rails/progressive.go`
- Create: `internal/agentcore/harness/rails/progressive_test.go`

**Python 对照:** `openjiuwen/harness/rails/progressive_tool_rail.py` (635行)

这是最核心也最大的一个 Task。按 Python 逻辑逐方法翻译。

- [x] **Step 1: 创建 progressive.go 骨架**

定义结构体和常量：

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
    // visibleToolsKey session 中可见工具列表的 state key
    visibleToolsKey = "__progressive_visible_tool_names__"
    // discoveryTraceKey session 中工具发现轨迹的 state key
    discoveryTraceKey = "__progressive_tool_discovery_trace__"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProgressiveToolRail 渐进式工具权限 Rail。
//
// LLM 初始只看到少量导航工具（search_tools / load_tools）和默认可见工具，
// 通过 search 发现、load 按需加载，避免一次性把数百个工具 schema 塞入 prompt。
//
// 对应 Python: ProgressiveToolRail (openjiuwen/harness/rails/progressive_tool_rail.py)
type ProgressiveToolRail struct {
    DeepAgentRail
    // config 深层配置
    config *hschema.DeepAgentConfig
    // defaultVisible 默认可见工具
    defaultVisible map[string]struct{}
    // alwaysVisible 始终可见工具
    alwaysVisible map[string]struct{}
    // maxLoadedTools 最大加载工具数
    maxLoadedTools int
    // metaToolNames 元工具名称集合
    metaToolNames map[string]struct{}
    // ownedToolNames 已注册到 ability_manager 的工具名
    ownedToolNames map[string]struct{}
    // ownedToolIDs 已注册到 resource_mgr 的工具 ID
    ownedToolIDs map[string]struct{}
    // cachedAllTools 全量工具缓存
    cachedAllTools []schema.ToolInfoInterface
}
```

- [x] **Step 2: 实现 NewProgressiveToolRail 构造函数**

```go
// NewProgressiveToolRail 创建渐进式工具权限 Rail。
func NewProgressiveToolRail(config *hschema.DeepAgentConfig) *ProgressiveToolRail {
    r := &ProgressiveToolRail{
        config:         config,
        defaultVisible: toSet(config.ProgressiveToolDefaultVisibleTools),
        alwaysVisible:  toSet(config.ProgressiveToolAlwaysVisibleTools),
        maxLoadedTools: config.EffectiveProgressiveToolMaxLoadedTools(),
        metaToolNames:  make(map[string]struct{}),
        ownedToolNames: make(map[string]struct{}),
        ownedToolIDs:   make(map[string]struct{}),
    }
    r.DeepAgentRail = *NewDeepAgentRail()
    r.DeepAgentRail.WithPriority(90) // 对齐 Python: priority = 90
    return r
}
```

- [x] **Step 3: 实现 Init / Uninit**

`Init(agent RailAgent)` — 注册 SearchToolsTool + LoadToolsTool 到 resource_mgr + ability_manager
`Uninit(agent RailAgent)` — 从 ability_manager 移除元工具

对照 Python L52-111。

- [x] **Step 4: 实现 BeforeInvoke / BeforeModelCall**

`BeforeInvoke(ctx, cbc)` — 缓存全量工具清单 + 初始化 session 可见工具
`BeforeModelCall(ctx, cbc)` — 注入导航节+规则节到 SystemPromptBuilder + 过滤 callable tools

BeforeModelCall 中获取 builder：`cbc.Agent().SystemPromptBuilder()`
对照 Python L114-216。

- [x] **Step 5: 实现 searchTools / loadTools 核心逻辑**

`searchTools(ctx, query, limit, detailLevel)` — 模糊匹配+评分排序
`loadTools(ctx, session, toolNames, replace)` — 加载工具到 session 可见列表

对照 Python L242-361。

- [x] **Step 6: 实现 buildNavigationSection / buildProgressiveToolRulesSection / buildNavigationEntries**

对照 Python L363-439。

- [x] **Step 7: 实现 session state 读写方法**

`getVisibleTools(session)` / `setVisibleTools(session, names)` / `initVisibleTools(session, defaultVisibleTools)` / `appendTrace(session, event)`

对照 Python L495-546。

- [x] **Step 8: 实现辅助静态方法**

`buildToolSummary` / `toolGroupForNavigation` / `toolGroupRank` / `toolGroupToCN` / `toolSummaryForNavigation`

对照 Python L441-616。

- [x] **Step 9: 实现 GetCallbacks**

覆盖 DeepAgentRail.GetCallbacks，合并 BeforeInvoke + BeforeModelCall 等事件。

- [x] **Step 10: 创建 progressive_test.go**

测试用例（按项目规范中文命名）：
- `TestNewProgressiveToolRail` — 验证构造
- `TestProgressiveToolRail_搜索评分` — 验证评分算法（完全匹配+100，包含+40等）
- `TestProgressiveToolRail_工具过滤` — 验证 BeforeModelCall 过滤逻辑（meta+baseline+session_visible 通过，其他被过滤）
- `TestProgressiveToolRail_可见工具管理` — 验证 init/set/get visible tools
- `TestProgressiveToolRail_最大加载数溢出` — 验证 maxLoadedTools 溢出时截断
- `TestProgressiveToolRail_工具分组` — 验证 toolGroupForNavigation / toolGroupRank / toolGroupToCN
- `TestProgressiveToolRail_loadTools_replace模式` — 验证 replace=true 替换 vs replace=false 合并

- [x] **Step 11: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/...`

- [x] **Step 12: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/... -count=1`

- [x] **Step 13: 提交**

```
feat(harness/rails): 实现 ProgressiveToolRail 渐进式工具权限
```

---

## Task 10: deep_agent.go 回填 + 全量集成验证

**Files:**
- Modify: `internal/agentcore/harness/deep_agent.go`
- Modify: `internal/agentcore/harness/deep_agent_test.go`

- [x] **Step 1: 回填 ProgressiveToolRail 创建（L1251-1252）**

将：
```go
// ⤵️ 9.11 回填：ProgressiveToolRail 创建
logger.Debug(logComponent).Msg("ProgressiveToolRail 待创建，⤵️ 9.11 回填")
```

替换为：
```go
d.pendingRails = append(d.pendingRails, rails.NewProgressiveToolRail(config))
logger.Debug(logComponent).Msg("ProgressiveToolRail 已创建，⤴️ 9.11 回填")
```

新增 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"`

- [x] **Step 2: 更新 IMPLEMENTATION_PLAN.md 中 9.11 状态**

将 9.11 行的 `☐` 改为 `✅`。

- [x] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/...`

- [x] **Step 4: 运行 harness 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -count=1 -timeout 180s`

- [x] **Step 5: 运行 rail 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/rail/... -count=1`

- [x] **Step 6: 运行 prompts 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/prompts/... ./internal/agentcore/harness/prompts/... -count=1`

- [x] **Step 7: 提交**

```
feat(deep_agent): 回填 ProgressiveToolRail 创建，9.11 完成
```

---

## Task 11: 全量编译 + 覆盖率检查

- [x] **Step 1: 检查残留 go 进程**

Run: `pgrep -f 'go (build|test)'` — 如有则 kill

- [x] **Step 2: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`

- [x] **Step 3: 运行全量单元测试**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/... -count=1 -timeout 300s`

- [x] **Step 4: 检查覆盖率**

确保 harness/rails、harness/tools/tool_discovery、single_agent/rail、single_agent/prompts 包覆盖率 ≥ 85%

- [x] **Step 5: 提交**

```
chore: 9.11 ProgressiveToolRail 完整实现，全量测试通过
```
