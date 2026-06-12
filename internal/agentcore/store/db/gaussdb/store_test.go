package gaussdb

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/db"
	"gorm.io/gorm"
)

// ──────────────────────────── GaussDbStore 测试 ────────────────────────────

// TestGaussDbStore_接口满足 编译期验证 *GaussDbStore 满足 db.BaseDbStore 接口。
func TestGaussDbStore_接口满足(t *testing.T) {
	var _ db.BaseDbStore = (*GaussDbStore)(nil)
}

// TestNewGaussDbStoreWithDB 验证从已有 *gorm.DB 构造 GaussDbStore。
func TestNewGaussDbStoreWithDB(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	store := NewGaussDbStoreWithDB(gormDB)
	if store == nil {
		t.Fatal("NewGaussDbStoreWithDB 返回 nil")
	}
}

// TestGaussDbStore_GetDB 验证 GetDB 返回正确的 *gorm.DB 实例。
func TestGaussDbStore_GetDB(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	store := NewGaussDbStoreWithDB(gormDB)
	result := store.GetDB(context.Background())
	if result != gormDB {
		t.Error("GetDB 返回的 *gorm.DB 与构造时传入的不一致")
	}
}

// TestGaussDbStore_GetDB_NilDB 验证持有 nil *gorm.DB 时 GetDB 返回 nil。
func TestGaussDbStore_GetDB_NilDB(t *testing.T) {
	store := NewGaussDbStoreWithDB(nil)
	result := store.GetDB(context.Background())
	if result != nil {
		t.Errorf("GetDB 返回 %v, 期望 nil", result)
	}
}

// TestGaussDbStore_GetDB_忽略Context 验证 GetDB 忽略 context 参数。
func TestGaussDbStore_GetDB_忽略Context(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	store := NewGaussDbStoreWithDB(gormDB)

	ctx1 := context.Background()
	ctx2 := context.TODO()

	result1 := store.GetDB(ctx1)
	result2 := store.GetDB(ctx2)

	if result1 != result2 {
		t.Error("不同 context 调用 GetDB 返回了不同的 *gorm.DB 实例")
	}
	if result1 != gormDB {
		t.Error("GetDB 返回的 *gorm.DB 与构造时传入的不一致")
	}
}

// TestGaussDbStore_Close 验证 Close 正常关闭底层连接。
func TestGaussDbStore_Close(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	store := NewGaussDbStoreWithDB(gormDB)
	if err := store.Close(); err != nil {
		t.Errorf("Close 返回错误: %v", err)
	}
}

// TestGaussDbStore_Close_NilDB 验证持有 nil *gorm.DB 时 Close 返回错误。
func TestGaussDbStore_Close_NilDB(t *testing.T) {
	store := NewGaussDbStoreWithDB(nil)
	err := store.Close()
	if err == nil {
		t.Error("期望 Close 对 nil *gorm.DB 返回错误，但得到 nil")
	}
}
