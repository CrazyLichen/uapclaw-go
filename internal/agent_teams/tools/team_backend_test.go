package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// newTestAgentCard 创建测试用 AgentCard（对齐 Python: AgentCard(id=..., name=..., description=...)）。
func newTestAgentCard(id, name, desc string) *agentschema.AgentCard {
	return agentschema.NewAgentCard(
		agentschema.WithAgentID(id),
		agentschema.WithAgentName(name),
		agentschema.WithAgentDescription(desc),
	)
}

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

	result := tb.SpawnMember(ctx, "teammate1", "Teammate 1", newTestAgentCard("card1", "Teammate 1", "desc"), string(atschema.TeamRoleTeammate), "desc", "prompt", "")
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

	tb.SpawnMember(ctx, "teammate1", "Teammate 1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	result := tb.SpawnMember(ctx, "teammate1", "Teammate 1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	if result.OK {
		t.Error("SpawnMember() 第二次应返回 fail")
	}
}

// TestSpawnMember_HITT缓存写透 测试 HUMAN_AGENT 角色写入缓存。
func TestSpawnMember_HITT缓存写透(t *testing.T) {
	tb := newTestTeamBackendWithOptions(WithEnableHITT(true))
	ctx := context.Background()

	result := tb.SpawnMember(ctx, "human1", "Human 1", nil, string(atschema.TeamRoleHumanAgent), "", "", "")
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
	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

	// 添加成员
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	tb.SpawnMember(ctx, "teammate2", "T2", nil, string(atschema.TeamRoleTeammate), "", "", "")

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

	outcome, err := tb.BuildTeam(ctx, "Test Team", "description", "Leader", "leader desc", nil, nil)
	_ = outcome
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
	outcome, err := tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", &enableHITT, nil)
	_ = outcome
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

	outcome, err := tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	_ = outcome
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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

	// 有活跃成员（leader 是 BUSY，但跳过 self，所以只有非 self 成员算活跃）→ 应返回 false
	ok, err := tb.CleanTeam(ctx)
	if err != nil {
		t.Fatalf("CleanTeam() error = %v", err)
	}
	// leader 被跳过，没有其他成员，所以应该返回 true
	if !ok {
		t.Error("CleanTeam() = false（leader 被跳过，无其他活跃成员），应返回 true")
	}
}

// TestForceCleanTeam 测试强制清理团队。
func TestForceCleanTeam(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")
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

// TestShutdownMember_已终态 测试关闭已终态成员（幂等返回 success，对齐 Python）。
func TestShutdownMember_已终态(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	tb.db.Member().UpdateMemberStatus(ctx, "teammate1", tb.TeamName(), string(atschema.MemberStatusShutdown))

	result := tb.ShutdownMember(ctx, "teammate1")
	if !result.OK {
		t.Error("ShutdownMember(已终态) should return success (idempotent, aligns with Python)")
	}
}

// TestCancelMember 测试取消成员。
func TestCancelMember(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")
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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	tb.SpawnMember(ctx, "teammate2", "T2", nil, string(atschema.TeamRoleTeammate), "", "", "")

	started, err := tb.Startup(ctx, nil, false)
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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")

	ok, err := tb.StartupMember(ctx, "teammate1", nil)
	if err != nil {
		t.Fatalf("StartupMember() error = %v", err)
	}
	if !ok {
		t.Error("StartupMember() = false, want true")
	}
}

// TestStartupMember_回调失败回滚 测试 onCreated 失败时回滚到 UNSTARTED。
// Python: startup_member 中 _spawn_and_publish 失败 → 回滚 STARTING→UNSTARTED
func TestStartupMember_回调失败回滚(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")

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
// Python: _spawn_and_publish(member_name, on_created)
func TestSpawnAndPublish(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()
	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

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
	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

	tb.taskManager.Add(ctx, "Task 1", "desc")
	tb.taskManager.Add(ctx, "Task 2", "desc")

	count, err := tb.CancelAllTasks(ctx, nil)
	if err != nil {
		t.Errorf("CancelAllTasks() 返回错误: %v", err)
	}
	if count != 2 {
		t.Errorf("CancelAllTasks() = %d, want 2", count)
	}
}

// TestApprovePlan 测试审批计划。
func TestApprovePlan(t *testing.T) {
	planDir := t.TempDir()
	tb := newTestTeamBackendWithOptions(WithPlanStorageDir(planDir))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

	// 创建任务
	task, _ := tb.taskManager.Add(ctx, "Plan Task", "desc")
	// Claim 任务
	_, _ = tb.taskManager.Claim(ctx, task.TaskID)
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

// TestApproveTool 测试审批工具调用（需成员存在）。
func TestApproveTool(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")

	result := tb.ApproveTool(ctx, "teammate1", "tool-call-1", true, "ok", false)
	if !result.OK {
		t.Errorf("ApproveTool() = %v, want OK", result)
	}

	// 成员不存在应返回 fail
	result = tb.ApproveTool(ctx, "nonexistent", "tool-call-2", true, "", false)
	if result.OK {
		t.Error("ApproveTool(nonexistent) should fail")
	}
}

// TestGetTeamInfo 测试获取团队信息。
func TestGetTeamInfo(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "description", "Leader", "leader desc", nil, nil)

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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

	ts := tb.GetTeamUpdatedAt(ctx)
	// 内存数据库时间戳可能为 0
	_ = ts
}

// TestGetMembersMaxUpdatedAt 测试获取成员最大更新时间。
func TestGetMembersMaxUpdatedAt(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

	ts := tb.GetMembersMaxUpdatedAt(ctx)
	_ = ts
}

// TestHumanAgentNames_Concurrent 测试并发读写 HITT 缓存。
func TestHumanAgentNames_Concurrent(t *testing.T) {
	tb := newTestTeamBackendWithOptions(WithEnableHITT(true))
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

	// 创建一个终态任务（对齐 Python: if not tasks: return None — 无任务时无法判定完成）
	tb.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID:   "task-1",
		TeamName: tb.TeamName(),
		Title:    "测试任务",
		Status:   string(atschema.TaskStatusCompleted),
	})

	// 关闭所有成员
	tb.db.Member().UpdateMemberStatus(ctx, "leader", tb.TeamName(), string(atschema.MemberStatusShutdownRequested))
	tb.db.Member().UpdateMemberStatus(ctx, "leader", tb.TeamName(), string(atschema.MemberStatusShutdown))

	snapshot, err := tb.IsTeamCompleted(ctx)
	if err != nil {
		t.Fatalf("IsTeamCompleted() error = %v", err)
	}
	if snapshot == nil {
		t.Error("IsTeamCompleted() = nil（所有任务终态+所有成员已终态），want non-nil")
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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "qwen-max")

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

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)

	// 验证 Leader 的 options 里带 model_ref
	leader, _ := tb.GetMember(ctx, "leader")
	if leader == nil {
		t.Fatal("Leader not found")
	}
	if database.GetMemberModelRef(leader) == nil {
		t.Error("Leader options.model_ref is nil, want non-nil")
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

	outcome, err := tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	_ = outcome
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

	outcome, err := tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	_ = outcome
	if err != nil {
		t.Fatalf("BuildTeam() error = %v", err)
	}

	// human-agent 未创建
	if tb.IsHumanAgent("human1") {
		t.Error("IsHumanAgent('human1') = true（HITT 未启用），want false")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestCancelMember_Busy 成员取消时重置 CLAIMED 任务
func TestCancelMember_Busy(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	tb.db.Member().UpdateMemberStatus(ctx, "teammate1", tb.TeamName(), string(atschema.MemberStatusReady))

	// 创建任务并让 teammate1 认领
	tb.taskManager.Add(ctx, "测试任务", "内容")
	pendingTasks, _ := tb.taskManager.GetClaimableTasks(ctx)
	if len(pendingTasks) > 0 {
		_, _ = tb.taskManager.Claim(ctx, pendingTasks[0].TaskID)
	}

	// 设为 BUSY
	tb.db.Member().UpdateMemberStatus(ctx, "teammate1", tb.TeamName(), string(atschema.MemberStatusBusy))

	result := tb.CancelMember(ctx, "teammate1")
	if !result.OK {
		t.Fatalf("CancelMember(BUSY) = %v, want OK", result)
	}
}

// TestWithForce_WithApproved_WithFeedback 测试 Option 构造函数
func TestWithForce_WithApproved_WithFeedback(t *testing.T) {
	// WithForce 强制模式
	shutdownCfg := &shutdownConfig{}
	WithForce(true)(shutdownCfg)
	if !shutdownCfg.force {
		t.Error("WithForce(true) 应设置 force=true")
	}

	// WithApproved 已批准
	approveCfg := &approvePlanConfig{}
	WithApproved(false)(approveCfg)
	if approveCfg.approved {
		t.Error("WithApproved(false) 应设置 approved=false")
	}

	// WithFeedback 带反馈
	feedbackCfg := &approvePlanConfig{}
	WithFeedback("needs revision")(feedbackCfg)
	if feedbackCfg.feedback != "needs revision" {
		t.Error("WithFeedback 应设置 feedback")
	}
}

// TestCancelTask_有Assignee 测试取消有认领人的任务
func TestCancelTask_有Assignee(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")

	task, _ := tb.taskManager.Add(ctx, "可取消任务", "内容")
	_, _ = tb.taskManager.Claim(ctx, task.TaskID)

	result := tb.CancelTask(ctx, task.TaskID)
	if !result.OK {
		t.Errorf("CancelTask() = %v, want OK", result)
	}
}

// TestApprovePlan_通过Backend 测试通过 TeamBackend 审批计划
func TestApprovePlan_通过Backend(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	// 设为 plan_mode（InMemory 特有方法）
	if imDB, ok := tb.db.(*database.InMemoryTeamDatabase); ok {
		imDB.SetMemberMode("teammate1", tb.TeamName(), "plan_mode")
	} else {
		t.Skip("需要 InMemoryTeamDatabase")
	}

	task, _ := tb.taskManager.Add(ctx, "计划任务", "内容")

	planFile := filepath.Join(t.TempDir(), "plan.md")
	os.WriteFile(planFile, []byte("# 计划"), 0o644)

	// 用 teammate1 的 taskManager 提交计划
	teammateTM := NewTeamTaskManager(tb.db, tb.TeamName(), "teammate1", nil, nil,
		tb.taskManager.plansDir, tb.taskManager.teamPlanID, tb.taskManager.leaderMemberName)
	record, err := teammateTM.SubmitPlan(ctx, task.TaskID, planFile, "call_1")
	if err != nil {
		t.Fatalf("SubmitPlan 返回错误: %v", err)
	}

	// 通过 Backend 审批
	result := tb.ApprovePlan(ctx, record.PlanID, WithApproved(true))
	if !result.OK {
		t.Errorf("ApprovePlan(通过) = %v, want OK", result)
	}
}

// TestCleanTeam_有活跃成员 有活跃成员时无法清理
func TestCleanTeam_有活跃成员(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	tb.db.Member().UpdateMemberStatus(ctx, "teammate1", tb.TeamName(), string(atschema.MemberStatusReady))

	// 成员非 SHUTDOWN → CleanTeam 应返回 false
	cleaned, err := tb.CleanTeam(ctx)
	if err != nil {
		t.Fatalf("CleanTeam 返回错误: %v", err)
	}
	if cleaned {
		t.Error("有活跃成员时 CleanTeam 应返回 false")
	}
}

// TestCleanTeam_所有成员已关闭 所有成员 SHUTDOWN 后可清理
func TestCleanTeam_所有成员已关闭(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	// 将成员设为 SHUTDOWN
	tb.db.Member().UpdateMemberStatus(ctx, "teammate1", tb.TeamName(), string(atschema.MemberStatusShutdown))

	cleaned, err := tb.CleanTeam(ctx)
	if err != nil {
		t.Fatalf("CleanTeam 返回错误: %v", err)
	}
	if !cleaned {
		t.Error("所有成员 SHUTDOWN 后 CleanTeam 应返回 true")
	}
}

// TestForceCleanTeam_不关闭成员 不 shutdown 直接强制删除
func TestForceCleanTeam_不关闭成员(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")

	// shutdownMembers=false → 直接删除
	success, err := tb.ForceCleanTeam(ctx, false)
	if err != nil {
		t.Fatalf("ForceCleanTeam 返回错误: %v", err)
	}
	if !success {
		t.Error("ForceCleanTeam(false) 应成功")
	}
}

// TestForceCleanTeam_关闭成员 shutdownMembers=true
func TestForceCleanTeam_关闭成员(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")

	// 将成员设为 ready 以便可以被 shutdown
	tb.db.Member().UpdateMemberStatus(ctx, "teammate1", tb.TeamName(), string(atschema.MemberStatusReady))

	success, err := tb.ForceCleanTeam(ctx, true)
	if err != nil {
		t.Fatalf("ForceCleanTeam 返回错误: %v", err)
	}
	if !success {
		t.Error("ForceCleanTeam(true) 应成功")
	}
}

// TestApprovePlan_空PlanID 空计划 ID 应返回失败
func TestApprovePlan_空PlanID(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	result := tb.ApprovePlan(ctx, "")
	if result.OK {
		t.Error("空 planID 应返回失败")
	}
}

// TestApprovePlan_不存在的PlanID 不存在的计划应返回失败
func TestApprovePlan_不存在的PlanID(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	result := tb.ApprovePlan(ctx, "nonexistent_plan_id")
	if result.OK {
		t.Error("不存在的 planID 应返回失败")
	}
}

// TestRefreshHumanAgentRoster_HITT未启用 HITT 未启用时不应 panic
func TestRefreshHumanAgentRoster_HITT未启用(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	// 不应 panic
	tb.RefreshHumanAgentRoster(ctx)
}

// TestRegisterCleanupPath_空路径 空路径应跳过
func TestRegisterCleanupPath_空路径(t *testing.T) {
	tb := newTestTeamBackend()
	tb.RegisterCleanupPath("")
	if len(tb.cleanupPaths) != 0 {
		t.Error("空路径不应注册到清理路径")
	}
}

// TestRegisterCleanupPath_有效路径 有效路径应注册
func TestRegisterCleanupPath_有效路径(t *testing.T) {
	tb := newTestTeamBackend()
	tb.RegisterCleanupPath("/tmp/test_cleanup")
	if len(tb.cleanupPaths) != 1 {
		t.Error("有效路径应注册到清理路径")
	}
}

// TestRemoveCleanupPaths_空列表 无清理路径时直接返回
func TestRemoveCleanupPaths_空列表(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()
	err := tb.RemoveCleanupPaths(ctx)
	if err != nil {
		t.Errorf("无清理路径时不应返回错误: %v", err)
	}
}

// TestRemoveCleanupPaths_不存在路径 路径不存在时跳过
func TestRemoveCleanupPaths_不存在路径(t *testing.T) {
	tb := newTestTeamBackend()
	tb.RegisterCleanupPath("/tmp/nonexistent_path_for_test_xxx")
	ctx := context.Background()
	err := tb.RemoveCleanupPaths(ctx)
	if err != nil {
		t.Errorf("不存在路径不应返回错误: %v", err)
	}
}

// TestShutdownMember_成员不存在 不存在的成员应返回失败
func TestShutdownMember_成员不存在(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	result := tb.ShutdownMember(ctx, "nonexistent_member")
	if result.OK {
		t.Error("不存在的成员应返回失败")
	}
}

// TestShutdownMember_已关闭 已 SHUTDOWN 的成员幂等返回成功
func TestShutdownMember_已关闭(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	tb.db.Member().UpdateMemberStatus(ctx, "teammate1", tb.TeamName(), string(atschema.MemberStatusShutdown))

	result := tb.ShutdownMember(ctx, "teammate1")
	if !result.OK {
		t.Errorf("已 SHUTDOWN 的成员应幂等返回成功: %s", result.Reason)
	}
}

// TestCancelMember_成员不存在 不存在的成员应返回失败
func TestCancelMember_成员不存在(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	result := tb.CancelMember(ctx, "nonexistent_member")
	if result.OK {
		t.Error("不存在的成员应返回失败")
	}
}

// TestSpawnMember_已存在成员 重复创建同名成员应返回失败
func TestSpawnMember_已存在成员(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	result := tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	if !result.OK {
		t.Fatalf("首次 SpawnMember 应成功: %s", result.Reason)
	}
	// 再次创建同名成员应返回 false
	result2 := tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	if result2.OK {
		t.Error("重复 SpawnMember 同名成员应返回失败")
	}
}

// TestStartupMember_回调失败 回调失败时应回滚状态
func TestStartupMember_回调失败2(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	tb.SpawnMember(ctx, "teammate1", "T1", nil, string(atschema.TeamRoleTeammate), "", "", "")

	// 回调返回错误
	started, err := tb.StartupMember(ctx, "teammate1", func(ctx context.Context, memberName string) error {
		return errors.New("spawn callback failed")
	})
	if err == nil {
		t.Error("回调失败时应返回错误")
	}
	if started {
		t.Error("回调失败时不应标记为已启动")
	}
}

// TestTeamBackend_GetTeamUpdatedAtState 测试团队 mtime 探针恒 present=true
//
// 偏差 D-1：Go 无 workspace_cache（md 文件 mtime 覆盖层），探针直读 DB 列，
// present 恒 true——行为等价上游「演化关闭」路径。
// Python: get_team_updated_at_state (team.py L1940-1969)
func TestTeamBackend_GetTeamUpdatedAtState(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()
	ts, present := tb.GetTeamUpdatedAtState(ctx)
	if !present {
		t.Fatal("present = false, want true（偏差 D-1）")
	}
	_ = ts
}

// TestTeamBackend_GetMemberUpdatedAtState 测试单成员 mtime 探针
//
// Python: get_member_updated_at_state (team.py L2007-2024)
func TestTeamBackend_GetMemberUpdatedAtState(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()

	// 成员行不存在：返回 (0, true)——没有「必须更新」信号
	ts, present := tb.GetMemberUpdatedAtState(ctx, "ghost", "prompt")
	if !present {
		t.Fatal("present = false, want true")
	}
	if ts != 0 {
		t.Fatalf("ts = %d, want 0（成员行不存在）", ts)
	}

	// 成员行存在：返回其 UpdatedAt
	_ = tb.db.Member().CreateMember(ctx, "alice", "test-team", "Alice", "{}", "ready", "teammate", "", "", "build_mode", "", "")
	ts2, present2 := tb.GetMemberUpdatedAtState(ctx, "alice", "prompt")
	if !present2 {
		t.Fatal("present = false, want true")
	}
	_ = ts2
}

// TestTeamBackend_ForkEnabled 测试 fork 开关恒 false
//
// 偏差 D-8：上游直读 TeamAgentSpec.enable_fork，Go 的 TeamAgentSpec 无该字段。
// Python: fork_enabled (team.py L505-517)
func TestTeamBackend_ForkEnabled(t *testing.T) {
	tb := newTestTeamBackend()
	if tb.ForkEnabled() {
		t.Fatal("ForkEnabled() = true, want false（偏差 D-8）")
	}
}

// TestTeamBackend_Stamp_空实现不panic 测试两个 stamp 空实现
//
// 偏差 D-1/D-4：present 恒 true，stamp 分支永不进入，故为空实现。
// Python: stamp_team_card_updated_at (L1971) / stamp_member_prompt_updated_at (L2025)
func TestTeamBackend_Stamp_空实现不panic(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()
	tb.StampTeamCardUpdatedAt(ctx, 123)
	tb.StampMemberPromptUpdatedAt(ctx, "leader", 123)
}

// TestBuildTeam_首次建队返回Outcome 验证新建路径的返回值。
func TestBuildTeam_首次建队返回Outcome(t *testing.T) {
	tb := newTestTeamBackend()
	outcome, err := tb.BuildTeam(context.Background(), "Test Team", "desc", "Leader", "leader desc", nil, nil)
	if err != nil {
		t.Fatalf("BuildTeam 返回错误: %v", err)
	}
	if outcome == nil {
		t.Fatal("outcome 不应为 nil")
	}
	if outcome.TakenOver {
		t.Fatal("首次建队 TakenOver 应为 false")
	}
}

// TestBuildTeam_二次调用接管 验证幂等接管路径（NEW_TEAM_IN_SESSION）。
//
// 新会话继承一个正在跑的团队时，承载协作政策的 build_team 结果随旧历史
// 一起消失，而团队/名册/任务都还在。因为那是政策的唯一交付点（F_76），
// 在这里失败会让 leader 只剩 bootstrap。
//
// Python: build_team Step 0 / _reattach_team (team.py L2164-2258)
func TestBuildTeam_二次调用接管(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()
	if _, err := tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil); err != nil {
		t.Fatalf("首次 BuildTeam 返回错误: %v", err)
	}
	outcome, err := tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	if err != nil {
		t.Fatalf("接管不应报错: %v", err)
	}
	if outcome == nil || !outcome.TakenOver {
		t.Fatalf("第二次调用 TakenOver 应为 true，实际 %+v", outcome)
	}
	// 接管不写行、不注册成员：leader 仍是唯一成员，且没有第二个成员被注册
	leader, err := tb.GetMember(ctx, "leader")
	if err != nil {
		t.Fatalf("GetMember 返回错误: %v", err)
	}
	if leader == nil {
		t.Fatal("接管后 leader 应仍在名册上")
	}
}

// TestBuildTeam_接管读回验证开关 验证能力参数不适用于接管，从行里读回。
// Python: _reattach_team —— self._enable_task_verification = bool(existing.enable_task_verification)
func TestBuildTeam_接管读回验证开关(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()
	if _, err := tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil); err != nil {
		t.Fatalf("首次 BuildTeam 返回错误: %v", err)
	}
	// 人工把行改成 true，模拟「团队建队时就配了验证闸」
	team, err := tb.DB().Team().GetTeam(ctx, tb.TeamName())
	if err != nil || team == nil {
		t.Fatalf("GetTeam 失败: %v", err)
	}
	team.EnableTaskVerification = true

	outcome, err := tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil, nil)
	if err != nil {
		t.Fatalf("接管返回错误: %v", err)
	}
	if !outcome.EnableTaskVerification {
		t.Fatal("接管应从团队行读回 enable_task_verification=true")
	}
}

// TestMemberMessagingAndSubtasksEnabled 验证两个探针。
// Python: member_messaging_enabled / member_subtasks_enabled (team.py L516-543)
func TestMemberMessagingAndSubtasksEnabled(t *testing.T) {
	tb := newTestTeamBackendWithOptions(
		WithMemberMessaging(true),
		WithMaxSubtaskDepth(2),
	)
	if !tb.MemberMessagingEnabled() {
		t.Fatal("MemberMessagingEnabled 应为 true")
	}
	if !tb.MemberSubtasksEnabled() {
		t.Fatal("MemberSubtasksEnabled 在 MaxSubtaskDepth>0 时应为 true")
	}
	tb0 := newTestTeamBackendWithOptions(WithMaxSubtaskDepth(0))
	if tb0.MemberSubtasksEnabled() {
		t.Fatal("MaxSubtaskDepth=0 时 MemberSubtasksEnabled 应为 false")
	}
}

// ──────────────────────────── AutostartUnstarted 测试 ────────────────────────────

// TestAutostartUnstarted_非Leader返回空 非 leader 不 spawn（它管不了）。
func TestAutostartUnstarted_非Leader返回空(t *testing.T) {
	called := false
	tb := NewTeamBackend("t", "m", false, database.NewInMemoryTeamDatabase(), newTestMessager(),
		WithOnMemberStarted(func(_ context.Context, _ string) error { called = true; return nil }))

	started, err := tb.AutostartUnstarted(context.Background(), false)
	require.NoError(t, err)
	assert.Nil(t, started)
	assert.False(t, called)
}

// TestAutostartUnstarted_回调nil返回空 没注入回调时不动作。
func TestAutostartUnstarted_回调nil返回空(t *testing.T) {
	tb := newTestTeamBackend()

	started, err := tb.AutostartUnstarted(context.Background(), false)
	require.NoError(t, err)
	assert.Nil(t, started)
}

// TestAutostartUnstarted_启动成员 回调注入后启动 UNSTARTED 成员。
func TestAutostartUnstarted_启动成员(t *testing.T) {
	var got []string
	tb := newTestTeamBackendWithOptions(
		WithOnMemberStarted(func(_ context.Context, name string) error {
			got = append(got, name)
			return nil
		}),
	)
	ctx := context.Background()
	tb.BuildTeam(ctx, "T", "d", "L", "ld", nil, nil)
	tb.SpawnMember(ctx, "a1", "A1", nil, string(atschema.TeamRoleTeammate), "", "", "")
	tb.SpawnMember(ctx, "a2", "A2", nil, string(atschema.TeamRoleTeammate), "", "", "")

	started, err := tb.AutostartUnstarted(ctx, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a1", "a2"}, started)
	assert.ElementsMatch(t, []string{"a1", "a2"}, got)
}

// ──────────────────────────── Startup 的 task-scoped 筛选 ────────────────────────────

// setMemberLifetime 直接把 lifetime 写进成员 options（内存实现返回指针，改动即生效）。
func setMemberLifetime(t *testing.T, tb *TeamBackend, memberName string, lifetime database.MemberLifetime) {
	t.Helper()
	m, err := tb.db.Member().GetMember(context.Background(), memberName, tb.teamName)
	require.NoError(t, err)
	require.NotNil(t, m)
	m.Options = database.BuildMemberOptions(database.MemberOptionsInput{Lifetime: &lifetime})
}

// TestStartup_taskScoped无开放任务时跳过 默认不叫醒没有活儿的 task-scoped 成员。
func TestStartup_taskScoped无开放任务时跳过(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()
	tb.BuildTeam(ctx, "T", "d", "L", "ld", nil, nil)
	tb.SpawnMember(ctx, "resident", "R", nil, string(atschema.TeamRoleTeammate), "", "", "")
	tb.SpawnMember(ctx, "scoped", "S", nil, string(atschema.TeamRoleTeammate), "", "", "")
	setMemberLifetime(t, tb, "scoped", database.MemberLifetimeShutdown)

	started, err := tb.Startup(ctx, func(_ context.Context, _ string) error { return nil }, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"resident"}, started, "task-scoped 且无开放任务的成员应跳过")
}

// TestStartup_includeIdleTaskScoped启动全部 广播路径要连空闲的 task-scoped 成员一起叫醒。
func TestStartup_includeIdleTaskScoped启动全部(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()
	tb.BuildTeam(ctx, "T", "d", "L", "ld", nil, nil)
	tb.SpawnMember(ctx, "resident", "R", nil, string(atschema.TeamRoleTeammate), "", "", "")
	tb.SpawnMember(ctx, "scoped", "S", nil, string(atschema.TeamRoleTeammate), "", "", "")
	setMemberLifetime(t, tb, "scoped", database.MemberLifetimeDormant)

	started, err := tb.Startup(ctx, func(_ context.Context, _ string) error { return nil }, true)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"resident", "scoped"}, started)
}

// TestStartup_taskScoped有开放任务时启动 板子上有活儿就必须起来。
func TestStartup_taskScoped有开放任务时启动(t *testing.T) {
	tb := newTestTeamBackend()
	ctx := context.Background()
	tb.BuildTeam(ctx, "T", "d", "L", "ld", nil, nil)
	tb.SpawnMember(ctx, "scoped", "S", nil, string(atschema.TeamRoleTeammate), "", "", "")
	setMemberLifetime(t, tb, "scoped", database.MemberLifetimeShutdown)

	// 给该成员挂一个非终态任务：先建任务再认领（认领即写入 assignee）
	assignee := "scoped"
	task, addErr := tb.taskManager.Add(ctx, "任务", "内容")
	require.NoError(t, addErr)
	ok, claimErr := tb.db.Task().ClaimTask(ctx, task.TaskID, assignee)
	require.NoError(t, claimErr)
	require.True(t, ok)

	started, err := tb.Startup(ctx, func(_ context.Context, _ string) error { return nil }, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"scoped"}, started, "有开放任务的 task-scoped 成员必须启动")
}
