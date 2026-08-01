package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSharedMemoryManager_EnsureDir 验证目录创建
func TestSharedMemoryManager_EnsureDir(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	if err := mgr.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "team-memory")); os.IsNotExist(err) {
		t.Errorf("Directory not created")
	}
}

// TestSharedMemoryManager_ReadTeamSummary_空文件 验证不存在时返回空字符串
func TestSharedMemoryManager_ReadTeamSummary_空文件(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	_ = mgr.EnsureDir()
	result := mgr.ReadTeamSummary(context.Background())
	if result != "" {
		t.Errorf("Expected empty string for nonexistent file, got %q", result)
	}
}

// TestSharedMemoryManager_WriteAndRead 验证写入后读取
func TestSharedMemoryManager_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	_ = mgr.EnsureDir()

	content := "# 团队共享记忆\n\n### [decision] 选择了方案 A"
	if err := mgr.WriteTeamSummary(context.Background(), content); err != nil {
		t.Fatalf("WriteTeamSummary failed: %v", err)
	}
	result := mgr.ReadTeamSummary(context.Background())
	if result != content {
		t.Errorf("Expected %q, got %q", content, result)
	}
}

// TestSharedMemoryManager_AppendEntry 验证追加条目
func TestSharedMemoryManager_AppendEntry(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	_ = mgr.EnsureDir()

	if err := mgr.WriteTeamSummary(context.Background(), "初始内容"); err != nil {
		t.Fatalf("WriteTeamSummary failed: %v", err)
	}
	if err := mgr.AppendEntry(context.Background(), "追加条目"); err != nil {
		t.Fatalf("AppendEntry failed: %v", err)
	}
	result := mgr.ReadTeamSummary(context.Background())
	if !strings.Contains(result, "初始内容") || !strings.Contains(result, "追加条目") {
		t.Errorf("Expected both contents, got %q", result)
	}
	if !strings.Contains(result, "---") {
		t.Errorf("Expected separator ---, got %q", result)
	}
}

// TestSharedMemoryManager_AppendEntry_空初始 验证追加到空文件
func TestSharedMemoryManager_AppendEntry_空初始(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	_ = mgr.EnsureDir()

	if err := mgr.AppendEntry(context.Background(), "首条内容"); err != nil {
		t.Fatalf("AppendEntry failed: %v", err)
	}
	result := mgr.ReadTeamSummary(context.Background())
	if result != "首条内容" {
		t.Errorf("Expected %q, got %q", "首条内容", result)
	}
}
