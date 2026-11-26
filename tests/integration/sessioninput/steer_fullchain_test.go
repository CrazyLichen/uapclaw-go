//go:build integration

// steer 全链集成：server 侧 adapter 投递链 → steering 队列 → ReAct 循环
// drain → SessionInputGuard 状态机 → MockLLM（跨 coordinator/adapter/harness/
// mockllm ≥4 子系统）。对应 spec 2026-11-26-domain25.6-steer-wiring-design.md
// §8.2 讨论点验证（steer 插话/idle 分流/交付义务节/准入窗口）。
package sessioninput

import (
	"context"
	"strings"
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

// TestSteer_全链_插话进MockLLM轮次 真交互执行路径 e2e：
// StartInteraction（监督器+executeRound 设活跃轮）→ SendInput → 模型调用1
// （工具阻塞）→ server 传入 steer → adapter 四步（真实 bound_round 复核）
// → ReAct drain 吸收 → before_model_call 消费交付义务 → MockLLM 第二次调用
// → NextOutput 收最终文本。[此前挂账段随本用例闭合]
func (s *SteerFullChainSuite) TestSteer_全链_插话进MockLLM轮次() {
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
		Card:           agentschema.NewAgentCard(),
		ToolInstances:  []tool.Tool{stub},
		MaxIterations:  0, // 未配置=不限 → 准入窗口恒开（Python limit None 语义）
		EnableTaskLoop: true,
		Rails:          []agentinterfaces.AgentRail{s.guard},
	})
	s.Require().NoError(err)

	sess := s.NewTestSession("session-A")
	s.Require().NoError(sess.PreRun(ctx, map[string]any{}))

	s.deep = adapter.NewDeepAdapter()
	s.deep.SetSessionScopedForTest("session-A")
	s.deep.SetInstanceForTest(agent)
	s.deep.SetSessionInputGuard(s.guard)
	s.guard.BindOwner(agent, s.deep)

	// MockLLM：①工具调用（进入阻塞工具，窗口保持）②最终文本
	// 计数用增量（套件内 MockLLM 计数器跨用例累计）
	baseline := s.MockLLM.StreamCallCount()
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("wait_tool", `{}`),
		mockllm.CreateTextResponse("已结合你的补充完成"),
	)

	// 真交互执行：监督器启动 + 输出租约 + 用户输入（executeRound 设活跃轮）
	s.Require().NoError(agent.Start(ctx, sess))
	lease, attachErr := agent.AttachOutput(ctx)
	s.Require().NoError(attachErr)
	s.Require().NotNil(lease)
	go func() {
		_ = agent.SendInput(ctx, hschema.SendInputRequest{
			RequestID: "req-anchor",
			Inputs:    map[string]any{"query": "查 Q3 预算"},
		})
	}()

	// 等准入窗口打开（真实 executeRound → before_model_call/before_tool_call）
	s.Require().Eventually(func() bool {
		return s.guard.Accepting()
	}, 10*time.Second, 10*time.Millisecond, "准入窗口应打开")

	// server 传入 steer：经 adapter 投递链全链（真实 bound_round 复核）
	ch, derr := s.deep.DeliverSessionInput(ctx, fullchainSteerRequest("改用 V2 数据"),
		map[string]any{"query": "改用 V2 数据"})
	s.Require().NoError(derr)
	s.Require().NotNil(ch, "活跃轮存在时 steer 应被接受")
	close(hold) // 放行工具 → loop 继续 → drain 吸收 steer

	// 等第二次模型调用（drain 吸收 steer → before_model_call 消费交付义务）
	s.Require().Eventually(func() bool {
		return s.MockLLM.StreamCallCount() >= baseline+2
	}, 10*time.Second, 10*time.Millisecond, "第二次模型调用应吸收 steer")

	// 断言 ①：steering 队列已被 drain 吸收
	select {
	case leftover := <-agent.SteeringQueueChan():
		s.Failf("steering 队列应已消费，残留 %q", leftover)
	default:
	}

	// 断言 ②：交付义务节进系统提示词（builder 接口无节枚举，取构建快照）
	sectionText := s.guard.DeliverySectionTextForTest()
	s.Require().NotEmpty(sectionText, "交付义务节应存在（session_input_delivery）")
	s.Require().Contains(sectionText, "改用 V2 数据")
	s.Require().Contains(sectionText, "quoted-delivery-data")

	// 断言 ③：最终文本经输出租约推给消费者
	deadline := time.Now().Add(10 * time.Second)
	finalText := ""
	for time.Now().Before(deadline) {
		item, ok := agent.NextOutput(ctx, lease.Lease())
		if !ok {
			break
		}
		if text := payloadText(item); strings.Contains(text, "已结合你的补充完成") {
			finalText = text
			break
		}
	}
	s.Require().NotEmpty(finalText, "最终文本应经租约推送")
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
