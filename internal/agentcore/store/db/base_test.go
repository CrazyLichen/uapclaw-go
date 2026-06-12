package db

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeDbStore 用于验证 BaseDbStore 接口可被实现。
type fakeDbStore struct {
	db *gorm.DB
}

func (f *fakeDbStore) GetDB(_ context.Context) *gorm.DB {
	return f.db
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──── 接口编译验证测试 ────

// TestBaseDbStore_接口满足 验证 fakeDbStore 满足 BaseDbStore 接口。
func TestBaseDbStore_接口满足(t *testing.T) {
	var _ BaseDbStore = (*fakeDbStore)(nil)
}

// TestFakeDbStore_GetDB 验证 fakeDbStore.GetDB 返回预期的 *gorm.DB 实例。
func TestFakeDbStore_GetDB(t *testing.T) {
	ctx := context.Background()
	store := &fakeDbStore{db: nil}
	result := store.GetDB(ctx)
	if result != nil {
		t.Errorf("GetDB 返回 %v, 期望 nil", result)
	}
}

// TestBaseDbStore_接口方法签名 验证接口方法签名与预期一致。
func TestBaseDbStore_接口方法签名(t *testing.T) {
	// 通过 fake 实现调用，确保 GetDB 接受 context.Context 参数并返回 *gorm.DB
	store := &fakeDbStore{db: nil}
	ctx := context.Background()
	db := store.GetDB(ctx)
	_ = db // db 为 nil，仅验证方法签名正确
}
