package main

// 10.4.6 ACP CLI 冒烟入口测试。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestExitCodeFor 退出码解析全分支（0/1/2/130/未知/包装）。
func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"配置错", &acpChatExitError{code: 2}, 2},
		{"会话错", &acpChatExitError{code: 1}, 1},
		{"中断", &acpChatExitError{code: 130}, 130},
		{"包装后仍可取码", fmt.Errorf("wrap: %w", &acpChatExitError{code: 2}), 2},
		{"普通错误回落 1", errors.New("boom"), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.err); got != tc.want {
				t.Errorf("exitCodeFor = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestLoadAcpChatConfigFrom 从显式路径加载 config.yaml（不碰默认工作区）。
func TestLoadAcpChatConfigFrom(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := "acp_agents:\n  codex:\n    command: codex\n    args:\n      - exec\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("写临时 config.yaml 失败: %v", err)
	}

	raw, err := loadAcpChatConfigFrom(cfgPath, "")
	if err != nil {
		t.Fatalf("loadAcpChatConfigFrom 失败: %v", err)
	}
	agents, ok := raw["acp_agents"].(map[string]any)
	if !ok {
		t.Fatalf("acp_agents 类型不符: %#v", raw["acp_agents"])
	}
	if _, ok := agents["codex"]; !ok {
		t.Errorf("未解析出 codex profile: %#v", agents)
	}
}

// TestLoadAcpChatConfigFrom_文件缺失不报错 缺文件 → 空 map（对齐 config.Load 语义）。
func TestLoadAcpChatConfigFrom_文件缺失不报错(t *testing.T) {
	raw, err := loadAcpChatConfigFrom(filepath.Join(t.TempDir(), "nope.yaml"), "")
	if err != nil {
		t.Fatalf("期望不报错, 实际 %v", err)
	}
	if len(raw) != 0 {
		t.Errorf("期望空配置, 实际 %#v", raw)
	}
}

// TestLoadAcpChatConfigFrom_加载dotenv envFile 指向真实文件时写入环境变量。
func TestLoadAcpChatConfigFrom_加载dotenv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("ACP_CHAT_TEST_KEY=xyz\n"), 0o600); err != nil {
		t.Fatalf("写临时 .env 失败: %v", err)
	}
	os.Unsetenv("ACP_CHAT_TEST_KEY")
	t.Cleanup(func() { os.Unsetenv("ACP_CHAT_TEST_KEY") })

	if _, err := loadAcpChatConfigFrom(filepath.Join(dir, "nope.yaml"), envFile); err != nil {
		t.Fatalf("loadAcpChatConfigFrom 失败: %v", err)
	}
	if got := os.Getenv("ACP_CHAT_TEST_KEY"); got != "xyz" {
		t.Errorf("dotenv 未生效, ACP_CHAT_TEST_KEY = %q", got)
	}
}

// TestAcpChatSkipDotenv 只认 "1"（对齐 Python strip()=="1"）。
func TestAcpChatSkipDotenv(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"1", true},
		{" 1 ", true},
		{"0", false},
		{"", false},
		{"true", false},
	}
	for _, tc := range cases {
		getenv := func(string) string { return tc.val }
		if got := acpChatSkipDotenv(getenv); got != tc.want {
			t.Errorf("acpChatSkipDotenv(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
	if acpChatSkipDotenv(nil) {
		t.Error("getenv nil 应返回 false")
	}
}
