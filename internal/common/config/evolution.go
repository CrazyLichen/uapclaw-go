// Package config 内 evolution.go：技能演进开关读取。
//
// 对应 Python 代码：jiuwenswarm/common/config.py:317-337
// （_get_evolution_config / get_skill_evolution_enabled）
package config

// ──────────────────────────── 导出函数 ────────────────────────────

// EvolutionConfig 返回 react.evolution 配置段（非 dict 返回空 map）。
// Python: _get_evolution_config (config.py:317-328)
func EvolutionConfig(cfg map[string]any) map[string]any {
	if cfg == nil {
		return map[string]any{}
	}
	reactCfg, ok := cfg["react"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	evoCfg, ok := reactCfg["evolution"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return evoCfg
}

// SkillEvolutionEnabled 返回规范化的 react.evolution.skill_evolution 开关。
// Python: get_skill_evolution_enabled (config.py:335-337)
func SkillEvolutionEnabled(cfg map[string]any) bool {
	return EvolutionConfig(cfg)["skill_evolution"] == true
}
