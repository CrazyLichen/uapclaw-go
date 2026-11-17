package stream_actor

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
)

// TestPayloadQueue_无界不阻塞 send 侧永不阻塞（D2，对齐 asyncio.Queue 无界语义）
func TestPayloadQueue_无界不阻塞(t *testing.T) {
	q := newPayloadQueue()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			q.put(StreamPayload{})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("put 10000 帧不应阻塞（无界队列）")
	}
}

// TestPayloadQueue_Get_取消 Get 阻塞时 ctx 取消可退出
func TestPayloadQueue_Get_取消(t *testing.T) {
	q := newPayloadQueue()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	start := time.Now()
	_, ok := q.get(ctx)
	if ok {
		t.Fatal("取消后 get 应返回 ok=false")
	}
	if time.Since(start) > time.Second {
		t.Fatal("ctx 取消应立即解除阻塞")
	}
}

// TestPayloadQueue_关闭 Get 在 Close 后立即返回 false（已入队帧仍可取）
func TestPayloadQueue_关闭(t *testing.T) {
	q := newPayloadQueue()
	q.put(StreamPayload{})
	q.close()
	if _, ok := q.get(context.Background()); !ok {
		t.Fatal("已入队帧应在 close 后仍可取出")
	}
	if _, ok := q.get(context.Background()); ok {
		t.Fatal("取尽后 close 队列 get 应返回 false")
	}
}

// TestPayloadQueue_len 队列长度辅助
func TestPayloadQueue_len(t *testing.T) {
	q := newPayloadQueue()
	q.put(StreamPayload{})
	q.put(StreamPayload{})
	if q.len() != 2 {
		t.Fatalf("len 应为 2, got %d", q.len())
	}
}

// TestGetProducerID 对齐 Python _get_producer_id：单键 dict 取首键，否则 ValueError（Go panic）
func TestGetProducerID(t *testing.T) {
	if got := getProducerID(map[string]any{"nodeA": "x"}); got != "nodeA" {
		t.Fatalf("got %q", got)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("双键 message 应 panic 对齐 Python ValueError")
			}
		}()
		getProducerID(map[string]any{"a": 1, "b": 2})
	}()
}

// TestIsEndMessage 对齐 Python _is_end_message：内容 END_ 前缀判定（D5）
func TestIsEndMessage(t *testing.T) {
	if !isEndMessage(map[string]any{"nodeA": "END_nodeA"}) {
		t.Fatal("END_ 前缀应判定为结束帧")
	}
	if isEndMessage(map[string]any{"nodeA": "hello"}) {
		t.Fatal("普通内容不应判定为结束帧")
	}
	if isEndMessage(map[string]any{"nodeA": 123}) {
		t.Fatal("非字符串内容不应判定为结束帧")
	}
}

// TestStreamGraph_注册幂等 首注册优先（对齐 Python add_stream_consumer）
func TestStreamGraph_注册幂等(t *testing.T) {
	g := NewStreamGraph()
	c1 := &utils.EndFrame{Source: "c1"}
	c2 := &utils.EndFrame{Source: "c2"}
	// 借用 StreamConsumer 接口验证注册行为需要 fake，这里用最小 fake
	f1 := &fakeGraphConsumer{}
	f2 := &fakeGraphConsumer{}
	g.AddStreamConsumer(f1, "n")
	g.AddStreamConsumer(f2, "n")
	if g.GetNode("n") != f1 {
		t.Fatal("重复注册应忽略（首注册优先）")
	}
	if g.GetNode("missing") != nil {
		t.Fatal("未注册应返回 nil")
	}
	_ = c1
	_ = c2
}

// fakeGraphConsumer 最小 StreamConsumer
type fakeGraphConsumer struct{}

func (f *fakeGraphConsumer) StreamCall(ctx context.Context, event chan struct{}, done func(error)) {}
func (f *fakeGraphConsumer) ShouldHandleMessage() bool                                             { return false }
func (f *fakeGraphConsumer) IsDone() bool                                                          { return false }
