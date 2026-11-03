package adapter

import (
	"sort"
	"testing"

	cerails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildContextProcessorRail_读取yaml六段 验证 adapter 把 yaml 的 processor 配置传给 rail
func TestBuildContextProcessorRail_读取yaml六段(t *testing.T) {
	d := &DeepAdapter{}
	reactCfg := map[string]any{
		"context_engine_config": map[string]any{
			"dialogue_compressor_config":          map[string]any{"tokens_threshold": 50000},
			"round_level_compressor_config":       map[string]any{"trigger_total_tokens": 120000},
			"full_compact_processor_config":       map[string]any{"trigger_total_tokens": 90000},
			"tool_result_budget_processor_config": map[string]any{"tokens_threshold": 1000},
		},
	}
	rail := d.buildContextProcessorRail(reactCfg)
	if rail == nil {
		t.Fatal("不应返回 nil")
	}
	ceRail, ok := rail.(*cerails.ContextProcessorRail)
	if !ok {
		t.Fatalf("类型应为 *ContextProcessorRail，实际 %T", rail)
	}
	names := ceRail.UserProcessorTypes()
	sort.Strings(names)
	want := []string{"DialogueCompressor", "FullCompactProcessor", "RoundLevelCompressor", "ToolResultBudgetProcessor"}
	if len(names) != len(want) {
		t.Fatalf("应为 %v，实际 %v", want, names)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("第 %d 项应为 %s，实际 %s", i, n, names[i])
		}
	}
}

// TestBuildContextProcessorRail_无配置返回preset 验证没有 yaml 段时不传 user processors
func TestBuildContextProcessorRail_无配置返回preset(t *testing.T) {
	d := &DeepAdapter{}
	rail := d.buildContextProcessorRail(map[string]any{})
	ceRail, ok := rail.(*cerails.ContextProcessorRail)
	if !ok {
		t.Fatalf("类型应为 *ContextProcessorRail，实际 %T", rail)
	}
	if got := ceRail.UserProcessorTypes(); len(got) != 0 {
		t.Errorf("无 yaml 配置时不应有 user processors，实际 %v", got)
	}
}
