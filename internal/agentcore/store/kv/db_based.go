package kv

import (
	"encoding/base64"
	"encoding/json"
	"sync"
	"sync/atomic"

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
		db: db,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureTable 惰性建表。
// 快速路径：atomic.Bool 无锁检查；慢路径：sync.Once 保证只建一次。
// 对齐 Python: _create_table_if_not_exist()
func (s *DbBasedKVStore) ensureTable() {
	if s.tableCreated.Load() {
		return
	}
	s.tableOnce.Do(func() {
		s.db.AutoMigrate(&KVStoreRow{})
		s.tableCreated.Store(true)
	})
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
