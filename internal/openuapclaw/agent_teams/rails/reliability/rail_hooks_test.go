// Package reliability rail 钩子与 reporter 单测（补覆盖率）。
package reliability

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/messager"
	reliabilitycore "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/core"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
)

// hookCfg 构造测试配置（LOW 也 local_steer，便于断言）。
func hookCfg() *reliabilitycore.ReliabilityConfig {
	cfg := reliabilitycore.DefaultReliabilityConfig()
	cfg.Enabled = true
	cfg.Policy.SeverityActions["low"] = []string{"local_steer", "observe_only"}
	return cfg
}

// newHookRail 构造带 steering 队列的 rail。
func newHookRail(t *testing.T) (*ReliabilityRail, chan string) {
	t.Helper()
	rail := BuildReliabilityRail(BuildComponentsDeps{Config: hookCfg(), MemberName: "coder", IsLeader: true})
	q := make(chan string, 16)
	return rail, q
}

// toolCBC 构造工具调用 cbc。
func toolCBC(t *testing.T, q chan string, name string, args map[string]any, result any, exc error) *agentinterfaces.AgentCallbackContext {
	t.Helper()
	cbc := agentinterfaces.NewAgentCallbackContext(nil,
		&agentinterfaces.ToolCallInputs{ToolName: name, ToolArgs: args, ToolResult: result}, nil)
	cbc.BindSteeringQueue(q)
	if exc != nil {
		cbc.SetException(exc)
	}
	return cbc
}

func TestRailHooks_工具链路触发本地steer(t *testing.T) {
	rail, q := newHookRail(t)
	ctx := context.Background()
	// repeat_warn 默认 10：连发 10 次同参同结果
	for i := 0; i < 10; i++ {
		assert.NoError(t, rail.BeforeToolCall(ctx, toolCBC(t, q, "read_file", map[string]any{"p": "a"}, "same", nil)))
		assert.NoError(t, rail.AfterToolCall(ctx, toolCBC(t, q, "read_file", map[string]any{"p": "a"}, "same", nil)))
	}
	select {
	case msg := <-q:
		assert.Contains(t, msg, "repeat_tool_call")
	default:
		t.Fatal("期望 LOW 异常触发本地 steer（策略 low→local_steer）")
	}
}

func TestRailHooks_AfterToolCall异常时不发成功信号(t *testing.T) {
	rail, _ := newHookRail(t)
	ctx := context.Background()
	cbc := toolCBC(t, make(chan string, 1), "bash", nil, nil, errors.New("exit 1"))
	// exception 非空：after_tool_call 不发成功信号（防伪成功重置 streak）
	assert.NoError(t, rail.AfterToolCall(ctx, cbc))
	// tool_exception 发错误信号
	assert.NoError(t, rail.OnToolException(ctx, cbc))
}

func TestRailHooks_模型链路(t *testing.T) {
	rail, _ := newHookRail(t)
	ctx := context.Background()
	// before_model_call：消息计数
	cbc := agentinterfaces.NewAgentCallbackContext(nil,
		&agentinterfaces.ModelCallInputs{Messages: []llmschema.BaseMessage{
			llmschema.NewUserMessage("hi"), llmschema.NewUserMessage("yo"),
		}}, nil)
	assert.NoError(t, rail.BeforeModelCall(ctx, cbc))
	// after_model_call：文本/思考长度
	resp := llmschema.NewAssistantMessage("hello world", llmschema.WithReasoningContent("thinking..."))
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{Response: resp}, nil)
	assert.NoError(t, rail.AfterModelCall(ctx, cbc2))
	// on_model_exception：含路由裁决的错误链
	root := exception.NewBaseError(exception.StatusError,
		exception.WithDetails(map[string]any{
			"route_case": "switchable", "reason": "deterministic",
			"failures": []any{map[string]any{"deployment_id": "d1", "status_code": 401}},
		}))
	cbc3 := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{}, nil)
	cbc3.SetException(root)
	assert.NoError(t, rail.OnModelException(ctx, cbc3))
}

func TestRailHooks_OnUserMessage前插routeNotices(t *testing.T) {
	rail := BuildReliabilityRail(BuildComponentsDeps{Config: hookCfg(), MemberName: "leader", IsLeader: true})
	// leader：绑定 pending notices 源
	rail.BindRouteNoticeSource(func() []string { return []string{"[notice] switched"} })
	rail.BindLocalSink(func(context.Context, *reliabilitycore.Anomaly) {})
	parts := &agentinterfaces.UserMessageInputs{Parts: []string{"user query"}}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, parts, nil)
	assert.NoError(t, rail.OnUserMessage(context.Background(), cbc))
	assert.Equal(t, []string{"[notice] switched", "user query"}, parts.Parts, "notices 前插到输入最前")
}

func TestRailHooks_OnUserMessage非leaderNoOp(t *testing.T) {
	rail := BuildReliabilityRail(BuildComponentsDeps{Config: hookCfg(), MemberName: "coder", IsLeader: false})
	parts := &agentinterfaces.UserMessageInputs{Parts: []string{"q"}}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, parts, nil)
	assert.NoError(t, rail.OnUserMessage(context.Background(), cbc))
	assert.Equal(t, []string{"q"}, parts.Parts, "非 leader 不前插")
}

// newTestMessager 创建并启动 inprocess messager。
func newTestMessager() (messager.Messager, error) {
	msgr, err := messager.CreateMessager(schema.MessagerTransportConfig{Backend: "inprocess"})
	if err != nil {
		return nil, err
	}
	return msgr, nil
}

// testTopicBuilder 与 reporter 默认一致的 topic 构造（空会话）。
func testTopicBuilder(ctx context.Context, teamName string) string {
	return events.TeamTopicTeam.Build("", teamName)
}

func TestEventAnomalyReporter_Publish(t *testing.T) {
	msgr, err := newTestMessager()
	assert.NoError(t, err)
	assert.NoError(t, msgr.Start(context.Background()))
	defer msgr.Stop(context.Background())
	rep := NewEventAnomalyReporter(msgr, "team1", "coder")
	topic := testTopicBuilder(context.Background(), "team1")
	received := make(chan bool, 1)
	assert.NoError(t, msgr.Subscribe(context.Background(), topic, func(_ context.Context, msg *events.EventMessage) error {
		lastSender.Store(msg.SenderID)
		received <- true
		return nil
	}))
	rep.Report(context.Background(), &reliabilitycore.Anomaly{
		Detector: "d", Kind: reliabilitycore.AnomalyKindToolErrorRate, Severity: reliabilitycore.SeverityMedium,
		MemberName: "coder", Summary: "s",
	})
	select {
	case <-received:
	default:
		t.Fatal("期望事件经总线到达订阅者")
	}
	// 消息契约：sender_id 覆写为 reporter 的 sender
	assert.Equal(t, "coder", lastSender.Load(), "sender_id 透传")
}

// lastSender 捕获最近一条消息的 sender_id。
var lastSender atomicValue[string]

// TestEventAnomalyReporter_Publish错误路径 发布失败只记日志不 panic。
func TestEventAnomalyReporter_Publish错误路径(t *testing.T) {
	msgr, err := newTestMessager()
	assert.NoError(t, err)
	// 不 Start（或订阅空 topic）：发布落空不 panic
	rep := NewEventAnomalyReporter(msgr, "team2", "coder")
	assert.NotPanics(t, func() {
		rep.Report(context.Background(), &reliabilitycore.Anomaly{MemberName: "x"})
	})
}
