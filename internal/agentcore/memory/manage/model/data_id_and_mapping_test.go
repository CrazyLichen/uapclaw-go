package model

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDataIdManager_创建 验证 NewDataIdManager 返回非空实例
func TestDataIdManager_创建(t *testing.T) {
	m := NewDataIdManager()
	assert.NotNil(t, m)
}

// TestDataIdManager_生成ID 验证 GenerateNextID 返回 24 字符 hex 字符串
func TestDataIdManager_生成ID(t *testing.T) {
	m := NewDataIdManager()
	id := m.GenerateNextID("user1")
	assert.Len(t, id, 24, "ID 应为 12 字节 = 24 字符 hex")
}

// TestDataIdManager_不同用户ID不同 验证不同用户生成不同 ID（由于随机部分，即使同用户也可能不同）
func TestDataIdManager_不同用户ID不同(t *testing.T) {
	m := NewDataIdManager()
	id1 := m.GenerateNextID("user1")
	id2 := m.GenerateNextID("user2")
	// 由于时间戳和随机数，两次调用几乎一定不同
	assert.NotEqual(t, id1, id2, "不同用户应生成不同 ID")
}

// TestDataIdManager_多次生成不重复 验证多次生成不重复
func TestDataIdManager_多次生成不重复(t *testing.T) {
	m := NewDataIdManager()
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id := m.GenerateNextID("user1")
		seen[id] = struct{}{}
	}
	assert.GreaterOrEqual(t, len(seen), 90, "100 次生成应有至少 90 个不同值")
}

// TestScopeUserMappingManager_创建 验证 NewScopeUserMappingManager 返回非空实例
func TestScopeUserMappingManager_创建(t *testing.T) {
	store, _ := newTestSqlDbStore(t)
	m := NewScopeUserMappingManager(store)
	assert.NotNil(t, m)
}

// TestScopeUserMappingManager_添加和查询 验证 Add 和 GetByScopeID 工作
func TestScopeUserMappingManager_添加和查询(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}
	m := NewScopeUserMappingManager(store)

	ctx := context.Background()
	err := m.Add(ctx, "user1", "scope1")
	assert.NoError(t, err)

	results, err := m.GetByScopeID(ctx, "scope1")
	assert.NoError(t, err)
	assert.Len(t, results, 1)
}

// TestScopeUserMappingManager_幂等添加 验证重复添加不报错
func TestScopeUserMappingManager_幂等添加(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}
	m := NewScopeUserMappingManager(store)

	ctx := context.Background()
	err := m.Add(ctx, "user1", "scope1")
	assert.NoError(t, err)
	err = m.Add(ctx, "user1", "scope1")
	assert.NoError(t, err)
}

// TestScopeUserMappingManager_按ScopeID删除 验证 DeleteByScopeID 工作
func TestScopeUserMappingManager_按ScopeID删除(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}
	m := NewScopeUserMappingManager(store)

	ctx := context.Background()
	err := m.Add(ctx, "user1", "scope1")
	assert.NoError(t, err)

	err = m.DeleteByScopeID(ctx, "scope1")
	assert.NoError(t, err)

	results, err := m.GetByScopeID(ctx, "scope1")
	assert.NoError(t, err)
	assert.Nil(t, results)
}

// TestScopeUserMappingManager_空结果查询 验证查询不存在的 scope 返回 nil
func TestScopeUserMappingManager_空结果查询(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}
	m := NewScopeUserMappingManager(store)

	ctx := context.Background()
	results, err := m.GetByScopeID(ctx, "nonexistent")
	assert.NoError(t, err)
	assert.Nil(t, results)
}

// TestSqlDbStore_CountWithTimeRange 验证 CountWithTimeRange 基本功能
func TestSqlDbStore_CountWithTimeRange(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	ctx := context.Background()
	// 写入一条记录
	err := store.Write(ctx, "user_message", map[string]any{
		"message_id": "msg1",
		"user_id":    "user1",
		"scope_id":   "scope1",
		"content":    "hello",
		"session_id": "sess1",
		"role":       "user",
		"timestamp":  time.Now().Format(time.RFC3339),
	})
	assert.NoError(t, err)

	// 不加时间范围
	count, err := store.CountWithTimeRange(ctx, "user_message", nil, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// 加时间范围（包含当前时间）
	now := time.Now()
	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)
	count2, err := store.CountWithTimeRange(ctx, "user_message", nil, &start, &end)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), count2)

	// 时间范围不包含（在当前时间之后才开始）
	futureStart := now.Add(time.Hour * 24)
	count3, err := store.CountWithTimeRange(ctx, "user_message", nil, &futureStart, nil)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), count3)
}

// TestSqlDbStore_CountWithTimeRange_无效列名 验证 CountWithTimeRange 校验列名
func TestSqlDbStore_CountWithTimeRange_无效列名(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	ctx := context.Background()
	_, err := store.CountWithTimeRange(ctx, "user_message", map[string]any{"invalid;col": "val"}, nil, nil)
	assert.Error(t, err)
}
