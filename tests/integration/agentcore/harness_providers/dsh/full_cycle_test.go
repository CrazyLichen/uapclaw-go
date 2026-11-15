//go:build integration

// Package dsh 集成测试：fake SDK 工厂驱动的 DshHarness 全周期链路。
// 真实跨 internal 二级子系统：harness_providers 基座（SerializedTurnHarness/
// 事件缓冲/工厂注册）+ harness_protocol（事件/结果/上下文）+ dsh 适配器
// （config/composition/mapping）。SDK 边界为进程缝，用脚本式 fake（上游
// test_dsh_e2e 需真实 dsh runtime + DEEPSEEK_API_KEY，按 spec §6.3 不迁；
// 本套件覆盖其编排语义的单测级等价物）。
package dsh

import (
	"context"
	"sync"
	"testing"
	"time"

	deepseekharness "github.com/theoneLee/deepseek-harness-sdk-go"
	dsh "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers/dsh"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"

	"github.com/stretchr/testify/suite"

	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 集成级 fake SDK ────────────────────────────

// itestRunScript 一次 Run 的脚本。
type itestRunScript struct {
	notifications []deepseekharness.Notification
	result        *deepseekharness.RunResult
	err           error
	waitReleased  bool
	released      chan struct{}
	closeCount    *int
	closeMu       *sync.Mutex
}

// itestFakeHarness 集成级 fake SDKHarness（只实现导出接口 dsh.SDKHarness）。
type itestFakeHarness struct {
	state     *itestFakeState
	sessionID string
}

// itestFakeSession 集成级 fake SDKSession。
type itestFakeSession struct {
	script    *itestRunScript
	sessionID string
}

// itestFakeState 记录构造选项与会话。
type itestFakeState struct {
	mu                  sync.Mutex
	constructorOptions  []dsh.SDKOptions
	scripts             []*itestRunScript
	closeCount          int
}

func (s *itestFakeState) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, script := range s.scripts {
		select {
		case <-script.released:
		default:
			close(script.released)
		}
	}
}

// StartSession dsh.SDKHarness 实现。
func (f *itestFakeHarness) StartSession(sessionID string) (dsh.SDKSession, error) {
	f.sessionID = sessionID
	return &itestFakeSession{script: f.state.scripts[0], sessionID: sessionID}, nil
}

// Close dsh.SDKHarness 实现（幂等）。
func (f *itestFakeHarness) Close() error {
	f.state.mu.Lock()
	f.state.closeCount++
	f.state.mu.Unlock()
	f.state.release()
	return nil
}

// Run dsh.SDKSession 实现：同步回放通知后返回脚本结果。
func (s *itestFakeSession) Run(
	input any, onNotification func(deepseekharness.Notification),
) (*deepseekharness.RunResult, error) {
	for _, raw := range s.script.notifications {
		payload := map[string]any{}
		for key, value := range raw.Payload {
			payload[key] = value
		}
		if raw.Method == "session.event" {
			if _, ok := payload["sessionId"]; !ok {
				payload["sessionId"] = s.sessionID
			}
		}
		onNotification(deepseekharness.Notification{Method: raw.Method, Payload: payload})
	}
	if s.script.waitReleased {
		select {
		case <-s.script.released:
		case <-time.After(5 * time.Second):
			return nil, context.DeadlineExceeded
		}
	}
	if s.script.err != nil {
		return nil, s.script.err
	}
	return s.script.result, nil
}

// newItestHarness 构造挂集成级 fake 的 DshHarness（跨子系统入口）。
func newItestHarness(state *itestFakeState) (*dsh.DshHarness, error) {
	return dsh.NewDshHarnessWithFactory(dsh.DefaultDshHarnessConfig(),
		func(options dsh.SDKOptions) (dsh.SDKHarness, error) {
			state.mu.Lock()
			state.constructorOptions = append(state.constructorOptions, options)
			state.mu.Unlock()
			return &itestFakeHarness{state: state}, nil
		})
}

// ──────────────────────────── 套件 ────────────────────────────

// DshFullCycleSuite 全周期编排：Start → Send → 事件消费 → Stop。
type DshFullCycleSuite struct {
	isuite.AgentSuite
}

func TestDshFullCycleSuite(t *testing.T) {
	suite.Run(t, new(DshFullCycleSuite))
}

// harnessCtx 构造最小合法宿主上下文。
func (s *DshFullCycleSuite) harnessCtx() harnessprotocol.HarnessContext {
	testCtx, err := harnessprotocol.NewHarnessContext("researcher", "team-a_researcher",
		"team-session-1", "", harnessprotocol.ContextOptions{})
	s.Require().NoError(err)
	return testCtx
}

// textInput 构造文本输入。
func (s *DshFullCycleSuite) textInput(text string) harnessprotocol.HarnessInput {
	input, err := harnessprotocol.NewHarnessInput(text, nil)
	s.Require().NoError(err)
	return input
}

func strPtr(v string) *string { return &v }

// TestFullCycle_事件序列与终态 验证 Start→Send→事件映射→终态→Stop 全链路。
func (s *DshFullCycleSuite) TestFullCycle_事件序列与终态() {
	state := &itestFakeState{scripts: []*itestRunScript{{
		released: make(chan struct{}),
		result: &deepseekharness.RunResult{FinalResponse: "done", FinishReason: strPtr("completed")},
		notifications: []deepseekharness.Notification{
			notificationOf("session.event", map[string]any{"event": map[string]any{
				"type": "assistant/chunk",
				"data": map[string]any{
					"turn": 1, "step": 1,
					"chunk": map[string]any{"type": "block-end", "index": 0,
						"block": map[string]any{"type": "text", "text": "hello"}},
				},
			}}),
			notificationOf("session.event", map[string]any{"event": map[string]any{
				"type": "turn/end",
				"data": map[string]any{"reason": map[string]any{"kind": "completed"}},
			}}),
		},
	}}}
	h, err := newItestHarness(state)
	s.Require().NoError(err)
	ctx := context.Background()
	s.Require().NoError(h.Start(ctx, s.harnessCtx()))

	cursor := h.Events()
	receipt, err := h.Send(ctx, s.textInput("go"), harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	s.Require().NotNil(receipt.MessageID)

	// 泵到终态
	var terminal *harnessprotocol.TurnResult
	var sawFinal bool
	for i := 0; i < 200 && terminal == nil; i++ {
		event, err := cursor.Next(ctx)
		s.Require().NoError(err)
		if event == nil {
			break
		}
		if out, ok := event.Event.(harnessprotocol.OutputEvent); ok &&
			out.Operation == harnessprotocol.OutputOperationFinal {
			sawFinal = true
			s.Equal("hello", out.Content)
		}
		if lifecycle, ok := event.Event.(harnessprotocol.TurnLifecycleEvent); ok &&
			harnessprotocol.IsTerminalTurnEventKind(lifecycle.Kind) {
			terminal = lifecycle.Result
		}
	}
	s.Require().NotNil(terminal)
	s.Equal(harnessprotocol.TurnStatusCompleted, terminal.Status)
	s.True(sawFinal)
	s.Equal("done", terminal.FinalOutput)

	s.Require().NoError(h.Stop(ctx))
	s.Equal(harnessprotocol.HarnessStateTerminated, h.State())
	// overlay 未创建（无 prompt/mcp/skills）
	state.mu.Lock()
	options := state.constructorOptions
	state.mu.Unlock()
	s.Require().Len(options, 1)
	s.Empty(options[0].ExtraEnv)
}

// TestFullCycle_Stop中断链 Stop 释放阻塞 Run → HARNESS_STOP 终态。
func (s *DshFullCycleSuite) TestFullCycle_Stop中断链() {
	state := &itestFakeState{scripts: []*itestRunScript{{
		err: context.Canceled, waitReleased: true,
		released: make(chan struct{}),
	}}}
	h, err := newItestHarness(state)
	s.Require().NoError(err)
	ctx := context.Background()
	s.Require().NoError(h.Start(ctx, s.harnessCtx()))

	_, err = h.Send(ctx, s.textInput("active"), harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	// Stop → Close 释放 Run → stop_requested → 终止
	s.Require().NoError(h.Stop(ctx))
	s.Equal(harnessprotocol.HarnessStateTerminated, h.State())

	state.mu.Lock()
	closeCount := state.closeCount
	state.mu.Unlock()
	s.Equal(1, closeCount)
}

// notificationOf 构造一条 SDK 通知。
func notificationOf(method string, payload map[string]any) deepseekharness.Notification {
	return deepseekharness.Notification{Method: method, Payload: payload}
}
