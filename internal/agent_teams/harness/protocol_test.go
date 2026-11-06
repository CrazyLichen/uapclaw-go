package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventBus_状态事件同步送达 验证订阅回调在触发协程内同步执行（C7）。
func TestEventBus_状态事件同步送达(t *testing.T) {
	bus := newEventBus()
	called := false
	err := bus.subscribe(context.Background(),
		func(_ context.Context, ev HarnessStateEvent) { called = true },
		nil,
	)
	require.NoError(t, err)

	bus.emitState(context.Background(), HarnessStateEvent{
		Old: HarnessStateIdle, New: HarnessStateRunning, SessionID: "s1",
	})
	assert.True(t, called, "回调必须同步执行，不能另起协程")
}

// TestEventBus_轮次事件载荷 验证 on_round 三个关键字段可见（C7/C11）。
func TestEventBus_轮次事件载荷(t *testing.T) {
	bus := newEventBus()
	var got HarnessRoundEvent
	require.NoError(t, bus.subscribe(context.Background(), nil,
		func(_ context.Context, ev HarnessRoundEvent) { got = ev },
	))

	bus.emitRound(context.Background(), HarnessRoundEvent{Kind: "started", RoundID: 3})
	assert.Equal(t, "started", got.Kind)
	assert.Equal(t, 3, got.RoundID)
}

// TestHarnessProtocol_哨兵错误可判定 验证 ErrPauseUnsupported 可用 errors.Is 判断（C2）。
func TestHarnessProtocol_哨兵错误可判定(t *testing.T) {
	assert.True(t, errors.Is(ErrPauseUnsupported, ErrPauseUnsupported))
	assert.EqualError(t, ErrPauseUnsupported, "harness: pause/resume 尚未实现（25.1c）")
}

// TestProtocol_类型转换边界 覆盖 toHarnessState/toString/toInt/toMap 各分支。
func TestProtocol_类型转换边界(t *testing.T) {
	// toHarnessState
	assert.Equal(t, HarnessStateRunning, toHarnessState("running"))
	assert.Equal(t, HarnessStatePaused, toHarnessState(HarnessStatePaused)) // HarnessState 类型
	assert.Equal(t, HarnessStateIdle, toHarnessState(123))                  // 非法类型兜底
	assert.Equal(t, HarnessStateIdle, toHarnessState(nil))

	// toString
	assert.Equal(t, "x", toString("x"))
	assert.Equal(t, "", toString(123))
	assert.Equal(t, "", toString(nil))

	// toInt
	assert.Equal(t, 3, toInt(3))
	assert.Equal(t, 3, toInt(int64(3)))
	assert.Equal(t, 3, toInt(float64(3)))
	assert.Equal(t, 0, toInt("nope"))

	// toMap
	assert.Equal(t, map[string]any{"k": 1}, toMap(map[string]any{"k": 1}))
	assert.Nil(t, toMap("not-a-map"))
	assert.Nil(t, toMap(nil))
}
