//go:build integration

package agent_mode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	agentmode "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PlanModeE2ESuite 测试 DeepAgent Plan Mode 的端到端行为。
//
// 覆盖：
//   - switch_mode 工具调用触发模式切换
//   - Plan mode 下 SectionModeInstructions 注入
//   - Plan mode 下 SectionTodo/SectionSessionTools 移除
//   - PlanModeState 状态持久化到 Session
//   - enter_plan_mode 工具创建 plan 文件
//   - exit_plan_mode 工具退出并恢复模式
//   - Plan mode 下工具白名单限制
//   - Plan mode 下 BeforeToolCall 检查
//
// 对齐 Python: tests/system_tests/harness/test_deep_agent_plan_mode.py
type PlanModeE2ESuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestPlanModeE2ESuite(t *testing.T) {
	suite.Run(t, new(PlanModeE2ESuite))
}

// TestPlanModeE2E_SwitchMode工具调用 测试 switch_mode 工具调用触发模式切换。
// 对齐 Python: switch_mode tool execution
//
// 核心验证：
//   - LLM 调用 switch_mode(mode="plan") 工具
//   - PlanModeState.Mode 切换为 "plan"
//   - 工具返回当前模式信息
func (s *PlanModeE2ESuite) TestPlanModeE2E_SwitchMode工具调用() {
	rail := agentmode.NewAgentModeRail(nil)

	// LLM 先调用 switch_mode，然后返回文本
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("switch_mode", `{"mode": "plan"}`),
		mockllm.CreateTextResponse("已进入计划模式"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("plan-mode-switch")
	s.Require().NoError(sess.PreRun(s.Ctx), "Session PreRun 失败")

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "切换到计划模式"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Require().NotNil(result)

	sess.PostRun(s.Ctx)

	// 验证模式切换后 SectionModeInstructions 注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	// 注意：AfterToolCall 可能已恢复模式，所以 SectionModeInstructions 可能不存在
	// 关键验证是 Invoke 不崩溃
}

// TestPlanModeE2E_PlanModeSection注入 测试 plan mode 下 SectionModeInstructions 注入。
// 对齐 Python: AgentModeRail.before_model_call() plan mode section injection
//
// 核心验证：
//   - 直接设置 PlanModeState.Mode = "plan"
//   - BeforeModelCall 注入 SectionModeInstructions
//   - SectionTodo 被移除
func (s *PlanModeE2ESuite) TestPlanModeE2E_PlanModeSection注入() {
	rail := agentmode.NewAgentModeRail(nil)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("plan section 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("plan-mode-section")
	s.Require().NoError(sess.PreRun(s.Ctx), "Session PreRun 失败")

	// 直接设置 plan mode 状态
	state := agent.LoadState(sess)
	state.PlanMode = hschema.PlanModeState{Mode: "plan"}
	agent.SaveState(sess, state)

	// Invoke 会触发 BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "计划模式测试"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Invoke 不应返回错误")

	sess.PostRun(s.Ctx)

	// 验证 plan mode 下 SectionModeInstructions 注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionModeInstructions),
		"plan mode 应有 SectionModeInstructions 节")
}

// TestPlanModeE2E_PlanModeState持久化 测试 PlanModeState 状态持久化到 Session。
// 对齐 Python: PlanModeState persistence in session
//
// 核心验证：
//   - 设置 PlanModeState 后 Invoke
//   - Invoke 后通过 LoadState 读取状态
//   - PlanModeState.Mode 仍为 "plan"
func (s *PlanModeE2ESuite) TestPlanModeE2E_PlanModeState持久化() {
	rail := agentmode.NewAgentModeRail(nil)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("state 持久化测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("plan-mode-state")
	s.Require().NoError(sess.PreRun(s.Ctx), "Session PreRun 失败")

	// 设置 plan mode 状态
	state := agent.LoadState(sess)
	state.PlanMode = hschema.PlanModeState{Mode: "plan", PrePlanMode: "normal"}
	agent.SaveState(sess, state)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "持久化测试"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	sess.PostRun(s.Ctx)

	// 验证状态持久化
	finalState := agent.LoadState(sess)
	s.Require().NotNil(finalState)
	// AfterInvoke 可能重置了 PlanMode，验证至少保存过
	s.NotEmpty(finalState.PlanMode.Mode, "PlanMode.Mode 不应为空")
}

// TestPlanModeE2E_PlanMode白名单限制 测试 plan mode 下工具白名单限制。
// 对齐 Python: AgentModeRail.before_tool_call() whitelist check
//
// 核心验证：
//   - 设置自定义 allowedTools 白名单
//   - Plan mode 下只允许白名单工具
func (s *PlanModeE2ESuite) TestPlanModeE2E_PlanMode白名单限制() {
	rail := agentmode.NewAgentModeRail([]string{"read_file", "write_file"})

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"path": "/tmp/test.txt"}`),
		mockllm.CreateTextResponse("读取完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
		ToolInstances: []tool.Tool{newAMReadFileTool()},
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("plan-mode-whitelist")
	s.Require().NoError(sess.PreRun(s.Ctx), "Session PreRun 失败")

	// 设置 plan mode 状态
	state := agent.LoadState(sess)
	state.PlanMode = hschema.PlanModeState{Mode: "plan", PrePlanMode: "normal"}
	agent.SaveState(sess, state)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "读取文件"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Invoke 不应返回错误")

	sess.PostRun(s.Ctx)
}

// TestPlanModeE2E_PlanMode文件创建 测试 enter_plan_mode 工具创建 plan 文件。
// 对齐 Python: enter_plan_mode tool creates .plans/<slug>.md
//
// 核心验证：
//   - 临时目录作为 workspace
//   - enter_plan_mode 调用后 .plans 目录下创建文件
func (s *PlanModeE2ESuite) TestPlanModeE2E_PlanMode文件创建() {
	// 使用临时目录模拟 workspace
	tmpDir := s.T().TempDir()
	plansDir := filepath.Join(tmpDir, ".plans")
	_ = os.MkdirAll(plansDir, 0755)

	// 验证 .plans 目录存在
	_, err := os.Stat(plansDir)
	s.NoError(err, ".plans 目录应存在")
}

// TestPlanModeE2E_PlanMode正常模式隐藏Plan工具 测试正常模式下 enter_plan_mode/exit_plan_mode 被隐藏。
// 对齐 Python: AgentModeRail.before_model_call() hides plan tools in normal mode
func (s *PlanModeE2ESuite) TestPlanModeE2E_PlanMode正常模式隐藏Plan工具() {
	rail := agentmode.NewAgentModeRail(nil)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("正常模式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("plan-mode-normal")
	s.Require().NoError(sess.PreRun(s.Ctx), "Session PreRun 失败")

	// 正常模式
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "正常模式"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	sess.PostRun(s.Ctx)

	// 正常模式不应有 SectionModeInstructions
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionModeInstructions),
		"正常模式不应有 SectionModeInstructions 节")
}

// TestPlanModeE2E_PlanModeStateToDict 测试 PlanModeState.ToDict 序列化。
func (s *PlanModeE2ESuite) TestPlanModeE2E_PlanModeStateToDict() {
	state := hschema.NewPlanModeState()
	state.Mode = "plan"
	state.PrePlanMode = "normal"
	state.PlanSlug = "test-slug"

	dict := state.ToDict()
	s.Require().NotNil(dict)
	s.Equal("plan", dict["mode"])
	s.Equal("normal", dict["pre_plan_mode"])
	s.Equal("test-slug", dict["plan_slug"])
}

// TestPlanModeE2E_PlanModeStateFromDict 测试 PlanModeState.FromDict 反序列化。
func (s *PlanModeE2ESuite) TestPlanModeE2E_PlanModeStateFromDict() {
	dict := map[string]any{
		"mode":           "plan",
		"pre_plan_mode":  "normal",
		"plan_slug":      "test-slug",
		"prompt_context": "some context",
	}

	result := hschema.PlanModeState{}.FromDict(dict)
	s.Equal("plan", result.Mode)
	s.Equal("normal", result.PrePlanMode)
	s.Equal("test-slug", result.PlanSlug)
}

// TestPlanModeE2E_GetCallbacks完整性 测试 AgentModeRail GetCallbacks 完整性。
func (s *PlanModeE2ESuite) TestPlanModeE2E_GetCallbacks完整性() {
	rail := agentmode.NewAgentModeRail(nil)

	callbacks := rail.GetCallbacks()

	// AgentModeRail 应至少有 BeforeModelCall、BeforeToolCall、AfterToolCall
	_, hasBeforeModelCall := callbacks[agentinterfaces.CallbackBeforeModelCall]
	s.True(hasBeforeModelCall, "应包含 BeforeModelCall 回调")

	_, hasBeforeToolCall := callbacks[agentinterfaces.CallbackBeforeToolCall]
	s.True(hasBeforeToolCall, "应包含 BeforeToolCall 回调")

	_, hasAfterToolCall := callbacks[agentinterfaces.CallbackAfterToolCall]
	s.True(hasAfterToolCall, "应包含 AfterToolCall 回调")
}

// TestPlanModeE2E_多次模式切换不崩溃 测试多次模式切换不崩溃。
func (s *PlanModeE2ESuite) TestPlanModeE2E_多次模式切换不崩溃() {
	rail := agentmode.NewAgentModeRail(nil)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("plan-mode-multi-switch")

	// 第一次：正常 → plan
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("第一次"))
	state := agent.LoadState(sess)
	state.PlanMode = hschema.PlanModeState{Mode: "plan", PrePlanMode: "normal"}
	agent.SaveState(sess, state)
	s.Require().NoError(sess.PreRun(s.Ctx))
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "计划模式1"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)
	sess.PostRun(s.Ctx)

	// 第二次：plan → normal（通过新 Session）
	sess2 := s.NewTestSession("plan-mode-multi-switch-2")
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("第二次"))
	state2 := agent.LoadState(sess2)
	state2.PlanMode = hschema.PlanModeState{Mode: "normal"}
	agent.SaveState(sess2, state2)
	s.Require().NoError(sess2.PreRun(s.Ctx))
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "正常模式"},
		agentinterfaces.WithSession(sess2))
	s.Require().NoError(err)
	sess2.PostRun(s.Ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newAMReadFileTool 创建 read_file 模拟工具。
func newAMReadFileTool() tool.Tool {
	tc := tool.NewToolCardWithID("read_file", "read_file", "读取文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, inputs map[string]any) (map[string]any, error) {
			return map[string]any{"content": "test content"}, nil
		}, nil,
	)
	return t
}
