package memory

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewMemoryRail_默认值 测试默认值和优先级
func TestNewMemoryRail_默认值(t *testing.T) {
	embCfg := &embedding.EmbeddingConfig{}
	rail := NewMemoryRail(embCfg, true)

	if rail.Priority() != memoryRailPriority {
		t.Errorf("Priority() = %d, want %d", rail.Priority(), memoryRailPriority)
	}
	if rail.embeddingConfig != embCfg {
		t.Error("embeddingConfig 未设置")
	}
	if !rail.isProactive {
		t.Error("isProactive 应为 true")
	}
	if rail.initialized {
		t.Error("initialized 应为 false")
	}
	if rail.managerInitialized {
		t.Error("managerInitialized 应为 false")
	}
	if len(rail.ownedToolNames) != 0 {
		t.Error("ownedToolNames 应为空")
	}
	if len(rail.ownedToolIDs) != 0 {
		t.Error("ownedToolIDs 应为空")
	}
}

// TestNewMemoryRail_非主动模式 测试非主动模式
func TestNewMemoryRail_非主动模式(t *testing.T) {
	rail := NewMemoryRail(nil, false)
	if rail.isProactive {
		t.Error("isProactive 应为 false")
	}
}

// TestMemoryRail_Priority 测试优先级
func TestMemoryRail_Priority(t *testing.T) {
	rail := NewMemoryRail(nil, true)
	if rail.Priority() != 80 {
		t.Errorf("Priority() = %d, want 80", rail.Priority())
	}
}

// TestMemoryRailSetToSortedSlice 测试 set 到排序切片的转换
func TestMemoryRailSetToSortedSlice(t *testing.T) {
	s := map[string]struct{}{
		"memory_search": {},
		"memory_get":    {},
		"memory_write":  {},
	}
	result := memorySetToSortedSlice(s)
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	if result[0] != "memory_get" {
		t.Errorf("result[0] = %q, want %q", result[0], "memory_get")
	}
	if result[1] != "memory_search" {
		t.Errorf("result[1] = %q, want %q", result[1], "memory_search")
	}
	if result[2] != "memory_write" {
		t.Errorf("result[2] = %q, want %q", result[2], "memory_write")
	}
}
