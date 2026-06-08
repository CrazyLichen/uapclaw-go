package kv

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// PipelineResult Pipeline 操作的执行结果。
//
// 调用者根据 Op 字段判断应访问哪个字段：
//   - Op 为 "set"：仅检查 Err 是否为 nil
//   - Op 为 "get"：通过 Value 获取返回值（key 不存在时 Value 为 nil）
//   - Op 为 "exists"：通过 Exists 获取布尔结果
type PipelineResult struct {
	// Op 操作类型："set"、"get"、"exists"
	Op string
	// Key 操作的键
	Key string
	// Value Get 操作返回的值，仅 Op 为 "get" 时有效
	Value []byte
	// Exists Exists 操作的结果，仅 Op 为 "exists" 时有效
	Exists bool
	// Err 操作执行错误，nil 表示成功
	Err error
}

// ──────────────────────────── 接口 ────────────────────────────

// BaseKVStore 键值存储后端的抽象接口。
//
// 所有 KV 存储后端（内存、文件、数据库、Redis 等）必须实现此接口。
// 插件开发者可直接实现此接口，调用方通过直接导入和实例化使用。
//
// 对应 Python: openjiuwen/core/foundation/store/base_kv_store.py (BaseKVStore)
type BaseKVStore interface {
	// Set 存储或覆盖一个键值对。
	Set(ctx context.Context, key string, value []byte) error

	// ExclusiveSet 原子性地设置键值对，仅当 key 不存在时成功。
	// expiry 为过期秒数，0 表示不过期。
	// 返回 true 表示设置成功，false 表示 key 已存在。
	ExclusiveSet(ctx context.Context, key string, value []byte, expiry int) (bool, error)

	// Get 根据 key 获取值，key 不存在时返回 nil, nil。
	Get(ctx context.Context, key string) ([]byte, error)

	// Exists 检查 key 是否存在。
	Exists(ctx context.Context, key string) (bool, error)

	// Delete 删除指定 key，key 不存在时不执行操作。
	Delete(ctx context.Context, key string) error

	// GetByPrefix 获取所有以 prefix 开头的键值对。
	GetByPrefix(ctx context.Context, prefix string) (map[string][]byte, error)

	// DeleteByPrefix 删除所有以 prefix 开头的键值对。
	// batchSize 为每批删除的数量，0 表示一次性删除。
	DeleteByPrefix(ctx context.Context, prefix string, batchSize int) error

	// MGet 批量获取多个 key 的值。
	// 返回值与输入 keys 顺序对应，不存在的 key 对应位置为 nil。
	MGet(ctx context.Context, keys []string) ([][]byte, error)

	// BatchDelete 批量删除多个 key，返回成功删除的数量。
	// batchSize 为每批删除的数量，0 表示一次性删除。
	BatchDelete(ctx context.Context, keys []string, batchSize int) (int, error)

	// Pipeline 创建批量操作管道，用于减少网络往返。
	Pipeline(ctx context.Context) KVPipeline
}

// KVPipeline 批量操作管道接口。
//
// 用于收集多个操作后一次性提交执行，减少网络往返。
// 使用方式：
//
//	p := store.Pipeline(ctx)
//	p.Set(ctx, "k1", []byte("v1"))
//	p.Get(ctx, "k2")
//	p.Exists(ctx, "k3")
//	results, err := p.Execute(ctx)
//
// 对应 Python: openjiuwen/core/foundation/store/base_kv_store.py (BasedKVStorePipeline)
type KVPipeline interface {
	// Set 向管道中添加一个 Set 操作（仅记录，不立即执行）。
	Set(ctx context.Context, key string, value []byte) error

	// Get 向管道中添加一个 Get 操作（仅记录，不立即执行）。
	Get(ctx context.Context, key string) error

	// Exists 向管道中添加一个 Exists 操作（仅记录，不立即执行）。
	Exists(ctx context.Context, key string) error

	// Execute 提交并执行管道中的所有操作，返回各操作的结果。
	// 执行后管道被清空，可复用。
	Execute(ctx context.Context) ([]PipelineResult, error)
}
