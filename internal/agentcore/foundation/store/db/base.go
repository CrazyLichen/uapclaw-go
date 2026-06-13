package db

import (
	"context"

	"gorm.io/gorm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseDbStore SQL 数据库抽象接口，提供数据库引擎访问。
//
// 本接口是数据库连接的依赖注入点，让上层组件（SqlDbStore、SqlMessageStore 等）
// 通过接口获取数据库连接，而非直接依赖具体引擎。
// BaseDbStore 与 BaseKVStore、BaseVectorStore 是平级关系，互不包含：
//   - BaseKVStore 负责键值对存储
//   - BaseVectorStore 负责向量存储与检索
//   - BaseDbStore 负责提供数据库连接
//
// 对应 Python: openjiuwen/core/foundation/store/base_db_store.py (BaseDbStore)
type BaseDbStore interface {
	// GetDB 返回 GORM 数据库实例，调用者可使用返回值执行数据库操作。
	//
	// 对应 Python: BaseDbStore.get_async_engine() -> AsyncEngine
	GetDB(ctx context.Context) *gorm.DB
}
