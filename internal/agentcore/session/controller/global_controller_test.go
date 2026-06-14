package controller

import (
	"os"
	"testing"
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
	g.CreateIfNotExistsAgent("a1")
	isNew, _, _ := g.CreateIfNotExistsAgent("a1")
	if isNew {
		t.Error("已存在时应返回 is_new=false")
	}
}

func TestGlobalSessionController_GetAgent(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	g.CreateIfNotExistsAgent("a1")
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

func TestGlobalSessionController_FlushAll(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTestGlobalController(tmpDir)
	g.CreateIfNotExistsAgent("a1")

	if err := g.FlushAll(); err != nil {
		t.Fatalf("FlushAll() 返回错误: %v", err)
	}
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
