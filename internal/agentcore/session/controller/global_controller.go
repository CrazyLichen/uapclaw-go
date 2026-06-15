package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GlobalSessionConfig 全局会话控制器配置
type GlobalSessionConfig struct {
	// BasePath 所有 Agent 数据存储的根目录
	BasePath string
}

// GlobalSessionController 全局会话控制器（sync.Once 单例）。
// 作为系统统一入口，管理所有 Agent SessionController 实例，
// 提供跨 Agent 批量异步加载/刷盘操作。
// 对应 Python: openjiuwen/core/session/session_controller/global_controller.py (GlobalSessionController)
type GlobalSessionController struct {
	mu sync.Mutex
	// BasePath 存储根目录
	BasePath string
	// Controllers Agent 控制器映射：agentID → SessionController
	Controllers map[string]*SessionController
	// dataContainerType 数据容器类型
	dataContainerType string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	globalController     *GlobalSessionController
	globalControllerOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetGlobalSessionController 获取全局会话控制器单例
func GetGlobalSessionController() *GlobalSessionController {
	globalControllerOnce.Do(func() {
		globalController = &GlobalSessionController{
			BasePath:    "./agents",
			Controllers: make(map[string]*SessionController),
		}
	})
	return globalController
}

// SetConfig 设置全局会话控制器配置
func (g *GlobalSessionController) SetConfig(config GlobalSessionConfig) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.BasePath = config.BasePath
}

// LoadAgent 加载指定 Agent 的会话数据
func (g *GlobalSessionController) LoadAgent(agentID string, loadActiveOnly bool) error {
	g.mu.Lock()
	controller := g.getOrCreateController(agentID)
	g.mu.Unlock()
	return controller.Load(loadActiveOnly)
}

// LoadScope 加载指定作用域的会话数据（跨所有 Agent）
func (g *GlobalSessionController) LoadScope(sessionScope SessionScope, loadActiveOnly bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, controller := range g.Controllers {
		if err := controller.LoadScope(sessionScope, loadActiveOnly); err != nil {
			return err
		}
	}
	return nil
}

// LoadAll 加载所有已注册 Agent 的会话数据
func (g *GlobalSessionController) LoadAll(loadActiveOnly bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if info, err := os.Stat(g.BasePath); err == nil && info.IsDir() {
		entries, err := os.ReadDir(g.BasePath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				controller := g.getOrCreateController(entry.Name())
				if err := controller.Load(loadActiveOnly); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// FlushAgent 刷盘指定 Agent 的会话数据
func (g *GlobalSessionController) FlushAgent(agentID string) error {
	g.mu.Lock()
	controller, ok := g.Controllers[agentID]
	g.mu.Unlock()

	if !ok {
		logger.Warn(logger.ComponentAgentCore).
			Str("action", "flush_agent").
			Str("agent_id", agentID).
			Msg("Agent 未找到，跳过刷盘")
		return nil
	}
	return controller.Flush()
}

// FlushSession 刷盘指定会话数据
func (g *GlobalSessionController) FlushSession(sessionID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, controller := range g.Controllers {
		if _, ok := controller.SessionCache[sessionID]; ok {
			return controller.FlushSession(sessionID)
		}
	}
	return nil
}

// FlushScope 刷盘指定作用域的会话数据（跨所有 Agent）
func (g *GlobalSessionController) FlushScope(sessionScope SessionScope) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, controller := range g.Controllers {
		if err := controller.FlushScope(sessionScope); err != nil {
			return err
		}
	}
	return nil
}

// FlushAll 刷盘所有已注册 Agent 的会话数据
func (g *GlobalSessionController) FlushAll() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, controller := range g.Controllers {
		if err := controller.Flush(); err != nil {
			return err
		}
	}
	return nil
}

// GetAgent 获取指定 Agent 的会话控制器（不执行加载）
func (g *GlobalSessionController) GetAgent(agentID string) *SessionController {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.Controllers[agentID]
}

// CreateIfNotExistsAgent 获取或创建 Agent 的会话控制器
func (g *GlobalSessionController) CreateIfNotExistsAgent(agentID string) (bool, *SessionController, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if controller, ok := g.Controllers[agentID]; ok {
		return false, controller, nil
	}

	if err := g.ensureBasePath(); err != nil {
		return false, nil, err
	}
	controller := NewSessionController(agentID, g.BasePath, g.dataContainerType)
	if err := controller.Load(true); err != nil {
		return false, nil, err
	}
	g.Controllers[agentID] = controller
	return true, controller, nil
}

// RemoveAgent 删除指定 Agent 的所有数据
func (g *GlobalSessionController) RemoveAgent(agentID string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	controller, ok := g.Controllers[agentID]
	if !ok {
		return false, nil
	}

	controller.RemoveAll()
	delete(g.Controllers, agentID)

	agentDir := SessionPaths{}.AgentDir(g.BasePath, agentID)
	if err := os.RemoveAll(agentDir); err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("action", "remove_agent_dir").
			Str("agent_id", agentID).
			Str("path", agentDir).
			Err(err).
			Msg("删除 Agent 目录失败")
	}

	return true, nil
}

// RemoveAll 删除所有 Agent 的会话数据
func (g *GlobalSessionController) RemoveAll() {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, controller := range g.Controllers {
		controller.RemoveAll()
	}
	g.Controllers = make(map[string]*SessionController)
	if err := os.RemoveAll(g.BasePath); err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("action", "remove_all_dirs").
			Str("path", g.BasePath).
			Err(err).
			Msg("删除存储根目录失败")
	}
}

// CleanupAgentInactiveSessions 清理指定 Agent 的非活跃会话
func (g *GlobalSessionController) CleanupAgentInactiveSessions(agentID string) (map[string][]CleanupResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	controller, ok := g.Controllers[agentID]
	if !ok {
		return nil, fmt.Errorf("agent %q 未找到", agentID)
	}

	cleanedSessions := make(map[string][]CleanupResult)
	scopeMetas := controller.ListMetas()

	for scopeKeyStr := range scopeMetas {
		sessionScope, err := ParseSessionScope(scopeKeyStr)
		if err != nil {
			continue
		}
		scopeCleaned, err := controller.CleanupScopeInactiveSessions(sessionScope)
		if err != nil {
			continue
		}
		if len(scopeCleaned) > 0 {
			cleanedSessions[agentID] = append(cleanedSessions[agentID], scopeCleaned...)
		}
	}

	return cleanedSessions, nil
}

// CleanupScopeInactiveSessions 清理指定作用域下跨所有 Agent 的非活跃会话
func (g *GlobalSessionController) CleanupScopeInactiveSessions(sessionScope SessionScope) map[string][]SessionMeta {
	g.mu.Lock()
	defer g.mu.Unlock()

	cleanedSessions := make(map[string][]SessionMeta)
	for agentID, controller := range g.Controllers {
		scopeMetas := controller.ListMetas()
		if _, ok := scopeMetas[sessionScope.String()]; ok {
			results, err := controller.CleanupScopeInactiveSessions(sessionScope)
			if err != nil {
				continue
			}
			for _, result := range results {
				cleanedSessions[agentID] = append(cleanedSessions[agentID], result.Sessions...)
			}
		}
	}
	return cleanedSessions
}

// CleanupOrphanFiles 扫描并清理孤立目录（磁盘上不在 sessions.json 中的会话目录）
func (g *GlobalSessionController) CleanupOrphanFiles(agentID string, dryRun bool) map[string][]string {
	g.mu.Lock()
	defer g.mu.Unlock()

	result := make(map[string][]string)

	agentsToProcess := []string{}
	if agentID != "" {
		if _, ok := g.Controllers[agentID]; ok {
			agentsToProcess = append(agentsToProcess, agentID)
		} else {
			agentDir := SessionPaths{}.AgentDir(g.BasePath, agentID)
			if info, err := os.Stat(agentDir); err == nil && info.IsDir() {
				agentsToProcess = append(agentsToProcess, agentID)
			}
		}
	} else {
		if info, err := os.Stat(g.BasePath); err == nil && info.IsDir() {
			entries, _ := os.ReadDir(g.BasePath)
			for _, e := range entries {
				if e.IsDir() {
					agentsToProcess = append(agentsToProcess, e.Name())
				}
			}
		}
	}

	for _, currentAgentID := range agentsToProcess {
		sessionsDir := SessionPaths{}.SessionsDir(g.BasePath, currentAgentID)
		if info, err := os.Stat(sessionsDir); err != nil || !info.IsDir() {
			continue
		}

		// 读取 sessions.json 获取已注册的 sessionID
		registeredSessions := make(map[string]bool)
		metaFile := SessionPaths{}.MetaFile(g.BasePath, currentAgentID)
		if data, err := os.ReadFile(metaFile); err == nil {
			var metaData map[string]any
			if json.Unmarshal(data, &metaData) == nil {
				for _, scopeMetaData := range metaData {
					if sm, ok := scopeMetaData.(map[string]any); ok {
						if sessions, ok := sm["sessions"].([]any); ok {
							for _, s := range sessions {
								if sessionData, ok := s.(map[string]any); ok {
									if sid, ok := sessionData["session_id"].(string); ok {
										registeredSessions[sid] = true
									}
								}
							}
						}
					}
				}
			}
		}

		// 扫描 sessions 目录下的子目录
		entries, _ := os.ReadDir(sessionsDir)
		var orphanDirs []string
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == "downstreams" {
				continue
			}
			stateFile := filepath.Join(sessionsDir, entry.Name(), "state.data")
			if _, err := os.Stat(stateFile); err == nil {
				if !registeredSessions[entry.Name()] {
					orphanDirs = append(orphanDirs, entry.Name())
				}
			}
		}

		if len(orphanDirs) > 0 {
			result[currentAgentID] = orphanDirs

			if !dryRun {
				for _, dirName := range orphanDirs {
					orphanDir := SessionPaths{}.SessionDir(g.BasePath, currentAgentID, dirName)
					if err := os.RemoveAll(orphanDir); err != nil {
						logger.Warn(logger.ComponentAgentCore).
							Str("action", "cleanup_orphan_dir").
							Str("agent_id", currentAgentID).
							Str("path", orphanDir).
							Err(err).
							Msg("删除孤立目录失败")
					}
				}
				logger.Info(logger.ComponentAgentCore).
					Str("action", "cleanup_orphan_files").
					Str("agent_id", currentAgentID).
					Int("deleted", len(orphanDirs)).
					Msg("删除孤立目录")
			}
		}
	}

	return result
}

// CreateDirectSession 便捷方法：创建私聊会话
func CreateDirectSession(agentID, userID, sessionID string, opts ...ContainerOption) (bool, *ChainSession, error) {
	instance := GetGlobalSessionController()
	sessionScope := SessionScopeFactory{}.CreateDirect(userID)
	_, controller, err := instance.CreateIfNotExistsAgent(agentID)
	if err != nil {
		return false, nil, err
	}
	return controller.CreateIfNotExists(sessionScope, sessionID, opts...)
}

// CreateGroupSession 便捷方法：创建群聊会话
func CreateGroupSession(agentID, groupID, sessionID string, opts ...ContainerOption) (bool, *ChainSession, error) {
	instance := GetGlobalSessionController()
	sessionScope := SessionScopeFactory{}.CreateGroup(groupID)
	_, controller, err := instance.CreateIfNotExistsAgent(agentID)
	if err != nil {
		return false, nil, err
	}
	return controller.CreateIfNotExists(sessionScope, sessionID, opts...)
}

// GetDirectSessionData 便捷方法：获取私聊会话数据
func GetDirectSessionData(agentID, userID string) map[string]any {
	instance := GetGlobalSessionController()
	controller := instance.GetAgent(agentID)
	if controller == nil {
		return nil
	}
	sessionScope := SessionScopeFactory{}.CreateDirect(userID)
	session := controller.GetScopeActiveSession(sessionScope)
	if session == nil {
		return nil
	}
	return session.GetData()
}

// UpdateDirectSessionData 便捷方法：更新私聊会话数据
func UpdateDirectSessionData(agentID, userID string, data map[string]any) bool {
	instance := GetGlobalSessionController()
	controller := instance.GetAgent(agentID)
	if controller == nil {
		return false
	}
	sessionScope := SessionScopeFactory{}.CreateDirect(userID)
	session := controller.GetScopeActiveSession(sessionScope)
	if session == nil {
		return false
	}
	return session.UpdateData(data)
}

// AddDirectSessionDownstream 便捷方法：添加私聊会话下游关系
func AddDirectSessionDownstream(callerAgentID, callerUserID, targetAgentID, targetUserID string, policy SharingPolicy) bool {
	instance := GetGlobalSessionController()

	callerController := instance.GetAgent(callerAgentID)
	if callerController == nil {
		return false
	}
	targetController := instance.GetAgent(targetAgentID)
	if targetController == nil {
		return false
	}

	callerScope := SessionScopeFactory{}.CreateDirect(callerUserID)
	targetScope := SessionScopeFactory{}.CreateDirect(targetUserID)

	callerSession := callerController.GetScopeActiveSession(callerScope)
	if callerSession == nil {
		return false
	}
	targetSession := targetController.GetScopeActiveSession(targetScope)
	if targetSession == nil {
		return false
	}

	callerSession.AddDownstream(targetAgentID, targetSession.SessionID, policy)
	if err := callerSession.Flush(); err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("action", "add_downstream_flush").
			Str("caller_agent_id", callerAgentID).
			Str("session_id", callerSession.SessionID).
			Err(err).
			Msg("刷盘调用方会话失败")
	}
	return true
}

// CleanupUserSessions 便捷方法：清理用户的所有非活跃会话
func CleanupUserSessions(agentID, userID string) ([]CleanupResult, error) {
	instance := GetGlobalSessionController()
	sessionScope := SessionScopeFactory{}.CreateDirect(userID)
	controller := instance.GetAgent(agentID)
	if controller == nil {
		return nil, nil
	}
	return controller.CleanupScopeInactiveSessions(sessionScope)
}

// GetUserSessionHistory 便捷方法：获取用户会话历史
func GetUserSessionHistory(agentID, userID string) []*ChainSession {
	instance := GetGlobalSessionController()
	sessionScope := SessionScopeFactory{}.CreateDirect(userID)
	controller := instance.GetAgent(agentID)
	if controller == nil {
		return nil
	}
	return controller.GetScopeSessions(sessionScope)
}

// FlushUserSession 便捷方法：刷盘用户活跃会话数据
func FlushUserSession(agentID, userID string) error {
	instance := GetGlobalSessionController()
	sessionScope := SessionScopeFactory{}.CreateDirect(userID)
	controller := instance.GetAgent(agentID)
	if controller == nil {
		return fmt.Errorf("agent %q 未找到", agentID)
	}
	return controller.FlushScope(sessionScope)
}

// VisualizeCallChain 生成调用链可视化文本
func VisualizeCallChain(agentID, sessionID string, depth int) string {
	instance := GetGlobalSessionController()
	controller := instance.GetAgent(agentID)
	if controller == nil {
		return fmt.Sprintf("Agent %q 未找到", agentID)
	}

	session, ok := controller.SessionCache[sessionID]
	if !ok {
		return fmt.Sprintf("会话 %q 未在 Agent %q 缓存中找到", sessionID, agentID)
	}

	var lines []string
	lines = append(lines, "ChainSession 调用链可视化")
	lines = append(lines, strings.Repeat("=", 50))
	scopeKey := SessionScopeKey{AgentID: agentID, SessionScope: session.SessionScope}
	status := "Inactive"
	if session.IsActive() {
		status = "Active"
	}
	lines = append(lines, fmt.Sprintf("当前会话: %s [%s]", scopeKey.String(), truncateID(sessionID)))
	lines = append(lines, fmt.Sprintf("状态: %s", status))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("调用链关系 (深度: %d):", depth))
	lines = append(lines, strings.Repeat("-", 50))

	var buildTree func(s *ChainSession, prefix string, currentDepth int)
	buildTree = func(s *ChainSession, prefix string, currentDepth int) {
		if currentDepth > depth {
			return
		}
		downstreams := s.GetDownstreams()
		i := 0
		for key, policy := range downstreams {
			connector := "├─►"
			if currentDepth >= depth || i == len(downstreams)-1 {
				connector = "└─►"
			}
			lines = append(lines, fmt.Sprintf("%s%s %s [%s]", prefix, connector, key[0], truncateID(key[1])))
			lines = append(lines, fmt.Sprintf("%s│   ├─ 权限: %v", prefix, policy.Permission))
			if policy.FieldScopes != nil {
				lines = append(lines, fmt.Sprintf("%s│   ├─ 字段范围: %v", prefix, policy.FieldScopes))
			} else {
				lines = append(lines, fmt.Sprintf("%s│   ├─ 字段范围: 全部字段", prefix))
			}
			targetCtrl := instance.GetAgent(key[0])
			if targetCtrl != nil {
				if targetSession, ok := targetCtrl.SessionCache[key[1]]; ok {
					buildTree(targetSession, prefix+"│   ", currentDepth+1)
				} else {
					lines = append(lines, fmt.Sprintf("%s│   └─ (未加载)", prefix))
				}
			}
			i++
		}
	}

	buildTree(session, "", 1)
	return strings.Join(lines, "\n")
}

// onAgentSessionCreated AGENT_SESSION_CREATED 回调：
// 将 ChainSession 的 DataContainer.session 注入真实 Session 实例
func onAgentSessionCreated(ctx context.Context, data *callback.SessionCallEventData) any {
	if data.SessionID == "" || data.Card == nil || data.Session == nil {
		return nil
	}

	// 从 Card 提取 AgentID
	card, ok := data.Card.(*schema.AgentCard)
	if !ok {
		return nil
	}

	instance := GetGlobalSessionController()
	controller := instance.GetAgent(card.ID)
	if controller == nil {
		return nil
	}

	session, ok := controller.SessionCache[data.SessionID]
	if !ok {
		return nil
	}

	// 将 DataContainer 转为 AgentSessionContainer，注入 StateAccessor
	if asc, ok := session.DataContainer.(*AgentSessionContainer); ok {
		if sa, ok := data.Session.(StateAccessor); ok {
			asc.SetSession(sa)
			logger.Debug(logger.ComponentAgentCore).
				Str("action", "on_session_created").
				Str("agent_id", card.ID).
				Str("session_id", data.SessionID).
				Msg("注入 StateAccessor 到 AgentSessionContainer")
		}
	}
	return nil
}

// ⤵️ 5.13+ 回填：等 AgentTeamEvents 定义后注册 P2P/PubSub 回调
// callback.GetCallbackFramework().OnTeamEvent(callback.AgentP2PReceived, onAgentP2PReceived)
// callback.GetCallbackFramework().OnTeamEvent(callback.AgentPubsubReceived, onAgentPubsubReceived)

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureBasePath 确保存储根目录存在
func (g *GlobalSessionController) ensureBasePath() error {
	if g.BasePath != "" {
		if err := os.MkdirAll(g.BasePath, 0o755); err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("action", "ensure_base_path").
				Str("path", g.BasePath).
				Err(err).
				Msg("创建存储根目录失败")
			return err
		}
	}
	return nil
}

// truncateID 截断 ID 用于显示，最多 8 个字符
func truncateID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8] + "..."
}

// getOrCreateController 获取或创建 Agent 的会话控制器（调用方须持锁）
func (g *GlobalSessionController) getOrCreateController(agentID string) *SessionController {
	if controller, ok := g.Controllers[agentID]; ok {
		return controller
	}
	if err := g.ensureBasePath(); err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("action", "get_or_create_controller").
			Str("agent_id", agentID).
			Err(err).
			Msg("创建存储根目录失败，继续使用空路径")
	}
	controller := NewSessionController(agentID, g.BasePath, g.dataContainerType)
	g.Controllers[agentID] = controller
	return controller
}

// init 注册 AGENT_SESSION_CREATED 回调
func init() {
	callback.GetCallbackFramework().OnSession(callback.AgentSessionCreated, onAgentSessionCreated)
}
