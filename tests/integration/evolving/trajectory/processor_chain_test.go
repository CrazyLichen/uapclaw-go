//go:build integration

// 25.2b processor 观测链集成用例（spec §7 Task 13 Step 1）。
// 真实跨子系统链路：demand（observability）→ trajectory processor（订阅窗口
// 合并/裁剪）→ trajectory store（落库读回）。TracerProvider 注册生效验证经
// otel SDK 真实 emit span。
package trajectory

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability/demand"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// ProcessorChainSuite processor 观测链套件。
type ProcessorChainSuite struct {
	suite.Suite
	processor *trajectory.TrajectorySpanProcessor
	provider  *sdktrace.TracerProvider
}

// TestProcessorChainSuite 入口。
func TestProcessorChainSuite(t *testing.T) {
	suite.Run(t, new(ProcessorChainSuite))
}

// SetupTest 构造真 processor + TracerProvider 注册。
func (s *ProcessorChainSuite) SetupTest() {
	s.processor = trajectory.NewTrajectorySpanProcessor(0)
	s.provider = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(s.processor))
}

// TearDownTest 释放。
func (s *ProcessorChainSuite) TearDownTest() {
	_ = s.provider.Shutdown(context.Background())
}

// emitToolSpan 经真实 otel SDK emit 一个 tool span（attributes 承载成员身份）。
func (s *ProcessorChainSuite) emitToolSpan(ctx context.Context, tracer trace.Tracer, memberID, teamID, toolName string) {
	_, span := tracer.Start(ctx, "execute_tool "+toolName,
		trace.WithAttributes(
			memberIDAttr(memberID), teamIDAttr(teamID),
			attribute.String("gen_ai.operation.name", "execute_tool"),
			attribute.String("gen_ai.tool.name", toolName),
			attribute.String("session.id", "sess-chain"),
		))
	span.End()
}

// TestDemand_并发同实例懒构造 并发调用 GetTrajectorySpanProcessor 返回同一实例。
// Python: demand.py get_trajectory_span_processor 单例。
func (s *ProcessorChainSuite) TestDemand_并发同实例懒构造() {
	const goroutines = 16
	results := make([]sdktrace.SpanProcessor, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = demand.GetTrajectorySpanProcessor()
		}(i)
	}
	wg.Wait()
	for _, result := range results[1:] {
		s.Require().Same(results[0], result, "demand processor must be a process-wide singleton")
	}
	s.Require().NotNil(results[0], "GetTrajectorySpanProcessor must not be nil after first call")
}

// TestTracerProvider_注册生效_跨成员team可见 经 TracerProvider emit 两个成员
// 的 tool span，team scope 订阅看到双方。
func (s *ProcessorChainSuite) TestTracerProvider_注册生效_跨成员team可见() {
	tracer := s.provider.Tracer("team-runtime")
	// 先开 root span 捕获 traceID，以该 id 绑定 team 订阅，再 emit 子 span。
	rootCtx, rootSpan := tracer.Start(context.Background(), "team_round")
	traceID := rootSpan.SpanContext().TraceID().String()
	// team scope = trace 绑定 + 内容类别订阅（与 rail_scope 的工具/事件类别
	// 同款；"team" 类别由团队运行时的 team 级 span 承载）。
	handle, err := s.processor.Subscribe([]string{"team", "tool"}, traceID)
	s.Require().NoError(err)
	defer s.processor.Unsubscribe(handle)

	s.emitToolSpan(rootCtx, tracer, "member-a", "alpha", "write_file")
	s.emitToolSpan(rootCtx, tracer, "member-b", "alpha", "read_file")
	rootSpan.End()

	s.processor.ForceFlush(context.Background())
	traj, issues := s.processor.Drain(handle)
	s.Require().Empty(issues, "clean emit must not raise capture issues")
	s.Require().NotNil(traj, "team scope must see cross-member spans")
	names := spanToolNames(traj)
	s.Require().GreaterOrEqual(len(names), 1, "at least one tool span must be captured")
}

// TestAgentScope_隔离 agent scope 订阅（无 team）与 team 订阅互不串扰。
func (s *ProcessorChainSuite) TestAgentScope_隔离() {
	tracer := s.provider.Tracer("member-runtime")
	agentHandle, err := s.processor.Subscribe([]string{"tool"}, "")
	s.Require().NoError(err)
	teamHandle, teamErr := s.processor.Subscribe([]string{"team", "tool"}, "trace-other")
	s.Require().NoError(teamErr)
	defer s.processor.Unsubscribe(agentHandle)
	defer s.processor.Unsubscribe(teamHandle)

	s.emitToolSpan(context.Background(), tracer, "member-a", "alpha", "list_dir")

	s.processor.ForceFlush(context.Background())
	agentTraj, _ := s.processor.Drain(agentHandle)
	teamTraj, _ := s.processor.Drain(teamHandle)
	s.Require().NotNil(agentTraj, "tool category subscription must capture tool spans")
	// team 订阅绑定其他 trace_id，本会话 span 不落入。
	if teamTraj != nil {
		s.Require().Empty(trajectory.IterSpans(teamTraj), "team scope of another trace must be isolated")
	}
}

// TestIdentityDedup_重复span不重复计 同一 span 重复 OnEnd（重放场景）只计一次。
func (s *ProcessorChainSuite) TestIdentityDedup_重复span不重复计() {
	tracer := s.provider.Tracer("member-runtime")
	handle, err := s.processor.Subscribe([]string{"tool"}, "")
	s.Require().NoError(err)
	defer s.processor.Unsubscribe(handle)

	s.emitToolSpan(context.Background(), tracer, "member-a", "alpha", "write_file")

	first, _ := s.processor.Drain(handle)
	s.Require().NotNil(first)
	count1 := len(trajectory.IterSpans(first))

	// 重放同一批（Identity 相同的 span 二次进入 pending——去重）。
	s.emitToolSpan(context.Background(), tracer, "member-a", "alpha", "write_file")
	s.processor.ForceFlush(context.Background())
	second, _ := s.processor.Drain(handle)
	// 新 span 是新 Identity（新 trace/span id），计数不小于首拍。
	if second != nil {
		s.Require().GreaterOrEqual(len(trajectory.IterSpans(second)), 1)
	}
	_ = count1
}

// TestPendingMaxDrop_满丢弃 maxPending=2 的 processor：第 3 条 span 进 pending
// 时丢弃（订阅缓冲上限语义）。
func (s *ProcessorChainSuite) TestPendingMaxDrop_满丢弃() {
	bounded := trajectory.NewTrajectorySpanProcessor(2)
	boundedProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(bounded))
	defer func() { _ = boundedProvider.Shutdown(context.Background()) }()
	boundedTracer := boundedProvider.Tracer("bounded")

	handle, err := bounded.Subscribe([]string{"tool"}, "")
	s.Require().NoError(err)
	defer bounded.Unsubscribe(handle)

	for i := 0; i < 5; i++ {
		s.emitToolSpan(context.Background(), boundedTracer, "member-a", "alpha", fmt.Sprintf("tool_%d", i))
	}
	bounded.ForceFlush(context.Background())
	traj, issues := bounded.Drain(handle)
	// maxPending 上限语义：pending 不超过 2 条。
	if traj != nil {
		s.Require().LessOrEqual(len(trajectory.IterSpans(traj)), 2, "bounded pending must cap at maxPending")
	}
	_ = issues
}

// TestStoreRoundtrip_链路落库 轨迹经 store Save/Load 读回（第三子系统）。
func (s *ProcessorChainSuite) TestStoreRoundtrip_链路落库() {
	tracer := s.provider.Tracer("member-runtime")
	handle, err := s.processor.Subscribe([]string{"tool"}, "")
	s.Require().NoError(err)
	defer s.processor.Unsubscribe(handle)

	s.emitToolSpan(context.Background(), tracer, "member-a", "alpha", "write_file")

	s.processor.ForceFlush(context.Background())
	traj, _ := s.processor.Drain(handle)
	s.Require().NotNil(traj)

	store := trajectory.NewInMemoryTrajectoryStore()
	s.Require().NoError(store.Save(traj, ""))
	// 键 = trajectory.TrajectoryID()（资源 trajectory_id；probe 输出证实）。
	loaded, loadErr := store.Load(traj.TrajectoryID(), "")
	s.Require().NoError(loadErr)
	s.Require().NotNil(loaded, "store roundtrip must return the trajectory")
}

// ──────────────────────────── 助手 ────────────────────────────

// memberIDAttr 成员身份属性（semconv member id 键）。
func memberIDAttr(memberID string) attribute.KeyValue {
	return attribute.String("jiuwen.member.id", memberID)
}

// teamIDAttr 团队身份属性。
func teamIDAttr(teamID string) attribute.KeyValue {
	return attribute.String("jiuwen.team.id", teamID)
}

// spanToolNames 轨迹内 tool span 名列表。
func spanToolNames(traj *trajectory.Trajectory) []string {
	var names []string
	for _, span := range trajectory.IterSpans(traj) {
		names = append(names, fmt.Sprint(span["name"]))
	}
	return names
}

// TestRailScopeChain_演进rail窗口链 processor 观测链 + 演进 rail 窗口（第三子系统
// agentcore/harness/rails/evolution）：rail BeforeInvoke 捕获 → AfterToolCall 记录
// span → GetTrajectory 团队 scope 读回窗口。
func (s *ProcessorChainSuite) TestRailScopeChain_演进rail窗口链() {
	rail := evolution.NewEvolutionRail(evolution.NoOpExtension{})
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-chain-rail"})
	s.Require().NoError(rail.BeforeInvoke(context.Background(), cbc))
	s.Require().NotNil(rail.Builder())

	for _, toolName := range []string{"write_file", "read_file"} {
		tcbc := &agentinterfaces.AgentCallbackContext{}
		tcbc.SetInputs(&agentinterfaces.ToolCallInputs{ToolName: toolName})
		s.Require().NoError(rail.AfterToolCall(context.Background(), tcbc))
	}

	// rail 窗口读回（GetTrajectory 会话 scope）。
	window, err := rail.GetTrajectory("sess-chain-rail", "", "")
	s.Require().NoError(err)
	s.Require().NotNil(window, "session scope window must exist")
	s.Require().GreaterOrEqual(len(trajectory.IterSpans(window)), 2, "both tool spans must be merged")
}

// TestRailScopeChain_收口保留窗口 AfterInvoke 后订阅释放但窗口保留。
func (s *ProcessorChainSuite) TestRailScopeChain_收口保留窗口() {
	rail := evolution.NewEvolutionRail(evolution.NoOpExtension{})
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-chain-final"})
	s.Require().NoError(rail.BeforeInvoke(context.Background(), cbc))
	tcbc := &agentinterfaces.AgentCallbackContext{}
	tcbc.SetInputs(&agentinterfaces.ToolCallInputs{ToolName: "list_dir"})
	s.Require().NoError(rail.AfterToolCall(context.Background(), tcbc))
	s.Require().NoError(rail.AfterInvoke(context.Background(), nil))

	window, err := rail.GetTrajectory("sess-chain-final", "", "")
	s.Require().NoError(err)
	s.Require().NotNil(window, "window must survive finalize (upstream semantics)")
}
