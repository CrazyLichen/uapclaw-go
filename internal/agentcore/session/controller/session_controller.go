package controller

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionController 单 Agent 会话管理器。
// 负责管理该 Agent 下所有会话的生命周期，包括创建、查询、激活、删除，
// 以及维护 sessions.json 元数据文件和会话对象缓存。
// 对应 Python: openjiuwen/core/session/session_controller/session_controller.py (SessionController)
type SessionController struct {
	mu sync.Mutex
	// AgentID 所属 Agent 标识
	AgentID string
	// rootPath 存储根目录
	rootPath string
	// BasePath 会话存储目录（rootPath/agentID/sessions）
	BasePath string
	// dataContainerType 数据容器类型
	dataContainerType string
	// SessionCache 会话对象缓存：sessionID → ChainSession
	SessionCache map[string]*ChainSession
	// MetaMap 元数据映射：SessionScope → ScopeSessionsMeta
	MetaMap map[SessionScope]*ScopeSessionsMeta
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionController 创建单 Agent 会话管理器。
// basePath 为存储根目录，实际会话目录为 basePath/agentID/sessions。
func NewSessionController(agentID string, basePath string, dataContainerType ...string) *SessionController {
	dct := DefaultDataContainerType
	if len(dataContainerType) > 0 && dataContainerType[0] != "" {
		dct = dataContainerType[0]
	}
	sc := &SessionController{
		AgentID:           agentID,
		rootPath:          basePath,
		BasePath:          SessionPaths{}.SessionsDir(basePath, agentID),
		dataContainerType: dct,
		SessionCache:      make(map[string]*ChainSession),
		MetaMap:           make(map[SessionScope]*ScopeSessionsMeta),
	}
	os.MkdirAll(sc.BasePath, 0o755)
	return sc
}

// ──────────────────────────── 持久化方法 ────────────────────────────

// Flush 持久化所有变更到磁盘
func (sc *SessionController) Flush() error {
	sc.mu.Lock()
	// 收集需要 flush 的 session 列表
	sessions := make([]*ChainSession, 0, len(sc.SessionCache))
	for _, s := range sc.SessionCache {
		sessions = append(sessions, s)
	}
	sc.mu.Unlock()

	// 并行 flush 所有 session
	for _, s := range sessions {
		if err := s.Flush(); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("action", "session_controller_flush").
				Str("agent_id", sc.AgentID).
				Str("session_id", s.SessionID).
				Err(err).
				Msg("刷写会话失败")
			return err
		}
	}

	// 写元数据文件
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.writeMetaFile()
}

// FlushSession 持久化指定会话到磁盘
func (sc *SessionController) FlushSession(sessionID string) error {
	sc.mu.Lock()
	session, ok := sc.SessionCache[sessionID]
	sc.mu.Unlock()

	if !ok {
		return nil
	}

	if err := session.Flush(); err != nil {
		return err
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.writeMetaFile()
}

// FlushScope 持久化指定作用域的会话到磁盘
func (sc *SessionController) FlushScope(sessionScope SessionScope) error {
	sc.mu.Lock()
	sessions := make([]*ChainSession, 0)
	for id, s := range sc.SessionCache {
		if s.SessionScope.String() == sessionScope.String() {
			sessions = append(sessions, s)
			_ = id
		}
	}
	sc.mu.Unlock()

	for _, s := range sessions {
		if err := s.Flush(); err != nil {
			return err
		}
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.writeMetaFile()
}

// Load 从磁盘加载此 Agent 的会话元数据
func (sc *SessionController) Load(loadActiveOnly bool) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	metaFile := SessionPaths{}.MetaFile(sc.rootPath, sc.AgentID)
	data, err := os.ReadFile(metaFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var metaData map[string]any
	if err := json.Unmarshal(data, &metaData); err != nil {
		return err
	}

	// 清空当前元数据
	sc.MetaMap = make(map[SessionScope]*ScopeSessionsMeta)

	for scopeKeyStr, scopeMetaData := range metaData {
		scopeKey, err := ParseSessionScopeKey(scopeKeyStr)
		if err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("action", "session_controller_load").
				Str("scope_key", scopeKeyStr).
				Err(err).
				Msg("解析 SessionScopeKey 失败")
			continue
		}

		// 反序列化 ScopeSessionsMeta
		scopeMetaBytes, _ := json.Marshal(scopeMetaData)
		var scopeMeta ScopeSessionsMeta
		if err := json.Unmarshal(scopeMetaBytes, &scopeMeta); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("action", "session_controller_load").
				Str("scope_key", scopeKeyStr).
				Err(err).
				Msg("解析 ScopeSessionsMeta 失败")
			continue
		}

		sc.MetaMap[scopeKey.SessionScope] = &scopeMeta

		// 加载会话数据
		if loadActiveOnly {
			if scopeMeta.ActiveSession != "" {
				sc.loadSession(scopeKey.SessionScope, scopeMeta.ActiveSession)
			}
		} else {
			for _, sm := range scopeMeta.Sessions {
				sc.loadSession(scopeKey.SessionScope, sm.SessionID)
			}
		}
	}

	logger.Info(logger.ComponentAgentCore).
		Str("action", "session_controller_loaded").
		Str("agent_id", sc.AgentID).
		Int("scopes", len(sc.MetaMap)).
		Int("cache_size", len(sc.SessionCache)).
		Msg("会话控制器加载完成")

	return nil
}

// LoadScope 从磁盘加载指定作用域的会话数据
func (sc *SessionController) LoadScope(sessionScope SessionScope, loadActiveOnly bool) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	metaFile := SessionPaths{}.MetaFile(sc.rootPath, sc.AgentID)
	data, err := os.ReadFile(metaFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var metaData map[string]any
	if err := json.Unmarshal(data, &metaData); err != nil {
		return err
	}

	for scopeKeyStr, scopeMetaData := range metaData {
		scopeKey, err := ParseSessionScopeKey(scopeKeyStr)
		if err != nil {
			continue
		}
		if scopeKey.SessionScope.String() != sessionScope.String() {
			continue
		}

		scopeMetaBytes, _ := json.Marshal(scopeMetaData)
		var scopeMeta ScopeSessionsMeta
		if err := json.Unmarshal(scopeMetaBytes, &scopeMeta); err != nil {
			continue
		}

		sc.MetaMap[scopeKey.SessionScope] = &scopeMeta

		if loadActiveOnly {
			if scopeMeta.ActiveSession != "" {
				sc.loadSession(scopeKey.SessionScope, scopeMeta.ActiveSession)
			}
		} else {
			for _, sm := range scopeMeta.Sessions {
				sc.loadSession(scopeKey.SessionScope, sm.SessionID)
			}
		}
		break
	}

	return nil
}

// ──────────────────────────── 会话管理方法 ────────────────────────────

// CreateIfNotExists 在指定作用域内获取或创建会话。
// 若该 SessionScope 已有活跃会话则直接返回（is_new=false），
// 否则创建新会话（is_new=true）。
func (sc *SessionController) CreateIfNotExists(sessionScope SessionScope, sessionID string, opts ...ContainerOption) (bool, *ChainSession, error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// 检查 sessionID 是否已存在
	if _, ok := sc.SessionCache[sessionID]; ok {
		return false, nil, fmt.Errorf("sessionID %q 已存在", sessionID)
	}
	for _, existingScopeMeta := range sc.MetaMap {
		if existingScopeMeta.GetSession(sessionID) != nil {
			return false, nil, fmt.Errorf("sessionID %q 已存在", sessionID)
		}
	}

	// 获取或创建 SessionScope 元数据
	if _, ok := sc.MetaMap[sessionScope]; !ok {
		scopeKey := SessionScopeKey{AgentID: sc.AgentID, SessionScope: sessionScope}
		sc.MetaMap[sessionScope] = &ScopeSessionsMeta{
			SessionScopeKey: scopeKey.String(),
		}
	}
	scopeMeta := sc.MetaMap[sessionScope]

	// 检查是否已有活跃会话
	activeSession := scopeMeta.GetActiveSession()
	if activeSession != nil {
		// 返回已有活跃会话
		if _, ok := sc.SessionCache[activeSession.SessionID]; !ok {
			sc.loadSession(sessionScope, activeSession.SessionID)
		}
		if s, ok := sc.SessionCache[activeSession.SessionID]; ok {
			return false, s, nil
		}
	}

	// 创建新会话
	sessionDir := SessionPaths{}.SessionDir(sc.rootPath, sc.AgentID, sessionID)
	os.MkdirAll(sessionDir, 0o755)

	// 创建数据容器
	container, err := GetFactory().Create(sc.dataContainerType, opts...)
	if err != nil {
		return false, nil, err
	}

	session := NewChainSession(sc.AgentID, sessionScope, sessionID, container, sessionDir)

	// 创建会话元数据
	sessionMeta := CreateNewSessionMeta(sessionID, sc.dataContainerType)
	session.UpdateFromMeta(sessionMeta)

	// 添加到元数据
	scopeMeta.AddSession(sessionMeta)

	// 添加到缓存
	sc.SessionCache[sessionID] = session

	// 保存到磁盘
	session.Flush()
	sc.writeMetaFile()

	logger.Info(logger.ComponentAgentCore).
		Str("action", "session_created").
		Str("agent_id", sc.AgentID).
		Str("session_id", sessionID).
		Str("scope", sessionScope.String()).
		Str("container_type", sc.dataContainerType).
		Msg("创建新会话")

	return true, session, nil
}

// GetScopeActiveSession 获取指定作用域的当前活跃会话
func (sc *SessionController) GetScopeActiveSession(sessionScope SessionScope) *ChainSession {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	scopeMeta, ok := sc.MetaMap[sessionScope]
	if !ok || scopeMeta.ActiveSession == "" {
		return nil
	}

	session, ok := sc.SessionCache[scopeMeta.ActiveSession]
	if !ok {
		sc.loadSession(sessionScope, scopeMeta.ActiveSession)
		session = sc.SessionCache[scopeMeta.ActiveSession]
	}
	return session
}

// GetScopeSessions 获取指定作用域下所有已加载的会话列表
func (sc *SessionController) GetScopeSessions(sessionScope SessionScope) []*ChainSession {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	scopeMeta, ok := sc.MetaMap[sessionScope]
	if !ok {
		return nil
	}

	var sessions []*ChainSession
	for _, sm := range scopeMeta.Sessions {
		if s, ok := sc.SessionCache[sm.SessionID]; ok {
			sessions = append(sessions, s)
		}
	}
	return sessions
}

// ActivateSession 激活指定会话
func (sc *SessionController) ActivateSession(sessionID string) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// 查找会话
	var session *ChainSession
	var targetScope SessionScope

	for _, s := range sc.SessionCache {
		if s.SessionID == sessionID {
			session = s
			targetScope = s.SessionScope
			break
		}
	}

	if session == nil {
		// 尝试从元数据查找
		for scope, scopeMeta := range sc.MetaMap {
			if sm := scopeMeta.GetSession(sessionID); sm != nil {
				targetScope = scope
				sc.loadSession(scope, sessionID)
				session = sc.SessionCache[sessionID]
				break
			}
		}
	}

	if session == nil {
		return fmt.Errorf("会话 %q 未找到", sessionID)
	}

	// 激活会话
	if scopeMeta, ok := sc.MetaMap[targetScope]; ok {
		scopeMeta.ActivateSession(sessionID)
		session.SetIsActive(true)
		session.Flush()
		sc.writeMetaFile()

		logger.Info(logger.ComponentAgentCore).
			Str("action", "session_activated").
			Str("agent_id", sc.AgentID).
			Str("session_id", sessionID).
			Msg("会话已激活")
	}

	return nil
}

// GetScopeMeta 获取指定作用域的元数据
func (sc *SessionController) GetScopeMeta(sessionScope SessionScope) ScopeSessionsMeta {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if scopeMeta, ok := sc.MetaMap[sessionScope]; ok {
		return *scopeMeta
	}
	scopeKey := SessionScopeKey{AgentID: sc.AgentID, SessionScope: sessionScope}
	return ScopeSessionsMeta{
		SessionScopeKey: scopeKey.String(),
	}
}

// ListMetas 获取所有已知作用域的元数据映射副本
func (sc *SessionController) ListMetas() map[SessionScope]ScopeSessionsMeta {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	result := make(map[SessionScope]ScopeSessionsMeta, len(sc.MetaMap))
	for scope, meta := range sc.MetaMap {
		result[scope] = *meta
	}
	return result
}

// ──────────────────────────── 清理方法 ────────────────────────────

// CleanupScopeInactiveSessions 清理指定作用域下的非活跃会话
func (sc *SessionController) CleanupScopeInactiveSessions(sessionScope SessionScope) ([]CleanupResult, error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	scopeMeta, ok := sc.MetaMap[sessionScope]
	if !ok {
		return nil, fmt.Errorf("会话作用域 %s 未找到", sessionScope.String())
	}

	var cleaned []SessionMeta
	var inactiveSessions []SessionMeta

	for _, sm := range scopeMeta.Sessions {
		if !sm.IsActive {
			inactiveSessions = append(inactiveSessions, sm)
		}
	}

	for _, sm := range inactiveSessions {
		// 从缓存移除
		delete(sc.SessionCache, sm.SessionID)

		// 删除磁盘数据
		sessionDir := SessionPaths{}.SessionDir(sc.rootPath, sc.AgentID, sm.SessionID)
		os.RemoveAll(sessionDir)

		// 从元数据移除
		scopeMeta.RemoveSession(sm.SessionID)
		cleaned = append(cleaned, sm)
	}

	if len(cleaned) > 0 {
		sc.writeMetaFile()
	}

	logger.Info(logger.ComponentAgentCore).
		Str("action", "cleanup_inactive").
		Str("agent_id", sc.AgentID).
		Str("scope", sessionScope.String()).
		Int("cleaned", len(cleaned)).
		Msg("清理非活跃会话")

	return []CleanupResult{{SessionScope: sessionScope, Sessions: cleaned}}, nil
}

// RemoveSession 删除指定会话及其所有持久化数据
func (sc *SessionController) RemoveSession(sessionID string, sessionScope *SessionScope) []RemoveResult {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	var removed []RemoveResult

	var scopesToSearch []SessionScope
	if sessionScope != nil {
		scopesToSearch = []SessionScope{*sessionScope}
	} else {
		for scope := range sc.MetaMap {
			scopesToSearch = append(scopesToSearch, scope)
		}
	}

	for _, scope := range scopesToSearch {
		scopeMeta, ok := sc.MetaMap[scope]
		if !ok {
			continue
		}
		sm := scopeMeta.GetSession(sessionID)
		if sm == nil {
			continue
		}

		// 从缓存移除
		delete(sc.SessionCache, sessionID)

		// 删除磁盘数据
		sessionDir := SessionPaths{}.SessionDir(sc.rootPath, sc.AgentID, sessionID)
		os.RemoveAll(sessionDir)

		// 从元数据移除
		removedMeta := scopeMeta.RemoveSession(sessionID)
		if removedMeta != nil {
			removed = append(removed, RemoveResult{SessionScope: scope, SessionMeta: *removedMeta})
		}
	}

	if len(removed) > 0 {
		sc.writeMetaFile()
	}

	return removed
}

// RemoveScopeSessions 删除指定作用域下的所有会话
func (sc *SessionController) RemoveScopeSessions(sessionScope SessionScope) []SessionMeta {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	scopeMeta, ok := sc.MetaMap[sessionScope]
	if !ok {
		return nil
	}

	var removed []SessionMeta
	for _, sm := range scopeMeta.Sessions {
		delete(sc.SessionCache, sm.SessionID)
		sessionDir := SessionPaths{}.SessionDir(sc.rootPath, sc.AgentID, sm.SessionID)
		os.RemoveAll(sessionDir)
		removed = append(removed, sm)
	}

	scopeMeta.Sessions = nil
	scopeMeta.ActiveSession = ""
	if len(scopeMeta.Sessions) == 0 {
		delete(sc.MetaMap, sessionScope)
	}

	sc.writeMetaFile()

	logger.Info(logger.ComponentAgentCore).
		Str("action", "remove_scope_sessions").
		Str("agent_id", sc.AgentID).
		Str("scope", sessionScope.String()).
		Int("count", len(removed)).
		Msg("删除作用域下所有会话")

	return removed
}

// RemoveAll 删除此 Agent 的所有会话数据和元数据文件
func (sc *SessionController) RemoveAll() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.SessionCache = make(map[string]*ChainSession)
	sc.MetaMap = make(map[SessionScope]*ScopeSessionsMeta)
	os.RemoveAll(sc.BasePath)

	logger.Info(logger.ComponentAgentCore).
		Str("action", "remove_all").
		Str("agent_id", sc.AgentID).
		Msg("删除所有会话数据")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadSession 加载指定会话到缓存
func (sc *SessionController) loadSession(sessionScope SessionScope, sessionID string) {
	if _, ok := sc.SessionCache[sessionID]; ok {
		return
	}

	// 确定数据容器类型
	dct := sc.dataContainerType
	if scopeMeta, ok := sc.MetaMap[sessionScope]; ok {
		if sm := scopeMeta.GetSession(sessionID); sm != nil && sm.DataContainerType != "" {
			dct = sm.DataContainerType
		}
	}

	// 创建数据容器
	container, err := GetFactory().Load(dct, sc.AgentID, sessionID, nil)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("action", "load_session").
			Str("session_id", sessionID).
			Err(err).
			Msg("加载数据容器失败")
		return
	}

	sessionDir := SessionPaths{}.SessionDir(sc.rootPath, sc.AgentID, sessionID)
	session := NewChainSession(sc.AgentID, sessionScope, sessionID, container, sessionDir)
	if err := session.Load(); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("action", "load_session").
			Str("session_id", sessionID).
			Err(err).
			Msg("加载会话失败")
		return
	}

	sc.SessionCache[sessionID] = session

	logger.Debug(logger.ComponentAgentCore).
		Str("action", "load_session").
		Str("session_id", sessionID).
		Str("container_type", dct).
		Msg("加载会话到缓存")
}

// writeMetaFile 写入元数据文件
func (sc *SessionController) writeMetaFile() error {
	metaData := make(map[string]any)
	for sessionScope, scopeMeta := range sc.MetaMap {
		scopeKey := SessionScopeKey{AgentID: sc.AgentID, SessionScope: sessionScope}
		metaData[scopeKey.String()] = scopeMeta
	}

	metaFile := SessionPaths{}.MetaFile(sc.rootPath, sc.AgentID)
	os.MkdirAll(filepath.Dir(metaFile), 0o755)

	data, err := json.MarshalIndent(metaData, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaFile, data, 0o644)
}
