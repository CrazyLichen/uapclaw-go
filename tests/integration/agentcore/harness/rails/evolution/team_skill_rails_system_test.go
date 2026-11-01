//go:build integration

package evolution_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"

	"github.com/stretchr/testify/require"
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

// patchMockClient 返回团队技能 patch JSON 的模拟 LLM 客户端
type patchMockClient struct {
	model_clients.BaseModelClient
	// response 固定响应内容
	response string
}

// Invoke 返回预置的 patch JSON
func (m *patchMockClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(m.response), nil
}

// Stream 未实现
func (m *patchMockClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

// newPatchMockModel 构造返回 patch JSON 的 *llm.Model
func newPatchMockModel(t *testing.T) *llm.Model {
	t.Helper()
	provider := fmt.Sprintf("mock_team_skill_patch_%d", time.Now().UnixNano())
	mockClient := &patchMockClient{response: `{"need_patch": true, "section": "Workflow", "title": "tighten handoff",
"summary": "handoff quality drifted", "content": "### Experience: tighten handoff\nRequire the leader to restate output format before merge.",
"reason": "handoff quality drifted during collaboration"}`}
	model_clients.GetClientRegistry().Register(provider, "llm", func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return mockClient, nil
	})
	model, err := llm.NewModel(
		&llmschema.ModelClientConfig{ClientProvider: provider, ClientID: provider + "_id"},
		&llmschema.ModelRequestConfig{ModelName: "mock-model"},
	)
	require.NoError(t, err, "创建 mock model 失败")
	return model
}

// TestTeamSkillEvolutionRail_审批后演化日志落盘
// 对齐 Python: test_team_skill_rail_generates_and_persists_patch_after_completion
//
// 链路：before_invoke 建轨迹 → 喂 read_file/spawn_member/view_task 工具调用
// → RequestUserEvolution 生成待审批 patch（返回 requestID + 审批事件）
// → ApproveRecord 落盘 → LoadFullEvolutionLog 断言 Workflow 章节与内容。
func (s *TeamSkillRailsSystemSuite) TestTeamSkillEvolutionRail_审批后演化日志落盘() {
	skillsDir := s.T().TempDir()
	skillDir := filepath.Join(skillsDir, "research-team")
	s.Require().NoError(os.MkdirAll(skillDir, 0o755))
	skillMD := "---\nname: research-team\ndescription: simple research team\nkind: team-skill\n---\n" +
		"# Workflow\n1. leader assigns tasks\n2. members execute\n"
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o600))

	rail := evolution.NewTeamSkillEvolutionRail(
		[]string{skillsDir},
		newPatchMockModel(s.T()),
		"mock-model",
		"cn",
		evolution.WithTeamSkillAutoScan(false),
		evolution.WithTeamSkillAutoSave(false),
		evolution.WithTeamSkillAsyncEvolution(false),
	)

	ctl := &recordingController{}
	agent := &teamLeaderAgent{ctl: ctl}
	ctx := context.Background()

	// 1. 建立轨迹会话
	invokeCbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.InvokeInputs{ConversationID: "team-evolve"}, nil)
	s.Require().NoError(rail.BeforeInvoke(ctx, invokeCbc))

	// 2. 工具调用序列（对齐 Python 用例）
	toolCalls := []*agentinterfaces.ToolCallInputs{
		{ToolName: "read_file", ToolArgs: map[string]any{"path": filepath.Join(skillDir, "SKILL.md")}, ToolResult: "loaded"},
		{ToolName: "spawn_member", ToolArgs: map[string]any{"name": "researcher"}, ToolResult: map[string]any{"status": "spawned"}},
		{ToolName: "view_task", ToolArgs: map[string]any{}, ToolResult: "task-a completed\ntask-b completed"},
	}
	for _, ti := range toolCalls {
		cbc := agentinterfaces.NewAgentCallbackContext(agent, ti, nil)
		s.Require().NoError(rail.AfterToolCall(ctx, cbc))
	}

	// 3. 用户主动请求演化（autoApprove=false → 应产生待审批）
	result, err := rail.RequestUserEvolution(ctx, "research-team", "tighten handoff", false)
	s.Require().NoError(err)
	s.Require().NotNil(result.RequestID, "应返回待审批 requestID")

	// 4. 审批通过 → 落盘
	s.Require().NoError(rail.ApproveRecord(ctx, *result.RequestID))

	// 5. 断言演化日志
	log, err := rail.EvolutionStore().LoadFullEvolutionLog(ctx, "research-team")
	s.Require().NoError(err)
	s.Require().Len(log.Entries, 1, "应写入 1 条演化记录")
	s.Equal("Workflow", log.Entries[0].Change.Section)
	s.Contains(log.Entries[0].Change.Content, "tighten handoff")
}
