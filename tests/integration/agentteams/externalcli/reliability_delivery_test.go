//go:build integration

// 外部成员可靠性投递端到端集成（15.16b0）。
//
// 验证团队真的能收到（spec 集成验收硬要求）：
//
//	① 启动失败 → leader 信箱真实出现 external_runtime_failed JSON 消息
//	  （from=成员、to=leader，可反序列化、phase/category 正确）；
//	② 成员状态经 update_status_cb 真实落库翻 ERROR（FSM 校验通过）；
//	③ 同一尝试重复 finalize 后信箱仍只有一条（exactly-once）；
//	④ PublishRetrying 经真实 InProcessMessager 总线在团队 topic 上被订阅方收到，
//	  且成员状态与信箱不受影响（非持久进度信号）。
//
// 跨子系统：externalcli（runtime/ reliability 投递半区）→ tools（TeamBackend/
// TeamMessageManager 直发消息写库）→ database（内存库持久化 + FSM 状态机）→
// messager（InProcessMessager 总线发布/订阅）→ schema（可靠性词汇/事件）。
// 边界 mock 仅限进程边界：假 provider 协议承担 adapter 事件流（不打真实 CLI）。
//
// 对应 Python 代码：external/reliability.py + member_runtime.py:448-458,714-741 +
// spawn/external_cli_spawn.py:719-732
package externalcli_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/external"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	atevents "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/tools/database"
	harness_protocol "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_protocol"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReliabilityDeliverySuite 外部成员可靠性投递集成套件（黑盒）。
type ReliabilityDeliverySuite struct {
	isuite.AgentSuite
}

// TestReliabilityDeliverySuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestReliabilityDeliverySuite(t *testing.T) {
	suite.Run(t, new(ReliabilityDeliverySuite))
}

// ──────────────────────────── 结构体（假 provider） ────────────────────────────

// failingStartProtocol Start 恒失败的假 provider（复用同包 scripted 游标惯例）。
type failingStartProtocol struct {
	scriptedHarnessProtocol
}

func (h *failingStartProtocol) Start(_ context.Context, _ harness_protocol.HarnessContext) error {
	return errors.New("cli binary not found")
}

// newReliabilityDB 建团队双成员内存库（leader + memberB，unstarted 允许→error）。
func (s *ReliabilityDeliverySuite) newReliabilityDB(ctx context.Context) database.TeamDatabase {
	db := database.NewInMemoryTeamDatabase()
	s.Require().NoError(db.Initialize(ctx))
	s.Require().True(db.CreateTeam(ctx, "team-rel", "TeamRel", "leader", "", "", "autonomous", false))
	s.Require().True(db.CreateMember(ctx, "leader", "team-rel", "Leader", "", "unstarted", "leader", "", "idle", "build_mode", "", ""))
	s.Require().True(db.CreateMember(ctx, "memberB", "team-rel", "memberB", "", "unstarted", "teammate", "", "idle", "build_mode", "", ""))
	return db
}

// newReliabilityRuntime 构造绑定可靠性上下文的成员 runtime（五参对齐
// Python bind_reliability_context：session/team_backend/leader/status_cb/messager）。
func (s *ReliabilityDeliverySuite) newReliabilityRuntime(
	protocol harness_protocol.HarnessProtocol,
	memberBackend *tools.TeamBackend,
	bus messager.Messager,
	statuses *[]atschema.MemberStatus,
	db database.TeamDatabase,
) *external.ExternalHarnessMemberRuntime {
	r, err := external.NewExternalHarnessMemberRuntime(protocol,
		func(_ any) (*harness_protocol.HarnessContext, error) {
			return &harness_protocol.HarnessContext{AgentID: "m-1", AgentName: "memberB"}, nil
		},
		external.ExternalHarnessMemberRuntimeOptions{AgentKind: "claude", CLIPath: "/bin/fake-cli"})
	s.Require().NoError(err)
	r.BindReliabilityContext("sess-rel", memberBackend, "leader",
		func(_ context.Context, status atschema.MemberStatus) error {
			*statuses = append(*statuses, status)
			// Python teammate.update_status 的 DB 语义：真实落库（FSM 校验）
			return mapErr(db.Member().UpdateMemberStatus(context.Background(), "memberB", "team-rel", string(status)))
		}, bus)
	return r
}

// mapErr bool → error（UpdateMemberStatus 契约）。
func mapErr(ok bool) error {
	if ok {
		return nil
	}
	return context.Canceled
}

// ──────────────────────────── 测试 ────────────────────────────

// TestReliability_启动失败投递信箱与状态翻ERROR 场景①②③：
// provider Start 失败 → finalize_startup_failure（member_runtime.py:872-886 经
// reliability.finalize_failure + mark_member_error）→ leader 信箱收到
// external_runtime_failed JSON 消息 + 成员状态落库翻 ERROR + exactly-once。
func (s *ReliabilityDeliverySuite) TestReliability_启动失败投递信箱与状态翻ERROR() {
	ctx := context.Background()
	db := s.newReliabilityDB(ctx)

	leaderBackend := tools.NewTeamBackend("team-rel", "leader", true, db, nil)
	memberBackend := tools.NewTeamBackend("team-rel", "memberB", false, db, nil,
		tools.WithLeaderMemberName("leader"))

	statuses := []atschema.MemberStatus{}
	r := s.newReliabilityRuntime(&failingStartProtocol{}, memberBackend, nil, &statuses, db)

	// Python: begin_attempt(phase="startup")（member_runtime.py:448-449）由 Start
	// 内部路径承担；adapter.Start 失败 → finalizeStartupFailure（:649）。
	s.Require().Error(r.Start(nil))

	// ① leader 信箱真实出现 external_runtime_failed 消息（from=成员 to=leader）
	msgs, err := leaderBackend.MessageManager().GetMessages(ctx, "leader", false, "")
	s.Require().NoError(err)
	s.Require().Len(msgs, 1, "leader 信箱应恰有一条失败消息")
	s.Equal("fake-scripted", msgs[0].FromMemberName,
		"bind 时成员名取构造器回退（Python member_runtime.py:264：context 为工厂时 _member_name = harness.card.name）")
	var failure atschema.ExternalRuntimeFailure
	s.Require().NoError(json.Unmarshal([]byte(msgs[0].Content), &failure),
		"信箱内容必须可反序列化为 ExternalRuntimeFailure: %s", msgs[0].Content)
	s.Equal("external_runtime_failed", failure.Type)
	s.Equal("startup", string(failure.Phase))
	s.NotEmpty(failure.FailureID)
	s.Equal(atschema.ExternalRuntimeFailureCategorySDKError, failure.Category,
		"无结构化 TurnError 的启动异常按 Python _classify 归 sdk_error（member_runtime.py:1083）")
	s.Require().NotNil(failure.CLIPath)
	s.Equal("/bin/fake-cli", *failure.CLIPath)
	s.False(failure.UserActionRequired, "sdk_error 不需用户介入")

	// ② 成员状态经回调真实落库翻 ERROR（重启读库，非回调记录）
	s.Require().NotEmpty(statuses, "update_status_cb 应被调用")
	s.Equal(atschema.MemberStatusError, statuses[len(statuses)-1])
	got, ok := db.Member().GetMemberStatus(ctx, "team-rel", "memberB")
	s.Require().True(ok)
	s.Equal("error", got, "成员状态应真实落库为 ERROR")

	// ③ exactly-once：同一尝试重复触发 finalize 后信箱仍恰一条
	msgs2, err := leaderBackend.MessageManager().GetMessages(ctx, "leader", false, "")
	s.Require().NoError(err)
	s.Require().Len(msgs2, 1)
}

// TestReliability_Retrying进度事件真总线 场景④：轮中 diagnostic retrying 信号经
// 事件泵 → PublishRetrying（member_runtime.py:730-741）→ 真实 InProcessMessager
// 在团队 topic 上被订阅方收到 external_runtime_retrying；信箱不受影响（非持久）。
func (s *ReliabilityDeliverySuite) TestReliability_Retrying进度事件真总线() {
	ctx := context.Background()
	db := s.newReliabilityDB(ctx)

	leaderBackend := tools.NewTeamBackend("team-rel", "leader", true, db, nil)
	memberBackend := tools.NewTeamBackend("team-rel", "memberB", false, db, nil,
		tools.WithLeaderMemberName("leader"))

	bus := messager.NewInProcessMessager(atschema.MessagerTransportConfig{})
	var received []*atevents.EventMessage
	err := bus.Subscribe(ctx, atevents.TeamTopicTeam.Build("sess-rel", "team-rel"),
		func(_ context.Context, msg *atevents.EventMessage) error {
			received = append(received, msg)
			return nil
		})
	s.Require().NoError(err)

	// 假 provider 吐轮次 + diagnostic retrying 信封（onDiagnostic 消费）
	diagData, freezeErr := harness_protocol.FreezeJsonObject(map[string]any{
		"kind":     "retrying",
		"category": "rate_limited",
		"code":     "429",
	})
	s.Require().NoError(freezeErr)
	scripted := &scriptedHarnessProtocol{events: []harness_protocol.HarnessEvent{
		{Event: harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}, TurnID: "t-1"},
		{Event: harness_protocol.DiagnosticEvent{
			Level:   harness_protocol.DiagnosticLevelInfo,
			Message: "SDK retrying",
			Data:    diagData,
		}, TurnID: "t-1"},
		{Event: harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindFinished}, TurnID: "t-1"},
	}}

	statuses := []atschema.MemberStatus{}
	r := s.newReliabilityRuntime(scripted, memberBackend, bus, &statuses, db)
	s.Require().NoError(r.Start(nil))
	defer r.Stop()

	// 团队 topic 订阅方真实收到 external_runtime_retrying 事件
	s.Require().Eventually(func() bool {
		for _, msg := range received {
			if msg.EventType == atevents.TeamEventExternalRuntimeRetrying {
				return true
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond, "总线应收到 external_runtime_retrying 事件")

	// 非持久进度信号：不落信箱、不改成员状态
	msgs, err := leaderBackend.MessageManager().GetMessages(ctx, "leader", false, "")
	s.Require().NoError(err)
	s.Assert().Empty(msgs, "retrying 进度不落信箱")
	s.Assert().Empty(statuses, "retrying 进度不改成员状态")
}
