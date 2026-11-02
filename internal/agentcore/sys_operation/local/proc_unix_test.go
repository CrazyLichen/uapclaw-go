//go:build !windows

package local

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestApplyProcessGroup_设置独立进程组 测试 Unix 下 Setpgid 被置位
func TestApplyProcessGroup_设置独立进程组(t *testing.T) {
	cmd := exec.Command("true")
	applyProcessGroup(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr 不应为 nil")
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid 应为 true（子进程独立成组）")
	}

	// 已有 SysProcAttr 时只补字段，不整体替换
	existing := &syscall.SysProcAttr{}
	cmd2 := exec.Command("true")
	cmd2.SysProcAttr = existing
	applyProcessGroup(cmd2)
	if cmd2.SysProcAttr != existing {
		t.Error("已有 SysProcAttr 应原地补字段，不应新建")
	}
}

// TestKillProcessTreePlatform_终止进程 测试进程组终止路径真实杀掉子进程
func TestKillProcessTreePlatform_终止进程(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	applyProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动 sleep 失败: %v", err)
	}
	handler := &AsyncProcessHandler{cmd: cmd}

	handler.killProcessTree()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("killProcessTreePlatform 未能终止子进程")
	}
}

// TestKillProcessTreePlatform_未启动进程 测试 cmd.Process 为 nil 时直接返回
func TestKillProcessTreePlatform_未启动进程(t *testing.T) {
	handler := &AsyncProcessHandler{cmd: exec.Command("true")}
	handler.killProcessTree() // 不应 panic
}
