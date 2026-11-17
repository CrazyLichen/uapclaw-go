//go:build integration

package harness_protocol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers/native"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	basesuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// NativeV2MockLLMSuite 用真实 MockLLM 驱动 L2 轮次，验证两层 supervisor
// 经 native_protocol.control 帧同步、全周期 cold resume。
// 审计要求：每个 _test.go 自带 suite.Run（含纯 helper 文件）。
type NativeV2MockLLMSuite struct {
	basesuite.AgentSuite
}

func TestNativeV2MockLLMSuite(t *testing.T) {
	suite.Run(t, new(NativeV2MockLLMSuite))
}

// mockConfig 把 NewSpecWithMockLLM 造出的 spec 经 JSON 往返成 config 的
// deep_agent 字典（对齐 NativeV2HarnessProvider.Create 的 JSON 边界）。
func (s *NativeV2MockLLMSuite) mockConfig() hp.JsonObject {
	spec, err := s.NewSpecWithMockLLM(3)
	s.Require().NoError(err)
	raw, err := json.Marshal(spec)
	s.Require().NoError(err)
	var m map[string]any
	s.Require().NoError(json.Unmarshal(raw, &m))
	return hp.JsonObject{"deep_agent": m, "language": "cn"}
}

// waitForState 轮询公开 State() 直到目标相位或超时。
func waitForState(h hp.HarnessProtocol, want hp.HarnessState, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for h.State() != want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if h.State() != want {
		return &hp.HarnessStateError{Msg: "state did not reach " + string(want) + ", got " + string(h.State())}
	}
	return nil
}

// TestFullCycle_MockLLM 真·全周期：
// Start → Send（MockLLM 产出真实轮次）→ 等两源同步 IDLE →
// ExportCheckpoint 成功 → Stop → 合规快照新实例 cold restore → Stop。
func (s *NativeV2MockLLMSuite) TestFullCycle_MockLLM() {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("第一轮答复"))
	p, err := harness_providers.ResolveProvider("native_v2")
	s.Require().NoError(err)
	cfg := s.mockConfig()
	h, err := p.Create(cfg)
	s.Require().NoError(err)
	hc := hp.HarnessContext{
		ProtocolVersion: hp.ProtocolVersion,
		HostSessionID:   "host-llm", AgentID: "agent-llm",
		ResumePolicy: hp.ResumePolicyNew,
	}
	s.Require().NoError(h.Start(ctx, hc))

	receipt, err := h.Send(ctx, hp.HarnessInput{Content: "请回答"}, hp.DeliveryModeAuto)
	s.Require().NoError(err)
	s.Require().NotEmpty(receipt.TurnID)

	// 先等协议层 supervisor 起轮转 RUNNING（避免 Send 后立即读到初始 IDLE
	// 的竞态），再等 MockLLM 轮次完成、control(IDLE) 帧把两源一起带回 IDLE。
	s.Require().NoError(waitForState(h, hp.HarnessStateRunning, 3*time.Second),
		"Send 后协议层 supervisor 应进入 RUNNING")
	s.Require().NoError(waitForState(h, hp.HarnessStateIdle, 5*time.Second),
		"MockLLM 轮次结束后两层 supervisor 应同步到 IDLE")

	cp, err := h.ExportCheckpoint(ctx)
	s.Require().NoError(err, "IDLE 边界应能导出 checkpoint（两道守卫都过）")
	s.Require().NotNil(cp)
	s.Equal("native_v2", cp.Provider)
	s.Equal("1", cp.SchemaVersion)
	s.Require().NoError(h.Stop(ctx))

	// cold restore：用真实导出的快照启动新实例，走 12 条校验 + 装配链。
	h2, err := p.Create(cfg)
	s.Require().NoError(err)
	hc2 := hc
	hc2.ResumePolicy = hp.ResumePolicyResumeIfAvailable
	hc2.Checkpoint = cp
	s.Require().NoError(h2.Start(ctx, hc2), "cold restore 的 Start 应通过作用域校验并完成装配")
	s.Equal("host-llm:agent-llm", *h2.ProviderSessionID())
	s.Require().NoError(h2.Stop(ctx))
}
