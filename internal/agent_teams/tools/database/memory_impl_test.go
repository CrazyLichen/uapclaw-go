package database

import (
	"context"
	"testing"
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
