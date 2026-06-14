package controller

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestGlobalController 创建测试用的 GlobalSessionController（不使用全局单例）
func newTestGlobalController(tmpDir string) *GlobalSessionController {
	return &GlobalSessionController{
		BasePath:    tmpDir,
		Controllers: make(map[string]*SessionController),
	}
}

// ──────────────────────────── 基础功能测试 ────────────────────────────

func TestGlobalSessionController_SetConfig(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	g.SetConfig(GlobalSessionConfig{BasePath: tmpDir + "/new"})
	if g.BasePath != tmpDir+"/new" {
		t.Errorf("BasePath = %q, want %q", g.BasePath, tmpDir+"/new")
	}
}

func TestGlobalSessionController_CreateIfNotExistsAgent_首次创建(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	isNew, controller, err := g.CreateIfNotExistsAgent("a1")
	if err != nil {
		t.Fatalf("CreateIfNotExistsAgent() 返回错误: %v", err)
	}
	if !isNew {
		t.Error("首次创建应返回 is_new=true")
	}
	if controller == nil {
		t.Fatal("controller 不应为 nil")
	}
}

func TestGlobalSessionController_CreateIfNotExistsAgent_已存在(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, _, err := g.CreateIfNotExistsAgent("a1")
	require.NoError(t, err)
	isNew, _, _ := g.CreateIfNotExistsAgent("a1")
	if isNew {
		t.Error("已存在时应返回 is_new=false")
	}
}

func TestGlobalSessionController_CreateIfNotExistsAgent_ensureBasePath失败(t *testing.T) {
	// 使用一个只读路径，使 MkdirAll 失败
	g := newTestGlobalController("/proc/1/readonly-impossible-path")
	g.BasePath = "/proc/1/readonly-impossible-path"
	_, _, err := g.CreateIfNotExistsAgent("a1")
	require.Error(t, err, "ensureBasePath 失败应返回错误")
}

func TestGlobalSessionController_GetAgent(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, _, err := g.CreateIfNotExistsAgent("a1")
	require.NoError(t, err)
	controller := g.GetAgent("a1")
	if controller == nil {
		t.Error("已注册的 Agent 应返回非 nil")
	}
}

func TestGlobalSessionController_GetAgent_未注册(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	controller := g.GetAgent("nonexistent")
	if controller != nil {
		t.Error("未注册的 Agent 应返回 nil")
	}
}

func TestGlobalSessionController_FlushAgent(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")

	if err := g.FlushAgent("a1"); err != nil {
		t.Fatalf("FlushAgent() 返回错误: %v", err)
	}
}

func TestGlobalSessionController_FlushAgent_未找到Agent(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	// 未注册的 Agent 应返回 nil（跳过刷盘）
	err := g.FlushAgent("nonexistent")
	assert.NoError(t, err, "未找到的 Agent 刷盘应返回 nil")
}

func TestGlobalSessionController_FlushSession(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")

	err := g.FlushSession("s1")
	assert.NoError(t, err, "FlushSession 应成功")
}

func TestGlobalSessionController_FlushSession_未找到(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	g.CreateIfNotExistsAgent("a1")

	err := g.FlushSession("nonexistent")
	assert.NoError(t, err, "未找到的 session 应返回 nil")
}

func TestGlobalSessionController_FlushAll(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	g.CreateIfNotExistsAgent("a1")

	if err := g.FlushAll(); err != nil {
		t.Fatalf("FlushAll() 返回错误: %v", err)
	}
}

func TestGlobalSessionController_FlushScope(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")

	err := g.FlushScope(scope)
	assert.NoError(t, err, "FlushScope 应成功")
}

func TestGlobalSessionController_LoadScope(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")
	require.NoError(t, g.FlushAgent("a1"))

	// 新控制器加载作用域
	g2 := newTestGlobalController(tmpDir)
	g2.CreateIfNotExistsAgent("a1")
	err := g2.LoadScope(scope, true)
	assert.NoError(t, err, "LoadScope 应成功")
}

func TestGlobalSessionController_LoadAll(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")
	require.NoError(t, g.FlushAgent("a1"))

	// 新控制器加载全部
	g2 := newTestGlobalController(tmpDir)
	err := g2.LoadAll(true)
	assert.NoError(t, err, "LoadAll 应成功")
}

func TestGlobalSessionController_LoadAll_目录不存在(t *testing.T) {
	g := newTestGlobalController("/nonexistent/path/12345")
	err := g.LoadAll(true)
	assert.NoError(t, err, "目录不存在时 LoadAll 应返回 nil")
}

func TestGlobalSessionController_RemoveAgent(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	g.CreateIfNotExistsAgent("a1")

	removed, err := g.RemoveAgent("a1")
	if err != nil {
		t.Fatalf("RemoveAgent() 返回错误: %v", err)
	}
	if !removed {
		t.Error("应返回 true")
	}
	if g.GetAgent("a1") != nil {
		t.Error("删除后应返回 nil")
	}
}

func TestGlobalSessionController_RemoveAgent_未找到(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	removed, err := g.RemoveAgent("nonexistent")
	assert.NoError(t, err, "未找到的 Agent 不应返回错误")
	assert.False(t, removed, "未找到的 Agent 应返回 false")
}

func TestGlobalSessionController_RemoveAll(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	g.CreateIfNotExistsAgent("a1")

	g.RemoveAll()
	if len(g.Controllers) != 0 {
		t.Error("RemoveAll 后 Controllers 应为空")
	}
}

func TestGlobalSessionController_LoadAgent(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")
	g.FlushAgent("a1")

	// 新控制器加载
	g2 := newTestGlobalController(tmpDir)
	if err := g2.LoadAgent("a1", true); err != nil {
		t.Fatalf("LoadAgent() 返回错误: %v", err)
	}
}

// ──────────────────────────── 清理测试 ────────────────────────────

func TestGlobalSessionController_CleanupOrphanFiles_dryRun(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")
	g.FlushAgent("a1")

	// 创建一个孤立目录
	orphanDir := SessionPaths{}.SessionDir(tmpDir, "a1", "orphan-session")
	os.MkdirAll(orphanDir, 0o755)
	os.WriteFile(SessionPaths{}.StateFile(orphanDir), []byte("{}"), 0o644)

	result := g.CleanupOrphanFiles("a1", true)
	if len(result["a1"]) != 1 {
		t.Fatalf("dryRun 应检测到 1 个孤立目录，得到 %d", len(result["a1"]))
	}
	if result["a1"][0] != "orphan-session" {
		t.Errorf("孤立目录名 = %q, want %q", result["a1"][0], "orphan-session")
	}

	// 确认未删除
	if _, err := os.Stat(orphanDir); os.IsNotExist(err) {
		t.Error("dryRun 模式不应删除目录")
	}
}

func TestGlobalSessionController_CleanupOrphanFiles_删除(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")
	g.FlushAgent("a1")

	// 创建一个孤立目录
	orphanDir := SessionPaths{}.SessionDir(tmpDir, "a1", "orphan-session")
	os.MkdirAll(orphanDir, 0o755)
	os.WriteFile(SessionPaths{}.StateFile(orphanDir), []byte("{}"), 0o644)

	result := g.CleanupOrphanFiles("a1", false)
	if len(result["a1"]) != 1 {
		t.Fatalf("应删除 1 个孤立目录，得到 %d", len(result["a1"]))
	}

	// 确认已删除
	if _, err := os.Stat(orphanDir); !os.IsNotExist(err) {
		t.Error("非 dryRun 模式应删除目录")
	}
}

func TestGlobalSessionController_CleanupOrphanFiles_空AgentID扫描全部(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")
	g.FlushAgent("a1")

	// 创建孤立目录
	orphanDir := SessionPaths{}.SessionDir(tmpDir, "a1", "orphan-session")
	os.MkdirAll(orphanDir, 0o755)
	os.WriteFile(SessionPaths{}.StateFile(orphanDir), []byte("{}"), 0o644)

	// 空 agentID 应扫描所有 Agent
	result := g.CleanupOrphanFiles("", true)
	assert.Contains(t, result, "a1", "应检测到 a1 的孤立目录")
}

func TestGlobalSessionController_CleanupOrphanFiles_未注册Agent的磁盘目录(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)

	// 在磁盘上创建一个未在 Controllers 中注册的 Agent 目录
	agentDir := SessionPaths{}.AgentDir(tmpDir, "disk-only-agent")
	sessionsDir := SessionPaths{}.SessionsDir(tmpDir, "disk-only-agent")
	orphanDir := SessionPaths{}.SessionDir(tmpDir, "disk-only-agent", "orphan1")
	os.MkdirAll(orphanDir, 0o755)
	os.WriteFile(SessionPaths{}.StateFile(orphanDir), []byte("{}"), 0o644)
	// 写一个空的 sessions.json
	os.MkdirAll(sessionsDir, 0o755)
	// 需要在 AgentDir 下创建 sessions.json 的上级目录
	metaFile := SessionPaths{}.MetaFile(tmpDir, "disk-only-agent")
	os.MkdirAll(filepath.Dir(metaFile), 0o755)
	_ = agentDir

	result := g.CleanupOrphanFiles("disk-only-agent", true)
	assert.Contains(t, result, "disk-only-agent", "应检测到磁盘上未注册 Agent 的孤立目录")
}

func TestGlobalSessionController_CleanupOrphanFiles_downstreams目录跳过(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")
	g.FlushAgent("a1")

	// 创建 downstreams 目录（应被跳过）
	dsDir := filepath.Join(SessionPaths{}.SessionsDir(tmpDir, "a1"), "downstreams")
	os.MkdirAll(dsDir, 0o755)

	result := g.CleanupOrphanFiles("a1", true)
	// downstreams 目录应被跳过，不算作孤立目录
	_, hasA1 := result["a1"]
	if hasA1 && len(result["a1"]) > 0 {
		// 只有非 downstreams 目录才算孤立目录
		for _, name := range result["a1"] {
			assert.NotEqual(t, "downstreams", name, "downstreams 目录不应被标记为孤立")
		}
	}
}

func TestGlobalSessionController_CleanupOrphanFiles_无stateFile跳过(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")
	g.FlushAgent("a1")

	// 创建一个没有 state.data 的子目录（不应被识别为孤立会话）
	emptyDir := SessionPaths{}.SessionDir(tmpDir, "a1", "empty-dir")
	os.MkdirAll(emptyDir, 0o755)
	// 不写 state.data

	result := g.CleanupOrphanFiles("a1", true)
	_, hasA1 := result["a1"]
	if hasA1 {
		for _, name := range result["a1"] {
			assert.NotEqual(t, "empty-dir", name, "无 state.data 的目录不应被识别为孤立")
		}
	}
}

func TestGlobalSessionController_CleanupAgentInactiveSessions(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")

	// 手动添加一个非活跃会话
	scopeMeta := controller.MetaMap[scope]
	meta2 := CreateNewSessionMeta("s2", "agent")
	meta2.IsActive = false
	scopeMeta.AddSession(meta2)

	cleaned, err := g.CleanupAgentInactiveSessions("a1")
	require.NoError(t, err, "CleanupAgentInactiveSessions 不应返回错误")
	assert.Contains(t, cleaned, "a1", "应包含 a1 的清理结果")
}

func TestGlobalSessionController_CleanupAgentInactiveSessions_未找到Agent(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, err := g.CleanupAgentInactiveSessions("nonexistent")
	require.Error(t, err, "未找到的 Agent 应返回错误")
}

func TestGlobalSessionController_CleanupScopeInactiveSessions(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	_, controller, _ := g.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "s1")

	// 手动添加非活跃会话
	scopeMeta := controller.MetaMap[scope]
	meta2 := CreateNewSessionMeta("s2", "agent")
	meta2.IsActive = false
	scopeMeta.AddSession(meta2)

	cleaned := g.CleanupScopeInactiveSessions(scope)
	// 应返回清理结果
	assert.NotNil(t, cleaned, "CleanupScopeInactiveSessions 不应返回 nil")
}

// ──────────────────────────── 便捷方法测试 ────────────────────────────

func TestCreateDirectSession(t *testing.T) {
	tmpDir := t.TempDir()
	// 替换全局单例的 BasePath（仅测试用）
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	isNew, session, err := CreateDirectSession("a1", "u1", "s1")
	if err != nil {
		t.Fatalf("CreateDirectSession() 返回错误: %v", err)
	}
	if !isNew {
		t.Error("首次创建应返回 is_new=true")
	}
	if session == nil {
		t.Fatal("session 不应为 nil")
	}

	// 清理
	instance.RemoveAll()
}

func TestCreateGroupSession(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	isNew, session, err := CreateGroupSession("a1", "g1", "s1")
	if err != nil {
		t.Fatalf("CreateGroupSession() 返回错误: %v", err)
	}
	if !isNew {
		t.Error("首次创建应返回 is_new=true")
	}
	_ = session

	// 清理
	instance.RemoveAll()
}

func TestGetDirectSessionData(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	// 先创建会话
	CreateDirectSession("a1", "u1", "s1")

	// 获取会话数据
	data := GetDirectSessionData("a1", "u1")
	// AgentSessionContainer 在未注入 session 时 Get(nil) 返回 nil
	// 但函数本身应正常执行不 panic
	_ = data

	// 未注册的 Agent 应返回 nil
	data2 := GetDirectSessionData("nonexistent", "u1")
	assert.Nil(t, data2, "未注册的 Agent 应返回 nil")

	// 清理
	instance.RemoveAll()
}

func TestUpdateDirectSessionData(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	CreateDirectSession("a1", "u1", "s1")

	// 更新会话数据
	ok := UpdateDirectSessionData("a1", "u1", map[string]any{"key": "val"})
	// AgentSessionContainer 未注入 session 时 Update 返回 false
	// 但函数本身应正常执行
	_ = ok

	// 未注册的 Agent 应返回 false
	ok2 := UpdateDirectSessionData("nonexistent", "u1", map[string]any{"key": "val"})
	assert.False(t, ok2, "未注册的 Agent 应返回 false")

	// 清理
	instance.RemoveAll()
}

func TestAddDirectSessionDownstream(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	// 创建两个 Agent 的会话
	CreateDirectSession("caller-agent", "u1", "caller-s1")
	CreateDirectSession("target-agent", "u2", "target-s1")

	ok := AddDirectSessionDownstream("caller-agent", "u1", "target-agent", "u2", SharingPolicy{Permission: PermissionRead})
	assert.True(t, ok, "添加下游关系应成功")

	// 清理
	instance.RemoveAll()
}

func TestAddDirectSessionDownstream_调用方Agent不存在(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	CreateDirectSession("target-agent", "u2", "target-s1")

	ok := AddDirectSessionDownstream("nonexistent", "u1", "target-agent", "u2", SharingPolicy{Permission: PermissionRead})
	assert.False(t, ok, "调用方 Agent 不存在时应返回 false")

	// 清理
	instance.RemoveAll()
}

func TestAddDirectSessionDownstream_目标Agent不存在(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	CreateDirectSession("caller-agent", "u1", "caller-s1")

	ok := AddDirectSessionDownstream("caller-agent", "u1", "nonexistent", "u2", SharingPolicy{Permission: PermissionRead})
	assert.False(t, ok, "目标 Agent 不存在时应返回 false")

	// 清理
	instance.RemoveAll()
}

func TestAddDirectSessionDownstream_调用方会话不存在(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	CreateDirectSession("caller-agent", "u1", "caller-s1")
	CreateDirectSession("target-agent", "u2", "target-s1")

	// 使用不存在的用户 ID，无法找到活跃会话
	ok := AddDirectSessionDownstream("caller-agent", "nonexistent-user", "target-agent", "u2", SharingPolicy{Permission: PermissionRead})
	assert.False(t, ok, "调用方会话不存在时应返回 false")

	// 清理
	instance.RemoveAll()
}

func TestAddDirectSessionDownstream_目标会话不存在(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	CreateDirectSession("caller-agent", "u1", "caller-s1")
	CreateDirectSession("target-agent", "u2", "target-s1")

	// 使用不存在的目标用户 ID
	ok := AddDirectSessionDownstream("caller-agent", "u1", "target-agent", "nonexistent-user", SharingPolicy{Permission: PermissionRead})
	assert.False(t, ok, "目标会话不存在时应返回 false")

	// 清理
	instance.RemoveAll()
}

func TestCleanupUserSessions(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	CreateDirectSession("a1", "u1", "s1")

	results, err := CleanupUserSessions("a1", "u1")
	assert.NoError(t, err, "CleanupUserSessions 不应返回错误")
	// 可能没有非活跃会话可清理
	_ = results

	// 未注册的 Agent
	results2, err2 := CleanupUserSessions("nonexistent", "u1")
	assert.NoError(t, err2, "未注册 Agent 不应返回错误")
	assert.Nil(t, results2, "未注册 Agent 应返回 nil")

	// 清理
	instance.RemoveAll()
}

func TestGetUserSessionHistory(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	CreateDirectSession("a1", "u1", "s1")

	sessions := GetUserSessionHistory("a1", "u1")
	assert.NotNil(t, sessions, "已注册的 Agent 应返回会话列表")

	// 未注册的 Agent
	sessions2 := GetUserSessionHistory("nonexistent", "u1")
	assert.Nil(t, sessions2, "未注册的 Agent 应返回 nil")

	// 清理
	instance.RemoveAll()
}

func TestFlushUserSession(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	CreateDirectSession("a1", "u1", "s1")

	err := FlushUserSession("a1", "u1")
	assert.NoError(t, err, "FlushUserSession 应成功")

	// 清理
	instance.RemoveAll()
}

func TestFlushUserSession_未找到Agent(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	err := FlushUserSession("nonexistent", "u1")
	require.Error(t, err, "未找到的 Agent 应返回错误")
	assert.Contains(t, err.Error(), "未找到", "错误信息应包含 '未找到'")

	// 清理
	instance.RemoveAll()
}

func TestVisualizeCallChain(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	_, controller, _ := instance.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	_, session, _ := controller.CreateIfNotExists(scope, "s1")
	session.AddDownstream("a2", "s2", SharingPolicy{Permission: PermissionRead})

	// 手动注册 a2 控制器
	instance.CreateIfNotExistsAgent("a2")

	result := VisualizeCallChain("a1", "s1", 3)
	if !contains(result, "调用链") {
		t.Errorf("可视化结果应包含 '调用链'，得到: %s", result)
	}

	// 清理
	instance.RemoveAll()
}

func TestVisualizeCallChain_Agent未找到(t *testing.T) {
	result := VisualizeCallChain("nonexistent", "s1", 3)
	assert.Contains(t, result, "未找到", "Agent 未找到时应包含提示")
}

func TestVisualizeCallChain_会话未找到(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	instance.CreateIfNotExistsAgent("a1")

	result := VisualizeCallChain("a1", "nonexistent-session", 3)
	assert.Contains(t, result, "未在", "会话未找到时应包含提示")

	// 清理
	instance.RemoveAll()
}

func TestVisualizeCallChain_带FieldScopes(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()

	_, controller, _ := instance.CreateIfNotExistsAgent("a1")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	_, session, _ := controller.CreateIfNotExists(scope, "s1")
	session.AddDownstream("a2", "s2", SharingPolicy{
		Permission:  PermissionRead,
		FieldScopes: map[string]struct{}{"name": {}, "age": {}},
	})

	instance.CreateIfNotExistsAgent("a2")

	result := VisualizeCallChain("a1", "s1", 3)
	assert.Contains(t, result, "字段范围", "应包含字段范围信息")

	// 清理
	instance.RemoveAll()
}

// ──────────────────────────── 回调测试 ────────────────────────────

func TestOnAgentSessionCreated_正常注入(t *testing.T) {
	tmpDir := t.TempDir()
	instance := GetGlobalSessionController()
	instance.mu.Lock()
	instance.BasePath = tmpDir
	instance.Controllers = make(map[string]*SessionController)
	instance.mu.Unlock()
	defer instance.RemoveAll()

	_, controller, _ := instance.CreateIfNotExistsAgent("test-agent")
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	controller.CreateIfNotExists(scope, "test-session")

	// 获取会话并注入 StateAccessor
	session := controller.SessionCache["test-session"]
	require.NotNil(t, session)

	// 构造回调数据
	card := &schema.AgentCard{BaseCard: schema.BaseCard{ID: "test-agent"}}
	sa := newFakeStateAccessor()
	data := &callback.SessionCallEventData{
		SessionID: "test-session",
		Card:      card,
		Session:   sa,
	}

	result := onAgentSessionCreated(context.Background(), data)
	assert.Nil(t, result, "onAgentSessionCreated 应返回 nil")

	// 清理
	instance.RemoveAll()
}

func TestOnAgentSessionCreated_空SessionID(t *testing.T) {
	data := &callback.SessionCallEventData{
		SessionID: "",
		Card:      &schema.AgentCard{BaseCard: schema.BaseCard{ID: "a1"}},
		Session:   newFakeStateAccessor(),
	}
	result := onAgentSessionCreated(context.Background(), data)
	assert.Nil(t, result, "空 SessionID 应返回 nil")
}

func TestOnAgentSessionCreated_空Card(t *testing.T) {
	data := &callback.SessionCallEventData{
		SessionID: "s1",
		Card:      nil,
		Session:   newFakeStateAccessor(),
	}
	result := onAgentSessionCreated(context.Background(), data)
	assert.Nil(t, result, "空 Card 应返回 nil")
}

func TestOnAgentSessionCreated_空Session(t *testing.T) {
	data := &callback.SessionCallEventData{
		SessionID: "s1",
		Card:      &schema.AgentCard{BaseCard: schema.BaseCard{ID: "a1"}},
		Session:   nil,
	}
	result := onAgentSessionCreated(context.Background(), data)
	assert.Nil(t, result, "空 Session 应返回 nil")
}

func TestOnAgentSessionCreated_Card类型不匹配(t *testing.T) {
	data := &callback.SessionCallEventData{
		SessionID: "s1",
		Card:      "not-an-agent-card", // 不是 *schema.AgentCard
		Session:   newFakeStateAccessor(),
	}
	result := onAgentSessionCreated(context.Background(), data)
	assert.Nil(t, result, "Card 类型不匹配应返回 nil")
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

func TestEnsureBasePath_空路径(t *testing.T) {
	g := newTestGlobalController("")
	err := g.ensureBasePath()
	assert.NoError(t, err, "空 BasePath 应直接返回 nil")
}

func TestEnsureBasePath_正常创建(t *testing.T) {
	tmpDir := t.TempDir()
	newPath := tmpDir + "/new-base"
	g := newTestGlobalController(newPath)
	err := g.ensureBasePath()
	require.NoError(t, err, "ensureBasePath 应成功创建目录")

	info, statErr := os.Stat(newPath)
	assert.NoError(t, statErr, "目录应已创建")
	assert.True(t, info.IsDir(), "应为目录")
}

func TestTruncateID(t *testing.T) {
	assert.Equal(t, "abc", truncateID("abc"), "短 ID 不应截断")
	assert.Equal(t, "12345678...", truncateID("123456789012345"), "长 ID 应截断为 8 字符 + ...")
	assert.Equal(t, "12345678", truncateID("12345678"), "刚好 8 字符不应截断")
}

func TestGetOrCreateController_新创建(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	g.mu.Lock()
	ctrl := g.getOrCreateController("new-agent")
	g.mu.Unlock()
	assert.NotNil(t, ctrl, "getOrCreateController 应返回非 nil")
	assert.Equal(t, "new-agent", ctrl.AgentID, "AgentID 应匹配")
	_, ok := g.Controllers["new-agent"]
	assert.True(t, ok, "新控制器应已添加到 map")
}

// ──────────────────────────── 辅助函数 ────────────────────────────

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
