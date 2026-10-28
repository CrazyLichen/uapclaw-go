package acp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// peerFsScript 返回 fs 交互测试对端脚本绝对路径（Windows 跳过）。
// 绝对路径：cmd.Dir 设为 root 后相对路径会在子进程 chdir 后失效。
func peerFsScript(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("testdata shell 脚本不支持 Windows")
	}
	abs, err := filepath.Abs("./testdata/acp_peer_fs.sh")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// TestAcpStdioClient_对端fs读写 验证 handlePeerRequest 的 fs 读写分支与响应回写。
func TestAcpStdioClient_对端fs读写(t *testing.T) {
	root := t.TempDir()
	// 准备 root 内文件（3 行，验证 line/limit 切片）
	if err := os.WriteFile(filepath.Join(root, "in_root.txt"),
		[]byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewAcpStdioClient(peerFsScript(t), nil, root, nil)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	if _, err := c.Chat(context.Background(), "hi"); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	// write 分支应已写文件（handlePeerRequest 异步执行，轮询等待）
	deadline := time.Now().Add(3 * time.Second)
	var written []byte
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(root, "written.txt")); err == nil {
			written = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if string(written) != "hello-acp" {
		t.Errorf("written.txt = %q", string(written))
	}
}

// TestHandlePeerRequest_纯函数分支 直接构造消息验证响应行。
func TestHandlePeerRequest_纯函数分支(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "f.txt"), []byte("a\nb\nc\n"), 0o644)
	c := NewAcpStdioClient("unused", nil, root, nil)

	// 在 stdin 未连接状态下验证响应构造：writeResponse 返回 stdin closed 错误不 panic
	c.mu.Lock()
	c.cmd = nil
	c.stdin = nil
	c.mu.Unlock()

	// read 越界路径 → -32001（相对路径按进程 cwd 解析后越界）
	c.handlePeerRequest(map[string]any{
		"jsonrpc": "2.0", "id": float64(1),
		"method":  "fs/read_text_file",
		"params":  map[string]any{"path": "../../outside.txt"},
	})
	// read 不存在文件（root 内）→ ENOENT -32002
	c.handlePeerRequest(map[string]any{
		"jsonrpc": "2.0", "id": float64(2),
		"method":  "fs/read_text_file",
		"params":  map[string]any{"path": filepath.Join(root, "nope.txt")},
	})
	// read 无 path 键（nil）→ 拒绝
	c.handlePeerRequest(map[string]any{
		"jsonrpc": "2.0", "id": float64(8),
		"method":  "fs/read_text_file",
		"params":  map[string]any{},
	})
	// write 越界路径 → -32001
	c.handlePeerRequest(map[string]any{
		"jsonrpc": "2.0", "id": float64(3),
		"method":  "fs/write_text_file",
		"params":  map[string]any{"path": "../x.txt", "content": "x"},
	})
	// write params nil → -32602
	c.handlePeerRequest(map[string]any{
		"jsonrpc": "2.0", "id": float64(4),
		"method":  "fs/write_text_file",
		"params":  "not-a-dict",
	})
	// read params nil → -32602
	c.handlePeerRequest(map[string]any{
		"jsonrpc": "2.0", "id": float64(9),
		"method":  "fs/read_text_file",
		"params":  "not-a-dict",
	})
	// 未知方法 → -32601
	c.handlePeerRequest(map[string]any{
		"jsonrpc": "2.0", "id": float64(5),
		"method":  "some/unknown",
	})
	// read 真实文件（root 内，绝对路径）
	c.handlePeerRequest(map[string]any{
		"jsonrpc": "2.0", "id": float64(6),
		"method":  "fs/read_text_file",
		"params":  map[string]any{"path": filepath.Join(root, "f.txt"), "line": float64(2)},
	})
	// write root 内（绝对路径，真实落盘）
	c.handlePeerRequest(map[string]any{
		"jsonrpc": "2.0", "id": float64(7),
		"method":  "fs/write_text_file",
		"params":  map[string]any{"path": filepath.Join(root, "w.txt"), "content": "ok"},
	})
	b, err := os.ReadFile(filepath.Join(root, "w.txt"))
	if err != nil || string(b) != "ok" {
		t.Errorf("w.txt = %q err=%v", string(b), err)
	}
}

// TestHandlePeerRequest_权限自动应答 验证 request_permission allow-once 分支。
func TestHandlePeerRequest_权限自动应答(t *testing.T) {
	t.Setenv("ACP_AUTO_APPROVE_PERMISSIONS", "1")
	c := NewAcpStdioClient("unused", nil, "", nil)
	c.handlePeerRequest(map[string]any{
		"jsonrpc": "2.0", "id": float64(1),
		"method":  "session/request_permission",
	})
	// 无崩溃即通过（写失败被忽略；日志分支由 autoApprovePermissions 覆盖）
}

// TestHandlePeerRequest_权限取消应答 验证 request_permission cancelled 分支。
func TestHandlePeerRequest_权限取消应答(t *testing.T) {
	t.Setenv("ACP_AUTO_APPROVE_PERMISSIONS", "false")
	if autoApprovePermissions() {
		t.Fatal("false 应关闭自动应答")
	}
	c := NewAcpStdioClient("unused", nil, "", nil)
	c.handlePeerRequest(map[string]any{
		"jsonrpc": "2.0", "id": "perm-1",
		"method":  "session/request_permission",
	})
}

// TestSliceFileLines 行切片语义。
func TestSliceFileLines(t *testing.T) {
	text := "a\r\nb\nc"
	// 全量（splitlines(True) 保留行界符）
	if got := sliceFileLines(text, nil, nil); got != text {
		t.Errorf("全量 = %q", got)
	}
	// line=2 起始到尾（splitlines(True): ["a\r\n","b\n","c"]）
	if got := sliceFileLines(text, float64(2), nil); got != "b\nc" {
		t.Errorf("line2 = %q", got)
	}
	// limit 截断
	if got := sliceFileLines(text, float64(1), float64(2)); got != "a\r\nb\n" {
		t.Errorf("line1+limit2 = %q", got)
	}
	// line<1 钳 1
	if got := sliceFileLines(text, float64(0), nil); got != text {
		t.Errorf("line0 = %q", got)
	}
	// 字符串数字（line=2 limit=1 → 第 2 行 "b\n"）
	if got := sliceFileLines(text, "2", "1"); got != "b\n" {
		t.Errorf("字符串参数 = %q", got)
	}
	// line 超界 → 空
	if got := sliceFileLines(text, float64(99), nil); got != "" {
		t.Errorf("超界 = %q", got)
	}
	// limit<=0 → 到尾
	if got := sliceFileLines(text, float64(1), float64(0)); got != text {
		t.Errorf("limit0 = %q", got)
	}
}

// TestSplitLinesKeepEnds 行界符保留语义。
func TestSplitLinesKeepEnds(t *testing.T) {
	if got := splitLinesKeepEnds(""); got != nil {
		t.Errorf("空串 = %v", got)
	}
	if got := splitLinesKeepEnds("a"); len(got) != 1 || got[0] != "a" {
		t.Errorf("单行 = %v", got)
	}
	if got := splitLinesKeepEnds("a\nb\n"); len(got) != 2 || got[0] != "a\n" || got[1] != "b\n" {
		t.Errorf("尾换行 = %v", got)
	}
	if got := splitLinesKeepEnds("a\nb"); len(got) != 2 || got[1] != "b" {
		t.Errorf("无尾换行 = %v", got)
	}
}

// TestResultTextFallback 结果文本回退。
func TestResultTextFallback(t *testing.T) {
	if got := resultTextFallback(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
	if got := resultTextFallback(map[string]any{"stopReason": "end_turn"}); got != "end_turn" {
		t.Errorf("stopReason = %q", got)
	}
	if got := resultTextFallback(map[string]any{"stop_reason": "max_tokens"}); got != "max_tokens" {
		t.Errorf("stop_reason = %q", got)
	}
	if got := resultTextFallback(map[string]any{"x": 1.0}); !strings.Contains(got, `"x":1`) {
		t.Errorf("序列化 = %q", got)
	}
	if got := resultTextFallback("plain"); got != "plain" {
		t.Errorf("标量 = %q", got)
	}
}

// TestToInt 宽松整数转换。
func TestToInt(t *testing.T) {
	if v, ok := toInt(float64(3)); !ok || v != 3 {
		t.Errorf("float64 = %v %v", v, ok)
	}
	if v, ok := toInt(7); !ok || v != 7 {
		t.Errorf("int = %v %v", v, ok)
	}
	if v, ok := toInt(int64(9)); !ok || v != 9 {
		t.Errorf("int64 = %v %v", v, ok)
	}
	if v, ok := toInt(" 12 "); !ok || v != 12 {
		t.Errorf("字符串 = %v %v", v, ok)
	}
	if _, ok := toInt("abc"); ok {
		t.Error("非数字字符串应失败")
	}
	if _, ok := toInt(nil); ok {
		t.Error("nil 应失败")
	}
}

// TestExpandUser ~ 展开语义。
func TestExpandUser(t *testing.T) {
	if got, err := expandUser("/abs"); err != nil || got != "/abs" {
		t.Errorf("普通路径 = %q %v", got, err)
	}
	home, _ := os.UserHomeDir()
	if got, err := expandUser("~/x"); err != nil || got != filepath.Join(home, "x") {
		t.Errorf("~ 展开 = %q %v", got, err)
	}
}

// TestResolvedSessionRoot 会话根目录解析。
func TestResolvedSessionRoot(t *testing.T) {
	if got := resolvedSessionRoot(""); got == "" {
		t.Error("空 cwd 应回退 getwd")
	}
	if got := resolvedSessionRoot("  "); got == "" {
		t.Error("空白 cwd 应回退 getwd")
	}
	dir := t.TempDir()
	if got := resolvedSessionRoot(" " + dir + " "); got != dir {
		t.Errorf("trim 后解析 = %q want %q", got, dir)
	}
}

// TestResolvedPathInsideRoot 根内判定。
func TestResolvedPathInsideRoot(t *testing.T) {
	root := t.TempDir()
	// root 本身
	if got := resolvedPathInsideRoot(root, root); got != root {
		t.Errorf("root 本身 = %q", got)
	}
	// root 内
	inside := filepath.Join(root, "sub", "f.txt")
	if got := resolvedPathInsideRoot(root, inside); got == "" {
		t.Errorf("root 内路径被拒: %q", got)
	}
	// 相对路径（会以进程 cwd 解析，通常越界）
	if got := resolvedPathInsideRoot(root, 123); got != "" {
		t.Errorf("非字符串应拒: %q", got)
	}
	if got := resolvedPathInsideRoot(root, "   "); got != "" {
		t.Errorf("空白应拒: %q", got)
	}
	// 越界（真实存在才能 EvalSymlinks；不存在时 Abs 前缀判定）
	if got := resolvedPathInsideRoot(root, filepath.Join(root, "..", "x.txt")); got != "" {
		t.Errorf("越界路径应拒: %q", got)
	}
	// 不存在的 root 内路径（EvalSymlinks 失败回退 Abs）
	if got := resolvedPathInsideRoot(root, filepath.Join(root, "noexist.txt")); got == "" {
		t.Errorf("root 内不存在路径应放行（Abs 近似）")
	}
}

// TestClose各段超时env覆盖 验证 env 读取分支。
func TestClose各段超时env覆盖(t *testing.T) {
	t.Setenv("ACP_CLOSE_STDIN_WAIT_S", "0.05")
	t.Setenv("ACP_CLOSE_TERM_WAIT_S", "0.05")
	t.Setenv("ACP_CLOSE_KILL_WAIT_S", "0.05")
	t.Setenv("ACP_CLOSE_STDERR_JOIN_S", "0.05")
	if d := closeStdinWaitS(); d != 50*time.Millisecond {
		t.Errorf("closeStdinWaitS = %v", d)
	}
	if d := closeTermWaitS(); d != 50*time.Millisecond {
		t.Errorf("closeTermWaitS = %v", d)
	}
	if d := closeKillWaitS(); d != 50*time.Millisecond {
		t.Errorf("closeKillWaitS = %v", d)
	}
	if d := closeStderrJoinS(); d != 50*time.Millisecond {
		t.Errorf("closeStderrJoinS = %v", d)
	}
}

// TestEnvSeconds解析失败回退 验证非法值回退默认。
func TestEnvSeconds解析失败回退(t *testing.T) {
	t.Setenv("ACP_TEST_ENV_SECONDS_BAD", "not-a-number")
	if d := envSeconds("ACP_TEST_ENV_SECONDS_BAD", 2.5); d != 2500*time.Millisecond {
		t.Errorf("回退默认 = %v", d)
	}
	t.Setenv("ACP_TEST_ENV_SECONDS_ZERO", "0")
	if d := envSeconds("ACP_TEST_ENV_SECONDS_ZERO", 2.5); d != 2500*time.Millisecond {
		t.Errorf("零值回退默认 = %v", d)
	}
	if d := envSeconds("ACP_TEST_ENV_SECONDS_UNSET", 3); d != 3*time.Second {
		t.Errorf("未设置 = %v", d)
	}
}

// TestDefaultChatTimeoutEnv 默认聊天超时 env 覆盖。
func TestDefaultChatTimeoutEnv(t *testing.T) {
	t.Setenv("ACP_CHAT_TIMEOUT", "1.5")
	if d := defaultChatTimeout(); d != 1500*time.Millisecond {
		t.Errorf("env 覆盖 = %v", d)
	}
	t.Setenv("ACP_CHAT_TIMEOUT", "bad")
	if d := defaultChatTimeout(); d != 600*time.Second {
		t.Errorf("非法回退 = %v", d)
	}
}

// TestDebugChat 调试开关。
func TestDebugChat(t *testing.T) {
	for _, v := range []string{"1", "true", "yes", "TRUE"} {
		t.Setenv("ACP_CHAT_DEBUG", v)
		if !debugChat() {
			t.Errorf("%s 应开启调试", v)
		}
	}
	t.Setenv("ACP_CHAT_DEBUG", "0")
	if debugChat() {
		t.Error("0 应关闭调试")
	}
}

// TestAutoApprovePermissions 自动应答开关。
func TestAutoApprovePermissions(t *testing.T) {
	t.Setenv("ACP_AUTO_APPROVE_PERMISSIONS", "")
	if !autoApprovePermissions() {
		t.Error("默认应开启")
	}
	t.Setenv("ACP_AUTO_APPROVE_PERMISSIONS", "0")
	if autoApprovePermissions() {
		t.Error("0 应关闭")
	}
}

// TestKillProcessGroup 进程组开关。
func TestKillProcessGroup(t *testing.T) {
	t.Setenv("ACP_KILL_PROCESS_GROUP", "0")
	if killProcessGroup() {
		t.Error("0 应关闭")
	}
	t.Setenv("ACP_KILL_PROCESS_GROUP", "")
	if runtime.GOOS == "windows" {
		if killProcessGroup() {
			t.Error("Windows 恒关闭")
		}
	} else if !killProcessGroup() {
		t.Error("默认应开启")
	}
}

// TestWaitChan与ChanClosed 通道辅助。
func TestWaitChan与ChanClosed(t *testing.T) {
	ch := make(chan struct{})
	if chanClosed(ch) {
		t.Error("未关闭应为 false")
	}
	close(ch)
	if !chanClosed(ch) {
		t.Error("已关闭应为 true")
	}
	if chanClosed(nil) {
		t.Error("nil 通道应为 false")
	}
	// 超时路径
	ch2 := make(chan struct{})
	start := time.Now()
	waitChan(ch2, 30*time.Millisecond)
	if time.Since(start) < 25*time.Millisecond {
		t.Error("应等满超时")
	}
	waitChan(nil, 30*time.Millisecond) // nil 不等待不崩溃
	waitChan(ch, 30*time.Millisecond)  // 已关闭立即返回
}

// TestChat错误响应传播 验证 JSON-RPC error 转 error。
func TestChat错误响应传播(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("testdata shell 脚本不支持 Windows")
	}
	// 对 session/prompt 回 error
	dir := t.TempDir()
	script := filepath.Join(dir, "err_peer.sh")
	_ = os.WriteFile(script, []byte(`#!/bin/sh
while IFS= read -r line; do
    method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
    rid=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
    case "$method" in
        initialize)
            printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$rid"
            ;;
        session/new)
            printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"err-sess"}}\n' "$rid"
            ;;
        *)
            printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"boom"}}\n' "$rid"
            ;;
    esac
done
`), 0o755)
	c := NewAcpStdioClient(script, nil, "", nil)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	_, err := c.Chat(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "ACP JSON-RPC error") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error 响应应转为错误，实际 %v", err)
	}
}

// TestRpcCallEOF传播 silent peer 退出 → EOF → Connect 失败（rpcCall 的流关闭分支）。
func TestRpcCallEOF传播(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("testdata shell 脚本不支持 Windows")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "silent_peer.sh")
	_ = os.WriteFile(script, []byte("#!/bin/sh\nsleep 3\n"), 0o755)
	c := NewAcpStdioClient(script, nil, "", nil)
	start := time.Now()
	err := c.Connect(context.Background())
	if err == nil {
		_ = c.Close()
		t.Fatal("silent peer Connect 应失败")
	}
	if time.Since(start) > 30*time.Second {
		t.Errorf("EOF 应在脚本退出后立即触发，耗时 %v", time.Since(start))
	}
}

// TestFirstNonNil 首个非 nil。
func TestFirstNonNil(t *testing.T) {
	if got := firstNonNil(nil, nil); got != nil {
		t.Errorf("全 nil = %v", got)
	}
	if got := firstNonNil(nil, "x", "y"); got != "x" {
		t.Errorf("首个非 nil = %v", got)
	}
}

// TestWriteLineStdinClosed stdin 关闭后写报错。
func TestWriteLineStdinClosed(t *testing.T) {
	c := NewAcpStdioClient("unused", nil, "", nil)
	if err := c.writeLine([]byte(`{"x":1}`)); err == nil {
		t.Error("stdin 关闭应报错")
	}
}

// TestRegisterPending重复 registerPending 重复 ID 报错。
func TestRegisterPending重复(t *testing.T) {
	c := NewAcpStdioClient("unused", nil, "", nil)
	if _, err := c.registerPending(1); err != nil {
		t.Fatalf("首次登记: %v", err)
	}
	if _, err := c.registerPending(1); err == nil {
		t.Error("重复 ID 应报错")
	}
	c.removePending(1)
}

// TestFailAllPending 失败投递。
func TestFailAllPending(t *testing.T) {
	c := NewAcpStdioClient("unused", nil, "", nil)
	ch, _ := c.registerPending(5)
	c.failAllPending(errors.New("boom"))
	select {
	case res := <-ch:
		if res.Err == nil || res.Err.Error() != "boom" {
			t.Errorf("res = %+v", res)
		}
	default:
		t.Error("应唤醒等待者")
	}
	// 清空后无残留
	if len(c.pending) != 0 {
		t.Errorf("pending 应清空: %v", c.pending)
	}
}

// TestDeliverPending未知ID 未知 ID 投递被忽略。
func TestDeliverPending未知ID(t *testing.T) {
	c := NewAcpStdioClient("unused", nil, "", nil)
	c.deliverPending(float64(99), &rpcResult{Msg: map[string]any{"result": nil}})
	// 非 int ID 被忽略
	c.deliverPending("abc", &rpcResult{Msg: map[string]any{}})
}

// TestUpdates通道缓冲满 updates chan 满时丢弃不阻塞。
func TestUpdates通道缓冲满(t *testing.T) {
	c := NewAcpStdioClient("unused", nil, "", nil)
	// 填满 256 缓冲
	for i := 0; i < 300; i++ {
		select {
		case c.updatesCh <- "x":
		default:
		}
	}
	// dispatchMessage 投递应走 default 分支不阻塞
	done := make(chan struct{})
	go func() {
		c.dispatchMessage(map[string]any{
			"jsonrpc": "2.0",
			"method":  "session/update",
			"params":  map[string]any{"update": map[string]any{"content": map[string]any{"type": "text", "text": "t"}}},
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("缓冲满时 dispatchMessage 应立即返回")
	}
}

// TestDispatchMessage非对象分发 验证非对象消息与对端请求异步分发。
func TestDispatchMessage非对象分发(t *testing.T) {
	c := NewAcpStdioClient("unused", nil, "", nil)
	// 非 id 响应/非对端请求/非 session/update → 忽略
	c.dispatchMessage(map[string]any{"jsonrpc": "2.0", "method": "other/notify"})
	// 对端请求 → 异步 handlePeerRequest（stdin 关闭，写失败被忽略）
	c.dispatchMessage(map[string]any{
		"jsonrpc": "2.0", "id": float64(1), "method": "unknown/x",
	})
	// result 响应（有 id）
	ch, _ := c.registerPending(42)
	c.dispatchMessage(map[string]any{
		"jsonrpc": "2.0", "id": float64(42), "result": map[string]any{"ok": true},
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			t.Errorf("res = %+v", res)
		}
	case <-time.After(time.Second):
		t.Fatal("应唤醒等待者")
	}
	// error 响应（有 id）
	ch2, _ := c.registerPending(43)
	c.dispatchMessage(map[string]any{
		"jsonrpc": "2.0", "id": float64(43),
		"error":   map[string]any{"code": float64(-1), "message": "e"},
	})
	select {
	case res := <-ch2:
		if res.Err == nil {
			t.Error("error 响应应带 Err")
		}
	case <-time.After(time.Second):
		t.Fatal("应唤醒等待者")
	}
}

// TestChat多段update聚合 多条 session/update 拼接。
func TestChat多段update聚合(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("testdata shell 脚本不支持 Windows")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "multi_peer.sh")
	_ = os.WriteFile(script, []byte(`#!/bin/sh
while IFS= read -r line; do
    method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
    rid=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
    case "$method" in
        initialize)
            printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$rid"
            ;;
        session/new)
            printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"multi-sess"}}\n' "$rid"
            ;;
        *)
            printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"content":{"type":"text","text":"part1-"}}}}\n'
            printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"content":{"type":"text","text":"part2"}}}}\n'
            printf '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"content":{"type":"image","text":"ignored"}}}}\n'
            printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$rid"
            ;;
    esac
done
`), 0o755)
	c := NewAcpStdioClient(script, nil, "", nil)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	out, err := c.Chat(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	// parts 聚合 + resultTextFallback（{} 序列化为 "{}"）——
	// Python: "".join(parts).strip() or _result_text_fallback(result)
	if out != "part1-part2" {
		t.Errorf("聚合 = %q", out)
	}
}

// TestChatFallback无update 无 update 文本时回退 result。
func TestChatFallback无update(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("testdata shell 脚本不支持 Windows")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fb_peer.sh")
	_ = os.WriteFile(script, []byte(`#!/bin/sh
while IFS= read -r line; do
    method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
    rid=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
    case "$method" in
        initialize)
            printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$rid"
            ;;
        session/new)
            printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"fb-sess"}}\n' "$rid"
            ;;
        *)
            printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$rid"
            ;;
    esac
done
`), 0o755)
	c := NewAcpStdioClient(script, nil, "", nil)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	out, err := c.Chat(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out != "end_turn" {
		t.Errorf("回退 = %q", out)
	}
}
