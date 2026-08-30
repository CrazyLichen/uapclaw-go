package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
)

func TestSQLMemberDao_CreateAndGet(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "")

	ok := dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")
	assert.True(t, ok)

	m, err := dao.GetMember(context.Background(), "m1", "t1")
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "M1", m.DisplayName)

	// 对齐 Python: IntegrityError → False
	ok = dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")
	assert.False(t, ok)

	// 对齐 Python: GetMember 不存在
	m2, err := dao.GetMember(context.Background(), "nonexist", "t1")
	assert.NoError(t, err)
	assert.Nil(t, m2)
}

func TestSQLMemberDao_GetTeamMembers(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "")

	dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")
	dao.CreateMember(context.Background(), "m2", "t1", "M2", "{}", fsm.MemberStatusBusy, "teammate", "", "", "build_mode", "", "")

	members, err := dao.GetTeamMembers(context.Background(), "t1", "")
	require.NoError(t, err)
	assert.Equal(t, 2, len(members))

	// 对齐 Python: status 过滤
	busyMembers, err := dao.GetTeamMembers(context.Background(), "t1", fsm.MemberStatusBusy)
	require.NoError(t, err)
	assert.Equal(t, 1, len(busyMembers))
}

func TestSQLMemberDao_UpdateMemberStatus(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "")
	dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")

	// 对齐 Python: is_valid_transition(ready, busy) → True
	ok := dao.UpdateMemberStatus(context.Background(), "m1", "t1", fsm.MemberStatusBusy)
	assert.True(t, ok)

	// 对齐 Python: is_valid_transition(busy, shutdown) → 不合法
	ok = dao.UpdateMemberStatus(context.Background(), "m1", "t1", fsm.MemberStatusShutdown)
	assert.False(t, ok)

	// 对齐 Python: 成员不存在
	ok = dao.UpdateMemberStatus(context.Background(), "nonexist", "t1", fsm.MemberStatusReady)
	assert.False(t, ok)
}

func TestSQLMemberDao_TryTransitionMemberStatus_CAS(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "")
	dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")

	// 对齐 Python: CAS — where status = from_status, update to_status
	ok := dao.TryTransitionMemberStatus(context.Background(), "m1", "t1", fsm.MemberStatusReady, fsm.MemberStatusBusy)
	assert.True(t, ok)

	// CAS 失败：当前已是 busy，不是 ready
	ok = dao.TryTransitionMemberStatus(context.Background(), "m1", "t1", fsm.MemberStatusReady, fsm.MemberStatusBusy)
	assert.False(t, ok)
}

func TestSQLMemberDao_ListHumanAgentNames(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "")

	dao.CreateMember(context.Background(), "ha1", "t1", "HA1", "{}", fsm.MemberStatusReady, "human_agent", "", "", "build_mode", "", "")
	dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")

	names, err := dao.ListHumanAgentNames(context.Background(), "t1")
	require.NoError(t, err)
	assert.Equal(t, []string{"ha1"}, names)
}

func TestSQLMemberDao_GetMembersMaxUpdatedAt(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "")

	maxTs := dao.GetMembersMaxUpdatedAt(context.Background(), "t1")
	assert.Equal(t, int64(0), maxTs) // 无成员

	dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")
	maxTs = dao.GetMembersMaxUpdatedAt(context.Background(), "t1")
	assert.True(t, maxTs > 0)
}

func TestSQLMemberDao_UpdateMemberExecutionStatus(t *testing.T) {
	defer restoreGetSessionID()
	db := newTestSqlDB(t)
	dao := db.Member()
	db.Team().CreateTeam(context.Background(), "t1", "T1", "l1", "", "")
	dao.CreateMember(context.Background(), "m1", "t1", "M1", "{}", fsm.MemberStatusReady, "teammate", "", fsm.ExecutionStatusIdle, "build_mode", "", "")

	// 对齐 Python: is_valid_execution_transition(idle, starting) → True
	ok := dao.UpdateMemberExecutionStatus(context.Background(), "m1", "t1", fsm.ExecutionStatusStarting)
	assert.True(t, ok)

	// 对齐 Python: is_valid_execution_transition(starting, idle) → 不合法
	ok = dao.UpdateMemberExecutionStatus(context.Background(), "m1", "t1", fsm.ExecutionStatusIdle)
	assert.False(t, ok)

	// 对齐 Python: 成员不存在
	ok = dao.UpdateMemberExecutionStatus(context.Background(), "nonexist", "t1", fsm.ExecutionStatusStarting)
	assert.False(t, ok)
}
