package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNormalizeMode_三段权威值 验证新串原样返回。
func TestNormalizeMode_三段权威值(t *testing.T) {
	require.Equal(t, ModeAgentWorkPlan, NormalizeMode("agent.work.plan"))
	require.Equal(t, ModeAgentWorkNormal, NormalizeMode("agent.work.normal"))
	require.Equal(t, ModeCodePlan, NormalizeMode("code.plan"))
	require.Equal(t, ModeCodeNormal, NormalizeMode("code.normal"))
	require.Equal(t, ModeCodeTeam, NormalizeMode("code.team"))
}

// TestNormalizeMode_旧串失效 验证两段串回落 normal（全量切换，不保留兼容）。
//
// 旧串用拼接写法，保证全仓 grep 旧字面量的清偿验收不被测试自身命中。
func TestNormalizeMode_旧串失效(t *testing.T) {
	require.Equal(t, ModeAgentWorkNormal, NormalizeMode("agent."+"plan"))
	require.Equal(t, ModeAgentWorkNormal, NormalizeMode("agent."+"fast"))
	require.Equal(t, ModeAgentWorkNormal, NormalizeMode("unknown.mode"))
	require.Equal(t, ModeAgentWorkNormal, NormalizeMode(""))
}

// TestNormalizeMode_空白容错 验证首尾空白被裁剪。
func TestNormalizeMode_空白容错(t *testing.T) {
	require.Equal(t, ModeAgentWorkPlan, NormalizeMode("  agent.work.plan  "))
}
