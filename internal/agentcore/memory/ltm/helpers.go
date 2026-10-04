package ltm

import (
	"context"
	"fmt"

	db "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	vector "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateKVStore 校验 KV 存储是否已注册。
func validateKVStore(store kv.BaseKVStore) error {
	if store == nil {
		return exception.BuildError(exception.StatusMemoryRegisterStoreExecutionError,
			exception.WithParam("store_type", "kv store"),
			exception.WithMsg("kv store is required, cannot be None"),
		)
	}
	return nil
}

// validateVectorStore 校验向量存储。
// S-02: 对齐 Python isinstance + None 检查，Go 额外增加 nil 校验（接口值为 nil 时类型仍匹配）。
func validateVectorStore(store vector.BaseVectorStore) error {
	if store == nil {
		return exception.BuildError(exception.StatusMemoryRegisterStoreExecutionError,
			exception.WithParam("store_type", "vector store"),
			exception.WithMsg("vector store is required, cannot be None"),
		)
	}
	return nil
}

// validateDbStore 校验数据库存储。
// S-02: 对齐 Python isinstance + None 检查，Go 额外增加 nil 校验（接口值为 nil 时类型仍匹配）。
func validateDbStore(store db.BaseDbStore) error {
	if store == nil {
		return exception.BuildError(exception.StatusMemoryRegisterStoreExecutionError,
			exception.WithParam("store_type", "db store"),
			exception.WithMsg("db store is required, cannot be None"),
		)
	}
	return nil
}

// validateMessageStore 校验消息存储。
// S-02: 对齐 Python isinstance + None 检查，Go 额外增加 nil 校验（接口值为 nil 时类型仍匹配）。
func validateMessageStore(store db.BaseMessageStore) error {
	if store == nil {
		return exception.BuildError(exception.StatusMemoryRegisterStoreExecutionError,
			exception.WithParam("store_type", "message store"),
			exception.WithMsg("message store is required, cannot be None"),
		)
	}
	return nil
}

// acquireUserLock 获取用户级分布式锁并执行操作，自动释放锁。
// 对齐 Python: lock = DistributedLock(kv_store, f"user/{user_id}"); async with lock:
func acquireUserLock(ctx context.Context, kvStore kv.BaseKVStore, userID string, fn func(ctx context.Context) error) error {
	lock := common.NewDistributedLock(kvStore, fmt.Sprintf("user/%s", userID))
	return lock.WithLock(ctx, fn)
}
