//go:build integration

// steer 全链集成：server 侧 adapter 投递链 → steering 队列 → ReAct 循环
// drain → SessionInputGuard 状态机 → MockLLM（跨 coordinator/adapter/harness/
// mockllm ≥4 子系统）。对应 spec 2026-11-26-domain25.6-steer-wiring-design.md
// §8.2 讨论点验证（steer 插话/idle 分流/交付义务节/准入窗口）。
package sessioninput

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	hconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/harness_config"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SteerFullChainSuite steer 全链套件（AgentSuite 能力：MockLLM + DeepAgent 工厂）。
type SteerFullChainSuite struct {
	isuite.AgentSuite
	// coordinator 每用例独立协调器
	coordinator *sessionpkg.RuntimeSessionCoordinator
	// guard 会话输入守卫（与 agent 同生命周期）
	guard *adapter.SessionInputGuard
	// deep 宿主适配器
	deep *adapter.DeepAdapter
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSteerFullChainSuite 运行本套件。
func TestSteerFullChainSuite(t *testing.T) {
	suite.Run(t, new(SteerFullChainSuite))
}

// SetupTest 每用例：独立协调器（agent 由用例自建）。
func (s *SteerFullChainSuite) SetupTest() {
	s.coordinator = sessionpkg.NewRuntimeSessionCoordinator(
		sessionpkg.DefaultCoordinatorOptions())
}

// stubTool 最小可执行工具（拉开 ReAct 窗口：模型调用1 → 工具 → 模型调用2）。
// hold 非 nil 时 Invoke 阻塞直到通道关闭——为 steer 投递制造确定性窗口
// （工具执行期 = 准入窗口开）。
type stubTool struct {
	card  *tool.ToolCard
	hold  chan struct{}
	locks sync.WaitGroup
}

func (t *stubTool) Card() *tool.ToolCard { return t.card }

func (t *stubTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	if t.hold != nil {
		t.locks.Add(1)
		defer t.locks.Done()
		<-t.hold
	}
	return map[string]any{"content": "ok"}, nil
}

func (t *stubTool) Stream(context.Context, map[string]any, ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *stubTool) GetCallTimeout(map[string]any, float64) float64 { return 30 }

// newStubTool 构造桩工具（手工建卡，不走 registry）。
func newStubTool() tool.Tool {
	card := &tool.ToolCard{BaseCard: *cschema.NewBaseCard()}
	card.Name = "wait_tool"
	return &stubTool{card: card}
}

// fullchainSteerRequest 构造 steer 请求。
func fullchainSteerRequest(query string) *schema.AgentRequest {
	params := map[string]any{"input_mode": "steer", "query": query}
	raw, _ := json.Marshal(params)
	sid := "session-A"
	return &schema.AgentRequest{RequestID: "req-steer-1", ChannelID: "web", SessionID: &sid, Params: raw}
}

// payloadText 从流元素提取文本（OutputSchema 宽松提取）。
func payloadText(item any) string {
	if m, ok := item.(interface{ ToMap() map[string]any }); ok {
		data := m.ToMap()
		if payload, ok := data["payload"].(map[string]any); ok {
			if text, ok := payload["content"].(string); ok {
				return text
			}
		}
		if text, ok := data["content"].(string); ok {
			return text
		}
	}
	return ""
}

// ──────────────────────────── 用例 ────────────────────────────

// TestSteer_全链_投递窗口 server 传入 steer（活跃轮+准入窗口开）→ adapter
// 投递链四步全走通：①execution 复核 ②bound_round 复核 ③同步入队（真实
// EnqueueSteerEntry）④边界发布（boundary_ready 关闭）+ guard 交付义务登记。
// [挂账] loop-drain→MockLLM 段需 session-runtime 交互执行脚手架
// （executeRound 设活跃轮；普通流不设——adapter ② 复核对普通流正确拒收），
// 随 session runtime e2e 批回填。
func (s *SteerFullChainSuite) TestSteer_全链_投递窗口() {
	ctx := s.Ctx
	hold := make(chan struct{})
	stub := &stubTool{
		card: func() *tool.ToolCard {
			c := &tool.ToolCard{BaseCard: *cschema.NewBaseCard()}
			c.Name = "wait_tool"
			return c
		}(),
		hold: hold,
	}

	// guard 经构造期 Rails 注入（RegisterRail 早于 reactAgent 创建时
	// bridge 事件会被 registerRailSelective 静默丢弃——必须走 Rails 参数）
	s.guard = adapter.NewSessionInputGuard(nil, nil)
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Card:          agentschema.NewAgentCard(),
		ToolInstances: []tool.Tool{stub},
		MaxIterations: 0, // 未配置=不限 → 准入窗口恒开（Python limit None 语义）
		Rails:         []agentinterfaces.AgentRail{s.guard},
	})
	s.Require().NoError(err)

	s.deep = adapter.NewDeepAdapter()
	s.deep.SetSessionScopedForTest("session-A")
	s.deep.SetInstanceForTest(agent)
	s.deep.SetSessionInputGuard(s.guard)
	s.guard.BindOwner(agent, s.deep)

	work := hschema.NewUserWorkItem("req-anchor", map[string]any{"query": "查 Q3 预算"}, false, true)
	agent.SetActiveRoundForTest(work)
	agent.SetOutputStreamForTest(true)
	agent.SetSteeringQueueForTest(make(chan string, 8))

	// MockLLM：①工具调用（进入阻塞工具，窗口保持）②最终文本
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("wait_tool", `{}`),
		mockllm.CreateTextResponse("已结合你的补充完成"),
	)

	// 后台跑流（真实 ReAct：模型调用1 → stubTool(阻塞)）
	streamCh, streamErr := agent.Stream(ctx, map[string]any{"query": "查 Q3 预算"})
	s.Require().NoError(streamErr)
	consumed := make(chan []string, 1)
	go func() {
		var payloads []string
		for item := range streamCh {
			payloads = append(payloads, payloadText(item))
		}
		consumed <- payloads
	}()

	// 等准入窗口打开（before_model_call/before_tool_call rail 真实触发）
	s.Require().Eventually(func() bool {
		return s.guard.Accepting()
	}, 5*time.Second, 10*time.Millisecond, "准入窗口应打开")

	// server 传入 steer：经 adapter 投递链全链（DeliverSessionInput）
	ch, derr := s.deep.DeliverSessionInput(ctx, fullchainSteerRequest("改用 V2 数据"),
		map[string]any{"query": "改用 V2 数据"})
	s.Require().NoError(derr)
	s.Require().NotNil(ch, "活跃轮存在时 steer 应被接受")

	// 断言 ①：真实入队（EnqueueSteerEntry → steering 队列持有 display）
	queue := agent.SteeringQueueChan()
	s.Require().Equal("改用 V2 数据", <-queue)

	// 断言 ②：账本条目登记 + 边界发布关闭（④ publish_input_received 真实走通）
	entry := adapter.SessionInputLedgerForTest().Lookup("改用 V2 数据")
	s.Require().NotNil(entry, "账本应登记条目")
	select {
	case <-entry.BoundaryReady:
	default:
		s.Fail("边界信号应已关闭")
	}

	// 放行工具让流自然收尾
	close(hold)
	texts := <-consumed
	s.Require().NotEmpty(texts)
}

// TestSteer_全链_idle分流MockLLM新轮 无活跃轮 → idle 兜底 → 新轮消费 MockLLM
// （coordinator 三守卫分流 + MockLLM 真实消费）。
func (s *SteerFullChainSuite) TestSteer_全链_idle分流MockLLM新轮() {
	ctx := s.Ctx
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Card:          agentschema.NewAgentCard(),
		MaxIterations: 2,
	})
	s.Require().NoError(err)

	_, regErr := s.coordinator.RegisterSession("session-A", "web",
		sessionpkg.SessionPersistencePolicyPersistent)
	s.Require().NoError(regErr)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("idle 新轮回复"))
	idleCalled := false
	err = s.coordinator.StreamSessionInput(ctx, sessionpkg.StreamSessionInputOptions{
		SessionID: "session-A",
		RequestID: "req-idle-mock",
		Operation: func(context.Context, string) (<-chan *schema.AgentResponseChunk, error) {
			return nil, errors.New("无活跃轮不应走 operation")
		},
		IdleOperation: func(opCtx context.Context) (<-chan *schema.AgentResponseChunk, error) {
			idleCalled = true
			// idle 闭包 = 新轮（Python service.py idle_input → 普通流）
			ch, sErr := agent.Stream(opCtx, map[string]any{"query": "新问题"})
			if sErr != nil {
				return nil, sErr
			}
			out := make(chan *schema.AgentResponseChunk, 8)
			go func() {
				defer close(out)
				for item := range ch {
					out <- &schema.AgentResponseChunk{
						RequestID: "req-idle-mock", ChannelID: "web",
						Payload: map[string]any{"content": payloadText(item)},
					}
				}
			}()
			return out, nil
		},
	}, func(*schema.AgentResponseChunk) bool { return true })
	s.Require().NoError(err)
	s.Require().True(idleCalled, "无活跃轮应走 idle")
	s.Require().Equal(1, s.MockLLM.InvokeCallCount(), "新轮应消费 MockLLM")
}
