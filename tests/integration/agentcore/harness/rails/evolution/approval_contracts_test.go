//go:build integration

package evolution

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	evolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ApprovalRuntimeSuite 测试 EvolutionApprovalRuntime 审批运行时。
//
// 对齐 Python: tests/unit_tests/harness/rails/evolution/test_evolution_approval_runtime.py
//   - LookupPendingApprovalSnapshot
//   - ApprovePendingRequest
//   - RejectPendingRequest
//   - FinalizeStagedEvolutionRequest（requires_approval / auto_approved 路径）
type ApprovalRuntimeSuite struct {
	isuite.BaseIntegrationSuite
	// manager mock 审批管理器
	manager *fakeApprovalManager
	// runtime 审批运行时
	runtime *evolution.EvolutionApprovalRuntime
	// pendingSnapshots 暂存审批快照
	pendingSnapshots evolution.PendingApprovalSnapshotStore
}

// fakeApprovalManager mock 审批管理器，实现 ApprovalManager 接口。
type fakeApprovalManager struct {
	approveErr    error
	rejectErr     error
	approveResult experience.ExperienceApplyResult
	rejectResult  experience.ExperienceApplyResult
}

func (m *fakeApprovalManager) ApproveRequest(_ context.Context, _ string) (experience.ExperienceApplyResult, error) {
	return m.approveResult, m.approveErr
}

func (m *fakeApprovalManager) RejectRequest(_ context.Context, _ string) (experience.ExperienceApplyResult, error) {
	return m.rejectResult, m.rejectErr
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestApprovalRuntimeSuite 运行审批运行时集成测试套件。
func TestApprovalRuntimeSuite(t *testing.T) {
	suite.Run(t, new(ApprovalRuntimeSuite))
}

func (s *ApprovalRuntimeSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
}

func (s *ApprovalRuntimeSuite) TearDownSuite() {
	s.BaseIntegrationSuite.TearDownSuite()
}

func (s *ApprovalRuntimeSuite) SetupTest() {
	s.manager = &fakeApprovalManager{
		approveResult: experience.ExperienceApplyResult{AppliedCount: 1},
		rejectResult:  experience.ExperienceApplyResult{AppliedCount: 1},
	}
	s.pendingSnapshots = make(evolution.PendingApprovalSnapshotStore)
	s.runtime = evolution.NewEvolutionApprovalRuntime(s.manager, s.pendingSnapshots)
}

// TestApprovalRuntime_LookupPendingApprovalSnapshot存在 测试查找存在的暂存快照。
func (s *ApprovalRuntimeSuite) TestApprovalRuntime_LookupPendingApprovalSnapshot存在() {
	s.pendingSnapshots["req_1"] = &experience.PendingChange{SkillName: "skill_a"}
	pending := s.runtime.LookupPendingApprovalSnapshot("req_1", "test_rail", "test_action")
	s.NotNil(pending)
	s.Equal("skill_a", pending.SkillName)
}

// TestApprovalRuntime_LookupPendingApprovalSnapshot不存在 测试查找不存在的暂存快照返回 nil。
func (s *ApprovalRuntimeSuite) TestApprovalRuntime_LookupPendingApprovalSnapshot不存在() {
	pending := s.runtime.LookupPendingApprovalSnapshot("nonexistent", "test_rail", "test_action")
	s.Nil(pending)
}

// TestApprovalRuntime_ApprovePendingRequest成功 测试批准请求成功路径。
func (s *ApprovalRuntimeSuite) TestApprovalRuntime_ApprovePendingRequest成功() {
	s.pendingSnapshots["req_2"] = &experience.PendingChange{SkillName: "skill_b"}
	pending, result, err := s.runtime.ApprovePendingRequest(s.Ctx, "req_2", "rail", "action")
	s.Require().NoError(err)
	s.NotNil(pending)
	s.NotNil(result)
	s.Equal("skill_b", pending.SkillName)
}

// TestApprovalRuntime_ApprovePendingRequest不存在 测试批准不存在的请求返回 nil。
func (s *ApprovalRuntimeSuite) TestApprovalRuntime_ApprovePendingRequest不存在() {
	pending, result, err := s.runtime.ApprovePendingRequest(s.Ctx, "nonexistent", "rail", "action")
	s.Require().NoError(err)
	s.Nil(pending)
	s.Nil(result)
}

// TestApprovalRuntime_RejectPendingRequest成功 测试拒绝请求成功路径。
func (s *ApprovalRuntimeSuite) TestApprovalRuntime_RejectPendingRequest成功() {
	s.pendingSnapshots["req_3"] = &experience.PendingChange{SkillName: "skill_c"}
	pending, result, err := s.runtime.RejectPendingRequest(s.Ctx, "req_3", "rail", "action")
	s.Require().NoError(err)
	s.NotNil(pending)
	s.NotNil(result)
}

// TestApprovalRuntime_RejectPendingRequest不存在 测试拒绝不存在的请求返回 nil。
func (s *ApprovalRuntimeSuite) TestApprovalRuntime_RejectPendingRequest不存在() {
	pending, result, err := s.runtime.RejectPendingRequest(s.Ctx, "nonexistent", "rail", "action")
	s.Require().NoError(err)
	s.Nil(pending)
	s.Nil(result)
}

// TestApprovalRuntime_FinalizeStagedEvolutionRequest需要审批 测试需要审批路径调用 emit。
func (s *ApprovalRuntimeSuite) TestApprovalRuntime_FinalizeStagedEvolutionRequest需要审批() {
	request := &experience.ExperienceApprovalRequest{
		RequestID: "req_4",
	}
	emitCalled := false
	emitFn := func(req *experience.ExperienceApprovalRequest) error {
		emitCalled = true
		s.Equal("req_4", req.RequestID)
		return nil
	}

	err := s.runtime.FinalizeStagedEvolutionRequest(request, true, emitFn, nil)
	s.Require().NoError(err)
	s.True(emitCalled)
}

// TestApprovalRuntime_FinalizeStagedEvolutionRequest自动审批 测试自动审批路径调用 onAutoApproved。
func (s *ApprovalRuntimeSuite) TestApprovalRuntime_FinalizeStagedEvolutionRequest自动审批() {
	request := &experience.ExperienceApprovalRequest{
		RequestID: "req_5",
	}
	autoApproved := false
	onAutoApprovedFn := func(req *experience.ExperienceApprovalRequest) error {
		autoApproved = true
		s.Equal("req_5", req.RequestID)
		return nil
	}

	err := s.runtime.FinalizeStagedEvolutionRequest(request, false, nil, onAutoApprovedFn)
	s.Require().NoError(err)
	s.True(autoApproved)
}

// TestApprovalRuntime_FinalizeStagedEvolutionRequestNil 测试 request 为 nil 时空操作。
func (s *ApprovalRuntimeSuite) TestApprovalRuntime_FinalizeStagedEvolutionRequestNil() {
	err := s.runtime.FinalizeStagedEvolutionRequest(nil, true, nil, nil)
	s.Require().NoError(err)
}

// TestApprovalRuntime_FinalizeStagedEvolutionRequestEmit失败不中断 测试 emit 回调失败时不中断。
func (s *ApprovalRuntimeSuite) TestApprovalRuntime_FinalizeStagedEvolutionRequestEmit失败不中断() {
	request := &experience.ExperienceApprovalRequest{RequestID: "req_6"}
	emitFn := func(_ *experience.ExperienceApprovalRequest) error {
		return context.DeadlineExceeded
	}

	// 即使 emit 失败，FinalizeStagedEvolutionRequest 也应返回 nil
	err := s.runtime.FinalizeStagedEvolutionRequest(request, true, emitFn, nil)
	s.Require().NoError(err)
}

// ──────────────────────────── Contracts 测试 ────────────────────────────

// ContractsSuite 测试 Evolution 契约类型序列化/转换。
//
// 对齐 Python: tests/unit_tests/harness/rails/evolution/test_contracts.py
type ContractsSuite struct {
	isuite.BaseIntegrationSuite
}

// TestContractsSuite 运行契约类型集成测试套件。
func TestContractsSuite(t *testing.T) {
	suite.Run(t, new(ContractsSuite))
}

// TestEvolutionSnapshot_ToLegacyDict 测试快照转换为 dict 形态。
func (s *ContractsSuite) TestEvolutionSnapshot_ToLegacyDict() {
	skillName := "my-skill"
	snapshot := evolution.EvolutionSnapshot{
		Trajectory: &trajectory.Trajectory{SessionID: "session-1"},
		Messages:   []map[string]any{{"role": "user", "content": "hello"}},
		SkillName:  &skillName,
		SessionID:  "session-1",
	}

	dict := snapshot.ToLegacyDict()
	s.Equal(snapshot.Trajectory, dict["trajectory"])
	s.Equal(snapshot.Messages, dict["messages"])
	s.Equal("my-skill", dict["skill_name"])
	s.Equal("session-1", dict["session_id"])
}

// TestEvolutionSnapshot_ToLegacyDict空字段 测试空字段不写入 dict。
func (s *ContractsSuite) TestEvolutionSnapshot_ToLegacyDict空字段() {
	snapshot := evolution.EvolutionSnapshot{
		Trajectory: &trajectory.Trajectory{SessionID: "s1"},
	}

	dict := snapshot.ToLegacyDict()
	s.Equal(snapshot.Trajectory, dict["trajectory"])
	_, hasSkillName := dict["skill_name"]
	s.False(hasSkillName)
	_, hasSessionID := dict["session_id"]
	s.False(hasSessionID)
}

// TestFromLegacyDict 测试从 dict 恢复 EvolutionSnapshot。
func (s *ContractsSuite) TestFromLegacyDict() {
	skillName := "test-skill"
	dict := map[string]any{
		"trajectory": &trajectory.Trajectory{SessionID: "s2"},
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
		"skill_name": skillName,
		"session_id": "s2",
	}

	snapshot := evolution.FromLegacyDict(dict)
	s.NotNil(snapshot.Trajectory)
	s.Equal("s2", snapshot.Trajectory.SessionID)
	s.Equal("test-skill", *snapshot.SkillName)
	s.Equal("s2", snapshot.SessionID)
}

// TestEvolutionHostEventMeta_ToPayload 测试元数据转换为 payload。
func (s *ContractsSuite) TestEvolutionHostEventMeta_ToPayload() {
	railKind := "regular"
	stage := "completed"
	status := "failed"

	meta := evolution.EvolutionHostEventMeta{
		EventKind: evolution.EvolutionEventKindOutcome,
		RailKind:  &railKind,
		Stage:     &stage,
		Status:    &status,
	}

	payload := meta.ToPayload()
	s.Equal("outcome", payload["event_kind"])
	s.Equal("regular", payload["rail_kind"])
	s.Equal("completed", payload["stage"])
	s.Equal("failed", payload["status"])
}

// TestEvolutionHostEventMeta_ToPayload空字段 测试空字段不写入 payload。
func (s *ContractsSuite) TestEvolutionHostEventMeta_ToPayload空字段() {
	meta := evolution.EvolutionHostEventMeta{
		EventKind: evolution.EvolutionEventKindApproval,
	}

	payload := meta.ToPayload()
	s.Equal("approval", payload["event_kind"])
	_, hasRailKind := payload["rail_kind"]
	s.False(hasRailKind)
}

// TestEvolutionRequestResult_HasChanges 测试 HasChanges 判断。
func (s *ContractsSuite) TestEvolutionRequestResult_HasChanges() {
	// 无变更
	result := evolution.EvolutionRequestResult{}
	s.False(result.HasChanges())

	// 有 records
	result = evolution.EvolutionRequestResult{
		Records: []checkpointing.EvolutionRecord{},
	}
	s.False(result.HasChanges()) // 空切片不算

	// 有 ApprovalEvent
	event := &stream.OutputSchema{Type: "test"}
	result = evolution.EvolutionRequestResult{ApprovalEvent: event}
	s.True(result.HasChanges())
}

// TestSimplifyRequestResult_HasChanges 测试简化请求的 HasChanges 判断。
func (s *ContractsSuite) TestSimplifyRequestResult_HasChanges() {
	// 无变更
	result := evolution.SimplifyRequestResult{}
	s.False(result.HasChanges())

	// 有 ApprovalEvent
	event := &stream.OutputSchema{Type: "test"}
	result = evolution.SimplifyRequestResult{ApprovalEvent: event}
	s.True(result.HasChanges())
}

// TestEvolutionTriggerPoint枚举值 测试触发点枚举值。
func (s *ContractsSuite) TestEvolutionTriggerPoint枚举值() {
	s.Equal(evolution.EvolutionTriggerPoint("after_invoke"), evolution.TriggerAfterInvoke)
	s.Equal(evolution.EvolutionTriggerPoint("after_model_call"), evolution.TriggerAfterModelCall)
	s.Equal(evolution.EvolutionTriggerPoint("after_tool_call"), evolution.TriggerAfterToolCall)
	s.Equal(evolution.EvolutionTriggerPoint("after_task_iteration"), evolution.TriggerAfterTaskIteration)
	s.Equal(evolution.EvolutionTriggerPoint("none"), evolution.TriggerNone)
}

// TestEvolutionEventKind常量 测试事件类型常量。
func (s *ContractsSuite) TestEvolutionEventKind常量() {
	s.Equal("approval", evolution.EvolutionEventKindApproval)
	s.Equal("progress", evolution.EvolutionEventKindProgress)
	s.Equal("outcome", evolution.EvolutionEventKindOutcome)
}
