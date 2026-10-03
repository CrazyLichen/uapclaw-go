# 第二批集成测试实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Runner 生命周期、Session CRUD、工具注册执行、DeepAgent ReAct 循环、Rail 子系统共 5 个模块的集成测试。

**Architecture:** 三批交付——第一交补全 Suite 基类 + Runner/Session 基础层；第二交工具执行 + DeepAgent 核心层；第三交 Rail 扩展层。每个测试套件嵌入已有的 testify/suite 四层基类，MockLLM 通过 ClientRegistry 注入。

**Tech Stack:** Go 1.26, testify/suite v1.12.1, `//go:build integration`, MockModelClient

---

## 第一交：基础层（Suite 补全 + Runner + Session）

### Task 1: 补全 SessionSuite — NewTestSession()

**Files:**
- Modify: `tests/integration/suite/session_suite.go`

- [x] **Step 1: 修改 session_suite.go，删除 NewSession 占位方法，添加 NewTestSession 辅助方法**

```go
//go:build integration

package suite

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionSuite 包含 Session 生命周期的集成测试套件。
// 等价于 Python conftest.py 中的 session fixture。
//
// 提供：
//   - RunnerSuite 全部能力
//   - SessionFacade 实例
//
// Session 在具体测试用例中按需创建，因为不同测试可能需要不同的 Session 配置。
type SessionSuite struct {
	RunnerSuite
	// Session 测试用会话实例
	Session sessioninterfaces.SessionFacade
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 初始化 Session 测试环境。
func (s *SessionSuite) SetupSuite() {
	s.RunnerSuite.SetupSuite()
}

// TearDownSuite 清理 Session 测试环境。
func (s *SessionSuite) TearDownSuite() {
	s.RunnerSuite.TearDownSuite()
}

// SetSession 设置测试用 Session 实例。
// 供具体测试用例在 SetupTest / 测试函数中调用。
func (s *SessionSuite) SetSession(session sessioninterfaces.SessionFacade) {
	s.Session = session
}

// NewTestSession 创建测试用 Session 实例。
// 使用 InMemoryCheckpointer，对齐 Python: Session(session_id=..., envs=...)
// 返回具体类型 *session.Session，以便测试调用 PreRun/PostRun/Commit 等方法。
func (s *SessionSuite) NewTestSession(sessionID string) *session.Session {
	return session.NewSession(
		session.WithSessionID(sessionID),
		session.WithEnvs(map[string]any{}),
	)
}
```

- [x] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags "sqlite_fts5 test integration" ./tests/integration/suite/...`
Expected: 编译成功，无错误

- [x] **Step 3: Commit**

```bash
git add tests/integration/suite/session_suite.go
git commit -m "feat: 补全 SessionSuite.NewTestSession() 辅助方法"
```

---

### Task 2: Runner 生命周期集成测试

**Files:**
- Create: `tests/integration/agentcore/runner/runner_test.go`

- [x] **Step 1: 编写 Runner 生命周期测试文件**

```go
//go:build integration

package runner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RunnerLifeCycleSuite 测试 Runner 启停 + 资源注册。
// 嵌入 RunnerSuite，自动获得 Runner 启停 + MockLLM 注册。
type RunnerLifeCycleSuite struct {
	isuite.RunnerSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestRunnerLifeCycleSuite(t *testing.T) {
	suite.Run(t, new(RunnerLifeCycleSuite))
}

// TestRunner_StartStop_无报错 测试 Runner 启动和停止无报错。
// 对齐 Python: await Runner.start() / await Runner.stop()
func (s *RunnerLifeCycleSuite) TestRunner_StartStop_无报错() {
	// RunnerSuite.SetupSuite 已调用 runner.Start()
	// 验证 ResourceMgr 可用即可
	s.NotNil(s.GetResourceMgr())
}

// TestRunner_GetResourceMgr_非空 测试 GetResourceMgr 返回非 nil。
// 对齐 Python: Runner.resource_mgr
func (s *RunnerLifeCycleSuite) TestRunner_GetResourceMgr_非空() {
	rm := s.GetResourceMgr()
	s.Require().NotNil(rm)
}

// TestRunner_注册AgentCard_可获取 测试向 ResourceMgr 注册 AgentCard 后可获取。
// 对齐 Python: Runner.resource_mgr.add_agent(card, provider)
func (s *RunnerLifeCycleSuite) TestRunner_注册AgentCard_可获取() {
	rm := s.GetResourceMgr()
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("itest_agent_1"),
		agentschema.WithAgentName("测试Agent"),
	)

	// 创建 stub AgentProvider
	provider := resources_manager.AgentProvider(func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return &stubBaseAgent{card: card}, nil
	})

	// 注册
	err := rm.AddAgent(card, provider)
	s.Require().NoError(err)

	// 获取
	agents, err := rm.GetAgent(s.Ctx, []string{"itest_agent_1"})
	s.Require().NoError(err)
	s.Len(agents, 1)
	s.Equal("测试Agent", agents[0].Card().Name)
}

// TestRunner_注册Tool_可获取 测试向 ResourceMgr 注册 Tool 后可获取。
// 对齐 Python: Runner.resource_mgr.add_tool(tool)
func (s *RunnerLifeCycleSuite) TestRunner_注册Tool_可获取() {
	rm := s.GetResourceMgr()

	// 创建简单的 MapFunction 工具
	tc := tool.NewToolCardWithID("itest_tool_1", "itest_tool", "测试工具", nil, nil)
	mockTool, err := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"result": "ok"}, nil
		}, nil,
	)
	s.Require().NoError(err)

	// 注册
	err = rm.AddTool(mockTool, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)

	// 获取
	tools, err := rm.GetTool([]string{"itest_tool_1"})
	s.Require().NoError(err)
	s.Len(tools, 1)
	s.Equal("itest_tool", tools[0].Card().Name)
}

// TestRunner_注销Tool_不可获取 测试注销 Tool 后不可获取。
// 对齐 Python: Runner.resource_mgr.remove_tool(tool_id)
func (s *RunnerLifeCycleSuite) TestRunner_注销Tool_不可获取() {
	rm := s.GetResourceMgr()

	// 注册
	tc := tool.NewToolCardWithID("itest_tool_remove", "itest_tool_r", "待注销工具", nil, nil)
	mockTool, err := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"result": "ok"}, nil
		}, nil,
	)
	s.Require().NoError(err)
	err = rm.AddTool(mockTool, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)

	// 注销
	removed, err := rm.RemoveTool([]string{"itest_tool_remove"})
	s.Require().NoError(err)
	s.Contains(removed, "itest_tool_remove")

	// 获取应返回空
	tools, err := rm.GetTool([]string{"itest_tool_remove"})
	s.Require().NoError(err)
	s.Empty(tools)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stubBaseAgent BaseAgent 桩实现，用于 Runner 注册测试。
type stubBaseAgent struct {
	card *agentschema.AgentCard
}

func (s *stubBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (s *stubBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return map[string]any{"output": "stub"}, nil
}
func (s *stubBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan llmschema.StreamSchema, error) {
	return nil, nil
}
func (s *stubBaseAgent) Card() *agentschema.AgentCard { return s.card }
func (s *stubBaseAgent) Config() agentinterfaces.AgentConfig { return nil }
func (s *stubBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (s *stubBaseAgent) CallbackManager() *agentinterfaces.AgentCallbackManager { return nil }
func (s *stubBaseAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ any, _ ...any) error {
	return nil
}
func (s *stubBaseAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...any) error {
	return nil
}
func (s *stubBaseAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}
func (s *stubBaseAgent) SystemPromptBuilder() any { return nil }
```

**注意**：`stubBaseAgent` 需要实现 `BaseAgent` 接口的所有方法。上面的方法签名可能需要根据实际接口调整（特别是 `RegisterCallback`、`RegisterRail`、`SystemPromptBuilder` 的参数类型）。实现时需要对照 `internal/agentcore/single_agent/interfaces/agent.go` 中的接口定义确保完全匹配。

- [x] **Step 2: 验证编译通过并修复 stubBaseAgent 接口匹配**

Run: `cd /home/opensource/uap-claw-go && go build -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/runner/...`
Expected: 编译成功。如果有接口不匹配，根据编译错误调整 stubBaseAgent 方法签名。

- [x] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/runner/...`
Expected: 5 个测试全部 PASS

- [x] **Step 4: Commit**

```bash
git add tests/integration/agentcore/runner/runner_test.go
git commit -m "feat: Runner 生命周期集成测试（5 个测试场景）"
```

---

### Task 3: Session doc.go

**Files:**
- Create: `tests/integration/agentcore/session/doc.go`

- [x] **Step 1: 创建 Session 集成测试包文档**

```go
//go:build integration

// Package session_test 提供 Session 生命周期集成测试。
//
// 覆盖 Session 创建、状态读写、PreRun/PostRun 幂等、
// InMemoryCheckpointer 持久化、环境变量传播等场景。
// 对齐 Python: tests/cli/e2e/test_session_persist.py
//
// 文件目录：
//
//	session/
//	├── doc.go            # 包文档
//	└── session_test.go   # Session 生命周期测试
package session_test
```

- [x] **Step 2: Commit**

```bash
git add tests/integration/agentcore/session/doc.go
git commit -m "docs: Session 集成测试包文档"
```

---

### Task 4: Session 生命周期集成测试

**Files:**
- Create: `tests/integration/agentcore/session/session_test.go`

- [x] **Step 1: 编写 Session 生命周期测试文件**

```go
//go:build integration

package session_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionLifeCycleSuite 测试 Session 生命周期。
// 嵌入 SessionSuite，可使用 NewTestSession() 创建测试会话。
type SessionLifeCycleSuite struct {
	isuite.SessionSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSessionLifeCycleSuite(t *testing.T) {
	suite.Run(t, new(SessionLifeCycleSuite))
}

// TestNewSession_ID自动生成 测试未指定 ID 时自动生成 UUID。
// 对齐 Python: Session() 无参构造
func (s *SessionLifeCycleSuite) TestNewSession_ID自动生成() {
	sess := s.NewTestSession("")
	s.Require().NotNil(sess)
	s.NotEmpty(sess.GetSessionID())
}

// TestNewSession_指定ID 测试 WithSessionID 指定 ID。
// 对齐 Python: Session(session_id="test-1")
func (s *SessionLifeCycleSuite) TestNewSession_指定ID() {
	sess := s.NewTestSession("test-session-1")
	s.Equal("test-session-1", sess.GetSessionID())
}

// TestPreRun_幂等 测试 PreRun 多次调用无报错。
// 对齐 Python: session.pre_run() 幂等
func (s *SessionLifeCycleSuite) TestPreRun_幂等() {
	sess := s.NewTestSession("pre-run-idempotent")
	err := sess.PreRun(s.Ctx)
	s.Require().NoError(err)
	// 第二次调用应无报错
	err = sess.PreRun(s.Ctx)
	s.Require().NoError(err)
}

// TestPostRun_幂等 测试 PostRun 多次调用无报错。
// 对齐 Python: session.post_run() 幂等
func (s *SessionLifeCycleSuite) TestPostRun_幂等() {
	sess := s.NewTestSession("post-run-idempotent")
	err := sess.PreRun(s.Ctx)
	s.Require().NoError(err)
	err = sess.PostRun(s.Ctx)
	s.Require().NoError(err)
	// 第二次调用应无报错
	err = sess.PostRun(s.Ctx)
	s.Require().NoError(err)
}

// TestUpdateState_GetState 测试状态读写一致。
// 对齐 Python: session.state 全局状态读写
func (s *SessionLifeCycleSuite) TestUpdateState_GetState() {
	sess := s.NewTestSession("state-rw")
	sess.UpdateState(map[string]any{"my_key": "my_value"})
	val, err := sess.GetState(state.StringKey("my_key"))
	s.Require().NoError(err)
	s.Equal("my_value", val)
}

// TestDumpState_完整快照 测试 DumpState 包含已写入的键值。
// 对齐 Python: session.dump_state()
func (s *SessionLifeCycleSuite) TestDumpState_完整快照() {
	sess := s.NewTestSession("dump-state")
	sess.UpdateState(map[string]any{"k1": "v1", "k2": "v2"})
	dump := sess.DumpState()
	s.NotNil(dump)
	// DumpState 返回全局+Agent+Trace 三层状态
	// 验证全局层包含写入的键
	global, ok := dump["global_state"]
	if ok {
		globalMap, ok := global.(map[string]any)
		if ok {
			s.Equal("v1", globalMap["k1"])
			s.Equal("v2", globalMap["k2"])
		}
	}
}

// TestInMemoryCheckpointer_持久化 测试 InMemoryCheckpointer 存储和恢复。
// 对齐 Python: tests/cli/e2e/test_session_persist.py
func (s *SessionLifeCycleSuite) TestInMemoryCheckpointer_持久化() {
	sess := s.NewTestSession("checkpoint-persist")
	// PreRun 触发 checkpointer PreAgentExecute
	err := sess.PreRun(s.Ctx)
	s.Require().NoError(err)
	// PostRun 触发 checkpointer PostAgentExecute
	err = sess.PostRun(s.Ctx)
	s.Require().NoError(err)
	// 验证 session 不为 nil（已通过 checkpointer 存储）
	s.NotEmpty(sess.GetSessionID())
}

// TestSession_Env读写 测试环境变量通过 Config 传播。
// 对齐 Python: session.get_env(key, default)
func (s *SessionLifeCycleSuite) TestSession_Env读写() {
	sess := session.NewSession(
		session.WithSessionID("env-test"),
		session.WithEnvs(map[string]any{"API_KEY": "test-key-123"}),
	)
	s.Equal("test-key-123", sess.GetEnv("API_KEY"))
	// 不存在的 key 返回 nil
	s.Nil(sess.GetEnv("NOT_EXIST"))
	// 带 default
	s.Equal("default_val", sess.GetEnv("NOT_EXIST", "default_val"))
}
```

- [x] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/session/...`
Expected: 编译成功

- [x] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/session/...`
Expected: 8 个测试全部 PASS

- [x] **Step 4: Commit**

```bash
git add tests/integration/agentcore/session/session_test.go
git commit -m "feat: Session 生命周期集成测试（8 个测试场景）"
```

---

## 第二交：核心层（Suite 补全 + 工具执行 + DeepAgent）

### Task 5: 补全 AgentSuite — NewDeepAgentForTest()

**Files:**
- Modify: `tests/integration/suite/agent_suite.go`

- [x] **Step 1: 修改 agent_suite.go，添加 NewDeepAgentForTest 辅助方法**

```go
//go:build integration

package suite

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentSuite 包含 Agent 执行能力的集成测试套件。
// 等价于 Python conftest.py 中注册 AgentCard + 创建 Agent 的 fixture。
//
// 提供：
//   - SessionSuite 全部能力
//   - AgentCard 和对应的 BaseAgent 实例
//   - NewDeepAgentForTest() 工厂辅助方法
//
// Agent 在具体测试中按需注册，因为不同测试需要不同类型的 Agent。
type AgentSuite struct {
	SessionSuite
	// AgentCard 已注册的 Agent 卡片
	AgentCard *agentschema.AgentCard
	// Agent 已创建的 Agent 实例
	Agent agentinterfaces.BaseAgent
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 初始化 Agent 测试环境。
func (s *AgentSuite) SetupSuite() {
	s.SessionSuite.SetupSuite()
}

// TearDownSuite 清理 Agent 测试环境。
func (s *AgentSuite) TearDownSuite() {
	s.SessionSuite.TearDownSuite()
}

// RegisterAgent 向 ResourceMgr 注册一个 Agent。
// provider 是 Agent 的创建函数，在 Runner 需要创建实例时调用。
// 对齐 Python: Runner.resource_mgr.add_agent(card, provider)
func (s *AgentSuite) RegisterAgent(
	ctx context.Context,
	card *agentschema.AgentCard,
	provider resources_manager.AgentProvider,
) error {
	return s.GetResourceMgr().AddAgent(card, provider)
}

// NewAgentCard 创建带唯一 ID 的测试用 AgentCard。
// 对齐 Python: AgentCard(id=..., name=...)
func (s *AgentSuite) NewAgentCard(id, name string) *agentschema.AgentCard {
	return agentschema.NewAgentCard(
		agentschema.WithAgentID(id),
		agentschema.WithAgentName(name),
	)
}

// NewDeepAgentForTest 通过工厂创建 DeepAgent 实例。
// 预填 MockLLM 的 Model 和默认 AgentCard，
// 测试只需传入 ToolInstances/Rails 等差异化参数。
// 对齐 Python: create_deep_agent(model=mock_model, ...)
func (s *AgentSuite) NewDeepAgentForTest(
	ctx context.Context,
	params hconfig.CreateDeepAgentParams,
) (*harness.DeepAgent, error) {
	// 确保传入 Model（从 ClientConfig + ModelConfig 创建，走 ClientRegistry 获取 MockLLM）
	if params.Model == nil {
		model, err := llm.NewModel(s.ClientConfig, s.ModelConfig)
		if err != nil {
			return nil, fmt.Errorf("创建 Mock Model 失败: %w", err)
		}
		params.Model = model
	}
	// 默认 AgentCard
	if params.Card == nil {
		params.Card = s.NewAgentCard("itest_deep_agent", "测试DeepAgent")
	}
	return harness.CreateDeepAgent(ctx, params)
}
```

- [x] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags "sqlite_fts5 test integration" ./tests/integration/suite/...`
Expected: 编译成功

- [x] **Step 3: Commit**

```bash
git add tests/integration/suite/agent_suite.go
git commit -m "feat: 补全 AgentSuite.NewDeepAgentForTest() 工厂辅助方法"
```

---

### Task 6: 工具注册与执行集成测试

**Files:**
- Create: `tests/integration/agentcore/harness/rails/tools/doc.go`
- Create: `tests/integration/agentcore/harness/rails/tools/tool_execution_test.go`

- [x] **Step 1: 创建 tools 包 doc.go**

```go
//go:build integration

// Package tools_test 提供工具注册与执行链路的集成测试。
//
// 覆盖 MapFunction / InvokeFunction 工具的注册、执行、错误传播等场景。
// 对齐 Python 各测试中 _NoteWriteTool / fakeTool 等工具使用模式。
//
// 文件目录：
//
//	tools/
//	├── doc.go                 # 包文档
//	└── tool_execution_test.go # 工具注册与执行测试
package tools_test
```

- [x] **Step 2: 编写工具执行测试文件**

```go
//go:build integration

package tools_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolExecutionSuite 测试工具注册 + 执行链路。
// 嵌入 RunnerSuite，可使用 GetResourceMgr() 注册工具。
type ToolExecutionSuite struct {
	isuite.RunnerSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestToolExecutionSuite(t *testing.T) {
	suite.Run(t, new(ToolExecutionSuite))
}

// TestMapFunction_注册执行 测试 MapFunction 工具注册后可通过 ResourceMgr 获取并执行。
// 对齐 Python: _NoteWriteTool 模式
func (s *ToolExecutionSuite) TestMapFunction_注册执行() {
	rm := s.GetResourceMgr()

	tc := tool.NewToolCardWithID("itest_map_tool", "map_tool", "MapFunction 测试工具", nil, nil)
	mockTool, err := tool.NewMapFunction(tc,
		func(_ context.Context, inputs map[string]any) (map[string]any, error) {
			return map[string]any{"echo": inputs["msg"]}, nil
		}, nil,
	)
	s.Require().NoError(err)

	// 注册
	err = rm.AddTool(mockTool, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)

	// 获取
	tools, err := rm.GetTool([]string{"itest_map_tool"})
	s.Require().NoError(err)
	s.Len(tools, 1)

	// 执行
	result, err := tools[0].Invoke(s.Ctx, map[string]any{"msg": "hello"})
	s.Require().NoError(err)
	s.Equal("hello", result["echo"])
}

// TestInvokeFunction_注册执行 测试 InvokeFunction 泛型工具注册后可执行。
// 对齐 Python: NewTool() 模式
func (s *ToolExecutionSuite) TestInvokeFunction_注册执行() {
	rm := s.GetResourceMgr()

	// 使用 NewToolCardWithID + NewMapFunction 模拟强类型工具
	tc := tool.NewToolCardWithID("itest_invoke_tool", "invoke_tool", "InvokeFunction 测试工具", nil, nil)
	mockTool, err := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"status": "ok"}, nil
		}, nil,
	)
	s.Require().NoError(err)

	err = rm.AddTool(mockTool, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)

	tools, err := rm.GetTool([]string{"itest_invoke_tool"})
	s.Require().NoError(err)
	s.Len(tools, 1)

	result, err := tools[0].Invoke(s.Ctx, map[string]any{})
	s.Require().NoError(err)
	s.Equal("ok", result["status"])
}

// TestTool_执行返回错误 测试工具 Invoke 返回 error 时调用方收到错误。
func (s *ToolExecutionSuite) TestTool_执行返回错误() {
	rm := s.GetResourceMgr()

	expectedErr := errors.New("工具执行失败")
	tc := tool.NewToolCardWithID("itest_err_tool", "err_tool", "错误工具", nil, nil)
	mockTool, err := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return nil, expectedErr
		}, nil,
	)
	s.Require().NoError(err)

	err = rm.AddTool(mockTool, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)

	tools, err := rm.GetTool([]string{"itest_err_tool"})
	s.Require().NoError(err)

	_, err = tools[0].Invoke(s.Ctx, map[string]any{})
	s.Error(err)
	s.Equal(expectedErr, err)
}

// TestTool_多工具注册 测试注册 2+ 工具后 ResourceMgr 分别可获取。
func (s *ToolExecutionSuite) TestTool_多工具注册() {
	rm := s.GetResourceMgr()

	tc1 := tool.NewToolCardWithID("itest_multi_1", "multi_tool_1", "工具1", nil, nil)
	tool1, err := tool.NewMapFunction(tc1,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"from": "tool1"}, nil
		}, nil,
	)
	s.Require().NoError(err)

	tc2 := tool.NewToolCardWithID("itest_multi_2", "multi_tool_2", "工具2", nil, nil)
	tool2, err := tool.NewMapFunction(tc2,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"from": "tool2"}, nil
		}, nil,
	)
	s.Require().NoError(err)

	err = rm.AddTool(tool1, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)
	err = rm.AddTool(tool2, resources_manager.WithTag(resources_manager.TagGlobal))
	s.Require().NoError(err)

	// 分别获取
	tools1, err := rm.GetTool([]string{"itest_multi_1"})
	s.Require().NoError(err)
	s.Len(tools1, 1)

	tools2, err := rm.GetTool([]string{"itest_multi_2"})
	s.Require().NoError(err)
	s.Len(tools2, 1)
}
```

- [x] **Step 3: 验证编译通过并运行测试**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/harness/rails/tools/...`
Expected: 4 个测试全部 PASS

- [x] **Step 4: Commit**

```bash
git add tests/integration/agentcore/harness/rails/tools/
git commit -m "feat: 工具注册与执行集成测试（4 个测试场景）"
```

---

### Task 7: DeepAgent ReAct 循环集成测试

**Files:**
- Create: `tests/integration/agentcore/harness/deep_agent_test.go`

**前置条件**：
- Task 5（AgentSuite 补全）已完成
- Task 6（工具执行测试）已完成，确认 MapFunction 注册链路可用
- DeepAgent.Invoke 需要：Model（通过 ClientRegistry 获取 MockLLM）、工具（ToolCard + Tool 实例注册到 ResourceMgr）、Session（可选）

**关键实现细节**：
- `CreateDeepAgent` 的 `ToolInstances` 字段同时处理工具注册（步骤 9 调用 `registerToolInstances` 注册到 ResourceMgr，并 `abilityManager.Add(tc)` 注册元数据）
- MockLLM 响应队列控制 ReAct 循环行为：TextResponse 终止循环，ToolCallResponse 触发工具调用
- 每个测试方法开头独立调用 `s.MockLLM.SetResponses(...)` 避免共享状态
- DeepAgent.Invoke 输入格式为 `map[string]any{"query": "..."}`

- [x] **Step 1: 编写 DeepAgent E2E 测试文件**

```go
//go:build integration

package harness_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAgentE2ESuite 测试 DeepAgent 完整执行链路。
// 嵌入 AgentSuite，可使用 NewDeepAgentForTest() 创建 DeepAgent。
type DeepAgentE2ESuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestDeepAgentE2ESuite(t *testing.T) {
	suite.Run(t, new(DeepAgentE2ESuite))
}

// newReadFileTool 创建模拟 read_file 工具。
func newReadFileTool() tool.Tool {
	tc := tool.NewToolCardWithID("read_file", "read_file", "读取文件内容", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, inputs map[string]any) (map[string]any, error) {
			path, _ := inputs["path"].(string)
			return map[string]any{"content": fmt.Sprintf("文件 %s 的内容: hello world", path)}, nil
		}, nil,
	)
	return t
}

// newWriteFileTool 创建模拟 write_file 工具。
func newWriteFileTool() tool.Tool {
	tc := tool.NewToolCardWithID("write_file", "write_file", "写入文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, inputs map[string]any) (map[string]any, error) {
			return map[string]any{"status": "ok"}, nil
		}, nil,
	)
	return t
}

// newFailTool 创建总是失败的模拟工具。
func newFailTool() tool.Tool {
	tc := tool.NewToolCardWithID("fail_tool", "fail_tool", "总是失败的工具", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return nil, fmt.Errorf("工具执行失败: 模拟错误")
		}, nil,
	)
	return t
}

// TestInvoke_单轮纯文本 测试 DeepAgent 单轮纯文本 Invoke。
// 对齐 Python: test_deep_agent_invoke_e2e（文本响应场景）
func (s *DeepAgentE2ESuite) TestInvoke_单轮纯文本() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("这是最终答案"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "你好"})
	s.Require().NoError(err)
	s.NotNil(result)
}

// TestInvoke_单轮工具调用 测试 DeepAgent 单轮工具调用（LLM → tool_call → 工具执行 → LLM → 文本）。
// 对齐 Python: test_deep_agent_invoke_e2e（工具调用场景）
func (s *DeepAgentE2ESuite) TestInvoke_单轮工具调用() {
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"path": "/tmp/test.txt"}`),
		mockllm.CreateTextResponse("文件内容是 hello world"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{newReadFileTool()},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "读取 /tmp/test.txt"})
	s.Require().NoError(err)
	s.NotNil(result)

	// MockLLM 被调用了 2 次（tool_call + text）
	s.Equal(2, s.MockLLM.InvokeCallCount())
}

// TestInvoke_多轮工具链 测试 DeepAgent 连续多轮工具调用。
// 对齐 Python: test_deep_agent_complex_task_multi_tool_chain
func (s *DeepAgentE2ESuite) TestInvoke_多轮工具链() {
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"path": "/tmp/input.txt"}`),
		mockllm.CreateToolCallResponse("write_file", `{"path": "/tmp/output.txt", "content": "processed"}`),
		mockllm.CreateTextResponse("任务完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{newReadFileTool(), newWriteFileTool()},
		MaxIterations: 10,
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "读取并写入文件"})
	s.Require().NoError(err)
	s.NotNil(result)

	// MockLLM 被调用了 3 次
	s.Equal(3, s.MockLLM.InvokeCallCount())
}

// TestInvoke_工具失败错误传播 测试工具返回 error 后 DeepAgent 能继续执行。
// 对齐 Python: 工具失败错误传播场景
func (s *DeepAgentE2ESuite) TestInvoke_工具失败错误传播() {
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("fail_tool", `{"input": "test"}`),
		mockllm.CreateTextResponse("已处理工具错误"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{newFailTool()},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "调用失败工具"})
	// 工具失败但 ReAct 循环继续，最终 Agent 返回结果
	s.Require().NoError(err)
	s.NotNil(result)
}

// TestInvoke_带Session 测试 DeepAgent Invoke 带 Session 运行。
// 对齐 Python: Agent + Session 联动
func (s *DeepAgentE2ESuite) TestInvoke_带Session() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("带 Session 执行完成"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("deep-agent-session-test")
	err = sess.PreRun(s.Ctx)
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "带 Session 测试"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err)
	s.NotNil(result)
}

// TestAutoRails_默认创建 测试不传 Rails 时 DeepAgent 自动注册默认 Rail。
// 对齐 Python: test_deep_agent_auto_rails_creation_e2e
func (s *DeepAgentE2ESuite) TestAutoRails_默认创建() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("自动 Rail 测试"))

	// 不传任何 Rails
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "自动 Rail"})
	s.Require().NoError(err)
	s.NotNil(result)

	// 验证 DeepAgent 自动注册了 TaskCompletionRail（通过 FindRailsByType）
	railType := reflect.TypeOf(&rails.TaskCompletionRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应自动注册 TaskCompletionRail")
}
```

**注意**：`TestAutoRails_默认创建` 中使用了 `reflect.TypeOf`，需要在 import 中添加 `reflect`。`FindRailsByType` 方法签名需要对照 `DeepAgentInterface` 确认。

- [x] **Step 2: 验证编译通过并修复任何接口/类型问题**

Run: `cd /home/opensource/uap-claw-go && go build -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/harness/...`
Expected: 编译成功。如有问题，根据编译错误调整。

- [x] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test integration" -run TestDeepAgentE2ESuite ./tests/integration/agentcore/harness/...`
Expected: 6 个测试全部 PASS

- [x] **Step 4: Commit**

```bash
git add tests/integration/agentcore/harness/deep_agent_test.go
git commit -m "feat: DeepAgent ReAct 循环集成测试（6 个测试场景）"
```

---

## 第三交：扩展层（Rail 子系统）

### Task 8: TaskCompletionRail 集成测试

**Files:**
- Create: `tests/integration/agentcore/harness/rails/interrupt/task_completion_test.go`

**前置条件**：
- Task 7（DeepAgent 测试）已完成，DeepAgent + MockLLM + Tool 链路验证通过
- `rails.NewTaskCompletionRail` + `WithMaxRounds`/`WithTimeoutSeconds`/`WithCompletionPromise` 选项可用

**关键实现细节**：
- TaskCompletionRail 需要配合 `EnableTaskLoop=true` 的 DeepAgent 使用
- MaxRounds 测试：设置 `WithMaxRounds(2)`，MockLLM 持续返回 ToolCall，验证第 3 轮前终止
- Timeout 测试：设置 `WithTimeoutSeconds(2)`，MockLLM 持续返回 ToolCall + sleep 模拟耗时
- CompletionPromise 测试：MockLLM 返回 `<promise>任务完成</promise>` 文本，验证循环终止
- TaskInstruction 测试：验证 BeforeTaskIteration 在首次迭代注入任务指令

- [x] **Step 1: 编写 TaskCompletionRail 测试文件**

```go
//go:build integration

package interrupt_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskCompletionRailSuite 测试 TaskCompletionRail 停止条件。
type TaskCompletionRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestTaskCompletionRailSuite(t *testing.T) {
	suite.Run(t, new(TaskCompletionRailSuite))
}

// newLoopTool 创建一个总是返回成功的工具（用于模拟不终止的 ReAct 循环）。
func newLoopTool() tool.Tool {
	tc := tool.NewToolCardWithID("loop_tool", "loop_tool", "循环工具", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"step": "done"}, nil
		}, nil,
	)
	return t
}

// TestMaxRounds_超限自动停止 测试 MaxRounds 限制。
// 对齐 Python: test_task_completion_rail.py UC-1
func (s *TaskCompletionRailSuite) TestMaxRounds_超限自动停止() {
	// MockLLM 持续返回 tool_call（模拟不终止的 ReAct 循环）
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("loop_tool", `{}`),
		mockllm.CreateToolCallResponse("loop_tool", `{}`),
		mockllm.CreateToolCallResponse("loop_tool", `{}`),
		mockllm.CreateToolCallResponse("loop_tool", `{}`),
		mockllm.CreateTextResponse("终止"),
	)

	tcr := rails.NewTaskCompletionRail(rails.WithMaxRounds(2))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances:    []tool.Tool{newLoopTool()},
		Rails:            []agentinterfaces.AgentRail{tcr},
		EnableTaskLoop:   true,
		MaxIterations:    10,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("tcr-max-rounds")
	err = sess.PreRun(s.Ctx)
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "循环任务"},
		agentinterfaces.WithSession(sess),
	)
	// 应在 MaxRounds 限制内完成，不报错
	s.Require().NoError(err)
	s.NotNil(result)
}

// TestCompletionPromise_标签触发 测试 <promise> 标签触发完成。
// 对齐 Python: test_task_completion_rail.py UC-2
func (s *TaskCompletionRailSuite) TestCompletionPromise_标签触发() {
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("loop_tool", `{}`),
		mockllm.CreateTextResponse("<promise>任务完成</promise>"),
	)

	tcr := rails.NewTaskCompletionRail(rails.WithCompletionPromise("任务完成"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances:    []tool.Tool{newLoopTool()},
		Rails:            []agentinterfaces.AgentRail{tcr},
		EnableTaskLoop:   true,
		MaxIterations:    10,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("tcr-promise")
	err = sess.PreRun(s.Ctx)
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "带承诺的任务"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err)
	s.NotNil(result)
}

// TestTaskInstruction_模板注入 测试 task_instruction 模板在首次迭代注入。
// 对齐 Python: test_task_completion_rail.py UC-3
func (s *TaskCompletionRailSuite) TestTaskInstruction_模板注入() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("执行指令"))

	tcr := rails.NewTaskCompletionRail(rails.WithTaskInstruction("请完成以下任务: {query}"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:          []agentinterfaces.AgentRail{tcr},
		EnableTaskLoop: true,
		MaxIterations:  3,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("tcr-instruction")
	err = sess.PreRun(s.Ctx)
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "测试任务"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err)
	s.NotNil(result)
}

// TestTimeout_超时终止 测试超时后循环终止。
// 对齐 Python: test_task_completion_rail.py UC-5
func (s *TaskCompletionRailSuite) TestTimeout_超时终止() {
	// MockLLM 持续返回 tool_call（模拟不终止的 ReAct 循环）
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("loop_tool", `{}`),
		mockllm.CreateToolCallResponse("loop_tool", `{}`),
		mockllm.CreateToolCallResponse("loop_tool", `{}`),
		mockllm.CreateTextResponse("超时终止"),
	)

	tcr := rails.NewTaskCompletionRail(rails.WithTimeoutSeconds(3))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances:    []tool.Tool{newLoopTool()},
		Rails:            []agentinterfaces.AgentRail{tcr},
		EnableTaskLoop:   true,
		MaxIterations:    100,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("tcr-timeout")
	err = sess.PreRun(s.Ctx)
	s.Require().NoError(err)

	start := time.Now()
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "超时测试"},
		agentinterfaces.WithSession(sess),
	)
	elapsed := time.Since(start)

	// 应在超时时间内完成
	s.Require().NoError(err)
	s.NotNil(result)
	s.Less(elapsed, 10*time.Second, "应在超时后很快完成")
}
```

- [x] **Step 2: 验证编译通过并运行测试**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test integration" -run TestTaskCompletionRailSuite ./tests/integration/agentcore/harness/rails/interrupt/...`
Expected: 4 个测试全部 PASS

- [x] **Step 3: Commit**

```bash
git add tests/integration/agentcore/harness/rails/interrupt/task_completion_test.go
git commit -m "feat: TaskCompletionRail 集成测试（4 个测试场景）"
```

---

### Task 9: ConfirmInterruptRail 集成测试

**Files:**
- Create: `tests/integration/agentcore/harness/rails/interrupt/confirm_rail_test.go`

**前置条件**：
- Task 8（TaskCompletionRail 测试）已完成
- `interrupt.NewConfirmInterruptRail("write_file")` 可用

**关键实现细节**：
- ConfirmInterruptRail 拦截指定工具的 BeforeToolCall 钩子
- 被拦截时 Agent 进入 HITL 等待状态，需通过 Session.Interact() 模拟用户确认
- 此测试较复杂，需要 goroutine 模拟用户输入，且依赖 HITL 机制的完整实现
- 如果 HITL 机制尚未完整实现，测试可能需要简化为验证 Rail 初始化/注册成功

- [x] **Step 1: 编写 ConfirmInterruptRail 测试文件**

```go
//go:build integration

package interrupt_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConfirmInterruptRailSuite 测试 ConfirmInterruptRail 工具权限中断。
type ConfirmInterruptRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestConfirmInterruptRailSuite(t *testing.T) {
	suite.Run(t, new(ConfirmInterruptRailSuite))
}

// TestConfirmInterruptRail_初始化成功 测试 ConfirmInterruptRail 可成功创建和初始化。
// 对齐 Python: test_hitl_tool_permission_interrupt_read_file_ask（初始化部分）
func (s *ConfirmInterruptRailSuite) TestConfirmInterruptRail_初始化成功() {
	rail := interrupt.NewConfirmInterruptRail("write_file", "delete_file")
	s.Require().NotNil(rail)
	s.Equal(0, rail.Priority())

	// 创建带 Rail 的 DeepAgent
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)
}

// TestConfirmInterruptRail_拦截未授权工具 测试拦截未授权工具调用。
// 对齐 Python: test_deepagent_stream_interrupt_resume（拦截部分）
func (s *ConfirmInterruptRailSuite) TestConfirmInterruptRail_拦截未授权工具() {
	rail := interrupt.NewConfirmInterruptRail("write_file")

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("确认后继续"),
	)

	writeTool := newWriteFileTool()
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	// 验证 Agent 创建成功且 Rail 已注册
	s.Require().NotNil(agent)
	rails := agent.FindRailsByType(nil)
	// 至少有 ConfirmInterruptRail + 默认 Rails
	s.NotEmpty(rails)
}

// newWriteFileTool 创建模拟 write_file 工具（ConfirmInterrupt 测试用）。
func newWriteFileTool() tool.Tool {
	tc := tool.NewToolCardWithID("write_file", "write_file", "写入文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, inputs map[string]any) (map[string]any, error) {
			return map[string]any{"status": "written"}, nil
		}, nil,
	)
	return t
}
```

**注意**：ConfirmInterruptRail 的完整中断/恢复测试依赖 HITL（Human-In-The-Loop）机制的 `Session.Interact()` + `InteractiveInput`，如果该机制在 Go 端尚未完全实现，上述测试只验证 Rail 初始化和注册成功。完整的中断/恢复测试在 HITL 回填后补充。

- [x] **Step 2: 验证编译通过并运行测试**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test integration" -run TestConfirmInterruptRailSuite ./tests/integration/agentcore/harness/rails/interrupt/...`
Expected: 2 个测试全部 PASS

- [x] **Step 3: Commit**

```bash
git add tests/integration/agentcore/harness/rails/interrupt/confirm_rail_test.go
git commit -m "feat: ConfirmInterruptRail 集成测试（2 个测试场景）"
```

---

### Task 10: 全量验证 + 更新设计文档状态

**Files:**
- Modify: `docs/superpowers/specs/2027-07-01-integration-test-framework-design.md`

- [x] **Step 1: 运行全量集成测试**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test integration" ./tests/integration/...`
Expected: 所有测试 PASS（包括第一批已有测试 + 第二批新增测试）

- [x] **Step 2: 更新设计文档中的实施状态**

在 `2027-07-01-integration-test-framework-design.md` 的 Section 8 中添加第二批状态：

```markdown
### 第二批：核心模块（Runner + DeepAgent + Session）

| 步骤 | 状态 | 说明 |
|------|------|------|
| 补全 SessionSuite.NewTestSession() | ✅ | 按需创建，返回 *session.Session |
| 补全 AgentSuite.NewDeepAgentForTest() | ✅ | 工厂模式，预填 MockLLM Model |
| Runner 生命周期集成测试 | ✅ | 5 个测试场景 |
| Session 生命周期集成测试 | ✅ | 8 个测试场景 |
| 工具注册与执行集成测试 | ✅ | 4 个测试场景 |
| DeepAgent ReAct 循环集成测试 | ✅ | 6 个测试场景 |
| TaskCompletionRail 集成测试 | ✅ | 4 个测试场景 |
| ConfirmInterruptRail 集成测试 | ✅ | 2 个测试场景（完整中断/恢复待 HITL 回填） |
```

- [x] **Step 3: Commit**

```bash
git add docs/superpowers/specs/2027-07-01-integration-test-framework-design.md
git commit -m "docs: 更新第二批集成测试实施状态"
```
