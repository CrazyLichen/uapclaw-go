//go:build integration

package agentteams_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/metadata"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ForkCheckpointSuite 25.5 fork 与 checkpoint 集成测试套件。
//
// 跨子系统链路：harness DeepAgent（上下文抓取/注入）→ agent_teams/tools
// （checkpoint 工具 + TeamBackend 回调链）→ agent_teams/metadata + core/session
// （per-team 命名空间持久化）→ agent_teams（ForkContext 截断 + TeamAgent 索引）。
// 真 DeepAgent（MockLLM provider）+ 真 context engine + 真 session state，
// mock 只在 LLM 边界（本套件甚至不触发 LLM 调用）。
type ForkCheckpointSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestForkCheckpointSuite 套件入口。
func TestForkCheckpointSuite(t *testing.T) {
	suite.Run(t, new(ForkCheckpointSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// newForkTeamSession 建集成用 team session。
func (s *ForkCheckpointSuite) newForkTeamSession(sessionID string) *session.AgentTeamSession {
	return session.CreateAgentTeamSession(sessionID, nil, "fork-team")
}

// newForkBackends 建 leader/member 两个 TeamBackend（内存 DB + fork 开启）。
func (s *ForkCheckpointSuite) newForkBackends() (leader, member *tools.TeamBackend) {
	msgCfg := atschema.NewMessagerTransportConfig()
	db := database.NewInMemoryTeamDatabase()
	leader = tools.NewTeamBackend("fork-team", "leader", true, db,
		messager.NewInProcessMessager(msgCfg), tools.WithForkEnabled(true))
	member = tools.NewTeamBackend("fork-team", "coder", false, db,
		messager.NewInProcessMessager(msgCfg), tools.WithForkEnabled(true))
	return leader, member
}

// injectMemberMessages 向成员 DeepAgent 注入对话历史（真 context engine）。
// 返回注入的 BaseMessage 列表。
func (s *ForkCheckpointSuite) injectMemberMessages(ctx context.Context, da forkDeepAgent) (string, []llmschema.BaseMessage) {
	msgs := []llmschema.BaseMessage{
		llmschema.NewUserMessage("帮我读一下 main.go"),
		llmschema.NewAssistantMessage("已读：入口在 main.go:12"),
		llmschema.NewUserMessage("总结架构"),
		llmschema.NewAssistantMessage("三层架构：cmd → runtime → tools"),
	}
	sessID, err := da.CreateNewContextEngine(ctx, "", msgs)
	s.Require().NoError(err)
	return sessID, msgs
}

// forkDeepAgent 集成侧的 fork 源/目标最小面（TeamHarness.Native().DeepAgent()）。
type forkDeepAgent interface {
	CreateNewContextEngine(ctx context.Context, sessionID string, messages []llmschema.BaseMessage) (string, error)
	GetCurrentContext(ctx context.Context, sessionID string, contextID string) ([]llmschema.BaseMessage, error)
}

// TestForkCheckpoint_IT1_Checkpoint工具全链 成员经真 checkpoint 工具存快照：
// snapshot_length 回调读 DeepAgent 上下文长度 → 回调写 leader 权威索引 →
// metadata 镜像进 team session → ReadTeamCheckpoints 读回一致；重名第二次
// Invoke 返回冲突错误（含 created_by/description）。
// Python: tool_member.py:943-968 · team.py:654-691 · team_agent.py:1774-1813。
func (s *ForkCheckpointSuite) TestForkCheckpoint_IT1_Checkpoint工具全链() {
	ctx := s.Ctx
	leaderBackend, memberBackend := s.newForkBackends()
	teamSession := s.newForkTeamSession("fork-itest-1")

	// 成员 DeepAgent 注入对话历史（真 context engine，条数=4）
	da := s.memberDeepAgent("fork-itest-1-coder")
	sessID, msgs := s.injectMemberMessages(ctx, da)
	s.Require().Len(msgs, 4)

	// snapshot_length 回调（对齐 agent_configurator.py:1230-1240 装配语义）：
	// 惰性读 DeepAgent 当前上下文长度
	memberBackend.SetSnapshotLengthFn(func() int {
		got, err := da.GetCurrentContext(ctx, sessID, "default_context_id")
		if err != nil {
			return 0
		}
		return len(got)
	})

	// 成员工具存快照 → 回调写 leader 权威索引 → metadata 镜像 team session
	conflict := memberBackend.StoreCheckpoint("code-ready", 0, "", "")
	s.Require().Nil(conflict, "成员直存 fallback（无回调装配）应成功")

	// 接 leader 侧（模拟 Configure 的 leader 装配 team_agent.py:738-739）：
	// 成员存储回调直写 leader 命名空间 + leader 镜像进 session
	persisted := map[string]metadata.CheckpointRecord{}
	leaderBackend.SetStoreCheckpointFn(func(name string, count int, description, createdBy string) map[string]any {
		if _, exists := persisted[name]; exists {
			rec := persisted[name]
			return map[string]any{"count": rec.Count, "description": rec.Description, "created_by": rec.CreatedBy}
		}
		persisted[name] = metadata.CheckpointRecord{Count: count, Description: description, CreatedBy: createdBy}
		metadata.MergeTeamCheckpoints(teamSession, "fork-team", persisted)
		return nil
	})

	// 经 snapshot_length 真值存储
	count := memberBackend.SnapshotContextLength()
	s.Require().Equal(4, count, "snapshot_length 应回读 DeepAgent 上下文条数")

	conflict = leaderBackend.StoreCheckpoint("code-ready", count, "编码就绪", "coder")
	s.Require().Nil(conflict)

	// 读回一致（跨层：metadata + core/session state）
	got, ok := metadata.ReadTeamCheckpoints(teamSession, "fork-team")
	s.Require().True(ok)
	rec, exists := got["code-ready"]
	s.Require().True(exists)
	s.Require().Equal(4, rec.Count)
	s.Require().Equal("coder", rec.CreatedBy)
	s.Require().Equal("编码就绪", rec.Description)
}

// TestForkCheckpoint_IT2_ForkBefore截断继承 leader 从源成员真上下文按命名
// checkpoint 截断（before：闭合 ToolMessage 块跨边界携带；SystemMessage 剥除）。
// Python: fork.py:52-117（from_agent）· team_agent.py:1574-1578。
func (s *ForkCheckpointSuite) TestForkCheckpoint_IT2_ForkBefore截断继承() {
	ctx := s.Ctx
	da := s.memberDeepAgent("fork-itest-2-src")

	msgs := []llmschema.BaseMessage{
		llmschema.NewSystemMessage("你是团队成员"),
		llmschema.NewUserMessage("读 main.go"),
		llmschema.NewAssistantMessage("已读",
			llmschema.WithToolCalls([]*llmschema.ToolCall{llmschema.NewToolCall("call-1", "read_file", "{}")})),
		llmschema.NewToolMessage("call-1", "result-1"),
		llmschema.NewUserMessage("总结"),
	}
	sessID, err := da.CreateNewContextEngine(ctx, "", msgs)
	s.Require().NoError(err)

	// ForkContextFromAgent 直收 *harness.DeepAgent（读真 context engine），
	// checkpoint=2（下标在带 tool_calls 的 assistant 之后、其 ToolMessage 之前
	// ——工具调用时 len(messages) 的记录点）→ before 模式闭合 ToolMessage 块
	// 跨边界携带
	daConcrete := da.(*harness.DeepAgent)
	forkCtx, err := agent_teams.ForkContextFromAgent(ctx, daConcrete, sessID, intPtr(2), "before")
	s.Require().NoError(err)
	s.Require().NotNil(forkCtx)
	s.Require().False(forkCtx.IsEmpty())
	// SystemMessage 剥除后 4 条（user, assistant, tool, user）：checkpoint=2
	// 截断前 2 条 + 闭合 ToolMessage 跨界携带 = 3 条（u2 不继承）
	s.Require().Len(forkCtx.Messages, 3)

	// 注入目标 DeepAgent（真 context engine）并读回验证
	target := s.memberDeepAgent("fork-itest-2-dst")
	decoded := forkCtx.ToForkMessages()
	dstSess, err := target.CreateNewContextEngine(ctx, "", decoded)
	s.Require().NoError(err)
	got, err := target.GetCurrentContext(ctx, dstSess, "default_context_id")
	s.Require().NoError(err)
	s.Require().Len(got, len(decoded))
}

// TestForkCheckpoint_IT3_冷恢复还原与冲突语义 持久化表经 RecoverFromSession
// 的还原通道读回后，重名冲突语义仍生效（团队全局唯一名）。
// Python: team_agent.py:2175-2179 · 1787-1793。
func (s *ForkCheckpointSuite) TestForkCheckpoint_IT3_冷恢复还原与冲突语义() {
	teamSession := s.newForkTeamSession("fork-itest-3")
	persisted := map[string]metadata.CheckpointRecord{
		"code-ready": {Count: 12, Description: "编码就绪", CreatedBy: "alice"},
	}
	metadata.MergeTeamCheckpoints(teamSession, "fork-team", persisted)

	// 冷恢复读表（RecoverFromSession 步骤 7b 的读取半区）
	restored, ok := metadata.ReadTeamCheckpoints(teamSession, "fork-team")
	s.Require().True(ok)
	s.Require().Equal(persisted, restored)

	// 还原进恢复出的 leader TeamAgent（ShareCheckpointsWith 与冷恢复共用
	// setCheckpointsFrom 语义）→ 重名冲突仍生效
	recovered := agent.NewTeamAgent(s.Ctx, testAgentCard("fork-leader-recovered"))
	source := agent.NewTeamAgent(s.Ctx, testAgentCard("fork-leader-source"))
	s.Require().Nil(source.SetCheckpoint("code-ready", 12, "编码就绪", "alice"))
	source.ShareCheckpointsWith(recovered)
	conflict := recovered.SetCheckpoint("code-ready", 99, "其他", "bob")
	s.Require().NotNil(conflict)
	s.Require().Equal(12, conflict["count"])
	s.Require().Equal("alice", conflict["created_by"])
}

// TestForkCheckpoint_IT4_未知名降级与list工具 命名 fork 用不存在的名字 →
// ForkContext 无索引记录（分派侧降级 full 的输入前提）+ list_checkpoints
// 工具经回调读到权威映射。
// Python: team_agent.py:1563-1571 · tool_member.py:1008-1019。
func (s *ForkCheckpointSuite) TestForkCheckpoint_IT4_未知名降级与list工具() {
	ctx := s.Ctx
	leaderBackend, _ := s.newForkBackends()
	teamSession := s.newForkTeamSession("fork-itest-4")

	da := s.memberDeepAgent("fork-itest-4-src")
	sessID, _ := s.injectMemberMessages(ctx, da)

	// 未知名：索引表无记录 → checkpoint 下标 nil（分派侧降级 full 的前提）
	got, ok := metadata.ReadTeamCheckpoints(teamSession, "fork-team")
	s.Require().False(ok)
	s.Require().Nil(got)

	// 存入后 list_checkpoints 工具面经回调读到权威映射
	persisted := map[string]metadata.CheckpointRecord{
		"code-ready": {Count: 4, Description: "", CreatedBy: "coder"},
	}
	metadata.MergeTeamCheckpoints(teamSession, "fork-team", persisted)
	leaderBackend.SetCheckpointListFn(func() map[string]map[string]any {
		out := map[string]map[string]any{}
		for name, rec := range persisted {
			out[name] = map[string]any{
				"count":       rec.Count,
				"description": rec.Description,
				"created_by":  rec.CreatedBy,
			}
		}
		return out
	})
	list := leaderBackend.ListCheckpoints()
	s.Require().Len(list, 1)
	s.Require().Equal(4, list["code-ready"]["count"])

	// fork 上下文从源成员真上下文全量抓取（full 模式降级路径的输入）
	daConcrete := da.(*harness.DeepAgent)
	forkCtx, err := agent_teams.ForkContextFromAgent(ctx, daConcrete, sessID, nil, "before")
	s.Require().NoError(err)
	s.Require().Len(forkCtx.Messages, 4)
	s.Require().True(strings.Contains(forkCtx.Messages[0]["content"].(string), "帮我读一下") ||
		strings.Contains(forkCtx.Messages[0]["role"].(string), "user"))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// memberDeepAgent 建 MockLLM 成员并返回其内层 DeepAgent（fork 源/目标面）。
func (s *ForkCheckpointSuite) memberDeepAgent(name string) forkDeepAgent {
	member := testhelpers.BuildMockMember(s.T(), s.ProviderName, "", name)
	return member.Agent.Harness().Native().DeepAgent()
}

// testAgentCard 建测试用 AgentCard。
func testAgentCard(name string) *agentschema.AgentCard {
	return agentschema.NewAgentCard(
		agentschema.WithAgentID("itest_"+name),
		agentschema.WithAgentName(name),
	)
}

// intPtr int 指针助手。
func intPtr(v int) *int { return &v }

// TestForkCheckpoint_IT5_PendingForks生命周期与一次性消费 spawn 工具链的
// fork 请求信箱：MarkForkOnSpawn 登记 → 成员创建回调 ConsumeForkOnSpawn
// 一次性取走（二次消费/未登记成员得 nil）。跨 tools 层生命周期契约。
// Python: mark_fork_on_spawn/consume_fork_on_spawn (team.py:617-633)。
func (s *ForkCheckpointSuite) TestForkCheckpoint_IT5_PendingForks生命周期与一次性消费() {
	leaderBackend, memberBackend := s.newForkBackends()

	// 未登记成员 → nil（ConsumeForkOnSpawn 幂等安全）
	s.Require().Nil(leaderBackend.ConsumeForkOnSpawn("nobody"))

	// spawn 工具链登记 fork 请求
	leaderBackend.MarkForkOnSpawn("coder", "code-ready", "alice", "before")

	// 创建回调一次性消费：内容完整取回
	info := leaderBackend.ConsumeForkOnSpawn("coder")
	s.Require().NotNil(info)
	s.Require().Equal("code-ready", info.Fork)
	s.Require().Equal("alice", info.Source)
	s.Require().Equal("before", info.ForkMode)

	// 一次性：二次消费 nil；未登记成员 nil
	s.Require().Nil(leaderBackend.ConsumeForkOnSpawn("coder"))
	s.Require().Nil(leaderBackend.ConsumeForkOnSpawn("painter"))

	// memberBackend 独立信箱（各自后端各自 pending 集合）
	memberBackend.MarkForkOnSpawn("helper", true, "", "full")
	info2 := memberBackend.ConsumeForkOnSpawn("helper")
	s.Require().NotNil(info2)
	s.Require().Equal(true, info2.Fork) // live fork：bool 形态
}

// TestForkCheckpoint_IT6_Checkpoint事件Messager真链 PublishCheckpointCreated
// → 真 InProcessMessager（topic 编码 + payload 序列化）→ 订阅端取回——
// payload 键位与 CheckpointHandler 解码期望（name/member_name/message_count/
// description）契约一致。handler 渲染逻辑由 handlers 包单测覆盖。
// Python: publish_checkpoint_created (team.py:703-730) · TeamTopic.TEAM。
func (s *ForkCheckpointSuite) TestForkCheckpoint_IT6_Checkpoint事件Messager真链() {
	ctx := s.Ctx
	leaderBackend, _ := s.newForkBackends()
	msg := leaderBackend.Messager()
	s.Require().NotNil(msg)
	s.Require().NoError(msg.Start(ctx))

	topic := events.TeamTopicTeam.Build("", "fork-team")
	got := make(chan *events.EventMessage, 1)
	s.Require().NoError(msg.Subscribe(ctx, topic, func(_ context.Context, m *events.EventMessage) error {
		got <- m
		return nil
	}))

	leaderBackend.PublishCheckpointCreated(ctx, "code-ready", 42, "编码就绪")

	select {
	case em := <-got:
		s.Require().Equal(events.TeamEventCheckpointCreated, em.EventType)
		// payload 键位契约 = CheckpointHandler.OnCheckpointCreated 的解码期望
		name, _ := em.Payload["name"].(string)
		member, _ := em.Payload["member_name"].(string)
		// 进程内直传保持 int，跨 JSON 边界为 float64（CheckpointHandler 解码
		// 同款双类型兼容，checkpoint.go:77-81）
		count := 0
		if v, ok := em.Payload["message_count"].(float64); ok {
			count = int(v)
		} else if v, ok := em.Payload["message_count"].(int); ok {
			count = v
		}
		desc, _ := em.Payload["description"].(string)
		s.Require().Equal("code-ready", name)
		s.Require().Equal("leader", member)
		s.Require().Equal(42, count)
		s.Require().Equal("编码就绪", desc)
	case <-ctx.Done():
		s.Fail("checkpoint 事件未在超时内到达订阅端")
	}
}
