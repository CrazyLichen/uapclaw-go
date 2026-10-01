package registry

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// PoolAccessor 运行时池访问接口。
// 打破 agent ↔ runtime 循环依赖：agent 和 runtime 都依赖 registry，互不 import。
// Python 对应：TeamRuntimeManager.pool 属性
type PoolAccessor interface {
	// Pool 返回运行时池
	Pool() PoolEntry
}

// PoolEntry 运行时池条目访问接口。
// Python 对应：TeamRuntimePool
type PoolEntry interface {
	// GetEntry 获取指定团队的活跃条目（可能为 nil）
	GetEntry(teamName string) PoolTeamEntry
	// RemoveEntry 移除指定团队的活跃条目
	RemoveEntry(teamName string)
}

// PoolTeamEntry 池中团队条目。
// Python 对应：ActiveTeam
//
// 方法名用 GetSessionID 而非 SessionID，避免与 ActiveTeam.SessionID 字段冲突。
type PoolTeamEntry interface {
	// GetSessionID 返回当前绑定的 session ID
	GetSessionID() string
}

// ──────────────────────────── 非导出函数 ────────────────────────────
