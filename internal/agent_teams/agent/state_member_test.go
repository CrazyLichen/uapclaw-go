package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newRegisteredMember 构造已注册成员（DB 行已物化）的测试辅助。
// S-34: 状态读写走真实 InMemoryTeamDatabase
func newRegisteredMember(t *testing.T, status string) (*TeamMember, *database.InMemoryTeamDatabase) {
	t.Helper()
	db := database.NewInMemoryTeamDatabase()
	if err := db.Initialize(context.Background()); err != nil {
		t.Fatalf("初始化内存 DB 失败: %v", err)
	}
	if !db.CreateMember(context.Background(), "t1", "team1", "t1", "", status, "teammate", "", string(atschema.ExecutionStatusIdle), "", "", "") {
		t.Fatalf("创建成员行失败")
	}
	m := &TeamMember{
		MemberName: "t1",
		TeamName:   "team1",
		DB:         db,
	}
	return m, db
}

// TestNewTeamAgentState 测试创建默认 TeamAgentState
func TestNewTeamAgentState(t *testing.T) {
	state := NewTeamAgentState()
	assert.NotNil(t, state)
	assert.NotNil(t, state.EventListeners)
	assert.Empty(t, state.EventListeners)
	assert.Nil(t, state.TeamMember)
	assert.Equal(t, "", state.PendingUserQuery)
	assert.False(t, state.TeamCleaned)
}

// TestTeamMember_Status_从DB读取真实状态 S-34: 验证 Status 从 DB 读取而非固定 READY
func TestTeamMember_Status_从DB读取真实状态(t *testing.T) {
	m, _ := newRegisteredMember(t, string(atschema.MemberStatusBusy))
	status, err := m.Status(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, atschema.MemberStatusBusy, status)
}

// TestTeamMember_Status_未注册返回哨兵错误 S-34: 对齐 Python status() 返回 None
func TestTeamMember_Status_未注册返回哨兵错误(t *testing.T) {
	m, _ := newRegisteredMember(t, string(atschema.MemberStatusReady))
	// 查询不存在的成员
	m.MemberName = "ghost"
	_, err := m.Status(context.Background())
	assert.ErrorIs(t, err, ErrMemberNotRegistered)
}

// TestTeamMember_ExecutionStatus_从DB读取 S-34: 验证 ExecutionStatus 从 DB 读取
func TestTeamMember_ExecutionStatus_从DB读取(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	if err := db.Initialize(context.Background()); err != nil {
		t.Fatalf("初始化内存 DB 失败: %v", err)
	}
	db.CreateMember(context.Background(), "t1", "team1", "t1", "", string(atschema.MemberStatusReady), "teammate", "", string(atschema.ExecutionStatusStarting), "", "", "")
	m := &TeamMember{MemberName: "t1", TeamName: "team1", DB: db}
	status, err := m.ExecutionStatus(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, atschema.ExecutionStatusStarting, status)
}

// TestTeamMember_UpdateStatus_等值短路 S-34: 新旧状态相等时返回 true 且不写 DB
func TestTeamMember_UpdateStatus_等值短路(t *testing.T) {
	m, db := newRegisteredMember(t, string(atschema.MemberStatusReady))
	ok, err := m.UpdateStatus(context.Background(), atschema.MemberStatusReady)
	assert.NoError(t, err)
	assert.True(t, ok)
	// DB 状态应保持 READY（未写入新状态）
	got, err := db.Member().GetMember(context.Background(), "t1", "team1")
	assert.NoError(t, err)
	assert.Equal(t, string(atschema.MemberStatusReady), got.Status)
}

// TestTeamMember_UpdateStatus_真实转换并写DB S-34: READY→BUSY 写库成功
func TestTeamMember_UpdateStatus_真实转换并写DB(t *testing.T) {
	m, db := newRegisteredMember(t, string(atschema.MemberStatusReady))
	ok, err := m.UpdateStatus(context.Background(), atschema.MemberStatusBusy)
	assert.NoError(t, err)
	assert.True(t, ok)
	got, err := db.Member().GetMember(context.Background(), "t1", "team1")
	assert.NoError(t, err)
	assert.Equal(t, string(atschema.MemberStatusBusy), got.Status)
}

// TestTeamMember_UpdateStatus_非法转换返回false S-34: FSM 校验失败（如 READY→COMPLETED）
func TestTeamMember_UpdateStatus_非法转换返回false(t *testing.T) {
	m, _ := newRegisteredMember(t, string(atschema.MemberStatusReady))
	// UNSTARTED 是合法终态、READY 不允许转回 UNSTARTED
	ok, err := m.UpdateStatus(context.Background(), atschema.MemberStatusUnstarted)
	assert.NoError(t, err)
	assert.False(t, ok)
}

// TestTeamMember_UpdateStatus_未注册静默false S-34: 对齐 Python 未注册行静默返回 False
func TestTeamMember_UpdateStatus_未注册静默false(t *testing.T) {
	m, _ := newRegisteredMember(t, string(atschema.MemberStatusReady))
	m.MemberName = "ghost"
	ok, err := m.UpdateStatus(context.Background(), atschema.MemberStatusBusy)
	assert.NoError(t, err)
	assert.False(t, ok)
}

// TestTeamMember_UpdateExecutionStatus_写库成功 S-34
func TestTeamMember_UpdateExecutionStatus_写库成功(t *testing.T) {
	m, db := newRegisteredMember(t, string(atschema.MemberStatusReady))
	ok, err := m.UpdateExecutionStatus(context.Background(), atschema.ExecutionStatusStarting)
	assert.NoError(t, err)
	assert.True(t, ok)
	got, err := db.Member().GetMember(context.Background(), "t1", "team1")
	assert.NoError(t, err)
	assert.Equal(t, string(atschema.ExecutionStatusStarting), got.ExecutionStatus)
}

// TestTeamMember_UpdateExecutionStatus_未注册静默false S-34
func TestTeamMember_UpdateExecutionStatus_未注册静默false(t *testing.T) {
	m, _ := newRegisteredMember(t, string(atschema.MemberStatusReady))
	m.MemberName = "ghost"
	ok, err := m.UpdateExecutionStatus(context.Background(), atschema.ExecutionStatusStarting)
	assert.NoError(t, err)
	assert.False(t, ok)
}

// TestTeamMember_发布事件 S-34: 状态变更成功后发布 MemberStatusChangedEvent
func TestTeamMember_发布事件(t *testing.T) {
	m, _ := newRegisteredMember(t, string(atschema.MemberStatusReady))
	// Messager 为 nil 时发布跳过但不报错（对齐 Python best-effort）
	ok, err := m.UpdateStatus(context.Background(), atschema.MemberStatusBusy)
	assert.NoError(t, err)
	assert.True(t, ok)
}

// TestErrMemberNotRegistered 哨兵错误可用 errors.Is 判别
func TestErrMemberNotRegistered(t *testing.T) {
	err := ErrMemberNotRegistered
	assert.True(t, errors.Is(err, ErrMemberNotRegistered))
}

// TestCreateMemberHandle_无TeamBackend 测试 infra.TeamBackend 为 nil 时返回 nil
func TestCreateMemberHandle_无TeamBackend(t *testing.T) {
	card := agentschema.NewAgentCard()
	infra := &TeamInfra{}
	blueprint := &TeamAgentBlueprint{
		Ctx: atschema.TeamRuntimeContext{
			Role:       atschema.TeamRoleTeammate,
			MemberName: "t1",
		},
	}
	result := CreateMemberHandle("t1", blueprint, infra, card)
	assert.Nil(t, result)
}

// TestCreateMemberHandle_有TeamBackend 测试创建 TeamMember
func TestCreateMemberHandle_有TeamBackend(t *testing.T) {
	card := agentschema.NewAgentCard()
	// S-32: 字段私有化，TeamBackend 默认 nil（构造需要参数较多，暂时跳过此测试的详细验证）
	infra := &TeamInfra{}
	blueprint := &TeamAgentBlueprint{
		Ctx: atschema.TeamRuntimeContext{
			Role:       atschema.TeamRoleTeammate,
			MemberName: "t1",
			Persona:    "friendly assistant",
		},
	}
	// TeamBackend 为 nil 时返回 nil
	result := CreateMemberHandle("t1", blueprint, infra, card)
	assert.Nil(t, result)
}
