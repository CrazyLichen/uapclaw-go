package adapter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGoalEventTypes_取值与上游逐字一致 协议字符串是前端契约键，不得本地化或改名。
// Python: interface_deep.py:188/192/19801/15635
func TestGoalEventTypes_取值与上游逐字一致(t *testing.T) {
	require.Equal(t, "goal.updated", goalUpdatedEventType)
	require.Equal(t, "execution.error", executionErrorEventType)
	require.Equal(t, "goal.snapshot", goalSnapshotEventType)
	require.Equal(t, "goal.confirm_required", goalConfirmRequiredEventType)
	require.Equal(t, "runtime.accepted", runtimeAcceptedEventType)
	require.Equal(t, "goal_intermediate", goalIntermediateField)
}

// TestGoalResultTypes_取值与上游逐字一致 结果类型决定后续走流式还是控制响应。
// Python: interface_deep.py:17903/17929
func TestGoalResultTypes_取值与上游逐字一致(t *testing.T) {
	require.Equal(t, "goal_stream", goalResultStream)
	require.Equal(t, "goal_control", goalResultControl)
	require.Equal(t, "goal_error", goalResultError)
	require.Equal(t, "goal_confirm_required", goalResultConfirmRequired)
}

// TestGoalErrorCodes_取值与上游逐字一致 错误码被前端按值分支，不得翻译。
// Python: interface_deep.py:17815/17833/17862/17900
func TestGoalErrorCodes_取值与上游逐字一致(t *testing.T) {
	require.Equal(t, "goal_manager_not_started", goalErrManagerNotStarted)
	require.Equal(t, "no_goal", goalErrNoGoal)
	require.Equal(t, "invalid_state", goalErrInvalidState)
	require.Equal(t, "invalid_action", goalErrInvalidAction)
	require.Equal(t, "already_exists", goalErrAlreadyExists)
}
