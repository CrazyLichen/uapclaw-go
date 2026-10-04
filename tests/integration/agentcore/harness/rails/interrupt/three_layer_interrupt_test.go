//go:build integration

package interrupt_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ThreeLayerInterruptSuite 三层嵌套中断集成测试套件。
// 对齐 Python: tests/system_tests/harness/rail/test_hitl_rail.py 中的嵌套中断场景
//
// 测试 DeepAgent → ReActAgent → Tool 三层嵌套中断传播：
//  1. 模拟子 Agent 工具返回中断 dict → handleSubAgentInterrupt 路径
//  2. 子 Agent 中断 + 直接工具中断混合
//  3. 子 Agent 中断恢复
//
// 运行方式: go test -tags="sqlite_fts5 test integration" ./tests/integration/agentcore/harness/rails/interrupt/...
type ThreeLayerInterruptSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestThreeLayerInterruptSuite 运行三层嵌套中断测试套件
func TestThreeLayerInterruptSuite(t *testing.T) {
	suite.Run(t, new(ThreeLayerInterruptSuite))
}

// TestThreeLayer_子Agent中断触发 测试子 Agent 工具返回中断 dict 时，
// 中断结果正确传播到 DeepAgent 层级。
// 模拟场景：DeepAgent → ReActAgent → task_tool（子 Agent 工具）
// task_tool 内部子 Agent 触发中断，返回 {"result_type":"interrupt","interrupt_ids":[...]}
func (s *ThreeLayerInterruptSuite) TestThreeLayer_子Agent中断触发() {
	ctx := s.Ctx

	// 创建模拟子 Agent 工具：返回子 Agent 中断 dict
	var taskToolInvokeCount atomic.Int32
	taskTool := newSubAgentInterruptTool(&taskToolInvokeCount, "sub_task_001", "子 Agent 请求确认")

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("task_tool"))

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("task_tool", `{"task":"执行子任务"}`),
		mockllm.CreateTextResponse("子任务完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{taskTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("three-layer-sub-interrupt")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "执行子任务"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Invoke 调用失败")
	s.Require().NotNil(result, "结果不应为 nil")

	// 验证中断结果包含子 Agent 中断
	s.Equal("interrupt", result["result_type"], "应为 interrupt 类型")
	interruptIDs, ok := result["interrupt_ids"].([]string)
	s.Require().True(ok, "interrupt_ids 应为 []string")
	s.NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	sess.PostRun(ctx)
}

// TestThreeLayer_混合中断 测试子 Agent 中断和直接工具中断同时存在的场景。
// MockLLM 返回 MultiToolCall：一个是普通工具触发 ConfirmInterruptRail，
// 另一个是子 Agent 工具返回中断 dict。
func (s *ThreeLayerInterruptSuite) TestThreeLayer_混合中断() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	var taskToolInvokeCount atomic.Int32
	taskTool := newSubAgentInterruptTool(&taskToolInvokeCount, "sub_task_002", "子 Agent 需确认")

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file", "task_tool"))

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "write_file", ArgsJSON: `{"filepath":"/tmp/test.txt","content":"hello"}`},
			{Name: "task_tool", ArgsJSON: `{"task":"执行子任务"}`},
		}),
		mockllm.CreateTextResponse("所有任务完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool, taskTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("three-layer-mixed")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 触发混合中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件并执行子任务"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Invoke 调用失败")
	s.Require().NotNil(result, "结果不应为 nil")

	// 验证中断结果
	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 2)
	s.Len(interruptIDs, 2, "应有 2 个中断：write_file + task_tool")

	sess.PostRun(ctx)
}

// TestThreeLayer_子Agent中断恢复 测试子 Agent 工具返回中断 dict 后，
// ConfirmInterruptRail 也拦截子 Agent 工具时的中断恢复。
// 场景：task_tool 被 ConfirmInterruptRail 拦截 → 确认恢复 → 工具正常执行。
func (s *ThreeLayerInterruptSuite) TestThreeLayer_子Agent中断恢复() {
	ctx := s.Ctx

	var taskToolInvokeCount atomic.Int32
	taskTool := newSimpleTaskTool(&taskToolInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("task_tool"))

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("task_tool", `{"task":"执行子任务"}`),
		mockllm.CreateTextResponse("子任务完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{taskTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 10,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("three-layer-resume")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "执行子任务"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	// 第 2 步：确认并恢复
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")
	s.Require().NotNil(result, "恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(1), taskToolInvokeCount.Load(), "确认后 task_tool 应执行")

	sess.PostRun(ctx)
}

// TestThreeLayer_直接工具中断非子Agent 测试直接工具中断（非子 Agent）路径。
// 确认工具中断不会走 handleSubAgentInterrupt，而是走 handleToolInterruptException。
func (s *ThreeLayerInterruptSuite) TestThreeLayer_直接工具中断非子Agent() {
	ctx := s.Ctx

	var writeInvokeCount atomic.Int32
	writeTool := newTrackedWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("write_file", `{"filepath":"/tmp/test.txt","content":"hello"}`),
		mockllm.CreateTextResponse("文件已写入"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("three-layer-direct")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 触发直接工具中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "写入文件"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Invoke 调用失败")
	s.Require().NotNil(result, "结果不应为 nil")

	// 验证中断结果
	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.NotEmpty(interruptIDs, "直接工具中断应有 interrupt_id")

	// 确认恢复
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(1), writeInvokeCount.Load(), "确认后 write_file 应执行 1 次")

	sess.PostRun(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newSimpleTaskTool 创建简单的模拟子 Agent 工具，正常返回结果。
func newSimpleTaskTool(invokeCount *atomic.Int32) tool.Tool {
	tc := tool.NewToolCardWithID("task_tool", "task_tool", "执行子任务", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			invokeCount.Add(1)
			return map[string]any{"status": "ok", "task": "completed"}, nil
		}, nil,
	)
	return t
}

// newSubAgentInterruptTool 创建模拟子 Agent 工具，返回子 Agent 中断 dict。
// 模拟子 Agent 内部触发中断后，中断结果通过工具返回值向上传播。
func newSubAgentInterruptTool(invokeCount *atomic.Int32, interruptID, message string) tool.Tool {
	tc := tool.NewToolCardWithID("task_tool", "task_tool", "执行子任务", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			invokeCount.Add(1)
			// 返回子 Agent 中断 dict，模拟子 Agent 的中断传播
			// 对齐 Python: tool_result = {"result_type": "interrupt", "interrupt_ids": [...], "state": [...]}
			return map[string]any{
				"result_type":   "interrupt",
				"interrupt_ids": []string{interruptID},
				"state": []any{map[string]any{
					"type":  "interaction",
					"index": 0,
					"payload": map[string]any{
						"id":    interruptID,
						"value": map[string]any{"message": message},
					},
				}},
			}, nil
		}, nil,
	)
	return t
}

// newConditionalSubAgentTool 创建条件性子 Agent 工具。
// 第一次调用返回子 Agent 中断，后续调用正常返回。
func newConditionalSubAgentTool(callCount *atomic.Int32) tool.Tool {
	tc := tool.NewToolCardWithID("task_tool", "task_tool", "执行子任务", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(ctx context.Context, args map[string]any) (map[string]any, error) {
			count := callCount.Add(1)
			if count == 1 {
				// 第一次调用：返回子 Agent 中断
				return map[string]any{
					"result_type":   "interrupt",
					"interrupt_ids": []string{"sub_task_cond"},
					"state": []any{map[string]any{
						"type":  "interaction",
						"index": 0,
						"payload": map[string]any{
							"id":    "sub_task_cond",
							"value": map[string]any{"message": "子 Agent 需确认"},
						},
					}},
				}, nil
			}
			// 后续调用：正常返回
			return map[string]any{"status": "ok", "task": "completed"}, nil
		}, nil,
	)
	return t
}

// subAgentInterruptDict 构建子 Agent 中断 dict 的辅助函数。
func subAgentInterruptDict(interruptID string, message string) map[string]any {
	dict := map[string]any{
		"result_type":   "interrupt",
		"interrupt_ids": []string{interruptID},
		"state": []any{map[string]any{
			"type":  "interaction",
			"index": 0,
			"payload": map[string]any{
				"id":    interruptID,
				"value": map[string]any{"message": message},
			},
		}},
	}
	// 使用 json 序列化/反序列化确保类型一致性
	raw, _ := json.Marshal(dict)
	var result map[string]any
	_ = json.Unmarshal(raw, &result)
	return result
}
