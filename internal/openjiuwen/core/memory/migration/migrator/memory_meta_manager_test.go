package migrator

import (
	"context"
	"testing"
)

// mockSqlDbQuerier 用于测试的 SqlDbQuerier 模拟实现
type mockSqlDbQuerier struct {
	records []map[string]any
}

func newMockSqlDbQuerier() *mockSqlDbQuerier {
	return &mockSqlDbQuerier{records: make([]map[string]any, 0)}
}

func (m *mockSqlDbQuerier) Write(_ context.Context, _ string, data map[string]any) error {
	m.records = append(m.records, data)
	return nil
}

func (m *mockSqlDbQuerier) ConditionGet(_ context.Context, _ string, conditions map[string]any, _ []string) ([]map[string]any, error) {
	// 简化实现：只支持 table_name 条件
	tableNames, ok := conditions["table_name"]
	if !ok {
		return m.records, nil
	}
	names, ok := tableNames.([]string)
	if !ok {
		return m.records, nil
	}
	var result []map[string]any
	for _, r := range m.records {
		if tn, ok := r["table_name"].(string); ok {
			for _, n := range names {
				if tn == n {
					result = append(result, r)
					break
				}
			}
		}
	}
	return result, nil
}

func (m *mockSqlDbQuerier) Exist(_ context.Context, _ string, conditions map[string]any) (bool, error) {
	for _, r := range m.records {
		match := true
		for k, v := range conditions {
			if rv, ok := r[k]; !ok || rv != v {
				match = false
				break
			}
		}
		if match {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockSqlDbQuerier) Delete(_ context.Context, _ string, conditions map[string]any) error {
	var remaining []map[string]any
	for _, r := range m.records {
		match := true
		for k, v := range conditions {
			if rv, ok := r[k]; !ok || rv != v {
				match = false
				break
			}
		}
		if !match {
			remaining = append(remaining, r)
		}
	}
	m.records = remaining
	return nil
}

// TestMemoryMetaManager_Add 添加版本记录
func TestMemoryMetaManager_Add(t *testing.T) {
	q := newMockSqlDbQuerier()
	mgr := NewMemoryMetaManager(q)
	ctx := context.Background()

	if err := mgr.Add(ctx, "user_message", "1"); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}

	results, err := mgr.GetByTableName(ctx, "user_message")
	if err != nil {
		t.Fatalf("GetByTableName 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望 1 条记录, got %d", len(results))
	}
}

// TestMemoryMetaManager_Add_幂等 验证重复添加不报错
func TestMemoryMetaManager_Add_幂等(t *testing.T) {
	q := newMockSqlDbQuerier()
	mgr := NewMemoryMetaManager(q)
	ctx := context.Background()

	if err := mgr.Add(ctx, "user_message", "1"); err != nil {
		t.Fatalf("第一次 Add 失败: %v", err)
	}
	if err := mgr.Add(ctx, "user_message", "1"); err != nil {
		t.Fatalf("第二次 Add 失败: %v", err)
	}

	results, _ := mgr.GetByTableName(ctx, "user_message")
	if len(results) != 1 {
		t.Errorf("幂等添加后应只有 1 条记录, got %d", len(results))
	}
}

// TestMemoryMetaManager_Add_空参数 验证空参数时静默返回
func TestMemoryMetaManager_Add_空参数(t *testing.T) {
	q := newMockSqlDbQuerier()
	mgr := NewMemoryMetaManager(q)
	ctx := context.Background()

	if err := mgr.Add(ctx, "", "1"); err != nil {
		t.Errorf("空 tableName 应静默返回, got error: %v", err)
	}
	if err := mgr.Add(ctx, "user_message", ""); err != nil {
		t.Errorf("空 schemaVersion 应静默返回, got error: %v", err)
	}
}

// TestMemoryMetaManager_GetByTableName_不存在 验证不存在的表返回空切片
func TestMemoryMetaManager_GetByTableName_不存在(t *testing.T) {
	q := newMockSqlDbQuerier()
	mgr := NewMemoryMetaManager(q)
	ctx := context.Background()

	results, err := mgr.GetByTableName(ctx, "nonexistent_table")
	if err != nil {
		t.Fatalf("GetByTableName 不应报错: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("不存在的表应返回 0 条记录, got %d", len(results))
	}
}

// TestMemoryMetaManager_DeleteByTableName 删除版本记录
func TestMemoryMetaManager_DeleteByTableName(t *testing.T) {
	q := newMockSqlDbQuerier()
	mgr := NewMemoryMetaManager(q)
	ctx := context.Background()

	_ = mgr.Add(ctx, "user_message", "1")
	_ = mgr.Add(ctx, "user_message", "2")

	if err := mgr.DeleteByTableName(ctx, "user_message"); err != nil {
		t.Fatalf("DeleteByTableName 失败: %v", err)
	}

	results, _ := mgr.GetByTableName(ctx, "user_message")
	if len(results) != 0 {
		t.Errorf("删除后应无记录, got %d", len(results))
	}
}
