package database

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
)

// ──────────────────────────── TeamDao 测试 ────────────────────────────

// TestCreateTeam_成功 创建新团队返回 true。
func TestCreateTeam_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	result := db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "测试团队", "你是一个领导者")
	if !result {
		t.Error("CreateTeam 应返回 true")
	}

	team, err := db.GetTeam(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetTeam 返回错误: %v", err)
	}
	if team == nil {
		t.Fatal("GetTeam 应返回团队数据")
	}
	if team.TeamName != "alpha" {
		t.Errorf("TeamName: got %q, want %q", team.TeamName, "alpha")
	}
	if team.DisplayName != "Alpha Team" {
		t.Errorf("DisplayName: got %q, want %q", team.DisplayName, "Alpha Team")
	}
	if team.LeaderMemberName != "leader1" {
		t.Errorf("LeaderMemberName: got %q, want %q", team.LeaderMemberName, "leader1")
	}
	if team.Desc != "测试团队" {
		t.Errorf("Desc: got %q, want %q", team.Desc, "测试团队")
	}
	if team.Prompt != "你是一个领导者" {
		t.Errorf("Prompt: got %q, want %q", team.Prompt, "你是一个领导者")
	}
	if team.Created == 0 {
		t.Error("Created 应非零")
	}
	if team.UpdatedAt == 0 {
		t.Error("UpdatedAt 应非零")
	}
}

// TestCreateTeam_已存在 重复创建返回 false（对齐 Python IntegrityError → False）。
func TestCreateTeam_已存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	result := db.CreateTeam(ctx, "alpha", "Other", "other", "", "")
	if result {
		t.Error("重复创建团队应返回 false")
	}
}

// TestGetTeam_存在 查到返回 *Team。
func TestGetTeam_存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	team, err := db.GetTeam(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetTeam 返回错误: %v", err)
	}
	if team == nil {
		t.Fatal("GetTeam 应返回团队数据")
	}
	if team.TeamName != "alpha" {
		t.Errorf("TeamName: got %q, want %q", team.TeamName, "alpha")
	}
}

// TestGetTeam_不存在 查不到返回 nil。
func TestGetTeam_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	team, err := db.GetTeam(ctx, "nonexist")
	if err != nil {
		t.Fatalf("GetTeam 返回错误: %v", err)
	}
	if team != nil {
		t.Error("不存在团队应返回 nil")
	}
}

// TestTeamExists_存在 团队存在时返回 true。
func TestTeamExists_存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	if !db.TeamExists(ctx, "alpha") {
		t.Error("TeamExists 应返回 true")
	}
}

// TestTeamExists_不存在 团队不存在时返回 false。
func TestTeamExists_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	if db.TeamExists(ctx, "nonexist") {
		t.Error("TeamExists 应返回 false")
	}
}

// TestDeleteTeam_成功 删除成功并级联删成员。
func TestDeleteTeam_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent2", "alpha", "Agent2", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	result := db.DeleteTeam(ctx, "alpha")
	if !result {
		t.Error("DeleteTeam 应返回 true")
	}

	// 级联：成员应也被删除
	if db.TeamExists(ctx, "alpha") {
		t.Error("团队应已不存在")
	}
	member, _ := db.GetMember(ctx, "agent1", "alpha")
	if member != nil {
		t.Error("成员 agent1 应已被级联删除")
	}
	member2, _ := db.GetMember(ctx, "agent2", "alpha")
	if member2 != nil {
		t.Error("成员 agent2 应已被级联删除")
	}
}

// TestDeleteTeam_不存在 删除不存在的团队返回 false。
func TestDeleteTeam_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	result := db.DeleteTeam(ctx, "nonexist")
	if result {
		t.Error("删除不存在的团队应返回 false")
	}
}

// TestGetTeamUpdatedAt_存在 返回团队的 updated_at 时间戳。
func TestGetTeamUpdatedAt_存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	ts := db.GetTeamUpdatedAt(ctx, "alpha")
	if ts == 0 {
		t.Error("GetTeamUpdatedAt 应返回非零时间戳")
	}
}

// TestGetTeamUpdatedAt_不存在 返回 0。
func TestGetTeamUpdatedAt_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	ts := db.GetTeamUpdatedAt(ctx, "nonexist")
	if ts != 0 {
		t.Errorf("不存在团队应返回 0, got %d", ts)
	}
}

// ──────────────────────────── MemberDao 测试 ────────────────────────────

// TestCreateMember_成功 所有参数完整创建成员。
func TestCreateMember_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	result := db.CreateMember(ctx, "agent1", "alpha", "Agent One", `{"name":"agent1"}`,
		"ready", "teammate", "智能助手", "idle", "build_mode", "你是一个助手",
		`{"model_id":"qwen-max","model_name":"qwen-max"}`)
	if !result {
		t.Error("CreateMember 应返回 true")
	}

	member, err := db.GetMember(ctx, "agent1", "alpha")
	if err != nil {
		t.Fatalf("GetMember 返回错误: %v", err)
	}
	if member == nil {
		t.Fatal("GetMember 应返回成员数据")
	}
	if member.MemberName != "agent1" {
		t.Errorf("MemberName: got %q, want %q", member.MemberName, "agent1")
	}
	if member.TeamName != "alpha" {
		t.Errorf("TeamName: got %q, want %q", member.TeamName, "alpha")
	}
	if member.DisplayName != "Agent One" {
		t.Errorf("DisplayName: got %q, want %q", member.DisplayName, "Agent One")
	}
	if member.Desc != "智能助手" {
		t.Errorf("Desc: got %q, want %q", member.Desc, "智能助手")
	}
	if member.AgentCard != `{"name":"agent1"}` {
		t.Errorf("AgentCard: got %q, want %q", member.AgentCard, `{"name":"agent1"}`)
	}
	if member.Status != "ready" {
		t.Errorf("Status: got %q, want %q", member.Status, "ready")
	}
	if member.ExecutionStatus != "idle" {
		t.Errorf("ExecutionStatus: got %q, want %q", member.ExecutionStatus, "idle")
	}
	if member.Mode != "build_mode" {
		t.Errorf("Mode: got %q, want %q", member.Mode, "build_mode")
	}
	if member.Role != "teammate" {
		t.Errorf("Role: got %q, want %q", member.Role, "teammate")
	}
	if member.Prompt != "你是一个助手" {
		t.Errorf("Prompt: got %q, want %q", member.Prompt, "你是一个助手")
	}
	if member.ModelRefJSON != `{"model_id":"qwen-max","model_name":"qwen-max"}` {
		t.Errorf("ModelRefJSON: got %q, want %q", member.ModelRefJSON, `{"model_id":"qwen-max","model_name":"qwen-max"}`)
	}
	if member.UpdatedAt == 0 {
		t.Error("UpdatedAt 应非零")
	}
}

// TestCreateMember_已存在 重复创建返回 false。
func TestCreateMember_已存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateMember(ctx, "agent1", "alpha", "Agent One", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	result := db.CreateMember(ctx, "agent1", "alpha", "Other", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	if result {
		t.Error("重复创建成员应返回 false")
	}
}

// TestGetMember_存在 查到返回 *TeamMember。
func TestGetMember_存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "Agent One", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	member, err := db.GetMember(ctx, "agent1", "alpha")
	if err != nil {
		t.Fatalf("GetMember 返回错误: %v", err)
	}
	if member == nil {
		t.Fatal("GetMember 应返回成员数据")
	}
}

// TestGetMember_不存在 查不到返回 nil。
func TestGetMember_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	member, err := db.GetMember(ctx, "nonexist", "alpha")
	if err != nil {
		t.Fatalf("GetMember 返回错误: %v", err)
	}
	if member != nil {
		t.Error("不存在成员应返回 nil")
	}
}

// TestGetTeamMembers_全部 获取全部团队成员（无 status 过滤）。
func TestGetTeamMembers_全部(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateMember(ctx, "agent1", "alpha", "A1", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent2", "alpha", "A2", "{}", "busy", "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent3", "alpha", "A3", "{}", "paused", "teammate", "", "", "build_mode", "", "")

	members, err := db.GetTeamMembers(ctx, "alpha", "")
	if err != nil {
		t.Fatalf("GetTeamMembers 返回错误: %v", err)
	}
	if len(members) != 3 {
		t.Errorf("成员数量: got %d, want 3", len(members))
	}
}

// TestGetTeamMembers_按状态过滤 只返回指定状态的成员。
func TestGetTeamMembers_按状态过滤(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateMember(ctx, "agent1", "alpha", "A1", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent2", "alpha", "A2", "{}", "busy", "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent3", "alpha", "A3", "{}", "paused", "teammate", "", "", "build_mode", "", "")

	members, err := db.GetTeamMembers(ctx, "alpha", "ready")
	if err != nil {
		t.Fatalf("GetTeamMembers 返回错误: %v", err)
	}
	if len(members) != 1 {
		t.Errorf("ready 成员数量: got %d, want 1", len(members))
	}
	if members[0].MemberName != "agent1" {
		t.Errorf("成员名: got %q, want %q", members[0].MemberName, "agent1")
	}
}

// TestUpdateMemberStatus_合法转换 READY→BUSY 返回 true。
func TestUpdateMemberStatus_合法转换(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "A1", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	result := db.UpdateMemberStatus(ctx, "agent1", "alpha", "busy")
	if !result {
		t.Error("合法状态转换应返回 true")
	}

	member, _ := db.GetMember(ctx, "agent1", "alpha")
	if member.Status != "busy" {
		t.Errorf("Status: got %q, want %q", member.Status, "busy")
	}
}

// TestUpdateMemberStatus_非法转换 UNSTARTED→BUSY 返回 false（对齐 Python FSM 校验）。
func TestUpdateMemberStatus_非法转换(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "A1", "{}", "unstarted", "teammate", "", "", "build_mode", "", "")

	result := db.UpdateMemberStatus(ctx, "agent1", "alpha", "busy")
	if result {
		t.Error("非法状态转换 UNSTARTED→BUSY 应返回 false")
	}
}

// TestUpdateMemberStatus_成员不存在 返回 false。
func TestUpdateMemberStatus_成员不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	result := db.UpdateMemberStatus(ctx, "nonexist", "alpha", "busy")
	if result {
		t.Error("不存在成员的状态更新应返回 false")
	}
}

// TestTryTransitionMemberStatus_CAS成功 from_status 匹配时更新。
func TestTryTransitionMemberStatus_CAS成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "A1", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	result := db.TryTransitionMemberStatus(ctx, "agent1", "alpha", "ready", "busy")
	if !result {
		t.Error("CAS 成功应返回 true")
	}

	member, _ := db.GetMember(ctx, "agent1", "alpha")
	if member.Status != "busy" {
		t.Errorf("Status: got %q, want %q", member.Status, "busy")
	}
}

// TestTryTransitionMemberStatus_CAS失败 from_status 不匹配时返回 false。
func TestTryTransitionMemberStatus_CAS失败(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "A1", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	result := db.TryTransitionMemberStatus(ctx, "agent1", "alpha", "busy", "paused")
	if result {
		t.Error("CAS 失败（from_status 不匹配）应返回 false")
	}

	member, _ := db.GetMember(ctx, "agent1", "alpha")
	if member.Status != "ready" {
		t.Errorf("CAS 失败后 Status 不应变化: got %q, want %q", member.Status, "ready")
	}
}

// TestListHumanAgentNames_有human_agent 返回 human_agent 角色的成员名列表。
func TestListHumanAgentNames_有human_agent(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateMember(ctx, "leader1", "alpha", "Leader", "{}", "ready", "leader", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "human1", "alpha", "Human1", "{}", "ready", "human_agent", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "human2", "alpha", "Human2", "{}", "ready", "human_agent", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	names, err := db.ListHumanAgentNames(ctx, "alpha")
	if err != nil {
		t.Fatalf("ListHumanAgentNames 返回错误: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("human_agent 数量: got %d, want 2", len(names))
	}
}

// TestListHumanAgentNames_无human_agent 返回空列表。
func TestListHumanAgentNames_无human_agent(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateMember(ctx, "leader1", "alpha", "Leader", "{}", "ready", "leader", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "Agent1", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	names, err := db.ListHumanAgentNames(ctx, "alpha")
	if err != nil {
		t.Fatalf("ListHumanAgentNames 返回错误: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("无 human_agent 时应返回空列表: got %d", len(names))
	}
}

// TestGetMembersMaxUpdatedAt_有数据 返回最大 updated_at。
func TestGetMembersMaxUpdatedAt_有数据(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateMember(ctx, "agent1", "alpha", "A1", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "agent2", "alpha", "A2", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	maxTs := db.GetMembersMaxUpdatedAt(ctx, "alpha")
	if maxTs == 0 {
		t.Error("有成员时应返回非零 MAX(updated_at)")
	}
}

// TestGetMembersMaxUpdatedAt_无数据 返回 0。
func TestGetMembersMaxUpdatedAt_无数据(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	maxTs := db.GetMembersMaxUpdatedAt(ctx, "alpha")
	if maxTs != 0 {
		t.Errorf("无成员时应返回 0, got %d", maxTs)
	}
}

// TestUpdateMemberExecutionStatus_合法转换 idle→starting 返回 true。
func TestUpdateMemberExecutionStatus_合法转换(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "A1", "{}", "ready", "teammate", "", "idle", "build_mode", "", "")

	result := db.UpdateMemberExecutionStatus(ctx, "agent1", "alpha", "starting")
	if !result {
		t.Error("合法执行状态转换 idle→starting 应返回 true")
	}

	member, _ := db.GetMember(ctx, "agent1", "alpha")
	if member.ExecutionStatus != "starting" {
		t.Errorf("ExecutionStatus: got %q, want %q", member.ExecutionStatus, "starting")
	}
}

// TestUpdateMemberExecutionStatus_非法转换 idle→running 返回 false。
func TestUpdateMemberExecutionStatus_非法转换(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "A1", "{}", "ready", "teammate", "", "idle", "build_mode", "", "")

	result := db.UpdateMemberExecutionStatus(ctx, "agent1", "alpha", "running")
	if result {
		t.Error("非法执行状态转换 idle→running 应返回 false")
	}
}

// ──────────────────────────── TeamDatabase 门面测试 ────────────────────────────

// TestInMemoryTeamDatabase_Initialize 初始化应成功。
func TestInMemoryTeamDatabase_Initialize(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	if err := db.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 返回错误: %v", err)
	}
}

// TestInMemoryTeamDatabase_CreateCurSessionTables_noop InMemory 模式下为 no-op。
func TestInMemoryTeamDatabase_CreateCurSessionTables_noop(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	if err := db.CreateCurSessionTables(ctx); err != nil {
		t.Fatalf("CreateCurSessionTables 应返回 nil: %v", err)
	}
}

// TestInMemoryTeamDatabase_CleanupAllRuntimeState 清空所有数据。
func TestInMemoryTeamDatabase_CleanupAllRuntimeState(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "A1", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	droppedTables, droppedDirs, err := db.CleanupAllRuntimeState(ctx)
	if err != nil {
		t.Fatalf("CleanupAllRuntimeState 返回错误: %v", err)
	}
	if len(droppedTables) != 0 {
		t.Errorf("InMemory 不返回 droppedTables: got %d", len(droppedTables))
	}
	if len(droppedDirs) != 0 {
		t.Errorf("InMemory 不返回 droppedDirs: got %d", len(droppedDirs))
	}

	if db.TeamExists(ctx, "alpha") {
		t.Error("Cleanup 后团队应不存在")
	}
}

// TestInMemoryTeamDatabase_ForceDeleteTeamSession 删除团队及所有成员。
func TestInMemoryTeamDatabase_ForceDeleteTeamSession(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "agent1", "alpha", "A1", "{}", "ready", "teammate", "", "", "build_mode", "", "")

	result := db.ForceDeleteTeamSession(ctx, "alpha")
	if !result {
		t.Error("ForceDeleteTeamSession 应返回 true")
	}

	if db.TeamExists(ctx, "alpha") {
		t.Error("ForceDelete 后团队应不存在")
	}
	member, _ := db.GetMember(ctx, "agent1", "alpha")
	if member != nil {
		t.Error("ForceDelete 后成员应不存在")
	}
}

// TestInMemoryTeamDatabase_ForceDeleteTeamSession_不存在 删除不存在的团队。
func TestInMemoryTeamDatabase_ForceDeleteTeamSession_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	result := db.ForceDeleteTeamSession(ctx, "nonexist")
	if result {
		t.Error("删除不存在的团队应返回 false")
	}
}

// TestInMemoryTeamDatabase_Close 关闭后数据清空。
func TestInMemoryTeamDatabase_Close(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	if err := db.Close(); err != nil {
		t.Fatalf("Close 返回错误: %v", err)
	}
}

// TestInMemoryTeamDatabase_Team_自引用 Team() 返回 db 自身。
func TestInMemoryTeamDatabase_Team_自引用(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	dao := db.Team()
	if dao != db {
		t.Error("Team() 应返回 db 自身（对齐 Python self.team = self）")
	}
}

// TestInMemoryTeamDatabase_Member_自引用 Member() 返回 db 自身。
func TestInMemoryTeamDatabase_Member_自引用(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	dao := db.Member()
	if dao != db {
		t.Error("Member() 应返回 db 自身（对齐 Python self.member = self）")
	}
}

// ──────────────────────────── TaskDao 测试 ────────────────────────────

func TestCreateTask_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	task := &TeamTaskBase{TaskID: "task1", TeamName: "alpha", Title: "任务1", Content: "内容1", Status: fsm.TaskStatusPending}
	ok, err := db.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("CreateTask 返回错误: %v", err)
	}
	if !ok {
		t.Error("CreateTask 应返回 true")
	}

	got, _ := db.GetTask(ctx, "task1")
	if got == nil {
		t.Fatal("GetTask 应返回任务数据")
	}
	if got.TaskID != "task1" {
		t.Errorf("TaskID: got %q, want %q", got.TaskID, "task1")
	}
	if got.UpdatedAt == 0 {
		t.Error("UpdatedAt 应非零")
	}
}

func TestCreateTask_已存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	task := &TeamTaskBase{TaskID: "task1", TeamName: "alpha", Title: "任务1", Status: fsm.TaskStatusPending}
	db.CreateTask(ctx, task)
	ok, _ := db.CreateTask(ctx, &TeamTaskBase{TaskID: "task1", TeamName: "alpha", Title: "其他", Status: fsm.TaskStatusPending})
	if ok {
		t.Error("重复创建任务应返回 false")
	}
}

func TestGetTask_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	got, err := db.GetTask(ctx, "nonexist")
	if err != nil {
		t.Fatalf("GetTask 返回错误: %v", err)
	}
	if got != nil {
		t.Error("不存在任务应返回 nil")
	}
}

func TestGetTeamTasks_按状态过滤(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "P1"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusClaimed, Title: "C1"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t3", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "P2"})

	pending, _ := db.GetTeamTasks(ctx, "alpha", fsm.TaskStatusPending)
	if len(pending) != 2 {
		t.Errorf("pending 任务数量: got %d, want 2", len(pending))
	}

	all, _ := db.GetTeamTasks(ctx, "alpha", "")
	if len(all) != 3 {
		t.Errorf("全部任务数量: got %d, want 3", len(all))
	}
}

func TestGetTasksByAssignee(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Assignee: "agent1", Status: fsm.TaskStatusClaimed, Title: "A1"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Assignee: "agent2", Status: fsm.TaskStatusClaimed, Title: "A2"})

	result, _ := db.GetTasksByAssignee(ctx, "alpha", "agent1", "")
	if len(result) != 1 {
		t.Errorf("agent1 的任务数量: got %d, want 1", len(result))
	}
}

func TestDeleteTask_级联删依赖(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "上游"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "下游"})
	db.MutateDependencyGraph(ctx, "alpha", nil, []EdgeSpec{{TaskID: "t2", DependsOnID: "t1"}})

	db.DeleteTask(ctx, "t1")
	deps, _ := db.GetTaskDependencies(ctx, "t2")
	if len(deps) != 0 {
		t.Errorf("上游删除后，下游依赖应为0: got %d", len(deps))
	}
}

func TestClaimTask_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "任务"})

	ok, _ := db.ClaimTask(ctx, "t1", "agent1")
	if !ok {
		t.Error("ClaimTask PENDING→CLAIMED 应返回 true")
	}
	task, _ := db.GetTask(ctx, "t1")
	if task.Assignee != "agent1" {
		t.Errorf("Assignee: got %q, want %q", task.Assignee, "agent1")
	}
}

func TestClaimTask_非法转换(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusCompleted, Title: "已完成"})

	ok, _ := db.ClaimTask(ctx, "t1", "agent1")
	if ok {
		t.Error("ClaimTask COMPLETED→CLAIMED 应返回 false")
	}
}

func TestResetTask_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusClaimed, Assignee: "agent1", Title: "任务"})

	ok, _ := db.ResetTask(ctx, "t1")
	if !ok {
		t.Error("ResetTask CLAIMED→PENDING 应返回 true")
	}
	task, _ := db.GetTask(ctx, "t1")
	if task.Assignee != "" {
		t.Errorf("ResetTask 后 Assignee 应为空: got %q", task.Assignee)
	}
}

func TestApprovePlanTask_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusClaimed, Title: "任务"})

	ok, _ := db.ApprovePlanTask(ctx, "t1")
	if !ok {
		t.Error("ApprovePlanTask CLAIMED→PLAN_APPROVED 应返回 true")
	}
	task, _ := db.GetTask(ctx, "t1")
	if task.Status != fsm.TaskStatusPlanApproved {
		t.Errorf("Status: got %q, want %q", task.Status, fsm.TaskStatusPlanApproved)
	}
}

func TestMutateDependencyGraph_简单成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "上游"})
	newTasks := []NewTaskSpec{{TaskID: "t2", Title: "下游", InitialStatus: fsm.TaskStatusPending}}
	edges := []EdgeSpec{{TaskID: "t2", DependsOnID: "t1"}}

	result := db.MutateDependencyGraph(ctx, "alpha", newTasks, edges)
	if !result.Ok {
		t.Errorf("管线应成功: reason=%s", result.Reason)
	}

	task2, _ := db.GetTask(ctx, "t2")
	if task2 == nil {
		t.Fatal("t2 应已创建")
	}
	if task2.Status != fsm.TaskStatusBlocked {
		t.Errorf("t2 有未解决依赖应变为 blocked: got %q", task2.Status)
	}
}

func TestMutateDependencyGraph_环路检测(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "A"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "B"})

	edges := []EdgeSpec{
		{TaskID: "t1", DependsOnID: "t2"},
		{TaskID: "t2", DependsOnID: "t1"},
	}
	result := db.MutateDependencyGraph(ctx, "alpha", nil, edges)
	if result.Ok {
		t.Error("环路应导致管线失败")
	}
}

func TestMutateDependencyGraph_taskID冲突(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "已有"})

	newTasks := []NewTaskSpec{{TaskID: "t1", Title: "冲突"}}
	result := db.MutateDependencyGraph(ctx, "alpha", newTasks, nil)
	if result.Ok {
		t.Error("task_id 冲突应导致管线失败")
	}
}

func TestCompleteTask_终止传播(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "upstream", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "上游"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "downstream", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "下游"})

	db.MutateDependencyGraph(ctx, "alpha", nil, []EdgeSpec{{TaskID: "downstream", DependsOnID: "upstream"}})

	down, _ := db.GetTask(ctx, "downstream")
	if down.Status != fsm.TaskStatusBlocked {
		t.Fatalf("下游应被阻塞: got %q", down.Status)
	}

	// 先认领上游，然后完成（对齐 Python FSM：pending→claimed→completed）
	db.ClaimTask(ctx, "upstream", "leader1")
	refreshed, _ := db.CompleteTask(ctx, "upstream")
	if len(refreshed) == 0 {
		t.Error("完成上游应刷新下游任务")
	}

	down, _ = db.GetTask(ctx, "downstream")
	if down.Status != fsm.TaskStatusPending {
		t.Errorf("上游完成后下游应解除阻塞: got %q", down.Status)
	}
}

func TestCancelTask_终止传播(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "upstream", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "上游"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "downstream", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "下游"})

	db.MutateDependencyGraph(ctx, "alpha", nil, []EdgeSpec{{TaskID: "downstream", DependsOnID: "upstream"}})

	refreshed, _ := db.CancelTask(ctx, "upstream")
	if len(refreshed) == 0 {
		t.Error("取消上游应刷新下游任务")
	}

	down, _ := db.GetTask(ctx, "downstream")
	if down.Status != fsm.TaskStatusPending {
		t.Errorf("取消上游后下游应解除阻塞: got %q", down.Status)
	}
}

func TestCancelAllTasks(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "任务1"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusClaimed, Assignee: "agent1", Title: "任务2"})

	result, _ := db.CancelAllTasks(ctx, "alpha", nil)
	if len(result.Cancelled) != 2 {
		t.Errorf("应取消2个任务: got %d", len(result.Cancelled))
	}
}

func TestCancelAllTasks_skipAssignees(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "任务1"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusClaimed, Assignee: "agent1", Title: "任务2"})

	result, _ := db.CancelAllTasks(ctx, "alpha", []string{"agent1"})
	if len(result.Cancelled) != 1 {
		t.Errorf("skipAssignees 后应只取消1个任务: got %d", len(result.Cancelled))
	}
}

func TestVerifyAndFixTaskConsistency(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusClaimed, Assignee: "agent1", Title: "A"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "B"})

	// 通过管线添加依赖，t2→t1
	db.MutateDependencyGraph(ctx, "alpha", nil, []EdgeSpec{{TaskID: "t2", DependsOnID: "t1"}})
	// t2 应变为 blocked
	t2, _ := db.GetTask(ctx, "t2")
	if t2.Status != fsm.TaskStatusBlocked {
		t.Fatalf("t2 应被阻塞: got %q", t2.Status)
	}

	// 完成上游 t1 → 下游依赖被解决
	db.ClaimTask(ctx, "t1", "leader1")
	db.CompleteTask(ctx, "t1")

	// 此时 t2 应从 blocked→pending（已被 terminateTaskInSession 刷新）
	t2, _ = db.GetTask(ctx, "t2")
	if t2.Status != fsm.TaskStatusPending {
		t.Errorf("上游完成后 t2 应解除阻塞: got %q", t2.Status)
	}
}

func TestUpdateTask_禁止编辑CLAIMED(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusClaimed, Title: "已认领"})

	ok, _ := db.UpdateTask(ctx, "t1", "新标题", "新内容")
	if ok {
		t.Error("CLAIMED 状态下应禁止编辑")
	}
}

func TestUpdateTask_允许编辑PENDING(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "待定"})

	ok, _ := db.UpdateTask(ctx, "t1", "新标题", "新内容")
	if !ok {
		t.Error("PENDING 状态下应允许编辑")
	}
	task, _ := db.GetTask(ctx, "t1")
	if task.Title != "新标题" {
		t.Errorf("Title: got %q, want %q", task.Title, "新标题")
	}
}

func TestGetUnresolvedDependenciesCount(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "上游"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "下游"})
	db.MutateDependencyGraph(ctx, "alpha", nil, []EdgeSpec{{TaskID: "t2", DependsOnID: "t1"}})

	count, _ := db.GetUnresolvedDependenciesCount(ctx, "t2")
	if count != 1 {
		t.Errorf("t2 未解决依赖应为1: got %d", count)
	}

	// 完成上游后 t2 的依赖变为已解决
	db.ClaimTask(ctx, "t1", "leader1")
	db.CompleteTask(ctx, "t1")
	count, _ = db.GetUnresolvedDependenciesCount(ctx, "t2")
	if count != 0 {
		t.Errorf("上游完成后 t2 未解决依赖应为0: got %d", count)
	}
}

func TestGetTasksDependingOn(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "上游"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "下游"})
	db.MutateDependencyGraph(ctx, "alpha", nil, []EdgeSpec{{TaskID: "t2", DependsOnID: "t1"}})

	result, _ := db.GetTasksDependingOn(ctx, "t1")
	if len(result) != 1 {
		t.Errorf("t1 阻塞的任务应为1: got %d", len(result))
	}
	if result[0].TaskID != "t2" {
		t.Errorf("阻塞任务ID: got %q, want %q", result[0].TaskID, "t2")
	}
}

func TestUpdateTaskStatus_终态传播(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "upstream", TeamName: "alpha", Status: fsm.TaskStatusClaimed, Assignee: "agent1", Title: "上游"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "downstream", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "下游"})
	db.MutateDependencyGraph(ctx, "alpha", nil, []EdgeSpec{{TaskID: "downstream", DependsOnID: "upstream"}})

	// 通过 UpdateTaskStatus 完成上游（对齐 Python: update_task_status → complete 传播）
	refreshed, _ := db.UpdateTaskStatus(ctx, "upstream", fsm.TaskStatusCompleted)
	if len(refreshed) == 0 {
		t.Error("UpdateTaskStatus 终态应触发传播")
	}

	down, _ := db.GetTask(ctx, "downstream")
	if down.Status != fsm.TaskStatusPending {
		t.Errorf("上游完成后下游应解除阻塞: got %q", down.Status)
	}
}

func TestAddTaskWithBidirectionalDependencies(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "已有任务"})

	newTask := &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Title: "新任务", Status: fsm.TaskStatusPending}
	result := db.AddTaskWithBidirectionalDependencies(ctx, "alpha", newTask, []string{"t1"})
	if !result.Ok {
		t.Errorf("应成功: reason=%s", result.Reason)
	}

	t2, _ := db.GetTask(ctx, "t2")
	if t2.Status != fsm.TaskStatusBlocked {
		t.Errorf("t2 依赖 t1 应被阻塞: got %q", t2.Status)
	}
}

func TestTask_自引用(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	dao := db.Task()
	if dao != db {
		t.Error("Task() 应返回 db 自身（对齐 Python self.task = self）")
	}
}

func TestApprovePlanTask_非法转换(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "未认领"})

	ok, _ := db.ApprovePlanTask(ctx, "t1")
	if ok {
		t.Error("PENDING→PLAN_APPROVED 应返回 false（需先 CLAIMED）")
	}
}

func TestUpdateTask_禁止编辑PLAN_APPROVED(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPlanApproved, Title: "已审批"})

	ok, _ := db.UpdateTask(ctx, "t1", "新标题", "新内容")
	if ok {
		t.Error("PLAN_APPROVED 状态下应禁止编辑")
	}
}

func TestResetTask_非法转换(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusCompleted, Title: "已完成"})

	ok, _ := db.ResetTask(ctx, "t1")
	if ok {
		t.Error("COMPLETED→PENDING 应返回 false")
	}
}

func TestMutateDependencyGraph_端点缺失(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	// 只有 t1，但 edge 引用不存在的 t3
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "A"})
	edges := []EdgeSpec{{TaskID: "t1", DependsOnID: "t3"}}
	result := db.MutateDependencyGraph(ctx, "alpha", nil, edges)
	if result.Ok {
		t.Error("端点缺失应导致管线失败")
	}
}

func TestMutateDependencyGraph_终态目标(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")

	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusCompleted, Title: "已完成"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "下游"})
	// 对齐 Python: 下游 t2 依赖已完成 t1 是允许的（终态依赖自动 resolved=True）
	edges := []EdgeSpec{{TaskID: "t2", DependsOnID: "t1"}}
	result := db.MutateDependencyGraph(ctx, "alpha", nil, edges)
	if !result.Ok {
		t.Errorf("下游 PENDING 任务依赖终态上游应成功，got: %s", result.Reason)
	}
	// 验证依赖行 resolved=True
	deps, _ := db.GetTaskDependencies(ctx, "t2")
	if len(deps) != 1 || !deps[0].Resolved {
		t.Error("终态依赖应自动标记为 resolved=True")
	}
}

func TestMutateDependencyGraph_CLAIMED源拒绝(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "m1", "alpha", "M1", "", "active", "teammate", "", "idle", "build_mode", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusClaimed, Assignee: "m1", Title: "已认领"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "上游"})
	// 尝试让已认领任务 t1 依赖 t2（给 CLAIMED 源任务加依赖）
	edges := []EdgeSpec{{TaskID: "t1", DependsOnID: "t2"}}
	result := db.MutateDependencyGraph(ctx, "alpha", nil, edges)
	if result.Ok {
		t.Error("CLAIMED 源任务添加依赖应被拒绝")
	}
}

func TestMutateDependencyGraph_PLAN_APPROVED源拒绝(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha Team", "leader1", "", "")
	db.CreateMember(ctx, "m1", "alpha", "M1", "", "active", "teammate", "", "idle", "build_mode", "", "")
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPlanApproved, Assignee: "m1", Title: "计划已批准"})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPending, Title: "上游"})
	edges := []EdgeSpec{{TaskID: "t1", DependsOnID: "t2"}}
	result := db.MutateDependencyGraph(ctx, "alpha", nil, edges)
	if result.Ok {
		t.Error("PLAN_APPROVED 源任务添加依赖应被拒绝")
	}
}

// ──────────────────────────── MessageDao 测试 ────────────────────────────

// newTestMessageDB 创建测试用消息数据库
func newTestMessageDB() *InMemoryTeamDatabase {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	_ = db.Initialize(ctx)
	_ = db.CreateTeam(ctx, "team1", "Team1", "leader", "", "")
	_ = db.CreateMember(ctx, "alice", "team1", "Alice", "", "active", "teammate", "", "idle", "build_mode", "", "")
	_ = db.CreateMember(ctx, "bob", "team1", "Bob", "", "active", "teammate", "", "idle", "build_mode", "", "")
	_ = db.CreateMember(ctx, "leader", "team1", "Leader", "", "active", "leader", "", "idle", "build_mode", "", "")
	return db
}

// TestCreateMessage_直发 测试创建直发消息
func TestCreateMessage_直发(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	msg := &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "hello", Broadcast: false, IsRead: BoolPtr(false),
	}
	if !db.CreateMessage(ctx, msg) {
		t.Error("CreateMessage 应返回 true")
	}
	if msg.Timestamp == 0 {
		t.Error("Timestamp 应被设置")
	}
}

// TestCreateMessage_广播 测试创建广播消息
func TestCreateMessage_广播(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	msg := &TeamMessageBase{
		MessageID: "msg_2", TeamName: "team1",
		FromMemberName: "alice", Content: "announcement",
		Broadcast: true, IsRead: nil,
	}
	if !db.CreateMessage(ctx, msg) {
		t.Error("CreateMessage 应返回 true")
	}
}

// TestCreateMessage_冲突 测试 messageID 冲突
func TestCreateMessage_冲突(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	msg := &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "hello", Broadcast: false, IsRead: BoolPtr(false),
	}
	db.CreateMessage(ctx, msg)
	if db.CreateMessage(ctx, msg) {
		t.Error("重复 messageID 应返回 false")
	}
}

// TestGetMessage 按 ID 查找消息
func TestGetMessage(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "hello", Broadcast: false, IsRead: BoolPtr(false),
	})
	msg, err := db.GetMessage(ctx, "msg_1")
	if err != nil {
		t.Fatalf("GetMessage 失败: %v", err)
	}
	if msg == nil {
		t.Fatal("GetMessage 应返回消息")
	}
	if msg.Content != "hello" {
		t.Errorf("Content = %q, want hello", msg.Content)
	}
}

// TestGetMessage_不存在 测试查找不存在的消息
func TestGetMessage_不存在(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	msg, err := db.GetMessage(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetMessage 失败: %v", err)
	}
	if msg != nil {
		t.Error("不存在的消息应返回 nil")
	}
}

// TestGetMessages_直发 测试获取直发消息
func TestGetMessages_直发(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "hello", Broadcast: false, IsRead: BoolPtr(false),
	})
	msgs, err := db.GetMessages(ctx, "team1", "bob", false, "")
	if err != nil {
		t.Fatalf("GetMessages 失败: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("GetMessages 返回 %d 条, want 1", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("Content = %q, want hello", msgs[0].Content)
	}
}

// TestGetMessages_未读 测试获取未读消息
func TestGetMessages_未读(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "unread", Broadcast: false, IsRead: BoolPtr(false),
	})
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_2", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "read", Broadcast: false, IsRead: BoolPtr(true),
	})
	msgs, _ := db.GetMessages(ctx, "team1", "bob", true, "")
	if len(msgs) != 1 {
		t.Errorf("未读消息数 = %d, want 1", len(msgs))
	}
	if msgs[0].Content != "unread" {
		t.Errorf("Content = %q, want unread", msgs[0].Content)
	}
}

// TestGetMessages_按发送者 测试按发送者过滤
func TestGetMessages_按发送者(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "from alice", Broadcast: false, IsRead: BoolPtr(false),
	})
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_2", TeamName: "team1",
		FromMemberName: "leader", ToMemberName: "bob",
		Content: "from leader", Broadcast: false, IsRead: BoolPtr(false),
	})
	msgs, _ := db.GetMessages(ctx, "team1", "bob", false, "alice")
	if len(msgs) != 1 {
		t.Errorf("alice 的消息数 = %d, want 1", len(msgs))
	}
}

// TestGetBroadcastMessages 测试获取广播消息
func TestGetBroadcastMessages(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", Content: "announcement",
		Broadcast: true, IsRead: nil,
	})
	msgs, err := db.GetBroadcastMessages(ctx, "team1", "bob", false, "")
	if err != nil {
		t.Fatalf("GetBroadcastMessages 失败: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("GetBroadcastMessages 返回 %d 条, want 1", len(msgs))
	}
	// alice 不应收到自己的广播
	msgs2, _ := db.GetBroadcastMessages(ctx, "team1", "alice", false, "")
	if len(msgs2) != 0 {
		t.Errorf("alice 不应收到自己的广播, 返回 %d 条", len(msgs2))
	}
}

// TestGetBroadcastMessages_未读 测试 watermark 过滤
func TestGetBroadcastMessages_未读(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	// 创建广播消息
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", Content: "old broadcast",
		Broadcast: true, IsRead: nil,
	})
	// 标记已读
	db.MarkMessageRead(ctx, "msg_1", "bob")

	// 手动设置一条更新的广播消息（timestamp 更大）
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_2", TeamName: "team1",
		FromMemberName: "alice", Content: "new broadcast",
		Broadcast: true, IsRead: nil,
	})
	// 确保 msg_2 的 timestamp 大于 watermark
	msg, _ := db.GetMessage(ctx, "msg_2")
	rs := db.readStatus[memberKey("bob", "team1")]
	if rs != nil && rs.ReadAt != nil && msg.Timestamp <= *rs.ReadAt {
		msg.Timestamp = *rs.ReadAt + 1000
	}
	msgs, _ := db.GetBroadcastMessages(ctx, "team1", "bob", true, "")
	if len(msgs) != 1 {
		t.Errorf("未读广播数 = %d, want 1", len(msgs))
	}
	if len(msgs) > 0 && msgs[0].Content != "new broadcast" {
		t.Errorf("Content = %q, want new broadcast", msgs[0].Content)
	}
}

// TestGetTeamMessages 测试获取团队所有消息
func TestGetTeamMessages(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "direct", Broadcast: false, IsRead: BoolPtr(false),
	})
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_2", TeamName: "team1",
		FromMemberName: "alice", Content: "broadcast",
		Broadcast: true, IsRead: nil,
	})
	msgs, err := db.GetTeamMessages(ctx, "team1", "")
	if err != nil {
		t.Fatalf("GetTeamMessages 失败: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("GetTeamMessages 返回 %d 条, want 2", len(msgs))
	}
}

// TestGetTeamMessages_按类型 测试按 broadcast 过滤
func TestGetTeamMessages_按类型(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "direct", Broadcast: false, IsRead: BoolPtr(false),
	})
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_2", TeamName: "team1",
		FromMemberName: "alice", Content: "broadcast",
		Broadcast: true, IsRead: nil,
	})
	msgs, _ := db.GetTeamMessages(ctx, "team1", "true")
	if len(msgs) != 1 {
		t.Errorf("广播消息数 = %d, want 1", len(msgs))
	}
	if msgs[0].MessageID != "msg_2" {
		t.Errorf("MessageID = %q, want msg_2", msgs[0].MessageID)
	}
}

// TestHasUnreadMessages_直发未读 测试直发未读消息
func TestHasUnreadMessages_直发未读(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "hello", Broadcast: false, IsRead: BoolPtr(false),
	})
	if !db.HasUnreadMessages(ctx, "team1", true) {
		t.Error("有未读直发消息时 HasUnreadMessages 应返回 true")
	}
}

// TestHasUnreadMessages_全部已读 测试全部已读
func TestHasUnreadMessages_全部已读(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "hello", Broadcast: false, IsRead: BoolPtr(true),
	})
	if db.HasUnreadMessages(ctx, "team1", true) {
		t.Error("全部已读时 HasUnreadMessages 应返回 false")
	}
}

// TestHasUnreadMessages_不含广播 测试不含广播
func TestHasUnreadMessages_不含广播(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", Content: "broadcast",
		Broadcast: true, IsRead: nil,
	})
	if db.HasUnreadMessages(ctx, "team1", false) {
		t.Error("不含广播时 HasUnreadMessages 应返回 false（无直发未读）")
	}
}

// TestMarkMessageRead_直发 测试标记直发已读
func TestMarkMessageRead_直发(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "hello", Broadcast: false, IsRead: BoolPtr(false),
	})
	if !db.MarkMessageRead(ctx, "msg_1", "bob") {
		t.Error("MarkMessageRead 应返回 true")
	}
	msg, _ := db.GetMessage(ctx, "msg_1")
	if msg.IsRead == nil || !*msg.IsRead {
		t.Error("标记已读后 IsRead 应为 true")
	}
}

// TestMarkMessageRead_广播 测试标记广播已读
func TestMarkMessageRead_广播(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", Content: "announcement",
		Broadcast: true, IsRead: nil,
	})
	if !db.MarkMessageRead(ctx, "msg_1", "bob") {
		t.Error("MarkMessageRead 应返回 true")
	}
	// 验证 watermark 已设置
	rs := db.readStatus[memberKey("bob", "team1")]
	if rs == nil || rs.ReadAt == nil {
		t.Error("标记已读后应设置 read_status watermark")
	}
}

// TestMarkMessageRead_消息不存在 测试标记不存在的消息
func TestMarkMessageRead_消息不存在(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	if db.MarkMessageRead(ctx, "nonexistent", "bob") {
		t.Error("不存在的消息应返回 false")
	}
}

// TestMarkMessageRead_成员不存在 测试标记不存在的成员
func TestMarkMessageRead_成员不存在(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", ToMemberName: "bob",
		Content: "hello", Broadcast: false, IsRead: BoolPtr(false),
	})
	if db.MarkMessageRead(ctx, "msg_1", "nonexistent") {
		t.Error("不存在的成员应返回 false")
	}
}

// TestForceDeleteTeamSession_消息级联 测试删除 team 时消息级联删除
func TestForceDeleteTeamSession_消息级联(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", Content: "test",
		Broadcast: true, IsRead: nil,
	})
	db.ForceDeleteTeamSession(ctx, "team1")
	msgs, _ := db.GetTeamMessages(ctx, "team1", "")
	if len(msgs) != 0 {
		t.Errorf("删除 team 后消息数 = %d, want 0", len(msgs))
	}
}

// TestCleanupAllRuntimeState_消息 测试清理所有消息
func TestCleanupAllRuntimeState_消息(t *testing.T) {
	db := newTestMessageDB()
	ctx := context.Background()
	db.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "msg_1", TeamName: "team1",
		FromMemberName: "alice", Content: "test",
		Broadcast: true, IsRead: nil,
	})
	db.CleanupAllRuntimeState(ctx)
	msgs, _ := db.GetTeamMessages(ctx, "team1", "")
	if len(msgs) != 0 {
		t.Errorf("清理后消息数 = %d, want 0", len(msgs))
	}
}
