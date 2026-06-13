package model

import (
	"context"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ScopeUserMappingManager 作用域用户映射管理器，管理 scope_user_mapping 表的 CRUD。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/scope_user_mapping_manager.py (ScopeUserMappingManager)
type ScopeUserMappingManager struct {
	// sqlDb 通用 SQL CRUD 层
	sqlDb *SqlDbStore
	// metaTable 映射表名
	metaTable string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewScopeUserMappingManager 创建 ScopeUserMappingManager 实例。
//
// 对应 Python: ScopeUserMappingManager.__init__(sql_db_store)
func NewScopeUserMappingManager(sqlDb *SqlDbStore) *ScopeUserMappingManager {
	return &ScopeUserMappingManager{
		sqlDb:     sqlDb,
		metaTable: "scope_user_mapping",
	}
}

// Add 添加作用域用户映射。已存在时跳过（幂等）。
//
// 对应 Python: ScopeUserMappingManager.add(user_id, scope_id, **kwargs)
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
//
// 对应 Python: ScopeUserMappingManager.delete_by_scope_id(scope_id)
func (m *ScopeUserMappingManager) DeleteByScopeID(ctx context.Context, scopeID string) error {
	return m.sqlDb.Delete(ctx, m.metaTable,
		map[string]any{"scope_id": scopeID})
}

// GetByScopeID 按 scope_id 查询映射记录。
//
// 对应 Python: ScopeUserMappingManager.get_by_scope_id(scope_id)
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
