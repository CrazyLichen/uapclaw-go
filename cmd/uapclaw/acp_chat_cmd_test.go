package main

// 10.4.6 ACP CLI 冒烟入口测试。

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/acp"
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

// fakeAcpChatDeps 构造注入依赖；连接/对话行为由参数决定。
func fakeAcpChatDeps(raw map[string]any, connectErr, chatErr error, out string) (acpChatDeps, *bytes.Buffer) {
	buf := new(bytes.Buffer)
	return acpChatDeps{
		loadConfig: func(context.Context) (map[string]any, error) { return raw, nil },
		connect:    func(context.Context, *acp.AcpStdioClient) error { return connectErr },
		chat:       func(context.Context, *acp.AcpStdioClient, string) (string, error) { return out, chatErr },
		stdout:     buf,
		getenv:     func(string) string { return "" },
	}, buf
}

// validAcpAgentsRaw 合法 profile 原始配置。
func validAcpAgentsRaw() map[string]any {
	return map[string]any{
		"acp_agents": map[string]any{
			"codex": map[string]any{
				"command": "sh",
				"args":    []any{"-c", "cat"},
			},
		},
	}
}

// TestRunAcpChat_退出码矩阵 覆盖解析错/连接错/对话错/成功/空响应/信号取消。
func TestRunAcpChat_退出码矩阵(t *testing.T) {
	ctx := context.Background()

	t.Run("配置加载失败", func(t *testing.T) {
		deps, _ := fakeAcpChatDeps(nil, nil, nil, "")
		deps.loadConfig = func(context.Context) (map[string]any, error) {
			return nil, errors.New("boom")
		}
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeConfig {
			t.Errorf("码 = %d, want %d", got, exitCodeConfig)
		}
	})

	t.Run("无acp_agents", func(t *testing.T) {
		deps, _ := fakeAcpChatDeps(map[string]any{}, nil, nil, "")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeConfig {
			t.Errorf("码 = %d, want %d", got, exitCodeConfig)
		}
	})

	t.Run("未知profile", func(t *testing.T) {
		deps, _ := fakeAcpChatDeps(validAcpAgentsRaw(), nil, nil, "")
		if got := runAcpChat(ctx, deps, "nope", "hi"); got != exitCodeConfig {
			t.Errorf("码 = %d, want %d", got, exitCodeConfig)
		}
	})

	t.Run("command为空", func(t *testing.T) {
		raw := map[string]any{"acp_agents": map[string]any{"codex": map[string]any{"command": "  "}}}
		deps, _ := fakeAcpChatDeps(raw, nil, nil, "")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeConfig {
			t.Errorf("码 = %d, want %d", got, exitCodeConfig)
		}
	})

	t.Run("args非列表_严格", func(t *testing.T) {
		raw := map[string]any{"acp_agents": map[string]any{
			"codex": map[string]any{"command": "sh", "args": "not-a-list"}}}
		deps, _ := fakeAcpChatDeps(raw, nil, nil, "")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeConfig {
			t.Errorf("严格解析应返回 %d, 实际 %d", exitCodeConfig, got)
		}
	})

	t.Run("spec键带空格", func(t *testing.T) {
		deps, buf := fakeAcpChatDeps(validAcpAgentsRaw(), nil, nil, "ok")
		if got := runAcpChat(ctx, deps, "  codex  ", "hi"); got != exitCodeOK {
			t.Errorf("profile 应 trim, 码 = %d", got)
		}
		if buf.String() != "ok\n" {
			t.Errorf("stdout = %q", buf.String())
		}
	})

	t.Run("连接失败", func(t *testing.T) {
		deps, buf := fakeAcpChatDeps(validAcpAgentsRaw(), errors.New("connect boom"), nil, "")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeSession {
			t.Errorf("码 = %d, want %d", got, exitCodeSession)
		}
		if buf.Len() != 0 {
			t.Errorf("失败不应写 stdout, 实际 %q", buf.String())
		}
	})

	t.Run("对话失败", func(t *testing.T) {
		deps, _ := fakeAcpChatDeps(validAcpAgentsRaw(), nil, errors.New("chat boom"), "")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeSession {
			t.Errorf("码 = %d, want %d", got, exitCodeSession)
		}
	})

	t.Run("成功", func(t *testing.T) {
		deps, buf := fakeAcpChatDeps(validAcpAgentsRaw(), nil, nil, "hello there")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeOK {
			t.Errorf("码 = %d, want %d", got, exitCodeOK)
		}
		if buf.String() != "hello there\n" {
			t.Errorf("stdout = %q", buf.String())
		}
	})

	t.Run("空响应占位", func(t *testing.T) {
		deps, buf := fakeAcpChatDeps(validAcpAgentsRaw(), nil, nil, "")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeOK {
			t.Errorf("码 = %d, want %d", got, exitCodeOK)
		}
		if buf.String() != "(empty response)\n" {
			t.Errorf("stdout = %q", buf.String())
		}
	})

	t.Run("信号取消", func(t *testing.T) {
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		deps, _ := fakeAcpChatDeps(validAcpAgentsRaw(), nil, context.Canceled, "")
		if got := runAcpChat(canceled, deps, "codex", "hi"); got != exitCodeInterrupted {
			t.Errorf("码 = %d, want %d", got, exitCodeInterrupted)
		}
	})
}

// TestAcpChatCloseTimeout 关闭超时解析（缺省/非法回落 30s，0 与负数保留）。
func TestAcpChatCloseTimeout(t *testing.T) {
	cases := []struct {
		val  string
		want time.Duration
	}{
		{"", 30 * time.Second},
		{"5", 5 * time.Second},
		{"2.5", 2500 * time.Millisecond},
		{"abc", 30 * time.Second},
		{"-1", 0},
	}
	for _, tc := range cases {
		getenv := func(string) string { return tc.val }
		if got := acpChatCloseTimeout(getenv); got != tc.want {
			t.Errorf("acpChatCloseTimeout(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
	if got := acpChatCloseTimeout(nil); got != 30*time.Second {
		t.Errorf("getenv nil = %v, want 30s", got)
	}
}

// blockingCloser 关闭阻塞的桩，用于超时用例。
type blockingCloser struct{ release chan struct{} }

// Close 阻塞直到 release 被关闭。
func (b *blockingCloser) Close() error {
	<-b.release
	return nil
}

// errorCloser 关闭返回错误的桩。
type errorCloser struct{}

// Close 恒返回错误。
func (e *errorCloser) Close() error { return errors.New("close boom") }

// TestCloseAcpChatClientWithTimeout 超时与错误都不致命。
func TestCloseAcpChatClientWithTimeout(t *testing.T) {
	blocker := &blockingCloser{release: make(chan struct{})}
	start := time.Now()
	closeAcpChatClientWithTimeout(blocker, 20*time.Millisecond)
	close(blocker.release)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("超时未生效, 耗时 %v", elapsed)
	}

	// 错误路径不 panic、不阻塞
	closeAcpChatClientWithTimeout(&errorCloser{}, time.Second)
	// nil 安全
	closeAcpChatClientWithTimeout(nil, time.Second)
}

// TestNewAcpChatCmd_参数校验 参数个数错 → 码 2 的 typed error。
func TestNewAcpChatCmd_参数校验(t *testing.T) {
	cmd := newAcpChatCmd()
	if cmd.Use != "acp-chat <profile> [message]" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if !cmd.SilenceUsage || !cmd.SilenceErrors {
		t.Error("应设置 SilenceUsage/SilenceErrors")
	}

	for _, args := range [][]string{nil, {"a", "b", "c"}} {
		err := cmd.RunE(cmd, args)
		var exitErr *acpChatExitError
		if !errors.As(err, &exitErr) || exitErr.code != exitCodeConfig {
			t.Errorf("args=%v 期望码 %d, 实际 %v", args, exitCodeConfig, err)
		}
	}
}

// TestNewAcpChatCmd_已注册 根命令包含 acp-chat。
func TestNewAcpChatCmd_已注册(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "acp-chat" {
			found = true
			break
		}
	}
	if !found {
		t.Error("根命令未注册 acp-chat")
	}
}

// TestRunAcpChat_E2E假对端 真客户端 + testdata 假 ACP 对端（Windows 跳过，脚本依赖 sh）。
func TestRunAcpChat_E2E假对端(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("acp_peer.sh 依赖 sh")
	}

	// cmd/uapclaw → ../../internal/swarm/acp/testdata/acp_peer.sh
	peer := "../../internal/swarm/acp/testdata/acp_peer.sh"
	if _, err := os.Stat(peer); err != nil {
		t.Fatalf("假对端脚本缺失: %v", err)
	}

	raw := map[string]any{
		"acp_agents": map[string]any{
			"peer": map[string]any{
				"command": "sh",
				"args":    []any{"-c", "sh " + peer},
			},
		},
	}

	buf := new(bytes.Buffer)
	deps := defaultAcpChatDeps()
	deps.loadConfig = func(context.Context) (map[string]any, error) { return raw, nil }
	deps.stdout = buf
	deps.getenv = func(string) string { return "" }

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if code := runAcpChat(ctx, deps, "peer", "hi"); code != exitCodeOK {
		t.Fatalf("码 = %d, want %d", code, exitCodeOK)
	}
	if !strings.Contains(buf.String(), "peer-reply") {
		t.Errorf("stdout = %q, 期望含 peer-reply", buf.String())
	}
}
