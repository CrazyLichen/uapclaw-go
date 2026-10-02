package team

import (
	"context"
	"sync"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTeamManager 测试创建 TeamManager 实例
func TestNewTeamManager(t *testing.T) {
	mgr := NewTeamManager()
	if mgr == nil {
		t.Fatal("NewTeamManager 应返回非 nil")
	}
	if mgr.activeSessionID != nil {
		t.Error("初始 activeSessionID 应为 nil")
	}
	if mgr.activeTeamName != nil {
		t.Error("初始 activeTeamName 应为 nil")
	}
	if len(mgr.teamAgents) != 0 {
		t.Error("初始 teamAgents 应为空")
	}
	if len(mgr.streamTasks) != 0 {
		t.Error("初始 streamTasks 应为空")
	}
}

// TestGetTeamManager_懒创建 测试懒创建
func TestGetTeamManager_懒创建(t *testing.T) {
	// 清理
	ResetTeamManager("")

	mgr := GetTeamManager("test-channel")
	if mgr == nil {
		t.Fatal("GetTeamManager 应返回非 nil")
	}

	// 同一 channel 应返回同一实例
	mgr2 := GetTeamManager("test-channel")
	if mgr != mgr2 {
		t.Error("同一 channel 应返回同一 TeamManager 实例")
	}

	// 不同 channel 应返回不同实例
	mgr3 := GetTeamManager("other-channel")
	if mgr == mgr3 {
		t.Error("不同 channel 应返回不同 TeamManager 实例")
	}

	// 空 channel 应使用 "default"
	mgrDefault := GetTeamManager("")
	if mgrDefault == nil {
		t.Fatal("空 channel 应返回默认 TeamManager")
	}
	mgrExplicit := GetTeamManager("default")
	if mgrDefault != mgrExplicit {
		t.Error("空 channel 应等同于 'default'")
	}

	// 清理
	ResetTeamManager("")
}

// TestGetTeamManager_并发安全 测试并发获取同一 channel 的安全性
func TestGetTeamManager_并发安全(t *testing.T) {
	ResetTeamManager("")

	var wg sync.WaitGroup
	results := make(chan *TeamManager, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr := GetTeamManager("concurrent-channel")
			results <- mgr
		}()
	}
	wg.Wait()
	close(results)

	var first *TeamManager
	for mgr := range results {
		if first == nil {
			first = mgr
		} else if first != mgr {
			t.Error("并发获取应返回同一 TeamManager 实例")
			break
		}
	}

	ResetTeamManager("")
}

// TestActivePendingState 测试 Active/Pending 状态管理
func TestActivePendingState(t *testing.T) {
	mgr := NewTeamManager()

	// 初始状态
	if mgr.ActiveSessionID() != nil {
		t.Error("初始 ActiveSessionID 应为 nil")
	}

	// 设置 pending
	sid1 := "sess-1"
	tn1 := "team-1"
	mgr.pendingSessionID = &sid1
	mgr.pendingTeamName = &tn1
	if *mgr.PendingSessionID() != "sess-1" {
		t.Error("PendingSessionID 应为 sess-1")
	}

	// CommitRuntimeReady
	mgr.CommitRuntimeReady(sid1, tn1)
	if *mgr.ActiveSessionID() != "sess-1" {
		t.Error("CommitRuntimeReady 后 ActiveSessionID 应为 sess-1")
	}
	if mgr.PendingSessionID() != nil {
		t.Error("CommitRuntimeReady 后 PendingSessionID 应为 nil")
	}

	// ClearActiveRuntime
	mgr.ClearActiveRuntime(sid1)
	if mgr.ActiveSessionID() != nil {
		t.Error("ClearActiveRuntime 后 ActiveSessionID 应为 nil")
	}

	// ClearActiveRuntime 不匹配时不改变
	sid2 := "sess-2"
	mgr.activeSessionID = &sid2
	mgr.ClearActiveRuntime("wrong-session")
	if *mgr.ActiveSessionID() != "sess-2" {
		t.Error("不匹配的 ClearActiveRuntime 不应改变状态")
	}
}

// TestPrepareRuntimeActivation 测试 PrepareRuntimeActivation
func TestPrepareRuntimeActivation(t *testing.T) {
	mgr := NewTeamManager()
	ctx := context.Background()

	sid1 := "sess-old"
	tn1 := "team-old"
	mgr.activeSessionID = &sid1
	mgr.activeTeamName = &tn1

	// PrepareRuntimeActivation 应设置 pending
	err := mgr.PrepareRuntimeActivation(ctx, "sess-new", "team-new")
	if err != nil {
		t.Fatalf("PrepareRuntimeActivation 不应返回错误: %v", err)
	}
	if mgr.PendingSessionID() == nil || *mgr.PendingSessionID() != "sess-new" {
		t.Error("PrepareRuntimeActivation 后 PendingSessionID 应为 sess-new")
	}
}

// TestInteract_非活跃Session拒绝 测试 interact 对非活跃 session 的拒绝
func TestInteract_非活跃Session拒绝(t *testing.T) {
	mgr := NewTeamManager()
	ctx := context.Background()

	// 无活跃 session
	ok, err := mgr.Interact(ctx, "sess-1", "hello")
	if err != nil {
		t.Fatalf("Interact 不应返回错误: %v", err)
	}
	if ok {
		t.Error("无活跃 session 时 Interact 应返回 false")
	}

	// session 不匹配
	sid := "sess-active"
	mgr.activeSessionID = &sid
	tn := "team-active"
	mgr.activeTeamName = &tn
	ok, err = mgr.Interact(ctx, "sess-wrong", "hello")
	if err != nil {
		t.Fatalf("Interact 不应返回错误: %v", err)
	}
	if ok {
		t.Error("session 不匹配时 Interact 应返回 false")
	}
}

// TestStreamTaskRegistration 测试流任务注册/查询
func TestStreamTaskRegistration(t *testing.T) {
	mgr := NewTeamManager()

	if mgr.HasStreamTask("sess-1") {
		t.Error("初始不应有流任务")
	}

	called := false
	cancel := func() { called = true }
	mgr.RegisterStreamTask("sess-1", cancel)

	if !mgr.HasStreamTask("sess-1") {
		t.Error("注册后应有流任务")
	}

	popped := mgr.PopStreamTask("sess-1")
	if popped == nil {
		t.Fatal("PopStreamTask 应返回非 nil")
	}
	popped()
	if !called {
		t.Error("cancel 函数应被调用")
	}
	if mgr.HasStreamTask("sess-1") {
		t.Error("Pop 后不应有流任务")
	}
}

// TestCancelAllStreamTasks 测试取消所有流任务
func TestCancelAllStreamTasks(t *testing.T) {
	mgr := NewTeamManager()
	called1, called2 := false, false
	mgr.RegisterStreamTask("sess-1", func() { called1 = true })
	mgr.RegisterStreamTask("sess-2", func() { called2 = true })

	mgr.CancelAllStreamTasks("测试原因")
	if !called1 || !called2 {
		t.Error("CancelAllStreamTasks 应调用所有 cancel 函数")
	}
	if mgr.HasStreamTask("sess-1") || mgr.HasStreamTask("sess-2") {
		t.Error("CancelAllStreamTasks 后不应有流任务")
	}
}

// TestSkillRegistration 测试技能注册/查询
func TestSkillRegistration(t *testing.T) {
	mgr := NewTeamManager()

	// TeamSkillRail
	mgr.RegisterTeamSkillRail("sess-1", "rail-instance")
	if mgr.GetTeamSkillRail("sess-1") != "rail-instance" {
		t.Error("GetTeamSkillRail 应返回注册的 rail")
	}

	// TeamSkillCreateRail
	mgr.RegisterTeamSkillCreateRail("sess-1", "create-rail")
	if mgr.GetTeamSkillCreateRail("sess-1") != "create-rail" {
		t.Error("GetTeamSkillCreateRail 应返回注册的 rail")
	}

	// SkillSyncTarget
	mgr.RegisterTeamSkillSyncTarget("sess-1", "/source", "/target")
	if !mgr.HasTeamSkillSyncTarget("sess-1") {
		t.Error("HasTeamSkillSyncTarget 应为 true")
	}
	if mgr.HasTeamSkillSyncTarget("sess-2") {
		t.Error("未注册的 session 应返回 false")
	}

	// TeamMemberSkillEvolutionRail
	mgr.RegisterTeamMemberSkillEvolutionRail("sess-1", "member-rail-1")
	mgr.RegisterTeamMemberSkillEvolutionRail("sess-1", "member-rail-2")
	mgr.RegisterTeamMemberSkillEvolutionRail("sess-1", "member-rail-1") // 重复注册
	rails := mgr.teamMemberSkillEvoRails["sess-1"]
	if len(rails) != 2 {
		t.Errorf("应注册 2 个 member rail，实际 %d", len(rails))
	}
}

// TestMonitorRegistration 测试监控注册/查询
func TestMonitorRegistration(t *testing.T) {
	mgr := NewTeamManager()

	if mgr.GetMonitor("sess-1") != nil {
		t.Error("初始不应有监控")
	}

	mgr.RegisterMonitor("sess-1", "monitor-handler")
	if mgr.GetMonitor("sess-1") == nil {
		t.Error("注册后应有监控")
	}
}

// TestEvolutionWatcher 测试演进监控注册/查询
func TestEvolutionWatcher(t *testing.T) {
	mgr := NewTeamManager()

	if mgr.GetTeamEvolutionWatcher("sess-1") != nil {
		t.Error("初始不应有演进监控")
	}

	called := false
	cancel := func() { called = true }
	mgr.RegisterTeamEvolutionWatcher("sess-1", cancel)

	if mgr.GetTeamEvolutionWatcher("sess-1") == nil {
		t.Error("注册后应有演进监控")
	}

	popped := mgr.PopTeamEvolutionWatcher("sess-1")
	if popped == nil {
		t.Fatal("PopTeamEvolutionWatcher 应返回非 nil")
	}
	popped()
	if !called {
		t.Error("cancel 函数应被调用")
	}
}

// TestBuildSessionScopedTeamName 测试 session 作用域 team name 构建
func TestBuildSessionScopedTeamName(t *testing.T) {
	tests := []struct {
		teamName  string
		sessionID string
		expected  string
	}{
		{"my-team", "sess-123", "my-team_sess-123"},
		{"my-team", "", "my-team"},
		{"", "sess-123", "team_sess-123"},
		{"team-abc_sess-123", "sess-123", "team-abc_sess-123"}, // 已有后缀不重复
		{"my-team", "abc!@#def", "my-team_abc___def"},          // 特殊字符替换
	}

	for _, tt := range tests {
		result := buildSessionScopedTeamName(tt.teamName, tt.sessionID)
		if result != tt.expected {
			t.Errorf("buildSessionScopedTeamName(%q, %q) = %q, want %q",
				tt.teamName, tt.sessionID, result, tt.expected)
		}
	}
}

// TestResetTeamManager 测试重置
func TestResetTeamManager(t *testing.T) {
	ResetTeamManager("")
	_ = GetTeamManager("ch-1")
	_ = GetTeamManager("ch-2")

	ResetTeamManager("ch-1")
	mgr1 := GetTeamManager("ch-1")
	_ = mgr1 // 重新创建了
	mgr2 := GetTeamManager("ch-2")
	if mgr2 == nil {
		t.Error("ch-2 应保留")
	}

	ResetTeamManager("")
	// 全部清空后重新创建
	mgrNew := GetTeamManager("ch-1")
	if mgrNew == nil {
		t.Error("清空后应能重新创建")
	}
	ResetTeamManager("")
}

// TestCleanupRuntimeLocals 测试清理本地运行时状态
func TestCleanupRuntimeLocals(t *testing.T) {
	mgr := NewTeamManager()

	// 注册一些状态
	called1 := false
	mgr.RegisterStreamTask("sess-1", func() { called1 = true })
	mgr.RegisterTeamSkillRail("sess-1", "rail")
	mgr.RegisterTeamMemberSkillEvolutionRail("sess-1", "member-rail")
	mgr.teamRailContexts["sess-1"] = &TeamRailMountContext{}

	// 清理
	mgr.cleanupRuntimeLocals("sess-1")

	if mgr.HasStreamTask("sess-1") {
		t.Error("cleanupRuntimeLocals 后不应有流任务")
	}
	if !called1 {
		t.Error("流任务 cancel 应被调用")
	}
	if mgr.GetTeamSkillRail("sess-1") != nil {
		t.Error("cleanupRuntimeLocals 后不应有 skill rail")
	}
	if len(mgr.teamMemberSkillEvoRails) > 0 {
		t.Error("cleanupRuntimeLocals 后不应有 member skill rails")
	}
	if mgr.GetTeamRailContext("sess-1") != nil {
		t.Error("cleanupRuntimeLocals 后不应有 rail context")
	}
}

// TestGlobalFunctions 测试全局函数
func TestGlobalFunctions(t *testing.T) {
	ResetTeamManager("")

	// SyncTeamSkillsAcrossManagers：未注册时应返回 false
	if SyncTeamSkillsAcrossManagers("no-such-session") {
		t.Error("未注册的 session 应返回 false")
	}

	// CancelAllTeamStreamTasksAcrossManagers：不应 panic
	CancelAllTeamStreamTasksAcrossManagers("测试原因")

	// GetAllTeamManagers：初始为空
	managers := GetAllTeamManagers()
	if len(managers) != 0 {
		t.Errorf("初始应有 0 个 TeamManager，实际 %d", len(managers))
	}

	// 添加后应有
	_ = GetTeamManager("ch-1")
	managers = GetAllTeamManagers()
	if len(managers) != 1 {
		t.Errorf("应有 1 个 TeamManager，实际 %d", len(managers))
	}

	ResetTeamManager("")
}
