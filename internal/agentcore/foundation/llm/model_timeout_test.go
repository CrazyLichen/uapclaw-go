package llm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 结构体 ────────────────────────────

// slowStreamClient 按脚本在发送前等待，用于制造首帧/空闲超时。
// 内嵌 BaseModelClient 接口以获得完整方法集：本用例只走 Stream，
// 其它方法不会被调用（调用即 panic，属测试辅助的常规写法）。
type slowStreamClient struct {
	model_clients.BaseModelClient
	delays []time.Duration
	chunks []*llmschema.AssistantMessageChunk
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Stream 按下标依次等待后发送分片，并尊重 ctx 取消。
func (s *slowStreamClient) Stream(ctx context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	ch := make(chan *llmschema.AssistantMessageChunk, len(s.chunks))
	go func() {
		defer close(ch)
		for i, chunk := range s.chunks {
			delay := time.Duration(0)
			if i < len(s.delays) {
				delay = s.delays[i]
			}
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
			select {
			case ch <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// floatPtr 返回浮点指针。
func floatPtr(f float64) *float64 { return &f }

// newTimeoutModel 构造带流超时配置的 Model（不经注册表，直接注入 client）。
func newTimeoutModel(client model_clients.BaseModelClient, first, idle *float64) *Model {
	return &Model{
		ModelConfig: llmschema.NewModelRequestConfig(llmschema.WithModelName("m")),
		ClientConfig: &llmschema.ModelClientConfig{
			ClientProvider:          "OpenAI",
			APIKey:                  "k",
			APIBase:                 "http://localhost",
			Timeout:                 60,
			StreamFirstChunkTimeout: first,
			StreamIdleTimeout:       idle,
		},
		client:            client,
		callbackFramework: callback.NewCallbackFramework(),
	}
}

// drainStream 读完通道并返回收到的 StreamError（若有）。
func drainStream(ch <-chan *llmschema.AssistantMessageChunk) error {
	var streamErr error
	for chunk := range ch {
		if chunk != nil && chunk.StreamError != nil {
			streamErr = chunk.StreamError
		}
	}
	return streamErr
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestModel_Stream_首帧超时 验证首个携带输出的分片迟迟不到时产出超时错误。
func TestModel_Stream_首帧超时(t *testing.T) {
	m := newTimeoutModel(&slowStreamClient{
		delays: []time.Duration{300 * time.Millisecond},
		chunks: []*llmschema.AssistantMessageChunk{llmschema.NewAssistantMessageChunk("x")},
	}, floatPtr(0.05), floatPtr(5))

	ch, err := m.Stream(context.Background(), model_clients.NewTextMessagesParam("hi"))
	if err != nil {
		t.Fatalf("Stream 不应立即报错: %v", err)
	}
	streamErr := drainStream(ch)
	if streamErr == nil {
		t.Fatal("应收到超时错误")
	}
	msg := streamErr.Error()
	for _, want := range []string{"LLM stream timeout", "stream frame timeout", "stage=first_token", "token_chunk_count=0"} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误文案缺少 %q: %s", want, msg)
		}
	}
}

// TestModel_Stream_空闲超时 验证已出首帧后相邻分片间隔过长时产出超时错误。
func TestModel_Stream_空闲超时(t *testing.T) {
	m := newTimeoutModel(&slowStreamClient{
		delays: []time.Duration{0, 400 * time.Millisecond},
		chunks: []*llmschema.AssistantMessageChunk{
			llmschema.NewAssistantMessageChunk("a"),
			llmschema.NewAssistantMessageChunk("b"),
		},
	}, floatPtr(1), floatPtr(0.05))

	ch, err := m.Stream(context.Background(), model_clients.NewTextMessagesParam("hi"))
	if err != nil {
		t.Fatalf("Stream 不应立即报错: %v", err)
	}
	streamErr := drainStream(ch)
	if streamErr == nil || !strings.Contains(streamErr.Error(), "stage=idle_token") {
		t.Fatalf("期望 idle_token 超时，实际 %v", streamErr)
	}
}

// TestModel_Stream_框架帧不重置空闲预算 验证 role-only 帧不重置 idle 预算。
func TestModel_Stream_框架帧不重置空闲预算(t *testing.T) {
	m := newTimeoutModel(&slowStreamClient{
		delays: []time.Duration{0, 30 * time.Millisecond, 60 * time.Millisecond},
		chunks: []*llmschema.AssistantMessageChunk{
			llmschema.NewAssistantMessageChunk("a"),
			{AssistantMessage: llmschema.AssistantMessage{
				DefaultMessage: llmschema.DefaultMessage{Role: llmschema.RoleTypeAssistant},
			}},
			llmschema.NewAssistantMessageChunk("b"),
		},
	}, floatPtr(1), floatPtr(0.08))

	ch, err := m.Stream(context.Background(), model_clients.NewTextMessagesParam("hi"))
	if err != nil {
		t.Fatalf("Stream 不应立即报错: %v", err)
	}
	if streamErr := drainStream(ch); streamErr == nil {
		t.Fatal("框架帧不应重置空闲预算，应超时")
	}
}

// TestModel_Stream_禁用超时不报错 验证 nil 预算表示禁用。
func TestModel_Stream_禁用超时不报错(t *testing.T) {
	m := newTimeoutModel(&slowStreamClient{
		delays: []time.Duration{80 * time.Millisecond},
		chunks: []*llmschema.AssistantMessageChunk{llmschema.NewAssistantMessageChunk("a")},
	}, nil, nil)

	ch, err := m.Stream(context.Background(), model_clients.NewTextMessagesParam("hi"))
	if err != nil {
		t.Fatalf("Stream 不应立即报错: %v", err)
	}
	if streamErr := drainStream(ch); streamErr != nil {
		t.Fatalf("禁用后不应超时: %v", streamErr)
	}
}

// TestModel_Stream_排空超时兜底 客户端忽略 ctx 且不关通道时，排空必须超时返回而不是挂住。
func TestModel_Stream_排空超时兜底(t *testing.T) {
	originalDrain := streamDrainTimeout
	streamDrainTimeout = 20 * time.Millisecond
	t.Cleanup(func() { streamDrainTimeout = originalDrain })

	// 通道永不关闭：模拟"客户端忽略 ctx 也不退出"的最坏情况
	neverClosed := make(chan *llmschema.AssistantMessageChunk)
	m := newTimeoutModel(&stubbornStreamClient{ch: neverClosed}, floatPtr(0.02), floatPtr(0.02))

	done := make(chan error, 1)
	go func() {
		ch, err := m.Stream(context.Background(), model_clients.NewTextMessagesParam("hi"))
		if err != nil {
			done <- err
			return
		}
		var streamErr error
		for chunk := range ch {
			if chunk != nil && chunk.StreamError != nil {
				streamErr = chunk.StreamError
			}
		}
		done <- streamErr
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("应产出超时错误")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("排空未超时返回，说明仍会永久挂住")
	}
}

// stubbornStreamClient 返回一个永不关闭的通道，且不响应 ctx 取消。
// 内嵌 *mockModelClient 补齐 BaseModelClient 的其余方法（不会被调用）。
type stubbornStreamClient struct {
	*mockModelClient
	// ch 永不关闭的分片通道
	ch chan *llmschema.AssistantMessageChunk
}

// Invoke 空实现。
func (s *stubbornStreamClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return nil, nil
}

// Stream 直接返回永不关闭的通道。
func (s *stubbornStreamClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return s.ch, nil
}

// TestModel_Stream_取消后排空超时兜底 父 ctx 取消且客户端不关通道时，排空同样必须超时返回。
func TestModel_Stream_取消后排空超时兜底(t *testing.T) {
	originalDrain := streamDrainTimeout
	streamDrainTimeout = 20 * time.Millisecond
	t.Cleanup(func() { streamDrainTimeout = originalDrain })

	neverClosed := make(chan *llmschema.AssistantMessageChunk)
	m := newTimeoutModel(&stubbornStreamClient{ch: neverClosed}, floatPtr(0.02), floatPtr(0.02))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	ch, err := m.Stream(ctx, model_clients.NewTextMessagesParam("hi"))
	if err != nil {
		return // 取消态下直接返回错误也是允许的收尾
	}
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("取消后排空未超时返回，说明仍会永久挂住")
	}
}
