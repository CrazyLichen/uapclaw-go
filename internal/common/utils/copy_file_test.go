// copy_file.go 单元测试。
package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFile_内容权限与mtime(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "nested", "dst.txt")
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile 失败: %v", err)
	}
	raw, _ := os.ReadFile(dst)
	if string(raw) != "hello" {
		t.Fatalf("内容不符: %q", raw)
	}
	info, _ := os.Stat(dst)
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("权限未保留: %v", info.Mode().Perm())
	}
	srcInfo, _ := os.Stat(src)
	if !info.ModTime().Equal(srcInfo.ModTime()) {
		t.Fatal("mtime 未保留（shutil.copy2 语义）")
	}
}

func TestCopyFile_源缺失报错(t *testing.T) {
	dir := t.TempDir()
	if err := CopyFile(filepath.Join(dir, "nope"), filepath.Join(dir, "dst")); err == nil {
		t.Fatal("源缺失必须报错（Python OSError）")
	}
}
