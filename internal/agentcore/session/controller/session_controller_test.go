package controller

import (
	"os"
	"testing"
)

func TestNewSessionController(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	if sc.AgentID != "a1" {
		t.Errorf("AgentID = %q, want %q", sc.AgentID, "a1")
	}
	if _, err := os.Stat(sc.BasePath); os.IsNotExist(err) {
		t.Error("BasePath 目录应已创建")
	}
}

func TestSessionController_CreateIfNotExists_首次创建(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}}
	isNew, session, err := sc.CreateIfNotExists(scope, "s1")
	if err != nil {
		t.Fatalf("CreateIfNotExists() 返回错误: %v", err)
	}
	if !isNew {
		t.Error("首次创建应返回 is_new=true")
	}
	if session == nil {
		t.Fatal("session 不应为 nil")
	}
	if session.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", session.SessionID, "s1")
	}
}

func TestSessionController_CreateIfNotExists_已有active(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	sc.CreateIfNotExists(scope, "s1")

	isNew, session, err := sc.CreateIfNotExists(scope, "s2")
	if err != nil {
		t.Fatalf("CreateIfNotExists() 返回错误: %v", err)
	}
	if isNew {
		t.Error("已有活跃会话时 is_new 应为 false")
	}
	if session.SessionID != "s1" {
		t.Errorf("应返回已有会话 s1，得到 %q", session.SessionID)
	}
}

func TestSessionController_CreateIfNotExists_sessionID重复(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope1 := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	scope2 := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u2"}}
	sc.CreateIfNotExists(scope1, "s1")

	_, _, err := sc.CreateIfNotExists(scope2, "s1")
	if err == nil {
		t.Error("sessionID 重复应返回错误")
	}
}

func TestSessionController_GetScopeActiveSession(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	sc.CreateIfNotExists(scope, "s1")

	session := sc.GetScopeActiveSession(scope)
	if session == nil {
		t.Fatal("应返回活跃会话")
	}
	if session.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", session.SessionID, "s1")
	}
}

func TestSessionController_GetScopeActiveSession_无活跃(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	session := sc.GetScopeActiveSession(scope)
	if session != nil {
		t.Error("无活跃会话应返回 nil")
	}
}

func TestSessionController_GetScopeSessions(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	sc.CreateIfNotExists(scope, "s1")

	sessions := sc.GetScopeSessions(scope)
	if len(sessions) != 1 {
		t.Fatalf("Sessions 长度 = %d, want 1", len(sessions))
	}
}

func TestSessionController_ActivateSession(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	sc.CreateIfNotExists(scope, "s1")

	// 手动添加第二个会话
	scope2 := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u2"}}
	sc.CreateIfNotExists(scope2, "s2")

	// 激活 s2（不同 scope 下的）
	err := sc.ActivateSession("s2")
	if err != nil {
		t.Fatalf("ActivateSession() 返回错误: %v", err)
	}
}

func TestSessionController_GetScopeMeta(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	sc.CreateIfNotExists(scope, "s1")

	meta := sc.GetScopeMeta(scope)
	if meta.ActiveSession != "s1" {
		t.Errorf("ActiveSession = %q, want %q", meta.ActiveSession, "s1")
	}
}

func TestSessionController_ListMetas(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	sc.CreateIfNotExists(scope, "s1")

	metas := sc.ListMetas()
	if len(metas) != 1 {
		t.Fatalf("ListMetas 长度 = %d, want 1", len(metas))
	}
}

func TestSessionController_FlushAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	sc.CreateIfNotExists(scope, "s1")

	// Flush
	if err := sc.Flush(); err != nil {
		t.Fatalf("Flush() 返回错误: %v", err)
	}

	// 验证 sessions.json 存在
	metaFile := SessionPaths{}.MetaFile(tmpDir, "a1")
	if _, err := os.Stat(metaFile); os.IsNotExist(err) {
		t.Fatal("Flush 后 sessions.json 应存在")
	}

	// 创建新控制器并 Load
	sc2 := NewSessionController("a1", tmpDir)
	if err := sc2.Load(true); err != nil {
		t.Fatalf("Load() 返回错误: %v", err)
	}

	session := sc2.GetScopeActiveSession(scope)
	if session == nil {
		t.Fatal("Load 后应能获取活跃会话")
	}
	if session.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", session.SessionID, "s1")
	}
}

func TestSessionController_CleanupScopeInactiveSessions(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	sc.CreateIfNotExists(scope, "s1")

	// 手动添加非活跃会话
	scopeMeta := sc.MetaMap[scope]
	meta2 := CreateNewSessionMeta("s2", "agent")
	meta2.IsActive = false
	scopeMeta.AddSession(meta2)

	// 手动创建 session 目录
	sessionDir := SessionPaths{}.SessionDir(tmpDir, "a1", "s2")
	os.MkdirAll(sessionDir, 0o755)

	results, err := sc.CleanupScopeInactiveSessions(scope)
	if err != nil {
		t.Fatalf("CleanupScopeInactiveSessions() 返回错误: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("结果长度 = %d, want 1", len(results))
	}
	if len(results[0].Sessions) != 1 {
		t.Errorf("清理的会话数 = %d, want 1", len(results[0].Sessions))
	}
	if results[0].Sessions[0].SessionID != "s2" {
		t.Errorf("清理的会话 ID = %q, want %q", results[0].Sessions[0].SessionID, "s2")
	}
}

func TestSessionController_RemoveSession(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	sc.CreateIfNotExists(scope, "s1")

	removed := sc.RemoveSession("s1", nil)
	if len(removed) != 1 {
		t.Fatalf("删除结果长度 = %d, want 1", len(removed))
	}
	if removed[0].SessionMeta.SessionID != "s1" {
		t.Errorf("删除的 SessionID = %q, want %q", removed[0].SessionMeta.SessionID, "s1")
	}
	if _, ok := sc.SessionCache["s1"]; ok {
		t.Error("删除后缓存中不应存在该会话")
	}
}

func TestSessionController_RemoveScopeSessions(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	sc.CreateIfNotExists(scope, "s1")

	removed := sc.RemoveScopeSessions(scope)
	if len(removed) != 1 {
		t.Fatalf("删除结果长度 = %d, want 1", len(removed))
	}
}

func TestSessionController_RemoveAll(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewSessionController("a1", tmpDir)
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	sc.CreateIfNotExists(scope, "s1")

	sc.RemoveAll()
	if len(sc.SessionCache) != 0 {
		t.Error("RemoveAll 后缓存应为空")
	}
	if len(sc.MetaMap) != 0 {
		t.Error("RemoveAll 后元数据应为空")
	}
}
