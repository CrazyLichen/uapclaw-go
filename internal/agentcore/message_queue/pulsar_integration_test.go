//go:build integration

package message_queue

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/drunner/dmessage_queue"
)

// TestPulsarMessageQueue_真实收发 测试真实 Pulsar 端的完整链路。
//
// 运行方式: PULSAR_URL=pulsar://localhost:6650 go test -tags=integration ./internal/agentcore/message_queue/...
func TestPulsarMessageQueue_真实收发(t *testing.T) {
	url := os.Getenv("PULSAR_URL")
	if url == "" {
		t.Skip("未设置 PULSAR_URL，跳过真实 Pulsar 测试")
	}

	topic := "persistent://public/default/uapclaw-9.83-test"

	q := NewMessageQueuePulsar(config.NewPulsarConfig(url))
	q.Start()
	require.True(t, q.running.Load(), "Pulsar 客户端启动失败，检查 PULSAR_URL 是否可达")
	defer func() {
		if err := q.Stop(context.Background()); err != nil {
			t.Logf("Stop 报错: %v", err)
		}
	}()

	sub, err := q.Subscribe(topic)
	require.NoError(t, err)

	received := make(chan map[string]any, 1)
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		received <- payload
		return nil, nil
	})
	sub.Activate()

	msg := dmessage_queue.NewDmqRequestMessage(map[string]any{"q": "integration"})
	msg.MessageID = "it-1"
	msg.ReplyTopic = "reply.t"
	require.NoError(t, q.Produce(context.Background(), topic, msg))

	select {
	case payload := <-received:
		assert.Equal(t, "reply.t", payload["reply_topic"])
		assert.Equal(t, "DmqRequestMessage", payload["__class__"])
	case <-time.After(10 * time.Second):
		t.Fatal("10s 内未收到消息")
	}
}
