package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
)

func TestSQLMemberDao_CreateAndGet(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "", "autonomous", false)

	ok := dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")
	assert.True(t, ok)

	m, err := dao.GetMember(context.Background(), "m1", "t1")
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "M1", m.DisplayName)

	// Python: IntegrityError → False
	ok = dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")
	assert.False(t, ok)

	// Python: GetMember 不存在
	m2, err := dao.GetMember(context.Background(), "nonexist", "t1")
	assert.NoError(t, err)
	assert.Nil(t, m2)
}

func TestSQLMemberDao_GetTeamMembers(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "", "autonomous", false)

	dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")
	dao.CreateMember(context.Background(), "m2", "t1", "M2", "{}", fsm.MemberStatusBusy, "teammate", "", "", "build_mode", "", "")

	members, err := dao.GetTeamMembers(context.Background(), "t1", "")
	require.NoError(t, err)
	assert.Equal(t, 2, len(members))

	// Python: status 过滤
	busyMembers, err := dao.GetTeamMembers(context.Background(), "t1", fsm.MemberStatusBusy)
	require.NoError(t, err)
	assert.Equal(t, 1, len(busyMembers))
}

func TestSQLMemberDao_UpdateMemberStatus(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "", "autonomous", false)
	dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")

	// Python: is_valid_transition(ready, busy) → True
	ok := dao.UpdateMemberStatus(context.Background(), "m1", "t1", fsm.MemberStatusBusy)
	assert.True(t, ok)

	// Python: is_valid_transition(busy, shutdown) → 不合法
	ok = dao.UpdateMemberStatus(context.Background(), "m1", "t1", fsm.MemberStatusShutdown)
	assert.False(t, ok)

	// Python: 成员不存在
	ok = dao.UpdateMemberStatus(context.Background(), "nonexist", "t1", fsm.MemberStatusReady)
	assert.False(t, ok)
}

func TestSQLMemberDao_TryTransitionMemberStatus_CAS(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "", "autonomous", false)
	dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")

	// Python: CAS — where status = from_status, update to_status
	ok := dao.TryTransitionMemberStatus(context.Background(), "m1", "t1", fsm.MemberStatusReady, fsm.MemberStatusBusy)
	assert.True(t, ok)

	// CAS 失败：当前已是 busy，不是 ready
	ok = dao.TryTransitionMemberStatus(context.Background(), "m1", "t1", fsm.MemberStatusReady, fsm.MemberStatusBusy)
	assert.False(t, ok)
}

func TestSQLMemberDao_ListHumanAgentNames(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "", "autonomous", false)

	dao.CreateMember(context.Background(), "ha1", "t1", "HA1", "{}", fsm.MemberStatusReady, "human_agent", "", "", "build_mode", "", "")
	dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")

	names, err := dao.ListHumanAgentNames(context.Background(), "t1")
	require.NoError(t, err)
	assert.Equal(t, []string{"ha1"}, names)
}

func TestSQLMemberDao_GetMembersMaxUpdatedAt(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "", "autonomous", false)

	maxTs := dao.GetMembersMaxUpdatedAt(context.Background(), "t1")
	assert.Equal(t, int64(0), maxTs) // 无成员

	dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")
	maxTs = dao.GetMembersMaxUpdatedAt(context.Background(), "t1")
	assert.True(t, maxTs > 0)
}

func TestSQLMemberDao_UpdateMemberExecutionStatus(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "", "autonomous", false)
	dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", fsm.ExecutionStatusIdle, "build_mode", "", "")

	// Python: is_valid_execution_transition(idle, starting) → True
	ok := dao.UpdateMemberExecutionStatus(context.Background(), "m1", "t1", fsm.ExecutionStatusStarting)
	assert.True(t, ok)

	// Python: is_valid_execution_transition(starting, idle) → 不合法
	ok = dao.UpdateMemberExecutionStatus(context.Background(), "m1", "t1", fsm.ExecutionStatusIdle)
	assert.False(t, ok)

	// Python: 成员不存在
	ok = dao.UpdateMemberExecutionStatus(context.Background(), "nonexist", "t1", fsm.ExecutionStatusStarting)
	assert.False(t, ok)
}

// TestUpdateMemberServedModel_三态 记账：幽灵成员 false、写入+bump updated_at、
// 同模型不变更不 bump。
// Python: member_dao.py:807-843 update_member_served_model
func TestUpdateMemberServedModel_三态(t *testing.T) {
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "team1", "Team1", "leader", "", "", "autonomous", false)
	if !dao.CreateMember(context.Background(), "alice", "team1", "Alice", "{}", "UNSTARTED", "agent", "", "", "build_mode", "", "") {
		t.Fatal("建成员失败")
	}
	// 1. 幽灵成员 → false
	if dao.UpdateMemberServedModel(context.Background(), "ghost", "team1", "m1") {
		t.Fatal("幽灵成员应返回 false")
	}
	// 2. 行存在 → 写入 served_models + bump updated_at
	if !dao.UpdateMemberServedModel(context.Background(), "alice", "team1", "m1") {
		t.Fatal("正常记账应返回 true")
	}
	row, err := dao.GetMember(context.Background(), "alice", "team1")
	if err != nil || row == nil {
		t.Fatalf("读回失败: %v", err)
	}
	opts := LoadMemberOptions(row.Options)
	if len(opts.ServedModels) != 1 || opts.ServedModels[0].Model != "m1" {
		t.Fatalf("served_models: %+v", opts.ServedModels)
	}
	first := row.UpdatedAt
	// 3. 同模型重复记账 → true 但不 bump
	if !dao.UpdateMemberServedModel(context.Background(), "alice", "team1", "m1") {
		t.Fatal("同模型应返回 true")
	}
	row2, _ := dao.GetMember(context.Background(), "alice", "team1")
	if row2.UpdatedAt != first {
		t.Fatal("同模型记账不应 bump updated_at")
	}
	// 4. 换模型 → 第二条记录 + bump
	if !dao.UpdateMemberServedModel(context.Background(), "alice", "team1", "m2") {
		t.Fatal("换模型应返回 true")
	}
	row3, _ := dao.GetMember(context.Background(), "alice", "team1")
	opts3 := LoadMemberOptions(row3.Options)
	if len(opts3.ServedModels) != 2 || opts3.ServedModels[1].Model != "m2" {
		t.Fatalf("换模型后: %+v", opts3.ServedModels)
	}
	if row3.UpdatedAt < first {
		t.Fatal("换模型应 bump updated_at（毫秒粒度下允许同值；名册探针只要求不回退）")
	}
}
