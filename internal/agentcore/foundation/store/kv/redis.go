package kv

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RedisStore 基于 go-redis 的 Redis KV 存储实现。
//
// 支持 Standalone 和 Cluster 两种模式，通过构造函数传入的客户端类型自动检测。
// 当传入 *redis.ClusterClient 时，自动启用 Cluster 模式，
// GetByPrefix/DeleteByPrefix 会使用 ForEachMaster 遍历所有 master 节点。
//
// 对应 Python: openjiuwen/extensions/store/kv/redis_store.py
type RedisStore struct {
	// client Redis 命令接口（Standalone 和 Cluster 均满足）
	client redis.Cmdable
	// clusterClient Cluster 模式下保存的 *redis.ClusterClient 引用
	// 非 nil 时表示 Cluster 模式，用于 ForEachMaster 全节点 SCAN
	clusterClient *redis.ClusterClient
}

// redisPipeline Redis Pipeline 实现，包装 go-redis Pipeliner 为 KVPipeline 接口。
type redisPipeline struct {
	// pipe go-redis 原生 Pipeliner
	pipe redis.Pipeliner
	// ops 操作元数据，用于 Execute 时组装 PipelineResult
	ops []pipelineOp
}

// pipelineOp Pipeline 操作元数据
type pipelineOp struct {
	// op 操作类型："set"、"get"、"exists"
	op string
	// key 操作的键
	key string
	// setCmd Set 操作的命令引用（op 为 "set" 时有效）
	setCmd *redis.StatusCmd
	// getCmd Get 操作的命令引用（op 为 "get" 时有效）
	getCmd *redis.StringCmd
	// existsCmd Exists 操作的命令引用（op 为 "exists" 时有效）
	existsCmd *redis.IntCmd
}

// Option RedisStore 的函数式选项
type Option func(*RedisStore)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件，agentcore 下统一使用 ComponentAgentCore
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时校验 RedisStore 满足 BaseKVStore 接口
var _ BaseKVStore = (*RedisStore)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRedisStore 创建基于 Redis 的 KV 存储实例。
//
// client: Redis 客户端，支持 *redis.Client（Standalone）和 *redis.ClusterClient（Cluster）。
// 构造函数自动通过类型断言检测 Cluster 模式，无需手动配置。
//
// 对齐 Python: RedisStore(redis: Redis | RedisCluster)
func NewRedisStore(client redis.Cmdable, opts ...Option) *RedisStore {
	s := &RedisStore{client: client}

	// 自动检测 Cluster 模式
	if cc, ok := client.(*redis.ClusterClient); ok {
		s.clusterClient = cc
	}

	// 应用函数式选项
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// WithClusterClient 手动设置 Cluster 客户端引用。
// 当 Cmdable 接口的底层类型无法被断言为 *redis.ClusterClient 时使用
// （例如通过包装器或代理传入的 ClusterClient）。
func WithClusterClient(cc *redis.ClusterClient) Option {
	return func(s *RedisStore) {
		s.clusterClient = cc
	}
}

// Set 存储或覆盖一个键值对。
// 对齐 Python: RedisStore.set(key, value)
func (s *RedisStore) Set(ctx context.Context, key string, value []byte) error {
	err := s.client.Set(ctx, key, value, 0).Err()
	if err != nil {
		logger.Error(logComponent).
			Str("key", key).
			Err(err).
			Msg("设置 key 失败")
		return err
	}
	logger.Debug(logComponent).
		Str("key", key).
		Msg("成功设置 key")
	return nil
}

// ExclusiveSet 原子性地设置键值对，仅当 key 不存在时成功。
// expiry 为过期秒数，0 表示不过期。
// 返回 true 表示设置成功，false 表示 key 已存在。
// 对齐 Python: RedisStore.exclusive_set(key, value, expiry)
func (s *RedisStore) ExclusiveSet(ctx context.Context, key string, value []byte, expiry int) (bool, error) {
	var ttl time.Duration
	if expiry > 0 {
		ttl = time.Duration(expiry) * time.Second
	}
	ok, err := s.client.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		logger.Error(logComponent).
			Str("key", key).
			Err(err).
			Msg("排他设置 key 失败")
		return false, err
	}
	logger.Debug(logComponent).
		Str("key", key).
		Bool("result", ok).
		Int("expiry", expiry).
		Msg("排他设置 key")
	return ok, nil
}

// Get 根据 key 获取值，key 不存在时返回 nil, nil。
// 对齐 Python: RedisStore.get(key)
func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		logger.Debug(logComponent).
			Str("key", key).
			Msg("key 不存在")
		return nil, nil
	}
	if err != nil {
		logger.Error(logComponent).
			Str("key", key).
			Err(err).
			Msg("获取 key 失败")
		return nil, err
	}
	logger.Debug(logComponent).
		Str("key", key).
		Msg("成功获取 key")
	return val, nil
}

// Exists 检查 key 是否存在。
// 对齐 Python: RedisStore.exists(key)
func (s *RedisStore) Exists(ctx context.Context, key string) (bool, error) {
	n, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		logger.Error(logComponent).
			Str("key", key).
			Err(err).
			Msg("检查 key 存在失败")
		return false, err
	}
	return n > 0, nil
}

// Delete 删除指定 key。key 不存在时返回 nil（不报错），与 Python 行为一致。
// 对齐 Python: RedisStore.delete(key)
func (s *RedisStore) Delete(ctx context.Context, key string) error {
	n, err := s.client.Del(ctx, key).Result()
	if err != nil {
		logger.Error(logComponent).
			Str("key", key).
			Err(err).
			Msg("删除 key 失败")
		return err
	}
	logger.Debug(logComponent).
		Str("key", key).
		Int64("deleted", n).
		Msg("删除 key")
	return nil
}

// GetByPrefix 获取所有以 prefix 开头的键值对。
// Standalone 模式使用 SCAN 迭代；Cluster 模式使用 ForEachMaster 遍历所有 master 节点。
// 对齐 Python: RedisStore.get_by_prefix(prefix)
func (s *RedisStore) GetByPrefix(ctx context.Context, prefix string) (map[string][]byte, error) {
	if s.isCluster() {
		return s.getByPrefixCluster(ctx, prefix)
	}
	return s.getByPrefixStandalone(ctx, prefix)
}

// DeleteByPrefix 删除所有以 prefix 开头的键值对。
// batchSize 为每批删除的数量，0 或负数表示一次性删除。
// Standalone 模式使用 SCAN 迭代；Cluster 模式使用 ForEachMaster 遍历所有 master 节点。
// 对齐 Python: RedisStore.delete_by_prefix(prefix, batch_size)
func (s *RedisStore) DeleteByPrefix(ctx context.Context, prefix string, batchSize int) error {
	if s.isCluster() {
		return s.deleteByPrefixCluster(ctx, prefix, batchSize)
	}
	return s.deleteByPrefixStandalone(ctx, prefix, batchSize)
}

// MGet 批量获取多个 key 的值。
// 返回值与输入 keys 顺序对应，不存在的 key 对应位置为 nil。
// MGET 失败时（如 Cluster CROSSSLOT），回退到 Pipeline+逐个 GET。
// 对齐 Python: RedisStore.mget(keys)
func (s *RedisStore) MGet(ctx context.Context, keys []string) ([][]byte, error) {
	if len(keys) == 0 {
		return [][]byte{}, nil
	}

	// 尝试 MGET
	vals, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		// MGET 失败（可能是 Cluster CROSSSLOT 或其他原因），回退到 Pipeline+逐个 GET
		// 对齐 Python: try MGET → except → fallback to individual GETs
		logger.Warn(logComponent).
			Err(err).
			Int("key_count", len(keys)).
			Msg("MGET 失败，回退到逐个 GET")
		return s.mGetFallback(ctx, keys)
	}

	// 转换结果为 [][]byte，同时统计找到的数量
	result := make([][]byte, len(keys))
	foundCount := 0
	for i, val := range vals {
		if val == nil {
			continue
		}
		foundCount++
		switch v := val.(type) {
		case string:
			result[i] = []byte(v)
		case []byte:
			result[i] = v
		}
	}
	logger.Debug(logComponent).
		Int("key_count", len(keys)).
		Int("found_count", foundCount).
		Msg("批量获取 keys")
	return result, nil
}

// BatchDelete 批量删除多个 key，返回成功删除的数量。
// batchSize 为每批删除的数量，0 或负数表示一次性删除。
// 对齐 Python: RedisStore.batch_delete(keys, batch_size)
func (s *RedisStore) BatchDelete(ctx context.Context, keys []string, batchSize int) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	if batchSize <= 0 {
		n, err := s.client.Del(ctx, keys...).Result()
		if err != nil {
			logger.Error(logComponent).
				Int("key_count", len(keys)).
				Err(err).
				Msg("批量删除失败")
			return 0, err
		}
		logger.Debug(logComponent).
			Int("key_count", len(keys)).
			Int64("deleted", n).
			Msg("批量删除 keys")
		return int(n), nil
	}
	totalDeleted := 0
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		n, err := s.client.Del(ctx, keys[i:end]...).Result()
		if err != nil {
			logger.Error(logComponent).
				Int("batch", i/batchSize+1).
				Err(err).
				Msg("分批删除失败")
			return totalDeleted, err
		}
		totalDeleted += int(n)
		logger.Debug(logComponent).
			Int("batch", i/batchSize+1).
			Int("batch_size", end-i).
			Int64("deleted_in_batch", n).
			Msg("分批删除进度")
	}
	logger.Debug(logComponent).
		Int("key_count", len(keys)).
		Int("deleted", totalDeleted).
		Msg("分批删除 keys")
	return totalDeleted, nil
}

// Pipeline 创建批量操作管道，用于减少网络往返。
// 包装 go-redis 原生 Pipeliner 为 KVPipeline 接口。
// 对齐 Python: RedisStore.pipeline()
// Pipeline 创建批量操作管道。Pipeline 为一次性使用，Execute 后不可再次调用。
func (s *RedisStore) Pipeline(_ context.Context) KVPipeline {
	return &redisPipeline{
		pipe: s.client.Pipeline(),
		ops:  make([]pipelineOp, 0),
	}
}

// RefreshTTL 刷新指定 keys 的 TTL。
// 这是 RedisStore 独有的方法，不在 BaseKVStore 接口中。
// ttlSeconds <= 0 或 keys 为空时直接返回 nil。
// 失败时静默忽略（仅记录 Warn 日志），对齐 Python 行为。
//
// 对齐 Python: RedisStore.refresh_ttl(keys, ttl_seconds)
// RefreshTTL 批量刷新键的 TTL。
// 注意：与 GetByPrefix/DeleteByPrefix 不同，此方法不使用 ForEachMaster，
// 因为 keys 是用户显式传入的，Pipeline 会根据 key 的 hash slot 自动路由到对应节点。
func (s *RedisStore) RefreshTTL(ctx context.Context, keys []string, ttlSeconds int) error {
	if len(keys) == 0 || ttlSeconds <= 0 {
		return nil
	}
	pipe := s.client.Pipeline()
	for _, key := range keys {
		pipe.Expire(ctx, key, time.Duration(ttlSeconds)*time.Second)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "REDIS_REFRESH_TTL_ERROR").
			Int("key_count", len(keys)).
			Int("ttl_seconds", ttlSeconds).
			Err(err).
			Msg("刷新 TTL 失败，静默忽略")
		return nil
	}
	logger.Debug(logComponent).
		Int("key_count", len(keys)).
		Int("ttl_seconds", ttlSeconds).
		Msg("成功刷新 TTL")
	return nil
}

// Set 向管道中添加一个 Set 操作（仅记录，不立即执行）。
// expiry 为过期秒数，0 表示不过期。
func (p *redisPipeline) Set(ctx context.Context, key string, value []byte, expiry int) error {
	var ttl time.Duration
	if expiry > 0 {
		ttl = time.Duration(expiry) * time.Second
	}
	cmd := p.pipe.Set(ctx, key, value, ttl)
	p.ops = append(p.ops, pipelineOp{op: "set", key: key, setCmd: cmd})
	return nil
}

// Get 向管道中添加一个 Get 操作（仅记录，不立即执行）。
func (p *redisPipeline) Get(ctx context.Context, key string) error {
	cmd := p.pipe.Get(ctx, key)
	p.ops = append(p.ops, pipelineOp{op: "get", key: key, getCmd: cmd})
	return nil
}

// Exists 向管道中添加一个 Exists 操作（仅记录，不立即执行）。
func (p *redisPipeline) Exists(ctx context.Context, key string) error {
	cmd := p.pipe.Exists(ctx, key)
	p.ops = append(p.ops, pipelineOp{op: "exists", key: key, existsCmd: cmd})
	return nil
}

// Execute 提交并执行管道中的所有操作，返回各操作的结果。
// 执行后管道被清空，可复用。
func (p *redisPipeline) Execute(ctx context.Context) ([]PipelineResult, error) {
	// 拷贝操作列表
	ops := p.ops
	p.ops = p.ops[:0] // 清空允许 Pipeline 复用

	if len(ops) == 0 {
		return []PipelineResult{}, nil
	}

	// 执行所有管道命令
	_, _ = p.pipe.Exec(ctx)

	// 组装结果
	results := make([]PipelineResult, len(ops))
	for i, op := range ops {
		results[i] = PipelineResult{Op: op.op, Key: op.key}
		switch op.op {
		case "set":
			results[i].Err = op.setCmd.Err()
		case "get":
			val, err := op.getCmd.Bytes()
			if err == redis.Nil {
				results[i].Value = nil
				results[i].Err = nil
			} else if err != nil {
				results[i].Err = err
			} else {
				results[i].Value = val
			}
		case "exists":
			results[i].Exists = op.existsCmd.Val() > 0
			results[i].Err = op.existsCmd.Err()
		}
	}

	return results, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isCluster 返回是否为 Cluster 模式。
// 等价于 Python 的 self._is_cluster，但通过 clusterClient != nil 推断，无需额外字段。
func (s *RedisStore) isCluster() bool {
	return s.clusterClient != nil
}

// getByPrefixStandalone Standalone 模式下按前缀获取键值对。
func (s *RedisStore) getByPrefixStandalone(ctx context.Context, prefix string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	pattern := prefix + "*"
	var keys []string
	var cursor uint64
	for {
		batch, scanCursor, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("SCAN 失败: %w", err)
		}
		cursor = scanCursor
		keys = append(keys, batch...)
		if cursor == 0 {
			break
		}
	}
	if len(keys) == 0 {
		return result, nil
	}
	// 使用 MGet 批量获取，避免 N+1 网络往返
	vals, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	for i, val := range vals {
		if val == nil {
			continue
		}
		if str, ok := val.(string); ok {
			result[keys[i]] = []byte(str)
		}
	}
	logger.Debug(logComponent).
		Str("prefix", prefix).
		Int("count", len(result)).
		Msg("按前缀获取键值对")
	return result, nil
}

// getByPrefixCluster Cluster 模式下按前缀获取键值对，使用 ForEachMaster 遍历所有 master 节点。
func (s *RedisStore) getByPrefixCluster(ctx context.Context, prefix string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	pattern := prefix + "*"
	err := s.clusterClient.ForEachMaster(ctx, func(ctx context.Context, master *redis.Client) error {
		var cursor uint64
		for {
			keys, cursor, err := master.Scan(ctx, cursor, pattern, 100).Result()
			if err != nil {
				return err
			}
			for _, key := range keys {
				val, err := master.Get(ctx, key).Bytes()
				if err == redis.Nil {
					continue
				}
				if err != nil {
					return err
				}
				result[key] = val
			}
			if cursor == 0 {
				break
			}
		}
		return nil
	})
	if err != nil {
		logger.Error(logComponent).
			Str("prefix", prefix).
			Err(err).
			Msg("Cluster 模式按前缀获取键值对失败")
		return nil, err
	}
	logger.Debug(logComponent).
		Str("prefix", prefix).
		Int("count", len(result)).
		Msg("Cluster 模式按前缀获取键值对")
	return result, nil
}

// deleteByPrefixStandalone Standalone 模式下按前缀删除键值对。
// 流式分批：SCAN 过程中边收集边删除，避免大量 key 占用内存。
// 对齐 Python: scan_iter 边收集边删除的行为。
func (s *RedisStore) deleteByPrefixStandalone(ctx context.Context, prefix string, batchSize int) error {
	pattern := prefix + "*"
	var keys []string
	var cursor uint64
	totalDeleted := 0
	for {
		batch, scanCursor, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("SCAN 失败: %w", err)
		}
		cursor = scanCursor
		keys = append(keys, batch...)
		// 达到批次大小时立即删除，释放内存
		if batchSize > 0 && len(keys) >= batchSize {
			if err := s.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
			totalDeleted += len(keys)
			keys = keys[:0]
		}
		if cursor == 0 {
			break
		}
	}
	// 删除剩余的 keys
	if len(keys) > 0 {
		if err := s.client.Del(ctx, keys...).Err(); err != nil {
			return err
		}
		totalDeleted += len(keys)
	}
	if totalDeleted > 0 {
		logger.Debug(logComponent).
			Str("prefix", prefix).
			Int("count", totalDeleted).
			Msg("按前缀删除键值对")
	}
	return nil
}

// deleteByPrefixCluster Cluster 模式下按前缀删除键值对，使用 ForEachMaster 遍历所有 master 节点。
func (s *RedisStore) deleteByPrefixCluster(ctx context.Context, prefix string, batchSize int) error {
	pattern := prefix + "*"
	err := s.clusterClient.ForEachMaster(ctx, func(ctx context.Context, master *redis.Client) error {
		var keys []string
		var cursor uint64
		for {
			batch, cursor, err := master.Scan(ctx, cursor, pattern, 100).Result()
			if err != nil {
				return err
			}
			keys = append(keys, batch...)
			if cursor == 0 {
				break
			}
		}
		if len(keys) == 0 {
			return nil
		}
		if batchSize <= 0 {
			return master.Del(ctx, keys...).Err()
		}
		for i := 0; i < len(keys); i += batchSize {
			end := i + batchSize
			if end > len(keys) {
				end = len(keys)
			}
			if err := master.Del(ctx, keys[i:end]...).Err(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		logger.Error(logComponent).
			Str("prefix", prefix).
			Err(err).
			Msg("Cluster 模式按前缀删除失败")
		return err
	}
	logger.Debug(logComponent).
		Str("prefix", prefix).
		Msg("Cluster 模式按前缀删除键值对")
	return nil
}

// mGetFallback 使用 Pipeline 逐个 GET 的回退方案。
// 对齐 Python: MGET failed, falling back to individual GETs
func (s *RedisStore) mGetFallback(ctx context.Context, keys []string) ([][]byte, error) {
	pipe := s.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.Get(ctx, key)
	}
	_, _ = pipe.Exec(ctx) // 忽略整体错误，逐个读取结果

	result := make([][]byte, len(keys))
	for i, cmd := range cmds {
		val, err := cmd.Bytes()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			continue // 对齐 Python：单个 key 失败时对应位置为 nil
		}
		result[i] = val
	}
	return result, nil
}
