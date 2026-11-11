package adapter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestProcessMessageStreamImpl_根走外层选择函数 验证第 0 步调用的是外层。
// Python: interface_deep.py:18963 _get_session_adapter_for_request
//
// 构造一个根 adapter（isSessionScoped=false）并注入 child 工厂，
// 确认拿到的 child 是会话子适配器而非根自身（外层函数的核心作用之一）。
func TestProcessMessageStreamImpl_根走外层选择函数(t *testing.T) {
	d := NewDeepAdapter()
	d.isSessionScoped = false
	d.channelID = "process_cli"
	d.childMode = "agent"
	d.sessionChildFactory = func() (AgentAdapter, error) {
		child := NewDeepAdapter()
		child.channelID = "process_cli"
		return child, nil
	}

	sid := "sess-wiring-001"
	req := &schema.AgentRequest{
		RequestID: "r1",
		ChannelID: "process_cli",
		ReqMethod: schema.ReqMethodChatSend,
		SessionID: &sid,
	}

	// 走外层选择函数应成功拿到 child（不是根自身）
	child, err := d.getSessionAdapterForRequest(context.Background(), req, sessionAdapterRequestOptions{
		ReserveActivity: false,
	})
	require.NoError(t, err)
	require.NotNil(t, child)
	require.True(t, child.IsSessionScoped(), "应拿到会话子适配器")
}

// TestProcessMessageStreamImpl_消费runOptions 验证 child 上的 run options 被消费。
// Python: :18987-18993
func TestProcessMessageStreamImpl_消费runOptions(t *testing.T) {
	child := NewDeepAdapter()
	child.isSessionScoped = true
	child.channelID = "process_cli"
	maxTurns := 7
	child.processCliRunOptions = &ProcessCliRunOptions{MaxTurns: &maxTurns}

	options := child.TakeProcessCliRunOptions()
	require.NotNil(t, options)
	require.Equal(t, 7, *options.MaxTurns)

	// 取过一次即清空
	require.Nil(t, child.TakeProcessCliRunOptions())
}
