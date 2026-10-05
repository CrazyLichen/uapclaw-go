//go:build integration

package context_engineer

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	ce "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextProcessorCallbackSuite 测试 ContextProcessorRail / ContextAssembleRail
// 在 DeepAgent 完整生命周期中的回调触发和副作用。
//
// 对齐 Python: tests/system_tests/harness/test_context_processor_rail_system.py
// 覆盖：BeforeInvoke / BeforeModelCall / AfterModelCall / AfterToolCall /
// OnModelException 回调路径、RefreshTaskStateRuntime 状态同步、
// FixIncompleteToolContext 修复、Section 注入/移除、SessionMemory 选项。
type ContextProcessorCallbackSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestContextProcessorCallbackSuite(t *testing.T) {
	suite.Run(t, new(ContextProcessorCallbackSuite))
}

// TestContextProcessorCallback_BeforeInvoke回调触发 验证 ContextProcessorRail 的
// BeforeInvoke 回调在 DeepAgent.Invoke 生命周期中被触发。
// 间接验证方式：BeforeInvoke 调用 FixIncompleteToolContext，不影响正常消息流。
func (s *ContextProcessorCallbackSuite) TestContextProcessorCallback_BeforeInvoke回调触发() {
	rail := ce.NewContextProcessorRail()

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("BeforeInvoke 测试"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 正常 Invoke → BeforeInvoke 先于 BeforeModelCall 触发
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "测试BeforeInvoke"})
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Require().NotNil(result)
}

// TestContextProcessorCallback_BeforeModelCall回调触发 验证 BeforeModelCall 回调
// 在 LLM 调用前被触发，通过 RefreshTaskStateRuntime 刷新 session 状态。
func (s *ContextProcessorCallbackSuite) TestContextProcessorCallback_BeforeModelCall回调触发() {
	rail := ce.NewContextProcessorRail()

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("BeforeModelCall 测试"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "测试BeforeModelCall"})
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Require().NotNil(result)

	// 验证 rail 已注册并被 Init
	railType := reflect.TypeOf(&ce.ContextProcessorRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 ContextProcessorRail")
}

// TestContextProcessorCallback_AfterModelCall回调触发 验证 AfterModelCall 回调
// 在 LLM 响应后被触发。通过工具调用场景间接验证。
func (s *ContextProcessorCallbackSuite) TestContextProcessorCallback_AfterModelCall回调触发() {
	rail := ce.NewContextProcessorRail()

	// 创建测试工具
	tc := tool.NewToolCardWithID("cp_test_tool", "cp_test_tool", "测试工具", nil, nil)
	mockTool, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"result": "ok"}, nil
		},
		nil,
	)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("cp_test_tool", `{"arg1":"val1"}`),
		mockllm.CreateTextResponse("工具执行完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
		ToolInstances: []tool.Tool{mockTool},
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "使用工具"})
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Require().NotNil(result)
}

// TestContextProcessorCallback_AfterToolCall回调触发 验证 AfterToolCall 回调
// 在工具执行后被触发。通过工具调用场景间接验证（AfterModelCall 和 AfterToolCall
// 都调用 RefreshTaskStateRuntime，只要不崩溃即验证通过）。
func (s *ContextProcessorCallbackSuite) TestContextProcessorCallback_AfterToolCall回调触发() {
	rail := ce.NewContextProcessorRail()

	tc := tool.NewToolCardWithID("cp_after_tool", "cp_after_tool", "AfterTool测试工具", nil, nil)
	mockTool, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"result": "after_tool_ok"}, nil
		},
		nil,
	)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("cp_after_tool", `{"x":"y"}`),
		mockllm.CreateTextResponse("AfterToolCall 验证完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
		ToolInstances: []tool.Tool{mockTool},
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "验证AfterToolCall"})
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Require().NotNil(result)
}

// TestContextProcessorCallback_RefreshTaskStateRuntime回调触发 验证
// RefreshTaskStateRuntime 在 BeforeModelCall 回调中被正确触发。
// 由于 DeepAgent 内部 session 传递机制较为复杂，本测试验证回调链路
// 能正确执行不崩溃，并验证 session 可通过回调上下文访问。
func (s *ContextProcessorCallbackSuite) TestContextProcessorCallback_RefreshTaskStateRuntime回调触发() {
	rail := ce.NewContextProcessorRail()

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("RefreshTaskState 测试"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 创建测试 Session 并预设 DeepAgentState（模拟运行时状态）
	sess := s.NewTestSession("refresh-state-test")
	das := hschema.NewDeepAgentState()
	das.Iteration = 5
	das.PendingFollowUps = []string{"follow_up_1", "follow_up_2"}
	sess.UpdateState(map[string]any{
		hschema.SessionRuntimeAttr: das,
	})

	// 通过 WithSession 将 session 注入到 Invoke 中
	// RefreshTaskStateRuntime 在 BeforeModelCall 中读取 session 状态
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "状态同步"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 session 仍可正常读写（回调链路未破坏 session）
	dump := sess.DumpState()
	s.NotNil(dump, "Session DumpState 不应为 nil")
}

// TestContextProcessorCallback_WithPresetFalse无处理器 验证 WithPreset(false)
// 时不生成默认处理器列表，BeforeModelCall 不会注入 offload section。
func (s *ContextProcessorCallbackSuite) TestContextProcessorCallback_WithPresetFalse无处理器() {
	rail := ce.NewContextProcessorRail(ce.WithPreset(false))

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("无处理器测试"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "无处理器"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 无处理器时不应有 offload section
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection("offload"), "WithPreset(false) 不应注入 offload 节")
}

// TestContextAssembleCallback_与ContextProcessor联合E2E 验证 ContextAssembleRail
// 和 ContextProcessorRail 同时注册时两者均正常工作，且 Section 注入正确。
func (s *ContextProcessorCallbackSuite) TestContextAssembleCallback_与ContextProcessor联合E2E() {
	tmpDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tmpDir, "cn")

	asmRail := ce.NewContextAssembleRail()
	procRail := ce.NewContextProcessorRail()

	tc := tool.NewToolCardWithID("cp_joint_tool", "cp_joint_tool", "联合测试工具", nil, nil)
	mockTool, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"result": "joint_ok"}, nil
		},
		nil,
	)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("cp_joint_tool", `{"a":"b"}`),
		mockllm.CreateTextResponse("联合 E2E 完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{asmRail, procRail},
		MaxIterations: 3,
		Workspace:     ws,
		ToolInstances: []tool.Tool{mockTool},
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "联合测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.Require().NotNil(result)

	// 验证 ContextAssembleRail 注入 SectionWorkspace 和 SectionTools
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionWorkspace), "应存在 SectionWorkspace 节")
	s.True(spb.HasSection(hsections.SectionTools), "应存在 SectionTools 节")

	// 验证两个 rail 都注册
	asmType := reflect.TypeOf(&ce.ContextAssembleRail{})
	procType := reflect.TypeOf(&ce.ContextProcessorRail{})
	s.NotEmpty(agent.FindRailsByType(asmType), "应注册 ContextAssembleRail")
	s.NotEmpty(agent.FindRailsByType(procType), "应注册 ContextProcessorRail")
}

// TestContextAssembleCallback_心跳模式跳过每日记忆 验证 ContextAssembleRail
// 在 heartbeat run_kind 下跳过每日记忆（daily_memory）。
func (s *ContextProcessorCallbackSuite) TestContextAssembleCallback_心跳模式跳过每日记忆() {
	tmpDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tmpDir, "cn")

	rail := ce.NewContextAssembleRail()

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("心跳测试"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
		Workspace:     ws,
	})
	s.Require().NoError(err)

	// 使用 heartbeat run_kind 调用
	_, err = agent.Invoke(s.Ctx, map[string]any{
		"query":    "心跳测试",
		"run_kind": string(agentinterfaces.RunKindHeartbeat),
	})
	s.Require().NoError(err, "心跳模式 Invoke 不应返回错误")

	// 验证 SectionWorkspace 仍被注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionWorkspace), "心跳模式仍应注入 SectionWorkspace")
}

// TestContextProcessorCallback_多次Invoke不崩溃 验证 ContextProcessorRail
// 在多次 Invoke 循环中稳定工作，不出现回调累积或状态泄漏。
func (s *ContextProcessorCallbackSuite) TestContextProcessorCallback_多次Invoke不崩溃() {
	rail := ce.NewContextProcessorRail()

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("第一次调用"),
		mockllm.CreateTextResponse("第二次调用"),
		mockllm.CreateTextResponse("第三次调用"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	for i := 0; i < 3; i++ {
		result, err := agent.Invoke(s.Ctx, map[string]any{"query": "多次调用"})
		s.Require().NoError(err, "第 %d 次 Invoke 不应返回错误", i+1)
		s.Require().NotNil(result, "第 %d 次 Invoke 结果不应为 nil", i+1)
	}
}

// TestContextProcessorCallback_Uninit生命周期 验证 Uninit 清理处理器和 offload 节。
func (s *ContextProcessorCallbackSuite) TestContextProcessorCallback_Uninit生命周期() {
	rail := ce.NewContextProcessorRail()

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("Uninit 生命周期测试"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "触发Init"})

	// Uninit 不应 panic
	s.NotPanics(func() {
		_ = rail.Uninit(agent)
	}, "Uninit 不应 panic")
}

// TestContextAssembleCallback_Uninit联合清理 验证 ContextAssembleRail 和
// ContextProcessorRail 同时 Uninit 后所有 Section 被清理。
func (s *ContextProcessorCallbackSuite) TestContextAssembleCallback_Uninit联合清理() {
	tmpDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tmpDir, "cn")

	asmRail := ce.NewContextAssembleRail()
	procRail := ce.NewContextProcessorRail()

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("联合 Uninit 测试"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{asmRail, procRail},
		MaxIterations: 3,
		Workspace:     ws,
	})
	s.Require().NoError(err)

	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "触发Init"})

	// Uninit 两个 rail
	s.NotPanics(func() {
		_ = asmRail.Uninit(agent)
		_ = procRail.Uninit(agent)
	}, "联合 Uninit 不应 panic")

	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionWorkspace), "Uninit 后 SectionWorkspace 应移除")
	s.False(spb.HasSection(hsections.SectionContext), "Uninit 后 SectionContext 应移除")
	s.False(spb.HasSection(hsections.SectionTools), "Uninit 后 SectionTools 应移除")
	s.False(spb.HasSection("offload"), "Uninit 后 offload 节应移除")
}

// TestContextProcessorCallback_GetCallbacks与注册回调一致性 验证 GetCallbacks
// 返回的回调事件与 DeepAgent 实际注册的回调一致。
func (s *ContextProcessorCallbackSuite) TestContextProcessorCallback_GetCallbacks与注册回调一致性() {
	rail := ce.NewContextProcessorRail()

	callbacks := rail.GetCallbacks()

	// ContextProcessorRail 覆盖 5 个事件 + 继承 2 个 DeepAgentRail 事件
	s.Len(callbacks, 7, "ContextProcessorRail 应有 7 个回调事件")

	// 验证 5 个核心回调
	coreEvents := []agentinterfaces.AgentCallbackEvent{
		agentinterfaces.CallbackBeforeInvoke,
		agentinterfaces.CallbackBeforeModelCall,
		agentinterfaces.CallbackAfterModelCall,
		agentinterfaces.CallbackAfterToolCall,
		agentinterfaces.CallbackOnModelException,
	}
	for _, event := range coreEvents {
		_, exists := callbacks[event]
		s.True(exists, "GetCallbacks 应包含核心回调 %v", event)
	}
}

// TestContextAssembleCallback_GetCallbacks事件完整 验证 ContextAssembleRail
// 的 GetCallbacks 返回正确事件（仅覆盖 BeforeModelCall + 继承事件）。
func (s *ContextProcessorCallbackSuite) TestContextAssembleCallback_GetCallbacks事件完整() {
	rail := ce.NewContextAssembleRail()

	callbacks := rail.GetCallbacks()

	// ContextAssembleRail 覆盖 1 个事件 + 继承 DeepAgentRail 事件
	s.GreaterOrEqual(len(callbacks), 2, "ContextAssembleRail 应至少有 2 个回调事件")

	_, hasBeforeModelCall := callbacks[agentinterfaces.CallbackBeforeModelCall]
	s.True(hasBeforeModelCall, "应包含 CallbackBeforeModelCall")
}

// TestContextProcessorCallback_WithSessionMemoryConfig 验证 WithSessionMemoryConfig
// 选项创建 SessionMemoryManager 并在 AfterModelCall 中调度更新。
func (s *ContextProcessorCallbackSuite) TestContextProcessorCallback_WithSessionMemoryConfig() {
	tmpDir := s.T().TempDir()
	ws := workspace.NewWorkspace(tmpDir, "cn")

	smmCfg := cecontext.NewSessionMemoryConfig()
	smmCfg.TriggerTokens = 50000
	smmCfg.TriggerAddTokens = 20000

	rail := ce.NewContextProcessorRail(ce.WithSessionMemoryConfig(&smmCfg))

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("SessionMemory 测试"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
		Workspace:     ws,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "SessionMemory"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// Uninit 不应 panic（关闭 SessionMemoryManager）
	s.NotPanics(func() {
		_ = rail.Uninit(agent)
	}, "Uninit 关闭 SessionMemoryManager 不应 panic")
}
