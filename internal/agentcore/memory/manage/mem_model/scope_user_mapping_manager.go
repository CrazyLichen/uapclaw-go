package mem_model

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/common"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ScopeUserMappingStore 作用域用户映射存储的抽象。
//
// 存在两个后端：db_store 存在时用 ScopeUserMappingManager（SQL 表），
// 缺失时用 KvScopeUserMappingManager（KV）。LongTermMemory 只依赖本接口，
// 从而不再强制要求 db_store。
// Python: 上游两个类无共同基类，靠 duck typing；Go 需显式接口。
type ScopeUserMappingStore interface {
	// Add 添加映射（幂等）
	Add(ctx context.Context, userID string, scopeID string) error
	// DeleteByScopeID 删除某 scope 下的全部映射
	DeleteByScopeID(ctx context.Context, scopeID string) error
	// GetByScopeID 查询某 scope 下的全部映射
	GetByScopeID(ctx context.Context, scopeID string) ([]map[string]any, error)
}

// ScopeUserMappingManager 作用域用户映射管理器，管理 scope_user_mapping 表的 CRUD。
// Python: openjiuwen/core/memory/manage/mem_model/scope_user_mapping_manager.py (ScopeUserMappingManager)
type ScopeUserMappingManager struct {
	// sqlDb 通用 SQL CRUD 层
	sqlDb *SqlDbStore
	// metaTable 映射表名
	metaTable string
}

// KvScopeUserMappingManager 基于 KV 存储的作用域用户映射管理器。
//
// 为什么需要它：LongTermMemory 原先强依赖 db_store，没有 db 就整体不可用。
// db_store 缺失时改用 kv 后端记映射，记忆功能的其他部分（片段/摘要/变量）本来
// 就走 kv 与 memory_index，不依赖 db。
// 对齐 Python KvScopeUserMappingManager（scope_user_mapping_manager.py:50）。
type KvScopeUserMappingManager struct {
	// kvStore KV 存储
	kvStore kv.BaseKVStore
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// metaTableName 映射表名
	metaTableName = "scope_user_mapping"
	// scopeUserMappingPrefix KV 中作用域用户映射的键前缀
	// Python: _SCOPE_USER_MAPPING_PREFIX = "scope_user_mapping"
	scopeUserMappingPrefix = "scope_user_mapping"
	// kvDeleteBatchSize 按前缀批量删除时每批处理条数
	kvDeleteBatchSize = 100
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewScopeUserMappingManager 创建 ScopeUserMappingManager 实例。
// Python: ScopeUserMappingManager.__init__(sql_db_store)
func NewScopeUserMappingManager(sqlDb *SqlDbStore) *ScopeUserMappingManager {
	return &ScopeUserMappingManager{
		sqlDb:     sqlDb,
		metaTable: metaTableName,
	}
}

// NewKvScopeUserMappingManager 创建 KV 后端映射管理器，并注册键前缀。
//
// Python: KvScopeUserMappingManager.__init__(kv_store) + kv_prefix_registry.register_current
func NewKvScopeUserMappingManager(kvStore kv.BaseKVStore) (*KvScopeUserMappingManager, error) {
	if err := commonKVPrefixRegistryRegisterCurrent(scopeUserMappingPrefix); err != nil {
		return nil, fmt.Errorf("注册 scope_user_mapping 前缀失败: %w", err)
	}
	return &KvScopeUserMappingManager{kvStore: kvStore}, nil
}

// Add 添加作用域用户映射。已存在时跳过（幂等）。
// Python: ScopeUserMappingManager.add(user_id, scope_id, **kwargs)
func (m *ScopeUserMappingManager) Add(ctx context.Context, userID string, scopeID string) error {
	data := map[string]any{
		"user_id":  userID,
		"scope_id": scopeID,
	}
	exists, err := m.sqlDb.Exist(ctx, m.metaTable,
		map[string]any{"user_id": userID, "scope_id": scopeID})
	if err != nil {
		return fmt.Errorf("检查映射是否存在失败: %w", err)
	}
	if exists {
		return nil
	}
	return m.sqlDb.Write(ctx, m.metaTable, data)
}

// DeleteByScopeID 按 scope_id 删除映射记录。
// Python: ScopeUserMappingManager.delete_by_scope_id(scope_id)
func (m *ScopeUserMappingManager) DeleteByScopeID(ctx context.Context, scopeID string) error {
	return m.sqlDb.Delete(ctx, m.metaTable,
		map[string]any{"scope_id": scopeID})
}

// GetByScopeID 按 scope_id 查询映射记录。
// Python: ScopeUserMappingManager.get_by_scope_id(scope_id)
func (m *ScopeUserMappingManager) GetByScopeID(ctx context.Context, scopeID string) ([]map[string]any, error) {
	results, err := m.sqlDb.ConditionGet(ctx, m.metaTable,
		map[string]any{"scope_id": []string{scopeID}}, nil)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results, nil
}

// Add 添加映射，已存在时跳过（幂等）。
// Python: KvScopeUserMappingManager.add
func (m *KvScopeUserMappingManager) Add(ctx context.Context, userID string, scopeID string) error {
	key := scopeUserMappingKey(scopeID, userID)
	exists, err := m.kvStore.Exists(ctx, key)
	if err != nil {
		return fmt.Errorf("检查映射是否存在失败: %w", err)
	}
	if exists {
		return nil
	}
	value, err := json.Marshal(map[string]string{
		"user_id":  userID,
		"scope_id": scopeID,
	})
	if err != nil {
		return fmt.Errorf("序列化映射失败: %w", err)
	}
	return m.kvStore.Set(ctx, key, value)
}

// DeleteByScopeID 删除该 scope 下的全部映射。
// Python: KvScopeUserMappingManager.delete_by_scope_id
func (m *KvScopeUserMappingManager) DeleteByScopeID(ctx context.Context, scopeID string) error {
	return m.kvStore.DeleteByPrefix(ctx, scopeUserMappingPrefixFor(scopeID), kvDeleteBatchSize)
}

// GetByScopeID 查询该 scope 下的全部映射，无记录时返回 nil。
// Python: KvScopeUserMappingManager.get_by_scope_id
func (m *KvScopeUserMappingManager) GetByScopeID(ctx context.Context, scopeID string) ([]map[string]any, error) {
	pairs, err := m.kvStore.GetByPrefix(ctx, scopeUserMappingPrefixFor(scopeID))
	if err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	results := make([]map[string]any, 0, len(pairs))
	for _, raw := range pairs {
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			logger.Warn(logComponent).Err(err).Msg("failed to decode scope user mapping entry, skipping")
			continue
		}
		results = append(results, item)
	}
	return results, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// commonKVPrefixRegistryRegisterCurrent 把前缀注册进全局 KV 前缀注册表。
func commonKVPrefixRegistryRegisterCurrent(prefix string) error {
	return common.KVPrefixRegistry.RegisterCurrent(prefix)
}

// scopeUserMappingKey 构造单个映射的 KV 键。
// Python: f"{_SCOPE_USER_MAPPING_PREFIX}/{scope_id}/{user_id}"
func scopeUserMappingKey(scopeID, userID string) string {
	return fmt.Sprintf("%s/%s/%s", scopeUserMappingPrefix, scopeID, userID)
}

// scopeUserMappingPrefixFor 构造按 scope 查询/删除的 KV 前缀。
// Python: f"{_SCOPE_USER_MAPPING_PREFIX}/{scope_id}/"
func scopeUserMappingPrefixFor(scopeID string) string {
	return fmt.Sprintf("%s/%s/", scopeUserMappingPrefix, scopeID)
}
