//go:build integration

package harness_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	securityrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAgentE2ESuite 测试 DeepAgent 完整执行链路。
// 嵌入 AgentSuite，可使用 NewDeepAgentForTest() 创建 DeepAgent。
// 对齐 Python: tests/system_tests/harness/test_deep_agent_e2e.py
type DeepAgentE2ESuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestDeepAgentE2ESuite(t *testing.T) {
	suite.Run(t, new(DeepAgentE2ESuite))
}

// newReadFileTool 创建模拟 read_file 工具。
// 对齐 Python: ReadFileTool
func newReadFileTool() tool.Tool {
	tc := tool.NewToolCardWithID("read_file", "read_file", "读取文件内容", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, inputs map[string]any) (map[string]any, error) {
			path, _ := inputs["path"].(string)
			return map[string]any{"content": fmt.Sprintf("文件 %s 的内容: hello world", path)}, nil
		}, nil,
	)
	return t
}

// newWriteFileTool 创建模拟 write_file 工具。
// 对齐 Python: WriteFileTool
func newWriteFileTool() tool.Tool {
	tc := tool.NewToolCardWithID("write_file", "write_file", "写入文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"status": "ok"}, nil
		}, nil,
	)
	return t
}

// newFailTool 创建总是失败的模拟工具。
// 对齐 Python: 各测试中的错误工具场景
func newFailTool() tool.Tool {
	tc := tool.NewToolCardWithID("fail_tool", "fail_tool", "总是失败的工具", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return nil, fmt.Errorf("工具执行失败: 模拟错误")
		}, nil,
	)
	return t
}

// TestInvoke_单轮纯文本 测试 DeepAgent 单轮纯文本 Invoke。
// 对齐 Python: test_deep_agent_invoke_e2e（文本响应场景）
func (s *DeepAgentE2ESuite) TestInvoke_单轮纯文本() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("这是最终答案"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "你好"})
	s.Require().NoError(err)
	s.NotNil(result)
}

// TestInvoke_单轮工具调用 测试 DeepAgent 单轮工具调用（LLM → tool_call → 工具执行 → LLM → 文本）。
// 对齐 Python: test_deep_agent_invoke_e2e（工具调用场景）
func (s *DeepAgentE2ESuite) TestInvoke_单轮工具调用() {
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"path": "/tmp/test.txt"}`),
		mockllm.CreateTextResponse("文件内容是 hello world"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{newReadFileTool()},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "读取 /tmp/test.txt"})
	s.Require().NoError(err)
	s.NotNil(result)

	// MockLLM 被调用了 2 次（tool_call + text）
	s.Equal(2, s.MockLLM.InvokeCallCount())
}

// TestInvoke_多轮工具链 测试 DeepAgent 连续多轮工具调用。
// 对齐 Python: test_deep_agent_complex_task_multi_tool_chain
func (s *DeepAgentE2ESuite) TestInvoke_多轮工具链() {
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("read_file", `{"path": "/tmp/input.txt"}`),
		mockllm.CreateToolCallResponse("write_file", `{"path": "/tmp/output.txt", "content": "processed"}`),
		mockllm.CreateTextResponse("任务完成"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{newReadFileTool(), newWriteFileTool()},
		MaxIterations: 10,
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "读取并写入文件"})
	s.Require().NoError(err)
	s.NotNil(result)

	// MockLLM 被调用了 3 次
	s.Equal(3, s.MockLLM.InvokeCallCount())
}

// TestInvoke_工具失败错误传播 测试工具返回 error 后 DeepAgent 能继续执行。
// 对齐 Python: 工具失败错误传播场景
func (s *DeepAgentE2ESuite) TestInvoke_工具失败错误传播() {
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("fail_tool", `{"input": "test"}`),
		mockllm.CreateTextResponse("已处理工具错误"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{newFailTool()},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "调用失败工具"})
	// 工具失败但 ReAct 循环继续，最终 Agent 返回结果
	s.Require().NoError(err)
	s.NotNil(result)
}

// TestInvoke_带Session 测试 DeepAgent Invoke 带 Session 运行。
// 对齐 Python: Agent + Session 联动
func (s *DeepAgentE2ESuite) TestInvoke_带Session() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("带 Session 执行完成"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("deep-agent-session-test")
	err = sess.PreRun(s.Ctx)
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "带 Session 测试"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err)
	s.NotNil(result)
}

// TestAutoRails_默认创建 测试不传 Rails 时 DeepAgent 自动注册默认 Rail。
// 对齐 Python: test_deep_agent_auto_rails_creation_e2e
func (s *DeepAgentE2ESuite) TestAutoRails_默认创建() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("自动 Rail 测试"))

	// 不传任何 Rails
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "自动 Rail"})
	s.Require().NoError(err)
	s.NotNil(result)

	// 验证 DeepAgent 自动注册了 SafetyPromptRail（addDefaultRails 始终添加）
	railType := reflect.TypeOf(&securityrail.SafetyPromptRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应自动注册 SafetyPromptRail（SecurityRail）")
}
