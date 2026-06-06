package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/version"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestVersionFlag 验证 --version 输出包含版本号
func TestVersionFlag(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"--version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("执行 --version 失败: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, version.Version) {
		t.Errorf("--version 输出未包含版本号 %q, 实际输出: %s", version.Version, output)
	}
}

// TestHelpFlag 验证 --help 显示所有子命令
func TestHelpFlag(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd := newRootCmd()
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("执行 --help 失败: %v", err)
	}

	output := buf.String()

	// 验证 2 个子命令都出现在帮助输出中
	expectedCommands := []string{"serve", "run"}
	for _, cmd := range expectedCommands {
		if !strings.Contains(output, cmd) {
			t.Errorf("--help 输出未包含子命令 %q", cmd)
		}
	}
}

// TestRootCommand 验证根命令基本信息
func TestRootCommand(t *testing.T) {
	rootCmd := newRootCmd()

	if rootCmd.Use != "jiuwenbox" {
		t.Errorf("根命令 Use 期望 'jiuwenbox', 实际 '%s'", rootCmd.Use)
	}

	if rootCmd.Version != version.Version {
		t.Errorf("根命令 Version 期望 '%s', 实际 '%s'", version.Version, rootCmd.Version)
	}
}

// TestSubcommands 验证所有子命令已注册
func TestSubcommands(t *testing.T) {
	rootCmd := newRootCmd()

	expectedCommands := []string{"serve", "run"}
	for _, name := range expectedCommands {
		found := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("子命令 %q 未注册", name)
		}
	}
}

// TestServeCmd_Execute 验证 serve 子命令执行输出
func TestServeCmd_Execute(t *testing.T) {
	buf := captureStdout(t, func() {
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"serve"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("执行 serve 失败: %v", err)
		}
	})

	if !strings.Contains(buf, "尚未实现") {
		t.Errorf("serve 输出未包含 '尚未实现', 实际输出: %s", buf)
	}
}

// TestRunCmd_Execute 验证 run 子命令执行输出
func TestRunCmd_Execute(t *testing.T) {
	buf := captureStdout(t, func() {
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"run"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("执行 run 失败: %v", err)
		}
	})

	if !strings.Contains(buf, "尚未实现") {
		t.Errorf("run 输出未包含 '尚未实现', 实际输出: %s", buf)
	}
}

// TestServeCmd_Info 验证 serve 命令的 Use/Short 字段
func TestServeCmd_Info(t *testing.T) {
	cmd := newServeCmd()

	if cmd.Use != "serve" {
		t.Errorf("serve 命令 Use 期望 'serve', 实际 '%s'", cmd.Use)
	}
	if cmd.Short != "启动沙箱 HTTP API 服务" {
		t.Errorf("serve 命令 Short 期望 '启动沙箱 HTTP API 服务', 实际 '%s'", cmd.Short)
	}
}

// TestRunCmd_Info 验证 run 命令的 Use/Short 字段
func TestRunCmd_Info(t *testing.T) {
	cmd := newRunCmd()

	if cmd.Use != "run [命令]" {
		t.Errorf("run 命令 Use 期望 'run [命令]', 实际 '%s'", cmd.Use)
	}
	if cmd.Short != "在沙箱中运行指定命令" {
		t.Errorf("run 命令 Short 期望 '在沙箱中运行指定命令', 实际 '%s'", cmd.Short)
	}
}

// captureStdout 捕获 os.Stdout 输出（fmt.Println 写入 os.Stdout，不经过 cobra SetOut）
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建 pipe 失败: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()
	w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("读取 pipe 失败: %v", err)
	}
	r.Close()

	return buf.String()
}
