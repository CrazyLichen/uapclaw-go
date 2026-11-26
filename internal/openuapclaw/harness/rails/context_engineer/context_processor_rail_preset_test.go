package context_engineer

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

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
// 必须不 panic，默认路径只激活 4 个处理器（FullCompact 被过滤）。
func TestFactoryTemplatePresetMerge_NoPanicDefaultActiveSet(t *testing.T) {
	ctxCfg := loadFactoryContextEngineConfig(t)
	rail := NewContextProcessorRail(WithUserProcessors(buildFactoryUserProcessors(ctxCfg)))

	base := rail.buildPresetProcessors(nil, nil)
	// 24.2：session_memory preset 追加 SessionMemoryCompressor（共 8 个）
	if len(base) != 8 {
		t.Fatalf("base 应包含全部 8 个已知处理器（默认 4 + session_memory 4 含 SessionMemoryCompressor），实际 %d", len(base))
	}

	merged, err := MergeProcessors(base, rail.userProcessors, nil, nil)
	if err != nil {
		t.Fatalf("出厂模板合并不应报错: %v", err)
	}
	active := rail.filterActiveProcessors(merged)

	got := make(map[string]bool, len(active))
	for _, spec := range active {
		got[spec.Type] = true
	}
	want := []string{
		"MessageSummaryOffloader",
		"DialogueCompressor",
		"CurrentRoundCompressor",
		"RoundLevelCompressor",
	}
	if len(active) != len(want) {
		t.Fatalf("默认路径激活处理器数 = %d, want %d（%v）", len(active), len(want), got)
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("默认路径缺少处理器 %s", name)
		}
	}
	if got["FullCompactProcessor"] {
		t.Error("session_memory 未启用时不应激活 FullCompactProcessor")
	}
}

// TestSessionMemoryPreset_ActiveSetIsThree 开启 session memory 后只激活三件套。
func TestSessionMemoryPreset_ActiveSetIsThree(t *testing.T) {
	ctxCfg := loadFactoryContextEngineConfig(t)
	rail := NewContextProcessorRail(WithUserProcessors(buildFactoryUserProcessors(ctxCfg)))
	rail.sessionMemoryEnabled = true // 同包测试，直接置位以隔离 adapter 解析逻辑

	base := rail.buildPresetProcessors(nil, nil)
	merged, err := MergeProcessors(base, rail.userProcessors, nil, nil)
	if err != nil {
		t.Fatalf("合并不应报错: %v", err)
	}
	active := rail.filterActiveProcessors(merged)

	got := make(map[string]bool, len(active))
	for _, spec := range active {
		got[spec.Type] = true
	}
	want := []string{"ToolResultBudgetProcessor", "MicroCompactProcessor", "FullCompactProcessor"}
	if len(active) != len(want) {
		t.Fatalf("session_memory 路径激活处理器数 = %d, want %d（%v）", len(active), len(want), got)
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("session_memory 路径缺少处理器 %s", name)
		}
	}
	if got["RoundLevelCompressor"] {
		t.Error("session_memory 路径不应激活默认 preset 的 RoundLevelCompressor")
	}
}

// TestFactoryTemplatePresetMerge_ToolResultBudgetConfigFromTemplate 锁定模板接线：
// TestFactoryTemplatePresetMerge_无SessionMemory预设 上游模板已收敛，出厂模板不再
// 承载 FullCompactProcessor / ToolResultBudgetProcessor 预设段（仅在
// session_memory_config 启用时挂载）；模板映射不产生这两个 user processors。
func TestFactoryTemplatePresetMerge_无SessionMemory预设(t *testing.T) {
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

// TestFilterActiveProcessors_CustomTypeKept 自定义处理器类型不受激活集过滤。
func TestFilterActiveProcessors_CustomTypeKept(t *testing.T) {
	rail := NewContextProcessorRail(WithUserProcessors([]ceiface.ProcessorSpec{
		{Type: "MyCustomProcessor", Config: &offloader.MessageSummaryOffloaderConfig{}},
	}))
	specs := []ceiface.ProcessorSpec{
		{Type: "RoundLevelCompressor", Config: &offloader.MessageSummaryOffloaderConfig{}},
		{Type: "MyCustomProcessor", Config: &offloader.MessageSummaryOffloaderConfig{}},
	}
	active := rail.filterActiveProcessors(specs)
	if len(active) != 2 {
		t.Fatalf("激活数 = %d, want 2（自定义类型必须保留）", len(active))
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
