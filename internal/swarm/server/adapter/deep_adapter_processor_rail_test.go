package adapter

import (
	"path/filepath"
	"sort"
	"testing"

	cerails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
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

// TestBuildContextProcessorRail_出厂模板段全部接线 验证 resources/config.yaml 里的每个
// processor 段都能被 adapter 映射为 user processor：往模板加段却忘了加映射会在此失败。
func TestBuildContextProcessorRail_出厂模板段全部接线(t *testing.T) {
	cfg, err := config.New(filepath.Join("..", "..", "..", "..", "resources", "config.yaml"))
	if err != nil {
		t.Fatalf("构造配置对象失败: %v", err)
	}
	data, err := cfg.Load()
	if err != nil {
		t.Fatalf("加载出厂模板失败: %v", err)
	}
	reactCfg, _ := data["react"].(map[string]any)
	if reactCfg == nil {
		t.Fatal("出厂模板缺少 react 段")
	}

	d := &DeepAdapter{}
	ceRail, ok := d.buildContextProcessorRail(reactCfg).(*cerails.ContextProcessorRail)
	if !ok {
		t.Fatalf("类型应为 *ContextProcessorRail")
	}

	names := ceRail.UserProcessorTypes()
	sort.Strings(names)
	want := []string{
		"CurrentRoundCompressor",
		"DialogueCompressor",
		"FullCompactProcessor",
		"MessageSummaryOffloader",
		"RoundLevelCompressor",
		"ToolResultBudgetProcessor",
	}
	if len(names) != len(want) {
		t.Fatalf("模板段映射 = %v, want %v", names, want)
	}
	for i, name := range want {
		if names[i] != name {
			t.Errorf("第 %d 项 = %s, want %s", i, names[i], name)
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
