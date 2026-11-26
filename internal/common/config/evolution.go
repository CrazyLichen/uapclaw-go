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

// EvolutionReviewFeedbackMinConfidence 返回审查器驱动演进的最低置信度。
// Python: get_evolution_review_feedback_min_confidence (config.py:514-521)：
// 缺省 0.7，钳制 [0,1]，非法值回落 0.7。
func EvolutionReviewFeedbackMinConfidence(cfg map[string]any) float64 {
	raw, ok := EvolutionConfig(cfg)["review_feedback_min_confidence"]
	if !ok {
		return 0.7
	}
	switch v := raw.(type) {
	case float64:
		if v < 0.0 {
			return 0.0
		}
		if v > 1.0 {
			return 1.0
		}
		return v
	case int:
		f := float64(v)
		if f < 0.0 {
			return 0.0
		}
		if f > 1.0 {
			return 1.0
		}
		return f
	default:
		return 0.7
	}
}

// EvolutionAutoSaveEnabled 返回规范化的 react.evolution.auto_save 开关
// （纯配置读取，不落盘不读 env）。
// Python: get_evolution_auto_save_enabled (config.py:524-526)
func EvolutionAutoSaveEnabled(cfg map[string]any) bool {
	return EvolutionConfig(cfg)["auto_save"] == true
}
