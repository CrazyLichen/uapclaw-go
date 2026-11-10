package harness_protocol

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCursor 验证 HarnessEventCursor 接口方法集完整的最小实现。
// 只断言签名，不实现任何租约行为（行为在 15.5 的 provider 基座落地）。
type fakeCursor struct {
	events []*HarnessEvent
	idx    int
	closed bool
}

func (f *fakeCursor) Iter() HarnessEventCursor { return f }

func (f *fakeCursor) Next(_ context.Context) (*HarnessEvent, error) {
	if f.idx >= len(f.events) {
		return nil, ErrEventStreamDone
	}
	ev := f.events[f.idx]
	f.idx++
	return ev, nil
}

func (f *fakeCursor) Close(_ context.Context) error {
	f.closed = true
	return nil
}

// 编译期：fakeCursor 满足 HarnessEventCursor 的 3 方法。
var _ HarnessEventCursor = (*fakeCursor)(nil)

// TestHarnessEventCursor_三方法 验证接口方法集与耗尽语义。
// Python: stream.py:13-27 HarnessEventCursor（__aiter__ / __anext__ / aclose）
func TestHarnessEventCursor_三方法(t *testing.T) {
	p, err := NewStateChangedEvent(HarnessStateIdle, HarnessStateRunning)
	require.NoError(t, err)
	ev, err := NewHarnessEvent(HarnessEventOptions{
		Sequence: 1, Timestamp: 1, Event: &p, HostSessionID: "s1", AgentID: "a1",
	})
	require.NoError(t, err)

	cur := &fakeCursor{events: []*HarnessEvent{&ev}}
	assert.Equal(t, cur, cur.Iter())

	got, err := cur.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, got.Sequence)

	_, err = cur.Next(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEventStreamDone)

	require.NoError(t, cur.Close(context.Background()))
	assert.True(t, cur.closed)
}

// TestErrEventStreamDone_哨兵穿透包装 验证 errors.Is 可穿透 %w 包装。
// Python: stream.py:12-22 __anext__ 抛 StopAsyncIteration
func TestErrEventStreamDone_哨兵穿透包装(t *testing.T) {
	assert.EqualError(t, ErrEventStreamDone, "harness event stream exhausted")
	wrapped := fmt.Errorf("cursor drained: %w", ErrEventStreamDone)
	assert.True(t, errors.Is(wrapped, ErrEventStreamDone))
	assert.False(t, errors.Is(errors.New("other"), ErrEventStreamDone))
}
