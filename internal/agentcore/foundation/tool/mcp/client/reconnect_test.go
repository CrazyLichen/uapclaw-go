package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// flakyClient 可编排失败次数的测试客户端。
type flakyClient struct {
	// mu 保护下面的计数字段
	mu sync.Mutex
	// failures 剩余需要失败的次数
	failures int
	// errMsg 失败时的错误消息
	errMsg string
	// calls 调用次数
	calls int
	// disconnects 断开次数
	disconnects int
	// connects 连接次数
	connects int
	// connectFail 连接是否失败
	connectFail bool
}

func (f *flakyClient) Connect(_ context.Context, _ ...types.ConnectOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connects++
	if f.connectFail {
		return errors.New("connect refused")
	}
	return nil
}

func (f *flakyClient) Disconnect(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnects++
	return nil
}

func (f *flakyClient) ListTools(context.Context) ([]*types.McpToolCard, error) { return nil, nil }
func (f *flakyClient) CallTool(context.Context, string, map[string]any) (any, error) {
	return nil, nil
}
func (f *flakyClient) GetToolInfo(context.Context, string) (*types.McpToolCard, error) {
	return nil, nil
}
func (f *flakyClient) ListResources(context.Context) ([]map[string]any, error) { return nil, nil }
func (f *flakyClient) ReadResource(context.Context, string) ([]map[string]any, error) {
	return nil, nil
}
func (f *flakyClient) Close() error { return nil }

// next 返回本次调用的错误（按剩余失败次数递减）。
func (f *flakyClient) next() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failures > 0 {
		f.failures--
		msg := f.errMsg
		if msg == "" {
			msg = "connection closed"
		}
		return errors.New(msg)
	}
	return nil
}

// counts 返回当前计数快照。
func (f *flakyClient) counts() (calls, disconnects, connects int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.disconnects, f.connects
}

// ──────────────────────────── 错误分类 ────────────────────────────

// TestIsRetryableTransportError_传输错误 验证传输层错误可重试
func TestIsRetryableTransportError_传输错误(t *testing.T) {
	cases := []string{
		"connection closed",
		"stream closed",
		"session terminated",
		"session not found",
		"Connection reset by peer: broken pipe",
		"transport: Error while dialing: EndOfStream",
	}
	for _, msg := range cases {
		if !IsRetryableTransportError(errors.New(msg)) {
			t.Fatalf("%q 应判为可重试", msg)
		}
	}
}

// TestIsRetryableTransportError_业务错误 验证业务错误不重试
func TestIsRetryableTransportError_业务错误(t *testing.T) {
	for _, msg := range []string{"invalid argument", "tool not found", "permission denied"} {
		if IsRetryableTransportError(errors.New(msg)) {
			t.Fatalf("%q 不应判为可重试", msg)
		}
	}
	if IsRetryableTransportError(nil) {
		t.Fatal("nil 不应判为可重试")
	}
}

// TestIsRetryableTransportError_大小写不敏感 验证匹配不区分大小写
func TestIsRetryableTransportError_大小写不敏感(t *testing.T) {
	if !IsRetryableTransportError(errors.New("CONNECTION CLOSED")) {
		t.Fatal("大写文案也应命中")
	}
}

// codedErr 带 JSON-RPC 错误码的测试错误。
type codedErr struct {
	code int
	msg  string
}

func (c *codedErr) Error() string   { return c.msg }
func (c *codedErr) Code() int       { return c.code }
func (c *codedErr) Message() string { return c.msg }

// TestIsRetryableTransportError_错误码 验证按 JSON-RPC 码判定
func TestIsRetryableTransportError_错误码(t *testing.T) {
	if !IsRetryableTransportError(&codedErr{code: connectionClosedCode, msg: "boom"}) {
		t.Fatal("连接关闭码应可重试")
	}
	if !IsRetryableTransportError(&codedErr{code: sessionTerminatedCode, msg: "Session terminated"}) {
		t.Fatal("会话终止码应可重试")
	}
	// 同码但消息不含 session：不算会话失效
	if IsRetryableTransportError(&codedErr{code: sessionTerminatedCode, msg: "bad params"}) {
		t.Fatal("非会话消息不应可重试")
	}
}

// ──────────────────────────── 重连流程 ────────────────────────────

// TestWithReconnect_传输错误后重试成功 验证重连一次后拿到结果
func TestWithReconnect_传输错误后重试成功(t *testing.T) {
	c := &flakyClient{failures: 1}
	got, err := withReconnect(context.Background(), c, 0, "CallTool", func() (string, error) {
		if e := c.next(); e != nil {
			return "", e
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("重连后应成功，实际 %v", err)
	}
	if got != "ok" {
		t.Fatalf("应返回 ok，实际 %q", got)
	}
	_, disconnects, connects := c.counts()
	if disconnects != 1 || connects != 1 {
		t.Fatalf("应断开 1 次、重连 1 次，实际 disconnects=%d connects=%d", disconnects, connects)
	}
}

// TestWithReconnect_业务错误不重连 验证非传输错误直接上抛
func TestWithReconnect_业务错误不重连(t *testing.T) {
	c := &flakyClient{failures: 1, errMsg: "invalid argument"}
	_, err := withReconnect(context.Background(), c, 0, "CallTool", func() (string, error) {
		if e := c.next(); e != nil {
			return "", e
		}
		return "ok", nil
	})
	if err == nil || !strings.Contains(err.Error(), "invalid argument") {
		t.Fatalf("应返回原始业务错误，实际 %v", err)
	}
	_, disconnects, _ := c.counts()
	if disconnects != 0 {
		t.Fatalf("业务错误不应触发重连，实际断开 %d 次", disconnects)
	}
}

// TestWithReconnect_调用方取消不重连 验证 ctx 取消原样抛出
func TestWithReconnect_调用方取消不重连(t *testing.T) {
	c := &flakyClient{failures: 5}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := withReconnect(ctx, c, 0, "CallTool", func() (string, error) {
		return "", errors.New("connection closed")
	})
	if err == nil {
		t.Fatal("应返回错误")
	}
	calls, disconnects, _ := c.counts()
	if calls != 0 || disconnects != 0 {
		t.Fatalf("取消后应立即返回，实际 calls=%d disconnects=%d", calls, disconnects)
	}
}

// TestWithReconnect_始终失败按次数耗尽 验证重试次数上限
func TestWithReconnect_始终失败按次数耗尽(t *testing.T) {
	c := &flakyClient{failures: 100}
	_, err := withReconnect(context.Background(), c, 3, "CallTool", func() (string, error) {
		if e := c.next(); e != nil {
			return "", e
		}
		return "ok", nil
	})
	if err == nil {
		t.Fatal("应返回错误")
	}
	calls, disconnects, _ := c.counts()
	if calls != 3 {
		t.Fatalf("总尝试次数应为 3，实际 %d", calls)
	}
	if disconnects != 2 {
		t.Fatalf("应重连 2 次，实际 %d", disconnects)
	}
}

// TestWithReconnect_成功后清空退避 验证成功会重置 failureCount
func TestWithReconnect_成功后清空退避(t *testing.T) {
	c := &flakyClient{failures: 1}
	_, _ = withReconnect(context.Background(), c, 0, "CallTool", func() (string, error) {
		if e := c.next(); e != nil {
			return "", e
		}
		return "ok", nil
	})
	state := reconnectStateOf(c)
	if state.failureCount != 0 {
		t.Fatalf("成功后 failureCount 应清零，实际 %d", state.failureCount)
	}
	if !state.cooldownUntil.IsZero() {
		t.Fatal("成功后应清除冷却")
	}
}

// TestWithReconnect_冷却期内不重连 验证退避生效
func TestWithReconnect_冷却期内不重连(t *testing.T) {
	c := &flakyClient{}
	state := reconnectStateOf(c)
	state.cooldownUntil = time.Now().Add(10 * time.Second)

	c.failures = 1
	_, err := withReconnect(context.Background(), c, 0, "CallTool", func() (string, error) {
		if e := c.next(); e != nil {
			return "", e
		}
		return "ok", nil
	})
	if err == nil {
		t.Fatal("冷却期内应直接失败")
	}
	_, disconnects, _ := c.counts()
	if disconnects != 0 {
		t.Fatalf("冷却期内不应重连，实际断开 %d 次", disconnects)
	}
}

// TestWithReconnect_重连失败记退避 验证连接失败进入冷却
func TestWithReconnect_重连失败记退避(t *testing.T) {
	c := &flakyClient{failures: 5, connectFail: true}
	_, _ = withReconnect(context.Background(), c, 0, "CallTool", func() (string, error) {
		if e := c.next(); e != nil {
			return "", e
		}
		return "ok", nil
	})
	state := reconnectStateOf(c)
	if state.failureCount != 1 {
		t.Fatalf("应记录 1 次失败，实际 %d", state.failureCount)
	}
	if state.cooldownRemaining() <= 0 {
		t.Fatal("应进入冷却")
	}
}

// TestWithReconnect_代际变化不再重连 验证并发已重建时不重复拆连接
func TestWithReconnect_代际变化不再重连(t *testing.T) {
	c := &flakyClient{}
	state := reconnectStateOf(c)
	bumpGeneration(state)
	generationBefore := currentGeneration(state)

	callIdx := 0
	_, err := withReconnect(context.Background(), c, 3, "CallTool", func() (string, error) {
		callIdx++
		c.next()
		if callIdx == 1 {
			// 模拟另一调用方在此期间完成了重连
			bumpGeneration(state)
			return "", errors.New("connection closed")
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("代际变化后应重试成功，实际 %v", err)
	}
	_, disconnects, _ := c.counts()
	if disconnects != 0 {
		t.Fatalf("代际已变化不应再拆连接，实际断开 %d 次", disconnects)
	}
	if generationBefore == currentGeneration(state) {
		t.Fatal("代际应已递增")
	}
}

// TestWithReconnect_并发不重复重连 验证实例锁生效
func TestWithReconnect_并发不重复重连(t *testing.T) {
	c := &flakyClient{failures: 1000}
	var wg sync.WaitGroup
	var succeed atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := withReconnect(context.Background(), c, 2, "CallTool", func() (string, error) {
				if e := c.next(); e != nil {
					return "", e
				}
				return "ok", nil
			})
			if err == nil {
				succeed.Add(1)
			}
		}()
	}
	wg.Wait()
	if succeed.Load() != 0 {
		t.Fatalf("全部失败场景不应有成功，实际 %d", succeed.Load())
	}
}

// TestReconnectState_退避递增 验证退避按指数增长且有上限
func TestReconnectState_退避递增(t *testing.T) {
	state := &reconnectState{}
	var prev time.Duration
	for i := 0; i < 8; i++ {
		state.recordFailure()
		remaining := time.Until(state.cooldownUntil)
		if remaining <= 0 {
			t.Fatalf("第 %d 次失败应设置冷却", i+1)
		}
		// 上限 120s * (1+jitter)
		if remaining > time.Duration(reconnectMaxDelay*1.21*float64(time.Second)) {
			t.Fatalf("冷却不应超过上限，实际 %v", remaining)
		}
		prev = remaining
	}
	if prev <= 0 {
		t.Fatal("冷却应始终为正")
	}
}

// TestResolveAttempts_优先级 验证客户端覆盖 > 环境变量 > 默认
func TestResolveAttempts_优先级(t *testing.T) {
	if got := resolveAttempts(5); got != 5 {
		t.Fatalf("客户端覆盖应优先，实际 %d", got)
	}
	t.Setenv("OPENJIUWEN_MCP_RECONNECT_ATTEMPTS", "4")
	if got := resolveAttempts(0); got != 4 {
		t.Fatalf("环境变量应生效，实际 %d", got)
	}
	t.Setenv("OPENJIUWEN_MCP_RECONNECT_ATTEMPTS", "abc")
	if got := resolveAttempts(0); got != defaultReconnectAttempts {
		t.Fatalf("非法环境变量应用默认，实际 %d", got)
	}
}

// TestShouldAttemptReconnect_未连接守卫 验证按 lazy 标记区分主动断开
func TestShouldAttemptReconnect_未连接守卫(t *testing.T) {
	c := &flakyClient{}
	guardErr := fmt.Errorf("not connected to streamable-http server")

	if ShouldAttemptReconnect(c, guardErr) {
		t.Fatal("未标记 lazyReconnect 时不应重连")
	}
	reconnectStateOf(c).lazyReconnect = true
	if !ShouldAttemptReconnect(c, guardErr) {
		t.Fatal("lazyReconnect 时应允许重连")
	}
}
