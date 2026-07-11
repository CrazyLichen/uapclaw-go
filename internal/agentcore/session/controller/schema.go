package controller

import (
	"sort"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionMeta 单会话元数据，存储在 sessions.json 中。
// 对应 Python: openjiuwen/core/session/session_controller/schema.py (SessionMeta)
type SessionMeta struct {
	// SessionID 会话唯一标识（UUID）
	SessionID string `json:"session_id"`
	// CreatedAt 创建时间戳（UTC 秒数）
	CreatedAt float64 `json:"created_at"`
	// UpdatedAt 最后更新时间戳（UTC 秒数）
	UpdatedAt float64 `json:"updated_at"`
	// Version 会话数据版本号，用于乐观锁或迁移
	Version int `json:"version"`
	// IsActive 是否为当前活跃会话，每个 SessionScopeKey 下只允许一个活跃会话
	IsActive bool `json:"is_active"`
	// DataContainerType 数据容器类型，必须在 DataContainerFactory 中注册
	DataContainerType string `json:"data_container_type"`
}

// ScopeSessionsMeta 单个 SessionScope 下所有会话的元数据集合。
// 对应 Python: openjiuwen/core/session/session_controller/schema.py (ScopeSessionsMeta)
type ScopeSessionsMeta struct {
	// SessionScopeKey 对应的 SessionScopeKey 字符串表示
	SessionScopeKey string `json:"session_scope_key"`
	// ActiveSession 当前活跃会话的 sessionID，可为空
	ActiveSession string `json:"active_session"`
	// Sessions 所有会话元数据列表，按更新时间降序排列
	Sessions []SessionMeta `json:"sessions"`
}

// CleanupResult 清理结果，包含被清理会话所在的 Scope 和元数据列表
type CleanupResult struct {
	// SessionScope 被清理的会话作用域
	SessionScope SessionScope
	// Sessions 被清理的会话元数据列表
	Sessions []SessionMeta
}

// RemoveResult 删除结果，包含被删除会话所在的 Scope 和元数据
type RemoveResult struct {
	// SessionScope 被删除的会话作用域
	SessionScope SessionScope
	// SessionMeta 被删除的会话元数据
	SessionMeta SessionMeta
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateNewSessionMeta 创建新的会话元数据，设置当前时间戳和默认值。
func CreateNewSessionMeta(sessionID string, dataContainerType string) SessionMeta {
	now := float64(time.Now().UnixMilli()) / 1000.0
	if dataContainerType == "" {
		dataContainerType = "agent"
	}
	return SessionMeta{
		SessionID:         sessionID,
		CreatedAt:         now,
		UpdatedAt:         now,
		Version:           1,
		IsActive:          true,
		DataContainerType: dataContainerType,
	}
}

// ──── SessionMeta 方法 ────

// UpdateTimestamp 更新时间戳为当前时间
func (m *SessionMeta) UpdateTimestamp() {
	m.UpdatedAt = float64(time.Now().UnixMilli()) / 1000.0
}

// IncrementVersion 递增版本号
func (m *SessionMeta) IncrementVersion() {
	m.Version++
}

// ──── ScopeSessionsMeta 方法 ────

// GetSession 根据 sessionID 获取会话元数据，未找到返回 nil
func (m *ScopeSessionsMeta) GetSession(sessionID string) *SessionMeta {
	for i := range m.Sessions {
		if m.Sessions[i].SessionID == sessionID {
			return &m.Sessions[i]
		}
	}
	return nil
}

// AddSession 添加会话元数据。
// 若添加的会话为活跃状态，先将其他会话全部去激活。
func (m *ScopeSessionsMeta) AddSession(meta SessionMeta) {
	if meta.IsActive {
		m.DeactivateAllSessions()
		m.ActiveSession = meta.SessionID
	}
	m.Sessions = append(m.Sessions, meta)
	m.SortSessions()
}

// RemoveSession 删除指定会话的元数据，返回被删除的元数据。
// 若删除的是活跃会话，同时清空 ActiveSession 标记。
func (m *ScopeSessionsMeta) RemoveSession(sessionID string) *SessionMeta {
	for i := range m.Sessions {
		if m.Sessions[i].SessionID == sessionID {
			removed := m.Sessions[i]
			m.Sessions = append(m.Sessions[:i], m.Sessions[i+1:]...)
			if m.ActiveSession == sessionID {
				m.ActiveSession = ""
			}
			return &removed
		}
	}
	return nil
}

// ActivateSession 激活指定会话，先将所有会话去激活。
// 返回是否成功激活。
func (m *ScopeSessionsMeta) ActivateSession(sessionID string) bool {
	session := m.GetSession(sessionID)
	if session == nil {
		return false
	}
	m.DeactivateAllSessions()
	session.IsActive = true
	session.UpdateTimestamp()
	m.ActiveSession = sessionID
	m.SortSessions()
	return true
}

// DeactivateAllSessions 将所有会话设为非活跃状态
func (m *ScopeSessionsMeta) DeactivateAllSessions() {
	for i := range m.Sessions {
		m.Sessions[i].IsActive = false
	}
	m.ActiveSession = ""
}

// SortSessions 按更新时间降序排列会话列表
func (m *ScopeSessionsMeta) SortSessions() {
	sort.Slice(m.Sessions, func(i, j int) bool {
		return m.Sessions[i].UpdatedAt > m.Sessions[j].UpdatedAt
	})
}

// GetActiveSession 获取当前活跃会话的元数据，无活跃会话返回 nil
func (m *ScopeSessionsMeta) GetActiveSession() *SessionMeta {
	if m.ActiveSession == "" {
		return nil
	}
	return m.GetSession(m.ActiveSession)
}

// UpdateSessionTimestamp 更新指定会话的时间戳。
// 返回是否成功更新。
func (m *ScopeSessionsMeta) UpdateSessionTimestamp(sessionID string) bool {
	session := m.GetSession(sessionID)
	if session == nil {
		return false
	}
	session.UpdateTimestamp()
	m.SortSessions()
	return true
}

// IncrementSessionVersion 递增指定会话的版本号。
// 返回是否成功递增。
func (m *ScopeSessionsMeta) IncrementSessionVersion(sessionID string) bool {
	session := m.GetSession(sessionID)
	if session == nil {
		return false
	}
	session.IncrementVersion()
	return true
}
