package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// TestTurnNumberFromInputs_只接受正整数 验证 turn 号解析边界。
// Python: isinstance(raw, int) and not isinstance(raw, bool) and raw > 0（react_agent.py L2879-2884）
func TestTurnNumberFromInputs_只接受正整数(t *testing.T) {
	assert.Equal(t, 3, turnNumberFromInputs(map[string]any{"_turn_number": 3}))
	assert.Equal(t, 4, turnNumberFromInputs(map[string]any{"_turn_number": int64(4)}))
	assert.Equal(t, 0, turnNumberFromInputs(map[string]any{"_turn_number": 0}))
	assert.Equal(t, 0, turnNumberFromInputs(map[string]any{"_turn_number": -2}))
	assert.Equal(t, 0, turnNumberFromInputs(map[string]any{"_turn_number": "3"}))
	assert.Equal(t, 0, turnNumberFromInputs(map[string]any{"_turn_number": 3.0}))
	assert.Equal(t, 0, turnNumberFromInputs(map[string]any{"_turn_number": true}))
	assert.Equal(t, 0, turnNumberFromInputs(map[string]any{}))
	assert.Equal(t, 0, turnNumberFromInputs(nil))
}

// TestApplyTurnNumberExtra_双写 验证 turn 号同时进入 cbc.extra 与 invokeInputs.Extra。
// Python: ctx.extra["_turn_number"] = invoke_inputs.turn_number（react_agent.py L2966-2967）
func TestApplyTurnNumberExtra_双写(t *testing.T) {
	invokeInputs := &interfaces.InvokeInputs{TurnNumber: 5}
	cbc := interfaces.NewAgentCallbackContext(nil, invokeInputs, nil)

	applyTurnNumberExtra(cbc, invokeInputs)

	raw, ok := cbc.GetExtra("_turn_number")
	require.True(t, ok, "turn 号必须落到 cbc.extra")
	assert.Equal(t, 5, raw)
	require.NotNil(t, invokeInputs.Extra)
	assert.Equal(t, 5, invokeInputs.Extra["_turn_number"])
}

// TestApplyTurnNumberExtra_未提供不写 验证 0 值与 nil 入参都不污染 extra。
func TestApplyTurnNumberExtra_未提供不写(t *testing.T) {
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)

	applyTurnNumberExtra(cbc, &interfaces.InvokeInputs{})
	_, ok := cbc.GetExtra("_turn_number")
	assert.False(t, ok, "未提供 turn 号不得写入")

	// nil 入参不得 panic
	applyTurnNumberExtra(nil, &interfaces.InvokeInputs{TurnNumber: 1})
	applyTurnNumberExtra(cbc, nil)
	_, ok = cbc.GetExtra("_turn_number")
	assert.False(t, ok)
}
