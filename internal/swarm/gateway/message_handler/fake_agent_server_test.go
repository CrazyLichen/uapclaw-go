package message_handler

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeReply 假 AgentServer 的响应构造器：入参为请求的 request_id 与原请求帧。
type fakeReply func(requestID string, req map[string]any) *schema.AgentResponse

// fakeAgentServer 用 ChannelTransport 扮演 AgentServer：
// 启动时先回 connection.ack 让 AgentClient 进入 connected，之后按请求 method
// 查表把响应编码成 E2A 线帧回送。
type fakeAgentServer struct {
	// tr 进程内传输
	tr *transport.ChannelTransport
	// client 被测代码持有的 AgentClient
	client *routing.AgentClient
	// mu 保护 replies / delays / methods
	mu sync.Mutex
	// replies method → 响应构造器
	replies map[string]fakeReply
	// delays method → 回复前延迟（用于制造时序屏障）
	delays map[string]time.Duration
	// methods 已收到的请求 method 序列（按到达顺序）
	methods []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newFakeAgentServer 启动假 AgentServer，返回它和已建立连接的 MessageHandler。
func newFakeAgentServer(t *testing.T, replies map[string]fakeReply) (*fakeAgentServer, *MessageHandler) {
	t.Helper()

	f := &fakeAgentServer{
		tr:      transport.NewChannelTransport(),
		replies: replies,
		delays:  make(map[string]time.Duration),
	}
	f.client = routing.NewAgentClient(f.tr)

	ack, err := json.Marshal(transport.BuildConnectionAckFrame())
	if err != nil {
		t.Fatalf("marshal connection.ack: %v", err)
	}
	// 先入队 ack：Connect 启动 receiverLoop 后立即取到，serverReady 置真
	f.tr.RecvCh() <- ack

	go f.serve()

	mh := NewMessageHandler(f.client)
	if err := f.client.Connect(context.Background()); err != nil {
		t.Fatalf("connect fake agent server: %v", err)
	}
	if !f.client.IsConnected() {
		t.Fatal("fake agent server: AgentClient 未进入 connected 状态")
	}

	t.Cleanup(func() {
		_ = f.tr.Close()
		f.client.Disconnect()
	})
	return f, mh
}

// serve 消费 Gateway 发来的请求帧，按 method 查表回送响应。
func (f *fakeAgentServer) serve() {
	for frame := range f.tr.SendCh() {
		var req map[string]any
		if err := json.Unmarshal(frame, &req); err != nil {
			continue
		}
		method, _ := req["method"].(string)
		rid := transport.WireRequestIDKey(req["request_id"])

		f.mu.Lock()
		f.methods = append(f.methods, method)
		reply := f.replies[method]
		delay := f.delays[method]
		f.mu.Unlock()

		if delay > 0 {
			time.Sleep(delay)
		}
		if reply == nil {
			continue
		}
		resp := reply(rid, req)
		if resp == nil {
			continue
		}

		wire := e2a.EncodeAgentResponseForWire(resp, rid, 0)
		// 强制回填 request_id / response_id：AgentClient 的队列按 request_id 路由，
		// 不依赖 E2A 编码器是否把 responseID 写进顶层字段。
		wire["request_id"] = rid
		wire["response_id"] = rid

		out, err := json.Marshal(wire)
		if err != nil {
			continue
		}
		f.tr.RecvCh() <- out
	}
}

// methodCount 返回指定 method 已收到的请求数。
func (f *fakeAgentServer) methodCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.methods {
		if m == method {
			n++
		}
	}
	return n
}

// setDelay 为指定 method 设置回复延迟。
func (f *fakeAgentServer) setDelay(method string, d time.Duration) {
	f.mu.Lock()
	f.delays[method] = d
	f.mu.Unlock()
}

// waitNotice 从出站队列取下一条通知。
func waitNotice(t *testing.T, mh *MessageHandler) *schema.Message {
	t.Helper()
	select {
	case notice := <-mh.robotMessages:
		return notice
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for channel notice")
		return nil
	}
}

// waitNoticeContaining 跳过不含 substr 的通知，直到取到匹配的一条。
func waitNoticeContaining(t *testing.T, mh *MessageHandler, substr string) *schema.Message {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case notice := <-mh.robotMessages:
			content, _ := notice.Payload["content"].(string)
			errText, _ := notice.Payload["error"].(string)
			if strings.Contains(content, substr) || strings.Contains(errText, substr) {
				return notice
			}
		case <-deadline:
			t.Fatalf("timeout waiting for notice containing %q", substr)
			return nil
		}
	}
}
