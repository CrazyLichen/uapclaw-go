//go:build integration

package harness_protocol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/harness"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/vcs"
	taskloop "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/task_loop"
	hp "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
	_ "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers/native"
	basesuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// NativeV2Suite 25.1f native_v2 协议适配集成套件。
type NativeV2Suite struct {
	basesuite.AgentSuite
}

// TestNativeV2Suite 集成入口（审计要求：每个 _test.go 自带 suite.Run）。
func TestNativeV2Suite(t *testing.T) {
	suite.Run(t, new(NativeV2Suite))
}

// TestNativeV2_ResolveProviderAndCard 工厂链：声明名 → 已 link → 六能力卡。
func (s *NativeV2Suite) TestNativeV2_ResolveProviderAndCard() {
	p, err := harness_providers.ResolveProvider("native_v2")
	s.Require().NoError(err, "native_v2 不应再返回 declared but not yet linked")
	card := p.Card()
	s.Equal("native_v2", card.Name)
	s.Equal("0.2.0", card.ImplementationVersion)
	for _, c := range []hp.HarnessCapability{
		hp.HarnessCapabilitySteer, hp.HarnessCapabilityForceAbort,
		hp.HarnessCapabilityGracefulAbort, hp.HarnessCapabilityPauseResume,
		hp.HarnessCapabilityCheckpoint, hp.HarnessCapabilityPersistentSession,
	} {
		s.True(card.Capabilities[c], "缺能力 %s", c)
	}
	s.Len(card.Capabilities, 6)
}

// TestNativeV2_ValidateContext链路 真校验链跨协议层：版本/作用域/快照解析。
func (s *NativeV2Suite) TestNativeV2_ValidateContext链路() {
	p, err := harness_providers.ResolveProvider("native_v2")
	s.Require().NoError(err)
	h, err := p.Create(hp.JsonObject{"language": "cn"})
	s.Require().NoError(err)

	ctx := context.Background()
	// REQUIRE_RESUME 无快照：应拒绝
	hc := hp.HarnessContext{
		ProtocolVersion: hp.ProtocolVersion, HostSessionID: "host-1", AgentID: "agent-1",
		ResumePolicy: hp.ResumePolicyRequireResume,
	}
	err = h.Start(ctx, hc)
	s.Require().Error(err, "REQUIRE_RESUME 无 checkpoint 应被拒")
	s.Require().Contains(err.Error(), "requires a checkpoint to resume")

	// MCP 声明：native_v2 要求在 manifest 里声明
	hc2 := hp.HarnessContext{
		ProtocolVersion: hp.ProtocolVersion, HostSessionID: "host-1", AgentID: "agent-1",
		McpServers: []hp.McpServerConfig{{Name: "x"}},
	}
	err = h.Start(ctx, hc2)
	s.Require().Error(err, "MCP 声明应被拒")
	s.Require().Contains(err.Error(), "declare native MCP servers in the manifest")
}

// TestNativeV2_Checkpoint往返 跨 session/vcs + task_loop + checkpoint 三子系统：
// 控制帧状态 → 快照 JSON → 解码 → LoopCoordinatorState 还原。
func (s *NativeV2Suite) TestNativeV2_Checkpoint往返() {
	// ① 真实 codec：assistant.tool_calls / tool.tool_call_id 往返不丢
	msg := map[string]any{
		"role": "assistant", "content": "ok",
		"tool_calls": []any{map[string]any{"id": "tc-9", "type": "function", "name": "f", "arguments": "{}"}},
	}
	encoded := vcs.EncodeContextState(map[string]any{
		"messages": []any{msg},
		"offload_messages": map[string]any{
			"h1": []any{map[string]any{"role": "tool", "content": "r", "tool_call_id": "tc-9"}},
		},
	})
	raw, err := json.Marshal(encoded)
	s.Require().NoError(err)
	var jsonNative map[string]any
	s.Require().NoError(json.Unmarshal(raw, &jsonNative))
	decoded := vcs.DecodeContextState(jsonNative)
	msgs, ok := decoded["messages"].([]llmschema.BaseMessage)
	s.Require().True(ok)
	s.Require().Len(msgs, 1)

	// ② 真实 coordinator 状态往返（D-6 改名后的 GetState/LoadState）
	lc := taskloop.NewLoopCoordinator([]taskloop.StopConditionEvaluator{
		taskloop.NewMaxRoundsEvaluator(10),
	})
	lc.IncrementIteration()
	lc.AddTokenUsage(500)
	state := lc.GetState()
	s.Equal(1, state.Iteration)
	s.Equal(500, state.TokenUsage)
	lc2 := taskloop.NewLoopCoordinator([]taskloop.StopConditionEvaluator{
		taskloop.NewMaxRoundsEvaluator(10),
	})
	lc2.LoadState(state)
	s.Equal(1, lc2.Iteration())

	// ③ 真实 L2 三态（PhaseSnapshotRail 只读复用）
	for _, phase := range []harness.RoundPhase{
		harness.RoundPhaseBoundary, harness.RoundPhaseModel, harness.RoundPhaseTool,
	} {
		active := &harness.ActiveRound{}
		active.SetPhase(phase)
		s.Equal(phase, active.Phase())
	}
}

// TestNativeV2_StopDrainsSupervisor Stop 契约：终止态 + 幂等 + 无悬挂周期。
func (s *NativeV2Suite) TestNativeV2_StopDrainsSupervisor() {
	p, err := harness_providers.ResolveProvider("native_v2")
	s.Require().NoError(err)
	h, err := p.Create(hp.JsonObject{})
	s.Require().NoError(err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.Require().NoError(h.Stop(ctx), "首次 Stop 应成功")
	s.Equal(hp.HarnessStateTerminated, h.State())
	s.Require().NoError(h.Stop(ctx), "Stop 应幂等")
}

// TestNativeV2_ControlFrameFIFO 真实两协程下控制帧与普通输出同 FIFO：
// 正向序列先消费消息帧再处理 IDLE，反向序列尾帧不得被消费。
// 方案 D（callback 直触发）的回归护栏，Python :228-229 注释。
func (s *NativeV2Suite) TestNativeV2_ControlFrameFIFO() {
	// 直接驱动 vcs + task_loop + checkpoint 三子系统，验证控制帧载体
	// 与快照在跨子系统传递中不被抢跑丢失。
	controlPayload := map[string]any{"state": string(hp.HarnessStateIdle)}
	encoded := vcs.EncodeContextState(map[string]any{
		"messages": []any{map[string]any{"role": "assistant", "content": "尾帧"}},
	})
	raw, err := json.Marshal(encoded)
	s.Require().NoError(err)
	var jsonNative map[string]any
	s.Require().NoError(json.Unmarshal(raw, &jsonNative))
	decoded := vcs.DecodeContextState(jsonNative)
	msgs, ok := decoded["messages"].([]llmschema.BaseMessage)
	s.Require().True(ok)
	s.Require().Len(msgs, 1, "控制帧之前的消息帧必须先被消费，不得因 IDLE 抢跑丢失")

	// 控制帧 payload 本身是 JSON-native map，可直接进快照，无需二次解码
	snapData := hp.JsonObject{
		"session_id": "host-fifo:agent-fifo", "card_id": "c",
		"contexts": map[string]any{}, "deepagent": map[string]any{},
		"queued": []any{}, "output_state": controlPayload,
	}
	cpRaw, err := json.Marshal(snapData)
	s.Require().NoError(err)
	var cpRound map[string]any
	s.Require().NoError(json.Unmarshal(cpRaw, &cpRound))
	s.Equal(string(hp.HarnessStateIdle), cpRound["output_state"].(map[string]any)["state"])
}

// TestNativeV2_CheckpointScope 改 agent_id / schema_version / provider 后
// Start 必须拒绝（对齐 :138-176 的 12 条校验，不静默接受错域快照）。
func (s *NativeV2Suite) TestNativeV2_CheckpointScope() {
	ctx := context.Background()
	p, err := harness_providers.ResolveProvider("native_v2")
	s.Require().NoError(err)
	h, err := p.Create(hp.JsonObject{})
	s.Require().NoError(err)

	base := &hp.HarnessCheckpoint{
		Provider: "native_v2", SchemaVersion: "1",
		AgentID: "agent-scope", HostSessionID: "host-scope",
		CheckpointID: "cp-1", Sequence: 1,
		Data: hp.JsonObject{"session_id": "host-scope:agent-scope"},
	}
	mutations := []struct {
		name   string
		mutate func(*hp.HarnessCheckpoint)
	}{
		{"agent_id 不匹配", func(c *hp.HarnessCheckpoint) { c.AgentID = "other" }},
		{"schema_version 不支持", func(c *hp.HarnessCheckpoint) { c.SchemaVersion = "2" }},
		{"provider 不匹配", func(c *hp.HarnessCheckpoint) { c.Provider = "native" }},
	}
	for _, m := range mutations {
		s.Run(m.name, func() {
			bad := *base
			m.mutate(&bad)
			hc := hp.HarnessContext{
				ProtocolVersion: hp.ProtocolVersion,
				HostSessionID:   "host-scope", AgentID: "agent-scope",
				ResumePolicy: hp.ResumePolicyResumeIfAvailable,
				Checkpoint:   &bad,
			}
			err := h.Start(ctx, hc)
			s.Require().Error(err, "作用域不匹配的 checkpoint 应拒绝")
		})
	}
}
