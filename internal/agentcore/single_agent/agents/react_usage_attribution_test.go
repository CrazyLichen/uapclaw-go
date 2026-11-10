package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// attributionSession 带归属能力的会话夹具（来源元数据/团队/血缘/缓存身份）。
type attributionSession struct {
	// testSessionFacade 基础会话面
	*testSessionFacade
	// sourceMetadata 来源元数据
	sourceMetadata map[string]any
	// teamID 团队标识
	teamID string
	// parentSessionID 产品侧父会话
	parentSessionID string
	// cacheID 缓存身份
	cacheID string
	// parentCacheID 父级缓存身份
	parentCacheID string
	// env 环境变量
	env map[string]any
}

// GetSourceMetadata 返回来源元数据
func (s *attributionSession) GetSourceMetadata() map[string]any { return s.sourceMetadata }

// GetTeamID 返回团队标识
func (s *attributionSession) GetTeamID() string { return s.teamID }

// GetParentSessionID 返回父会话
func (s *attributionSession) GetParentSessionID() string { return s.parentSessionID }

// GetCacheIdentity 返回缓存身份
func (s *attributionSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{CacheID: s.cacheID, ParentCacheID: s.parentCacheID}
}

// GetEnv 返回环境变量
func (s *attributionSession) GetEnv(key string, _ ...any) any { return s.env[key] }

// buildContextStub 成员身份来源夹具。
type buildContextStub struct {
	// memberName 成员名
	memberName string
}

// GetMemberName 返回成员名
func (b *buildContextStub) GetMemberName() string { return b.memberName }

// ──────────────────────────── 非导出函数 ────────────────────────────

// newAttributionAgent 构造只带身份卡片的最小 ReActAgent。
func newAttributionAgent(t *testing.T, agentID string) *ReActAgent {
	t.Helper()
	card := agentschema.NewAgentCard(agentschema.WithAgentName("attr-agent"))
	card.ID = agentID
	return NewReActAgent(card, nil)
}

// ──────────────────────────── 测试 ────────────────────────────

// TestResolveContextUsageAttribution_无会话兜底 验证只有 agent 自身信息时的归属。
// Python: _context_usage_attribution 的能力探测兜底（react_agent.py L1326-1418）
func TestResolveContextUsageAttribution_无会话兜底(t *testing.T) {
	a := newAttributionAgent(t, "agent-1")
	cbc := interfaces.NewAgentCallbackContext(a, &interfaces.InvokeInputs{}, nil)

	got := a.resolveContextUsageAttribution(cbc)

	assert.Equal(t, "agent-1", got["agent_id"], "agent id 回退到 card.id")
	assert.Equal(t, "default", got["execution_session_id"], "无会话时执行会话 id 为 default")
	assert.Equal(t, "default", got["product_session_id"])
	assert.Equal(t, "default", got["cache_identity"])
	assert.Equal(t, "default", got["parent_cache_identity"])
	assert.Equal(t, "provider", got["cache_mode"])
	assert.Equal(t, "session", got["cache_scope"])
	assert.Equal(t, []string{"agent-1"}, got["agent_path"])
	assert.Equal(t, 0, got["depth"])
	assert.Equal(t, "agent-1|default|default", got["context_owner_id"])
}

// TestResolveContextUsageAttribution_来源元数据与构建上下文 验证团队/成员来源。
func TestResolveContextUsageAttribution_来源元数据与构建上下文(t *testing.T) {
	a := newAttributionAgent(t, "agent-2")
	a.SetUsageBuildContext(&buildContextStub{memberName: "worker"})
	sess := &attributionSession{
		testSessionFacade: &testSessionFacade{id: "sess-2"},
		sourceMetadata:    map[string]any{"source_team_id": "team-9", "source_member_name": "leader"},
	}
	cbc := interfaces.NewAgentCallbackContext(a, &interfaces.InvokeInputs{}, sess)

	got := a.resolveContextUsageAttribution(cbc)

	assert.Equal(t, "team-9", got["team_id"], "团队标识取自来源元数据")
	assert.Equal(t, "leader", got["member_name"], "来源元数据优先于构建上下文")
	assert.Equal(t, "sess-2", got["execution_session_id"])
	assert.Equal(t, "leader", got["agent_path"].([]string)[0])
}

// TestResolveContextUsageAttribution_成员名四级回退 验证第四级
// `getattr(self, "member_name", None)` 在前三级都缺失时仍是归属来源。
// Python: react_agent.py L1367-1371
func TestResolveContextUsageAttribution_成员名四级回退(t *testing.T) {
	a := newAttributionAgent(t, "agent-4")
	// 前三级：ctx 归属 / source_metadata / build_context 全部为空，
	// 只挂上 agent 自身的 member_name（对应 Python 宿主动态挂属性）。
	a.usageMemberName = "host-member"
	cbc := interfaces.NewAgentCallbackContext(a, &interfaces.InvokeInputs{}, nil)

	got := a.resolveContextUsageAttribution(cbc)

	assert.Equal(t, "host-member", got["member_name"], "第四级成员名回退生效")
	assert.Equal(t, []string{"host-member"}, got["agent_path"], "路径用成员名兜底")
	// Python: owner_parts = [team_id, member_name or agent_id or "agent", ...]
	// 无团队时 team_id 为空片段被过滤，故不含 agent-4 前缀。
	assert.Equal(t, "host-member|default|default", got["context_owner_id"])
}

// TestResolveContextUsageAttribution_归属值优先 验证 ctx 归属覆盖会话推断。
func TestResolveContextUsageAttribution_归属值优先(t *testing.T) {
	a := newAttributionAgent(t, "agent-3")
	a.SetUsageBuildContext(&buildContextStub{memberName: "worker"})
	sess := &attributionSession{
		testSessionFacade: &testSessionFacade{id: "sess-3"},
		sourceMetadata:    map[string]any{"source_team_id": "team-x", "source_member_name": "leader"},
	}
	cbc := interfaces.NewAgentCallbackContext(a, &interfaces.InvokeInputs{}, sess)
	cbc.SetContextUsageAttribution(map[string]any{
		"agent_id":    "agent-override",
		"team_id":     "team-override",
		"member_name": "member-override",
		"agent_path":  []string{"root", "member-override"},
		"depth":       1,
	})

	got := a.resolveContextUsageAttribution(cbc)

	assert.Equal(t, "agent-override", got["agent_id"])
	assert.Equal(t, "team-override", got["team_id"])
	assert.Equal(t, "member-override", got["member_name"])
	assert.Equal(t, []string{"root", "member-override"}, got["agent_path"])
	assert.Equal(t, 1, got["depth"])
}

// TestResolveContextUsageAttribution_缓存身份与父会话 验证缓存/血缘能力。
func TestResolveContextUsageAttribution_缓存身份与父会话(t *testing.T) {
	a := newAttributionAgent(t, "agent-4")
	sess := &attributionSession{
		testSessionFacade: &testSessionFacade{id: "sess-4"},
		teamID:            "team-4",
		parentSessionID:   "parent-4",
		cacheID:           "cache-4",
		parentCacheID:     "parent-cache-4",
	}
	cbc := interfaces.NewAgentCallbackContext(a, &interfaces.InvokeInputs{}, sess)

	got := a.resolveContextUsageAttribution(cbc)

	assert.Equal(t, "team-4", got["team_id"], "无来源元数据时回退到会话能力")
	// parent_session_id 只用于推导 product_session_id，不单独回填（对齐 Python 的
	// return {**values, ...}——父会话键只在 values 里出现过才回写）。
	assert.Equal(t, "parent-4", got["product_session_id"], "产品会话优先用父会话")
	assert.Equal(t, "cache-4", got["cache_identity"])
	assert.Equal(t, "parent-cache-4", got["parent_cache_identity"])
}

// TestResolveContextUsageAttribution_环境变量兜底父会话 验证 env 兜底链。
// Python: session.get_env(KV_CACHE_AFFINITY_PARENT_SESSION_ID_ENV)（react_agent.py L1371-1380）
func TestResolveContextUsageAttribution_环境变量兜底父会话(t *testing.T) {
	a := newAttributionAgent(t, "agent-5")
	sess := &attributionSession{
		testSessionFacade: &testSessionFacade{id: "sess-5"},
		env:               map[string]any{kvCacheAffinityParentSessionIDEnv: " parent-env "},
	}
	cbc := interfaces.NewAgentCallbackContext(a, &interfaces.InvokeInputs{}, sess)

	got := a.resolveContextUsageAttribution(cbc)

	require.Equal(t, "parent-env", got["product_session_id"], "env 兜底出的父会话参与产品会话推导")
}
