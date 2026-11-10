package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
)

// TestSession_ParentSessionID绑定 验证血缘绑定语义：空值忽略、同值幂等、异值报错。
// Python: Session.bind_parent_session_id (core/session/agent.py L77-100)
func TestSession_ParentSessionID绑定(t *testing.T) {
	s := NewSession(WithSessionID("child-1"))

	assert.Empty(t, s.GetParentSessionID())

	require.NoError(t, s.BindParentSessionID("  "))
	assert.Empty(t, s.GetParentSessionID(), "空值不绑定")

	require.NoError(t, s.BindParentSessionID(" parent-1 "))
	assert.Equal(t, "parent-1", s.GetParentSessionID(), "首次绑定去空白")

	require.NoError(t, s.BindParentSessionID("parent-1"))
	assert.Equal(t, "parent-1", s.GetParentSessionID(), "同值幂等")

	assert.Error(t, s.BindParentSessionID("parent-2"), "改绑必须报错")
	assert.Equal(t, "parent-1", s.GetParentSessionID(), "改绑失败不改变原值")
}

// TestSession_GetCacheIdentity_自指 验证无任何线索时会话自指。
// Python: Session.get_cache_identity 末分支 (core/session/agent.py L171-174)
func TestSession_GetCacheIdentity_自指(t *testing.T) {
	s := NewSession(WithSessionID("solo"))

	identity, err := s.GetCacheIdentity()
	require.NoError(t, err)
	assert.Equal(t, kv_cache.KVCacheIdentity{CacheID: "solo", ParentCacheID: "solo"}, identity)
}

// TestSession_GetCacheIdentity_团队作用域 验证团队成员的缓存身份与父级回落。
// Python: Session.get_cache_identity 首分支 (core/session/agent.py L135-145)
func TestSession_GetCacheIdentity_团队作用域(t *testing.T) {
	s := NewSession(WithSessionID("team-sess"))
	s.SetTeamCacheScope("team-1", "worker")

	identity, err := s.GetCacheIdentity()
	require.NoError(t, err)
	assert.Equal(t, "team:team-sess:team:team-1:member:worker", identity.CacheID)
	assert.Equal(t, "team-sess", identity.ParentCacheID, "无父会话时回落自身")

	require.NoError(t, s.BindParentSessionID("product-1"))
	identity, err = s.GetCacheIdentity()
	require.NoError(t, err)
	assert.Equal(t, "product-1", identity.ParentCacheID, "有父会话时用父会话")
}

// TestSession_GetCacheIdentity_来源元数据 验证团队来源元数据推出的成员身份。
// Python: Session.get_cache_identity 第二分支 (core/session/agent.py L145-155)
func TestSession_GetCacheIdentity_来源元数据(t *testing.T) {
	s := NewSession(
		WithSessionID("member-sess"),
		WithSourceMetadata(map[string]any{"source_team_id": "team-9", "source_agent_id": "worker"}),
	)

	identity, err := s.GetCacheIdentity()
	require.NoError(t, err)
	assert.Equal(t, "team:member-sess:team:team-9:member:worker", identity.CacheID)
	assert.Equal(t, "member-sess", identity.ParentCacheID, "来源元数据分支父级取自身")
}

// TestSession_GetCacheIdentity_环境变量 验证 env 指定缓存身份的兜底分支。
// Python: Session.get_cache_identity 第三分支 (core/session/agent.py L156-170)
func TestSession_GetCacheIdentity_环境变量(t *testing.T) {
	s := NewSession(
		WithSessionID("env-sess"),
		WithEnvs(map[string]any{
			kv_cache.KVCacheAffinitySessionIDEnv:       "cache-env",
			kv_cache.KVCacheAffinityParentSessionIDEnv: "parent-env",
		}),
	)

	identity, err := s.GetCacheIdentity()
	require.NoError(t, err)
	assert.Equal(t, "cache-env", identity.CacheID)
	assert.Equal(t, "parent-env", identity.ParentCacheID)
}

// TestAgentTeamSession_CreateAgentSession_成员身份 验证成员名与缓存作用域注入。
// Python: AgentTeamSession.create_agent_session (core/session/agent_team.py L164-188)
func TestAgentTeamSession_CreateAgentSession_成员身份(t *testing.T) {
	ts := NewAgentTeamSession(WithAgentTeamSessionID("team-sess"), WithAgentTeamTeamID("team-1"))

	child := ts.CreateAgentSession(nil, "worker", false, "worker-name")
	require.NotNil(t, child)

	meta := child.GetSourceMetadata()
	assert.Equal(t, "worker", meta["source_agent_id"])
	assert.Equal(t, "team-1", meta["source_team_id"])
	assert.Equal(t, "worker-name", meta["source_member_name"])

	identity, err := child.GetCacheIdentity()
	require.NoError(t, err)
	assert.Equal(t, "team:team-sess:team:team-1:member:worker", identity.CacheID,
		"子会话必须带团队缓存作用域")
}
