package rail

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUsageAttributionCtx_绑定与隔离 验证 ctx 派生不影响父 ctx。
// Python: ContextVar set/reset 语义（rail/base.py L71-118）
func TestUsageAttributionCtx_绑定与隔离(t *testing.T) {
	parent := context.Background()

	child := BindUsageAttribution(parent, map[string]any{"invocation_id": "inv-1"})
	child = BindUsageInvocationID(child, "inv-1")
	child = BindUsageDelegation(child, map[string]any{"delegation_id": "d-1"})

	assert.Equal(t, "inv-1", CurrentUsageInvocationID(child))
	assert.Equal(t, "inv-1", CurrentUsageAttribution(child)["invocation_id"])
	assert.Equal(t, "d-1", CurrentUsageDelegation(child)["delegation_id"])

	// 父 ctx 不受影响（等价 Python reset 后回到父绑定）
	assert.Empty(t, CurrentUsageInvocationID(parent))
	assert.Empty(t, CurrentUsageAttribution(parent))
	assert.Empty(t, CurrentUsageDelegation(parent))
}

// TestCurrentUsageAttribution_返回副本 验证读取方改不到内部字典。
func TestCurrentUsageAttribution_返回副本(t *testing.T) {
	ctx := BindUsageAttribution(context.Background(), map[string]any{"depth": 2})

	got := CurrentUsageAttribution(ctx)
	got["depth"] = 9

	assert.Equal(t, 2, CurrentUsageAttribution(ctx)["depth"])
}

// TestCurrentUsage_空ctx 验证 nil/空 ctx 的兜底行为。
func TestCurrentUsage_空ctx(t *testing.T) {
	//nolint:staticcheck // 显式验证 nil ctx 的兜底路径
	var nilCtx context.Context
	assert.Empty(t, CurrentUsageInvocationID(nilCtx))
	assert.Empty(t, CurrentUsageAttribution(nilCtx))
	assert.Empty(t, CurrentUsageDelegation(nilCtx))
}

// TestBuildUsageDelegationAttribution_字段与回退 验证委派归属各键与三级回退。
// Python: build_usage_delegation_attribution (rail/base.py L131-172)
func TestBuildUsageDelegationAttribution_字段与回退(t *testing.T) {
	parent := map[string]any{
		"invocation_id":        "inv-parent",
		"execution_session_id": "exec-parent",
		"agent_path":           []string{"root", "leader"},
		"depth":                1,
		"cache_identity":       "cache-parent",
		"cache_mode":           "provider",
		"cache_scope":          "session",
	}

	got := BuildUsageDelegationAttribution(context.Background(), "worker", "sess-parent", "deleg-1", parent, "")

	assert.Equal(t, "worker", got["agent_id"])
	assert.Equal(t, "sess-parent", got["parent_session_id"])
	assert.Equal(t, "deleg-1", got["delegation_id"])
	assert.Equal(t, "inv-parent", got["parent_invocation_id"], "父调用标识回退到父归属")
	assert.Equal(t, []string{"root", "leader", "worker"}, got["agent_path"])
	assert.Equal(t, 2, got["depth"], "有父路径时深度 +1")
	assert.Equal(t, "exec-parent", got["product_session_id"], "产品会话回退到父执行会话")
	assert.Equal(t, "cache-parent", got["parent_cache_identity"])
	assert.Equal(t, "provider", got["cache_mode"])
	assert.Equal(t, "session", got["cache_scope"])
}

// TestBuildUsageDelegationAttribution_无父归属 验证无父归属时的缺省值。
func TestBuildUsageDelegationAttribution_无父归属(t *testing.T) {
	ctx := BindUsageInvocationID(context.Background(), "inv-current")

	got := BuildUsageDelegationAttribution(ctx, "", "sess-1", "deleg-2", nil, "")

	assert.Nil(t, got["agent_id"], "agent_id 缺省保持 None 语义")
	assert.Equal(t, []string{"agent"}, got["agent_path"])
	assert.Equal(t, 0, got["depth"])
	assert.Equal(t, "inv-current", got["parent_invocation_id"], "回退到当前 ctx 的调用标识")
	assert.Equal(t, "sess-1", got["product_session_id"])
	assert.Equal(t, "provider", got["cache_mode"])
	assert.Equal(t, "session", got["cache_scope"])
}

// TestBuildUsageDelegationAttribution_显式父调用标识优先 验证入参优先于 ctx。
func TestBuildUsageDelegationAttribution_显式父调用标识优先(t *testing.T) {
	ctx := BindUsageInvocationID(context.Background(), "inv-current")

	got := BuildUsageDelegationAttribution(ctx, "worker", "sess-1", "deleg-3", nil, "inv-explicit")

	require.Equal(t, "inv-explicit", got["parent_invocation_id"])
}

// TestUpdateUsageAttribution_同链可见 验证原地刷新对同一 ctx 链的读取方立即可见。
// Python: ctx.context_usage_attribution.clear() + update()（react_agent.py L1478-1479）
func TestUpdateUsageAttribution_同链可见(t *testing.T) {
	ctx := BindUsageAttribution(context.Background(), map[string]any{"invocation_id": "inv-1"})

	UpdateUsageAttribution(ctx, map[string]any{
		"invocation_id": "inv-1",
		"team_id":       "team-1",
		"member_name":   "worker",
	})

	got := CurrentUsageAttribution(ctx)
	assert.Equal(t, "team-1", got["team_id"], "刷新后的键必须对同链可见")
	assert.Equal(t, "worker", got["member_name"])
}

// TestUpdateUsageAttribution_父ctx不受影响 验证刷新不跨链泄漏。
func TestUpdateUsageAttribution_父ctx不受影响(t *testing.T) {
	parent := context.Background()
	ctx := BindUsageAttribution(parent, map[string]any{"invocation_id": "inv-1"})

	UpdateUsageAttribution(ctx, map[string]any{"team_id": "team-1"})

	assert.Empty(t, CurrentUsageAttribution(parent), "父 ctx 不应看到子链刷新")
}

// TestUpdateUsageAttribution_未绑定为空操作 验证无绑定时不 panic。
func TestUpdateUsageAttribution_未绑定为空操作(t *testing.T) {
	UpdateUsageAttribution(context.Background(), map[string]any{"team_id": "team-1"})
	//nolint:staticcheck // 显式验证 nil ctx 的兜底路径
	UpdateUsageAttribution(nil, map[string]any{"team_id": "team-1"})
}
