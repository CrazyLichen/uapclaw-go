package core

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func testDeployment(id string) *Deployment {
	d, err := NewDeployment(DeploymentParams{ModelName: "m", APIKey: "k", APIBase: "u", ID: id, Provider: "openai"})
	require.NoError(t, err)
	return d
}

// TestRoutingContext_MarkAttempt 验证尝试链追加（py:50）。
func TestRoutingContext_MarkAttempt(t *testing.T) {
	ctx := NewRoutingContext("m", nil, nil)
	ctx.MarkAttempt(testDeployment("d1"))
	ctx.MarkAttempt(testDeployment("d2"))
	require.Equal(t, []string{"d1", "d2"}, ctx.AttemptedDeployments)
}

// TestRoutingContext_SetSuccessFailure 验证成功/失败标记与延迟（py:54/60/64）。
func TestRoutingContext_SetSuccessFailure(t *testing.T) {
	ctx := NewRoutingContext("m", nil, nil)
	require.Equal(t, 0.0, ctx.Latency()) // end_time 为 nil → 0.0（py:66-68）
	resp := map[string]any{"ok": true}
	ctx.SetSuccess(testDeployment("d1"), resp)
	require.Same(t, ctx.SelectedDeployment, ctx.SelectedDeployment)
	require.Equal(t, "d1", ctx.SelectedDeployment.ID)
	require.Equal(t, resp, ctx.Response)
	require.NotNil(t, ctx.EndTime)
	require.GreaterOrEqual(t, ctx.Latency(), 0.0)

	err := errors.New("x")
	ctx.SetFailure(err)
	require.Same(t, err, ctx.Error)
	require.NotNil(t, ctx.EndTime)
}

// TestRoutingContext_ToLogDict 验证日志字典字段（py:70-78）。
func TestRoutingContext_ToLogDict(t *testing.T) {
	ctx := NewRoutingContext("m", nil, nil)
	ctx.MarkAttempt(testDeployment("d1"))
	ctx.SetSuccess(testDeployment("d2"), nil)
	d := ctx.ToLogDict()
	require.Equal(t, "m", d["model"])
	require.Equal(t, "d2", d["selected_deployment"])
	require.Equal(t, []string{"d1"}, d["attempted"])
	require.Equal(t, true, d["success"])
}

// TestRoutingContext_ToLogDict_失败 验证 success=False 与 selected 为 nil。
func TestRoutingContext_ToLogDict_失败(t *testing.T) {
	ctx := NewRoutingContext("m", nil, map[string]any{"k": 1})
	ctx.SetFailure(errors.New("boom"))
	require.Equal(t, nil, ctx.ToLogDict()["selected_deployment"])
	require.Equal(t, false, ctx.ToLogDict()["success"])
	require.Equal(t, map[string]any{"k": 1}, ctx.Kwargs) // kwargs nil → 空 dict（py:29）
}
