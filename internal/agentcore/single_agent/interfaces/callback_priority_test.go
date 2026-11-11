package interfaces

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// perEventRail 按事件返回不同优先级，用于验证分档生效。
type perEventRail struct {
	*BaseRail
}

func (r *perEventRail) CallbackPriorityFor(event AgentCallbackEvent) int {
	if event == CallbackAfterReactIteration {
		return -1000
	}
	return 1000
}

// TestBaseRail_CallbackPriorityFor_默认转发Priority 验证既有 rail 零改动可用。
// Python: AgentRail.callback_priority(event) 基类默认返回 self.priority
func TestBaseRail_CallbackPriorityFor_默认转发Priority(t *testing.T) {
	base := NewBaseRail().WithPriority(80)
	require.Equal(t, 80, base.CallbackPriorityFor(CallbackBeforeModelCall))
	require.Equal(t, 80, base.CallbackPriorityFor(CallbackAfterReactIteration))
}

// TestAgentRail_CallbackPriorityFor_可覆写 验证接口方法可被 rail 覆写。
// Python: iteration_limit.py:26-29 callback_priority 按事件分档
func TestAgentRail_CallbackPriorityFor_可覆写(t *testing.T) {
	var rail AgentRail = &perEventRail{BaseRail: NewBaseRail()}
	p := rail.(*perEventRail)
	require.Equal(t, 1000, p.CallbackPriorityFor(CallbackBeforeModelCall))
	require.Equal(t, -1000, p.CallbackPriorityFor(CallbackAfterReactIteration))
}
