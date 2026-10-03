//go:build integration

package sys_operation

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	sysoprail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SysOperationRailSuite 测试 SysOperationRail 系统操作。
//
// 对齐 Python: 无 Python 对应测试文件（Go 端独有）
// 注意：SysOperationRail 未覆盖 GetCallbacks()，回调方法不会自动触发，
// 仅验证 Init 注册工具和 Uninit 清理。
type SysOperationRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSysOperationRailSuite(t *testing.T) {
	suite.Run(t, new(SysOperationRailSuite))
}

// TestSysOperationRail_Init注册工具 测试 SysOperationRail Init 后注册 file/shell 工具。
// 对齐 Python: Go 端 TestSysOperationRail_Init注册工具 ——
// SysOperationRail.init 注册 read_file/write_file/bash 等工具到 AbilityManager。
// 注意：需先 Invoke 触发 ensureInitialized。
func (s *SysOperationRailSuite) TestSysOperationRail_Init注册工具() {
	rail := sysoprail.NewSysOperationRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("系统操作测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先 Invoke 触发 ensureInitialized → Rail Init
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "系统操作测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 SysOperationRail 已注册
	railType := reflect.TypeOf(&sysoprail.SysOperationRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 SysOperationRail")

	// 验证核心工具注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("read_file"), "应注册 read_file")
	s.NotNil(am.Get("bash"), "应注册 bash")
	s.NotNil(am.Get("write_file"), "应注册 write_file（默认非只读模式）")
}

// TestSysOperationRail_只读模式不注册写入工具 测试只读模式下 write_file/edit_file 不注册。
// 对齐 Python: Go 端 TestSysOperationRail_InitReadOnly ——
// WithReadOnly(true) 时不注册写入类工具。
func (s *SysOperationRailSuite) TestSysOperationRail_只读模式不注册写入工具() {
	rail := sysoprail.NewSysOperationRail(
		sysoprail.WithReadOnly(true),
	)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("只读模式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先 Invoke 触发 ensureInitialized
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "只读模式测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 只读模式下 write_file 和 edit_file 不注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("write_file"), "只读模式不应注册 write_file")
	s.Nil(am.Get("edit_file"), "只读模式不应注册 edit_file")
	s.NotNil(am.Get("read_file"), "只读模式仍应注册 read_file")
}

// TestSysOperationRail_Uninit清理 测试 Uninit 后工具从 AbilityManager 移除。
// 对齐 Python: Go 端 TestSysOperationRail_Uninit ——
// Uninit 从 AbilityManager 和 ResourceMgr 移除已注册工具。
func (s *SysOperationRailSuite) TestSysOperationRail_Uninit清理() {
	rail := sysoprail.NewSysOperationRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("清理测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行一次 Invoke 确保 Init 完成
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "清理前"})

	// Uninit
	rail.Uninit(agent)

	// 验证工具已移除
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("read_file"), "Uninit 后 read_file 应移除")
	s.Nil(am.Get("bash"), "Uninit 后 bash 应移除")
}
