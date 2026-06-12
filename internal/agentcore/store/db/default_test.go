package db

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// ──────────────────────────── 编译验证 ────────────────────────────

// TestDefaultDbStore_接口满足 编译期验证 *DefaultDbStore 满足 BaseDbStore 接口。
func TestDefaultDbStore_接口满足(t *testing.T) {
	var _ BaseDbStore = (*DefaultDbStore)(nil)
}

// ──────────────────────────── 构造函数测试 ────────────────────────────

// TestNewDefaultDbStore 验证构造函数返回非 nil 实例。
func TestNewDefaultDbStore(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	store := NewDefaultDbStore(db)
	if store == nil {
		t.Fatal("NewDefaultDbStore 返回 nil")
	}
}

// TestNewDefaultDbStore_NilDB 验证传入 nil *gorm.DB 时构造函数不会 panic。
func TestNewDefaultDbStore_NilDB(t *testing.T) {
	store := NewDefaultDbStore(nil)
	if store == nil {
		t.Fatal("NewDefaultDbStore(nil) 返回 nil")
	}
}

// ──────────────────────────── GetDB 测试 ────────────────────────────

// TestDefaultDbStore_GetDB 验证 GetDB 返回正确的 *gorm.DB 实例。
func TestDefaultDbStore_GetDB(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	store := NewDefaultDbStore(db)
	result := store.GetDB(context.Background())
	if result != db {
		t.Error("GetDB 返回的 *gorm.DB 与构造时传入的不一致")
	}
}

// TestDefaultDbStore_GetDB_NilDB 验证持有 nil *gorm.DB 时 GetDB 返回 nil。
func TestDefaultDbStore_GetDB_NilDB(t *testing.T) {
	store := NewDefaultDbStore(nil)
	result := store.GetDB(context.Background())
	if result != nil {
		t.Errorf("GetDB 返回 %v, 期望 nil", result)
	}
}

// TestDefaultDbStore_GetDB_忽略Context 验证 GetDB 忽略 context 参数，
// 多次调用使用不同 context 返回相同的 *gorm.DB 实例。
func TestDefaultDbStore_GetDB_忽略Context(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	store := NewDefaultDbStore(db)

	ctx1 := context.Background()
	ctx2 := context.TODO()

	result1 := store.GetDB(ctx1)
	result2 := store.GetDB(ctx2)

	if result1 != result2 {
		t.Error("不同 context 调用 GetDB 返回了不同的 *gorm.DB 实例")
	}
	if result1 != db {
		t.Error("GetDB 返回的 *gorm.DB 与构造时传入的不一致")
	}
}
