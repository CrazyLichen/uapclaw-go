package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// kvCacheTestSession 实现 SessionFacade 中本测例用到的能力。
type kvCacheTestSession struct {
	id, parentID string
	cacheID      string
	parentCache  string
	env          map[string]any
	state        map[string]any
}

func (s *kvCacheTestSession) GetSessionID() string { return s.id }
func (s *kvCacheTestSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{CacheID: s.cacheID, ParentCacheID: s.parentCache}
}
func (s *kvCacheTestSession) GetParentSessionID() string { return s.parentID }
func (s *kvCacheTestSession) GetEnv(k string, _ ...any) any {
	return s.env[k]
}
func (s *kvCacheTestSession) UpdateState(d map[string]any) {
	for k, v := range d {
		s.state[k] = v
	}
}
func (s *kvCacheTestSession) GetState(k state.StateKey) (any, error) {
	_ = k
	return nil, nil
}
func (s *kvCacheTestSession) DumpState() map[string]any                        { return s.state }
func (s *kvCacheTestSession) WriteStream(_ context.Context, _ any) error       { return nil }
func (s *kvCacheTestSession) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (s *kvCacheTestSession) Interact(_ context.Context, _ any) error          { return nil }
func (s *kvCacheTestSession) ClearSession(_ context.Context)                   {}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestKVCacheSessionAdapter_实现两个窄接口 编译期断言。
func TestKVCacheSessionAdapter_实现两个窄接口(t *testing.T) {
	var sess *kvCacheTestSession
	_ = sess
	adapter := newKVCacheSessionAdapter(nil)
	assert.Nil(t, adapter, "nil 门面返回 nil，kv_cache 侧容忍")

	var _ kv_cache.LineageSession = (*kvCacheSessionAdapter)(nil)
	var _ kv_cache.TurnNumSession = (*kvCacheSessionAdapter)(nil)
}

// TestKVFiveSteps_开关两态 验证主调用点四步（第④步由 KVCacheManager 承担）。
func TestKVFiveSteps_开关两态(t *testing.T) {
	hook := kv_cache.NewKVCacheModelCallHook()

	t.Run("关闭时四字段都不出现", func(t *testing.T) {
		runtime := hook.ResolveRuntime(&modelAffinityStub{affinity: true}, nil)
		assert.False(t, runtime.EnableAffinity)
		kw := hook.BuildInvokeKwargs(runtime, &modelAffinityStub{affinity: true}, nil, "sid", "pid", 1)
		assert.Empty(t, kw)
	})

	t.Run("开启时四字段齐全且 turn_num 为正", func(t *testing.T) {
		llmStub := &modelAffinityStub{affinity: true}
		runtime := hook.ResolveRuntime(llmStub, &kv_cache.KVCacheAffinityConfig{EnableKVCacheAffinity: true})
		require.True(t, runtime.EnableAffinity)
		require.True(t, runtime.SupportsAffinity)

		sess := &kvCacheTestSession{id: "s1", cacheID: "cid", parentCache: "pid",
			env: map[string]any{}, state: map[string]any{}}
		adapter := newKVCacheSessionAdapter(sess)

		sid, parent := hook.ResolveLineage(runtime, adapter, "fallback")
		assert.Equal(t, "cid", sid)
		assert.Equal(t, "pid", parent)

		turn := hook.ResolveTurnNum(map[string]any{}, adapter)
		assert.Equal(t, 1, turn)

		kw := hook.BuildInvokeKwargs(runtime, llmStub, adapter, sid, parent, turn)
		assert.Equal(t, "cid", kw["session_id"])
		assert.Equal(t, "pid", kw["parent_session_id"])
		assert.Equal(t, 1, kw["turn_num"])
		assert.Equal(t, true, kw["enable_kv_cache_affinity"])
	})
}

// modelAffinityStub 满足 kv_cache.AffinityLLM。
type modelAffinityStub struct{ affinity bool }

func (m *modelAffinityStub) SupportsKVCacheAffinity() bool { return m.affinity }
func (m *modelAffinityStub) BuildKVCacheAffinityInvokeKwargs(
	_ any, sessionID, parentSessionID string, turnNum int,
) map[string]any {
	if !m.affinity {
		return map[string]any{}
	}
	return map[string]any{
		"session_id":               sessionID,
		"parent_session_id":        parentSessionID,
		"turn_num":                 turnNum,
		"enable_kv_cache_affinity": true,
	}
}

var _ sessioninterfaces.SessionFacade = (*kvCacheTestSession)(nil)
