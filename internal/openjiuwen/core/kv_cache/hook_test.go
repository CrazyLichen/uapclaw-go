package kv_cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// affinityLLMStub 满足 AffinityLLM 窄接口，记录 BuildInvokeKwargs 是否被调。
type affinityLLMStub struct {
	affinity bool
	building bool
}

func (l *affinityLLMStub) SupportsKVCacheAffinity() bool { return l.affinity }
func (l *affinityLLMStub) BuildKVCacheAffinityInvokeKwargs(
	sess any, sessionID, parentSessionID string, turnNum int,
) map[string]any {
	l.building = true
	if !l.affinity {
		return map[string]any{}
	}
	return map[string]any{
		"session_id":               sessionID,
		"parent_session_id":        parentSessionID,
		"turn_num":                 turnNum,
		"enable_kv_cache_affinity": true,
	}
}

// turnNumStub 满足 TurnNumSession 窄接口。
// Python: resolve_turn_num 里 session.get_state/update_state 被 try/except 全吞，
// 故 Go 窄接口不带 error（用户决策「无 error」）。
type turnNumStub struct{ state map[string]any }

func (s *turnNumStub) GetState(key string) any { return s.state[key] }
func (s *turnNumStub) UpdateState(d map[string]any) {
	for k, v := range d {
		s.state[k] = v
	}
}

// lineageAndTurnStub 同时满足 LineageSession 与 TurnNumSession。
type lineageAndTurnStub struct {
	turnNumStub
	id, cacheID, parentCacheID, parentSID string
	env                                   map[string]any
}

func (s *lineageAndTurnStub) GetSessionID() string { return s.id }
func (s *lineageAndTurnStub) GetCacheIdentity() KVCacheIdentity {
	return KVCacheIdentity{CacheID: s.cacheID, ParentCacheID: s.parentCacheID}
}
func (s *lineageAndTurnStub) GetParentSessionID() string { return s.parentSID }
func (s *lineageAndTurnStub) GetEnv(k string, _ ...any) any {
	return s.env[k]
}

// TestResolveRuntime_开关两态 对齐 hook.py:62-80。
func TestResolveRuntime_开关两态(t *testing.T) {
	h := NewKVCacheModelCallHook()
	r := h.ResolveRuntime(&affinityLLMStub{affinity: true}, &KVCacheAffinityConfig{EnableKVCacheAffinity: true})
	assert.True(t, r.EnableAffinity)
	assert.True(t, r.SupportsAffinity)

	r = h.ResolveRuntime(&affinityLLMStub{affinity: true}, nil)
	assert.False(t, r.EnableAffinity, "config 为 None 时 Python 用 KVCacheAffinityConfig() 默认 false")
	assert.False(t, r.SupportsAffinity, "开关关闭时不探测 provider")

	r = h.ResolveRuntime(&affinityLLMStub{affinity: false}, &KVCacheAffinityConfig{EnableKVCacheAffinity: true})
	assert.True(t, r.EnableAffinity)
	assert.False(t, r.SupportsAffinity, "provider 不支持时只记一次性 Warn")
}

// TestResolveLineage_开关关闭时不读会话 对齐 hook.py:83-92「return None, None」。
func TestResolveLineage_开关关闭时不读会话(t *testing.T) {
	h := NewKVCacheModelCallHook()
	sess := &lineageAndTurnStub{turnNumStub: turnNumStub{state: map[string]any{}},
		id: "raw", cacheID: "cid"}
	r := KVCacheCallCapabilities{EnableAffinity: false, SupportsAffinity: false}
	sid, parent := h.ResolveLineage(r, sess, "fb")
	assert.Equal(t, "", sid)
	assert.Equal(t, "", parent)
}

// TestResolveLineage_开启走血缘与fallback 对齐 hook.py:83-92。
func TestResolveLineage_开启走血缘与fallback(t *testing.T) {
	h := NewKVCacheModelCallHook()
	r := KVCacheCallCapabilities{EnableAffinity: true, SupportsAffinity: true}

	sess := &lineageAndTurnStub{turnNumStub: turnNumStub{state: map[string]any{}},
		id: "raw", cacheID: "cid", parentCacheID: "pid"}
	sid, parent := h.ResolveLineage(r, sess, "fb")
	assert.Equal(t, "cid", sid)
	assert.Equal(t, "pid", parent)

	// 无 identity 时 resolve_session_lineage 返回空 → fallback 到 fallback_session_id 双字段
	empty := &lineageAndTurnStub{turnNumStub: turnNumStub{state: map[string]any{}}}
	sid, parent = h.ResolveLineage(r, empty, "fb")
	assert.Equal(t, "fb", sid)
	assert.Equal(t, "fb", parent)
}

// TestResolveTurnNum_三路优先级与state回写 对齐 hook.py:33-59。
func TestResolveTurnNum_三路优先级与state回写(t *testing.T) {
	h := NewKVCacheModelCallHook()
	sess := &turnNumStub{state: map[string]any{}}

	assert.Equal(t, 7, h.ResolveTurnNum(map[string]any{"_turn_number": 7}, sess),
		"host 提供真实轮次优先")
	assert.Equal(t, 5, h.ResolveTurnNum(map[string]any{"_kv_cache_turn_num": 5}, sess))

	extra := map[string]any{}
	assert.Equal(t, 1, h.ResolveTurnNum(extra, sess), "无 host 值时从 state 自增")
	assert.Equal(t, 1, sess.state[KVCacheTurnNumberStateKey])
	assert.Equal(t, 1, extra["_kv_cache_turn_num"], "回写 callback extra")
	// Python 语义（已用上游源码复刻验证）：同一个 extra 再调会命中
	// 第 1 步的 _kv_cache_turn_num 优先分支，仍返回 1；换新 extra 才走 state 自增。
	assert.Equal(t, 1, h.ResolveTurnNum(extra, sess), "同 extra 命中自身回写值")
	assert.Equal(t, 2, h.ResolveTurnNum(map[string]any{}, sess), "新 extra 从 state 自增到 2")

	// Python: bool 是 int 子类，但显式排除；Go 侧 bool 不会命中 int 断言
	assert.Equal(t, 3, h.ResolveTurnNum(map[string]any{"_turn_number": true}, sess))
}

// TestBuildInvokeKwargs_开关两态 对齐 hook.py:119-140。
func TestBuildInvokeKwargs_开关两态(t *testing.T) {
	h := NewKVCacheModelCallHook()
	llm := &affinityLLMStub{affinity: true}
	r := KVCacheCallCapabilities{EnableAffinity: true, SupportsAffinity: true}
	got := h.BuildInvokeKwargs(r, llm, nil, "sid", "pid", 3)
	assert.Equal(t, "sid", got["session_id"])
	assert.Equal(t, "pid", got["parent_session_id"])
	assert.Equal(t, 3, got["turn_num"])
	assert.Equal(t, true, got["enable_kv_cache_affinity"])
	assert.True(t, llm.building)

	llm2 := &affinityLLMStub{affinity: true}
	assert.Empty(t, h.BuildInvokeKwargs(KVCacheCallCapabilities{}, llm2, nil, "sid", "pid", 3),
		"关闭时返回空 map")
	assert.False(t, llm2.building, "关闭时不调 provider 的 build_affinity")
}

// TestHandleContextWindowChange_开关门卫 对齐 hook.py:95-117（releaseFn 无 error）。
func TestHandleContextWindowChange_开关门卫(t *testing.T) {
	h := NewKVCacheModelCallHook()
	called := false
	release := func() { called = true }
	h.HandleContextWindowChange(release, KVCacheCallCapabilities{})
	assert.False(t, called, "关闭时不调释放")
	h.HandleContextWindowChange(release, KVCacheCallCapabilities{EnableAffinity: true, SupportsAffinity: true})
	assert.True(t, called, "开启时调用释放")
}

// TestResetWarnings 对齐 hook.py:29-30。
func TestResetWarnings(t *testing.T) {
	h := NewKVCacheModelCallHook()
	h.ResolveRuntime(&affinityLLMStub{affinity: false}, &KVCacheAffinityConfig{EnableKVCacheAffinity: true})
	h.ResetWarnings()
	r := h.ResolveRuntime(&affinityLLMStub{affinity: false}, &KVCacheAffinityConfig{EnableKVCacheAffinity: true})
	assert.False(t, r.SupportsAffinity)
}
