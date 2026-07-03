# 8.35 HierarchicalTeam (msgbus) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现消息总线驱动的层级多 Agent 团队 HierarchicalTeam，包括前置重构（AbilityManager 接口化、CommunicableAgent 移除 AgentID）

**Architecture:** SupervisorAgent 双重嵌入 CommunicableAgent + ReActAgent，通过 P2PAbilityManager 拦截 AgentCard tool_call 转 P2P 派发。HierarchicalTeam 复用 StandaloneInvokeContext 管理 Session 生命周期。

**Tech Stack:** Go 1.22+, sync.WaitGroup 并行模式, channel Semaphore 限流

**设计文档:** `docs/superpowers/specs/2025-07-22-hierarchical-team-msgbus-8.35-design.md`

---

## 文件结构

### 新建文件

| 文件 | 职责 |
|------|------|
| `internal/agentcore/single_agent/schema/execute_result.go` | ExecuteResult + AddAbilityResult 类型定义（从 ability 迁移） |
| `internal/agentcore/multi_agent/teams/hierarchical/doc.go` | 包文档 |
| `internal/agentcore/multi_agent/teams/hierarchical/hierarchical_config.go` | HierarchicalTeamConfig 配置 |
| `internal/agentcore/multi_agent/teams/hierarchical/hierarchical_config_test.go` | 配置测试 |
| `internal/agentcore/multi_agent/teams/hierarchical/p2p_ability_manager.go` | P2PAbilityManager |
| `internal/agentcore/multi_agent/teams/hierarchical/p2p_ability_manager_test.go` | P2PAbilityManager 测试 |
| `internal/agentcore/multi_agent/teams/hierarchical/supervisor_agent.go` | SupervisorAgent |
| `internal/agentcore/multi_agent/teams/hierarchical/supervisor_agent_test.go` | SupervisorAgent 测试 |
| `internal/agentcore/multi_agent/teams/hierarchical/hierarchical_team.go` | HierarchicalTeam 实现 BaseTeam |
| `internal/agentcore/multi_agent/teams/hierarchical/hierarchical_team_test.go` | HierarchicalTeam 测试 |

### 修改文件

| 文件 | 变更摘要 |
|------|---------|
| `internal/agentcore/single_agent/ability/ability_types.go` | 移除 ExecuteResult + AddAbilityResult，改为 type alias 引用 schema 包 |
| `internal/agentcore/single_agent/ability/ability_manager.go` | Execute/Add 返回类型引用改为 schema 包 |
| `internal/agentcore/single_agent/interfaces/interface.go` | 新增 AbilityManagerInterface；BaseAgent.AbilityManager() 返回 AbilityManagerInterface |
| `internal/agentcore/single_agent/agents/react_agent.go` | abilityManager 字段改为 AbilityManagerInterface |
| `internal/agentcore/single_agent/agents/react_helpers.go` | getAbilityManager() 返回 AbilityManagerInterface；新增 SetAbilityManager() |
| `internal/agentcore/single_agent/agents/react_prompt.go` | AbilityManager() 返回 AbilityManagerInterface |
| `internal/agentcore/multi_agent/team_runtime/communicable_agent.go` | 移除 AgentID() 导出方法 |
| `internal/agentcore/controller/controller.go` | abilityMgr 改为 AbilityManagerInterface |
| `internal/agentcore/controller/modules/task_scheduler.go` | abilityMgr 改为 AbilityManagerInterface |
| `internal/agentcore/controller/modules/task_executor.go` | AbilityMgr 改为 AbilityManagerInterface |
| `internal/agentcore/controller/modules/event_handler.go` | AbilityMgr 改为 AbilityManagerInterface |
| `internal/agentcore/multi_agent/teams/handoff/container_agent.go` | 移除类型断言，直接调用接口方法 |
| `internal/agentcore/multi_agent/teams/doc.go` | 添加 hierarchical/ 条目 |
| `IMPLEMENTATION_PLAN.md` | 8.35 ☐ → 🔄 |

---

## Task 1: ExecuteResult + AddAbilityResult 迁移到 schema 包

**Files:**
- Create: `internal/agentcore/single_agent/schema/execute_result.go`
- Modify: `internal/agentcore/single_agent/ability/ability_types.go`
- Test: `internal/agentcore/single_agent/schema/execute_result_test.go`

- [ ] **Step 1: 创建 execute_result.go**

在 `internal/agentcore/single_agent/schema/` 下新建 `execute_result.go`，定义从 ability 包迁移出的类型：

```go
package schema

import llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"

// ──────────────────────────── 结构体 ────────────────────────────

// AddAbilityResult 添加能力的返回结果。
//
// 对应 Python: AddAbilityResult
type AddAbilityResult struct {
	// Name 能力名称
	Name string
	// Added 是否成功添加
	Added bool
	// Reason 未添加的原因（如 "duplicate_tool"、"added_tool"）
	Reason string
}

// ExecuteResult 单个工具调用的执行结果。
type ExecuteResult struct {
	// Result 执行结果。
	Result any
	// ToolMsg 返回给 LLM 的 ToolMessage
	ToolMsg *llmschema.ToolMessage
}
```

- [ ] **Step 2: 修改 ability_types.go 使用 type alias**

将 `ability_types.go` 中的 `ExecuteResult` 和 `AddAbilityResult` 改为 type alias 指向 schema 包，保持 ability 包的 API 兼容：

```go
// AddAbilityResult 添加能力的返回结果。
// 类型别名，实际定义在 schema 包。
type AddAbilityResult = saschema.AddAbilityResult

// ExecuteResult 单个工具调用的执行结果。
// 类型别名，实际定义在 schema 包。
type ExecuteResult = saschema.ExecuteResult
```

同时移除原有的 struct 定义，保留 `AbilityExecutionError`、`InterruptAutoConfirmKey`、`NewAbilityExecutionError`、`BuildToolMessageContent`、`errorToExecuteResult` 等在 ability 包内的定义。

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/...`
Expected: 编译通过

- [ ] **Step 4: 运行现有测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/ability/... ./internal/agentcore/single_agent/schema/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "refactor: 将 ExecuteResult 和 AddAbilityResult 迁移到 schema 包

- 新建 schema/execute_result.go 定义 ExecuteResult + AddAbilityResult
- ability_types.go 改为 type alias 引用 schema 包
- 保持 ability 包 API 兼容"
```

---

## Task 2: 定义 AbilityManagerInterface

**Files:**
- Modify: `internal/agentcore/single_agent/interfaces/interface.go`

- [ ] **Step 1: 在 interface.go 中新增 AbilityManagerInterface**

在 `BaseAgent` 接口定义之前，新增 `AbilityManagerInterface`：

```go
// AbilityManagerInterface 能力管理器接口，Agent 通过此接口注册和调度能力。
//
// 对应 Python: AbilityManager 的公开方法集。
// 具体实现：ability.AbilityManager、P2PAbilityManager。
type AbilityManagerInterface interface {
	// Add 添加单个能力。
	Add(ability schema.Ability) agentschema.AddAbilityResult
	// AddMany 批量添加能力。
	AddMany(abilities []schema.Ability) []agentschema.AddAbilityResult
	// Remove 移除指定名称的能力。
	Remove(name string) schema.Ability
	// RemoveMany 批量移除能力。
	RemoveMany(names []string) []schema.Ability
	// Get 获取指定名称的能力。
	Get(name string) schema.Ability
	// List 列出所有已注册能力。
	List() []schema.Ability
	// ListToolInfo 列出工具信息供 LLM 使用。
	ListToolInfo(ctx context.Context, names []string, mcpServerName ...string) ([]*schema.ToolInfo, error)
	// Execute 执行工具调用。
	Execute(
		ctx context.Context,
		cbc *rail.AgentCallbackContext,
		toolCalls []*llmschema.ToolCall,
		sess sessioninterfaces.SessionFacade,
		tag string,
	) []agentschema.ExecuteResult
	// SetContextEngine 设置上下文引擎。
	SetContextEngine(ce ceinterface.ContextEngine)
	// ReorderTools 重排工具顺序。
	ReorderTools(orderedNames []string)
}
```

需新增 import: `ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"`

- [ ] **Step 2: 修改 BaseAgent.AbilityManager() 返回类型**

将 `AbilityManager() any` 改为 `AbilityManager() AbilityManagerInterface`

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/interfaces/...`
Expected: PASS（仅 interfaces 包自身）

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat: 定义 AbilityManagerInterface 接口

- 在 interfaces 包新增 AbilityManagerInterface
- BaseAgent.AbilityManager() 返回类型从 any 改为 AbilityManagerInterface
- 新增 context_engine/interface 导入"
```

---

## Task 3: ReActAgent 适配 AbilityManagerInterface

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_agent.go`
- Modify: `internal/agentcore/single_agent/agents/react_helpers.go`
- Modify: `internal/agentcore/single_agent/agents/react_prompt.go`

- [ ] **Step 1: 修改 react_agent.go abilityManager 字段类型**

将 `abilityManager *ability.AbilityManager` 改为 `abilityManager interfaces.AbilityManagerInterface`

- [ ] **Step 2: 修改 react_helpers.go**

- `getAbilityManager()` 返回类型从 `*ability.AbilityManager` 改为 `interfaces.AbilityManagerInterface`
- 新增 `SetAbilityManager()` 方法：

```go
// SetAbilityManager 设置能力管理器，允许外部注入自定义实现。
func (a *ReActAgent) SetAbilityManager(am interfaces.AbilityManagerInterface) {
	a.abilityManager = am
}
```

- [ ] **Step 3: 修改 react_prompt.go**

- `AbilityManager()` 返回类型从 `any` 改为 `interfaces.AbilityManagerInterface`
- 方法体不变，直接返回 `a.abilityManager`

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/...`
Expected: PASS

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add -A && git commit -m "refactor: ReActAgent.abilityManager 改为 AbilityManagerInterface

- abilityManager 字段类型改为接口
- 新增 SetAbilityManager() 方法
- AbilityManager() 返回接口类型
- getAbilityManager() 返回接口类型"
```

---

## Task 4: Controller 等模块适配 AbilityManagerInterface

**Files:**
- Modify: `internal/agentcore/controller/controller.go`
- Modify: `internal/agentcore/controller/modules/task_scheduler.go`
- Modify: `internal/agentcore/controller/modules/task_executor.go`
- Modify: `internal/agentcore/controller/modules/event_handler.go`

- [ ] **Step 1: 修改 controller.go**

- `abilityMgr` 字段类型从 `*ability.AbilityManager` 改为 `interfaces.AbilityManagerInterface`
- `Init()` 参数 `abilityMgr` 类型改为 `interfaces.AbilityManagerInterface`
- `AbilityManager()` 返回类型改为 `interfaces.AbilityManagerInterface`
- `SetAbilityManager()` 参数类型改为 `interfaces.AbilityManagerInterface`

- [ ] **Step 2: 修改 modules 各文件**

- `task_scheduler.go`: `abilityMgr` 字段类型改为 `interfaces.AbilityManagerInterface`
- `task_executor.go`: `AbilityMgr` 字段类型改为 `interfaces.AbilityManagerInterface`
- `event_handler.go`: `AbilityMgr` 字段类型改为 `interfaces.AbilityManagerInterface`

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/controller/...`
Expected: PASS

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/controller/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "refactor: Controller 等模块适配 AbilityManagerInterface

- Controller.abilityMgr 改为接口类型
- TaskScheduler/TaskExecutor/EventHandler 同步修改"
```

---

## Task 5: ContainerAgent 适配 AbilityManagerInterface

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent.go`

- [ ] **Step 1: 修改 injectToolsOnce**

将类型断言 `abilityMgrAny.(*ability.AbilityManager)` 替换为直接使用接口：

```go
// 注入到 AbilityManager
abilityMgr := targetAgent.AbilityManager()
if abilityMgr == nil {
    return
}
// 直接调用接口方法，无需类型断言
abilityMgr.Add(handoffTool.Card())
```

移除 `ability` 包的 import（如不再需要）。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/multi_agent/teams/handoff/...`
Expected: PASS

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/multi_agent/teams/handoff/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "refactor: ContainerAgent 移除 AbilityManager 类型断言

- injectToolsOnce 直接调用接口方法
- 无需 abilityMgrAny.(*ability.AbilityManager) 断言"
```

---

## Task 6: CommunicableAgent 移除 AgentID()

**Files:**
- Modify: `internal/agentcore/multi_agent/team_runtime/communicable_agent.go`
- Modify: `internal/agentcore/multi_agent/team_runtime/communicable_agent_test.go`

- [ ] **Step 1: 移除 CommunicableAgent.AgentID() 方法**

从 `communicable_agent.go` 中删除：

```go
// AgentID 返回 Agent 标识。
func (c *CommunicableAgent) AgentID() string {
	return c.agentID
}
```

`agentID` 私有字段保留。

- [ ] **Step 2: 调整测试文件**

修改 `communicable_agent_test.go` 中对 `c.AgentID()` 的调用：
- 移除对 AgentID() 返回值的断言测试
- 或改为测试 IsBound() 等替代方法

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/multi_agent/team_runtime/...`
Expected: PASS

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/multi_agent/team_runtime/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 5: 全量编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add -A && git commit -m "refactor: CommunicableAgent 移除 AgentID() 导出方法

- 解决与 ReActAgent.AgentID() 的双重嵌入方法冲突
- agentID 私有字段保留，通信方法内部使用"
```

---

## Task 7: 全量编译和测试验证（接口化重构完成后）

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: PASS

- [ ] **Step 2: 运行全部测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/... -count=1 -timeout 300s`
Expected: PASS

- [ ] **Step 3: 提交（如有遗漏修复）**

```bash
git add -A && git commit -m "fix: 修复 AbilityManager 接口化重构遗漏"
```

---

## Task 8: HierarchicalTeamConfig 实现

**Files:**
- Create: `internal/agentcore/multi_agent/teams/hierarchical/doc.go`
- Create: `internal/agentcore/multi_agent/teams/hierarchical/hierarchical_config.go`
- Create: `internal/agentcore/multi_agent/teams/hierarchical/hierarchical_config_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package hierarchical 提供层级多 Agent 团队实现（消息总线模式和工具委托模式）。
//
// 消息总线模式（msgbus）下，SupervisorAgent 通过 ReAct 循环推理，
// LLM 返回 tool_call 时通过 P2PAbilityManager 派发给子 Agent 执行。
//
// 文件目录：
//
//	hierarchical/
//	├── doc.go                      # 包文档
//	├── hierarchical_config.go      # HierarchicalTeamConfig 配置定义
//	├── hierarchical_team.go        # HierarchicalTeam 实现 BaseTeam 接口
//	├── p2p_ability_manager.go      # P2PAbilityManager P2P 能力管理器
//	└── supervisor_agent.go         # SupervisorAgent 监督者 Agent
//
// 对应 Python 代码：openjiuwen/core/multi_agent/teams/hierarchical_msgbus/
package hierarchical
```

- [ ] **Step 2: 创建 hierarchical_config.go**

```go
package hierarchical

import (
	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HierarchicalTeamConfig 层级团队（消息总线模式）配置。
//
// 对应 Python: HierarchicalTeamConfig (hierarchical_msgbus/hierarchical_config.py)
type HierarchicalTeamConfig struct {
	// TeamConfig 嵌入基础团队配置
	maschema.TeamConfig
	// SupervisorAgent 监督者 Agent 卡片（必填）
	SupervisorAgent *agentschema.AgentCard
	// Timeout P2P 通信超时秒数，默认 1800.0
	Timeout float64
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultP2PTimeout 默认 P2P 通信超时秒数
	defaultP2PTimeout = 1800.0
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHierarchicalTeamConfig 创建默认 HierarchicalTeamConfig。
func NewHierarchicalTeamConfig() *HierarchicalTeamConfig {
	return &HierarchicalTeamConfig{
		Timeout: defaultP2PTimeout,
	}
}
```

- [ ] **Step 3: 创建 hierarchical_config_test.go**

编写基本测试：`TestNewHierarchicalTeamConfig` 验证默认 timeout 为 1800.0。

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/multi_agent/teams/hierarchical/...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat: 新建 hierarchical 包 + HierarchicalTeamConfig

- 创建 doc.go 包文档
- 创建 HierarchicalTeamConfig 配置定义
- 默认 P2P timeout 1800.0"
```

---

## Task 9: P2PAbilityManager 实现

**Files:**
- Create: `internal/agentcore/multi_agent/teams/hierarchical/p2p_ability_manager.go`
- Create: `internal/agentcore/multi_agent/teams/hierarchical/p2p_ability_manager_test.go`

- [ ] **Step 1: 创建 p2p_ability_manager.go**

核心结构体和 Execute() 覆写方法，对齐 AbilityManager 并行模式（sync.WaitGroup + 预分配 results slice + channel Semaphore 限流）。

关键实现点：
- 嵌入 `ability.AbilityManager`
- 持有 `*SupervisorAgent`、`maxParallel`、`timeout`
- `sem chan struct{}` + `semOnce sync.Once` 懒初始化
- `Execute()` 分区 agent_calls vs other_calls → 并行执行 → 结果按原序重组
- `executeSingleP2P()` 通过 `m.supervisor.Send()` 做 P2P 派发

- [ ] **Step 2: 创建 p2p_ability_manager_test.go**

编写测试用例：
- `TestP2PAbilityManager_Execute_无Agent调用` — fast path 委托基类
- `TestP2PAbilityManager_Execute_纯Agent调用` — mock supervisor.Send 全部走 P2P
- `TestP2PAbilityManager_Execute_混合调用` — Agent + 普通工具并行
- `TestP2PAbilityManager_Execute_并行限流` — maxParallel=2 时验证限流
- `TestP2PAbilityManager_Execute_异常处理` — Send 失败返回 error ToolMessage
- `TestP2PAbilityManager_满足AbilityManagerInterface` — 编译时接口检查

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/multi_agent/teams/hierarchical/...`
Expected: PASS

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/multi_agent/teams/hierarchical/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat: 实现 P2PAbilityManager

- 嵌入 AbilityManager + 覆写 Execute()
- 分区 agent_calls vs other_calls 并行派发
- channel Semaphore 限流
- supervisor.Send() 做 P2P 消息派发"
```

---

## Task 10: SupervisorAgent 实现

**Files:**
- Create: `internal/agentcore/multi_agent/teams/hierarchical/supervisor_agent.go`
- Create: `internal/agentcore/multi_agent/teams/hierarchical/supervisor_agent_test.go`

- [ ] **Step 1: 创建 supervisor_agent.go**

核心结构体双重嵌入 `CommunicableAgent + ReActAgent`，关键方法：
- `NewSupervisorAgent()` 构造函数（创建 ReActAgent → 创建 P2PAbilityManager → SetAbilityManager 注入 → 组装）
- `Create()` 工厂方法（返回 AgentCard + AgentProvider，懒构造闭包）
- `RegisterSubAgentCard()` 子 Agent 注册
- 编译时接口检查：`var _ BaseAgent = (*SupervisorAgent)(nil)`、`var _ Communicable = (*SupervisorAgent)(nil)`、`var _ RuntimeBindable = (*SupervisorAgent)(nil)`

注意：NewSupervisorAgent 构造 P2PAbilityManager 需要 supervisor 引用，但 supervisor 尚未构造完成。解决方案：先构造 SupervisorAgent 壳（ReActAgent 已创建），然后创建 P2PAbilityManager（传入 supervisor 指针），最后 SetAbilityManager 注入。

- [ ] **Step 2: 创建 supervisor_agent_test.go**

编写测试用例：
- `TestNewSupervisorAgent` — 构造验证，确认 P2PAbilityManager 已注入
- `TestSupervisorAgent_Create_空Agents报错` — agents 列表为空时 panic/error
- `TestSupervisorAgent_RegisterSubAgentCard` — 子 Agent 注册到 P2PAbilityManager
- `TestSupervisorAgent_满足BaseAgent接口` — 编译时接口检查
- `TestSupervisorAgent_满足Communicable接口` — 编译时接口检查
- `TestSupervisorAgent_满足RuntimeBindable接口` — 编译时接口检查

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/multi_agent/teams/hierarchical/...`
Expected: PASS

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/multi_agent/teams/hierarchical/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat: 实现 SupervisorAgent

- 双重嵌入 CommunicableAgent + ReActAgent
- 构造时注入 P2PAbilityManager
- Create() 工厂方法返回 (AgentCard, AgentProvider)
- RegisterSubAgentCard() 子 Agent 注册
- 编译时接口检查 BaseAgent/Communicable/RuntimeBindable"
```

---

## Task 11: HierarchicalTeam 实现

**Files:**
- Create: `internal/agentcore/multi_agent/teams/hierarchical/hierarchical_team.go`
- Create: `internal/agentcore/multi_agent/teams/hierarchical/hierarchical_team_test.go`

- [ ] **Step 1: 创建 hierarchical_team.go**

实现 BaseTeam 接口的全部 13 个方法，核心：
- `NewHierarchicalTeam()` 构造函数
- `Invoke()`: assertReady → StandaloneInvokeContext → runtime.Send(supervisorID)
- `Stream()`: assertReady → StandaloneStreamContext → runtime.Send → WriteStream
- `AddAgent()`: runtime.RegisterAgent + 识别 supervisor 设置 P2PTimeout
- 其余方法委托 runtime（与 HandoffTeam 一致）
- `assertReady()`: 校验 supervisorID 非空且 runtime.HasAgent(supervisorID)
- 编译时接口检查：`var _ BaseTeam = (*HierarchicalTeam)(nil)`

- [ ] **Step 2: 创建 hierarchical_team_test.go**

编写测试用例：
- `TestNewHierarchicalTeam` — 构造验证
- `TestHierarchicalTeam_Invoke_Supervisor未注册` — assertReady 报错
- `TestHierarchicalTeam_AddAgent` — Agent 注册
- `TestHierarchicalTeam_AddAgent_Supervisor设置Timeout` — supervisor 注册时 SetP2PTimeout
- `TestHierarchicalTeam_RemoveAgent` — Agent 注销
- `TestHierarchicalTeam_满足BaseTeam接口` — 编译时接口检查
- `TestHierarchicalTeam_Send` — 委托 runtime
- `TestHierarchicalTeam_Publish` — 委托 runtime

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/multi_agent/teams/hierarchical/...`
Expected: PASS

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/multi_agent/teams/hierarchical/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat: 实现 HierarchicalTeam (msgbus)

- 实现 BaseTeam 接口全部 13 个方法
- Invoke/Stream 复用 StandaloneInvokeContext/StandaloneStreamContext
- AddAgent 识别 supervisor 并设置 P2PTimeout
- assertReady 校验 supervisor 已注册"
```

---

## Task 12: 回填和文档更新

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 teams/doc.go 文件目录**

添加 hierarchical/ 子目录条目：

```
//	├── hierarchical/     # HierarchicalTeam 层级团队（msgbus + tools 模式）
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 8.35 状态**

将 `8.35 | ☐ | HierarchicalTeam (msgbus)` 改为 `8.35 | 🔄 | HierarchicalTeam (msgbus)`

- [ ] **Step 3: 提交**

```bash
git add -A && git commit -m "docs: 回填 teams/doc.go 和 IMPLEMENTATION_PLAN.md

- teams/doc.go 添加 hierarchical/ 条目
- IMPLEMENTATION_PLAN.md 8.35 ☐ → 🔄"
```

---

## Task 13: 全量编译和测试

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: PASS

- [ ] **Step 2: 运行全部测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./... -count=1 -timeout 600s`
Expected: PASS

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/multi_agent/teams/hierarchical/...`
Expected: 各文件覆盖率 ≥ 85%

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md 8.35 为完成**

将 `8.35 | 🔄` 改为 `8.35 | ✅`

- [ ] **Step 5: 最终提交**

```bash
git add -A && git commit -m "feat: 完成 8.35 HierarchicalTeam (msgbus) 实现

- 全量编译和测试通过
- IMPLEMENTATION_PLAN.md 8.35 🔄 → ✅"
```
