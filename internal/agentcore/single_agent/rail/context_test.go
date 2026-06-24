package rail

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeRailAgent 实现 RailAgent 接口，用于测试
type fakeRailAgent struct {
	// cbMgr 回调管理器
	cbMgr *AgentCallbackManager
	// agentID Agent 唯一标识
	agentID string
}

func (f *fakeRailAgent) CallbackManager() *AgentCallbackManager { return f.cbMgr }
func (f *fakeRailAgent) AgentID() string                        { return f.agentID }

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
	ctx.PushSteering("test")
}

// TestPushSteering_正常写入 验证正常写入后可 DrainSteering 读出
func TestPushSteering_正常写入(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	q := make(chan string, steeringQueueSize)
	ctx.BindSteeringQueue(q)

	ctx.PushSteering("msg1")
	ctx.PushSteering("msg2")

	msgs := ctx.DrainSteering()
	assert.Equal(t, []string{"msg1", "msg2"}, msgs)
}

// TestPushSteering_队列满丢弃 验证队列满时 PushSteering 返回 ErrSteeringQueueFull
func TestPushSteering_队列满丢弃(t *testing.T) {
	q := make(chan string, 2)
	ctx := NewAgentCallbackContext(nil, nil, nil)
	ctx.BindSteeringQueue(q)

	ctx.PushSteering("a")
	ctx.PushSteering("b")
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
	ctx.BindSteeringQueue(make(chan string, steeringQueueSize))
	assert.Nil(t, ctx.DrainSteering())
}

// TestHasPendingSteering 验证各种状态下 HasPendingSteering
func TestHasPendingSteering(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)

	// 无队列
	assert.False(t, ctx.HasPendingSteering())

	// 空队列
	q := make(chan string, steeringQueueSize)
	ctx.BindSteeringQueue(q)
	assert.False(t, ctx.HasPendingSteering())

	// 有消息
	ctx.PushSteering("msg")
	assert.True(t, ctx.HasPendingSteering())

	// drain 后
	ctx.DrainSteering()
	assert.False(t, ctx.HasPendingSteering())
}

// TestBindSteeringQueue 验证绑定后 push/drain 可用
func TestBindSteeringQueue(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	q := make(chan string, steeringQueueSize)
	ctx.BindSteeringQueue(q)
	assert.Equal(t, q, ctx.SteeringQueue())

	ctx.PushSteering("test")
	msgs := ctx.DrainSteering()
	assert.Equal(t, []string{"test"}, msgs)
}

// TestSteeringQueue 验证 SteeringQueue 返回绑定的队列
func TestSteeringQueue(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.Nil(t, ctx.SteeringQueue())

	q := make(chan string, steeringQueueSize)
	ctx.BindSteeringQueue(q)
	assert.Equal(t, q, ctx.SteeringQueue())
}

// TestFireLifecycle_正常流程 验证 before → fn → after，inputs 恢复
func TestFireLifecycle_正常流程(t *testing.T) {
	origInputs := &InvokeInputs{}
	ctx := NewAgentCallbackContext(nil, origInputs, nil)

	executed := false
	err := ctx.FireLifecycle(CallbackBeforeInvoke, CallbackAfterInvoke, func() error {
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
	err := ctx.FireLifecycle(CallbackBeforeModelCall, CallbackAfterModelCall, func() error {
		return testErr
	})

	assert.Equal(t, testErr, err)
	assert.Equal(t, testErr, ctx.Exception())
}

// TestFireLifecycle_恢复Inputs 验证 fn 内修改 inputs 后，after 时恢复
func TestFireLifecycle_恢复Inputs(t *testing.T) {
	origInputs := &InvokeInputs{}
	ctx := NewAgentCallbackContext(nil, origInputs, nil)

	_ = ctx.FireLifecycle(CallbackBeforeToolCall, CallbackAfterToolCall, func() error {
		ctx.SetInputs(&ToolCallInputs{})
		assert.IsType(t, &ToolCallInputs{}, ctx.Inputs()) // fn 内 inputs 已变
		return nil
	})

	assert.Equal(t, origInputs, ctx.Inputs()) // fn 后 inputs 恢复
}

// TestFire_无回调管理器 agent 为 nil 时 Fire 返回 nil（无 panic）
func TestFire_无回调管理器(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	err := ctx.Fire(CallbackBeforeModelCall)
	assert.NoError(t, err)
}

// TestFire_回调管理器为nil agent 存在但 CallbackManager 返回 nil 时 Fire 返回 nil
func TestFire_回调管理器为nil(t *testing.T) {
	agent := &fakeRailAgent{cbMgr: nil}
	ctx := NewAgentCallbackContext(agent, nil, nil)
	err := ctx.Fire(CallbackBeforeModelCall)
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

	agent := &fakeRailAgent{cbMgr: mgr}
	ctx := NewAgentCallbackContext(agent, nil, nil)
	err := ctx.Fire(CallbackBeforeModelCall)
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

// ──────────────────────────── 非导出函数 ────────────────────────────
