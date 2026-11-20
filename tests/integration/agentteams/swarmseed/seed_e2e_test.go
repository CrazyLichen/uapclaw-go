//go:build integration

package swarmseed_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/external"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SwarmSeedSuite 平台 seed 生产工厂端到端集成套件（15.16c）。
//
// 跨子系统：swarm/agents/swarm（enrich seed 生产段 + 平台工厂注册）→
// agent_teams/schema（TeamAgentSpec JSON 往返 + MaterializeBuildContext）→
// agent_teams/external（ResolveExternalPaths 消费端 cwd 选择链）。
type SwarmSeedSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSwarmSeedSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestSwarmSeedSuite(t *testing.T) {
	suite.Run(t, new(SwarmSeedSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// TestSeed跨JSON边界往返不丢 enrich 生产段挂活载体与种子，JSON 往返后
// 经平台工厂重建，团队级字段逐项不丢。
func (s *SwarmSeedSuite) TestSeed跨JSON边界往返不丢() {
	spec := &schema.TeamAgentSpec{TeamName: "pay"}
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(spec, swarm.EnrichSeedParams{
		SessionID:       "sess-e2e",
		Mode:            "team",
		ProjectDir:      "/proj/dir",
		ChannelID:       "web",
		RequestID:       "req-1",
		RequestMetadata: map[string]any{"mode": "team"},
	}))
	s.Require().NotNil(spec.BuildContext, "活载体应挂载（assembly.py:536）")
	s.Require().NotEmpty(spec.BuildContextSeed, "seed 应生成（assembly.py:540）")

	raw, err := json.Marshal(spec)
	s.Require().NoError(err)
	var received schema.TeamAgentSpec
	s.Require().NoError(json.Unmarshal(raw, &received))
	s.Require().NotNil(received.BuildContextSeed, "seed 必须跨 JSON 边界保留")
	s.Nil(received.BuildContext, "活载体不参与序列化")

	s.Require().NoError(received.MaterializeBuildContext())
	s.Require().NotNil(received.BuildContext, "materialize 必须经平台工厂重建")
	bc, ok := received.BuildContext.(*buildctx.SwarmBuildContext)
	s.Require().True(ok, "应重建为平台子类，got %T", received.BuildContext)
	s.Equal("sess-e2e", bc.SessionID, "session_id 不丢")
	s.Equal("pay", bc.TeamID, "team_id 不丢")
	s.Equal("/proj/dir", bc.GetProjectDir(), "project_dir 不丢（install_skills 门卫依赖）")
	s.Equal("web", bc.Channel, "channel 不丢")
	s.Equal("team", bc.Mode, "mode 不丢")
}

// TestExternalcli消费端吃到重建context 重建后的活载体驱动 externalcli
// cwd 选择链（15.12 paths.go:86-101 消费面）。
func (s *SwarmSeedSuite) TestExternalcli消费端吃到重建context() {
	spec := &schema.TeamAgentSpec{TeamName: "pay"}
	s.Require().NoError(swarm.EnrichTeamSpecForSwarmSeed(spec, swarm.EnrichSeedParams{
		SessionID: "sess-cli", Mode: "team", ProjectDir: "/proj/cli",
	}))
	// 模拟跨序列化边界：只带 seed 过去
	raw, err := json.Marshal(spec)
	s.Require().NoError(err)
	var received schema.TeamAgentSpec
	s.Require().NoError(json.Unmarshal(raw, &received))
	s.Require().NoError(received.MaterializeBuildContext())

	ctx := &schema.TeamRuntimeContext{Role: schema.TeamRoleTeammate, MemberName: "beta"}
	cwd, extraDirs := external.ResolveExternalPaths(&received, ctx, "", "pay")
	s.Equal("/proj/cli", cwd, "projectless 显式 cwd/worktree 缺席时 cwd 取 project_dir")
	// appendExtraDir 排除与 cwd 相同的路径：project_dir 即 cwd，不重复入列
	for _, dir := range extraDirs {
		s.NotEqual("/proj/cli", dir, "project_dir==cwd 不应重复入 extra_dirs")
	}
	s.Contains(extraDirs, "/root/.uapclaw/.agent_teams/pay/team-workspace",
		"team_workspace 应入 extra_dirs（无项目共享面）")
}

// Test基线空壳不回归 无 seed 的 spec：materialize no-op、ResolveExternalPaths
// 与改动前行为一致（不触碰 seed 链路）。
func (s *SwarmSeedSuite) Test基线空壳不回归() {
	spec := &schema.TeamAgentSpec{TeamName: "bare"}
	s.Require().NoError(spec.MaterializeBuildContext())
	s.Nil(spec.BuildContext, "无 seed 不得重建")

	ctx := &schema.TeamRuntimeContext{Role: schema.TeamRoleTeammate, MemberName: "beta"}
	cwd, _ := external.ResolveExternalPaths(spec, ctx, "/explicit/cwd", "bare")
	s.Equal("/explicit/cwd", cwd, "显式 cwd 优先路径不受 seed 链影响")
}
