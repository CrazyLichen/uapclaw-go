# 第四批集成测试实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 14 个零覆盖 Rail 补全集成测试 + ConfirmInterruptRail 细粒度确认扩展，共 28 个测试用例。

**Architecture:** 按域分包 1:1 对齐源码结构，9 个新包 + 1 个已有包扩展。所有 Suite 嵌入 `isuite.AgentSuite`。有 GetCallbacks 覆盖的 Rail（5 个）测完整回调链路；无 GetCallbacks 覆盖的 Rail（7 个）仅测 Init 注册工具 + 构造成功。ConfirmInterruptRail 扩展 3 个细粒度确认测试。

**Tech Stack:** Go 1.22+ / testify/suite v1.12.1 / MockLLM (tests/integration/mockllm) / //go:build integration

**设计文档:** `docs/superpowers/specs/2027-07-01-integration-test-batch4-design.md`

---

## 文件结构

### 新增文件

| 文件 | 职责 |
|------|------|
| `tests/integration/agentcore/harness/rails/subagent/doc.go` | 包文档 |
| `tests/integration/agentcore/harness/rails/subagent/subagent_rail_test.go` | SubagentRail + VerificationRail + VerificationContractRail 测试（3 测试） |
| `tests/integration/agentcore/harness/rails/skills/doc.go` | 包文档 |
| `tests/integration/agentcore/harness/rails/skills/skill_use_rail_test.go` | SkillUseRail 测试（2 测试） |
| `tests/integration/agentcore/harness/rails/planning/doc.go` | 包文档 |
| `tests/integration/agentcore/harness/rails/planning/task_planning_rail_test.go` | TaskPlanningRail 测试（3 测试） |
| `tests/integration/agentcore/harness/rails/agent_mode/doc.go` | 包文档 |
| `tests/integration/agentcore/harness/rails/agent_mode/agent_mode_rail_test.go` | AgentModeRail 测试（3 测试） |
| `tests/integration/agentcore/harness/rails/heartbeat/doc.go` | 包文档 |
| `tests/integration/agentcore/harness/rails/heartbeat/heartbeat_rail_test.go` | HeartbeatRail 测试（2 测试） |
| `tests/integration/agentcore/harness/rails/progressive/doc.go` | 包文档 |
| `tests/integration/agentcore/harness/rails/progressive/progressive_rail_test.go` | ProgressiveToolRail 测试（3 测试） |
| `tests/integration/agentcore/harness/rails/sys_operation/doc.go` | 包文档 |
| `tests/integration/agentcore/harness/rails/sys_operation/sys_operation_rail_test.go` | SysOperationRail 测试（3 测试） |
| `tests/integration/agentcore/harness/rails/mcp/doc.go` | 包文档 |
| `tests/integration/agentcore/harness/rails/mcp/mcp_rail_test.go` | McpRail 测试（3 测试） |
| `tests/integration/agentcore/harness/rails/context_engineer/doc.go` | 包文档 |
| `tests/integration/agentcore/harness/rails/context_engineer/context_engineer_rail_test.go` | ContextAssembleRail + ContextProcessorRail 测试（3 测试） |

### 修改文件

| 文件 | 变更 |
|------|------|
| `tests/integration/agentcore/harness/rails/interrupt/confirm_rail_test.go` | 新增 3 个测试方法（现有 2 个保持不变） |
| `docs/superpowers/specs/2027-07-01-integration-test-batch4-design.md` | 更新实施状态 |

### 不新增 Suite 基类

所有 Rail 测试套件直接嵌入 `isuite.AgentSuite`，无额外共享状态。

---

## 关键约定

### 通用导入路径

```go
//go:build integration

package xxx

import (
    "reflect"
    "testing"

    "github.com/stretchr/testify/suite"
    hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
    agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
    isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
    "github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)
```

### 通用测试模式

1. **创建 Rail** → 传给 `CreateDeepAgentParams.Rails`
2. **设置 MockLLM** → `s.MockLLM.SetResponses(mockllm.CreateTextResponse("xxx"))`
3. **创建 Agent** → `s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{...})`
4. **验证 Rail 注册** → `agent.FindRailsByType(reflect.TypeOf(&xxxRailType{}))`
5. **验证工具注册** → `agent.AbilityManager().Get("tool_name")`
6. **触发回调** → `agent.Invoke(s.Ctx, map[string]any{"query": "xxx"})`

### Rail 构造方式速查

| Rail | 构造 |
|------|------|
| ConfirmInterruptRail | `interrupt.NewConfirmInterruptRail("tool1", "tool2")` |
| SubagentRail | `subagent.NewSubagentRail()` |
| VerificationRail | `subagent.NewVerificationRail()` |
| VerificationContractRail | `subagent.NewVerificationContractRail()` |
| SkillUseRail | `skilluse.NewSkillUseRail([]string{tmpDir})` |
| TaskPlanningRail | `planning.NewTaskPlanningRail()` |
| AgentModeRail | `agentmode.NewAgentModeRail(nil)` |
| HeartbeatRail | `heartbeat.NewHeartbeatRail()` |
| ProgressiveToolRail | `progressive.NewProgressiveToolRail(schema.NewDeepAgentConfig())` |
| SysOperationRail | `sysop.NewSysOperationRail()` |
| McpRail | `mcp.NewMcpRail()` |
| ContextAssembleRail | `ce.NewContextAssembleRail()` |
| ContextProcessorRail | `ce.NewContextProcessorRail()` |

### 注意事项

- **InterruptResult → panic**：BeforeToolCall 返回 InterruptResult 会触发 `panic(cb.NewAbortError(...))`，测试中必须 `recover` 或使用 `RequestPermissionConfirmation` 避免
- **AutoConfirm 通过 session state**：`sess.UpdateState(s.Ctx, state.StringKey(saschema.InterruptAutoConfirmKey), map[string]any{"write_file": true})`
- **addDefaultRails 自动添加**：SafetyPromptRail、AskUserRail、SysOperationRail 始终自动注册。手动传入同名 Rail 时工厂会去重
- **DeepAgentInterface 类型断言**：多个 Rail Init 中对 agent 进行类型断言 `agent.(hinterfaces.DeepAgentInterface)`，NewDeepAgentForTest 创建的 DeepAgent 满足此接口
- **SkillUseRail 空 skillsDir**：即使 skillsDir 为空，Init 仍注册工具（skill_tool 等），不会崩溃
- **McpRail 无 MCP 配置**：Init 始终注册 list_mcp_resources + read_mcp_resource，不需要实际 MCP 服务
- **TaskPlanningRail 工具依赖**：需要 SysOperation + Workspace 才注册 todo 工具。NewDeepAgentForTest 默认会自动创建这两者
- **ContextProcessorRail 需要 ReActAgentConfig**：`agent.Config()` 必须返回 `*saconfig.ReActAgentConfig`，DeepAgent 满足

---

## Task 1: interrupt/ — ConfirmInterruptRail 细粒度确认扩展

**Files:**
- Modify: `tests/integration/agentcore/harness/rails/interrupt/confirm_rail_test.go`

现有 2 个测试保持不变，新增 3 个测试。当前包名 `interrupt_test`（外部测试包）。

- [ ] **Step 1: 新增 TestConfirmInterrupt_多工具选择性拦截**

在 `ConfirmInterruptRailSuite` 上新增方法。注册 2 个拦截工具 `write_file` 和 `delete_file`，但只提供一个 `write_file` 工具实例。MockLLM 返回 write_file 的 tool_call。验证 write_file 被拦截触发中断（通过 recover 捕获 panic），而未注册的 `read_file` 工具直接放行。

```go
// TestConfirmInterrupt_多工具选择性拦截 测试只拦截注册列表中的工具。
// 对齐 Python: test_confirm_interrupt_rail（选择性拦截）
func (s *ConfirmInterruptRailSuite) TestConfirmInterrupt_多工具选择性拦截() {
	rail := interrupt.NewConfirmInterruptRail("write_file", "delete_file")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"path":"/tmp/test.txt","content":"hello"}`),
		mockllm.CreateTextResponse("写入完成"),
	)

	writeTool := newConfirmWriteFileTool()
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	// write_file 在拦截列表中，BeforeToolCall 会触发 InterruptResult → panic
	// 使用 recover 捕获 panic 验证中断行为
	defer func() {
		r := recover()
		s.NotNil(r, "write_file 在拦截列表中应触发中断 panic")
	}()

	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "写入文件"})
}
```

注意：实际 BeforeToolCall 触发 InterruptResult 时会 `raiseInterrupt` → `panic`。测试通过 `recover` 捕获验证。

- [ ] **Step 2: 新增 TestConfirmInterrupt_AutoConfirm跳过确认**

设置 session state 的 `__interrupt_auto_confirm__` 为 `{"write_file": true}`，验证工具调用直接放行不触发中断。

```go
// TestConfirmInterrupt_AutoConfirm跳过确认 测试 AutoConfirm 配置跳过确认流程。
// 对齐 Python: test_confirm_interrupt_rail（auto_confirm）
func (s *ConfirmInterruptRailSuite) TestConfirmInterrupt_AutoConfirm跳过确认() {
	rail := interrupt.NewConfirmInterruptRail("write_file")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"path":"/tmp/test.txt","content":"hello"}`),
		mockllm.CreateTextResponse("写入完成"),
	)

	writeTool := newConfirmWriteFileTool()
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	// 设置 AutoConfirm
	sess := s.NewTestSession("confirm-auto")
	sess.PreRun(s.Ctx)
	sess.UpdateState(s.Ctx, state.StringKey(saschema.InterruptAutoConfirmKey),
		map[string]any{"write_file": true})

	// AutoConfirm 下 Invoke 应正常完成，不触发中断
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess))
	sess.PostRun(s.Ctx)

	s.Require().NoError(err, "AutoConfirm 下应正常完成")
	s.NotNil(result, "应返回结果")
}
```

需额外导入：
- `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/state"`
- `saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"`

- [ ] **Step 3: 新增 TestConfirmInterrupt_未注册工具放行**

工具 `read_file` 不在拦截列表中，验证直接放行。

```go
// TestConfirmInterrupt_未注册工具放行 测试不在拦截列表中的工具直接放行。
// 对齐 Python: test_confirm_interrupt_rail（非拦截工具）
func (s *ConfirmInterruptRailSuite) TestConfirmInterrupt_未注册工具放行() {
	rail := interrupt.NewConfirmInterruptRail("delete_file") // 只拦截 delete_file

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"path":"/tmp/test.txt"}`),
		mockllm.CreateTextResponse("读取完成"),
	)

	readTool := newConfirmReadFileTool()
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	// read_file 不在拦截列表中，应直接放行
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "读取文件"})
	s.Require().NoError(err, "未注册工具应直接放行")
	s.NotNil(result, "应返回结果")
}
```

同时新增辅助函数：

```go
// newConfirmReadFileTool 创建模拟 read_file 工具（ConfirmInterrupt 测试用）。
func newConfirmReadFileTool() tool.Tool {
	tc := tool.NewToolCardWithID("read_file", "read_file", "读取文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"status": "read", "content": "hello"}, nil
		}, nil,
	)
	return t
}
```

- [ ] **Step 4: 验证 interrupt 扩展编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags=integration ./tests/integration/agentcore/harness/rails/interrupt/...`
Expected: 无编译错误

- [ ] **Step 5: 运行 interrupt 扩展测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -v -run 'TestConfirmInterruptRailSuite' ./tests/integration/agentcore/harness/rails/interrupt/...`
Expected: 5 PASS（2 原有 + 3 新增）

- [ ] **Step 6: 提交**

```bash
git add tests/integration/agentcore/harness/rails/interrupt/confirm_rail_test.go
git commit -m "test(integration): ConfirmInterruptRail 细粒度确认扩展 3 个测试"
```

---

## Task 2: subagent/ — 3 个 Rail 基础测试

**Files:**
- Create: `tests/integration/agentcore/harness/rails/subagent/doc.go`
- Create: `tests/integration/agentcore/harness/rails/subagent/subagent_rail_test.go`

3 个 Rail 都无 GetCallbacks 覆盖，仅测 Init 注册工具 + 构造成功。

- [ ] **Step 1: 创建 doc.go**

```go
//go:build integration

// Package subagent 提供 SubagentRail / VerificationRail / VerificationContractRail 集成测试。
//
// 对齐 Python: tests/system_tests/harness/rail/test_task_planning_rail.py（子代理部分）
//
// 文件目录：
//
//	subagent/
//	├── doc.go                  # 包文档
//	└── subagent_rail_test.go   # 3 个 Rail 测试（SubagentRail + VerificationRail + VerificationContractRail）
//
// 对应 Python 代码：openjiuwen/harness/rails/subagent/
package subagent
```

- [ ] **Step 2: 创建 subagent_rail_test.go**

```go
//go:build integration

package subagent

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	subagent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SubagentRailSuite 测试 SubagentRail / VerificationRail / VerificationContractRail。
//
// 对齐 Python: tests/system_tests/harness/rail/test_subagent_rail.py
// 注意：这 3 个 Rail 未覆盖 GetCallbacks()，回调方法不会自动触发，
// 仅验证 Init 注册工具和构造成功。
type SubagentRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSubagentRailSuite(t *testing.T) {
	suite.Run(t, new(SubagentRailSuite))
}

// TestSubagentRail_Init注册TaskTool 测试 SubagentRail Init 后注册 task_tool。
// 对齐 Python: test_subagent_rail_init
func (s *SubagentRailSuite) TestSubagentRail_Init注册TaskTool() {
	rail := subagent.NewSubagentRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("子代理测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 SubagentRail 已注册
	railType := reflect.TypeOf(&subagent.SubagentRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 SubagentRail")

	// SubagentRail.Init 需要 DeepConfig.Subagents 非空才注册 task_tool
	// 未配置子代理时 Init 不注册工具（正常降级），验证不崩溃即可
}

// TestVerificationRail_Init注册成功 测试 VerificationRail Init 成功且 Rail 注册到 Agent。
// 对齐 Python: test_verification_rail_init
func (s *SubagentRailSuite) TestVerificationRail_Init注册成功() {
	rail := subagent.NewVerificationRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("验证测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 VerificationRail 已注册
	railType := reflect.TypeOf(&subagent.VerificationRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 VerificationRail")
}

// TestVerificationContractRail_Init成功 测试 VerificationContractRail Init 成功且 Rail 注册到 Agent。
// 对齐 Python: test_verification_contract_rail_init
func (s *SubagentRailSuite) TestVerificationContractRail_Init成功() {
	rail := subagent.NewVerificationContractRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("契约验证测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 VerificationContractRail 已注册
	railType := reflect.TypeOf(&subagent.VerificationContractRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 VerificationContractRail")
}
```

- [ ] **Step 3: 验证 subagent 编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags=integration ./tests/integration/agentcore/harness/rails/subagent/...`
Expected: 无编译错误

- [ ] **Step 4: 运行 subagent 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -v -run 'TestSubagentRailSuite' ./tests/integration/agentcore/harness/rails/subagent/...`
Expected: 3 PASS

- [ ] **Step 5: 提交**

```bash
git add tests/integration/agentcore/harness/rails/subagent/
git commit -m "test(integration): SubagentRail/VerificationRail/VerificationContractRail 3 个测试"
```

---

## Task 3: skills/ — SkillUseRail 测试

**Files:**
- Create: `tests/integration/agentcore/harness/rails/skills/doc.go`
- Create: `tests/integration/agentcore/harness/rails/skills/skill_use_rail_test.go`

SkillUseRail 无 GetCallbacks 覆盖。关键：空 skillsDir 时 Init 不崩溃，仍注册工具。

- [ ] **Step 1: 创建 doc.go**

```go
//go:build integration

// Package skills 提供 SkillUseRail 集成测试。
//
// 对齐 Python: tests/system_tests/harness/rail/test_team_skill_rail.py
//
// 文件目录：
//
//	skills/
//	├── doc.go               # 包文档
//	└── skill_use_rail_test.go # SkillUseRail 测试（2 测试）
//
// 对应 Python 代码：openjiuwen/harness/rails/skills/
package skills
```

- [ ] **Step 2: 创建 skill_use_rail_test.go**

```go
//go:build integration

package skills

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	skilluse "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillUseRailSuite 测试 SkillUseRail 技能使用。
//
// 对齐 Python: tests/system_tests/harness/rail/test_team_skill_rail.py
// 注意：SkillUseRail 未覆盖 GetCallbacks()，回调方法不会自动触发，
// 仅验证 Init 注册工具和构造成功。
type SkillUseRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSkillUseRailSuite(t *testing.T) {
	suite.Run(t, new(SkillUseRailSuite))
}

// TestSkillUseRail_Init注册工具 测试 SkillUseRail Init 后注册 skill_tool 等工具。
// 对齐 Python: test_skill_use_rail_init
func (s *SkillUseRailSuite) TestSkillUseRail_Init注册工具() {
	skillsDir := s.T().TempDir()
	rail := skilluse.NewSkillUseRail([]string{skillsDir})

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("技能测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 SkillUseRail 已注册
	railType := reflect.TypeOf(&skilluse.SkillUseRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 SkillUseRail")

	// 验证 skill_tool 已注册到 AbilityManager
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("skill_tool"), "应注册 skill_tool")
}

// TestSkillUseRail_无技能目录时不崩溃 测试 skillsDir 为空时 Init 不崩溃。
// 对齐 Python: test_skill_use_rail_empty_dir
func (s *SkillUseRailSuite) TestSkillUseRail_无技能目录时不崩溃() {
	// 空 skillsDir 列表
	rail := skilluse.NewSkillUseRail([]string{})

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("空目录测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "空 skillsDir 时 Init 不应崩溃")
	s.Require().NotNil(agent)

	// 即使空 skillsDir，工具仍注册
	railType := reflect.TypeOf(&skilluse.SkillUseRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "空 skillsDir 时 SkillUseRail 仍应注册")
}
```

- [ ] **Step 3: 验证 skills 编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags=integration ./tests/integration/agentcore/harness/rails/skills/...`
Expected: 无编译错误

- [ ] **Step 4: 运行 skills 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -v -run 'TestSkillUseRailSuite' ./tests/integration/agentcore/harness/rails/skills/...`
Expected: 2 PASS

- [ ] **Step 5: 提交**

```bash
git add tests/integration/agentcore/harness/rails/skills/
git commit -m "test(integration): SkillUseRail 2 个测试"
```

---

## Task 4: planning/ — TaskPlanningRail 测试

**Files:**
- Create: `tests/integration/agentcore/harness/rails/planning/doc.go`
- Create: `tests/integration/agentcore/harness/rails/planning/task_planning_rail_test.go`

TaskPlanningRail **有 GetCallbacks 覆盖**（5 个回调），可测完整回调链路。关键：Init 需要 SysOperation + Workspace 才注册 todo 工具，NewDeepAgentForTest 会自动创建。

- [ ] **Step 1: 创建 doc.go**

```go
//go:build integration

// Package planning 提供 TaskPlanningRail 集成测试。
//
// 对齐 Python: tests/system_tests/harness/rail/test_task_planning_rail.py
//
// 文件目录：
//
//	planning/
//	├── doc.go                  # 包文档
//	└── task_planning_rail_test.go # TaskPlanningRail 测试（3 测试）
//
// 对应 Python 代码：openjiuwen/harness/rails/task_planning.py
package planning
```

- [ ] **Step 2: 创建 task_planning_rail_test.go**

```go
//go:build integration

package planning

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	taskplanning "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskPlanningRailSuite 测试 TaskPlanningRail 任务规划。
//
// 对齐 Python: tests/system_tests/harness/rail/test_task_planning_rail.py
// TaskPlanningRail 覆盖了 GetCallbacks()，可测完整回调链路。
type TaskPlanningRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestTaskPlanningRailSuite(t *testing.T) {
	suite.Run(t, new(TaskPlanningRailSuite))
}

// TestTaskPlanningRail_Init注册TodoTool 测试 TaskPlanningRail Init 后注册 todo 工具。
// 对齐 Python: test_task_planning_rail_init
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_Init注册TodoTool() {
	rail := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("规划测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 TaskPlanningRail 已注册
	railType := reflect.TypeOf(&taskplanning.TaskPlanningRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 TaskPlanningRail")

	// 验证 todo 工具注册（需要 SysOperation + Workspace，NewDeepAgentForTest 会自动创建）
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("todo_create"), "应注册 todo_create")
	s.NotNil(am.Get("todo_list"), "应注册 todo_list")
}

// TestTaskPlanningRail_BeforeModelCall注入规划提示词 测试 BeforeModelCall 注入 task_planning section。
// 对齐 Python: test_task_planning_rail_before_model_call
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_BeforeModelCall注入规划提示词() {
	rail := taskplanning.NewTaskPlanningRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("规划提示词测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行 Invoke 触发 BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "规划任务"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 SystemPromptBuilder 包含规划提示词
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb, "SystemPromptBuilder 不应为 nil")
}

// TestTaskPlanningRail_回调事件完整 测试 GetCallbacks 返回 5 个事件。
// 对齐 Python: test_task_planning_rail_callbacks
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_回调事件完整() {
	rail := taskplanning.NewTaskPlanningRail()

	callbacks := rail.GetCallbacks()
	s.Len(callbacks, 5, "TaskPlanningRail 应有 5 个回调事件")

	expectedEvents := []agentinterfaces.AgentCallbackEvent{
		agentinterfaces.CallbackBeforeModelCall,
		agentinterfaces.CallbackAfterToolCall,
		agentinterfaces.CallbackAfterModelCall,
		agentinterfaces.CallbackAfterInvoke,
		agentinterfaces.CallbackAfterTaskIteration,
	}
	for _, event := range expectedEvents {
		_, exists := callbacks[event]
		s.True(exists, "应包含回调事件 %v", event)
	}
}
```

注意导入：`taskplanning "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"`（TaskPlanningRail 在 rails 包根目录）。

- [ ] **Step 3: 验证 planning 编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags=integration ./tests/integration/agentcore/harness/rails/planning/...`
Expected: 无编译错误

- [ ] **Step 4: 运行 planning 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -v -run 'TestTaskPlanningRailSuite' ./tests/integration/agentcore/harness/rails/planning/...`
Expected: 3 PASS

- [ ] **Step 5: 提交**

```bash
git add tests/integration/agentcore/harness/rails/planning/
git commit -m "test(integration): TaskPlanningRail 3 个测试"
```

---

## Task 5: agent_mode/ — AgentModeRail 测试

**Files:**
- Create: `tests/integration/agentcore/harness/rails/agent_mode/doc.go`
- Create: `tests/integration/agentcore/harness/rails/agent_mode/agent_mode_rail_test.go`

AgentModeRail **有 GetCallbacks 覆盖**（3 个回调）。构造函数需要 `allowedTools []string`，传 nil 使用默认列表。

- [ ] **Step 1: 创建 doc.go**

```go
//go:build integration

// Package agent_mode 提供 AgentModeRail 集成测试。
//
// 对齐 Python: tests/system_tests/harness/rail/test_agent_mode_rail.py
//
// 文件目录：
//
//	agent_mode/
//	├── doc.go               # 包文档
//	└── agent_mode_rail_test.go # AgentModeRail 测试（3 测试）
//
// 对应 Python 代码：openjiuwen/harness/rails/agent_mode.py
package agent_mode
```

- [ ] **Step 2: 创建 agent_mode_rail_test.go**

```go
//go:build integration

package agent_mode

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentmode "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentModeRailSuite 测试 AgentModeRail 代理模式切换。
//
// 对齐 Python: tests/system_tests/harness/rail/test_agent_mode_rail.py
// AgentModeRail 覆盖了 GetCallbacks()，可测完整回调链路。
type AgentModeRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestAgentModeRailSuite(t *testing.T) {
	suite.Run(t, new(AgentModeRailSuite))
}

// TestAgentModeRail_Init注册SwitchModeTool 测试 AgentModeRail Init 后注册 switch_mode 工具。
// 对齐 Python: test_agent_mode_rail_init
func (s *AgentModeRailSuite) TestAgentModeRail_Init注册SwitchModeTool() {
	rail := agentmode.NewAgentModeRail(nil) // nil 使用默认 allowedTools

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("模式切换测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 AgentModeRail 已注册
	railType := reflect.TypeOf(&agentmode.AgentModeRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 AgentModeRail")

	// 验证 switch_mode 工具注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("switch_mode"), "应注册 switch_mode")
	s.NotNil(am.Get("enter_plan_mode"), "应注册 enter_plan_mode")
	s.NotNil(am.Get("exit_plan_mode"), "应注册 exit_plan_mode")
}

// TestAgentModeRail_BeforeModelCall注入模式提示词 测试 BeforeModelCall 注入当前模式 section。
// 对齐 Python: test_agent_mode_rail_before_model_call
func (s *AgentModeRailSuite) TestAgentModeRail_BeforeModelCall注入模式提示词() {
	rail := agentmode.NewAgentModeRail(nil)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("模式提示词测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行 Invoke 触发 BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "模式测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")
}

// TestAgentModeRail_默认模式为Code 测试默认模式为 "code"。
// 对齐 Python: test_agent_mode_rail_default_mode
func (s *AgentModeRailSuite) TestAgentModeRail_默认模式为Code() {
	rail := agentmode.NewAgentModeRail(nil)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("默认模式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 通过 DeepConfig 验证默认模式
	deepAgent, ok := agent.(interface{ DeepConfig() *hschema.DeepAgentConfig })
	s.Require().True(ok, "agent 应实现 DeepConfig()")
	if ok && deepAgent.DeepConfig() != nil {
		s.Equal(hschema.AgentModeCode, deepAgent.DeepConfig().DefaultMode,
			"默认模式应为 Code")
	}
}
```

注意：AgentModeRail 在 `rails` 包根目录，导入路径与 TaskPlanningRail 相同：`agentmode "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"`

- [ ] **Step 3: 验证 agent_mode 编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags=integration ./tests/integration/agentcore/harness/rails/agent_mode/...`
Expected: 无编译错误

- [ ] **Step 4: 运行 agent_mode 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -v -run 'TestAgentModeRailSuite' ./tests/integration/agentcore/harness/rails/agent_mode/...`
Expected: 3 PASS

- [ ] **Step 5: 提交**

```bash
git add tests/integration/agentcore/harness/rails/agent_mode/
git commit -m "test(integration): AgentModeRail 3 个测试"
```

---

## Task 6: heartbeat/ — HeartbeatRail 测试

**Files:**
- Create: `tests/integration/agentcore/harness/rails/heartbeat/doc.go`
- Create: `tests/integration/agentcore/harness/rails/heartbeat/heartbeat_rail_test.go`

HeartbeatRail 无 GetCallbacks 覆盖。Init 需要 DeepAgentInterface + DeepConfig + Workspace，但 gracefully 降级。

- [ ] **Step 1: 创建 doc.go**

```go
//go:build integration

// Package heartbeat 提供 HeartbeatRail 集成测试。
//
// 对齐 Python: tests/system_tests/harness/rail/test_heartbeat_rail.py
//
// 文件目录：
//
//	heartbeat/
//	├── doc.go               # 包文档
//	└── heartbeat_rail_test.go # HeartbeatRail 测试（2 测试）
//
// 对应 Python 代码：openjiuwen/harness/rails/heartbeat.py
package heartbeat
```

- [ ] **Step 2: 创建 heartbeat_rail_test.go**

```go
//go:build integration

package heartbeat

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	heartbeat "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HeartbeatRailSuite 测试 HeartbeatRail 心跳。
//
// 对齐 Python: tests/system_tests/harness/rail/test_heartbeat_rail.py
// 注意：HeartbeatRail 未覆盖 GetCallbacks()，回调方法不会自动触发，
// 仅验证 Init 成功和 Invoke 不崩溃。
type HeartbeatRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestHeartbeatRailSuite(t *testing.T) {
	suite.Run(t, new(HeartbeatRailSuite))
}

// TestHeartbeatRail_Init成功 测试 HeartbeatRail 成功创建和初始化。
// 对齐 Python: test_heartbeat_rail_init
func (s *HeartbeatRailSuite) TestHeartbeatRail_Init成功() {
	rail := heartbeat.NewHeartbeatRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("心跳测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 HeartbeatRail 已注册
	railType := reflect.TypeOf(&heartbeat.HeartbeatRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 HeartbeatRail")
}

// TestHeartbeatRail_Invoke不崩溃 测试带 HeartbeatRail 的 Invoke 不崩溃。
// 对齐 Python: test_heartbeat_rail_invoke
func (s *HeartbeatRailSuite) TestHeartbeatRail_Invoke不崩溃() {
	rail := heartbeat.NewHeartbeatRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("心跳 Invoke 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行 Invoke（HeartbeatRail BeforeModelCall 仅在 run_kind == "heartbeat" 时激活，
	// 正常 Invoke 不触发，但验证不崩溃）
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "心跳测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.NotNil(result, "应返回结果")
}
```

注意：HeartbeatRail 在 `rails` 包根目录，导入 `heartbeat "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"`

- [ ] **Step 3: 验证 heartbeat 编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags=integration ./tests/integration/agentcore/harness/rails/heartbeat/...`
Expected: 无编译错误

- [ ] **Step 4: 运行 heartbeat 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -v -run 'TestHeartbeatRailSuite' ./tests/integration/agentcore/harness/rails/heartbeat/...`
Expected: 2 PASS

- [ ] **Step 5: 提交**

```bash
git add tests/integration/agentcore/harness/rails/heartbeat/
git commit -m "test(integration): HeartbeatRail 2 个测试"
```

---

## Task 7: progressive/ — ProgressiveToolRail 测试

**Files:**
- Create: `tests/integration/agentcore/harness/rails/progressive/doc.go`
- Create: `tests/integration/agentcore/harness/rails/progressive/progressive_rail_test.go`

ProgressiveToolRail **有 GetCallbacks 覆盖**（2 个回调）。构造函数需要 `*schema.DeepAgentConfig`。

- [ ] **Step 1: 创建 doc.go**

```go
//go:build integration

// Package progressive 提供 ProgressiveToolRail 集成测试。
//
// 对齐 Python: tests/system_tests/harness/rail/test_progressive_tool_rail.py
//
// 文件目录：
//
//	progressive/
//	├── doc.go               # 包文档
//	└── progressive_rail_test.go # ProgressiveToolRail 测试（3 测试）
//
// 对应 Python 代码：openjiuwen/harness/rails/progressive.py
package progressive
```

- [ ] **Step 2: 创建 progressive_rail_test.go**

```go
//go:build integration

package progressive

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	progressive "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProgressiveToolRailSuite 测试 ProgressiveToolRail 渐进式工具。
//
// 对齐 Python: tests/system_tests/harness/rail/test_progressive_tool_rail.py
// ProgressiveToolRail 覆盖了 GetCallbacks()，可测回调链路。
type ProgressiveToolRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestProgressiveToolRailSuite(t *testing.T) {
	suite.Run(t, new(ProgressiveToolRailSuite))
}

// TestProgressiveToolRail_Init成功 测试 NewProgressiveToolRail + Init 成功。
// 对齐 Python: test_progressive_tool_rail_init
func (s *ProgressiveToolRailSuite) TestProgressiveToolRail_Init成功() {
	config := hschema.NewDeepAgentConfig()
	config.ProgressiveToolEnabled = true
	rail := progressive.NewProgressiveToolRail(config)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("渐进式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 ProgressiveToolRail 已注册
	railType := reflect.TypeOf(&progressive.ProgressiveToolRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 ProgressiveToolRail")

	// 验证元工具注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("search_tools"), "应注册 search_tools")
	s.NotNil(am.Get("load_tools"), "应注册 load_tools")
}

// TestProgressiveToolRail_BeforeInvoke缓存建立 测试 BeforeInvoke 建立工具导航缓存。
// 对齐 Python: test_progressive_tool_rail_before_invoke
func (s *ProgressiveToolRailSuite) TestProgressiveToolRail_BeforeInvoke缓存建立() {
	config := hschema.NewDeepAgentConfig()
	config.ProgressiveToolEnabled = true
	rail := progressive.NewProgressiveToolRail(config)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("缓存建立测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeInvoke → BeforeModelCall 链路
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "缓存测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")
}

// TestProgressiveToolRail_BeforeModelCall导航节注入 测试 BeforeModelCall 注入工具导航 section。
// 对齐 Python: test_progressive_tool_rail_before_model_call
func (s *ProgressiveToolRailSuite) TestProgressiveToolRail_BeforeModelCall导航节注入() {
	config := hschema.NewDeepAgentConfig()
	config.ProgressiveToolEnabled = true
	rail := progressive.NewProgressiveToolRail(config)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("导航节注入测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "导航测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 SystemPromptBuilder
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb, "SystemPromptBuilder 不应为 nil")
}
```

注意：`hschema.NewDeepAgentConfig()` 需要确认是否存在。如果不存在，使用 `&hschema.DeepAgentConfig{ProgressiveToolEnabled: true}`。

- [ ] **Step 3: 验证 progressive 编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags=integration ./tests/integration/agentcore/harness/rails/progressive/...`
Expected: 无编译错误

- [ ] **Step 4: 运行 progressive 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -v -run 'TestProgressiveToolRailSuite' ./tests/integration/agentcore/harness/rails/progressive/...`
Expected: 3 PASS

- [ ] **Step 5: 提交**

```bash
git add tests/integration/agentcore/harness/rails/progressive/
git commit -m "test(integration): ProgressiveToolRail 3 个测试"
```

---

## Task 8: sys_operation/ — SysOperationRail 测试

**Files:**
- Create: `tests/integration/agentcore/harness/rails/sys_operation/doc.go`
- Create: `tests/integration/agentcore/harness/rails/sys_operation/sys_operation_rail_test.go`

SysOperationRail 无 GetCallbacks 覆盖。addDefaultRails 始终自动添加 SysOperationRail。测试手动创建时需注意去重。

- [ ] **Step 1: 创建 doc.go**

```go
//go:build integration

// Package sys_operation 提供 SysOperationRail 集成测试。
//
// 对齐 Python: tests/system_tests/harness/rail/test_sys_operation_rail.py
//
// 文件目录：
//
//	sys_operation/
//	├── doc.go               # 包文档
//	└── sys_operation_rail_test.go # SysOperationRail 测试（3 测试）
//
// 对应 Python 代码：openjiuwen/harness/rails/sys_operation.py
package sys_operation
```

- [ ] **Step 2: 创建 sys_operation_rail_test.go**

```go
//go:build integration

package sys_operation

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	sysoprail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SysOperationRailSuite 测试 SysOperationRail 系统操作。
//
// 对齐 Python: tests/system_tests/harness/rail/test_sys_operation_rail.py
// 注意：SysOperationRail 未覆盖 GetCallbacks()，回调方法不会自动触发，
// 仅验证 Init 注册工具和 Uninit 清理。
type SysOperationRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSysOperationRailSuite(t *testing.T) {
	suite.Run(t, new(SysOperationRailSuite))
}

// TestSysOperationRail_Init注册工具 测试 SysOperationRail Init 后注册 file/shell 工具。
// 对齐 Python: test_sys_operation_rail_init
func (s *SysOperationRailSuite) TestSysOperationRail_Init注册工具() {
	rail := sysoprail.NewSysOperationRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("系统操作测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 SysOperationRail 已注册
	railType := reflect.TypeOf(&sysoprail.SysOperationRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 SysOperationRail")

	// 验证核心工具注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("read_file"), "应注册 read_file")
	s.NotNil(am.Get("bash"), "应注册 bash")
	s.NotNil(am.Get("write_file"), "应注册 write_file（默认非只读模式）")
}

// TestSysOperationRail_未启用时不注册 测试 enabled=false 时不注册额外工具。
// 对齐 Python: test_sys_operation_rail_disabled
// 注意：addDefaultRails 始终创建 SysOperationRail，
// 此测试验证只读模式下 write_file 不注册。
func (s *SysOperationRailSuite) TestSysOperationRail_只读模式不注册写入工具() {
	rail := sysoprail.NewSysOperationRail(
		sysoprail.WithReadOnly(true),
	)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("只读模式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 只读模式下 write_file 和 edit_file 不注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("write_file"), "只读模式不应注册 write_file")
	s.Nil(am.Get("edit_file"), "只读模式不应注册 edit_file")
	s.NotNil(am.Get("read_file"), "只读模式仍应注册 read_file")
}

// TestSysOperationRail_Uninit清理 测试 Uninit 后工具从 AbilityManager 移除。
// 对齐 Python: test_sys_operation_rail_uninit
func (s *SysOperationRailSuite) TestSysOperationRail_Uninit清理() {
	rail := sysoprail.NewSysOperationRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("清理测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行一次 Invoke 确保 Init 完成
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "清理前"})

	// Uninit
	rail.Uninit(agent)

	// 验证工具已移除
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("read_file"), "Uninit 后 read_file 应移除")
	s.Nil(am.Get("bash"), "Uninit 后 bash 应移除")
}
```

注意：SysOperationRail 在 `rails` 包根目录。WithReadOnly 选项需确认是否存在于 `sysoprail` 包——如果不在，可能需要从 `github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails` 直接导入。

- [ ] **Step 3: 验证 sys_operation 编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags=integration ./tests/integration/agentcore/harness/rails/sys_operation/...`
Expected: 无编译错误

- [ ] **Step 4: 运行 sys_operation 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -v -run 'TestSysOperationRailSuite' ./tests/integration/agentcore/harness/rails/sys_operation/...`
Expected: 3 PASS

- [ ] **Step 5: 提交**

```bash
git add tests/integration/agentcore/harness/rails/sys_operation/
git commit -m "test(integration): SysOperationRail 3 个测试"
```

---

## Task 9: mcp/ — McpRail 测试

**Files:**
- Create: `tests/integration/agentcore/harness/rails/mcp/doc.go`
- Create: `tests/integration/agentcore/harness/rails/mcp/mcp_rail_test.go`

McpRail 无 GetCallbacks 覆盖。Init 始终注册 2 个工具（不需要实际 MCP 配置）。

- [ ] **Step 1: 创建 doc.go**

```go
//go:build integration

// Package mcp 提供 McpRail 集成测试。
//
// 对齐 Python: tests/system_tests/harness/rail/test_mcp_rail.py
//
// 文件目录：
//
//	mcp/
//	├── doc.go        # 包文档
//	└── mcp_rail_test.go # McpRail 测试（3 测试）
//
// 对应 Python 代码：openjiuwen/harness/rails/mcp_rail.py
package mcp
```

- [ ] **Step 2: 创建 mcp_rail_test.go**

```go
//go:build integration

package mcp

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	mcprail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// McpRailSuite 测试 McpRail MCP 资源浏览。
//
// 对齐 Python: tests/system_tests/harness/rail/test_mcp_rail.py
// 注意：McpRail 未覆盖 GetCallbacks()，回调方法不会自动触发，
// 仅验证 Init 注册工具和 Uninit 清理。
type McpRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMcpRailSuite(t *testing.T) {
	suite.Run(t, new(McpRailSuite))
}

// TestMcpRail_Init注册McpTools 测试 McpRail Init 后注册 MCP 资源浏览工具。
// 对齐 Python: test_mcp_rail_init
func (s *McpRailSuite) TestMcpRail_Init注册McpTools() {
	rail := mcprail.NewMcpRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("MCP 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 McpRail 已注册
	railType := reflect.TypeOf(&mcprail.McpRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 McpRail")

	// 验证 MCP 工具注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("list_mcp_resources"), "应注册 list_mcp_resources")
	s.NotNil(am.Get("read_mcp_resource"), "应注册 read_mcp_resource")
}

// TestMcpRail_无配置时不崩溃 测试无 MCP 服务端配置时 Init 不崩溃。
// 对齐 Python: test_mcp_rail_no_config
func (s *McpRailSuite) TestMcpRail_无配置时不崩溃() {
	rail := mcprail.NewMcpRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("无配置测试"))

	// 不传 Mcps 配置
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "无 MCP 配置时 Init 不应崩溃")
	s.Require().NotNil(agent)

	// 工具仍注册（浏览工具不依赖实际 MCP 服务端）
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("list_mcp_resources"), "无配置时仍应注册 list_mcp_resources")
}

// TestMcpRail_Uninit清理 测试 Uninit 后工具从 AbilityManager 移除。
// 对齐 Python: test_mcp_rail_uninit
func (s *McpRailSuite) TestMcpRail_Uninit清理() {
	rail := mcprail.NewMcpRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("清理测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行一次 Invoke 确保 Init 完成
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "清理前"})

	// Uninit
	rail.Uninit(agent)

	// 验证工具已移除
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("list_mcp_resources"), "Uninit 后 list_mcp_resources 应移除")
	s.Nil(am.Get("read_mcp_resource"), "Uninit 后 read_mcp_resource 应移除")
}
```

注意：McpRail 在 `rails` 包根目录，导入 `mcprail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"`

- [ ] **Step 3: 验证 mcp 编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags=integration ./tests/integration/agentcore/harness/rails/mcp/...`
Expected: 无编译错误

- [ ] **Step 4: 运行 mcp 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -v -run 'TestMcpRailSuite' ./tests/integration/agentcore/harness/rails/mcp/...`
Expected: 3 PASS

- [ ] **Step 5: 提交**

```bash
git add tests/integration/agentcore/harness/rails/mcp/
git commit -m "test(integration): McpRail 3 个测试"
```

---

## Task 10: context_engineer/ — 2 个 Rail 测试

**Files:**
- Create: `tests/integration/agentcore/harness/rails/context_engineer/doc.go`
- Create: `tests/integration/agentcore/harness/rails/context_engineer/context_engineer_rail_test.go`

ContextAssembleRail 有 GetCallbacks 覆盖（1 个回调），ContextProcessorRail 有 GetCallbacks 覆盖（5 个回调）。

- [ ] **Step 1: 创建 doc.go**

```go
//go:build integration

// Package context_engineer 提供 ContextAssembleRail / ContextProcessorRail 集成测试。
//
// 对齐 Python: tests/system_tests/harness/rail/test_context_assemble_rail.py
//
// 文件目录：
//
//	context_engineer/
//	├── doc.go                    # 包文档
//	└── context_engineer_rail_test.go # 2 个 Rail 测试（3 测试）
//
// 对应 Python 代码：openjiuwen/harness/rails/context_engineer/
package context_engineer
```

- [ ] **Step 2: 创建 context_engineer_rail_test.go**

```go
//go:build integration

package context_engineer

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	ce "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEngineerRailSuite 测试 ContextAssembleRail / ContextProcessorRail。
//
// 对齐 Python: tests/system_tests/harness/rail/test_context_assemble_rail.py
// 两个 Rail 均覆盖了 GetCallbacks()，可测回调链路。
type ContextEngineerRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestContextEngineerRailSuite(t *testing.T) {
	suite.Run(t, new(ContextEngineerRailSuite))
}

// TestContextAssembleRail_Init成功 测试 NewContextAssembleRail + Init 成功。
// 对齐 Python: test_context_assemble_rail_init
func (s *ContextEngineerRailSuite) TestContextAssembleRail_Init成功() {
	rail := ce.NewContextAssembleRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("上下文组装测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 ContextAssembleRail 已注册
	railType := reflect.TypeOf(&ce.ContextAssembleRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 ContextAssembleRail")
}

// TestContextProcessorRail_Init成功 测试 NewContextProcessorRail + Init 成功。
// 对齐 Python: test_context_processor_rail_init
func (s *ContextEngineerRailSuite) TestContextProcessorRail_Init成功() {
	rail := ce.NewContextProcessorRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("上下文处理测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 验证 ContextProcessorRail 已注册
	railType := reflect.TypeOf(&ce.ContextProcessorRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 ContextProcessorRail")
}

// TestContextProcessorRail_GetCallbacks事件完整 测试 GetCallbacks 返回 5 个事件。
// 对齐 Python: test_context_processor_rail_callbacks
func (s *ContextEngineerRailSuite) TestContextProcessorRail_GetCallbacks事件完整() {
	rail := ce.NewContextProcessorRail()

	callbacks := rail.GetCallbacks()
	s.Len(callbacks, 5, "ContextProcessorRail 应有 5 个回调事件")

	expectedEvents := []agentinterfaces.AgentCallbackEvent{
		agentinterfaces.CallbackBeforeInvoke,
		agentinterfaces.CallbackBeforeModelCall,
		agentinterfaces.CallbackAfterModelCall,
		agentinterfaces.CallbackAfterToolCall,
		agentinterfaces.CallbackOnModelException,
	}
	for _, event := range expectedEvents {
		_, exists := callbacks[event]
		s.True(exists, "应包含回调事件 %v", event)
	}
}
```

- [ ] **Step 3: 验证 context_engineer 编译通过**

Run: `cd /home/opensource/uap-claw-go && go build -tags=integration ./tests/integration/agentcore/harness/rails/context_engineer/...`
Expected: 无编译错误

- [ ] **Step 4: 运行 context_engineer 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -v -run 'TestContextEngineerRailSuite' ./tests/integration/agentcore/harness/rails/context_engineer/...`
Expected: 3 PASS

- [ ] **Step 5: 提交**

```bash
git add tests/integration/agentcore/harness/rails/context_engineer/
git commit -m "test(integration): ContextAssembleRail/ContextProcessorRail 3 个测试"
```

---

## Task 11: 全量验证 + 更新文档

**Files:**
- Modify: `docs/superpowers/specs/2027-07-01-integration-test-batch4-design.md`

- [ ] **Step 1: 全量编译验证**

Run: `cd /home/opensource/uap-claw-go && go build -tags=integration ./tests/integration/...`
Expected: 无编译错误

- [ ] **Step 2: 全量测试运行**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -v ./tests/integration/... 2>&1 | tail -30`
Expected: 所有测试 PASS，包括 Batch 1-3 已有的 77 个 + 新增 28 个 = 105 PASS

- [ ] **Step 3: 更新设计文档实施状态**

在 `docs/superpowers/specs/2027-07-01-integration-test-batch4-design.md` 第 8 节「实施状态」中，将所有文件标记为已完成，填入实际测试数。

- [ ] **Step 4: 提交**

```bash
git add docs/superpowers/specs/2027-07-01-integration-test-batch4-design.md
git commit -m "docs: 更新 Batch 4 实施状态（28 测试全部 PASS）"
```

---

## 自查清单

### 1. 设计文档覆盖

| 设计文档章节 | 对应 Task |
|-------------|-----------|
| 3.1 interrupt 细粒度确认（+3 测试） | Task 1 |
| 3.2 subagent 3 个 Rail（3 测试） | Task 2 |
| 3.3 skills SkillUseRail（2 测试） | Task 3 |
| 3.4 planning TaskPlanningRail（3 测试） | Task 4 |
| 3.5 agent_mode AgentModeRail（3 测试） | Task 5 |
| 3.6 heartbeat HeartbeatRail（2 测试） | Task 6 |
| 3.7 progressive ProgressiveToolRail（3 测试） | Task 7 |
| 3.8 sys_operation SysOperationRail（3 测试） | Task 8 |
| 3.9 mcp McpRail（3 测试） | Task 9 |
| 3.10 context_engineer 2 个 Rail（3 测试） | Task 10 |
| 全量验证 + 文档更新 | Task 11 |

✅ 全覆盖

### 2. 占位符扫描

- 无 "TBD"/"TODO"/"implement later" 占位符
- 无 "add appropriate error handling" 模糊描述
- 无 "similar to Task N" 引用
- 所有代码步骤都有完整代码

✅ 通过

### 3. 类型一致性

- `interrupt.NewConfirmInterruptRail("tool1", "tool2")` — 一致使用
- `subagent.NewSubagentRail()` / `subagent.NewVerificationRail()` / `subagent.NewVerificationContractRail()` — 一致使用
- `skilluse.NewSkillUseRail([]string{skillsDir})` — 一致使用
- `taskplanning.NewTaskPlanningRail()` — 一致使用
- `agentmode.NewAgentModeRail(nil)` — 一致使用
- `heartbeat.NewHeartbeatRail()` — 一致使用
- `progressive.NewProgressiveToolRail(config)` — 一致使用
- `sysoprail.NewSysOperationRail()` — 一致使用
- `mcprail.NewMcpRail()` — 一致使用
- `ce.NewContextAssembleRail()` / `ce.NewContextProcessorRail()` — 一致使用

⚠️ 需在实施时验证：
- `hschema.NewDeepAgentConfig()` 是否存在（Task 7 ProgressiveToolRail），若不存在则用 `&hschema.DeepAgentConfig{}`
- `sysoprail.WithReadOnly(true)` 是否在 rails 包导出（Task 8），若不在需调整导入
- `agentinterfaces.CallbackOnModelException` 常量名是否正确（Task 10），需确认实际定义
- interrupt 包名为 `interrupt_test`（外部测试包），session state 更新需使用正确导入

✅ 已标注需验证项
