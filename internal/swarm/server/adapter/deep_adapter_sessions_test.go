package adapter

import (
	"sync"
	"testing"
)

// TestSessionAdapterCache_增删查 会话级缓存的三个基本操作。
func TestSessionAdapterCache_增删查(t *testing.T) {
	cache := newSessionAdapterCache()

	cache.remember("s1", map[string]any{"k": "v"})
	if !cache.has("s1") {
		t.Error("记住后应存在")
	}
	if got := cache.get("s1"); got == nil {
		t.Error("应能取出")
	}
	cache.forget("s1")
	if cache.has("s1") {
		t.Error("忘记后不应存在")
	}
}

// TestSessionAdapterCache_并发安全 多协程读写不 panic 不竞态。
func TestSessionAdapterCache_并发安全(t *testing.T) {
	cache := newSessionAdapterCache()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			const sid = "s1"
			cache.remember(sid, map[string]any{"n": n})
			_ = cache.has(sid)
			_ = cache.get(sid)
			cache.touch(sid)
			cache.forget(sid)
		}(i)
	}
	wg.Wait()
}

// TestSessionAdapterCache_Cleanup 清理返回是否清到东西。
func TestSessionAdapterCache_Cleanup(t *testing.T) {
	cache := newSessionAdapterCache()
	cache.remember("s1", map[string]any{})
	if !cache.cleanup("s1") {
		t.Error("有缓存时应返回 true")
	}
	if cache.cleanup("s1") {
		t.Error("无缓存时应返回 false")
	}
}

// TestSessionAdapterCache_Count 会话数统计。
func TestSessionAdapterCache_Count(t *testing.T) {
	cache := newSessionAdapterCache()
	if cache.count() != 0 {
		t.Error("初始应为 0")
	}
	cache.remember("a", map[string]any{})
	cache.remember("b", map[string]any{})
	if cache.count() != 2 {
		t.Errorf("应为 2，实际 %d", cache.count())
	}
	cache.forget("a")
	if cache.count() != 1 {
		t.Errorf("应为 1，实际 %d", cache.count())
	}
}

// TestSessionAdapterCache_Touch不存在 对不存在的会话 touch 不创建条目。
func TestSessionAdapterCache_Touch不存在(t *testing.T) {
	cache := newSessionAdapterCache()
	cache.touch("nope")
	if cache.count() != 0 {
		t.Error("touch 不应创建条目")
	}
}

// TestDeepAdapter_CleanupSessionAdapter 清理返回是否清到东西。
func TestDeepAdapter_CleanupSessionAdapter(t *testing.T) {
	adapter := NewDeepAdapter()
	adapter.sessionAdapters.remember("s1", map[string]any{})
	if !adapter.CleanupSessionAdapter("s1") {
		t.Error("有缓存时应返回 true")
	}
	if adapter.CleanupSessionAdapter("s1") {
		t.Error("无缓存时应返回 false")
	}
}

// TestDeepAdapter_HasSessionRuntime 缓存或活跃计数任一非空即为真。
func TestDeepAdapter_HasSessionRuntime(t *testing.T) {
	adapter := NewDeepAdapter()
	if adapter.HasSessionRuntime("s1") {
		t.Error("初始不应有会话运行时")
	}

	adapter.sessionAdapters.remember("s1", map[string]any{})
	if !adapter.HasSessionRuntime("s1") {
		t.Error("有缓存时应为真")
	}
	adapter.CleanupSessionAdapter("s1")

	adapter.markSessionActive("s2")
	if !adapter.HasSessionRuntime("s2") {
		t.Error("有活跃计数时应为真")
	}
	adapter.unmarkSessionActive("s2")
	if adapter.HasSessionRuntime("s2") {
		t.Error("计数归零后应为假")
	}
}

// TestOtherActiveSessions_清理会话计数 清理会话后其他会话计数不受影响。
func TestOtherActiveSessions_清理会话计数(t *testing.T) {
	adapter := NewDeepAdapter()
	adapter.markSessionActive("s1")
	adapter.markSessionActive("s2")
	adapter.CleanupSessionAdapter("s1")
	// s1 只剩活跃计数，仍在运行时里
	if !adapter.HasSessionRuntime("s1") {
		t.Error("s1 仍有活跃计数，应为真")
	}
	if !adapter.HasSessionRuntime("s2") {
		t.Error("s2 不受影响，应为真")
	}
}
