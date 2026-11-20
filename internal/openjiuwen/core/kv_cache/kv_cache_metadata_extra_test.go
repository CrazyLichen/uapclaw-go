package kv_cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lineageStub 满足 ResolveSessionLineage 需要的最小会话能力。
// Python: resolve_session_lineage(session: Any) 用 getattr 逐个探测方法。
type lineageStub struct {
	id, cacheID, parentCacheID, parentSID string
	env                                   map[string]any
	hasGetEnv, hasParent                  bool
}

func (s *lineageStub) GetSessionID() string { return s.id }
func (s *lineageStub) GetCacheIdentity() KVCacheIdentity {
	return KVCacheIdentity{CacheID: s.cacheID, ParentCacheID: s.parentCacheID}
}
func (s *lineageStub) GetParentSessionID() string { return s.parentSID }
func (s *lineageStub) GetEnv(key string, _ ...any) any {
	if !s.hasGetEnv {
		return nil
	}
	return s.env[key]
}

// TestContextCompressorCacheIdentity_两分支
// Python: kv_cache_metadata.py:23-38（两处 raise ValueError → 保留 error）。
func TestContextCompressorCacheIdentity_两分支(t *testing.T) {
	id, err := ContextCompressorCacheIdentity("owner", "RoundLevelCompressor")
	require.NoError(t, err)
	assert.Equal(t, "owner:compressor:round-level", id.CacheID)
	assert.Equal(t, "owner", id.ParentCacheID)

	id, err = ContextCompressorCacheIdentity("  owner  ", "CurrentRoundCompressor")
	require.NoError(t, err)
	assert.Equal(t, "owner:compressor:current-round", id.CacheID, "owner 需 strip")

	_, err = ContextCompressorCacheIdentity("", "DialogueCompressor")
	assert.Error(t, err, "owner_cache_id is required")
	_, err = ContextCompressorCacheIdentity("owner", "Unknown")
	assert.Error(t, err, "unsupported context compressor type")
}

// TestSelfParentKwargs 对齐 kv_cache_metadata.py:41-42。
func TestSelfParentKwargs(t *testing.T) {
	assert.Equal(t, map[string]any{"session_id": "s1", "parent_session_id": "s1"},
		SelfParentKwargs("s1"))
}

// TestResolveSessionLineage_五分支 逐条对齐 kv_cache_metadata.py:56-86。
// ★ 严格对齐：Python 单参数，Go 不许加 fallbackSessionID。
func TestResolveSessionLineage_五分支(t *testing.T) {
	cases := []struct {
		name       string
		sess       *lineageStub
		wantSID    string
		wantParent string
	}{
		{"cache_identity 优先", &lineageStub{id: "raw", cacheID: "cid", parentCacheID: "pid"}, "cid", "pid"},
		{"identity 无 parent 时自继承", &lineageStub{id: "raw", cacheID: "cid"}, "cid", "cid"},
		{"无 identity 走 env 双键", &lineageStub{id: "raw", hasGetEnv: true, env: map[string]any{
			KVCacheAffinitySessionIDEnv: "envsid", KVCacheAffinityParentSessionIDEnv: "envpid"}}, "envsid", "envpid"},
		{"env 无 parent 走 get_parent_session_id", &lineageStub{id: "raw", hasGetEnv: true,
			env: map[string]any{KVCacheAffinitySessionIDEnv: "envsid"}, parentSID: "psid"}, "envsid", "psid"},
		{"全空回退 GetSessionID 自继承", &lineageStub{id: "raw"}, "raw", "raw"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sid, parent := ResolveSessionLineage(c.sess)
			assert.Equal(t, c.wantSID, sid)
			assert.Equal(t, c.wantParent, parent)
		})
	}
	sid, parent := ResolveSessionLineage(nil)
	assert.Equal(t, "", sid, "Python 返回 (None, None)")
	assert.Equal(t, "", parent)
}

// TestFirstChangedIndex 对齐 kv_cache_metadata.py:88-95（泛型逐元素比较）。
func TestFirstChangedIndex(t *testing.T) {
	assert.Equal(t, 1, *FirstChangedIndex([]int{1, 2, 3}, []int{1, 9, 3}))
	assert.Nil(t, FirstChangedIndex([]int{1, 2}, []int{1, 2, 3}), "纯追加返回 nil")
	assert.Equal(t, 2, *FirstChangedIndex([]int{1, 2, 3}, []int{1, 2}), "变短返回新长度")
	assert.Nil(t, FirstChangedIndex([]string{}, []string{}), "两空序列返回 nil")
}

// TestRangeKwargs 对齐 kv_cache_metadata.py:98-103。
func TestRangeKwargs(t *testing.T) {
	assert.Equal(t, map[string]any{"msg_start": 1, "msg_end": 3}, MessageRangeKwargs(1, 3))
	assert.Equal(t, map[string]any{"tools_start": 0, "tools_end": 2}, ToolsRangeKwargs(0, 2))
}
