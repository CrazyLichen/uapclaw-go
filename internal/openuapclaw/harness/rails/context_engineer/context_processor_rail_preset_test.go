package context_engineer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	context_engine "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine"
	cecontext "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/context"
	ceiface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	forkedcompressor "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/offloader"
)

// factoryTemplatePath 返回出厂配置模板相对本包测试文件的路径。
func factoryTemplatePath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "..", "..", "resources", "config.yaml")
}

// loadFactoryContextEngineConfig 加载出厂模板的 react.context_engine_config 段。
func loadFactoryContextEngineConfig(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(factoryTemplatePath(t))
	if err != nil {
		t.Fatalf("读取出厂模板失败: %v", err)
	}
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		t.Fatalf("解析出厂模板失败: %v", err)
	}
	react, _ := root["react"].(map[string]any)
	if react == nil {
		t.Fatal("出厂模板缺少 react 段")
	}
	ctxCfg, _ := react["context_engine_config"].(map[string]any)
	if ctxCfg == nil {
		t.Fatal("出厂模板缺少 react.context_engine_config 段")
	}
	return ctxCfg
}

// buildFactoryUserProcessors 按 adapter 的映射规则把模板段转成 ProcessorSpec。
func buildFactoryUserProcessors(ctxCfg map[string]any) []ceiface.ProcessorSpec {
	specs := []struct {
		configKey string
		typeName  string
	}{
		{"message_summary_offloader_config", "MessageSummaryOffloader"},
		{"dialogue_compressor_config", "DialogueCompressor"},
		{"current_round_compressor_config", "CurrentRoundCompressor"},
		{"round_level_compressor_config", "RoundLevelCompressor"},
		{"full_compact_processor_config", "FullCompactProcessor"},
		{"tool_result_budget_processor_config", "ToolResultBudgetProcessor"},
	}
	out := make([]ceiface.ProcessorSpec, 0, len(specs))
	for _, spec := range specs {
		if m, _ := ctxCfg[spec.configKey].(map[string]any); len(m) > 0 {
			out = append(out, ceiface.ProcessorSpec{Type: spec.typeName, ConfigOverrides: m})
		}
	}
	return out
}

// TestMergeConfigWithOverrides_NilValueSkipped 锁定 S-01：
// 出厂模板里的 `messages_threshold: null` 不得再触发 reflect panic。
func TestMergeConfigWithOverrides_NilValueSkipped(t *testing.T) {
	base := &offloader.MessageSummaryOffloaderConfig{LargeMessageThreshold: 60000}
	merged := MergeConfigWithOverrides(base, map[string]any{
		"protected_tool_names":      nil,
		"customized_summary_prompt": nil,
		"large_message_threshold":   12345,
	})
	cfg, ok := merged.(*offloader.MessageSummaryOffloaderConfig)
	if !ok {
		t.Fatalf("返回类型 = %T, want *MessageSummaryOffloaderConfig", merged)
	}
	if cfg.ProtectedToolNames != nil {
		t.Error("nil 覆盖应保持字段为原值/nil")
	}
	if cfg.LargeMessageThreshold != 12345 {
		t.Errorf("LargeMessageThreshold = %d, want 12345（非 nil 覆盖必须生效）", cfg.LargeMessageThreshold)
	}
}

// TestFactoryTemplatePresetMerge_NoPanicDefaultActiveSet 用出厂模板驱动预设合并：
// 必须不 panic；24.15 单 forked preset 链（5 处理器，无两分支互斥）。
func TestFactoryTemplatePresetMerge_NoPanicDefaultActiveSet(t *testing.T) {
	ctxCfg := loadFactoryContextEngineConfig(t)
	rail := NewContextProcessorRail(WithUserProcessors(buildFactoryUserProcessors(ctxCfg)))

	base := rail.buildPresetProcessors(nil, nil)
	// 24.15：单 forked 链 Offloader + SessionMemoryCompressor + Dialogue + CurrentRound + RoundLevel
	if len(base) != 5 {
		t.Fatalf("base 应为单 forked 链 5 个处理器，实际 %d", len(base))
	}
	// SessionMemoryCompressor 默认 disabled（Python: enabled=False，用户覆盖启用）
	for _, spec := range base {
		if spec.Type == "SessionMemoryCompressor" {
			if cfg, ok := spec.Config.(*forkedcompressor.SessionMemoryCompressorConfig); !ok || cfg.Enabled {
				t.Errorf("SessionMemoryCompressor preset 应为 forked 配置且 enabled=false, got %T", spec.Config)
			}
		}
	}
	// 三压缩器配置类型应为 forked 版
	wantForked := map[string]bool{
		"DialogueCompressor": true, "CurrentRoundCompressor": true, "RoundLevelCompressor": true,
	}
	for _, spec := range base {
		if wantForked[spec.Type] {
			switch spec.Config.(type) {
			case *forkedcompressor.DialogueCompressorConfig,
				*forkedcompressor.CurrentRoundCompressorConfig,
				*forkedcompressor.RoundLevelCompressorConfig:
			default:
				t.Errorf("%s 配置应为 forked 版, got %T", spec.Type, spec.Config)
			}
		}
	}

	// 出厂模板含陈旧键（FullCompact/ToolResultBudget dict 覆盖）→ 合并报错但降级继续
	merged, mergeErr := MergeProcessors(base, rail.userProcessors, nil, nil)
	if mergeErr == nil {
		t.Fatal("陈旧 dict 覆盖应触发合并错误（对齐 PY ValueError）")
	}
	_ = mergeErr
	got := make(map[string]bool, len(merged))
	for _, spec := range merged {
		got[spec.Type] = true
	}
	for _, name := range []string{"MessageSummaryOffloader", "SessionMemoryCompressor", "DialogueCompressor", "CurrentRoundCompressor", "RoundLevelCompressor"} {
		if !got[name] {
			t.Errorf("单链缺少处理器 %s（合并结果 %v）", name, got)
		}
	}
	if got["FullCompactProcessor"] && !userOverridePresent(rail, "FullCompactProcessor") {
		t.Error("FullCompactProcessor 只能来自用户覆盖，不应在 preset 中")
	}
}

// TestFactoryTemplatePresetMerge_无SessionMemoryLegacy预设 上游模板已收敛，出厂模板
// 不再承载 FullCompactProcessor / ToolResultBudgetProcessor 预设段（仅在
// session_memory_config 启用时挂载）；模板映射不产生这两个 user processors。
func TestFactoryTemplatePresetMerge_无SessionMemoryLegacy预设(t *testing.T) {
	ctxCfg := loadFactoryContextEngineConfig(t)
	if _, ok := ctxCfg["full_compact_processor_config"]; ok {
		t.Error("出厂模板不应包含 full_compact_processor_config")
	}
	if _, ok := ctxCfg["tool_result_budget_processor_config"]; ok {
		t.Error("出厂模板不应包含 tool_result_budget_processor_config")
	}
	specs := buildFactoryUserProcessors(ctxCfg)
	for _, spec := range specs {
		switch spec.Type {
		case "FullCompactProcessor", "ToolResultBudgetProcessor":
			t.Errorf("出厂模板映射不应产生 %s", spec.Type)
		}
	}
}

// userOverridePresent 用户覆盖集中是否含指定类型
func userOverridePresent(rail *ContextProcessorRail, typeName string) bool {
	for _, spec := range rail.userProcessors {
		if spec.Type == typeName {
			return true
		}
	}
	return false
}

// TestPresetChain_IndependentOfSessionMemoryToggle 单 forked 链与 session_memory 开关无关
// （取代旧两分支互斥断言）。
func TestPresetChain_IndependentOfSessionMemoryToggle(t *testing.T) {
	rail := NewContextProcessorRail()
	off := rail.buildPresetProcessors(nil, nil)
	rail.sessionMemoryEnabled = true
	on := rail.buildPresetProcessors(nil, nil)
	if len(off) != len(on) {
		t.Errorf("开关不应改变 preset 链长度: %d vs %d", len(off), len(on))
	}
	for i := range off {
		if off[i].Type != on[i].Type {
			t.Errorf("链顺序不应随开关变化: %s vs %s", off[i].Type, on[i].Type)
		}
	}
}

// TestActivate_SwitchesFactoriesTo forked Activate 后工厂应解析为 forked 配置类型
// （Python: forked.activate() 切换 _PROCESSOR_MAP）。
func TestActivate_SwitchesFactoriesTo(t *testing.T) {
	forkedcompressor.Activate()
	for _, typeName := range []string{"SessionMemoryCompressor", "DialogueCompressor", "CurrentRoundCompressor", "RoundLevelCompressor"} {
		factory, ok := context_engine.GetProcessorFactory(typeName)
		if !ok {
			t.Fatalf("%s 工厂未注册", typeName)
		}
		cfgFactory, err := factory(nil)
		if err == nil {
			t.Errorf("%s 空配置应被拒绝（forked 工厂类型断言）", typeName)
			_ = cfgFactory
		}
	}
}

// TestFactoryTemplatePresetMerge_StaleKeysDegrade 锁定陈旧键降级语义：
// 出厂模板中的 FullCompact/ToolResultBudget dict 覆盖在单 forked 链下不再存在于
// preset，MergeProcessors 对齐 Python _merge_processors（context_processor_rail.py:176-180）
// 抛错（Go 收集为聚合错误），rail init 侧降级为 warn + 跳过违规处理器。
func TestFactoryTemplatePresetMerge_StaleKeysDegrade(t *testing.T) {
	ctxCfg := loadFactoryContextEngineConfig(t)
	rail := NewContextProcessorRail(WithUserProcessors(buildFactoryUserProcessors(ctxCfg)))

	base := rail.buildPresetProcessors(nil, nil)
	merged, err := MergeProcessors(base, rail.userProcessors, nil, nil)
	if err == nil {
		t.Fatal("陈旧 dict 覆盖应触发合并错误（对齐 PY ValueError）")
	}
	errText := err.Error()
	if !strings.Contains(errText, "FullCompactProcessor") || !strings.Contains(errText, "ToolResultBudgetProcessor") {
		t.Errorf("错误应点名陈旧类型, got: %v", errText)
	}
	// 降级：单链 5 处理器仍在合并结果中
	got := make(map[string]bool, len(merged))
	for _, spec := range merged {
		got[spec.Type] = true
	}
	for _, name := range []string{"MessageSummaryOffloader", "SessionMemoryCompressor", "DialogueCompressor", "CurrentRoundCompressor", "RoundLevelCompressor"} {
		if !got[name] {
			t.Errorf("降级后单链不应丢失 %s（结果 %v）", name, got)
		}
	}
}

// TestSessionMemoryCompressorPreset_YamlOverride 24.2：SessionMemoryCompressor 并入 preset
// —— session_memory_config 传入时内嵌 Memory 透传 + enabled 开关同时驱动 manager 创建。
func TestSessionMemoryCompressorPreset_YamlOverride(t *testing.T) {
	// 传配置 → enabled + manager 创建
	memCfg := cecontext.NewSessionMemoryConfig()
	var memCfgPtr *cecontext.SessionMemoryConfig = &memCfg
	rail := NewContextProcessorRail(WithSessionMemoryConfig(&memCfg))
	if rail.sessionMemoryMgr == nil {
		t.Fatal("传入 session_memory_config 应创建 SessionMemoryManager")
	}
	base := rail.buildPresetProcessors(nil, nil)
	var smSpec *ceiface.ProcessorSpec
	for i := range base {
		if base[i].Type == "SessionMemoryCompressor" {
			smSpec = &base[i]
			break
		}
	}
	if smSpec == nil {
		t.Fatal("preset 应含 SessionMemoryCompressor spec")
	}
	cfg, ok := smSpec.Config.(*forkedcompressor.SessionMemoryCompressorConfig)
	if !ok {
		t.Fatalf("spec 配置类型不符，实际 %T", smSpec.Config)
	}
	if cfg.Memory != memCfgPtr {
		t.Error("内嵌 Memory 应透传 rail 解析的 session_memory_config（同实例）")
	}

	// 不传配置 → spec 存在但默认 disabled
	rail2 := NewContextProcessorRail()
	base2 := rail2.buildPresetProcessors(nil, nil)
	var smCfg2 *forkedcompressor.SessionMemoryCompressorConfig
	for i := range base2 {
		if base2[i].Type == "SessionMemoryCompressor" {
			smCfg2 = base2[i].Config.(*forkedcompressor.SessionMemoryCompressorConfig)
			break
		}
	}
	if smCfg2 == nil {
		t.Fatal("未传配置时 preset 也应含 SessionMemoryCompressor（yaml 可覆盖命中）")
	}
	if smCfg2.Enabled {
		t.Error("默认应 disabled")
	}
}
