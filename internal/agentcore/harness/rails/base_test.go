package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewDeepAgentRail 验证默认优先级和零值字段
func TestNewDeepAgentRail(t *testing.T) {
	r := NewDeepAgentRail()
	assert.Equal(t, 50, r.Priority())
	assert.Nil(t, r.Workspace())
	assert.Nil(t, r.SysOperation())
}

// TestDeepAgentRail_SetWorkspace 验证 Set/Get Workspace
func TestDeepAgentRail_SetWorkspace(t *testing.T) {
	r := NewDeepAgentRail()
	assert.Nil(t, r.Workspace())
	// 设置 nil 不 panic 即可（实际使用传真实 Workspace）
	r.SetWorkspace(nil)
	assert.Nil(t, r.Workspace())
}

// TestDeepAgentRail_SetSysOperation 验证 Set/Get SysOperation
func TestDeepAgentRail_SetSysOperation(t *testing.T) {
	r := NewDeepAgentRail()
	assert.Nil(t, r.SysOperation())
	var nilOp sys_operation.SysOperation
	r.SetSysOperation(nilOp)
	assert.Nil(t, r.SysOperation())
}

// TestDeepAgentRail_GetCallbacks_基础hooks 验证未覆盖 task-iteration 时只返回基础事件
func TestDeepAgentRail_GetCallbacks_基础hooks(t *testing.T) {
	r := NewDeepAgentRail()
	callbacks := r.GetCallbacks()
	// BaseRail 默认不覆盖任何钩子，GetCallbacks 返回空 map
	// DeepAgentRail 的 GetCallbacks 会添加 task-iteration 钩子
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeTaskIteration)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterTaskIteration)
}

// TestDeepAgentRail_WithPriority 验证优先级设置
func TestDeepAgentRail_WithPriority(t *testing.T) {
	r := NewDeepAgentRail()
	r.WithPriority(90)
	assert.Equal(t, 90, r.Priority())
}

// taskIterationRail 覆盖 BeforeTaskIteration 的测试 Rail
type taskIterationRail struct {
	DeepAgentRail
	called bool
}

func (r *taskIterationRail) BeforeTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	r.called = true
	return nil
}

func (r *taskIterationRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	// 覆盖 BeforeTaskIteration
	callbacks[agentinterfaces.CallbackBeforeTaskIteration] = func(_ context.Context, railCtx any) error {
		return r.BeforeTaskIteration(context.Background(), railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// TestDeepAgentRail_GetCallbacks_合并taskIteration 验证子类覆盖 BeforeTaskIteration 后回调映射正确
func TestDeepAgentRail_GetCallbacks_合并taskIteration(t *testing.T) {
	r := &taskIterationRail{}
	callbacks := r.GetCallbacks()
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeTaskIteration)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterTaskIteration)
}
