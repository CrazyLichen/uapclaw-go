package prompts

import (
	"context"
	"sync"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MtimeSectionCache 基于 mtime 探针的 PromptSection 缓存。
// Python: MtimeSectionCache (openjiuwen/agent_teams/prompts/section_cache.py)
//
// 探针（probe）返回单调递增整数（DB 的 updated_at），值不变则跳过 fetchAndBuild。
// Go 用 sync.Mutex 替代 Python 的 asyncio 锁。
type MtimeSectionCache struct {
	// probe 探针函数，返回 DB 的 updated_at 时间戳
	probe func(ctx context.Context) int64
	// fetchAndBuild 全量拉取+构建函数
	fetchAndBuild func(ctx context.Context) *saprompt.PromptSection
	// cached 缓存的 PromptSection
	cached *saprompt.PromptSection
	// cachedMtime 缓存时的 mtime 值
	cachedMtime int64
	// initialized 是否已初始化
	initialized bool
	// mu 互斥锁
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMtimeSectionCache 创建 mtime 探针缓存。
// Python: MtimeSectionCache(probe, fetch_and_build)
func NewMtimeSectionCache(
	probe func(ctx context.Context) int64,
	fetchAndBuild func(ctx context.Context) *saprompt.PromptSection,
) *MtimeSectionCache {
	return &MtimeSectionCache{
		probe:         probe,
		fetchAndBuild: fetchAndBuild,
	}
}

// Refresh 刷新缓存：探针值不变返回缓存，变化则重新 fetchAndBuild。
// Python: async refresh() -> Optional[PromptSection]
func (c *MtimeSectionCache) Refresh(ctx context.Context) *saprompt.PromptSection {
	c.mu.Lock()
	defer c.mu.Unlock()

	currentMtime := c.probe(ctx)
	if c.initialized && c.cachedMtime == currentMtime {
		return c.cached
	}

	section := c.fetchAndBuild(ctx)
	c.cached = section
	c.cachedMtime = currentMtime
	c.initialized = true
	return section
}

// Invalidate 强制下次 Refresh 重新拉取。
// Python: invalidate()
func (c *MtimeSectionCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 对齐 Python: _cached_section = None, _cached_mtime = 0, _initialized = False
	c.cached = nil
	c.cachedMtime = 0
	c.initialized = false
}

// ──────────────────────────── 非导出函数 ────────────────────────────
