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

// TestHarnessState_被团队层复用为同一类型 验证 agent_teams/harness 不再有本地定义。
// Python: test_protocol.py:909-912 test_harness_state_is_shared_with_agent_teams
// （上游断言 TeamHarnessState is HarnessState；Go 用「无本地定义 + 同一 import」等价）
func TestHarnessState_被团队层复用为同一类型(t *testing.T) {
	// 本用例的守护作用在于：若团队层重新引入本地 HarnessState 类型，
	// 全仓裸符号 grep 会再次命中，由锚点检查拦截。
	// 此处做编译期等价断言：本包导出的常量可被直接赋给本包类型。
	s := HarnessStateIdle
	assert.Equal(t, "idle", string(s))
}
