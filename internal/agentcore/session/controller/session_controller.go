package controller

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sync/errgroup"

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
	// MetaMap 元数据映射：SessionScope.String() → ScopeSessionsMeta
	MetaMap map[string]*ScopeSessionsMeta
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

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
		MetaMap:           make(map[string]*ScopeSessionsMeta),
	}
	if err := os.MkdirAll(sc.BasePath, 0o755); err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("action", "new_session_controller").
			Str("agent_id", agentID).
			Str("base_path", sc.BasePath).
			Err(err).
			Msg("创建会话目录失败")
	}
	return sc
}

// Flush 持久化所有变更到磁盘。
// 对齐 Python asyncio.gather：在锁内并发 Flush 所有 session，
// 每个 ChainSession 有自己的 mu 保护，并发安全。
func (sc *SessionController) Flush() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// 对齐 Python asyncio.gather：并发 Flush 所有 session
	eg := &errgroup.Group{}
	for _, s := range sc.SessionCache {
		s := s
		eg.Go(func() error {
			if err := s.Flush(); err != nil {
				logger.Error(logger.ComponentAgentCore).
					Str("action", "session_controller_flush").
					Str("agent_id", sc.AgentID).
					Str("session_id", s.SessionID).
					Err(err).
					Msg("刷写会话失败")
				return err
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	return sc.writeMetaFile()
}

// FlushSession 持久化指定会话到磁盘
// 对齐 Python：整个 flush 过程持锁
func (sc *SessionController) FlushSession(sessionID string) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	session, ok := sc.SessionCache[sessionID]
	if !ok {
		return nil
	}

	if err := session.Flush(); err != nil {
		return err
	}

	return sc.writeMetaFile()
}

// FlushScope 持久化指定作用域的会话到磁盘。
// 对齐 Python asyncio.gather：在锁内并发 Flush 匹配 scope 的 session。
// T-08 修复：scope 不存在时快速返回，对齐 Python flush_scope 的快速返回检查。
func (sc *SessionController) FlushScope(sessionScope SessionScope) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// T-08 修复：对齐 Python flush_scope 中 if session_scope not in self.meta_map: return True
	if _, ok := sc.MetaMap[sessionScope.String()]; !ok {
		return nil
	}

	// 对齐 Python asyncio.gather：并发 Flush 匹配 scope 的 session
	eg := &errgroup.Group{}
	for _, s := range sc.SessionCache {
		s := s
		if s.SessionScope.String() == sessionScope.String() {
			eg.Go(func() error {
				return s.Flush()
			})
		}
	}
	if err := eg.Wait(); err != nil {
		return err
	}

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
	sc.MetaMap = make(map[string]*ScopeSessionsMeta)

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

		sc.MetaMap[scopeKey.SessionScope.String()] = &scopeMeta

		// 加载会话数据
		if loadActiveOnly {
			if scopeMeta.ActiveSession != "" {
				if err := sc.loadSession(scopeKey.SessionScope, scopeMeta.ActiveSession); err != nil {
					logger.Warn(logger.ComponentAgentCore).
						Str("action", "session_controller_load").
						Str("session_id", scopeMeta.ActiveSession).
						Err(err).
						Msg("加载活跃会话失败，跳过")
				}
			}
		} else {
			for _, sm := range scopeMeta.Sessions {
				if err := sc.loadSession(scopeKey.SessionScope, sm.SessionID); err != nil {
					logger.Warn(logger.ComponentAgentCore).
						Str("action", "session_controller_load").
						Str("session_id", sm.SessionID).
						Err(err).
						Msg("加载会话失败，跳过")
				}
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

		sc.MetaMap[scopeKey.SessionScope.String()] = &scopeMeta

		if loadActiveOnly {
			if scopeMeta.ActiveSession != "" {
				if err := sc.loadSession(scopeKey.SessionScope, scopeMeta.ActiveSession); err != nil {
					logger.Warn(logger.ComponentAgentCore).
						Str("action", "load_scope").
						Str("session_id", scopeMeta.ActiveSession).
						Err(err).
						Msg("加载活跃会话失败，跳过")
				}
			}
		} else {
			for _, sm := range scopeMeta.Sessions {
				if err := sc.loadSession(scopeKey.SessionScope, sm.SessionID); err != nil {
					logger.Warn(logger.ComponentAgentCore).
						Str("action", "load_scope").
						Str("session_id", sm.SessionID).
						Err(err).
						Msg("加载会话失败，跳过")
				}
			}
		}
		break
	}

	return nil
}

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
	if _, ok := sc.MetaMap[sessionScope.String()]; !ok {
		scopeKey := SessionScopeKey{AgentID: sc.AgentID, SessionScope: sessionScope}
		sc.MetaMap[sessionScope.String()] = &ScopeSessionsMeta{
			SessionScopeKey: scopeKey.String(),
		}
	}
	scopeMeta := sc.MetaMap[sessionScope.String()]

	// 检查是否已有活跃会话
	activeSession := scopeMeta.GetActiveSession()
	if activeSession != nil {
		// 返回已有活跃会话
		if _, ok := sc.SessionCache[activeSession.SessionID]; !ok {
			if err := sc.loadSession(sessionScope, activeSession.SessionID); err != nil {
				return false, nil, err
			}
		}
		if s, ok := sc.SessionCache[activeSession.SessionID]; ok {
			return false, s, nil
		}
	}

	// 创建新会话
	// 创建会话目录
	sessionDir := SessionPaths{}.SessionDir(sc.rootPath, sc.AgentID, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return false, nil, fmt.Errorf("创建会话目录失败: %w", err)
	}

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
	if err := session.Flush(); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("action", "create_if_not_exists_flush").
			Str("agent_id", sc.AgentID).
			Str("session_id", sessionID).
			Err(err).
			Msg("创建会话后刷写失败")
		return false, nil, fmt.Errorf("创建会话后刷写失败: %w", err)
	}
	if err := sc.writeMetaFile(); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("action", "create_if_not_exists_write_meta").
			Str("agent_id", sc.AgentID).
			Err(err).
			Msg("创建会话后写入元数据失败")
		return false, nil, fmt.Errorf("创建会话后写入元数据失败: %w", err)
	}

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

	scopeMeta, ok := sc.MetaMap[sessionScope.String()]
	if !ok || scopeMeta.ActiveSession == "" {
		return nil
	}

	session, ok := sc.SessionCache[scopeMeta.ActiveSession]
	if !ok {
		if err := sc.loadSession(sessionScope, scopeMeta.ActiveSession); err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("action", "get_scope_active_session").
				Str("session_id", scopeMeta.ActiveSession).
				Err(err).
				Msg("加载活跃会话失败")
			return nil
		}
		session = sc.SessionCache[scopeMeta.ActiveSession]
	}
	return session
}

// GetScopeSessions 获取指定作用域下所有已加载的会话列表
func (sc *SessionController) GetScopeSessions(sessionScope SessionScope) []*ChainSession {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	scopeMeta, ok := sc.MetaMap[sessionScope.String()]
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
		for scopeKey, scopeMeta := range sc.MetaMap {
			if sm := scopeMeta.GetSession(sessionID); sm != nil {
				parsedScope, err := ParseSessionScope(scopeKey)
				if err != nil {
					continue
				}
				targetScope = parsedScope
				if err := sc.loadSession(parsedScope, sessionID); err != nil {
					logger.Warn(logger.ComponentAgentCore).
						Str("action", "activate_session").
						Str("session_id", sessionID).
						Err(err).
						Msg("加载会话失败")
				}
				session = sc.SessionCache[sessionID]
				break
			}
		}
	}

	if session == nil {
		return fmt.Errorf("会话 %q 未找到", sessionID)
	}

	// 激活会话
	if scopeMeta, ok := sc.MetaMap[targetScope.String()]; ok {
		scopeMeta.ActivateSession(sessionID)
		session.SetIsActive(true)
		if err := session.Flush(); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("action", "activate_session_flush").
				Str("agent_id", sc.AgentID).
				Str("session_id", sessionID).
				Err(err).
				Msg("激活会话后刷写失败")
			return fmt.Errorf("激活会话后刷写失败: %w", err)
		}
		if err := sc.writeMetaFile(); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("action", "activate_session_write_meta").
				Str("agent_id", sc.AgentID).
				Err(err).
				Msg("激活会话后写入元数据失败")
			return fmt.Errorf("激活会话后写入元数据失败: %w", err)
		}

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

	if scopeMeta, ok := sc.MetaMap[sessionScope.String()]; ok {
		return *scopeMeta
	}
	scopeKey := SessionScopeKey{AgentID: sc.AgentID, SessionScope: sessionScope}
	return ScopeSessionsMeta{
		SessionScopeKey: scopeKey.String(),
	}
}

// ListMetas 获取所有已知作用域的元数据映射副本
func (sc *SessionController) ListMetas() map[string]ScopeSessionsMeta {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	result := make(map[string]ScopeSessionsMeta, len(sc.MetaMap))
	for scope, meta := range sc.MetaMap {
		result[scope] = *meta
	}
	return result
}

// CleanupScopeInactiveSessions 清理指定作用域下的非活跃会话
func (sc *SessionController) CleanupScopeInactiveSessions(sessionScope SessionScope) ([]CleanupResult, error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	scopeMeta, ok := sc.MetaMap[sessionScope.String()]
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
		if err := os.RemoveAll(sessionDir); err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("action", "cleanup_inactive_remove").
				Str("agent_id", sc.AgentID).
				Str("session_id", sm.SessionID).
				Err(err).
				Msg("删除非活跃会话目录失败")
		}

		// 从元数据移除
		scopeMeta.RemoveSession(sm.SessionID)
		cleaned = append(cleaned, sm)
	}

	if len(cleaned) > 0 {
		if err := sc.writeMetaFile(); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("action", "cleanup_inactive_write_meta").
				Str("agent_id", sc.AgentID).
				Err(err).
				Msg("清理非活跃会话后写入元数据失败")
			return nil, fmt.Errorf("清理非活跃会话后写入元数据失败: %w", err)
		}
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
		for scopeKey := range sc.MetaMap {
			parsedScope, err := ParseSessionScope(scopeKey)
			if err != nil {
				continue
			}
			scopesToSearch = append(scopesToSearch, parsedScope)
		}
	}

	for _, scope := range scopesToSearch {
		scopeMeta, ok := sc.MetaMap[scope.String()]
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
		if err := os.RemoveAll(sessionDir); err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("action", "remove_session_remove").
				Str("agent_id", sc.AgentID).
				Str("session_id", sessionID).
				Err(err).
				Msg("删除会话目录失败")
		}

		// 从元数据移除
		removedMeta := scopeMeta.RemoveSession(sessionID)
		if removedMeta != nil {
			removed = append(removed, RemoveResult{SessionScope: scope, SessionMeta: *removedMeta})
		}
	}

	if len(removed) > 0 {
		if err := sc.writeMetaFile(); err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("action", "remove_session_write_meta").
				Str("agent_id", sc.AgentID).
				Err(err).
				Msg("删除会话后写入元数据失败")
		}
	}

	return removed
}

// RemoveScopeSessions 删除指定作用域下的所有会话
func (sc *SessionController) RemoveScopeSessions(sessionScope SessionScope) []SessionMeta {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	scopeMeta, ok := sc.MetaMap[sessionScope.String()]
	if !ok {
		return nil
	}

	var removed []SessionMeta
	for _, sm := range scopeMeta.Sessions {
		delete(sc.SessionCache, sm.SessionID)
		sessionDir := SessionPaths{}.SessionDir(sc.rootPath, sc.AgentID, sm.SessionID)
		if err := os.RemoveAll(sessionDir); err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("action", "remove_scope_sessions_remove").
				Str("agent_id", sc.AgentID).
				Str("session_id", sm.SessionID).
				Err(err).
				Msg("删除作用域会话目录失败")
		}
		removed = append(removed, sm)
	}

	scopeMeta.Sessions = nil
	scopeMeta.ActiveSession = ""
	delete(sc.MetaMap, sessionScope.String())

	if err := sc.writeMetaFile(); err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("action", "remove_scope_sessions_write_meta").
			Str("agent_id", sc.AgentID).
			Err(err).
			Msg("删除作用域会话后写入元数据失败")
	}

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
	sc.MetaMap = make(map[string]*ScopeSessionsMeta)
	if err := os.RemoveAll(sc.BasePath); err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("action", "remove_all").
			Str("agent_id", sc.AgentID).
			Str("base_path", sc.BasePath).
			Err(err).
			Msg("删除所有会话数据失败")
	}

	logger.Info(logger.ComponentAgentCore).
		Str("action", "remove_all").
		Str("agent_id", sc.AgentID).
		Msg("删除所有会话数据")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadSession 加载指定会话到缓存
// 返回 error 让调用方感知加载失败
func (sc *SessionController) loadSession(sessionScope SessionScope, sessionID string) error {
	if _, ok := sc.SessionCache[sessionID]; ok {
		return nil
	}

	// 确定数据容器类型
	dct := sc.dataContainerType
	if scopeMeta, ok := sc.MetaMap[sessionScope.String()]; ok {
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
		return fmt.Errorf("加载数据容器失败: %w", err)
	}

	sessionDir := SessionPaths{}.SessionDir(sc.rootPath, sc.AgentID, sessionID)
	session := NewChainSession(sc.AgentID, sessionScope, sessionID, container, sessionDir)
	if err := session.Load(); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("action", "load_session").
			Str("session_id", sessionID).
			Err(err).
			Msg("加载会话失败")
		return fmt.Errorf("加载会话失败: %w", err)
	}

	sc.SessionCache[sessionID] = session

	logger.Debug(logger.ComponentAgentCore).
		Str("action", "load_session").
		Str("session_id", sessionID).
		Str("container_type", dct).
		Msg("加载会话到缓存")
	return nil
}

// writeMetaFile 写入元数据文件
func (sc *SessionController) writeMetaFile() error {
	metaData := make(map[string]any)
	for _, scopeMeta := range sc.MetaMap {
		// 使用 ScopeSessionsMeta 中存储的 SessionScopeKey 作为 JSON 的 key
		metaData[scopeMeta.SessionScopeKey] = scopeMeta
	}

	metaFile := SessionPaths{}.MetaFile(sc.rootPath, sc.AgentID)
	if err := os.MkdirAll(filepath.Dir(metaFile), 0o755); err != nil {
		return fmt.Errorf("创建元数据目录失败: %w", err)
	}

	data, err := json.MarshalIndent(metaData, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaFile, data, 0o644)
}
