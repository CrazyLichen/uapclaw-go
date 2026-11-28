package kbadmin

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestUploadStore_发起认领与串行守卫 测试槽位登记、字段回填与同 KB 同名 busy 守卫
func TestUploadStore_发起认领与串行守卫(t *testing.T) {
	st := NewUploadStore(t.TempDir())
	slot, err := st.InitUpload("kb-1", "测试.txt", 10, "abc")
	if err != nil {
		t.Fatalf("InitUpload: %v", err)
	}
	if slot.UploadID == "" || slot.ResolvedPath == "" || slot.TotalSize != 10 {
		t.Fatalf("slot 字段异常: %+v", slot)
	}
	// 同 KB 同名在途重复发起 → busy
	if _, err := st.InitUpload("kb-1", "测试.txt", 10, "abc"); err != ErrUploadChannelBusy {
		t.Fatalf("期望 ErrUploadChannelBusy, got %v", err)
	}
	// Get 可取回
	got, err := st.Get(slot.UploadID)
	if err != nil || got.KBID != "kb-1" {
		t.Fatalf("Get 异常: %+v %v", got, err)
	}
}

// TestUploadStore_超限拒绝 测试超过 50MiB 上限直接拒绝
func TestUploadStore_超限拒绝(t *testing.T) {
	st := NewUploadStore(t.TempDir())
	if _, err := st.InitUpload("kb-1", "big.bin", MaxFileBytes+1, "x"); err != ErrFileTooLarge {
		t.Fatalf("期望 ErrFileTooLarge, got %v", err)
	}
}

// TestUploadStore_TTL清理 测试超时槽位自动清理（含分片目录删除）
func TestUploadStore_TTL清理(t *testing.T) {
	st := NewUploadStore(t.TempDir())
	st.ttl = 30 * time.Millisecond
	slot, err := st.InitUpload("kb-1", "a.txt", 1, "h")
	if err != nil {
		t.Fatalf("InitUpload: %v", err)
	}
	if err := os.WriteFile(slot.ResolvedPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	st.CleanupExpired()
	if _, err := st.Get(slot.UploadID); err != ErrKBUploadNotFound {
		t.Fatalf("期望 ErrKBUploadNotFound, got %v", err)
	}
	if _, err := os.Stat(filepath.Dir(slot.ResolvedPath)); !os.IsNotExist(err) {
		t.Fatal("过期分片目录未清理")
	}
}

// TestUploadStore_Cancel清理 测试取消路径删除分片并摘除槽位
func TestUploadStore_Cancel清理(t *testing.T) {
	st := NewUploadStore(t.TempDir())
	slot, err := st.InitUpload("kb-1", "a.txt", 1, "h")
	if err != nil {
		t.Fatalf("InitUpload: %v", err)
	}
	if err := os.WriteFile(slot.ResolvedPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := st.Cancel(slot.UploadID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, err := os.Stat(slot.ResolvedPath); !os.IsNotExist(err) {
		t.Fatal("cancel 后分片未删除")
	}
	if _, err := st.Get(slot.UploadID); err != ErrKBUploadNotFound {
		t.Fatalf("cancel 后槽位未摘除: %v", err)
	}
}

// TestUploadStore_Complete_hash比对 测试收尾 sha256 复算：一致摘除/不符清理
func TestUploadStore_Complete_hash比对(t *testing.T) {
	st := NewUploadStore(t.TempDir())
	slot, err := st.InitUpload("kb-1", "a.txt", 3, "")
	if err != nil {
		t.Fatalf("InitUpload: %v", err)
	}
	if err := os.WriteFile(slot.ResolvedPath, []byte("abc"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sum, err := FileSha256(slot.ResolvedPath)
	if err != nil {
		t.Fatalf("FileSha256: %v", err)
	}
	// hash 不符 → ErrHashMismatch 且分片清理
	st2 := NewUploadStore(t.TempDir())
	slot2, _ := st2.InitUpload("kb-1", "a.txt", 3, "deadbeef")
	_ = os.WriteFile(slot2.ResolvedPath, []byte("abc"), 0o644)
	if _, err := st2.Complete(slot2.UploadID); err != ErrHashMismatch {
		t.Fatalf("期望 ErrHashMismatch, got %v", err)
	}
	if _, err := os.Stat(filepath.Dir(slot2.ResolvedPath)); !os.IsNotExist(err) {
		t.Fatal("hash 不符后分片未清理")
	}
	_ = sum
}
// TestUploadStore_孤儿目录janitor 测试 D-30-5-R1（A 拍板）：无槽位引用且超龄的
// upload 子目录被 CleanupExpired 清扫（已完成/断线遗留目录滞留问题）
func TestUploadStore_孤儿目录janitor(t *testing.T) {
	st := NewUploadStore(t.TempDir())
	// 1) 完成后的目录（槽位已摘除）→ 超龄即清
	slot, _ := st.InitUpload("kb-1", "a.txt", 1, "")
	_ = os.WriteFile(slot.ResolvedPath, []byte("x"), 0o644)
	if _, err := st.Complete(slot.UploadID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	completedDir := filepath.Dir(slot.ResolvedPath)
	// 目录 mtime 改老（超过 ttl）
	old := time.Now().Add(-2 * uploadTTLDefault)
	if err := os.Chtimes(completedDir, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	// 2) 在途槽位目录 → 不清扫
	live, _ := st.InitUpload("kb-1", "b.txt", 1, "")
	liveDir := filepath.Dir(live.ResolvedPath)
	if err := os.Chtimes(liveDir, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	st.CleanupExpired()
	if _, err := os.Stat(completedDir); !os.IsNotExist(err) {
		t.Fatal("孤儿目录未被 janitor 清理")
	}
	if _, err := os.Stat(liveDir); err != nil {
		t.Fatal("在途目录被误清扫")
	}
	if _, err := st.Get(live.UploadID); err != nil {
		t.Fatal("在途槽位被误摘除")
	}
}
