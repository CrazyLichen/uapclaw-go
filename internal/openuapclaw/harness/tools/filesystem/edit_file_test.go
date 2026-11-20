package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── fileExistsCheck 测试 ────────────────────────────

// TestFileExistsCheck_存在文件 测试存在的文件
func TestFileExistsCheck_存在文件(t *testing.T) {
	// 创建临时文件
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(tmpFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileExistsCheck(tmpFile) {
		t.Error("已创建的文件应存在")
	}
}

// TestFileExistsCheck_不存在文件 测试不存在的文件
func TestFileExistsCheck_不存在文件(t *testing.T) {
	if fileExistsCheck("/nonexistent/path/file.txt") {
		t.Error("不存在的文件应返回 false")
	}
}

// TestFileExistsCheck_存在目录 测试存在的目录
func TestFileExistsCheck_存在目录(t *testing.T) {
	tmpDir := t.TempDir()
	if !fileExistsCheck(tmpDir) {
		t.Error("已创建的目录应存在")
	}
}
