//go:build integration

package interrupt_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConfirmInterruptRailSuite 测试 ConfirmInterruptRail 工具权限中断。
// 对齐 Python: tests/system_tests/harness/test_confirm_interrupt_rail.py
type ConfirmInterruptRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestConfirmInterruptRailSuite(t *testing.T) {
	suite.Run(t, new(ConfirmInterruptRailSuite))
}

// TestConfirmInterruptRail_初始化成功 测试 ConfirmInterruptRail 可成功创建和初始化。
// 对齐 Python: test_hitl_tool_permission_interrupt_read_file_ask（初始化部分）
func (s *ConfirmInterruptRailSuite) TestConfirmInterruptRail_初始化成功() {
	rail := interrupt.NewConfirmInterruptRail("write_file", "delete_file")
	s.Require().NotNil(rail)

	// 创建带 Rail 的 DeepAgent
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)
}

// TestConfirmInterruptRail_拦截未授权工具 测试拦截未授权工具调用。
// 对齐 Python: test_deepagent_stream_interrupt_resume（拦截部分）
// 注意：完整的中断/恢复流程依赖 HITL Session.Interact() 机制，
// 此测试仅验证 Rail 注册和 Agent 创建成功。
func (s *ConfirmInterruptRailSuite) TestConfirmInterruptRail_拦截未授权工具() {
	rail := interrupt.NewConfirmInterruptRail("write_file")

	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("确认后继续"),
	)

	writeTool := newConfirmWriteFileTool()
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	// 验证 Agent 创建成功且 Rail 已注册
	s.Require().NotNil(agent)
	railType := reflect.TypeOf(&interrupt.ConfirmInterruptRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 ConfirmInterruptRail")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newConfirmWriteFileTool 创建模拟 write_file 工具（ConfirmInterrupt 测试用）。
func newConfirmWriteFileTool() tool.Tool {
	tc := tool.NewToolCardWithID("write_file", "write_file", "写入文件", nil, nil)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"status": "written"}, nil
		}, nil,
	)
	return t
}
