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

// TestChatCmd_Execute 验证 chat 子命令执行输出
func TestChatCmd_Execute(t *testing.T) {
	buf := captureStdout(t, func() {
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"chat"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("执行 chat 失败: %v", err)
		}
	})

	if !strings.Contains(buf, "尚未实现") {
		t.Errorf("chat 输出未包含 '尚未实现', 实际输出: %s", buf)
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

// TestAppCmd_Execute 验证 app 子命令执行输出
func TestAppCmd_Execute(t *testing.T) {
	buf := captureStdout(t, func() {
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"app"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("执行 app 失败: %v", err)
		}
	})

	if !strings.Contains(buf, "尚未实现") {
		t.Errorf("app 输出未包含 '尚未实现', 实际输出: %s", buf)
	}
}

// TestAgentServerCmd_Execute 验证 agentserver 子命令执行输出
func TestAgentServerCmd_Execute(t *testing.T) {
	buf := captureStdout(t, func() {
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"agentserver"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("执行 agentserver 失败: %v", err)
		}
	})

	if !strings.Contains(buf, "尚未实现") {
		t.Errorf("agentserver 输出未包含 '尚未实现', 实际输出: %s", buf)
	}
}

// TestGatewayCmd_Execute 验证 gateway 子命令执行输出
func TestGatewayCmd_Execute(t *testing.T) {
	buf := captureStdout(t, func() {
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"chat"})
		rootCmd.SetArgs([]string{"gateway"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("执行 gateway 失败: %v", err)
		}
	})

	if !strings.Contains(buf, "尚未实现") {
		t.Errorf("gateway 输出未包含 '尚未实现', 实际输出: %s", buf)
	}
}

// TestWebCmd_Execute 验证 web 子命令执行输出
func TestWebCmd_Execute(t *testing.T) {
	buf := captureStdout(t, func() {
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"web"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("执行 web 失败: %v", err)
		}
	})

	if !strings.Contains(buf, "尚未实现") {
		t.Errorf("web 输出未包含 '尚未实现', 实际输出: %s", buf)
	}
}

// TestInitCmd_Execute 验证 init 子命令执行输出
func TestInitCmd_Execute(t *testing.T) {
	buf := captureStdout(t, func() {
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"init"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("执行 init 失败: %v", err)
		}
	})

	if !strings.Contains(buf, "尚未实现") {
		t.Errorf("init 输出未包含 '尚未实现', 实际输出: %s", buf)
	}
}

// TestAcpCmd_Execute 验证 acp 子命令执行输出
func TestAcpCmd_Execute(t *testing.T) {
	buf := captureStdout(t, func() {
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"acp"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("执行 acp 失败: %v", err)
		}
	})

	if !strings.Contains(buf, "尚未实现") {
		t.Errorf("acp 输出未包含 '尚未实现', 实际输出: %s", buf)
	}
}

// TestMakeDotenvPreRunE_无参数 验证无 --dotenv/--name 时不报错
func TestMakeDotenvPreRunE_无参数(t *testing.T) {
	rootCmd := newRootCmd()
	chatCmd, _, err := rootCmd.Find([]string{"chat"})
	if err != nil {
		t.Fatalf("查找 chat 子命令失败: %v", err)
	}

	preRunE := makeDotenvPreRunE()
	err = preRunE(chatCmd, nil)
	if err != nil {
		t.Errorf("无 --dotenv/--name 时期望返回 nil, 实际返回: %v", err)
	}
}

// TestMakeDotenvPreRunE_无效dotenv 验证无效 dotenv 路径时返回错误
func TestMakeDotenvPreRunE_无效dotenv(t *testing.T) {
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"--dotenv", "/nonexistent/path/.env", "chat"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("无效 dotenv 路径时期望返回错误, 实际返回 nil")
	}
}

// TestRootCommand_PersistentFlags 验证全局参数已注册
func TestRootCommand_PersistentFlags(t *testing.T) {
	rootCmd := newRootCmd()

	dotenvFlag := rootCmd.PersistentFlags().Lookup("dotenv")
	if dotenvFlag == nil {
		t.Error("根命令未注册 --dotenv 全局参数")
	}

	nameFlag := rootCmd.PersistentFlags().Lookup("name")
	if nameFlag == nil {
		t.Error("根命令未注册 --name 全局参数")
	}
}

// TestSubcommandsHavePreRunE 验证带 PreRunE 的子命令
func TestSubcommandsHavePreRunE(t *testing.T) {
	rootCmd := newRootCmd()

	// 以下子命令应设置 PreRunE
	withPreRunE := []string{"chat", "serve", "app", "agentserver", "gateway", "web", "acp"}
	for _, name := range withPreRunE {
		found := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == name {
				if cmd.PreRunE == nil {
					t.Errorf("子命令 %q 未设置 PreRunE", name)
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("子命令 %q 未注册", name)
		}
	}

	// init 子命令不应设置 PreRunE
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "init" {
			if cmd.PreRunE != nil {
				t.Error("init 子命令不应设置 PreRunE")
			}
			break
		}
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
	_ = w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("读取 pipe 失败: %v", err)
	}
	_ = r.Close()

	return buf.String()
}
