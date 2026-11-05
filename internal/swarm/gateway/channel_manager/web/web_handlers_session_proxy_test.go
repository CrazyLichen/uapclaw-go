package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionProxy_注册齐全 session.rename / session.plan_status 两个薄代理已注册。
func TestSessionProxy_注册齐全(t *testing.T) {
	d := RegisterWebHandlers(nil)

	for _, method := range []string{"session.rename", "session.plan_status"} {
		_, ok := d.methodHandlers[method]
		assert.True(t, ok, "方法未注册: %s", method)
	}
}

// TestSessionProxy_无客户端回SERVICE_UNAVAILABLE 缺 AgentServer 客户端时回上游同款文案。
func TestSessionProxy_无客户端回SERVICE_UNAVAILABLE(t *testing.T) {
	wc := newTestChannel()

	for _, method := range []string{"session.rename", "session.plan_status"} {
		_, err := callHandler(sessionProxyForward(wc, nil, method), map[string]any{"title": "x"}, "sid")
		require.Error(t, err, "%s 无客户端应失败", method)
		assert.Equal(t, WsErrServiceUnavailable, errorCode(err))
		assert.Equal(t, "AgentServer is unavailable", err.Error())
	}
}
