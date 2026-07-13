package subagents

import (
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildExploreAgentConfig 构建 explore 子代理配置。
// 对齐 Python: _build_explore_subagent_config(model, config, config_base)
// ⤵️ 9.38-49 Harness 工具集: explore 子代理具体配置待回填
func BuildExploreAgentConfig(model *llm.Model, config map[string]any, configBase map[string]any) *hschema.SubAgentConfig {
	// ⤵️ 9.38-49: 构建完整的 explore SubAgentConfig
	// 包含：agent_card + system_prompt + tools + model + rails
	cfg := hschema.NewSubAgentConfig()
	cfg.Model = model
	return cfg
}
