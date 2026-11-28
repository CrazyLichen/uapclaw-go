package kbadmin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// newDocSvcWithStore 测试助手：内存外真实 SQLite store + DocService + 临时目录
func newDocSvcWithStore(t *testing.T) (*DocService, *Store, string) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "kbadmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := NewDocService(store)
	dir := t.TempDir()
	return svc, store, dir
}

// TestSubmitParseTask_建任务建文档 建任务链：kb_doc(pending) + kb_task(pending)
func TestSubmitParseTask_建任务建文档(t *testing.T) {
	svc, store, dir := newDocSvcWithStore(t)
	kb := &KBInfo{ID: "kb-1", Name: "n", Type: "document", CreatedAt: 1, UpdatedAt: 1}
	if err := store.KB().Create(kb); err != nil {
		t.Fatal(err)
	}
	fp := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(fp, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	task, doc, err := svc.SubmitParseTask("kb-1", "a.txt", fp, 5, "hash-a")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "pending" || doc.ParseStatus != "pending" {
		t.Fatalf("任务/文档应 pending: %+v %+v", task, doc)
	}
	if doc.ContentHash != "hash-a" || doc.KBID != "kb-1" {
		t.Fatalf("doc 字段不符: %+v", doc)
	}
	if task.DocID != doc.ID {
		t.Fatalf("task.DocID 应指向 doc: %+v", task)
	}
}

// TestSubmitParseTask_重复上传拒单 同 KB 同 hash 二次提交 → DOC_DUPLICATE
func TestSubmitParseTask_重复上传拒单(t *testing.T) {
	svc, store, dir := newDocSvcWithStore(t)
	_ = store.KB().Create(&KBInfo{ID: "kb-1", Name: "n", CreatedAt: 1, UpdatedAt: 1})
	fp := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(fp, []byte("hello"), 0o644)
	_, _, err := svc.SubmitParseTask("kb-1", "a.txt", fp, 5, "dup-hash")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.SubmitParseTask("kb-1", "other.txt", fp, 5, "dup-hash")
	if !errors.Is(err, ErrDocDuplicate) {
		t.Fatalf("同 KB 同 hash 第二次应 DOC_DUPLICATE, got %v", err)
	}
}

// TestSubmitParseTask_failed文档放行重复 去重判据不含 failed/cancelled（可重传修复）
func TestSubmitParseTask_failed文档放行重复(t *testing.T) {
	svc, store, dir := newDocSvcWithStore(t)
	_ = store.KB().Create(&KBInfo{ID: "kb-1", Name: "n", CreatedAt: 1, UpdatedAt: 1})
	fp := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(fp, []byte("hello"), 0o644)
	_, doc, err := svc.SubmitParseTask("kb-1", "a.txt", fp, 5, "dup-hash")
	if err != nil {
		t.Fatal(err)
	}
	doc.ParseStatus = "failed"
	if err := store.Doc().Update(doc); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.SubmitParseTask("kb-1", "a.txt", fp, 5, "dup-hash"); err != nil {
		t.Fatalf("failed 文档同 hash 应放行重建, got %v", err)
	}
}

// TestSubmitParseTask_kb不存在拒单
func TestSubmitParseTask_kb不存在拒单(t *testing.T) {
	svc, _, dir := newDocSvcWithStore(t)
	fp := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(fp, []byte("hello"), 0o644)
	if _, _, err := svc.SubmitParseTask("no-such", "a.txt", fp, 5, ""); !errors.Is(err, ErrKBNotFound) {
		t.Fatalf("期望 ErrKBNotFound, got %v", err)
	}
}
