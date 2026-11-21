package codebuddy

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	providers "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
)

// HarnessSuite CodebuddyHarness 行为套件（fake transport 注入）。
type HarnessSuite struct{ suite.Suite }

// TestHarnessSuite 套件入口。
func TestHarnessSuite(t *testing.T) { suite.Run(t, new(HarnessSuite)) }

// fakeTransport 脚本化传输缝：SendFrame 交给 onSend，ReadFrame 按脚本出帧。
type fakeTransport struct {
	mu       sync.Mutex
	sent     []map[string]any
	frames   []WireFrame
	index    int
	closed   bool
	hang     bool
	sendHook func(frame map[string]any)
}

// SendFrame 记录外发帧并可注入钩子。
func (f *fakeTransport) SendFrame(frame map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, frame)
	if f.sendHook != nil {
		f.sendHook(frame)
	}
	return nil
}

// ReadFrame 按脚本回放；hang 模拟挂起（ctx 取消返回）。
func (f *fakeTransport) ReadFrame(ctx context.Context) (WireFrame, error) {
	if f.hang {
		<-ctx.Done()
		return WireFrame{}, ctx.Err()
	}
	if f.index >= len(f.frames) {
		return WireFrame{}, context.DeadlineExceeded
	}
	frame := f.frames[f.index]
	f.index++
	return frame, nil
}

// StderrTail fake 无 stderr。
func (f *fakeTransport) StderrTail() string { return "" }

// Close 幂等关闭。
func (f *fakeTransport) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// scriptFactory 构造带脚本 fake 的工厂。
func (s *HarnessSuite) scriptFactory(frames ...string) (TransportFactory, *fakeTransport) {
	fake := &fakeTransport{}
	for _, line := range frames {
		frame, err := parseFrame(json.RawMessage(line))
		s.Require().NoError(err)
		fake.frames = append(fake.frames, frame)
	}
	return func(options TransportOptions) (Transport, error) {
		return fake, nil
	}, fake
}

// collectTurnEvents 消费单轮事件。
func (s *HarnessSuite) collectTurnEvents(h *CodebuddyHarness, turnID string) []harnessprotocol.HarnessEvent {
	id := turnID
	cursor := h.TurnEvents(&id)
	defer func() { _ = cursor.Close(context.Background()) }()
	var events []harnessprotocol.HarnessEvent
	for {
		event, err := cursor.Next(context.Background())
		if err != nil {
			break
		}
		events = append(events, *event)
	}
	return events
}

// newHarness 构造注入 fake 的 harness。
func (s *HarnessSuite) newHarness(factory TransportFactory) *CodebuddyHarness {
	harness, err := NewCodebuddyHarnessWithFactory(CodebuddyConfig{}, factory)
	s.Require().NoError(err)
	return harness
}

// TestStart_idle与OpenSession Start 后 idle 且会话标识来自 init 帧。
func (s *HarnessSuite) TestStart_idle与OpenSession() {
	factory, _ := s.scriptFactory(
		`{"type":"system","subtype":"init","session_id":"sess-1"}`)
	h := s.newHarness(factory)
	s.Require().NoError(h.Start(context.Background(), harnessprotocol.HarnessContext{
		AgentName: "e2e", AgentID: "e2e", HostSessionID: "host-1",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
	}))
	s.Equal(harnessprotocol.HarnessStateIdle, h.State())
	// 会话标识协商推迟到 ExecuteTurn 首帧（CLI 2.181.0 实测）；此处断言占位非空
	s.Require().NotNil(h.ProviderSessionID())
	s.NotEmpty(*h.ProviderSessionID())
	s.Require().NoError(h.Stop(context.Background()))
}

// TestSend_text整轮 一轮：OutputEvent(PONG) + FINISHED。
func (s *HarnessSuite) TestSend_text整轮() {
	factory, _ := s.scriptFactory(
		`{"type":"system","subtype":"init","session_id":"sess-1"}`,
		`{"type":"assistant","session_id":"sess-1","message":{"content":[{"type":"text","text":"PONG"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"PONG","duration_ms":4069,"num_turns":2}`)
	h := s.newHarness(factory)
	s.Require().NoError(h.Start(context.Background(), harnessprotocol.HarnessContext{
		AgentName: "e2e", AgentID: "e2e", HostSessionID: "host-1",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
	}))
	receipt, err := h.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "hi"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectTurnEvents(h, receipt.TurnID)
	kinds := map[string]int{}
	for _, event := range events {
		switch payload := event.Event.(type) {
		case harnessprotocol.TurnLifecycleEvent:
			kinds[string(payload.Kind)]++
		case harnessprotocol.OutputEvent:
			s.Equal("PONG", payload.Content)
		}
	}
	s.Equal(1, kinds[string(harnessprotocol.TurnEventKindStarted)])
	s.Equal(1, kinds[string(harnessprotocol.TurnEventKindFinished)])
	// init 帧在轮内到达：真实会话标识已协商
	s.Equal("sess-1", h.sessionID)
	s.Require().NoError(h.Stop(context.Background()))
}

// TestSend_result错误轮 is_error → FAILED + 结构化错误。
func (s *HarnessSuite) TestSend_result错误轮() {
	factory, _ := s.scriptFactory(
		`{"type":"system","subtype":"init","session_id":"sess-1"}`,
		`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"boom"}`)
	h := s.newHarness(factory)
	s.Require().NoError(h.Start(context.Background(), harnessprotocol.HarnessContext{
		AgentName: "e2e", AgentID: "e2e", HostSessionID: "host-1",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
	}))
	receipt, err := h.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "hi"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectTurnEvents(h, receipt.TurnID)
	var failed *harnessprotocol.TurnLifecycleEvent
	for _, event := range events {
		if payload, ok := event.Event.(harnessprotocol.TurnLifecycleEvent); ok && payload.Kind == harnessprotocol.TurnEventKindFailed {
			failed = &payload
		}
	}
	s.Require().NotNil(failed)
	s.Require().NotNil(failed.Result)
	s.Require().NotNil(failed.Result.Error)
	s.Require().NoError(h.Stop(context.Background()))
}

// TestAbort_graceful写控制帧 GRACEFUL 外发 interrupt 控制帧。
func (s *HarnessSuite) TestAbort_graceful写控制帧() {
	factory, fake := s.scriptFactory(
		`{"type":"system","subtype":"init","session_id":"sess-1"}`)
	h := s.newHarness(factory)
	s.Require().NoError(h.Start(context.Background(), harnessprotocol.HarnessContext{
		AgentName: "e2e", AgentID: "e2e", HostSessionID: "host-1",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
	}))
	// 直接驱动 InterruptTurn（base.Abort 仅对活跃轮次调用 provider 钩子；
	// fake 脚本耗尽后轮次已终态，这里单测钩子本身的帧形状）
	s.Require().NoError(h.InterruptTurn(context.Background(), nil, harnessprotocol.AbortModeGraceful))
	found := false
	for _, frame := range fake.sent {
		if frame["type"] == "control_request" {
			request, _ := frame["request"].(map[string]any)
			if request["subtype"] == "interrupt" {
				found = true
			}
		}
	}
	s.True(found, "未外发 interrupt 控制帧")
	// FORCE 走关闭路径
	s.Require().NoError(h.InterruptTurn(context.Background(), nil, harnessprotocol.AbortModeForce))
	s.True(fake.closed, "FORCE 未关闭传输")
	s.Require().NoError(h.Stop(context.Background()))
}

// TestExportCheckpoint_resume载体 成功轮后 ExportCheckpoint 携带 session_id。
func (s *HarnessSuite) TestExportCheckpoint_resume载体() {
	factory, _ := s.scriptFactory(
		`{"type":"system","subtype":"init","session_id":"sess-1"}`,
		`{"type":"assistant","session_id":"sess-1","message":{"content":[{"type":"text","text":"PONG"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"PONG","duration_ms":100}`)
	h := s.newHarness(factory)
	s.Require().NoError(h.Start(context.Background(), harnessprotocol.HarnessContext{
		AgentName: "e2e", AgentID: "e2e", HostSessionID: "host-1",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
	}))
	receipt, err := h.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "hi"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	s.Require().NotEmpty(s.collectTurnEvents(h, receipt.TurnID), "轮次未完成")
	checkpoint, err := h.ExportCheckpoint(context.Background())
	s.Require().NoError(err)
	s.Require().NotNil(checkpoint)
	s.Equal("codebuddy", checkpoint.Provider)
	s.Equal("sess-1", checkpoint.Data["session_id"])
	s.Require().NoError(h.Stop(context.Background()))
}

// TestResume_args含resume 上下文带 checkpoint 时启动参数含 --resume。
func (s *HarnessSuite) TestResume_args含resume() {
	var captured TransportOptions
	factory := func(options TransportOptions) (Transport, error) {
		captured = options
		fake := &fakeTransport{frames: []WireFrame{}}
		frame, err := parseFrame(json.RawMessage(`{"type":"system","subtype":"init","session_id":"sess-2"}`))
		s.Require().NoError(err)
		fake.frames = append(fake.frames, frame)
		return fake, nil
	}
	h := s.newHarness(factory)
	checkpoint, err := harnessprotocol.NewHarnessCheckpoint(harnessprotocol.CheckpointOptions{
		Provider: "codebuddy", SchemaVersion: "1",
		AgentID: "e2e", HostSessionID: "host-1", CheckpointID: "cp-1", Sequence: 1,
		Data: harnessprotocol.JsonObject{"session_id": "sess-1"},
	})
	s.Require().NoError(err)
	s.Require().NoError(h.Start(context.Background(), harnessprotocol.HarnessContext{
		AgentName: "e2e", AgentID: "e2e", HostSessionID: "host-1",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
		Checkpoint:      &checkpoint,
	}))
	s.Contains(strings.Join(captured.Args, " "), "--resume sess-1")
	s.Require().NoError(h.Stop(context.Background()))
}

// TestMcpServers_上下文翻译成mcpConfig 宿主 MCP server（HarnessContext.
// McpServers）翻译成 --mcp-config 内联 JSON；Python 契约：宿主放 context、
// provider 负责翻译（对齐 member_runtime.py:492-506 的宿主侧契约）。
func (s *HarnessSuite) TestMcpServers_上下文翻译成mcpConfig() {
	var captured TransportOptions
	factory := func(options TransportOptions) (Transport, error) {
		captured = options
		fake := &fakeTransport{frames: []WireFrame{}}
		frame, err := parseFrame(json.RawMessage(`{"type":"system","subtype":"init","session_id":"sess-mcp"}`))
		s.Require().NoError(err)
		fake.frames = append(fake.frames, frame)
		return fake, nil
	}
	h := s.newHarness(factory)
	url := "http://127.0.0.1:9/mcp"
	s.Require().NoError(h.Start(context.Background(), harnessprotocol.HarnessContext{
		AgentName: "e2e", AgentID: "e2e", HostSessionID: "host-1",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
		McpServers: []harnessprotocol.McpServerConfig{
			{Name: "openjiuwen-team", Transport: harnessprotocol.McpTransportStdio,
				Command: []string{"uapclawmcpserver"}},
			{Name: "remote", Transport: harnessprotocol.McpTransportHTTP, URL: &url},
		},
	}))
	args := strings.Join(captured.Args, "\x00")
	s.Contains(args, "--mcp-config")
	s.Contains(args, `"openjiuwen-team"`)
	s.Contains(args, `"uapclawmcpserver"`)
	s.Contains(args, `"remote"`)
	s.Contains(args, "http://127.0.0.1:9/mcp")
	s.Require().NoError(h.Stop(context.Background()))
}

// TestMcpServers_空上下文不加flag 无宿主 server 时不追加 --mcp-config。
func (s *HarnessSuite) TestMcpServers_空上下文不加flag() {
	var captured TransportOptions
	factory := func(options TransportOptions) (Transport, error) {
		captured = options
		return &fakeTransport{frames: []WireFrame{}}, nil
	}
	h := s.newHarness(factory)
	s.Require().NoError(h.Start(context.Background(), harnessprotocol.HarnessContext{
		AgentName: "e2e", AgentID: "e2e", HostSessionID: "host-1",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
	}))
	// 只允许 config 自带的 mcp_config（本用例未配置）——断言 --mcp-config 不出现
	s.NotContains(strings.Join(captured.Args, "\x00"), "--mcp-config")
	s.Require().NoError(h.Stop(context.Background()))
}

// Test超时_inactivity fake 挂起 → 轮按单轮超时失败（终态 FAILED）。
func (s *HarnessSuite) Test超时_inactivity() {
	harness, err := NewCodebuddyHarnessWithFactory(CodebuddyConfig{TurnTimeoutSeconds: 1},
		func(options TransportOptions) (Transport, error) {
			return &hangTransport{}, nil
		})
	s.Require().NoError(err)
	s.Require().NoError(harness.Start(context.Background(), harnessprotocol.HarnessContext{
		AgentName: "e2e", AgentID: "e2e", HostSessionID: "host-1",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
	}))
	receipt, err := harness.Send(context.Background(),
		harnessprotocol.HarnessInput{Content: "hi"}, harnessprotocol.DeliveryModeAuto)
	s.Require().NoError(err)
	events := s.collectTurnEvents(harness, receipt.TurnID)
	var failed *harnessprotocol.TurnLifecycleEvent
	for _, event := range events {
		if payload, ok := event.Event.(harnessprotocol.TurnLifecycleEvent); ok && payload.Kind == harnessprotocol.TurnEventKindFailed {
			failed = &payload
		}
	}
	s.Require().NotNil(failed, "超时轮未归 FAILED")
	s.Require().NoError(harness.Stop(context.Background()))
}

// hangTransport 挂起传输：先回 init 帧，此后 ReadFrame 恒阻塞到 ctx。
type hangTransport struct {
	calls int
}

// SendFrame 无操作。
func (t *hangTransport) SendFrame(frame map[string]any) error { return nil }

// ReadFrame 首次回 init 帧，此后恒阻塞到 ctx 取消。
func (t *hangTransport) ReadFrame(ctx context.Context) (WireFrame, error) {
	t.calls++
	if t.calls == 1 {
		return parseFrame(json.RawMessage(`{"type":"system","subtype":"init","session_id":"sess-hang"}`))
	}
	<-ctx.Done()
	return WireFrame{}, ctx.Err()
}

// StderrTail 恒空。
func (t *hangTransport) StderrTail() string { return "" }

// Close 无操作。
func (t *hangTransport) Close() error { return nil }

// TestListModels_未声明 未声明 MODEL_DISCOVERY 走哨兵。
func (s *HarnessSuite) TestListModels_未声明() {
	h := s.newHarness(func(options TransportOptions) (Transport, error) {
		return &fakeTransport{}, nil
	})
	_, err := h.ListModels(context.Background())
	s.Require().Error(err)
}

// TestStop_terminated Stop 后 terminated。
func (s *HarnessSuite) TestStop_terminated() {
	h := s.newHarness(func(options TransportOptions) (Transport, error) {
		frame, err := parseFrame(json.RawMessage(`{"type":"system","subtype":"init","session_id":"sess-1"}`))
		s.Require().NoError(err)
		return &fakeTransport{frames: []WireFrame{frame}}, nil
	})
	s.Require().NoError(h.Start(context.Background(), harnessprotocol.HarnessContext{
		AgentName: "e2e", AgentID: "e2e", HostSessionID: "host-1",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
	}))
	s.Require().NoError(h.Stop(context.Background()))
	s.Equal(harnessprotocol.HarnessStateTerminated, h.State())
}

// 未使用导入守卫（bufio/io 供未来真进程用例复用）。
var (
	_ = bufio.NewReader
	_ = io.Discard
	_ = providers.PendingTurn{}
)
