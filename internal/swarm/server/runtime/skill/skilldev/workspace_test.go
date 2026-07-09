package skilldev

import (
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewWorkspaceProvider(t *testing.T) {
	wp := NewWorkspaceProvider("/tmp/test_skilldev")
	if wp.baseDir != "/tmp/test_skilldev" {
		t.Errorf("期望 baseDir=/tmp/test_skilldev, 实际 %s", wp.baseDir)
	}
}

func TestWorkspaceProvider_GetLocalPath(t *testing.T) {
	wp := NewWorkspaceProvider("/base")
	path := wp.GetLocalPath("task_123")
	expected := filepath.Join("/base", "task_123")
	if path != expected {
		t.Errorf("期望 %s, 实际 %s", expected, path)
	}
}

func TestWorkspaceProvider_EnsureLocal(t *testing.T) {
	baseDir := t.TempDir()
	wp := NewWorkspaceProvider(baseDir)

	workspace, err := wp.EnsureLocal("task_123")
	if err != nil {
		t.Fatalf("EnsureLocal 失败: %v", err)
	}

	expectedBase := filepath.Join(baseDir, "task_123")
	if workspace != expectedBase {
		t.Errorf("期望 %s, 实际 %s", expectedBase, workspace)
	}

	// 验证子目录都已创建
	expectedSubDirs := []string{"resources", "skill", "evals", "output"}
	for _, sub := range expectedSubDirs {
		dir := filepath.Join(workspace, sub)
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("子目录 %s 不存在: %v", dir, err)
		} else if !info.IsDir() {
			t.Errorf("期望 %s 是目录", dir)
		}
	}
}

func TestWorkspaceProvider_EnsureLocal_重复调用(t *testing.T) {
	baseDir := t.TempDir()
	wp := NewWorkspaceProvider(baseDir)

	// 第一次调用
	workspace1, err := wp.EnsureLocal("task_1")
	if err != nil {
		t.Fatalf("第一次 EnsureLocal 失败: %v", err)
	}

	// 第二次调用（目录已存在）
	workspace2, err := wp.EnsureLocal("task_1")
	if err != nil {
		t.Fatalf("第二次 EnsureLocal 失败: %v", err)
	}

	if workspace1 != workspace2 {
		t.Errorf("期望两次调用返回相同路径, 实际 %s vs %s", workspace1, workspace2)
	}
}

func TestWorkspaceProvider_SyncToRemote(t *testing.T) {
	wp := NewWorkspaceProvider("/base")

	// 本地实现为空操作，应返回 nil
	err := wp.SyncToRemote("task_123")
	if err != nil {
		t.Errorf("期望 nil 错误, 实际 %v", err)
	}
}

func TestWorkspaceProvider_EnsureLocal_不同任务隔离(t *testing.T) {
	baseDir := t.TempDir()
	wp := NewWorkspaceProvider(baseDir)

	ws1, err := wp.EnsureLocal("task_a")
	if err != nil {
		t.Fatalf("EnsureLocal task_a 失败: %v", err)
	}

	ws2, err := wp.EnsureLocal("task_b")
	if err != nil {
		t.Fatalf("EnsureLocal task_b 失败: %v", err)
	}

	if ws1 == ws2 {
		t.Error("期望不同任务的工作区路径不同")
	}

	// 验证两个工作区都存在
	for _, ws := range []string{ws1, ws2} {
		skillDir := filepath.Join(ws, "skill")
		if _, err := os.Stat(skillDir); os.IsNotExist(err) {
			t.Errorf("工作区 %s 的 skill 子目录不存在", ws)
		}
	}
}
