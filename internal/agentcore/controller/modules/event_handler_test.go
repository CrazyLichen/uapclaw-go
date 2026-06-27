package modules

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEventHandlerBase_HandleFollowUp_默认实现 验证 HandleFollowUp 返回 not_supported 状态
func TestEventHandlerBase_HandleFollowUp_默认实现(t *testing.T) {
	base := &EventHandlerBase{}
	result, err := base.HandleFollowUp(context.Background(), nil)
	assert.NoError(t, err)
	assert.Equal(t, "not_supported", result["status"])
}

// TestEventHandlerBase_PrepareRound_默认实现 验证 PrepareRound 返回 0
func TestEventHandlerBase_PrepareRound_默认实现(t *testing.T) {
	base := &EventHandlerBase{}
	round := base.PrepareRound()
	assert.Equal(t, 0, round)
}

// TestEventHandlerBase_WaitCompletion_默认实现 验证 WaitCompletion 返回 completed 状态
func TestEventHandlerBase_WaitCompletion_默认实现(t *testing.T) {
	base := &EventHandlerBase{}
	result := base.WaitCompletion(context.Background(), time.Second)
	assert.Equal(t, "completed", result["status"])
}

// TestEventHandlerBase_OnAbort_默认实现 验证 OnAbort 不 panic
func TestEventHandlerBase_OnAbort_默认实现(t *testing.T) {
	base := &EventHandlerBase{}
	assert.NotPanics(t, func() {
		base.OnAbort()
	})
}

// TestEventHandlerBase_GetBase 验证 GetBase 返回自身
func TestEventHandlerBase_GetBase(t *testing.T) {
	base := &EventHandlerBase{}
	assert.Equal(t, base, base.GetBase())
}

// TestEventHandlerInput_字段访问 验证 Event 和 Session 字段正确设置
func TestEventHandlerInput_字段访问(t *testing.T) {
	// 使用 nil 占位验证字段赋值和读取
	input := &EventHandlerInput{
		Event:   nil, // schema.Event 是接口，nil 合法
		Session: nil, // sessioninterfaces.SessionFacade 是接口，nil 合法
	}
	assert.Nil(t, input.Event)
	assert.Nil(t, input.Session)

	// 验证 EventHandlerBase 字段赋值
	base := &EventHandlerBase{
		Config:        &config.ControllerConfig{},
		ContextEngine: nil, // iface.ContextEngine 接口，nil 合法
		TaskManager:   &TaskManager{},
		TaskScheduler: &TaskScheduler{},
		AbilityMgr:    "test-ability",
	}
	assert.NotNil(t, base.Config)
	assert.NotNil(t, base.TaskManager)
	assert.NotNil(t, base.TaskScheduler)
	assert.Equal(t, "test-ability", base.AbilityMgr)
}

// TestEventHandlerBase_字段类型验证 验证各字段类型正确赋值
func TestEventHandlerBase_字段类型验证(t *testing.T) {
	tm := &TaskManager{}
	ts := &TaskScheduler{}
	base := &EventHandlerBase{
		Config:        nil,
		ContextEngine: nil,
		TaskManager:   tm,
		TaskScheduler: ts,
		AbilityMgr:    nil,
	}
	// 确保指针类型正确赋值
	assert.Equal(t, tm, base.TaskManager)
	assert.Equal(t, ts, base.TaskScheduler)
	// 确保接口类型可以赋值为 nil
	var _ iface.ContextEngine = base.ContextEngine
	var _ sessioninterfaces.SessionFacade = nil
}
