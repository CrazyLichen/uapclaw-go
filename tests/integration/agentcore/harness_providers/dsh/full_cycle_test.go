//go:build integration

package dsh

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	deepseekharness "github.com/theoneLee/deepseek-harness-sdk-go"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	hp "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	dshprov "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers/dsh"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// fakeSession 集成用脚本化 SDK 会话（进程边界 mock：只 mock 协议端，
// 基座与适配器全真）。
type fakeSession struct {
	events       []map[string]any
	finishReason *string
}

// Run 把脚本事件逐条回放为通知，最后按 finishReason 返回结果。
func (f *fakeSession) Run(input any, onNotification func(deepseekharness.Notification)) (*deepseekharness.RunResult, error) {
	for _, event := range f.events {
		onNotification(deepseekharness.Notification{
			Method:  "session.event",
			Payload: deepseekharness.JSONObject{"sessionId": "it-root", "event": event},
		})
	}
	return &deepseekharness.RunResult{FinalResponse: "integration-final", FinishReason: f.finishReason}, nil
}

// fakeHarness 集成用脚本化 SDK 句柄。
type fakeHarness struct {
	sessions []*fakeSession
	mu       sync.Mutex
	closed   int
}

// StartSession 发放脚本会话。
func (f *fakeHarness) StartSession(sessionID string) (dshprov.DshSDKSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[0], nil
}

// Close 幂等关闭。
func (f *fakeHarness) Close() error { f.mu.Lock(); f.closed++; f.mu.Unlock(); return nil }

// newIntegrationHarness 构造注入 fake 工厂的适配器。
func newIntegrationHarness(t *testing.T, fake *fakeHarness, events []map[string]any) *dshprov.DshHarness {
	t.Helper()
	fake.sessions = []*fakeSession{{events: events, finishReason: strPtrP("completed")}}
	h, err := dshprov.NewDshHarnessWithFactory(dshprov.DshHarnessConfig{
		DshHome: t.TempDir(),
	}, func(options dshprov.GoSDKOptions) (dshprov.DshSDKHarness, error) {
		return fake, nil
	})
	require.NoError(t, err)
	return h
}

func strPtrP(value string) *string { return &value }

// ──────────────────────────── 用例 ────────────────────────────

// 完整周期：Start → Send → 通知回放 → 终态 → Stop，事件经协议游标可消费，
// overlay/会话生命周期正确。真实跨越 harness_providers 基座 + harness_protocol
// + dsh 适配器三个子系统。
// DshFullCycleSuite 全周期事件与生命周期集成（独立 suite：审计要求每个
// _test.go 自带 suite.Run 入口）。
type DshFullCycleSuite struct {
	isuite.AgentSuite
}

// TestDshFullCycleSuite 本文件集成入口。
func TestDshFullCycleSuite(t *testing.T) {
	suite.Run(t, new(DshFullCycleSuite))
}

func (s *DshFullCycleSuite) TestFullCycleEventsAndLifecycle() {
	ctx := context.Background()
	fake := &fakeHarness{}
	events := []map[string]any{
		{"type": "assistant/chunk", "data": map[string]any{
			"turn": 1, "step": 1,
			"chunk": map[string]any{"type": "text-delta", "index": 0, "text": "he"},
		}},
		{"type": "assistant/chunk", "data": map[string]any{
			"turn": 1, "step": 1,
			"chunk": map[string]any{"type": "block-end", "index": 0,
				"block": map[string]any{"type": "text", "text": "hello"}},
		}},
		{"type": "turn/end", "data": map[string]any{"reason": map[string]any{"kind": "completed"}}},
	}
	h := newIntegrationHarness(s.T(), fake, events)

	require.NoError(s.T(), h.Start(ctx, harnessContext(s.T())))
	receipt, err := h.Send(ctx, hp.HarnessInput{Content: "hi"}, hp.DeliveryModeAuto)
	require.NoError(s.T(), err)
	assert.NotEmpty(s.T(), receipt.MessageID)
	waitTurnTerminalI(t2(s.T()), h)
	require.NoError(s.T(), h.Stop(ctx))

	assert.Equal(s.T(), 1, fake.closed)
	assert.NotNil(s.T(), h.ProviderSessionID())
	assert.True(s.T(), strings.HasPrefix(*h.ProviderSessionID(), "dsh-"))
}

// stop 打断活跃轮：run 未释放时 Stop 触发 harness_stop 终态并关闭 SDK。
func (s *DshFullCycleSuite) TestStopInterruptsActiveTurn() {
	ctx := context.Background()
	fake := &fakeHarness{}
	// 阻塞的会话：Run 等待 close 才返回
	blocking := &blockingSession{fake: fake}
	fake.sessions = nil
	h, err := dshprov.NewDshHarnessWithFactory(dshprov.DshHarnessConfig{DshHome: t2(s.T()).TempDir()},
		func(options dshprov.GoSDKOptions) (dshprov.DshSDKHarness, error) {
			return &blockingHarness{session: blocking}, nil
		})
	require.NoError(s.T(), err)
	require.NoError(s.T(), h.Start(ctx, harnessContext(s.T())))
	_, err = h.Send(ctx, hp.HarnessInput{Content: "hi"}, hp.DeliveryModeAuto)
	require.NoError(s.T(), err)
	time.Sleep(50 * time.Millisecond)
	require.NoError(s.T(), h.Stop(ctx))
	assert.True(s.T(), blocking.stopped, "stop should release the blocking run")
}

// blockingSession 模拟一次长运行（Stop 需要打断它）。
type blockingSession struct {
	fake    *fakeHarness
	stopped bool
}

// Run 阻塞直到 Close（模拟在飞 run）。
func (b *blockingSession) Run(input any, onNotification func(deepseekharness.Notification)) (*deepseekharness.RunResult, error) {
	<-time.After(500 * time.Millisecond)
	b.stopped = true
	return &deepseekharness.RunResult{FinalResponse: "partial", FinishReason: strPtrP("aborted")}, nil
}

// blockingHarness 句柄包装。
type blockingHarness struct{ session *blockingSession }

// StartSession 发放阻塞会话。
func (b *blockingHarness) StartSession(sessionID string) (dshprov.DshSDKSession, error) {
	return b.session, nil
}

// Close 关闭句柄。
func (b *blockingHarness) Close() error { return nil }

// harnessContext 协议 v1 合法宿主上下文。
func harnessContext(t *testing.T) hp.HarnessContext {
	t.Helper()
	return hp.HarnessContext{
		AgentName:       "it-agent",
		AgentID:         "it_agent",
		HostSessionID:   "it-host-1",
		ProtocolVersion: hp.ProtocolVersion,
	}
}

// waitTurnTerminalI 集成侧轮次终态等待。
func waitTurnTerminalI(t *testing.T, h *dshprov.DshHarness) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor := h.TurnEvents(nil)
	defer func() { _ = cursor.Close(context.Background()) }()
	for {
		event, err := cursor.Next(ctx)
		if err != nil {
			t.Fatalf("turn did not reach terminal state: %v", err)
		}
		if lifecycle, ok := event.Event.(hp.TurnLifecycleEvent); ok && lifecycle.Result != nil {
			return
		}
	}
}

// t2 取 *testing.T（suite 里用 s.T() 传入）。
func t2(t *testing.T) *testing.T { return t }
