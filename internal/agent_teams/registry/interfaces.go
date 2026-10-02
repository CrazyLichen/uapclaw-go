package registry

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
