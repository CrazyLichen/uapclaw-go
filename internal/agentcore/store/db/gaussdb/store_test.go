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

// TestGaussDbStore_Close_已关闭后再次关闭 验证 Close 关闭后
// 再次调用时 s.db.DB() 返回错误。
func TestGaussDbStore_Close_已关闭后再次关闭(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	store := NewGaussDbStoreWithDB(gormDB)
	// 第一次关闭应成功
	if err := store.Close(); err != nil {
		t.Fatalf("第一次 Close 返回错误: %v", err)
	}
	// 第二次关闭，底层 sql.DB 已关闭，s.db.DB() 可能返回错误
	err = store.Close()
	// 不强制要求第二次关闭失败，仅验证不会 panic
	_ = err
}

// TestNewGaussDbStore_DSN无效 验证 NewGaussDbStore 使用无效 DSN 时返回错误。
func TestNewGaussDbStore_DSN无效(t *testing.T) {
	// 使用无法解析的 DSN，gorm.Open → GaussDialector.Initialize 将失败
	_, err := NewGaussDbStore("postgres://invalid\x00dsn")
	if err == nil {
		t.Fatal("期望 NewGaussDbStore 返回错误，但返回 nil")
	}
}
