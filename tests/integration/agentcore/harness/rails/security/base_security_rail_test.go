//go:build integration

package security

import (
	"context"
	"strings"
	"sync"
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

// BaseSecurityRailSuite 测试 BaseSecurityRail 安全护栏全生命周期。
//
// 对齐 Python: tests/system_tests/rail/test_base_security_rail_integration.py
type BaseSecurityRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestBaseSecurityRailSuite(t *testing.T) {
	suite.Run(t, new(BaseSecurityRailSuite))
}

// TestSecurityReject_修改工具结果 测试 SecurityReject 在 AFTER_TOOL_CALL 事件中修改工具结果。
// 对齐 Python: test_security_reject_modifies_tool_result
func (s *BaseSecurityRailSuite) TestSecurityReject_修改工具结果() {
	// 创建返回敏感内容的 read_file 工具
	readTool := newSecretReadTool("this contains secret data")

	// 创建 RejectToolResultRail：当工具结果包含 "secret" 时返回 SecurityReject
	rejectRail := newRejectToolResultRail("secret")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/test.txt"}`),
		mockllm.CreateTextResponse("结果已审查"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{rejectRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("security-reject-test")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取文件",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// 工具被调用了一次
	s.Equal(1, readTool.InvokeCount(), "read_file 应被调用一次")

	// 输出包含 "blocked"（小写），说明 SecurityReject 修改了工具结果
	output, _ := result["output"].(string)
	s.True(
		strings.Contains(strings.ToLower(output), "blocked"),
		"输出应包含 'blocked'，实际: %s", output,
	)
}

// TestSecurityAllow_原样传递工具结果 测试 SecurityAllow 不修改工具结果。
// 对齐 Python: test_security_allow_passes_tool_result
func (s *BaseSecurityRailSuite) TestSecurityAllow_原样传递工具结果() {
	// 创建返回安全内容的 read_file 工具
	readTool := newSecretReadTool("normal safe content")

	// 创建 RejectToolResultRail：当工具结果包含 "secret" 时返回 SecurityReject
	// 安全内容不含 "secret"，所以会返回 SecurityAllow
	rejectRail := newRejectToolResultRail("secret")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/test.txt"}`),
		mockllm.CreateTextResponse("safe content"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{rejectRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("security-allow-test")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取文件",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// 工具被调用了一次
	s.Equal(1, readTool.InvokeCount(), "read_file 应被调用一次")

	// 输出包含原始内容关键词
	output, _ := result["output"].(string)
	s.True(
		strings.Contains(strings.ToLower(output), "safe") || strings.Contains(strings.ToLower(output), "content"),
		"输出应包含 'safe' 或 'content'，实际: %s", output,
	)
}

// TestMultiEventRail_记录所有订阅事件 测试 Rail 在一个 invoke 周期内接收到所有订阅事件的回调。
// 对齐 Python: test_multi_event_rail_records_all_events
func (s *BaseSecurityRailSuite) TestMultiEventRail_记录所有订阅事件() {
	readTool := newSecretReadTool("hello world")

	// 创建监听 BEFORE_INVOKE + BEFORE_MODEL_CALL + AFTER_TOOL_CALL 的多事件 Rail
	multiRail := newMultiEventRecordingRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/test.txt"}`),
		mockllm.CreateTextResponse("完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{multiRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("multi-event-test")
	_, err = agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取文件",
		"session": sess,
	})
	s.Require().NoError(err)

	// 验证 Rail 接收到了所有三个事件
	events := multiRail.RecordedEvents()
	s.Contains(events, agentinterfaces.CallbackBeforeInvoke, "应接收到 BEFORE_INVOKE 事件")
	s.Contains(events, agentinterfaces.CallbackBeforeModelCall, "应接收到 BEFORE_MODEL_CALL 事件")
	s.Contains(events, agentinterfaces.CallbackAfterToolCall, "应接收到 AFTER_TOOL_CALL 事件")
}

// TestPriorityOrdering_高优先级先执行 测试高优先级 Rail 在同一事件上先于低优先级 Rail 执行。
// 对齐 Python: test_priority_ordering_high_runs_before_low
func (s *BaseSecurityRailSuite) TestPriorityOrdering_高优先级先执行() {
	readTool := newSecretReadTool("hello world")

	var orderList []string
	var mu sync.Mutex

	// 高优先级 Rail（priority=90，BaseSecurityRail 默认优先级）
	highRail := newPriorityRecordingRail("high_priority", 90, &orderList, &mu)
	// 低优先级 Rail（priority=10）
	lowRail := newPriorityRecordingRail("low_priority", 10, &orderList, &mu)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/test.txt"}`),
		mockllm.CreateTextResponse("完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{highRail, lowRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("priority-test")
	_, err = agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取文件",
		"session": sess,
	})
	s.Require().NoError(err)

	// 验证执行顺序：高优先级先于低优先级
	mu.Lock()
	defer mu.Unlock()
	if len(orderList) >= 2 {
		s.Equal("high_priority", orderList[0], "高优先级 Rail 应先执行")
		s.Equal("low_priority", orderList[1], "低优先级 Rail 应后执行")
	} else {
		s.Fail("应至少记录两个 Rail 的执行，实际: %v", orderList)
	}
}

// TestSecurityInterrupt_BEFORE_TOOL_CALL 测试 SecurityInterrupt 在 BEFORE_TOOL_CALL 事件上触发中断。
// 对齐 Python: test_security_interrupt_with_human_approval（Go 端 INTERRUPT 不支持 MODEL 事件，
// 改测 TOOL 事件上的中断）
func (s *BaseSecurityRailSuite) TestSecurityInterrupt_BEFORE_TOOL_CALL() {
	readTool := newSecretReadTool("hello world")

	// 创建在 BEFORE_TOOL_CALL 上触发 Interrupt 的 Rail
	interruptRail := newInterruptBeforeToolCallRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/test.txt"}`),
		mockllm.CreateTextResponse("完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{interruptRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("interrupt-test")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取文件",
		"session": sess,
	})
	// Interrupt 会导致 abort，不会返回普通结果
	// 关键验证：agent 检测到了 interrupt 而非正常完成
	s.NotNil(result)
	// result_type 应为 "interrupt"
	resultType, _ := result["result_type"].(string)
	s.Equal("interrupt", resultType, "应为中断结果")
}

// TestReject_同时修改ToolResult和ToolMsg 测试 SecurityReject 同时修改 ToolResult 和 ToolMsg。
// 对齐 Python: test_reject_modifies_tool_message
func (s *BaseSecurityRailSuite) TestReject_同时修改ToolResult和ToolMsg() {
	readTool := newSecretReadTool("secret_api_key=12345")

	// 创建 StrictRejectRail：在 AFTER_TOOL_CALL 上严格检测 "secret" 并 Reject
	strictRail := newStrictRejectRail("secret")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/test.txt"}`),
		mockllm.CreateTextResponse("结果已审查"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{strictRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("strict-reject-test")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取文件",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// 工具被调用了一次
	s.Equal(1, readTool.InvokeCount(), "read_file 应被调用一次")

	// SecurityReject 在 AFTER_TOOL_CALL 上触发 forceFinish
	output, _ := result["output"].(string)
	s.True(
		strings.Contains(strings.ToLower(output), "blocked"),
		"输出应包含 'blocked'，实际: %s", output,
	)
}

// TestCustomPattern_不匹配时Allow 测试自定义模式的 Reject Rail：内容不匹配模式时返回 Allow。
// 对齐 Python: test_chain_of_tool_calls_with_mixed_results
func (s *BaseSecurityRailSuite) TestCustomPattern_不匹配时Allow() {
	readTool := newSecretReadTool("normal content without the pattern")

	// 创建自定义模式 "blocked_pattern" 的 RejectToolResultRail
	// 工具返回的内容不包含 "blocked_pattern"，所以应为 Allow
	rejectRail := newRejectToolResultRail("blocked_pattern")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"filepath":"/tmp/test.txt"}`),
		mockllm.CreateTextResponse("完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{readTool.Tool()},
		Rails:         []agentinterfaces.AgentRail{rejectRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("custom-pattern-test")
	result, err := agent.Invoke(s.Ctx, map[string]any{
		"query":   "读取文件",
		"session": sess,
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)

	// 工具被调用了一次
	s.Equal(1, readTool.InvokeCount(), "read_file 应被调用一次")

	// 输出不包含 "blocked"（因为 Allow，原始内容通过）
	output, _ := result["output"].(string)
	s.False(
		strings.Contains(strings.ToLower(output), "blocked"),
		"Allow 场景下输出不应包含 'blocked'，实际: %s", output,
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// secretReadTool 返回预设内容的 read_file 工具，带调用计数。
type secretReadTool struct {
	// content 工具返回的内容
	content string
	// invokeCount 调用次数
	invokeCount int
	// mu 保护计数器
	mu sync.Mutex
	// toolInstance 底层 tool.Tool 实例
	toolInstance tool.Tool
}

// newSecretReadTool 创建返回预设内容的 read_file 工具。
func newSecretReadTool(content string) *secretReadTool {
	t := &secretReadTool{
		content: content,
	}
	tc := tool.NewToolCardWithID("read_file", "read_file", "读取文件", nil, nil)
	toolInst, _ := tool.NewMapFunction(tc,
		func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
			t.mu.Lock()
			t.invokeCount++
			t.mu.Unlock()
			filepath, _ := inputs["filepath"].(string)
			return map[string]any{
				"success":  true,
				"content":  t.content,
				"filepath": filepath,
			}, nil
		}, nil,
	)
	t.toolInstance = toolInst
	return t
}

// Tool 返回底层 tool.Tool 接口。
func (t *secretReadTool) Tool() tool.Tool {
	return t.toolInstance
}

// InvokeCount 返回工具被调用的次数。
func (t *secretReadTool) InvokeCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.invokeCount
}

// rejectToolResultRail 在 AFTER_TOOL_CALL 上检测工具结果是否包含指定模式。
// 包含时返回 SecurityReject（修改工具结果），否则返回 SecurityAllow。
// 对齐 Python: RejectToolResultRail
type rejectToolResultRail struct {
	*securityrail.BaseSecurityRail
	// pattern 检测模式
	pattern string
}

// newRejectToolResultRail 创建 RejectToolResultRail。
func newRejectToolResultRail(pattern string) *rejectToolResultRail {
	r := &rejectToolResultRail{
		pattern: pattern,
	}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackAfterToolCall),
		securityrail.WithSecurityCheckFn(r.check),
	)
	return r
}

// TypeName 返回类型名。
func (r *rejectToolResultRail) TypeName() string {
	return "RejectToolResultRail"
}

// check 安全检查逻辑。
func (r *rejectToolResultRail) check(_ context.Context, securityCtx *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	cbc := securityCtx.CallbackCtx
	toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || toolInputs == nil {
		return r.Allow(nil), nil
	}

	// 从 ToolResult 中提取内容
	var content string
	if toolInputs.ToolResult != nil {
		switch v := toolInputs.ToolResult.(type) {
		case string:
			content = v
		case map[string]any:
			if c, ok2 := v["content"]; ok2 {
				content, _ = c.(string)
			}
		}
	}

	if strings.Contains(strings.ToLower(content), r.pattern) {
		return r.Reject(
			"Blocked: detected '"+r.pattern+"' in tool result",
			nil, // result
			nil, // toolResult
			nil, // toolMessage
		), nil
	}
	return r.Allow(nil), nil
}

// strictRejectRail 在 AFTER_TOOL_CALL 上严格检测并 Reject，同时修改 ToolResult 和 ToolMsg。
// 对齐 Python: StrictRejectRail
type strictRejectRail struct {
	*securityrail.BaseSecurityRail
	// pattern 检测模式
	pattern string
}

// newStrictRejectRail 创建 StrictRejectRail。
func newStrictRejectRail(pattern string) *strictRejectRail {
	r := &strictRejectRail{
		pattern: pattern,
	}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackAfterToolCall),
		securityrail.WithSecurityCheckFn(r.check),
	)
	return r
}

// check 安全检查逻辑：严格检测后同时修改 ToolResult 和 ToolMsg。
func (r *strictRejectRail) check(_ context.Context, securityCtx *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	cbc := securityCtx.CallbackCtx
	toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || toolInputs == nil {
		return r.Allow(nil), nil
	}

	// 从 ToolResult 中提取内容
	var content string
	if toolInputs.ToolResult != nil {
		switch v := toolInputs.ToolResult.(type) {
		case string:
			content = v
		case map[string]any:
			if c, ok2 := v["content"]; ok2 {
				content, _ = c.(string)
			}
		}
	}

	if strings.Contains(strings.ToLower(content), r.pattern) {
		errorMsg := "Sensitive data blocked"
		// 修改 ToolResult 和 ToolMsg（对齐 Python 的 apply_security_decision）
		toolInputs.ToolResult = errorMsg
		if toolInputs.ToolCall != nil {
			toolInputs.ToolMsg = llmschema.NewToolMessage(toolInputs.ToolCall.ID, errorMsg)
		}
		return r.Reject(errorMsg, nil, nil, nil), nil
	}
	return r.Allow(nil), nil
}

// multiEventRecordingRail 监听多个事件并记录收到的所有事件。
// 对齐 Python: MultiEventRail
type multiEventRecordingRail struct {
	*securityrail.BaseSecurityRail
	// mu 保护并发访问
	mu sync.Mutex
	// events 记录收到的事件
	events []agentinterfaces.AgentCallbackEvent
	// decisions 记录的决策
	decisions []securityrail.SecurityDecision
}

// newMultiEventRecordingRail 创建 MultiEventRecordingRail。
func newMultiEventRecordingRail() *multiEventRecordingRail {
	r := &multiEventRecordingRail{}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(
			agentinterfaces.CallbackBeforeInvoke,
			agentinterfaces.CallbackBeforeModelCall,
			agentinterfaces.CallbackAfterToolCall,
		),
		securityrail.WithSecurityCheckFn(r.check),
	)
	return r
}

// TypeName 返回类型名。
func (r *multiEventRecordingRail) TypeName() string {
	return "MultiEventRail"
}

// check 安全检查：记录事件并始终返回 Allow。
func (r *multiEventRecordingRail) check(_ context.Context, securityCtx *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	r.mu.Lock()
	r.events = append(r.events, securityCtx.Event)
	r.mu.Unlock()
	decision := r.Allow(nil)
	r.mu.Lock()
	r.decisions = append(r.decisions, decision)
	r.mu.Unlock()
	return decision, nil
}

// RecordedEvents 返回记录的事件列表。
func (r *multiEventRecordingRail) RecordedEvents() []agentinterfaces.AgentCallbackEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]agentinterfaces.AgentCallbackEvent, len(r.events))
	copy(result, r.events)
	return result
}

// priorityRecordingRail 按优先级顺序记录执行的 Rail。
// 对齐 Python: HighPrioritySecurityRail / LowPrioritySecurityRail
type priorityRecordingRail struct {
	*securityrail.BaseSecurityRail
	// name Rail 名称
	name string
	// orderList 共享的执行顺序列表
	orderList *[]string
	// mu 保护列表的互斥锁
	mu *sync.Mutex
}

// newPriorityRecordingRail 创建 PriorityRecordingRail。
func newPriorityRecordingRail(name string, priority int, orderList *[]string, mu *sync.Mutex) *priorityRecordingRail {
	r := &priorityRecordingRail{
		name:      name,
		orderList: orderList,
		mu:        mu,
	}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackAfterToolCall),
		securityrail.WithSecurityCheckFn(r.check),
	)
	r.WithPriority(priority)
	return r
}

// TypeName 返回类型名。
func (r *priorityRecordingRail) TypeName() string {
	return r.name + "SecurityRail"
}

// check 安全检查：记录执行顺序并返回 Allow。
func (r *priorityRecordingRail) check(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	r.mu.Lock()
	*r.orderList = append(*r.orderList, r.name)
	r.mu.Unlock()
	return r.Allow(nil), nil
}

// interruptBeforeToolCallRail 在 BEFORE_TOOL_CALL 上触发 SecurityInterrupt。
// 对齐 Python: InterceptBeforeModelRail（但 Go 端 Interrupt 不支持 MODEL 事件，改用 TOOL 事件）
type interruptBeforeToolCallRail struct {
	*securityrail.BaseSecurityRail
}

// newInterruptBeforeToolCallRail 创建 InterruptBeforeToolCallRail。
func newInterruptBeforeToolCallRail() *interruptBeforeToolCallRail {
	r := &interruptBeforeToolCallRail{}
	r.BaseSecurityRail = securityrail.NewBaseSecurityRail(
		securityrail.WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall),
		securityrail.WithSecurityCheckFn(r.check),
	)
	return r
}

// TypeName 返回类型名。
func (r *interruptBeforeToolCallRail) TypeName() string {
	return "InterruptBeforeToolCallRail"
}

// check 安全检查：在 BEFORE_TOOL_CALL 上触发 SecurityInterrupt。
func (r *interruptBeforeToolCallRail) check(_ context.Context, _ *securityrail.SecurityCheckContext) (securityrail.SecurityDecision, error) {
	req := &saschema.InterruptRequest{
		Message: "此工具调用需要人工审批",
		PayloadSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"approved": map[string]any{"type": "boolean"}},
			"required":   []string{"approved"},
		},
		AutoConfirmKey: "human_approval_required",
	}
	return r.Interrupt(req, "tool_interrupt"), nil
}

// 编译时验证各 Rail 实现满足 AgentRail 接口
var (
	_ agentinterfaces.AgentRail = (*rejectToolResultRail)(nil)
	_ agentinterfaces.AgentRail = (*strictRejectRail)(nil)
	_ agentinterfaces.AgentRail = (*multiEventRecordingRail)(nil)
	_ agentinterfaces.AgentRail = (*priorityRecordingRail)(nil)
	_ agentinterfaces.AgentRail = (*interruptBeforeToolCallRail)(nil)
)
