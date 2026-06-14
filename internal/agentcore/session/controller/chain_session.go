package controller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChainSession 链式会话，持有 DataContainer + 下游关系 + 持久化能力。
// 每个实例代表一个具体的对话会话，维护与其他会话的下游调用关系以实现单向数据可见性。
// 对应 Python: openjiuwen/core/session/session_controller/chain_session.py (ChainSession)
type ChainSession struct {
	mu sync.Mutex
	// AgentID 所属 Agent 标识
	AgentID string
	// SessionScope 会话作用域
	SessionScope SessionScope
	// SessionID 会话唯一标识
	SessionID string
	// DataContainer 数据容器
	DataContainer DataContainer
	// sessionDir 会话存储目录
	sessionDir string
	// dataContainerType 数据容器类型
	dataContainerType string
	// downstreamPolicies 下游关系映射：[agentID, sessionID] → SharingPolicy
	downstreamPolicies map[[2]string]SharingPolicy
	// createdAt 创建时间戳
	createdAt float64
	// updatedAt 更新时间戳
	updatedAt float64
	// version 版本号
	version int
	// isActive 是否活跃
	isActive bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChainSession 创建链式会话实例
func NewChainSession(agentID string, sessionScope SessionScope, sessionID string, dataContainer DataContainer, sessionDir string) *ChainSession {
	return &ChainSession{
		AgentID:            agentID,
		SessionScope:       sessionScope,
		SessionID:          sessionID,
		DataContainer:      dataContainer,
		sessionDir:         sessionDir,
		dataContainerType:  DefaultDataContainerType,
		downstreamPolicies: make(map[[2]string]SharingPolicy),
		version:            1,
	}
}

// ──────────────────────────── 持久化方法 ────────────────────────────

// Load 从磁盘加载会话数据和下游关系。
// 1. 读取 state.data → 恢复 meta + data container
// 2. 扫描 downstreams/*.link → 恢复下游关系
// 3. 跳过 removed:true 的 .link 文件（崩溃恢复安全）
func (cs *ChainSession) Load() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	logger.Debug(logger.ComponentAgentCore).
		Str("action", "chain_session_load").
		Str("session_id", cs.SessionID).
		Str("session_dir", cs.sessionDir).
		Msg("加载链式会话")

	// 加载 state.data
	stateFile := SessionPaths{}.StateFile(cs.sessionDir)
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var stateData map[string]any
	if err := json.Unmarshal(data, &stateData); err != nil {
		return err
	}

	// 恢复元数据
	if meta, ok := stateData["meta"].(map[string]any); ok {
		cs.createdAt = floatVal(meta["created_at"])
		cs.updatedAt = floatVal(meta["updated_at"])
		cs.version = intVal(meta["version"])
		cs.isActive = boolVal(meta["is_active"])
		if dct, ok := meta["data_container_type"].(string); ok && dct != "" {
			cs.dataContainerType = dct
		}
	}

	// 恢复数据容器
	// ⤵️ 后续回填：AgentSessionContainer.load 委托给 StateAccessor，当前简化处理

	// 加载下游关系
	downstreamsDir := SessionPaths{}.DownstreamsDir(cs.sessionDir)
	entries, err := os.ReadDir(downstreamsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".link" {
			continue
		}

		linkPath := filepath.Join(downstreamsDir, entry.Name())
		linkData, err := os.ReadFile(linkPath)
		if err != nil {
			continue
		}

		var link map[string]any
		if err := json.Unmarshal(linkData, &link); err != nil {
			continue
		}

		// 跳过已标记删除的 link 文件
		if removed, ok := link["removed"].(bool); ok && removed {
			_ = os.Remove(linkPath)
			continue
		}

		// 从文件名解析 target 信息：{target_agent}_{target_session}.link
		filename := entry.Name()
		stem := filename[:len(filename)-5] // 去掉 .link
		idx := indexOfUnderscore(stem)
		if idx < 0 {
			continue
		}
		targetAgent := stem[:idx]
		targetSession := stem[idx+1:]

		// 解析共享策略
		policy := SharingPolicy{Permission: PermissionRead}
		if permData, ok := link["permission"].(map[string]any); ok {
			if level, ok := permData["level"].(float64); ok && level == 1 {
				policy.Permission = PermissionRead
			}
			if fieldScopes, ok := permData["field_scopes"].([]any); ok && len(fieldScopes) > 0 {
				policy.FieldScopes = make(map[string]struct{}, len(fieldScopes))
				for _, f := range fieldScopes {
					if fs, ok := f.(string); ok {
						policy.FieldScopes[fs] = struct{}{}
					}
				}
			}
		}

		cs.downstreamPolicies[[2]string{targetAgent, targetSession}] = policy

		logger.Debug(logger.ComponentAgentCore).
			Str("action", "chain_session_load_link").
			Str("session_id", cs.SessionID).
			Str("target_agent", targetAgent).
			Str("target_session", targetSession).
			Msg("加载下游关系")
	}

	logger.Info(logger.ComponentAgentCore).
		Str("action", "chain_session_loaded").
		Str("session_id", cs.SessionID).
		Int("downstreams", len(cs.downstreamPolicies)).
		Bool("active", cs.isActive).
		Msg("链式会话加载完成")

	return nil
}

// Flush 持久化当前会话状态到磁盘。
// 1. 更新 updatedAt 时间戳
// 2. 调用 DataContainer.Dump() 写入 state.data
// 3. 写/清理 downstreams/*.link 文件
func (cs *ChainSession) Flush() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.updatedAt = float64(time.Now().UnixMilli()) / 1000.0

	logger.Debug(logger.ComponentAgentCore).
		Str("action", "chain_session_flush").
		Str("session_id", cs.SessionID).
		Msg("刷写链式会话")

	// 准备数据
	stateData := map[string]any{
		"meta": map[string]any{
			"created_at":          cs.createdAt,
			"updated_at":          cs.updatedAt,
			"version":             cs.version,
			"is_active":           cs.isActive,
			"data_container_type": cs.dataContainerType,
		},
	}

	// 序列化 DataContainer
	if cs.DataContainer != nil {
		dump, err := cs.DataContainer.Dump()
		if err != nil {
			return err
		}
		stateData["data"] = dump
	} else {
		stateData["data"] = map[string]any{}
	}

	// 写入 state.data
	if err := os.MkdirAll(cs.sessionDir, 0o755); err != nil {
		return err
	}
	stateFile := SessionPaths{}.StateFile(cs.sessionDir)
	data, err := json.MarshalIndent(stateData, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(stateFile, data, 0o644); err != nil {
		return err
	}

	// 处理下游关系
	downstreamsDir := SessionPaths{}.DownstreamsDir(cs.sessionDir)
	if err := os.MkdirAll(downstreamsDir, 0o755); err != nil {
		return err
	}

	// 收集当前存在的 .link 文件
	existingLinks := make(map[string]bool)
	entries, _ := os.ReadDir(downstreamsDir)
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".link" {
			existingLinks[filepath.Join(downstreamsDir, e.Name())] = true
		}
	}

	// 写入当前下游关系
	currentLinks := make(map[string]bool)
	for key, policy := range cs.downstreamPolicies {
		linkFilename := key[0] + "_" + key[1] + ".link"
		linkPath := filepath.Join(downstreamsDir, linkFilename)
		currentLinks[linkPath] = true

		fieldScopes := make([]string, 0, len(policy.FieldScopes))
		for f := range policy.FieldScopes {
			fieldScopes = append(fieldScopes, f)
		}

		linkData := map[string]any{
			"permission": map[string]any{
				"level":        policy.Permission,
				"field_scopes": fieldScopes,
			},
			"created_at": cs.updatedAt,
		}
		linkBytes, _ := json.MarshalIndent(linkData, "", "  ")
		_ = os.WriteFile(linkPath, linkBytes, 0o644)
	}

	// 清理已删除的下游关系
	for linkPath := range existingLinks {
		if currentLinks[linkPath] {
			continue
		}
		// 先标记为 removed
		linkData, err := os.ReadFile(linkPath)
		if err == nil {
			var link map[string]any
			if json.Unmarshal(linkData, &link) == nil {
				link["removed"] = true
				if bytes, err := json.MarshalIndent(link, "", "  "); err == nil {
					_ = os.WriteFile(linkPath, bytes, 0o644)
				}
			}
		}
		_ = os.Remove(linkPath)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("action", "chain_session_flushed").
		Str("session_id", cs.SessionID).
		Int("version", cs.version).
		Int("downstreams", len(cs.downstreamPolicies)).
		Msg("链式会话刷写完成")

	return nil
}

// ──────────────────────────── 下游关系管理方法 ────────────────────────────

// AddDownstream 添加下游会话（被调用者），表示当前会话可以单向读取目标会话的数据
func (cs *ChainSession) AddDownstream(targetAgent, targetSession string, policy SharingPolicy) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.downstreamPolicies[[2]string{targetAgent, targetSession}] = policy
	cs.updatedAt = float64(time.Now().UnixMilli()) / 1000.0

	logger.Debug(logger.ComponentAgentCore).
		Str("action", "add_downstream").
		Str("session_id", cs.SessionID).
		Str("target_agent", targetAgent).
		Str("target_session", targetSession).
		Msg("添加下游关系")
}

// RemoveDownstream 移除指定的下游关系
func (cs *ChainSession) RemoveDownstream(targetAgent, targetSession string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.downstreamPolicies, [2]string{targetAgent, targetSession})
	cs.updatedAt = float64(time.Now().UnixMilli()) / 1000.0

	logger.Debug(logger.ComponentAgentCore).
		Str("action", "remove_downstream").
		Str("session_id", cs.SessionID).
		Str("target_agent", targetAgent).
		Str("target_session", targetSession).
		Msg("移除下游关系")
}

// HasDownstream 检查指定的下游关系是否存在
func (cs *ChainSession) HasDownstream(targetAgent, targetSession string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	_, ok := cs.downstreamPolicies[[2]string{targetAgent, targetSession}]
	return ok
}

// GetDownstreams 获取所有下游关系的副本
func (cs *ChainSession) GetDownstreams() map[[2]string]SharingPolicy {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	result := make(map[[2]string]SharingPolicy, len(cs.downstreamPolicies))
	for k, v := range cs.downstreamPolicies {
		result[k] = v
	}
	return result
}

// GetDownstreamPolicy 获取指定下游关系的共享策略，不存在返回 nil
func (cs *ChainSession) GetDownstreamPolicy(targetAgent, targetSession string) *SharingPolicy {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if p, ok := cs.downstreamPolicies[[2]string{targetAgent, targetSession}]; ok {
		return &p
	}
	return nil
}

// RemoveAllDownstreams 清空所有下游关系
func (cs *ChainSession) RemoveAllDownstreams() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.downstreamPolicies = make(map[[2]string]SharingPolicy)
	cs.updatedAt = float64(time.Now().UnixMilli()) / 1000.0

	logger.Debug(logger.ComponentAgentCore).
		Str("action", "remove_all_downstreams").
		Str("session_id", cs.SessionID).
		Msg("清空所有下游关系")
}

// ──────────────────────────── 数据访问方法 ────────────────────────────

// GetData 获取当前会话的完整数据
func (cs *ChainSession) GetData() map[string]any {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.DataContainer == nil {
		return nil
	}
	return cs.DataContainer.Get(nil)
}

// UpdateData 原子更新当前会话数据
func (cs *ChainSession) UpdateData(data map[string]any) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.DataContainer == nil {
		return false
	}
	success := cs.DataContainer.Update(data)
	if success {
		cs.version++
		cs.updatedAt = float64(time.Now().UnixMilli()) / 1000.0
	}
	return success
}

// CanSee 检查当前会话是否对目标会话有读权限。
// 可见性规则：1. 目标是自身 → true；2. 目标是下游会话 → true；3. 其他 → false
func (cs *ChainSession) CanSee(targetAgent, targetSession string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if targetAgent == cs.AgentID && targetSession == cs.SessionID {
		return true
	}
	_, ok := cs.downstreamPolicies[[2]string{targetAgent, targetSession}]
	return ok
}

// ──────────────────────────── 元数据方法 ────────────────────────────

// ToSessionMeta 转换为 SessionMeta 元数据对象
func (cs *ChainSession) ToSessionMeta() SessionMeta {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return SessionMeta{
		SessionID:         cs.SessionID,
		CreatedAt:         cs.createdAt,
		UpdatedAt:         cs.updatedAt,
		Version:           cs.version,
		IsActive:          cs.isActive,
		DataContainerType: cs.dataContainerType,
	}
}

// UpdateFromMeta 从 SessionMeta 更新会话信息
func (cs *ChainSession) UpdateFromMeta(meta SessionMeta) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.createdAt = meta.CreatedAt
	cs.updatedAt = meta.UpdatedAt
	cs.version = meta.Version
	cs.isActive = meta.IsActive
	if meta.DataContainerType != "" {
		cs.dataContainerType = meta.DataContainerType
	}
}

// SessionKey 获取全局唯一键
func (cs *ChainSession) SessionKey() SessionScopeKey {
	return SessionScopeKey{AgentID: cs.AgentID, SessionScope: cs.SessionScope}
}

// CreatedAt 获取创建时间戳
func (cs *ChainSession) CreatedAt() float64 {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.createdAt
}

// UpdatedAt 获取更新时间戳
func (cs *ChainSession) UpdatedAt() float64 {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.updatedAt
}

// Version 获取版本号
func (cs *ChainSession) Version() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.version
}

// IsActive 获取活跃状态
func (cs *ChainSession) IsActive() bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.isActive
}

// SetIsActive 设置活跃状态，激活时同时更新时间戳
func (cs *ChainSession) SetIsActive(value bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.isActive = value
	if value {
		cs.updatedAt = float64(time.Now().UnixMilli()) / 1000.0
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// downstreamKey 构造下游关系的 map key
func downstreamKey(agentID, sessionID string) [2]string {
	return [2]string{agentID, sessionID}
}

// floatVal 从 any 中安全提取 float64
func floatVal(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// intVal 从 any 中安全提取 int
func intVal(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

// boolVal 从 any 中安全提取 bool
func boolVal(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// indexOfUnderscore 找到字符串中第一个不在 prefix 位置的 '_' 的索引
func indexOfUnderscore(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			return i
		}
	}
	return -1
}
