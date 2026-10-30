package team

import (
	"context"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTeamManager 测试创建 TeamManager 实例
func TestNewTeamManager(t *testing.T) {
	mgr := NewTeamManager()
	if mgr == nil {
		t.Fatal("NewTeamManager 应返回非 nil")
	}
	if mgr.activeSessionID != "" {
		t.Error("初始 activeSessionID 应为空")
	}
	if mgr.activeTeamName != "" {
		t.Error("初始 activeTeamName 应为空")
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
	sid1 := "sess-1"
	tn1 := "team-1"

	// 初始状态
	if mgr.ActiveSessionID() != "" {
		t.Error("初始 ActiveSessionID 应为空")
	}

	// 设置 pending
	mgr.pendingSessionID = sid1
	mgr.pendingTeamName = tn1
	if mgr.PendingSessionID() != "sess-1" {
		t.Error("PendingSessionID 应为 sess-1")
	}

	// CommitRuntimeReady 提交运行时就绪
	mgr.CommitRuntimeReady(sid1, tn1)
	if mgr.ActiveSessionID() != "sess-1" {
		t.Error("CommitRuntimeReady 后 ActiveSessionID 应为 sess-1")
	}
	if mgr.PendingSessionID() != "" {
		t.Error("CommitRuntimeReady 后 PendingSessionID 应为空")
	}

	// ClearActiveRuntime 清除活跃运行时
	mgr.ClearActiveRuntime(sid1)
	if mgr.ActiveSessionID() != "" {
		t.Error("ClearActiveRuntime 后 ActiveSessionID 应为空")
	}

	// ClearActiveRuntime 不匹配时不改变
	sid2 := "sess-2"
	mgr.activeSessionID = sid2
	mgr.ClearActiveRuntime("wrong-session")
	if mgr.ActiveSessionID() != "sess-2" {
		t.Error("不匹配的 ClearActiveRuntime 不应改变状态")
	}
}

// TestPrepareRuntimeActivation 测试 PrepareRuntimeActivation
func TestPrepareRuntimeActivation(t *testing.T) {
	mgr := NewTeamManager()
	ctx := context.Background()

	sid1 := "sess-old"
	tn1 := "team-old"
	mgr.activeSessionID = sid1
	mgr.activeTeamName = tn1

	// PrepareRuntimeActivation 应设置 pending
	err := mgr.PrepareRuntimeActivation(ctx, "sess-new", "team-new")
	if err != nil {
		t.Fatalf("PrepareRuntimeActivation 不应返回错误: %v", err)
	}
	if mgr.PendingSessionID() == "" || mgr.PendingSessionID() != "sess-new" {
		t.Error("PrepareRuntimeActivation 后 PendingSessionID 应为 sess-new")
	}
}

// TestInteract_非活跃Session拒绝 测试 interact 对非活跃 session 的拒绝
// S-07: Interact 只返回 bool（对齐 Python 永不抛异常语义）
func TestInteract_非活跃Session拒绝(t *testing.T) {
	mgr := NewTeamManager()
	ctx := context.Background()

	// 无活跃 session
	if ok := mgr.Interact(ctx, "sess-1", interaction.NewInteractInput("hello")); ok {
		t.Error("无活跃 session 时 Interact 应返回 false")
	}

	// session 不匹配
	sid := "sess-active"
	mgr.activeSessionID = sid
	tn := "team-active"
	mgr.activeTeamName = tn
	if ok := mgr.Interact(ctx, "sess-wrong", interaction.NewInteractInput("hello")); ok {
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

	// TeamSkillRail 团队技能轨道
	skillRail := &evolution.TeamSkillEvolutionRail{}
	mgr.RegisterTeamSkillRail("sess-1", skillRail)
	if mgr.GetTeamSkillRail("sess-1") != skillRail {
		t.Error("GetTeamSkillRail 应返回注册的 rail")
	}

	// TeamSkillCreateRail 团队技能创建轨道
	createRail := &evolution.TeamSkillCreateRail{}
	mgr.RegisterTeamSkillCreateRail("sess-1", createRail)
	if mgr.GetTeamSkillCreateRail("sess-1") != createRail {
		t.Error("GetTeamSkillCreateRail 应返回注册的 rail")
	}

	// SkillSyncTarget 技能同步目标
	mgr.RegisterTeamSkillSyncTarget("sess-1", "/source", "/target")
	if !mgr.HasTeamSkillSyncTarget("sess-1") {
		t.Error("HasTeamSkillSyncTarget 应为 true")
	}
	if mgr.HasTeamSkillSyncTarget("sess-2") {
		t.Error("未注册的 session 应返回 false")
	}

	// TeamMemberSkillEvolutionRail 成员技能演化轨道
	memberRail1 := &evolution.SkillEvolutionRail{}
	memberRail2 := &evolution.SkillEvolutionRail{}
	mgr.RegisterTeamMemberSkillEvolutionRail("sess-1", memberRail1)
	mgr.RegisterTeamMemberSkillEvolutionRail("sess-1", memberRail2)
	mgr.RegisterTeamMemberSkillEvolutionRail("sess-1", memberRail1) // 重复注册
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

	handler := newFakeMonitorHandler()
	mgr.RegisterMonitor("sess-1", handler)
	if mgr.GetMonitor("sess-1") == nil {
		t.Error("注册后应有监控")
	}
}

// fakeMonitorHandler 测试用的 TeamMonitorHandler 实现
type fakeMonitorHandler struct {
	stopped  bool
	eventsCh chan map[string]any
}

func newFakeMonitorHandler() *fakeMonitorHandler {
	return &fakeMonitorHandler{
		eventsCh: make(chan map[string]any, 16),
	}
}

func (f *fakeMonitorHandler) Stop(_ context.Context) error {
	f.stopped = true
	close(f.eventsCh)
	return nil
}

func (f *fakeMonitorHandler) IsRunning() bool {
	return !f.stopped
}

func (f *fakeMonitorHandler) Events() <-chan map[string]any {
	return f.eventsCh
}

func (f *fakeMonitorHandler) GetTeamSnapshot(_ context.Context) (map[string]any, error) {
	return map[string]any{"team_id": "test-team"}, nil
}

func (f *fakeMonitorHandler) TeamID() string {
	return "test-team"
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
	mgr.RegisterTeamSkillRail("sess-1", &evolution.TeamSkillEvolutionRail{})
	mgr.RegisterTeamMemberSkillEvolutionRail("sess-1", &evolution.SkillEvolutionRail{})
	mgr.teamRailContexts["sess-1"] = &TeamRailMountContext{}

	// 清理
	mgr.cleanupRuntimeLocals(context.Background(), "sess-1")

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

// ──────────────────────────── loadTeamSpec model_pool（M-38）测试 ────────────────────────────

// TestLoadTeamSpec_多模型构建modelPool 测试 M-38 model_pool 构建。
//
// models.defaults 两条目（都有 model_name）→ spec.ModelPool 长度 2 +
// strategy="by_model_name"；entry 字段（model_name/api_key/api_base/client_provider/
// metadata.client/metadata.request）正确。
func TestLoadTeamSpec_多模型构建modelPool(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{
						"model_name":      "model-a",
						"api_key":         "key-a",
						"api_base":        "http://a",
						"client_provider": "openai",
						"temperature":     0.7,
						"timeout_ms":      nil, // nil 值应被排除
					},
					"model_config_obj": map[string]any{"max_tokens": 4096},
				},
				map[string]any{
					"model_client_config": map[string]any{
						"model_name": "model-b",
					},
				},
			},
		},
	}

	spec, _ := loadTeamSpec("sess-m38", configBase)
	if spec == nil {
		t.Fatal("loadTeamSpec 应返回非 nil")
	}
	if len(spec.ModelPool) != 2 {
		t.Fatalf("ModelPool 长度 = %d, want 2", len(spec.ModelPool))
	}
	if spec.ModelPoolStrategy != "by_model_name" {
		t.Errorf("ModelPoolStrategy = %q, want by_model_name", spec.ModelPoolStrategy)
	}

	// 第一个 entry 字段断言
	e0 := spec.ModelPool[0]
	if e0.ModelName != "model-a" {
		t.Errorf("entry[0].ModelName = %q, want model-a", e0.ModelName)
	}
	if e0.APIKey != "key-a" || e0.APIBaseURL != "http://a" || e0.APIProvider != "openai" {
		t.Errorf("entry[0] 连接字段错误: %+v", e0)
	}
	if e0.Metadata == nil {
		t.Fatal("entry[0].Metadata 不应为 nil")
	}
	clientMeta, _ := e0.Metadata["client"].(map[string]any)
	if clientMeta == nil {
		t.Fatal("metadata.client 不应为 nil")
	}
	// 身份四键应被排除，temperature 保留，timeout_ms（nil）排除
	if _, exists := clientMeta["model_name"]; exists {
		t.Error("metadata.client 不应包含 model_name")
	}
	if clientMeta["temperature"] != 0.7 {
		t.Errorf("metadata.client[temperature] = %v, want 0.7", clientMeta["temperature"])
	}
	if _, exists := clientMeta["timeout_ms"]; exists {
		t.Error("metadata.client 不应包含 nil 值键 timeout_ms")
	}
	reqMeta, _ := e0.Metadata["request"].(map[string]any)
	if reqMeta == nil || reqMeta["max_tokens"] != float64(4096) {
		t.Errorf("metadata.request = %v, want {max_tokens: 4096}", reqMeta)
	}

	// 第二个 entry（仅 model_name，其余零值）
	e1 := spec.ModelPool[1]
	if e1.ModelName != "model-b" {
		t.Errorf("entry[1].ModelName = %q, want model-b", e1.ModelName)
	}
}

// TestLoadTeamSpec_单模型无pool 测试单条目不构建 pool。
func TestLoadTeamSpec_单模型无pool(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"default": map[string]any{
				"model_client_config": map[string]any{"model_name": "only-one"},
			},
		},
	}

	spec, _ := loadTeamSpec("sess-m38-single", configBase)
	if spec == nil {
		t.Fatal("loadTeamSpec 应返回非 nil")
	}
	if len(spec.ModelPool) != 0 {
		t.Errorf("单模型不应构建 ModelPool，实际 %d 条", len(spec.ModelPool))
	}
}

// TestLoadTeamSpec_缺modelName跳过 测试 mcc 无 model_name 的 entry 被跳过。
func TestLoadTeamSpec_缺modelName跳过(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{
						"model_name": "real-model",
					},
				},
				map[string]any{
					"model_client_config": map[string]any{
						"api_key": "no-name-here",
					},
				},
			},
		},
	}

	spec, _ := loadTeamSpec("sess-m38-skip", configBase)
	if spec == nil {
		t.Fatal("loadTeamSpec 应返回非 nil")
	}
	// 缺 model_name 的 entry 跳过后 pool 只剩 1 条——但单条也写入（len(defaultModels)=2 触发构建，
	// 跳过后 poolEntries=1 仍非空）
	if len(spec.ModelPool) != 1 {
		t.Fatalf("ModelPool 长度 = %d, want 1（缺名跳过）", len(spec.ModelPool))
	}
	if spec.ModelPool[0].ModelName != "real-model" {
		t.Errorf("唯一 entry 应为 real-model，实际 %q", spec.ModelPool[0].ModelName)
	}
}

// TestLoadTeamSpec_显式nil默认spec 测试显式传 nil configBase 时默认 spec（测试可控性保留）。
func TestLoadTeamSpec_显式nil默认spec(t *testing.T) {
	spec, _ := loadTeamSpec("sess-m38-nil", nil)
	if spec == nil {
		t.Fatal("应返回非 nil")
	}
	if spec.TeamName == "" && spec.Leader.MemberName == "" {
		// 默认 spec 的具体字段不强断言，非 nil + 不 panic 即可
		t.Log("默认 spec 返回")
	}
}

// TestRegisterTeamCallbackDefaults_实例继承 验证包级默认回调在创建实例时被继承。
// 对齐 Python: _broadcast_event 为模块级函数、configure_code_team_member_agent
// 在 customizer 内延迟 import——任意 TeamManager 创建后即可用，无需逐请求注入。
func TestRegisterTeamCallbackDefaults_实例继承(t *testing.T) {
	// 恢复原默认值，避免污染其他测试
	origBroadcast, origConfigurer := snapshotTeamCallbackDefaults()
	t.Cleanup(func() {
		RegisterTeamCallbackDefaults(origBroadcast, origConfigurer)
	})

	broadcastCalled := false
	RegisterTeamCallbackDefaults(
		func(channelID, sessionID string, event map[string]any) { broadcastCalled = true },
		func(context.Context, interfaces.DeepAgentInterface, interfaces.DeepAgentInterface,
			*skillruntime.SkillManager, string, string, string, string, string, string, bool) {
		},
	)

	mgr := NewTeamManager()
	if mgr.onEventBroadcast == nil {
		t.Fatal("NewTeamManager 应继承包级 onEventBroadcast 默认值")
	}
	if mgr.codeMemberConfigurer == nil {
		t.Fatal("NewTeamManager 应继承包级 codeMemberConfigurer 默认值")
	}

	// 实例字段在创建后只读——调用即触发注册的回调，无需再次 Set
	mgr.onEventBroadcast("ch", "sess", map[string]any{"event_type": "x"})
	if !broadcastCalled {
		t.Error("实例回调应指向注册的默认实现")
	}
}

// TestNewTeamManager_未注册默认为nil 验证未注册默认回调时字段为 nil（走既有 nil 兜底）。
func TestNewTeamManager_未注册默认为nil(t *testing.T) {
	origBroadcast, origConfigurer := snapshotTeamCallbackDefaults()
	t.Cleanup(func() {
		RegisterTeamCallbackDefaults(origBroadcast, origConfigurer)
	})
	RegisterTeamCallbackDefaults(nil, nil)

	mgr := NewTeamManager()
	if mgr.onEventBroadcast != nil {
		t.Error("未注册默认回调时 onEventBroadcast 应为 nil")
	}
	if mgr.codeMemberConfigurer != nil {
		t.Error("未注册默认回调时 codeMemberConfigurer 应为 nil")
	}
}
