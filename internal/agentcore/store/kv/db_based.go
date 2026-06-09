package kv

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KVStoreRow kv_store 表的 GORM 模型。
//
// 对应 Python: openjiuwen/core/foundation/store/kv/db_based_kv_store.py (KVStoreTable)
type KVStoreRow struct {
	// Key 键，主键
	Key string `gorm:"column:key;type:varchar(255);primaryKey"`
	// Value 值，BLOB 类型
	Value []byte `gorm:"column:value;type:blob;not null"`
}

// exclusiveValue ExclusiveSet 的 JSON 包装格式。
//
// 对应 Python: {EXCLUSIVE_VALUE_KEY: value, EXCLUSIVE_EXPIRY_KEY: expire_at}
type exclusiveValue struct {
	// ExclusiveValue Base64 编码的原始 []byte
	ExclusiveValue string `json:"exclusive_value"`
	// ExclusiveExpiry 过期时间戳（Unix 秒），0 表示不过期
	ExclusiveExpiry int64 `json:"exclusive_expiry"`
}

// DbBasedKVStore 基于 GORM 的数据库 KV 存储实现。
//
// 支持 SQLite、MySQL、PostgreSQL 三种方言，通过 GORM clause.OnConflict
// 自动处理方言差异。构造函数接受已初始化的 *gorm.DB，调用方负责配置方言和连接池。
//
// 对应 Python: openjiuwen/core/foundation/store/kv/db_based_kv_store.py (DbBasedKVStore)
type DbBasedKVStore struct {
	// db GORM 数据库实例
	db *gorm.DB
	// tableCreated 建表标志，atomic.Bool 做快速路径
	tableCreated atomic.Bool
	// tableOnce 慢路径建表，保证只执行一次
	tableOnce sync.Once
	// tableReady 建表完成信号，用于阻塞等待建表完成
	tableReady chan struct{}
}

// dbBasedPipeline 数据库 Pipeline 实现。
type dbBasedPipeline struct {
	// ops 待执行的操作列表
	ops []operation
	// store 关联的 DbBasedKVStore 实例
	store *DbBasedKVStore
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// exclusiveValueKey JSON 中值的字段名
	exclusiveValueKey = "exclusive_value"
	// exclusiveExpiryKey JSON 中过期时间的字段名
	exclusiveExpiryKey = "exclusive_expiry"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDbBasedKVStore 创建基于 GORM 的数据库 KV 存储。
//
// db: 已初始化的 GORM 数据库实例（调用方负责配置方言和连接池）。
// 对齐 Python: DbBasedKVStore(engine: AsyncEngine)
func NewDbBasedKVStore(db *gorm.DB) *DbBasedKVStore {
	return &DbBasedKVStore{
		db:         db,
		tableReady: make(chan struct{}),
	}
}

// Set 存储或覆盖一个键值对。
// 对齐 Python: DbBasedKVStore.set(key, value)
func (s *DbBasedKVStore) Set(ctx context.Context, key string, value []byte) error {
	s.ensureTable()
	row := &KVStoreRow{
		Key:   key,
		Value: value,
	}
	return upsertStatement(s.db.WithContext(ctx), row).Error
}

// Get 根据 key 获取值，key 不存在时返回 nil, nil。
// 尝试 JSON 解析，如果含 exclusive_expiry 字段则解包返回实际值。
// 对齐 Python: DbBasedKVStore.get(key) — 解包 exclusive dict
func (s *DbBasedKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.ensureTable()
	var row KVStoreRow
	err := s.db.WithContext(ctx).Where("key = ?", key).First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	// 尝试解包 exclusive JSON
	if decoded, ok := decodeExclusiveValue(row.Value); ok {
		return decoded, nil
	}
	return row.Value, nil
}

// Exists 检查 key 是否存在。
// 对齐 Python: DbBasedKVStore.exists(key)
func (s *DbBasedKVStore) Exists(ctx context.Context, key string) (bool, error) {
	s.ensureTable()
	var count int64
	err := s.db.WithContext(ctx).Model(&KVStoreRow{}).Where("key = ?", key).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Delete 删除指定 key，key 不存在时不执行操作。
// 对齐 Python: DbBasedKVStore.delete(key)
func (s *DbBasedKVStore) Delete(ctx context.Context, key string) error {
	s.ensureTable()
	return s.db.WithContext(ctx).Where("key = ?", key).Delete(&KVStoreRow{}).Error
}

// ExclusiveSet 原子性地设置键值对，仅当 key 不存在或已过期时成功。
// expiry 为过期秒数，0 表示不过期。
// 返回 true 表示设置成功，false 表示 key 已存在且未过期。
// 依赖数据库主键唯一性保证原子性，不加应用层锁。
// 对齐 Python: DbBasedKVStore.exclusive_set(key, value, expiry)
func (s *DbBasedKVStore) ExclusiveSet(ctx context.Context, key string, value []byte, expiry int) (bool, error) {
	s.ensureTable()
	now := time.Now().Unix()

	// 查询已有记录
	var row KVStoreRow
	err := s.db.WithContext(ctx).Where("key = ?", key).First(&row).Error
	if err == nil {
		// key 已存在，检查是否过期
		var ev exclusiveValue
		if err := json.Unmarshal(row.Value, &ev); err != nil {
			// 无法解析为 exclusive JSON，视为未过期
			return false, nil
		}
		if ev.ExclusiveExpiry == 0 || ev.ExclusiveExpiry > now {
			// 未过期：ExpiryAt==0 表示永不过期；ExpiryAt > now 表示尚未到期
			return false, nil
		}
		// 已过期，允许覆盖（继续执行写入）
	} else if err != gorm.ErrRecordNotFound {
		return false, err
	}

	// 计算过期时间戳
	var expireAt int64
	if expiry > 0 {
		expireAt = now + int64(expiry)
	}

	// 编码为 exclusive JSON
	encodedValue, err := encodeExclusiveValue(value, expireAt)
	if err != nil {
		return false, fmt.Errorf("编码 exclusive 值失败: %w", err)
	}

	// upsert 写入
	newRow := &KVStoreRow{
		Key:   key,
		Value: encodedValue,
	}
	if err := upsertStatement(s.db.WithContext(ctx), newRow).Error; err != nil {
		return false, err
	}
	return true, nil
}

// GetByPrefix 获取所有以 prefix 开头的键值对。
// 对齐 Python: DbBasedKVStore.get_by_prefix(prefix) — 解码 _decode_value
func (s *DbBasedKVStore) GetByPrefix(ctx context.Context, prefix string) (map[string][]byte, error) {
	s.ensureTable()
	var rows []KVStoreRow
	err := s.db.WithContext(ctx).Where("key LIKE ?", prefix+"%").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string][]byte, len(rows))
	for _, row := range rows {
		// 尝试解包 exclusive JSON，否则返回原始 BLOB
		if decoded, ok := decodeExclusiveValue(row.Value); ok {
			result[row.Key] = decoded
		} else {
			result[row.Key] = row.Value
		}
	}
	return result, nil
}

// DeleteByPrefix 删除所有以 prefix 开头的键值对。
// batchSize 为每批删除的数量，0 表示一次性删除。
// 对齐 Python: DbBasedKVStore.delete_by_prefix(prefix, batch_size)
func (s *DbBasedKVStore) DeleteByPrefix(ctx context.Context, prefix string, batchSize int) error {
	s.ensureTable()
	db := s.db.WithContext(ctx)

	if batchSize <= 0 {
		return db.Where("key LIKE ?", prefix+"%").Delete(&KVStoreRow{}).Error
	}

	// 分批删除：先查询匹配的 key，再按批次删除
	var keys []string
	if err := db.Model(&KVStoreRow{}).Where("key LIKE ?", prefix+"%").Pluck("key", &keys).Error; err != nil {
		return err
	}

	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		if err := db.Where("key IN ?", keys[i:end]).Delete(&KVStoreRow{}).Error; err != nil {
			return err
		}
	}
	return nil
}

// MGet 批量获取多个 key 的值。
// 返回值与输入 keys 顺序对应，不存在的 key 对应位置为 nil。
// 对齐 Python: DbBasedKVStore.mget(keys)
func (s *DbBasedKVStore) MGet(ctx context.Context, keys []string) ([][]byte, error) {
	s.ensureTable()
	if len(keys) == 0 {
		return [][]byte{}, nil
	}

	var rows []KVStoreRow
	err := s.db.WithContext(ctx).Where("key IN ?", keys).Find(&rows).Error
	if err != nil {
		return nil, err
	}

	// 构建 lookup map
	lookup := make(map[string][]byte, len(rows))
	for _, row := range rows {
		if decoded, ok := decodeExclusiveValue(row.Value); ok {
			lookup[row.Key] = decoded
		} else {
			lookup[row.Key] = row.Value
		}
	}

	// 按输入顺序组装结果
	result := make([][]byte, len(keys))
	for i, key := range keys {
		result[i] = lookup[key] // 不存在时为零值 nil
	}
	return result, nil
}

// BatchDelete 批量删除多个 key，返回成功删除的数量。
// batchSize 为每批删除的数量，0 表示一次性删除。
// 对齐 Python: DbBasedKVStore.batch_delete(keys, batch_size)
func (s *DbBasedKVStore) BatchDelete(ctx context.Context, keys []string, batchSize int) (int, error) {
	s.ensureTable()
	if len(keys) == 0 {
		return 0, nil
	}

	db := s.db.WithContext(ctx)

	if batchSize <= 0 {
		result := db.Where("key IN ?", keys).Delete(&KVStoreRow{})
		if result.Error != nil {
			return 0, result.Error
		}
		return int(result.RowsAffected), nil
	}

	// 分批删除
	totalDeleted := 0
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		result := db.Where("key IN ?", keys[i:end]).Delete(&KVStoreRow{})
		if result.Error != nil {
			return totalDeleted, result.Error
		}
		totalDeleted += int(result.RowsAffected)
	}
	return totalDeleted, nil
}

// Pipeline 创建批量操作管道。
// 对齐 Python: DbBasedKVStore.pipeline()
func (s *DbBasedKVStore) Pipeline(_ context.Context) KVPipeline {
	return &dbBasedPipeline{
		ops:   make([]operation, 0),
		store: s,
	}
}

// Set 向管道中添加一个 Set 操作（仅记录，不立即执行）。
// expiry 为过期秒数，0 表示不过期。
func (p *dbBasedPipeline) Set(_ context.Context, key string, value []byte, expiry int) error {
	p.ops = append(p.ops, operation{op: "set", key: key, value: value, expiry: expiry})
	return nil
}

// Get 向管道中添加一个 Get 操作（仅记录，不立即执行）。
func (p *dbBasedPipeline) Get(_ context.Context, key string) error {
	p.ops = append(p.ops, operation{op: "get", key: key})
	return nil
}

// Exists 向管道中添加一个 Exists 操作（仅记录，不立即执行）。
func (p *dbBasedPipeline) Exists(_ context.Context, key string) error {
	p.ops = append(p.ops, operation{op: "exists", key: key})
	return nil
}

// Execute 提交并执行管道中的所有操作，返回各操作的结果。
// 操作按类型分组（set/get/exists），在一个事务内批量执行。
// 执行后管道被清空，可复用。
// 对齐 Python: DbBasedKVStore.pipeline().execute()
func (p *dbBasedPipeline) Execute(ctx context.Context) ([]PipelineResult, error) {
	// 拷贝操作列表并清空，允许 Pipeline 复用
	ops := p.ops
	p.ops = nil

	if len(ops) == 0 {
		return []PipelineResult{}, nil
	}

	p.store.ensureTable()

	// 分组操作
	type setOp struct {
		key    string
		value  []byte
		expiry int
	}
	var setOps []setOp
	var getKeys []string
	var existsKeys []string

	for _, op := range ops {
		switch op.op {
		case "set":
			setOps = append(setOps, setOp{key: op.key, value: op.value, expiry: op.expiry})
		case "get":
			getKeys = append(getKeys, op.key)
		case "exists":
			existsKeys = append(existsKeys, op.key)
		}
	}

	// 收集 set 操作的结果（保持顺序）
	setResults := make([]PipelineResult, 0, len(setOps))

	err := p.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 批量 set
		for _, op := range setOps {
			row := &KVStoreRow{
				Key:   op.key,
				Value: op.value,
			}
			if err := upsertStatement(tx, row).Error; err != nil {
				setResults = append(setResults, PipelineResult{Op: "set", Key: op.key, Err: err})
				continue
			}
			setResults = append(setResults, PipelineResult{Op: "set", Key: op.key})
		}

		// 批量 get
		getResults := make(map[string][]byte)
		if len(getKeys) > 0 {
			var rows []KVStoreRow
			if err := tx.Where("key IN ?", getKeys).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				if decoded, ok := decodeExclusiveValue(row.Value); ok {
					getResults[row.Key] = decoded
				} else {
					getResults[row.Key] = row.Value
				}
			}
		}

		// 批量 exists
		existsResults := make(map[string]bool)
		if len(existsKeys) > 0 {
			var rows []KVStoreRow
			if err := tx.Select("key").Where("key IN ?", existsKeys).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				existsResults[row.Key] = true
			}
		}

		// 按原始操作顺序组装结果
		reordered := make([]PipelineResult, 0, len(ops))
		setIdx := 0
		for _, op := range ops {
			switch op.op {
			case "set":
				if setIdx < len(setResults) {
					reordered = append(reordered, setResults[setIdx])
					setIdx++
				}
			case "get":
				val := getResults[op.key] // 不存在时为零值 nil
				reordered = append(reordered, PipelineResult{Op: "get", Key: op.key, Value: val})
			case "exists":
				reordered = append(reordered, PipelineResult{
					Op:     "exists",
					Key:    op.key,
					Exists: existsResults[op.key],
				})
			}
		}
		setResults = reordered
		return nil
	})

	if err != nil {
		return nil, err
	}
	return setResults, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureTable 惰性建表。
// 快速路径：atomic.Bool 无锁检查；慢路径：sync.Once 保证只建一次。
// tableReady channel 保证建表完成后其他 goroutine 才继续。
// 对齐 Python: _create_table_if_not_exist()
func (s *DbBasedKVStore) ensureTable() {
	if s.tableCreated.Load() {
		return
	}
	s.tableOnce.Do(func() {
		s.db.AutoMigrate(&KVStoreRow{})
		s.tableCreated.Store(true)
		close(s.tableReady)
	})
	if !s.tableCreated.Load() {
		<-s.tableReady
	}
}

// encodeExclusiveValue 将 value 和 expireAt 编码为 exclusive JSON 格式的 []byte。
// 对齐 Python: json.dumps({EXCLUSIVE_VALUE_KEY: encoded_value, EXCLUSIVE_EXPIRY_KEY: expire_at})
func encodeExclusiveValue(value []byte, expireAt int64) ([]byte, error) {
	ev := exclusiveValue{
		ExclusiveValue:  base64.StdEncoding.EncodeToString(value),
		ExclusiveExpiry: expireAt,
	}
	return json.Marshal(ev)
}

// decodeExclusiveValue 尝试从 BLOB 解码 exclusive JSON 格式。
// 如果数据是 exclusive JSON（含 exclusive_expiry 字段），返回解码后的值和 true。
// 否则返回 nil 和 false。
// 对齐 Python: get() 中的 json.loads + EXCLUSIVE_EXPIRY_KEY 检查
func decodeExclusiveValue(data []byte) ([]byte, bool) {
	var ev exclusiveValue
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, false
	}
	// 检查是否真的是 exclusive 格式：必须含 exclusive_value 或 exclusive_expiry 字段
	if ev.ExclusiveValue == "" && ev.ExclusiveExpiry == 0 {
		return nil, false
	}
	decoded, err := base64.StdEncoding.DecodeString(ev.ExclusiveValue)
	if err != nil {
		return nil, false
	}
	return decoded, true
}

// upsertStatement 构造 GORM upsert 语句，自动处理方言差异。
// 对齐 Python: _get_upsert_stmt() — 区分 MySQL/SQLite 方言
// Go 版本通过 GORM clause.OnConflict 统一处理
func upsertStatement(db *gorm.DB, row *KVStoreRow) *gorm.DB {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(row)
}

// TableName 返回表名，实现 GORM Tabler 接口。
func (KVStoreRow) TableName() string {
	return "kv_store"
}
