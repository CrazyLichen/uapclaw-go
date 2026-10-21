// echo 插件：端到端测试用最小扩展。
// 声明两个事件订阅 + transport 能力；HandleEvent 给 memory_blocks 追加一条；
// transport 为回环（Send 的数据写进 Recv 通道）。
package main

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin"
)

// echoExtension 回环扩展实现
type echoExtension struct {
	recvCh chan []byte
}

func newEchoExtension() *echoExtension {
	return &echoExtension{recvCh: make(chan []byte, 16)}
}

// Register 注册声明（对齐 Python register_extensions）
func (e *echoExtension) Register(ctx context.Context) ([]plugin.Declaration, error) {
	return []plugin.Declaration{{
		Metadata: plugin.Metadata{
			ID: "echo-ext", Name: "回环扩展", Version: "0.1.0",
			Description: "端到端测试用", Author: "test",
		},
		EventSubs: []plugin.EventSubscription{
			{Event: "agent_server:memory_before_chat", Priority: 100},
			{Event: "gateway:before_chat_request", Priority: 200},
		},
		ProvidesTransport: true,
	}}, nil
}

// Shutdown 关闭
func (e *echoExtension) Shutdown(ctx context.Context) error { return nil }

// HandleEvent 事件处理：memory_blocks 追加一条（验证写回闭环）
func (e *echoExtension) HandleEvent(ctx context.Context, event string, ctxData map[string]any) (map[string]any, error) {
	if ctxData == nil {
		ctxData = map[string]any{}
	}
	if event == "agent_server:memory_before_chat" {
		blocks, _ := ctxData["memory_blocks"].([]any)
		ctxData["memory_blocks"] = append(blocks, "来自echo插件")
	}
	if event == "gateway:before_chat_request" {
		if p, ok := ctxData["params"].(map[string]any); ok {
			p["echo_touched"] = true
		} else {
			ctxData["params"] = map[string]any{"echo_touched": true}
		}
	}
	return ctxData, nil
}

// Send 回环发送
func (e *echoExtension) Send(ctx context.Context, data []byte) error {
	e.recvCh <- data
	return nil
}

// Recv 返回接收通道
func (e *echoExtension) Recv() (<-chan []byte, error) { return e.recvCh, nil }

// Close 关闭
func (e *echoExtension) Close() error { close(e.recvCh); return nil }

func main() {
	plugin.ServeExtensionWithTransport(newEchoExtension(), newEchoExtension())
}
