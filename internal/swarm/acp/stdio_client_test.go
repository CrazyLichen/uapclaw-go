package acp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// peerScript 返回测试对端脚本路径（Windows 跳过全部 stdio 用例）。
func peerScript(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("testdata shell 脚本不支持 Windows")
	}
	return "./testdata/acp_peer.sh"
}

// TestAcpStdioClient_Connect握手 验证 initialize + session/new 握手与 sessionId 提取。
func TestAcpStdioClient_Connect握手(t *testing.T) {
	c := NewAcpStdioClient(peerScript(t), nil, "", nil)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	if got := c.SessionID(); got != "peer-sess-1" {
		t.Errorf("sessionID = %q", got)
	}
	if !c.IsConnected() {
		t.Error("应已连接")
	}
}

// TestAcpStdioClient_Chat聚合 验证 session/update 文本聚合与对端请求回写。
func TestAcpStdioClient_Chat聚合(t *testing.T) {
	c := NewAcpStdioClient(peerScript(t), nil, "", nil)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	out, err := c.Chat(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out != "peer-reply" {
		t.Errorf("聚合文本 = %q", out)
	}
}

// TestAcpStdioClient_Chat超时 验证超时报错。
func TestAcpStdioClient_Chat超时(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("testdata shell 脚本不支持 Windows")
	}
	// 用 sleep 型脚本：完成握手后对 session/prompt 延迟超过超时
	dir := t.TempDir()
	script := filepath.Join(dir, "slow_peer.sh")
	_ = os.WriteFile(script, []byte(`#!/bin/sh
while IFS= read -r line; do
    method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
    rid=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
    case "$method" in
        initialize)
            printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$rid"
            ;;
        session/new)
            printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"slow-sess-1"}}\n' "$rid"
            ;;
        *)
            sleep 30
            ;;
    esac
done
`), 0o755)
	c := NewAcpStdioClient(script, nil, "", nil)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	_, err := c.Chat(context.Background(), "hi", WithChatTimeout(300*time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "timed out or EOF") {
		t.Errorf("应报超时错误，实际 %v", err)
	}
}

// TestAcpStdioClient_未连接Chat报错 验证未连接守卫。
func TestAcpStdioClient_未连接Chat报错(t *testing.T) {
	c := NewAcpStdioClient("whatever", nil, "", nil)
	_, err := c.Chat(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("应报 not connected，实际 %v", err)
	}
}

// TestAcpStdioClient_Closed后Chat报错 验证 closed 守卫。
func TestAcpStdioClient_Closed后Chat报错(t *testing.T) {
	c := NewAcpStdioClient(peerScript(t), nil, "", nil)
	_ = c.Close()
	_, err := c.Chat(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("应报 closed，实际 %v", err)
	}
}

// TestAcpStdioClient_未连接Connect失败传播 验证 spawn 失败时 Connect 报错。
func TestAcpStdioClient_未连接Connect失败传播(t *testing.T) {
	c := NewAcpStdioClient("/nonexistent/peer-99.sh", nil, "", nil)
	if err := c.Connect(context.Background()); err == nil {
		t.Error("spawn 失败应报错")
	}
	if c.IsConnected() {
		t.Error("失败后不应连接")
	}
}

// TestAcpStdioClient_Close幂等 验证三段关闭幂等且断开状态。
func TestAcpStdioClient_Close幂等(t *testing.T) {
	c := NewAcpStdioClient(peerScript(t), nil, "", nil)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("第一次 Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("第二次 Close 应幂等: %v", err)
	}
	if c.IsConnected() {
		t.Error("Close 后不应连接")
	}
}

// TestAcpStdioClient_Close未启动也安全 验证未启动 Close 不崩溃。
func TestAcpStdioClient_Close未启动也安全(t *testing.T) {
	c := NewAcpStdioClient("whatever", nil, "", nil)
	if err := c.Close(); err != nil {
		t.Errorf("未启动 Close: %v", err)
	}
}

// TestAcpStdioClient_Closed后Connect报错 验证 closed 后拒绝重连。
func TestAcpStdioClient_Closed后Connect报错(t *testing.T) {
	c := NewAcpStdioClient(peerScript(t), nil, "", nil)
	_ = c.Close()
	if err := c.Connect(context.Background()); err == nil {
		t.Error("closed 后 Connect 应报错")
	}
}

// TestAcpStdioClient_空command报错 验证空命令校验。
func TestAcpStdioClient_空command报错(t *testing.T) {
	c := NewAcpStdioClient("  ", nil, "", nil)
	if err := c.Connect(context.Background()); err == nil {
		t.Error("空 command 应报错")
	}
}

// TestAcpStdioClient_Chat后IsConnected 验证子进程已退出时 IsConnected=false。
func TestAcpStdioClient_Chat后IsConnected(t *testing.T) {
	c := NewAcpStdioClient(peerScript(t), nil, "", nil)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	if _, err := c.Chat(context.Background(), "hi"); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	// 对端循环仍存活（等待下一行），连接应保持
	if !c.IsConnected() {
		t.Error("对端存活时应保持连接")
	}
}
