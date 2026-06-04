package main

import (
	"bytes"
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

	// 验证所有 8 个子命令都出现在帮助输出中
	expectedCommands := []string{"chat", "serve", "app", "agentserver", "gateway", "web", "init", "acp"}
	for _, cmd := range expectedCommands {
		if !strings.Contains(output, cmd) {
			t.Errorf("--help 输出未包含子命令 %q", cmd)
		}
	}
}

// TestRootCommand 验证根命令基本信息
func TestRootCommand(t *testing.T) {
	rootCmd := newRootCmd()

	if rootCmd.Use != "uapclaw" {
		t.Errorf("根命令 Use 期望 'uapclaw', 实际 '%s'", rootCmd.Use)
	}

	if rootCmd.Version != version.Version {
		t.Errorf("根命令 Version 期望 '%s', 实际 '%s'", version.Version, rootCmd.Version)
	}
}

// TestSubcommands 验证所有子命令已注册
func TestSubcommands(t *testing.T) {
	rootCmd := newRootCmd()

	expectedCommands := []string{"chat", "serve", "app", "agentserver", "gateway", "web", "init", "acp"}
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
