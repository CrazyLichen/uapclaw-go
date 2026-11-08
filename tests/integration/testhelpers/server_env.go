//go:build integration

package testhelpers

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	srvruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ServerEnv 进程内 AgentServer 环境：按 E2A 信封投递请求，读回响应/push 帧。
//
// 用途：替代「IT 直连 adapter + 手工补 plan 同步」的旧脚手架，让集成用例从
// AgentServer 入口驱动（分发 → AgentRuntime → adapter），断言看到真实链路行为。
type ServerEnv struct {
	// T 测试句柄
	T *testing.T
	// Transport 进程内传输通道
	Transport *transport.ChannelTransport
	// Server 受测 AgentServer
	Server *server.AgentServer
	// mu 保护响应与 push 缓冲
	mu sync.Mutex
	// responses 非流式响应（按到达顺序）
	responses []*schema.AgentResponse
	// chunks 流式 chunk（按到达顺序）
	chunks []*schema.AgentResponseChunk
	// pushes 服务端主动 push 帧
	pushes []map[string]any
	// cancel 停止读取协程与 server
	cancel context.CancelFunc
	// seq 请求序号（生成唯一 request_id）
	seq int64
	// ChannelID 请求渠道（默认 web）
	ChannelID string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// serverEnvWaitTimeout 单次等待响应的上限
	serverEnvWaitTimeout = 30 * time.Second
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewServerEnv 启动进程内 AgentServer 并开始收集出站帧。
func NewServerEnv(t *testing.T, cfg *config.Config, factory srvruntime.AgentFactory) *ServerEnv {
	t.Helper()
	tr := transport.NewChannelTransport()
	srv := server.NewAgentServer(cfg, tr)
	if factory != nil {
		srv.SetAgentFactoryForTest(factory)
		// Start() 在 goroutine 里应用 override；预创建/首轮请求前必须已生效。
		srv.AgentManager().SetAgentFactory(factory)
	}

	ctx, cancel := context.WithCancel(context.Background())
	env := &ServerEnv{T: t, Transport: tr, Server: srv, cancel: cancel, ChannelID: "web"}
	if err := srv.Start(ctx); err != nil {
		cancel()
		t.Fatalf("启动 AgentServer 失败: %v", err)
	}
	go env.readLoop(ctx)
	t.Cleanup(func() {
		cancel()
		_ = srv.Stop()
	})
	return env
}

// Send 投递一条请求，返回 request_id。
func (e *ServerEnv) Send(method string, params map[string]any, sessionID string, isStream bool) string {
	e.T.Helper()
	requestID := fmt.Sprintf("it-req-%d", atomic.AddInt64(&e.seq, 1))
	envelope := map[string]any{
		"request_id": requestID,
		"method":     method,
		"params":     params,
		"channel":    e.channelID(),
		"is_stream":  isStream,
		"timestamp":  float64(time.Now().UnixNano()) / float64(time.Second),
	}
	if sessionID != "" {
		envelope["session_id"] = sessionID
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		e.T.Fatalf("编码信封失败: %v", err)
	}
	if err := e.Transport.Send(context.Background(), raw); err != nil {
		e.T.Fatalf("投递请求失败: %v", err)
	}
	return requestID
}

// AwaitUnary 等待指定请求的非流式响应。
func (e *ServerEnv) AwaitUnary(requestID string) *schema.AgentResponse {
	e.T.Helper()
	deadline := time.Now().Add(serverEnvWaitTimeout)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		idx := -1
		for i, resp := range e.responses {
			if resp.RequestID == requestID {
				idx = i
				break
			}
		}
		if idx < 0 && len(e.responses) > 0 {
			// 适配器未回填 request_id 时按到达顺序兜底
			idx = 0
		}
		if idx >= 0 {
			resp := e.responses[idx]
			e.responses = append(e.responses[:idx], e.responses[idx+1:]...)
			e.mu.Unlock()
			return resp
		}
		e.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	e.T.Fatalf("等待非流式响应超时: request_id=%s", requestID)
	return nil
}

// AwaitStreamEnd 等待指定请求的流式 chunk 直到 is_complete。
func (e *ServerEnv) AwaitStreamEnd(requestID string) []*schema.AgentResponseChunk {
	e.T.Helper()
	deadline := time.Now().Add(serverEnvWaitTimeout)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		var out []*schema.AgentResponseChunk
		rest := make([]*schema.AgentResponseChunk, 0, len(e.chunks))
		done := false
		for _, chunk := range e.chunks {
			if chunk.RequestID == requestID || chunk.RequestID == "" {
				out = append(out, chunk)
				if chunk.IsComplete {
					done = true
				}
				continue
			}
			rest = append(rest, chunk)
		}
		if done {
			e.chunks = rest
			e.mu.Unlock()
			return out
		}
		e.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	e.T.Fatalf("等待流式结束超时: request_id=%s", requestID)
	return nil
}

// Pushes 返回当前已收到的 push 帧快照。
func (e *ServerEnv) Pushes() []map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]map[string]any(nil), e.pushes...)
}

// LastPushPayload 返回最近一次 push 帧的 payload（无 push 时返回 nil）。
func (e *ServerEnv) LastPushPayload() map[string]any {
	pushes := e.Pushes()
	if len(pushes) == 0 {
		return nil
	}
	return PushPayload(pushes[len(pushes)-1])
}

// PushPayload 从 push 帧取出业务 payload。
//
// E2A 帧把业务负载放在 body.delta（chunk 形）或 body.payload（response 形）；
// 兼容旧的顶层 payload 形状。
func PushPayload(push map[string]any) map[string]any {
	if payload, ok := push["payload"].(map[string]any); ok {
		return payload
	}
	body, _ := push["body"].(map[string]any)
	if body == nil {
		return nil
	}
	if delta, ok := body["delta"].(map[string]any); ok {
		return delta
	}
	if payload, ok := body["payload"].(map[string]any); ok {
		return payload
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// channelID 返回请求渠道标识。
func (e *ServerEnv) channelID() string {
	if e.ChannelID == "" {
		return "web"
	}
	return e.ChannelID
}

// readLoop 持续消费出站帧并按类型分类。
func (e *ServerEnv) readLoop(ctx context.Context) {
	recvCh, err := e.Transport.Recv()
	if err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-recvCh:
			if !ok {
				return
			}
			e.dispatch(data)
		}
	}
}

// dispatch 解析单帧并归类。
func (e *ServerEnv) dispatch(data []byte) {
	var frame map[string]any
	if err := json.Unmarshal(data, &frame); err != nil {
		return
	}
	if frame["type"] == "event" {
		return
	}
	if metadata, ok := frame["metadata"].(map[string]any); ok {
		if isPush, _ := metadata[e2a.E2AWireServerPushKey].(bool); isPush {
			e.mu.Lock()
			e.pushes = append(e.pushes, frame)
			e.mu.Unlock()
			return
		}
	}
	if isStream, _ := frame["is_stream"].(bool); isStream {
		chunk, err := e2a.ParseAgentServerWireChunk(frame)
		if err != nil {
			return
		}
		e.mu.Lock()
		e.chunks = append(e.chunks, chunk)
		e.mu.Unlock()
		return
	}
	resp, err := e2a.ParseAgentServerWireUnary(frame)
	if err != nil {
		return
	}
	e.mu.Lock()
	e.responses = append(e.responses, resp)
	e.mu.Unlock()
}
