package subagents

import (
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildBrowserAgentConfig 构建 browser 子代理配置。
// 对齐 Python: _build_browser_subagent_config(model, config, config_base)
// ⤵️ 9.38-49 Harness 工具集: browser 子代理具体配置待回填
func BuildBrowserAgentConfig(model *llm.Model, config map[string]any, configBase map[string]any) *hschema.SubAgentConfig {
	// ⤵️ 9.38-49: 构建完整的 browser SubAgentConfig
	// 包含：agent_card + system_prompt + tools + model + rails
	cfg := hschema.NewSubAgentConfig()
	cfg.Model = model
	return cfg
}
