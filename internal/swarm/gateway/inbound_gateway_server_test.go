package gateway

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// waitMessages 轮询等待收集到 n 条消息。
func waitMessages(t *testing.T, mu *sync.Mutex, got *[]*schema.Message, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		c := len(*got)
		mu.Unlock()
		if c >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitIDs 轮询等待收集到 n 个 ID（panic 恢复测试用，got 在锁内追加）。
func waitIDs(t *testing.T, mu *sync.Mutex, got *[]string, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		c := len(*got)
		mu.Unlock()
		if c >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ──────────────────────────── InboundGatewayServer 测试 ────────────────────────────

func TestInboundGatewayServer_消费到handler(t *testing.T) {
	var mu sync.Mutex
	var got []*schema.Message
	s := NewInboundGatewayServer(func(ctx context.Context, msg *schema.Message) {
		mu.Lock()
		got = append(got, msg)
		mu.Unlock()
	})
	require.NoError(t, s.Start(context.Background()))
	defer s.Stop()

	require.NoError(t, s.HandleMessage(context.Background(), &schema.Message{ID: "m1"}))

	waitMessages(t, &mu, &got, 1)
	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, got, 1)
}

func TestInboundGatewayServer_panic恢复不中断(t *testing.T) {
	var mu sync.Mutex
	var got []string
	s := NewInboundGatewayServer(func(ctx context.Context, msg *schema.Message) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, msg.ID)
		if msg.ID == "boom" {
			panic("inbound handler panicked")
		}
	})
	require.NoError(t, s.Start(context.Background()))
	defer s.Stop()

	require.NoError(t, s.HandleMessage(context.Background(), &schema.Message{ID: "boom"}))
	require.NoError(t, s.HandleMessage(context.Background(), &schema.Message{ID: "after"}))

	waitIDs(t, &mu, &got, 2)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"boom", "after"}, got)
}

func TestInboundGatewayServer_Start幂等(t *testing.T) {
	s := NewInboundGatewayServer(func(ctx context.Context, msg *schema.Message) {})
	require.NoError(t, s.Start(context.Background()))
	require.NoError(t, s.Start(context.Background())) // 第二次不起新 serveLoop
	defer s.Stop()
	assert.True(t, s.running.Load())
}

func TestInboundGatewayServer_Stop等待退出(t *testing.T) {
	handlerStarted := make(chan struct{})
	handlerDone := make(chan struct{})
	s := NewInboundGatewayServer(func(ctx context.Context, msg *schema.Message) {
		// 阻塞直到 ctx 取消
		close(handlerStarted)
		<-ctx.Done()
		close(handlerDone)
	})
	require.NoError(t, s.Start(context.Background()))

	require.NoError(t, s.HandleMessage(context.Background(), &schema.Message{ID: "w1"}))
	// 同步点：等 handler 真正开始执行（消息已被 serveLoop 消费），
	// 避免 Stop 先置 running=false 导致循环零次执行的调度竞态
	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("handler 未被 serveLoop 调度")
	}

	s.Stop()
	// Stop 返回即 serveLoop 已退出（taskDone 已 close）；ctx 已传播到 handler
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("serveLoop ctx 未传播到 handler")
	}
	s.Stop() // 二次 Stop 幂等不 panic
}

func TestInboundGatewayServer_满队丢弃(t *testing.T) {
	// 不启动 serveLoop（queue 持续满）
	s := NewInboundGatewayServer(func(ctx context.Context, msg *schema.Message) {})
	// 填满 256
	for i := 0; i < inboundQueueCapacity; i++ {
		require.NoError(t, s.HandleMessage(context.Background(), &schema.Message{ID: "m"}))
	}
	// 第 257 条：满队丢弃，仍返回 nil（不阻塞不报错）
	require.NoError(t, s.HandleMessage(context.Background(), &schema.Message{ID: "overflow"}))
	// 溢出消息未入队（queue 长度仍为容量值）
	assert.Equal(t, inboundQueueCapacity, len(s.queue))
}
