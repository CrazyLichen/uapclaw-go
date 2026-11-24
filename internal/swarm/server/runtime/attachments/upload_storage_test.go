package attachments

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAtomicWriteUnique_并发同名不覆盖 对齐 atomic_write_unique 的 O_EXCL 语义。
// Python: upload_storage.py:108-152。
func TestAtomicWriteUnique_并发同名不覆盖(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cat.png")
	got1, err := AtomicWriteUnique(target, []byte("first"))
	if err != nil {
		t.Fatalf("首写失败: %v", err)
	}
	if got1 != target {
		t.Fatalf("首写应占用原名: %q", got1)
	}
	got2, err := AtomicWriteUnique(target, []byte("second"))
	if err != nil {
		t.Fatalf("并写失败: %v", err)
	}
	if got2 != filepath.Join(dir, "cat-1.png") {
		t.Fatalf("并写应落 cat-1.png: %q", got2)
	}
	// 原文件不被覆盖
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "first" {
		t.Fatalf("原文件被覆盖: %q %v", b, err)
	}
}

// TestAtomicWriteUnique_并发goroutine竞态 O_EXCL 保证两写各占各槽。
func TestAtomicWriteUnique_并发goroutine竞态(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dup.bin")
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := AtomicWriteUnique(target, []byte("x"))
			if err != nil {
				t.Errorf("并写失败: %v", err)
				return
			}
			mu.Lock()
			seen[got]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(seen) != 8 {
		t.Fatalf("8 并发应产生 8 个不同路径，实际 %d", len(seen))
	}
}

// TestAtomicWriteUnique_自动建目录 target 父目录不存在时 mkdir -p。
func TestAtomicWriteUnique_自动建目录(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "deep", "f.txt")
	got, err := AtomicWriteUnique(target, []byte("v"))
	if err != nil || got != target {
		t.Fatalf("写失败: %q %v", got, err)
	}
}

// TestUniqueUploadPath_stemN变体 对齐 unique_upload_path。
// Python: upload_storage.py:96-105。
func TestUniqueUploadPath_stemN变体(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "f.txt")
	if got := UniqueUploadPath(base); got != base {
		t.Fatalf("空闲时应返回原路径: %q", got)
	}
	if err := os.WriteFile(base, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := UniqueUploadPath(base); got != filepath.Join(dir, "f-1.txt") {
		t.Fatalf("占用时应返回 f-1.txt: %q", got)
	}
}

// TestReadSessionUploadChunk_边界矩阵 对齐 read_session_upload_chunk 错误语义。
// Python: upload_storage.py:26-93。
func TestReadSessionUploadChunk_边界矩阵(t *testing.T) {
	sid := "itest-chunk-sess"
	uploads := filepath.Join(t.TempDir(), "agent", "sessions", sid, "uploads")
	t.Setenv("UAPCLAW_DATA_DIR", filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(uploads)))))
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	content := make([]byte, UploadReadChunkBytes+10)
	for i := range content {
		content[i] = byte(i % 251)
	}
	name := "big.bin"
	if err := os.WriteFile(filepath.Join(uploads, name), content, 0o644); err != nil {
		t.Fatal(err)
	}

	// 正常分块
	got, err := ReadSessionUploadChunk(sid, filepath.Join(uploads, name), 0, UploadReadChunkBytes)
	if err != nil {
		t.Fatalf("正常分块失败: %v", err)
	}
	if got["size"].(int64) != int64(len(content)) || got["eof"] != false || got["chunk_size"].(int) != UploadReadChunkBytes {
		t.Fatalf("首块形状不符: %v", got)
	}
	// 末块 eof=true
	tailOff := len(content) - 10
	got, err = ReadSessionUploadChunk(sid, filepath.Join(uploads, name), tailOff, UploadReadChunkBytes)
	if err != nil {
		t.Fatalf("末块失败: %v", err)
	}
	if got["eof"] != true || got["chunk_size"].(int) != 10 {
		t.Fatalf("末块形状不符: %v", got)
	}

	// 错误矩阵
	cases := []struct {
		name   string
		sid    string
		path   string
		offset int
		limit  int
		want   error
	}{
		{"会话名非法", "../evil", filepath.Join(uploads, name), 0, 10, ErrInvalidUploadRequest},
		{"offset 负数", sid, filepath.Join(uploads, name), -1, 10, ErrInvalidUploadRequest},
		{"limit 零", sid, filepath.Join(uploads, name), 0, 0, ErrInvalidUploadRequest},
		{"limit 超上限", sid, filepath.Join(uploads, name), 0, UploadReadChunkBytes + 1, ErrInvalidUploadRequest},
		{"相对路径", sid, "rel.bin", 0, 10, ErrInvalidUploadRequest},
		{"跨会话目录", sid, filepath.Join(t.TempDir(), "other.bin"), 0, 10, ErrUploadAccessDenied},
		{"文件名点点", sid, filepath.Join(uploads, "..", "x"), 0, 10, ErrUploadAccessDenied},
		{"offset 超文件大小", sid, filepath.Join(uploads, name), len(content) + 1, 10, ErrInvalidUploadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ReadSessionUploadChunk(tc.sid, tc.path, tc.offset, tc.limit); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	// symlink 拒绝
	if err := os.Symlink(filepath.Join(uploads, name), filepath.Join(uploads, "link.bin")); err != nil {
		t.Skipf("symlink 不可用: %v", err)
	}
	if _, err := ReadSessionUploadChunk(sid, filepath.Join(uploads, "link.bin"), 0, 10); !errors.Is(err, ErrUploadAccessDenied) {
		t.Fatalf("symlink 应拒绝: %v", err)
	}
}
