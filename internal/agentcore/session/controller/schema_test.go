package controller

import "testing"

// ──────────────────────────── SessionMeta 测试 ────────────────────────────

func TestCreateNewSessionMeta(t *testing.T) {
	meta := CreateNewSessionMeta("sess1", "agent")
	if meta.SessionID != "sess1" {
		t.Errorf("SessionID = %q, want %q", meta.SessionID, "sess1")
	}
	if meta.CreatedAt <= 0 {
		t.Errorf("CreatedAt 应为正数，得到 %v", meta.CreatedAt)
	}
	if meta.UpdatedAt <= 0 {
		t.Errorf("UpdatedAt 应为正数，得到 %v", meta.UpdatedAt)
	}
	if meta.Version != 1 {
		t.Errorf("Version = %d, want 1", meta.Version)
	}
	if !meta.IsActive {
		t.Errorf("IsActive 应为 true")
	}
	if meta.DataContainerType != "agent" {
		t.Errorf("DataContainerType = %q, want %q", meta.DataContainerType, "agent")
	}
}

func TestCreateNewSessionMeta_默认容器类型(t *testing.T) {
	meta := CreateNewSessionMeta("sess1", "")
	if meta.DataContainerType != "agent" {
		t.Errorf("DataContainerType = %q, want %q", meta.DataContainerType, "agent")
	}
}

func TestSessionMeta_UpdateTimestamp(t *testing.T) {
	meta := CreateNewSessionMeta("sess1", "agent")
	oldUpdatedAt := meta.UpdatedAt
	meta.UpdateTimestamp()
	if meta.UpdatedAt < oldUpdatedAt {
		t.Errorf("UpdateTimestamp 后 UpdatedAt 应不小于之前值")
	}
}

func TestSessionMeta_IncrementVersion(t *testing.T) {
	meta := CreateNewSessionMeta("sess1", "agent")
	meta.IncrementVersion()
	if meta.Version != 2 {
		t.Errorf("Version = %d, want 2", meta.Version)
	}
}

// ──────────────────────────── ScopeSessionsMeta 测试 ────────────────────────────

func newTestScopeSessionsMeta() *ScopeSessionsMeta {
	return &ScopeSessionsMeta{
		SessionScopeKey: "agent:a1:main",
	}
}

func TestScopeSessionsMeta_AddSession(t *testing.T) {
	m := newTestScopeSessionsMeta()
	meta := CreateNewSessionMeta("sess1", "agent")
	m.AddSession(meta)
	if len(m.Sessions) != 1 {
		t.Fatalf("Sessions 长度 = %d, want 1", len(m.Sessions))
	}
	if m.ActiveSession != "sess1" {
		t.Errorf("ActiveSession = %q, want %q", m.ActiveSession, "sess1")
	}
}

func TestScopeSessionsMeta_AddSession_激活时去激活其他(t *testing.T) {
	m := newTestScopeSessionsMeta()
	meta1 := CreateNewSessionMeta("sess1", "agent")
	meta1.IsActive = true
	m.AddSession(meta1)

	meta2 := CreateNewSessionMeta("sess2", "agent")
	meta2.IsActive = true
	m.AddSession(meta2)

	// sess1 应被去激活
	s1 := m.GetSession("sess1")
	if s1 == nil {
		t.Fatal("sess1 不应被删除")
	}
	if s1.IsActive {
		t.Errorf("添加新活跃会话后，旧会话应被去激活")
	}
	if m.ActiveSession != "sess2" {
		t.Errorf("ActiveSession = %q, want %q", m.ActiveSession, "sess2")
	}
}

func TestScopeSessionsMeta_RemoveSession(t *testing.T) {
	m := newTestScopeSessionsMeta()
	m.AddSession(CreateNewSessionMeta("sess1", "agent"))
	removed := m.RemoveSession("sess1")
	if removed == nil {
		t.Fatal("RemoveSession 应返回被删除的元数据")
	}
	if removed.SessionID != "sess1" {
		t.Errorf("删除的 SessionID = %q, want %q", removed.SessionID, "sess1")
	}
	if len(m.Sessions) != 0 {
		t.Errorf("删除后 Sessions 长度应为 0")
	}
}

func TestScopeSessionsMeta_RemoveSession_删除活跃会话(t *testing.T) {
	m := newTestScopeSessionsMeta()
	m.AddSession(CreateNewSessionMeta("sess1", "agent"))
	m.RemoveSession("sess1")
	if m.ActiveSession != "" {
		t.Errorf("删除活跃会话后 ActiveSession 应为空，得到 %q", m.ActiveSession)
	}
}

func TestScopeSessionsMeta_RemoveSession_不存在(t *testing.T) {
	m := newTestScopeSessionsMeta()
	removed := m.RemoveSession("nonexistent")
	if removed != nil {
		t.Errorf("删除不存在的会话应返回 nil")
	}
}

func TestScopeSessionsMeta_ActivateSession(t *testing.T) {
	m := newTestScopeSessionsMeta()
	meta1 := CreateNewSessionMeta("sess1", "agent")
	meta1.IsActive = true
	m.AddSession(meta1)

	meta2 := CreateNewSessionMeta("sess2", "agent")
	meta2.IsActive = false
	m.AddSession(meta2)

	ok := m.ActivateSession("sess2")
	if !ok {
		t.Errorf("ActivateSession 应返回 true")
	}
	if m.ActiveSession != "sess2" {
		t.Errorf("ActiveSession = %q, want %q", m.ActiveSession, "sess2")
	}
	s1 := m.GetSession("sess1")
	if s1.IsActive {
		t.Errorf("激活 sess2 后 sess1 应被去激活")
	}
	s2 := m.GetSession("sess2")
	if !s2.IsActive {
		t.Errorf("sess2 应为活跃状态")
	}
}

func TestScopeSessionsMeta_ActivateSession_不存在(t *testing.T) {
	m := newTestScopeSessionsMeta()
	ok := m.ActivateSession("nonexistent")
	if ok {
		t.Errorf("激活不存在的会话应返回 false")
	}
}

func TestScopeSessionsMeta_DeactivateAllSessions(t *testing.T) {
	m := newTestScopeSessionsMeta()
	m.AddSession(CreateNewSessionMeta("sess1", "agent"))
	m.DeactivateAllSessions()
	if m.ActiveSession != "" {
		t.Errorf("DeactivateAllSessions 后 ActiveSession 应为空")
	}
	for _, s := range m.Sessions {
		if s.IsActive {
			t.Errorf("所有会话应为非活跃状态")
		}
	}
}

func TestScopeSessionsMeta_SortSessions(t *testing.T) {
	m := newTestScopeSessionsMeta()
	meta1 := CreateNewSessionMeta("sess1", "agent")
	meta1.IsActive = false
	m.AddSession(meta1)
	meta2 := CreateNewSessionMeta("sess2", "agent")
	meta2.IsActive = false
	m.AddSession(meta2)
	// 手动设置时间戳模拟不同更新时间
	m.Sessions[0].UpdatedAt = 1000.0
	m.Sessions[1].UpdatedAt = 2000.0
	m.SortSessions()
	if m.Sessions[0].SessionID != "sess2" {
		t.Errorf("排序后第一个应为 updated_at 更大的 sess2")
	}
}

func TestScopeSessionsMeta_GetActiveSession(t *testing.T) {
	m := newTestScopeSessionsMeta()
	m.AddSession(CreateNewSessionMeta("sess1", "agent"))
	active := m.GetActiveSession()
	if active == nil {
		t.Fatal("GetActiveSession 应返回活跃会话")
	}
	if active.SessionID != "sess1" {
		t.Errorf("ActiveSession.SessionID = %q, want %q", active.SessionID, "sess1")
	}
}

func TestScopeSessionsMeta_GetActiveSession_无活跃(t *testing.T) {
	m := newTestScopeSessionsMeta()
	active := m.GetActiveSession()
	if active != nil {
		t.Errorf("无活跃会话时应返回 nil")
	}
}

func TestScopeSessionsMeta_UpdateSessionTimestamp(t *testing.T) {
	m := newTestScopeSessionsMeta()
	m.AddSession(CreateNewSessionMeta("sess1", "agent"))
	oldUpdatedAt := m.GetSession("sess1").UpdatedAt
	ok := m.UpdateSessionTimestamp("sess1")
	if !ok {
		t.Errorf("UpdateSessionTimestamp 应返回 true")
	}
	if m.GetSession("sess1").UpdatedAt < oldUpdatedAt {
		t.Errorf("更新后 UpdatedAt 应不小于之前值")
	}
}

func TestScopeSessionsMeta_UpdateSessionTimestamp_不存在(t *testing.T) {
	m := newTestScopeSessionsMeta()
	ok := m.UpdateSessionTimestamp("nonexistent")
	if ok {
		t.Errorf("更新不存在的会话应返回 false")
	}
}

func TestScopeSessionsMeta_IncrementSessionVersion(t *testing.T) {
	m := newTestScopeSessionsMeta()
	m.AddSession(CreateNewSessionMeta("sess1", "agent"))
	ok := m.IncrementSessionVersion("sess1")
	if !ok {
		t.Errorf("IncrementSessionVersion 应返回 true")
	}
	if m.GetSession("sess1").Version != 2 {
		t.Errorf("递增后 Version = %d, want 2", m.GetSession("sess1").Version)
	}
}

func TestScopeSessionsMeta_IncrementSessionVersion_不存在(t *testing.T) {
	m := newTestScopeSessionsMeta()
	ok := m.IncrementSessionVersion("nonexistent")
	if ok {
		t.Errorf("递增不存在的会话应返回 false")
	}
}
