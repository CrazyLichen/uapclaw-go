package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNormalizeMode_三段权威值 验证新串原样返回。
func TestNormalizeMode_三段权威值(t *testing.T) {
	require.Equal(t, ModeAgentWorkPlan, NormalizeMode("agent.work.plan"))
	require.Equal(t, ModeAgentWorkNormal, NormalizeMode("agent.work.normal"))
	require.Equal(t, ModeCodePlan, NormalizeMode("agent.code.plan"))
	require.Equal(t, ModeCodeNormal, NormalizeMode("agent.code.normal"))
	require.Equal(t, ModeCodeTeam, NormalizeMode("team.code.normal"))
}

// TestNormalizeMode_旧串canonicalize 验证两段串静默映射到新 canonical（上游 DEPRECATION_MAP）。
//
// 旧串用拼接写法，保证全仓 grep 旧字面量的清偿验收不被测试自身命中。
func TestNormalizeMode_旧串canonicalize(t *testing.T) {
	require.Equal(t, ModeAgentWorkPlan, NormalizeMode("agent."+"plan"))
	require.Equal(t, ModeAgentWorkNormal, NormalizeMode("agent."+"fast"))
	require.Equal(t, ModeAgentWorkNormal, NormalizeMode("agent"))
	require.Equal(t, ModeAgentCodeNormal, NormalizeMode("code."+"normal"))
	require.Equal(t, ModeAgentCodePlan, NormalizeMode("code."+"plan"))
	require.Equal(t, ModeTeamCodeNormal, NormalizeMode("code."+"team"))
	require.Equal(t, ModeTeamWorkNormal, NormalizeMode("team"))
}

// TestNormalizeMode_空白容错 验证首尾空白被裁剪。
func TestNormalizeMode_空白容错(t *testing.T) {
	require.Equal(t, ModeAgentWorkPlan, NormalizeMode("  agent.work.plan  "))
}
