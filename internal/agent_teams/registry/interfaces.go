package registry

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// PoolAccessor 运行时池访问接口。
// 打破 agent ↔ runtime 循环依赖：agent 和 runtime 都依赖 registry，互不 import。
// runner 也依赖 registry 存储此接口类型，避免 any。
// Python 对应：TeamRuntimeManager.pool 属性
type PoolAccessor interface {
	// PoolReader 返回运行时池的最小读取+移除接口。
	// 方法名用 PoolReader 避免与 TeamRuntimeManager.Pool() *TeamRuntimePool 签名冲突。
	PoolReader() PoolReader
}

// SessionReleaser 团队会话释放接口。
// 打破 runner ↔ runtime 循环依赖：runner 通过此接口调用 TeamRuntimeManager.ReleaseSession，
// 无需 import runtime 包。
// Python 对应：TeamRuntimeManager.release_session(session_id, force)
type SessionReleaser interface {
	// ReleaseSession 释放指定 session 的动态表。
	ReleaseSession(ctx context.Context, sessionID string, force bool) error
}

// TeamSessionChecker 团队会话检查接口。
// 打破 runner ↔ runtime 循环依赖：runner 通过此接口判断是否为团队 session。
// Python 对应：TeamRuntimeManager.resolve_team_session_release_info(session_id) is not None
type TeamSessionChecker interface {
	// IsTeamSession 判断指定 session 是否为团队会话（包含持久化的团队 bucket）。
	IsTeamSession(sessionID string) bool
}

// PoolReader 运行时池最小读取+移除接口。
// agent 包通过此接口访问 TeamRuntimePool，无需 import runtime 包。
type PoolReader interface {
	// GetSessionIDForTeam 获取指定团队的 session ID（空字符串表示不存在）
	GetSessionIDForTeam(teamName string) string
	// RemoveTeam 移除指定团队的活跃条目
	RemoveTeam(teamName string)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
