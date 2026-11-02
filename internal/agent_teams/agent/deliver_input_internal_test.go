package agent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	harnesspkg "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
)

// TestTeamAgent_degradeAtRoundBoundary_无活跃循环入队 轮次边界失败降级为排队且返回 nil。
//
// M-02 方案 A：`ErrNoActiveLoop` 表示"轮次刚结束、controller 已回收"的正常竞态，
// 此时消息必须进 pendingInputs 而不是给用户一个假错误。
func TestTeamAgent_degradeAtRoundBoundary_无活跃循环入队(t *testing.T) {
	sc := &StreamController{}
	a := &TeamAgent{streamController: sc}

	require.NoError(t, a.degradeAtRoundBoundary("hello", harnesspkg.ErrNoActiveLoop))

	sc.mu.Lock()
	pending := append([]any(nil), sc.pendingInputs...)
	sc.mu.Unlock()
	assert.Equal(t, []any{"hello"}, pending)
}

// TestTeamAgent_degradeAtRoundBoundary_其他错误上抛 非轮次边界错误必须原样返回。
func TestTeamAgent_degradeAtRoundBoundary_其他错误上抛(t *testing.T) {
	sc := &StreamController{}
	a := &TeamAgent{streamController: sc}

	boom := errors.New("publish failed")
	require.ErrorIs(t, a.degradeAtRoundBoundary("hello", boom), boom)

	sc.mu.Lock()
	defer sc.mu.Unlock()
	assert.Empty(t, sc.pendingInputs, "非轮次边界错误不应入队")
}
