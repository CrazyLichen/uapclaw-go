package interfaces

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeBaseAgent 实现 BaseAgent 接口，用于测试
type fakeBaseAgent struct {
	// cbMgr 回调管理器
	cbMgr *AgentCallbackManager
	// agentID Agent 唯一标识
	agentID string
}

func (f *fakeBaseAgent) Configure(_ context.Context, _ AgentConfig) error { return nil }
func (f *fakeBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeBaseAgent) Card() *agentschema.AgentCard                               { return nil }
func (f *fakeBaseAgent) Config() AgentConfig                                        { return nil }
func (f *fakeBaseAgent) AbilityManager() AbilityManagerInterface                    { return nil }
func (f *fakeBaseAgent) CallbackManager() *AgentCallbackManager                     { return f.cbMgr }
func (f *fakeBaseAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return nil }
func (f *fakeBaseAgent) RegisterCallback(_ context.Context, _ AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgent) RegisterRail(_ context.Context, _ AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgent) UnregisterRail(_ context.Context, _ AgentRail) error { return nil }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewAgentCallbackContext 验证构造函数字段初始化
func TestNewAgentCallbackContext(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.NotNil(t, ctx)
	assert.Nil(t, ctx.Agent())
	assert.Nil(t, ctx.Inputs())
	assert.Nil(t, ctx.Session())
	assert.NotNil(t, ctx.Extra())
	assert.Empty(t, ctx.Extra())
}

// TestAgentCallbackContext_GetterSetter 验证各 getter/setter 方法
func TestAgentCallbackContext_GetterSetter(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, &InvokeInputs{}, nil)

	// Event
	ctx.SetEvent(CallbackBeforeInvoke)
	assert.Equal(t, CallbackBeforeInvoke, ctx.Event())

	// Inputs
	inputs := &ModelCallInputs{}
	ctx.SetInputs(inputs)
	assert.Equal(t, inputs, ctx.Inputs())

	// Exception
	err := assert.AnError
	ctx.SetException(err)
	assert.Equal(t, err, ctx.Exception())

	// RetryAttempt
	ctx.SetRetryAttempt(3)
	assert.Equal(t, 3, ctx.RetryAttempt())

	// Extra 可以直接修改
	ctx.Extra()["key"] = "value"
	assert.Equal(t, "value", ctx.Extra()["key"])
}

// TestPushSteering_无队列 验证无队列时 PushSteering 为 no-op
func TestPushSteering_无队列(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	// 不应 panic
	_ = ctx.PushSteering("test")
}

// TestPushSteering_正常写入 验证正常写入后可 DrainSteering 读出
func TestPushSteering_正常写入(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	q := make(chan string, 4096)
	ctx.BindSteeringQueue(q)

	_ = ctx.PushSteering("msg1")
	_ = ctx.PushSteering("msg2")

	msgs := ctx.DrainSteering()
	assert.Equal(t, []string{"msg1", "msg2"}, msgs)
}

// TestPushSteering_队列满丢弃 验证队列满时 PushSteering 返回 ErrSteeringQueueFull
func TestPushSteering_队列满丢弃(t *testing.T) {
	q := make(chan string, 2)
	ctx := NewAgentCallbackContext(nil, nil, nil)
	ctx.BindSteeringQueue(q)

	_ = ctx.PushSteering("a")
	_ = ctx.PushSteering("b")
	// 队列已满（容量 2），再写应返回 ErrSteeringQueueFull
	err := ctx.PushSteering("c")
	assert.Equal(t, ErrSteeringQueueFull, err)

	msgs := ctx.DrainSteering()
	assert.Equal(t, []string{"a", "b"}, msgs)
}

// TestDrainSteering_无队列 验证无队列时返回 nil
func TestDrainSteering_无队列(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.Nil(t, ctx.DrainSteering())
}

// TestDrainSteering_空队列 验证空队列返回 nil
func TestDrainSteering_空队列(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	ctx.BindSteeringQueue(make(chan string, 4096))
	assert.Nil(t, ctx.DrainSteering())
}

// TestHasPendingSteering 验证各种状态下 HasPendingSteering
func TestHasPendingSteering(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)

	// 无队列
	assert.False(t, ctx.HasPendingSteering())

	// 空队列
	q := make(chan string, 4096)
	ctx.BindSteeringQueue(q)
	assert.False(t, ctx.HasPendingSteering())

	// 有消息
	_ = ctx.PushSteering("msg")
	assert.True(t, ctx.HasPendingSteering())

	// drain 后
	ctx.DrainSteering()
	assert.False(t, ctx.HasPendingSteering())
}

// TestBindSteeringQueue 验证绑定后 push/drain 可用
func TestBindSteeringQueue(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	q := make(chan string, 4096)
	ctx.BindSteeringQueue(q)
	assert.Equal(t, q, ctx.SteeringQueue())

	_ = ctx.PushSteering("test")
	msgs := ctx.DrainSteering()
	assert.Equal(t, []string{"test"}, msgs)
}

// TestSteeringQueue 验证 SteeringQueue 返回绑定的队列
func TestSteeringQueue(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.Nil(t, ctx.SteeringQueue())

	q := make(chan string, 4096)
	ctx.BindSteeringQueue(q)
	assert.Equal(t, q, ctx.SteeringQueue())
}

// TestFireLifecycle_正常流程 验证 before → fn → after，inputs 恢复
func TestFireLifecycle_正常流程(t *testing.T) {
	origInputs := &InvokeInputs{}
	ctx := NewAgentCallbackContext(nil, origInputs, nil)

	executed := false
	err := ctx.FireLifecycle(context.Background(), CallbackBeforeInvoke, CallbackAfterInvoke, func() error {
		executed = true
		// fn 内修改 inputs
		ctx.SetInputs(&ModelCallInputs{})
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, executed)
	// inputs 应恢复为原始值
	assert.Equal(t, origInputs, ctx.Inputs())
}

// TestFireLifecycle_异常时设置Exception 验证 fn 出错时 exception 被设置
func TestFireLifecycle_异常时设置Exception(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, &InvokeInputs{}, nil)

	testErr := assert.AnError
	err := ctx.FireLifecycle(context.Background(), CallbackBeforeModelCall, CallbackAfterModelCall, func() error {
		return testErr
	})

	assert.Equal(t, testErr, err)
	assert.Equal(t, testErr, ctx.Exception())
}

// TestFireLifecycle_恢复Inputs 验证 fn 内修改 inputs 后，after 时恢复
func TestFireLifecycle_恢复Inputs(t *testing.T) {
	origInputs := &InvokeInputs{}
	ctx := NewAgentCallbackContext(nil, origInputs, nil)

	_ = ctx.FireLifecycle(context.Background(), CallbackBeforeToolCall, CallbackAfterToolCall, func() error {
		ctx.SetInputs(&ToolCallInputs{})
		assert.IsType(t, &ToolCallInputs{}, ctx.Inputs()) // fn 内 inputs 已变
		return nil
	})

	assert.Equal(t, origInputs, ctx.Inputs()) // fn 后 inputs 恢复
}

// TestFire_无回调管理器 agent 为 nil 时 Fire 返回 nil（无 panic）
func TestFire_无回调管理器(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	err := ctx.Fire(context.Background(), CallbackBeforeModelCall)
	assert.NoError(t, err)
}

// TestFire_回调管理器为nil agent 存在但 CallbackManager 返回 nil 时 Fire 返回 nil
func TestFire_回调管理器为nil(t *testing.T) {
	agent := &fakeBaseAgent{cbMgr: nil}
	ctx := NewAgentCallbackContext(agent, nil, nil)
	err := ctx.Fire(context.Background(), CallbackBeforeModelCall)
	assert.NoError(t, err)
}

// TestFire_正常触发 agent 和 CallbackManager 都存在时 Fire 正常执行
func TestFire_正常触发(t *testing.T) {
	mgr := NewAgentCallbackManager("test_fire_agent")
	defer mgr.Clear()

	var called bool
	fn := func(_ context.Context, _ any) error {
		called = true
		return nil
	}
	mgr.RegisterCallback(context.Background(), CallbackBeforeModelCall, fn)

	agent := &fakeBaseAgent{cbMgr: mgr}
	ctx := NewAgentCallbackContext(agent, nil, nil)
	err := ctx.Fire(context.Background(), CallbackBeforeModelCall)
	assert.NoError(t, err)
	assert.True(t, called)
}

// ──────────────────────────── Retry / ForceFinish ────────────────────────────

// TestRequestRetry_设置请求 验证 RequestRetry 设置 retryRequest
func TestRequestRetry_设置请求(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.Nil(t, ctx.ConsumeRetryRequest())

	ctx.RequestRetry(2.5)
	req := ctx.ConsumeRetryRequest()
	assert.NotNil(t, req)
	assert.Equal(t, 2.5, req.DelaySeconds)
}

// TestConsumeRetryRequest_一次性消费 验证 consume-and-clear 模式
func TestConsumeRetryRequest_一次性消费(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	ctx.RequestRetry(1.0)

	// 第一次消费返回请求
	req := ctx.ConsumeRetryRequest()
	assert.NotNil(t, req)
	assert.Equal(t, 1.0, req.DelaySeconds)

	// 第二次消费返回 nil（已清除）
	req2 := ctx.ConsumeRetryRequest()
	assert.Nil(t, req2)
}

// TestRequestForceFinish_设置请求 验证 RequestForceFinish 设置 forceFinishRequest
func TestRequestForceFinish_设置请求(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.False(t, ctx.HasForceFinishRequest())

	result := map[string]any{"status": "early_exit"}
	ctx.RequestForceFinish(result)
	assert.True(t, ctx.HasForceFinishRequest())

	req := ctx.ConsumeForceFinish()
	assert.NotNil(t, req)
	assert.Equal(t, "early_exit", req.Result["status"])
}

// TestConsumeForceFinish_一次性消费 验证 consume-and-clear 模式
func TestConsumeForceFinish_一次性消费(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	ctx.RequestForceFinish(map[string]any{"ok": true})

	// 第一次消费返回请求
	req := ctx.ConsumeForceFinish()
	assert.NotNil(t, req)
	assert.True(t, req.Result["ok"].(bool))

	// 第二次消费返回 nil（已清除）
	req2 := ctx.ConsumeForceFinish()
	assert.Nil(t, req2)
}

// TestHasForceFinishRequest_消费后为false 验证消费后 HasForceFinishRequest 返回 false
func TestHasForceFinishRequest_消费后为false(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	ctx.RequestForceFinish(map[string]any{})
	assert.True(t, ctx.HasForceFinishRequest())

	_ = ctx.ConsumeForceFinish()
	assert.False(t, ctx.HasForceFinishRequest())
}

// TestRequestRetry_负数归零 验证负数 delaySeconds 被静默归零
func TestRequestRetry_负数归零(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)

	ctx.RequestRetry(-1.5)
	req := ctx.ConsumeRetryRequest()
	assert.NotNil(t, req)
	assert.Equal(t, 0.0, req.DelaySeconds)

	// 正数不受影响
	ctx.RequestRetry(3.0)
	req = ctx.ConsumeRetryRequest()
	assert.NotNil(t, req)
	assert.Equal(t, 3.0, req.DelaySeconds)

	// 零不受影响
	ctx.RequestRetry(0.0)
	req = ctx.ConsumeRetryRequest()
	assert.NotNil(t, req)
	assert.Equal(t, 0.0, req.DelaySeconds)
}

// TestForkForToolCall_字段共享与隔离 验证 ForkForToolCall 的共享/独立语义
func TestForkForToolCall_字段共享与隔离(t *testing.T) {
	agent := &fakeBaseAgent{agentID: "test-agent"}
	sess := session.NewSession()
	parentInputs := &InvokeInputs{}
	parent := NewAgentCallbackContext(agent, parentInputs, sess)
	q := make(chan string, 4096)
	parent.BindSteeringQueue(q)
	parent.Extra()["shared_key"] = "shared_val"

	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "search", Arguments: `{"q":"hello"}`}
	child := parent.ForkForToolCall(toolCall)

	// 共享字段
	assert.Equal(t, agent, child.Agent())                          // agent 引用共享
	assert.Equal(t, sess, child.Session())                         // session 引用共享
	assert.Equal(t, parent.Extra(), child.Extra())                 // extra 字典引用共享
	assert.Equal(t, parent.SteeringQueue(), child.SteeringQueue()) // steeringQueue 引用共享

	// 独立字段
	assert.Nil(t, child.ConsumeRetryRequest())     // retryRequest 独立零值
	assert.False(t, child.HasForceFinishRequest()) // forceFinishRequest 独立零值
	assert.Nil(t, child.Exception())               // exception 独立零值
	assert.Equal(t, 0, child.RetryAttempt())       // retryAttempt 独立零值

	// inputs 为 ToolCallInputs
	inputs, ok := child.Inputs().(*ToolCallInputs)
	assert.True(t, ok)
	assert.Equal(t, toolCall, inputs.ToolCall)
	assert.Equal(t, "search", inputs.ToolName)

	// extra 修改互相可见（引用共享）
	child.Extra()["child_key"] = "child_val"
	assert.Equal(t, "child_val", parent.Extra()["child_key"])

	// force-finish 独立：子 ctx 设置不影响父
	child.RequestForceFinish(map[string]any{"reason": "budget_exceeded"})
	assert.True(t, child.HasForceFinishRequest())
	assert.False(t, parent.HasForceFinishRequest())
}

// TestAgentCallbackContext_Config_SetConfig 验证 Config/SetConfig 方法
func TestAgentCallbackContext_Config_SetConfig(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)

	// 默认 nil
	assert.Nil(t, ctx.Config())

	// SetConfig
	ctx.SetConfig("some_config")
	assert.Equal(t, "some_config", ctx.Config())
}

// TestAgentCallbackContext_ModelContext_SetModelContext 验证 ModelContext/SetModelContext 方法
func TestAgentCallbackContext_ModelContext_SetModelContext(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)

	// 默认 nil
	assert.Nil(t, ctx.ModelContext())

	// SetModelContext（nil 满足接口）
	ctx.SetModelContext(nil)
	assert.Nil(t, ctx.ModelContext())
}

// TestAllBaseCallbackEvents 验证 AllBaseCallbackEvents 返回 8 个基础事件
func TestAllBaseCallbackEvents(t *testing.T) {
	events := AllBaseCallbackEvents()
	assert.Equal(t, 8, len(events))

	// 验证包含基础事件
	seen := make(map[AgentCallbackEvent]bool)
	for _, e := range events {
		seen[e] = true
	}
	assert.True(t, seen[CallbackBeforeInvoke])
	assert.True(t, seen[CallbackAfterInvoke])
	assert.True(t, seen[CallbackBeforeModelCall])
	assert.True(t, seen[CallbackAfterModelCall])
	assert.True(t, seen[CallbackOnModelException])
	assert.True(t, seen[CallbackBeforeToolCall])
	assert.True(t, seen[CallbackAfterToolCall])
	assert.True(t, seen[CallbackOnToolException])
	// 不应包含 task-iteration 事件
	assert.False(t, seen[CallbackBeforeTaskIteration])
	assert.False(t, seen[CallbackAfterTaskIteration])
}

// TestAllDeepCallbackEvents 验证 AllDeepCallbackEvents 返回 2 个 Deep 扩展事件
func TestAllDeepCallbackEvents(t *testing.T) {
	events := AllDeepCallbackEvents()
	assert.Equal(t, 2, len(events))
	assert.Equal(t, CallbackBeforeTaskIteration, events[0])
	assert.Equal(t, CallbackAfterTaskIteration, events[1])
}

// TestWithSession 验证 WithSession 选项函数
func TestWithSession(t *testing.T) {
	sess := session.NewSession(session.WithSessionID("test_sess"))
	opt := WithSession(sess)
	o := &AgentOptions{}
	opt(o)
	assert.Equal(t, sess, o.Session)
}

// TestWithStreamModes 验证 WithStreamModes 选项函数
func TestWithStreamModes(t *testing.T) {
	modes := []stream.StreamMode{stream.StreamModeOutput}
	opt := WithStreamModes(modes)
	o := &AgentOptions{}
	opt(o)
	assert.Equal(t, modes, o.StreamModes)
}

// TestNewAgentOptions 验证 NewAgentOptions 构建函数
func TestNewAgentOptions(t *testing.T) {
	sess := session.NewSession(session.WithSessionID("test_sess"))
	modes := []stream.StreamMode{stream.StreamModeOutput}

	opts := NewAgentOptions(
		WithSession(sess),
		WithStreamModes(modes),
	)
	assert.Equal(t, sess, opts.Session)
	assert.Equal(t, modes, opts.StreamModes)
}

// TestNewAgentOptions_空选项 验证无选项时返回零值
func TestNewAgentOptions_空选项(t *testing.T) {
	opts := NewAgentOptions()
	assert.Nil(t, opts.Session)
	assert.Nil(t, opts.StreamModes)
}

// TestWithWorkflowSession 验证 WithWorkflowSession 选项函数
func TestWithWorkflowSession(t *testing.T) {
	sess := &session.WorkflowSession{}
	opt := WithWorkflowSession(sess)
	o := &WorkflowOptions{}
	opt(o)
	assert.Equal(t, sess, o.Session)
}

// TestWithWorkflowContext 验证 WithWorkflowContext 选项函数
func TestWithWorkflowContext(t *testing.T) {
	ctx := map[string]any{"key": "value"}
	opt := WithWorkflowContext(ctx)
	o := &WorkflowOptions{}
	opt(o)
	assert.Equal(t, ctx, o.Context)
}

// TestNewWorkflowOptions 验证 NewWorkflowOptions 构建函数
func TestNewWorkflowOptions(t *testing.T) {
	sess := &session.WorkflowSession{}
	ctx := map[string]any{"key": "value"}

	opts := NewWorkflowOptions(
		WithWorkflowSession(sess),
		WithWorkflowContext(ctx),
	)
	assert.Equal(t, sess, opts.Session)
	assert.Equal(t, ctx, opts.Context)
}

// TestNewWorkflowOptions_空选项 验证无选项时返回零值
func TestNewWorkflowOptions_空选项(t *testing.T) {
	opts := NewWorkflowOptions()
	assert.Nil(t, opts.Session)
	assert.Nil(t, opts.Context)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
