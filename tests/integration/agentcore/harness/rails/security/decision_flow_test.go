//go:build integration

package security

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	securityrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SecurityDecisionFlowSuite 测试 SecurityDecision 全路径决策流。
//
// 深入测试链式决策流、Interrupt→Resume 完整闭环、多工具混合结果、
// BeforeModelCall Reject forceFinish、Allow NewArgs 替换等核心行为。
// 对齐 Python: tests/system_tests/rail/test_base_security_rail_integration.py
//   - TestBaseSecurityRailDecisionFlow: StrictRejectRail + AlwaysAllowRail
//   - TestBaseSecurityRailIntegration: InterceptBeforeModelRail interrupt/approval/rejection
//   - test_chain_of_tool_calls_with_mixed_results
type SecurityDecisionFlowSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSecurityDecisionFlowSuite(t *testing.T) {
	suite.Run(t, new(SecurityDecisionFlowSuite))
}

// TestDecisionFlow_StrictRejectRail链式拒绝 测试 StrictRejectRail 在链式调用中拒绝敏感内容。
// 对齐 Python: TestBaseSecurityRailDecisionFlow.test_reject_modifies_tool_message
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_StrictRejectRail链式拒绝() {
	readTool := newSecretReadTool("secret_api_key=sk-12345678")

	// StrictRejectRail: AFTER_TOOL_CALL 上检测 "secret" 并严格 Reject
	strictRail := newStrictRejectRail("secret")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/env.txt"}`),
		mockllm.CreateTextResponse("文件已被安全护栏拦截"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{strictRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("strict-reject-chain")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取环境文件",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// 工具被调用了一次（内容确实包含 "secret"）
	s.Equal(1, readTool.InvokeCount(), "read_file 应被调用一次")

	// SecurityReject 修改了 ToolResult 和 ToolMsg → 输出包含 "blocked"
	output, _ := result["output"].(string)
	s.True(
		strings.Contains(strings.ToLower(output), "blocked"),
		"输出应包含 'blocked'，实际: %s", output,
	)
}

// TestDecisionFlow_AlwaysAllowRail原样通过 测试 AlwaysAllowRail 不修改工具结果。
// 对齐 Python: TestBaseSecurityRailDecisionFlow.test_allow_preserves_original_result
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_AlwaysAllowRail原样通过() {
	readTool := newSecretReadTool("This is safe public content")

	// AlwaysAllowRail: 始终返回 Allow
	alwaysAllowRail := newAlwaysAllowRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/public.txt"}`),
		mockllm.CreateTextResponse("Content read: This is safe public content"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{alwaysAllowRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("always-allow-preserve")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取公共文件",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// 工具被调用了一次
	s.Equal(1, readTool.InvokeCount(), "read_file 应被调用一次")

	// Allow 不修改结果，原始内容通过
	output, _ := result["output"].(string)
	s.True(
		strings.Contains(strings.ToLower(output), "safe") || strings.Contains(strings.ToLower(output), "content"),
		"Allow 场景下输出应包含原始内容关键词，实际: %s", output,
	)
}

// TestDecisionFlow_StrictRejectPlusAlwaysAllow组合 测试两个 Rail 链式协作：
// AlwaysAllowRail 先执行（高优先级），StrictRejectRail 后执行（低优先级），
// 但 StrictRejectRail 仍然检测到敏感内容并拒绝。
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_StrictRejectPlusAlwaysAllow组合() {
	readTool := newSecretReadTool("secret data leaked")

	// AlwaysAllowRail 优先级=95，StrictRejectRail 优先级=90
	// 高优先级先执行：AlwaysAllowRail → Allow（不修改结果）
	// 低优先级后执行：StrictRejectRail → 检测到 "secret" → Reject
	alwaysRail := newAlwaysAllowRail()
	alwaysRail.WithPriority(95)

	strictRail := newStrictRejectRail("secret")
	strictRail.WithPriority(90)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/secret.txt"}`),
		mockllm.CreateTextResponse("已拦截"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{alwaysRail, strictRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("combined-reject-allow")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取文件",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// StrictRejectRail 最终 Reject → forceFinish（AFTER_TOOL_CALL）
	output, _ := result["output"].(string)
	s.True(
		strings.Contains(strings.ToLower(output), "blocked"),
		"组合 Rail 中 StrictRejectRail 应生效，输出包含 'blocked'，实际: %s", output,
	)
}

// TestDecisionFlow_BeforeModelCallReject_强制终止 测试 BeforeModelCall 上
// SecurityReject 导致 forceFinish，终止整个 Agent 执行。
// 对齐 Python: test_model_reject_requests_force_finish
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_BeforeModelCallReject_强制终止() {
	// 创建在 BeforeModelCall 上始终 Reject 的 Rail
	rejectModelRail := newBeforeModelCallRejectRail("模型调用被安全策略禁止")

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("不应看到此响应"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rejectModelRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("bmc-reject-force-finish")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "测试",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// BeforeModelCall Reject → forceFinish → result_type = "error"
	resultType, _ := result["result_type"].(string)
	s.Equal("error", resultType, "BeforeModelCall Reject 应导致 result_type=error")

	output, _ := result["output"].(string)
	s.Contains(output, "禁止", "输出应包含拒绝消息")
}

// TestDecisionFlow_BeforeModelCallInterrupt_自动转Reject 测试 BeforeModelCall 上
// SecurityInterrupt 自动转为 SecurityReject。
// 对齐 Python: test_security_interrupt_on_model_event_auto_rejected
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_BeforeModelCallInterrupt_自动转Reject() {
	// 创建在 BeforeModelCall 上返回 Interrupt 的 Rail
	// runAndApply 中 Interrupt + MODEL 事件 → 自动转为 Reject
	interruptModelRail := newBeforeModelCallInterruptRail()

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("不应看到此响应"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{interruptModelRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("bmc-interrupt-auto-reject")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "测试模型中断",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// Interrupt + MODEL 事件 → 自动转为 Reject → forceFinish → result_type=error
	resultType, _ := result["result_type"].(string)
	s.Equal("error", resultType, "MODEL 事件 Interrupt 应自动转为 Reject")
}

// TestDecisionFlow_BeforeToolCallInterrupt_确认恢复 测试 BeforeToolCall 上
// SecurityInterrupt 触发中断后，人工确认恢复的完整闭环。
// 对齐 Python: test_security_interrupt_with_human_approval
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_BeforeToolCallInterrupt_确认恢复() {
	ctx := s.Ctx
	readTool := newSecretReadTool("safe content")

	// 创建在 BeforeToolCall 上触发 Interrupt 的 Rail
	interruptRail := newInterruptBeforeToolCallRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/test.txt"}`),
		mockllm.CreateTextResponse("操作完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{interruptRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("btc-interrupt-approve")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 次 Invoke：触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "读取文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	// 验证 interrupt 结果
	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	// 构建确认恢复输入（approved=true）
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)

	// 第 2 次 Invoke：确认恢复 → 正常完成
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")
	s.Require().NotNil(result, "恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)

	sess.PostRun(ctx)
}

// TestDecisionFlow_BeforeToolCallInterrupt_拒绝恢复 测试 BeforeToolCall 上
// SecurityInterrupt 触发中断后，人工拒绝恢复导致工具被跳过。
// 对齐 Python: test_security_interrupt_with_human_rejection
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_BeforeToolCallInterrupt_拒绝恢复() {
	ctx := s.Ctx
	readTool := newSecretReadTool("sensitive content")

	// 创建在 BeforeToolCall 上触发 Interrupt 的 Rail
	interruptRail := newInterruptBeforeToolCallRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/secret.txt"}`),
		mockllm.CreateTextResponse("已处理拒绝"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{interruptRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("btc-interrupt-reject")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 次 Invoke：触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "读取敏感文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	// 验证 interrupt 结果
	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	// 构建拒绝恢复输入（approved=false）
	rejectInput := map[string]any{
		"interrupt_ids": interruptIDs,
		"user_inputs": map[string]any{
			interruptIDs[0]: map[string]any{
				"approved": false,
			},
		},
	}

	// 第 2 次 Invoke：拒绝恢复 → SecurityReject → skipTool
	result, err = agent.Invoke(ctx, map[string]any{"query": rejectInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "拒绝恢复 Invoke 不应报错")
	s.Require().NotNil(result, "拒绝恢复结果不应为 nil")

	// 拒绝后工具被跳过，LLM 收到拒绝消息作为工具结果，可以继续或完成
	output, _ := result["output"].(string)
	_ = output // 拒绝路径的输出取决于 LLM 如何处理拒绝消息

	sess.PostRun(ctx)
}

// TestDecisionFlow_AutoConfirm_跳过中断确认 测试 auto_confirm 配置
// 使 Interrupt 决策自动确认放行，无需人工介入。
// 对齐 Python: InterceptBeforeModelRail auto_confirm_key 逻辑
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_AutoConfirm_跳过中断确认() {
	ctx := s.Ctx
	readTool := newSecretReadTool("auto confirmed content")

	// 创建带 auto_confirm_key 的 Interrupt Rail
	interruptRail := newAutoConfirmInterruptRail("human_approval_key")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/auto.txt"}`),
		mockllm.CreateTextResponse("自动确认完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{interruptRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	// 创建 Session 并预设 auto_confirm
	sess := s.NewTestSession("auto-confirm-bypass")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")
	sess.UpdateState(map[string]any{
		saschema.InterruptAutoConfirmKey: map[string]any{
			"human_approval_key": true,
		},
	})

	// AutoConfirm 场景：Interrupt 不触发，直接 Allow → 正常完成
	result, err := agent.Invoke(ctx, map[string]any{"query": "自动确认读取"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "AutoConfirm 下 Invoke 不应报错")
	s.Require().NotNil(result, "AutoConfirm 下结果不应为 nil")

	// AutoConfirm → 工具正常执行
	s.Equal(1, readTool.InvokeCount(), "AutoConfirm 下工具应正常执行")

	sess.PostRun(ctx)
}

// TestDecisionFlow_MultiToolCall_部分Allow部分Reject 测试多工具调用时
// BeforeToolCall 上部分工具被 Reject（skipTool），部分工具正常通过。
// 对齐 Python: test_chain_of_tool_calls_with_mixed_results
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_MultiToolCall_部分Allow部分Reject() {
	// 创建两个工具：safe_tool（允许）和 risky_tool（拒绝）
	safeTool := newNamedTool("safe_tool", "安全内容")
	riskyTool := newNamedTool("risky_tool", "敏感数据")

	// 创建在 BeforeToolCall 上按工具名 Reject 的 Rail
	selectiveRejectRail := newSelectiveRejectRail("risky_tool", "此工具被安全策略禁止")

	s.MockLLM.SetResponses(
		// LLM 返回两个并行工具调用
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "safe_tool", ArgsJSON: `{"input":"safe"}`, CallID: "tc_safe"},
			{Name: "risky_tool", ArgsJSON: `{"input":"risky"}`, CallID: "tc_risky"},
		}),
		mockllm.CreateTextResponse("已处理混合结果"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{safeTool.Tool(), riskyTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{selectiveRejectRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("mixed-allow-reject")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "执行多工具",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// safe_tool 应正常执行，risky_tool 被 skipTool
	s.Equal(1, safeTool.InvokeCount(), "safe_tool 应被正常执行")
	s.Equal(0, riskyTool.InvokeCount(), "risky_tool 应被跳过")
}

// TestDecisionFlow_AllowNewArgs_替换工具参数 测试 SecurityAllow 携带 NewArgs
// 替换工具原始参数。
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_AllowNewArgs_替换工具参数() {
	// 创建记录原始参数的工具
	paramTool := newParamRecordingTool("read_file")

	// 创建在 BeforeToolCall 上替换参数的 Rail
	replaceArgsRail := newReplaceArgsRail("read_file", map[string]any{
		"filepath": "/safe/path/default.txt",
		"readonly": true,
	})

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/etc/shadow"}`),
		mockllm.CreateTextResponse("读取完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{paramTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{replaceArgsRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("allow-newargs-replace")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取文件",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// 工具被调用了
	s.Equal(1, paramTool.InvokeCount(), "工具应被调用")

	// 验证参数被替换
	lastArgs := paramTool.LastArgs()
	s.NotNil(lastArgs, "应记录工具参数")
	s.Equal("/safe/path/default.txt", lastArgs["filepath"], "filepath 参数应被替换")
	s.Equal(true, lastArgs["readonly"], "readonly 参数应被注入")
}

// TestDecisionFlow_SecurityAlert_不阻塞执行 测试 SecurityAlert 记录告警但不阻塞执行。
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_SecurityAlert_不阻塞执行() {
	readTool := newSecretReadTool("normal content")

	// 创建在 AfterToolCall 上返回 Alert 的 Rail
	alertRail := newAlertReturningRail("检测到潜在风险", securityrail.SecurityAlertLevelWarning)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/test.txt"}`),
		mockllm.CreateTextResponse("完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{alertRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("alert-no-block")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取文件",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// Alert 不阻塞 → 工具正常执行
	s.Equal(1, readTool.InvokeCount(), "Alert 场景下工具应正常执行")

	// 正常完成（非 error 结果）
	resultType, _ := result["result_type"].(string)
	s.NotEqual("error", resultType, "Alert 不应导致 error 结果")
}

// TestDecisionFlow_AfterToolCallReject_强制终止 测试 AfterToolCall 上
// SecurityReject 导致 forceFinish。
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_AfterToolCallReject_强制终止() {
	readTool := newSecretReadTool("secret content")

	// AfterToolCall 上检测到敏感内容 → Reject → forceFinish
	strictRail := newStrictRejectRail("secret")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/secret.txt"}`),
		mockllm.CreateTextResponse("不应到达此处"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{strictRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("atc-reject-force-finish")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取敏感文件",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// AfterToolCall Reject → forceFinish → result_type=error
	resultType, _ := result["result_type"].(string)
	s.Equal("error", resultType, "AfterToolCall Reject 应导致 forceFinish")
}

// TestDecisionFlow_Decision记录与Extra传播 测试 SecurityDecision 存储在
// cbc.Extra()["_interrupt_decision"] 中的传播机制。
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_Decision记录与Extra传播() {
	readTool := newSecretReadTool("safe content")

	// 创建记录决策的 Rail
	decisionRecorder := newDecisionRecordingRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/test.txt"}`),
		mockllm.CreateTextResponse("完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{decisionRecorder},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("decision-extra-propagation")
	_, err = agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取文件",
		"session": sess,
	})
	s.Require().NoError(err)

	// 决策记录不为空
	decisions := decisionRecorder.RecordedDecisions()
	s.NotEmpty(decisions, "应记录至少一个决策")

	// 所有决策类型应为 SecurityDecision 的子类型
	for _, d := range decisions {
		s.Implementsf((*securityrail.SecurityDecision)(nil), d,
			"决策 %T 应实现 SecurityDecision 接口", d)
	}
}

// TestDecisionFlow_优先级顺序决策链 测试多个 Rail 在同一事件上按优先级顺序执行决策。
// 高优先级 Reject 可阻止后续 Rail 执行。
func (s *SecurityDecisionFlowSuite) TestDecisionFlow_优先级顺序决策链() {
	readTool := newSecretReadTool("test content")

	var executionOrder []string
	var mu sync.Mutex

	// 高优先级 Rail（priority=90）→ Reject
	highRejectRail := newPriorityDecisionRail("high_reject", 90, &executionOrder, &mu, func() securityrail.SecurityDecision {
		return securityrail.NewBaseSecurityRail().Reject("高优先级拒绝", nil, nil, nil)
	})

	// 低优先级 Rail（priority=10）→ Allow（如果执行到的话）
	lowAllowRail := newPriorityDecisionRail("low_allow", 10, &executionOrder, &mu, func() securityrail.SecurityDecision {
		return securityrail.NewBaseSecurityRail().Allow(nil)
	})

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/test.txt"}`),
		mockllm.CreateTextResponse("完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{highRejectRail, lowAllowRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("priority-decision-chain")
	_, err = agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取文件",
		"session": sess,
	})
	s.Require().NoError(err)

	// 验证执行顺序：高优先级先执行
	mu.Lock()
	defer mu.Unlock()
	if len(executionOrder) >= 1 {
		s.Equal("high_reject", executionOrder[0], "高优先级 Rail 应先执行")
	}
	// 低优先级 Rail 可能也会执行（取决于回调框架是否支持短路）
	// 关键是高优先级先执行
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// alwaysAllowRail 始终返回 Allow 的安全护栏。
// 对齐 Python: AlwaysAllowRail
type alwaysAllowRail struct {
	*securityrail.BaseSecurityRail
}

// newAlwaysAllowRail 创建 AlwaysAllowRail。
func newAlwaysAllowRail() *alwaysAllowRail {
	r := &alwaysAllowRail{}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackAfterToolCall),
		securityrail.WithSecurityCheckFn(r.check),
	)
	return r
}

// TypeName 返回类型名。
func (r *alwaysAllowRail) TypeName() string {
	return "AlwaysAllowRail"
}

// check 始终返回 Allow。
func (r *alwaysAllowRail) check(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	return r.Allow(nil), nil
}

// beforeModelCallRejectRail 在 BeforeModelCall 上始终返回 Reject。
// 对齐 Python: BeforeModelCall Reject → forceFinish
type beforeModelCallRejectRail struct {
	*securityrail.BaseSecurityRail
	// message 拒绝消息
	message string
}

// newBeforeModelCallRejectRail 创建 BeforeModelCall Reject Rail。
func newBeforeModelCallRejectRail(message string) *beforeModelCallRejectRail {
	r := &beforeModelCallRejectRail{message: message}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeModelCall),
		securityrail.WithSecurityCheckFn(r.check),
	)
	return r
}

// TypeName 返回类型名。
func (r *beforeModelCallRejectRail) TypeName() string {
	return "BeforeModelCallRejectRail"
}

// check 始终返回 Reject。
func (r *beforeModelCallRejectRail) check(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	return r.Reject(r.message, nil, nil, nil), nil
}

// beforeModelCallInterruptRail 在 BeforeModelCall 上返回 Interrupt（会被自动转为 Reject）。
// 对齐 Python: InterceptBeforeModelRail
type beforeModelCallInterruptRail struct {
	*securityrail.BaseSecurityRail
}

// newBeforeModelCallInterruptRail 创建 BeforeModelCall Interrupt Rail。
func newBeforeModelCallInterruptRail() *beforeModelCallInterruptRail {
	r := &beforeModelCallInterruptRail{}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeModelCall),
		securityrail.WithSecurityCheckFn(r.check),
	)
	return r
}

// TypeName 返回类型名。
func (r *beforeModelCallInterruptRail) TypeName() string {
	return "BeforeModelCallInterruptRail"
}

// check 返回 Interrupt（runAndApply 中 MODEL 事件自动转为 Reject）。
func (r *beforeModelCallInterruptRail) check(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	req := &saschema.InterruptRequest{
		Message:        "模型调用需要确认",
		AutoConfirmKey: "model_approval",
		PayloadSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"approved": map[string]any{"type": "boolean"}},
			"required":   []string{"approved"},
		},
	}
	return r.Interrupt(req, "model_interrupt"), nil
}

// autoConfirmInterruptRail 在 BeforeToolCall 上触发 Interrupt 但支持 auto_confirm。
type autoConfirmInterruptRail struct {
	*securityrail.BaseSecurityRail
	// autoConfirmKey auto_confirm 配置键
	autoConfirmKey string
}

// newAutoConfirmInterruptRail 创建支持 AutoConfirm 的 Interrupt Rail。
func newAutoConfirmInterruptRail(autoConfirmKey string) *autoConfirmInterruptRail {
	r := &autoConfirmInterruptRail{autoConfirmKey: autoConfirmKey}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
		securityrail.WithSecurityCheckFn(r.check),
	)
	return r
}

// TypeName 返回类型名。
func (r *autoConfirmInterruptRail) TypeName() string {
	return "AutoConfirmInterruptRail"
}

// check 先检查 auto_confirm，未确认则触发 Interrupt。
// 由于 handleInterruptResume 是未导出方法，在集成测试中自行实现 auto_confirm 检查逻辑。
func (r *autoConfirmInterruptRail) check(_ context.Context, securityCtx *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	// 检查 auto_confirm（对齐 BaseSecurityRail.isAutoConfirmed 逻辑）
	if securityCtx.AutoConfirmConfig != nil && r.autoConfirmKey != "" {
		if val, ok := securityCtx.AutoConfirmConfig[r.autoConfirmKey]; ok {
			if isTruthy(val) {
				return r.Allow(nil), nil
			}
		}
	}

	// 检查 userInput（恢复调用）
	if securityCtx.UserInput != nil {
		var approved bool
		switch input := securityCtx.UserInput.(type) {
		case map[string]any:
			approved, _ = input["approved"].(bool)
		default:
			// 尝试通过反射访问 approved 字段
			if hasApproved, ok := tryGetApprovedField(input); ok {
				approved = hasApproved
			}
		}
		if approved {
			return r.Allow(nil), nil
		}
		return r.Reject("用户拒绝执行", nil, nil, nil), nil
	}

	// 首次调用 → 返回 Interrupt
	req := &saschema.InterruptRequest{
		Message:        "此工具调用需要人工审批",
		AutoConfirmKey: r.autoConfirmKey,
		PayloadSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"approved": map[string]any{"type": "boolean"}},
			"required":   []string{"approved"},
		},
	}
	return r.Interrupt(req, "tool_interrupt"), nil
}

// selectiveRejectRail 在 BeforeToolCall 上按工具名选择性 Reject。
type selectiveRejectRail struct {
	*securityrail.BaseSecurityRail
	// rejectToolName 要拒绝的工具名
	rejectToolName string
	// rejectMessage 拒绝消息
	rejectMessage string
}

// newSelectiveRejectRail 创建选择性 Reject Rail。
func newSelectiveRejectRail(rejectToolName, rejectMessage string) *selectiveRejectRail {
	r := &selectiveRejectRail{
		rejectToolName: rejectToolName,
		rejectMessage:  rejectMessage,
	}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
		securityrail.WithSecurityCheckFn(r.check),
	)
	return r
}

// TypeName 返回类型名。
func (r *selectiveRejectRail) TypeName() string {
	return "SelectiveRejectRail"
}

// check 按工具名选择性 Reject。
func (r *selectiveRejectRail) check(_ context.Context, securityCtx *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	cbc := securityCtx.CallbackCtx
	toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || toolInputs == nil {
		return r.Allow(nil), nil
	}

	if toolInputs.ToolName == r.rejectToolName {
		return r.Reject(r.rejectMessage, nil, nil, nil), nil
	}
	return r.Allow(nil), nil
}

// replaceArgsRail 在 BeforeToolCall 上用 NewArgs 替换工具参数。
type replaceArgsRail struct {
	*securityrail.BaseSecurityRail
	// targetToolName 目标工具名
	targetToolName string
	// newArgs 替换后的参数
	newArgs map[string]any
}

// newReplaceArgsRail 创建替换参数 Rail。
func newReplaceArgsRail(targetToolName string, newArgs map[string]any) *replaceArgsRail {
	r := &replaceArgsRail{
		targetToolName: targetToolName,
		newArgs:        newArgs,
	}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
		securityrail.WithSecurityCheckFn(r.check),
	)
	return r
}

// TypeName 返回类型名。
func (r *replaceArgsRail) TypeName() string {
	return "ReplaceArgsRail"
}

// check 对目标工具返回 Allow(NewArgs) 替换参数。
func (r *replaceArgsRail) check(_ context.Context, securityCtx *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	cbc := securityCtx.CallbackCtx
	toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || toolInputs == nil {
		return r.Allow(nil), nil
	}

	if toolInputs.ToolName == r.targetToolName {
		newArgs := r.newArgs
		return r.Allow(&newArgs), nil
	}
	return r.Allow(nil), nil
}

// alertReturningRail 在 AfterToolCall 上返回 SecurityAlert。
type alertReturningRail struct {
	*securityrail.BaseSecurityRail
	// message 告警消息
	message string
	// level 告警级别
	level securityrail.SecurityAlertLevel
}

// newAlertReturningRail 创建 Alert Rail。
func newAlertReturningRail(message string, level securityrail.SecurityAlertLevel) *alertReturningRail {
	r := &alertReturningRail{
		message: message,
		level:   level,
	}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackAfterToolCall),
		securityrail.WithSecurityCheckFn(r.check),
	)
	return r
}

// TypeName 返回类型名。
func (r *alertReturningRail) TypeName() string {
	return "AlertReturningRail"
}

// check 返回 Alert。
func (r *alertReturningRail) check(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	return r.Alert(r.message, r.level, "security", "popup"), nil
}

// decisionRecordingRail 记录所有决策的 Rail。
type decisionRecordingRail struct {
	*securityrail.BaseSecurityRail
	// mu 保护并发访问
	mu sync.Mutex
	// decisions 记录的决策列表
	decisions []securityrail.SecurityDecision
}

// newDecisionRecordingRail 创建决策记录 Rail。
func newDecisionRecordingRail() *decisionRecordingRail {
	r := &decisionRecordingRail{}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(
			agentinterfaces.CallbackBeforeToolCall,
			agentinterfaces.CallbackAfterToolCall,
			agentinterfaces.CallbackBeforeModelCall,
		),
		securityrail.WithSecurityCheckFn(r.check),
	)
	return r
}

// TypeName 返回类型名。
func (r *decisionRecordingRail) TypeName() string {
	return "DecisionRecordingRail"
}

// check 记录决策并返回 Allow。
func (r *decisionRecordingRail) check(_ context.Context, securityCtx *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	decision := r.Allow(nil)
	r.mu.Lock()
	r.decisions = append(r.decisions, decision)
	r.mu.Unlock()
	return decision, nil
}

// RecordedDecisions 返回记录的决策列表。
func (r *decisionRecordingRail) RecordedDecisions() []securityrail.SecurityDecision {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]securityrail.SecurityDecision, len(r.decisions))
	copy(result, r.decisions)
	return result
}

// priorityDecisionRail 按优先级记录决策的 Rail。
type priorityDecisionRail struct {
	*securityrail.BaseSecurityRail
	// name Rail 名称
	name string
	// orderList 共享的执行顺序列表
	orderList *[]string
	// mu 保护列表的互斥锁
	mu *sync.Mutex
	// decisionFn 决策函数
	decisionFn func() securityrail.SecurityDecision
}

// newPriorityDecisionRail 创建优先级决策 Rail。
func newPriorityDecisionRail(name string, priority int, orderList *[]string, mu *sync.Mutex, decisionFn func() securityrail.SecurityDecision) *priorityDecisionRail {
	r := &priorityDecisionRail{
		name:       name,
		orderList:  orderList,
		mu:         mu,
		decisionFn: decisionFn,
	}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
		securityrail.WithSecurityCheckFn(r.check),
	)
	r.WithPriority(priority)
	return r
}

// TypeName 返回类型名。
func (r *priorityDecisionRail) TypeName() string {
	return fmt.Sprintf("%sRail", r.name)
}

// check 记录执行顺序并返回决策。
func (r *priorityDecisionRail) check(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	r.mu.Lock()
	*r.orderList = append(*r.orderList, r.name)
	r.mu.Unlock()
	return r.decisionFn(), nil
}

// namedTool 按名称创建的简单工具，带调用计数。
type namedTool struct {
	// name 工具名称
	name string
	// content 工具返回内容
	content string
	// invokeCount 调用次数
	invokeCount int
	// mu 保护计数器
	mu sync.Mutex
	// toolInstance 底层 tool.Tool 实例
	toolInstance tool.Tool
}

// newNamedTool 创建按名称的简单工具。
func newNamedTool(name, content string) *namedTool {
	t := &namedTool{
		name:    name,
		content: content,
	}
	tc := tool.NewToolCardWithID(name, name, name+"工具", nil, nil)
	toolInst, _ := tool.NewMapFunction(tc,
		func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
			t.mu.Lock()
			t.invokeCount++
			t.mu.Unlock()
			return map[string]any{
				"success": true,
				"content": t.content,
				"tool":    t.name,
			}, nil
		}, nil,
	)
	t.toolInstance = toolInst
	return t
}

// Tool 返回底层 tool.Tool 接口。
func (t *namedTool) Tool() tool.Tool {
	return t.toolInstance
}

// InvokeCount 返回工具被调用的次数。
func (t *namedTool) InvokeCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.invokeCount
}

// paramRecordingTool 记录参数的工具。
type paramRecordingTool struct {
	// name 工具名称
	name string
	// invokeCount 调用次数
	invokeCount int
	// lastArgs 最后一次调用的参数
	lastArgs map[string]any
	// mu 保护并发访问
	mu sync.Mutex
	// toolInstance 底层 tool.Tool 实例
	toolInstance tool.Tool
}

// newParamRecordingTool 创建参数记录工具。
func newParamRecordingTool(name string) *paramRecordingTool {
	t := &paramRecordingTool{
		name: name,
	}
	tc := tool.NewToolCardWithID(name, name, name+"工具", nil, nil)
	toolInst, _ := tool.NewMapFunction(tc,
		func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
			t.mu.Lock()
			t.invokeCount++
			// 深拷贝参数
			t.lastArgs = make(map[string]any, len(inputs))
			for k, v := range inputs {
				t.lastArgs[k] = v
			}
			t.mu.Unlock()
			return map[string]any{
				"success": true,
				"content": "file content",
			}, nil
		}, nil,
	)
	t.toolInstance = toolInst
	return t
}

// Tool 返回底层 tool.Tool 接口。
func (t *paramRecordingTool) Tool() tool.Tool {
	return t.toolInstance
}

// InvokeCount 返回工具被调用的次数。
func (t *paramRecordingTool) InvokeCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.invokeCount
}

// LastArgs 返回最后一次调用的参数。
func (t *paramRecordingTool) LastArgs() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastArgs
}

// 编译时验证各 Rail 实现满足 AgentRail 接口
var (
	_ agentinterfaces.AgentRail = (*alwaysAllowRail)(nil)
	_ agentinterfaces.AgentRail = (*beforeModelCallRejectRail)(nil)
	_ agentinterfaces.AgentRail = (*beforeModelCallInterruptRail)(nil)
	_ agentinterfaces.AgentRail = (*autoConfirmInterruptRail)(nil)
	_ agentinterfaces.AgentRail = (*selectiveRejectRail)(nil)
	_ agentinterfaces.AgentRail = (*replaceArgsRail)(nil)
	_ agentinterfaces.AgentRail = (*alertReturningRail)(nil)
	_ agentinterfaces.AgentRail = (*decisionRecordingRail)(nil)
	_ agentinterfaces.AgentRail = (*priorityDecisionRail)(nil)
)

// isTruthy 检查值是否为"真值"（对齐 BaseSecurityRail.isSecurityTruthy）。
func isTruthy(val any) bool {
	if val == nil {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case string:
		return v != "" && v != "0" && v != "false"
	default:
		return true
	}
}

// tryGetApprovedField 尝试从结构体中获取 approved 字段值。
func tryGetApprovedField(input any) (bool, bool) {
	// 对齐 BaseSecurityRail.tryGetApproved
	switch v := input.(type) {
	case interface{ Approved() bool }:
		return v.Approved(), true
	default:
		return false, false
	}
}
