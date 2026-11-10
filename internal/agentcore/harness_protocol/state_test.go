package harness_protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHarnessState_值集 验证 5 个相位取值与上游逐字一致。
// Python: state.py:9-20 HarnessState（idle/running/pausing/paused/terminated）
func TestHarnessState_值集(t *testing.T) {
	assert.Equal(t, HarnessState("idle"), HarnessStateIdle)
	assert.Equal(t, HarnessState("running"), HarnessStateRunning)
	assert.Equal(t, HarnessState("pausing"), HarnessStatePausing)
	assert.Equal(t, HarnessState("paused"), HarnessStatePaused)
	assert.Equal(t, HarnessState("terminated"), HarnessStateTerminated)
}

// TestHarnessState_零值为空串 验证零值不等于任何合法相位（需显式赋 Idle）。
func TestHarnessState_零值为空串(t *testing.T) {
	var s HarnessState
	assert.Equal(t, HarnessState(""), s)
	assert.NotEqual(t, HarnessStateIdle, s)
}
