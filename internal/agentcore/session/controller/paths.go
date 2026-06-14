package controller

import "path/filepath"

// ──────────────────────────── 结构体 ────────────────────────────

// SessionPaths 会话存储路径工具，提供静态方法构建各种存储路径。
// 对应 Python: openjiuwen/core/session/session_controller/utils.py (SessionPaths)
type SessionPaths struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// AgentDir 获取指定 Agent 的根目录路径
func (SessionPaths) AgentDir(basePath, agentID string) string {
	return filepath.Join(basePath, agentID)
}

// SessionsDir 获取指定 Agent 的 sessions 目录路径
func (SessionPaths) SessionsDir(basePath, agentID string) string {
	return filepath.Join(basePath, agentID, "sessions")
}

// MetaFile 获取指定 Agent 的 sessions.json 元数据文件路径
func (SessionPaths) MetaFile(basePath, agentID string) string {
	return filepath.Join(basePath, agentID, "sessions", "sessions.json")
}

// SessionDir 获取指定会话的目录路径
func (SessionPaths) SessionDir(basePath, agentID, sessionID string) string {
	return filepath.Join(basePath, agentID, "sessions", sessionID)
}

// StateFile 获取指定会话目录下的 state.data 文件路径
func (SessionPaths) StateFile(sessionDir string) string {
	return filepath.Join(sessionDir, "state.data")
}

// DownstreamsDir 获取指定会话目录下的 downstreams 目录路径
func (SessionPaths) DownstreamsDir(sessionDir string) string {
	return filepath.Join(sessionDir, "downstreams")
}

// LinkFile 获取指定下游关系的 .link 文件路径
func (SessionPaths) LinkFile(sessionDir, targetAgent, targetSession string) string {
	return filepath.Join(sessionDir, "downstreams", targetAgent+"_"+targetSession+".link")
}
