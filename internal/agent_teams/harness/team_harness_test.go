package harness_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	harness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// mockDeepAgent 实现 DeepAgentInterface 的测试桩。
type mockDeepAgent struct {
	hinterfaces.DeepAgentInterface // 嵌入接口以部分满足，未调用的方法会 panic
}

// TestNewTeamHarness 测试 TeamHarness 构造函数。
func TestNewTeamHarness(t *testing.T) {
	var agent hinterfaces.DeepAgentInterface // nil 接口
	h := harness.NewTeamHarness(
		agent,
		string(atschema.TeamRoleLeader),
		"leader_1",
		false,
	)
	assert.NotNil(t, h)
	assert.Nil(t, h.InnerAgent())
	assert.Equal(t, string(atschema.TeamRoleLeader), h.Role())
	assert.Equal(t, "leader_1", h.MemberName())
}

// TestTeamHarness_RunAgentCustomizer 测试自定义钩子调用
// S-01: 统一签名 ctx + error
func TestTeamHarness_RunAgentCustomizer(t *testing.T) {
	var capturedAgent hinterfaces.DeepAgentInterface
	var capturedName string
	var capturedRole string
	called := false

	customizer := func(ctx context.Context, deepAgent hinterfaces.DeepAgentInterface, memberName string, roleValue string) error {
		called = true
		capturedAgent = deepAgent
		capturedName = memberName
		capturedRole = roleValue
		return nil
	}

	agent := &mockDeepAgent{}
	h := harness.NewTeamHarness(agent, string(atschema.TeamRoleLeader), "leader_1", false)
	h.RunAgentCustomizer(context.Background(), customizer)

	assert.True(t, called)
	assert.NotNil(t, capturedAgent)
	assert.Equal(t, "leader_1", capturedName)
	assert.Equal(t, "leader", capturedRole)
}

// TestTeamHarness_RunAgentCustomizer_NilAgent 测试 deepAgent 为 nil 时跳过钩子（S-06 守卫）
func TestTeamHarness_RunAgentCustomizer_NilAgent(t *testing.T) {
	called := false
	customizer := func(ctx context.Context, deepAgent hinterfaces.DeepAgentInterface, memberName string, roleValue string) error {
		called = true
		return nil
	}
	h := harness.NewTeamHarness(nil, string(atschema.TeamRoleLeader), "leader_1", false)
	h.RunAgentCustomizer(context.Background(), customizer)
	assert.False(t, called) // nil agent → 跳过
}

// TestTeamHarness_RunAgentCustomizer_Error吞掉 测试 customizer 返回 error 时只记日志不 panic
func TestTeamHarness_RunAgentCustomizer_Error吞掉(t *testing.T) {
	customizer := func(ctx context.Context, deepAgent hinterfaces.DeepAgentInterface, memberName string, roleValue string) error {
		return errors.New("customizer failed")
	}
	agent := &mockDeepAgent{}
	h := harness.NewTeamHarness(agent, string(atschema.TeamRoleLeader), "leader_1", false)
	assert.NotPanics(t, func() {
		h.RunAgentCustomizer(context.Background(), customizer)
	})
}

// TestTeamHarness_RunAgentCustomizer_Nil 测试 nil 自定义器不 panic
func TestTeamHarness_RunAgentCustomizer_Nil(t *testing.T) {
	h := harness.NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.NotPanics(t, func() {
		h.RunAgentCustomizer(context.Background(), nil)
	})
}

// TestTeamHarnessBuild 测试 Build 函数（25.1b：6 参数 + spec 驱动）。
func TestTeamHarnessBuild(t *testing.T) {
	h := harness.TeamHarnessBuild(
		atschema.DeepAgentSpec{},
		string(atschema.TeamRoleTeammate),
		"teammate_1",
		true, // initialPlanMode
		nil,  // buildCtx
		nil,  // taskEventTracker
	)
	assert.NotNil(t, h)
	assert.Equal(t, string(atschema.TeamRoleTeammate), h.Role())
	assert.Equal(t, "teammate_1", h.MemberName())
}

// TestTeamHarness_StubMethods 测试 TODO 占位方法返回零值
func TestTeamHarness_StubMethods(t *testing.T) {
	h := harness.NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.Nil(t, h.DeepConfig())
	assert.Nil(t, h.Workspace())
	assert.Nil(t, h.SysOperation())
	assert.Nil(t, h.Model())
	assert.False(t, h.HasPendingInterrupt())
	assert.Nil(t, h.FindRails(nil))
}

// TestTeamHarness_IsPendingInterruptResumeValid 测试中断恢复验证占位返回 false
func TestTeamHarness_IsPendingInterruptResumeValid(t *testing.T) {
	h := harness.NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.False(t, h.IsPendingInterruptResumeValid(nil))
	assert.False(t, h.IsPendingInterruptResumeValid("some input"))
}

// TestTeamHarness_PersistDBState 测试持久化占位方法不 panic
func TestTeamHarness_PersistDBState(t *testing.T) {
	h := harness.NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.NotPanics(t, func() {
		h.PersistTeamDBState()
		h.MarkTeamCleaned()
		h.MarkTeamBuilt()
		h.RequestCompletionPoll()
		h.WakeMailboxIfInterruptCleared()
		_ = h.InitCwdForRound(context.Background())
	})
}

// TestTeamHarness_Steer 测试 Steer 在无底层 DeepAgent 时返回可判定的错误
//
// M-02：投递失败必须可被上层识别（原实现返回 nil，DeliverResult 恒为 success）。
func TestTeamHarness_RegisterRail(t *testing.T) {
	h := harness.NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.Error(t, h.RegisterRail(context.TODO(), nil))
	// 类型正确但底层 Agent 缺失 → 同样报错
	assert.Error(t, h.RegisterRail(context.TODO(), agentinterfaces.NewBaseRail()))
}

// TestTeamHarness_UnregisterRail 测试 UnregisterRail 对非法入参返回错误
func TestTeamHarness_UnregisterRail(t *testing.T) {
	h := harness.NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.Error(t, h.UnregisterRail(context.TODO(), nil))
	assert.Error(t, h.UnregisterRail(context.TODO(), agentinterfaces.NewBaseRail()))
}

// TestTeamHarness_RegisterMemberTools_Nil 测试 nil memoryManager 不 panic
func TestTeamHarness_RegisterMemberTools_Nil(t *testing.T) {
	h := harness.NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.NotPanics(t, func() {
		h.RegisterMemberTools(nil)
	})
}

// TestTeamHarness_InjectMemberMemory_Nil 测试 nil memoryManager 返回 nil
func TestTeamHarness_InjectMemberMemory_Nil(t *testing.T) {
	h := harness.NewTeamHarness(nil, string(atschema.TeamRoleLeader), "", false)
	assert.NoError(t, h.InjectMemberMemory(context.TODO(), nil, ""))
}

// TestTeamHarness_RunStreaming_非BaseAgent降级空流 M-17 回归测试。
// deepAgent 非 nil 但未实现 BaseAgent 时应降级为关闭的空流（Warn 日志路径），
// 而非 panic 或阻塞。
