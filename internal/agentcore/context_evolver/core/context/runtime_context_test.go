package context

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewRuntimeContext(t *testing.T) {
	rc := NewRuntimeContext()
	assert.NotNil(t, rc)
	assert.Equal(t, 0, len(rc.ToDict()))
}

func TestRuntimeContext_Set_Get(t *testing.T) {
	rc := NewRuntimeContext()
	rc.Set("user_id", "alice")
	rc.Set("count", 42)

	assert.Equal(t, "alice", rc.Get("user_id"))
	assert.Equal(t, 42, rc.Get("count"))
	assert.Nil(t, rc.Get("nonexistent"))
}

func TestRuntimeContext_GetDefault(t *testing.T) {
	rc := NewRuntimeContext()
	assert.Equal(t, "fallback", rc.GetDefault("missing", "fallback"))
	rc.Set("key", "value")
	assert.Equal(t, "value", rc.GetDefault("key", "fallback"))
}

func TestRuntimeContext_GetTyped(t *testing.T) {
	rc := NewRuntimeContext()
	rc.Set("name", "bob")
	rc.Set("score", 0.95)

	name, ok := GetTyped[string](rc, "name")
	assert.True(t, ok)
	assert.Equal(t, "bob", name)

	score, ok := GetTyped[float64](rc, "score")
	assert.True(t, ok)
	assert.InDelta(t, 0.95, score, 0.001)

	_, ok = GetTyped[int](rc, "name")
	assert.False(t, ok) // 类型不匹配

	_, ok = GetTyped[string](rc, "missing")
	assert.False(t, ok) // 键不存在
}

func TestRuntimeContext_ToDict_快照隔离(t *testing.T) {
	rc := NewRuntimeContext()
	rc.Set("k", "v1")
	snapshot := rc.ToDict()
	rc.Set("k", "v2")
	assert.Equal(t, "v1", snapshot["k"]) // 快照不受后续修改影响
}

func TestRuntimeContext_并发安全(t *testing.T) {
	rc := NewRuntimeContext()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rc.Set("key", i)
			_ = rc.Get("key")
		}(i)
	}
	wg.Wait()
	// 不 panic 即通过
}

func TestRuntimeContext_覆盖(t *testing.T) {
	rc := NewRuntimeContext()
	rc.Set("key", "v1")
	rc.Set("key", "v2")
	assert.Equal(t, "v2", rc.Get("key"))
}

func TestRuntimeContext_String(t *testing.T) {
	rc := NewRuntimeContext()
	rc.Set("k", "v")
	s := rc.String()
	assert.Contains(t, s, "RuntimeContext")
	assert.Contains(t, s, "k")
}
