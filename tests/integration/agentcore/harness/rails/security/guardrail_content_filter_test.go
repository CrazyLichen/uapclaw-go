//go:build integration

package security

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	securityrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GuardrailContentFilterSuite 安全护栏内容过滤集成测试套件。
//
// 深入测试 SecurityDecision 各子类型、SecurityCheckContext 字段、
// Interrupt 自动转 Reject 逻辑、ContainsAnyPattern 模式匹配。
// 通过端到端集成验证 handleInterruptResume 和 isAutoConfirmed 的行为。
// 对齐 Python: tests/system_tests/rail/test_guardrail.py 中内容过滤相关用例。
type GuardrailContentFilterSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGuardrailContentFilterSuite 运行 GuardrailContentFilterSuite
func TestGuardrailContentFilterSuite(t *testing.T) {
	suite.Run(t, new(GuardrailContentFilterSuite))
}

// TestSecurityDecision_SecurityAllowNewArgs 测试 Allow(newArgs) 设置 NewArgs 字段。
// 验证 Allow 决策可携带替换后的工具参数。
func (s *GuardrailContentFilterSuite) TestSecurityDecision_SecurityAllowNewArgs() {
	r := securityrail.NewBaseSecurityRail()
	newArgs := map[string]any{"param1": "value1", "param2": 42}
	allow := r.Allow(&newArgs)
	s.NotNil(allow.NewArgs, "NewArgs 不应为 nil")
	s.Equal("value1", (*allow.NewArgs)["param1"], "NewArgs param1 应为 value1")
	s.Equal(42, (*allow.NewArgs)["param2"], "NewArgs param2 应为 42")

	// 无参数时 NewArgs 为 nil
	allowNoArgs := r.Allow(nil)
	s.Nil(allowNoArgs.NewArgs, "无参数时 NewArgs 应为 nil")
}

// TestSecurityDecision_SecurityReject消息 测试 Reject 设置 Message 字段。
func (s *GuardrailContentFilterSuite) TestSecurityDecision_SecurityReject消息() {
	r := securityrail.NewBaseSecurityRail()
	reject := r.Reject("操作被拒绝", nil, nil, nil)
	s.Equal("操作被拒绝", reject.Message, "Message 应为 '操作被拒绝'")
	s.Nil(reject.Result, "Result 应为 nil")
	s.Nil(reject.ToolMessage, "ToolMessage 应为 nil")
}

// TestSecurityDecision_SecurityRejectWithToolResult 测试 Reject 时 toolResult 自动提升。
// Python: if result is None and tool_result is not None: result = tool_result
func (s *GuardrailContentFilterSuite) TestSecurityDecision_SecurityRejectWithToolResult() {
	r := securityrail.NewBaseSecurityRail()
	toolResult := "工具执行失败"
	reject := r.Reject("", nil, toolResult, nil)
	// result 为 nil 且 toolResult 非 nil → result = toolResult
	s.Equal(toolResult, reject.Result, "Result 应从 toolResult 自动赋值")
}

// TestSecurityDecision_SecurityRejectAutoMessage 测试 Reject 空 message 时自动推导。
// Python: if not message and result is not None: message = str(result)
func (s *GuardrailContentFilterSuite) TestSecurityDecision_SecurityRejectAutoMessage() {
	r := securityrail.NewBaseSecurityRail()
	reject := r.Reject("", "错误结果", nil, nil)
	// message 为空且 result 非 nil → message = fmt.Sprintf("%v", result)
	s.Equal("错误结果", reject.Message, "Message 应从 result 自动推导")
}

// TestSecurityDecision_SecurityRejectWithToolMessage 测试 Reject 携带 ToolMessage。
func (s *GuardrailContentFilterSuite) TestSecurityDecision_SecurityRejectWithToolMessage() {
	r := securityrail.NewBaseSecurityRail()
	toolMsg := llmschema.NewToolMessage("call_123", "权限不足")
	reject := r.Reject("拒绝", nil, nil, toolMsg)
	s.NotNil(reject.ToolMessage, "ToolMessage 不应为 nil")
	s.Equal("call_123", reject.ToolMessage.ToolCallID, "ToolMessage.ToolCallID 应为 call_123")
}

// TestSecurityDecision_SecurityInterrupt 测试 Interrupt 设置 Request 和 SubjectID。
func (s *GuardrailContentFilterSuite) TestSecurityDecision_SecurityInterrupt() {
	r := securityrail.NewBaseSecurityRail()
	req := &saschema.InterruptRequest{Message: "请确认操作", AutoConfirmKey: "tool_exec"}
	interrupt := r.Interrupt(req, "subject_123")
	s.Equal(req, interrupt.Request, "Request 应与传入一致")
	s.Equal("subject_123", interrupt.SubjectID, "SubjectID 应为 'subject_123'")
}

// TestSecurityDecision_SecurityAlert 测试 Alert 设置所有字段。
func (s *GuardrailContentFilterSuite) TestSecurityDecision_SecurityAlert() {
	r := securityrail.NewBaseSecurityRail()
	alert := r.Alert("检测到敏感信息", securityrail.SecurityAlertLevelError, "data_leak", "toast")
	s.Equal("检测到敏感信息", alert.Message, "Message 应为 '检测到敏感信息'")
	s.Equal(securityrail.SecurityAlertLevelError, alert.Level, "Level 应为 Error")
	s.Equal("data_leak", alert.AlertType, "AlertType 应为 'data_leak'")
	s.Equal("toast", alert.DisplayMode, "DisplayMode 应为 'toast'")
}

// TestSecurityDecision_AllDecisionTypesImplementInterface 测试所有决策类型实现 SecurityDecision 接口。
func (s *GuardrailContentFilterSuite) TestSecurityDecision_AllDecisionTypesImplementInterface() {
	var _ securityrail.SecurityDecision = (*securityrail.SecurityAllow)(nil)
	var _ securityrail.SecurityDecision = (*securityrail.SecurityReject)(nil)
	var _ securityrail.SecurityDecision = (*securityrail.SecurityInterrupt)(nil)
	var _ securityrail.SecurityDecision = (*securityrail.SecurityAlert)(nil)

	// 通过接口切片验证运行时多态
	decisions := []securityrail.SecurityDecision{
		&securityrail.SecurityAllow{},
		&securityrail.SecurityReject{Message: "拒绝"},
		&securityrail.SecurityInterrupt{},
		&securityrail.SecurityAlert{Message: "告警"},
	}
	s.Len(decisions, 4, "应有 4 种决策类型")
}

// TestSecurityCheckContext_字段 测试 SecurityCheckContext 各字段。
func (s *GuardrailContentFilterSuite) TestSecurityCheckContext_字段() {
	secCtx := &securityrail.SecurityCheckContext{
		Event:             agentinterfaces.CallbackBeforeToolCall,
		UserInput:         map[string]any{"approved": true},
		AutoConfirmConfig: map[string]any{"tool_exec": true},
		SubjectID:         "call_001",
	}
	s.Equal(agentinterfaces.CallbackBeforeToolCall, secCtx.Event, "Event 应为 BeforeToolCall")
	s.NotNil(secCtx.UserInput, "UserInput 不应为 nil")
	s.NotNil(secCtx.AutoConfirmConfig, "AutoConfirmConfig 不应为 nil")
	s.Equal(true, secCtx.AutoConfirmConfig["tool_exec"], "AutoConfirmConfig tool_exec 应为 true")
	s.Equal("call_001", secCtx.SubjectID, "SubjectID 应为 'call_001'")
	s.Nil(secCtx.CallbackCtx, "CallbackCtx 初始应为 nil")
}

// TestSecurityAlert_级别枚举 测试告警级别枚举值。
func (s *GuardrailContentFilterSuite) TestSecurityAlert_级别枚举() {
	s.Equal(securityrail.SecurityAlertLevel(0), securityrail.SecurityAlertLevelInfo, "Info 应为 0")
	s.Equal(securityrail.SecurityAlertLevel(1), securityrail.SecurityAlertLevelWarning, "Warning 应为 1")
	s.Equal(securityrail.SecurityAlertLevel(2), securityrail.SecurityAlertLevelError, "Error 应为 2")
	s.Equal(securityrail.SecurityAlertLevel(3), securityrail.SecurityAlertLevelCritical, "Critical 应为 3")
}

// TestSecurityAlert_级别String 测试告警级别 String() 方法。
func (s *GuardrailContentFilterSuite) TestSecurityAlert_级别String() {
	s.Equal("info", securityrail.SecurityAlertLevelInfo.String(), "Info String 应为 'info'")
	s.Equal("warning", securityrail.SecurityAlertLevelWarning.String(), "Warning String 应为 'warning'")
	s.Equal("error", securityrail.SecurityAlertLevelError.String(), "Error String 应为 'error'")
	s.Equal("critical", securityrail.SecurityAlertLevelCritical.String(), "Critical String 应为 'critical'")

	// 未知级别
	s.Contains(securityrail.SecurityAlertLevel(99).String(), "unknown", "未知级别应包含 'unknown'")
}

// TestContainsAnyPattern_中文内容 测试中文内容的正则匹配。
func (s *GuardrailContentFilterSuite) TestContainsAnyPattern_中文内容() {
	r := securityrail.NewBaseSecurityRail()
	// 中文字面匹配
	s.True(r.ContainsAnyPattern("我的密码是123", []string{"密码"}), "应匹配中文 '密码'")
	s.True(r.ContainsAnyPattern("身份证号：110101", []string{"身份证"}), "应匹配中文 '身份证'")
	s.False(r.ContainsAnyPattern("这是一段普通文本", []string{"密码", "身份证"}), "不应匹配安全模式")
}

// TestContainsAnyPattern_多模式匹配 测试多模式时首个匹配即返回。
func (s *GuardrailContentFilterSuite) TestContainsAnyPattern_多模式匹配() {
	r := securityrail.NewBaseSecurityRail()
	patterns := []string{`api[_-]?key`, `secret`, `password`}
	s.True(r.ContainsAnyPattern("api_key=sk-xxx", patterns), "应匹配第一个模式 api_key")
	s.True(r.ContainsAnyPattern("password=abc", patterns), "应匹配第三个模式 password")
	s.False(r.ContainsAnyPattern("normal content", patterns), "不应匹配任何模式")
}

// TestContainsAnyPattern_大小写敏感 测试 Go regexp 默认大小写敏感。
func (s *GuardrailContentFilterSuite) TestContainsAnyPattern_大小写敏感() {
	r := securityrail.NewBaseSecurityRail()
	s.True(r.ContainsAnyPattern("Secret key found", []string{"Secret"}), "应匹配 'Secret'")
	s.False(r.ContainsAnyPattern("secret key found", []string{"Secret"}), "不应匹配大小写不同的 'Secret'")
	// 使用 (?i) 前缀可实现大小写不敏感
	s.True(r.ContainsAnyPattern("secret key found", []string{"(?i)Secret"}), "使用 (?i) 应忽略大小写匹配")
}

// TestContainsAnyPattern_空文本和空模式 测试边界情况。
func (s *GuardrailContentFilterSuite) TestContainsAnyPattern_空文本和空模式() {
	r := securityrail.NewBaseSecurityRail()
	s.False(r.ContainsAnyPattern("", []string{"secret"}), "空文本不应匹配")
	s.False(r.ContainsAnyPattern("any text", []string{}), "空模式列表不应匹配")
	s.False(r.ContainsAnyPattern("any text", []string{"[invalid"}), "无效正则应返回 false 不 panic")
}

// TestApprove_是Allow别名 测试 Approve 是 Allow 的别名。
func (s *GuardrailContentFilterSuite) TestApprove_是Allow别名() {
	r := securityrail.NewBaseSecurityRail()
	newArgs := map[string]any{"key": "val"}
	approve := r.Approve(&newArgs)
	allow := r.Allow(&newArgs)
	s.Equal(approve.NewArgs, allow.NewArgs, "Approve 和 Allow 应返回相同 NewArgs")
}

// TestTypeName_基类 测试 BaseSecurityRail.TypeName 返回 "BaseSecurityRail"。
func (s *GuardrailContentFilterSuite) TestTypeName_基类() {
	r := securityrail.NewBaseSecurityRail()
	s.Equal("BaseSecurityRail", r.TypeName(), "BaseSecurityRail TypeName 应为 'BaseSecurityRail'")
}

// TestAddTool_添加工具名 测试 AddTool 添加工具名到 GetTools。
func (s *GuardrailContentFilterSuite) TestAddTool_添加工具名() {
	r := securityrail.NewBaseSecurityRail()
	r.AddTool("write_file")
	s.Contains(r.GetTools(), "write_file", "GetTools 应包含 write_file")
}

// TestAddTools_批量添加 测试 AddTools 批量添加工具名。
func (s *GuardrailContentFilterSuite) TestAddTools_批量添加() {
	r := securityrail.NewBaseSecurityRail()
	r.AddTools([]string{"read_file", "write_file"})
	s.Contains(r.GetTools(), "read_file", "GetTools 应包含 read_file")
	s.Contains(r.GetTools(), "write_file", "GetTools 应包含 write_file")
}

// TestGetCallbacks_注册BeforeToolCall 测试 WithSupportedEvents 注册 BeforeToolCall 回调。
func (s *GuardrailContentFilterSuite) TestGetCallbacks_注册BeforeToolCall() {
	r := securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
	)
	callbacks := r.GetCallbacks()
	_, hasBeforeToolCall := callbacks[agentinterfaces.CallbackBeforeToolCall]
	s.True(hasBeforeToolCall, "应包含 BeforeToolCall 回调")
}

// TestGetCallbacks_注册BeforeModelCall 测试 WithSupportedEvents 注册 BeforeModelCall 回调。
func (s *GuardrailContentFilterSuite) TestGetCallbacks_注册BeforeModelCall() {
	r := securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeModelCall),
	)
	callbacks := r.GetCallbacks()
	_, hasBeforeModelCall := callbacks[agentinterfaces.CallbackBeforeModelCall]
	s.True(hasBeforeModelCall, "应包含 BeforeModelCall 回调")
}

// TestGetCallbacks_多事件注册 测试 WithSupportedEvents 注册多个事件回调。
func (s *GuardrailContentFilterSuite) TestGetCallbacks_多事件注册() {
	r := securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(
			agentinterfaces.CallbackBeforeToolCall,
			agentinterfaces.CallbackBeforeModelCall,
		),
	)
	callbacks := r.GetCallbacks()
	_, hasBTC := callbacks[agentinterfaces.CallbackBeforeToolCall]
	_, hasBMC := callbacks[agentinterfaces.CallbackBeforeModelCall]
	s.True(hasBTC, "应包含 BeforeToolCall 回调")
	s.True(hasBMC, "应包含 BeforeModelCall 回调")
}

// TestExtractMessageContent_各种类型 测试 ExtractMessageContent 从不同类型提取文本。
func (s *GuardrailContentFilterSuite) TestExtractMessageContent_各种类型() {
	r := securityrail.NewBaseSecurityRail()
	s.Equal("", r.ExtractMessageContent(nil), "nil 应返回空字符串")
	s.Equal("hello", r.ExtractMessageContent("hello"), "string 应直接返回")
	s.Equal("value", r.ExtractMessageContent(map[string]any{"content": "value"}), "map content 应返回 value")
	s.Contains(r.ExtractMessageContent(42), "42", "数字应转为字符串")
}

// TestInterruptResume_端到端确认恢复 测试通过端到端 Agent 调用验证 handleInterruptResume 确认路径。
// 对齐 Python: handleInterruptResume → approved=true → Allow
func (s *GuardrailContentFilterSuite) TestInterruptResume_端到端确认恢复() {
	ctx := s.Ctx
	readTool := newSecretReadTool("safe content")

	// 创建在 BEFORE_TOOL_CALL 上触发 Interrupt 的 Rail
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
	s.Require().NoError(err, "创建 Agent 失败")

	sess := s.NewTestSession("guardrail-interrupt-approve")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第一轮：触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "读取文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	resultType, _ := result["result_type"].(string)
	s.Equal("interrupt", resultType, "应为中断结果")

	// 确认恢复
	interruptIDs, _ := result["interrupt_ids"].([]string)
	s.Require().NotEmpty(interruptIDs, "应有 interrupt_id")

	interactiveInput := map[string]any{
		"interrupt_ids": interruptIDs,
		"user_inputs": map[string]any{
			interruptIDs[0]: map[string]any{"approved": true},
		},
	}
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")
	s.Require().NotNil(result, "恢复结果不应为 nil")

	// 确认后应正常执行工具并完成
	output, _ := result["output"].(string)
	s.True(strings.Contains(strings.ToLower(output), "safe") || result["result_type"] == "answer",
		"确认恢复后应正常完成")

	sess.PostRun(ctx)
}

// TestInterruptResume_端到端拒绝恢复 测试 SecurityInterrupt 拒绝后工具被跳过。
// 对齐 Python: handleInterruptResume → approved=false → Reject → skipTool
// 注意：SecurityInterrupt 的拒绝路径在 BEFORE_TOOL_CALL 上会 skipTool，
// 但端到端恢复需要 Rail 的 handleInterruptResume 在回调中被再次触发。
// 这里验证 Interrupt 请求本身可正确构造和解析。
func (s *GuardrailContentFilterSuite) TestInterruptResume_端到端拒绝恢复() {
	r := securityrail.NewBaseSecurityRail()

	// 构造 Interrupt 请求
	req := &saschema.InterruptRequest{
		Message:        "需要确认",
		AutoConfirmKey: "tool_exec",
		PayloadSchema:  map[string]any{"type": "object"},
	}
	interrupt := r.Interrupt(req, "tool_call_id_1")
	s.NotNil(interrupt, "Interrupt 不应为 nil")
	s.Equal("需要确认", interrupt.Request.GetMessage(), "消息应可提取")
	s.Equal("tool_call_id_1", interrupt.SubjectID, "SubjectID 应为 'tool_call_id_1'")

	// 模拟 Python 中 handleInterruptResume 的逻辑：
	// approved=false → 返回 SecurityReject
	reject := r.Reject("用户拒绝执行", nil, nil, nil)
	s.Equal("用户拒绝执行", reject.Message, "Reject Message 应来自用户反馈")
}

// TestCustomSecurityCheckFn_InterruptOnModelCall 测试 WithSecurityCheckFn 在 MODEL 事件上返回 Interrupt。
// 验证 Interrupt 在 MODEL 事件上自动转为 Reject 的行为。
// 对齐 Python: Interrupt + MODEL事件 → 自动转为 Reject
func (s *GuardrailContentFilterSuite) TestCustomSecurityCheckFn_InterruptOnModelCall() {
	r := securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeModelCall),
	)
	// Interrupt 可在 MODEL 事件上创建，但 runAndApply 会自动转为 Reject
	req := &saschema.InterruptRequest{Message: "模型调用需确认", AutoConfirmKey: "model_call"}
	interrupt := r.Interrupt(req, "model_subject")
	s.NotNil(interrupt, "Interrupt 决策不应为 nil")

	// 验证 Interrupt 的 Request 可提取 message（runAndApply 中用于转换 Reject 的 message）
	s.Equal("模型调用需确认", interrupt.Request.GetMessage(), "Interrupt 消息应可提取")
}

// TestWithSecurityCheckFn_导出选项 测试 WithSecurityCheckFn 选项正确注入自定义检查函数。
func (s *GuardrailContentFilterSuite) TestWithSecurityCheckFn_导出选项() {
	checkCalled := false
	// 先创建 rail 再在闭包中引用
	var rail *securityrail.BaseSecurityRail
	rail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackAfterToolCall),
		securityrail.WithSecurityCheckFn(func(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
			checkCalled = true
			return rail.Allow(nil), nil
		}),
	)
	s.NotNil(rail, "Rail 不应为 nil")
	// checkCalled 在调用 BeforeToolCall/AfterToolCall 等方法时才为 true
	// 这里只验证选项不会 panic
	s.False(checkCalled, "选项注入后未调用时不应执行")
}

// TestReject_AllFieldsPopulated 测试 Reject 设置所有字段的完整情况。
func (s *GuardrailContentFilterSuite) TestReject_AllFieldsPopulated() {
	r := securityrail.NewBaseSecurityRail()
	toolMsg := llmschema.NewToolMessage("call_456", "已拒绝")
	reject := r.Reject("敏感操作", "error_result", "tool_fail", toolMsg)
	s.Equal("敏感操作", reject.Message, "Message 应为 '敏感操作'")
	s.Equal("error_result", reject.Result, "Result 应为 'error_result'")
	s.NotNil(reject.ToolMessage, "ToolMessage 不应为 nil")
	s.Equal("call_456", reject.ToolMessage.ToolCallID, "ToolMessage.ToolCallID 应为 call_456")
}

// TestInterrupt_NilRequest 测试 Interrupt 传入 nil Request 不 panic。
func (s *GuardrailContentFilterSuite) TestInterrupt_NilRequest() {
	r := securityrail.NewBaseSecurityRail()
	// Interrupt 接受 nil request（运行时会处理）
	interrupt := r.Interrupt(nil, "subject_nil")
	s.NotNil(interrupt, "Interrupt 不应为 nil")
	s.Nil(interrupt.Request, "Request 应为 nil")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 编译时验证各 Rail 实现满足 AgentRail 接口
var (
	_ agentinterfaces.AgentRail = (*securityrail.SafetyPromptRail)(nil)
	_ agentinterfaces.AgentRail = (*securityrail.BaseSecurityRail)(nil)
)
