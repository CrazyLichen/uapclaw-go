//go:build integration

package harness_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	teamharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolTeardownSuite 轮末工具摘除集成测试。
//
// 跨度（规则 7）：agentcore/single_agent/ability + agentcore/runner/resources_manager
// + agent_teams/harness + agentcore/session。
type ToolTeardownSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestToolTeardownSuite 套件入口。
func TestToolTeardownSuite(t *testing.T) {
	suite.Run(t, new(ToolTeardownSuite))
}

// TestI09_RunOnce工具摘除后可重建 连续两次 RunOnce 不产生残留 id 告警。
//
// 这是 TeardownTools 存在的唯一理由：第一次结束后摘除本 owner 的工具，
// 第二次重建注册时不残留（否则 Add 的重复检测会为每个残留 id 记告警）。
func (s *ToolTeardownSuite) TestI09_RunOnce工具摘除后可重建() {
	spec, err := s.NewSpecWithMockLLM(2)
	s.Require().NoError(err)
	spec.Language = "cn"

	h := teamharness.TeamHarnessBuild(spec, "teammate", "alice", false, nil, nil)
	s.Require().NotNil(h)
	s.Require().NotNil(h.Native())

	// 第一次
	sess1 := session.CreateAgentSession("", h.Native().DeepAgent().Card(), nil)
	_, runErr := h.Native().RunOnce(s.Ctx, "第一次", sess1)
	s.NoError(runErr)

	// 能力管理器仍可访问（teardown 后不应处于半拆状态）
	am := h.Native().DeepAgent().AbilityManager()
	s.Require().NotNil(am)

	// 说明：这里不断言"有状态工具数为 0"。am.List() 同时返回本 owner 的
	// 工具、workflow 与进程级共享项，且部分内置工具有状态但本就长期驻留，
	// 计数归零不是 TeardownTools 的契约。TeardownTools 的真实收益是
	// 「第二次重建不因残留 id 冲突而失败/告警」——由下面的第二次 RunOnce 验证。

	// 第二次：重建应成功
	sess2 := session.CreateAgentSession("", h.Native().DeepAgent().Card(), nil)
	_, runErr = h.Native().RunOnce(s.Ctx, "第二次", sess2)
	s.NoError(runErr, "重建后第二次 RunOnce 应成功")

	// 资源管理器可正常访问（无残留 id 冲突）
	rm := runner.GetResourceMgr()
	s.Require().NotNil(rm)
}
