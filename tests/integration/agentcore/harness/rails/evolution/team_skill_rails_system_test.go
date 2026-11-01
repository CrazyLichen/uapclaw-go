//go:build integration

package evolution_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamSkillRailsSystemSuite 团队技能 rails 系统测试。
//
// 对齐 Python: tests/system_tests/harness/rail/test_team_skill_rails_system.py
// 的第一个用例（spawn 达阈值 → follow-up 入队）。
//
// 本用例同时是 getLoopController 修复的回归守卫：修复前 LoopController 解析恒为 nil，
// follow-up 永不入队。
type TeamSkillRailsSystemSuite struct {
	isuite.BaseIntegrationSuite
}

// recordingController 记录 EnqueueFollowUp 调用；嵌入控制器接口满足其余方法。
type recordingController struct {
	controller.ControllerInterface
	// followUps 已入队的 prompt
	followUps []string
}

// EnqueueFollowUp 记录入队内容
func (r *recordingController) EnqueueFollowUp(msg string) { r.followUps = append(r.followUps, msg) }

// teamLeaderAgent 只覆写 LoopController 的测试 agent。
type teamLeaderAgent struct {
	agentinterfaces.BaseAgent
	// ctl 循环控制器
	ctl controller.ControllerInterface
}

// Card 返回 Agent 卡片（避免嵌入 nil 接口导致的空指针）
func (a *teamLeaderAgent) Card() *agentschema.AgentCard {
	return agentschema.NewAgentCard(agentschema.WithAgentID("team-leader"))
}

// LoopController 返回循环控制器（签名对齐 DeepAgent.LoopController）
func (a *teamLeaderAgent) LoopController() controller.ControllerInterface { return a.ctl }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTeamSkillRailsSystemSuite 运行团队技能 rails 系统测试
func TestTeamSkillRailsSystemSuite(t *testing.T) {
	suite.Run(t, new(TeamSkillRailsSystemSuite))
}

// TestTeamSkillCreateRail_达阈值入队followup
// 对齐 Python: test_team_skill_create_rail_queues_follow_up_after_spawn_threshold
func (s *TeamSkillRailsSystemSuite) TestTeamSkillCreateRail_达阈值入队followup() {
	skillsDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(skillsDir,
		evolution.WithTeamSkillCreateMinTeamMembers(2),
	)

	ctl := &recordingController{}
	agent := &teamLeaderAgent{ctl: ctl}
	ctx := context.Background()

	// 1. before_invoke 初始化轨迹（产出 sessionID）
	invokeCbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.InvokeInputs{ConversationID: "team-create"}, nil)
	s.Require().NoError(rail.BeforeInvoke(ctx, invokeCbc))

	// 2. 两次 spawn_member 工具调用
	for i := 0; i < 2; i++ {
		toolCbc := agentinterfaces.NewAgentCallbackContext(agent,
			&agentinterfaces.ToolCallInputs{
				ToolName:   "spawn_member",
				ToolArgs:   map[string]any{"name": "worker"},
				ToolResult: map[string]any{"status": "spawned"},
			}, nil)
		s.Require().NoError(rail.AfterToolCall(ctx, toolCbc))
	}

	// 3. 标记团队完成，再触发任务迭代回调
	s.True(rail.NotifyTeamCompleted(invokeCbc), "满足阈值时应标记团队完成")
	s.Require().NoError(rail.OnAfterTaskIteration(ctx, invokeCbc))

	// 4. 断言入队：恰 1 条，含 ask_user 与 team-skill-creator
	s.Require().Len(ctl.followUps, 1, "应恰有 1 条 follow-up 入队")
	s.Contains(ctl.followUps[0], "ask_user")
	s.Contains(ctl.followUps[0], "team-skill-creator")

	// 5. 防重复：再次触发不应二次入队
	s.Require().NoError(rail.OnAfterTaskIteration(ctx, invokeCbc))
	s.Len(ctl.followUps, 1, "同一会话重复触发不应重复入队")
}

// TestTeamSkillCreateRail_未达阈值不入队 对齐 Python 阈值语义的反向用例
func (s *TeamSkillRailsSystemSuite) TestTeamSkillCreateRail_未达阈值不入队() {
	skillsDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(skillsDir,
		evolution.WithTeamSkillCreateMinTeamMembers(2),
	)

	ctl := &recordingController{}
	agent := &teamLeaderAgent{ctl: ctl}
	ctx := context.Background()

	invokeCbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.InvokeInputs{ConversationID: "team-create-2"}, nil)
	s.Require().NoError(rail.BeforeInvoke(ctx, invokeCbc))

	// 仅 1 次 spawn_member，未达阈值 2
	toolCbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.ToolCallInputs{
			ToolName:   "spawn_member",
			ToolArgs:   map[string]any{"name": "worker"},
			ToolResult: map[string]any{"status": "spawned"},
		}, nil)
	s.Require().NoError(rail.AfterToolCall(ctx, toolCbc))

	s.True(rail.NotifyTeamCompleted(invokeCbc))
	s.Require().NoError(rail.OnAfterTaskIteration(ctx, invokeCbc))

	s.Empty(ctl.followUps, "未达阈值不应入队")
}

// TestTeamSkillEvolutionRail_审批后演化日志落盘
// 对齐 Python: test_team_skill_rail_generates_and_persists_patch_after_completion
//
// 未迁移原因（如实记录，不用弱断言伪装）：Python 用例通过
// rail.after_invoke → drain_pending_approval_events() → on_approve_record(request_id) 驱动；
// Go 侧的审批事件由 EvolutionRail 内部 ApprovalRuntime 持有，事件取出入口依赖
// pendingHostEvents/审批快照内部状态，外部包在零生产改动前提下无法稳定驱动该链路。
// 该链路的包内单测已覆盖 ApproveRecord/RejectRecord 分支
// （见 internal/.../team_skill_evolution_rail_test.go 的 TestApprovalLifecycle）。
func (s *TeamSkillRailsSystemSuite) TestTeamSkillEvolutionRail_审批后演化日志落盘() {
	s.T().Skip("审批→演化日志落盘链路需内部审批事件出口，零生产改动下无法从外部包稳定驱动；包内已有 ApproveRecord 分支覆盖")
}
