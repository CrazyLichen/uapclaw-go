package spawn

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sharedLogComponent 日志组件
	sharedLogComponent = logger.ComponentChannel
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sharedRuntime 进程级 TeamRuntime 单例
	// ⤵️ 预留：TeamRuntime（9.85）实现后回填类型
	sharedRuntime any
	// sharedMemoryDB 进程级 InMemoryTeamDatabase 单例
	// ⤵️ 预留：TeamDatabase（9.64）实现后回填类型
	sharedMemoryDB any
	// sharedDBInstances 按 db_type::connection_string 索引的 TeamDatabase 实例
	// ⤵️ 预留：TeamDatabase（9.64）实现后回填类型
	sharedDBInstances = make(map[string]any)
	// resourcesMu 共享资源读写锁
	resourcesMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSharedRuntime 返回进程级 TeamRuntime 单例，首次调用时创建。
// 对齐 Python: get_shared_runtime()
// ⤵️ 预留：TeamRuntime（9.85）实现后回填
func GetSharedRuntime() any {
	resourcesMu.Lock()
	defer resourcesMu.Unlock()

	if sharedRuntime == nil {
		logger.Info(sharedLogComponent).Msg("创建共享 TeamRuntime 单例（TODO #9.85）")
		// TODO(#9.85): sharedRuntime = NewTeamRuntime()
	}
	return sharedRuntime
}

// GetSharedDB 返回进程级数据库实例。
// 对齐 Python: get_shared_db(config)
//
// db_type == "memory" → 全局唯一 InMemoryTeamDatabase 单例。
// db_type != "memory" → 按 db_type::connection_string 去重。
// ⤵️ 预留：TeamDatabase（9.64）实现后回填
func GetSharedDB(config any) any {
	resourcesMu.Lock()
	defer resourcesMu.Unlock()

	// TODO(#9.64): 解析 config.db_type
	// if dbType == "memory" { return _getSharedMemoryDB() }
	// return _getSharedDBInstance(config)

	logger.Debug(sharedLogComponent).Msg("GetSharedDB 当前返回 nil（TODO #9.64）")
	return nil
}

// CleanupSharedResources 重置所有进程级全局单例。
// 对齐 Python: cleanup_shared_resources()
// 用于测试间重置。
func CleanupSharedResources() {
	resourcesMu.Lock()
	defer resourcesMu.Unlock()

	sharedRuntime = nil
	sharedMemoryDB = nil
	sharedDBInstances = make(map[string]any)

	// ⤵️ 预留：Messager（9.65）实现后回填
	// cleanupInprocessBus()

	logger.Debug(sharedLogComponent).Msg("已清理共享资源")
}
