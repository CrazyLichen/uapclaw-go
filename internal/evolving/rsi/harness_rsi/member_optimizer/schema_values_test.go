package memberoptimizer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

)

func TestSchema值清单_排除视图(t *testing.T) {
	// Python: mechanism_attributor.py:17-19 三个排除视图
	mt := MechanismTypeValuesExcludingRail()
	require.NotContains(t, mt, "rail")
	require.Contains(t, mt, "insufficient_role_evidence")
	fs := FailureSignatureValuesExcludingConfigOrRailMismatch()
	require.NotContains(t, fs, "config_or_rail_mismatch")
	require.Contains(t, fs, "insufficient_role_evidence")
	os_ := OptimizationSurfaceValuesExcludingRail()
	require.NotContains(t, os_, "rail")
	require.Contains(t, os_, "none")
}

func TestBridge快照还原(t *testing.T) {
	// 测试进程未 import agents 子包（blank import 成环），桥为零值；
	// 仅验证快照/替换/还原机制本身（真实桥注册由集成测试覆盖）。
	saved := SnapshotAgentsBridge()
	probe := AgentsBridge{}
	probe.InvokeStructured = func(context.Context, StructuredInvokeParams) (map[string]any, error) {
		return map[string]any{}, nil
	}
	ReplaceAgentsBridge(probe)
	require.NotNil(t, agentsBridge.InvokeStructured)
	RestoreAgentsBridge(saved)
	require.Nil(t, agentsBridge.InvokeStructured) // 还原为快照（本测试进程为零值桥）
}

func TestPyFloatHelpers(t *testing.T) {
	require.Equal(t, 0.5, pyMinFloat(0.5, 0.9))
	require.Equal(t, 0.9, pyMaxFloat(0.5, 0.9))
}

func TestValidateTeamModelConfig_导出包装(t *testing.T) {
	cfg, err := ValidateTeamModelConfig(map[string]any{
		"model_client_config": map[string]any{"client_id": "c", "client_provider": "llm_OpenAI", "model_name": "m"},
	})
	require.NoError(t, err)
	require.Equal(t, "c", cfg.ModelClientConfig.ClientID)
	_, err = ValidateTeamModelConfig(map[string]any{"model_client_config": "bad"})
	require.Error(t, err)
}

func TestValidateClientRequestConfigJSON(t *testing.T) {
	client, err := ValidateClientConfigJSON(map[string]any{
		"client_id": "c", "client_provider": "llm_OpenAI", "model_name": "m", "api_key": "k", "base_url": "http://x",
	})
	require.NoError(t, err)
	require.Equal(t, "c", client.ClientID)
	request, err := ValidateRequestConfigJSON(map[string]any{"max_tokens": 100})
	require.NoError(t, err)
	require.NotNil(t, request)
}

func TestPyJSONNumber_浮点格式(t *testing.T) {
	require.Equal(t, "1", pyJSONNumber(1.0))
	require.Equal(t, "1.5", pyJSONNumber(1.5))
}

func TestLoader边界助手_轨迹窗口(t *testing.T) {
	// boundTrajectoryWindowSummary / boundNormalizedTraceSummary / boundNormalizedToolCalls
	// Python loader.py:972-1064 有界化语义：超限截断
	window := boundTrajectoryWindowSummary(map[string]any{
		"failure_signatures": []any{"sig1", "sig2"},
		"recent_events": []any{
			map[string]any{"event_type": "e", "summary": "s"},
			"not-a-dict",
		},
	})
	require.NotNil(t, window)
	normalized := boundNormalizedTraceSummary(map[string]any{
		"traces": []any{map[string]any{
			"trace_id": "t1", "member_role": "solver",
			"messages": []any{map[string]any{"message_index": 1, "tool_calls": []any{map[string]any{"name": "n", "error": "e", "step_pointer": "sp"}}}},
		}},
	})
	require.NotNil(t, normalized)
	calls := boundNormalizedToolCalls([]any{map[string]any{"name": "n", "error": "e"}})
	require.NotNil(t, calls)
	require.Empty(t, boundNormalizedToolCalls([]any{"bad"}))
}
