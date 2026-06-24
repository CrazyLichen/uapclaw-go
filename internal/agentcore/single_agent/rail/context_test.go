package rail

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

// TestPushSteering_队列满丢弃 验证队列满时 PushSteering 不阻塞
func TestPushSteering_队列满丢弃(t *testing.T) {
	q := make(chan string, 2)
	ctx := NewAgentCallbackContext(nil, nil, nil)
	ctx.BindSteeringQueue(q)

	ctx.PushSteering("a")
	ctx.PushSteering("b")
	// 队列已满（容量 2），再写应 no-op 不阻塞
	ctx.PushSteering("c")

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

// TestRequestRetry_预留Panic 验证 RequestRetry 方法 panic 信息包含 "6.10"
func TestRequestRetry_预留Panic(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.PanicsWithValue(t, "TODO: 6.10 RetryRequest", func() {
		ctx.RequestRetry(1.0)
	})
}

// TestConsumeRetryRequest_预留Panic 验证 ConsumeRetryRequest 方法 panic 信息包含 "6.10"
func TestConsumeRetryRequest_预留Panic(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.PanicsWithValue(t, "TODO: 6.10 RetryRequest", func() {
		_ = ctx.ConsumeRetryRequest()
	})
}

// TestRequestForceFinish_预留Panic 验证 RequestForceFinish 方法 panic 信息包含 "6.10"
func TestRequestForceFinish_预留Panic(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.PanicsWithValue(t, "TODO: 6.10 ForceFinishRequest", func() {
		ctx.RequestForceFinish(nil)
	})
}

// TestConsumeForceFinish_预留Panic 验证 ConsumeForceFinish 方法 panic 信息包含 "6.10"
func TestConsumeForceFinish_预留Panic(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.PanicsWithValue(t, "TODO: 6.10 ForceFinishRequest", func() {
		_ = ctx.ConsumeForceFinish()
	})
}

// TestHasForceFinishRequest_预留Panic 验证 HasForceFinishRequest 方法 panic 信息包含 "6.10"
func TestHasForceFinishRequest_预留Panic(t *testing.T) {
	ctx := NewAgentCallbackContext(nil, nil, nil)
	assert.PanicsWithValue(t, "TODO: 6.10 ForceFinishRequest", func() {
		_ = ctx.HasForceFinishRequest()
	})
}
