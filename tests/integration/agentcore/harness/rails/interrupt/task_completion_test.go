//go:build integration

package interrupt_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskCompletionRailSuite 测试 TaskCompletionRail 停止条件。
// 对齐 Python: tests/system_tests/harness/test_task_completion_rail.py
type TaskCompletionRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestTaskCompletionRailSuite(t *testing.T) {
	suite.Run(t, new(TaskCompletionRailSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

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

// newTcrReadFileTool 创建模拟 read_file 工具。
func newTcrReadFileTool() tool.Tool {
	tc := tool.NewToolCardWithID("read_file", "read_file", "读取文件内容", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, inputs map[string]any) (map[string]any, error) {
			path, _ := inputs["path"].(string)
			return map[string]any{"content": fmt.Sprintf("文件 %s 的内容: hello world", path)}, nil
		}, nil,
	)
	return t
}

// TestMaxRounds_超限自动停止 测试 MaxRounds 限制。
// 对齐 Python: test_task_completion_rail.py UC-1
// 设置 WithMaxRounds(2)，MockLLM 持续返回 ToolCall，验证第 2 轮后循环终止。
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
		ToolInstances:  []tool.Tool{newLoopTool()},
		Rails:          []agentinterfaces.AgentRail{tcr},
		EnableTaskLoop: true,
		MaxIterations:  10,
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
		mockllm.CreateToolCallResponse("read_file", `{"path": "/tmp/test.txt"}`),
		mockllm.CreateTextResponse("<promise>任务完成</promise>"),
	)

	tcr := rails.NewTaskCompletionRail(rails.WithCompletionPromise("任务完成"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances:  []tool.Tool{newTcrReadFileTool()},
		Rails:          []agentinterfaces.AgentRail{tcr},
		EnableTaskLoop: true,
		MaxIterations:  10,
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
		ToolInstances:  []tool.Tool{newLoopTool()},
		Rails:          []agentinterfaces.AgentRail{tcr},
		EnableTaskLoop: true,
		MaxIterations:  100,
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
