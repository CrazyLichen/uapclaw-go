package prompts

import (
	"context"
	"sync/atomic"
	"testing"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// TestMtimeSectionCache_首次刷新 测试首次刷新调用 fetchAndBuild
func TestMtimeSectionCache_首次刷新(t *testing.T) {
	var fetchCount int32
	cache := NewMtimeSectionCache(
		func(ctx context.Context) int64 { return 1 },
		func(ctx context.Context) *saprompt.PromptSection {
			atomic.AddInt32(&fetchCount, 1)
			s := saprompt.NewPromptSection("test", map[string]string{"cn": "hello"}, 10)
			return &s
		},
	)
	result := cache.Refresh(context.Background())
	if result == nil {
		t.Fatal("Refresh returned nil")
	}
	if atomic.LoadInt32(&fetchCount) != 1 {
		t.Fatalf("expected 1 fetch, got %d", fetchCount)
	}
}

// TestMtimeSectionCache_缓存命中 测试 mtime 不变时返回缓存
func TestMtimeSectionCache_缓存命中(t *testing.T) {
	var fetchCount int32
	mtime := int64(1)
	cache := NewMtimeSectionCache(
		func(ctx context.Context) int64 { return mtime },
		func(ctx context.Context) *saprompt.PromptSection {
			atomic.AddInt32(&fetchCount, 1)
			s := saprompt.NewPromptSection("test", map[string]string{"cn": "hello"}, 10)
			return &s
		},
	)
	cache.Refresh(context.Background()) // 首次
	cache.Refresh(context.Background()) // 缓存命中
	if atomic.LoadInt32(&fetchCount) != 1 {
		t.Fatalf("expected 1 fetch (cached), got %d", fetchCount)
	}
}

// TestMtimeSectionCache_mtime变化刷新 测试 mtime 变化时重新拉取
func TestMtimeSectionCache_mtime变化刷新(t *testing.T) {
	var fetchCount int32
	mtime := int64(1)
	cache := NewMtimeSectionCache(
		func(ctx context.Context) int64 { return atomic.LoadInt64(&mtime) },
		func(ctx context.Context) *saprompt.PromptSection {
			atomic.AddInt32(&fetchCount, 1)
			s := saprompt.NewPromptSection("test", map[string]string{"cn": "hello"}, 10)
			return &s
		},
	)
	cache.Refresh(context.Background()) // 首次
	atomic.StoreInt64(&mtime, 2)
	cache.Refresh(context.Background()) // mtime 变化，重新拉取
	if atomic.LoadInt32(&fetchCount) != 2 {
		t.Fatalf("expected 2 fetches (mtime changed), got %d", fetchCount)
	}
}

// TestMtimeSectionCache_Invalidate 测试强制失效
func TestMtimeSectionCache_Invalidate(t *testing.T) {
	var fetchCount int32
	cache := NewMtimeSectionCache(
		func(ctx context.Context) int64 { return 1 },
		func(ctx context.Context) *saprompt.PromptSection {
			atomic.AddInt32(&fetchCount, 1)
			s := saprompt.NewPromptSection("test", map[string]string{"cn": "hello"}, 10)
			return &s
		},
	)
	cache.Refresh(context.Background()) // 首次
	cache.Invalidate()
	cache.Refresh(context.Background()) // Invalidate 后重新拉取
	if atomic.LoadInt32(&fetchCount) != 2 {
		t.Fatalf("expected 2 fetches (after invalidate), got %d", fetchCount)
	}
}

// TestMtimeSectionCache_fetchAndBuild返回nil 测试 fetchAndBuild 返回 nil 的情况
func TestMtimeSectionCache_fetchAndBuild返回nil(t *testing.T) {
	cache := NewMtimeSectionCache(
		func(ctx context.Context) int64 { return 1 },
		func(ctx context.Context) *saprompt.PromptSection { return nil },
	)
	result := cache.Refresh(context.Background())
	if result != nil {
		t.Fatal("expected nil when fetchAndBuild returns nil")
	}
	// 再次调用应返回缓存的 nil（mtime 不变）
	result2 := cache.Refresh(context.Background())
	if result2 != nil {
		t.Fatal("expected cached nil on second call")
	}
}
