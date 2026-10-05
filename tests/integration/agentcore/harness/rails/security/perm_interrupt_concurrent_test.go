//go:build integration

package security

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	securityrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermInterruptConcurrentSuite 测试 PermissionInterruptRail 并发工具中断/恢复场景。
//
// 覆盖：2+ 并发 ASK 工具中断、两轮部分拒绝、混合 ALLOW+ASK 选择性中断、
// ConfirmPayload struct 并发恢复。
//
// 对齐 Python: tests/system_tests/harness/rail/test_deep_agent_tool_permission_interrupt.py
// + test_hitl_rail_concurrent_tools_partial_reject_two_rounds
type PermInterruptConcurrentSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPermInterruptConcurrentSuite 运行 PermissionInterrupt 并发中断恢复测试套件
func TestPermInterruptConcurrentSuite(t *testing.T) {
	suite.Run(t, new(PermInterruptConcurrentSuite))
}

// TestPermInterrupt_两个并发ASK全部确认 测试 2 个并发 write_file (ask) 全部确认后执行。
//
// 流程：
//  1. MockLLM 返回 2 个并发 write_file tool_calls
//  2. 两个都触发 ASK → 2 个 interrupt_ids
//  3. 全部确认 → answer，两个工具都执行
func (s *PermInterruptConcurrentSuite) TestPermInterrupt_两个并发ASK全部确认() {
	ctx := s.Ctx

	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"write_file": "ask",
		},
	}

	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return "/workspace" },
	}
	permRail := securityrail.NewPermissionInterruptRail(
		config, nil, nil, nil, "", host,
	)

	var writeInvokeCount atomic.Int32
	writeTool := newPermTrackedWriteFileTool(&writeInvokeCount)

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/a.txt","content":"aa"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/b.txt","content":"bb"}`},
		}),
		mockllm.CreateTextResponse("文件已写入"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("perm-concurrent-2-confirm")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 次 Invoke：2 个 write_file 触发 ASK → interrupt 结果
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入两个文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 2)
	s.Len(interruptIDs, 2, "应有 2 个 interrupt_id")

	// 全部确认
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	interactiveInput.UserInputs[interruptIDs[1]] = map[string]any{
		"approved":     true,
		"feedback":     "Confirm",
		"auto_confirm": false,
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(2), writeInvokeCount.Load(), "确认后两个 write_file 应全部执行")

	sess.PostRun(ctx)
}

// TestPermInterrupt_两个并发ASK两轮部分拒绝 测试 2 个并发 ASK 工具两轮部分拒绝。
//
// 流程：
//  1. MockLLM 返回 2 个并发 write_file tool_calls
//  2. 第 1 次 Invoke：2 个 interrupt_ids
//  3. 第 2 次 Invoke：只拒绝第 2 个，保留第 1 个 → 仍为 interrupt（第 1 个待处理）
//  4. 第 3 次 Invoke：确认第 1 个 → answer
func (s *PermInterruptConcurrentSuite) TestPermInterrupt_两个并发ASK两轮部分拒绝() {
	ctx := s.Ctx

	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"write_file": "ask",
		},
	}

	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return "/workspace" },
	}
	permRail := securityrail.NewPermissionInterruptRail(
		config, nil, nil, nil, "", host,
	)

	var writeInvokeCount atomic.Int32
	writeTool := newPermTrackedWriteFileTool(&writeInvokeCount)

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/a.txt","content":"aa"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/b.txt","content":"bb"}`},
		}),
		mockllm.CreateTextResponse("部分操作完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("perm-concurrent-2-two-round-reject")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 次 Invoke：2 个 interrupt_ids
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入两个文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 2)
	s.Len(interruptIDs, 2, "应有 2 个 interrupt_id")

	// 第 2 次 Invoke：只拒绝第 2 个，保留第 1 个
	interactiveInput := testhelpers.RejectInterrupt(interruptIDs[1], "不允许写入 b.txt")

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "第 2 次 Invoke 失败")
	s.Require().NotNil(result, "第 2 次结果不应为 nil")

	// 第 1 个仍待处理，应得到新的中断（1 个 interrupt_id）
	remainingIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Len(remainingIDs, 1, "应剩余 1 个 interrupt_id")
	s.Equal(interruptIDs[0], remainingIDs[0], "剩余的 interrupt_id 应为第 1 个")

	// 第 3 次 Invoke：确认第 1 个，得到 answer
	confirmInput := testhelpers.ConfirmInterrupt(remainingIDs[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": confirmInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "第 3 次 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(1), writeInvokeCount.Load(), "只有确认的 write_file 应执行")

	sess.PostRun(ctx)
}

// TestPermInterrupt_三个并发ASK部分确认部分拒绝 测试 3 个并发 ASK 工具中 2 确认 1 拒绝。
func (s *PermInterruptConcurrentSuite) TestPermInterrupt_三个并发ASK部分确认部分拒绝() {
	ctx := s.Ctx

	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"write_file": "ask",
		},
	}

	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return "/workspace" },
	}
	permRail := securityrail.NewPermissionInterruptRail(
		config, nil, nil, nil, "", host,
	)

	var writeInvokeCount atomic.Int32
	writeTool := newPermTrackedWriteFileTool(&writeInvokeCount)

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/a.txt","content":"aa"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/b.txt","content":"bb"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/c.txt","content":"cc"}`},
		}),
		mockllm.CreateTextResponse("部分操作完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("perm-concurrent-3-partial")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "写入三个文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 3)
	s.Len(interruptIDs, 3, "应有 3 个 interrupt_id")

	// 确认 2 个，拒绝 1 个
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	interactiveInput.UserInputs[interruptIDs[1]] = map[string]any{
		"approved": true, "feedback": "Confirm", "auto_confirm": false,
	}
	interactiveInput.UserInputs[interruptIDs[2]] = map[string]any{
		"approved": false, "feedback": "不允许写入 c.txt",
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(2), writeInvokeCount.Load(), "只有确认的 2 个 write_file 应执行")

	sess.PostRun(ctx)
}

// TestPermInterrupt_混合DENY和ASK选择性中断 测试并发工具中 DENY 直接拒绝 + ASK 中断。
//
// 场景：rm_file=deny, write_file=ask
// 并发调用 rm_file + write_file → rm_file 被拒绝（不中断），write_file 中断
func (s *PermInterruptConcurrentSuite) TestPermInterrupt_混合DENY和ASK选择性中断() {
	ctx := s.Ctx

	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"rm_file":    "deny",
			"write_file": "ask",
		},
	}

	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return "/workspace" },
	}
	permRail := securityrail.NewPermissionInterruptRail(
		config, nil, nil, nil, "", host,
	)

	var rmInvokeCount atomic.Int32
	var writeInvokeCount atomic.Int32
	rmTool := newPermTrackedRmFileTool(&rmInvokeCount)
	writeTool := newPermTrackedWriteFileTool(&writeInvokeCount)

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "rm_file", ArgsJSON: `{"filepath":"/tmp/hosts"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/output.txt","content":"output"}`},
		}),
		mockllm.CreateTextResponse("操作完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{rmTool, writeTool},
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("perm-concurrent-mixed-deny-ask")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "删除并写入"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")

	// rm_file DENY 直接拒绝（不中断），write_file ASK 中断 → 1 个 interrupt_id
	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Len(interruptIDs, 1, "应有 1 个 interrupt_id（仅 write_file）")
	s.Equal(int32(0), rmInvokeCount.Load(), "rm_file 被 DENY 不应执行")
	s.Equal(int32(0), writeInvokeCount.Load(), "write_file 被中断不应执行")

	// 确认 write_file
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.GreaterOrEqual(writeInvokeCount.Load(), int32(1), "确认后 write_file 应执行")

	sess.PostRun(ctx)
}

// TestPermInterrupt_ConfirmPayload并发恢复 测试使用 ConfirmPayload struct 对象恢复 2 个并发 ASK 中断。
//
// 对齐 Python: test_hitl_tool_permission_interrupt_resume_with_confirm_payload_object
func (s *PermInterruptConcurrentSuite) TestPermInterrupt_ConfirmPayload并发恢复() {
	ctx := s.Ctx

	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"write_file": "ask",
		},
	}

	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return "/workspace" },
	}
	permRail := securityrail.NewPermissionInterruptRail(
		config, nil, nil, nil, "", host,
	)

	var writeInvokeCount atomic.Int32
	writeTool := newPermTrackedWriteFileTool(&writeInvokeCount)

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/a.txt","content":"aa"}`},
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/b.txt","content":"bb"}`},
		}),
		mockllm.CreateTextResponse("文件已写入"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("perm-concurrent-confirm-payload")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 次 Invoke：2 个 ASK → 2 个 interrupt_ids
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入两个文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 2)
	s.Len(interruptIDs, 2, "应有 2 个 interrupt_id")

	// 使用 ConfirmPayload struct（而非 map[string]any）构建恢复输入
	interactiveInput := &sessioninteraction.InteractiveInput{
		UserInputs: map[string]any{
			interruptIDs[0]: &interrupt.ConfirmPayload{
				Approved:    true,
				Feedback:    "Confirm",
				AutoConfirm: false,
			},
			interruptIDs[1]: &interrupt.ConfirmPayload{
				Approved:    true,
				Feedback:    "Confirm",
				AutoConfirm: false,
			},
		},
	}

	// 第 2 次 Invoke：恢复 → 正常完成
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")
	s.Require().NotNil(result, "恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(2), writeInvokeCount.Load(), "确认后两个 write_file 应全部执行")

	sess.PostRun(ctx)
}

// TestPermInterrupt_拒绝后ConfirmPayload反馈 测试拒绝 ConfirmPayload 后工具不执行且反馈正确传播。
func (s *PermInterruptConcurrentSuite) TestPermInterrupt_拒绝后ConfirmPayload反馈() {
	ctx := s.Ctx

	config := map[string]any{
		"enabled": true,
		"tools": map[string]any{
			"write_file": "ask",
		},
	}

	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return "/workspace" },
	}
	permRail := securityrail.NewPermissionInterruptRail(
		config, nil, nil, nil, "", host,
	)

	var writeInvokeCount atomic.Int32
	writeTool := newPermTrackedWriteFileTool(&writeInvokeCount)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/denied.txt","content":"no"}`),
		mockllm.CreateTextResponse("操作被拒绝"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{permRail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("perm-concurrent-reject-feedback")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 次 Invoke：ASK → interrupt
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)

	// 使用 ConfirmPayload struct 拒绝
	interactiveInput := &sessioninteraction.InteractiveInput{
		UserInputs: map[string]any{
			interruptIDs[0]: &interrupt.ConfirmPayload{
				Approved: false,
				Feedback: "不允许写入此文件",
			},
		},
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "拒绝恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(0), writeInvokeCount.Load(), "拒绝后 write_file 不应执行")

	sess.PostRun(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newPermTrackedWriteFileTool 创建带 invoke_count 追踪的 write_file 工具。
func newPermTrackedWriteFileTool(invokeCount *atomic.Int32) tool.Tool {
	tc := tool.NewToolCardWithID("write_file", "write_file", "写入文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			invokeCount.Add(1)
			return map[string]any{"status": "ok", "tool": "write_file"}, nil
		}, nil,
	)
	return t
}

// newPermTrackedReadFileTool 创建带 invoke_count 追踪的 read_file 工具。
func newPermTrackedReadFileTool(invokeCount *atomic.Int32) tool.Tool {
	tc := tool.NewToolCardWithID("read_file", "read_file", "读取文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			invokeCount.Add(1)
			return map[string]any{"status": "ok", "tool": "read_file", "content": "file content"}, nil
		}, nil,
	)
	return t
}

// newPermTrackedRmFileTool 创建带 invoke_count 追踪的 rm_file 工具。
func newPermTrackedRmFileTool(invokeCount *atomic.Int32) tool.Tool {
	tc := tool.NewToolCardWithID("rm_file", "rm_file", "删除文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			invokeCount.Add(1)
			return map[string]any{"status": "ok", "tool": "rm_file"}, nil
		}, nil,
	)
	return t
}
