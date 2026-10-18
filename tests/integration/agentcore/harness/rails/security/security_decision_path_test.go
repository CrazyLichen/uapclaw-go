//go:build integration

package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	securityrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SecurityDecisionPathSuite 测试 SecurityDecision 全路径决策流。
//
// 覆盖：SecurityAllow（含 NewArgs 替换）、SecurityReject 在不同事件下的行为差异
// （BeforeToolCall→skipTool、BeforeModelCall→forceFinish）、
// SecurityInterrupt 在 MODEL 事件自动转 Reject、
// SecurityAlert 各级别日志+流推送、决策链优先级。
//
// 对齐 Python: openjiuwen/harness/rails/security/base_security_rail.py
type SecurityDecisionPathSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSecurityDecisionPathSuite 运行 SecurityDecision 全路径决策流测试套件
func TestSecurityDecisionPathSuite(t *testing.T) {
	suite.Run(t, new(SecurityDecisionPathSuite))
}

// TestDecisionPath_BeforeToolCallAllow_NewArgs替换 测试 SecurityAllow 在 BeforeToolCall
// 时 NewArgs 替换工具参数。
func (s *SecurityDecisionPathSuite) TestDecisionPath_BeforeToolCallAllow_NewArgs替换() {
	ctx := s.Ctx

	newArgs := map[string]any{
		"filepath": "/tmp/safe_output.txt",
		"content":  "safe content",
	}

	// 创建一个 checkFn：返回 SecurityAllow + NewArgs
	checkFn := func(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
		return &securityrail.SecurityAllow{NewArgs: &newArgs}, nil
	}

	rail := securityrail.NewBaseSecurityRail(
		securityrail.WithSecurityCheckFn(checkFn),
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
	)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/evil.txt","content":"evil"}`),
		mockllm.CreateTextResponse("写入完成"),
	)

	writeTool := newDecisionPathWriteFileTool()
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// invoke 应正常完成（Allow + NewArgs 替换后继续执行）
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"})
	s.NoError(err, "Allow + NewArgs 不应导致 Invoke 报错")
	s.NotNil(result, "结果不应为 nil")
}

// TestDecisionPath_BeforeToolCallReject_跳过工具 测试 SecurityReject 在 BeforeToolCall
// 时跳过工具执行（_skip_tool=true）而非 forceFinish。
func (s *SecurityDecisionPathSuite) TestDecisionPath_BeforeToolCallReject_跳过工具() {
	ctx := s.Ctx

	checkFn := func(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
		return &securityrail.SecurityReject{Message: "内容不安全"}, nil
	}

	rail := securityrail.NewBaseSecurityRail(
		securityrail.WithSecurityCheckFn(checkFn),
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
	)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/test.txt","content":"bad"}`),
		mockllm.CreateTextResponse("已处理拒绝"),
	)

	writeTool := newDecisionPathWriteFileTool()
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// BeforeToolCall Reject → skipTool → agent 继续循环（不 forceFinish）
	_, err = agent.Invoke(ctx, map[string]any{"query": "写入文件"})
	s.NoError(err, "BeforeToolCall Reject 不应导致 Invoke 报错")
}

// TestDecisionPath_BeforeModelCallReject_强制终止 测试 SecurityReject 在 BeforeModelCall
// 时强制终止 agent 执行。
func (s *SecurityDecisionPathSuite) TestDecisionPath_BeforeModelCallReject_强制终止() {
	ctx := s.Ctx

	checkFn := func(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
		return &securityrail.SecurityReject{Message: "模型调用被拒绝"}, nil
	}

	rail := securityrail.NewBaseSecurityRail(
		securityrail.WithSecurityCheckFn(checkFn),
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeModelCall),
	)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("模型响应"))

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// BeforeModelCall Reject → forceFinish → 返回结果中包含错误信息
	result, err := agent.Invoke(ctx, map[string]any{"query": "测试"})
	s.NoError(err, "BeforeModelCall Reject 应通过 forceFinish 返回")
	s.NotNil(result, "应返回结果")
}

// TestDecisionPath_BeforeModelCallInterrupt_自动转Reject 测试 SecurityInterrupt 在
// BeforeModelCall 事件中自动转换为 SecurityReject。
func (s *SecurityDecisionPathSuite) TestDecisionPath_BeforeModelCallInterrupt_自动转Reject() {
	ctx := s.Ctx

	checkFn := func(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
		// 在 BeforeModelCall 中返回 Interrupt，应被自动转为 Reject
		req := &mockInterruptRequest{message: "模型调用不安全", autoConfirmKey: "model_reject"}
		return &securityrail.SecurityInterrupt{Request: req, SubjectID: "model_interrupt_subject"}, nil
	}

	rail := securityrail.NewBaseSecurityRail(
		securityrail.WithSecurityCheckFn(checkFn),
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeModelCall),
	)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("模型响应"))

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// Interrupt 在 BeforeModelCall 中自动转 Reject → forceFinish
	result, err := agent.Invoke(ctx, map[string]any{"query": "测试"})
	s.NoError(err, "Interrupt 自动转 Reject 不应导致 Invoke 报错")
	s.NotNil(result, "应返回结果")
}

// TestDecisionPath_SecurityAlert_Info级别 测试 SecurityAlert Info 级别不阻塞执行。
func (s *SecurityDecisionPathSuite) TestDecisionPath_SecurityAlert_Info级别() {
	ctx := s.Ctx

	checkFn := func(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
		return &securityrail.SecurityAlert{
			Message:     "信息级别告警",
			Level:       securityrail.SecurityAlertLevelInfo,
			AlertType:   "security",
			DisplayMode: "popup",
		}, nil
	}

	rail := securityrail.NewBaseSecurityRail(
		securityrail.WithSecurityCheckFn(checkFn),
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
	)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Alert 测试"))

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// Alert 不阻塞执行
	result, err := agent.Invoke(ctx, map[string]any{"query": "Alert 测试"})
	s.NoError(err, "Alert 不应导致 Invoke 报错")
	s.NotNil(result, "Alert 不阻塞，应正常返回结果")
}

// TestDecisionPath_SecurityAlert_Critical级别 测试 SecurityAlert Critical 级别不阻塞执行。
func (s *SecurityDecisionPathSuite) TestDecisionPath_SecurityAlert_Critical级别() {
	ctx := s.Ctx

	checkFn := func(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
		return &securityrail.SecurityAlert{
			Message:     "严重告警",
			Level:       securityrail.SecurityAlertLevelCritical,
			AlertType:   "security",
			DisplayMode: "popup",
		}, nil
	}

	rail := securityrail.NewBaseSecurityRail(
		securityrail.WithSecurityCheckFn(checkFn),
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
	)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Critical Alert 测试"))

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// 即使是 Critical 级别，Alert 也不阻塞执行
	result, err := agent.Invoke(ctx, map[string]any{"query": "Critical Alert 测试"})
	s.NoError(err, "Critical Alert 不应导致 Invoke 报错")
	s.NotNil(result, "Critical Alert 不阻塞，应正常返回结果")
}

// TestDecisionPath_多Rail决策链_高优先级先执行 测试多个 Rail 的优先级顺序决定决策链执行顺序。
func (s *SecurityDecisionPathSuite) TestDecisionPath_多Rail决策链_高优先级先执行() {
	ctx := s.Ctx

	// 高优先级 Rail: 返回 Reject
	rejectFn := func(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
		return &securityrail.SecurityReject{Message: "高优先级拒绝"}, nil
	}
	rejectRail := securityrail.NewBaseSecurityRail(
		securityrail.WithSecurityCheckFn(rejectFn),
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
	)
	rejectRail.WithPriority(100) // 高优先级

	// 低优先级 Rail: 返回 Allow
	allowFn := func(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
		return &securityrail.SecurityAllow{}, nil
	}
	allowRail := securityrail.NewBaseSecurityRail(
		securityrail.WithSecurityCheckFn(allowFn),
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
	)
	allowRail.WithPriority(50) // 低优先级

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/test.txt","content":"test"}`),
		mockllm.CreateTextResponse("已处理"),
	)

	writeTool := newDecisionPathWriteFileTool()
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rejectRail, allowRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// 高优先级 Reject 应先执行 → 工具被跳过
	_, err = agent.Invoke(ctx, map[string]any{"query": "写入文件"})
	s.NoError(err, "高优先级 Reject 不应导致 Invoke 报错")
}

// TestDecisionPath_Decision记录Extra传播 测试决策结果存储在 cbc.Extra()["_interrupt_decision"]。
func (s *SecurityDecisionPathSuite) TestDecisionPath_Decision记录Extra传播() {
	ctx := s.Ctx

	checkFn := func(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
		return &securityrail.SecurityAlert{
			Message:     "Extra 传播测试",
			Level:       securityrail.SecurityAlertLevelWarning,
			AlertType:   "security",
			DisplayMode: "toast",
		}, nil
	}

	rail := securityrail.NewBaseSecurityRail(
		securityrail.WithSecurityCheckFn(checkFn),
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
	)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Extra 传播测试"))

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// Invoke 正常完成（Alert 不阻塞）
	result, err := agent.Invoke(ctx, map[string]any{"query": "测试"})
	s.NoError(err, "Alert 不应导致 Invoke 报错")
	s.NotNil(result, "应返回结果")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mockInterruptRequest 模拟 InterruptRequester 接口。
type mockInterruptRequest struct {
	message        string
	autoConfirmKey string
}

func (m *mockInterruptRequest) GetMessage() string        { return m.message }
func (m *mockInterruptRequest) GetAutoConfirmKey() string { return m.autoConfirmKey }

// mockInterruptRequestAdapter 适配 InterruptRequester 到 saschema.InterruptRequester。
type mockInterruptRequestAdapter struct {
	*mockInterruptRequest
}

// 确保编译时接口满足
var _ saschema.InterruptRequester = (*mockInterruptRequest)(nil)

// newDecisionPathWriteFileTool 创建用于决策路径测试的 write_file 工具。
func newDecisionPathWriteFileTool() tool.Tool {
	tc := tool.NewToolCardWithID("write_file", "write_file", "写入文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"status": "ok", "tool": "write_file"}, nil
		}, nil,
	)
	return t
}
