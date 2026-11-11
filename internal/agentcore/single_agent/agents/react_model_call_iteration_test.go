package agents

import (
	"testing"

	"github.com/stretchr/testify/require"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// TestModelCallInputs_有ReactIteration字段 验证字段存在且可读写。
// Python: rail/base.py:319 react_iteration: int = 0
func TestModelCallInputs_有ReactIteration字段(t *testing.T) {
	inputs := &agentinterfaces.ModelCallInputs{ReactIteration: 3}
	require.Equal(t, 3, inputs.ReactIteration)
}

// TestExtraInt_各类型取值 验证 extra 读取助手。
// Python: react_agent.py:1043 int(ctx.extra.get("_react_iteration", 0) or 0)
func TestExtraInt_各类型取值(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)

	require.Equal(t, 0, extraInt(cbc, "_react_iteration"), "缺失时返回 0")

	cbc.SetExtra("_react_iteration", 5)
	require.Equal(t, 5, extraInt(cbc, "_react_iteration"))

	cbc.SetExtra("_react_iteration", int64(7))
	require.Equal(t, 7, extraInt(cbc, "_react_iteration"))

	cbc.SetExtra("_react_iteration", float64(9))
	require.Equal(t, 9, extraInt(cbc, "_react_iteration"))

	cbc.SetExtra("_react_iteration", "bad")
	require.Equal(t, 0, extraInt(cbc, "_react_iteration"), "类型不符返回 0")

	cbc.SetExtra("_react_iteration", -1)
	require.Equal(t, 0, extraInt(cbc, "_react_iteration"), "负值返回 0")
}
