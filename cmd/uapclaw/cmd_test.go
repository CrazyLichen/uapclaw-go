package main

import (
	"path/filepath"
	"testing"
)

// TestReadExtensionDirs_分号分割 测试 ';' 分割与 strip
func TestReadExtensionDirs_分号分割(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	got := readExtensionDirs(map[string]any{"extension_dirs": dir1 + " ; " + dir2})
	if len(got) != 2 {
		t.Fatalf("dirs = %v, 期望 2", got)
	}
	// 相对路径转绝对（按 cwd）
	got2 := readExtensionDirs(map[string]any{"extension_dirs": "./relative/path"})
	if len(got2) != 1 {
		t.Fatalf("dirs2 = %v, 期望 1", got2)
	}
	if !filepath.IsAbs(got2[0]) {
		t.Errorf("相对路径应转绝对: %q", got2[0])
	}
}

// TestReadExtensionDirs_非字符串与缺失 测试仅支持字符串（对齐 Python）
func TestReadExtensionDirs_非字符串与缺失(t *testing.T) {
	if got := readExtensionDirs(nil); len(got) != 0 {
		t.Errorf("nil 应返回空: %v", got)
	}
	if got := readExtensionDirs(map[string]any{}); len(got) != 0 {
		t.Errorf("缺失段应返回空: %v", got)
	}
	if got := readExtensionDirs(map[string]any{"extension_dirs": []string{"/a"}}); len(got) != 0 {
		t.Errorf("非字符串应返回空（对齐 Python isinstance(str) 检查）: %v", got)
	}
	if got := readExtensionDirs("不是map"); len(got) != 0 {
		t.Errorf("extensions 非 map 应返回空: %v", got)
	}
}

// TestReadExtensionDirs_空段丢弃 测试空字符串段被过滤
func TestReadExtensionDirs_空段丢弃(t *testing.T) {
	got := readExtensionDirs(map[string]any{"extension_dirs": "/a;; ;/b"})
	if len(got) != 2 {
		t.Errorf("dirs = %v, 期望 2（空段丢弃）", got)
	}
}
