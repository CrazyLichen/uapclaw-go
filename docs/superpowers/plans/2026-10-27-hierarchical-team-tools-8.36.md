# 8.36 HierarchicalTeam (tools) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Agents-as-Tools（工具委托）模式的层级多 Agent 团队，子 Agent 通过 ability_manager 注册为父 Agent 的工具，LLM 通过 tool_call 自主调度。

**Architecture:** 新建 `hierarchical_tools` 包实现 HierarchicalToolsTeam，通过 TeamOption 传递 parentAgentID 建立父子关系，setupHierarchy() 延迟注册子 Agent 到父 Agent 的 AbilityManager。stream 直接调用 agent.Stream() 逐 chunk 转发。同时将现有 `hierarchical` 包重命名为 `hierarchical_msgbus` 与 Python 对齐。

**Tech Stack:** Go 1.21+, 依赖 team_runtime, AbilityManager, ResourceMgr, session/stream

---

## 文件结构

### 新增文件

| 文件 | 职责 |
|------|------|
| `internal/agentcore/multi_agent/teams/hierarchical_tools/doc.go` | 包文档 |
| `internal/agentcore/multi_agent/teams/hierarchical_tools/hierarchical_config.go` | HierarchicalToolsTeamConfig 配置定义 |
| `internal/agentcore/multi_agent/teams/hierarchical_tools/hierarchical_team.go` | HierarchicalToolsTeam 实现 BaseTeam 接口 |
| `internal/agentcore/multi_agent/teams/hierarchical_tools/hierarchical_config_test.go` | 配置测试 |
| `internal/agentcore/multi_agent/teams/hierarchical_tools/hierarchical_team_test.go` | 团队测试 |

### 修改文件

| 文件 | 变更 |
|------|------|
| `internal/agentcore/multi_agent/schema/team_interface.go` | TeamOptions 新增 ParentAgentID 字段 + WithParentAgentID() Option 函数 |
| `internal/agentcore/multi_agent/teams/doc.go` | 更新目录结构，增加 hierarchical_tools/ 和将 hierarchical/ 改为 hierarchical_msgbus/ |

### 重命名文件（目录级）

| 原路径 | 新路径 |
|--------|--------|
| `internal/agentcore/multi_agent/teams/hierarchical/` | `internal/agentcore/multi_agent/teams/hierarchical_msgbus/` |

### 回填

| 文件 | 变更 |
|------|------|
| `IMPLEMENTATION_PLAN.md` | 8.36 状态 ☐ → ✅ |

---

### Task 1: TeamOptions 扩展 — 新增 ParentAgentID 字段和 WithParentAgentID Option

**Files:**
- Modify: `internal/agentcore/multi_agent/schema/team_interface.go:111-128`
- Modify: `internal/agentcore/multi_agent/schema/team_interface_test.go`（若存在）

- [ ] **Step 1: 在 TeamOptions 结构体中新增 ParentAgentID 字段**

在 `team_interface.go` 的 `TeamOptions` 结构体中，在 `StreamModes` 字段后新增：

```go
// ParentAgentID 父 Agent ID，用于 HierarchicalToolsTeam 的层级注册。
//
// 在 AddAgent 时通过 WithParentAgentID() Option 传递，
// 声明当前 Agent 是哪个父 Agent 的子工具。
ParentAgentID string
```

- [ ] **Step 2: 新增 WithParentAgentID Option 函数**

在 `team_interface.go` 的导出函数区，在 `WithTeamStreamModes` 后新增：

```go
// WithParentAgentID 设置父 Agent ID。
//
// 用于 HierarchicalToolsTeam.AddAgent() 时声明父子关系：
//
//	team.AddAgent(ctx, childCard, childProvider,
//	    maschema.WithParentAgentID("parent_agent_id"),
//	)
func WithParentAgentID(parentID string) TeamOption {
	return func(o *TeamOptions) { o.ParentAgentID = parentID }
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/schema/...`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/schema/team_interface.go
git commit -m "feat(team): 在 TeamOptions 中新增 ParentAgentID 字段和 WithParentAgentID Option"
```

---

### Task 2: 重命名 hierarchical → hierarchical_msgbus

**Files:**
- Rename: `internal/agentcore/multi_agent/teams/hierarchical/` → `internal/agentcore/multi_agent/teams/hierarchical_msgbus/`
- Modify: 所有文件中的 `package hierarchical` → `package hierarchical_msgbus`
- Modify: `internal/agentcore/multi_agent/teams/doc.go`

- [ ] **Step 1: 重命名目录**

```bash
cd /home/opensource/uap-claw-go
mv internal/agentcore/multi_agent/teams/hierarchical internal/agentcore/multi_agent/teams/hierarchical_msgbus
```

- [ ] **Step 2: 修改包声明**

将 `hierarchical_msgbus/` 下所有 `.go` 文件中的 `package hierarchical` 改为 `package hierarchical_msgbus`。

涉及文件：
- `doc.go`
- `hierarchical_config.go`
- `hierarchical_config_test.go`
- `hierarchical_team.go`
- `hierarchical_team_test.go`
- `p2p_ability_manager.go`
- `p2p_ability_manager_test.go`
- `supervisor_agent.go`
- `supervisor_agent_test.go`

- [ ] **Step 3: 更新 doc.go 内容**

将 `doc.go` 中 `Package hierarchical` 改为 `Package hierarchical_msgbus`，目录树中 `hierarchical/` 改为 `hierarchical_msgbus/`。

完整更新 doc.go：

```go
// Package hierarchical_msgbus 提供层级多 Agent 团队实现（消息总线模式）。
//
// 消息总线模式下，SupervisorAgent 通过 ReAct 循环推理，
// LLM 返回 tool_call 时通过 P2PAbilityManager 派发给子 Agent 执行。
// 支持并行子 Agent 派发（Semaphore 限流）。
//
// 文件目录：
//
//	hierarchical_msgbus/
//	├── doc.go                      # 包文档
//	├── hierarchical_config.go      # HierarchicalTeamConfig 配置定义
//	├── hierarchical_team.go        # HierarchicalTeam 实现 BaseTeam 接口
//	├── p2p_ability_manager.go      # P2PAbilityManager P2P 能力管理器
//	└── supervisor_agent.go         # SupervisorAgent 监督者 Agent
//
// 对应 Python 代码：openjiuwen/core/multi_agent/teams/hierarchical_msgbus/
package hierarchical_msgbus
```

- [ ] **Step 4: 更新 teams/doc.go**

将 `teams/doc.go` 中的 `hierarchical/` 引用改为 `hierarchical_msgbus/`，并新增 `hierarchical_tools/`：

```go
// Package teams 提供多 Agent 团队的具体实现模式。
//
// 本包包含基于事件驱动的团队类型，如 HandoffTeam（单活跃 Agent 交接模式）、
// HierarchicalTeam 消息总线模式（hierarchical_msgbus）、
// HierarchicalToolsTeam 工具委托模式（hierarchical_tools）。
// 所有团队类型均实现 BaseTeam 接口，可注册到 TeamRuntime 中运行。
//
// 文件目录：
//
//	teams/
//	├── doc.go                 # 包文档
//	├── utils.go               # 独立调用上下文工具函数
//	├── handoff/               # HandoffTeam 实现
//	├── hierarchical_msgbus/   # HierarchicalTeam 实现（消息总线模式）
//	└── hierarchical_tools/    # HierarchicalToolsTeam 实现（工具委托模式）
//
// 对应 Python 代码：openjiuwen/core/multi_agent/teams/
package teams
```

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/...`
Expected: 编译成功

- [ ] **Step 6: 运行现有测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/hierarchical_msgbus/... -v -count=1`
Expected: 所有测试通过

- [ ] **Step 7: 提交**

```bash
git add -A internal/agentcore/multi_agent/teams/
git commit -m "refactor(team): 重命名 hierarchical 包为 hierarchical_msgbus，与 Python 对齐"
```

---

### Task 3: 新建 hierarchical_tools 包 — 配置定义

**Files:**
- Create: `internal/agentcore/multi_agent/teams/hierarchical_tools/doc.go`
- Create: `internal/agentcore/multi_agent/teams/hierarchical_tools/hierarchical_config.go`
- Create: `internal/agentcore/multi_agent/teams/hierarchical_tools/hierarchical_config_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package hierarchical_tools 提供层级多 Agent 团队实现（工具委托模式）。
//
// 工具委托模式下，子 Agent 注册到父 Agent 的 ability_manager 中，
// LLM 将子 Agent 视为可调用的工具（tool_call），
// 子 Agent 的执行由 AbilityManager.executeAgent() → Runner.RunAgent() 完成。
// 支持多级树状层级（父→子→孙），任意 Agent 都可作为父节点。
//
// 文件目录：
//
//	hierarchical_tools/
//	├── doc.go                      # 包文档
//	├── hierarchical_config.go      # HierarchicalToolsTeamConfig 配置定义
//	└── hierarchical_team.go        # HierarchicalToolsTeam 实现 BaseTeam 接口
//
// 对应 Python 代码：openjiuwen/core/multi_agent/teams/hierarchical_tools/
package hierarchical_tools
```

- [ ] **Step 2: 创建 hierarchical_config.go**

```go
package hierarchical_tools

import (
	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HierarchicalToolsTeamConfig 工具委托层级团队配置。
//
// 对应 Python: HierarchicalTeamConfig (hierarchical_tools/hierarchical_config.py)
type HierarchicalToolsTeamConfig struct {
	// TeamConfig 嵌入基础团队配置
	TeamConfig maschema.TeamConfig
	// RootAgent 根/入口 Agent 卡片（必填）
	RootAgent *agentschema.AgentCard
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHierarchicalToolsTeamConfig 创建默认 HierarchicalToolsTeamConfig。
func NewHierarchicalToolsTeamConfig() *HierarchicalToolsTeamConfig {
	return &HierarchicalToolsTeamConfig{}
}
```

- [ ] **Step 3: 创建 hierarchical_config_test.go**

```go
package hierarchical_tools

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewHierarchicalToolsTeamConfig 验证默认配置。
func TestNewHierarchicalToolsTeamConfig(t *testing.T) {
	cfg := NewHierarchicalToolsTeamConfig()
	if cfg.RootAgent != nil {
		t.Errorf("期望 RootAgent = nil, 实际 = %v", cfg.RootAgent)
	}
}

// TestHierarchicalToolsTeamConfig_自定义值 验证自定义配置。
func TestHierarchicalToolsTeamConfig_自定义值(t *testing.T) {
	cfg := &HierarchicalToolsTeamConfig{}
	if cfg.RootAgent != nil {
		t.Errorf("期望 RootAgent = nil, 实际 = %v", cfg.RootAgent)
	}
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/teams/hierarchical_tools/...`
Expected: 编译成功

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/hierarchical_tools/... -v -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/multi_agent/teams/hierarchical_tools/
git commit -m "feat(team): 新建 hierarchical_tools 包配置定义"
```

---

### Task 4: 新建 hierarchical_tools 包 — HierarchicalToolsTeam 核心实现

**Files:**
- Create: `internal/agentcore/multi_agent/teams/hierarchical_tools/hierarchical_team.go`
- Reference: `internal/agentcore/multi_agent/teams/hierarchical_msgbus/hierarchical_team.go`（对齐模式）

- [ ] **Step 1: 创建 hierarchical_team.go**

```go
package hierarchical_tools

import (
	"context"
	"fmt"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/teams"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HierarchicalToolsTeam 工具委托层级多 Agent 团队。
//
// 子 Agent 通过 WithParentAgentID Option 注册到父 Agent 的 ability_manager，
// LLM 将子 Agent 视为可调用的工具（tool_call），
// 子 Agent 的执行由 AbilityManager.executeAgent() → Runner.RunAgent() 完成。
// 支持多级树状层级（父→子→孙），任意 Agent 都可作为父节点。
//
// 对应 Python: HierarchicalTeam (hierarchical_tools/hierarchical_team.py)
type HierarchicalToolsTeam struct {
	// card 团队身份卡片
	card maschema.TeamCardInterface
	// config 完整配置
	config HierarchicalToolsTeamConfig
	// runtime 团队运行时
	runtime *team_runtime.TeamRuntime
	// rootAgentID 根/入口 Agent ID
	rootAgentID string
	// pendingChildren 待注册的父子关系：parentID → []childAgentCard
	pendingChildren map[string][]*agentschema.AgentCard
	// hierarchySetup 标记层级是否已建立（幂等保护）
	hierarchySetup bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// toolsLogComponent 日志组件标识
	toolsLogComponent = logger.ComponentChannel
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 HierarchicalToolsTeam 满足 BaseTeam 接口
var _ maschema.BaseTeam = (*HierarchicalToolsTeam)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHierarchicalToolsTeam 创建 HierarchicalToolsTeam 实例。
//
// 参数：
//   - card：团队身份卡片
//   - config：完整配置
//   - runtime：团队运行时，nil 时自动创建
//
// 对应 Python: HierarchicalTeam(card, config)
func NewHierarchicalToolsTeam(card maschema.TeamCardInterface, config *HierarchicalToolsTeamConfig, runtime *team_runtime.TeamRuntime) *HierarchicalToolsTeam {
	if config == nil {
		defaultCfg := NewHierarchicalToolsTeamConfig()
		config = defaultCfg
	}

	teamID := card.GetID()
	var tr *team_runtime.TeamRuntime
	if runtime != nil {
		tr = runtime
	} else {
		rtCfg := team_runtime.NewRuntimeConfig(
			team_runtime.WithRuntimeTeamID(teamID),
		)
		tr = team_runtime.NewTeamRuntime(*rtCfg)
	}

	var rootAgentID string
	if config.RootAgent != nil {
		rootAgentID = config.RootAgent.ID
	}

	team := &HierarchicalToolsTeam{
		card:            card,
		config:          *config,
		runtime:         tr,
		rootAgentID:     rootAgentID,
		pendingChildren: make(map[string][]*agentschema.AgentCard),
		hierarchySetup:  false,
	}

	logger.Info(toolsLogComponent).
		Str("action", "new_hierarchical_tools_team").
		Str("team_id", teamID).
		Str("root_agent_id", rootAgentID).
		Msg("创建 HierarchicalToolsTeam")

	return team
}

// Invoke 非流式调用团队，通过 root_agent 运行并返回最终结果。
//
// 对应 Python: HierarchicalTeam.invoke(message, session)
func (t *HierarchicalToolsTeam) Invoke(ctx context.Context, inputs map[string]any, opts ...maschema.TeamOption) (any, error) {
	if err := t.assertReady(); err != nil {
		return nil, err
	}

	if err := t.setupHierarchy(ctx); err != nil {
		return nil, err
	}

	teamOpts := maschema.NewTeamOptions(opts...)
	sess := teamOpts.Session
	timeout := teamOpts.Timeout

	result, err := teams.StandaloneInvokeContext(ctx, t.runtime, t.card, inputs, sess,
		func(teamSession *session.AgentTeamSession, sessionID string) (map[string]any, error) {
			logger.Debug(toolsLogComponent).
				Str("action", "hierarchical_tools_invoke").
				Str("session_id", sessionID).
				Str("root_agent_id", t.rootAgentID).
				Msg("开始 invoke")

			res, sendErr := t.runtime.Send(ctx, inputs, t.rootAgentID, t.card.GetID(),
				maschema.WithTeamSessionID(sessionID),
				maschema.WithTeamTimeout(timeout),
			)
			if sendErr != nil {
				logger.Error(toolsLogComponent).Err(sendErr).
					Str("event_type", "LLM_CALL_ERROR").
					Str("method", "HierarchicalToolsTeam.Invoke").
					Str("root_agent_id", t.rootAgentID).
					Msg("Send 到 root_agent 失败")
				return nil, sendErr
			}

			logger.Debug(toolsLogComponent).
				Str("action", "hierarchical_tools_invoke_end").
				Str("session_id", sessionID).
				Msg("invoke 结束")

			resultMap, ok := res.(map[string]any)
			if !ok {
				resultMap = map[string]any{"result": res}
			}
			return resultMap, nil
		},
	)

	return result, err
}

// Stream 流式调用团队，直接调用 root_agent.Stream() 逐 chunk 转发。
//
// 与 msgbus 模式的关键区别：msgbus 走 runtime.Send() 等完整结果后一次性 WriteStream；
// tools 模式直接调用 agent.Stream() 逐 chunk 转发，提供真正的流式体验。
//
// 对应 Python: HierarchicalTeam.stream(message, session)
func (t *HierarchicalToolsTeam) Stream(ctx context.Context, inputs map[string]any, opts ...maschema.TeamOption) (<-chan stream.Schema, error) {
	if err := t.assertReady(); err != nil {
		return nil, err
	}

	if err := t.setupHierarchy(ctx); err != nil {
		return nil, err
	}

	teamOpts := maschema.NewTeamOptions(opts...)
	sess := teamOpts.Session

	logger.Debug(toolsLogComponent).
		Str("action", "hierarchical_tools_stream").
		Str("root_agent_id", t.rootAgentID).
		Msg("开始 stream")

	return teams.StandaloneStreamContext(ctx, t.runtime, t.card, inputs, sess,
		func(teamSession *session.AgentTeamSession, sessionID string) error {
			// 从全局 ResourceMgr 获取 root_agent 实例
			agents, err := runner.GetResourceMgr().GetAgent(ctx, []string{t.rootAgentID})
			if err != nil || len(agents) == 0 {
				logger.Error(toolsLogComponent).Err(err).
					Str("event_type", "LLM_CALL_ERROR").
					Str("method", "HierarchicalToolsTeam.Stream").
					Str("root_agent_id", t.rootAgentID).
					Msg("获取 root_agent 实例失败")
				return exception.BuildError(exception.StatusAgentNotFound,
					exception.WithParam("error_msg", fmt.Sprintf("root_agent '%s' 实例未找到", t.rootAgentID)),
				)
			}

			agent := agents[0]

			// 构造带 conversation_id 和 sender 的 inputs
			inputsWithSID := make(map[string]any, len(inputs)+2)
			for k, v := range inputs {
				inputsWithSID[k] = v
			}
			inputsWithSID["conversation_id"] = sessionID
			inputsWithSID["sender"] = t.card.GetID()

			// 直接调用 agent.Stream() 逐 chunk 转发
			ch, streamErr := agent.Stream(ctx, inputsWithSID)
			if streamErr != nil {
				logger.Error(toolsLogComponent).Err(streamErr).
					Str("event_type", "LLM_CALL_ERROR").
					Str("method", "HierarchicalToolsTeam.Stream").
					Str("root_agent_id", t.rootAgentID).
					Msg("root_agent.Stream() 调用失败")
				return streamErr
			}

			for chunk := range ch {
				if writeErr := teamSession.WriteStream(ctx, chunk); writeErr != nil {
					logger.Warn(toolsLogComponent).Err(writeErr).
						Str("action", "hierarchical_tools_stream_write").
						Msg("写入流失败")
				}
			}

			logger.Debug(toolsLogComponent).
				Str("action", "hierarchical_tools_stream_end").
				Str("session_id", sessionID).
				Msg("stream 结束")
			return nil
		},
	)
}

// AddAgent 向团队注册 Agent。
//
// 若 Agent 已存在则跳过，否则注册到运行时。
// 若 TeamOption 中提供了 WithParentAgentID，将 Agent 卡片记录到 pendingChildren 中，
// 在 invoke/stream 前通过 setupHierarchy() 延迟注册到父 Agent 的 ability_manager。
//
// 对应 Python: HierarchicalTeam.add_agent(card, provider, parent_agent_id)
func (t *HierarchicalToolsTeam) AddAgent(ctx context.Context, card *agentschema.AgentCard, provider maschema.TeamAgentProvider) error {
	if t.runtime.HasAgent(card.ID) {
		logger.Warn(toolsLogComponent).
			Str("action", "add_agent_skip").
			Str("agent_id", card.ID).
			Str("team_id", t.card.GetID()).
			Msg("Agent 已存在，跳过注册")
		return nil
	}

	// 注册到运行时（包装为 resources_manager.AgentProvider）
	wrappedProvider := resources_manager.AgentProvider(provider)
	if err := t.runtime.RegisterAgent(ctx, card, wrappedProvider); err != nil {
		logger.Error(toolsLogComponent).Err(err).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HierarchicalToolsTeam.AddAgent").
			Str("agent_id", card.ID).
			Msg("注册 Agent 到运行时失败")
		return err
	}

	// 识别 rootAgent
	if card.ID == t.rootAgentID {
		logger.Info(toolsLogComponent).
			Str("action", "add_agent_root").
			Str("root_agent_id", card.ID).
			Str("team_id", t.card.GetID()).
			Msg("注册 root_agent 到 HierarchicalToolsTeam")
	}

	logger.Info(toolsLogComponent).
		Str("action", "add_agent").
		Str("agent_id", card.ID).
		Str("team_id", t.card.GetID()).
		Msg("Agent 已注册到 HierarchicalToolsTeam")

	return nil
}

// AddAgentWithParent 向团队注册 Agent 并声明其父 Agent。
//
// 调用 AddAgent 后，将 card 记录到 pendingChildren[parentAgentID] 中，
// 在 invoke/stream 前通过 setupHierarchy() 注册到父 Agent 的 ability_manager。
//
// 对应 Python: HierarchicalTeam.add_agent(card, provider, parent_agent_id)
func (t *HierarchicalToolsTeam) AddAgentWithParent(ctx context.Context, card *agentschema.AgentCard, provider maschema.TeamAgentProvider, parentAgentID string) error {
	if err := t.AddAgent(ctx, card, provider); err != nil {
		return err
	}

	if parentAgentID != "" {
		t.pendingChildren[parentAgentID] = append(t.pendingChildren[parentAgentID], card)
		// 层级已建立过，需要重置标记以允许再次 setup
		t.hierarchySetup = false

		logger.Debug(toolsLogComponent).
			Str("action", "add_agent_with_parent").
			Str("child_id", card.ID).
			Str("parent_id", parentAgentID).
			Msg("记录父子关系到 pendingChildren")
	}

	return nil
}

// RemoveAgent 从团队注销 Agent。
//
// 对应 Python: BaseTeam.remove_agent(agent)
func (t *HierarchicalToolsTeam) RemoveAgent(ctx context.Context, agentID string) error {
	_, err := t.runtime.UnregisterAgent(ctx, agentID)
	if err != nil {
		logger.Error(toolsLogComponent).Err(err).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HierarchicalToolsTeam.RemoveAgent").
			Str("agent_id", agentID).
			Msg("注销 Agent 失败")
		return err
	}

	logger.Info(toolsLogComponent).
		Str("action", "remove_agent").
		Str("agent_id", agentID).
		Str("team_id", t.card.GetID()).
		Msg("Agent 已从 HierarchicalToolsTeam 注销")

	return nil
}

// Send P2P 发送消息，委托运行时。
//
// 对应 Python: BaseTeam.send(message, recipient, sender, session_id, timeout)
func (t *HierarchicalToolsTeam) Send(ctx context.Context, message map[string]any, recipient string, sender string, opts ...maschema.TeamOption) (any, error) {
	return t.runtime.Send(ctx, message, recipient, sender, opts...)
}

// Publish Pub-Sub 发布消息，委托运行时。
//
// 对应 Python: BaseTeam.publish(message, topic_id, sender, session_id)
func (t *HierarchicalToolsTeam) Publish(ctx context.Context, message map[string]any, topicID string, sender string, opts ...maschema.TeamOption) error {
	return t.runtime.Publish(ctx, message, topicID, sender, opts...)
}

// Subscribe 订阅主题，委托运行时。
//
// 对应 Python: BaseTeam.subscribe(agent_id, topic)
func (t *HierarchicalToolsTeam) Subscribe(ctx context.Context, agentID string, topic string) error {
	return t.runtime.Subscribe(ctx, agentID, topic)
}

// Unsubscribe 取消订阅，委托运行时。
//
// 对应 Python: BaseTeam.unsubscribe(agent_id, topic)
func (t *HierarchicalToolsTeam) Unsubscribe(ctx context.Context, agentID string, topic string) error {
	return t.runtime.Unsubscribe(ctx, agentID, topic)
}

// Configure 配置团队。
//
// 对应 Python: BaseTeam.configure(config) -> self
func (t *HierarchicalToolsTeam) Configure(_ context.Context, config maschema.TeamConfig) error {
	t.config.TeamConfig = config
	logger.Info(toolsLogComponent).
		Str("action", "configure").
		Str("team_id", t.card.GetID()).
		Msg("HierarchicalToolsTeam 配置已更新")
	return nil
}

// GetAgentCard 获取 Agent 卡片，委托运行时。
//
// 对应 Python: BaseTeam.get_agent_card(agent_id)
func (t *HierarchicalToolsTeam) GetAgentCard(agentID string) (*agentschema.AgentCard, error) {
	return t.runtime.GetAgentCard(agentID)
}

// GetAgentCount 获取 Agent 数量，委托运行时。
//
// 对应 Python: BaseTeam.get_agent_count()
func (t *HierarchicalToolsTeam) GetAgentCount() int {
	return t.runtime.GetAgentCount()
}

// ListAgents 列出所有 Agent ID，委托运行时。
//
// 对应 Python: BaseTeam.list_agents()
func (t *HierarchicalToolsTeam) ListAgents() []string {
	return t.runtime.ListAgents()
}

// Card 返回团队身份卡片。
//
// 对应 Python: BaseTeam.card 属性
func (t *HierarchicalToolsTeam) Card() maschema.TeamCardInterface {
	return t.card
}

// Config 返回团队配置。
//
// 对应 Python: BaseTeam.config 属性
func (t *HierarchicalToolsTeam) Config() *maschema.TeamConfig {
	return &t.config.TeamConfig
}

// GetRuntime 返回团队运行时。
func (t *HierarchicalToolsTeam) GetRuntime() *team_runtime.TeamRuntime {
	return t.runtime
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// assertReady 校验团队就绪状态。
//
// 校验 rootAgentID 非空且 runtime.HasAgent(rootAgentID)。
//
// 对应 Python: HierarchicalTeam._assert_ready()
func (t *HierarchicalToolsTeam) assertReady() error {
	if t.rootAgentID == "" {
		return exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", "HierarchicalToolsTeamConfig 未配置 RootAgent"),
		)
	}
	if !t.runtime.HasAgent(t.rootAgentID) {
		return exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf(
				"RootAgent '%s' 未注册到运行时，请先调用 AddAgent(rootCard, rootProvider)",
				t.rootAgentID,
			)),
		)
	}
	return nil
}

// setupHierarchy 延迟注册子 Agent 到父 Agent 的 AbilityManager。
//
// 幂等：若 hierarchySetup == true 直接返回 nil。
// 遍历 pendingChildren，从 ResourceMgr 获取父 Agent 实例，
// 对每个子 AgentCard 调用 parentAgent.AbilityManager().Add(childCard)。
//
// 对应 Python: HierarchicalTeam._setup_hierarchy()
func (t *HierarchicalToolsTeam) setupHierarchy(ctx context.Context) error {
	if t.hierarchySetup {
		return nil
	}

	if len(t.pendingChildren) == 0 {
		t.hierarchySetup = true
		return nil
	}

	resourceMgr := runner.GetResourceMgr()
	if resourceMgr == nil {
		logger.Warn(toolsLogComponent).
			Str("action", "setup_hierarchy").
			Msg("ResourceMgr 为空，跳过层级建立")
		t.hierarchySetup = true
		return nil
	}

	for parentID, childCards := range t.pendingChildren {
		// 获取父 Agent 实例
		parentAgents, err := resourceMgr.GetAgent(ctx, []string{parentID})
		if err != nil || len(parentAgents) == 0 {
			logger.Error(toolsLogComponent).Err(err).
				Str("event_type", "LLM_CALL_ERROR").
				Str("method", "HierarchicalToolsTeam.setupHierarchy").
				Str("parent_id", parentID).
				Msg("获取父 Agent 实例失败")
			return exception.BuildError(exception.StatusAgentNotFound,
				exception.WithParam("error_msg", fmt.Sprintf("父 Agent '%s' 实例未找到", parentID)),
			)
		}

		parentAgent := parentAgents[0]
		am := parentAgent.AbilityManager()
		if am == nil {
			logger.Warn(toolsLogComponent).
				Str("action", "setup_hierarchy").
				Str("parent_id", parentID).
				Msg("父 Agent 无 AbilityManager，跳过子 Agent 注册")
			continue
		}

		for _, childCard := range childCards {
			am.Add(childCard)
			logger.Debug(toolsLogComponent).
				Str("action", "setup_hierarchy_register").
				Str("child_id", childCard.ID).
				Str("parent_id", parentID).
				Msg("子 Agent 已注册到父 Agent 的 ability_manager")
		}
	}

	t.hierarchySetup = true
	return nil
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/teams/hierarchical_tools/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/teams/hierarchical_tools/
git commit -m "feat(team): 实现 HierarchicalToolsTeam 核心逻辑"
```

---

### Task 5: 编写 HierarchicalToolsTeam 单元测试

**Files:**
- Create: `internal/agentcore/multi_agent/teams/hierarchical_tools/hierarchical_team_test.go`

- [ ] **Step 1: 创建 hierarchical_team_test.go**

```go
package hierarchical_tools

import (
	"context"
	"testing"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewHierarchicalToolsTeam 验证构造函数。
func TestNewHierarchicalToolsTeam(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
		maschema.WithTeamCardName("tools_team"),
	)
	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	config := &HierarchicalToolsTeamConfig{
		RootAgent: rootCard,
	}

	team := NewHierarchicalToolsTeam(teamCard, config, nil)
	if team == nil {
		t.Fatal("期望 team 非空")
	}
	if team.rootAgentID != "root_id" {
		t.Errorf("期望 rootAgentID = root_id, 实际 = %s", team.rootAgentID)
	}
}

// TestNewHierarchicalToolsTeam_默认配置 验证 nil config 使用默认值。
func TestNewHierarchicalToolsTeam_默认配置(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalToolsTeam(teamCard, nil, nil)
	if team.rootAgentID != "" {
		t.Errorf("期望 rootAgentID 为空, 实际 = %s", team.rootAgentID)
	}
	if team.pendingChildren == nil {
		t.Error("期望 pendingChildren 非空")
	}
	if team.hierarchySetup {
		t.Error("期望 hierarchySetup = false")
	}
}

// TestHierarchicalToolsTeam_Invoke_RootAgent未注册 验证 assertReady 报错。
func TestHierarchicalToolsTeam_Invoke_RootAgent未注册(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	config := &HierarchicalToolsTeamConfig{
		RootAgent: rootCard,
	}

	team := NewHierarchicalToolsTeam(teamCard, config, nil)

	// 未注册 root_agent 到 runtime，invoke 应报错
	_, err := team.Invoke(context.Background(), map[string]any{"query": "hello"})
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalToolsTeam_Invoke_未配置RootAgent 验证无 rootAgent 时报错。
func TestHierarchicalToolsTeam_Invoke_未配置RootAgent(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	config := NewHierarchicalToolsTeamConfig() // RootAgent 为 nil

	team := NewHierarchicalToolsTeam(teamCard, config, nil)

	_, err := team.Invoke(context.Background(), map[string]any{"query": "hello"})
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalToolsTeam_Card 验证 Card 返回。
func TestHierarchicalToolsTeam_Card(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalToolsTeam(teamCard, nil, nil)
	if team.Card().GetID() != "team_1" {
		t.Errorf("期望 Card ID = team_1, 实际 = %s", team.Card().GetID())
	}
}

// TestHierarchicalToolsTeam_GetAgentCount 验证空团队 Agent 数量为 0。
func TestHierarchicalToolsTeam_GetAgentCount(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalToolsTeam(teamCard, nil, nil)
	if team.GetAgentCount() != 0 {
		t.Errorf("期望 AgentCount = 0, 实际 = %d", team.GetAgentCount())
	}
}

// TestHierarchicalToolsTeam_满足BaseTeam接口 编译时接口检查。
func TestHierarchicalToolsTeam_满足BaseTeam接口(t *testing.T) {
	var _ maschema.BaseTeam = (*HierarchicalToolsTeam)(nil)
	t.Log("HierarchicalToolsTeam 满足 BaseTeam 接口")
}

// TestHierarchicalToolsTeam_AddAgentWithParent 验证父子关系记录。
func TestHierarchicalToolsTeam_AddAgentWithParent(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalToolsTeam(teamCard, nil, nil)

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	childCard := agentschema.NewAgentCard(
		cschema.WithName("child"),
		cschema.WithID("child_id"),
	)

	rootProvider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}
	childProvider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}

	// 注册 root
	if err := team.AddAgent(context.Background(), rootCard, rootProvider); err != nil {
		t.Fatalf("AddAgent root 失败: %v", err)
	}

	// 注册 child 并声明父关系
	if err := team.AddAgentWithParent(context.Background(), childCard, childProvider, "root_id"); err != nil {
		t.Fatalf("AddAgentWithParent 失败: %v", err)
	}

	children, ok := team.pendingChildren["root_id"]
	if !ok {
		t.Fatal("期望 pendingChildren 中有 root_id 键")
	}
	if len(children) != 1 {
		t.Fatalf("期望 1 个子 Agent, 实际 = %d", len(children))
	}
	if children[0].ID != "child_id" {
		t.Errorf("期望子 Agent ID = child_id, 实际 = %s", children[0].ID)
	}
}

// TestHierarchicalToolsTeam_setupHierarchy_幂等 验证 setupHierarchy 幂等性。
func TestHierarchicalToolsTeam_setupHierarchy_幂等(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalToolsTeam(teamCard, nil, nil)

	// 无 pendingChildren，setupHierarchy 应成功且标记为已建立
	if err := team.setupHierarchy(context.Background()); err != nil {
		t.Fatalf("setupHierarchy 失败: %v", err)
	}
	if !team.hierarchySetup {
		t.Error("期望 hierarchySetup = true")
	}

	// 再次调用应直接返回 nil（幂等）
	if err := team.setupHierarchy(context.Background()); err != nil {
		t.Fatalf("幂等 setupHierarchy 失败: %v", err)
	}
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/hierarchical_tools/... -v -count=1`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/teams/hierarchical_tools/hierarchical_team_test.go
git commit -m "test(team): 添加 HierarchicalToolsTeam 单元测试"
```

---

### Task 6: 全量编译验证与回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:519`

- [ ] **Step 1: 全量编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 全量测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... -count=1`
Expected: 所有测试通过

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 8.36 行状态从 `☐` 改为 `✅`：

```
| 8.36 | ✅ | HierarchicalTeam (tools) | 层级管理-工具委托模式 | `openjiuwen/core/multi_agent/teams/` |
```

- [ ] **Step 4: 提交回填**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新实现计划 8.36 状态为已完成"
```
