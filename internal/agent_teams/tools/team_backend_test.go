package tools

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// newTestMessager 创建测试用的 Messager。
func newTestMessager() messager.Messager {
	return messager.NewInProcessMessager(atschema.NewMessagerTransportConfig())
}

// newTestTeamBackend 创建测试用的 TeamBackend。
func newTestTeamBackend() *TeamBackend {
	db := database.NewInMemoryTeamDatabase()
	msg := newTestMessager()
	return NewTeamBackend("test-team", "leader", true, db, msg)
}

// newTestTeamBackendWithOptions 创建带选项的测试 TeamBackend。
func newTestTeamBackendWithOptions(opts ...TeamBackendOption) *TeamBackend {
	db := database.NewInMemoryTeamDatabase()
	msg := newTestMessager()
	return NewTeamBackend("test-team", "leader", true, db, msg, opts...)
}

// TestNewTeamBackend 测试构造函数 + Functional Options。
func TestNewTeamBackend(t *testing.T) {
	tb := newTestTeamBackend()
	if tb.TeamName() != "test-team" {
		t.Errorf("TeamName() = %q, want %q", tb.TeamName(), "test-team")
	}
	if tb.MemberName() != "leader" {
		t.Errorf("MemberName() = %q, want %q", tb.MemberName(), "leader")
	}
	if !tb.IsLeader() {
		t.Error("IsLeader() = false, want true")
	}
	if tb.LeaderMemberName() != "leader" {
		t.Errorf("LeaderMemberName() = %q, want %q", tb.LeaderMemberName(), "leader")
	}
	if tb.DB() == nil {
		t.Error("DB() = nil, want non-nil")
	}
	if tb.TaskManager() == nil {
		t.Error("TaskManager() = nil, want non-nil")
	}
	if tb.MessageManager() == nil {
		t.Error("MessageManager() = nil, want non-nil")
	}
}

// TestNewTeamBackend_WithOptions 测试 Functional Options。
func TestNewTeamBackend_WithOptions(t *testing.T) {
	tb := newTestTeamBackendWithOptions(
		WithLeaderMemberName("custom_leader"),
		WithTeammateMode("predefined"),
		WithEnableHITT(true),
	)
	if tb.LeaderMemberName() != "custom_leader" {
		t.Errorf("LeaderMemberName() = %q, want %q", tb.LeaderMemberName(), "custom_leader")
	}
	if !tb.HITTEnabled() {
		t.Error("HITTEnabled() = false, want true")
	}
}

// TestSpawnMember 测试正常创建成员。
func TestSpawnMember(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	result := tb.SpawnMember(ctx, "teammate1", "Teammate 1", "card1", string(atschema.TeamRoleTeammate), "desc", "prompt", "")
	if !result.OK {
		t.Fatalf("SpawnMember() = %v, want OK", result)
	}

	// 验证成员已创建
	member, err := tb.GetMember(ctx, "teammate1")
	if err != nil {
		t.Fatalf("GetMember() error = %v", err)
	}
	if member == nil {
		t.Fatal("GetMember() = nil, want non-nil")
	}
	if member.MemberName != "teammate1" {
		t.Errorf("MemberName = %q, want %q", member.MemberName, "teammate1")
	}
}

// TestSpawnMember_已存在 测试重复创建。
func TestSpawnMember_已存在(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.SpawnMember(ctx, "teammate1", "Teammate 1", "", string(atschema.TeamRoleTeammate), "", "", "")
	result := tb.SpawnMember(ctx, "teammate1", "Teammate 1", "", string(atschema.TeamRoleTeammate), "", "", "")
	if result.OK {
		t.Error("SpawnMember() 第二次应返回 fail")
	}
}

// TestSpawnMember_HITT缓存写透 测试 HUMAN_AGENT 角色写入缓存。
func TestSpawnMember_HITT缓存写透(t *testing.T) {
	tb := newTestTeamBackendWithOptions(WithEnableHITT(true))
	ctx := context.Background()

	result := tb.SpawnMember(ctx, "human1", "Human 1", "", string(atschema.TeamRoleHumanAgent), "", "", "")
	if !result.OK {
		t.Fatalf("SpawnMember() = %v, want OK", result)
	}
	if !tb.IsHumanAgent("human1") {
		t.Error("IsHumanAgent('human1') = false, want true")
	}
}

// TestListMembers 测试列出成员（排除自身）。
func TestListMembers(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	// 先创建团队和 leader
	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	// 添加成员
	tb.SpawnMember(ctx, "teammate1", "T1", "", string(atschema.TeamRoleTeammate), "", "", "")
	tb.SpawnMember(ctx, "teammate2", "T2", "", string(atschema.TeamRoleTeammate), "", "", "")

	members, err := tb.ListMembers(ctx)
	if err != nil {
		t.Fatalf("ListMembers() error = %v", err)
	}
	// 排除自身（leader），应只有 teammate1 和 teammate2
	if len(members) != 2 {
		t.Errorf("ListMembers() count = %d, want 2", len(members))
	}
}

// TestBuildTeam 测试完整构建流程。
func TestBuildTeam(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	err := tb.BuildTeam(ctx, "Test Team", "description", "Leader", "leader desc", nil)
	if err != nil {
		t.Fatalf("BuildTeam() error = %v", err)
	}

	// 验证团队已创建
	team, err := tb.GetTeamInfo(ctx)
	if err != nil {
		t.Fatalf("GetTeamInfo() error = %v", err)
	}
	if team == nil {
		t.Fatal("GetTeamInfo() = nil, want non-nil")
	}

	// 验证 Leader 已注册
	leader, err := tb.GetMember(ctx, "leader")
	if err != nil {
		t.Fatalf("GetMember(leader) error = %v", err)
	}
	if leader == nil {
		t.Fatal("GetMember(leader) = nil, want non-nil")
	}
	if leader.Status != string(atschema.MemberStatusBusy) {
		t.Errorf("Leader status = %q, want %q", leader.Status, atschema.MemberStatusBusy)
	}
}

// TestBuildTeam_HITT天花板 测试 spec 天花板。
func TestBuildTeam_HITT天花板(t *testing.T) {
	// spec_enable_hitt=false, 但 enable_hitt=true → 应报错
	tb := newTestTeamBackend() // 默认 HITT 未启用
	ctx := context.Background()

	enableHITT := true
	err := tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", &enableHITT)
	if err != ErrHITTConfigInvalid {
		t.Errorf("BuildTeam() error = %v, want ErrHITTConfigInvalid", err)
	}
}

// TestBuildTeam_预定义成员 测试预定义成员创建。
func TestBuildTeam_预定义成员(t *testing.T) {
	predefined := []atschema.TeamMemberSpec{
		{MemberName: "teammate1", DisplayName: "T1", RoleType: atschema.TeamRoleTeammate, Persona: "p1"},
		{MemberName: "teammate2", DisplayName: "T2", RoleType: atschema.TeamRoleTeammate, Persona: "p2"},
	}
	tb := newTestTeamBackendWithOptions(WithPredefinedMembers(predefined))
	ctx := context.Background()

	err := tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	if err != nil {
		t.Fatalf("BuildTeam() error = %v", err)
	}

	// 验证预定义成员已创建
	m1, _ := tb.GetMember(ctx, "teammate1")
	if m1 == nil {
		t.Error("teammate1 未创建")
	}
	m2, _ := tb.GetMember(ctx, "teammate2")
	if m2 == nil {
		t.Error("teammate2 未创建")
	}
}

// TestCleanTeam 测试清理团队。
func TestCleanTeam(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	// 有活跃成员 → 应返回 false
	ok, err := tb.CleanTeam(ctx)
	if err != nil {
		t.Fatalf("CleanTeam() error = %v", err)
	}
	if ok {
		t.Error("CleanTeam() = true（有活跃成员），应返回 false")
	}

	// 关闭所有成员（BUSY → SHUTDOWN_REQUESTED → SHUTDOWN）
	tb.db.Member().UpdateMemberStatus(ctx, "leader", tb.TeamName(), string(atschema.MemberStatusShutdownRequested))
	tb.db.Member().UpdateMemberStatus(ctx, "leader", tb.TeamName(), string(atschema.MemberStatusShutdown))

	// 再次清理 → 应成功
	ok, err = tb.CleanTeam(ctx)
	if err != nil {
		t.Fatalf("CleanTeam() error = %v", err)
	}
	if !ok {
		t.Error("CleanTeam() = false（无活跃成员），应返回 true")
	}
}

// TestForceCleanTeam 测试强制清理团队。
func TestForceCleanTeam(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	// 强制清理（含关闭成员）
	ok, err := tb.ForceCleanTeam(ctx, true)
	if err != nil {
		t.Fatalf("ForceCleanTeam() error = %v", err)
	}
	if !ok {
		t.Error("ForceCleanTeam() = false, want true")
	}
}

// TestShutdownMember 测试关闭成员。
func TestShutdownMember(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	tb.SpawnMember(ctx, "teammate1", "T1", "", string(atschema.TeamRoleTeammate), "", "", "")
	// 先启动成员
	tb.db.Member().UpdateMemberStatus(ctx, "teammate1", tb.TeamName(), string(atschema.MemberStatusReady))

	result := tb.ShutdownMember(ctx, "teammate1")
	if !result.OK {
		t.Fatalf("ShutdownMember() = %v, want OK", result)
	}
}

// TestShutdownMember_不存在 测试关闭不存在的成员。
func TestShutdownMember_不存在(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	result := tb.ShutdownMember(ctx, "nonexistent")
	if result.OK {
		t.Error("ShutdownMember(不存在) = OK, want fail")
	}
}

// TestShutdownMember_已终态 测试关闭已终态成员。
func TestShutdownMember_已终态(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	tb.SpawnMember(ctx, "teammate1", "T1", "", string(atschema.TeamRoleTeammate), "", "", "")
	tb.db.Member().UpdateMemberStatus(ctx, "teammate1", tb.TeamName(), string(atschema.MemberStatusShutdown))

	result := tb.ShutdownMember(ctx, "teammate1")
	if result.OK {
		t.Error("ShutdownMember(已终态) = OK, want fail")
	}
}

// TestCancelMember 测试取消成员。
func TestCancelMember(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	tb.SpawnMember(ctx, "teammate1", "T1", "", string(atschema.TeamRoleTeammate), "", "", "")
	tb.db.Member().UpdateMemberStatus(ctx, "teammate1", tb.TeamName(), string(atschema.MemberStatusReady))

	result := tb.CancelMember(ctx, "teammate1")
	if !result.OK {
		t.Fatalf("CancelMember() = %v, want OK", result)
	}
}

// TestStartup 测试启动所有 UNSTARTED 成员。
func TestStartup(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	tb.SpawnMember(ctx, "teammate1", "T1", "", string(atschema.TeamRoleTeammate), "", "", "")
	tb.SpawnMember(ctx, "teammate2", "T2", "", string(atschema.TeamRoleTeammate), "", "", "")

	started, err := tb.Startup(ctx, nil)
	if err != nil {
		t.Fatalf("Startup() error = %v", err)
	}
	if len(started) != 2 {
		t.Errorf("Startup() count = %d, want 2", len(started))
	}
}

// TestStartupMember 测试 CAS 启动单个成员。
func TestStartupMember(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	tb.SpawnMember(ctx, "teammate1", "T1", "", string(atschema.TeamRoleTeammate), "", "", "")

	ok, err := tb.StartupMember(ctx, "teammate1", nil)
	if err != nil {
		t.Fatalf("StartupMember() error = %v", err)
	}
	if !ok {
		t.Error("StartupMember() = false, want true")
	}
}

// TestStartupMember_回调失败回滚 测试 onCreated 失败时回滚到 UNSTARTED。
// 对齐 Python: startup_member 中 _spawn_and_publish 失败 → 回滚 STARTING→UNSTARTED
func TestStartupMember_回调失败回滚(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	tb.SpawnMember(ctx, "teammate1", "T1", "", string(atschema.TeamRoleTeammate), "", "", "")

	errOnCreated := errors.New("spawn failed")
	ok, err := tb.StartupMember(ctx, "teammate1", func(ctx context.Context, memberName string) error {
		return errOnCreated
	})
	if err == nil {
		t.Fatal("StartupMember() 应返回错误")
	}
	if ok {
		t.Error("StartupMember() = true, want false on error")
	}
	// 验证回滚：成员应回到 UNSTARTED
	member, _ := tb.GetMember(ctx, "teammate1")
	if member == nil {
		t.Fatal("回滚后成员不应为 nil")
	}
	if member.Status != string(atschema.MemberStatusUnstarted) {
		t.Errorf("回滚后成员状态 = %s, want unstarted", member.Status)
	}
}

// TestSpawnAndPublish 测试 spawnAndPublish 内部方法。
// 对齐 Python: _spawn_and_publish(member_name, on_created)
func TestSpawnAndPublish(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()
	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	var called bool
	onCreated := func(ctx context.Context, memberName string) error {
		called = true
		return nil
	}

	err := tb.spawnAndPublish(ctx, "leader", onCreated)
	if err != nil {
		t.Fatalf("spawnAndPublish() error = %v", err)
	}
	if !called {
		t.Error("onCreated 未被调用")
	}
}

// TestSpawnAndPublish_nil回调 测试 onCreated 为 nil 时跳过回调。
func TestSpawnAndPublish_nil回调(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()
	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	err := tb.spawnAndPublish(ctx, "leader", nil)
	if err != nil {
		t.Fatalf("spawnAndPublish(nil) error = %v", err)
	}
}

// TestHITTEnabled 测试 HITT 能力开关。
func TestHITTEnabled(t *testing.T) {
	tb := newTestTeamBackend()
	if tb.HITTEnabled() {
		t.Error("HITTEnabled() = true（默认未启用），want false")
	}

	tb2 := newTestTeamBackendWithOptions(WithEnableHITT(true))
	if !tb2.HITTEnabled() {
		t.Error("HITTEnabled() = false（WithEnableHITT(true)），want true")
	}
}

// TestHumanAgentNames 测试 HITT 名册。
func TestHumanAgentNames(t *testing.T) {
	tb := newTestTeamBackendWithOptions(WithEnableHITT(true))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	tb.SpawnHumanAgent(ctx, "human1", "Human 1", "", "")
	tb.SpawnHumanAgent(ctx, "human2", "Human 2", "", "")

	names := tb.HumanAgentNames()
	if len(names) != 2 {
		t.Errorf("HumanAgentNames() count = %d, want 2", len(names))
	}
}

// TestIsHumanAgent 测试 human-agent 判断。
func TestIsHumanAgent(t *testing.T) {
	tb := newTestTeamBackendWithOptions(WithEnableHITT(true))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	tb.SpawnHumanAgent(ctx, "human1", "Human 1", "", "")

	if !tb.IsHumanAgent("human1") {
		t.Error("IsHumanAgent('human1') = false, want true")
	}
	if tb.IsHumanAgent("leader") {
		t.Error("IsHumanAgent('leader') = true, want false")
	}
}

// TestRegisterHumanAgentInbound 测试注册/清除 inbound 回调。
func TestRegisterHumanAgentInbound(t *testing.T) {
	tb := newTestTeamBackendWithOptions(WithEnableHITT(true))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	tb.SpawnHumanAgent(ctx, "human1", "Human 1", "", "")

	// 注册回调
	cb := func(ctx context.Context, memberName string, payload any) error { return nil }
	err := tb.RegisterHumanAgentInbound(ctx, "human1", cb)
	if err != nil {
		t.Fatalf("RegisterHumanAgentInbound() error = %v", err)
	}

	// 获取回调
	got := tb.GetHumanAgentInbound("human1")
	if got == nil {
		t.Error("GetHumanAgentInbound('human1') = nil, want non-nil")
	}

	// 清除回调
	err = tb.RegisterHumanAgentInbound(ctx, "human1", nil)
	if err != nil {
		t.Fatalf("RegisterHumanAgentInbound(nil) error = %v", err)
	}
	got = tb.GetHumanAgentInbound("human1")
	if got != nil {
		t.Error("GetHumanAgentInbound('human1') after clear = non-nil, want nil")
	}
}

// TestRegisterHumanAgentInbound_未知成员 测试注册到未知成员。
func TestRegisterHumanAgentInbound_未知成员(t *testing.T) {
	tb := newTestTeamBackendWithOptions(WithEnableHITT(true))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	cb := func(ctx context.Context, memberName string, payload any) error { return nil }
	err := tb.RegisterHumanAgentInbound(ctx, "unknown", cb)
	if err == nil {
		t.Error("RegisterHumanAgentInbound(未知成员) = nil, want error")
	}
}

// TestRefreshHumanAgentRoster 测试从 DB 重建名册。
func TestRefreshHumanAgentRoster(t *testing.T) {
	tb := newTestTeamBackendWithOptions(WithEnableHITT(true))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	tb.SpawnHumanAgent(ctx, "human1", "Human 1", "", "")

	// 刷新后应仍能识别
	tb.RefreshHumanAgentRoster(ctx)
	if !tb.IsHumanAgent("human1") {
		t.Error("IsHumanAgent('human1') after refresh = false, want true")
	}
}

// TestRegisterCleanupPath 测试注册清理路径。
func TestRegisterCleanupPath(t *testing.T) {
	tb := newTestTeamBackend()
	tb.RegisterCleanupPath("/tmp/test-cleanup")
	tb.RegisterCleanupPath("/tmp/test-cleanup") // 去重
	tb.RegisterCleanupPath("")                  // 空路径忽略

	if len(tb.cleanupPaths) != 1 {
		t.Errorf("cleanupPaths count = %d, want 1", len(tb.cleanupPaths))
	}
}

// TestRemoveCleanupPaths 测试清理路径删除。
func TestRemoveCleanupPaths(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	// 创建临时目录
	tmpDir := t.TempDir()
	tb.RegisterCleanupPath(tmpDir)

	// 清理
	tb.RemoveCleanupPaths(ctx)

	if len(tb.cleanupPaths) != 0 {
		t.Errorf("cleanupPaths count after remove = %d, want 0", len(tb.cleanupPaths))
	}
}

// TestCancelTask 测试取消任务。
func TestCancelTask(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	// 创建任务
	_, err := tb.taskManager.Add(ctx, "Test Task", "desc")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	// 获取任务 ID
	tasks, _ := tb.taskManager.ListTasks(ctx, "")
	if len(tasks) == 0 {
		t.Fatal("No tasks created")
	}
	taskID := tasks[0].TaskID

	result := tb.CancelTask(ctx, taskID)
	if !result.OK {
		t.Errorf("CancelTask() = %v, want OK", result)
	}
}

// TestCancelAllTasks 测试批量取消任务。
func TestCancelAllTasks(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	tb.taskManager.Add(ctx, "Task 1", "desc")
	tb.taskManager.Add(ctx, "Task 2", "desc")

	result := tb.CancelAllTasks(ctx, nil)
	if !result.OK {
		t.Errorf("CancelAllTasks() = %v, want OK", result)
	}
}

// TestApprovePlan 测试审批计划。
func TestApprovePlan(t *testing.T) {
	planDir := t.TempDir()
	tb := newTestTeamBackendWithOptions(WithPlanStorageDir(planDir))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	// 创建任务
	task, _ := tb.taskManager.Add(ctx, "Plan Task", "desc")
	// Claim 任务
	tb.taskManager.Claim(ctx, task.TaskID)
	// 设置成员为 PLAN_MODE
	memDB := tb.DB().(*database.InMemoryTeamDatabase)
	memDB.SetMemberMode("leader", tb.TeamName(), "plan_mode")
	// 创建计划文件
	planFile := planDir + "/plan.md"
	os.WriteFile(planFile, []byte("# Plan"), 0644)
	// 提交计划
	plan, err := tb.taskManager.SubmitPlan(ctx, task.TaskID, planFile, "tool-call-1")
	if err != nil {
		t.Fatalf("SubmitPlan() error = %v", err)
	}

	result := tb.ApprovePlan(ctx, plan.PlanID)
	if !result.OK {
		t.Errorf("ApprovePlan() = %v, want OK", result)
	}
}

// TestApproveTool 测试审批工具调用。
func TestApproveTool(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	result := tb.ApproveTool(ctx, "teammate1", "tool-call-1", true, "ok", false)
	if !result.OK {
		t.Errorf("ApproveTool() = %v, want OK", result)
	}
}

// TestGetTeamInfo 测试获取团队信息。
func TestGetTeamInfo(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "description", "Leader", "leader desc", nil)

	team, err := tb.GetTeamInfo(ctx)
	if err != nil {
		t.Fatalf("GetTeamInfo() error = %v", err)
	}
	if team == nil {
		t.Fatal("GetTeamInfo() = nil, want non-nil")
	}
	if team.TeamName != "test-team" {
		t.Errorf("TeamName = %q, want %q", team.TeamName, "test-team")
	}
}

// TestGetTeamUpdatedAt 测试获取团队更新时间。
func TestGetTeamUpdatedAt(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	ts := tb.GetTeamUpdatedAt(ctx)
	// 内存数据库时间戳可能为 0
	_ = ts
}

// TestGetMembersMaxUpdatedAt 测试获取成员最大更新时间。
func TestGetMembersMaxUpdatedAt(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	ts := tb.GetMembersMaxUpdatedAt(ctx)
	_ = ts
}

// TestHumanAgentNames_Concurrent 测试并发读写 HITT 缓存。
func TestHumanAgentNames_Concurrent(t *testing.T) {
	tb := newTestTeamBackendWithOptions(WithEnableHITT(true))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	var wg sync.WaitGroup
	// 并发写入
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "human_" + string(rune('0'+i))
			tb.SpawnHumanAgent(ctx, name, name, "", "")
		}(i)
	}
	// 并发读取
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tb.HumanAgentNames()
			_ = tb.HITTEnabled()
			_ = tb.IsHumanAgent("human_0")
		}()
	}
	wg.Wait()

	// 验证最终状态
	names := tb.HumanAgentNames()
	if len(names) != 10 {
		t.Errorf("HumanAgentNames() count = %d, want 10", len(names))
	}
}

// TestSpawnHumanAgent_HITT未启用 测试 HITT 未启用时创建 human-agent。
func TestSpawnHumanAgent_HITT未启用(t *testing.T) {
	tb := newTestTeamBackend() // 默认 HITT 未启用
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	result := tb.SpawnHumanAgent(ctx, "human1", "Human 1", "", "")
	if result.OK {
		t.Error("SpawnHumanAgent(HITT未启用) = OK, want fail")
	}
}

// TestOnTeamBuiltCallback 测试 BuildTeam 回调。
func TestOnTeamBuiltCallback(t *testing.T) {
	called := false
	tb := newTestTeamBackendWithOptions(WithOnTeamBuilt(func(ctx context.Context) error {
		called = true
		return nil
	}))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	if !called {
		t.Error("onTeamBuilt callback was not called")
	}
}

// TestOnTeamCleanedCallback 测试 CleanTeam 回调。
func TestOnTeamCleanedCallback(t *testing.T) {
	called := false
	tb := newTestTeamBackendWithOptions(WithOnTeamCleaned(func(ctx context.Context) error {
		called = true
		return nil
	}))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	// 关闭所有成员（BUSY → SHUTDOWN_REQUESTED → SHUTDOWN）
	tb.db.Member().UpdateMemberStatus(ctx, "leader", tb.TeamName(), string(atschema.MemberStatusShutdownRequested))
	tb.db.Member().UpdateMemberStatus(ctx, "leader", tb.TeamName(), string(atschema.MemberStatusShutdown))

	tb.CleanTeam(ctx)
	if !called {
		t.Error("onTeamCleaned callback was not called")
	}
}

// TestIsTeamCompleted_未完成 测试团队未完成。
func TestIsTeamCompleted_未完成(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	// 有活跃任务 → 未完成
	snapshot, err := tb.IsTeamCompleted(ctx)
	if err != nil {
		t.Fatalf("IsTeamCompleted() error = %v", err)
	}
	if snapshot != nil {
		t.Error("IsTeamCompleted() = non-nil（有活跃成员），want nil")
	}
}

// TestIsTeamCompleted_已完成 测试团队完成。
func TestIsTeamCompleted_已完成(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	// 关闭所有成员
	tb.db.Member().UpdateMemberStatus(ctx, "leader", tb.TeamName(), string(atschema.MemberStatusShutdownRequested))
	tb.db.Member().UpdateMemberStatus(ctx, "leader", tb.TeamName(), string(atschema.MemberStatusShutdown))

	snapshot, err := tb.IsTeamCompleted(ctx)
	if err != nil {
		t.Fatalf("IsTeamCompleted() error = %v", err)
	}
	if snapshot == nil {
		t.Error("IsTeamCompleted() = nil（所有成员已终态），want non-nil")
	}
}

// TestWithModelConfigAllocator 测试模型分配回调。
func TestWithModelConfigAllocator(t *testing.T) {
	called := false
	allocator := func(modelName string) *models.Allocation {
		called = true
		return nil
	}
	tb := newTestTeamBackendWithOptions(WithModelConfigAllocator(allocator))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	tb.SpawnMember(ctx, "teammate1", "T1", "", string(atschema.TeamRoleTeammate), "", "", "qwen-max")

	if !called {
		t.Error("modelConfigAllocator was not called")
	}
}

// TestWithLeaderAllocation 测试 Leader 模型分配。
func TestWithLeaderAllocation(t *testing.T) {
	tb := newTestTeamBackendWithOptions(WithLeaderAllocation(&models.Allocation{
		Entry:      models.ModelPoolEntry{ModelName: "qwen-max"},
		GroupIndex: 0,
	}))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)

	// 验证 Leader 有 model_ref_json
	leader, _ := tb.GetMember(ctx, "leader")
	if leader == nil {
		t.Fatal("Leader not found")
	}
	if leader.ModelRefJSON == "" {
		t.Error("Leader ModelRefJSON is empty, want non-empty")
	}
}

// TestWithPlanID 测试计划 ID。
func TestWithPlanID(t *testing.T) {
	tb := newTestTeamBackendWithOptions(WithPlanID("plan-123"))
	if tb.taskManager == nil {
		t.Error("taskManager is nil")
	}
	_ = tb.taskManager // 验证不为 nil
}

// TestBuildTeam_HITT启用 测试 HITT 启用时的构建。
func TestBuildTeam_HITT启用(t *testing.T) {
	predefined := []atschema.TeamMemberSpec{
		{MemberName: "human1", DisplayName: "Human 1", RoleType: atschema.TeamRoleHumanAgent, PromptHint: "You are a human agent"},
	}
	tb := newTestTeamBackendWithOptions(
		WithEnableHITT(true),
		WithPredefinedMembers(predefined),
	)
	ctx := context.Background()

	err := tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	if err != nil {
		t.Fatalf("BuildTeam() error = %v", err)
	}

	// 验证 human-agent 已创建
	if !tb.IsHumanAgent("human1") {
		t.Error("IsHumanAgent('human1') = false, want true")
	}
}

// TestBuildTeam_HITT未启用但有预定义HumanAgent 测试 HITT 未启用时跳过预定义 human-agent。
func TestBuildTeam_HITT未启用但有预定义HumanAgent(t *testing.T) {
	predefined := []atschema.TeamMemberSpec{
		{MemberName: "human1", DisplayName: "Human 1", RoleType: atschema.TeamRoleHumanAgent, PromptHint: "You are a human agent"},
	}
	tb := newTestTeamBackendWithOptions(WithPredefinedMembers(predefined))
	ctx := context.Background()

	err := tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	if err != nil {
		t.Fatalf("BuildTeam() error = %v", err)
	}

	// human-agent 未创建
	if tb.IsHumanAgent("human1") {
		t.Error("IsHumanAgent('human1') = true（HITT 未启用），want false")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
