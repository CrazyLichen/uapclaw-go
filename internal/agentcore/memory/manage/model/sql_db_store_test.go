package model

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/db"
)

// newTestSqlDbStore 创建测试用的 SqlDbStore 实例
func newTestSqlDbStore(t *testing.T) (*SqlDbStore, *gorm.DB) {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	dbStore := db.NewDefaultDbStore(gormDB)
	store := NewSqlDbStore(dbStore)
	return store, gormDB
}

// TestSqlDbStore_Write 写入数据
func TestSqlDbStore_Write(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	data := map[string]any{
		"message_id": "msg_test1_1700000000000",
		"user_id":    "user1",
		"scope_id":   "scope1",
		"content":    "hello",
		"session_id": "session1",
		"role":       "user",
		"timestamp":  "2024-01-01T00:00:00Z",
	}

	if err := store.Write(context.Background(), "user_message", data); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
}

// TestSqlDbStore_ConditionGet 条件查询
func TestSqlDbStore_ConditionGet(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	data := map[string]any{
		"message_id": "msg_test2_1700000000000",
		"user_id":    "user1",
		"scope_id":   "scope1",
		"content":    "hello",
		"role":       "user",
		"timestamp":  "2024-01-01T00:00:00Z",
	}
	if err := store.Write(context.Background(), "user_message", data); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	results, err := store.ConditionGet(context.Background(), "user_message",
		map[string]any{"message_id": []string{"msg_test2_1700000000000"}}, nil)
	if err != nil {
		t.Fatalf("ConditionGet 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("ConditionGet 返回 %d 条, 期望 1 条", len(results))
	}
}

// TestSqlDbStore_GetWithSort 排序查询
func TestSqlDbStore_GetWithSort(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	for i := 0; i < 3; i++ {
		data := map[string]any{
			"message_id": fmt.Sprintf("msg_sort_%d", i),
			"user_id":    "user1",
			"scope_id":   "scope1",
			"content":    fmt.Sprintf("content_%d", i),
			"role":       "user",
			"timestamp":  fmt.Sprintf("2024-01-0%dT00:00:00Z", i+1),
		}
		if err := store.Write(context.Background(), "user_message", data); err != nil {
			t.Fatalf("Write 失败: %v", err)
		}
	}

	results, err := store.GetWithSort(context.Background(), "user_message",
		map[string]any{"user_id": "user1"}, "timestamp", "DESC", 2)
	if err != nil {
		t.Fatalf("GetWithSort 失败: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("GetWithSort 返回 %d 条, 期望 2 条", len(results))
	}
}

// TestSqlDbStore_Update 更新数据
func TestSqlDbStore_Update(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	data := map[string]any{
		"message_id": "msg_update_test",
		"user_id":    "user1",
		"scope_id":   "scope1",
		"content":    "old_content",
		"role":       "user",
		"timestamp":  "2024-01-01T00:00:00Z",
	}
	if err := store.Write(context.Background(), "user_message", data); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	if err := store.Update(context.Background(), "user_message",
		map[string]any{"message_id": "msg_update_test"},
		map[string]any{"content": "new_content"}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
}

// TestSqlDbStore_Delete 删除数据
func TestSqlDbStore_Delete(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	data := map[string]any{
		"message_id": "msg_delete_test",
		"user_id":    "user1",
		"scope_id":   "scope1",
		"content":    "to_delete",
		"role":       "user",
		"timestamp":  "2024-01-01T00:00:00Z",
	}
	if err := store.Write(context.Background(), "user_message", data); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	if err := store.Delete(context.Background(), "user_message",
		map[string]any{"message_id": "msg_delete_test"}); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
}

// TestSqlDbStore_Exist 存在性检查
func TestSqlDbStore_Exist(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	data := map[string]any{
		"message_id": "msg_exist_test",
		"user_id":    "user1",
		"scope_id":   "scope1",
		"content":    "hello",
		"role":       "user",
		"timestamp":  "2024-01-01T00:00:00Z",
	}
	if err := store.Write(context.Background(), "user_message", data); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	exists, err := store.Exist(context.Background(), "user_message",
		map[string]any{"message_id": "msg_exist_test"})
	if err != nil {
		t.Fatalf("Exist 失败: %v", err)
	}
	if !exists {
		t.Error("期望记录存在")
	}

	notExists, err := store.Exist(context.Background(), "user_message",
		map[string]any{"message_id": "nonexistent"})
	if err != nil {
		t.Fatalf("Exist 失败: %v", err)
	}
	if notExists {
		t.Error("期望记录不存在")
	}
}

// TestSqlDbStore_Count 计数查询
func TestSqlDbStore_Count(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	for i := 0; i < 3; i++ {
		data := map[string]any{
			"message_id": fmt.Sprintf("msg_count_%d", i),
			"user_id":    "user1",
			"scope_id":   "scope1",
			"content":    fmt.Sprintf("content_%d", i),
			"role":       "user",
			"timestamp":  "2024-01-01T00:00:00Z",
		}
		if err := store.Write(context.Background(), "user_message", data); err != nil {
			t.Fatalf("Write 失败: %v", err)
		}
	}

	count, err := store.Count(context.Background(), "user_message",
		map[string]any{"user_id": "user1"})
	if err != nil {
		t.Fatalf("Count 失败: %v", err)
	}
	if count != 3 {
		t.Errorf("Count = %d, want 3", count)
	}
}

// TestSqlDbStore_Write_不存在的表 验证写入不存在的表返回错误
func TestSqlDbStore_Write_不存在的表(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	_ = gormDB // 只创建 store，不建表

	data := map[string]any{
		"message_id": "msg_err",
		"user_id":    "user1",
	}
	err := store.Write(context.Background(), "nonexistent_table", data)
	if err == nil {
		t.Error("期望写入不存在的表时返回错误")
	}
}

// TestSqlDbStore_ConditionGet_不存在的表 验证查询不存在的表返回错误
func TestSqlDbStore_ConditionGet_不存在的表(t *testing.T) {
	store, _ := newTestSqlDbStore(t)

	_, err := store.ConditionGet(context.Background(), "nonexistent_table",
		map[string]any{"message_id": []string{"x"}}, nil)
	if err == nil {
		t.Error("期望查询不存在的表时返回错误")
	}
}

// TestSqlDbStore_GetWithSort_不存在的表 验证排序查询不存在的表返回错误
func TestSqlDbStore_GetWithSort_不存在的表(t *testing.T) {
	store, _ := newTestSqlDbStore(t)

	_, err := store.GetWithSort(context.Background(), "nonexistent_table",
		map[string]any{"user_id": "user1"}, "timestamp", "ASC", 10)
	if err == nil {
		t.Error("期望排序查询不存在的表时返回错误")
	}
}

// TestSqlDbStore_Update_不存在的表 验证更新不存在的表返回错误
func TestSqlDbStore_Update_不存在的表(t *testing.T) {
	store, _ := newTestSqlDbStore(t)

	err := store.Update(context.Background(), "nonexistent_table",
		map[string]any{"message_id": "x"}, map[string]any{"content": "y"})
	if err == nil {
		t.Error("期望更新不存在的表时返回错误")
	}
}

// TestSqlDbStore_Delete_不存在的表 验证删除不存在的表返回错误
func TestSqlDbStore_Delete_不存在的表(t *testing.T) {
	store, _ := newTestSqlDbStore(t)

	err := store.Delete(context.Background(), "nonexistent_table",
		map[string]any{"message_id": "x"})
	if err == nil {
		t.Error("期望删除不存在的表时返回错误")
	}
}

// TestSqlDbStore_Count_不存在的表 验证计数不存在的表返回错误
func TestSqlDbStore_Count_不存在的表(t *testing.T) {
	store, _ := newTestSqlDbStore(t)

	_, err := store.Count(context.Background(), "nonexistent_table",
		map[string]any{"user_id": "user1"})
	if err == nil {
		t.Error("期望计数不存在的表时返回错误")
	}
}

// TestSqlDbStore_GetWithSortAndTimeRange 时间范围查询测试
func TestSqlDbStore_GetWithSortAndTimeRange(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	for i := 1; i <= 3; i++ {
		data := map[string]any{
			"message_id": fmt.Sprintf("msg_tr_%d", i),
			"user_id":    "user1",
			"scope_id":   "scope1",
			"content":    fmt.Sprintf("content_%d", i),
			"role":       "user",
			"timestamp":  fmt.Sprintf("2024-01-0%dT00:00:00Z", i),
		}
		if err := store.Write(context.Background(), "user_message", data); err != nil {
			t.Fatalf("Write 失败: %v", err)
		}
	}

	start, _ := time.Parse(time.RFC3339, "2024-01-02T00:00:00Z")
	end, _ := time.Parse(time.RFC3339, "2024-01-03T00:00:00Z")
	results, err := store.GetWithSortAndTimeRange(context.Background(), "user_message",
		map[string]any{"user_id": "user1"}, "timestamp", "ASC", 10, &start, &end)
	if err != nil {
		t.Fatalf("GetWithSortAndTimeRange 失败: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("时间范围查询返回 %d 条, 期望 2 条", len(results))
	}
}

// TestSqlDbStore_GetWithSortAndTimeRange_不存在的表 验证查询不存在的表返回错误
func TestSqlDbStore_GetWithSortAndTimeRange_不存在的表(t *testing.T) {
	store, _ := newTestSqlDbStore(t)

	start := time.Now()
	_, err := store.GetWithSortAndTimeRange(context.Background(), "nonexistent_table",
		map[string]any{}, "timestamp", "ASC", 10, &start, nil)
	if err == nil {
		t.Error("期望查询不存在的表时返回错误")
	}
}

// TestSqlDbStore_Exist_不存在的表 验证检查不存在的表返回错误
func TestSqlDbStore_Exist_不存在的表(t *testing.T) {
	store, _ := newTestSqlDbStore(t)

	_, err := store.Exist(context.Background(), "nonexistent_table",
		map[string]any{"message_id": "x"})
	if err == nil {
		t.Error("期望检查不存在的表时返回错误")
	}
}

// TestSqlDbStore_CreateBatch 批量插入
func TestSqlDbStore_CreateBatch(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	rows := make([]map[string]any, 3)
	for i := 0; i < 3; i++ {
		rows[i] = map[string]any{
			"message_id": fmt.Sprintf("msg_batch_%d", i),
			"user_id":    "user1",
			"scope_id":   "scope1",
			"content":    fmt.Sprintf("content_%d", i),
			"role":       "user",
			"timestamp":  fmt.Sprintf("2024-01-0%dT00:00:00Z", i+1),
		}
	}

	if err := store.CreateBatch(context.Background(), "user_message", rows); err != nil {
		t.Fatalf("CreateBatch 失败: %v", err)
	}

	count, _ := store.Count(context.Background(), "user_message", map[string]any{"user_id": "user1"})
	if count != 3 {
		t.Errorf("CreateBatch 后 Count = %d, want 3", count)
	}
}

// TestSqlDbStore_CreateBatch_空切片 验证空切片直接返回 nil
func TestSqlDbStore_CreateBatch_空切片(t *testing.T) {
	store, _ := newTestSqlDbStore(t)
	if err := store.CreateBatch(context.Background(), "user_message", []map[string]any{}); err != nil {
		t.Fatalf("空切片应返回 nil, got error: %v", err)
	}
}

// TestSqlDbStore_BatchGet 多组 OR 条件查询
func TestSqlDbStore_BatchGet(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	// 写入两条不同用户的消息
	_ = store.Write(context.Background(), "user_message", map[string]any{
		"message_id": "msg_bg_1", "user_id": "user1", "scope_id": "scope1",
		"content": "hello1", "role": "user", "timestamp": "2024-01-01T00:00:00Z",
	})
	_ = store.Write(context.Background(), "user_message", map[string]any{
		"message_id": "msg_bg_2", "user_id": "user2", "scope_id": "scope1",
		"content": "hello2", "role": "user", "timestamp": "2024-01-02T00:00:00Z",
	})

	results, err := store.BatchGet(context.Background(), "user_message", []map[string]any{
		{"user_id": "user1"},
		{"user_id": "user2"},
	})
	if err != nil {
		t.Fatalf("BatchGet 失败: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("BatchGet 返回 %d 条, 期望 2 条", len(results))
	}
}

// TestSqlDbStore_Get 按 ID 查询单条
// Python 硬编码 WHERE id = record_id，此处使用带 id 列的临时表测试
func TestSqlDbStore_Get(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	// 创建带 id 列的临时表（对齐 Python get 方法的 WHERE id = record_id）
	type TestRecord struct {
		ID      string `gorm:"primaryKey;size:64"`
		Content string `gorm:"size:256"`
	}
	if err := gormDB.AutoMigrate(&TestRecord{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}

	_ = store.Write(context.Background(), "test_records", map[string]any{
		"id": "rec_1", "content": "hello",
	})

	result, err := store.Get(context.Background(), "test_records", "rec_1", nil)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if result == nil {
		t.Fatal("Get 应返回一条记录")
	}
}

// TestSqlDbStore_Get_不存在 验证不存在的记录返回 nil
func TestSqlDbStore_Get_不存在(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	// 创建带 id 列的临时表
	type TestRecord struct {
		ID      string `gorm:"primaryKey;size:64"`
		Content string `gorm:"size:256"`
	}
	if err := gormDB.AutoMigrate(&TestRecord{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}

	result, err := store.Get(context.Background(), "test_records", "nonexistent", nil)
	if err != nil {
		t.Fatalf("Get 不应报错: %v", err)
	}
	if result != nil {
		t.Error("不存在的记录应返回 nil")
	}
}

// TestSqlDbStore_DeleteTable 删除整表
func TestSqlDbStore_DeleteTable(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	if err := store.DeleteTable(context.Background(), "user_message"); err != nil {
		t.Fatalf("DeleteTable 失败: %v", err)
	}

	if gormDB.Migrator().HasTable("user_message") {
		t.Error("表应该已被删除")
	}
}

// TestSqlDbStore_ConditionGet_类型校验 验证 values 非切片时返回 error
func TestSqlDbStore_ConditionGet_类型校验(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	_, err := store.ConditionGet(context.Background(), "user_message",
		map[string]any{"message_id": "not_a_slice"}, nil)
	if err == nil {
		t.Error("values 非切片时应返回 error")
	}
}

// TestSqlDbStore_GetWithSort_排序列不存在 验证排序列不存在时返回 error
func TestSqlDbStore_GetWithSort_排序列不存在(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	_, err := store.GetWithSort(context.Background(), "user_message",
		map[string]any{"user_id": "user1"}, "nonexistent_column", "ASC", 10)
	if err == nil {
		t.Error("排序列不存在时应返回 error")
	}
}

// TestSqlDbStore_GetTable 获取表列名列表
func TestSqlDbStore_GetTable(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	columns, err := store.GetTable(context.Background(), "user_message")
	if err != nil {
		t.Fatalf("GetTable 失败: %v", err)
	}
	if len(columns) == 0 {
		t.Error("GetTable 应返回列名列表")
	}
	// 验证缓存命中
	columns2, _ := store.GetTable(context.Background(), "user_message")
	if len(columns2) != len(columns) {
		t.Errorf("缓存命中后列数不一致: %d vs %d", len(columns2), len(columns))
	}
}

// TestSqlDbStore_InvalidateTableCache 清除表缓存
func TestSqlDbStore_InvalidateTableCache(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	// 预热缓存
	_, _ = store.GetTable(context.Background(), "user_message")
	// 清除缓存
	store.InvalidateTableCache("user_message")
	// 再次获取应重新查询（无报错即通过）
	_, err := store.GetTable(context.Background(), "user_message")
	if err != nil {
		t.Fatalf("InvalidateTableCache 后 GetTable 失败: %v", err)
	}
}
