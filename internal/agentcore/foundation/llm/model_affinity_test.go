package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestModelSupportsKVCacheAffinity_委托底层客户端
// Python: Model.supports_kv_cache_affinity() 委托 self._client
func TestModelSupportsKVCacheAffinity_委托底层客户端(t *testing.T) {
	m := &Model{client: &mockModelClient{affinitySupported: true}}
	assert.True(t, m.SupportsKVCacheAffinity())

	m2 := &Model{client: &mockModelClient{affinitySupported: false}}
	assert.False(t, m2.SupportsKVCacheAffinity())
}

// TestModelBuildKVCacheAffinityInvokeKwargs_透传参数
// Python: Model.build_kv_cache_affinity_invoke_kwargs()
func TestModelBuildKVCacheAffinityInvokeKwargs_透传参数(t *testing.T) {
	c := &mockModelClient{affinitySupported: true}
	m := &Model{client: c}
	got := m.BuildKVCacheAffinityInvokeKwargs(nil, "sid", "pid", 3)
	assert.Equal(t, "sid", got["session_id"])
	assert.Equal(t, "pid", got["parent_session_id"])
	assert.Equal(t, 3, got["turn_num"])
	assert.Equal(t, true, got["enable_kv_cache_affinity"])
	assert.Equal(t, "sid|pid", c.affinityArgs, "参数必须原样透传，否则父子 cache key 失配")
}

// TestModelBuildKVCacheAffinityInvokeKwargs_不支持时为空
func TestModelBuildKVCacheAffinityInvokeKwargs_不支持时为空(t *testing.T) {
	m := &Model{client: &mockModelClient{affinitySupported: false}}
	assert.Empty(t, m.BuildKVCacheAffinityInvokeKwargs(nil, "sid", "pid", 3))
}
