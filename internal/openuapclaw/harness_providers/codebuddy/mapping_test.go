package codebuddy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/suite"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
)

// MappingSuite 帧映射与累积器套件。
type MappingSuite struct{ suite.Suite }

// TestMappingSuite 套件入口。
func TestMappingSuite(t *testing.T) { suite.Run(t, new(MappingSuite)) }

// frameOf 辅助：解码一行 JSON 为线帧。
func (s *MappingSuite) frameOf(line string) WireFrame {
	frame, err := parseFrame(json.RawMessage(line))
	s.Require().NoError(err)
	return frame
}

// TestConsume_assistantText块 text 块 → ANSWER 通道 OutputEvent（delta）。
func (s *MappingSuite) TestConsume_assistantText块() {
	acc := NewCodebuddyTurnAccumulator("t1", "s1")
	mapped, err := acc.Consume(s.frameOf(
		`{"type":"assistant","session_id":"s1","message":{"id":"m1","content":[{"type":"text","text":"PONG"}]}}`))
	s.Require().NoError(err)
	s.Require().Len(mapped, 1)
	output, ok := mapped[0].Payload.(harnessprotocol.OutputEvent)
	s.Require().True(ok)
	s.Equal(harnessprotocol.OutputChannelAnswer, output.Channel)
	s.Equal("PONG", output.Content)
	s.Equal("PONG", acc.LastTextOutput())
}

// TestConsume_assistantToolUse块 tool_use 块 → tool item STARTED + COMPLETED。
func (s *MappingSuite) TestConsume_assistantToolUse块() {
	acc := NewCodebuddyTurnAccumulator("t1", "s1")
	mapped, err := acc.Consume(s.frameOf(
		`{"type":"assistant","session_id":"s1","message":{"id":"m1","content":[{"type":"tool_use","id":"call1","name":"read_file","input":{"path":"a"}}]}}`))
	s.Require().NoError(err)
	s.Require().Len(mapped, 2)
	started, ok := mapped[0].Payload.(harnessprotocol.ItemLifecycleEvent)
	s.Require().True(ok)
	s.Equal(harnessprotocol.ItemEventKindStarted, started.Kind)
	s.Equal("tool", started.ItemType)
	completed, ok := mapped[1].Payload.(harnessprotocol.ItemLifecycleEvent)
	s.Require().True(ok)
	s.Equal(harnessprotocol.ItemEventKindCompleted, completed.Kind)
	s.Equal("call1", mapped[0].ItemID)
	s.Equal("call1", mapped[1].ItemID)
}

// TestConsume_usageUpdate累积cost turnUsage 快照累计金额与 token。
func (s *MappingSuite) TestConsume_usageUpdate累积cost() {
	acc := NewCodebuddyTurnAccumulator("t1", "s1")
	_, err := acc.Consume(s.frameOf(
		`{"type":"system","subtype":"usage_update","session_id":"s1","_meta":{"codebuddy.ai/turnUsage":{"total":{"usage":{"input_tokens":10,"output_tokens":3},"credits":0}}}}`))
	s.Require().NoError(err)
	_, err = acc.Consume(s.frameOf(
		`{"type":"system","subtype":"usage_update","session_id":"s1","_meta":{"codebuddy.ai/turnUsage":{"total":{"usage":{"input_tokens":15,"output_tokens":7},"credits":0}}}}`))
	s.Require().NoError(err)
	usage := acc.Usage()
	s.Require().NotNil(usage)
	s.Equal(15, *usage.InputTokens)
	s.Equal(7, *usage.OutputTokens)
}

// TestConsume_未知帧忽略 未知帧不产事件不报错。
func (s *MappingSuite) TestConsume_未知帧忽略() {
	acc := NewCodebuddyTurnAccumulator("t1", "s1")
	mapped, err := acc.Consume(s.frameOf(`{"type":"system","subtype":"status","status":null}`))
	s.Require().NoError(err)
	s.Empty(mapped)
}

// TestBuildTerminalResult_success result success → FINISHED + FinalOutput。
func (s *MappingSuite) TestBuildTerminalResult_success() {
	acc := NewCodebuddyTurnAccumulator("t1", "s1")
	_, err := acc.Consume(s.frameOf(
		`{"type":"assistant","session_id":"s1","message":{"content":[{"type":"text","text":"PONG"}]}}`))
	s.Require().NoError(err)
	kind, result, err := acc.BuildTerminalResult(s.frameOf(
		`{"type":"result","subtype":"success","is_error":false,"result":"PONG","duration_ms":4069,"num_turns":2,"session_id":"s1"}`),
		100.0)
	s.Require().NoError(err)
	s.Equal(harnessprotocol.TurnEventKindFinished, kind)
	s.Equal("PONG", result.FinalOutput)
	s.Require().NotNil(result.DurationMS)
	s.Equal(4069, *result.DurationMS)
}

// TestBuildTerminalResult_error result is_error → FAILED + 结构化错误（协议不变量：
// failed 必须带 error）。
func (s *MappingSuite) TestBuildTerminalResult_error() {
	acc := NewCodebuddyTurnAccumulator("t1", "s1")
	kind, result, err := acc.BuildTerminalResult(s.frameOf(
		`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"boom","session_id":"s1"}`),
		100.0)
	s.Require().NoError(err)
	s.Equal(harnessprotocol.TurnEventKindFailed, kind)
	s.Require().NotNil(result.Error)
	s.Contains(result.Error.Message, "boom")
}

// TestBuildTerminalResult_错误maxTurns error_max_turns 也归 FAILED。
func (s *MappingSuite) TestBuildTerminalResult_错误maxTurns() {
	acc := NewCodebuddyTurnAccumulator("t1", "s1")
	kind, _, err := acc.BuildTerminalResult(s.frameOf(
		`{"type":"result","subtype":"error_max_turns","is_error":true,"result":"stopped","session_id":"s1"}`),
		100.0)
	s.Require().NoError(err)
	s.Equal(harnessprotocol.TurnEventKindFailed, kind)
}

// TestAccumulator_两轮不串 两个累积器事件互不串扰。
func (s *MappingSuite) TestAccumulator_两轮不串() {
	first := NewCodebuddyTurnAccumulator("t1", "s1")
	second := NewCodebuddyTurnAccumulator("t2", "s1")
	_, err := first.Consume(s.frameOf(
		`{"type":"assistant","session_id":"s1","message":{"content":[{"type":"text","text":"ALPHA"}]}}`))
	s.Require().NoError(err)
	_, err = second.Consume(s.frameOf(
		`{"type":"assistant","session_id":"s1","message":{"content":[{"type":"text","text":"BRAVO"}]}}`))
	s.Require().NoError(err)
	s.Equal("ALPHA", first.LastTextOutput())
	s.Equal("BRAVO", second.LastTextOutput())
}
